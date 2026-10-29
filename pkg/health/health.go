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
	"fmt"
	"net/http"
	"strings"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

func APIConnectionCheck(ctx context.Context, mgr ctrl.Manager) func(req *http.Request) error {
	log := ctrllog.FromContext(ctx)
	httpClient := mgr.GetHTTPClient()

	readyzURL := strings.TrimSuffix(mgr.GetConfig().Host, "/") + "/readyz"
	return func(req *http.Request) error {
		ctx, cancel := context.WithTimeout(req.Context(), 3*time.Second)
		defer cancel()
		httpReq, err := http.NewRequestWithContext(ctx, "GET", readyzURL, nil)
		if err != nil {
			log.Error(err, "HTTP request creation failed")
			return fmt.Errorf("failed to create request: %w", err)
		}

		// The HTTP client already has auth configured
		resp, err := httpClient.Do(httpReq)
		if err != nil {
			log.Error(err, "API server unreachable")
			return fmt.Errorf("API server unreachable: %w", err)
		}
		defer func() {
			if err := resp.Body.Close(); err != nil {
				log.Error(err, "failed to close response body")
			}
		}()

		if resp.StatusCode != http.StatusOK {
			log.Error(nil, "API server returned non-200 status", "status", resp.StatusCode)
			return fmt.Errorf("API server returned status %d", resp.StatusCode)
		}
		return nil
	}
}
