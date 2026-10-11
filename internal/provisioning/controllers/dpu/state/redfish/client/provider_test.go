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
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// bf3Key is the cache key of the test device at endpoint.
func bf3Key(endpoint string) cacheKey {
	return cacheKey{
		Endpoint:  endpoint,
		Namespace: providerNamespace,
		Family:    familyBF3,
		DeviceUID: "uid-1",
	}
}

// acquire fails the test on error.
func acquire(t *testing.T, p *Provider, key cacheKey) *Client {
	t.Helper()
	c, err := p.acquire(context.Background(), key)
	if err != nil {
		t.Fatalf("acquire %v: %v", key, err)
	}
	return c
}

// cachedEntries returns the number of cached clients.
func cachedEntries(p *Provider) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.entries)
}

// rotateCA appends a new CA to the namespace trust bundle, as a CA rotation does.
func rotateCA(t *testing.T, k8s client.Client, ca *ca) {
	t.Helper()
	rotated := newCA(t, "rotated-ca")
	cm := &corev1.ConfigMap{}
	if err := k8s.Get(context.Background(), types.NamespacedName{Name: CATrustBundleConfigMap, Namespace: providerNamespace}, cm); err != nil {
		t.Fatal(err)
	}
	patch := client.MergeFrom(cm.DeepCopy())
	cm.Data[CATrustBundleKey] = string(append(append([]byte{}, ca.pem...), rotated.pem...))
	if err := k8s.Patch(context.Background(), cm, patch); err != nil {
		t.Fatal(err)
	}
}

// Ten reconciles of an unchanged device must reuse one client, not build ten.
func TestRepeatedAcquireConstructsOnceAndReusesConnections(t *testing.T) {
	ca := newCA(t, "test-ca")
	certDir(t, ca)
	server := newBMC(t, ca, nil)
	p, _ := newTestProvider(t, caConfigMap(providerNamespace, ca.pem))
	key := bf3Key(server.URL)

	var first *Client
	for i := 0; i < 10; i++ {
		c := acquire(t, p, key)
		if first == nil {
			first = c
		} else if c != first {
			t.Fatalf("acquire %d returned a different client", i)
		}
		if _, _, err := c.GetRootService(context.Background()); err != nil {
			t.Fatalf("read %d: %v", i, err)
		}
	}

	if got := server.connections(); got != 1 {
		t.Errorf("connections = %d, want 1: the cached client should reuse its transport", got)
	}
	if got := cachedEntries(p); got != 1 {
		t.Errorf("entries = %d, want 1", got)
	}
}

// A known family skips root discovery entirely; an unknown one discovers once per cached client.
func TestRootDiscoveryHappensOncePerGeneration(t *testing.T) {
	ca := newCA(t, "test-ca")
	certDir(t, ca)
	server := newBMC(t, ca, nil)
	p, _ := newTestProvider(t, caConfigMap(providerNamespace, ca.pem))

	known := bf3Key(server.URL)
	for i := 0; i < 5; i++ {
		acquire(t, p, known)
	}
	if got := server.count("/" + APIRootService); got != 0 {
		t.Errorf("root reads = %d, want 0: a recorded family needs no discovery", got)
	}

	unknown := known
	unknown.Family = familyUnknown
	for i := 0; i < 5; i++ {
		acquire(t, p, unknown)
	}
	if got := server.count("/" + APIRootService); got != 1 {
		t.Errorf("root reads = %d, want 1: discovery belongs to the cached client, not the read", got)
	}
}

// Mounted material is read once per refresh interval for the whole fleet, and unchanged bytes keep
// the parsed snapshot rather than reparsing.
func TestCredentialRefreshIsCoalescedAndParsesOnlyChangedMaterial(t *testing.T) {
	ca := newCA(t, "test-ca")
	dir := certDir(t, ca)
	server := newBMC(t, ca, nil)
	p, _ := newTestProvider(t, caConfigMap(providerNamespace, ca.pem))

	now := time.Now()
	p.now = func() time.Time { return now }
	p.refreshInterval = time.Second

	acquire(t, p, bf3Key(server.URL))
	first := p.files

	// Within the interval the snapshot is not even re-read.
	for i := 0; i < 5; i++ {
		acquire(t, p, bf3Key(server.URL))
	}
	if p.files != first {
		t.Error("snapshot replaced inside the refresh interval")
	}

	// Past the interval with unchanged bytes: re-read, but the parsed material is kept.
	now = now.Add(2 * time.Second)
	acquire(t, p, bf3Key(server.URL))
	if p.files != first {
		t.Error("unchanged material was reparsed into a new snapshot")
	}

	// Changed bytes publish a new snapshot.
	certPEM, keyPEM := ca.clientPair(t)
	writePair(t, dir, certPEM, keyPEM)
	now = now.Add(2 * time.Second)
	acquire(t, p, bf3Key(server.URL))
	if p.files == first {
		t.Error("rotated material did not publish a new snapshot")
	}
}

// Distinct namespaces, device UIDs, families and endpoints never share a client.
func TestPoolIdentityIsolation(t *testing.T) {
	ca := newCA(t, "test-ca")
	certDir(t, ca)
	server := newBMC(t, ca, nil)
	other := newBMC(t, ca, nil)
	p, _ := newTestProvider(t,
		caConfigMap(providerNamespace, ca.pem),
		caConfigMap("other-namespace", ca.pem))

	base := bf3Key(server.URL)
	variants := map[string]cacheKey{"base": base}
	for name, mutate := range map[string]func(cacheKey) cacheKey{
		"namespace": func(k cacheKey) cacheKey { k.Namespace = "other-namespace"; return k },
		"family":    func(k cacheKey) cacheKey { k.Family = familyBF4; return k },
		"unknown":   func(k cacheKey) cacheKey { k.Family = familyUnknown; return k },
		"device":    func(k cacheKey) cacheKey { k.DeviceUID = "uid-2"; return k },
		"endpoint":  func(k cacheKey) cacheKey { k.Endpoint = other.URL; return k },
	} {
		variants[name] = mutate(base)
	}

	seen := map[*Client]string{}
	for name, key := range variants {
		c := acquire(t, p, key)
		if previous, ok := seen[c]; ok {
			t.Errorf("%q shares a client with %q", name, previous)
		}
		seen[c] = name
	}
	if len(seen) != len(variants) {
		t.Errorf("got %d distinct clients, want %d", len(seen), len(variants))
	}
}

// A device replaced at the same address does not inherit its predecessor's client, and retiring
// the endpoint drops every client keyed under it.
func TestReplacementAtSameAddressGetsANewClient(t *testing.T) {
	ca := newCA(t, "test-ca")
	certDir(t, ca)
	server := newBMC(t, ca, nil)
	p, _ := newTestProvider(t, caConfigMap(providerNamespace, ca.pem))

	old := bf3Key(server.URL)
	oldClient := acquire(t, p, old)

	replacement := old
	replacement.DeviceUID = "uid-2"
	if acquire(t, p, replacement) == oldClient {
		t.Error("replacement got the previous device's client")
	}

	p.RetireEndpoint(old.Endpoint)
	if got := cachedEntries(p); got != 0 {
		t.Errorf("entries = %d after RetireEndpoint, want 0", got)
	}
	if acquire(t, p, old) == oldClient {
		t.Error("a retired client was returned again")
	}
}

// A failed construction caches nothing and is retried by a later acquisition, not internally.
func TestFailedConstructionRetriesOnlyOnALaterAcquire(t *testing.T) {
	ca := newCA(t, "test-ca")
	certDir(t, ca)

	var fail = true
	var mu sync.Mutex
	server := newBMC(t, ca, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		failing := fail
		mu.Unlock()
		if failing {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Product":"BlueField-3"}`))
	})
	p, _ := newTestProvider(t, caConfigMap(providerNamespace, ca.pem))
	key := bf3Key(server.URL)
	key.Family = familyUnknown

	_, err := p.acquire(context.Background(), key)
	if !HasHTTPStatus(err, http.StatusServiceUnavailable) {
		t.Fatalf("construction error = %v, want the BMC's 503 preserved", err)
	}
	if got := cachedEntries(p); got != 0 {
		t.Errorf("entries = %d after a failed construction, want 0", got)
	}
	// One attempt, one request: no internal retry loop.
	if got := server.count("/" + APIRootService); got != 1 {
		t.Errorf("root reads = %d, want 1: the HTTP layer must not retry", got)
	}

	mu.Lock()
	fail = false
	mu.Unlock()
	acquire(t, p, key)
}

// An observed trust-bundle change stops handing out the old client immediately.
func TestCARotationReplacesTheCachedClient(t *testing.T) {
	ca := newCA(t, "test-ca")
	certDir(t, ca)
	server := newBMC(t, ca, nil)
	p, k8s := newTestProvider(t, caConfigMap(providerNamespace, ca.pem))
	key := bf3Key(server.URL)

	before := acquire(t, p, key)
	rotateCA(t, k8s, ca)
	if acquire(t, p, key) == before {
		t.Error("a caller obtained the pre-rotation client after the trust bundle changed")
	}
	if got := cachedEntries(p); got != 1 {
		t.Errorf("entries = %d after rotation, want the replacement only", got)
	}
}

// A trust-bundle ConfigMap write that leaves the bundle bytes unchanged keeps the cached client.
func TestUnchangedBundleRewriteKeepsTheCachedClient(t *testing.T) {
	ca := newCA(t, "test-ca")
	certDir(t, ca)
	server := newBMC(t, ca, nil)
	p, k8s := newTestProvider(t, caConfigMap(providerNamespace, ca.pem))
	key := bf3Key(server.URL)

	before := acquire(t, p, key)
	cm := &corev1.ConfigMap{}
	if err := k8s.Get(context.Background(), types.NamespacedName{Name: CATrustBundleConfigMap, Namespace: providerNamespace}, cm); err != nil {
		t.Fatal(err)
	}
	patch := client.MergeFrom(cm.DeepCopy())
	cm.Labels = map[string]string{"touched": "true"}
	if err := k8s.Patch(context.Background(), cm, patch); err != nil {
		t.Fatal(err)
	}
	if acquire(t, p, key) != before {
		t.Error("a ConfigMap write with an unchanged bundle replaced the cached client")
	}
}

// A construction that finishes after its material rotated is returned to its own caller, but is
// never cached over the client built from the newer material.
func TestStaleConstructionIsNotCached(t *testing.T) {
	ca := newCA(t, "test-ca")
	certDir(t, ca)

	started := make(chan struct{}, 1)
	admit := make(chan struct{})
	var held atomic.Bool
	server := newBMC(t, ca, func(w http.ResponseWriter, r *http.Request) {
		// Hold only the first construction open; later ones must not wait behind it.
		if r.URL.Path == "/"+APIRootService && held.CompareAndSwap(false, true) {
			started <- struct{}{}
			<-admit
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Product":"BlueField-3"}`))
	})
	p, k8s := newTestProvider(t, caConfigMap(providerNamespace, ca.pem))
	key := bf3Key(server.URL)
	key.Family = familyUnknown

	type result struct {
		client *Client
		err    error
	}
	stale := make(chan result, 1)
	go func() {
		c, err := p.acquire(context.Background(), key)
		stale <- result{c, err}
	}()
	<-started

	rotateCA(t, k8s, ca)
	fresh := acquire(t, p, key)
	close(admit)

	got := <-stale
	if got.err != nil || got.client == nil {
		t.Fatalf("acquire across a rotation = %v, %v; want the client it asked for", got.client, got.err)
	}
	if got.client == fresh {
		t.Fatal("the pre-rotation and post-rotation acquisitions shared a client")
	}
	if acquire(t, p, key) != fresh {
		t.Error("a construction from rotated material replaced the current client")
	}
}

// A construction still in flight when its endpoint is retired is returned to its caller but never
// cached: a reset, re-address or deletion cannot be undone by a build that started before it.
func TestRetirementDuringConstructionIsNotUndone(t *testing.T) {
	ca := newCA(t, "test-ca")
	certDir(t, ca)

	started := make(chan struct{}, 1)
	admit := make(chan struct{})
	var held atomic.Bool
	server := newBMC(t, ca, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/"+APIRootService && held.CompareAndSwap(false, true) {
			started <- struct{}{}
			<-admit
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Product":"BlueField-3"}`))
	})
	p, _ := newTestProvider(t, caConfigMap(providerNamespace, ca.pem))
	key := bf3Key(server.URL)
	key.Family = familyUnknown

	type result struct {
		client *Client
		err    error
	}
	inFlight := make(chan result, 1)
	go func() {
		c, err := p.acquire(context.Background(), key)
		inFlight <- result{c, err}
	}()
	<-started

	p.RetireEndpoint(key.Endpoint)
	close(admit)

	got := <-inFlight
	if got.err != nil || got.client == nil {
		t.Fatalf("acquire across a retirement = %v, %v; want the client it asked for", got.client, got.err)
	}
	if got := cachedEntries(p); got != 0 {
		t.Errorf("entries = %d, want 0: a build that started before the retirement was cached", got)
	}
	if acquire(t, p, key) == got.client {
		t.Error("a client built before the retirement was returned after it")
	}
}

// Required credential refresh failures fail acquisition closed: no stale-on-error grace period.
func TestFailClosedOnUnusableCredentialMaterial(t *testing.T) {
	ca := newCA(t, "test-ca")
	dir := certDir(t, ca)
	server := newBMC(t, ca, nil)
	p, k8s := newTestProvider(t, caConfigMap(providerNamespace, ca.pem))
	p.refreshInterval = time.Nanosecond
	key := bf3Key(server.URL)

	// Warm the cache so a later failure could only be answered from it.
	acquire(t, p, key)

	t.Run("expired client certificate", func(t *testing.T) {
		certPEM, keyPEM := ca.issue(t, "dpf-redfish-client", nil,
			time.Now().Add(-48*time.Hour), time.Now().Add(-time.Hour))
		writePair(t, dir, certPEM, keyPEM)
		_, err := p.acquire(context.Background(), key)
		if err == nil || !strings.Contains(err.Error(), "expired") {
			t.Fatalf("acquire with expired material = %v, want an expiry failure", err)
		}
		// Controllers route this to cert-manager reissuance, the only thing that can fix it.
		if !IsRedfishClientCertStale(err) {
			t.Errorf("expired client certificate is not classified as stale: %v", err)
		}
	})

	t.Run("not yet valid client certificate", func(t *testing.T) {
		certPEM, keyPEM := ca.issue(t, "dpf-redfish-client", nil,
			time.Now().Add(time.Hour), time.Now().Add(48*time.Hour))
		writePair(t, dir, certPEM, keyPEM)
		_, err := p.acquire(context.Background(), key)
		if err == nil || !strings.Contains(err.Error(), "not valid until") {
			t.Fatalf("acquire with not-yet-valid material = %v, want a not-valid-until failure", err)
		}
		if !IsRedfishClientCertStale(err) {
			t.Errorf("not-yet-valid client certificate is not classified as stale: %v", err)
		}
	})

	t.Run("removed trust bundle", func(t *testing.T) {
		certPEM, keyPEM := ca.clientPair(t)
		writePair(t, dir, certPEM, keyPEM)
		cm := &corev1.ConfigMap{}
		if err := k8s.Get(context.Background(), types.NamespacedName{Name: CATrustBundleConfigMap, Namespace: providerNamespace}, cm); err != nil {
			t.Fatal(err)
		}
		if err := k8s.Delete(context.Background(), cm); err != nil {
			t.Fatal(err)
		}
		if _, err := p.acquire(context.Background(), key); err == nil {
			t.Fatal("acquire succeeded after the trust bundle was removed")
		}
	})
}

// Retirement closes idle connections only; it never interrupts a request already in flight.
func TestRetirementDoesNotInterruptAnInFlightRequest(t *testing.T) {
	ca := newCA(t, "test-ca")
	certDir(t, ca)

	proceed := make(chan struct{})
	server := newBMC(t, ca, func(w http.ResponseWriter, r *http.Request) {
		<-proceed
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Product":"BlueField-3"}`))
	})
	p, _ := newTestProvider(t, caConfigMap(providerNamespace, ca.pem))
	key := bf3Key(server.URL)
	c := acquire(t, p, key)

	readErr := make(chan error, 1)
	go func() {
		_, _, err := c.GetRootService(context.Background())
		readErr <- err
	}()

	waitFor(t, func() bool { return server.connections() == 1 })
	p.RetireEndpoint(key.Endpoint)
	close(proceed)
	if err := <-readErr; err != nil {
		t.Errorf("in-flight read was interrupted by retirement: %v", err)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition not met before deadline")
}

// The recorded DPU type selects which mounted key pair a client authenticates with, so the
// mapping onto a family has to be exact: an unrecorded type must not alias a known one.
func TestDeviceKeyMapsRecordedTypeOntoFamily(t *testing.T) {
	ip, port := "10.1.2.3", uint32(8443)
	device := func(dpuType provisioningv1.DPUType) *provisioningv1.DPUDevice {
		return &provisioningv1.DPUDevice{
			ObjectMeta: metav1.ObjectMeta{Name: "d", Namespace: providerNamespace, UID: types.UID("uid-1")},
			Status: provisioningv1.DPUDeviceStatus{
				BMCIP: &ip, BMCPort: &port, DPUType: dpuType,
			},
		}
	}

	for dpuType, want := range map[provisioningv1.DPUType]bfFamily{
		provisioningv1.DPUTypeBlueField3: familyBF3,
		provisioningv1.DPUTypeBlueField4: familyBF4,
		provisioningv1.DPUTypeBlueField2: familyUnknown,
		provisioningv1.DPUTypeUnknown:    familyUnknown,
		"":                               familyUnknown,
	} {
		key, err := deviceKey(providerNamespace, device(dpuType))
		if err != nil {
			t.Fatalf("deviceKey(%q): %v", dpuType, err)
		}
		if key.Family != want {
			t.Errorf("deviceKey(%q).Family = %q, want %q", dpuType, key.Family, want)
		}
		if key.Endpoint != "https://10.1.2.3:8443" {
			t.Errorf("deviceKey(%q).Endpoint = %q", dpuType, key.Endpoint)
		}
		if key.DeviceUID != "uid-1" || key.Namespace != providerNamespace {
			t.Errorf("deviceKey(%q) identity = %+v", dpuType, key)
		}
	}

	// A device with no recorded address cannot be keyed at all.
	if _, err := deviceKey(providerNamespace, &provisioningv1.DPUDevice{}); err == nil {
		t.Error("deviceKey accepted a device with no BMC address")
	}
}
