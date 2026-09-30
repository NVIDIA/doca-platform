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
	"github.com/nvidia/doca-platform/test/e2e/upgrade/rollout"

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

// stripDefaultedDPUServiceSecurity rewinds the v26.8 DPUService.spec.security
// default so identity compare against v26.4 can ignore it, and bumps the
// matching before generation for that one spec write.
func stripDefaultedDPUServiceSecurity(before, after *[]map[string]interface{}) {
	wantAPIVersion := dpuservicev1.GroupVersion.String()
	for i, a := range *after {
		apiVersion, _ := a["apiVersion"].(string)
		kind, _ := a["kind"].(string)
		if apiVersion != wantAPIVersion || kind != dpuservicev1.DPUServiceKind {
			continue
		}
		unstructured.RemoveNestedField((*after)[i], "spec", "security")
		bumpMatchingBeforeGeneration(*before, (*after)[i])
	}
}

// The BFB LTS multi-hop upgrade path: install v25.10, validate the v26.4 hop
// with a mandatory full DPU rollout (so DPUs start reporting KubeletVersion),
// validate the v26.8 hop without reprovisioning, then validate HEAD by
// concurrently reprovisioning one DPU with the current BFB and the other with
// its existing BFB. Each phase is its own labeled Ginkgo container, selected by
// CI via its label. Append a new validationPhase for each future hop.
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

		// Reprovision both DPUs once under v26.4 so they start reporting
		// KubeletVersion (required for the v26.8 skew check). Move the first
		// DPUDeployment to current dependencies while the second keeps its
		// existing dependencies. DPUFlavorTemplate validation is skipped because
		// the resource was introduced after v26.4.
		rolloutAfterUpgrade: rolloutDependencies(
			rollout.WithoutDPUFlavorTemplateValidation(),
			rollout.ExpectDPFVersion(func() string { return dpfV264Version }),
			rollout.ForDPUDeployment(0, rollout.ReprovisionWithCurrentDependencies()),
			rollout.ForDPUDeployment(1, rollout.ReprovisionWithExistingDependencies(
				// DPUSetStrategy was introduced in v26.4 as a required field.
				func(spec *dpuservicev1.DPUDeploymentSpec) {
					spec.DPUs.DPUSetStrategy.Type = provisioningv1.RollingUpdateStrategyType
				},
			)),
		),
		verifyKubeletVersion: true,

		expectedDPFVersion:        func() string { return dpfV264Version },
		expectedKubernetesVersion: "v1.34.0",
		// Phase runs with -e2e.skip-cleanup, so clear the stale dpudevice-protection
		// finalizers here rather than at teardown (#5048585).
		removeStaleDPUDeviceFinalizers: true,

		// v26.4 post-rollout artifacts become the v26.8 comparison baseline.
		artifactsKey:                    "v26.4",
		compareArtifactsToBeforeRollout: "v25.10",

		expectedDPUServices:   expectedDPUServicesV2604,
		dpuClusterRunsCoreDNS: true,
	})

	validationPhase("v26.8", validationPhaseInput{
		label: Domain.DPFBFBLTSUpgradeV268,

		// Keep the BFB on LTS 3.2.1 and exercise a dependency rollout on the
		// first DPUDeployment. DPUs already report KubeletVersion after the
		// mandatory v26.4 rollout.
		rolloutAfterUpgrade: rolloutDependencies(
			rollout.ExpectDPFVersion(func() string { return dpfV268Version }),
			rollout.ForDPUDeployment(0, rollout.WithCurrentDependencies()),
		),
		verifyKubeletVersion: true,
		expectedDPFVersion:   func() string { return dpfV268Version },

		artifactsKey:                    "v26.8",
		compareArtifactsToBeforeRollout: "v26.4",
		artifactWaits:                   []artifactWait{waitForSFCInterfaceMigration},
		artifactChecks:                  []artifactCheck{assertSFCInterfaceMigration},
		artifactNormalizes: []artifactNormalize{
			filterUpgradeInterfaceArtifacts,
			stripDefaultedDPUServiceSecurity,
		},

		expectedDPUServices: expectedDPUServicesCurrent,
	})

	validationPhase("current", validationPhaseInput{
		label: Domain.DPFBFBLTSUpgradeCurrent,

		// Reprovision both DPUs concurrently: the selected DPU moves to the
		// current BFB while the other DPU keeps the existing BFB LTS 3.2.1.
		rolloutAfterUpgrade: rolloutDependencies(
			rollout.ExpectDPFVersion(func() string { return tag }),
			rollout.ForDPUDeployment(0, rollout.ReprovisionWithCurrentDependencies()),
			rollout.ForDPUDeployment(1, rollout.ReprovisionWithExistingDependencies()),
		),
		verifyKubeletVersion: true,
		expectedDPFVersion:   func() string { return tag },

		artifactsKey:                    "current",
		compareArtifactsToBeforeRollout: "v26.8",

		expectedDPUServices: expectedDPUServicesCurrent,
	})
})

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
