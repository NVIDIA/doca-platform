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
	"context"
	"encoding/base64"
	"encoding/json"
	"strconv"
	"sync"
	"time"

	vaultapi "github.com/hashicorp/vault/api"
)

// fakeAPI is a programmable LimitedVaultClient used across the vault package tests.
// The call counters and recorded slices are guarded by mu so tests that drive
// TokenManager.Run in a background goroutine can safely poll them. Tests that
// only call fakeAPI synchronously can keep reading the fields directly, since
// nothing else touches them concurrently.
type fakeAPI struct {
	mu sync.Mutex

	writeFunc         func(ctx context.Context, path string, data map[string]interface{}) (*vaultapi.Secret, error)
	renewFunc         func(ctx context.Context) error
	lookupFunc        func(ctx context.Context) (*vaultapi.Secret, error)
	validateTokenFunc func(ctx context.Context, token string) error

	setTokenCalls      []string
	validateTokenCalls []string
	writePaths         []string
	loginCalls         int
	renewCalls         int
	lookupCalls        int
}

var _ LimitedVaultClient = (*fakeAPI)(nil)

func (f *fakeAPI) SetToken(token string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setTokenCalls = append(f.setTokenCalls, token)
}

func (f *fakeAPI) ValidateToken(ctx context.Context, token string) error {
	f.mu.Lock()
	f.validateTokenCalls = append(f.validateTokenCalls, token)
	f.mu.Unlock()
	if f.validateTokenFunc == nil {
		return nil
	}
	return f.validateTokenFunc(ctx, token)
}

func (f *fakeAPI) Write(ctx context.Context, path string, data map[string]interface{}) (*vaultapi.Secret, error) {
	f.mu.Lock()
	f.writePaths = append(f.writePaths, path)
	f.mu.Unlock()
	if f.writeFunc == nil {
		return nil, nil
	}
	return f.writeFunc(ctx, path, data)
}

func (f *fakeAPI) Login(ctx context.Context, method vaultapi.AuthMethod) error {
	f.mu.Lock()
	f.loginCalls++
	f.mu.Unlock()
	// Most authenticator tests only need to know that Login was requested.
	// Tests for auth-method request bodies call method.Login directly.
	return nil
}

func (f *fakeAPI) RenewSelf(ctx context.Context) error {
	f.mu.Lock()
	f.renewCalls++
	f.mu.Unlock()
	if f.renewFunc == nil {
		return nil
	}
	return f.renewFunc(ctx)
}

func (f *fakeAPI) LookupSelf(ctx context.Context) (*vaultapi.Secret, error) {
	f.mu.Lock()
	f.lookupCalls++
	f.mu.Unlock()
	if f.lookupFunc == nil {
		return nil, nil
	}
	return f.lookupFunc(ctx)
}

// fakeAuthenticator returns programmed tokens/errors and records call counts.
// For call index i, an error at errs[i] takes precedence; otherwise, if a
// token is set at tokens[i], it is set on the client and Authenticate
// succeeds. calls is guarded by mu so tests that drive TokenManager.Run in a
// background goroutine can safely poll it via callCount.
type fakeAuthenticator struct {
	tokens []string
	errs   []error

	mu    sync.Mutex
	calls int
}

var _ Authenticator = (*fakeAuthenticator)(nil)

func (f *fakeAuthenticator) Authenticate(_ context.Context, vaultClient AuthClient) error {
	f.mu.Lock()
	idx := f.calls
	f.calls++
	f.mu.Unlock()

	if idx < len(f.errs) && f.errs[idx] != nil {
		return f.errs[idx]
	}
	if idx < len(f.tokens) {
		vaultClient.SetToken(f.tokens[idx])
	}
	return nil
}

// callCount safely reads calls. Use this instead of the field directly when
// Authenticate may be invoked concurrently, e.g. while TokenManager.Run is
// active in a background goroutine.
func (f *fakeAuthenticator) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// blockingAuthenticator blocks inside Authenticate until release is closed,
// or ctx is done. It is used to verify that check bounds a hung
// authentication attempt with its checkTimeout instead of hanging forever.
type blockingAuthenticator struct {
	release   chan struct{}
	started   chan struct{}
	startOnce sync.Once
}

var _ Authenticator = (*blockingAuthenticator)(nil)

func (b *blockingAuthenticator) Authenticate(ctx context.Context, _ AuthClient) error {
	if b.started != nil {
		b.startOnce.Do(func() { close(b.started) })
	}
	select {
	case <-b.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// --- secret builders ---

// testCiphertext is the canned Transit ciphertext returned by fakes.
const testCiphertext = "vault:v1:Zm9v"

func encryptSecret(keyVersion int) *vaultapi.Secret {
	return &vaultapi.Secret{
		Data: map[string]interface{}{
			"ciphertext":  testCiphertext,
			"key_version": json.Number(strconv.Itoa(keyVersion)),
		},
	}
}

func decryptSecret(plaintext []byte) *vaultapi.Secret {
	return &vaultapi.Secret{
		Data: map[string]interface{}{
			"plaintext": base64.StdEncoding.EncodeToString(plaintext),
		},
	}
}

// lookupSelfSecret builds a token lookup-self response whose creation_ttl
// equals ttl itself, as if the token was just created or renewed and none of
// its lifetime has elapsed yet. Use lookupSelfSecretAt to control the
// remaining ttl and creation_ttl independently, e.g. to exercise the
// fraction-of-lifetime-elapsed renewal check.
func lookupSelfSecret(ttl time.Duration) *vaultapi.Secret {
	return lookupSelfSecretAt(ttl, ttl)
}

func lookupSelfSecretAt(ttl, creationTTL time.Duration) *vaultapi.Secret {
	return &vaultapi.Secret{
		Data: map[string]interface{}{
			"ttl":          json.Number(strconv.Itoa(int(ttl.Seconds()))),
			"creation_ttl": json.Number(strconv.Itoa(int(creationTTL.Seconds()))),
			"renewable":    true,
		},
	}
}
