/*
Copyright 2025 NVIDIA

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

package health

import (
	"context"
	"net/http"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

func TestAPIConnectionCheck(t *testing.T) {
	tests := []struct {
		name           string
		contextTimeout time.Duration
		restConfigHost string
		wantError      bool
	}{
		{
			name:           "fail with unreachable server",
			restConfigHost: "https://unreachable-server:99999",
			wantError:      true,
		},
		{
			name:           "pass with healthy server with normal timeout",
			contextTimeout: 5 * time.Second,
			wantError:      false,
		},
		{
			name:           "fail with extremely short timeout",
			contextTimeout: 1 * time.Nanosecond,
			wantError:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			testMgr := mgr

			// If restConfigHost is set overwrite the manager with one that has the overwritten host.
			if tt.restConfigHost != "" {
				restConfigWithCustomHost := *cfg
				restConfigWithCustomHost.Host = tt.restConfigHost

				mgrWithHostConfigured, err := ctrl.NewManager(&restConfigWithCustomHost, ctrl.Options{
					Scheme: scheme.Scheme,
					Metrics: server.Options{
						BindAddress: "0",
					},
				})
				testMgr = mgrWithHostConfigured
				g.Expect(err).NotTo(HaveOccurred())
			}

			testCtx := context.Background()
			healthCheckFunc := APIConnectionCheck(testCtx, testMgr)

			// If contextTimeout is set update the context used for the http request.
			if tt.contextTimeout > 0 {
				testCtx, cancel = context.WithTimeout(testCtx, tt.contextTimeout)
				defer cancel()
			}
			req, err := http.NewRequestWithContext(testCtx, "GET", "/", nil)
			g.Expect(err).NotTo(HaveOccurred())

			// Perform the health check.
			err = healthCheckFunc(req)

			if tt.wantError {
				g.Expect(err).To(HaveOccurred())
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}
		})
	}
}
