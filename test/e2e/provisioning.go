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
	"fmt"
	"strings"
	"time"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	testutils "github.com/nvidia/doca-platform/test/utils"
	kamajiv1 "github.com/nvidia/doca-platform/third_party/api/kamaji/api/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// getProvisionInput builds ProvisionDPUClustersInput from systemTestInput
func getProvisionInput(input *systemTestInput) ProvisionDPUClustersInput {
	return ProvisionDPUClustersInput{
		numberOfNodesPerCluster: input.numberOfDPUNodes,
		dpuClusterPrerequisites: input.additionalProvisioningObjects,
		dpuClusters:             input.dpuClusters,
		dpuSet:                  input.dpuSet,
		bfb:                     input.bfb,
		dpuFlavor:               input.dpuFlavor,
		client:                  input.client,
		bfbImageURL:             input.bfbImageURL,
		restConfig:              input.restConfig,
	}
}

func BeforeProvisioning(ctx context.Context, input *systemTestInput) {
	By("Verifying DPU nodes are available for provisioning tests")
	Expect(input.hasDpuNodes()).To(BeTrue(),
		"SETUP ERROR: No DPU nodes found in cluster. "+
			"Provisioning tests require DPU nodes to be configured. "+
			"Please ensure DPU hardware is available and properly configured before running these tests.")

	By("Verifying no unexpected provisioning resources")
	provisioningResources := map[string]client.ObjectList{
		"DPUSet":     &provisioningv1.DPUSetList{},
		"DPU":        &provisioningv1.DPUList{},
		"BFB":        &provisioningv1.BFBList{},
		"DPUCluster": &provisioningv1.DPUClusterList{},
	}

	var dirty []string
	for name, list := range provisioningResources {
		if err := input.client.List(ctx, list); err != nil {
			continue
		}
		items, err := meta.ExtractList(list)
		if err != nil {
			continue
		}
		if len(items) > 0 {
			dirty = append(dirty, fmt.Sprintf("  • %s: %d instances", name, len(items)))
		}
	}

	if len(dirty) > 0 {
		Fail(fmt.Sprintf("Found unexpected provisioning resources:\n%s\n\n"+
			"These resources should have been cleaned up by the previous test's AfterAll.\n"+
			"Run cleanup manually or delete these resources before running tests again.",
			strings.Join(dirty, "\n")))
	}
}

func CreateProvisioningDPUCluster(ctx context.Context, input *systemTestInput) {
	By("Creating prerequisite objects for DPU cluster")
	provInput := getProvisionInput(input)

	for _, obj := range provInput.dpuClusterPrerequisites {
		// Deep copy to avoid mutating the shared original object
		objCopy := obj.DeepCopyObject().(client.Object)
		objCopy.SetLabels(testutils.AfterAllCleanupLabels)

		existing := objCopy.DeepCopyObject().(client.Object)
		err := input.client.Get(ctx, types.NamespacedName{
			Namespace: objCopy.GetNamespace(),
			Name:      objCopy.GetName(),
		}, existing)

		if apierrors.IsNotFound(err) {
			By(fmt.Sprintf("Creating prerequisite %s %s/%s",
				objCopy.GetObjectKind().GroupVersionKind().Kind,
				objCopy.GetNamespace(),
				objCopy.GetName()))
			Expect(input.client.Create(ctx, objCopy)).To(Succeed())
		} else {
			By(fmt.Sprintf("Prerequisite %s %s/%s already exists",
				objCopy.GetObjectKind().GroupVersionKind().Kind,
				objCopy.GetNamespace(),
				objCopy.GetName()))
			Expect(err).To(Succeed(), "Failed to check existing prerequisite")
		}
	}

	By("Creating DPUCluster object")
	// Deep copy to avoid mutating the shared original object
	dpuCluster := provInput.dpuClusters[0].DeepCopy()
	dpuCluster.SetLabels(testutils.AfterAllCleanupLabels)

	By(fmt.Sprintf("Creating DPUCluster %s/%s",
		dpuCluster.GetNamespace(),
		dpuCluster.GetName()))
	Expect(client.IgnoreAlreadyExists(input.client.Create(ctx, dpuCluster))).To(Succeed())

	By("Verifying DPUCluster exists")
	Eventually(func(g Gomega) {
		clusters := &provisioningv1.DPUClusterList{}
		g.Expect(input.client.List(ctx, clusters)).To(Succeed())
		g.Expect(clusters.Items).To(HaveLen(1), "Expected exactly one DPU cluster")
	}).WithTimeout(1 * time.Minute).Should(Succeed())

	By("Waiting for DPU cluster to reach Ready phase")
	Eventually(func(g Gomega) {
		clusters := &provisioningv1.DPUClusterList{}
		g.Expect(input.client.List(ctx, clusters)).To(Succeed())
		g.Expect(clusters.Items).To(HaveLen(1), "Expected exactly one DPU cluster")

		cluster := clusters.Items[0]
		By(fmt.Sprintf("DPU cluster %s Phase: %s", cluster.Name, cluster.Status.Phase))
		g.Expect(cluster.Status.Phase).To(Equal(provisioningv1.PhaseReady),
			"DPU cluster should reach Ready phase")
	}).WithTimeout(5 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())

	By("Creating DPU cluster client connection")
	getDPUClusterClients(ctx, getProvisionInput(input))

	By("Creating BFB object")
	provInput = getProvisionInput(input)

	// Override BFB URL if environment variable is set
	if provInput.bfbImageURL != "" {
		By(fmt.Sprintf("Overriding BFB URL with: %s", provInput.bfbImageURL))
		provInput.bfb.Spec.URL = provInput.bfbImageURL
	}

	Eventually(func(g Gomega) {
		bfb := provInput.bfb.DeepCopy()
		bfb.SetLabels(testutils.AfterAllCleanupLabels)
		By(fmt.Sprintf("Creating BFB %s/%s", bfb.GetNamespace(), bfb.GetName()))
		g.Expect(client.IgnoreAlreadyExists(input.client.Create(ctx, bfb))).To(Succeed())
	}).WithTimeout(10 * time.Second).Should(Succeed())

	By("Verifying BFB object exists")
	Eventually(func(g Gomega) {
		bfb := &provisioningv1.BFB{}
		g.Expect(input.client.Get(ctx, types.NamespacedName{
			Name:      provInput.bfb.Name,
			Namespace: provInput.bfb.Namespace,
		}, bfb)).To(Succeed(), "BFB should be created")
	}).WithTimeout(1 * time.Minute).Should(Succeed())

	By("Waiting for BFB to reach Ready phase")
	Eventually(func(g Gomega) {
		bfb := &provisioningv1.BFB{}
		g.Expect(input.client.Get(ctx, types.NamespacedName{
			Name:      provInput.bfb.Name,
			Namespace: provInput.bfb.Namespace,
		}, bfb)).To(Succeed())
		By(fmt.Sprintf("BFB %s Phase: %s", bfb.Name, bfb.Status.Phase))
		g.Expect(bfb.Status.Phase).To(Equal(provisioningv1.BFBReady),
			"BFB should reach Ready phase")
	}).WithTimeout(10 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())
}

func CreateProvisioningDPUSet(ctx context.Context, input *systemTestInput) {
	provInput := getProvisionInput(input)

	if provInput.dpuFlavor != nil {
		By("Creating DPUFlavor object (prerequisite for DPUSet)")
		Eventually(func(g Gomega) {
			dpuFlavor := provInput.dpuFlavor.DeepCopy()
			dpuFlavor.SetLabels(testutils.AfterAllCleanupLabels)
			By(fmt.Sprintf("Creating DPUFlavor %s/%s", dpuFlavor.GetNamespace(), dpuFlavor.GetName()))
			g.Expect(client.IgnoreAlreadyExists(input.client.Create(ctx, dpuFlavor))).To(Succeed())
		}).WithTimeout(60 * time.Second).Should(Succeed())

		By("Verifying DPUFlavor exists")
		Eventually(func(g Gomega) {
			dpuFlavor := &provisioningv1.DPUFlavor{}
			g.Expect(input.client.Get(ctx, types.NamespacedName{
				Name:      provInput.dpuFlavor.Name,
				Namespace: provInput.dpuFlavor.Namespace,
			}, dpuFlavor)).To(Succeed(), "DPUFlavor should be created")
		}).WithTimeout(1 * time.Minute).Should(Succeed())
	}

	By("Creating DPUSet")
	Eventually(func(g Gomega) {
		dpuset := provInput.dpuSet.DeepCopy()
		dpuset.SetLabels(testutils.AfterAllCleanupLabels)
		By(fmt.Sprintf("Creating DPUSet %s/%s", dpuset.GetNamespace(), dpuset.GetName()))
		g.Expect(client.IgnoreAlreadyExists(input.client.Create(ctx, dpuset))).To(Succeed())
	}).WithTimeout(60 * time.Second).Should(Succeed())

	By("Verifying DPUSet exists")
	Eventually(func(g Gomega) {
		dpusets := &provisioningv1.DPUSetList{}
		g.Expect(input.client.List(ctx, dpusets)).To(Succeed())
		g.Expect(dpusets.Items).To(HaveLen(1), "Expected exactly one DPUSet")
	}).WithTimeout(2 * time.Minute).Should(Succeed())

	By("Waiting for DPUSet controller to create DPU objects")
	Eventually(func(g Gomega) {
		dpus := &provisioningv1.DPUList{}
		g.Expect(input.client.List(ctx, dpus)).To(Succeed())
		g.Expect(dpus.Items).To(HaveLen(provInput.numberOfNodesPerCluster),
			fmt.Sprintf("Expected %d DPU objects", provInput.numberOfNodesPerCluster))

		for _, dpu := range dpus.Items {
			By(fmt.Sprintf("DPU %s created with Phase: %s", dpu.Name, dpu.Status.Phase))
			g.Expect(dpu.Spec.BFB).NotTo(BeEmpty(), "DPU should reference BFB")
			g.Expect(dpu.Spec.Cluster.Name).NotTo(BeEmpty(), "DPU should reference DPUCluster")
		}
	}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())

	By("Waiting for DPU nodes to join the DPU cluster as K8s Nodes")
	Eventually(func(g Gomega) {
		nodes := &corev1.NodeList{}
		g.Expect(dpuClusterClient[0].List(ctx, nodes)).To(Succeed(), "Should be able to list nodes in DPU cluster")

		g.Expect(nodes.Items).To(HaveLen(provInput.numberOfNodesPerCluster),
			fmt.Sprintf("DPU cluster should have %d K8s nodes", provInput.numberOfNodesPerCluster))
	}).WithTimeout(45 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())

	By("Waiting for all DPU objects to reach Ready phase")
	Eventually(func(g Gomega) {
		dpus := &provisioningv1.DPUList{}
		g.Expect(input.client.List(ctx, dpus)).To(Succeed())
		g.Expect(dpus.Items).To(HaveLen(provInput.numberOfNodesPerCluster))

		readyCount := 0
		for _, dpu := range dpus.Items {
			By(fmt.Sprintf("DPU %s Phase: %s", dpu.Name, dpu.Status.Phase))
			if dpu.Status.Phase == provisioningv1.DPUReady {
				readyCount++
			}
		}

		g.Expect(readyCount).To(Equal(provInput.numberOfNodesPerCluster),
			fmt.Sprintf("All %d DPUs should reach Ready phase", provInput.numberOfNodesPerCluster))
	}).WithTimeout(30 * time.Minute).WithPolling(30 * time.Second).Should(Succeed())
}

func VerifyProvisioning(ctx context.Context, input *systemTestInput) {
	provInput := getProvisionInput(input)
	deploymentName := fmt.Sprintf("in-cluster-%s", getServiceChainSetControllerDPUServiceName(provInput.dpuClusters[0].Name, provInput.dpuClusters[0].Namespace))
	By(fmt.Sprintf("Checking %s deployment", deploymentName))
	Eventually(func(g Gomega) {
		serviceSetDeployment := &appsv1.Deployment{}
		g.Expect(input.client.Get(ctx, client.ObjectKey{
			Namespace: dpfOperatorSystemNamespace,
			Name:      deploymentName,
		}, serviceSetDeployment)).To(Succeed())

		By(fmt.Sprintf("%s: Ready=%d/%d",
			deploymentName,
			serviceSetDeployment.Status.ReadyReplicas,
			*serviceSetDeployment.Spec.Replicas))
		g.Expect(serviceSetDeployment.Status.ReadyReplicas).To(Equal(*serviceSetDeployment.Spec.Replicas),
			fmt.Sprintf("%s should be ready", deploymentName))
	}).WithTimeout(10 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())

	By("Checking that DPUService objects have been deployed to DPUCluster")
	Eventually(func(g Gomega) {
		// Check Deployments with argocd instance label
		deployments := &appsv1.DeploymentList{}
		g.Expect(dpuClusterClient[0].List(ctx, deployments)).To(Succeed())

		found := map[string]bool{}
		for i := range deployments.Items {
			trackingIDAnnotation := deployments.Items[i].GetAnnotations()[argoCDTrackingIDAnnotation]
			By(fmt.Sprintf("Found Deployment with argocd tracking id: %s", trackingIDAnnotation))
			g.Expect(trackingIDAnnotation).NotTo(BeEmpty())
			found[trackingIDAnnotation] = true
		}

		// Check DaemonSets with argocd instance label
		provInput := getProvisionInput(input)
		daemonsets := &appsv1.DaemonSetList{}
		g.Expect(dpuClusterClient[0].List(ctx, daemonsets,
			client.InNamespace(provInput.dpuClusters[0].GetNamespace()))).To(Succeed())

		for i := range daemonsets.Items {
			trackingIDAnnotation := daemonsets.Items[i].GetLabels()[argoCDTrackingIDAnnotation]
			By(fmt.Sprintf("Found DaemonSet with argocd tracking id: %s", trackingIDAnnotation))
			g.Expect(trackingIDAnnotation).NotTo(BeEmpty())
			found[trackingIDAnnotation] = true
		}

		// Verify expected DPUServices are deployed
		By("Verifying expected DPUServices: Multus, Flannel, SRIOV, NVIPAM, OVS-CNI, SFC-Controller")
		g.Expect(found).To(HaveKey(ContainSubstring(operatorv1.MultusName.String())), "Multus should be deployed")
		g.Expect(found).To(HaveKey(ContainSubstring(operatorv1.FlannelName.String())), "Flannel should be deployed")
		g.Expect(found).To(HaveKey(ContainSubstring(operatorv1.SRIOVDevicePluginName.String())), "SRIOV should be deployed")
		g.Expect(found).To(HaveKey(ContainSubstring(operatorv1.NVIPAMName.String())), "NVIPAM should be deployed")
		g.Expect(found).To(HaveKey(ContainSubstring(operatorv1.OVSCNIName.String())), "OVS-CNI should be deployed")
		g.Expect(found).To(HaveKey(ContainSubstring(operatorv1.SFCControllerName.String())), "SFC-Controller should be deployed")
	}).WithTimeout(10 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())

	By("Checking DPUSet status has accurate DPU statistics")

	Eventually(func(g Gomega) {
		dpusets := &provisioningv1.DPUSetList{}
		g.Expect(input.client.List(ctx, dpusets)).To(Succeed())
		g.Expect(dpusets.Items).To(HaveLen(1))

		dpuset := dpusets.Items[0]
		totalDPUs := 0
		readyDPUs := 0

		for phase, count := range dpuset.Status.DPUStatistics {
			totalDPUs += count
			By(fmt.Sprintf("DPUSet %s - Phase %s: %d DPUs", dpuset.Name, phase, count))
			if phase == provisioningv1.DPUReady {
				readyDPUs = count
			}
		}

		g.Expect(totalDPUs).To(Equal(provInput.numberOfNodesPerCluster),
			"DPUSet should track all DPUs")
		g.Expect(readyDPUs).To(Equal(provInput.numberOfNodesPerCluster),
			"All DPUs should be in Ready phase")
	}).WithTimeout(2 * time.Minute).Should(Succeed())

	By("Waiting for all system pods to be ready in DPU cluster")
	VerifyClusterPods(ctx, dpuClusterClient[0], systemPodsToVerify)
}

func VerifyKamajiControlPlaneExtraArgs(ctx context.Context, input *systemTestInput) {
	provInput := getProvisionInput(input)

	By("Getting Kamaji TenantControlPlane resource")
	tcp := &kamajiv1.TenantControlPlane{}
	Eventually(func(g Gomega) {
		err := input.client.Get(ctx, types.NamespacedName{
			Name:      provInput.dpuClusters[0].Name,
			Namespace: provInput.dpuClusters[0].Namespace,
		}, tcp)
		g.Expect(err).NotTo(HaveOccurred(), "Should get TenantControlPlane resource")
	}).WithTimeout(2 * time.Minute).Should(Succeed())

	By("Verifying TenantControlPlane has ExtraArgs configured")
	Expect(tcp.Spec.ControlPlane.Deployment.ExtraArgs).NotTo(BeNil(), "ExtraArgs should be configured")

	By("Verifying kube-apiserver ExtraArgs")
	apiServerArgs := tcp.Spec.ControlPlane.Deployment.ExtraArgs.APIServer
	Expect(apiServerArgs).NotTo(BeEmpty(), "APIServer ExtraArgs should not be empty")

	By("Verifying audit log parameters for kube-apiserver")
	Expect(apiServerArgs).To(ContainElement("--audit-log-path=/var/log/kubernetes/audit.log"), "Should have audit-log-path parameter")
	Expect(apiServerArgs).To(ContainElement("--audit-policy-file=/etc/kubernetes/audit-policy.yaml"), "Should have audit-policy-file parameter")
	Expect(apiServerArgs).To(ContainElement("--audit-log-maxage=30"), "Should have audit-log-maxage parameter")
	Expect(apiServerArgs).To(ContainElement("--audit-log-maxbackup=10"), "Should have audit-log-maxbackup parameter")
	Expect(apiServerArgs).To(ContainElement("--audit-log-maxsize=100"), "Should have audit-log-maxsize parameter")

	By("Verifying security parameters for kube-apiserver")
	Expect(apiServerArgs).To(ContainElement("--anonymous-auth=true"), "Should have anonymous-auth=true parameter")
	Expect(apiServerArgs).To(ContainElement("--profiling=false"), "Should have profiling=false parameter")

	By("Verifying TLS cipher suites parameter for kube-apiserver")
	Expect(apiServerArgs).To(ContainElement("--tls-cipher-suites=TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384"), "Should have tls-cipher-suites parameter")

	By("Verifying request timeout parameter for kube-apiserver")
	Expect(apiServerArgs).To(ContainElement("--request-timeout=120s"), "Should have request-timeout parameter")

	By("Verifying admission plugins parameter for kube-apiserver")
	Expect(apiServerArgs).To(ContainElement("--enable-admission-plugins=NamespaceLifecycle,LimitRanger,ServiceAccount,AlwaysPullImages,MutatingAdmissionWebhook,ValidatingAdmissionWebhook,ResourceQuota,PodSecurity,PodNodeSelector,NodeRestriction,EventRateLimit"), "Should have enable-admission-plugins parameter")

	By("Verifying kube-controller-manager ExtraArgs")
	controllerManagerArgs := tcp.Spec.ControlPlane.Deployment.ExtraArgs.ControllerManager
	Expect(controllerManagerArgs).NotTo(BeEmpty(), "ControllerManager ExtraArgs should not be empty")
	Expect(controllerManagerArgs).To(ContainElement("--profiling=false"), "Should have profiling=false parameter")

	By("Verifying kube-scheduler ExtraArgs")
	schedulerArgs := tcp.Spec.ControlPlane.Deployment.ExtraArgs.Scheduler
	Expect(schedulerArgs).NotTo(BeEmpty(), "Scheduler ExtraArgs should not be empty")
	Expect(schedulerArgs).To(ContainElement("--profiling=false"), "Should have profiling=false parameter")
}

func DeleteProvisioning(ctx context.Context, input *systemTestInput) {
	provInput := getProvisionInput(input)

	By("Deleting DPUSet resource")
	Eventually(func(g Gomega) {
		dpuset := &provisioningv1.DPUSet{}
		err := input.client.Get(ctx, types.NamespacedName{
			Name:      provInput.dpuSet.Name,
			Namespace: provInput.dpuSet.Namespace,
		}, dpuset)

		if apierrors.IsNotFound(err) {
			By("DPUSet already deleted")
			return
		}
		g.Expect(err).To(Succeed())
		g.Expect(input.client.Delete(ctx, dpuset)).To(Succeed())
	}).WithTimeout(1 * time.Minute).Should(Succeed())

	By("Waiting for DPUSet to be deleted")
	Eventually(func(g Gomega) {
		dpusets := &provisioningv1.DPUSetList{}
		g.Expect(input.client.List(ctx, dpusets)).To(Succeed())
		g.Expect(dpusets.Items).To(BeEmpty(), "DPUSet should be deleted")
	}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())

	By("Waiting for DPU objects to be removed")
	Eventually(func(g Gomega) {
		dpus := &provisioningv1.DPUList{}
		g.Expect(input.client.List(ctx, dpus)).To(Succeed())
		g.Expect(dpus.Items).To(BeEmpty(),
			"All DPU objects should be cleaned up after DPUSet deletion")
	}).WithTimeout(10 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())

	By("Verifying K8s nodes are removed from DPU cluster")
	Eventually(func(g Gomega) {
		nodes := &corev1.NodeList{}
		g.Expect(dpuClusterClient[0].List(ctx, nodes)).To(Succeed())
		g.Expect(nodes.Items).To(BeEmpty(),
			"DPU cluster should have no nodes after deprovisioning")
	}).WithTimeout(10 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())

	if provInput.dpuFlavor != nil {
		By("Deleting DPUFlavor resource")
		Eventually(func(g Gomega) {
			dpuFlavor := &provisioningv1.DPUFlavor{}
			err := input.client.Get(ctx, types.NamespacedName{
				Name:      provInput.dpuFlavor.Name,
				Namespace: provInput.dpuFlavor.Namespace,
			}, dpuFlavor)

			if apierrors.IsNotFound(err) {
				By("DPUFlavor already deleted")
				return
			}
			g.Expect(err).To(Succeed())
			g.Expect(input.client.Delete(ctx, dpuFlavor)).To(Succeed())
		}).WithTimeout(1 * time.Minute).Should(Succeed())

		By("Waiting for DPUFlavor to be deleted")
		Eventually(func(g Gomega) {
			dpuFlavor := &provisioningv1.DPUFlavor{}
			err := input.client.Get(ctx, types.NamespacedName{
				Name:      provInput.dpuFlavor.Name,
				Namespace: provInput.dpuFlavor.Namespace,
			}, dpuFlavor)
			g.Expect(apierrors.IsNotFound(err)).To(BeTrue(), "DPUFlavor should be deleted")
		}).WithTimeout(2 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())
	}

	By("Deleting BFB resource")
	Eventually(func(g Gomega) {
		bfb := &provisioningv1.BFB{}
		err := input.client.Get(ctx, types.NamespacedName{
			Name:      provInput.bfb.Name,
			Namespace: provInput.bfb.Namespace,
		}, bfb)

		if apierrors.IsNotFound(err) {
			By("BFB already deleted")
			return
		}
		g.Expect(err).To(Succeed())
		g.Expect(input.client.Delete(ctx, bfb)).To(Succeed())
	}).WithTimeout(1 * time.Minute).Should(Succeed())

	By("Waiting for BFB to be deleted")
	Eventually(func(g Gomega) {
		bfb := &provisioningv1.BFB{}
		err := input.client.Get(ctx, types.NamespacedName{
			Name:      provInput.bfb.Name,
			Namespace: provInput.bfb.Namespace,
		}, bfb)
		g.Expect(apierrors.IsNotFound(err)).To(BeTrue(), "BFB should be deleted")
	}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())

	By("Deleting DPUCluster resource")
	Eventually(func(g Gomega) {
		cluster := &provisioningv1.DPUCluster{}
		err := input.client.Get(ctx, types.NamespacedName{
			Name:      provInput.dpuClusters[0].Name,
			Namespace: provInput.dpuClusters[0].Namespace,
		}, cluster)

		if apierrors.IsNotFound(err) {
			By("DPUCluster already deleted")
			return
		}
		g.Expect(err).To(Succeed())
		g.Expect(input.client.Delete(ctx, cluster)).To(Succeed())
	}).WithTimeout(1 * time.Minute).Should(Succeed())

	By("Waiting for DPUCluster to be deleted")
	Eventually(func(g Gomega) {
		clusters := &provisioningv1.DPUClusterList{}
		g.Expect(input.client.List(ctx, clusters)).To(Succeed())
		g.Expect(clusters.Items).To(BeEmpty(), "DPUCluster should be deleted")
	}).WithTimeout(15 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())

	By("Deleting prerequisite objects")
	for _, obj := range provInput.dpuClusterPrerequisites {
		Eventually(func(g Gomega) {
			existing := obj.DeepCopyObject().(client.Object)
			err := input.client.Get(ctx, types.NamespacedName{
				Namespace: obj.GetNamespace(),
				Name:      obj.GetName(),
			}, existing)

			if apierrors.IsNotFound(err) {
				return
			}
			g.Expect(err).To(Succeed())
			g.Expect(input.client.Delete(ctx, existing)).To(Succeed())
		}).WithTimeout(1 * time.Minute).Should(Succeed())
	}

	By("Waiting for prerequisite objects to be deleted")
	Eventually(func(g Gomega) {
		for _, obj := range provInput.dpuClusterPrerequisites {
			existing := obj.DeepCopyObject().(client.Object)
			err := input.client.Get(ctx, types.NamespacedName{
				Namespace: obj.GetNamespace(),
				Name:      obj.GetName(),
			}, existing)
			g.Expect(apierrors.IsNotFound(err)).To(BeTrue(),
				fmt.Sprintf("Prerequisite %s/%s should be deleted",
					obj.GetNamespace(), obj.GetName()))
		}
	}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())

	By("Verifying no DPU-related resources remain")
	Eventually(func(g Gomega) {
		By("Checking DPUSets are cleaned up")
		dpusets := &provisioningv1.DPUSetList{}
		g.Expect(input.client.List(ctx, dpusets)).To(Succeed())
		g.Expect(dpusets.Items).To(BeEmpty(), "No DPUSets should remain")

		By("Checking DPU objects are cleaned up")
		dpus := &provisioningv1.DPUList{}
		g.Expect(input.client.List(ctx, dpus)).To(Succeed())
		g.Expect(dpus.Items).To(BeEmpty(), "No DPU objects should remain")

		By("Checking BFBs are cleaned up")
		bfbs := &provisioningv1.BFBList{}
		g.Expect(input.client.List(ctx, bfbs)).To(Succeed())
		g.Expect(bfbs.Items).To(BeEmpty(), "No BFBs should remain")

		By("Checking DPUClusters are cleaned up")
		clusters := &provisioningv1.DPUClusterList{}
		g.Expect(input.client.List(ctx, clusters)).To(Succeed())
		g.Expect(clusters.Items).To(BeEmpty(), "No DPUClusters should remain")

		if provInput.dpuFlavor != nil {
			By("Checking DPUFlavors are cleaned up")
			flavors := &provisioningv1.DPUFlavorList{}
			g.Expect(input.client.List(ctx, flavors, client.InNamespace(provInput.dpuFlavor.Namespace))).To(Succeed())
			for _, flavor := range flavors.Items {
				g.Expect(flavor.Name).NotTo(Equal(provInput.dpuFlavor.Name),
					"DPUFlavor should be deleted")
			}
		}
	}).WithTimeout(2 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())

	By("Deprovisioning completed successfully")
}
