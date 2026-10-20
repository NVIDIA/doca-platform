/*
Copyright 2026 NVIDIA

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package client

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	"github.com/go-resty/resty/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// defaultCredRefreshInterval coalesces mounted key-pair re-reads across all acquisitions, so the
// detection window for a mounted-file change is one interval of active acquisition.
const defaultCredRefreshInterval = time.Second

// bfFamily is the BlueField generation a client's material is selected for. familyUnknown is a
// distinct cache identity that never aliases a known family: the family selects which mounted key
// pair the client authenticates with, so a wrong-family hit would use the wrong certificate.
type bfFamily string

const (
	familyUnknown bfFamily = ""
	familyBF3     bfFamily = "bf3"
	familyBF4     bfFamily = "bf4"
)

// familyFor maps a discovered or recorded BF4 flag onto a family.
func familyFor(isBF4 bool) bfFamily {
	if isBF4 {
		return familyBF4
	}
	return familyBF3
}

// isBF4 reports whether the family is BlueField-4.
func (f bfFamily) isBF4() bool { return f == familyBF4 }

// cacheKey identifies one cached mTLS client. It carries no credential material: rotation is
// expressed by the generation recorded alongside the entry, so obsolete credentials can never be
// reached by matching a key.
type cacheKey struct {
	// Endpoint is the BMC address, "https://host:port".
	Endpoint string
	// Namespace scopes the CA trust bundle and credential Secrets. Namespaces never share clients.
	Namespace string
	// Family selects the mounted client key pair.
	Family bfFamily
	// DeviceUID is the owning DPUDevice UID when known. A device replaced at the same address
	// has a different UID and therefore never gets its predecessor's client.
	DeviceUID string
}

// keyPair is one family's mounted client identity, or the reason it is unusable.
type keyPair struct {
	cert   tls.Certificate
	digest string
	err    error
}

// fileCreds is the process-global mounted client material. The mounted volume is not namespaced,
// so one snapshot serves every endpoint and its re-read is coalesced across all acquisitions.
type fileCreds struct {
	pairs map[bfFamily]keyPair
	// fingerprint changes whenever any family's effective material or usability changes.
	fingerprint string
}

// caCreds is one namespace's trust bundle as observed through the manager's ConfigMap informer.
type caCreds struct {
	pem     []byte
	pool    *x509.CertPool
	version string // resourceVersion: skips reparsing an unchanged object
	digest  string // content digest: identifies the effective material
}

// credGeneration identifies the exact material a client was built from, so a client built from
// rotated material can never be returned by a key match alone. It carries the bundle's content
// digest, not its resourceVersion, so a ConfigMap write that leaves the bundle unchanged does not
// reconnect the fleet.
type credGeneration struct {
	files    string
	caDigest string
}

type entry struct {
	client *Client
	gen    credGeneration
}

// ClientProvider is what controllers need from a Redfish client provider. Controllers depend on it
// rather than on *Provider so tests can inject their own implementation.
type ClientProvider interface {
	AcquireForDevice(ctx context.Context, namespace string, dpuDevice *provisioningv1.DPUDevice) (*Client, error)
	RetireEndpoint(endpoint string)
}

var _ ClientProvider = (*Provider)(nil)

// Provider caches one verified mTLS Redfish client per cacheKey across reconciles, so an unchanged
// device does not pay a root probe, a CA read and a TLS handshake on every reconcile.
//
// Clients are immutable and safe for concurrent use; callers never close them. A client is replaced
// when its credential generation changes and dropped on RetireEndpoint, at which point its idle
// connections are closed. CloseIdleConnections never interrupts a request in flight, so no
// reference counting is needed. Idle sockets of cached clients also expire on the transport's own
// idle timeout.
//
// The provider is process-local. It is exclusive only because the provisioning manager runs BMC
// work on the elected leader; it is not a cross-replica guarantee.
type Provider struct {
	// caReader is the manager's cache-backed reader. The provisioning manager already runs a
	// ConfigMap informer (the DPUDevice controller watches the trust bundle), so observing the
	// bundle through it adds no informer and no RBAC.
	caReader        client.Reader
	certDir         string
	refreshInterval time.Duration
	now             func() time.Time

	// mu guards the fields below. It is held across the mounted-file refresh, which reads two
	// small key pairs at most once per refreshInterval, and across the trust-bundle read from the
	// manager cache.
	mu          sync.Mutex
	files       *fileCreds
	lastRefresh time.Time
	caByNS      map[string]*caCreds
	entries     map[cacheKey]*entry
	// retired counts RetireEndpoint calls per endpoint, so a build that started before a
	// retirement is never cached after it.
	retired map[string]uint64
}

// NewProvider returns the provider owned by provisioning-manager startup. caReader must be the
// manager's cache-backed reader so trust bundle changes are observed rather than polled.
func NewProvider(caReader client.Reader) *Provider {
	return &Provider{
		caReader:        caReader,
		certDir:         getClientCertDir(),
		refreshInterval: defaultCredRefreshInterval,
		now:             time.Now,
		caByNS:          map[string]*caCreds{},
		entries:         map[cacheKey]*entry{},
		retired:         map[string]uint64{},
	}
}

// acquire returns the verified mTLS client for key, building it if the cached one is missing or
// was built from material that has since changed. ctx bounds this call and any build it does; it
// is never retained on the client.
//
// A client whose material was superseded, or whose endpoint was retired, while it was being built
// is not cached, but is still returned: it was valid when the caller asked, and its idle sockets
// expire on their own.
func (p *Provider) acquire(ctx context.Context, key cacheKey) (*Client, error) {
	if key.Endpoint == "" {
		return nil, errors.New("acquire: empty BMC endpoint")
	}
	// Credential freshness is established before a client is returned, and fails closed: there is
	// no stale-on-error grace period.
	files, err := p.refreshFileCreds(ctx)
	if err != nil {
		return nil, err
	}
	ca, err := p.readCACreds(ctx, key.Namespace)
	if err != nil {
		return nil, err
	}
	gen := credGeneration{files: files.fingerprint, caDigest: ca.digest}

	p.mu.Lock()
	if e, ok := p.entries[key]; ok && e.gen == gen {
		p.mu.Unlock()
		return e.client, nil
	}
	epoch := p.retired[key.Endpoint]
	p.mu.Unlock()

	cl, err := p.buildClient(ctx, key, ca, files)
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	var replaced *Client
	cur := credGeneration{files: p.files.fingerprint, caDigest: p.caByNS[key.Namespace].digest}
	if cur == gen && p.retired[key.Endpoint] == epoch {
		if old, ok := p.entries[key]; ok {
			replaced = old.client
		}
		p.entries[key] = &entry{client: cl, gen: gen}
	}
	p.mu.Unlock()
	if replaced != nil {
		replaced.CloseIdleConnections()
	}
	return cl, nil
}

// buildClient performs the construction I/O: family discovery when the caller does not know it,
// then the verified mTLS client. Root/family discovery happens once per cached client rather than
// on every reconcile.
func (p *Provider) buildClient(ctx context.Context, key cacheKey, ca *caCreds, files *fileCreds) (*Client, error) {
	family := key.Family
	if family == familyUnknown {
		isBF4, err := probeFamily(ctx, key.Endpoint)
		if err != nil {
			return nil, tlsClientError(key.Endpoint, err)
		}
		family = familyFor(isBF4)
	}
	pair := files.pairs[family]
	if pair.err != nil {
		return nil, tlsClientError(key.Endpoint, fmt.Errorf("client key pair for %s is unusable: %w", family, pair.err))
	}
	if err := verifyClientKeyPairChainsToCA(pair.cert, ca.pem); err != nil {
		return nil, tlsClientError(key.Endpoint, err)
	}
	return newVerifiedTLSClient(key.Endpoint, ca.pool, pair.cert, family.isBF4())
}

// probeFamily reads the unauthenticated root service to determine the BlueField generation.
func probeFamily(ctx context.Context, endpoint string) (bool, error) {
	rawClient, err := NewRawClient(endpoint)
	if err != nil {
		return false, fmt.Errorf("failed to create raw client: %w", err)
	}
	defer rawClient.CloseIdleConnections()
	_, rootServiceInfo, err := rawClient.GetRootService(ctx)
	if err != nil {
		return false, err
	}
	return rootServiceInfo != nil && rootServiceInfo.IsBF4(), nil
}

// RetireEndpoint drops every cached client at an endpoint regardless of family or device UID and
// closes their idle connections. Use it when the identity behind an address is what changed.
func (p *Provider) RetireEndpoint(endpoint string) {
	p.mu.Lock()
	p.retired[endpoint]++
	var closing []*Client
	for k, e := range p.entries {
		if k.Endpoint == endpoint {
			delete(p.entries, k)
			closing = append(closing, e.client)
		}
	}
	p.mu.Unlock()
	for _, c := range closing {
		c.CloseIdleConnections()
	}
}

// refreshFileCreds re-reads the mounted client key pairs at most once per interval across all
// acquisitions: the mounted volume is shared, so this is not a disk read per endpoint or per
// acquisition. Validity is re-checked even when the bytes are unchanged, so an expiring
// certificate invalidates on time instead of when it happens to be rewritten.
func (p *Provider) refreshFileCreds(ctx context.Context) (*fileCreds, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := p.now()
	if p.files != nil && now.Sub(p.lastRefresh) < p.refreshInterval {
		return p.files, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, requestError(nil, err)
	}
	next := p.loadFileCreds()
	p.lastRefresh = now
	// Unchanged and still valid: keep the parsed material rather than reparsing per endpoint.
	if p.files == nil || p.files.fingerprint != next.fingerprint {
		p.files = next
	}
	return p.files, nil
}

// loadFileCreds reads every family's mounted material and publishes it as one complete snapshot.
func (p *Provider) loadFileCreds() *fileCreds {
	fc := &fileCreds{pairs: map[bfFamily]keyPair{}}
	parts := make([]string, 0, 2)
	for _, family := range []bfFamily{familyBF3, familyBF4} {
		kp := p.loadKeyPair(family)
		fc.pairs[family] = kp
		state := kp.digest
		if kp.err != nil {
			state = "err:" + kp.err.Error()
		}
		parts = append(parts, string(family)+"="+state)
	}
	fc.fingerprint = strings.Join(parts, ";")
	return fc
}

// loadKeyPair reads and validates one family's mounted key pair. A pair that does not load, for
// example because the projected volume was replaced between the two reads, fails that family's
// builds closed until the next refresh reads a consistent pair.
func (p *Provider) loadKeyPair(family bfFamily) keyPair {
	if p.certDir == "" {
		return keyPair{err: errors.New("no Redfish client certificate directory configured")}
	}
	dir := p.certDir
	if family.isBF4() {
		dir += "-bf4"
	}
	certFile := filepath.Join(dir, corev1.TLSCertKey)
	keyFile := filepath.Join(dir, corev1.TLSPrivateKeyKey)
	certPEM, err := os.ReadFile(certFile)
	if err != nil {
		return keyPair{err: fmt.Errorf("failed to read client cert %s: %w", certFile, err)}
	}
	keyPEM, err := os.ReadFile(keyFile)
	if err != nil {
		return keyPair{err: fmt.Errorf("failed to read client key %s: %w", keyFile, err)}
	}
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return keyPair{err: fmt.Errorf("failed to load client key pair from %s: %w", dir, err)}
	}
	if err := checkKeyPairValidity(pair, p.now()); err != nil {
		// Stale, not merely unreadable: only cert-manager reissuance fixes it.
		return keyPair{err: fmt.Errorf("%w: client key pair in %s: %v", ErrRedfishClientCertStale, dir, err)}
	}
	sum := sha256.Sum256(append(append([]byte{}, certPEM...), keyPEM...))
	return keyPair{cert: pair, digest: hex.EncodeToString(sum[:])}
}

// checkKeyPairValidity rejects material that is expired or not yet valid. It runs on every
// refresh, including when the bytes are unchanged, so expiry invalidates on time.
func checkKeyPairValidity(pair tls.Certificate, now time.Time) error {
	leaf := pair.Leaf
	if leaf == nil {
		if len(pair.Certificate) == 0 {
			return errors.New("no certificate in key pair")
		}
		parsed, err := x509.ParseCertificate(pair.Certificate[0])
		if err != nil {
			return fmt.Errorf("failed to parse client certificate: %w", err)
		}
		leaf = parsed
	}
	if now.Before(leaf.NotBefore) {
		return fmt.Errorf("client certificate is not valid until %s", leaf.NotBefore.UTC().Format(time.RFC3339))
	}
	if now.After(leaf.NotAfter) {
		return fmt.Errorf("client certificate expired at %s", leaf.NotAfter.UTC().Format(time.RFC3339))
	}
	return nil
}

// readCACreds observes the namespace trust bundle through the manager's ConfigMap informer. The
// resourceVersion skips reparsing an unchanged object and the digest identifies the effective
// material. A missing or empty bundle fails the acquisition closed.
//
// The read happens under mu, so concurrent callers store bundles in the order they read them: an
// older read can never overwrite a newer one. The read is an in-memory cache lookup.
func (p *Provider) readCACreds(ctx context.Context, namespace string) (*caCreds, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	name := CATrustBundleConfigMapName()
	cm := &corev1.ConfigMap{}
	if err := p.caReader.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, cm); err != nil {
		return nil, fmt.Errorf("failed to get ConfigMap %q: %w", name, err)
	}
	if cached := p.caByNS[namespace]; cached != nil && cached.version == cm.ResourceVersion {
		return cached, nil
	}

	bundle := []byte(cm.Data[CATrustBundleKey])
	if len(bundle) == 0 {
		return nil, fmt.Errorf("no %q in ConfigMap %q", CATrustBundleKey, name)
	}
	pool, err := CAPoolFromBundle(bundle)
	if err != nil {
		return nil, fmt.Errorf("%w from ConfigMap %q", err, name)
	}
	sum := sha256.Sum256(bundle)
	out := &caCreds{pem: bundle, pool: pool, version: cm.ResourceVersion, digest: hex.EncodeToString(sum[:])}
	p.caByNS[namespace] = out
	return out, nil
}

// newVerifiedTLSClient builds the verified mTLS client from already-resolved material.
//
// The returned client is immutable: nothing writes its auth, TLS or header settings afterwards,
// which is what makes it safe to share between concurrent callers.
func newVerifiedTLSClient(endpoint string, caPool *x509.CertPool, pair tls.Certificate, isBF4 bool) (*Client, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to parse BMC address %q: %w", endpoint, err)
	}
	tlsCfg, err := newRedfishTLSConfig(caPool, []tls.Certificate{pair}, false, u.Hostname())
	if err != nil {
		return nil, err
	}
	rc := resty.New().SetBaseURL(endpoint).SetTLSClientConfig(tlsCfg)
	// As in read(): the BMC decides mTLS identity at handshake, so drop a connection it rejected.
	rc.OnAfterResponse(func(c *resty.Client, resp *resty.Response) error {
		if resp.StatusCode() == http.StatusUnauthorized {
			c.GetClient().CloseIdleConnections()
		}
		return nil
	})
	return &Client{
		Client: rc,
		IsBF4:  isBF4,
	}, nil
}

// deviceKey builds the cache identity for a DPUDevice: its BMC address, the owning namespace, the
// BlueField family once status records it, and the device UID so a device replaced at the same
// address never gets its predecessor's client.
//
// A device whose family is not yet recorded gets familyUnknown, which is its own cache identity.
// It never aliases a BF3 or BF4 entry, because the family selects the client certificate.
func deviceKey(namespace string, dpuDevice *provisioningv1.DPUDevice) (cacheKey, error) {
	address := dpuDevice.BMCAddress()
	if address == "" {
		return cacheKey{}, errors.New("empty BMC address")
	}
	family := familyUnknown
	switch dpuDevice.Status.DPUType {
	case provisioningv1.DPUTypeBlueField4:
		family = familyBF4
	case provisioningv1.DPUTypeBlueField3:
		family = familyBF3
	}
	return cacheKey{
		Endpoint:  address,
		Namespace: namespace,
		Family:    family,
		DeviceUID: string(dpuDevice.UID),
	}, nil
}

// AcquireForDevice returns the verified mTLS client for a DPUDevice's BMC.
func (p *Provider) AcquireForDevice(ctx context.Context, namespace string, dpuDevice *provisioningv1.DPUDevice) (*Client, error) {
	key, err := deviceKey(namespace, dpuDevice)
	if err != nil {
		return nil, err
	}
	return p.acquire(ctx, key)
}
