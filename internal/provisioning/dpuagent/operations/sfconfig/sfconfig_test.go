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
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	opts "github.com/nvidia/doca-platform/cmd/dpuagent/opts"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"
	pciutil "github.com/nvidia/doca-platform/internal/provisioning/utils/pci"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

const mlnxSFShowCmd = "mlnx-sf -a show -j"

var _ = Describe("SFConfig", func() {
	var tempDir string
	discoverTestPorts := func(_ pciutil.PortScope) ([]pciutil.NICPort, error) { //nolint:unparam
		return []pciutil.NICPort{
			{Netdev: "p0", PCIAddress: "0000:03:00.0"},
			{Netdev: "p1", PCIAddress: "0000:03:00.1"},
		}, nil
	}
	discoverBF4TestPorts := func(_ pciutil.PortScope) ([]pciutil.NICPort, error) { //nolint:unparam
		return []pciutil.NICPort{
			{Netdev: "p0", PCIAddress: "0000:03:00.0"},
			{Netdev: "p1", PCIAddress: "0001:03:00.0"},
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

		It("should run hostless workarounds before creating SFs", func() {
			dpuFlavor := provisioningv1.DPUFlavor{
				Spec: provisioningv1.DPUFlavorSpec{
					NVConfig: []provisioningv1.NVConfig{{Parameters: []string{"PF_TOTAL_SF=1"}}},
				},
			}
			mlnxsfOutput := `{
    "pci/0000:03:00.0/229376": {
        "device": "0000:03:00.0",
        "sfnum": 0,
        "aux_dev": "mlx5_core.sf.2",
        "sf_netdev": "enp3s0f0s0"
    }
}`
			Expect(os.MkdirAll(filepath.Dir(filepath.Join(tempDir, "sys/class/net/enp3s0f0s0/address")), 0755)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(tempDir, "sys/class/net/enp3s0f0s0/address"), []byte("02:36:17:17:a9:b0"), 0444)).To(Succeed())
			Expect(os.MkdirAll(filepath.Dir(filepath.Join(tempDir, "sys/bus/auxiliary/devices/mlx5_core.sf.2/driver/unbind")), 0777)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(tempDir, "sys/bus/auxiliary/devices/mlx5_core.sf.2/driver/unbind"), []byte(""), 0200)).To(Succeed())
			Expect(os.MkdirAll(filepath.Dir(filepath.Join(tempDir, "sys/bus/auxiliary/drivers/mlx5_core.sf/bind")), 0777)).To(Succeed())

			cmds := []string{}
			operation := &CreateSF{
				rootFS: tempDir,
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					cmds = append(cmds, cmd)
					var stdout bytes.Buffer
					if cmd == mlnxSFShowCmd {
						stdout.WriteString(mlnxsfOutput)
					}
					return stdout, bytes.Buffer{}, nil
				},
			}
			err := operation.Execute(ctx, &operations.Context{
				DPUFlavor:     dpuFlavor,
				DiscoverPorts: discoverTestPorts,
				LatestDPU: &provisioningv1.DPU{
					Status: provisioningv1.DPUStatus{Hostless: true},
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(cmds[:5]).To(Equal([]string{
				"doca-hugepages config --app cmx_target --size 2048 --num 4096",
				"doca-hugepages reload",
				"devlink dev eswitch set pci/0000:03:00.0 mode switchdev",
				"devlink dev eswitch set pci/0000:03:00.1 mode switchdev",
				"/sbin/mlnx-sf --action create --device 0000:03:00.0 --sfnum 0",
			}))
		})

		It("should not run hostless workarounds when not hostless", func() {
			dpuFlavor := provisioningv1.DPUFlavor{
				Spec: provisioningv1.DPUFlavorSpec{
					NVConfig: []provisioningv1.NVConfig{{Parameters: []string{"PF_TOTAL_SF=1"}}},
				},
			}
			mlnxsfOutput := `{
    "pci/0000:03:00.0/229376": {
        "device": "0000:03:00.0",
        "sfnum": 0,
        "aux_dev": "mlx5_core.sf.2",
        "sf_netdev": "enp3s0f0s0"
    }
}`
			Expect(os.MkdirAll(filepath.Dir(filepath.Join(tempDir, "sys/class/net/enp3s0f0s0/address")), 0755)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(tempDir, "sys/class/net/enp3s0f0s0/address"), []byte("02:36:17:17:a9:b0"), 0444)).To(Succeed())
			Expect(os.MkdirAll(filepath.Dir(filepath.Join(tempDir, "sys/bus/auxiliary/devices/mlx5_core.sf.2/driver/unbind")), 0777)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(tempDir, "sys/bus/auxiliary/devices/mlx5_core.sf.2/driver/unbind"), []byte(""), 0200)).To(Succeed())
			Expect(os.MkdirAll(filepath.Dir(filepath.Join(tempDir, "sys/bus/auxiliary/drivers/mlx5_core.sf/bind")), 0777)).To(Succeed())

			cmds := []string{}
			operation := &CreateSF{
				rootFS: tempDir,
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					cmds = append(cmds, cmd)
					var stdout bytes.Buffer
					if cmd == mlnxSFShowCmd {
						stdout.WriteString(mlnxsfOutput)
					}
					return stdout, bytes.Buffer{}, nil
				},
			}
			err := operation.Execute(ctx, &operations.Context{
				DPUFlavor:     dpuFlavor,
				DiscoverPorts: discoverTestPorts,
				LatestDPU: &provisioningv1.DPU{
					Status: provisioningv1.DPUStatus{Hostless: false},
				},
			})
			Expect(err).NotTo(HaveOccurred())
			for _, cmd := range cmds {
				Expect(cmd).NotTo(ContainSubstring("devlink dev eswitch set"))
				Expect(cmd).NotTo(ContainSubstring("doca-hugepages"))
			}
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
			// DMA SF is disabled here, so configureSFsOnDevice does no upfront
			// SF listing; the only mlnx-sf shows are verifyExpectedSFs + setGUIDForSF.
			orderedCommands := []expectedCommand{
				{cmd: "/sbin/mlnx-sf --action create --device 0000:03:00.0 --sfnum 0", stdout: ""},
				{cmd: "/sbin/mlnx-sf --action create --device 0000:03:00.0 --sfnum 1", stdout: ""},
				{cmd: "/sbin/mlnx-sf --action create --device 0000:03:00.0 --sfnum 101 -t", stdout: ""},
				{cmd: "/sbin/mlnx-sf --action create --device 0000:03:00.0 --sfnum 102 -t", stdout: ""},
				{cmd: mlnxSFShowCmd, stdout: mlnxsfOutput},
				{cmd: mlnxSFShowCmd, stdout: mlnxsfOutput},
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
					if cmd == mlnxSFShowCmd {
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
				{cmd: mlnxSFShowCmd, stdout: mlnxsfOutput},
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
				{cmd: mlnxSFShowCmd, stdout: mlnxsfOutput},
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
					if cmd == mlnxSFShowCmd {
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

		It("should reduce the created SF count when the SNAP DMA SF exists on the device", func() {
			By("mock the DPUFlavor with PF_TOTAL_SF=3 and one trusted SF")
			dpuFlavor := provisioningv1.DPUFlavor{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						cutil.TrustedSFCount: "1",
					},
				},
				Spec: provisioningv1.DPUFlavorSpec{
					NVConfig: []provisioningv1.NVConfig{
						{
							Parameters: []string{"PF_TOTAL_SF=3"},
						},
					},
					// Enable agent DMA SF handling so the pre-existing DMA SF is reserved.
					ScalableFunctions: &provisioningv1.ScalableFunctions{DMA: &provisioningv1.DMAScalableFunction{Enabled: ptr.To(true)}},
				},
			}

			By("mock mlnx-sf output containing the SNAP DMA SF (sfnum 8000, a foreign SF)")
			mlnxsfOutput := `
{
    "pci/0000:03:00.0/229376": {
        "device": "0000:03:00.0",
        "sfnum": 0,
        "aux_dev": "mlx5_core.sf.2"
    },
    "pci/0000:03:00.0/294913": {
        "device": "0000:03:00.0",
        "sfnum": 101,
        "aux_dev": "mlx5_core.sf.3"
    },
    "pci/0000:03:00.0/295000": {
        "device": "0000:03:00.0",
        "sfnum": 8000,
        "aux_dev": "mlx5_core.sf.4",
        "rdma_dev": "mlx5_2"
    }
}
`
			type expectedCommand struct {
				cmd    string
				stdout string
			}

			By("expecting one normal SF less (2 DPF SFs + 1 foreign SF = PF_TOTAL_SF) and a passing verification")
			orderedCommands := []expectedCommand{
				{cmd: mlnxSFShowCmd, stdout: mlnxsfOutput},
				{cmd: "/sbin/mlnx-sf --action create --device 0000:03:00.0 --sfnum 0", stdout: ""},
				{cmd: "/sbin/mlnx-sf --action create --device 0000:03:00.0 --sfnum 101 -t", stdout: ""},
				// ensureDMASFRepresentorUp reads mlnx-sf; the existing DMA SF has no
				// representor netdev here, so it issues no `ip link set ... up`.
				{cmd: mlnxSFShowCmd, stdout: mlnxsfOutput},
				{cmd: mlnxSFShowCmd, stdout: mlnxsfOutput},
				{cmd: mlnxSFShowCmd, stdout: mlnxsfOutput},
			}

			cmdIdx := 0
			operation := &CreateSF{
				rootFS: tempDir,
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					Expect(cmdIdx).To(BeNumerically("<", len(orderedCommands)), "unexpected extra command: %s", cmd)
					By(fmt.Sprintf("expecting command: %s, received: %s", orderedCommands[cmdIdx].cmd, cmd))
					Expect(cmd).To(Equal(orderedCommands[cmdIdx].cmd))
					expected := orderedCommands[cmdIdx]
					cmdIdx++

					var stdout, stderr bytes.Buffer
					stdout.WriteString(expected.stdout)
					return stdout, stderr, nil
				},
			}

			discoverSingleTestPort := func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
				return []pciutil.NICPort{{Netdev: "p0", PCIAddress: "0000:03:00.0"}}, nil
			}
			Expect(operation.Execute(ctx, &operations.Context{
				DPUFlavor:     dpuFlavor,
				DiscoverPorts: discoverSingleTestPort,
				LatestDPU:     &provisioningv1.DPU{Status: provisioningv1.DPUStatus{DPUType: provisioningv1.DPUTypeBlueField4}},
			})).To(Succeed())
			Expect(cmdIdx).To(Equal(len(orderedCommands)))
		})

		It("should error clearly when PF_TOTAL_SF cannot fit the trusted and SNAP DMA SFs", func() {
			By("mock a misconfigured DPUFlavor: PF_TOTAL_SF=1 with 2 trusted SFs")
			dpuFlavor := provisioningv1.DPUFlavor{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						cutil.TrustedSFCount: "2",
					},
				},
				Spec: provisioningv1.DPUFlavorSpec{
					NVConfig: []provisioningv1.NVConfig{
						{Parameters: []string{"PF_TOTAL_SF=1"}},
					},
				},
			}

			var commands []string
			operation := &CreateSF{
				rootFS: tempDir,
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					commands = append(commands, cmd)
					var stdout, stderr bytes.Buffer
					if cmd == mlnxSFShowCmd {
						stdout.WriteString("{}")
					}
					return stdout, stderr, nil
				},
			}

			err := operation.Execute(ctx, &operations.Context{DPUFlavor: dpuFlavor, DiscoverPorts: discoverTestPorts})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("insufficient SF capacity"))
			Expect(err.Error()).To(ContainSubstring("PF_TOTAL_SF=1"))
			By("no SF create was attempted")
			for _, cmd := range commands {
				Expect(cmd).NotTo(ContainSubstring("--action create"))
			}
		})

		It("should fail visibly when scalableFunctions.dma.enabled is set but no eligible target ECPF exists", func() {
			By("mock the DPUFlavor with PF_TOTAL_SF=2 and the DMA SF enabled")
			dpuFlavor := provisioningv1.DPUFlavor{
				Spec: provisioningv1.DPUFlavorSpec{
					NVConfig: []provisioningv1.NVConfig{
						{
							Parameters: []string{"PF_TOTAL_SF=2"},
						},
					},
					ScalableFunctions: &provisioningv1.ScalableFunctions{DMA: &provisioningv1.DMAScalableFunction{Enabled: ptr.To(true)}},
				},
			}

			By("the only ECPF is RDMA-bearing, so no ibdev-less 2nd-link ECPF exists (e.g. silencing did not happen)")
			Expect(os.MkdirAll(filepath.Join(tempDir, "sys/bus/pci/devices/0000:03:00.0/infiniband/mlx5_0"), 0755)).To(Succeed())

			var commands []string
			operation := &CreateSF{
				rootFS: tempDir,
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					commands = append(commands, cmd)
					var stdout, stderr bytes.Buffer
					return stdout, stderr, nil
				},
			}

			discoverSingleTestPort := func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
				return []pciutil.NICPort{{Netdev: "p0", PCIAddress: "0000:03:00.0"}}, nil
			}
			err := operation.Execute(ctx, &operations.Context{
				DPUFlavor:     dpuFlavor,
				DiscoverPorts: discoverSingleTestPort,
				LatestDPU:     &provisioningv1.DPU{Status: provisioningv1.DPUStatus{DPUType: provisioningv1.DPUTypeBlueField4}},
			})
			By("the operation fails with an actionable error instead of degrading silently")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no eligible ibdev-less 2nd-link ECPF"))
			By("no SFs are created: it fails at target selection, before the device loop")
			Expect(commands).To(BeEmpty())
		})

		It("should recognize the SNAP DMA SF by the scalableFunctions.dma.sfNum value", func() {
			By("enable the DMA SF and set scalableFunctions.dma.sfNum to 9000 in the flavor")
			dpuFlavor := provisioningv1.DPUFlavor{
				Spec: provisioningv1.DPUFlavorSpec{
					NVConfig: []provisioningv1.NVConfig{
						{
							Parameters: []string{"PF_TOTAL_SF=2"},
						},
					},
					ScalableFunctions: &provisioningv1.ScalableFunctions{DMA: &provisioningv1.DMAScalableFunction{Enabled: ptr.To(true), SFNum: ptr.To(int32(9000))}},
				},
			}

			By("mock mlnx-sf output containing an SF with the overridden sfnum")
			mlnxsfOutput := `
{
    "pci/0000:03:00.0/229376": {
        "device": "0000:03:00.0",
        "sfnum": 0,
        "aux_dev": "mlx5_core.sf.2"
    },
    "pci/0000:03:00.0/295002": {
        "device": "0000:03:00.0",
        "sfnum": 9000,
        "aux_dev": "mlx5_core.sf.7",
        "rdma_dev": "mlx5_2"
    }
}
`
			type expectedCommand struct {
				cmd    string
				stdout string
			}

			By("expecting one SF less: sfnum 9000 is counted as the SNAP DMA SF")
			orderedCommands := []expectedCommand{
				{cmd: mlnxSFShowCmd, stdout: mlnxsfOutput},
				{cmd: "/sbin/mlnx-sf --action create --device 0000:03:00.0 --sfnum 0", stdout: ""},
				// ensureDMASFRepresentorUp reads mlnx-sf; the existing DMA SF has no
				// representor netdev here, so it issues no `ip link set ... up`.
				{cmd: mlnxSFShowCmd, stdout: mlnxsfOutput},
				{cmd: mlnxSFShowCmd, stdout: mlnxsfOutput},
				{cmd: mlnxSFShowCmd, stdout: mlnxsfOutput},
			}

			cmdIdx := 0
			operation := &CreateSF{
				rootFS: tempDir,
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					Expect(cmdIdx).To(BeNumerically("<", len(orderedCommands)), "unexpected extra command: %s", cmd)
					Expect(cmd).To(Equal(orderedCommands[cmdIdx].cmd))
					expected := orderedCommands[cmdIdx]
					cmdIdx++

					var stdout, stderr bytes.Buffer
					stdout.WriteString(expected.stdout)
					return stdout, stderr, nil
				},
			}

			discoverSingleTestPort := func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
				return []pciutil.NICPort{{Netdev: "p0", PCIAddress: "0000:03:00.0"}}, nil
			}
			Expect(operation.Execute(ctx, &operations.Context{
				DPUFlavor:     dpuFlavor,
				DiscoverPorts: discoverSingleTestPort,
				LatestDPU:     &provisioningv1.DPU{Status: provisioningv1.DPUStatus{DPUType: provisioningv1.DPUTypeBlueField4}},
			})).To(Succeed())
			Expect(cmdIdx).To(Equal(len(orderedCommands)))
		})

		It("should reserve a slot and create the DMA SF on the ibdev-less ECPF", func() {
			By("mock a sysfs where 0001:03:00.0 is the silenced (ibdev-less) socket-direct ECPF")
			for bdf, rdmaDev := range map[string]string{
				"0000:03:00.0": "mlx5_0",
				"0001:03:00.0": "",
			} {
				devDir := filepath.Join(tempDir, "sys/bus/pci/devices", bdf)
				Expect(os.MkdirAll(devDir, 0755)).To(Succeed())
				Expect(os.WriteFile(filepath.Join(devDir, "device"), []byte("0xa2df\n"), 0444)).To(Succeed())
				if rdmaDev != "" {
					Expect(os.MkdirAll(filepath.Join(devDir, "infiniband", rdmaDev), 0755)).To(Succeed())
				}
			}
			By("mock the DMA SF aux device sysfs (sfnum 8000) on the ibdev-less ECPF")
			auxDir := filepath.Join(tempDir, "sys/bus/pci/devices/0001:03:00.0/mlx5_core.sf.9")
			Expect(os.MkdirAll(auxDir, 0755)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(auxDir, "sfnum"), []byte("8000\n"), 0444)).To(Succeed())

			dpuFlavor := provisioningv1.DPUFlavor{
				Spec: provisioningv1.DPUFlavorSpec{
					NVConfig: []provisioningv1.NVConfig{
						{Parameters: []string{"PF_TOTAL_SF=3"}},
					},
					// Configure the agent to create the DMA SF.
					ScalableFunctions: &provisioningv1.ScalableFunctions{DMA: &provisioningv1.DMAScalableFunction{Enabled: ptr.To(true)}},
				},
			}

			// Before the DMA SF is created there is no sfnum 8000; afterwards it
			// exposes an RDMA device and its own netdev is gone (only the
			// representor netdev remains). The workload SFs (3 on the master, 2
			// on the reserved ibdev-less ECPF) are present throughout.
			const workloadSFs = `
    "pci/0000:03:00.0/1": {"device": "0000:03:00.0", "sfnum": 0, "aux_dev": "mlx5_core.sf.2"},
    "pci/0000:03:00.0/2": {"device": "0000:03:00.0", "sfnum": 1, "aux_dev": "mlx5_core.sf.3"},
    "pci/0000:03:00.0/3": {"device": "0000:03:00.0", "sfnum": 2, "aux_dev": "mlx5_core.sf.4"},
    "pci/0001:03:00.0/1": {"device": "0001:03:00.0", "sfnum": 0, "aux_dev": "mlx5_core.sf.5"},
    "pci/0001:03:00.0/2": {"device": "0001:03:00.0", "sfnum": 1, "aux_dev": "mlx5_core.sf.6"}`
			beforeDMA := "{" + workloadSFs + "\n}\n"
			afterDMA := "{" + workloadSFs + `,
    "pci/0001:03:00.0/9": {"device": "0001:03:00.0", "sfnum": 8000, "netdev": "en3f1pf0sf8000", "aux_dev": "mlx5_core.sf.9", "rdma_dev": "mlx5_2"}
}
`
			dmaCreated := false
			var commands []string
			operation := &CreateSF{
				rootFS:               tempDir,
				auxDiscoveryInterval: time.Millisecond,
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					commands = append(commands, cmd)
					var stdout, stderr bytes.Buffer
					if strings.Contains(cmd, "--action create") && strings.Contains(cmd, "--sfnum 8000") {
						dmaCreated = true
					}
					if cmd == mlnxSFShowCmd {
						if dmaCreated {
							stdout.WriteString(afterDMA)
						} else {
							stdout.WriteString(beforeDMA)
						}
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

			createByDevice := map[string][]string{}
			var devlinkAndIP []string
			for _, cmd := range commands {
				switch {
				case strings.HasPrefix(cmd, "/sbin/mlnx-sf --action create --device 0000:03:00.0 "):
					createByDevice["0000:03:00.0"] = append(createByDevice["0000:03:00.0"], cmd)
				case strings.HasPrefix(cmd, "/sbin/mlnx-sf --action create --device 0001:03:00.0 "):
					createByDevice["0001:03:00.0"] = append(createByDevice["0001:03:00.0"], cmd)
				case strings.HasPrefix(cmd, "devlink ") || strings.HasPrefix(cmd, "ip link "):
					devlinkAndIP = append(devlinkAndIP, cmd)
				}
			}
			By("the master ECPF creates the full 3 workload SFs")
			Expect(createByDevice["0000:03:00.0"]).To(Equal([]string{
				"/sbin/mlnx-sf --action create --device 0000:03:00.0 --sfnum 0",
				"/sbin/mlnx-sf --action create --device 0000:03:00.0 --sfnum 1",
				"/sbin/mlnx-sf --action create --device 0000:03:00.0 --sfnum 2",
			}))
			By("the ibdev-less ECPF creates 2 workload SFs plus the DMA SF (sfnum 8000, vendor-derived MAC)")
			Expect(createByDevice["0001:03:00.0"]).To(Equal([]string{
				"/sbin/mlnx-sf --action create --device 0001:03:00.0 --sfnum 0",
				"/sbin/mlnx-sf --action create --device 0001:03:00.0 --sfnum 1",
				"/sbin/mlnx-sf --action create --device 0001:03:00.0 --sfnum 8000 --hwaddr 02:40:51:7c:e3:0f --disable-roce",
			}))
			By("the DMA SF netdev is disabled via devlink and its representor is brought up")
			Expect(devlinkAndIP).To(Equal([]string{
				"devlink dev param set auxiliary/mlx5_core.sf.9 name enable_eth value false cmode driverinit",
				"devlink dev reload auxiliary/mlx5_core.sf.9",
				"ip link set en3f1pf0sf8000 up",
			}))
		})

		It("should bring a pre-existing DMA SF's representor up without recreating or reloading it", func() {
			By("mock a sysfs where 0001:03:00.0 is the silenced (ibdev-less) socket-direct ECPF")
			for bdf, rdmaDev := range map[string]string{
				"0000:03:00.0": "mlx5_0",
				"0001:03:00.0": "",
			} {
				devDir := filepath.Join(tempDir, "sys/bus/pci/devices", bdf)
				Expect(os.MkdirAll(devDir, 0755)).To(Succeed())
				Expect(os.WriteFile(filepath.Join(devDir, "device"), []byte("0xa2df\n"), 0444)).To(Succeed())
				if rdmaDev != "" {
					Expect(os.MkdirAll(filepath.Join(devDir, "infiniband", rdmaDev), 0755)).To(Succeed())
				}
			}

			dpuFlavor := provisioningv1.DPUFlavor{
				Spec: provisioningv1.DPUFlavorSpec{
					NVConfig: []provisioningv1.NVConfig{
						{Parameters: []string{"PF_TOTAL_SF=3"}},
					},
					// Configure the agent to create the DMA SF.
					ScalableFunctions: &provisioningv1.ScalableFunctions{DMA: &provisioningv1.DMAScalableFunction{Enabled: ptr.To(true)}},
				},
			}

			By("mock mlnx-sf output where the DMA SF (sfnum 8000) already exists — an agent restart within a boot")
			// It exposes an RDMA device and no own netdev (only the representor
			// en3f1pf0sf8000), so verifyDMASFConsumable passes without a recreate.
			const sfShow = `{
    "pci/0000:03:00.0/1": {"device": "0000:03:00.0", "sfnum": 0, "aux_dev": "mlx5_core.sf.2"},
    "pci/0000:03:00.0/2": {"device": "0000:03:00.0", "sfnum": 1, "aux_dev": "mlx5_core.sf.3"},
    "pci/0000:03:00.0/3": {"device": "0000:03:00.0", "sfnum": 2, "aux_dev": "mlx5_core.sf.4"},
    "pci/0001:03:00.0/1": {"device": "0001:03:00.0", "sfnum": 0, "aux_dev": "mlx5_core.sf.5"},
    "pci/0001:03:00.0/2": {"device": "0001:03:00.0", "sfnum": 1, "aux_dev": "mlx5_core.sf.6"},
    "pci/0001:03:00.0/9": {"device": "0001:03:00.0", "sfnum": 8000, "netdev": "en3f1pf0sf8000", "aux_dev": "mlx5_core.sf.9", "rdma_dev": "mlx5_2"}
}
`
			var commands []string
			operation := &CreateSF{
				rootFS:               tempDir,
				auxDiscoveryInterval: time.Millisecond,
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					commands = append(commands, cmd)
					var stdout, stderr bytes.Buffer
					if cmd == mlnxSFShowCmd {
						stdout.WriteString(sfShow)
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

			By("the existing DMA SF is not recreated and its aux device is not reloaded")
			var devlinkAndIP []string
			for _, cmd := range commands {
				Expect(cmd).NotTo(ContainSubstring("--sfnum 8000"))
				Expect(cmd).NotTo(ContainSubstring("--disable-roce"))
				if strings.HasPrefix(cmd, "devlink ") || strings.HasPrefix(cmd, "ip link ") {
					devlinkAndIP = append(devlinkAndIP, cmd)
				}
			}
			By("its representor is still (re)asserted up — no devlink reload, only ip link")
			Expect(devlinkAndIP).To(Equal([]string{"ip link set en3f1pf0sf8000 up"}))
		})

		It("should NOT reserve a slot on an ibdev-less ECPF when scalableFunctions.dma.enabled is false", func() {
			By("mock a sysfs where 0001:03:00.0 is ibdev-less but the agent DMA SF is not enabled")
			for bdf, rdmaDev := range map[string]string{
				"0000:03:00.0": "mlx5_0",
				"0001:03:00.0": "",
			} {
				devDir := filepath.Join(tempDir, "sys/bus/pci/devices", bdf)
				Expect(os.MkdirAll(devDir, 0755)).To(Succeed())
				Expect(os.WriteFile(filepath.Join(devDir, "device"), []byte("0xa2df\n"), 0444)).To(Succeed())
				if rdmaDev != "" {
					Expect(os.MkdirAll(filepath.Join(devDir, "infiniband", rdmaDev), 0755)).To(Succeed())
				}
			}
			By("the flavor sets scalableFunctions.dma.enabled=false, so the agent does not create the DMA SF")

			dpuFlavor := provisioningv1.DPUFlavor{
				Spec: provisioningv1.DPUFlavorSpec{
					NVConfig: []provisioningv1.NVConfig{
						{Parameters: []string{"PF_TOTAL_SF=1"}},
					},
					ScalableFunctions: &provisioningv1.ScalableFunctions{DMA: &provisioningv1.DMAScalableFunction{Enabled: ptr.To(false)}},
				},
			}

			// mlnx-sf reports sfnum 0 created on both ECPFs (no netdev, so
			// setGUIDForSF skips them) — verification then passes for both.
			mlnxsfOutput := `
{
    "pci/0000:03:00.0/229376": {
        "device": "0000:03:00.0",
        "sfnum": 0,
        "aux_dev": "mlx5_core.sf.2"
    },
    "pci/0001:03:00.0/229376": {
        "device": "0001:03:00.0",
        "sfnum": 0,
        "aux_dev": "mlx5_core.sf.3"
    }
}
`
			var commands []string
			operation := &CreateSF{
				rootFS: tempDir,
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					commands = append(commands, cmd)
					var stdout, stderr bytes.Buffer
					if cmd == mlnxSFShowCmd {
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

			By("both ECPFs get the full workload SF count — no slot is reserved")
			createCommands := []string{}
			for _, cmd := range commands {
				if strings.HasPrefix(cmd, "/sbin/mlnx-sf --action create") {
					createCommands = append(createCommands, cmd)
				}
			}
			Expect(createCommands).To(ConsistOf(
				"/sbin/mlnx-sf --action create --device 0000:03:00.0 --sfnum 0",
				"/sbin/mlnx-sf --action create --device 0001:03:00.0 --sfnum 0",
			))
		})

		It("should NOT create the DMA SF on a non-BlueField-4 DPU even when scalableFunctions.dma.enabled is set", func() {
			By("scalableFunctions.dma.enabled is set in the flavor; only the BF4 gate should stop creation")
			// The target device p0 has no infiniband dir, so it is ibdev-less and
			// would qualify to host the DMA SF on BF4 — only the non-BF4 gate
			// prevents it here, making this a regression guard for that gate.
			dpuFlavor := provisioningv1.DPUFlavor{
				Spec: provisioningv1.DPUFlavorSpec{
					NVConfig: []provisioningv1.NVConfig{
						{Parameters: []string{"PF_TOTAL_SF=2"}},
					},
					ScalableFunctions: &provisioningv1.ScalableFunctions{DMA: &provisioningv1.DMAScalableFunction{Enabled: ptr.To(true)}},
				},
			}

			// No sfnum 8000; the workload SFs are netdev-less so setGUIDForSF skips them.
			mlnxsfOutput := `
{
    "pci/0000:03:00.0/229376": {"device": "0000:03:00.0", "sfnum": 0, "aux_dev": "mlx5_core.sf.2"},
    "pci/0000:03:00.0/229377": {"device": "0000:03:00.0", "sfnum": 1, "aux_dev": "mlx5_core.sf.3"}
}
`
			var commands []string
			operation := &CreateSF{
				rootFS:               tempDir,
				auxDiscoveryInterval: time.Millisecond,
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					commands = append(commands, cmd)
					var stdout, stderr bytes.Buffer
					if cmd == mlnxSFShowCmd {
						stdout.WriteString(mlnxsfOutput)
					}
					return stdout, stderr, nil
				},
			}

			err := operation.Execute(ctx, &operations.Context{
				DPUFlavor:     dpuFlavor,
				DiscoverPorts: discoverTestPorts,
				LatestDPU: &provisioningv1.DPU{
					Status: provisioningv1.DPUStatus{DPUType: provisioningv1.DPUTypeBlueField3},
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("only the workload SFs are created on p0 — no DMA SF (sfnum 8000)")
			createCommands := []string{}
			for _, cmd := range commands {
				if strings.HasPrefix(cmd, "/sbin/mlnx-sf --action create") {
					createCommands = append(createCommands, cmd)
				}
			}
			Expect(createCommands).To(ConsistOf(
				"/sbin/mlnx-sf --action create --device 0000:03:00.0 --sfnum 0",
				"/sbin/mlnx-sf --action create --device 0000:03:00.0 --sfnum 1",
			))
			for _, cmd := range commands {
				Expect(cmd).NotTo(ContainSubstring("--sfnum 8000"))
				Expect(cmd).NotTo(ContainSubstring("--disable-roce"))
			}
		})

		It("should skip GUID setup for the SNAP DMA SF and for netdev-less SFs", func() {
			By("mock mlnx-sf output with the SNAP DMA SF (netdev disabled), a netdev-less SF and a regular SF")
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
        "sfnum": 1,
        "aux_dev": "mlx5_core.sf.3"
    },
    "pci/0000:03:00.0/295000": {
        "device": "0000:03:00.0",
        "sfnum": 8000,
        "aux_dev": "mlx5_core.sf.4"
    }
}
`
			By("mock sysfs only for the regular SF; the DMA SF and netdev-less SF have no netdev to read")
			Expect(os.MkdirAll(filepath.Dir(filepath.Join(tempDir, "sys/class/net/enp3s0f0s0/address")), 0755)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(tempDir, "sys/class/net/enp3s0f0s0/address"), []byte("02:36:17:17:a9:b0"), 0444)).To(Succeed())
			Expect(os.MkdirAll(filepath.Dir(filepath.Join(tempDir, "sys/bus/auxiliary/devices/mlx5_core.sf.2/driver/unbind")), 0777)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(tempDir, "sys/bus/auxiliary/devices/mlx5_core.sf.2/driver/unbind"), []byte(""), 0200)).To(Succeed())
			Expect(os.MkdirAll(filepath.Dir(filepath.Join(tempDir, "sys/bus/auxiliary/drivers/mlx5_core.sf/bind")), 0777)).To(Succeed())

			var commands []string
			operation := &CreateSF{
				rootFS: tempDir,
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					commands = append(commands, cmd)
					var stdout, stderr bytes.Buffer
					if cmd == mlnxSFShowCmd {
						stdout.WriteString(mlnxsfOutput)
					}
					return stdout, stderr, nil
				},
			}

			Expect(operation.setGUIDForSF("0000:03:00.0")).To(Succeed())
			Expect(commands).To(Equal([]string{
				mlnxSFShowCmd,
				"/opt/mellanox/iproute2/sbin/mlxdevm port function set pci/0000:03:00.0/229376 hw_addr 02:36:17:17:a9:b0",
			}))

			By("only the regular SF's aux device was rebound")
			bindContent, err := os.ReadFile(filepath.Join(tempDir, "sys/bus/auxiliary/drivers/mlx5_core.sf/bind"))
			Expect(err).NotTo(HaveOccurred())
			Expect(string(bindContent)).To(Equal("mlx5_core.sf.2"))
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
					Expect(cmd).To(Equal(mlnxSFShowCmd))
					var stdout, stderr bytes.Buffer
					stdout.WriteString(mlnxsfOutput)
					return stdout, stderr, nil
				},
			}

			err := operation.verifyExpectedSFs("0000:03:00.0", []int{0}, map[int]error{}, false)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})

var _ = Describe("selectDMASFTarget", func() {
	var rootFS string

	// mockECPF creates the sysfs for an ECPF, giving it an RDMA (ibdev) device
	// when rdmaDev is non-empty (an ibdev-less ECPF passes rdmaDev="").
	mockECPF := func(bdf, rdmaDev string) {
		devDir := filepath.Join(rootFS, "sys/bus/pci/devices", bdf)
		Expect(os.MkdirAll(devDir, 0755)).To(Succeed())
		if rdmaDev != "" {
			Expect(os.MkdirAll(filepath.Join(devDir, "infiniband", rdmaDev), 0755)).To(Succeed())
		}
	}

	BeforeEach(func() {
		rootFS = GinkgoT().TempDir()
	})

	It("picks the ibdev-less ECPF on an RDMA-free link (standard socket-direct)", func() {
		mockECPF("0000:03:00.0", "mlx5_0") // master, has RDMA
		mockECPF("0001:03:00.0", "")       // silenced secondary, ibdev-less
		target, err := selectDMASFTarget(rootFS, []string{"0000:03:00.0", "0001:03:00.0"})
		Expect(err).NotTo(HaveOccurred())
		Expect(target).To(Equal("0001:03:00.0"))
	})

	It("eliminates an ibdev-less ECPF sharing a PCI link with an RDMA-bearing ECPF (step e)", func() {
		mockECPF("0000:03:00.0", "mlx5_0") // RDMA on link 0000:03
		mockECPF("0000:03:00.1", "")       // ibdev-less but same link -> eliminated
		target, err := selectDMASFTarget(rootFS, []string{"0000:03:00.0", "0000:03:00.1"})
		Expect(err).NotTo(HaveOccurred())
		Expect(target).To(BeEmpty())
	})

	It("picks the first eligible ECPF by BDF when several qualify (step f)", func() {
		mockECPF("0000:03:00.0", "mlx5_0") // master, RDMA on link 0000:03
		mockECPF("0002:03:00.0", "")       // ibdev-less, link 0002:03
		mockECPF("0001:03:00.0", "")       // ibdev-less, link 0001:03
		target, err := selectDMASFTarget(rootFS, []string{"0000:03:00.0", "0002:03:00.0", "0001:03:00.0"})
		Expect(err).NotTo(HaveOccurred())
		Expect(target).To(Equal("0001:03:00.0")) // first by sorted BDF
	})

	It("returns empty when every ECPF has an RDMA device (not socket-direct)", func() {
		mockECPF("0000:03:00.0", "mlx5_0")
		mockECPF("0001:03:00.0", "mlx5_1")
		target, err := selectDMASFTarget(rootFS, []string{"0000:03:00.0", "0001:03:00.0"})
		Expect(err).NotTo(HaveOccurred())
		Expect(target).To(BeEmpty())
	})
})

var _ = DescribeTable("dmaSFMAC",
	func(override string, expected string, expectErr bool) {
		mac, err := dmaSFMAC(override, "0001:03:00.0", 8000)
		if expectErr {
			Expect(err).To(HaveOccurred())
			return
		}
		Expect(err).NotTo(HaveOccurred())
		Expect(mac).To(Equal(expected))
	},
	// No override -> deterministic derivation over "<bdf>:<sfnum>".
	Entry("empty override derives the vendor-compatible MAC", "", deriveDMASFMAC("0001:03:00.0", 8000), false),
	// Overrides are validated and normalized to canonical colon form.
	Entry("canonical override passes through", "02:40:51:7c:e3:0f", "02:40:51:7c:e3:0f", false),
	Entry("uppercase override is lowercased", "02:40:51:7C:E3:0F", "02:40:51:7c:e3:0f", false),
	Entry("dash-separated override is normalized", "02-40-51-7c-e3-0f", "02:40:51:7c:e3:0f", false),
	Entry("EUI-64 (8-byte) override is rejected", "00:00:5e:00:53:00:00:01", "", true),
	Entry("garbage override is rejected", "not-a-mac", "", true),
)
