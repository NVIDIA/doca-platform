//go:build linux

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
	"encoding/json"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	"github.com/onsi/gomega/gbytes"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// TestMainBootstrapAbortStartsWorkers verifies fallback startup and process survival after an early bootstrap abort.
func TestMainBootstrapAbortStartsWorkers(t *testing.T) {
	if os.Getenv("DPF_TEST_AGENT_MAIN") == "1" {
		os.Args = append(os.Args[:1], flag.Args()...)
		main()
		return
	}
	// Run removes a stale marker before checking the DPU; never touch a real agent's state.
	if _, err := os.Stat("/run/dpu-agent"); !os.IsNotExist(err) {
		t.Skip("process test requires /run/dpu-agent to be absent")
	}
	server := missingDPUAPIServer(t)
	t.Cleanup(server.Close)
	dir := t.TempDir()
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "openssl"), []byte("#!/bin/sh\nexit 1\n"), 0700))
	tokenPath := filepath.Join(dir, "token")
	require.NoError(t, os.WriteFile(tokenPath, []byte("test-token"), 0600))
	kubeconfig := filepath.Join(dir, "kubeconfig")
	require.NoError(t, clientcmd.WriteToFile(clientcmdapi.Config{
		Clusters:  map[string]*clientcmdapi.Cluster{"test": {Server: server.URL}},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{"test": {TokenFile: tokenPath}},
		Contexts: map[string]*clientcmdapi.Context{"test": {
			Cluster:  "test",
			AuthInfo: "test",
		}},
		CurrentContext: "test",
	}, kubeconfig))
	executable, err := os.Executable()
	require.NoError(t, err)
	cmd := exec.CommandContext(t.Context(), executable, "-test.run=^TestMainBootstrapAbortStartsWorkers$", "--",
		"--zero-trust-mode", "--spiffe-mode", "--kubeconfig="+kubeconfig, "--token-file-path="+tokenPath,
		"--dpu-name=test-dpu", "--dpu-namespace=test-ns", "--dpu-uid=test-uid",
		"--skip-configure-kubelet", "--skip-reboot-method-discovery")
	cmd.Env = append(os.Environ(), "DPF_TEST_AGENT_MAIN=1")
	logs := gbytes.NewBuffer()
	cmd.Stdout, cmd.Stderr = logs, logs
	require.NoError(t, cmd.Start())
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
		close(done)
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("agent process did not stop")
		}
		if t.Failed() {
			t.Log(string(logs.Contents()))
		}
	})

	for _, message := range []string{
		"Bootstrap aborted for reprovision",
		"NIC provisioning: no local DMS session; skip runtime configuration loop",
		"Failed to report the SPIRE workload attestor condition",
		"Starting owned DPU reconcile loop",
	} {
		require.Eventually(t, func() bool { return strings.Contains(string(logs.Contents()), message) },
			5*time.Second, 10*time.Millisecond, "missing process behavior: %s", message)
	}
	require.NotContains(t, string(logs.Contents()), "[Start runtime workers]", "bootstrap must abort before the startup operation")
	select {
	case err := <-done:
		t.Fatalf("agent exited after expected bootstrap abort: %v", err)
	default:
	}
	require.NoError(t, cmd.Process.Signal(syscall.SIGTERM))
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("agent did not handle SIGTERM")
	}
	require.Contains(t, string(logs.Contents()), "DPUAgent stop signal received")
}

// missingDPUAPIServer provides discovery and an idle watch, but no DPU or CA bundle.
// The real main therefore aborts before hardware operations and keeps its workers alive.
func missingDPUAPIServer(t *testing.T) *httptest.Server {
	t.Helper()
	gv := provisioningv1.GroupVersion
	version := metav1.GroupVersionForDiscovery{
		GroupVersion: gv.String(),
		Version:      gv.Version,
	}
	group := metav1.APIGroup{
		Name:             gv.Group,
		Versions:         []metav1.GroupVersionForDiscovery{version},
		PreferredVersion: version,
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var response any
		switch r.URL.Path {
		case "/api":
			response = metav1.APIVersions{Versions: []string{"v1"}}
		case "/api/v1":
			response = metav1.APIResourceList{
				GroupVersion: "v1",
				APIResources: []metav1.APIResource{{
					Name:       "configmaps",
					Kind:       "ConfigMap",
					Namespaced: true,
				}},
			}
		case "/apis":
			response = metav1.APIGroupList{Groups: []metav1.APIGroup{group}}
		case "/apis/" + gv.Group:
			response = group
		case "/apis/" + gv.String():
			response = metav1.APIResourceList{
				GroupVersion: gv.String(),
				APIResources: []metav1.APIResource{{
					Name:       "dpus",
					Kind:       "DPU",
					Namespaced: true,
				}},
			}
		default:
			if strings.HasSuffix(r.URL.Path, "/dpus") && r.URL.Query().Get("watch") == "true" {
				if err := http.NewResponseController(w).Flush(); err != nil {
					t.Error(err)
					return
				}
				<-r.Context().Done()
				return
			}
			w.WriteHeader(http.StatusNotFound)
			response = metav1.Status{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Status",
				},
				Status: metav1.StatusFailure,
				Reason: metav1.StatusReasonNotFound,
				Code:   http.StatusNotFound,
			}
		}
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Error(err)
		}
	}))
}
