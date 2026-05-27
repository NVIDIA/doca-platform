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

// newRedfishTLSConfig returns TLS settings for Redfish HTTPS clients (bootstrap and mTLS).
func newRedfishTLSConfig(rootCAs *x509.CertPool, certs []tls.Certificate) *tls.Config {
	cfg := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		CipherSuites:       TLSRedfishCipherSuites,
		InsecureSkipVerify: true,
		RootCAs:            rootCAs,
		Certificates:       certs,
	}
	return cfg
}
