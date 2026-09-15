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

package sriovconfig

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/cmd/dpuagent/opts"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"
	pciutil "github.com/nvidia/doca-platform/internal/provisioning/utils/pci"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/utils/ptr"
)

func discoverBF4Ports(_ pciutil.PortScope) ([]pciutil.NICPort, error) { //nolint:unparam
	return []pciutil.NICPort{
		{Netdev: "p0", PCIAddress: "0000:03:00.0"},
		{Netdev: "p1", PCIAddress: "0000:03:00.1"},
	}, nil
}

// bf4Context is the operation context both SR-IOV operations run against in these
// specs: a BlueField-4 DPU with the two ports discoverBF4Ports reports.
func bf4Context(flavor provisioningv1.DPUFlavor) *operations.Context {
	return &operations.Context{
		DPUFlavor:     flavor,
		DiscoverPorts: discoverBF4Ports,
		LatestDPU:     &provisioningv1.DPU{Status: provisioningv1.DPUStatus{DPUType: provisioningv1.DPUTypeBlueField4}},
	}
}

var _ = Describe("declared function groups", func() {
	var tempDir string

	BeforeEach(func() {
		tempDir = GinkgoT().TempDir()
	})

	It("creates the declared SFs and VFs and writes one device plugin config for both", func() {
		By("declaring a workload group, a trusted group with pinned settings, and a VF group")
		dpuFlavor := provisioningv1.DPUFlavor{
			Spec: provisioningv1.DPUFlavorSpec{
				ScalableFunctions: []provisioningv1.ScalableFunction{
					{Count: ptr.To(int32(2)), Device: ptr.To("p0"), PoolName: ptr.To("bf_sf")},
					{
						Count:    ptr.To(int32(1)),
						Device:   ptr.To("p0"),
						PoolName: ptr.To("bf_sf_trusted"),
						Options: &provisioningv1.ScalableFunctionOptions{
							Trusted:     ptr.To(true),
							SFNumStart:  ptr.To(int32(101)),
							CPUList:     ptr.To("0-3"),
							MACAddress:  ptr.To("02:40:51:7C:E3:0F"),
							DisableRoCE: ptr.To(true),
						},
					},
				},
				VirtualFunctions: []provisioningv1.VirtualFunction{{
					Count:    ptr.To(int32(2)),
					Device:   ptr.To("p0"),
					PoolName: ptr.To("bf_vf"),
				}},
			},
		}

		By("mocking the sysfs the VF count is written through")
		numVFsPath := filepath.Join(tempDir, "sys/class/net/p0/device/sriov_numvfs")
		Expect(os.MkdirAll(filepath.Dir(numVFsPath), 0755)).To(Succeed())
		Expect(os.WriteFile(numVFsPath, []byte("0\n"), 0644)).To(Succeed())

		// Netdev-less SFs, so the GUID pass has nothing to rebind.
		mlnxsfOutput := `
{
    "pci/0000:03:00.0/1": {"device": "0000:03:00.0", "sfnum": 0, "aux_dev": "mlx5_core.sf.2"},
    "pci/0000:03:00.0/2": {"device": "0000:03:00.0", "sfnum": 1, "aux_dev": "mlx5_core.sf.3"},
    "pci/0000:03:00.0/3": {"device": "0000:03:00.0", "sfnum": 101, "aux_dev": "mlx5_core.sf.4"}
}
`
		var commands []string
		runBash := func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
			commands = append(commands, cmd)
			var stdout, stderr bytes.Buffer
			if cmd == mlnxSFShowCmd {
				stdout.WriteString(mlnxsfOutput)
			}
			return stdout, stderr, nil
		}

		sfOp := &ReconcileSF{rootFS: tempDir, runBash: runBash}
		vfOp := &ReconcileVF{rootFS: tempDir, runBash: runBash}

		// One context for both, which is how they share the device-plugin config.
		optCtx := bf4Context(dpuFlavor)
		Expect(sfOp.Execute(ctx, optCtx)).To(Succeed())
		Expect(vfOp.Execute(ctx, optCtx)).To(Succeed())

		By("each option becomes an mlnx-sf flag, and p1 is untouched because no group selects it")
		var creates, ipLinks []string
		for _, cmd := range commands {
			switch {
			case strings.HasPrefix(cmd, "/sbin/mlnx-sf --action create"):
				creates = append(creates, cmd)
			case strings.HasPrefix(cmd, "ip link "):
				ipLinks = append(ipLinks, cmd)
			}
		}
		Expect(creates).To(Equal([]string{
			"/sbin/mlnx-sf --action create --device 0000:03:00.0 --sfnum 0",
			"/sbin/mlnx-sf --action create --device 0000:03:00.0 --sfnum 1",
			"/sbin/mlnx-sf --action create --device 0000:03:00.0 --sfnum 101 -t --cpu-list 0-3 --hwaddr 02:40:51:7c:e3:0f --disable-roce",
		}))

		By("the VF count is written to sysfs; VF trust is not supported so no ip link commands run")
		Expect(os.ReadFile(numVFsPath)).To(BeEquivalentTo("2"))
		Expect(ipLinks).To(BeEmpty())

		By("the config the two operations share advertises the SF pools and the VF pool")
		config, err := os.ReadFile(filepath.Join(tempDir, devicePluginConfigPath))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(config)).To(ContainSubstring(`"resourceName": "bf_sf"`))
		Expect(string(config)).To(ContainSubstring(`"p0#0-1"`))
		Expect(string(config)).To(ContainSubstring(`"resourceName": "bf_sf_trusted"`))
		Expect(string(config)).To(ContainSubstring(`"p0#101-101"`))
		Expect(string(config)).To(ContainSubstring(`"resourceName": "bf_vf"`))
	})

	It("creates SFs on the host controller when hostDevice is set", func() {
		dpuFlavor := provisioningv1.DPUFlavor{
			Spec: provisioningv1.DPUFlavorSpec{
				ScalableFunctions: []provisioningv1.ScalableFunction{
					{Count: ptr.To(int32(1)), Device: ptr.To("p0"), HostDevice: ptr.To(true)},
				},
			},
		}
		mlnxsfOutput := `{"pci/0000:03:00.0/1": {"device": "0000:03:00.0", "sfnum": 0, "aux_dev": "mlx5_core.sf.2"}}`

		var commands []string
		operation := &ReconcileSF{
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

		Expect(operation.Execute(ctx, bf4Context(dpuFlavor))).To(Succeed())
		Expect(commands).To(ContainElement("/sbin/mlnx-sf --action create --device 0000:03:00.0 --sfnum 0 -C 1"))
	})

	It("rejects a flavor whose pool name is claimed by both an SF and a VF group", func() {
		dpuFlavor := provisioningv1.DPUFlavor{
			Spec: provisioningv1.DPUFlavorSpec{
				ScalableFunctions: []provisioningv1.ScalableFunction{{Count: ptr.To(int32(1)), Device: ptr.To("p0"), PoolName: ptr.To("shared")}},
				VirtualFunctions:  []provisioningv1.VirtualFunction{{Count: ptr.To(int32(1)), Device: ptr.To("p0"), PoolName: ptr.To("shared")}},
			},
		}
		noop := func(string) (bytes.Buffer, bytes.Buffer, error) { return bytes.Buffer{}, bytes.Buffer{}, nil }

		sfOp := &ReconcileSF{rootFS: tempDir, runBash: noop}
		vfOp := &ReconcileVF{rootFS: tempDir, runBash: noop}

		By("both operations refusing it, so the flavor cannot half-apply")
		optCtx := bf4Context(dpuFlavor)
		Expect(sfOp.Execute(ctx, optCtx)).To(MatchError(ContainSubstring("holds one device type")))
		Expect(vfOp.Execute(ctx, optCtx)).To(MatchError(ContainSubstring("holds one device type")))
	})
})

var _ = Describe("Virtual Function groups", func() {
	var tempDir string
	var numVFsPath string
	var commands []string
	var operation *ReconcileVF

	// mixedGroups declares 4 and 2 VFs on p0, in separate pools.
	mixedGroups := []provisioningv1.VirtualFunction{
		{Count: ptr.To(int32(4)), Device: ptr.To("p0"), PoolName: ptr.To("bf_vf")},
		{Count: ptr.To(int32(2)), Device: ptr.To("p0"), PoolName: ptr.To("bf_vf_2")},
	}

	// execute runs the operation against a flavor declaring only the given VF groups.
	execute := func(groups []provisioningv1.VirtualFunction) error {
		commands = nil
		return operation.Execute(ctx, bf4Context(provisioningv1.DPUFlavor{
			Spec: provisioningv1.DPUFlavorSpec{VirtualFunctions: groups},
		}))
	}

	ipLinkCommands := func() []string {
		var out []string
		for _, cmd := range commands {
			if strings.HasPrefix(cmd, "ip link ") {
				out = append(out, cmd)
			}
		}
		return out
	}

	BeforeEach(func() {
		tempDir = GinkgoT().TempDir()
		commands = nil
		operation = &ReconcileVF{
			rootFS: tempDir,
			runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
				commands = append(commands, cmd)
				return bytes.Buffer{}, bytes.Buffer{}, nil
			},
		}
		for _, netdev := range []string{"p0", "p1"} {
			path := filepath.Join(tempDir, "sys/class/net", netdev, "device/sriov_numvfs")
			Expect(os.MkdirAll(filepath.Dir(path), 0755)).To(Succeed())
			Expect(os.WriteFile(path, []byte("0\n"), 0644)).To(Succeed())
		}
		numVFsPath = filepath.Join(tempDir, "sys/class/net/p0/device/sriov_numvfs")
	})

	It("is skipped only by the operator's escape hatch, not by an empty declaration", func() {
		Expect(operation.ShouldSkip(&operations.Context{Options: opts.Options{SkipVFConfig: true}})).To(BeTrue())
		Expect(operation.ShouldSkip(&operations.Context{})).To(BeFalse())
	})

	It("creates the combined count once across both groups", func() {
		Expect(execute(mixedGroups)).To(Succeed())

		By("the two groups adding up to a single sriov_numvfs value")
		Expect(os.ReadFile(numVFsPath)).To(BeEquivalentTo("6"))

		By("each pool advertising its own index range")
		config, err := os.ReadFile(filepath.Join(tempDir, devicePluginConfigPath))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(config)).To(ContainSubstring(`"p0#0-3"`))
		Expect(string(config)).To(ContainSubstring(`"p0#4-5"`))
	})

	It("pins a MAC to the VF index the group owns", func() {
		Expect(execute([]provisioningv1.VirtualFunction{
			{Count: ptr.To(int32(4)), Device: ptr.To("p0")},
			{
				Count:   ptr.To(int32(1)),
				Device:  ptr.To("p0"),
				Options: &provisioningv1.VirtualFunctionOptions{MACAddress: ptr.To("02:40:51:7C:E3:0F")},
			},
		})).To(Succeed())
		Expect(ipLinkCommands()).To(Equal([]string{"ip link set p0 vf 4 mac 02:40:51:7c:e3:0f"}))
	})

	It("drops the existing VFs before changing a non-zero count", func() {
		Expect(os.WriteFile(numVFsPath, []byte("8\n"), 0644)).To(Succeed())

		Expect(execute([]provisioningv1.VirtualFunction{{Count: ptr.To(int32(2)), Device: ptr.To("p0")}})).To(Succeed())
		Expect(os.ReadFile(numVFsPath)).To(BeEquivalentTo("2"))
	})

	It("does not touch the count when it already matches the declaration", func() {
		Expect(execute(mixedGroups)).To(Succeed())

		Expect(execute(mixedGroups)).To(Succeed())
		By("the count staying put across agent restarts")
		Expect(os.ReadFile(numVFsPath)).To(BeEquivalentTo("6"))
	})

	It("sets a count of zero on a device whose group asks for no VFs", func() {
		Expect(execute(mixedGroups)).To(Succeed())

		Expect(execute([]provisioningv1.VirtualFunction{{Count: ptr.To(int32(0)), Device: ptr.To("p0")}})).To(Succeed())
		Expect(os.ReadFile(numVFsPath)).To(BeEquivalentTo("0"))
	})

	It("leaves a port no group selects exactly as it is", func() {
		Expect(os.WriteFile(numVFsPath, []byte("8\n"), 0644)).To(Succeed())

		By("a flavor with no VF groups at all, whose VFs DPF did not create")
		Expect(execute(nil)).To(Succeed())
		Expect(os.ReadFile(numVFsPath)).To(BeEquivalentTo("8\n"))

		By("and a flavor that only selects the other port")
		Expect(execute([]provisioningv1.VirtualFunction{{Count: ptr.To(int32(2)), Device: ptr.To("p1")}})).To(Succeed())
		Expect(os.ReadFile(numVFsPath)).To(BeEquivalentTo("8\n"))
	})
})
