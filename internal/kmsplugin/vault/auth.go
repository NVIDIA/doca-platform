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
	"fmt"
	"os"
	"strings"

	"github.com/nvidia/doca-platform/internal/kmsplugin/config"

	vaultapi "github.com/hashicorp/vault/api"
	"github.com/hashicorp/vault/api/auth/approle"
	"github.com/hashicorp/vault/api/auth/kubernetes"
	"github.com/hashicorp/vault/api/auth/userpass"
)

// Authenticator updates a Vault client with usable credentials. Implementations
// re-read any credential files on every call so that rotated Kubernetes Secrets
// are observed without restarting the plugin.
type Authenticator interface {
	// Authenticate updates the supplied client with a valid token.
	Authenticate(ctx context.Context, vaultClient AuthClient) error
}

// fileReader reads the full contents of a file. It is injected for testability.
type fileReader func(string) ([]byte, error)

// AuthenticatorOption configures optional NewAuthenticator behavior.
type AuthenticatorOption func(*authenticatorOptions)

// authenticatorOptions contains the optional settings for building an Authenticator.
type authenticatorOptions struct {
	readFile fileReader
}

// withFileReader overrides how credential files are read. Production code uses
// the default os.ReadFile; tests inject a fake reader.
func withFileReader(readFile fileReader) AuthenticatorOption {
	return func(o *authenticatorOptions) {
		o.readFile = readFile
	}
}

// NewAuthenticator builds the Authenticator for the configured auth method.
// Each auth method has its own constructor below so that adding or changing
// a method touches one function instead of growing this switch.
func NewAuthenticator(cfg *config.Config, opts ...AuthenticatorOption) (Authenticator, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config must not be nil")
	}

	options := authenticatorOptions{readFile: os.ReadFile}
	for _, opt := range opts {
		opt(&options)
	}
	readFile := options.readFile

	switch cfg.AuthMethod {
	case config.AuthMethodToken:
		return newTokenAuthenticator(cfg, readFile), nil
	case config.AuthMethodAppRole:
		return newAppRoleAuthenticator(cfg, readFile), nil
	case config.AuthMethodUserpass:
		return newUserpassAuthenticator(cfg, readFile), nil
	case config.AuthMethodKubernetes:
		return newKubernetesAuthenticator(cfg), nil
	case config.AuthMethodJWT:
		return newJWTAuthenticator(cfg, readFile), nil
	default:
		return nil, fmt.Errorf("unsupported auth method %q", cfg.AuthMethod)
	}
}

// newTokenAuthenticator builds the Authenticator for the token auth method.
func newTokenAuthenticator(cfg *config.Config, readFile fileReader) Authenticator {
	return &tokenAuthenticator{tokenFile: cfg.TokenFile, readFile: readFile}
}

// newAppRoleAuthenticator builds the Authenticator for the AppRole auth method.
func newAppRoleAuthenticator(cfg *config.Config, readFile fileReader) Authenticator {
	return &helperAuthenticator{
		name: string(config.AuthMethodAppRole),
		build: func() (vaultapi.AuthMethod, error) {
			roleID, err := readTrimmed(readFile, cfg.AppRoleRoleIDFile)
			if err != nil {
				return nil, fmt.Errorf("reading approle role ID: %w", err)
			}
			opts := []approle.LoginOption{}
			if cfg.AuthMount != "" {
				opts = append(opts, approle.WithMountPath(cfg.AuthMount))
			}
			return approle.NewAppRoleAuth(roleID, &approle.SecretID{FromFile: cfg.AppRoleSecretIDFile}, opts...)
		},
	}
}

// newUserpassAuthenticator builds the Authenticator for the userpass auth method.
func newUserpassAuthenticator(cfg *config.Config, readFile fileReader) Authenticator {
	return &helperAuthenticator{
		name: string(config.AuthMethodUserpass),
		build: func() (vaultapi.AuthMethod, error) {
			username, err := readTrimmed(readFile, cfg.UserpassUsernameFile)
			if err != nil {
				return nil, fmt.Errorf("reading userpass username: %w", err)
			}
			opts := []userpass.LoginOption{}
			if cfg.AuthMount != "" {
				opts = append(opts, userpass.WithMountPath(cfg.AuthMount))
			}
			return userpass.NewUserpassAuth(username, &userpass.Password{FromFile: cfg.UserpassPasswordFile}, opts...)
		},
	}
}

// newKubernetesAuthenticator builds the Authenticator for the Kubernetes auth
// method. Unlike the other methods, the service account JWT path is handed
// directly to the Vault SDK helper, which reads the file itself on every
// login attempt, so there is no readFile parameter here.
func newKubernetesAuthenticator(cfg *config.Config) Authenticator {
	return &helperAuthenticator{
		name: string(config.AuthMethodKubernetes),
		build: func() (vaultapi.AuthMethod, error) {
			opts := []kubernetes.LoginOption{
				kubernetes.WithServiceAccountTokenPath(cfg.KubernetesJWTFile),
			}
			if cfg.AuthMount != "" {
				opts = append(opts, kubernetes.WithMountPath(cfg.AuthMount))
			}
			return kubernetes.NewKubernetesAuth(cfg.KubernetesRole, opts...)
		},
	}
}

// newJWTAuthenticator builds the Authenticator for the JWT auth method.
func newJWTAuthenticator(cfg *config.Config, readFile fileReader) Authenticator {
	return &helperAuthenticator{
		name: string(config.AuthMethodJWT),
		build: func() (vaultapi.AuthMethod, error) {
			jwt, err := readTrimmed(readFile, cfg.JWTFile)
			if err != nil {
				return nil, fmt.Errorf("reading JWT file: %w", err)
			}
			return newJWTAuth(cfg.JWTRole, jwt, cfg.AuthMount)
		},
	}
}

// helperAuthenticator builds a Vault auth method and logs in with it. The
// method is rebuilt on every call so credential files are re-read.
type helperAuthenticator struct {
	name  string
	build func() (vaultapi.AuthMethod, error)
}

func (h *helperAuthenticator) Authenticate(ctx context.Context, vaultClient AuthClient) error {
	method, err := h.build()
	if err != nil {
		return fmt.Errorf("building %s auth: %w", h.name, err)
	}
	if err := vaultClient.Login(ctx, method); err != nil {
		return fmt.Errorf("%s login: %w", h.name, err)
	}
	return nil
}

// defaultJWTMountPath is the default Vault mount path for the JWT auth method.
const defaultJWTMountPath = "jwt"

// jwtAuthMethod implements vaultapi.AuthMethod for the JWT auth method. Unlike
// approle, userpass and kubernetes, the Vault Go SDK has no official helper
// package for JWT auth (see https://github.com/hashicorp/vault/issues/28912),
// so this hand-rolled type mirrors the shape of the vendored helpers, posting
// the same {"role", "jwt"} payload the kubernetes helper uses.
type jwtAuthMethod struct {
	mountPath string
	role      string
	jwt       string
}

// newJWTAuth builds a jwtAuthMethod, defaulting mountPath to defaultJWTMountPath when empty.
func newJWTAuth(role, jwt, mountPath string) (vaultapi.AuthMethod, error) {
	if role == "" {
		return nil, fmt.Errorf("no role name was provided")
	}
	if jwt == "" {
		return nil, fmt.Errorf("no JWT was provided")
	}
	if mountPath == "" {
		mountPath = defaultJWTMountPath
	}
	return &jwtAuthMethod{mountPath: mountPath, role: role, jwt: jwt}, nil
}

func (j *jwtAuthMethod) Login(ctx context.Context, client *vaultapi.Client) (*vaultapi.Secret, error) {
	data := map[string]interface{}{
		"role": j.role,
		"jwt":  j.jwt,
	}
	path := fmt.Sprintf("auth/%s/login", j.mountPath)
	resp, err := client.Logical().WriteWithContext(ctx, path, data)
	if err != nil {
		return nil, fmt.Errorf("unable to log in with JWT auth: %w", err)
	}
	return resp, nil
}

// tokenAuthenticator authenticates using a Vault token read from a file. The
// file is re-read on every call so a rotated token is picked up automatically.
type tokenAuthenticator struct {
	tokenFile string
	readFile  fileReader
}

func (t *tokenAuthenticator) Authenticate(ctx context.Context, vaultClient AuthClient) error {
	token, err := readTrimmed(t.readFile, t.tokenFile)
	if err != nil {
		return fmt.Errorf("reading token file: %w", err)
	}
	if token == "" {
		return fmt.Errorf("token file %q is empty", t.tokenFile)
	}

	// lookup-self validates the token so a garbage or expired token file is
	// caught here rather than on the next scheduled check.
	if err := vaultClient.ValidateToken(ctx, token); err != nil {
		return fmt.Errorf("token lookup-self: %w", err)
	}
	vaultClient.SetToken(token)
	return nil
}

// readTrimmed reads a file and trims surrounding whitespace, which mounted
// Secrets and tooling commonly append as a trailing newline.
func readTrimmed(readFile fileReader, path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("no file path configured")
	}
	data, err := readFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}
