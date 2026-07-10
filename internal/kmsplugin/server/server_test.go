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

package server

import (
	"context"
	"errors"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	kmsapi "k8s.io/kms/apis/v2"
)

var _ = Describe("Server", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	Describe("Status", func() {
		It("reports ok with the version and key ID on success", func() {
			s := New(&fakeBackend{statusFunc: func(_ context.Context) (string, error) {
				return "transit/k8s/4", nil
			}}, logr.Discard())

			resp, err := s.Status(ctx, &kmsapi.StatusRequest{})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Healthz).To(Equal("ok"))
			Expect(resp.Version).To(Equal("v2"))
			Expect(resp.KeyId).To(Equal("transit/k8s/4"))
		})

		It("propagates the backend status error and code", func() {
			s := New(&fakeBackend{statusFunc: func(_ context.Context) (string, error) {
				return "", status.Error(codes.Unavailable, "vault down")
			}}, logr.Discard())

			_, err := s.Status(ctx, &kmsapi.StatusRequest{})
			Expect(status.Code(err)).To(Equal(codes.Unavailable))
		})

		It("wraps a non-status backend error as Internal", func() {
			s := New(&fakeBackend{statusFunc: func(_ context.Context) (string, error) {
				return "", errors.New("plain failure")
			}}, logr.Discard())

			_, err := s.Status(ctx, &kmsapi.StatusRequest{})
			Expect(status.Code(err)).To(Equal(codes.Internal))
		})
	})

	Describe("Encrypt", func() {
		It("rejects a request without a uid", func() {
			s := New(&fakeBackend{}, logr.Discard())
			_, err := s.Encrypt(ctx, &kmsapi.EncryptRequest{Plaintext: []byte("x")})
			Expect(status.Code(err)).To(Equal(codes.InvalidArgument))
		})

		It("returns the ciphertext and key ID from the backend", func() {
			s := New(&fakeBackend{encryptFunc: func(_ context.Context, plaintext []byte) ([]byte, string, error) {
				Expect(plaintext).To(Equal([]byte("data")))
				return []byte("vault:v1:Zm9v"), "transit/k8s/4", nil
			}}, logr.Discard())

			resp, err := s.Encrypt(ctx, &kmsapi.EncryptRequest{Uid: "1", Plaintext: []byte("data")})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Ciphertext).To(Equal([]byte("vault:v1:Zm9v")))
			Expect(resp.KeyId).To(Equal("transit/k8s/4"))
		})

		It("propagates backend errors", func() {
			s := New(&fakeBackend{encryptFunc: func(_ context.Context, _ []byte) ([]byte, string, error) {
				return nil, "", status.Error(codes.PermissionDenied, "denied")
			}}, logr.Discard())

			_, err := s.Encrypt(ctx, &kmsapi.EncryptRequest{Uid: "1", Plaintext: []byte("data")})
			Expect(status.Code(err)).To(Equal(codes.PermissionDenied))
		})
	})

	Describe("Decrypt", func() {
		It("rejects a request without a uid", func() {
			s := New(&fakeBackend{}, logr.Discard())
			_, err := s.Decrypt(ctx, &kmsapi.DecryptRequest{Ciphertext: []byte("x")})
			Expect(status.Code(err)).To(Equal(codes.InvalidArgument))
		})

		It("returns the plaintext from the backend", func() {
			s := New(&fakeBackend{decryptFunc: func(_ context.Context, ciphertext []byte, keyID string) ([]byte, error) {
				Expect(string(ciphertext)).To(Equal("vault:v1:Zm9v"))
				Expect(keyID).To(Equal("transit/k8s/4"))
				return []byte("data"), nil
			}}, logr.Discard())

			resp, err := s.Decrypt(ctx, &kmsapi.DecryptRequest{Uid: "1", Ciphertext: []byte("vault:v1:Zm9v"), KeyId: "transit/k8s/4"})
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.Plaintext).To(Equal([]byte("data")))
		})
	})
})
