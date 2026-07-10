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
	"bytes"
	"context"
	"net"
	"os"
	"path/filepath"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/sys/unix"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	kmsapi "k8s.io/kms/apis/v2"
)

var _ = Describe("listenUnix", func() {
	currentUmask := func() int {
		old := unix.Umask(0)
		unix.Umask(old)
		return old
	}

	It("creates the socket with owner-only permissions and restores umask", func() {
		socketPath := filepath.Join(GinkgoT().TempDir(), "kms.sock")
		beforeUmask := currentUmask()

		listener, err := listenUnix(socketPath)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = listener.Close() })

		info, err := os.Stat(socketPath)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.Mode().Perm()).To(Equal(os.FileMode(0o600)))
		Expect(currentUmask()).To(Equal(beforeUmask))
	})

	It("removes a stale socket and listens on the same path", func() {
		socketPath := filepath.Join(GinkgoT().TempDir(), "kms.sock")
		stale, err := net.Listen("unix", socketPath)
		Expect(err).NotTo(HaveOccurred())
		Expect(stale.Close()).To(Succeed())

		listener, err := listenUnix(socketPath)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = listener.Close() })
	})

	It("wraps listen errors and restores umask", func() {
		socketPath := filepath.Join(GinkgoT().TempDir(), "missing", "kms.sock")
		beforeUmask := currentUmask()

		listener, err := listenUnix(socketPath)
		Expect(listener).To(BeNil())
		Expect(err).To(MatchError(ContainSubstring(socketPath)))
		Expect(currentUmask()).To(Equal(beforeUmask))
	})

	It("refuses to remove a non-socket file at the socket path", func() {
		socketPath := filepath.Join(GinkgoT().TempDir(), "kms.sock")
		Expect(os.WriteFile(socketPath, []byte("not a socket"), 0o600)).To(Succeed())

		listener, err := listenUnix(socketPath)
		Expect(listener).To(BeNil())
		Expect(err).To(MatchError(ContainSubstring("not a Unix domain socket")))

		// Refusing to remove it is the point: the file must be left in place.
		_, statErr := os.Stat(socketPath)
		Expect(statErr).NotTo(HaveOccurred())
	})
})

var _ = Describe("ServeListener", func() {
	newBackend := func() *fakeBackend {
		return &fakeBackend{
			encryptFunc: func(_ context.Context, plaintext []byte) ([]byte, string, error) {
				return append([]byte("ct:"), plaintext...), testKeyID, nil
			},
			decryptFunc: func(_ context.Context, ciphertext []byte, _ string) ([]byte, error) {
				return bytes.TrimPrefix(ciphertext, []byte("ct:")), nil
			},
			statusFunc: func(_ context.Context) (string, error) { return testKeyID, nil },
		}
	}

	// serveKMS starts the plugin on a Unix socket and returns a client connected
	// to it once the server is ready. Teardown is registered via DeferCleanup.
	serveKMS := func(ctx context.Context) kmsapi.KeyManagementServiceClient {
		socketPath := startServing(ctx, New(newBackend(), logr.Discard()))

		conn, err := grpc.NewClient("unix://"+socketPath, grpc.WithTransportCredentials(insecure.NewCredentials()))
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = conn.Close() })
		client := kmsapi.NewKeyManagementServiceClient(conn)

		Eventually(func(g Gomega, ctx context.Context) {
			_, err := client.Status(ctx, &kmsapi.StatusRequest{})
			g.Expect(err).NotTo(HaveOccurred())
		}).WithContext(ctx).WithTimeout(timeout).WithPolling(interval).Should(Succeed())

		return client
	}

	It("serves Status over the Unix socket", func(ctx SpecContext) {
		client := serveKMS(ctx)

		resp, err := client.Status(ctx, &kmsapi.StatusRequest{})
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.Healthz).To(Equal("ok"))
		Expect(resp.KeyId).To(Equal(testKeyID))
	})

	It("round-trips Encrypt and Decrypt", func(ctx SpecContext) {
		client := serveKMS(ctx)

		enc, err := client.Encrypt(ctx, &kmsapi.EncryptRequest{Uid: "uid-1", Plaintext: []byte("hello world")})
		Expect(err).NotTo(HaveOccurred())
		Expect(enc.KeyId).To(Equal(testKeyID))

		dec, err := client.Decrypt(ctx, &kmsapi.DecryptRequest{Uid: "uid-1", Ciphertext: enc.Ciphertext, KeyId: enc.KeyId})
		Expect(err).NotTo(HaveOccurred())
		Expect(dec.Plaintext).To(Equal([]byte("hello world")))
	})

	It("rejects requests without a uid", func(ctx SpecContext) {
		client := serveKMS(ctx)

		_, err := client.Encrypt(ctx, &kmsapi.EncryptRequest{Plaintext: []byte("x")})
		Expect(status.Code(err)).To(Equal(codes.InvalidArgument))
	})

	It("forces a stop instead of waiting indefinitely for a slow in-flight RPC", func(ctx SpecContext) {
		dir, err := os.MkdirTemp("", "kms-forced-stop")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = os.RemoveAll(dir) })
		socketPath := filepath.Join(dir, "kms.sock")

		// encryptFunc blocks like a slow backend call, but - like the real
		// vault.TransitService - still honors ctx, so it's Stop's cancellation
		// of the in-flight RPC that lets it return, not the never-closed slow
		// channel.
		encryptStarted := make(chan struct{})
		slow := make(chan struct{})
		DeferCleanup(func() { close(slow) })
		backend := &fakeBackend{
			encryptFunc: func(ctx context.Context, _ []byte) ([]byte, string, error) {
				close(encryptStarted)
				select {
				case <-slow:
				case <-ctx.Done():
				}
				return nil, "", ctx.Err()
			},
			statusFunc: func(_ context.Context) (string, error) { return testKeyID, nil },
		}

		listener, err := ListenUnix(socketPath)
		Expect(err).NotTo(HaveOccurred())

		serveCtx, cancel := context.WithCancel(ctx)
		serveDone := make(chan error, 1)
		go func() {
			serveDone <- ServeListener(serveCtx, listener, New(backend, logr.Discard()), logr.Discard())
		}()

		conn, err := grpc.NewClient("unix://"+socketPath, grpc.WithTransportCredentials(insecure.NewCredentials()))
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = conn.Close() })
		client := kmsapi.NewKeyManagementServiceClient(conn)

		Eventually(func(g Gomega, ctx context.Context) {
			_, err := client.Status(ctx, &kmsapi.StatusRequest{})
			g.Expect(err).NotTo(HaveOccurred())
		}).WithContext(ctx).WithTimeout(timeout).WithPolling(interval).Should(Succeed())

		rpcDone := make(chan struct{})
		go func() {
			defer GinkgoRecover()
			defer close(rpcDone)
			_, _ = client.Encrypt(context.Background(), &kmsapi.EncryptRequest{Uid: "uid-1", Plaintext: []byte("x")})
		}()
		Eventually(encryptStarted).WithTimeout(timeout).Should(BeClosed())

		cancel()
		// testGracefulStopTimeout is far shorter than timeout, so this only
		// succeeds in time if the forced Stop actually fired.
		Eventually(serveDone).WithTimeout(timeout).Should(Receive(Succeed()))
		Eventually(rpcDone).WithTimeout(timeout).Should(BeClosed())
	})
})
