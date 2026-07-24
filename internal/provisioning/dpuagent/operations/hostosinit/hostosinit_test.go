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

package hostosinit

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/cmd/dpuagent/opts"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations/nvconfig"
	pciutil "github.com/nvidia/doca-platform/internal/provisioning/utils/pci"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const testPCI = "0000:03:00.0"

func releaseRequiredFlavor() provisioningv1.DPUFlavor {
	return provisioningv1.DPUFlavor{
		Spec: provisioningv1.DPUFlavorSpec{
			NVConfig: []provisioningv1.NVConfig{{Device: ptr.To("p0"), Parameters: []string{"DELAY_HOST_OS_INIT=0x3"}}},
		},
	}
}

func newOptCtx(flavor provisioningv1.DPUFlavor) *operations.Context {
	return &operations.Context{
		DPUFlavor: flavor,
		LatestDPU: &provisioningv1.DPU{
			ObjectMeta: metav1.ObjectMeta{Name: "dpu-1", Namespace: "ns", UID: "uid-1"},
			Status: provisioningv1.DPUStatus{
				AgentStatus: &provisioningv1.AgentStatus{},
			},
		},
		Options: opts.Options{
			ZeroTrustMode: true,
			DPUNamespace:  "ns",
			DPUName:       "dpu-1",
			DPUUID:        "uid-1",
		},
		DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
			return []pciutil.NICPort{{Netdev: "p0", PCIAddress: testPCI}}, nil
		},
		UpdateStatusUntilSuccess: func(context.Context) error { return nil },
	}
}

const sampleMlxregGetReleased = `Sending access register...

Field Name              | Data
=====================================
delay_host_os_init_clr  | 0x00000000
delay_host_os_init      | 0x00000000
host_os_init_mode       | 0x00000003
=====================================
`

const sampleMlxregGetHeld = `Sending access register...

Field Name              | Data
=====================================
delay_host_os_init_clr  | 0x00000000
delay_host_os_init      | 0x00000001
host_os_init_mode       | 0x00000003
=====================================
`

var _ = Describe("ReleaseHostOSInit", func() {
	It("returns nil with skipped status when release is not required", func() {
		optCtx := newOptCtx(provisioningv1.DPUFlavor{})
		op := &ReleaseHostOSInit{}
		Expect(op.Execute(context.Background(), optCtx)).To(Succeed())
		Expect(optCtx.Status.HostOSInit).NotTo(BeNil())
		Expect(optCtx.Status.HostOSInit.Skipped).NotTo(BeNil())
	})

	It("returns nil with succeeded when hold register already cleared", func() {
		runBash := func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
			if strings.Contains(cmd, "mlxreg") && strings.Contains(cmd, "--get") {
				var stdout bytes.Buffer
				stdout.WriteString(sampleMlxregGetReleased)
				return stdout, bytes.Buffer{}, nil
			}
			return bytes.Buffer{}, bytes.Buffer{}, fmt.Errorf("unexpected: %s", cmd)
		}
		optCtx := newOptCtx(releaseRequiredFlavor())
		op := &ReleaseHostOSInit{runBash: runBash}
		Expect(op.Execute(context.Background(), optCtx)).To(Succeed())
		Expect(optCtx.Status.HostOSInit.Succeeded).NotTo(BeNil())
	})

	It("returns nil with succeeded after gate ready and mlxreg set", func() {
		gets := 0
		runBash := func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
			if strings.Contains(cmd, "mlxreg") && strings.Contains(cmd, "--get") {
				gets++
				var stdout bytes.Buffer
				if gets == 1 {
					stdout.WriteString(sampleMlxregGetHeld)
				} else {
					stdout.WriteString(sampleMlxregGetReleased)
				}
				return stdout, bytes.Buffer{}, nil
			}
			if strings.Contains(cmd, "mlxreg") && strings.Contains(cmd, "--set") {
				return bytes.Buffer{}, bytes.Buffer{}, nil
			}
			return bytes.Buffer{}, bytes.Buffer{}, fmt.Errorf("unexpected: %s", cmd)
		}
		scheme := runtime.NewScheme()
		Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())
		dpu := &provisioningv1.DPU{
			ObjectMeta: metav1.ObjectMeta{Name: "dpu-1", Namespace: "ns", UID: "uid-1"},
			Status: provisioningv1.DPUStatus{
				OperationalConditions: []metav1.Condition{{
					Type:   string(provisioningv1.DPUOperationalCondDPUServiceCriticalPodsReady),
					Status: metav1.ConditionTrue,
				}},
			},
		}
		optCtx := newOptCtx(releaseRequiredFlavor())
		optCtx.Client = fake.NewClientBuilder().WithScheme(scheme).WithObjects(dpu).WithStatusSubresource(dpu).Build()
		op := &ReleaseHostOSInit{runBash: runBash}
		Expect(op.Execute(context.Background(), optCtx)).To(Succeed())
		Expect(optCtx.Status.HostOSInit.Succeeded).NotTo(BeNil())
		Expect(optCtx.Status.HostOSInit.Succeeded.ReleaseAfter).NotTo(BeNil())
		Expect(optCtx.Status.HostOSInit.Succeeded.ReleaseAfter.DPUServiceCriticalPodsReady).NotTo(BeNil())
	})

	It("preflights and releases every wildcard target", func() {
		const secondPCI = "0000:03:00.1"
		trace := []string{}
		released := map[string]bool{}
		runBash := func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
			pci := testPCI
			if strings.Contains(cmd, secondPCI) {
				pci = secondPCI
			}
			switch {
			case strings.Contains(cmd, "--get"):
				trace = append(trace, "get "+pci)
				if released[pci] {
					return *bytes.NewBufferString(sampleMlxregGetReleased), bytes.Buffer{}, nil
				}
				return *bytes.NewBufferString(sampleMlxregGetHeld), bytes.Buffer{}, nil
			case strings.Contains(cmd, "--set"):
				trace = append(trace, "set "+pci)
				released[pci] = true
				return bytes.Buffer{}, bytes.Buffer{}, nil
			default:
				return bytes.Buffer{}, bytes.Buffer{}, fmt.Errorf("unexpected: %s", cmd)
			}
		}
		flavor := releaseRequiredFlavor()
		flavor.Spec.NVConfig[0].Device = nil
		optCtx := newOptCtx(flavor)
		optCtx.DiscoverPorts = func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
			return []pciutil.NICPort{
				{Netdev: "p1", PCIAddress: secondPCI},
				{Netdev: "p0", PCIAddress: testPCI},
			}, nil
		}
		scheme := runtime.NewScheme()
		Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())
		dpu := &provisioningv1.DPU{
			ObjectMeta: metav1.ObjectMeta{Name: "dpu-1", Namespace: "ns"},
			Status: provisioningv1.DPUStatus{OperationalConditions: []metav1.Condition{{
				Type: string(provisioningv1.DPUOperationalCondDPUServiceCriticalPodsReady), Status: metav1.ConditionTrue,
			}}},
		}
		optCtx.Client = fake.NewClientBuilder().WithScheme(scheme).WithObjects(dpu).WithStatusSubresource(dpu).Build()

		Expect((&ReleaseHostOSInit{runBash: runBash}).Execute(context.Background(), optCtx)).To(Succeed())
		Expect(trace).To(Equal([]string{
			"get " + testPCI, "get " + secondPCI,
			"set " + testPCI, "get " + testPCI,
			"set " + secondPCI, "get " + secondPCI,
		}))
	})

	It("retries only the unreleased wildcard target after a partial failure", func() {
		const secondPCI = "0000:03:00.1"
		setPCIs := []string{}
		secondSetFails := true
		secondReleased := false
		runBash := func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
			pci := testPCI
			if strings.Contains(cmd, secondPCI) {
				pci = secondPCI
			}
			if strings.Contains(cmd, "--get") {
				if pci == testPCI || secondReleased {
					return *bytes.NewBufferString(sampleMlxregGetReleased), bytes.Buffer{}, nil
				}
				return *bytes.NewBufferString(sampleMlxregGetHeld), bytes.Buffer{}, nil
			}
			if strings.Contains(cmd, "--set") {
				setPCIs = append(setPCIs, pci)
				if secondSetFails {
					secondSetFails = false
					return bytes.Buffer{}, bytes.Buffer{}, fmt.Errorf("temporary mlxreg failure")
				}
				secondReleased = true
				return bytes.Buffer{}, bytes.Buffer{}, nil
			}
			return bytes.Buffer{}, bytes.Buffer{}, fmt.Errorf("unexpected: %s", cmd)
		}
		flavor := releaseRequiredFlavor()
		flavor.Spec.NVConfig[0].Device = nil
		optCtx := newOptCtx(flavor)
		optCtx.DiscoverPorts = func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
			return []pciutil.NICPort{
				{Netdev: "p0", PCIAddress: testPCI},
				{Netdev: "p1", PCIAddress: secondPCI},
			}, nil
		}
		scheme := runtime.NewScheme()
		Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())
		dpu := &provisioningv1.DPU{
			ObjectMeta: metav1.ObjectMeta{Name: "dpu-1", Namespace: "ns"},
			Status: provisioningv1.DPUStatus{OperationalConditions: []metav1.Condition{{
				Type: string(provisioningv1.DPUOperationalCondDPUServiceCriticalPodsReady), Status: metav1.ConditionTrue,
			}}},
		}
		optCtx.Client = fake.NewClientBuilder().WithScheme(scheme).WithObjects(dpu).WithStatusSubresource(dpu).Build()
		op := &ReleaseHostOSInit{runBash: runBash}

		Expect(op.Execute(context.Background(), optCtx)).To(MatchError(ContainSubstring("temporary mlxreg failure")))
		Expect(op.Execute(context.Background(), optCtx)).To(Succeed())
		Expect(setPCIs).To(Equal([]string{secondPCI, secondPCI}))
	})

	It("soft-retries without mlxreg when current boot still has DELAY_HOST_OS_INIT pending", func() {
		calls := 0
		optCtx := newOptCtx(releaseRequiredFlavor())
		optCtx.CurrentBootID = "boot-id"
		optCtx.Status.LastObservedPendingNVConfig = &provisioningv1.PendingNVConfigState{
			BootID: "boot-id",
			Devices: []provisioningv1.PendingNVConfigDevice{{
				Device: testPCI,
				Entries: []provisioningv1.PendingNVConfigEntry{{
					Name: "delay_host_os_init", Current: "DEVICE_DEFAULT(0)", NextBoot: "ENABLE_USER(3)",
				}},
			}},
		}
		op := &ReleaseHostOSInit{runBash: func(string) (bytes.Buffer, bytes.Buffer, error) {
			calls++
			return bytes.Buffer{}, bytes.Buffer{}, nil
		}}

		err := op.Execute(context.Background(), optCtx)
		Expect(err).To(MatchError(ContainSubstring("host OS init release blocked")))
		Expect(err.Error()).To(ContainSubstring("DELAY_HOST_OS_INIT"))
		Expect(err.Error()).To(ContainSubstring(testPCI))
		Expect(calls).To(Equal(0))
		Expect(optCtx.Status.HostOSInit).To(BeNil())
	})

	It("returns error when gate is not ready without terminal hostOSInit", func() {
		runBash := func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
			if strings.Contains(cmd, "mlxreg") && strings.Contains(cmd, "--get") {
				var stdout bytes.Buffer
				stdout.WriteString(sampleMlxregGetHeld)
				return stdout, bytes.Buffer{}, nil
			}
			return bytes.Buffer{}, bytes.Buffer{}, fmt.Errorf("unexpected: %s", cmd)
		}
		scheme := runtime.NewScheme()
		Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())
		dpu := &provisioningv1.DPU{
			ObjectMeta: metav1.ObjectMeta{Name: "dpu-1", Namespace: "ns", UID: "uid-1"},
			Status: provisioningv1.DPUStatus{
				OperationalConditions: []metav1.Condition{{
					Type:   string(provisioningv1.DPUOperationalCondDPUServiceCriticalPodsReady),
					Status: metav1.ConditionFalse,
				}},
			},
		}
		optCtx := newOptCtx(releaseRequiredFlavor())
		optCtx.Client = fake.NewClientBuilder().WithScheme(scheme).WithObjects(dpu).WithStatusSubresource(dpu).Build()
		op := &ReleaseHostOSInit{runBash: runBash}
		err := op.Execute(context.Background(), optCtx)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("waiting for"))
		Expect(optCtx.Status.HostOSInit).To(BeNil())
	})

	It("returns error on mlxreg set without terminal hostOSInit", func() {
		runBash := func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
			if strings.Contains(cmd, "mlxreg") && strings.Contains(cmd, "--get") {
				var stdout bytes.Buffer
				stdout.WriteString(sampleMlxregGetHeld)
				return stdout, bytes.Buffer{}, nil
			}
			if strings.Contains(cmd, "mlxreg") && strings.Contains(cmd, "--set") {
				return bytes.Buffer{}, bytes.Buffer{}, fmt.Errorf("device busy")
			}
			return bytes.Buffer{}, bytes.Buffer{}, fmt.Errorf("unexpected: %s", cmd)
		}
		scheme := runtime.NewScheme()
		Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())
		dpu := &provisioningv1.DPU{
			ObjectMeta: metav1.ObjectMeta{Name: "dpu-1", Namespace: "ns", UID: "uid-1"},
			Status: provisioningv1.DPUStatus{
				OperationalConditions: []metav1.Condition{{
					Type:   string(provisioningv1.DPUOperationalCondDPUServiceCriticalPodsReady),
					Status: metav1.ConditionTrue,
				}},
			},
		}
		optCtx := newOptCtx(releaseRequiredFlavor())
		optCtx.Client = fake.NewClientBuilder().WithScheme(scheme).WithObjects(dpu).WithStatusSubresource(dpu).Build()
		op := &ReleaseHostOSInit{runBash: runBash}
		err := op.Execute(context.Background(), optCtx)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("mlxreg command failed"))
		Expect(optCtx.Status.HostOSInit).To(BeNil())
	})

	It("returns error on mlxreg get without terminal hostOSInit", func() {
		runBash := func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
			if strings.Contains(cmd, "mlxreg") && strings.Contains(cmd, "--get") {
				return bytes.Buffer{}, bytes.Buffer{}, fmt.Errorf("unknown register")
			}
			return bytes.Buffer{}, bytes.Buffer{}, fmt.Errorf("unexpected: %s", cmd)
		}
		optCtx := newOptCtx(releaseRequiredFlavor())
		op := &ReleaseHostOSInit{runBash: runBash}
		err := op.Execute(context.Background(), optCtx)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("unknown register"))
		Expect(optCtx.Status.HostOSInit).To(BeNil())
	})

	It("maps wildcard nvconfig device to all discovered ports", func() {
		flavor := provisioningv1.DPUFlavor{
			Spec: provisioningv1.DPUFlavorSpec{
				NVConfig: []provisioningv1.NVConfig{{Parameters: []string{"DELAY_HOST_OS_INIT=ENABLE_USER"}}},
			},
		}
		optCtx := newOptCtx(flavor)
		resolved, err := nvconfig.EnsureResolved(optCtx)
		Expect(err).NotTo(HaveOccurred())
		Expect(resolved.HostOSInitRequired).To(BeTrue())
		Expect(resolved.HostOSInitPCIs).To(Equal([]string{testPCI}))
	})

	It("parses mlxreg table and compact fields", func() {
		value, ok := parseMlxregField(sampleMlxregGetReleased, mlxregClearField)
		Expect(ok).To(BeTrue())
		Expect(value).To(Equal("0x00000000"))
		n, err := parseMlxregUint(value)
		Expect(err).NotTo(HaveOccurred())
		Expect(n).To(Equal(uint64(0)))

		value, ok = parseMlxregField("delay_host_os_init_clr=0x1\n", mlxregClearField)
		Expect(ok).To(BeTrue())
		Expect(value).To(Equal("0x1"))
	})

	It("keeps flavor and observed NVConfig value parsing distinct", func() {
		mode, ok := parseObservedNVConfigMode("ENABLE_USER(3)")
		Expect(ok).To(BeTrue())
		Expect(mode).To(Equal(uint64(3)))
		_, ok = parseObservedNVConfigMode("FUTURE_MODE(3)")
		Expect(ok).To(BeFalse())
		Expect(isFlavorUserMode("ENABLE_USER(3)")).To(BeFalse())
	})
})

func isFlavorUserMode(value string) bool {
	flavor := provisioningv1.DPUFlavor{Spec: provisioningv1.DPUFlavorSpec{
		NVConfig: []provisioningv1.NVConfig{{Parameters: []string{"DELAY_HOST_OS_INIT=" + value}}},
	}}
	resolved, err := nvconfig.EnsureResolved(newOptCtx(flavor))
	return err == nil && resolved.HostOSInitRequired
}
