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
	"strings"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	opts "github.com/nvidia/doca-platform/cmd/dpuagent/opts"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"
	pciutil "github.com/nvidia/doca-platform/internal/provisioning/utils/pci"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("SFConfig", func() {
	var tempDir string
	discoverTestPorts := func() ([]pciutil.NICPort, error) { //nolint:unparam
		return []pciutil.NICPort{
			{Netdev: "p0", PCIAddress: "0000:03:00.0", MSTDevice: "/dev/mst/mt41692_pciconf0"},
			{Netdev: "p1", PCIAddress: "0000:03:00.1", MSTDevice: "/dev/mst/mt41692_pciconf0.1"},
		}, nil
	}
	discoverBF4TestPorts := func() ([]pciutil.NICPort, error) { //nolint:unparam
		return []pciutil.NICPort{
			{Netdev: "p0", PCIAddress: "0000:03:00.0", MSTDevice: "/dev/mst/mt41692_pciconf0"},
			{Netdev: "p1", PCIAddress: "0001:03:00.0", MSTDevice: "/dev/mst/mt41692_pciconf1"},
		}, nil
	}

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
        "device": "0000:03:00.0",
        "sfnum": 0,
        "aux_dev": "mlx5_core.sf.2",
        "sf_netdev": "enp3s0f0s0"
    },
    "pci/0000:03:00.1/294912": {
        "device": "0000:03:00.0",
        "sfnum": 1,
        "aux_dev": "mlx5_core.sf.3",
        "sf_netdev": "enp3s0f1s0"
    },
    "pci/0000:03:00.100/294913": {
        "device": "0000:03:00.0",
        "sfnum": 101,
        "aux_dev": "mlx5_core.sf.4",
        "sf_netdev": "enp3s0f100s0"
    },
    "pci/0000:03:00.101/294914": {
        "device": "0000:03:00.0",
        "sfnum": 102,
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
			Expect(operation.Execute(ctx, &operations.Context{DPUFlavor: dpuFlavor, DiscoverPorts: discoverTestPorts})).To(Succeed())

			// setGUIDForSF iterates a map, so the final bind content is non-deterministic.
			By("bind should contain one of the auxiliary devices")
			bindContent, err := os.ReadFile(filepath.Join(tempDir, "sys/bus/auxiliary/drivers/mlx5_core.sf/bind"))
			Expect(err).NotTo(HaveOccurred())
			Expect(string(bindContent)).To(BeElementOf("mlx5_core.sf.2", "mlx5_core.sf.3", "mlx5_core.sf.4", "mlx5_core.sf.5"))
		})

		It("should set SF on all N/S PFs for BF4", func() {
			By("mock the DPUFlavor")
			dpuFlavor := provisioningv1.DPUFlavor{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						cutil.TrustedSFCount: "1",
					},
				},
				Spec: provisioningv1.DPUFlavorSpec{
					NVConfig: []provisioningv1.NVConfig{
						{
							Parameters: []string{"PF_TOTAL_SF=2"},
						},
					},
				},
			}

			By("mock the mlnx-sf output for both N/S PFs")
			mlnxsfOutput := `
{
    "pci/0000:03:00.0/229376": {
        "device": "0000:03:00.0",
        "sfnum": 0,
        "aux_dev": "mlx5_core.sf.2",
        "sf_netdev": "enp3s0f0s0"
    },
    "pci/0000:03:00.0/294913": {
        "device": "0000:03:00.0",
        "sfnum": 101,
        "aux_dev": "mlx5_core.sf.3",
        "sf_netdev": "enp3s0f101s0"
    },
    "pci/0001:03:00.0/229376": {
        "device": "0001:03:00.0",
        "sfnum": 0,
        "aux_dev": "mlx5_core.sf.4",
        "sf_netdev": "enp3s1f0s0"
    },
    "pci/0001:03:00.0/294913": {
        "device": "0001:03:00.0",
        "sfnum": 101,
        "aux_dev": "mlx5_core.sf.5",
        "sf_netdev": "enp3s1f101s0"
    }
}
`
			By("mock the SF sysfs files")
			sfFiles := map[string]struct {
				aux string
				mac string
			}{
				"enp3s0f0s0":   {aux: "mlx5_core.sf.2", mac: "02:36:17:17:a9:b0"},
				"enp3s0f101s0": {aux: "mlx5_core.sf.3", mac: "02:36:17:17:a9:b1"},
				"enp3s1f0s0":   {aux: "mlx5_core.sf.4", mac: "02:36:17:17:a9:b2"},
				"enp3s1f101s0": {aux: "mlx5_core.sf.5", mac: "02:36:17:17:a9:b3"},
			}
			for sfNetdev, sfFile := range sfFiles {
				Expect(os.MkdirAll(filepath.Dir(filepath.Join(tempDir, "sys/class/net", sfNetdev, "address")), 0755)).To(Succeed())
				Expect(os.WriteFile(filepath.Join(tempDir, "sys/class/net", sfNetdev, "address"), []byte(sfFile.mac), 0444)).To(Succeed())

				Expect(os.MkdirAll(filepath.Dir(filepath.Join(tempDir, "sys/bus/auxiliary/devices", sfFile.aux, "driver/unbind")), 0777)).To(Succeed())
				Expect(os.WriteFile(filepath.Join(tempDir, "sys/bus/auxiliary/devices", sfFile.aux, "driver/unbind"), []byte(""), 0200)).To(Succeed())
			}
			Expect(os.MkdirAll(filepath.Dir(filepath.Join(tempDir, "sys/bus/auxiliary/drivers/mlx5_core.sf/bind")), 0777)).To(Succeed())

			var commands []string
			operation := &CreateSF{
				rootFS: tempDir,
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					commands = append(commands, cmd)
					var stdout, stderr bytes.Buffer
					if cmd == "mlnx-sf -a show -j" {
						stdout.WriteString(mlnxsfOutput)
					}
					return stdout, stderr, nil
				},
			}
			err := operation.Execute(ctx, &operations.Context{
				DPUFlavor:     dpuFlavor,
				DiscoverPorts: discoverBF4TestPorts,
				LatestDPU: &provisioningv1.DPU{
					Status: provisioningv1.DPUStatus{DPUType: provisioningv1.DPUTypeBlueField4},
				},
			})
			Expect(err).NotTo(HaveOccurred())

			createCommands := []string{}
			createCommandsByDevice := map[string][]string{
				"0000:03:00.0": {},
				"0001:03:00.0": {},
			}
			mlxdevmCommands := []string{}
			for _, cmd := range commands {
				if strings.HasPrefix(cmd, "/sbin/mlnx-sf --action create") {
					createCommands = append(createCommands, cmd)
					switch {
					case strings.Contains(cmd, "--device 0000:03:00.0 "):
						createCommandsByDevice["0000:03:00.0"] = append(createCommandsByDevice["0000:03:00.0"], cmd)
					case strings.Contains(cmd, "--device 0001:03:00.0 "):
						createCommandsByDevice["0001:03:00.0"] = append(createCommandsByDevice["0001:03:00.0"], cmd)
					default:
						Fail(fmt.Sprintf("unexpected create command device: %s", cmd))
					}
				}
				if strings.HasPrefix(cmd, "/opt/mellanox/iproute2/sbin/mlxdevm port function set") {
					mlxdevmCommands = append(mlxdevmCommands, cmd)
				}
			}
			Expect(createCommands).To(HaveLen(4))
			Expect(createCommandsByDevice["0000:03:00.0"]).To(Equal([]string{
				"/sbin/mlnx-sf --action create --device 0000:03:00.0 --sfnum 0",
				"/sbin/mlnx-sf --action create --device 0000:03:00.0 --sfnum 101 -t",
			}))
			Expect(createCommandsByDevice["0001:03:00.0"]).To(Equal([]string{
				"/sbin/mlnx-sf --action create --device 0001:03:00.0 --sfnum 0",
				"/sbin/mlnx-sf --action create --device 0001:03:00.0 --sfnum 101 -t",
			}))
			Expect(mlxdevmCommands).To(ConsistOf(
				"/opt/mellanox/iproute2/sbin/mlxdevm port function set pci/0000:03:00.0/229376 hw_addr 02:36:17:17:a9:b0",
				"/opt/mellanox/iproute2/sbin/mlxdevm port function set pci/0000:03:00.0/294913 hw_addr 02:36:17:17:a9:b1",
				"/opt/mellanox/iproute2/sbin/mlxdevm port function set pci/0001:03:00.0/229376 hw_addr 02:36:17:17:a9:b2",
				"/opt/mellanox/iproute2/sbin/mlxdevm port function set pci/0001:03:00.0/294913 hw_addr 02:36:17:17:a9:b3",
			))
		})

		It("should return create error when expected SF is missing after creation", func() {
			By("mock the DPUFlavor")
			dpuFlavor := provisioningv1.DPUFlavor{
				Spec: provisioningv1.DPUFlavorSpec{
					NVConfig: []provisioningv1.NVConfig{
						{
							Parameters: []string{"PF_TOTAL_SF=2"},
						},
					},
				},
			}

			By("mock mlnx-sf output missing sfnum 1")
			mlnxsfOutput := `
{
    "pci/0000:03:00.0/229376": {
        "device": "0000:03:00.0",
        "sfnum": 0,
        "aux_dev": "mlx5_core.sf.2",
        "sf_netdev": "enp3s0f0s0"
    }
}
`

			type expectedCommand struct {
				cmd    string
				stdout string
				err    error
			}

			orderedCommands := []expectedCommand{
				{cmd: "/sbin/mlnx-sf --action create --device 0000:03:00.0 --sfnum 0", stdout: ""},
				{cmd: "/sbin/mlnx-sf --action create --device 0000:03:00.0 --sfnum 1", stdout: "partial", err: fmt.Errorf("create failed")},
				{cmd: "mlnx-sf -a show -j", stdout: mlnxsfOutput},
			}

			cmdIdx := 0
			operation := &CreateSF{
				rootFS: tempDir,
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					By(fmt.Sprintf("expecting command: %s, received: %s", orderedCommands[cmdIdx].cmd, cmd))
					Expect(cmd).To(Equal(orderedCommands[cmdIdx].cmd))
					expected := orderedCommands[cmdIdx]
					cmdIdx++

					var stdout, stderr bytes.Buffer
					stdout.WriteString(expected.stdout)
					return stdout, stderr, expected.err
				},
			}

			err := operation.Execute(ctx, &operations.Context{DPUFlavor: dpuFlavor, DiscoverPorts: discoverTestPorts})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to create SF 1"))
			Expect(err.Error()).To(ContainSubstring("create failed"))
		})

		It("should return trusted SF create error when trusted SF is missing after creation", func() {
			By("mock the DPUFlavor with one trusted SF")
			dpuFlavor := provisioningv1.DPUFlavor{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						cutil.TrustedSFCount: "1",
					},
				},
				Spec: provisioningv1.DPUFlavorSpec{
					NVConfig: []provisioningv1.NVConfig{
						{
							Parameters: []string{"PF_TOTAL_SF=2"},
						},
					},
				},
			}

			By("mock mlnx-sf output missing trusted sfnum 101")
			mlnxsfOutput := `
{
    "pci/0000:03:00.0/229376": {
        "device": "0000:03:00.0",
        "sfnum": 0,
        "aux_dev": "mlx5_core.sf.2",
        "sf_netdev": "enp3s0f0s0"
    }
}
`

			type expectedCommand struct {
				cmd    string
				stdout string
				err    error
			}

			orderedCommands := []expectedCommand{
				{cmd: "/sbin/mlnx-sf --action create --device 0000:03:00.0 --sfnum 0", stdout: ""},
				{cmd: "/sbin/mlnx-sf --action create --device 0000:03:00.0 --sfnum 101 -t", stdout: "partial", err: fmt.Errorf("trusted create failed")},
				{cmd: "mlnx-sf -a show -j", stdout: mlnxsfOutput},
			}

			cmdIdx := 0
			operation := &CreateSF{
				rootFS: tempDir,
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					By(fmt.Sprintf("expecting command: %s, received: %s", orderedCommands[cmdIdx].cmd, cmd))
					Expect(cmd).To(Equal(orderedCommands[cmdIdx].cmd))
					expected := orderedCommands[cmdIdx]
					cmdIdx++

					var stdout, stderr bytes.Buffer
					stdout.WriteString(expected.stdout)
					return stdout, stderr, expected.err
				},
			}

			err := operation.Execute(ctx, &operations.Context{DPUFlavor: dpuFlavor, DiscoverPorts: discoverTestPorts})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to create trusted SF 101"))
			Expect(err.Error()).To(ContainSubstring("trusted create failed"))
		})

		It("should keep create errors scoped per BF4 N/S PF", func() {
			By("mock the DPUFlavor")
			dpuFlavor := provisioningv1.DPUFlavor{
				Spec: provisioningv1.DPUFlavorSpec{
					NVConfig: []provisioningv1.NVConfig{
						{
							Parameters: []string{"PF_TOTAL_SF=1"},
						},
					},
				},
			}

			By("mock mlnx-sf output missing p1 sfnum 0")
			mlnxsfOutput := `
{
    "pci/0000:03:00.0/229376": {
        "device": "0000:03:00.0",
        "sfnum": 0,
        "aux_dev": "mlx5_core.sf.6",
        "sf_netdev": "enp3s0f9s0"
    }
}
`
			Expect(os.MkdirAll(filepath.Dir(filepath.Join(tempDir, "sys/class/net/enp3s0f9s0/address")), 0755)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(tempDir, "sys/class/net/enp3s0f9s0/address"), []byte("02:36:17:17:a9:b9"), 0444)).To(Succeed())
			Expect(os.MkdirAll(filepath.Dir(filepath.Join(tempDir, "sys/bus/auxiliary/devices/mlx5_core.sf.6/driver/unbind")), 0777)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(tempDir, "sys/bus/auxiliary/devices/mlx5_core.sf.6/driver/unbind"), []byte(""), 0200)).To(Succeed())
			Expect(os.MkdirAll(filepath.Dir(filepath.Join(tempDir, "sys/bus/auxiliary/drivers/mlx5_core.sf/bind")), 0777)).To(Succeed())

			operation := &CreateSF{
				rootFS: tempDir,
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					var stdout, stderr bytes.Buffer
					if cmd == "mlnx-sf -a show -j" {
						stdout.WriteString(mlnxsfOutput)
					}
					if cmd == "/sbin/mlnx-sf --action create --device 0001:03:00.0 --sfnum 0" {
						stdout.WriteString("partial")
						return stdout, stderr, fmt.Errorf("create failed on p1")
					}
					return stdout, stderr, nil
				},
			}

			err := operation.Execute(ctx, &operations.Context{
				DPUFlavor:     dpuFlavor,
				DiscoverPorts: discoverBF4TestPorts,
				LatestDPU: &provisioningv1.DPU{
					Status: provisioningv1.DPUStatus{DPUType: provisioningv1.DPUTypeBlueField4},
				},
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to create SF 0 on device 0001:03:00.0"))
			Expect(err.Error()).To(ContainSubstring("create failed on p1"))
		})

		It("should treat device with omitted PCI domain as the same device", func() {
			mlnxsfOutput := `
{
    "pci/0000:03:00.0/229376": {
        "device": "03:00.0",
        "sfnum": 0
    }
}
`
			operation := &CreateSF{
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					Expect(cmd).To(Equal("mlnx-sf -a show -j"))
					var stdout, stderr bytes.Buffer
					stdout.WriteString(mlnxsfOutput)
					return stdout, stderr, nil
				},
			}

			err := operation.verifyExpectedSFs("0000:03:00.0", []int{0}, map[int]error{})
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
