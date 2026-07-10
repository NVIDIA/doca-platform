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
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	kmsapi "k8s.io/kms/apis/v2"
)

const (
	timeout                 = time.Second * 30
	interval                = time.Millisecond * 250
	testGracefulStopTimeout = time.Millisecond * 200
)

var originalGracefulStopTimeout time.Duration

func TestServer(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "KMS Plugin Server Suite")
}

var _ = BeforeSuite(func() {
	originalGracefulStopTimeout = gracefulStopTimeout
	gracefulStopTimeout = testGracefulStopTimeout
})

var _ = AfterSuite(func() {
	gracefulStopTimeout = originalGracefulStopTimeout
})

// startServing starts kms on a fresh Unix domain socket under a temp
// directory and returns the socket path once the listener exists.
// ServeListener is stopped and the temp directory removed via DeferCleanup,
// so callers that need to control shutdown timing themselves (e.g. to assert
// on ServeListener's returned error) should not use this helper.
func startServing(ctx context.Context, kms kmsapi.KeyManagementServiceServer) string {
	dir, err := os.MkdirTemp("", "kms")
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() { _ = os.RemoveAll(dir) })
	socketPath := filepath.Join(dir, "kms.sock")

	listener, err := ListenUnix(socketPath)
	Expect(err).NotTo(HaveOccurred())

	serveCtx, cancel := context.WithCancel(ctx)
	serveDone := make(chan struct{})
	go func() {
		defer GinkgoRecover()
		defer close(serveDone)
		Expect(ServeListener(serveCtx, listener, kms, logr.Discard())).To(Succeed())
	}()
	DeferCleanup(func() {
		cancel()
		Eventually(serveDone).WithTimeout(timeout).Should(BeClosed())
	})

	return socketPath
}

// fakeBackend is a programmable Backend for the server tests.
type fakeBackend struct {
	encryptFunc func(ctx context.Context, plaintext []byte) ([]byte, string, error)
	decryptFunc func(ctx context.Context, ciphertext []byte, keyID string) ([]byte, error)
	statusFunc  func(ctx context.Context) (string, error)
}

func (f *fakeBackend) Encrypt(ctx context.Context, plaintext []byte) ([]byte, string, error) {
	return f.encryptFunc(ctx, plaintext)
}

func (f *fakeBackend) Decrypt(ctx context.Context, ciphertext []byte, keyID string) ([]byte, error) {
	return f.decryptFunc(ctx, ciphertext, keyID)
}

func (f *fakeBackend) Status(ctx context.Context) (string, error) {
	return f.statusFunc(ctx)
}
