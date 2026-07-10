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

// Package config parses the plugin configuration from command-line flags.
//
// Plugin specific behavior and Vault client settings are configured through flags (see BindFlags).
// The Vault client ignores VAULT_* environment variables so stray environment values cannot
// override the explicit plugin configuration.
//
// All authentication secret material is referenced through file paths so that
// Kubernetes can rotate the backing Secrets without changing the plugin
// configuration. The files are read lazily at authentication time, never here.
package config

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/pflag"
)

// Flag names accepted by the plugin.
const (
	// FlagSocketPath is the Unix domain socket path the KMS v2 gRPC server listens on.
	FlagSocketPath = "socket-path"
	// FlagVaultAddress is the Vault/OpenBao server address.
	FlagVaultAddress = "vault-address"
	// FlagVaultNamespace is the Vault/OpenBao namespace.
	FlagVaultNamespace = "vault-namespace"
	// FlagVaultCACert is the path to the CA certificate file used to verify the Vault/OpenBao server.
	FlagVaultCACert = "vault-ca-cert"
	// FlagTransitMount is the Vault Transit secrets engine mount path.
	FlagTransitMount = "vault-transit-mount"
	// FlagKeyName is the Vault Transit key used for encrypt and decrypt operations.
	FlagKeyName = "vault-key-name"
	// FlagAuthMethod selects the Vault auth method: token, approle, userpass, kubernetes or jwt.
	FlagAuthMethod = "vault-auth-method"
	// FlagAuthMount overrides the Vault auth mount path for approle, userpass, kubernetes or jwt.
	FlagAuthMount = "vault-auth-mount"
	// FlagTokenCheckInterval is how often the token manager checks the current Vault token.
	FlagTokenCheckInterval = "vault-token-check-interval"
	// FlagLoginTimeout bounds one token manager check cycle, including authentication.
	FlagLoginTimeout = "vault-login-timeout"

	// FlagTokenFile is the path to a file containing the Vault token for token auth.
	FlagTokenFile = "vault-token-file"

	// FlagAppRoleRoleIDFile is the path to a file containing the AppRole role ID.
	FlagAppRoleRoleIDFile = "vault-approle-role-id-file"
	// FlagAppRoleSecretIDFile is the path to a file containing the AppRole secret ID.
	FlagAppRoleSecretIDFile = "vault-approle-secret-id-file"

	// FlagUserpassUsernameFile is the path to a file containing the userpass username.
	FlagUserpassUsernameFile = "vault-userpass-username-file"
	// FlagUserpassPasswordFile is the path to a file containing the userpass password.
	FlagUserpassPasswordFile = "vault-userpass-password-file"

	// FlagKubernetesRole is the Vault Kubernetes auth role name (not a Kubernetes RBAC role).
	FlagKubernetesRole = "vault-kubernetes-role"
	// FlagKubernetesJWTFile is the path to a file containing the service account JWT.
	FlagKubernetesJWTFile = "vault-kubernetes-jwt-file"

	// FlagJWTRole is the Vault JWT auth role name.
	FlagJWTRole = "vault-jwt-role"
	// FlagJWTFile is the path to a file containing the JWT presented to Vault for JWT auth.
	FlagJWTFile = "vault-jwt-file"
)

// DefaultTransitMount is the default value for the transit mount flag.
const DefaultTransitMount = "transit"

const (
	// DefaultTokenCheckInterval is how often the token manager checks the current Vault token.
	DefaultTokenCheckInterval = 60 * time.Second
	// DefaultLoginTimeout bounds one token manager check cycle, including authentication.
	DefaultLoginTimeout = 45 * time.Second
)

var transitKeyNameRE = regexp.MustCompile(`^\w(([\w-.]+)?\w)?$`)

const (
	// DefaultSocketDir is the host path directory in which the Vault KMS plugin DaemonSet serves
	// its KMS v2 Unix domain socket. The kube-apiserver of encrypted Kamaji clusters mounts this
	// directory so it can reach the socket served on the same node.
	DefaultSocketDir = "/var/lib/dpf/kmsplugin/vault-kms"
	// DefaultSocketFile is the KMS v2 Unix domain socket served by the Vault KMS plugin DaemonSet.
	DefaultSocketFile = DefaultSocketDir + "/kms.sock"
)

// AuthMethod enumerates the supported Vault auth methods.
type AuthMethod string

const (
	// AuthMethodToken authenticates using a Vault token read from a file.
	AuthMethodToken AuthMethod = "token"
	// AuthMethodAppRole authenticates using the AppRole auth method.
	AuthMethodAppRole AuthMethod = "approle"
	// AuthMethodUserpass authenticates using the userpass auth method.
	AuthMethodUserpass AuthMethod = "userpass"
	// AuthMethodKubernetes authenticates using the Kubernetes auth method.
	AuthMethodKubernetes AuthMethod = "kubernetes"
	// AuthMethodJWT authenticates using the JWT auth method.
	AuthMethodJWT AuthMethod = "jwt"
)

// Config holds the plugin specific configuration.
type Config struct {
	// SocketPath is the Unix domain socket path served by the plugin.
	SocketPath string
	// VaultAddress is the Vault/OpenBao server address.
	VaultAddress string
	// VaultNamespace is the Vault/OpenBao namespace.
	VaultNamespace string
	// VaultCACertFile is the path to the CA certificate file used to verify the Vault/OpenBao server.
	VaultCACertFile string
	// TransitMount is the Vault Transit secrets engine mount path.
	TransitMount string
	// KeyName is the Vault Transit key name.
	KeyName string
	// AuthMethod selects how the plugin authenticates to Vault.
	AuthMethod AuthMethod
	// AuthMount optionally overrides the auth mount path for non-token methods.
	AuthMount string
	// TokenCheckInterval is how often the token manager checks the current Vault token.
	TokenCheckInterval time.Duration
	// LoginTimeout bounds one token manager check cycle, including authentication.
	LoginTimeout time.Duration

	// TokenFile is the path to the Vault token file (token auth).
	TokenFile string

	// AppRoleRoleIDFile is the path to the AppRole role ID file.
	AppRoleRoleIDFile string
	// AppRoleSecretIDFile is the path to the AppRole secret ID file.
	AppRoleSecretIDFile string

	// UserpassUsernameFile is the path to the userpass username file.
	UserpassUsernameFile string
	// UserpassPasswordFile is the path to the userpass password file.
	UserpassPasswordFile string

	// KubernetesRole is the Vault Kubernetes auth role name.
	KubernetesRole string
	// KubernetesJWTFile is the path to the service account JWT file.
	KubernetesJWTFile string

	// JWTRole is the Vault JWT auth role name.
	JWTRole string
	// JWTFile is the path to the file containing the JWT for JWT auth.
	JWTFile string
}

// BindFlags registers the plugin flags on the given FlagSet and returns a Config whose fields are
// populated once the FlagSet is parsed by the caller. Call Validate after parsing.
func BindFlags(fs *pflag.FlagSet) *Config {
	c := &Config{}
	fs.StringVar(&c.SocketPath, FlagSocketPath, "", "Unix domain socket path the KMS v2 gRPC server listens on.")
	fs.StringVar(&c.VaultAddress, FlagVaultAddress, "", "Vault/OpenBao server address.")
	fs.StringVar(&c.VaultNamespace, FlagVaultNamespace, "", "Vault/OpenBao namespace.")
	fs.StringVar(&c.VaultCACertFile, FlagVaultCACert, "", "Path to a CA certificate file used to verify the Vault/OpenBao server.")
	fs.StringVar(&c.TransitMount, FlagTransitMount, DefaultTransitMount, "Vault Transit secrets engine mount path.")
	fs.StringVar(&c.KeyName, FlagKeyName, "", "Vault Transit key used for encrypt and decrypt operations.")
	fs.StringVar((*string)(&c.AuthMethod), FlagAuthMethod, "", "Vault auth method: token, approle, userpass, kubernetes or jwt.")
	fs.StringVar(&c.AuthMount, FlagAuthMount, "", "Overrides the Vault auth mount path for approle, userpass, kubernetes or jwt.")
	fs.DurationVar(&c.TokenCheckInterval, FlagTokenCheckInterval, DefaultTokenCheckInterval, "How often to check and renew the current Vault token.")
	fs.DurationVar(&c.LoginTimeout, FlagLoginTimeout, DefaultLoginTimeout, "Maximum time for one Vault token check cycle, including authentication.")
	fs.StringVar(&c.TokenFile, FlagTokenFile, "", "Path to a file containing the Vault token for token auth.")
	fs.StringVar(&c.AppRoleRoleIDFile, FlagAppRoleRoleIDFile, "", "Path to a file containing the AppRole role ID.")
	fs.StringVar(&c.AppRoleSecretIDFile, FlagAppRoleSecretIDFile, "", "Path to a file containing the AppRole secret ID.")
	fs.StringVar(&c.UserpassUsernameFile, FlagUserpassUsernameFile, "", "Path to a file containing the userpass username.")
	fs.StringVar(&c.UserpassPasswordFile, FlagUserpassPasswordFile, "", "Path to a file containing the userpass password.")
	fs.StringVar(&c.KubernetesRole, FlagKubernetesRole, "", "Vault Kubernetes auth role name (not a Kubernetes RBAC role).")
	fs.StringVar(&c.KubernetesJWTFile, FlagKubernetesJWTFile, "", "Path to a file containing the service account JWT.")
	fs.StringVar(&c.JWTRole, FlagJWTRole, "", "Vault JWT auth role name.")
	fs.StringVar(&c.JWTFile, FlagJWTFile, "", "Path to a file containing the JWT presented to Vault for JWT auth.")
	return c
}

// Validate normalises and returns an error if the configuration is incomplete or if settings
// for the selected auth method are missing. The auth method specific checks live in their own
// validate*Auth method below, next to the flags each one requires.
func (c *Config) Validate() error {
	c.AuthMethod = AuthMethod(strings.ToLower(strings.TrimSpace(string(c.AuthMethod))))
	c.TransitMount = normalizeMountPath(c.TransitMount)
	c.AuthMount = normalizeMountPath(c.AuthMount)

	if c.SocketPath == "" {
		return missingError(FlagSocketPath)
	}
	if c.VaultAddress == "" {
		return missingError(FlagVaultAddress)
	}
	if c.KeyName == "" {
		return missingError(FlagKeyName)
	}
	if err := validateTransitKeyName(c.KeyName); err != nil {
		return err
	}
	if c.TransitMount == "" {
		return missingError(FlagTransitMount)
	}
	if err := validatePositiveDuration(FlagTokenCheckInterval, c.TokenCheckInterval); err != nil {
		return err
	}
	if err := validatePositiveDuration(FlagLoginTimeout, c.LoginTimeout); err != nil {
		return err
	}

	switch c.AuthMethod {
	case AuthMethodToken:
		return c.validateTokenAuth()
	case AuthMethodAppRole:
		return c.validateAppRoleAuth()
	case AuthMethodUserpass:
		return c.validateUserpassAuth()
	case AuthMethodKubernetes:
		return c.validateKubernetesAuth()
	case AuthMethodJWT:
		return c.validateJWTAuth()
	case "":
		return missingError(FlagAuthMethod)
	default:
		return fmt.Errorf("flag --%s has unsupported value %q, expected one of token, approle, userpass, kubernetes, jwt", FlagAuthMethod, c.AuthMethod)
	}
}

// normalizeMountPath follows Vault's mount path shape: callers may provide
// leading or trailing slashes, while API paths use the mount without them.
func normalizeMountPath(path string) string {
	return strings.Trim(strings.TrimSpace(path), "/")
}

// validateTransitKeyName mirrors Vault Transit route matching for key names:
// https://developer.hashicorp.com/vault/api-docs/secret/transit
// https://github.com/hashicorp/vault/blob/main/sdk/framework/path.go
func validateTransitKeyName(name string) error {
	if !transitKeyNameRE.MatchString(name) {
		return fmt.Errorf("flag --%s has invalid Vault Transit key name %q, expected a name matching %s", FlagKeyName, name, transitKeyNameRE.String())
	}
	return nil
}

func validatePositiveDuration(flag string, duration time.Duration) error {
	if duration <= 0 {
		return fmt.Errorf("flag --%s must be greater than zero", flag)
	}
	return nil
}

// validateTokenAuth checks the flags required by the token auth method.
func (c *Config) validateTokenAuth() error {
	if c.TokenFile == "" {
		return missingError(FlagTokenFile)
	}
	return nil
}

// validateAppRoleAuth checks the flags required by the AppRole auth method.
func (c *Config) validateAppRoleAuth() error {
	if c.AppRoleRoleIDFile == "" {
		return missingError(FlagAppRoleRoleIDFile)
	}
	if c.AppRoleSecretIDFile == "" {
		return missingError(FlagAppRoleSecretIDFile)
	}
	return nil
}

// validateUserpassAuth checks the flags required by the userpass auth method.
func (c *Config) validateUserpassAuth() error {
	if c.UserpassUsernameFile == "" {
		return missingError(FlagUserpassUsernameFile)
	}
	if c.UserpassPasswordFile == "" {
		return missingError(FlagUserpassPasswordFile)
	}
	return nil
}

// validateKubernetesAuth checks the flags required by the Kubernetes auth method.
func (c *Config) validateKubernetesAuth() error {
	if c.KubernetesRole == "" {
		return missingError(FlagKubernetesRole)
	}
	if c.KubernetesJWTFile == "" {
		return missingError(FlagKubernetesJWTFile)
	}
	return nil
}

// validateJWTAuth checks the flags required by the JWT auth method.
func (c *Config) validateJWTAuth() error {
	if c.JWTRole == "" {
		return missingError(FlagJWTRole)
	}
	if c.JWTFile == "" {
		return missingError(FlagJWTFile)
	}
	return nil
}

func missingError(flag string) error {
	return fmt.Errorf("flag --%s must be set", flag)
}
