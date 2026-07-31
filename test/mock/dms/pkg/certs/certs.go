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

// Package certs implements utilities for managing certificates for the mock DMS server.
// Parts of this package are from https://github.com/kubernetes-sigs/cluster-api/blob/04729d5bf69ddf9110953719e71e5fe79ec14f84/util/certs/certs.go
package certs

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
)

// EncodeCertPEM returns PEM-encoded certificate data.
func EncodeCertPEM(cert *x509.Certificate) []byte {
	block := pem.Block{
		Type:  "CERTIFICATE",
		Bytes: cert.Raw,
	}
	return pem.EncodeToMemory(&block)
}

// EncodePrivateKeyPEM returns PEM-encoded private key data.
func EncodePrivateKeyPEM(key *rsa.PrivateKey) []byte {
	block := pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	}

	return pem.EncodeToMemory(&block)
}

func ServerCertFromDirectory(path string) (*x509.Certificate, *rsa.PrivateKey, error) {
	// Decode the cert from the file.
	certContent, err := os.ReadFile(filepath.Join(path, "tls.crt"))
	if err != nil {
		return nil, nil, err
	}

	cert, err := parseCertificate(certContent)
	if err != nil {
		return nil, nil, err
	}
	// Parse key from file system.
	keyContent, err := os.ReadFile(filepath.Join(path, "tls.key"))
	if err != nil {
		return nil, nil, err
	}

	key, err := parseKey(keyContent)
	if err != nil {
		return nil, nil, err
	}
	return cert, key, nil
}

func parseCertificate(certContent []byte) (*x509.Certificate, error) {
	certBlock, _ := pem.Decode(certContent)
	if certBlock == nil {
		return nil, fmt.Errorf("unable to decode PEM data for cert")
	}

	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("unable to parse certificate: %w", err)
	}
	return cert, nil

}
func parseKey(keyContent []byte) (*rsa.PrivateKey, error) {
	keyBlock, _ := pem.Decode(keyContent)
	if keyBlock == nil {
		return nil, fmt.Errorf("unable to decode PEM data for key")
	}

	key, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("unable to parse private key: %w", err)
	}
	// Decode the key from the file
	return key, nil
}
