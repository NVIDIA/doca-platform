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

package install

import (
	"context"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Handler.download", func() {
	var (
		h     *Handler
		dst   string
		calls atomic.Int32
	)

	// startRegistry serves handler over TLS and points h at it with short retry timings.
	startRegistry := func(handler http.HandlerFunc) {
		srv := httptest.NewTLSServer(handler)
		GinkgoT().Cleanup(srv.Close)

		ca := filepath.Join(GinkgoT().TempDir(), "ca.crt")
		pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
		Expect(os.WriteFile(ca, pemBytes, 0600)).To(Succeed())
		h.bfbRegistryAddr = srv.URL
		h.caBundlePath = ca
	}

	// failFirst answers 503 for the first n requests and 200 "bfb" afterwards.
	failFirst := func(n int32) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			if calls.Add(1) <= n {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			_, _ = w.Write([]byte("bfb"))
		}
	}

	BeforeEach(func() {
		// make the CI download workarounds skip
		GinkgoT().Setenv(bfbRegistryServiceHostEnv, "")
		GinkgoT().Setenv(bfbRegistryServicePortEnv, "")
		GinkgoT().Setenv("KUBERNETES_SERVICE_HOST", "")

		calls.Store(0)
		h = &Handler{
			retryInterval: 10 * time.Millisecond,
			retryTimeout:  500 * time.Millisecond,
		}
		dst = filepath.Join(GinkgoT().TempDir(), "f.bfb")
	})

	It("retries until the registry recovers", func() {
		startRegistry(failFirst(3))

		Expect(h.download(context.Background(), "bfb/f", dst)).To(Succeed())
		Expect(os.ReadFile(dst)).To(Equal([]byte("bfb")))
		Expect(calls.Load()).To(BeNumerically(">=", 4))
	})

	It("gives up with the last error once the retry window is exhausted", func() {
		startRegistry(failFirst(1 << 30))

		start := time.Now()
		err := h.download(context.Background(), "bfb/f", dst)

		Expect(err).To(MatchError(ContainSubstring("503")))
		Expect(time.Since(start)).To(BeNumerically(">=", h.retryTimeout/2))
		Expect(calls.Load()).To(BeNumerically(">=", 2))
	})

	It("does not cut a slow transfer that outlives the retry window", func() {
		h.retryTimeout = 50 * time.Millisecond
		startRegistry(func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(300 * time.Millisecond)
			_, _ = w.Write([]byte("bfb"))
		})

		Expect(h.download(context.Background(), "bfb/f", dst)).To(Succeed())
	})

	It("stops retrying when the context is canceled", func() {
		h.retryTimeout = time.Minute
		startRegistry(failFirst(1 << 30))
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		GinkgoT().Cleanup(cancel)

		start := time.Now()
		Expect(h.download(ctx, "bfb/f", dst)).NotTo(Succeed())
		Expect(time.Since(start)).To(BeNumerically("<", 5*time.Second))
	})
})
