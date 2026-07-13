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

package config

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/pflag"
)

// parseConfig binds the plugin flags, parses the given args and validates the resulting Config.
func parseConfig(args ...string) (*Config, error) {
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	c := BindFlags(fs)
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	return c, c.Validate()
}

var _ = Describe("BindFlags and Validate", func() {
	It("parses a complete token configuration and defaults the transit mount", func() {
		cfg, err := parseConfig(
			"--socket-path=/run/kms/socket.sock",
			"--vault-address=https://vault.example:8200",
			"--vault-namespace=platform/kubernetes",
			"--vault-key-name=k8s-secrets",
			"--vault-auth-method=token",
			"--vault-token-file=/var/run/secrets/vault/token",
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.SocketPath).To(Equal("/run/kms/socket.sock"))
		Expect(cfg.VaultAddress).To(Equal("https://vault.example:8200"))
		Expect(cfg.VaultNamespace).To(Equal("platform/kubernetes"))
		Expect(cfg.KeyName).To(Equal("k8s-secrets"))
		Expect(cfg.AuthMethod).To(Equal(AuthMethodToken))
		Expect(cfg.TransitMount).To(Equal(DefaultTransitMount))
		Expect(cfg.TokenCheckInterval).To(Equal(DefaultTokenCheckInterval))
		Expect(cfg.LoginTimeout).To(Equal(DefaultLoginTimeout))
		Expect(cfg.TokenFile).To(Equal("/var/run/secrets/vault/token"))
	})

	It("honors explicit token manager timing", func() {
		cfg, err := parseConfig(
			"--socket-path=/run/kms/socket.sock",
			"--vault-address=https://vault.example:8200",
			"--vault-key-name=k8s-secrets",
			"--vault-auth-method=token",
			"--vault-token-file=/var/run/secrets/vault/token",
			"--vault-token-check-interval=30s",
			"--vault-login-timeout=10s",
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.TokenCheckInterval).To(Equal(30 * time.Second))
		Expect(cfg.LoginTimeout).To(Equal(10 * time.Second))
	})

	It("honors an explicit transit mount and auth mount", func() {
		cfg, err := parseConfig(
			"--socket-path=/run/kms/socket.sock",
			"--vault-address=https://vault.example:8200",
			"--vault-key-name=k8s-secrets",
			"--vault-transit-mount=/platform/transit-prod/",
			"--vault-auth-method=approle",
			"--vault-auth-mount=/platform/approle-prod/",
			"--vault-approle-role-id-file=/secrets/role-id",
			"--vault-approle-secret-id-file=/secrets/secret-id",
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.TransitMount).To(Equal("platform/transit-prod"))
		Expect(cfg.AuthMount).To(Equal("platform/approle-prod"))
	})

	It("normalises and trims the auth method", func() {
		cfg, err := parseConfig(
			"--socket-path=/run/kms/socket.sock",
			"--vault-address=https://vault.example:8200",
			"--vault-key-name=k8s-secrets",
			"--vault-auth-method=  Kubernetes  ",
			"--vault-kubernetes-role=kms",
			"--vault-kubernetes-jwt-file=/var/run/secrets/token",
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.AuthMethod).To(Equal(AuthMethodKubernetes))
	})

	It("parses a complete jwt configuration", func() {
		cfg, err := parseConfig(
			"--socket-path=/run/kms/socket.sock",
			"--vault-address=https://vault.example:8200",
			"--vault-key-name=k8s-secrets",
			"--vault-auth-method=jwt",
			"--vault-jwt-role=kms",
			"--vault-jwt-file=/var/run/secrets/vault/jwt",
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.AuthMethod).To(Equal(AuthMethodJWT))
		Expect(cfg.JWTRole).To(Equal("kms"))
		Expect(cfg.JWTFile).To(Equal("/var/run/secrets/vault/jwt"))
	})

	DescribeTable("normalises the auth mount for auth methods that use one",
		func(args []string) {
			cfg, err := parseConfig(args...)
			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.AuthMount).To(Equal("custom/auth"))
		},
		Entry("approle", []string{
			"--socket-path=/s",
			"--vault-address=https://vault.example:8200",
			"--vault-key-name=k8s",
			"--vault-auth-method=approle",
			"--vault-auth-mount=/custom/auth/",
			"--vault-approle-role-id-file=/role",
			"--vault-approle-secret-id-file=/secret",
		}),
		Entry("userpass", []string{
			"--socket-path=/s",
			"--vault-address=https://vault.example:8200",
			"--vault-key-name=k8s",
			"--vault-auth-method=userpass",
			"--vault-auth-mount=/custom/auth/",
			"--vault-userpass-username-file=/user",
			"--vault-userpass-password-file=/password",
		}),
		Entry("kubernetes", []string{
			"--socket-path=/s",
			"--vault-address=https://vault.example:8200",
			"--vault-key-name=k8s",
			"--vault-auth-method=kubernetes",
			"--vault-auth-mount=/custom/auth/",
			"--vault-kubernetes-role=kms",
			"--vault-kubernetes-jwt-file=/jwt",
		}),
		Entry("jwt", []string{
			"--socket-path=/s",
			"--vault-address=https://vault.example:8200",
			"--vault-key-name=k8s",
			"--vault-auth-method=jwt",
			"--vault-auth-mount=/custom/auth/",
			"--vault-jwt-role=kms",
			"--vault-jwt-file=/jwt",
		}),
	)

	DescribeTable("validation failures",
		func(args []string) {
			_, err := parseConfig(args...)
			Expect(err).To(HaveOccurred())
		},
		Entry("missing socket path", []string{
			"--vault-address=https://vault.example:8200",
			"--vault-key-name=k8s",
			"--vault-auth-method=token",
			"--vault-token-file=/t",
		}),
		Entry("missing vault address", []string{
			"--socket-path=/s",
			"--vault-key-name=k8s",
			"--vault-auth-method=token",
			"--vault-token-file=/t",
		}),
		Entry("plaintext vault address", []string{
			"--socket-path=/s",
			"--vault-address=http://vault.example:8200",
			"--vault-key-name=k8s",
			"--vault-auth-method=token",
			"--vault-token-file=/t",
		}),
		Entry("missing key name", []string{
			"--socket-path=/s",
			"--vault-address=https://vault.example:8200",
			"--vault-auth-method=token",
			"--vault-token-file=/t",
		}),
		Entry("missing transit mount", []string{
			"--socket-path=/s",
			"--vault-address=https://vault.example:8200",
			"--vault-transit-mount=",
			"--vault-key-name=k8s",
			"--vault-auth-method=token",
			"--vault-token-file=/t",
		}),
		Entry("slash-only transit mount", []string{
			"--socket-path=/s",
			"--vault-address=https://vault.example:8200",
			"--vault-transit-mount=/",
			"--vault-key-name=k8s",
			"--vault-auth-method=token",
			"--vault-token-file=/t",
		}),
		Entry("zero token check interval", []string{
			"--socket-path=/s",
			"--vault-address=https://vault.example:8200",
			"--vault-key-name=k8s",
			"--vault-auth-method=token",
			"--vault-token-file=/t",
			"--vault-token-check-interval=0s",
		}),
		Entry("negative login timeout", []string{
			"--socket-path=/s",
			"--vault-address=https://vault.example:8200",
			"--vault-key-name=k8s",
			"--vault-auth-method=token",
			"--vault-token-file=/t",
			"--vault-login-timeout=-1s",
		}),
		Entry("key name with slash", []string{
			"--socket-path=/s",
			"--vault-address=https://vault.example:8200",
			"--vault-key-name=k8s/secrets",
			"--vault-auth-method=token",
			"--vault-token-file=/t",
		}),
		Entry("key name with leading dash", []string{
			"--socket-path=/s",
			"--vault-address=https://vault.example:8200",
			"--vault-key-name=-k8s",
			"--vault-auth-method=token",
			"--vault-token-file=/t",
		}),
		Entry("key name with trailing dot", []string{
			"--socket-path=/s",
			"--vault-address=https://vault.example:8200",
			"--vault-key-name=k8s.",
			"--vault-auth-method=token",
			"--vault-token-file=/t",
		}),
		Entry("missing auth method", []string{
			"--socket-path=/s",
			"--vault-address=https://vault.example:8200",
			"--vault-key-name=k8s",
		}),
		Entry("unsupported auth method", []string{
			"--socket-path=/s",
			"--vault-address=https://vault.example:8200",
			"--vault-key-name=k8s",
			"--vault-auth-method=ldap",
		}),
		Entry("token method without token file", []string{
			"--socket-path=/s",
			"--vault-address=https://vault.example:8200",
			"--vault-key-name=k8s",
			"--vault-auth-method=token",
		}),
		Entry("approle method without role id file", []string{
			"--socket-path=/s",
			"--vault-address=https://vault.example:8200",
			"--vault-key-name=k8s",
			"--vault-auth-method=approle",
			"--vault-approle-secret-id-file=/secret",
		}),
		Entry("approle method without secret id file", []string{
			"--socket-path=/s",
			"--vault-address=https://vault.example:8200",
			"--vault-key-name=k8s",
			"--vault-auth-method=approle",
			"--vault-approle-role-id-file=/role",
		}),
		Entry("userpass method without username file", []string{
			"--socket-path=/s",
			"--vault-address=https://vault.example:8200",
			"--vault-key-name=k8s",
			"--vault-auth-method=userpass",
			"--vault-userpass-password-file=/password",
		}),
		Entry("userpass method without password file", []string{
			"--socket-path=/s",
			"--vault-address=https://vault.example:8200",
			"--vault-key-name=k8s",
			"--vault-auth-method=userpass",
			"--vault-userpass-username-file=/user",
		}),
		Entry("kubernetes method without role", []string{
			"--socket-path=/s",
			"--vault-address=https://vault.example:8200",
			"--vault-key-name=k8s",
			"--vault-auth-method=kubernetes",
			"--vault-kubernetes-jwt-file=/jwt",
		}),
		Entry("kubernetes method without jwt file", []string{
			"--socket-path=/s",
			"--vault-address=https://vault.example:8200",
			"--vault-key-name=k8s",
			"--vault-auth-method=kubernetes",
			"--vault-kubernetes-role=kms",
		}),
		Entry("jwt method without role", []string{
			"--socket-path=/s",
			"--vault-address=https://vault.example:8200",
			"--vault-key-name=k8s",
			"--vault-auth-method=jwt",
			"--vault-jwt-file=/jwt",
		}),
		Entry("jwt method without jwt file", []string{
			"--socket-path=/s",
			"--vault-address=https://vault.example:8200",
			"--vault-key-name=k8s",
			"--vault-auth-method=jwt",
			"--vault-jwt-role=kms",
		}),
	)
})
