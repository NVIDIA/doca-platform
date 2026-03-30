/*
Copyright 2025 NVIDIA

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

package certificate

import (
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"

	certificates "k8s.io/api/certificates/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/certificate"
)

// NewCertificateManager sets up a certificate manager without a
// client that can be used to sign new certificates (or rotate). If a CSR
// client is set later, it may begin rotating/renewing the client cert.
func NewCertificateManager(
	certDirectory string,
	nodeName types.NodeName,
	certFile string,
	keyFile string,
	clientsetFn certificate.ClientsetFunc,
	pairNamePrefix string,
	commonName string,
	organization []string,
) (certificate.Manager, error) {
	certificateStore, err := certificate.NewFileStore(
		pairNamePrefix,
		certDirectory,
		certDirectory,
		certFile,
		keyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize client certificate store: %v", err)
	}
	m, err := certificate.NewManager(&certificate.Config{
		ClientsetFn: clientsetFn,
		Template: &x509.CertificateRequest{
			Subject: pkix.Name{
				CommonName:   commonName,
				Organization: organization,
			},
		},
		SignerName:       certificates.KubeAPIServerClientSignerName,
		GetUsages:        certificate.DefaultKubeletClientGetUsages,
		CertificateStore: certificateStore,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize client certificate manager: %v", err)
	}

	return m, nil
}
