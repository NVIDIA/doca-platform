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

package vault

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/go-logr/logr"
	vaultapi "github.com/hashicorp/vault/api"
)

const defaultCARefreshInterval = time.Minute

// reloadableRoundTripper sends each new request through the currently active
// transport. Replacing the transport avoids mutating a tls.Config after it has
// been used and prevents new requests from reusing connections trusted by the
// previous CA bundle.
type reloadableRoundTripper struct {
	current atomic.Pointer[http.Transport]
}

// newReloadableRoundTripper wraps the initial transport in an atomically swappable RoundTripper.
func newReloadableRoundTripper(initial *http.Transport) *reloadableRoundTripper {
	t := &reloadableRoundTripper{}
	t.current.Store(initial)
	return t
}

// RoundTrip delegates the request to the currently active transport.
func (t *reloadableRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return t.current.Load().RoundTrip(req)
}

// CloseIdleConnections closes idle connections on the currently active transport.
func (t *reloadableRoundTripper) CloseIdleConnections() {
	t.current.Load().CloseIdleConnections()
}

// replace swaps in a freshly built transport and retires idle connections from the old one.
func (t *reloadableRoundTripper) replace(next *http.Transport) {
	previous := t.current.Swap(next)
	previous.CloseIdleConnections()
}

// caReloader watches a CA file and installs a fresh HTTP transport whenever
// the file contains a new valid bundle. Kubernetes ConfigMap volumes update
// files by replacing symlinks in the parent directory, so the directory is
// watched rather than the current file inode. Polling provides a fallback if a
// filesystem event is missed or watching is unavailable.
type caReloader struct {
	caCertFile   string
	caBundle     []byte
	transport    *reloadableRoundTripper
	log          logr.Logger
	pollInterval time.Duration
	newTransport func([]byte) (*http.Transport, error)
}

// newCAReloader builds a CA reloader around the transport created from the initial bundle.
func newCAReloader(caCertFile string, caBundle []byte, initial *http.Transport, log logr.Logger) *caReloader {
	return &caReloader{
		caCertFile:   caCertFile,
		caBundle:     bytes.Clone(caBundle),
		transport:    newReloadableRoundTripper(initial),
		log:          log,
		pollInterval: defaultCARefreshInterval,
		newTransport: newVaultHTTPTransport,
	}
}

// Run watches and polls the CA bundle until the context is canceled.
func (r *caReloader) Run(ctx context.Context) {
	ticker := time.NewTicker(r.pollInterval)
	defer ticker.Stop()
	defer r.transport.CloseIdleConnections()

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		r.log.Error(err, "failed to watch Vault CA certificate; falling back to polling", "path", r.caCertFile)
		r.reload()
		r.runPolling(ctx, ticker.C)
		return
	}
	defer func() { _ = watcher.Close() }()

	events := watcher.Events
	watchErrors := watcher.Errors
	if err := watcher.Add(filepath.Dir(r.caCertFile)); err != nil {
		r.log.Error(err, "failed to watch Vault CA certificate directory; falling back to polling", "path", r.caCertFile)
		events = nil
		watchErrors = nil
	}
	// Close the gap between the initial synchronous load and installing the
	// watch. If a change happens after the watch is installed, its event is
	// queued while this check runs.
	r.reload()

	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			r.reload()
		case err, ok := <-watchErrors:
			if !ok {
				watchErrors = nil
				continue
			}
			r.log.Error(err, "error watching Vault CA certificate; polling remains active", "path", r.caCertFile)
		case <-ticker.C:
			r.reload()
		}
	}
}

// runPolling reloads the CA bundle on each tick when filesystem watching is unavailable.
func (r *caReloader) runPolling(ctx context.Context, ticks <-chan time.Time) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			r.reload()
		}
	}
}

// reload installs a new transport only when the current CA file contains a valid new bundle.
func (r *caReloader) reload() {
	caBundle, err := os.ReadFile(r.caCertFile)
	if err != nil {
		r.log.Error(err, "failed to read updated Vault CA certificate; continuing with last known good CA", "path", r.caCertFile)
		return
	}
	if bytes.Equal(caBundle, r.caBundle) {
		return
	}

	transport, err := r.newTransport(caBundle)
	if err != nil {
		r.log.Error(err, "failed to load updated Vault CA certificate; continuing with last known good CA", "path", r.caCertFile)
		return
	}

	r.transport.replace(transport)
	r.caBundle = bytes.Clone(caBundle)
	r.log.Info("reloaded Vault CA certificate", "path", r.caCertFile)
}

// newVaultHTTPTransport creates a Vault-compatible HTTP transport using the provided CA bundle.
func newVaultHTTPTransport(caBundle []byte) (*http.Transport, error) {
	if err := validateCABundle(caBundle); err != nil {
		return nil, err
	}

	cfg := vaultapi.DefaultConfig()
	if cfg.Error != nil {
		return nil, cfg.Error
	}
	if err := cfg.ConfigureTLS(&vaultapi.TLSConfig{CACertBytes: caBundle}); err != nil {
		return nil, err
	}

	transport, ok := cfg.HttpClient.Transport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("unexpected Vault HTTP transport type %T", cfg.HttpClient.Transport)
	}
	return transport, nil
}

// validateCABundle rejects bundle contents that would otherwise fall back to system trust.
func validateCABundle(caBundle []byte) error {
	if len(caBundle) == 0 {
		return fmt.Errorf("CA certificate bundle is empty")
	}
	return nil
}
