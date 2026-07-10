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

package vault

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"

	vaultapi "github.com/hashicorp/vault/api"
)

// healthProbePlaintext is encrypted and then decrypted by Status to prove the
// Transit backend is actually usable.
var healthProbePlaintext = []byte("dpf-kms-vault-health-probe")

// TransitService is the Vault Transit backed implementation of the KMS
// operations. It never triggers authentication itself: it just uses whatever
// token TokenManager currently has set on the shared vaultClient, and maps
// whatever Vault returns to a gRPC error. A token that is valid but lacks the
// required Transit policy is not "fixed" here; it just fails until the token
// is replaced, since re-authenticating would produce the exact same policy.
type TransitService struct {
	vaultClient TransitWriter
	mount       string
	key         string
}

// NewTransitService builds a Transit service.
func NewTransitService(vaultClient TransitWriter, mount, key string) *TransitService {
	return &TransitService{
		vaultClient: vaultClient,
		mount:       mount,
		key:         key,
	}
}

// Encrypt encrypts plaintext with the Transit key and returns the ciphertext
// and the key ID identifying the key version used.
func (s *TransitService) Encrypt(ctx context.Context, plaintext []byte) ([]byte, string, error) {
	ciphertext, keyID, err := s.encrypt(ctx, plaintext)
	if err != nil {
		return nil, "", toGRPCError(err)
	}
	return ciphertext, keyID, nil
}

// Decrypt decrypts ciphertext previously produced by Encrypt. The keyID is
// accepted for interface compatibility; Transit ciphertext is self describing.
func (s *TransitService) Decrypt(ctx context.Context, ciphertext []byte, _ string) ([]byte, error) {
	plaintext, err := s.decrypt(ctx, ciphertext)
	if err != nil {
		return nil, toGRPCError(err)
	}
	return plaintext, nil
}

// Status performs a live encrypt then decrypt probe against Vault and returns
// the current key ID. It never reports cached health: every call exercises the
// real backend so a broken token, policy or connection surfaces immediately.
func (s *TransitService) Status(ctx context.Context) (string, error) {
	ciphertext, keyID, err := s.encrypt(ctx, healthProbePlaintext)
	if err != nil {
		return "", toGRPCError(err)
	}
	plaintext, err := s.decrypt(ctx, ciphertext)
	if err != nil {
		return "", toGRPCError(err)
	}
	if !bytes.Equal(plaintext, healthProbePlaintext) {
		return "", toGRPCError(fmt.Errorf("transit health probe mismatch: decrypted value does not match"))
	}
	return keyID, nil
}

func (s *TransitService) encrypt(ctx context.Context, plaintext []byte) ([]byte, string, error) {
	data := map[string]interface{}{
		"plaintext": base64.StdEncoding.EncodeToString(plaintext),
	}
	secret, err := s.vaultClient.Write(ctx, s.encryptPath(), data)
	if err != nil {
		return nil, "", err
	}
	return parseEncryptResponse(secret, s.mount, s.key)
}

func (s *TransitService) decrypt(ctx context.Context, ciphertext []byte) ([]byte, error) {
	data := map[string]interface{}{
		"ciphertext": string(ciphertext),
	}
	secret, err := s.vaultClient.Write(ctx, s.decryptPath(), data)
	if err != nil {
		return nil, err
	}
	return parseDecryptResponse(secret)
}

func (s *TransitService) encryptPath() string {
	return fmt.Sprintf("%s/encrypt/%s", s.mount, s.key)
}

func (s *TransitService) decryptPath() string {
	return fmt.Sprintf("%s/decrypt/%s", s.mount, s.key)
}

// parseEncryptResponse extracts the ciphertext and derives a stable key ID from
// the Transit encrypt response.
func parseEncryptResponse(secret *vaultapi.Secret, mount, key string) ([]byte, string, error) {
	if secret == nil || secret.Data == nil {
		return nil, "", fmt.Errorf("empty transit encrypt response")
	}
	raw, ok := secret.Data["ciphertext"]
	if !ok {
		return nil, "", fmt.Errorf("transit encrypt response missing ciphertext")
	}
	ciphertext, ok := raw.(string)
	if !ok || ciphertext == "" {
		return nil, "", fmt.Errorf("transit encrypt ciphertext is not a non-empty string")
	}

	version, err := parseKeyVersion(secret.Data)
	if err != nil {
		return nil, "", err
	}
	keyID := fmt.Sprintf("%s/%s/%d", mount, key, version)
	return []byte(ciphertext), keyID, nil
}

// parseDecryptResponse extracts and base64 decodes the plaintext from the
// Transit decrypt response.
func parseDecryptResponse(secret *vaultapi.Secret) ([]byte, error) {
	if secret == nil || secret.Data == nil {
		return nil, fmt.Errorf("empty transit decrypt response")
	}
	raw, ok := secret.Data["plaintext"]
	if !ok {
		return nil, fmt.Errorf("transit decrypt response missing plaintext")
	}
	encoded, ok := raw.(string)
	if !ok {
		return nil, fmt.Errorf("transit decrypt plaintext is not a string")
	}
	plaintext, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decoding transit plaintext: %w", err)
	}
	return plaintext, nil
}

// parseKeyVersion parses the Vault Transit key_version.
func parseKeyVersion(data map[string]interface{}) (int64, error) {
	return requirePositiveInt64(data, "key_version", "transit encrypt response")
}
