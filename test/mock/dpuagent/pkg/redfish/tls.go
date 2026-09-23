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

package redfish

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"strings"
	"sync"
	"time"
)

// certStore holds the HTTPS server key pair of the mock BMC. It starts with a self-signed
// certificate (the controller's bootstrap clients skip verification) and switches to the
// certificate the controller installs after GenerateCSR + ReplaceCertificate, at which point the
// controller's verified mTLS client can pin the DPF CA and the BMC IP.
type certStore struct {
	mu         sync.RWMutex
	current    *tls.Certificate
	pendingKey *ecdsa.PrivateKey
}

func newCertStore(commonName string) (*certStore, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate self-signed key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: commonName, Organization: []string{"NVIDIA mock BMC"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost", commonName},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("create self-signed certificate: %w", err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	return &certStore{current: &tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}}, nil
}

// getCertificate is the tls.Config.GetCertificate callback.
func (c *certStore) getCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.current, nil
}

// currentPEM returns the PEM of the certificate currently served.
func (c *certStore) currentPEM() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.current.Certificate[0]}))
}

// generateCSR creates a fresh key pair and a CSR with the requested subject and SANs. The key is
// kept pending until the signed certificate arrives, exactly like a BMC that regenerates its key on
// every CSR request.
func (c *certStore) generateCSR(commonName string, altNames []string) (string, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", fmt.Errorf("generate CSR key: %w", err)
	}
	tmpl := &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName:         commonName,
			Organization:       []string{"NVIDIA"},
			OrganizationalUnit: []string{"NBU"},
			Country:            []string{"US"},
			Province:           []string{"CA"},
			Locality:           []string{"Santa Clara"},
		},
	}
	for _, alt := range altNames {
		kind, value, found := strings.Cut(alt, ":")
		if !found {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.ToUpper(strings.TrimSpace(kind)) {
		case "IP":
			if ip := net.ParseIP(value); ip != nil {
				tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
			}
		case "DNS":
			tmpl.DNSNames = append(tmpl.DNSNames, value)
		}
	}
	if ip := net.ParseIP(commonName); ip != nil && !containsIP(tmpl.IPAddresses, ip) {
		tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, tmpl, key)
	if err != nil {
		return "", fmt.Errorf("create CSR: %w", err)
	}
	c.mu.Lock()
	c.pendingKey = key
	c.mu.Unlock()
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})), nil
}

func containsIP(list []net.IP, ip net.IP) bool {
	for _, x := range list {
		if x.Equal(ip) {
			return true
		}
	}
	return false
}

// replace installs a signed certificate chain. The leaf must match the pending CSR key or the
// current key; otherwise the BMC rejects it (the controller then regenerates a CSR).
func (c *certStore) replace(certPEM string) error {
	var chain [][]byte
	rest := []byte(certPEM)
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type == "CERTIFICATE" {
			chain = append(chain, block.Bytes)
		}
	}
	if len(chain) == 0 {
		return fmt.Errorf("no PEM certificate in CertificateString")
	}
	leaf, err := x509.ParseCertificate(chain[0])
	if err != nil {
		return fmt.Errorf("parse certificate: %w", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var key *ecdsa.PrivateKey
	if c.pendingKey != nil && c.pendingKey.PublicKey.Equal(leaf.PublicKey) {
		key = c.pendingKey
	} else if cur, ok := c.current.PrivateKey.(*ecdsa.PrivateKey); ok && cur.PublicKey.Equal(leaf.PublicKey) {
		key = cur
	} else {
		return fmt.Errorf("certificate public key does not match the BMC key pair; generate a new CSR")
	}
	c.current = &tls.Certificate{Certificate: chain, PrivateKey: key, Leaf: leaf}
	c.pendingKey = nil
	return nil
}
