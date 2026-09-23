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

package redfish

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/nvidia/doca-platform/test/mock/dpuagent/pkg/config"

	"k8s.io/klog/v2"
)

// Supervisor is what the BMC drives when the DPU "boots" or "reboots". It is implemented by the
// agent supervisor; the Redfish package only knows these two physical events.
type Supervisor interface {
	// Boot is the DPU powering into a freshly installed OS: artifact is the per-DPU configuration
	// the install flow delivered (bf.cfg bytes on BF3, seed.iso bytes on BF4).
	Boot(ctx context.Context, artifact []byte)
	// Reboot restarts an already installed DPU.
	Reboot(ctx context.Context)
}

// Server is the mock BMC HTTPS server.
type Server struct {
	state *State
	certs *certStore
	sup   Supervisor

	httpServer *http.Server
	listener   net.Listener
	ctx        context.Context

	// defaultDelay bounds the random extra latency added to every response that has no override;
	// a zero max means no delay.
	defaultDelay delayRange
	// delayOverrides is what WithResponseDelayOverrides received; NewServer expands it into
	// delayByPattern, keyed by ServeMux pattern.
	delayOverrides []config.ResponseDelayOverride
	delayByPattern map[string]delayRange
}

// delayRange is a closed range of extra latency; a zero max means no delay.
type delayRange struct {
	min, max time.Duration
}

// ServerOption tunes a Server at construction.
type ServerOption func(*Server)

// WithResponseDelay makes every Redfish response wait a uniformly random duration in
// [minDelay, maxDelay] before it is written, to imitate a slow BMC. Operations listed in
// WithResponseDelayOverrides use their own range instead.
func WithResponseDelay(minDelay, maxDelay time.Duration) ServerOption {
	return func(s *Server) {
		s.defaultDelay = delayRange{minDelay, maxDelay}
	}
}

// WithResponseDelayOverrides gives the named Redfish operations their own delay range. NewServer
// rejects names that are not in the route table, methods the operation does not serve and two
// overrides that cover the same operation and method.
func WithResponseDelayOverrides(overrides []config.ResponseDelayOverride) ServerOption {
	return func(s *Server) {
		s.delayOverrides = overrides
	}
}

// NewServer wires a BMC state and a supervisor into an HTTPS server. The server does not listen
// until Listen is called.
func NewServer(state *State, sup Supervisor, opts ...ServerOption) (*Server, error) {
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "mock-dpuagent"
	}
	certs, err := newCertStore(hostname)
	if err != nil {
		return nil, err
	}
	s := &Server{state: state, certs: certs, sup: sup, ctx: context.Background()}
	for _, opt := range opts {
		opt(s)
	}
	if err := s.buildDelayTable(); err != nil {
		return nil, err
	}
	s.httpServer = &http.Server{
		Handler:           s.handler(),
		ReadHeaderTimeout: 30 * time.Second,
		TLSConfig: &tls.Config{
			MinVersion:     tls.VersionTLS12,
			GetCertificate: certs.getCertificate,
			// mTLS is "enabled" from the controller's point of view, but the client certificate
			// is never checked: there is no failure path to test here.
			ClientAuth: tls.RequestClientCert,
		},
	}
	return s, nil
}

// Listen binds addr (for example "0.0.0.0:443" or "127.0.0.1:0" in tests).
func (s *Server) Listen(addr string) (net.Addr, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen %s: %w", addr, err)
	}
	s.listener = ln
	return ln.Addr(), nil
}

// Serve runs the HTTPS server until ctx is canceled. Downloads and supervisor callbacks started by
// requests are bound to ctx.
func (s *Server) Serve(ctx context.Context) error {
	if s.listener == nil {
		return errors.New("Listen must be called before Serve")
	}
	s.ctx = ctx
	errCh := make(chan error, 1)
	go func() {
		errCh <- s.httpServer.ServeTLS(s.listener, "", "")
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.httpServer.Shutdown(shutdownCtx)
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// buildDelayTable expands delayOverrides into delayByPattern. Every override must name an
// operation of the route table and only methods that operation serves; "*" or no methods means
// all of them. Two overrides covering the same operation and method are an error.
func (s *Server) buildDelayTable() error {
	byName := map[string][]route{}
	for _, rt := range routes {
		byName[rt.name] = append(byName[rt.name], rt)
	}
	s.delayByPattern = map[string]delayRange{}
	owner := map[string]int{}
	for i, o := range s.delayOverrides {
		rts, ok := byName[o.Name]
		if !ok {
			return fmt.Errorf("responseDelayOverrides[%d]: unknown Redfish operation %q, see the operation table in the README", i, o.Name)
		}
		served := servedMethods(rts)
		methods := o.Methods
		if o.AllMethods() {
			methods = served
		}
		minDelay, maxDelay := o.Range()
		for _, m := range methods {
			matched := false
			for _, rt := range rts {
				if rt.method != m {
					continue
				}
				matched = true
				p := rt.pattern()
				if j, dup := owner[p]; dup {
					return fmt.Errorf("responseDelayOverrides[%d] and [%d] both cover %s %s", j, i, m, o.Name)
				}
				owner[p] = i
				s.delayByPattern[p] = delayRange{minDelay, maxDelay}
			}
			if !matched {
				return fmt.Errorf("responseDelayOverrides[%d]: Redfish operation %q does not serve %s (serves %s)", i, o.Name, m, strings.Join(served, ", "))
			}
		}
	}
	return nil
}

// servedMethods lists the distinct methods of rts in a stable order.
func servedMethods(rts []route) []string {
	seen := map[string]bool{}
	var methods []string
	for _, rt := range rts {
		if !seen[rt.method] {
			seen[rt.method] = true
			methods = append(methods, rt.method)
		}
	}
	sort.Strings(methods)
	return methods
}

// delayFor returns the delay range of a ServeMux pattern: its override, or the default.
func (s *Server) delayFor(pattern string) delayRange {
	if d, ok := s.delayByPattern[pattern]; ok {
		return d
	}
	return s.defaultDelay
}

// delayed wraps next so every request first waits a uniformly random duration in d. The wait is
// cut short when the client goes away.
func (s *Server) delayed(d delayRange, next http.HandlerFunc) http.HandlerFunc {
	if d.max <= 0 {
		return next
	}
	return func(w http.ResponseWriter, r *http.Request) {
		wait := d.min
		if span := d.max - d.min; span > 0 {
			wait += time.Duration(rand.Int64N(int64(span) + 1))
		}
		if wait > 0 {
			timer := time.NewTimer(wait)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-r.Context().Done():
				return
			}
		}
		next(w, r)
	}
}

// ServerCertPEM returns the certificate currently served, for tests.
func (s *Server) ServerCertPEM() string {
	return s.certs.currentPEM()
}

func writeJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if body == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		klog.ErrorS(err, "failed to encode Redfish response")
	}
}

// writeError writes a DMTF-shaped error body so the controller's ErrorMessages() parsing works.
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]interface{}{
		"error": map[string]interface{}{
			"code":    code,
			"message": message,
			"@Message.ExtendedInfo": []map[string]interface{}{
				{"@odata.type": "#Message.v1_1_1.Message", "Message": message, "MessageId": code, "MessageSeverity": "Critical"},
			},
		},
	})
}

func writeSuccess(w http.ResponseWriter) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"@Message.ExtendedInfo": []map[string]interface{}{
			{
				"@odata.type":     "#Message.v1_1_1.Message",
				"Message":         "The request completed successfully.",
				"MessageId":       "Base.1.18.1.Success",
				"MessageSeverity": "OK",
				"Resolution":      "None.",
			},
		},
	})
}

func decodeBody(r *http.Request, into interface{}) error {
	if r.Body == nil {
		return errors.New("empty body")
	}
	return json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20)).Decode(into)
}

func odata(id string) map[string]interface{} {
	return map[string]interface{}{"@odata.id": id}
}
