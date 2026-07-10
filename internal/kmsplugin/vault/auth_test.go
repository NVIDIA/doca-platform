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
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"

	"github.com/nvidia/doca-platform/internal/kmsplugin/config"

	vaultapi "github.com/hashicorp/vault/api"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Authenticators", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	Describe("tokenAuthenticator", func() {
		It("reads the token file, validates it with lookup-self and then sets the token", func() {
			api := &fakeAPI{}
			auth := &tokenAuthenticator{
				tokenFile: "/secrets/token",
				readFile:  func(string) ([]byte, error) { return []byte("s.token-value\n"), nil },
			}

			err := auth.Authenticate(ctx, api)
			Expect(err).NotTo(HaveOccurred())
			Expect(api.validateTokenCalls).To(ConsistOf("s.token-value"))
			Expect(api.setTokenCalls).To(ConsistOf("s.token-value"))
		})

		It("re-reads the token file on every authentication attempt", func() {
			tokens := []string{"first-token", "second-token"}
			call := 0
			api := &fakeAPI{}
			auth := &tokenAuthenticator{
				tokenFile: "/secrets/token",
				readFile: func(string) ([]byte, error) {
					t := tokens[call]
					call++
					return []byte(t), nil
				},
			}

			Expect(auth.Authenticate(ctx, api)).To(Succeed())
			Expect(auth.Authenticate(ctx, api)).To(Succeed())

			Expect(api.validateTokenCalls).To(Equal([]string{"first-token", "second-token"}))
			Expect(api.setTokenCalls).To(Equal([]string{"first-token", "second-token"}))
		})

		It("fails when the token file is empty", func() {
			api := &fakeAPI{}
			auth := &tokenAuthenticator{
				tokenFile: "/secrets/token",
				readFile:  func(string) ([]byte, error) { return []byte("  \n"), nil },
			}
			Expect(auth.Authenticate(ctx, api)).To(HaveOccurred())
			Expect(api.validateTokenCalls).To(BeEmpty())
		})

		It("propagates token file read errors", func() {
			api := &fakeAPI{}
			auth := &tokenAuthenticator{
				tokenFile: "/secrets/token",
				readFile:  func(string) ([]byte, error) { return nil, errors.New("read failure") },
			}
			Expect(auth.Authenticate(ctx, api)).To(HaveOccurred())
		})

		It("propagates lookup-self failures", func() {
			api := &fakeAPI{validateTokenFunc: func(_ context.Context, _ string) error {
				return responseError(403)
			}}
			auth := &tokenAuthenticator{
				tokenFile: "/secrets/token",
				readFile:  func(string) ([]byte, error) { return []byte("token"), nil },
			}
			Expect(auth.Authenticate(ctx, api)).To(HaveOccurred())
			Expect(api.validateTokenCalls).To(ConsistOf("token"))
			Expect(api.setTokenCalls).To(BeEmpty())
		})
	})

	Describe("NewAuthenticator AppRole", func() {
		It("reads the role ID from file and logs in", func() {
			api := &fakeAPI{}
			cfg := &config.Config{
				AuthMethod:          config.AuthMethodAppRole,
				AppRoleRoleIDFile:   "/secrets/role-id",
				AppRoleSecretIDFile: "/secrets/secret-id",
			}
			auth, err := NewAuthenticator(cfg, withFileReader(func(string) ([]byte, error) { return []byte("role-123\n"), nil }))
			Expect(err).NotTo(HaveOccurred())

			Expect(auth.Authenticate(ctx, api)).To(Succeed())
			Expect(api.loginCalls).To(Equal(1))
		})

		It("fails before login when the role ID file cannot be read", func() {
			api := &fakeAPI{}
			cfg := &config.Config{
				AuthMethod:          config.AuthMethodAppRole,
				AppRoleRoleIDFile:   "/secrets/role-id",
				AppRoleSecretIDFile: "/secrets/secret-id",
			}
			auth, err := NewAuthenticator(cfg, withFileReader(func(string) ([]byte, error) { return nil, errors.New("nope") }))
			Expect(err).NotTo(HaveOccurred())

			Expect(auth.Authenticate(ctx, api)).To(HaveOccurred())
			Expect(api.loginCalls).To(Equal(0))
		})
	})

	Describe("NewAuthenticator Userpass", func() {
		It("reads the username from file and logs in", func() {
			api := &fakeAPI{}
			cfg := &config.Config{
				AuthMethod:           config.AuthMethodUserpass,
				UserpassUsernameFile: "/secrets/username",
				UserpassPasswordFile: "/secrets/password",
			}
			auth, err := NewAuthenticator(cfg, withFileReader(func(string) ([]byte, error) { return []byte("alice\n"), nil }))
			Expect(err).NotTo(HaveOccurred())

			Expect(auth.Authenticate(ctx, api)).To(Succeed())
			Expect(api.loginCalls).To(Equal(1))
		})
	})

	Describe("NewAuthenticator Kubernetes", func() {
		It("reads the JWT from the configured path and logs in", func() {
			jwtPath := filepath.Join(GinkgoT().TempDir(), "token")
			Expect(os.WriteFile(jwtPath, []byte("a.jwt.token"), 0o600)).To(Succeed())

			api := &fakeAPI{}
			cfg := &config.Config{
				AuthMethod:        config.AuthMethodKubernetes,
				KubernetesRole:    "kms",
				KubernetesJWTFile: jwtPath,
			}
			auth, err := NewAuthenticator(cfg)
			Expect(err).NotTo(HaveOccurred())

			Expect(auth.Authenticate(ctx, api)).To(Succeed())
			Expect(api.loginCalls).To(Equal(1))
		})

		It("fails when the JWT file is missing", func() {
			api := &fakeAPI{}
			cfg := &config.Config{
				AuthMethod:        config.AuthMethodKubernetes,
				KubernetesRole:    "kms",
				KubernetesJWTFile: filepath.Join(GinkgoT().TempDir(), "does-not-exist"),
			}
			auth, err := NewAuthenticator(cfg)
			Expect(err).NotTo(HaveOccurred())

			Expect(auth.Authenticate(ctx, api)).To(HaveOccurred())
			Expect(api.loginCalls).To(Equal(0))
		})
	})

	Describe("NewAuthenticator JWT", func() {
		It("reads the JWT from the configured file and logs in", func() {
			api := &fakeAPI{}
			cfg := &config.Config{
				AuthMethod: config.AuthMethodJWT,
				JWTRole:    "dpf-kms",
				JWTFile:    "/secrets/jwt",
			}
			auth, err := NewAuthenticator(cfg, withFileReader(func(string) ([]byte, error) { return []byte("a.jwt.token\n"), nil }))
			Expect(err).NotTo(HaveOccurred())

			Expect(auth.Authenticate(ctx, api)).To(Succeed())
			Expect(api.loginCalls).To(Equal(1))
		})

		It("fails before login when the JWT file cannot be read", func() {
			api := &fakeAPI{}
			cfg := &config.Config{
				AuthMethod: config.AuthMethodJWT,
				JWTRole:    "dpf-kms",
				JWTFile:    "/secrets/jwt",
			}
			auth, err := NewAuthenticator(cfg, withFileReader(func(string) ([]byte, error) { return nil, errors.New("nope") }))
			Expect(err).NotTo(HaveOccurred())

			Expect(auth.Authenticate(ctx, api)).To(HaveOccurred())
			Expect(api.loginCalls).To(Equal(0))
		})
	})

	Describe("newJWTAuth", func() {
		It("rejects an empty role", func() {
			_, err := newJWTAuth("", "jwt", "")
			Expect(err).To(HaveOccurred())
		})

		It("rejects an empty JWT", func() {
			_, err := newJWTAuth("role", "", "")
			Expect(err).To(HaveOccurred())
		})

		It("defaults the mount path", func() {
			method, err := newJWTAuth("role", "jwt", "")
			Expect(err).NotTo(HaveOccurred())

			jwtAuth, ok := method.(*jwtAuthMethod)
			Expect(ok).To(BeTrue())
			Expect(jwtAuth.mountPath).To(Equal(defaultJWTMountPath))
		})
	})

	Describe("jwtAuthMethod", func() {
		login := func(method *jwtAuthMethod) (string, map[string]string, *vaultapi.Secret, error) {
			var requestPath string
			var requestBody map[string]string
			var decodeErr error
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestPath = r.URL.Path
				decodeErr = json.NewDecoder(r.Body).Decode(&requestBody)
				_, _ = w.Write([]byte(`{"auth":{"client_token":"t"}}`))
			}))
			DeferCleanup(server.Close)

			cfg := vaultapi.DefaultConfig()
			cfg.Address = server.URL
			client, err := vaultapi.NewClient(cfg)
			Expect(err).NotTo(HaveOccurred())

			secret, err := method.Login(ctx, client)
			Expect(decodeErr).NotTo(HaveOccurred())
			return requestPath, requestBody, secret, err
		}

		It("posts the role and JWT to the default mount login path", func() {
			path, body, secret, err := login(&jwtAuthMethod{mountPath: defaultJWTMountPath, role: "role", jwt: "jwt"})
			Expect(err).NotTo(HaveOccurred())
			Expect(path).To(Equal("/v1/auth/jwt/login"))
			Expect(body).To(Equal(map[string]string{"role": "role", "jwt": "jwt"}))
			Expect(secret.Auth.ClientToken).To(Equal("t"))
		})

		It("posts the role and JWT to a custom mount login path", func() {
			path, body, secret, err := login(&jwtAuthMethod{mountPath: "custom", role: "role", jwt: "jwt"})
			Expect(err).NotTo(HaveOccurred())
			Expect(path).To(Equal("/v1/auth/custom/login"))
			Expect(body).To(Equal(map[string]string{"role": "role", "jwt": "jwt"}))
			Expect(secret.Auth.ClientToken).To(Equal("t"))
		})
	})

	Describe("NewAuthenticator validation", func() {
		It("rejects an unsupported auth method", func() {
			_, err := NewAuthenticator(&config.Config{AuthMethod: "ldap"})
			Expect(err).To(HaveOccurred())
		})

		It("rejects a nil config", func() {
			_, err := NewAuthenticator(nil)
			Expect(err).To(HaveOccurred())
		})
	})
})
