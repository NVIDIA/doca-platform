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
	"net/url"
	"strings"

	vaultapi "github.com/hashicorp/vault/api"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func newTestService(vaultClient LimitedVaultClient) *TransitService {
	return NewTransitService(vaultClient, "transit", "k8s")
}

func connectivityErr() error {
	return &url.Error{Op: "Post", URL: "https://vault.example.com", Err: context.DeadlineExceeded}
}

var _ = Describe("Transit Service", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	Describe("Encrypt", func() {
		It("encrypts and derives the key ID from the key version", func() {
			api := &fakeAPI{writeFunc: func(_ context.Context, _ string, _ map[string]interface{}) (*vaultapi.Secret, error) {
				return encryptSecret(3), nil
			}}
			svc := newTestService(api)

			ciphertext, keyID, err := svc.Encrypt(ctx, []byte("secret"))
			Expect(err).NotTo(HaveOccurred())
			Expect(string(ciphertext)).To(Equal("vault:v1:Zm9v"))
			Expect(keyID).To(Equal("transit/k8s/3"))
			Expect(api.writePaths).To(ConsistOf("transit/encrypt/k8s"))
		})

		It("maps connectivity failures to Unavailable", func() {
			api := &fakeAPI{writeFunc: func(_ context.Context, _ string, _ map[string]interface{}) (*vaultapi.Secret, error) {
				return nil, connectivityErr()
			}}
			svc := newTestService(api)

			_, _, err := svc.Encrypt(ctx, []byte("secret"))
			Expect(status.Code(err)).To(Equal(codes.Unavailable))
		})

		It("maps auth failures to PermissionDenied without retrying or touching authentication", func() {
			// TransitService never triggers a re-login itself: a token that is
			// valid but lacks the required policy just fails every time, since
			// fresh authentication would carry the exact same policy.
			api := &fakeAPI{writeFunc: func(_ context.Context, _ string, _ map[string]interface{}) (*vaultapi.Secret, error) {
				return nil, responseError(403)
			}}
			svc := newTestService(api)

			_, _, err := svc.Encrypt(ctx, []byte("secret"))
			Expect(status.Code(err)).To(Equal(codes.PermissionDenied))
			Expect(api.writePaths).To(HaveLen(1))
		})
	})

	Describe("Decrypt", func() {
		It("decrypts ciphertext back to plaintext", func() {
			plaintext := []byte("super-secret")
			api := &fakeAPI{writeFunc: func(_ context.Context, _ string, _ map[string]interface{}) (*vaultapi.Secret, error) {
				return decryptSecret(plaintext), nil
			}}
			svc := newTestService(api)

			got, err := svc.Decrypt(ctx, []byte("vault:v1:Zm9v"), "transit/k8s/3")
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal(plaintext))
			Expect(api.writePaths).To(ConsistOf("transit/decrypt/k8s"))
		})
	})

	Describe("response parsers", func() {
		DescribeTable("rejecting malformed encrypt responses",
			func(secret *vaultapi.Secret, wantErr string) {
				_, _, err := parseEncryptResponse(secret, "transit", "k8s")
				Expect(err).To(MatchError(ContainSubstring(wantErr)))
			},
			Entry("empty response",
				(*vaultapi.Secret)(nil),
				"empty transit encrypt response"),
			Entry("missing ciphertext",
				&vaultapi.Secret{Data: map[string]interface{}{"key_version": json.Number("1")}},
				"transit encrypt response missing ciphertext"),
			Entry("non-string ciphertext",
				&vaultapi.Secret{Data: map[string]interface{}{"ciphertext": 123, "key_version": json.Number("1")}},
				"transit encrypt ciphertext is not a non-empty string"),
			Entry("empty ciphertext",
				&vaultapi.Secret{Data: map[string]interface{}{"ciphertext": "", "key_version": json.Number("1")}},
				"transit encrypt ciphertext is not a non-empty string"),
			Entry("missing key version",
				&vaultapi.Secret{Data: map[string]interface{}{"ciphertext": testCiphertext}},
				"transit encrypt response is missing key_version"),
			Entry("non-positive key version",
				&vaultapi.Secret{Data: map[string]interface{}{"ciphertext": testCiphertext, "key_version": json.Number("0")}},
				"key_version 0 is not positive"),
		)

		DescribeTable("rejecting malformed decrypt responses",
			func(secret *vaultapi.Secret, wantErr string) {
				_, err := parseDecryptResponse(secret)
				Expect(err).To(MatchError(ContainSubstring(wantErr)))
			},
			Entry("empty response",
				(*vaultapi.Secret)(nil),
				"empty transit decrypt response"),
			Entry("missing plaintext",
				&vaultapi.Secret{Data: map[string]interface{}{}},
				"transit decrypt response missing plaintext"),
			Entry("non-string plaintext",
				&vaultapi.Secret{Data: map[string]interface{}{"plaintext": 123}},
				"transit decrypt plaintext is not a string"),
			Entry("invalid base64 plaintext",
				&vaultapi.Secret{Data: map[string]interface{}{"plaintext": "not-base64"}},
				"decoding transit plaintext"),
		)

		DescribeTable("parsing positive int64 fields",
			func(data map[string]interface{}, want int64, wantErr string) {
				got, err := requirePositiveInt64(data, "key_version", "transit encrypt response")
				if wantErr != "" {
					Expect(err).To(MatchError(ContainSubstring(wantErr)))
					return
				}
				Expect(err).NotTo(HaveOccurred())
				Expect(got).To(Equal(want))
			},
			Entry("accepts a positive json.Number",
				map[string]interface{}{"key_version": json.Number("3")}, int64(3), ""),
			Entry("requires the field",
				map[string]interface{}{}, int64(0), "transit encrypt response is missing key_version"),
			Entry("requires a json.Number",
				map[string]interface{}{"key_version": "3"}, int64(0), "key_version has unexpected type string"),
			Entry("rejects non-integer numbers",
				map[string]interface{}{"key_version": json.Number("3.5")}, int64(0), "parsing key_version"),
			Entry("rejects zero",
				map[string]interface{}{"key_version": json.Number("0")}, int64(0), "key_version 0 is not positive"),
			Entry("rejects negative numbers",
				map[string]interface{}{"key_version": json.Number("-1")}, int64(0), "key_version -1 is not positive"),
		)
	})

	Describe("repeated failures", func() {
		It("keeps reaching Vault on every call instead of suppressing requests", func() {
			// Earlier revisions suppressed or paced requests after backend
			// failures. Keep every call live because kube-apiserver relies on
			// each request reaching the backend and surfacing current health.
			api := &fakeAPI{writeFunc: func(_ context.Context, _ string, _ map[string]interface{}) (*vaultapi.Secret, error) {
				return nil, connectivityErr()
			}}
			svc := newTestService(api)

			_, _, err := svc.Encrypt(ctx, []byte("secret"))
			Expect(status.Code(err)).To(Equal(codes.Unavailable))
			writesAfterFirst := len(api.writePaths)
			Expect(writesAfterFirst).To(BeNumerically(">", 0))

			_, _, err = svc.Encrypt(ctx, []byte("secret"))
			Expect(status.Code(err)).To(Equal(codes.Unavailable))
			Expect(len(api.writePaths)).To(BeNumerically(">", writesAfterFirst))
		})
	})

	Describe("Status", func() {
		probeWriteFunc := func(_ context.Context, path string, _ map[string]interface{}) (*vaultapi.Secret, error) {
			if strings.Contains(path, "/encrypt/") {
				return encryptSecret(7), nil
			}
			return decryptSecret(healthProbePlaintext), nil
		}

		It("performs a live encrypt and decrypt probe and returns the key ID", func() {
			api := &fakeAPI{writeFunc: probeWriteFunc}
			svc := newTestService(api)

			keyID, err := svc.Status(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(keyID).To(Equal("transit/k8s/7"))
			Expect(api.writePaths).To(Equal([]string{"transit/encrypt/k8s", "transit/decrypt/k8s"}))
		})

		It("reaches the backend on every call and never returns cached health", func() {
			api := &fakeAPI{writeFunc: probeWriteFunc}
			svc := newTestService(api)

			_, err := svc.Status(ctx)
			Expect(err).NotTo(HaveOccurred())
			_, err = svc.Status(ctx)
			Expect(err).NotTo(HaveOccurred())

			Expect(api.writePaths).To(Equal([]string{
				"transit/encrypt/k8s", "transit/decrypt/k8s",
				"transit/encrypt/k8s", "transit/decrypt/k8s",
			}))
		})

		It("fails when the decrypted probe does not match", func() {
			api := &fakeAPI{writeFunc: func(_ context.Context, path string, _ map[string]interface{}) (*vaultapi.Secret, error) {
				if strings.Contains(path, "/encrypt/") {
					return encryptSecret(7), nil
				}
				return decryptSecret([]byte("tampered")), nil
			}}
			svc := newTestService(api)

			_, err := svc.Status(ctx)
			Expect(status.Code(err)).To(Equal(codes.Internal))
		})

		It("fails when the backend encrypt call errors", func() {
			api := &fakeAPI{writeFunc: func(_ context.Context, _ string, _ map[string]interface{}) (*vaultapi.Secret, error) {
				return nil, connectivityErr()
			}}
			svc := newTestService(api)

			_, err := svc.Status(ctx)
			Expect(status.Code(err)).To(Equal(codes.Unavailable))
		})
	})
})
