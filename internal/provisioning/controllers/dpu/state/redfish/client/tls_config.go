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
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
)

// TLSRedfishCipherSuites defines the allowed TLS 1.2 cipher suites for HTTPS to BlueField BMC Redfish.
var TLSRedfishCipherSuites = []uint16{
	tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
	tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
	tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
	tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
	tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
	tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
}

// newRedfishTLSConfig returns TLS settings for Redfish HTTPS clients.
//
// Pre-bootstrap clients (raw / basic auth) pass insecureSkipVerify=true because the BMC
// presents a self-generated/default certificate that does not chain to the DPF CA.
//
// The verified mTLS client passes insecureSkipVerify=false together with the DPF CA pool and the
// dialed BMC IP as serverName. When verification is requested without a CA pool the function returns
// an error rather than silently connecting unverified. The BlueField BMC firmware generates the server key + CSR itself and
// may encode the BMC IP only in the legacy Common Name (no IP SAN), which Go's default verifier
// rejects ("x509: cannot validate certificate ... doesn't contain any IP SANs" / "relies on legacy
// Common Name field"). For this client we therefore disable Go's built-in verification and enforce
// trust ourselves via VerifyPeerCertificate: the chain is verified against the DPF CA and the BMC
// identity is pinned by requiring the dialed IP to appear as an IP SAN or in the Common Name.
func newRedfishTLSConfig(rootCAs *x509.CertPool, certs []tls.Certificate, insecureSkipVerify bool, serverName string) (*tls.Config, error) {
	cfg := &tls.Config{
		MinVersion:   tls.VersionTLS12,
		CipherSuites: TLSRedfishCipherSuites,
		RootCAs:      rootCAs,
		Certificates: certs,
		ServerName:   serverName,
	}
	if insecureSkipVerify {
		cfg.InsecureSkipVerify = true
		return cfg, nil
	}
	// Verification was requested: without a CA pool we cannot verify the BMC, so fail closed
	// instead of silently downgrading to an unverified connection.
	if rootCAs == nil {
		return nil, fmt.Errorf("cannot verify BMC server certificate: no DPF CA pool provided")
	}
	// Custom verification: skip Go's default chain/hostname checks and verify ourselves so that
	// CN-only BMC certificates are accepted while still pinning to the DPF CA and the BMC IP.
	cfg.InsecureSkipVerify = true
	cfg.VerifyPeerCertificate = verifyBMCServerCert(rootCAs, serverName)
	return cfg, nil
}

// verifyBMCServerCert returns a tls.Config.VerifyPeerCertificate callback that verifies the BMC
// server certificate chain against the DPF CA pool and then pins the BMC identity. Identity is
// satisfied when expectedHost matches an IP SAN, a DNS SAN, or the certificate Common Name; the CN
// fallback accommodates BMC firmware that omits the IP SAN.
func verifyBMCServerCert(rootCAs *x509.CertPool, expectedHost string) func([][]byte, [][]*x509.Certificate) error {
	return func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
		if len(rawCerts) == 0 {
			return fmt.Errorf("no server certificate presented by BMC")
		}
		certs := make([]*x509.Certificate, 0, len(rawCerts))
		for i, raw := range rawCerts {
			cert, err := x509.ParseCertificate(raw)
			if err != nil {
				return fmt.Errorf("failed to parse BMC server certificate %d: %w", i, err)
			}
			certs = append(certs, cert)
		}

		// Verify the chain against the DPF CA without a hostname check (DNSName left empty); the
		// firmware may omit SANs entirely, so identity is pinned separately below.
		intermediates := x509.NewCertPool()
		for _, cert := range certs[1:] {
			intermediates.AddCert(cert)
		}
		if _, err := certs[0].Verify(x509.VerifyOptions{
			Roots:         rootCAs,
			Intermediates: intermediates,
		}); err != nil {
			return fmt.Errorf("BMC server certificate failed chain verification against DPF CA: %w", err)
		}

		return verifyBMCIdentity(certs[0], expectedHost)
	}
}

// verifyBMCIdentity pins the BMC identity: it requires expectedHost to appear as an IP SAN, a DNS
// SAN, or in the certificate Common Name. An empty expectedHost is treated as a failure
func verifyBMCIdentity(leaf *x509.Certificate, expectedHost string) error {
	if expectedHost == "" {
		return fmt.Errorf("cannot verify BMC server certificate identity: no expected host provided")
	}
	if ip := net.ParseIP(expectedHost); ip != nil {
		for _, sanIP := range leaf.IPAddresses {
			if sanIP.Equal(ip) {
				return nil
			}
		}
	}
	for _, dnsName := range leaf.DNSNames {
		if dnsName == expectedHost {
			return nil
		}
	}
	if leaf.Subject.CommonName == expectedHost {
		return nil
	}
	return fmt.Errorf("BMC server certificate identity mismatch: %q not found in IP SANs %v, DNS SANs %v, or Common Name %q",
		expectedHost, leaf.IPAddresses, leaf.DNSNames, leaf.Subject.CommonName)
}
