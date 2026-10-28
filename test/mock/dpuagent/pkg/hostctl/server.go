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

// DefaultAddr is the listen address; the reboot controller dials http://<bmcIP>:80/reboot and
// polls http://<bmcIP>:80/status.
const DefaultAddr = "0.0.0.0:80"

// RebootRequest is the body the reboot controller posts.
type RebootRequest struct {
	// Method is the DPUNode.status.rebootMethod the external system was asked to perform.
	Method string `json:"method"`
}

// StatusResponse is the body of GET /status.
type StatusResponse struct {
	// ArmBooted is false from a host reboot until the Arm has come up again.
	ArmBooted bool `json:"armBooted"`
}

// Host is implemented by the agent supervisor.
type Host interface {
	// HostReboot is a host power cycle or reboot as seen from the DPU: firmware activated by the
	// BMC takes effect and the DPU boots, into a freshly installed OS when the install flow left
	// one staged, otherwise back into the running one. When it returns the reboot has begun:
	// ArmBooted turns true only once this reboot has brought the Arm up.
	HostReboot(ctx context.Context, method string)
	// ArmBooted reports whether the Arm has come up since the last HostReboot. It stays true when
	// the Arm is shut down again afterwards.
	ArmBooted() bool
}

// Handler returns the /reboot and /status handler.
func Handler(ctx context.Context, host Host) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /reboot", func(w http.ResponseWriter, r *http.Request) {
		var req RebootRequest
		if r.Body != nil {
			_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req)
		}
		klog.InfoS("host reboot requested", "method", req.Method, "remote", r.RemoteAddr)
		host.HostReboot(ctx, req.Method)
		writeJSON(w, map[string]string{"status": "accepted", "method": req.Method})
	})
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, StatusResponse{ArmBooted: host.ArmBooted()})
	})
	return mux
}

// writeJSON answers 200 with body encoded as JSON.
func writeJSON(w http.ResponseWriter, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(body)
}

// Serve runs the host control server on addr until ctx is canceled.
func Serve(ctx context.Context, addr string, host Host) error {
	srv := &http.Server{Addr: addr, Handler: Handler(ctx, host), ReadHeaderTimeout: 10 * time.Second}
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
