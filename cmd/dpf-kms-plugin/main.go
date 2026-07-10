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

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nvidia/doca-platform/internal/kmsplugin/config"
	"github.com/nvidia/doca-platform/internal/kmsplugin/server"
	"github.com/nvidia/doca-platform/internal/kmsplugin/vault"

	"github.com/spf13/pflag"
	"k8s.io/component-base/logs"
	logsv1 "k8s.io/component-base/logs/api/v1"
	_ "k8s.io/component-base/logs/json/register"
	"k8s.io/klog/v2"
)

var (
	logOptions = logs.NewOptions()
	fs         = pflag.CommandLine
)

const (
	// readyzCommand runs the plugin as a readiness client for the DaemonSet
	// readiness probe instead of starting the KMS server.
	readyzCommand = "readyz"
	// defaultReadyzTimeout bounds a single readiness check so it fails cleanly
	// instead of being killed by the kubelet probe timeout.
	defaultReadyzTimeout = 15 * time.Second
)

func main() {
	// readyz runs the plugin as a readiness client instead of starting the
	// server: it connects to the KMS v2 Unix socket and reports whether the
	// running plugin is healthy. It is used by the DaemonSet readiness probe.
	if len(os.Args) > 1 && os.Args[1] == readyzCommand {
		if err := runReadyz(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "KMS plugin readiness check failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if err := runServer(); err != nil {
		klog.Fatalf("%v", err)
	}
}

// runServer parses flags, wires the Vault backend and serves the KMS v2
// plugin until it is signaled to stop or a fatal error occurs. main is
// responsible for turning a returned error into a fatal log line and a
// non-zero exit; runServer itself never exits the process.
func runServer() error {
	logsv1.AddFlags(logOptions, fs)
	cfg := config.BindFlags(fs)
	pflag.Parse()
	if err := logsv1.ValidateAndApply(logOptions, nil); err != nil {
		return fmt.Errorf("failed to validate and apply log options: %w", err)
	}
	log := klog.Background()

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	vaultClient, err := vault.NewClient(cfg.VaultAddress, cfg.VaultCACertFile, cfg.VaultNamespace)
	if err != nil {
		return fmt.Errorf("failed to build Vault client: %w", err)
	}

	authenticator, err := vault.NewAuthenticator(cfg)
	if err != nil {
		return fmt.Errorf("failed to build Vault authenticator: %w", err)
	}

	tokenManager := vault.NewTokenManager(vaultClient, authenticator, log.WithName("token-manager"),
		vault.WithTokenCheckInterval(cfg.TokenCheckInterval),
		vault.WithLoginTimeout(cfg.LoginTimeout))
	transitService := vault.NewTransitService(vaultClient, cfg.TransitMount, cfg.KeyName)

	// SIGHUP intentionally shuts down instead of reloading configuration:
	// credential files are re-read on each authentication attempt already.
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	defer cancel()

	listener, err := server.ListenUnix(cfg.SocketPath)
	if err != nil {
		return fmt.Errorf("failed to listen on KMS plugin socket: %w", err)
	}

	// Run performs its first check (initial authentication) immediately and
	// then on every check interval. The socket listener is created before this
	// goroutine starts because listener creation temporarily changes the
	// process-wide umask. Failed authentication is not fatal: the Status RPC
	// will report the plugin unhealthy and Run keeps retrying, so the plugin
	// recovers automatically once Vault becomes reachable.
	go tokenManager.Run(ctx)

	kms := server.New(transitService, log.WithName("kms"))
	if err := server.ServeListener(ctx, listener, kms, log); err != nil {
		return fmt.Errorf("KMS plugin server terminated: %w", err)
	}
	return nil
}

// runReadyz connects to the local KMS plugin socket and reports via the
// returned error whether it is healthy. It performs no Vault authentication
// of its own: it only talks to the local plugin socket, which in turn
// performs the live backend probe. The caller turns a non-nil error into the
// non-zero exit that signals the readiness probe to mark the pod NotReady.
func runReadyz(args []string) error {
	readyzFS := pflag.NewFlagSet(readyzCommand, pflag.ContinueOnError)
	socketPath := readyzFS.String(config.FlagSocketPath, config.DefaultSocketFile,
		"Unix domain socket path of the running KMS v2 plugin to check.")
	timeout := readyzFS.Duration("timeout", defaultReadyzTimeout,
		"Maximum time to wait for the readiness check to complete.")
	if err := readyzFS.Parse(args); err != nil {
		return fmt.Errorf("failed to parse readyz flags: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	return server.CheckReady(ctx, *socketPath)
}
