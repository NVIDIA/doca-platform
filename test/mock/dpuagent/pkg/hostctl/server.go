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

// Package hostctl is the plain-HTTP control entry point that stands in for "the host was rebooted
// by an external system". The mock reboot controller calls it when the DPUNode carries the
// external-reboot-required annotation.
package hostctl

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"k8s.io/klog/v2"
)

// DefaultAddr is the listen address; the reboot controller dials http://<bmcIP>:80/reboot.
const DefaultAddr = "0.0.0.0:80"

// RebootRequest is the body the reboot controller posts.
type RebootRequest struct {
	// Method is the DPUNode.status.rebootMethod the external system was asked to perform.
	Method string `json:"method"`
}

// HostRebooter is implemented by the agent supervisor.
type HostRebooter interface {
	// HostReboot is a host power cycle or reboot as seen from the DPU: firmware activated by the
	// BMC takes effect and the DPU OS restarts.
	HostReboot(ctx context.Context, method string)
}

// Handler returns the /reboot handler.
func Handler(ctx context.Context, rebooter HostRebooter) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /reboot", func(w http.ResponseWriter, r *http.Request) {
		var req RebootRequest
		if r.Body != nil {
			_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req)
		}
		klog.InfoS("host reboot requested", "method", req.Method, "remote", r.RemoteAddr)
		rebooter.HostReboot(ctx, req.Method)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "accepted", "method": req.Method})
	})
	return mux
}

// Serve runs the host control server on addr until ctx is canceled.
func Serve(ctx context.Context, addr string, rebooter HostRebooter) error {
	srv := &http.Server{Addr: addr, Handler: Handler(ctx, rebooter), ReadHeaderTimeout: 10 * time.Second}
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
