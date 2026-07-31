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
	"errors"
	"fmt"
	"os"
	"path/filepath"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/cmd/dpuagent/opts"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"
	"github.com/nvidia/doca-platform/internal/provisioning/utils/filesystem"

	"github.com/BurntSushi/toml"
	"github.com/Masterminds/semver/v3"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	containerdV1VersionOutput = "containerd github.com/containerd/containerd v1.7.20 8fc6bcff51318944179630522a095cc9dbf9f353"
	containerdV2VersionOutput = "containerd github.com/containerd/containerd/v2 2.2.1"
)

// getNestedValue navigates a map by keys and returns the value, or nil if not found.
func getNestedValue(m map[string]interface{}, keys ...string) interface{} {
	var val interface{} = m
	for _, key := range keys {
		mVal, ok := val.(map[string]interface{})
		if !ok {
			return nil
		}
		val = mVal[key]
	}
	return val
}

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
		It("should use config-mlnx.toml when config.toml is absent", func() {
			configPath := filepath.Join(tempDir, "/etc/containerd/config.toml")
			Expect(os.Remove(configPath)).To(Succeed())

			mlnxPath := filepath.Join(tempDir, "/etc/containerd/config-mlnx.toml")
			Expect(os.WriteFile(mlnxPath, []byte("version = 2\n"), 0644)).To(Succeed())

			operation := &ConfigureContainerd{
				rootFS: tempDir,
				getContainerdVersion: func() (string, error) {
					return containerdV1VersionOutput, nil
				},
			}

			err := operation.configureRegistryMirror("my.registry.com")
			Expect(err).NotTo(HaveOccurred())

			var config map[string]interface{}
			_, err = toml.DecodeFile(mlnxPath, &config)
			Expect(err).NotTo(HaveOccurred())

			registry := config["plugins"].(map[string]interface{})["io.containerd.grpc.v1.cri"].(map[string]interface{})["registry"].(map[string]interface{})
			mirror := registry["mirrors"].(map[string]interface{})["nvcr.io"].(map[string]interface{})
			endpoints := mirror["endpoint"].([]interface{})
			Expect(endpoints).To(HaveLen(1))
			Expect(endpoints[0]).To(Equal("my.registry.com"))
		})

		It("should skip if SkipContainerdConfigration is true", func() {
			operation := &ConfigureContainerd{}
			Expect(operation.ShouldSkip(&operations.Context{
				Options: opts.Options{
					SkipContainerdConfigration: true,
				},
			})).To(BeTrue())
		})

		It("should configure TLS compatibility and restart containerd when RegistryEndpoint is empty", func() {
			var executedCmds []string
			operation := &ConfigureContainerd{
				rootFS: tempDir,
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					executedCmds = append(executedCmds, cmd)
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
			Expect(executedCmds).To(Equal([]string{
				"systemctl daemon-reload",
				"systemctl stop containerd",
				"systemctl enable --now containerd",
			}))

			dropInPath := filepath.Join(tempDir, containerdSystemdDropInDir, containerdTLSDropInFile)
			content, err := os.ReadFile(dropInPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(content)).To(Equal(containerdTLSDropInContent))
			info, err := os.Stat(dropInPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(info.Mode().Perm()).To(Equal(os.FileMode(0644)))

			restartMarker := filepath.Join(tempDir, containerdRestartMarker)
			Expect(restartMarker).NotTo(BeAnExistingFile())
		})

		It("should add mirrors and not disable TLS verification", func() {
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
      config_path = "/etc/containerd/certs.d"
      [plugins."io.containerd.grpc.v1.cri".registry.mirrors]
        [plugins."io.containerd.grpc.v1.cri".registry.mirrors."docker.io"]
          endpoint = ["dockerhub.nvidia.com"]			
`
			configPath := filepath.Join(tempDir, "/etc/containerd/config.toml")
			Expect(os.WriteFile(configPath, []byte(originalContent), 0644)).To(Succeed())
			operation := &ConfigureContainerd{
				rootFS: tempDir,
				getContainerdVersion: func() (string, error) {
					return containerdV1VersionOutput, nil
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

			// TLS verification must not be disabled: the registry configs section is
			// written with insecure_skip_verify set to false for the nvcr.io mirror.
			tls := getNestedValue(config, "plugins", "io.containerd.grpc.v1.cri", "registry", "configs", "nvcr.io", "tls")
			Expect(tls).NotTo(BeNil())
			tlsMap := tls.(map[string]interface{})
			Expect(tlsMap["insecure_skip_verify"]).To(BeFalse())

			// Check Mirrors
			originalMirror := getNestedValue(config, "plugins", "io.containerd.grpc.v1.cri", "registry", "mirrors", "docker.io")
			Expect(originalMirror).NotTo(BeNil())
			originalMirrorMap := originalMirror.(map[string]interface{})
			originalEndpoints := originalMirrorMap["endpoint"].([]interface{})
			Expect(originalEndpoints).To(HaveLen(1))
			Expect(originalEndpoints[0]).To(Equal("dockerhub.nvidia.com"))

			mirror := getNestedValue(config, "plugins", "io.containerd.grpc.v1.cri", "registry", "mirrors", "nvcr.io")
			Expect(mirror).NotTo(BeNil())
			mirrorMap := mirror.(map[string]interface{})
			endpoints := mirrorMap["endpoint"].([]interface{})
			Expect(endpoints).To(HaveLen(1))
			Expect(endpoints[0]).To(Equal("my.registry.com"))

			// config_path should be removed once mirrors endpoint is configured.
			configPathValue := getNestedValue(config, "plugins", "io.containerd.grpc.v1.cri", "registry", "config_path")
			Expect(configPathValue).To(BeNil())

			dropInPath := filepath.Join(tempDir, containerdSystemdDropInDir, containerdTLSDropInFile)
			Expect(dropInPath).To(BeAnExistingFile())
		})

		It("should reuse config_path already declared under the containerd v2 images plugin", func() {
			originalContent := `
version = 2

[plugins]
  [plugins."io.containerd.cri.v1.images"]
    [plugins."io.containerd.cri.v1.images".registry]
      config_path = "/etc/containerd/certs.d"
`
			configPath := filepath.Join(tempDir, "/etc/containerd/config.toml")
			Expect(os.WriteFile(configPath, []byte(originalContent), 0644)).To(Succeed())

			operation := &ConfigureContainerd{
				rootFS: tempDir,
				getContainerdVersion: func() (string, error) {
					return containerdV2VersionOutput, nil
				},
			}

			err := operation.configureRegistryMirror("my.registry.com")
			Expect(err).NotTo(HaveOccurred())

			// The mirror must be written as a hosts.toml host-config file.
			hostsPath := filepath.Join(tempDir, "/etc/containerd/certs.d/nvcr.io/hosts.toml")
			var hosts map[string]interface{}
			_, err = toml.DecodeFile(hostsPath, &hosts)
			Expect(err).NotTo(HaveOccurred())
			Expect(hosts["server"]).To(Equal("https://nvcr.io"))
			host := hosts["host"].(map[string]interface{})["https://my.registry.com"].(map[string]interface{})
			Expect(host["capabilities"].([]interface{})).To(ConsistOf("pull", "resolve"))
			Expect(host["skip_verify"]).To(BeFalse())

			// The inline registry.mirrors format must NOT be used for containerd v2.
			var config map[string]interface{}
			_, err = toml.DecodeFile(configPath, &config)
			Expect(err).NotTo(HaveOccurred())
			Expect(getNestedValue(config, "plugins", "io.containerd.cri.v1.images", "registry", "mirrors")).To(BeNil())
			// The existing images-plugin config_path must be preserved.
			Expect(getNestedValue(config, "plugins", "io.containerd.cri.v1.images", "registry", "config_path")).To(Equal("/etc/containerd/certs.d"))
			Expect(filepath.Join(tempDir, containerdRestartMarker)).NotTo(BeAnExistingFile())
		})

		It("should preserve a scheme-qualified endpoint in containerd v2 hosts.toml", func() {
			operation := &ConfigureContainerd{
				rootFS: tempDir,
				getContainerdVersion: func() (string, error) {
					return containerdV2VersionOutput, nil
				},
			}

			err := operation.configureRegistryMirror("https://my.registry.com")
			Expect(err).NotTo(HaveOccurred())

			hostsPath := filepath.Join(tempDir, "/etc/containerd/certs.d/nvcr.io/hosts.toml")
			var hosts map[string]interface{}
			_, err = toml.DecodeFile(hostsPath, &hosts)
			Expect(err).NotTo(HaveOccurred())
			host := hosts["host"].(map[string]interface{})["https://my.registry.com"].(map[string]interface{})
			Expect(host["capabilities"].([]interface{})).To(ConsistOf("pull", "resolve"))
			Expect(host["skip_verify"]).To(BeFalse())
		})

		It("should reuse a legacy v1 config_path but record it under the images plugin for containerd v2", func() {
			// A system migrated from containerd 1.x may only declare config_path under
			// the legacy CRI plugin. containerd 2.x reads it from the images plugin, so
			// the value must be reused there for the mirror to take effect.
			originalContent := `
version = 2

[plugins]
  [plugins."io.containerd.grpc.v1.cri"]
    [plugins."io.containerd.grpc.v1.cri".registry]
      config_path = "/etc/containerd/certs.d"
`
			configPath := filepath.Join(tempDir, "/etc/containerd/config.toml")
			Expect(os.WriteFile(configPath, []byte(originalContent), 0644)).To(Succeed())

			operation := &ConfigureContainerd{
				rootFS: tempDir,
				getContainerdVersion: func() (string, error) {
					return containerdV2VersionOutput, nil
				},
			}

			err := operation.configureRegistryMirror("my.registry.com")
			Expect(err).NotTo(HaveOccurred())

			// hosts.toml is written under the reused host-config directory.
			hostsPath := filepath.Join(tempDir, "/etc/containerd/certs.d/nvcr.io/hosts.toml")
			var hosts map[string]interface{}
			_, err = toml.DecodeFile(hostsPath, &hosts)
			Expect(err).NotTo(HaveOccurred())
			Expect(hosts["server"]).To(Equal("https://nvcr.io"))

			var config map[string]interface{}
			_, err = toml.DecodeFile(configPath, &config)
			Expect(err).NotTo(HaveOccurred())
			// The reused path must now also be declared under the images plugin that
			// containerd 2.x actually reads.
			Expect(getNestedValue(config, "plugins", "io.containerd.cri.v1.images", "registry", "config_path")).To(Equal("/etc/containerd/certs.d"))
		})

		It("should add config_path for containerd v2 when absent", func() {
			operation := &ConfigureContainerd{
				rootFS: tempDir,
				getContainerdVersion: func() (string, error) {
					return containerdV2VersionOutput, nil
				},
			}

			err := operation.configureRegistryMirror("my.registry.com")
			Expect(err).NotTo(HaveOccurred())

			configPath := filepath.Join(tempDir, "/etc/containerd/config.toml")
			var config map[string]interface{}
			_, err = toml.DecodeFile(configPath, &config)
			Expect(err).NotTo(HaveOccurred())
			// config_path should be recorded under the containerd 2.x images plugin.
			Expect(getNestedValue(config, "plugins", "io.containerd.cri.v1.images", "registry", "config_path")).To(Equal("/etc/containerd/certs.d"))

			// hosts.toml should be created under the default host-config directory.
			hostsPath := filepath.Join(tempDir, "/etc/containerd/certs.d/nvcr.io/hosts.toml")
			var hosts map[string]interface{}
			_, err = toml.DecodeFile(hostsPath, &hosts)
			Expect(err).NotTo(HaveOccurred())
			Expect(hosts["server"]).To(Equal("https://nvcr.io"))
			Expect(filepath.Join(tempDir, containerdRestartMarker)).To(BeAnExistingFile())
		})
	})

	Context("TLS compatibility drop-in", func() {
		It("should create the managed drop-in and only mark content changes", func() {
			operation := &ConfigureContainerd{rootFS: tempDir}

			changed, err := operation.ensureTLSCompatibilityDropIn()
			Expect(err).NotTo(HaveOccurred())
			Expect(changed).To(BeTrue())

			dropInPath := filepath.Join(tempDir, containerdSystemdDropInDir, containerdTLSDropInFile)
			content, err := os.ReadFile(dropInPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(content)).To(Equal(containerdTLSDropInContent))
			info, err := os.Stat(dropInPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(info.Mode().Perm()).To(Equal(os.FileMode(0644)))

			Expect(operation.removeRestartMarker()).To(Succeed())
			changed, err = operation.ensureTLSCompatibilityDropIn()
			Expect(err).NotTo(HaveOccurred())
			Expect(changed).To(BeFalse())
			Expect(filepath.Join(tempDir, containerdRestartMarker)).NotTo(BeAnExistingFile())
		})

		It("should replace a different drop-in and create the marker first", func() {
			dropInDir := filepath.Join(tempDir, containerdSystemdDropInDir)
			Expect(os.MkdirAll(dropInDir, 0755)).To(Succeed())
			dropInPath := filepath.Join(dropInDir, containerdTLSDropInFile)
			Expect(os.WriteFile(dropInPath, []byte("old content"), 0600)).To(Succeed())

			operation := &ConfigureContainerd{rootFS: tempDir}
			changed, err := operation.ensureTLSCompatibilityDropIn()
			Expect(err).NotTo(HaveOccurred())
			Expect(changed).To(BeTrue())
			Expect(os.ReadFile(dropInPath)).To(Equal([]byte(containerdTLSDropInContent)))
			Expect(filepath.Join(tempDir, containerdRestartMarker)).To(BeAnExistingFile())
		})

		It("should not modify the drop-in when creating the marker fails", func() {
			dropInDir := filepath.Join(tempDir, containerdSystemdDropInDir)
			Expect(os.MkdirAll(dropInDir, 0755)).To(Succeed())
			dropInPath := filepath.Join(dropInDir, containerdTLSDropInFile)
			Expect(os.WriteFile(dropInPath, []byte("old content"), 0644)).To(Succeed())

			Expect(os.WriteFile(filepath.Join(tempDir, "run"), []byte("not a directory"), 0644)).To(Succeed())
			operation := &ConfigureContainerd{rootFS: tempDir}
			changed, err := operation.ensureTLSCompatibilityDropIn()
			Expect(err).To(HaveOccurred())
			Expect(changed).To(BeFalse())
			content, readErr := os.ReadFile(dropInPath)
			Expect(readErr).NotTo(HaveOccurred())
			Expect(string(content)).To(Equal("old content"))
		})

		It("should create the restart marker idempotently", func() {
			operation := &ConfigureContainerd{rootFS: tempDir}
			Expect(operation.createRestartMarker()).To(Succeed())
			Expect(operation.createRestartMarker()).To(Succeed())
			Expect(filepath.Join(tempDir, containerdRestartMarker)).To(BeAnExistingFile())
		})

		It("should preserve the marker and old content when the target write fails", func() {
			targetPath := filepath.Join(tempDir, "restart-sensitive.conf")
			Expect(os.WriteFile(targetPath, []byte("old content"), 0644)).To(Succeed())

			operation := &ConfigureContainerd{
				rootFS: tempDir,
				atomicWrite: func(name string, data []byte, perm os.FileMode) error {
					if name == targetPath {
						return errors.New("injected atomic write failure")
					}
					return filesystem.AtomicWrite(name, data, perm)
				},
			}
			changed, err := operation.writeRestartSensitiveFile(targetPath, []byte("new content"))
			Expect(err).To(HaveOccurred())
			Expect(changed).To(BeFalse())
			content, readErr := os.ReadFile(targetPath)
			Expect(readErr).NotTo(HaveOccurred())
			Expect(string(content)).To(Equal("old content"))
			Expect(filepath.Join(tempDir, containerdRestartMarker)).To(BeAnExistingFile())
		})
	})

	Context("containerd service reconciliation", func() {
		It("should restart in order when a marker exists and remove it after success", func() {
			var executedCmds []string
			operation := &ConfigureContainerd{
				rootFS: tempDir,
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					executedCmds = append(executedCmds, cmd)
					return bytes.Buffer{}, bytes.Buffer{}, nil
				},
			}
			Expect(operation.createRestartMarker()).To(Succeed())

			Expect(operation.reconcileContainerdService()).To(Succeed())
			Expect(executedCmds).To(Equal([]string{
				"systemctl daemon-reload",
				"systemctl stop containerd",
				"systemctl enable --now containerd",
			}))
			Expect(filepath.Join(tempDir, containerdRestartMarker)).NotTo(BeAnExistingFile())
		})

		It("should only enable containerd when no marker exists", func() {
			var executedCmds []string
			operation := &ConfigureContainerd{
				rootFS: tempDir,
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					executedCmds = append(executedCmds, cmd)
					return bytes.Buffer{}, bytes.Buffer{}, nil
				},
			}

			Expect(operation.reconcileContainerdService()).To(Succeed())
			Expect(executedCmds).To(Equal([]string{"systemctl enable --now containerd"}))
		})

		It("should return an error and leave the marker when marker removal fails", func() {
			markerPath := filepath.Join(tempDir, containerdRestartMarker)
			Expect(os.MkdirAll(markerPath, 0755)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(markerPath, "keep"), []byte(""), 0644)).To(Succeed())

			var executedCmds []string
			operation := &ConfigureContainerd{
				rootFS: tempDir,
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					executedCmds = append(executedCmds, cmd)
					return bytes.Buffer{}, bytes.Buffer{}, nil
				},
			}

			Expect(operation.reconcileContainerdService()).NotTo(Succeed())
			Expect(executedCmds).To(Equal([]string{
				"systemctl daemon-reload",
				"systemctl stop containerd",
				"systemctl enable --now containerd",
			}))
			Expect(markerPath).To(BeADirectory())
		})

		DescribeTable("should preserve the marker after a systemctl failure",
			func(failingCommand string, expectedCommands []string) {
				var executedCmds []string
				operation := &ConfigureContainerd{
					rootFS: tempDir,
					runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
						executedCmds = append(executedCmds, cmd)
						if cmd == failingCommand {
							return bytes.Buffer{}, *bytes.NewBufferString("command failed"), errors.New("failed")
						}
						return bytes.Buffer{}, bytes.Buffer{}, nil
					},
				}
				Expect(operation.createRestartMarker()).To(Succeed())

				Expect(operation.reconcileContainerdService()).NotTo(Succeed())
				Expect(executedCmds).To(Equal(expectedCommands))
				Expect(filepath.Join(tempDir, containerdRestartMarker)).To(BeAnExistingFile())
			},
			Entry("daemon-reload failure",
				"systemctl daemon-reload",
				[]string{"systemctl daemon-reload"},
			),
			Entry("stop failure",
				"systemctl stop containerd",
				[]string{"systemctl daemon-reload", "systemctl stop containerd"},
			),
			Entry("enable failure",
				"systemctl enable --now containerd",
				[]string{"systemctl daemon-reload", "systemctl stop containerd", "systemctl enable --now containerd"},
			),
		)

		It("should recover from a marker left after an interrupted execution", func() {
			dropInDir := filepath.Join(tempDir, containerdSystemdDropInDir)
			Expect(os.MkdirAll(dropInDir, 0755)).To(Succeed())
			Expect(os.WriteFile(
				filepath.Join(dropInDir, containerdTLSDropInFile),
				[]byte(containerdTLSDropInContent),
				0644,
			)).To(Succeed())

			var executedCmds []string
			operation := &ConfigureContainerd{
				rootFS: tempDir,
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					executedCmds = append(executedCmds, cmd)
					return bytes.Buffer{}, bytes.Buffer{}, nil
				},
			}
			Expect(operation.createRestartMarker()).To(Succeed())

			err := operation.Execute(ctx, &operations.Context{DPUFlavor: provisioningv1.DPUFlavor{}})
			Expect(err).NotTo(HaveOccurred())
			Expect(executedCmds).To(Equal([]string{
				"systemctl daemon-reload",
				"systemctl stop containerd",
				"systemctl enable --now containerd",
			}))
			Expect(filepath.Join(tempDir, containerdRestartMarker)).NotTo(BeAnExistingFile())
		})

		It("should not restart containerd again when managed files are unchanged", func() {
			var executedCmds []string
			operation := &ConfigureContainerd{
				rootFS: tempDir,
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					executedCmds = append(executedCmds, cmd)
					return bytes.Buffer{}, bytes.Buffer{}, nil
				},
			}
			optCtx := &operations.Context{DPUFlavor: provisioningv1.DPUFlavor{}}

			Expect(operation.Execute(ctx, optCtx)).To(Succeed())
			executedCmds = nil
			Expect(operation.Execute(ctx, optCtx)).To(Succeed())
			Expect(executedCmds).To(Equal([]string{"systemctl enable --now containerd"}))
		})

		It("should stop before writing the drop-in or reconciling when registry configuration fails", func() {
			configPath := filepath.Join(tempDir, containerdConfigDir, "config.toml")
			Expect(os.WriteFile(configPath, []byte("invalid = ["), 0644)).To(Succeed())

			var executedCmds []string
			operation := &ConfigureContainerd{
				rootFS: tempDir,
				getContainerdVersion: func() (string, error) {
					return containerdV1VersionOutput, nil
				},
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					executedCmds = append(executedCmds, cmd)
					return bytes.Buffer{}, bytes.Buffer{}, nil
				},
			}
			optCtx := &operations.Context{
				DPUFlavor: provisioningv1.DPUFlavor{
					Spec: provisioningv1.DPUFlavorSpec{
						ContainerdConfig: provisioningv1.ContainerdConfig{
							RegistryEndpoint: "my.registry.com",
						},
					},
				},
			}

			Expect(operation.Execute(ctx, optCtx)).NotTo(Succeed())
			Expect(executedCmds).To(BeEmpty())
			Expect(filepath.Join(tempDir, containerdSystemdDropInDir, containerdTLSDropInFile)).NotTo(BeAnExistingFile())
		})
	})

	Context("Containerd Version", func() {
		It("should extract version from output", func() {
			testCases := []struct {
				output          string
				expectedVersion *semver.Version
			}{
				{
					output:          containerdV1VersionOutput,
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
