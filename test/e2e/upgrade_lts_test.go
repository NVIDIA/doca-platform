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

package e2e

import (
	"context"
	"fmt"
	"os"
	"slices"
	"time"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// expectedDPUServicesV2510 returns the pre-v26.04 DPUService shape: singleton
// nvidia-k8s-ipam and servicechainset-controller services, no
// kube-state-metrics on the DPU cluster, no per-cluster controller split.
// Only the BFB LTS Phase 1 install runs against this shape; once the operator
// is upgraded to v26.4 the controller reshapes DPUServices to the current
// layout (see expectedDPUServicesCurrent in upgrade_test.go) without needing
// a DPU reprovision.
func expectedDPUServicesV2510(_ *SystemTestInput) []string {
	return []string{
		operatorv1.FlannelName.String(),
		operatorv1.MultusName.String(),
		operatorv1.SRIOVDevicePluginName.String(),
		operatorv1.OVSCNIName.String(),
		operatorv1.SFCControllerName.String(),
		operatorv1.ServiceChainSetCRDsName.String(),
		operatorv1.CNIInstallerName.String(),
		operatorv1.NVIPAMControllerName.String(),
		operatorv1.ServiceSetControllerName.String(),
	}
}

// The BFB LTS multi-hop upgrade path: install v25.10, validate the v26.4 hop
// with a mandatory full DPU rollout (so DPUs start reporting KubeletVersion),
// then validate the v26.7 hop without reprovisioning. Each phase is its own
// labeled Ginkgo container, selected by CI via its label. Append a new
// validationPhase for each future hop (v26.10 → …).
var _ = Describe("DPF Upgrade LTS", func() {
	installPhase("BFB LTS v25.10", installPhaseInput{
		label: Domain.DPFBFBLTSUpgrade,

		// Pin to the LTS BFB manifest even when CI exports BFB_IMAGE_URL.
		skipBFBImageURL: true,
		// v25.10's servicechainset-controller creates a DPUServiceCredentialRequest with an
		// empty spec.targetCluster.name that the current CRD rejects. Provisioning works
		// without it being Ready, so skip the DPFOperatorConfig.Ready wait.
		skipSystemComponentValidation: true,

		expectedKubernetesVersion: "v1.34.0",
		artifactsKey:              "v25.10",
		expectedDPUServices:       expectedDPUServicesV2510,
	})

	validationPhase("v26.4", validationPhaseInput{
		label: Domain.DPFBFBLTSUpgradeV264,

		// Reprovision all DPUs under v26.4 so they start reporting KubeletVersion
		// (required for the v26.7 skew check), then exercise a dependency rollout.
		rolloutAllDPUs:         true,
		rolloutDPFVersionMinor: "v26.4",
		rolloutDependencies:    true,
		verifyKubeletVersion:   true,

		expectedDPFVersion:        envOrDefault("DPF_V264_VERSION", "v26.4.0"),
		expectedKubernetesVersion: "v1.34.0",
		// Phase runs with -e2e.skip-cleanup, so clear the stale dpudevice-protection
		// finalizers here rather than at teardown (#5048585).
		removeStaleDPUDeviceFinalizers: true,

		// v26.4 post-rollout artifacts become the v26.7 comparison baseline.
		artifactsKey:               "v26.4",
		preRolloutArtifactsKey:     "v26.4-pre-rollout",
		preRolloutPrevArtifactsKey: "v25.10",

		expectedDPUServices: expectedDPUServicesCurrent,
	})

	validationPhase("current", validationPhaseInput{
		label: Domain.DPFBFBLTSUpgradeCurrent,

		// No rollout. BFB stays at LTS 3.2.1 and DPUs are not reprovisioned.
		rolloutAllDPUs:       false,
		verifyKubeletVersion: true,
		patchDeploymentMode:  true,

		artifactsKey:     "current",
		prevArtifactsKey: "v26.4",
		expectedChanges:  expectedChangesCurrent,

		expectedDPUServices: expectedDPUServicesCurrent,
	})
})

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

// rolloutAllDPUs deletes every DPU in the system namespace and waits for all
// to be recreated with the given expectedDPFVersion. Used in the BFB LTS
// upgrade path to reprovision all DPUs so they report their kubelet version.
func rolloutAllDPUs(ctx context.Context, input *SystemTestInput, expectedDPFVersionMajorMinor string) {
	By("Listing all DPUs before rollout")
	dpuList := &provisioningv1.DPUList{}
	Expect(input.Client.List(ctx, dpuList, client.InNamespace(DPFOperatorSystemNamespace))).To(Succeed())
	Expect(dpuList.Items).NotTo(BeEmpty(), "expected DPUs to be present before rollout")

	type dpuRecord struct {
		oldUID      string
		deviceLabel string
	}
	dpusBefore := make([]dpuRecord, len(dpuList.Items))
	for i, dpu := range dpuList.Items {
		deviceLabel := dpu.GetLabels()[util.DPUDeviceNameLabel]
		Expect(deviceLabel).NotTo(BeEmpty(), "DPU %s must have device name label", dpu.Name)
		dpusBefore[i] = dpuRecord{oldUID: string(dpu.GetUID()), deviceLabel: deviceLabel}
	}

	By(fmt.Sprintf("Deleting all %d DPUs to trigger rollout", len(dpusBefore)))
	for i := range dpuList.Items {
		Expect(client.IgnoreNotFound(input.Client.Delete(ctx, &dpuList.Items[i]))).To(Succeed())
	}

	By("Waiting for all DPUs to be recreated with DPFVersion matching " + expectedDPFVersionMajorMinor)
	Eventually(func(g Gomega) {
		for _, before := range dpusBefore {
			updated := &provisioningv1.DPUList{}
			g.Expect(input.Client.List(ctx, updated,
				client.InNamespace(DPFOperatorSystemNamespace),
				client.MatchingLabels{util.DPUDeviceNameLabel: before.deviceLabel},
			)).To(Succeed())
			g.Expect(updated.Items).To(HaveLen(1), "DPU for device %s should be recreated", before.deviceLabel)
			dpu := &updated.Items[0]
			g.Expect(string(dpu.GetUID())).NotTo(Equal(before.oldUID), "DPU for device %s should have a new UID", before.deviceLabel)
			g.Expect(dpu.Status.DPFVersion).NotTo(BeNil())
			g.Expect(*dpu.Status.DPFVersion).To(ContainSubstring(expectedDPFVersionMajorMinor),
				"DPU for device %s should have DPFVersion containing %s", before.deviceLabel, expectedDPFVersionMajorMinor)
		}
	}).WithTimeout(20 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())
}

// verifyDPUsHaveKubeletVersion asserts that every DPU in the system namespace
// has a non-empty KubeletVersion in its AgentStatus. Required after DPUs are
// reprovisioned with DPF v26.4+.
func verifyDPUsHaveKubeletVersion(ctx context.Context, input *SystemTestInput) {
	By("Verifying all DPUs report KubeletVersion")
	Eventually(func(g Gomega) {
		dpuList := &provisioningv1.DPUList{}
		g.Expect(input.Client.List(ctx, dpuList, client.InNamespace(DPFOperatorSystemNamespace))).To(Succeed())
		g.Expect(dpuList.Items).NotTo(BeEmpty())
		for _, dpu := range dpuList.Items {
			g.Expect(dpu.Status.AgentStatus).NotTo(BeNil(), "DPU %s should have AgentStatus", dpu.Name)
			g.Expect(dpu.Status.AgentStatus.KubeletVersion).NotTo(BeNil(), "DPU %s should have KubeletVersion", dpu.Name)
			g.Expect(*dpu.Status.AgentStatus.KubeletVersion).NotTo(BeEmpty(), "DPU %s KubeletVersion should not be empty", dpu.Name)
		}
	}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())
}

// removeStaleDPUDeviceProtectionFinalizers clears provisioning.dpu.nvidia.com/dpudevice-protection
// from DPUDevice objects that are not referenced by any active DPU.
//
// Workaround for v25.10 → v26.4 upgrade (#5048585): non-selected DPUDevices can retain the
// legacy finalizer after upgrade, which blocks DPUDevice deletion and stalls DPFOperatorConfig
// teardown. Only the finalizer is removed; DPUDevice objects are kept.
func removeStaleDPUDeviceProtectionFinalizers(ctx context.Context, testClient client.Client) {
	By("Removing stale dpudevice-protection finalizers from unreferenced DPUDevices (v25.10→v26.4 upgrade workaround)")

	dpuList := &provisioningv1.DPUList{}
	Expect(testClient.List(ctx, dpuList)).To(Succeed())

	referencedDPUDevices := make(map[string]struct{}, len(dpuList.Items))
	for i := range dpuList.Items {
		dpu := &dpuList.Items[i]
		if name := dpu.Spec.DPUDeviceName; name != "" {
			referencedDPUDevices[name] = struct{}{}
		}
		if name := dpu.GetLabels()[util.DPUDeviceNameLabel]; name != "" {
			referencedDPUDevices[name] = struct{}{}
		}
	}

	dpuDeviceList := &provisioningv1.DPUDeviceList{}
	Expect(testClient.List(ctx, dpuDeviceList)).To(Succeed())

	for i := range dpuDeviceList.Items {
		device := &dpuDeviceList.Items[i]
		if _, referenced := referencedDPUDevices[device.Name]; referenced {
			continue
		}
		if !slices.Contains(device.Finalizers, provisioningv1.DPUDeviceFinalizer) {
			continue
		}
		By(fmt.Sprintf("Patching DPUDevice %s/%s: remove %s finalizer",
			device.Namespace, device.Name, provisioningv1.DPUDeviceFinalizer))
		original := device.DeepCopy()
		device.Finalizers = slices.DeleteFunc(device.Finalizers, func(finalizer string) bool {
			return finalizer == provisioningv1.DPUDeviceFinalizer
		})
		Expect(testClient.Patch(ctx, device, client.MergeFrom(original))).To(Succeed())
	}
}
