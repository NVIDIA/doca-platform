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

// Package vault implements the Vault Transit backend used by the KMS plugin.
//
// The Vault dependency is isolated behind the small LimitedVaultClient interface so that
// the authentication, token management and Transit logic can be unit tested
// with fakes, without a running Vault server.
package vault

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/go-logr/logr"
	vaultapi "github.com/hashicorp/vault/api"
)

const vaultEnvironmentVariablePrefix = "VAULT_"

// TransitWriter performs the logical writes used for Transit encrypt and
// decrypt. TransitService depends only on this, not on any auth or token
// lifecycle method.
type TransitWriter interface {
	// Write performs a logical write, used for Transit encrypt and decrypt.
	Write(ctx context.Context, path string, data map[string]interface{}) (*vaultapi.Secret, error)
}

// AuthClient is what an Authenticator needs to install and validate
// credentials on the shared Vault client.
type AuthClient interface {
	// SetToken sets the token used for subsequent requests.
	SetToken(token string)
	// ValidateToken verifies a token without installing it on the client.
	ValidateToken(ctx context.Context, token string) error
	// Login authenticates using a Vault auth method and sets the resulting token.
	Login(ctx context.Context, method vaultapi.AuthMethod) error
}

// TokenLifecycleClient is what TokenManager calls directly to check and renew
// the current token, independent of how it was obtained.
type TokenLifecycleClient interface {
	// RenewSelf renews the current token.
	RenewSelf(ctx context.Context) error
	// LookupSelf returns metadata about the current token.
	LookupSelf(ctx context.Context) (*vaultapi.Secret, error)
}

// VaultTokenClient is the subset of the Vault client TokenManager depends on:
// AuthClient, which it hands to the configured Authenticator on every
// authentication attempt, plus the TokenLifecycleClient methods it calls
// directly to check and renew the token in between.
type VaultTokenClient interface {
	AuthClient
	TokenLifecycleClient
}

// LimitedVaultClient is the minimal subset of the Vault client used by this
// package. It is implemented for production by apiClientAdapter and by fakes
// in tests. It composes the narrower interfaces above so that TransitService,
// TokenManager and Authenticator implementations can each depend on only the
// methods they actually use.
type LimitedVaultClient interface {
	TransitWriter
	VaultTokenClient
}

// Client is the production Vault client and its background CA reload loop.
type Client interface {
	LimitedVaultClient
	Run(ctx context.Context)
}

// apiClientAdapter adapts *vaultapi.Client to the LimitedVaultClient interface.
type apiClientAdapter struct {
	client     *vaultapi.Client
	caReloader *caReloader
}

var _ Client = (*apiClientAdapter)(nil)

// newLimitedVaultClient wraps a Vault client so it satisfies the LimitedVaultClient interface.
func newLimitedVaultClient(client *vaultapi.Client) LimitedVaultClient {
	return &apiClientAdapter{client: client}
}

func (a *apiClientAdapter) SetToken(token string) { a.client.SetToken(token) }

func (a *apiClientAdapter) ValidateToken(ctx context.Context, token string) error {
	client, err := a.client.CloneWithHeaders()
	if err != nil {
		return err
	}
	client.SetToken(token)
	_, err = client.Auth().Token().LookupSelfWithContext(ctx)
	return err
}

func (a *apiClientAdapter) Write(ctx context.Context, path string, data map[string]interface{}) (*vaultapi.Secret, error) {
	return a.client.Logical().WriteWithContext(ctx, path, data)
}

func (a *apiClientAdapter) Login(ctx context.Context, method vaultapi.AuthMethod) error {
	_, err := a.client.Auth().Login(ctx, method)
	return err
}

func (a *apiClientAdapter) RenewSelf(ctx context.Context) error {
	_, err := a.client.Auth().Token().RenewSelfWithContext(ctx, 0)
	return err
}

func (a *apiClientAdapter) LookupSelf(ctx context.Context) (*vaultapi.Secret, error) {
	return a.client.Auth().Token().LookupSelfWithContext(ctx)
}

// Run reloads the configured CA certificate until the context is canceled.
// It returns immediately when no CA file was configured.
func (a *apiClientAdapter) Run(ctx context.Context) {
	if a.caReloader != nil {
		a.caReloader.Run(ctx)
	}
}

// NewClient builds the limited Vault client used by the KMS plugin from
// explicit plugin configuration.
func NewClient(address, caCertFile, namespace string, log logr.Logger) (Client, error) {
	client, caReloader, err := newAPIClient(address, caCertFile, namespace, log)
	if err != nil {
		return nil, err
	}
	return &apiClientAdapter{client: client, caReloader: caReloader}, nil
}

// newAPIClient builds a raw Vault API client and optional CA reload loop from
// explicit plugin configuration.
// All VAULT_* environment variables are removed before constructing the
// client so they cannot affect its address, namespace, TLS, proxy or
// authentication settings.
func newAPIClient(address, caCertFile, namespace string, log logr.Logger) (*vaultapi.Client, *caReloader, error) {
	if err := unsetVaultEnvironmentVariables(); err != nil {
		return nil, nil, err
	}

	cfg := vaultapi.DefaultConfig()
	if cfg.Error != nil {
		return nil, nil, cfg.Error
	}

	if address != "" {
		cfg.Address = address
	}

	var caReloader *caReloader
	if caCertFile != "" {
		caBundle, err := os.ReadFile(caCertFile)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read Vault CA certificate %q: %w", caCertFile, err)
		}
		if err := validateCABundle(caBundle); err != nil {
			return nil, nil, fmt.Errorf("invalid Vault CA certificate %q: %w", caCertFile, err)
		}
		if err := cfg.ConfigureTLS(&vaultapi.TLSConfig{CACertBytes: caBundle}); err != nil {
			return nil, nil, err
		}
		transport, ok := cfg.HttpClient.Transport.(*http.Transport)
		if !ok {
			return nil, nil, fmt.Errorf("unexpected Vault HTTP transport type %T", cfg.HttpClient.Transport)
		}
		caReloader = newCAReloader(caCertFile, caBundle, transport, log)
		cfg.HttpClient.Transport = caReloader.transport
	}

	client, err := vaultapi.NewClient(cfg)
	if err != nil {
		return nil, nil, err
	}
	if namespace != "" {
		client.SetNamespace(namespace)
	}

	return client, caReloader, nil
}

func unsetVaultEnvironmentVariables() error {
	for _, entry := range os.Environ() {
		name, _, ok := strings.Cut(entry, "=")
		if !ok || !strings.HasPrefix(name, vaultEnvironmentVariablePrefix) {
			continue
		}
		if err := os.Unsetenv(name); err != nil {
			return fmt.Errorf("failed to unset environment variable %q: %w", name, err)
		}
	}
	return nil
}
