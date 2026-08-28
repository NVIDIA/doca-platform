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
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nvidia/doca-platform/cmd/dpuagent/opts"
	hostagenttypes "github.com/nvidia/doca-platform/internal/provisioning/hostagent/service/types"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestMainPackage(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "dpuagent main")
}

var _ = Describe("buildClientConfig SPIFFE mode", func() {
	It("waits for a non-empty token file then populates BearerTokenFile from kubeconfig", func() {
		dir := GinkgoT().TempDir()
		tokenPath := filepath.Join(dir, "token")
		kubeconfigPath := filepath.Join(dir, "kubeconfig")

		kubeconfig := []byte(`apiVersion: v1
clusters:
- cluster:
    server: https://10.0.0.1:6443
  name: default
contexts:
- context:
    cluster: default
    user: default
  name: default
current-context: default
kind: Config
users:
- name: default
  user:
    tokenFile: ` + tokenPath + `
`)
		Expect(os.WriteFile(kubeconfigPath, kubeconfig, 0600)).To(Succeed())

		writeErr := make(chan error, 1)
		go func() {
			time.Sleep(500 * time.Millisecond)
			writeErr <- os.WriteFile(tokenPath, []byte("jwt-token"), 0600)
		}()

		cfg, err := buildClientConfig(context.Background(), &opts.Options{
			SpiffeMode:        true,
			Kubeconfig:        kubeconfigPath,
			TokenFilePath:     tokenPath,
			DPUName:           "dpu",
			DPUUID:            "uid",
			DPUFlavor:         "/tmp/flavor.yaml",
			KubeadmSecretName: "secret",
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.BearerTokenFile).To(Equal(tokenPath))
		Expect(<-writeErr).To(Succeed())
	})

	It("returns token-wait error without loading kubeconfig when context is canceled", func() {
		dir := GinkgoT().TempDir()
		tokenPath := filepath.Join(dir, "missing-token")
		kubeconfigPath := filepath.Join(dir, "missing-kubeconfig")

		cctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := buildClientConfig(cctx, &opts.Options{
			SpiffeMode:        true,
			Kubeconfig:        kubeconfigPath,
			TokenFilePath:     tokenPath,
			DPUName:           "dpu",
			DPUUID:            "uid",
			DPUFlavor:         "/tmp/flavor.yaml",
			KubeadmSecretName: "secret",
		})
		Expect(err).To(HaveOccurred())
		Expect(errors.Is(err, context.Canceled)).To(BeTrue())
		Expect(err.Error()).NotTo(ContainSubstring("loading SPIFFE kubeconfig"))
	})

	It("times out when the token file stays empty", func() {
		dir := GinkgoT().TempDir()
		tokenPath := filepath.Join(dir, "missing-token")

		start := time.Now()
		err := waitForNonEmptyTokenFile(context.Background(), tokenPath, 2*time.Second)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("context deadline exceeded"))
		Expect(time.Since(start)).To(BeNumerically(">=", time.Second))
		Expect(time.Since(start)).To(BeNumerically("<", 5*time.Second))
	})

	It("returns promptly when the context is canceled", func() {
		dir := GinkgoT().TempDir()
		tokenPath := filepath.Join(dir, "missing-token")
		cctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := waitForNonEmptyTokenFile(cctx, tokenPath, time.Minute)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("context canceled"))
	})
})

var _ = Describe("postClockReport", func() {
	request := hostagenttypes.ReportClockRequest{
		DPUName:      "worker2-mt2516604wwn",
		DPUNamespace: "dpf-operator-system",
		DPUUID:       "dpu-uid",
		DPUTime:      metav1.NewTime(time.Date(2026, 8, 17, 6, 28, 9, 0, time.UTC)),
	}

	It("posts the DPU clock and identity to the hostagent", func() {
		received := make(chan hostagenttypes.ReportClockRequest, 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(r.Body)
			Expect(err).NotTo(HaveOccurred())
			var got hostagenttypes.ReportClockRequest
			Expect(json.Unmarshal(body, &got)).To(Succeed())
			Expect(r.URL.Path).To(Equal("/report-clock"))
			Expect(r.Header.Get("Content-Type")).To(Equal("application/json"))
			received <- got
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		Expect(postClockReport(context.Background(), server.URL, request)).To(Succeed())

		got := <-received
		Expect(got.DPUName).To(Equal(request.DPUName))
		Expect(got.DPUNamespace).To(Equal(request.DPUNamespace))
		Expect(got.DPUUID).To(Equal(request.DPUUID))
		Expect(got.DPUTime.UTC()).To(Equal(request.DPUTime.UTC()))
	})

	It("fails when the hostagent rejects the report", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusConflict)
		}))
		defer server.Close()

		err := postClockReport(context.Background(), server.URL, request)
		Expect(err).To(MatchError(ContainSubstring("hostagent rejected the clock report")))
	})

	It("fails when the hostagent is unreachable", func() {
		server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		address := server.URL
		server.Close()

		Expect(postClockReport(context.Background(), address, request)).NotTo(Succeed())
	})
})
