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
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"
	. "github.com/onsi/gomega"
)

const (
	managersPath             = "/redfish/v1/Managers"
	truststoreCollectionPath = "/redfish/v1/Managers/BMC/Truststore/Certificates"
	truststoreCertPath       = "/redfish/v1/Managers/BMC/Truststore/Certificates/1"
)

func TestListTruststoreCerts(t *testing.T) {
	g := NewWithT(t)
	certPEM := testCertificatePEM(t)
	expectedFingerprint := pemFingerprint(t, certPEM)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == managersPath:
			_, _ = w.Write([]byte(`{"Members":[{"@odata.id":"/redfish/v1/Managers/BMC"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == truststoreCollectionPath:
			_, _ = w.Write([]byte(`{"Members":[{"@odata.id":"/redfish/v1/Managers/BMC/Truststore/Certificates/1"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == truststoreCertPath:
			_, _ = fmt.Fprintf(w, `{"CertificateString":%q}`, certPEM)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := &Client{
		Client: resty.New().SetBaseURL(server.URL),
	}

	got, err := client.ListTruststoreCerts()
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(got).To(HaveLen(1))
	g.Expect(got[0].URI).To(Equal("redfish/v1/Managers/BMC/Truststore/Certificates/1"))
	g.Expect(got[0].Fingerprint).To(Equal(expectedFingerprint))
}

func TestListTruststoreCertsReturnsErrorOnCollectionNonOK(t *testing.T) {
	g := NewWithT(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == managersPath:
			_, _ = w.Write([]byte(`{"Members":[{"@odata.id":"/redfish/v1/Managers/BMC"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == truststoreCollectionPath:
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":{"code":"Base.GeneralError"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := &Client{
		Client: resty.New().SetBaseURL(server.URL),
	}

	_, err := client.ListTruststoreCerts()
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("unexpected status code 500"))
}

func TestListTruststoreCertsReturnsErrorOnMemberNonOK(t *testing.T) {
	g := NewWithT(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == managersPath:
			_, _ = w.Write([]byte(`{"Members":[{"@odata.id":"/redfish/v1/Managers/BMC"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == truststoreCollectionPath:
			_, _ = w.Write([]byte(`{"Members":[{"@odata.id":"/redfish/v1/Managers/BMC/Truststore/Certificates/1"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == truststoreCertPath:
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":{"code":"Base.InsufficientPrivilege"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := &Client{
		Client: resty.New().SetBaseURL(server.URL),
	}

	_, err := client.ListTruststoreCerts()
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("unexpected status code 403"))
}

func TestDeleteTruststoreCert(t *testing.T) {
	g := NewWithT(t)
	deleteCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && r.URL.Path == "/redfish/v1/Managers/BMC/Truststore/Certificates/1" {
			deleteCount++
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := &Client{
		Client: resty.New().SetBaseURL(server.URL),
	}

	resp, info, err := client.DeleteTruststoreCert("/redfish/v1/Managers/BMC/Truststore/Certificates/1")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(info).To(BeNil())
	g.Expect(resp.StatusCode()).To(Equal(http.StatusNoContent))
	g.Expect(deleteCount).To(Equal(1))
}

func testCertificatePEM(t *testing.T) string {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "test-ca",
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func pemFingerprint(t *testing.T, certPEM string) string {
	t.Helper()
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		t.Fatalf("failed decoding pem")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("failed parsing cert: %v", err)
	}
	sum := sha256.Sum256(cert.Raw)
	return fmt.Sprintf("%x", sum)
}
