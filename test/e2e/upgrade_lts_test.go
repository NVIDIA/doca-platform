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
	"slices"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// expectedDPUServicesV2510 returns the pre-v26.04 DPUService shape: singleton
// nvidia-k8s-ipam and servicechainset-controller services, no
// kube-state-metrics on the DPU cluster, no per-cluster controller split.
// Only the BFB LTS Phase 1 install runs against this shape; once the operator
// is upgraded to v26.4 the controller reshapes DPUServices to the v26.04
// layout (see expectedDPUServicesV2604) without needing a DPU reprovision.
func expectedDPUServicesV2510(_ *systemTestInput) []string {
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

// expectedDPUServicesCurrent returns the DPUService shape at HEAD: the v26.04 shape plus
// dpu-monitoring, node-problem-detector, and opentelemetry-collector.
// Phases running v26.4 must use expectedDPUServicesV2604 instead.
//
// When the next release changes the shape, rename this to the version it describes and add a
// new expectedDPUServicesCurrent on top of it, so that "Current" always tracks HEAD.
func expectedDPUServicesCurrent(input *systemTestInput) []string {
	return append(expectedDPUServicesV2604(input),
		operatorv1.DPUMonitoringName.String(),
		operatorv1.NodeProblemDetectorName.String(),
		operatorv1.OpenTelemetryCollectorName.String(),
	)
}

// expectedDPUServicesV2604 returns the v26.04 DPUService shape: nvidia-k8s-ipam,
// servicechainset-controller and kube-state-metrics are each split into a per-cluster
// controller service plus a node/RBAC companion service.
//
// Only phases installing or validating v26.4 use this shape.
// expectedDPUServicesV268 and expectedDPUServicesCurrent build on it for later releases.
func expectedDPUServicesV2604(input *systemTestInput) []string {
	c := input.dpuClusters[0]
	return []string{
		operatorv1.FlannelName.String(),
		operatorv1.MultusName.String(),
		operatorv1.SRIOVDevicePluginName.String(),
		operatorv1.SFCControllerName.String(),
		operatorv1.ServiceChainSetCRDsName.String(),
		operatorv1.CNIInstallerName.String(),
		getPerClusterDPUServiceName(operatorv1.NVIPAMControllerName, c.Name, c.Namespace),
		operatorv1.NVIPAMNodeName.String(),
		getPerClusterDPUServiceName(operatorv1.ServiceSetControllerName, c.Name, c.Namespace),
		getPerClusterDPUServiceName(operatorv1.KubeStateMetricsName, c.Name, c.Namespace),
		operatorv1.KubeStateMetricsRBACName.String(),
	}
}

// expectedChangesV268 lists the spec changes the v26.4 → v26.8 hop intentionally
// introduces. v26.8 starts defaulting DPUService.spec.security; strip it from
// the v26.8 "after" artifacts when comparing against the v26.4 "before" baseline.
var expectedChangesV268 = []upgradeExpectedChange{
	{
		gvk: dpuservicev1.GroupVersion.WithKind("DPUService"),
		transform: func(artifact map[string]interface{}) {
			unstructured.RemoveNestedField(artifact, "spec", "security")
		},
	},
}

// The BFB LTS multi-hop upgrade path: install v25.10, validate the v26.4 hop
// with a mandatory full DPU rollout (so DPUs start reporting KubeletVersion),
// validate the v26.8 hop without reprovisioning, then validate HEAD. Each phase
// is its own labeled Ginkgo container, selected by CI via its label. Append a
// new validationPhase for each future hop (v26.10 → …).
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
		dpuClusterRunsCoreDNS:     true,
	})

	validationPhase("v26.4", validationPhaseInput{
		label: Domain.DPFBFBLTSUpgradeV264,

		// Reprovision all DPUs under v26.4 so they start reporting KubeletVersion
		// (required for the v26.8 skew check), then exercise a dependency rollout.
		rolloutAllDPUs:         true,
		rolloutDPFVersionMinor: "v26.4",
		rolloutDependencies:    true,
		verifyKubeletVersion:   true,

		// DPUFlavorTemplate was introduced after v26.4; skip its dependency validation.
		skipDPUFlavorTemplateValidation: true,

		expectedDPFVersion:        func() string { return dpfV264Version },
		expectedKubernetesVersion: "v1.34.0",
		// Phase runs with -e2e.skip-cleanup, so clear the stale dpudevice-protection
		// finalizers here rather than at teardown (#5048585).
		removeStaleDPUDeviceFinalizers: true,

		// v26.4 post-rollout artifacts become the v26.8 comparison baseline.
		artifactsKey:               "v26.4",
		preRolloutArtifactsKey:     "v26.4-pre-rollout",
		preRolloutPrevArtifactsKey: "v25.10",

		expectedDPUServices:   expectedDPUServicesV2604,
		dpuClusterRunsCoreDNS: true,
	})

	validationPhase("v26.8", validationPhaseInput{
		label: Domain.DPFBFBLTSUpgradeV268,

		// No DPU rollout needed: BFB stays at LTS 3.2.1 and DPUs already
		// report KubeletVersion after the mandatory v26.4 rollout.
		rolloutAllDPUs:            false,
		rolloutDependencies:       true,
		verifyKubeletVersion:      true,
		expectedDPFVersion:        func() string { return dpfV268Version },
		expectedKubernetesVersion: "v1.35.6",
		// TODO: Remove once we move to first beta release of 26.8
		dpuClusterRunsCoreDNS: true,

		artifactsKey:     "v26.8",
		prevArtifactsKey: "v26.4",
		// v26.8 starts defaulting DPUService.spec.security; strip it when
		// comparing v26.4 → v26.8 artifacts.
		expectedChanges: expectedChangesV268,

		expectedDPUServices: expectedDPUServicesCurrent,
	})

	validationPhase("current", validationPhaseInput{
		label: Domain.DPFBFBLTSUpgradeCurrent,

		// No DPU rollout. BFB stays at LTS 3.2.1 and DPUs are not reprovisioned.
		rolloutAllDPUs:       false,
		rolloutDependencies:  true,
		verifyKubeletVersion: true,
		expectedDPFVersion:   func() string { return tag },

		artifactsKey:     "current",
		prevArtifactsKey: "v26.8",

		expectedDPUServices: expectedDPUServicesCurrent,
	})
})

// rolloutAllDPUs deletes every DPU in the system namespace and waits for all
// to be recreated with the given expectedDPFVersion. Used in the BFB LTS
// upgrade path to reprovision all DPUs so they report their kubelet version.
func rolloutAllDPUs(ctx context.Context, input *systemTestInput, expectedDPFVersionMajorMinor string) {
	By("Listing all DPUs before rollout")
	dpuList := &provisioningv1.DPUList{}
	Expect(input.client.List(ctx, dpuList, client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())
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
		Expect(client.IgnoreNotFound(input.client.Delete(ctx, &dpuList.Items[i]))).To(Succeed())
	}

	By("Waiting for all DPUs to be recreated with DPFVersion matching " + expectedDPFVersionMajorMinor)
	Eventually(func(g Gomega) {
		for _, before := range dpusBefore {
			updated := &provisioningv1.DPUList{}
			g.Expect(input.client.List(ctx, updated,
				client.InNamespace(dpfOperatorSystemNamespace),
				client.MatchingLabels{util.DPUDeviceNameLabel: before.deviceLabel},
			)).To(Succeed())
			g.Expect(updated.Items).To(HaveLen(1), "DPU for device %s should be recreated", before.deviceLabel)
			dpu := &updated.Items[0]
			g.Expect(string(dpu.GetUID())).NotTo(Equal(before.oldUID), "DPU for device %s should have a new UID", before.deviceLabel)
			g.Expect(dpu.Status.DPFVersion).NotTo(BeNil())
			g.Expect(*dpu.Status.DPFVersion).To(ContainSubstring(expectedDPFVersionMajorMinor),
				"DPU for device %s should have DPFVersion containing %s", before.deviceLabel, expectedDPFVersionMajorMinor)
		}
	}).WithTimeout(20 * time.Minute).WithPolling(time.Second).Should(Succeed())
}

// verifyDPUsHaveKubeletVersion asserts that every DPU in the system namespace
// has a non-empty KubeletVersion in its AgentStatus. Required after DPUs are
// reprovisioned with DPF v26.4+.
func verifyDPUsHaveKubeletVersion(ctx context.Context, input *systemTestInput) {
	By("Verifying all DPUs report KubeletVersion")
	Eventually(func(g Gomega) {
		dpuList := &provisioningv1.DPUList{}
		g.Expect(input.client.List(ctx, dpuList, client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())
		g.Expect(dpuList.Items).NotTo(BeEmpty())
		for _, dpu := range dpuList.Items {
			g.Expect(dpu.Status.AgentStatus).NotTo(BeNil(), "DPU %s should have AgentStatus", dpu.Name)
			g.Expect(dpu.Status.AgentStatus.KubeletVersion).NotTo(BeNil(), "DPU %s should have KubeletVersion", dpu.Name)
			g.Expect(*dpu.Status.AgentStatus.KubeletVersion).NotTo(BeEmpty(), "DPU %s KubeletVersion should not be empty", dpu.Name)
		}
	}).WithTimeout(5 * time.Minute).WithPolling(time.Second).Should(Succeed())
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
