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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// newTestCA returns a self-signed CA certificate and its private key.
func newTestCA() (*x509.Certificate, *ecdsa.PrivateKey) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	Expect(err).NotTo(HaveOccurred())
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test DPF CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	Expect(err).NotTo(HaveOccurred())
	caCert, err := x509.ParseCertificate(der)
	Expect(err).NotTo(HaveOccurred())
	return caCert, key
}

// signLeaf signs a leaf certificate with the given CA, optionally setting a CN and IP SAN.
func signLeaf(caCert *x509.Certificate, caKey *ecdsa.PrivateKey, commonName string, ipSANs []net.IP) []byte {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	Expect(err).NotTo(HaveOccurred())
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  ipSANs,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, &key.PublicKey, caKey)
	Expect(err).NotTo(HaveOccurred())
	return der
}

var _ = Describe("Redfish TLS config", func() {
	It("allowlists ECDHE cipher suites for TLS 1.2", func() {
		Expect(TLSRedfishCipherSuites).To(Equal([]uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
		}))
	})

	It("sets MinVersion and CipherSuites and skips verification on bootstrap clients", func() {
		cfg, err := newRedfishTLSConfig(nil, nil, true, "")
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.MinVersion).To(Equal(uint16(tls.VersionTLS12)))
		Expect(cfg.CipherSuites).To(Equal(TLSRedfishCipherSuites))
		Expect(cfg.InsecureSkipVerify).To(BeTrue())
		Expect(cfg.RootCAs).To(BeNil())
		Expect(cfg.Certificates).To(BeNil())
		Expect(cfg.ServerName).To(BeEmpty())
	})

	It("uses a custom verifier on the mTLS client config", func() {
		pool := x509.NewCertPool()
		cert := tls.Certificate{}
		cfg, err := newRedfishTLSConfig(pool, []tls.Certificate{cert}, false, "10.1.2.3")
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.MinVersion).To(Equal(uint16(tls.VersionTLS12)))
		Expect(cfg.CipherSuites).To(Equal(TLSRedfishCipherSuites))
		// Go's default verification is disabled; trust is enforced by VerifyPeerCertificate.
		Expect(cfg.InsecureSkipVerify).To(BeTrue())
		Expect(cfg.VerifyPeerCertificate).NotTo(BeNil())
		Expect(cfg.RootCAs).To(Equal(pool))
		Expect(cfg.Certificates).To(Equal([]tls.Certificate{cert}))
		Expect(cfg.ServerName).To(Equal("10.1.2.3"))
	})

	It("fails closed when verification is requested without a CA pool", func() {
		cfg, err := newRedfishTLSConfig(nil, nil, false, "10.1.2.3")
		Expect(err).To(HaveOccurred())
		Expect(cfg).To(BeNil())
	})

	Describe("verifyBMCServerCert", func() {
		var (
			caCert *x509.Certificate
			caKey  *ecdsa.PrivateKey
			pool   *x509.CertPool
		)

		BeforeEach(func() {
			caCert, caKey = newTestCA()
			pool = x509.NewCertPool()
			pool.AddCert(caCert)
		})

		It("accepts a cert chaining to the DPF CA with the BMC IP as an IP SAN", func() {
			der := signLeaf(caCert, caKey, "bmc", []net.IP{net.ParseIP("10.1.2.3")})
			err := verifyBMCServerCert(pool, "10.1.2.3")([][]byte{der}, nil)
			Expect(err).NotTo(HaveOccurred())
		})

		It("accepts a CN-only cert (no IP SAN) when the CN matches the BMC IP", func() {
			der := signLeaf(caCert, caKey, "10.1.2.3", nil)
			err := verifyBMCServerCert(pool, "10.1.2.3")([][]byte{der}, nil)
			Expect(err).NotTo(HaveOccurred())
		})

		It("rejects a cert whose identity does not match the BMC IP", func() {
			der := signLeaf(caCert, caKey, "10.9.9.9", []net.IP{net.ParseIP("10.9.9.9")})
			err := verifyBMCServerCert(pool, "10.1.2.3")([][]byte{der}, nil)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("identity mismatch"))
		})

		It("rejects a cert that does not chain to the DPF CA", func() {
			otherCA, otherKey := newTestCA()
			der := signLeaf(otherCA, otherKey, "10.1.2.3", []net.IP{net.ParseIP("10.1.2.3")})
			err := verifyBMCServerCert(pool, "10.1.2.3")([][]byte{der}, nil)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("chain verification"))
		})

		It("rejects when no certificate is presented", func() {
			err := verifyBMCServerCert(pool, "10.1.2.3")(nil, nil)
			Expect(err).To(HaveOccurred())
		})

		It("fails closed when the expected host is empty", func() {
			der := signLeaf(caCert, caKey, "10.1.2.3", []net.IP{net.ParseIP("10.1.2.3")})
			err := verifyBMCServerCert(pool, "")([][]byte{der}, nil)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no expected host provided"))
		})
	})
})
