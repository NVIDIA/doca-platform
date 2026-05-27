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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

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

	It("sets MinVersion and CipherSuites on bootstrap clients", func() {
		cfg := newRedfishTLSConfig(nil, nil)
		Expect(cfg.MinVersion).To(Equal(uint16(tls.VersionTLS12)))
		Expect(cfg.CipherSuites).To(Equal(TLSRedfishCipherSuites))
		Expect(cfg.InsecureSkipVerify).To(BeTrue())
		Expect(cfg.RootCAs).To(BeNil())
		Expect(cfg.Certificates).To(BeNil())
	})

	It("sets mTLS fields on the Redfish TLS client config", func() {
		pool := x509.NewCertPool()
		cert := tls.Certificate{}
		cfg := newRedfishTLSConfig(pool, []tls.Certificate{cert})
		Expect(cfg.MinVersion).To(Equal(uint16(tls.VersionTLS12)))
		Expect(cfg.CipherSuites).To(Equal(TLSRedfishCipherSuites))
		Expect(cfg.RootCAs).To(Equal(pool))
		Expect(cfg.Certificates).To(Equal([]tls.Certificate{cert}))
	})
})
