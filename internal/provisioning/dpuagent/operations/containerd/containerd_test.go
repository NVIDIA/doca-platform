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

package containerd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/cmd/dpuagent/opts"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"

	"github.com/BurntSushi/toml"
	"github.com/Masterminds/semver/v3"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Containerd Configuration", func() {
	var tempDir string

	BeforeEach(func() {
		var err error
		tempDir, err = os.MkdirTemp("", "containerd-test-*")
		Expect(err).NotTo(HaveOccurred())
		filePath := filepath.Join(tempDir, "/etc/containerd/config.toml")
		Expect(os.MkdirAll(filepath.Dir(filePath), 0755)).To(Succeed())
		Expect(os.WriteFile(filePath, []byte(""), 0644)).To(Succeed())
	})

	AfterEach(func() {
		Expect(os.RemoveAll(tempDir)).To(Succeed())
	})

	Context("Containerd Configuration", func() {
		It("should skip if SkipContainerdConfigration is true", func() {
			operation := &ConfigureContainerd{}
			Expect(operation.ShouldSkip(&operations.Context{
				Options: opts.Options{
					SkipContainerdConfigration: true,
				},
			})).To(BeTrue())
		})

		It("should start containerd even when RegistryEndpoint is empty", func() {
			var executedCmd string
			operation := &ConfigureContainerd{
				rootFS: tempDir,
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					executedCmd = cmd
					return bytes.Buffer{}, bytes.Buffer{}, nil
				},
			}
			err := operation.Execute(ctx, &operations.Context{
				DPUFlavor: provisioningv1.DPUFlavor{
					Spec: provisioningv1.DPUFlavorSpec{
						ContainerdConfig: provisioningv1.ContainerdConfig{
							RegistryEndpoint: "",
						},
					},
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(executedCmd).To(Equal("systemctl enable --now containerd"))
		})

		It("should add TLS and mirrors", func() {
			originalContent := `
version = 2
root = "/var/lib/containerd"
state = "/run/containerd"
oom_score = 0

[grpc]
  max_recv_message_size = 16777216
  max_send_message_size = 16777216

[metrics]
  address = ""
  grpc_histogram = false

[plugins]
  [plugins."io.containerd.grpc.v1.cri"]
    sandbox_image = "registry.k8s.io/pause:3.9"
    [plugins."io.containerd.grpc.v1.cri".containerd]
      default_runtime_name = "runc"
      [plugins."io.containerd.grpc.v1.cri".containerd.runtimes]
        [plugins."io.containerd.grpc.v1.cri".containerd.runtimes.runc]
          runtime_type = "io.containerd.runc.v2"
          [plugins."io.containerd.grpc.v1.cri".containerd.runtimes.runc.options]
            systemdCgroup = true
    [plugins."io.containerd.grpc.v1.cri".registry]
      [plugins."io.containerd.grpc.v1.cri".registry.mirrors]
        [plugins."io.containerd.grpc.v1.cri".registry.mirrors."docker.io"]
          endpoint = ["dockerhub.nvidia.com"]			
`
			configPath := filepath.Join(tempDir, "/etc/containerd/config.toml")
			Expect(os.WriteFile(configPath, []byte(originalContent), 0644)).To(Succeed())
			operation := &ConfigureContainerd{
				rootFS: tempDir,
				getContainerdVersion: func() (string, error) {
					return "containerd github.com/containerd/containerd v1.7.20 8fc6bcff51318944179630522a095cc9dbf9f353", nil
				},
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					return bytes.Buffer{}, bytes.Buffer{}, nil
				},
			}
			err := operation.Execute(ctx, &operations.Context{
				Options: opts.Options{
					SkipDNSConfig: false,
				},
				DPUFlavor: provisioningv1.DPUFlavor{
					Spec: provisioningv1.DPUFlavorSpec{
						ContainerdConfig: provisioningv1.ContainerdConfig{
							RegistryEndpoint: "my.registry.com",
						},
					},
				},
			})
			Expect(err).NotTo(HaveOccurred())

			content, err := os.ReadFile(configPath)
			Expect(err).NotTo(HaveOccurred())
			By(fmt.Sprintf("content: %s", string(content)))

			// Verify Content
			var config map[string]interface{}
			_, err = toml.DecodeFile(configPath, &config)
			Expect(err).NotTo(HaveOccurred())

			// Helper to navigate map safely
			get := func(m map[string]interface{}, keys ...string) interface{} {
				var val interface{} = m
				for _, key := range keys {
					if mVal, ok := val.(map[string]interface{}); ok {
						val = mVal[key]
					} else {
						return nil
					}
				}
				return val
			}

			// Check TLS
			tls := get(config, "plugins", "io.containerd.grpc.v1.cri", "registry", "configs", "nvcr.io", "tls")
			Expect(tls).NotTo(BeNil())
			tlsMap := tls.(map[string]interface{})
			Expect(tlsMap["insecure_skip_verify"]).To(BeTrue())

			// Check Mirrors
			originalMirror := get(config, "plugins", "io.containerd.grpc.v1.cri", "registry", "mirrors", "docker.io")
			Expect(originalMirror).NotTo(BeNil())
			originalMirrorMap := originalMirror.(map[string]interface{})
			originalEndpoints := originalMirrorMap["endpoint"].([]interface{})
			Expect(originalEndpoints).To(HaveLen(1))
			Expect(originalEndpoints[0]).To(Equal("dockerhub.nvidia.com"))

			mirror := get(config, "plugins", "io.containerd.grpc.v1.cri", "registry", "mirrors", "nvcr.io")
			Expect(mirror).NotTo(BeNil())
			mirrorMap := mirror.(map[string]interface{})
			endpoints := mirrorMap["endpoint"].([]interface{})
			Expect(endpoints).To(HaveLen(1))
			Expect(endpoints[0]).To(Equal("my.registry.com"))
		})
	})

	Context("Containerd Version", func() {
		It("should extract version from output", func() {
			testCases := []struct {
				output          string
				expectedVersion *semver.Version
			}{
				{
					output:          "containerd github.com/containerd/containerd v1.7.20 8fc6bcff51318944179630522a095cc9dbf9f353",
					expectedVersion: semver.MustParse("1.7.20"),
				},
				{
					output:          "containerd github.com/containerd/containerd 1.7.27",
					expectedVersion: semver.MustParse("1.7.27"),
				},
			}
			for _, testCase := range testCases {
				operation := ConfigureContainerd{
					getContainerdVersion: func() (string, error) {
						return testCase.output, nil
					},
				}
				version, err := operation.containerdVersion()
				Expect(err).NotTo(HaveOccurred())
				Expect(version.Equal(testCase.expectedVersion)).To(BeTrue())
			}
		})
	})
})
