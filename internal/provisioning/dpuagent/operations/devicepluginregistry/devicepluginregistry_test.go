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

package devicepluginregistry

import (
	"context"
	"os"
	"path/filepath"

	"github.com/nvidia/doca-platform/cmd/dpuagent/opts"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("CleanPluginRegistry", func() {
	var tempDir string

	BeforeEach(func() {
		var err error
		tempDir, err = os.MkdirTemp("", "pluginregistry-test-*")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		Expect(os.RemoveAll(tempDir)).To(Succeed())
	})

	It("should be skipped if SkipCleanPluginRegistry is true", func() {
		operation := &CleanPluginRegistry{}
		Expect(operation.ShouldSkip(&operations.Context{
			Options: opts.Options{
				SkipCleanPluginRegistry: true,
			},
		})).To(BeTrue())
	})

	It("should remove stale device plugin registration sockets", func() {
		registryDir := filepath.Join(tempDir, "plugins_registry")
		Expect(os.MkdirAll(registryDir, 0755)).To(Succeed())
		staleSocket := filepath.Join(registryDir, "sriovdp.sock")
		Expect(os.WriteFile(staleSocket, []byte(""), 0644)).To(Succeed())
		keptFile := filepath.Join(registryDir, "not-a-socket.txt")
		Expect(os.WriteFile(keptFile, []byte(""), 0644)).To(Succeed())

		operation := &CleanPluginRegistry{
			pluginRegistryDir: registryDir,
			doneMarker:        filepath.Join(tempDir, "run", "kubelet-registry-cleaned"),
		}
		err := operation.Execute(context.Background(), &operations.Context{})
		Expect(err).NotTo(HaveOccurred())

		_, err = os.Stat(staleSocket)
		Expect(os.IsNotExist(err)).To(BeTrue())
		_, err = os.Stat(keptFile)
		Expect(err).NotTo(HaveOccurred())
	})

	It("should not fail if the plugin registry directory does not exist", func() {
		operation := &CleanPluginRegistry{
			pluginRegistryDir: filepath.Join(tempDir, "does-not-exist"),
			doneMarker:        filepath.Join(tempDir, "run", "kubelet-registry-cleaned"),
		}
		err := operation.Execute(context.Background(), &operations.Context{})
		Expect(err).NotTo(HaveOccurred())
	})

	It("should not repeat socket cleanup once the marker exists (same-boot restart)", func() {
		registryDir := filepath.Join(tempDir, "plugins_registry")
		Expect(os.MkdirAll(registryDir, 0755)).To(Succeed())
		liveSocket := filepath.Join(registryDir, "sriovdp.sock")
		Expect(os.WriteFile(liveSocket, []byte(""), 0644)).To(Succeed())

		markerPath := filepath.Join(tempDir, "run", "kubelet-registry-cleaned")
		Expect(os.MkdirAll(filepath.Dir(markerPath), 0755)).To(Succeed())
		Expect(os.WriteFile(markerPath, nil, 0644)).To(Succeed())

		operation := &CleanPluginRegistry{
			pluginRegistryDir: registryDir,
			doneMarker:        markerPath,
		}
		err := operation.Execute(context.Background(), &operations.Context{})
		Expect(err).NotTo(HaveOccurred())

		_, err = os.Stat(liveSocket)
		Expect(err).NotTo(HaveOccurred())
	})
})
