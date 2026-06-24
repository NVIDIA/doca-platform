/*
Copyright 2025 NVIDIA

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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

//nolint:dupl
var _ = Describe("DPF Upgrade tests", Labels{Domain.DPFUpgrade}, func() {
	Context("Should pass", Labels{Domain.RequiresNodes}, Serial, Ordered, func() {
		It("create DPFOperatorConfig", func() {
			SystemSetupBeforeSuite()
			By("Pre provisioning DPU cluster setup")
			provInput := getProvisionDPUClustersInput()
			ProvisionDPUClusters(ctx, provInput)
			// Use the hardcoded URL from previous/bfb.yaml regardless of BFB_IMAGE_URL, so the
			// pre-upgrade state always reflects the known previous release BFB.
			provInput.bfbImageURL = ""
			ProvisionBFBOrBlueFieldSoftwareAndDPUFlavor(ctx, provInput)
		})

		It("create DPUDeployments dependencies", func() {
			dpuServiceTemplate := generateDPUServiceTemplate(input, "")
			useDummyDPUServiceChart(dpuServiceTemplate)
			Expect(input.client.Create(ctx, dpuServiceTemplate)).To(Succeed())

			dpuServiceConfiguration := generateServiceConfiguration(input, "")
			Expect(input.client.Create(ctx, dpuServiceConfiguration)).To(Succeed())

			dpuServiceTemplate2 := generateDPUServiceTemplate(input, "2")
			useDummyDPUServiceChart(dpuServiceTemplate2)
			Expect(input.client.Create(ctx, dpuServiceTemplate2)).To(Succeed())

			dpuServiceConfiguration2 := generateServiceConfiguration(input, "2")
			dpuServiceConfiguration2.Spec.Interfaces = []dpuservicev1.ServiceInterfaceTemplate{{Name: "net2", Network: "mybrsfc"}}
			Expect(input.client.Create(ctx, dpuServiceConfiguration2)).To(Succeed())

			dpuServiceIPAM := input.ipPoolDPUServiceIPAM.DeepCopy()
			dpuServiceIPAM.SetLabels(CleanupScope.Suite)
			dpuServiceIPAM.SetName("dpudeployment-ipam-pool1")
			dpuServiceIPAM.SetNamespace(dpfOperatorSystemNamespace)
			// Remove selectors so it applies to all nodes
			dpuServiceIPAM.Spec.NodeSelector = nil
			Expect(input.client.Create(ctx, dpuServiceIPAM)).To(Succeed())
		})

		It("create DPUDeployment objects", func() {
			By("Get worker nodes")
			nodes := &corev1.NodeList{}
			Expect(input.client.List(ctx, nodes, client.MatchingLabels{"node-role.kubernetes.io/worker": ""})).To(Succeed())

			By("Creating the DPUDeployment objects")
			for i := 0; i < input.numberOfDPUNodes; i++ {
				node := &nodes.Items[i]
				dpuDeployment := generateDPUDeployment(input, "")
				dpuDeployment.SetName(node.GetName())
				// Intentionally using deprecated field, e2e tests will be updated once we have removed the deprecated field. Unit
				// tests cover the new field, e2e tests cover the old field since there is no more unit test coverage for the deprecated field.
				// This particular test must use the deprecated field since it's using the old CRDs on DPUDeployment creation.
				//nolint:staticcheck
				dpuDeployment.Spec.DPUs.DPUSets[0].NodeSelector = &metav1.LabelSelector{
					MatchLabels: map[string]string{"kubernetes.io/hostname": node.GetName()},
				}
				dpuDeployment.Spec.Services["example-2"] = dpuservicev1.DPUDeploymentServiceConfiguration{
					ServiceTemplate:      "example-2",
					ServiceConfiguration: "example-2",
				}
				dpuDeployment.Spec.ServiceChains.Switches[0] = dpuservicev1.DPUDeploymentSwitch{
					Ports: []dpuservicev1.DPUDeploymentPort{
						{
							Service: &dpuservicev1.DPUDeploymentService{
								InterfaceName: "net1",
								Name:          "example",
								IPAM: &dpuservicev1.IPAM{
									MatchLabels: map[string]string{
										"svc.dpu.nvidia.com/pool": "pool1",
									},
								},
							},
						},
						{
							Service: &dpuservicev1.DPUDeploymentService{
								InterfaceName: "net2",
								Name:          "example-2",
								IPAM: &dpuservicev1.IPAM{
									MatchLabels: map[string]string{
										"svc.dpu.nvidia.com/pool": "pool1",
									},
								},
							},
						},
					},
				}
				Expect(input.client.Create(ctx, dpuDeployment)).To(Succeed())
			}
		})

		It("get DPUCluster client", func() {
			By("Creating a client for the DPUCluster")
			getDPUClusterClients(ctx, getProvisionDPUClustersInput())
		})

		It("wait for DPUs to be provisioned", func() {
			By("Waiting for provisioning")
			VerifyDPUClusterWithNodes(ctx, getProvisionDPUClustersInput())
			By("Waiting for system components to be ready")
			verifySystemReady()
		})

		It("capture DPU and DPUService artifacts before upgrade", func() {
			collectArtifacts(upgradeArtifactsFile("before"))
		})
	})
})

var _ = Describe("DPF Upgrade validation", Labels{Domain.DPFUpgradeValidation}, func() {
	Context("Should pass", Labels{Domain.RequiresNodes}, Serial, Ordered, func() {
		It("patch DPFOperatorConfig for spec.deploymentMode (post-GA upgrade)", func() {
			// Purpose: apply the DPFOperatorConfig change for the breaking introduction of
			// DPFOperatorConfig.spec.deploymentMode (required, no default). The upgrade validation
			// run does not re-apply SystemSetupBeforeSuite, so a cluster upgraded from a GA build
			// can still have no deploymentMode, so use the mode from SetInput for this run.
			patchDPFOperatorConfigForSpecDeploymentMode(ctx, input)
		})

		It("validate rollout is done and pre-upgrade validation successful", func() {
			By("Validating pre-upgrade conditions")
			validatePreUpgradeConditions(ctx, input)
		})

		It("validate the DPF version is upgraded", func() {
			By("Validating the DPF version is upgraded")
			validateDPFVersionUpgrade()
		})

		It("validate DPUCluster upgrade completed", func() {
			By("Validating the DPUCluster upgrade completed")
			validateDPUClusterUpgrade(ctx, getProvisionDPUClustersInput())
		})

		// Initialize the DPUCluster clients after knowing the DPUClusters are upgraded, because
		// we do not expect disruptions for the clients.
		It("get DPUCluster client", func() {
			By("Creating a client for the DPUCluster")
			getDPUClusterClients(ctx, getProvisionDPUClustersInput())
		})

		It("validate DPUCluster", func() {
			By("Validating DPUCluster")
			VerifyDPUClusterWithNodes(ctx, getProvisionDPUClustersInput())
			By("Waiting for system components to be ready")
			verifySystemReady()
		})

		It("validate that DMS Pods are upgraded", func() {
			By("Validating that DMS Pods have the correct same image tag")
			VerifyHostAgentPodsImageTag(ctx, input, tag)
		})

		It("waiting for controllers to reconcile", func() {
			const reconciliationWait = 30 * time.Second
			By(fmt.Sprintf("Waiting %s for controllers to reconcile", reconciliationWait))
			time.Sleep(reconciliationWait)
		})

		It("validate DPU and DPUService artifacts after upgrade", func() {
			validateArtifactsAfterUpgrade()
		})

		It("perform DPU and DPUService rollout test", func() {
			By("Performing DPU and DPUService rollout for one of the DPUDeployments via BFB, DPUFlavor and DPUServiceTemplate update")
			rolloutDependencies(ctx, input)
			By("Waiting for provisioning")
			VerifyDPUClusterWithNodes(ctx, getProvisionDPUClustersInput())
			By("Waiting for system components to be ready")
			verifySystemReady()
		})
	})
})

func VerifyHostAgentPodsImageTag(ctx context.Context, input *systemTestInput, expectedTag string) {
	By("Verifying HostAgent Pods have the same image tag as the operator")
	Eventually(func(g Gomega) {
		dmsPods := &corev1.PodList{}
		g.Expect(input.client.List(ctx, dmsPods,
			client.InNamespace(dpfOperatorSystemNamespace),
			client.MatchingLabels{util.ProvisioningComponentLabelKey: "hostagent"},
		)).To(Succeed())

		for _, pod := range dmsPods.Items {
			g.Expect(pod.Spec.Containers).ToNot(BeEmpty(), "DMS Pod should have containers")
			for _, container := range pod.Spec.Containers {
				g.Expect(container.Image).To(ContainSubstring(expectedTag),
					fmt.Sprintf("DMS Pod %s should have image tag %s", pod.Name, expectedTag))
			}
		}
	}).WithTimeout(3*time.Minute).WithPolling(1*time.Second).Should(Succeed(),
		"DMS Pods should have the same image tag as the operator")
}

func validatePreUpgradeConditions(ctx context.Context, input *systemTestInput) {
	By("Validating pre-upgrade conditions of dpfoperatorconfig with stability verification")

	// Helper function to check if condition is ready
	checkConditionReady := func(g Gomega) {
		dpfOperatorConfig := &operatorv1.DPFOperatorConfig{}
		g.Expect(input.client.Get(ctx, client.ObjectKey{Name: configName, Namespace: dpfOperatorSystemNamespace}, dpfOperatorConfig)).To(Succeed())
		g.Expect(dpfOperatorConfig.Status.ObservedGeneration).To(Equal(dpfOperatorConfig.GetGeneration()))
		g.Expect(dpfOperatorConfig.Status.Conditions).NotTo(BeEmpty())
		g.Expect(conditions.IsTrue(dpfOperatorConfig, operatorv1.PreUpgradeValidationReadyCondition)).To(BeTrue())
	}

	// Function to check both readiness and stability
	checkReadinessAndStability := func(g Gomega) {
		// Step 1: Wait for condition to become True
		g.Eventually(checkConditionReady, 10*time.Second, 1*time.Second).Should(Succeed())

		// Step 2: Verify stability - if this fails, the outer Eventually will retry
		g.Consistently(checkConditionReady, 10*time.Second, 1*time.Second).Should(Succeed())
	}

	// Main retry loop: check readiness, then stability, retry if either fails
	By("Waiting for PreUpgradeValidationReady condition to become True and stable")
	Eventually(checkReadinessAndStability, 10*time.Minute, 20*time.Second).Should(Succeed(),
		"PreUpgradeValidationReady condition should be ready and stable")
}

// upgradeArtifactsFile constructs the file path in which artifacts are going to be stored before and after the upgrade
// for comparison purposes.
func upgradeArtifactsFile(phase string) string {
	return filepath.Join(artifactsDir, "..", "upgrade-artifacts-"+phase+".json")
}

// upgradeExpectedChange describes a known spec change introduced by an upgrade.
// objects that are recreated can't be handled by this struct and need to be handled in a different way.
// transform is applied only to the after artifact of the matching GVK before comparison, resetting the changed field(s)
// back to their pre-upgrade value so the assertion does not fail on expected changes. The matching before artifact's
// generation is bumped by one to account for the single spec change the upgrade introduced.
//
// Example — a field that flips from false to true after upgrade:
//
//	{gvk: dpuservicev1.GroupVersion.WithKind("DPUService"), transform: func(a map[string]interface{}) {
//	    spec, _ := a["spec"].(map[string]interface{})
//	    spec["something"] = false
//	}}
type upgradeExpectedChange struct {
	gvk       schema.GroupVersionKind
	transform func(artifact map[string]interface{})
}

// upgradeExpectedChanges lists spec changes that are intentionally introduced by an
// upgrade. Add entries here to prevent known expected changes from failing the
// artifact comparison.
var upgradeExpectedChanges = []upgradeExpectedChange{
	{
		// Post 26.4 the .spec.security.privileged field is introduced.
		// It gets defaulted after the upgrade if not set.
		gvk: dpuservicev1.GroupVersion.WithKind("DPUService"),
		transform: func(artifact map[string]interface{}) {
			unstructured.RemoveNestedField(artifact, "spec", "security")
		},
	},
}

func applyUpgradeExpectedChanges(before, after []map[string]interface{}) {
	type artifactKey struct{ apiVersion, kind, name, namespace string }
	beforeIdx := make(map[artifactKey]int, len(before))
	for i, b := range before {
		k := artifactKey{
			apiVersion: fmt.Sprintf("%v", b["apiVersion"]),
			kind:       fmt.Sprintf("%v", b["kind"]),
			name:       fmt.Sprintf("%v", b["name"]),
			namespace:  fmt.Sprintf("%v", b["namespace"]),
		}
		beforeIdx[k] = i
	}

	for i, artifact := range after {
		apiVersion, _ := artifact["apiVersion"].(string)
		kind, _ := artifact["kind"].(string)
		gv, err := schema.ParseGroupVersion(apiVersion)
		Expect(err).ToNot(HaveOccurred())
		artifactGVK := gv.WithKind(kind)
		for _, change := range upgradeExpectedChanges {
			if change.gvk != artifactGVK {
				continue
			}
			change.transform(after[i])
			// The spec change introduced by the upgrade bumped the generation once.
			// Increment the matching before artifact's generation so the comparison holds.
			k := artifactKey{
				apiVersion: apiVersion,
				kind:       kind,
				name:       fmt.Sprintf("%v", artifact["name"]),
				namespace:  fmt.Sprintf("%v", artifact["namespace"]),
			}
			if idx, ok := beforeIdx[k]; ok {
				if gen, ok := before[idx]["generation"].(float64); ok {
					before[idx]["generation"] = gen + 1
				}
			}
		}
	}
}

func validateArtifactsAfterUpgrade() {
	collectArtifacts(upgradeArtifactsFile("after"))
	allArtifactsBefore := getArtifacts(upgradeArtifactsFile("before"))
	allArtifactsAfter := getArtifacts(upgradeArtifactsFile("after"))

	By("Applying known expected upgrade changes to after artifacts before comparison")
	applyUpgradeExpectedChanges(allArtifactsBefore, allArtifactsAfter)

	By("Comparing artifacts before and after upgrade")
	Expect(allArtifactsAfter).To(HaveLen(len(allArtifactsBefore)),
		"Number of objects should remain the same after upgrade")

	sort.Slice(allArtifactsBefore, func(i, j int) bool {
		return fmt.Sprintf("%v", allArtifactsBefore[i]) <
			fmt.Sprintf("%v", allArtifactsBefore[j])
	})
	sort.Slice(allArtifactsAfter, func(i, j int) bool {
		return fmt.Sprintf("%v", allArtifactsAfter[i]) <
			fmt.Sprintf("%v", allArtifactsAfter[j])
	})
	Expect(allArtifactsAfter).To(BeComparableTo(allArtifactsBefore),
		"Artifact data (metadata and spec, ignoring order) should remain identical after upgrade")
}

func collectArtifacts(filePath string) {
	By("Collecting artifacts to: " + filePath)
	Expect(os.MkdirAll(filepath.Dir(filePath), 0755)).To(Succeed())

	allArtifacts := make([]map[string]interface{}, 0)

	By("Capturing DPU artifacts")
	dpuList := &provisioningv1.DPUList{}
	Expect(input.client.List(ctx, dpuList, client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())
	allArtifacts = append(allArtifacts, extractArtifacts(ToClientObjectSlice(dpuList.Items))...)

	By("Capturing DPUService artifacts with owned-by-dpudeployment label")
	dpuServiceList := &dpuservicev1.DPUServiceList{}
	Expect(input.client.List(ctx, dpuServiceList,
		client.InNamespace(dpfOperatorSystemNamespace),
		client.HasLabels{dpuservicev1.ParentDPUDeploymentNameLabel})).To(Succeed())
	allArtifacts = append(allArtifacts, extractArtifacts(ToClientObjectSlice(dpuServiceList.Items))...)

	By("Capturing DPUServiceChain artifacts")
	dpuServiceChainList := &dpuservicev1.DPUServiceChainList{}
	Expect(input.client.List(ctx, dpuServiceChainList, client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())
	allArtifacts = append(allArtifacts, extractArtifacts(ToClientObjectSlice(dpuServiceChainList.Items))...)

	By("Capturing DPUSet artifacts")
	dpuSetList := &provisioningv1.DPUSetList{}
	Expect(input.client.List(ctx, dpuSetList, client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())
	allArtifacts = append(allArtifacts, extractArtifacts(ToClientObjectSlice(dpuSetList.Items))...)

	By("Capturing DPUServiceInterface artifacts")
	dpuServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
	Expect(input.client.List(ctx, dpuServiceInterfaceList, client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())
	allArtifacts = append(allArtifacts, extractArtifacts(ToClientObjectSlice(dpuServiceInterfaceList.Items))...)

	By("Capturing ServiceChain artifacts from DPU cluster")
	serviceChainList := &dpuservicev1.ServiceChainList{}
	Expect(dpuClusterClient[0].List(ctx, serviceChainList)).To(Succeed())
	allArtifacts = append(allArtifacts, extractArtifacts(ToClientObjectSlice(serviceChainList.Items))...)

	By("Capturing ServiceInterface artifacts from DPU cluster")
	serviceInterfaceList := &dpuservicev1.ServiceInterfaceList{}
	Expect(dpuClusterClient[0].List(ctx, serviceInterfaceList)).To(Succeed())
	allArtifacts = append(allArtifacts, extractArtifacts(ToClientObjectSlice(serviceInterfaceList.Items))...)

	By("Capturing Pod artifacts from DPU cluster with service label but not system component label")
	podList := &corev1.PodList{}
	hasServiceLabelReq, reqErr := labels.NewRequirement(dpuservicev1.DPFServiceIDLabelKey, selection.Exists, nil)
	Expect(reqErr).ToNot(HaveOccurred())
	notSystemComponentReq, reqErr := labels.NewRequirement(operatorv1.DPFComponentLabelKey, selection.DoesNotExist, nil)
	Expect(reqErr).ToNot(HaveOccurred())
	podSelector := labels.NewSelector().Add(*hasServiceLabelReq, *notSystemComponentReq)
	Expect(dpuClusterClient[0].List(ctx, podList, &client.MatchingLabelsSelector{Selector: podSelector})).To(Succeed())
	allArtifacts = append(allArtifacts, extractArtifacts(ToClientObjectSlice(podList.Items))...)

	artifactData, err := json.MarshalIndent(allArtifacts, "", "  ")
	Expect(err).ToNot(HaveOccurred())

	By("Writing artifacts to: " + filePath)
	Expect(os.WriteFile(filePath, artifactData, 0644)).To(Succeed())
}

func getArtifacts(filePath string) []map[string]interface{} {
	By("Reading artifacts from: " + filePath)
	data, err := os.ReadFile(filePath)
	Expect(err).ToNot(HaveOccurred())

	var artifacts []map[string]interface{}
	Expect(json.Unmarshal(data, &artifacts)).To(Succeed())
	return artifacts
}

// ToClientObjectSlice converts a slice of concrete Kubernetes objects to []client.Object
// T is the value type (e.g., DPU), but *T must implement client.Object
func ToClientObjectSlice[T any](in []T) []client.Object {
	out := make([]client.Object, len(in))
	for i := range in {
		out[i] = any(&in[i]).(client.Object)
	}
	return out
}

// extractArtifacts extracts the GVK, name, namespace, UID, generation, and spec of Kubernetes objects.
// All other fields (status, volatile metadata) are excluded.
// GVK is resolved via the scheme because List calls do not populate TypeMeta on
// individual items.
func extractArtifacts(objects []client.Object) []map[string]interface{} {
	artifacts := make([]map[string]interface{}, 0, len(objects))
	for _, obj := range objects {
		data, err := json.Marshal(obj)
		Expect(err).ToNot(HaveOccurred())
		var m map[string]interface{}
		Expect(json.Unmarshal(data, &m)).To(Succeed())
		// Resolve GVK from the scheme — TypeMeta is not set on items from List calls.
		gvks, _, err := scheme.Scheme.ObjectKinds(obj)
		Expect(err).ToNot(HaveOccurred())
		Expect(gvks).ToNot(BeEmpty())
		artifact := map[string]interface{}{
			"apiVersion": gvks[0].GroupVersion().String(),
			"kind":       gvks[0].Kind,
			"name":       obj.GetName(),
			"namespace":  obj.GetNamespace(),
			"uid":        string(obj.GetUID()),
			"generation": obj.GetGeneration(),
			"spec":       m["spec"],
		}
		artifacts = append(artifacts, artifact)
	}
	return artifacts
}

// verifySystemReady checks if the DPF system components are ready.
// This is not a complete list of all system pods, but it includes the most important ones.
func verifySystemReady() {
	VerifyClusterPods(ctx, dpuClusterClient[0], []string{
		// Kubernetes system pods
		"kube-flannel-ds", "coredns", "kube-proxy",
		// DPF system components
		"nvidia-k8s-ipam", "sfc-controller",
		// DPUDeployment pods
		"example",
	})

	perClusterNVIPAMControllerServiceName := getPerClusterDPUServiceName("nvidia-k8s-ipam", input.dpuClusters[0].Name, input.dpuClusters[0].Namespace)
	perClusterServiceChainSetControllerServiceName := getPerClusterDPUServiceName("servicechainset-controller", input.dpuClusters[0].Name, input.dpuClusters[0].Namespace)
	perClusterKubeStateMetricsServiceName := getPerClusterDPUServiceName("kube-state-metrics", input.dpuClusters[0].Name, input.dpuClusters[0].Namespace)

	dpuServiceNames := []string{
		operatorv1.FlannelName.String(),
		operatorv1.MultusName.String(),
		operatorv1.SRIOVDevicePluginName.String(),
		operatorv1.SFCControllerName.String(),
		operatorv1.ServiceChainSetCRDsName.String(),
		operatorv1.CNIInstallerName.String(),
		perClusterNVIPAMControllerServiceName, "nvidia-k8s-ipam-node",
		perClusterServiceChainSetControllerServiceName,
		perClusterKubeStateMetricsServiceName, "kube-state-metrics-rbac",
	}

	verifyDPUServicesReady(ctx, input, dpfOperatorSystemNamespace, dpuServiceNames)
}

func validateDPFVersionUpgrade() {
	Eventually(func(g Gomega) {
		dpfOperatorConfig := &operatorv1.DPFOperatorConfig{}
		g.Expect(input.client.Get(ctx, client.ObjectKey{
			Name:      configName,
			Namespace: dpfOperatorSystemNamespace,
		}, dpfOperatorConfig)).To(Succeed())
		g.Expect(dpfOperatorConfig.Status.Version).NotTo(BeNil(),
			"DPFOperatorConfig.Status.Version must be set before comparing")
		g.Expect(*dpfOperatorConfig.Status.Version).To(Equal(tag))

	}).WithTimeout(1*time.Minute).WithPolling(1*time.Second).Should(Succeed(),
		"DPF version should be upgraded to the specified tag")
}

func validateDPUClusterUpgrade(ctx context.Context, input ProvisionDPUClustersInput) {
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

			g.Expect(dpuCluster.Status.Version).To(Equal(util.KubernetesVersion),
				"DPUCluster %s should report the upgraded Kubernetes version", klog.KObj(dpuCluster))

			g.Expect(dpuCluster.Status.Phase).To(Equal(provisioningv1.PhaseReady),
				"DPUCluster %s phase should be Ready after upgrade", klog.KObj(dpuCluster))

			readyCondition := conditions.Get(dpuCluster, conditions.ConditionType(provisioningv1.ConditionReady))
			g.Expect(readyCondition).ToNot(BeNil(), "DPUCluster %s Ready condition should be set after upgrade", klog.KObj(dpuCluster))
			g.Expect(readyCondition.Status).To(Equal(metav1.ConditionTrue), "DPUCluster %s Ready condition should be True after upgrade", klog.KObj(dpuCluster))
		}
	}).WithTimeout(20*time.Minute).WithPolling(1*time.Second).Should(Succeed(),
		"DPUClusters should finish upgrading and report a fresh Ready condition with the expected Kubernetes version")
}

// rolloutDependencies simulates a post-upgrade dependency rollout by creating new BFB, DPUFlavor,
// DPUServiceTemplate, and DPUServiceConfiguration objects from the current manifests and updating
// one DPUDeployment to reference them in a single patch.
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
	selectedDPUDeployment := &dpuDeploymentList.Items[0]
	By(fmt.Sprintf("Selected DPUDeployment: %s", selectedDPUDeployment.GetName()))

	By("Updating selected DPUDeployment to reference current BFB, DPUFlavor, DPUServiceTemplate and DPUServiceConfiguration")
	original := selectedDPUDeployment.DeepCopy()
	selectedDPUDeployment.Spec.DPUs.BFB = ptr.To(input.bfb.Name)
	selectedDPUDeployment.Spec.DPUs.Flavor = input.dpuFlavor.Name
	// Replace only the "example" DPUService (not the "example-2")
	svc := selectedDPUDeployment.Spec.Services[input.dpuServiceTemplate.Name]
	svc.ServiceTemplate = currentTemplate.Name
	svc.ServiceConfiguration = currentConfig.Name
	selectedDPUDeployment.Spec.Services[input.dpuServiceTemplate.Name] = svc
	Expect(input.client.Patch(ctx, selectedDPUDeployment, client.MergeFrom(original))).To(Succeed())

	By("Waiting for all DPUDeployment Reconciled conditions to become True")
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
}

// patchDPFOperatorConfigForSpecDeploymentMode supports the breaking change that introduced
// DPFOperatorConfig.spec.deploymentMode as a required field. DPFUpgradeValidation does not
// re-apply the full spec from SetInput. When deploymentMode is still empty after upgrade from
// a GA install, patch it from the current run input. For later breaking spec additions, add
// patchDPFOperatorConfigForSpec<Name>.
func patchDPFOperatorConfigForSpecDeploymentMode(ctx context.Context, input *systemTestInput) {
	By("Patching DPFOperatorConfig for required spec.deploymentMode (breaking change vs pre-upgrade GA)")
	cfg := &operatorv1.DPFOperatorConfig{}
	Expect(input.client.Get(ctx, client.ObjectKey{
		Name:      configName,
		Namespace: dpfOperatorSystemNamespace,
	}, cfg)).To(Succeed())
	if cfg.Spec.DeploymentMode != "" {
		return
	}
	original := cfg.DeepCopy()
	cfg.Spec.DeploymentMode = input.config.Spec.DeploymentMode
	Expect(input.client.Patch(ctx, cfg, client.MergeFrom(original))).To(Succeed())
}
