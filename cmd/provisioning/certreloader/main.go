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

// Command certreloader watches the bfb-registry server-certificate directory and
// triggers an nginx graceful reload (SIGHUP via `nginx -s reload`) whenever the
// mounted certificate changes. cert-manager renews bfb-registry-server-cert ->
// kubelet syncs the non-subPath Secret volume -> certreloader reloads nginx, so
// the certificate rotates with no dropped connections.
//
// kubelet updates the mounted directory by atomically swapping the `..data`
// symlink, so we watch the directory (not a single file) and debounce the burst
// of events that one swap produces.
package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
)

const (
	defaultCertDir   = "/etc/bfb-registry/tls"
	defaultNginxBin  = "/usr/local/nginx/sbin/nginx"
	defaultNginxConf = "/nginx/nginx.conf"
	defaultDebounce  = 2 * time.Second

	certFileName = "tls.crt"
	keyFileName  = "tls.key"
)

type config struct {
	certDir   string
	nginxBin  string
	nginxConf string
	debounce  time.Duration
}

func parseFlags() *config {
	cfg := &config{}
	flag.StringVar(&cfg.certDir, "cert-dir", defaultCertDir, "directory containing tls.crt and tls.key to watch")
	flag.StringVar(&cfg.nginxBin, "nginx-bin", defaultNginxBin, "path to the nginx binary")
	flag.StringVar(&cfg.nginxConf, "nginx-conf", defaultNginxConf, "path to the nginx configuration file")
	flag.DurationVar(&cfg.debounce, "debounce", defaultDebounce, "coalesce filesystem events within this window before reloading")
	flag.Parse()
	return cfg
}

func main() {
	cfg := parseFlags()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	onChange := func() {
		if reloadErr := reloadNginx(cfg); reloadErr != nil {
			log.Printf("reload failed: %v", reloadErr)
		}
	}
	if err := run(ctx, cfg, onChange); err != nil {
		log.Fatalf("cert-reloader: %v", err)
	}
}

// run watches cfg.certDir and invokes onChange once per settled burst of
// filesystem events (debounced by cfg.debounce). It returns when ctx is
// canceled (e.g. SIGINT/SIGTERM) or the watcher closes. onChange is injectable
// so the debounce behavior can be exercised in tests without invoking nginx.
func run(ctx context.Context, cfg *config, onChange func()) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("create fsnotify watcher: %w", err)
	}
	defer func() {
		if closeErr := watcher.Close(); closeErr != nil {
			log.Printf("close watcher: %v", closeErr)
		}
	}()

	// Watch the directory rather than the individual files: kubelet refreshes the
	// Secret mount by atomically swapping the `..data` symlink, so the leaf files'
	// own inodes never receive write events.
	if err := watcher.Add(cfg.certDir); err != nil {
		return fmt.Errorf("watch %q: %w", cfg.certDir, err)
	}
	log.Printf("watching %s for certificate changes (debounce=%s)", cfg.certDir, cfg.debounce)

	var mu sync.Mutex
	var timer *time.Timer
	stopTimer := func() {
		mu.Lock()
		defer mu.Unlock()
		if timer != nil {
			timer.Stop()
		}
	}
	schedule := func() {
		mu.Lock()
		defer mu.Unlock()
		if timer != nil {
			timer.Stop()
		}
		timer = time.AfterFunc(cfg.debounce, onChange)
	}

	for {
		select {
		case <-ctx.Done():
			stopTimer()
			log.Printf("context canceled, exiting")
			return nil
		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			log.Printf("fs event: %s", event)
			schedule()
		case watchErr, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			log.Printf("watcher error: %v", watchErr)
		}
	}
}

// reloadNginx validates the certificate keypair and, only if it parses, sends a
// graceful reload to the running nginx master. Validating first avoids reloading
// onto a half-written certificate (e.g. observed mid-swap).
func reloadNginx(cfg *config) error {
	certPath := filepath.Join(cfg.certDir, certFileName)
	keyPath := filepath.Join(cfg.certDir, keyFileName)
	if _, err := tls.LoadX509KeyPair(certPath, keyPath); err != nil {
		return fmt.Errorf("validate keypair: %w", err)
	}

	cmd := exec.Command(cfg.nginxBin, "-c", cfg.nginxConf, "-s", "reload")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("nginx -s reload: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}
	log.Printf("nginx reloaded successfully after certificate change")
	return nil
}
