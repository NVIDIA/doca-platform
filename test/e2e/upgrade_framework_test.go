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

// Upgrade validation framework.
//
// An upgrade path is one Describe block per file. Inside it, each phase is
// declared by calling installPhase / validationPhase, which emit a labeled,
// Serial+Ordered Ginkgo container with a consistent set of It steps. Each phase
// runs in its own CI invocation, selected by its Ginkgo label filter. The
// regular previous-GA → main upgrade lives in upgrade_test.go.
//
// validationPhase records its label so isUpgradeValidationPhase (consulted by
// BeforeSuite to skip cleanup between phases) needs no per-phase update.
//
// The companion files split out by responsibility:
//   upgrade_apply.go           — install-side creates and validation-side applies
//   upgrade_artifacts_test.go  — artifact snapshot + comparison

import (
	"context"
	"fmt"
	"strings"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/pkg/conditions"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const reconciliationWaitAfterRollout time.Duration = 30 * time.Second

// installPhaseInput configures one install phase of an upgrade path: provision
// DPUs and create the dependency resources from the phase's config manifests,
// then capture the initial artifact snapshot. All booleans default to false and
// most fields are optional.
type installPhaseInput struct {
	// label is the Ginkgo label used to filter this phase in CI.
	label string
	// skipBFBImageURL clears provInput.bfbImageURL before
	// ProvisionBFBOrBlueFieldSoftwareAndDPUFlavor, so the pre-upgrade state
	// reflects the hardcoded URL from the phase's BFB manifest regardless of
	// BFB_IMAGE_URL.
	skipBFBImageURL bool
	// skipSystemComponentValidation skips the current-shape system-component
	// checks during setup. Set for previous-release installs (e.g. BFB LTS
	// v25.10) whose deployed component shape differs from the current release.
	skipSystemComponentValidation bool
	// expectedKubernetesVersion, if set, is the DPUCluster Status.Version this
	// install should report instead of util.KubernetesVersion. Set for
	// previous-release installs (e.g. BFB LTS v25.10) on an older Kubernetes
	// version than HEAD.
	expectedKubernetesVersion string
	// artifactsKey, if set, captures a snapshot to upgrade-artifacts-<key>.json.
	artifactsKey string
	// expectedDPUServices returns the DPUService names verifySystemReady expects
	// on the DPU cluster at this phase's DPF release.
	expectedDPUServices func(input *systemTestInput) []string
}

// validationPhaseInput configures one validation phase of an upgrade path:
// validate existing resources after the operator has been upgraded externally.
// All booleans default to false and most fields are optional.
type validationPhaseInput struct {
	// label is the Ginkgo label used to filter this phase in CI.
	label string
	// expectedDPFVersion, if set, overrides TAG for this validation phase.
	// This is needed for intermediate released hops in multi-step upgrade paths.
	expectedDPFVersion string
	// patchDeploymentMode, if true, sets DPFOperatorConfig.spec.deploymentMode
	// when the deployed config leaves it empty. Only set it on hops that upgrade
	// a config predating the field (the patch is a no-op when the field is already
	// set).
	patchDeploymentMode bool
	// captureBeforeRollout makes capture+compare happen BEFORE rollout steps.
	// Set true for the regular upgrade, where artifact validation precedes the
	// post-upgrade rollout exercise.
	captureBeforeRollout bool
	// rolloutAllDPUs deletes every DPU and waits for them to be recreated with
	// the new DPFVersion. Used by BFB LTS phases that bump major.minor.
	rolloutAllDPUs bool
	// rolloutDPFVersionMinor is the major.minor expected in DPFVersion after
	// rolloutAllDPUs (e.g. "v26.4").
	rolloutDPFVersionMinor string
	// rolloutDependencies updates one DPUDeployment to a new dependency set
	// (BFB, DPUFlavor, DPUServiceTemplate, DPUServiceConfiguration) and waits
	// for reconciliation. Used by regular and BFB LTS upgrades.
	rolloutDependencies bool
	// verifyKubeletVersion asserts every DPU reports a non-empty KubeletVersion.
	// Required after DPUs are reprovisioned with DPF v26.4+.
	verifyKubeletVersion bool
	// removeStaleDPUDeviceFinalizers, if true, clears the dpudevice-protection
	// finalizer from unreferenced DPUDevices in an AfterAll. Set for phases that
	// preserve the cluster with -e2e.skip-cleanup (so the AfterSuite teardown,
	// which would otherwise clear them via DeleteDPFOperatorConfig, never runs):
	// after a v25.10 → v26.4 upgrade non-selected DPUDevices can retain the legacy
	// finalizer and stall the eventual teardown (#5048585).
	removeStaleDPUDeviceFinalizers bool
	// artifactsKey captures a snapshot to upgrade-artifacts-<key>.json.
	artifactsKey string
	// prevArtifactsKey compares the current snapshot against this previously
	// captured one.
	prevArtifactsKey string
	// preRolloutArtifactsKey, if set, captures a validation snapshot before any
	// rollout step, so a multi-hop path can prove the operator hop itself did not
	// recreate objects while keeping a separate post-rollout snapshot as the next
	// hop's baseline.
	preRolloutArtifactsKey string
	// preRolloutPrevArtifactsKey, if set, compares the pre-rollout snapshot
	// against a previous phase's snapshot.
	preRolloutPrevArtifactsKey string
	// expectedChanges lists spec changes this hop intentionally introduces (e.g.
	// a newly defaulted field) so the artifact comparison ignores them. Empty for
	// hops that introduce no such change. Applied to every snapshot comparison
	// this phase runs.
	expectedChanges []upgradeExpectedChange
	// expectedDPUServices returns the DPUService names verifySystemReady expects
	// on the DPU cluster at this phase's DPF release. Required; the shape might
	// differ between releases.
	expectedDPUServices func(input *systemTestInput) []string
	// expectedKubernetesVersion, if set, is the DPUCluster Status.Version this
	// install should report instead of util.KubernetesVersion. Set for
	// previous-release installs (e.g. BFB LTS v25.10) on an older Kubernetes
	// version than HEAD.
	expectedKubernetesVersion string
}

// validationPhaseLabels collects the Ginkgo label of every registered
// validation phase as a side effect of validationPhase. BeforeSuite consults it
// (via isUpgradeValidationPhase) to skip cleanup between phases.
var validationPhaseLabels []string

// isUpgradeValidationPhase reports whether the active Ginkgo label filter
// matches any upgrade *validation* phase. Used by BeforeSuite to skip cleanup
// between phases. Install phases are NOT covered here because Phase 1 needs
// normal pre-test cleanup.
func isUpgradeValidationPhase() bool {
	for _, label := range validationPhaseLabels {
		if Label(label).MatchesLabelFilter(GinkgoLabelFilter()) {
			return true
		}
	}
	return false
}

// installPhase emits the Ginkgo container for one install phase: provision DPU
// clusters + BFB + DPUFlavor, create DPUService dependencies (templates,
// configurations, IPAM, optional additional service variants), create
// DPUDeployments per worker node, and capture the initial artifact snapshot.
// Call from inside the upgrade path's Describe block.
func installPhase(description string, in installPhaseInput) {
	if in.expectedDPUServices == nil {
		panic(fmt.Sprintf("install phase %q must set expectedDPUServices", description))
	}
	Context("install: "+description, Labels{in.label, Domain.RequiresNodes}, Serial, Ordered, func() {

		It("create DPFOperatorConfig", func() {
			SystemSetupBeforeSuite(in.skipSystemComponentValidation)
			By("Pre provisioning DPU cluster setup")
			provInput := getProvisionDPUClustersInput()
			provInput.expectedKubernetesVersion = in.expectedKubernetesVersion
			ProvisionDPUClusters(ctx, provInput)
			if in.skipBFBImageURL {
				// Use the hardcoded URL from the BFB manifest regardless of
				// BFB_IMAGE_URL — pre-upgrade state reflects the known
				// previous-release BFB.
				provInput.bfbImageURL = ""
			}
			ProvisionBFBOrBlueFieldSoftwareAndDPUFlavor(ctx, provInput)
		})

		It("create DPUDeployment dependencies", func() {
			createDPUServiceTemplate(ctx, input, input.dpuServiceTemplate)
			createDPUServiceConfiguration(ctx, input, input.dpuServiceConfiguration)
			createAdditionalDPUServiceDependencies(ctx, input)
			createDPUServiceIPAMPool1(ctx, input)
		})

		It("create DPUDeployment objects", func() {
			By("Get worker nodes")
			nodes := &corev1.NodeList{}
			Expect(input.client.List(ctx, nodes,
				client.MatchingLabels{"node-role.kubernetes.io/worker": ""})).To(Succeed())

			By("Creating DPUDeployment objects for each DPU node")
			for i := 0; i < input.numberOfDPUNodes; i++ {
				node := &nodes.Items[i]
				dpuDeployment := input.dpuDeployment.DeepCopy()
				dpuDeployment.SetLabels(CleanupScope.Suite)
				dpuDeployment.SetName(node.GetName())
				// Per-node hostname selector — the only field that has to be
				// patched inline because it varies per worker node. Using
				// the deprecated NodeSelector intentionally: install runs
				// against the previous-release CRD schema. Unit tests cover
				// the new DPUNodeSelector field.
				//nolint:staticcheck
				dpuDeployment.Spec.DPUs.DPUSets[0].NodeSelector = &metav1.LabelSelector{
					MatchLabels: map[string]string{"kubernetes.io/hostname": node.GetName()},
				}
				Expect(input.client.Create(ctx, dpuDeployment)).To(Succeed())
			}
		})

		It("get DPUCluster client", func() {
			getDPUClusterClients(ctx, getProvisionDPUClustersInput())
		})

		It("wait for DPUs to be provisioned", func() {
			By("Waiting for provisioning")
			VerifyDPUClusterWithNodes(ctx, getProvisionDPUClustersInput())
			By("Waiting for system components to be ready")
			verifySystemReady(in.expectedDPUServices(input))
		})

		if in.artifactsKey != "" {
			It("capture DPU and DPUService artifacts after install", func() {
				collectArtifacts(upgradeArtifactsFile(in.artifactsKey))
			})
		}
	})
}

// validationPhase emits the Ginkgo container for one validation phase. The
// common steps (pre-upgrade check, DPF version, DPUCluster client, cluster
// health, DMS image tag) always run; everything else is gated on the input
// fields. Call from inside the upgrade path's Describe block.
func validationPhase(description string, in validationPhaseInput) {
	if in.expectedDPUServices == nil {
		panic(fmt.Sprintf("validation phase %q must set expectedDPUServices", description))
	}
	if in.rolloutAllDPUs && in.rolloutDPFVersionMinor == "" {
		panic(fmt.Sprintf("validation phase %q sets rolloutAllDPUs but not rolloutDPFVersionMinor", description))
	}
	validationPhaseLabels = append(validationPhaseLabels, in.label)
	Context("validation: "+description, Labels{in.label, Domain.RequiresNodes}, Serial, Ordered, func() {

		if in.removeStaleDPUDeviceFinalizers {
			// Runs even when this phase preserves the cluster (-e2e.skip-cleanup),
			// so the stale dpudevice-protection finalizers from a v25.10 → v26.4
			// upgrade are cleared before the cluster is eventually torn down
			// (#5048585). DeleteDPFOperatorConfig clears them for non-skip-cleanup
			// phases; this AfterAll covers the skip-cleanup ones.
			AfterAll(func() {
				removeStaleDPUDeviceProtectionFinalizers(ctx, input.client)
			})
		}

		if in.patchDeploymentMode {
			It("patch DPFOperatorConfig schema bridge fields", func() {
				patchDPFOperatorConfigForSpecDeploymentMode(ctx, input)
			})
		}
		It("validate pre-upgrade conditions pass", func() {
			validatePreUpgradeConditions(ctx, input)
		})
		It("validate the DPF version", func() {
			validateDPFVersionUpgrade(in.expectedDPFVersion)
		})
		It("validate DPUCluster ready", func() {
			validateDPUClusterUpgrade(ctx, getProvisionDPUClustersInput(), in.expectedKubernetesVersion)
		})
		// Create the DPUCluster client only after the DPUCluster upgrade is
		// confirmed complete. The control-plane roll during the upgrade tears
		// down the API server endpoint, so a client (and its tunnel) created
		// earlier binds to a stale endpoint; getDPUClusterClients is one-shot,
		// so it never re-binds, and later DPU-cluster calls (e.g. artifact
		// capture) fail with "connection refused".
		It("get DPUCluster client", func() {
			getDPUClusterClients(ctx, getProvisionDPUClustersInput())
		})
		It("validate DPUCluster is healthy", func() {
			VerifyDPUClusterWithNodes(ctx, getProvisionDPUClustersInput())
			By("Waiting for system components to be ready")
			verifySystemReady(in.expectedDPUServices(input))
		})
		It("validate that DMS Pods are upgraded", func() {
			VerifyHostAgentPodsImageTag(ctx, input)
		})

		It("wait for controllers to reconcile", func() {
			By(fmt.Sprintf("Waiting %s for controllers to reconcile", reconciliationWaitAfterRollout))
			time.Sleep(reconciliationWaitAfterRollout)
		})

		// Capture before any rollout step when the phase compares the
		// operator upgrade itself separately from an intentional rollout.
		if in.captureBeforeRollout {
			registerArtifactCaptureStep(description, "", in.artifactsKey, in.prevArtifactsKey, in.expectedChanges)
		}

		// Capture a pre-rollout snapshot for multi-hop paths that prove the
		// operator hop itself recreated nothing, kept separate from the
		// post-rollout snapshot that becomes the next hop's baseline.
		if in.preRolloutArtifactsKey != "" {
			registerArtifactCaptureStep(description, "before rollout", in.preRolloutArtifactsKey, in.preRolloutPrevArtifactsKey, in.expectedChanges)
		}

		if in.rolloutAllDPUs {
			It(fmt.Sprintf("roll out all DPUs with BFB LTS under %s", description), func() {
				rolloutAllDPUs(ctx, input, in.rolloutDPFVersionMinor)
			})
		}

		if in.rolloutDependencies {
			It("perform DPU and DPUService rollout test", func() {
				rolloutDependencies(ctx, input)
			})
		}

		It("wait for DPUs to be ready and system healthy after rollout", func() {
			VerifyDPUClusterWithNodes(ctx, getProvisionDPUClustersInput())
			By("Waiting for system components to be ready after rollout")
			verifySystemReady(in.expectedDPUServices(input))
		})

		if in.verifyKubeletVersion {
			It("verify all DPUs report KubeletVersion", func() {
				verifyDPUsHaveKubeletVersion(ctx, input)
			})
		}

		// Capture position #2 (BFB LTS): after rollout steps complete.
		if !in.captureBeforeRollout {
			registerArtifactCaptureStep(description, "", in.artifactsKey, in.prevArtifactsKey, in.expectedChanges)
		}
	})
}

// registerArtifactCaptureStep emits an It block that captures a snapshot and
// optionally compares it against a previous one. No-op if artifactsKey is
// empty. See upgrade_artifacts_test.go for the underlying capture/compare
// machinery.
func registerArtifactCaptureStep(phaseDescription, stepSuffix, artifactsKey, prevArtifactsKey string, expectedChanges []upgradeExpectedChange) {
	if artifactsKey == "" {
		return
	}
	itName := "capture DPU/DPUService artifacts"
	if stepSuffix != "" {
		// Disambiguate phases that capture more than one snapshot (e.g. the BFB
		// LTS v26.4 hop captures pre- and post-rollout).
		itName += " " + stepSuffix
	}
	It(itName, func() {
		collectArtifacts(upgradeArtifactsFile(artifactsKey))
		if prevArtifactsKey == "" {
			return
		}
		compareArtifactSnapshots(prevArtifactsKey, artifactsKey, phaseDescription, expectedChanges)
	})
}

// validatePreUpgradeConditions waits for DPFOperatorConfig to report
// PreUpgradeValidationReady=True and asserts the condition remains stable.
func validatePreUpgradeConditions(ctx context.Context, input *systemTestInput) {
	By("Validating pre-upgrade conditions of dpfoperatorconfig with stability verification")

	checkConditionReady := func(g Gomega) {
		dpfOperatorConfig := &operatorv1.DPFOperatorConfig{}
		g.Expect(input.client.Get(ctx, client.ObjectKey{Name: configName, Namespace: dpfOperatorSystemNamespace}, dpfOperatorConfig)).To(Succeed())
		g.Expect(dpfOperatorConfig.Status.ObservedGeneration).To(Equal(dpfOperatorConfig.GetGeneration()))
		g.Expect(dpfOperatorConfig.Status.Conditions).NotTo(BeEmpty())
		g.Expect(conditions.IsTrue(dpfOperatorConfig, operatorv1.PreUpgradeValidationReadyCondition)).To(BeTrue())
	}

	checkReadinessAndStability := func(g Gomega) {
		// Step 1: wait for the condition to become True.
		g.Eventually(checkConditionReady, 10*time.Second, 1*time.Second).Should(Succeed())
		// Step 2: verify stability — if this fails, the outer Eventually retries.
		g.Consistently(checkConditionReady, 10*time.Second, 1*time.Second).Should(Succeed())
	}

	By("Waiting for PreUpgradeValidationReady condition to become True and stable")
	Eventually(checkReadinessAndStability, 10*time.Minute, 20*time.Second).Should(Succeed(),
		"PreUpgradeValidationReady condition should be ready and stable")
}

// validateDPFVersionUpgrade asserts the operator has reached the expected
// version. Empty expectedVersion falls back to TAG, the version under test.
func validateDPFVersionUpgrade(expectedVersion string) {
	if expectedVersion == "" {
		expectedVersion = tag
	}
	Eventually(func(g Gomega) {
		dpfOperatorConfig := &operatorv1.DPFOperatorConfig{}
		g.Expect(input.client.Get(ctx, client.ObjectKey{
			Name:      configName,
			Namespace: dpfOperatorSystemNamespace,
		}, dpfOperatorConfig)).To(Succeed())
		g.Expect(dpfOperatorConfig.Status.Version).NotTo(BeNil(),
			"DPFOperatorConfig.Status.Version must be set before comparing")
		g.Expect(*dpfOperatorConfig.Status.Version).To(Equal(expectedVersion))
	}).WithTimeout(1*time.Minute).WithPolling(1*time.Second).Should(Succeed(),
		"DPF version should be upgraded to the expected version")
}

// validateDPUClusterUpgrade asserts that, after the operator upgrade, every
// Kamaji DPUCluster is in the Ready phase, carries a True Ready condition, and
// reports the expected Kubernetes version (expectedKubernetesVersion, defaulting
// to util.KubernetesVersion). Non-Kamaji clusters are skipped because their
// upgrade is not handled here. This complements the operator (DPF) version check
// in validateDPFVersionUpgrade.
func validateDPUClusterUpgrade(ctx context.Context, input ProvisionDPUClustersInput, expectedKubernetesVersion string) {
	if expectedKubernetesVersion == "" {
		expectedKubernetesVersion = util.KubernetesVersion
	}
	Expect(input.dpuClusters).ToNot(BeEmpty(), "expected at least one DPUCluster to validate after upgrade")
	hasKamajiDPUCluster := false
	for _, dpuCluster := range input.dpuClusters {
		if dpuCluster.Spec.Type == string(provisioningv1.KamajiCluster) {
			hasKamajiDPUCluster = true
			break
		}
	}
	Expect(hasKamajiDPUCluster).To(BeTrue(), "expected at least one Kamaji DPUCluster to validate after upgrade")

	Eventually(func(g Gomega) {
		for _, expectedDPUCluster := range input.dpuClusters {
			dpuCluster := &provisioningv1.DPUCluster{}
			g.Expect(input.client.Get(ctx, client.ObjectKeyFromObject(expectedDPUCluster), dpuCluster)).To(Succeed())

			// Ignore non-Kamaji clusters for now, because we do not handle their upgrade.
			if dpuCluster.Spec.Type != string(provisioningv1.KamajiCluster) {
				continue
			}

			g.Expect(dpuCluster.Status.Phase).To(Equal(provisioningv1.PhaseReady),
				"DPUCluster %s phase should be Ready after upgrade", klog.KObj(dpuCluster))

			readyCondition := conditions.Get(dpuCluster, conditions.ConditionType(provisioningv1.ConditionReady))
			g.Expect(readyCondition).ToNot(BeNil(), "DPUCluster %s Ready condition should be set after upgrade", klog.KObj(dpuCluster))
			g.Expect(readyCondition.Status).To(Equal(metav1.ConditionTrue), "DPUCluster %s Ready condition should be True after upgrade", klog.KObj(dpuCluster))

			g.Expect(dpuCluster.Status.Version).To(Equal(expectedKubernetesVersion),
				"DPUCluster %s should report the expected Kubernetes version", klog.KObj(dpuCluster))
		}
	}).WithTimeout(20*time.Minute).WithPolling(1*time.Second).Should(Succeed(),
		"DPUClusters should be Ready after upgrade and report the expected Kubernetes version")
}

// VerifyHostAgentPodsImageTag asserts every DMS Pod (hostagent component)
// carries the same image tag as the operator currently deployed in the
// cluster. The expected tag is read from DPFOperatorConfig.Status.Version
// rather than the test binary's TAG env var, so the check is correct across
// every phase of every upgrade path, including intermediates where the running
// operator is not yet the version under test.
func VerifyHostAgentPodsImageTag(ctx context.Context, input *systemTestInput) {
	By("Verifying HostAgent Pods have the same image tag as the deployed operator")
	Eventually(func(g Gomega) {
		cfg := &operatorv1.DPFOperatorConfig{}
		g.Expect(input.client.Get(ctx, client.ObjectKey{
			Name:      configName,
			Namespace: dpfOperatorSystemNamespace,
		}, cfg)).To(Succeed())
		g.Expect(cfg.Status.Version).ToNot(BeNil(),
			"DPFOperatorConfig.Status.Version must be set before checking DMS tags")
		operatorVersion := *cfg.Status.Version

		dmsPods := &corev1.PodList{}
		g.Expect(input.client.List(ctx, dmsPods,
			client.InNamespace(dpfOperatorSystemNamespace),
			client.MatchingLabels{util.ProvisioningComponentLabelKey: "hostagent"},
		)).To(Succeed())

		for _, pod := range dmsPods.Items {
			g.Expect(pod.Spec.Containers).ToNot(BeEmpty(), "DMS Pod should have containers")
			for _, container := range pod.Spec.Containers {
				g.Expect(container.Image).To(ContainSubstring(operatorVersion),
					fmt.Sprintf("DMS Pod %s should have image tag %s", pod.Name, operatorVersion))
			}
		}
	}).WithTimeout(3*time.Minute).WithPolling(1*time.Second).Should(Succeed(),
		"DMS Pods should have the same image tag as the deployed operator")
}

// verifySystemReady checks that the DPF system components are healthy on the
// DPU cluster. The pod-name list is intentionally a minimum viable subset of
// the important ones. The expected DPUService names are supplied by the caller
// (phase.expectedDPUServices) so each upgrade phase asserts the DPUService
// shape that matches its DPF release.
func verifySystemReady(dpuServiceNames []string) {
	VerifyClusterPods(ctx, dpuClusterClient[0], []string{
		// Kubernetes system pods
		"kube-flannel-ds", "coredns", "kube-proxy",
		// DPF system components
		"nvidia-k8s-ipam", "sfc-controller",
		// DPUDeployment pods
		"example",
	})

	verifyDPUServicesReady(ctx, input, dpfOperatorSystemNamespace, dpuServiceNames)
}

// rolloutDependencies simulates a post-upgrade dependency rollout by creating
// the current BFB, DPUFlavor, "-rollout"-suffixed DPUServiceTemplate, and
// DPUServiceConfiguration objects from the current manifests and updating one
// DPUDeployment to reference them.
func rolloutDependencies(ctx context.Context, input *systemTestInput) {
	By("Creating current BFB and DPUFlavor")
	ProvisionBFBOrBlueFieldSoftwareAndDPUFlavor(ctx, getProvisionDPUClustersInput())

	By("Creating current DPUServiceTemplate")
	currentTemplate := input.dpuServiceTemplate.DeepCopy()
	currentTemplate.SetLabels(CleanupScope.Suite)
	currentTemplate.SetName(input.dpuServiceTemplate.Name + "-rollout")
	useDummyDPUServiceChart(currentTemplate)
	Expect(input.client.Create(ctx, currentTemplate)).To(Succeed())

	By("Creating current DPUServiceConfiguration")
	currentConfig := input.dpuServiceConfiguration.DeepCopy()
	currentConfig.SetLabels(CleanupScope.Suite)
	currentConfig.SetName(input.dpuServiceConfiguration.Name + "-rollout")
	Expect(input.client.Create(ctx, currentConfig)).To(Succeed())

	By("Selecting one DPUDeployment to update")
	dpuDeploymentList := &dpuservicev1.DPUDeploymentList{}
	Expect(input.client.List(ctx, dpuDeploymentList, client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())
	Expect(dpuDeploymentList.Items).To(HaveLen(input.numberOfDPUNodes), "expected one DPUDeployment per DPU node")

	// Re-apply the target manifest so fields a new release adds (e.g. v26.4's
	// required dpuSetStrategy) reach DPUDeployments created under older releases.
	By("Re-applying the target manifest to every DPUDeployment")
	for i := range dpuDeploymentList.Items {
		dpuDeployment := &dpuDeploymentList.Items[i]
		patchBase := dpuDeployment.DeepCopy()
		desiredSpec := input.dpuDeployment.DeepCopy().Spec
		for j := range desiredSpec.DPUs.DPUSets {
			if j < len(dpuDeployment.Spec.DPUs.DPUSets) {
				// NodeSelector is filled in per worker node at install, not in the manifest.
				//nolint:staticcheck
				desiredSpec.DPUs.DPUSets[j].NodeSelector = dpuDeployment.Spec.DPUs.DPUSets[j].NodeSelector
			}
		}
		dpuDeployment.Spec = desiredSpec
		Expect(input.client.Patch(ctx, dpuDeployment, client.MergeFrom(patchBase))).To(Succeed())
	}

	selectedDPUDeployment := &dpuDeploymentList.Items[0]
	By(fmt.Sprintf("Selected DPUDeployment: %s", selectedDPUDeployment.GetName()))

	By("Updating selected DPUDeployment to reference current BFB, DPUFlavor, DPUServiceTemplate and DPUServiceConfiguration")
	original := selectedDPUDeployment.DeepCopy()
	selectedDPUDeployment.Spec.DPUs.BFB = ptr.To(input.bfb.Name)
	selectedDPUDeployment.Spec.DPUs.Flavor = input.dpuFlavor.Name
	primaryServiceName := input.dpuServiceTemplate.Name
	svc, ok := selectedDPUDeployment.Spec.Services[primaryServiceName]
	Expect(ok).To(BeTrue(), "DPUDeployment %s should contain service %s", selectedDPUDeployment.Name, primaryServiceName)
	svc.ServiceTemplate = currentTemplate.Name
	svc.ServiceConfiguration = currentConfig.Name
	selectedDPUDeployment.Spec.Services[primaryServiceName] = svc
	Expect(input.client.Patch(ctx, selectedDPUDeployment, client.MergeFrom(original))).To(Succeed())

	By("Waiting for selected DPUDeployment Reconciled conditions to become True")
	Eventually(func(g Gomega) {
		g.Expect(input.client.Get(ctx, client.ObjectKeyFromObject(selectedDPUDeployment), selectedDPUDeployment)).To(Succeed())
		for _, condType := range []conditions.ConditionType{
			dpuservicev1.ConditionDPUSetsReconciled,
			dpuservicev1.ConditionDPUServicesReconciled,
			dpuservicev1.ConditionDPUServiceChainsReconciled,
			dpuservicev1.ConditionDPUServiceInterfacesReconciled,
		} {
			g.Expect(conditions.IsTrue(selectedDPUDeployment, condType)).To(BeTrue(),
				"%s should be True for %s", condType, selectedDPUDeployment.Name)
		}
	}).WithTimeout(20 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())

	verifyDPUDeploymentDependencyTracking(ctx, input)
}

// verifyDPUDeploymentDependencyTracking asserts that consumed-by-DPUDeployment
// labels on dependency resources accurately reflect current DPUDeployment
// references. Referenced dependencies must carry dependency labels; unreferenced
// dependencies in the namespace must have been released. Regression coverage for
// the MR 4849 dependency tracking fix.
//
// Wrapped in Eventually because dependency tracking is reconciler-driven and
// may lag the DPUDeployment patch by a few seconds.
func verifyDPUDeploymentDependencyTracking(ctx context.Context, input *systemTestInput) {
	By("Verifying dependency consumed-by-DPUDeployment labels match current references")
	Eventually(func(g Gomega) {
		activeBFBs := map[string]bool{}
		activeFlavors := map[string]bool{}
		activeServiceConfigurations := map[string]bool{}
		activeServiceTemplates := map[string]bool{}
		deployments := &dpuservicev1.DPUDeploymentList{}
		g.Expect(input.client.List(ctx, deployments, client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())
		g.Expect(deployments.Items).NotTo(BeEmpty())
		for i := range deployments.Items {
			deployment := &deployments.Items[i]
			g.Expect(ptr.Deref(deployment.Spec.DPUs.BFB, "")).NotTo(BeEmpty(), "DPUDeployment %s should reference a BFB", deployment.Name)
			g.Expect(deployment.Spec.DPUs.Flavor).NotTo(BeEmpty(), "DPUDeployment %s should reference a DPUFlavor", deployment.Name)
			activeBFBs[ptr.Deref(deployment.Spec.DPUs.BFB, "")] = true
			activeFlavors[deployment.Spec.DPUs.Flavor] = true
			for serviceName, service := range deployment.Spec.Services {
				g.Expect(service.ServiceConfiguration).NotTo(BeEmpty(),
					"DPUDeployment %s service %s should reference a DPUServiceConfiguration", deployment.Name, serviceName)
				g.Expect(service.ServiceTemplate).NotTo(BeEmpty(),
					"DPUDeployment %s service %s should reference a DPUServiceTemplate", deployment.Name, serviceName)
				activeServiceConfigurations[service.ServiceConfiguration] = true
				activeServiceTemplates[service.ServiceTemplate] = true
			}
		}

		bfbs := &provisioningv1.BFBList{}
		g.Expect(input.client.List(ctx, bfbs, client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())
		assertDependencyLabels(g, "BFB", activeBFBs, ToClientObjectSlice(bfbs.Items))

		flavors := &provisioningv1.DPUFlavorList{}
		g.Expect(input.client.List(ctx, flavors, client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())
		assertDependencyLabels(g, "DPUFlavor", activeFlavors, ToClientObjectSlice(flavors.Items))

		configurations := &dpuservicev1.DPUServiceConfigurationList{}
		g.Expect(input.client.List(ctx, configurations, client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())
		assertDependencyLabels(g, "DPUServiceConfiguration", activeServiceConfigurations, ToClientObjectSlice(configurations.Items))

		templates := &dpuservicev1.DPUServiceTemplateList{}
		g.Expect(input.client.List(ctx, templates, client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())
		assertDependencyLabels(g, "DPUServiceTemplate", activeServiceTemplates, ToClientObjectSlice(templates.Items))
	}).WithTimeout(5 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())
}

func assertDependencyLabels(g Gomega, kind string, activeNames map[string]bool, objects []client.Object) {
	g.Expect(objects).NotTo(BeEmpty(), "expected %s objects to exist", kind)
	seen := map[string]bool{}
	for _, obj := range objects {
		seen[obj.GetName()] = true
		hasConsumedByLabel := hasDPUDeploymentDependencyLabel(obj)
		hasFinalizer := hasDPUDeploymentFinalizer(obj)
		if activeNames[obj.GetName()] {
			g.Expect(hasConsumedByLabel).To(BeTrue(),
				"referenced %s %s should have consumed-by-DPUDeployment labels", kind, obj.GetName())
			g.Expect(hasFinalizer).To(BeTrue(),
				"referenced %s %s should have DPUDeployment finalizer", kind, obj.GetName())
		} else {
			g.Expect(hasConsumedByLabel).To(BeFalse(),
				"unreferenced %s %s should not retain consumed-by-DPUDeployment labels", kind, obj.GetName())
			g.Expect(hasFinalizer).To(BeFalse(),
				"unreferenced %s %s should not retain DPUDeployment finalizer", kind, obj.GetName())
		}
	}
	for name := range activeNames {
		g.Expect(seen[name]).To(BeTrue(), "referenced %s %s should exist", kind, name)
	}
}

func hasDPUDeploymentDependencyLabel(obj client.Object) bool {
	for key := range obj.GetLabels() {
		if strings.HasPrefix(key, dpuservicev1.DependentDPUDeploymentLabelKeyPrefix) {
			return true
		}
	}
	return false
}

func hasDPUDeploymentFinalizer(obj client.Object) bool {
	for _, finalizer := range obj.GetFinalizers() {
		if finalizer == dpuservicev1.DPUDeploymentFinalizer {
			return true
		}
	}
	return false
}
