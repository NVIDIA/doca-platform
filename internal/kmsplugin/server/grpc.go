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
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/go-logr/logr"
	"golang.org/x/sys/unix"
	"google.golang.org/grpc"
	kmsapi "k8s.io/kms/apis/v2"
)

const (
	// socketMode restricts the Unix socket to the owner. The plugin and the
	// kube-apiserver must run as the same user so the kube-apiserver can connect.
	socketMode = 0o600
	// socketUmask creates a 0600 Unix socket without a chmod-after-listen race.
	socketUmask = 0o777 &^ socketMode
)

// gracefulStopTimeout bounds how long ServeListener waits for in-flight RPCs
// to finish during a graceful shutdown before forcibly stopping the server, so
// a single stuck request cannot prevent the plugin process from exiting on
// SIGTERM. It is a var rather than a const so tests can shorten it.
var gracefulStopTimeout = 10 * time.Second

// ListenUnix creates the Unix domain socket listener with owner-only permissions.
func ListenUnix(socketPath string) (net.Listener, error) {
	return listenUnix(socketPath)
}

// ServeListener serves the KMS v2 service on an already-created listener until
// ctx is canceled, then gracefully stops, falling back to an immediate stop if
// that takes longer than gracefulStopTimeout.
func ServeListener(ctx context.Context, listener net.Listener, kms kmsapi.KeyManagementServiceServer, log logr.Logger) error {
	// Read the timeout once here, on the goroutine that returns from
	// ServeListener, so tests that override the gracefulStopTimeout var can
	// restore it once ServeListener has returned without racing the shutdown
	// goroutine's read below.
	stopTimeout := gracefulStopTimeout

	grpcServer := grpc.NewServer()
	kmsapi.RegisterKeyManagementServiceServer(grpcServer, kms)

	go func() {
		<-ctx.Done()
		log.Info("shutting down KMS plugin gRPC server")

		stopped := make(chan struct{})
		go func() {
			defer close(stopped)
			grpcServer.GracefulStop()
		}()

		select {
		case <-stopped:
		case <-time.After(stopTimeout):
			// Stop cancels the context of every in-flight RPC, which is what
			// lets a handler blocked on a context-aware call (as TransitService
			// is; see vault.TransitService) actually return. It does not wait
			// for the abandoned GracefulStop goroutine above: that would just
			// reintroduce the same unbounded wait this timeout exists to avoid.
			log.Info("graceful shutdown did not complete in time, forcing stop", "timeout", stopTimeout)
			grpcServer.Stop()
		}
	}()

	log.Info("serving KMS plugin", "socket", listener.Addr().String())
	if err := grpcServer.Serve(listener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
		return fmt.Errorf("serving gRPC: %w", err)
	}
	return nil
}

// listenUnix creates the Unix domain socket, removing any stale socket left
// behind at the socket path by a previous run, with owner-only permissions.
// The umask change below is process-wide, so callers should avoid running
// goroutines that may create files while the listener is being opened.
func listenUnix(socketPath string) (net.Listener, error) {
	if err := removeStaleSocket(socketPath); err != nil {
		return nil, err
	}

	oldUmask := unix.Umask(socketUmask)
	defer unix.Umask(oldUmask)

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("listening on unix socket %q: %w", socketPath, err)
	}

	return listener, nil
}

// removeStaleSocket removes a Unix domain socket left behind at socketPath by
// a previous run of the plugin. It refuses to remove anything that is not
// itself a socket, so a misconfigured socket path can never cause the plugin
// to delete an unrelated file.
func removeStaleSocket(socketPath string) error {
	info, err := os.Lstat(socketPath)
	switch {
	case os.IsNotExist(err):
		return nil
	case err != nil:
		return fmt.Errorf("checking existing socket path %q: %w", socketPath, err)
	}
	if info.Mode().Type() != os.ModeSocket {
		return fmt.Errorf("refusing to remove %q: not a Unix domain socket", socketPath)
	}
	if err := os.Remove(socketPath); err != nil {
		return fmt.Errorf("removing stale socket %q: %w", socketPath, err)
	}
	return nil
}
