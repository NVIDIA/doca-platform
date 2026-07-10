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
	"time"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	kmsapi "k8s.io/kms/apis/v2"
)

const testKeyID = "transit/k8s/1"

// stubKMSServer is a KMS v2 server that returns a fixed Status response. It lets
// the readiness tests exercise CheckReady response validation without a backend.
type stubKMSServer struct {
	kmsapi.UnimplementedKeyManagementServiceServer
	resp *kmsapi.StatusResponse
	err  error
}

func (s *stubKMSServer) Status(context.Context, *kmsapi.StatusRequest) (*kmsapi.StatusResponse, error) {
	return s.resp, s.err
}

var _ = Describe("CheckReady", func() {
	It("succeeds when the plugin reports healthy", func(ctx SpecContext) {
		socketPath := startServing(ctx, New(&fakeBackend{
			statusFunc: func(_ context.Context) (string, error) { return testKeyID, nil },
		}, logr.Discard()))

		Eventually(func() error {
			return CheckReady(ctx, socketPath)
		}).WithContext(ctx).WithTimeout(timeout).WithPolling(interval).Should(Succeed())
	})

	It("fails when the backend status check fails", func(ctx SpecContext) {
		socketPath := startServing(ctx, New(&fakeBackend{
			statusFunc: func(_ context.Context) (string, error) {
				return "", status.Error(codes.Unavailable, "vault down")
			},
		}, logr.Discard()))

		Expect(CheckReady(ctx, socketPath)).To(MatchError(ContainSubstring("status check failed")))
	})

	It("fails when the socket does not exist", func(ctx SpecContext) {
		dir, err := os.MkdirTemp("", "kms-ready")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = os.RemoveAll(dir) })

		checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		Expect(CheckReady(checkCtx, filepath.Join(dir, "missing.sock"))).To(HaveOccurred())
	})

	It("fails when the plugin reports an unhealthy status", func(ctx SpecContext) {
		socketPath := startServing(ctx, &stubKMSServer{resp: &kmsapi.StatusResponse{
			Version: "v2", Healthz: "unhealthy", KeyId: testKeyID,
		}})

		Expect(CheckReady(ctx, socketPath)).To(MatchError(ContainSubstring("not healthy")))
	})

	It("fails when the plugin reports an unexpected API version", func(ctx SpecContext) {
		socketPath := startServing(ctx, &stubKMSServer{resp: &kmsapi.StatusResponse{
			Version: "v1", Healthz: "ok", KeyId: testKeyID,
		}})

		Expect(CheckReady(ctx, socketPath)).To(MatchError(ContainSubstring("API version")))
	})

	It("fails when the plugin reports an empty key ID", func(ctx SpecContext) {
		socketPath := startServing(ctx, &stubKMSServer{resp: &kmsapi.StatusResponse{
			Version: "v2", Healthz: "ok", KeyId: "",
		}})

		Expect(CheckReady(ctx, socketPath)).To(MatchError(ContainSubstring("empty key ID")))
	})
})
