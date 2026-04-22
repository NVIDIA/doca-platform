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
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	defaultSocketPath = "/var/run/dpf/pldm-unpack.sock"
	defaultScriptPath = "/opt/pldm-unpack/fwpkg_unpack.py"
	allowedPathPrefix = "/bfb/"
)

type config struct {
	socketPath   string
	socketMode   os.FileMode
	scriptPath   string
	pythonBin    string
	requestTout  time.Duration
	shutdownTout time.Duration
}

type unpackRequest struct {
	PackagePath     string `json:"packagePath"`
	OutDir          string `json:"outDir,omitempty"`
	Verbose         bool   `json:"verbose,omitempty"`
	ShowPkgContent  bool   `json:"showPkgContent,omitempty"`
	ShowAllMetadata bool   `json:"showAllMetadata,omitempty"`
	DumpBuilderJSON bool   `json:"dumpBuilderJSON,omitempty"`
}

type unpackResponse struct {
	Success   bool   `json:"success"`
	ExitCode  int    `json:"exitCode"`
	Command   string `json:"command"`
	Stdout    string `json:"stdout"`
	Stderr    string `json:"stderr"`
	Error     string `json:"error,omitempty"`
	StartedAt string `json:"startedAt"`
	EndedAt   string `json:"endedAt"`
}

func parseFlags() (*config, error) {
	cfg := &config{}

	socketMode := flag.String("socket-mode", "0660", "unix socket mode (octal)")
	flag.StringVar(&cfg.socketPath, "socket-path", defaultSocketPath, "unix socket path for HTTP server")
	flag.StringVar(&cfg.scriptPath, "script-path", defaultScriptPath, "path to NVIDIA fwpkg_unpack.py script")
	flag.StringVar(&cfg.pythonBin, "python-bin", "python3", "python executable")
	flag.DurationVar(&cfg.requestTout, "request-timeout", 2*time.Minute, "timeout per unpack request")
	flag.DurationVar(&cfg.shutdownTout, "shutdown-timeout", 10*time.Second, "graceful shutdown timeout")
	flag.Parse()

	modeValue, err := parseFileMode(*socketMode)
	if err != nil {
		return nil, err
	}
	cfg.socketMode = modeValue
	return cfg, nil
}

func parseFileMode(raw string) (os.FileMode, error) {
	if raw == "" {
		return 0, errors.New("socket mode is empty")
	}
	var v uint32
	_, err := fmt.Sscanf(raw, "%o", &v)
	if err != nil {
		return 0, fmt.Errorf("invalid socket mode %q: %w", raw, err)
	}
	return os.FileMode(v), nil
}

func main() {
	cfg, err := parseFlags()
	if err != nil {
		log.Fatalf("failed to parse flags: %v", err)
	}
	if err := validateConfig(cfg); err != nil {
		log.Fatalf("invalid configuration: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthzHandler)
	mux.Handle("/v1/unpack", unpackHandler(cfg))

	server := &http.Server{
		Handler:           logRequests(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	listener, err := createUnixListener(cfg.socketPath, cfg.socketMode)
	if err != nil {
		log.Fatalf("failed to create unix socket listener: %v", err)
	}
	defer func() {
		if closeErr := listener.Close(); closeErr != nil {
			log.Printf("failed to close listener: %v", closeErr)
		}
	}()

	log.Printf("starting PLDM unpack server on unix://%s", cfg.socketPath)

	errCh := make(chan error, 1)
	go func() {
		if serveErr := server.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			errCh <- serveErr
		}
		close(errCh)
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	select {
	case sig := <-sigCh:
		log.Printf("received signal %s, shutting down", sig.String())
	case serveErr := <-errCh:
		if serveErr != nil {
			log.Fatalf("server exited with error: %v", serveErr)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.shutdownTout)
	defer cancel()
	if shutdownErr := server.Shutdown(ctx); shutdownErr != nil {
		log.Printf("graceful shutdown failed: %v", shutdownErr)
	}
}

func validateConfig(cfg *config) error {
	if strings.TrimSpace(cfg.socketPath) == "" {
		return errors.New("socket-path is empty")
	}
	if strings.TrimSpace(cfg.scriptPath) == "" {
		return errors.New("script-path is empty")
	}
	if strings.TrimSpace(cfg.pythonBin) == "" {
		return errors.New("python-bin is empty")
	}
	return nil
}

// validatePaths ensures that PackagePath and OutDir resolve to locations
// under the allowed directory tree, preventing path-traversal attacks.
func validatePaths(req *unpackRequest) error {
	cleaned := filepath.Clean(req.PackagePath)
	if !strings.HasPrefix(cleaned, allowedPathPrefix) {
		return fmt.Errorf("packagePath %q is outside the allowed directory %s", req.PackagePath, allowedPathPrefix)
	}
	req.PackagePath = cleaned

	if req.OutDir != "" {
		cleaned = filepath.Clean(req.OutDir)
		if !strings.HasPrefix(cleaned, allowedPathPrefix) {
			return fmt.Errorf("outDir %q is outside the allowed directory %s", req.OutDir, allowedPathPrefix)
		}
		req.OutDir = cleaned
	}
	return nil
}

func createUnixListener(socketPath string, mode os.FileMode) (net.Listener, error) {
	socketDir := filepath.Dir(socketPath)
	if err := os.MkdirAll(socketDir, 0o755); err != nil {
		return nil, fmt.Errorf("create socket dir %q: %w", socketDir, err)
	}

	if _, err := os.Stat(socketPath); err == nil {
		if rmErr := os.Remove(socketPath); rmErr != nil {
			return nil, fmt.Errorf("remove existing socket %q: %w", socketPath, rmErr)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("stat socket %q: %w", socketPath, err)
	}

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("listen unix %q: %w", socketPath, err)
	}
	if chmodErr := os.Chmod(socketPath, mode); chmodErr != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("chmod socket %q: %w", socketPath, chmodErr)
	}
	return listener, nil
}

func healthzHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func unpackHandler(cfg *config) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req unpackRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("invalid json body: %v", err), http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(req.PackagePath) == "" {
			http.Error(w, "packagePath is required", http.StatusBadRequest)
			return
		}
		if err := validatePaths(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		resp, status := runUnpack(r.Context(), cfg, &req)
		writeJSON(w, status, resp)
	})
}

func runUnpack(parent context.Context, cfg *config, req *unpackRequest) (*unpackResponse, int) {
	start := time.Now().UTC()

	ctx, cancel := context.WithTimeout(parent, cfg.requestTout)
	defer cancel()

	args, err := buildCommandArgs(cfg.scriptPath, req)
	if err != nil {
		return &unpackResponse{
			Success:   false,
			ExitCode:  -1,
			Command:   commandString(cfg.pythonBin, args),
			Error:     err.Error(),
			StartedAt: start.Format(time.RFC3339Nano),
			EndedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		}, http.StatusBadRequest
	}

	cmd := exec.CommandContext(ctx, cfg.pythonBin, args...)
	stdout, runErr := cmd.Output()
	stderr := ""
	exitCode := 0
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			exitCode = exitErr.ExitCode()
			stderr = string(exitErr.Stderr)
		} else {
			exitCode = -1
		}
	}

	response := &unpackResponse{
		Success:   runErr == nil,
		ExitCode:  exitCode,
		Command:   commandString(cfg.pythonBin, args),
		Stdout:    string(stdout),
		Stderr:    stderr,
		StartedAt: start.Format(time.RFC3339Nano),
		EndedAt:   time.Now().UTC().Format(time.RFC3339Nano),
	}

	if runErr != nil {
		response.Error = runErr.Error()
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return response, http.StatusGatewayTimeout
		}
		return response, http.StatusInternalServerError
	}

	return response, http.StatusOK
}

func buildCommandArgs(scriptPath string, req *unpackRequest) ([]string, error) {
	modeCount := 0
	if req.ShowPkgContent {
		modeCount++
	}
	if req.ShowAllMetadata {
		modeCount++
	}
	if req.DumpBuilderJSON {
		modeCount++
	}
	if modeCount > 1 {
		return nil, errors.New("showPkgContent, showAllMetadata and dumpBuilderJSON are mutually exclusive")
	}

	args := []string{scriptPath}
	switch {
	case req.ShowPkgContent:
		args = append(args, "--show_pkg_content")
	case req.ShowAllMetadata:
		args = append(args, "--show_all_metadata")
	case req.DumpBuilderJSON:
		args = append(args, "--dump_builder_json")
	default:
		args = append(args, "--unpack")
	}

	if req.OutDir != "" {
		args = append(args, "--outdir", req.OutDir)
	}
	if req.Verbose {
		args = append(args, "--verbose")
	}
	args = append(args, req.PackagePath)
	return args, nil
}

func commandString(bin string, args []string) string {
	all := make([]string, 0, len(args)+1)
	all = append(all, bin)
	all = append(all, args...)
	return strings.Join(all, " ")
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("failed to write json response: %v", err)
	}
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s from %s (%s)", r.Method, r.URL.Path, r.RemoteAddr, time.Since(start))
	})
}
