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

package sfconfig

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	opts "github.com/nvidia/doca-platform/cmd/dpuagent/opts"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("SFConfig", func() {
	var tempDir string
	// var sysClassNet string

	BeforeEach(func() {
		var err error
		tempDir, err = os.MkdirTemp("", "sfconfig-test-*")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		Expect(os.RemoveAll(tempDir)).To(Succeed())
	})

	Context("set SF", func() {
		It("should be skipped if SkipSFConfig is true", func() {
			operation := &CreateSF{}
			Expect(operation.ShouldSkip(&operations.Context{Options: opts.Options{SkipSFConfig: true}})).To(BeTrue())
		})

		It("should set SF", func() {
			By("mock the DPUFlavor")
			dpuFlavor := provisioningv1.DPUFlavor{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						cutil.TrustedSFCount: "2",
					},
				},
				Spec: provisioningv1.DPUFlavorSpec{
					NVConfig: []provisioningv1.NVConfig{
						{
							Parameters: []string{"PF_TOTAL_SF=4"},
						},
					},
				},
			}

			By("mock the mlnx-sf output")
			mlnxsfOutput := `
{
    "pci/0000:03:00.0/229376": {
        "aux_dev": "mlx5_core.sf.2",
        "sf_netdev": "enp3s0f0s0"
    },
    "pci/0000:03:00.1/294912": {
        "aux_dev": "mlx5_core.sf.3",
        "sf_netdev": "enp3s0f1s0"
    },
    "pci/0000:03:00.100/294913": {
        "aux_dev": "mlx5_core.sf.4",
        "sf_netdev": "enp3s0f100s0"
    },
    "pci/0000:03:00.101/294914": {
        "aux_dev": "mlx5_core.sf.5",
        "sf_netdev": "enp3s0f101s0"
    }
}
`
			By("mock the system files that is aligned with the mlnx-sf output")
			Expect(os.MkdirAll(filepath.Dir(filepath.Join(tempDir, "sys/class/net/enp3s0f0s0/address")), 0755)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(tempDir, "sys/class/net/enp3s0f0s0/address"), []byte("02:36:17:17:a9:b0"), 0444)).To(Succeed())

			Expect(os.MkdirAll(filepath.Dir(filepath.Join(tempDir, "sys/class/net/enp3s0f1s0/address")), 0755)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(tempDir, "sys/class/net/enp3s0f1s0/address"), []byte("02:36:17:17:a9:b1"), 0444)).To(Succeed())

			Expect(os.MkdirAll(filepath.Dir(filepath.Join(tempDir, "sys/class/net/enp3s0f100s0/address")), 0755)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(tempDir, "sys/class/net/enp3s0f100s0/address"), []byte("02:36:17:17:a9:b2"), 0444)).To(Succeed())

			Expect(os.MkdirAll(filepath.Dir(filepath.Join(tempDir, "sys/class/net/enp3s0f101s0/address")), 0755)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(tempDir, "sys/class/net/enp3s0f101s0/address"), []byte("02:36:17:17:a9:b3"), 0444)).To(Succeed())

			Expect(os.MkdirAll(filepath.Dir(filepath.Join(tempDir, "sys/bus/auxiliary/devices/mlx5_core.sf.2/driver/unbind")), 0777)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(tempDir, "sys/bus/auxiliary/devices/mlx5_core.sf.2/driver/unbind"), []byte(""), 0200)).To(Succeed())

			Expect(os.MkdirAll(filepath.Dir(filepath.Join(tempDir, "sys/bus/auxiliary/devices/mlx5_core.sf.3/driver/unbind")), 0777)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(tempDir, "sys/bus/auxiliary/devices/mlx5_core.sf.3/driver/unbind"), []byte(""), 0200)).To(Succeed())

			Expect(os.MkdirAll(filepath.Dir(filepath.Join(tempDir, "sys/bus/auxiliary/devices/mlx5_core.sf.4/driver/unbind")), 0777)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(tempDir, "sys/bus/auxiliary/devices/mlx5_core.sf.4/driver/unbind"), []byte(""), 0200)).To(Succeed())

			Expect(os.MkdirAll(filepath.Dir(filepath.Join(tempDir, "sys/bus/auxiliary/devices/mlx5_core.sf.5/driver/unbind")), 0777)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(tempDir, "sys/bus/auxiliary/devices/mlx5_core.sf.5/driver/unbind"), []byte(""), 0200)).To(Succeed())

			Expect(os.MkdirAll(filepath.Dir(filepath.Join(tempDir, "sys/bus/auxiliary/drivers/mlx5_core.sf/bind")), 0777)).To(Succeed())

			By("list the expected bash commands (in the expected order) and their outputs")
			type expectedCommand struct {
				cmd    string
				stdout string
			}
			orderedCommands := []expectedCommand{
				{cmd: "/sbin/mlnx-sf --action create --device 0000:03:00.0 --sfnum 0", stdout: ""},
				{cmd: "/sbin/mlnx-sf --action create --device 0000:03:00.0 --sfnum 1", stdout: ""},
				{cmd: "/sbin/mlnx-sf --action create --device 0000:03:00.0 --sfnum 101 -t", stdout: ""},
				{cmd: "/sbin/mlnx-sf --action create --device 0000:03:00.0 --sfnum 102 -t", stdout: ""},
				{cmd: "mlnx-sf -a show -j", stdout: mlnxsfOutput},
			}
			unorderedCommands := []expectedCommand{
				{cmd: "/opt/mellanox/iproute2/sbin/mlxdevm port function set pci/0000:03:00.0/229376 hw_addr 02:36:17:17:a9:b0", stdout: ""},
				{cmd: "/opt/mellanox/iproute2/sbin/mlxdevm port function set pci/0000:03:00.1/294912 hw_addr 02:36:17:17:a9:b1", stdout: ""},
				{cmd: "/opt/mellanox/iproute2/sbin/mlxdevm port function set pci/0000:03:00.100/294913 hw_addr 02:36:17:17:a9:b2", stdout: ""},
				{cmd: "/opt/mellanox/iproute2/sbin/mlxdevm port function set pci/0000:03:00.101/294914 hw_addr 02:36:17:17:a9:b3", stdout: ""},
			}

			cmdIdx := 0
			operation := &CreateSF{
				rootFS: tempDir,
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					defer func() {
						cmdIdx++
					}()

					var expected expectedCommand
					if cmdIdx < len(orderedCommands) {
						By(fmt.Sprintf("expecting command: %s, received: %s", orderedCommands[cmdIdx].cmd, cmd))
						Expect(cmd).To(Equal(orderedCommands[cmdIdx].cmd))
						expected = orderedCommands[cmdIdx]
					} else {
						idx := -1
						for i := range unorderedCommands {
							if unorderedCommands[i].cmd == cmd {
								expected = unorderedCommands[i]
								idx = i
								break
							}
						}
						Expect(idx).NotTo(Equal(-1))
						unorderedCommands = append(unorderedCommands[:idx], unorderedCommands[idx+1:]...)
					}
					var stdout, stderr bytes.Buffer
					stdout.WriteString(expected.stdout)
					return stdout, stderr, nil
				},
			}
			Expect(operation.Execute(ctx, &operations.Context{DPUFlavor: dpuFlavor})).To(Succeed())

			// setGUIDForSF iterates a map, so the final bind content is non-deterministic.
			By("bind should contain one of the auxiliary devices")
			bindContent, err := os.ReadFile(filepath.Join(tempDir, "sys/bus/auxiliary/drivers/mlx5_core.sf/bind"))
			Expect(err).NotTo(HaveOccurred())
			Expect(string(bindContent)).To(BeElementOf("mlx5_core.sf.2", "mlx5_core.sf.3", "mlx5_core.sf.4", "mlx5_core.sf.5"))
		})
	})
})
