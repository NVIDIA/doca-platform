/*
Copyright 2024 NVIDIA

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

package controllers

import (
	"context"
	"reflect"
	"testing"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	operatorcontroller "github.com/nvidia/doca-platform/internal/operator/controllers"
	"github.com/nvidia/doca-platform/pkg/conditions"
	"github.com/nvidia/doca-platform/pkg/dpucluster"
	testutils "github.com/nvidia/doca-platform/test/utils"
	argov1 "github.com/nvidia/doca-platform/third_party/forked/argoproj/argo-cd/pkg/apis/application/v1alpha1"

	"github.com/fluxcd/pkg/runtime/patch"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/json"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

var _ = Describe("DPUService Controller", func() {
	Context("When reconciling a resource", func() {
		var (
			testNS              *corev1.Namespace
			testConfig          *operatorv1.DPFOperatorConfig
			dpuServiceInterface *dpuservicev1.DPUServiceInterface
			testDPU1NS          *corev1.Namespace
			testDPU2NS          *corev1.Namespace
			testDPU3NS          *corev1.Namespace
			cleanupObjs         []client.Object
		)
		BeforeEach(func() {
			By("creating the namespaces")
			testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "testns-"}}
			testDPU1NS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "dpu-dsr-"}}
			testDPU2NS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "dpu-dsr-"}}
			testDPU3NS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "dpu-dsr-"}}
			Expect(testClient.Create(ctx, testNS)).To(Succeed())
			Expect(testClient.Create(ctx, testDPU1NS)).To(Succeed())
			Expect(testClient.Create(ctx, testDPU2NS)).To(Succeed())
			Expect(testClient.Create(ctx, testDPU3NS)).To(Succeed())
			// Create the DPF System Namespace
			err := testClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace}})
			if !apierrors.IsAlreadyExists(err) {
				Expect(err).ToNot(HaveOccurred())
			}
			// Apply and get the DPFOperatorConfig. There is a race condition between the separate test runs why we have to fetch the config.
			// A real config is necessary to run our reconcileArgoSecrets tests.
			if testConfig == nil {
				testConfig = getMinimalDPFOperatorConfig()
				Expect(client.IgnoreAlreadyExists(testClient.Create(ctx, testConfig))).To(Succeed())
			}
			Expect(testClient.Get(ctx, client.ObjectKeyFromObject(testConfig), testConfig)).To(Succeed())

			dpfOperatorConfig := getMinimalDPFOperatorConfig()
			Expect(
				// this namespace can be created multiple times.
				client.IgnoreAlreadyExists(
					testClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: dpfOperatorConfig.GetNamespace()}})),
			).To(Succeed())

			dpuServiceInterface = getMinimalDPUServiceInterface(testNS.Name)
			Expect(client.IgnoreAlreadyExists(testClient.Create(ctx, dpuServiceInterface))).To(Succeed())
			cleanupObjs = append(cleanupObjs, dpuServiceInterface)
		})
		AfterEach(func() {
			By("Cleanup the Namespace and Secrets")
			Expect(testutils.CleanupAndWait(ctx, testClient, cleanupObjs...)).To(Succeed())
			Expect(testClient.Delete(ctx, testNS)).To(Succeed())
			Expect(testClient.Delete(ctx, testDPU1NS)).To(Succeed())
			Expect(testClient.Delete(ctx, testDPU2NS)).To(Succeed())
			Expect(testClient.Delete(ctx, testDPU3NS)).To(Succeed())
		})

		It("should successfully reconcile the DPUService", func() {
			clusters := []provisioningv1.DPUCluster{
				testutils.GetTestDPUCluster(testDPU1NS.Name, "cluster-one"),
			}
			cleanupObjs = createDPUClusters(clusters, cleanupObjs)

			By("Get DPUCluster client")
			dpuClusterConfigs, err := dpucluster.GetConfigs(ctx, testClient)
			Expect(err).ToNot(HaveOccurred())
			dpuClusterClient, err := dpuClusterConfigs[0].Client(ctx)
			Expect(err).ToNot(HaveOccurred())

			By("Create fake Node in DPUCluster")
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "dpu-node-1",
					Labels: map[string]string{
						// key is the nodeSelector used in the DPUService.
						"key":                                "dpu-node-1",
						provisioningv1.DPUNodeNameLabel:      "node-1",
						provisioningv1.DPUNodeNamespaceLabel: "test-namespace",
					},
				},
			}
			Expect(dpuClusterClient.Create(ctx, node)).To(Succeed())
			cleanupObjs = append(cleanupObjs, node)

			By("Create fake Node in DPUCluster with non-matching label")
			nonMatchingNode := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "dpu-node-2",
					Labels: map[string]string{
						"non-matching":                       "random",
						provisioningv1.DPUNodeNameLabel:      "node-2",
						provisioningv1.DPUNodeNamespaceLabel: "test-namespace",
					},
				},
			}
			Expect(dpuClusterClient.Create(ctx, nonMatchingNode)).To(Succeed())
			cleanupObjs = append(cleanupObjs, nonMatchingNode)

			By("Add addresses to the fake Node")
			patcher := patch.NewSerialPatcher(node, dpuClusterClient)
			node.Status.Addresses = []corev1.NodeAddress{
				{
					Type:    corev1.NodeInternalIP,
					Address: "192.168.1.10",
				},
				{
					Type:    corev1.NodeHostName,
					Address: "dpu-node-1",
				},
			}
			Expect(patcher.Patch(ctx, node, patch.WithFieldOwner("dpu-service-test"))).To(Succeed())
			nonMatchingNode.Status.Addresses = []corev1.NodeAddress{
				{
					Type:    corev1.NodeInternalIP,
					Address: "192.168.1.11",
				},
				{
					Type:    corev1.NodeHostName,
					Address: "dpu-node-2",
				},
			}
			Expect(patcher.Patch(ctx, nonMatchingNode, patch.WithFieldOwner("dpu-service-test"))).To(Succeed())

			expectedEndpointSliceEndpoint := []discoveryv1.Endpoint{{
				Addresses:  []string{"192.168.1.10"},
				NodeName:   ptr.To("node-1"),
				Conditions: discoveryv1.EndpointConditions{Ready: ptr.To(true)},
			}}

			dpuServices := getMinimalDPUServices(testNS.Name)
			// A DPUService that should be deployed to the same cluster the DPF system is deployed in.
			var deployInCluster = true
			hostDPUService := &dpuservicev1.DPUService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "host-dpu-service",
					Namespace: testNS.Name},
				Spec: dpuservicev1.DPUServiceSpec{
					DeployInCluster: &deployInCluster,
					HelmChart: dpuservicev1.HelmChart{
						Source: dpuservicev1.ApplicationSource{
							RepoURL:     "oci://repository.com",
							Version:     "v1.1",
							Chart:       "first-chart",
							ReleaseName: "release-one",
						},
					},
				},
			}

			By("create dpuservices and check the correct secrets, appproject and applications are created")
			// Create DPUServices which are reconciled to the DPU clusters.
			for i := range dpuServices {
				Expect(testClient.Create(ctx, dpuServices[i])).To(Succeed())
				cleanupObjs = append(cleanupObjs, dpuServices[i])
			}

			// Expect a hostDPUService to be reconciled to the host cluster.
			Expect(testClient.Create(ctx, hostDPUService)).To(Succeed())
			cleanupObjs = append(cleanupObjs, hostDPUService)

			Eventually(func(g Gomega) {
				assertDPUService(g, testClient, dpuServices)
			}).WithTimeout(30 * time.Second).Should(BeNil())

			// Check that argo secrets have been created correctly.
			Eventually(func(g Gomega) {
				assertArgoCDSecrets(g, testClient, clusters, &cleanupObjs, []string{testDPU1NS.Name, testDPU2NS.Name, testDPU3NS.Name})
			}).WithTimeout(30 * time.Second).Should(BeNil())

			// Check that the argo AppProject has been created correctly
			Eventually(func(g Gomega) {
				assertAppProject(g, testClient, clusters)
			}).WithTimeout(30 * time.Second).Should(BeNil())

			// Check that the argo Application has been created correctly
			Eventually(func(g Gomega) {
				assertApplication(g, testClient, testNS.Name, dpuServices, []*dpuservicev1.DPUServiceInterface{dpuServiceInterface}, clusters)
			}).WithTimeout(30 * time.Second).Should(BeNil())

			Eventually(func(g Gomega) {
				assertDPUServiceCondition(g, testClient, dpuServices)
			}).WithTimeout(30 * time.Second).Should(BeNil())

			Eventually(func(g Gomega) {
				assertDPUServiceConfigPorts(g, testClient, dpuServices, nil, nil, "")
			}).WithTimeout(30 * time.Second).Should(BeNil())

			By("update configPorts should expose the DPUService API by creating a Service type NodePort and set the status")
			configPorts := &dpuservicev1.ConfigPorts{
				ServiceType: corev1.ServiceTypeNodePort,
				Ports: []dpuservicev1.ConfigPort{
					{
						Name:     "port-one",
						Port:     8080,
						Protocol: corev1.ProtocolTCP,
					},
				},
			}
			patcher = patch.NewSerialPatcher(dpuServices[0], testClient)
			dpuServices[0].Spec.ConfigPorts = configPorts
			Expect(patcher.Patch(ctx, dpuServices[0], patch.WithFieldOwner(dpuServiceControllerName))).To(Succeed())

			// Create Service inside DPUCluster to test the NodePort creation.
			// This is necessary to populate the Service and EndpointSlice on the host cluster.
			By("create the Service inside the DPUCluster")
			labels := dpuServices[0].MatchLabels()
			svc := getExposedService(labels)
			Expect(dpuClusterClient.Create(ctx, svc)).To(Succeed())

			By("check that the ConfigPorts are reconciled with 1 matching corev1.Node")
			Eventually(func(g Gomega) {
				assertDPUServiceConfigPorts(g, testClient, []*dpuservicev1.DPUService{dpuServices[0]}, configPorts, expectedEndpointSliceEndpoint, dpuClusterConfigs[0].Cluster.Name)
			}).WithTimeout(30 * time.Second).Should(BeNil())

			By("check that all Endpoints are added to ConfigPorts after Node has correct labels")
			nodePatcher := patch.NewSerialPatcher(nonMatchingNode, dpuClusterClient)
			nonMatchingNode.ObjectMeta.Labels["key"] = "dpu-node-2"
			Expect(nodePatcher.Patch(ctx, nonMatchingNode, patch.WithFieldOwner("dpu-service-test"))).To(Succeed())

			// We have to delete the created service to be able to test an update.
			// In evntest we are using the same cluster for both management and DPU cluster.
			// If we don't delete the created service we will have a duplicated service with the same label selector.
			By("delete exposed service on management cluster")
			Expect(testClient.Delete(ctx, &corev1.Service{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "test-service"}})).To(Succeed())

			expectedEndpointSliceEndpoint = append(expectedEndpointSliceEndpoint, discoveryv1.Endpoint{
				Addresses:  []string{"192.168.1.11"},
				NodeName:   ptr.To("node-2"),
				Conditions: discoveryv1.EndpointConditions{Ready: ptr.To(true)},
			})
			Eventually(func(g Gomega) {
				assertConfigPortEndpointSlice(g, testClient, dpuServices[0], expectedEndpointSliceEndpoint, dpuClusterConfigs[0].Cluster.Name)
			}).WithTimeout(30 * time.Second).Should(BeNil())

			By("update configPorts should fail if the ServiceType is changed")
			dpuServices[0].Spec.ConfigPorts.ServiceType = corev1.ClusterIPNone
			err = patcher.Patch(ctx, dpuServices[0], patch.WithFieldOwner(dpuServiceControllerName))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(`Value is immutable`))

			By("update configPorts should succeed if we remove the config ports")
			dpuServices[0].Spec.ConfigPorts = nil
			Expect(patcher.Patch(ctx, dpuServices[0], patch.WithFieldOwner(dpuServiceControllerName))).To(Succeed())
			Expect(dpuServices[0].Status.ConfigPorts).To(BeEmpty())

			By("pause the DPUServices and ensure the application associated with it are paused")
			for _, dpuService := range dpuServices {
				patcher := patch.NewSerialPatcher(dpuService, testClient)
				dpuService.Spec.Paused = ptr.To(true)
				Expect(patcher.Patch(ctx, dpuService, patch.WithFieldOwner(dpuServiceControllerName))).To(Succeed())
			}

			By("creating a headless Service with a NodePort should fail")
			dpuServices[0].Spec.ConfigPorts = configPorts
			dpuServices[0].Spec.ConfigPorts.ServiceType = corev1.ClusterIPNone
			dpuServices[0].Spec.ConfigPorts.Ports[0].NodePort = ptr.To(uint16(30001))
			err = patcher.Patch(ctx, dpuServices[0], patch.WithFieldOwner(dpuServiceControllerName))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(`nodePort can only be set when serviceType is NodePort`))

			patcher = patch.NewSerialPatcher(hostDPUService, testClient)
			hostDPUService.Spec.Paused = ptr.To(true)
			Expect(patcher.Patch(ctx, hostDPUService, patch.WithFieldOwner(dpuServiceControllerName))).To(Succeed())

			// Ensure the applications are paused.
			Eventually(func(g Gomega) {
				assertApplicationPaused(g, testClient, dpuServices, clusters)
			}).WithTimeout(30 * time.Second).Should(BeNil())

			By("resume the DPUServices and ensure the application associated with it are resumed")
			for _, dpuService := range dpuServices {
				patcher := patch.NewSerialPatcher(dpuService, testClient)
				dpuService.Spec.Paused = ptr.To(false)
				Expect(patcher.Patch(ctx, dpuService, patch.WithFieldOwner(dpuServiceControllerName))).To(Succeed())
			}

			patcher = patch.NewSerialPatcher(hostDPUService, testClient)
			hostDPUService.Spec.Paused = ptr.To(false)
			Expect(patcher.Patch(ctx, hostDPUService, patch.WithFieldOwner(dpuServiceControllerName))).To(Succeed())

			// Ensure the applications are resumed.
			Eventually(func(g Gomega) {
				assertApplication(g, testClient, testNS.Name, dpuServices, []*dpuservicev1.DPUServiceInterface{dpuServiceInterface}, clusters)
			}).WithTimeout(30 * time.Second).Should(BeNil())

			By("delete the DPUService and ensure the application associated with it are deleted")
			for i := range dpuServices {
				Expect(testClient.Delete(ctx, dpuServices[i])).To(Succeed())
			}
			Expect(testClient.Delete(ctx, hostDPUService)).To(Succeed())
			// Ensure the applications are deleted.
			Eventually(func(g Gomega) {
				applications := &argov1.ApplicationList{}
				g.Expect(testClient.List(ctx, applications)).To(Succeed())
				// We're not running the ArgoCD controllers in this test so the finalizers must be removed here.
				// Do this in each loop as there's a race condition where the Application is patched again
				// by the DPUService controller.
				for i := range applications.Items {
					err := testClient.Patch(ctx, &applications.Items[i], client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"finalizers":[]}}`)))
					if err != nil && !apierrors.IsNotFound(err) {
						g.Expect(err).ToNot(HaveOccurred())
					}
				}
				g.Expect(applications.Items).To(BeEmpty())

			}).WithTimeout(30 * time.Second).Should(BeNil())

			// Ensure the DPUService finalizer is removed and they are deleted.
			Eventually(func(g Gomega) {
				gotDPUServices := &dpuservicev1.DPUServiceList{}
				g.Expect(testClient.List(ctx, gotDPUServices)).To(Succeed())
				g.Expect(gotDPUServices.Items).To(BeEmpty())
			}).WithTimeout(30 * time.Second).Should(BeNil())

			g := NewWithT(GinkgoT())
			assertDPUServiceAnnotationsClean(g, testClient, dpuServices)
		})

		It("should successfully reconcile a DPUService for all DPUClusters when dpuClusterSelector is not set", func() {
			By("Creating multiple DPUClusters")
			clusters := []provisioningv1.DPUCluster{
				testutils.GetTestDPUCluster(testDPU1NS.Name, "cluster-one"),
				testutils.GetTestDPUCluster(testDPU2NS.Name, "cluster-two"),
			}
			// Add labels to clusters
			clusters[0].Labels = map[string]string{"dpucluster": "cluster1"}
			clusters[1].Labels = map[string]string{"dpucluster": "cluster2"}
			cleanupObjs = createDPUClusters(clusters, cleanupObjs)

			By("Creating some secrets in the DPUService namespace")
			labels := map[string]string{dpuservicev1.DPFImagePullSecretLabelKey: ""}
			Expect(testClient.Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "dpf-secret-one", Namespace: testNS.Name, Labels: labels}})).To(Succeed())
			Expect(testClient.Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "dpf-secret-two", Namespace: testNS.Name, Labels: labels}})).To(Succeed())

			By("Creating a DPUService without dpuClusterSelector")
			dpuServices := getMinimalDPUServices(testNS.Name)
			dpuServices[0].Spec.DPUClusterSelector = nil
			Expect(testClient.Create(ctx, dpuServices[0])).To(Succeed())
			cleanupObjs = append(cleanupObjs, dpuServices[0])

			By("Validating that ArgoCD AppProject is configured for all DPUClusters")
			Eventually(func(g Gomega) {
				assertAppProject(g, testClient, clusters)
			}).WithTimeout(30 * time.Second).Should(Succeed())

			By("Validating that ArgoCD Secrets are created for all DPUClusters")
			Eventually(func(g Gomega) {
				testNamespaces := []string{testDPU1NS.Name, testDPU2NS.Name}
				assertArgoCDSecrets(g, testClient, clusters, &cleanupObjs, testNamespaces)
			}).WithTimeout(30 * time.Second).Should(Succeed())

			By("Validating that image pull secrets are created in each DPUCluster")
			Eventually(func(g Gomega) {
				dpuClusterConfigs, err := dpucluster.GetConfigs(ctx, testClient)
				g.Expect(err).ToNot(HaveOccurred())

				for _, config := range dpuClusterConfigs {
					clusterClient, err := config.Client(ctx)
					g.Expect(err).ToNot(HaveOccurred())
					secrets := &corev1.SecretList{}
					g.Expect(clusterClient.List(ctx, secrets,
						client.HasLabels{dpuservicev1.DPFImagePullSecretLabelKey},
						client.InNamespace(testNS.Name))).To(Succeed())

					g.Expect(secrets.Items).To(HaveLen(2))
					secretNames := []string{secrets.Items[0].Name, secrets.Items[1].Name}
					g.Expect(secretNames).To(ConsistOf("dpf-secret-one", "dpf-secret-two"))
				}
			}).WithTimeout(30 * time.Second).Should(Succeed())

			By("Validating that Applications are created for all DPUClusters")
			Eventually(func(g Gomega) {
				applications := &argov1.ApplicationList{}
				g.Expect(testClient.List(ctx, applications, client.InNamespace(testConfig.Namespace))).To(Succeed())

				// Should have 2 applications (one for each cluster)
				g.Expect(applications.Items).To(HaveLen(2))

				// Verify each cluster has an application
				clusterNames := make(map[string]bool)
				for _, app := range applications.Items {
					g.Expect(app.Labels).To(HaveKey(provisioningv1.DPUClusterNameLabelKey))
					clusterNames[app.Labels[provisioningv1.DPUClusterNameLabelKey]] = true
				}
				g.Expect(clusterNames).To(HaveKey("cluster-one"))
				g.Expect(clusterNames).To(HaveKey("cluster-two"))
			}).WithTimeout(30 * time.Second).Should(Succeed())

			By("Deleting the DPUService and ensuring associated applications are deleted")
			Expect(testClient.Delete(ctx, dpuServices[0])).To(Succeed())

			By("Ensuring the applications are deleted")
			Eventually(func(g Gomega) {
				applications := &argov1.ApplicationList{}
				g.Expect(testClient.List(ctx, applications, client.InNamespace(testConfig.Namespace),
					client.MatchingLabels{
						dpuservicev1.DPUServiceNameLabelKey:      dpuServices[0].Name,
						dpuservicev1.DPUServiceNamespaceLabelKey: dpuServices[0].Namespace,
					})).To(Succeed())
				// We're not running the ArgoCD controllers in this test so the finalizers must be removed here.
				// Do this in each loop as there's a race condition where the Application is patched again
				// by the DPUService controller.
				for i := range applications.Items {
					err := testClient.Patch(ctx, &applications.Items[i], client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"finalizers":[]}}`)))
					if err != nil && !apierrors.IsNotFound(err) {
						g.Expect(err).ToNot(HaveOccurred())
					}
				}
				g.Expect(applications.Items).To(BeEmpty())
			}).WithTimeout(30 * time.Second).Should(Succeed())

			By("Ensuring the DPUService is deleted")
			Eventually(func(g Gomega) {
				err := testClient.Get(ctx, client.ObjectKeyFromObject(dpuServices[0]), dpuServices[0])
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}).WithTimeout(30 * time.Second).Should(Succeed())

			By("Ensuring the secrets are deleted from the DPUClusters")
			Eventually(func(g Gomega) {
				dpuClusterConfigs, err := dpucluster.GetConfigs(ctx, testClient)
				g.Expect(err).ToNot(HaveOccurred())

				for _, config := range dpuClusterConfigs {
					clusterClient, err := config.Client(ctx)
					g.Expect(err).ToNot(HaveOccurred())
					secrets := &corev1.SecretList{}
					g.Expect(clusterClient.List(ctx, secrets,
						client.HasLabels{dpuservicev1.DPFImagePullSecretLabelKey},
						client.InNamespace(testNS.Name))).To(Succeed())

					g.Expect(secrets.Items).To(BeEmpty())
				}
			}).WithTimeout(30 * time.Second).Should(Succeed())
		})

		It("should successfully reconcile a DPUService for all matching DPUClusters", func() {
			By("Creating multiple DPUClusters with different labels")
			clusters := []provisioningv1.DPUCluster{
				testutils.GetTestDPUCluster(testDPU1NS.Name, "cluster-one"),
				testutils.GetTestDPUCluster(testDPU2NS.Name, "cluster-two"),
			}
			clusters[0].Labels = map[string]string{"dpucluster": "cluster1"}
			clusters[1].Labels = map[string]string{"dpucluster": "cluster2"}
			cleanupObjs = createDPUClusters(clusters, cleanupObjs)

			By("Creating image pull secrets in the DPUService namespace")
			labels := map[string]string{dpuservicev1.DPFImagePullSecretLabelKey: ""}
			Expect(testClient.Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "dpf-secret-one", Namespace: testNS.Name, Labels: labels}})).To(Succeed())
			Expect(testClient.Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "dpf-secret-two", Namespace: testNS.Name, Labels: labels}})).To(Succeed())

			By("Creating a DPUService with dpuClusterSelector matching only cluster1")
			dpuServices := getMinimalDPUServices(testNS.Name)
			dpuServices[0].Spec.DPUClusterSelector = &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"dpucluster": "cluster1",
				},
			}
			Expect(testClient.Create(ctx, dpuServices[0])).To(Succeed())
			cleanupObjs = append(cleanupObjs, dpuServices[0])

			By("Validating that ArgoCD AppProject is configured for all DPUClusters")
			Eventually(func(g Gomega) {
				assertAppProject(g, testClient, clusters)
			}).WithTimeout(30 * time.Second).Should(Succeed())

			By("Validating that ArgoCD Secrets are created for all DPUClusters")
			Eventually(func(g Gomega) {
				testNamespaces := []string{testDPU1NS.Name, testDPU2NS.Name}
				assertArgoCDSecrets(g, testClient, clusters, &cleanupObjs, testNamespaces)
			}).WithTimeout(30 * time.Second).Should(Succeed())

			By("Validating that image pull secrets are created in all DPUClusters")
			Eventually(func(g Gomega) {
				dpuClusterConfigs, err := dpucluster.GetConfigs(ctx, testClient)
				g.Expect(err).ToNot(HaveOccurred())

				for _, config := range dpuClusterConfigs {
					clusterClient, err := config.Client(ctx)
					g.Expect(err).ToNot(HaveOccurred())
					secrets := &corev1.SecretList{}
					g.Expect(clusterClient.List(ctx, secrets,
						client.HasLabels{dpuservicev1.DPFImagePullSecretLabelKey},
						client.InNamespace(testNS.Name))).To(Succeed())

					g.Expect(secrets.Items).To(HaveLen(2))
					secretNames := []string{secrets.Items[0].Name, secrets.Items[1].Name}
					g.Expect(secretNames).To(ConsistOf("dpf-secret-one", "dpf-secret-two"))
				}
			}).WithTimeout(30 * time.Second).Should(Succeed())

			By("Validating that Applications are created only for matching DPUClusters")
			Eventually(func(g Gomega) {
				applications := &argov1.ApplicationList{}
				g.Expect(testClient.List(ctx, applications, client.InNamespace(testConfig.Namespace))).To(Succeed())

				// Should have 1 application (cluster-one has dpucluster=cluster1)
				g.Expect(applications.Items).To(HaveLen(1))

				// Verify only matching cluster has an application
				g.Expect(applications.Items[0].Labels).To(HaveKey(provisioningv1.DPUClusterNameLabelKey))
				g.Expect(applications.Items[0].Labels[provisioningv1.DPUClusterNameLabelKey]).To(Equal("cluster-one"))
			}).WithTimeout(30 * time.Second).Should(Succeed())

			By("Deleting the DPUService and ensuring associated applications are deleted")
			Expect(testClient.Delete(ctx, dpuServices[0])).To(Succeed())

			By("Ensuring the applications are deleted")
			Eventually(func(g Gomega) {
				applications := &argov1.ApplicationList{}
				g.Expect(testClient.List(ctx, applications, client.InNamespace(testConfig.Namespace),
					client.MatchingLabels{
						dpuservicev1.DPUServiceNameLabelKey:      dpuServices[0].Name,
						dpuservicev1.DPUServiceNamespaceLabelKey: dpuServices[0].Namespace,
					})).To(Succeed())
				// We're not running the ArgoCD controllers in this test so the finalizers must be removed here.
				// Do this in each loop as there's a race condition where the Application is patched again
				// by the DPUService controller.
				for i := range applications.Items {
					err := testClient.Patch(ctx, &applications.Items[i], client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"finalizers":[]}}`)))
					if err != nil && !apierrors.IsNotFound(err) {
						g.Expect(err).ToNot(HaveOccurred())
					}
				}
				g.Expect(applications.Items).To(BeEmpty())
			}).WithTimeout(30 * time.Second).Should(Succeed())

			By("Ensuring the DPUService is deleted")
			Eventually(func(g Gomega) {
				err := testClient.Get(ctx, client.ObjectKeyFromObject(dpuServices[0]), dpuServices[0])
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}).WithTimeout(30 * time.Second).Should(Succeed())

			By("Ensuring the secrets are deleted from the DPUClusters")
			Eventually(func(g Gomega) {
				dpuClusterConfigs, err := dpucluster.GetConfigs(ctx, testClient)
				g.Expect(err).ToNot(HaveOccurred())

				for _, config := range dpuClusterConfigs {
					clusterClient, err := config.Client(ctx)
					g.Expect(err).ToNot(HaveOccurred())
					secrets := &corev1.SecretList{}
					g.Expect(clusterClient.List(ctx, secrets,
						client.HasLabels{dpuservicev1.DPFImagePullSecretLabelKey},
						client.InNamespace(testNS.Name))).To(Succeed())

					g.Expect(secrets.Items).To(BeEmpty())
				}
			}).WithTimeout(30 * time.Second).Should(Succeed())
		})

		It("should remove applications from non matching DPUClusters", func() {
			By("Creating multiple DPUClusters with labels")
			clusters := []provisioningv1.DPUCluster{
				testutils.GetTestDPUCluster(testDPU1NS.Name, "cluster-one"),
				testutils.GetTestDPUCluster(testDPU2NS.Name, "cluster-two"),
			}
			clusters[0].Labels = map[string]string{"dpucluster": "cluster1"}
			clusters[1].Labels = map[string]string{"dpucluster": "cluster2"}
			cleanupObjs = createDPUClusters(clusters, cleanupObjs)

			By("Creating image pull secrets in the DPUService namespace")
			labels := map[string]string{dpuservicev1.DPFImagePullSecretLabelKey: ""}
			Expect(testClient.Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "dpf-secret-one", Namespace: testNS.Name, Labels: labels}})).To(Succeed())
			Expect(testClient.Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "dpf-secret-two", Namespace: testNS.Name, Labels: labels}})).To(Succeed())

			By("Creating a DPUService without dpuClusterSelector (targets all clusters)")
			dpuServices := getMinimalDPUServices(testNS.Name)
			dpuServices[0].Spec.DPUClusterSelector = nil
			Expect(testClient.Create(ctx, dpuServices[0])).To(Succeed())
			cleanupObjs = append(cleanupObjs, dpuServices[0])

			By("Validating that ArgoCD AppProject is configured for all DPUClusters")
			Eventually(func(g Gomega) {
				assertAppProject(g, testClient, clusters)
			}).WithTimeout(30 * time.Second).Should(Succeed())

			By("Validating that ArgoCD Secrets are created for all DPUClusters")
			Eventually(func(g Gomega) {
				testNamespaces := []string{testDPU1NS.Name, testDPU2NS.Name}
				assertArgoCDSecrets(g, testClient, clusters, &cleanupObjs, testNamespaces)
			}).WithTimeout(30 * time.Second).Should(Succeed())

			By("Validating that image pull secrets are created in all DPUClusters initially")
			Eventually(func(g Gomega) {
				dpuClusterConfigs, err := dpucluster.GetConfigs(ctx, testClient)
				g.Expect(err).ToNot(HaveOccurred())

				for _, config := range dpuClusterConfigs {
					clusterClient, err := config.Client(ctx)
					g.Expect(err).ToNot(HaveOccurred())
					secrets := &corev1.SecretList{}
					g.Expect(clusterClient.List(ctx, secrets,
						client.HasLabels{dpuservicev1.DPFImagePullSecretLabelKey},
						client.InNamespace(testNS.Name))).To(Succeed())

					g.Expect(secrets.Items).To(HaveLen(2))
					secretNames := []string{secrets.Items[0].Name, secrets.Items[1].Name}
					g.Expect(secretNames).To(ConsistOf("dpf-secret-one", "dpf-secret-two"))
				}
			}).WithTimeout(30 * time.Second).Should(Succeed())

			By("Validating that Applications are created for all DPUClusters")
			Eventually(func(g Gomega) {
				applications := &argov1.ApplicationList{}
				g.Expect(testClient.List(ctx, applications, client.InNamespace(testConfig.Namespace))).To(Succeed())
				g.Expect(applications.Items).To(HaveLen(2))
			}).WithTimeout(30 * time.Second).Should(Succeed())

			By("Updating the DPUService dpuClusterSelector to match only cluster1")
			patcher := patch.NewSerialPatcher(dpuServices[0], testClient)
			dpuServices[0].Spec.DPUClusterSelector = &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"dpucluster": "cluster1",
				},
			}
			Expect(patcher.Patch(ctx, dpuServices[0], patch.WithFieldOwner("dpu-service-test"))).To(Succeed())

			By("Ensuring finalizers are removed from the Application for cluster-two that is being deleted")
			Eventually(func(g Gomega) {
				applications := &argov1.ApplicationList{}
				g.Expect(testClient.List(ctx, applications, client.InNamespace(testConfig.Namespace),
					client.MatchingLabels{
						dpuservicev1.DPUServiceNameLabelKey:      dpuServices[0].Name,
						dpuservicev1.DPUServiceNamespaceLabelKey: dpuServices[0].Namespace,
						provisioningv1.DPUClusterNameLabelKey:    "cluster-two",
					})).To(Succeed())
				// We're not running the ArgoCD controllers in this test so the finalizers must be removed here.
				// The application for cluster-two is being deleted because it no longer matches the selector.
				for i := range applications.Items {
					err := testClient.Patch(ctx, &applications.Items[i], client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"finalizers":[]}}`)))
					if err != nil && !apierrors.IsNotFound(err) {
						g.Expect(err).ToNot(HaveOccurred())
					}
				}
				// Verify the cluster-two application is eventually deleted
				g.Expect(applications.Items).To(BeEmpty())
			}).WithTimeout(30 * time.Second).Should(Succeed())

			By("Validating that Applications are removed from non-matching clusters")
			Eventually(func(g Gomega) {
				applications := &argov1.ApplicationList{}
				g.Expect(testClient.List(ctx, applications, client.InNamespace(testConfig.Namespace))).To(Succeed())

				// Should now have only 1 application (cluster-one has dpucluster=cluster1)
				g.Expect(applications.Items).To(HaveLen(1))

				// Verify only cluster-one has an application
				g.Expect(applications.Items[0].Labels).To(HaveKey(provisioningv1.DPUClusterNameLabelKey))
				g.Expect(applications.Items[0].Labels[provisioningv1.DPUClusterNameLabelKey]).To(Equal("cluster-one"))
			}).WithTimeout(30 * time.Second).Should(Succeed())

			By("Validating that image pull secrets remain in all clusters after selector update")
			Eventually(func(g Gomega) {
				dpuClusterConfigs, err := dpucluster.GetConfigs(ctx, testClient)
				g.Expect(err).ToNot(HaveOccurred())

				for _, config := range dpuClusterConfigs {
					clusterClient, err := config.Client(ctx)
					g.Expect(err).ToNot(HaveOccurred())
					secrets := &corev1.SecretList{}
					g.Expect(clusterClient.List(ctx, secrets,
						client.HasLabels{dpuservicev1.DPFImagePullSecretLabelKey},
						client.InNamespace(testNS.Name))).To(Succeed())

					g.Expect(secrets.Items).To(HaveLen(2))
					secretNames := []string{secrets.Items[0].Name, secrets.Items[1].Name}
					g.Expect(secretNames).To(ConsistOf("dpf-secret-one", "dpf-secret-two"))
				}
			}).WithTimeout(30 * time.Second).Should(Succeed())

			By("Deleting the DPUService and ensuring associated applications are deleted")
			Expect(testClient.Delete(ctx, dpuServices[0])).To(Succeed())

			By("Ensuring the applications are deleted")
			Eventually(func(g Gomega) {
				applications := &argov1.ApplicationList{}
				g.Expect(testClient.List(ctx, applications, client.InNamespace(testConfig.Namespace),
					client.MatchingLabels{
						dpuservicev1.DPUServiceNameLabelKey:      dpuServices[0].Name,
						dpuservicev1.DPUServiceNamespaceLabelKey: dpuServices[0].Namespace,
					})).To(Succeed())
				// We're not running the ArgoCD controllers in this test so the finalizers must be removed here.
				// Do this in each loop as there's a race condition where the Application is patched again
				// by the DPUService controller.
				for i := range applications.Items {
					err := testClient.Patch(ctx, &applications.Items[i], client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"finalizers":[]}}`)))
					if err != nil && !apierrors.IsNotFound(err) {
						g.Expect(err).ToNot(HaveOccurred())
					}
				}
				g.Expect(applications.Items).To(BeEmpty())
			}).WithTimeout(30 * time.Second).Should(Succeed())

			By("Ensuring the DPUService is deleted")
			Eventually(func(g Gomega) {
				err := testClient.Get(ctx, client.ObjectKeyFromObject(dpuServices[0]), dpuServices[0])
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}).WithTimeout(30 * time.Second).Should(Succeed())

			By("Ensuring the secrets are deleted from the DPUClusters")
			Eventually(func(g Gomega) {
				dpuClusterConfigs, err := dpucluster.GetConfigs(ctx, testClient)
				g.Expect(err).ToNot(HaveOccurred())

				for _, config := range dpuClusterConfigs {
					clusterClient, err := config.Client(ctx)
					g.Expect(err).ToNot(HaveOccurred())
					secrets := &corev1.SecretList{}
					g.Expect(clusterClient.List(ctx, secrets,
						client.HasLabels{dpuservicev1.DPFImagePullSecretLabelKey},
						client.InNamespace(testNS.Name))).To(Succeed())

					g.Expect(secrets.Items).To(BeEmpty())
				}
			}).WithTimeout(30 * time.Second).Should(Succeed())
		})

		It("should successfully reconcile a DPUService with maximum name length", func() {
			By("Adding fake kamaji cluster")
			// 63 is the max name length of a DPUCluster
			dpuCluster := testutils.GetTestDPUCluster(testNS.Name, utilrand.String(63))
			cleanupObjs = createDPUClusters([]provisioningv1.DPUCluster{dpuCluster}, cleanupObjs)

			By("creating the DPUService")
			dpuServices := getMinimalDPUServices(testNS.Name)
			dpuServices[0].Name = utilrand.String(63)
			Expect(testClient.Create(ctx, dpuServices[0])).To(Succeed())
			cleanupObjs = append(cleanupObjs, dpuServices[0])

			By("Validating that the underlying Application has been created")
			Eventually(func(g Gomega) {
				applications := &argov1.ApplicationList{}
				g.Expect(testClient.List(ctx, applications, client.InNamespace(testConfig.Namespace))).To(Succeed())
				g.Expect(applications.Items).To(HaveLen(1))
				for _, app := range applications.Items {
					g.Expect(app.Labels).To(HaveKey(dpuservicev1.DPUServiceNameLabelKey))
					g.Expect(app.Labels).To(HaveKey(dpuservicev1.DPUServiceNamespaceLabelKey))
				}
			}).WithTimeout(30 * time.Second).Should(Succeed())

			By("Cleaning up the DPUService and Application")
			// TODO: Refactor the whole file to use DeferCleanup instead of cleanupObjs to ensure smoother deletion on
			//failure
			Eventually(func(g Gomega) {
				g.Expect(testClient.Delete(ctx, dpuServices[0])).To(Succeed())

				applications := &argov1.ApplicationList{}
				g.Expect(testClient.List(ctx, applications, client.InNamespace(testConfig.Namespace))).To(Succeed())
				// We're not running the ArgoCD controllers in this test so the finalizers must be removed here.
				// Do this in each loop as there's a race condition where the Application is patched again
				// by the DPUService controller.
				for i := range applications.Items {
					err := testClient.Patch(ctx, &applications.Items[i], client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"finalizers":[]}}`)))
					if err != nil && !apierrors.IsNotFound(err) {
						g.Expect(err).ToNot(HaveOccurred())
					}
				}
				g.Expect(applications.Items).To(BeEmpty())
			}).WithTimeout(30 * time.Second).Should(Succeed())
		})

		It("should successfully reconcile an incluster DPUService with maximum name length", func() {
			By("creating the DPUService")
			dpuServices := getMinimalDPUServices(testNS.Name)
			dpuServices[0].Name = utilrand.String(63)
			dpuServices[0].Spec.DeployInCluster = ptr.To(true)
			Expect(testClient.Create(ctx, dpuServices[0])).To(Succeed())
			cleanupObjs = append(cleanupObjs, dpuServices[0])

			By("Validating that the underlying Application has been created")
			Eventually(func(g Gomega) {
				applications := &argov1.ApplicationList{}
				g.Expect(testClient.List(ctx, applications, client.InNamespace(testConfig.Namespace))).To(Succeed())
				g.Expect(applications.Items).To(HaveLen(1))
				for _, app := range applications.Items {
					g.Expect(app.Labels).To(HaveKey(dpuservicev1.DPUServiceNameLabelKey))
					g.Expect(app.Labels).To(HaveKey(dpuservicev1.DPUServiceNamespaceLabelKey))
				}
			}).WithTimeout(30 * time.Second).Should(Succeed())

			By("Cleaning up the DPUService and Application")
			// TODO: Refactor the whole file to use DeferCleanup instead of cleanupObjs to ensure smoother deletion on
			//failure
			Eventually(func(g Gomega) {
				g.Expect(testClient.Delete(ctx, dpuServices[0])).To(Succeed())

				applications := &argov1.ApplicationList{}
				g.Expect(testClient.List(ctx, applications, client.InNamespace(testConfig.Namespace))).To(Succeed())
				// We're not running the ArgoCD controllers in this test so the finalizers must be removed here.
				// Do this in each loop as there's a race condition where the Application is patched again
				// by the DPUService controller.
				for i := range applications.Items {
					err := testClient.Patch(ctx, &applications.Items[i], client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"finalizers":[]}}`)))
					if err != nil && !apierrors.IsNotFound(err) {
						g.Expect(err).ToNot(HaveOccurred())
					}
				}
				g.Expect(applications.Items).To(BeEmpty())
			}).WithTimeout(30 * time.Second).Should(Succeed())
		})

		It("should fail to create DPUService with resource name exceeding the maximum length", func() {
			By("creating the DPUService")
			dpuServices := getMinimalDPUServices(testNS.Name)
			dpuServices[0].Name = utilrand.String(64)
			Expect(testClient.Create(ctx, dpuServices[0])).To(HaveOccurred())
		})

		It("should successfully create the DPUService with serviceID and interfaces", func() {
			By("creating the DPUService with serviceID and interfaces")
			dpuService := getMinimalDPUServices(testNS.Name)
			Expect(testClient.Create(ctx, dpuService[0])).To(Succeed())
			cleanupObjs = append(cleanupObjs, dpuService[0])
		})
		It("should successfully create the DPUService with serviceID", func() {
			By("creating the DPUService with serviceID and interfaces")
			dpuService := getMinimalDPUServices(testNS.Name)
			dpuService[0].Spec.Interfaces = nil
			Expect(testClient.Create(ctx, dpuService[0])).To(Succeed())
			cleanupObjs = append(cleanupObjs, dpuService[0])
		})

		It("should fail to create the DPUService without serviceID but with interfaces", func() {
			By("creating the DPUService with serviceID and interfaces")
			dpuService := getMinimalDPUServices(testNS.Name)
			dpuService[0].Spec.ServiceID = nil
			Expect(testClient.Create(ctx, dpuService[0])).ToNot(Succeed())
		})

		It("should successfully create the DPUService without serviceID and generate one automatically", func() {
			clusters := []provisioningv1.DPUCluster{
				testutils.GetTestDPUCluster(testDPU1NS.Name, "cluster-one"),
			}
			cleanupObjs = createDPUClusters(clusters, cleanupObjs)

			By("creating the DPUService without serviceID")
			dpuService := getMinimalDPUServices(testNS.Name)
			dpuService[0].Spec.ServiceID = nil
			dpuService[0].Spec.Interfaces = nil
			Expect(testClient.Create(ctx, dpuService[0])).To(Succeed())
			cleanupObjs = append(cleanupObjs, dpuService[0])

			Eventually(func(g Gomega) {
				assertDPUService(g, testClient, []*dpuservicev1.DPUService{dpuService[0]})
				gotDPUService := &dpuservicev1.DPUService{}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuService[0]), gotDPUService)).To(Succeed())
				g.Expect(gotDPUService.Status.ServiceID).To(MatchRegexp(`^[a-z-0-9]{10}$`))
			}).WithTimeout(30 * time.Second).Should(BeNil())

			Eventually(func(g Gomega) {
				assertArgoCDSecrets(g, testClient, clusters, &cleanupObjs, []string{testDPU1NS.Name, testDPU2NS.Name, testDPU3NS.Name})
			}).WithTimeout(30 * time.Second).Should(BeNil())

			Eventually(func(g Gomega) {
				assertAppProject(g, testClient, clusters)
			}).WithTimeout(30 * time.Second).Should(BeNil())

			// Check that the argo Application has been created correctly with generated serviceID
			Eventually(func(g Gomega) {
				applications := &argov1.ApplicationList{}
				g.Expect(testClient.List(ctx, applications, client.MatchingLabels{
					dpuservicev1.DPUServiceNameLabelKey:      dpuService[0].Name,
					dpuservicev1.DPUServiceNamespaceLabelKey: dpuService[0].Namespace,
				})).To(Succeed())
				g.Expect(applications.Items).To(HaveLen(1))

				app := applications.Items[0]
				var appValuesMap map[string]interface{}
				g.Expect(json.Unmarshal(app.Spec.Source.Helm.ValuesObject.Raw, &appValuesMap)).To(Succeed())
				g.Expect(appValuesMap).To(HaveKey("serviceDaemonSet"))

				serviceDaemonSet, ok := appValuesMap["serviceDaemonSet"].(map[string]interface{})
				g.Expect(ok).To(BeTrue())
				g.Expect(serviceDaemonSet).To(HaveKey("labels"))

				labels, ok := serviceDaemonSet["labels"].(map[string]interface{})
				g.Expect(ok).To(BeTrue())
				g.Expect(labels).To(HaveKey(dpuservicev1.DPFServiceIDLabelKey))
				generatedServiceID := labels[dpuservicev1.DPFServiceIDLabelKey].(string)
				g.Expect(generatedServiceID).To(MatchRegexp(`^[a-z-0-9]{10}$`))
			}).WithTimeout(30 * time.Second).Should(Succeed())

			Expect(testClient.Delete(ctx, dpuService[0])).To(Succeed())
			// Ensure the applications are deleted.
			Eventually(func(g Gomega) {
				applications := &argov1.ApplicationList{}
				g.Expect(testClient.List(ctx, applications)).To(Succeed())
				// We're not running the ArgoCD controllers in this test so the finalizers must be removed here.
				// Do this in each loop as there's a race condition where the Application is patched again
				// by the DPUService controller.
				for i := range applications.Items {
					err := testClient.Patch(ctx, &applications.Items[i], client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"finalizers":[]}}`)))
					if err != nil && !apierrors.IsNotFound(err) {
						g.Expect(err).ToNot(HaveOccurred())
					}
				}
				g.Expect(applications.Items).To(BeEmpty())

			}).WithTimeout(30 * time.Second).Should(BeNil())

			// Ensure the DPUService finalizer is removed and they are deleted.
			Eventually(func(g Gomega) {
				gotDPUServices := &dpuservicev1.DPUServiceList{}
				g.Expect(testClient.List(ctx, gotDPUServices)).To(Succeed())
				g.Expect(gotDPUServices.Items).To(BeEmpty())
			}).WithTimeout(30 * time.Second).Should(BeNil())
		})

		// resources inside servicedaemonset
		It("should successfully request trusted_sf from NAD annotation", func() {
			clusters := []provisioningv1.DPUCluster{
				testutils.GetTestDPUCluster(testDPU1NS.Name, "cluster-one"),
			}
			cleanupObjs = createDPUClusters(clusters, cleanupObjs)

			By("creating the DPUService with serviceID and interfaces")
			dpuService := getDPUServiceWithoutHelmchartValues(testNS.Name)
			dpuServiceInterface := getMinimalDPUServiceInterfacewithCustomNAD(testNS.Name)
			dpuService[0].Spec.Interfaces[0] = dpuServiceInterface.Name
			dpuServiceNAD := getDPUServiceNADWithSpec(testNS.Name)
			dpuServiceNAD.Annotations = make(map[string]string)
			dpuServiceNAD.Annotations[dpuservicev1.TrustedSfAnnotationKey] = ""
			Expect(testClient.Create(ctx, dpuServiceNAD)).To(Succeed())
			cleanupObjs = append(cleanupObjs, dpuServiceNAD)
			Expect(testClient.Create(ctx, dpuServiceInterface)).To(Succeed())
			cleanupObjs = append(cleanupObjs, dpuServiceInterface)
			Expect(testClient.Create(ctx, dpuService[0])).To(Succeed())
			cleanupObjs = append(cleanupObjs, dpuService[0])

			Eventually(func(g Gomega) {
				assertDPUService(g, testClient, []*dpuservicev1.DPUService{dpuService[0]})
			}).WithTimeout(30 * time.Second).Should(BeNil())

			// Check that argo secrets have been created correctly.
			Eventually(func(g Gomega) {
				assertArgoCDSecrets(g, testClient, clusters, &cleanupObjs, []string{testDPU1NS.Name, testDPU2NS.Name, testDPU3NS.Name})
			}).WithTimeout(30 * time.Second).Should(BeNil())

			// Check that the argo AppProject has been created correctly
			Eventually(func(g Gomega) {
				assertAppProject(g, testClient, clusters)
			}).WithTimeout(30 * time.Second).Should(BeNil())

			Eventually(func(g Gomega) {
				// Ensure Resources is not nil
				values := getApplicationServiceDaemonSetValues(g, dpuService[0])
				g.Expect(values).ToNot(BeNil(), "Expected ApplicationValues to not be nil")
				resList := values.Resources
				g.Expect(resList).ToNot(BeNil(), "Expected Resources to not be nil")

				// Verify the resource values
				expectedResourceName := corev1.ResourceName("nvidia.com/bf_sf_trusted")
				expectedResourceValue := resource.MustParse("1")

				actualResourceValue, exists := resList[expectedResourceName]
				g.Expect(exists).To(BeTrue(), "Expected Resources to have key 'nvidia.com/bf_sf_trusted'")
				g.Expect(actualResourceValue.Equal(expectedResourceValue)).To(BeTrue(), "Expected 'nvidia.com/bf_sf_trusted' to have value 1")
			}).WithTimeout(30 * time.Second).Should(Succeed())

			Expect(testClient.Delete(ctx, dpuService[0])).To(Succeed())
			// Ensure the applications are deleted.
			Eventually(func(g Gomega) {
				applications := &argov1.ApplicationList{}
				g.Expect(testClient.List(ctx, applications)).To(Succeed())
				// We're not running the ArgoCD controllers in this test so the finalizers must be removed here.
				// Do this in each loop as there's a race condition where the Application is patched again
				// by the DPUService controller.
				for i := range applications.Items {
					err := testClient.Patch(ctx, &applications.Items[i], client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"finalizers":[]}}`)))
					if err != nil && !apierrors.IsNotFound(err) {
						g.Expect(err).ToNot(HaveOccurred())
					}
				}
				g.Expect(applications.Items).To(BeEmpty())

			}).WithTimeout(30 * time.Second).Should(BeNil())

			// Ensure the DPUService finalizer is removed and they are deleted.
			Eventually(func(g Gomega) {
				gotDPUServices := &dpuservicev1.DPUServiceList{}
				g.Expect(testClient.List(ctx, gotDPUServices)).To(Succeed())
				g.Expect(gotDPUServices.Items).To(BeEmpty())
			}).WithTimeout(30 * time.Second).Should(BeNil())
		})

		It("should successfully inject the resources in ServiceDaemonSet based on resourceType from customNAD in DPUServiceInterface", func() {
			clusters := []provisioningv1.DPUCluster{
				testutils.GetTestDPUCluster(testDPU1NS.Name, "cluster-one"),
			}
			cleanupObjs = createDPUClusters(clusters, cleanupObjs)

			By("creating the DPUService with serviceID and interfaces")
			dpuService := getDPUServiceWithoutHelmchartValues(testNS.Name)
			dpuServiceInterface := getMinimalDPUServiceInterfacewithCustomNAD(testNS.Name)
			dpuService[0].Spec.Interfaces[0] = dpuServiceInterface.Name
			dpuServiceNAD := getDPUServiceNADWithSpec(testNS.Name)
			Expect(testClient.Create(ctx, dpuServiceNAD)).To(Succeed())
			cleanupObjs = append(cleanupObjs, dpuServiceNAD)
			Expect(testClient.Create(ctx, dpuServiceInterface)).To(Succeed())
			cleanupObjs = append(cleanupObjs, dpuServiceInterface)
			Expect(testClient.Create(ctx, dpuService[0])).To(Succeed())
			cleanupObjs = append(cleanupObjs, dpuService[0])

			Eventually(func(g Gomega) {
				assertDPUService(g, testClient, []*dpuservicev1.DPUService{dpuService[0]})
			}).WithTimeout(30 * time.Second).Should(BeNil())

			// Check that argo secrets have been created correctly.
			Eventually(func(g Gomega) {
				assertArgoCDSecrets(g, testClient, clusters, &cleanupObjs, []string{testDPU1NS.Name, testDPU2NS.Name, testDPU3NS.Name})
			}).WithTimeout(30 * time.Second).Should(BeNil())

			// Check that the argo AppProject has been created correctly
			Eventually(func(g Gomega) {
				assertAppProject(g, testClient, clusters)
			}).WithTimeout(30 * time.Second).Should(BeNil())

			Eventually(func(g Gomega) {
				// Ensure Resources is not nil
				values := getApplicationServiceDaemonSetValues(g, dpuService[0])
				g.Expect(values).ToNot(BeNil(), "Expected ApplicationValues to not be nil")
				resList := values.Resources
				g.Expect(resList).ToNot(BeNil(), "Expected Resources to not be nil")

				// Verify the resource values
				expectedResourceName := corev1.ResourceName("nvidia.com/bf_sf")
				expectedResourceValue := resource.MustParse("1")

				actualResourceValue, exists := resList[expectedResourceName]
				g.Expect(exists).To(BeTrue(), "Expected Resources to have key 'nvidia.com/bf_sf'")
				g.Expect(actualResourceValue.Equal(expectedResourceValue)).To(BeTrue(), "Expected 'nvidia.com/bf_sf' to have value 1")
			}).WithTimeout(30 * time.Second).Should(Succeed())

			Expect(testClient.Delete(ctx, dpuService[0])).To(Succeed())
			// Ensure the applications are deleted.
			Eventually(func(g Gomega) {
				applications := &argov1.ApplicationList{}
				g.Expect(testClient.List(ctx, applications)).To(Succeed())
				// We're not running the ArgoCD controllers in this test so the finalizers must be removed here.
				// Do this in each loop as there's a race condition where the Application is patched again
				// by the DPUService controller.
				for i := range applications.Items {
					err := testClient.Patch(ctx, &applications.Items[i], client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"finalizers":[]}}`)))
					if err != nil && !apierrors.IsNotFound(err) {
						g.Expect(err).ToNot(HaveOccurred())
					}
				}
				g.Expect(applications.Items).To(BeEmpty())

			}).WithTimeout(30 * time.Second).Should(BeNil())

			// Ensure the DPUService finalizer is removed and they are deleted.
			Eventually(func(g Gomega) {
				gotDPUServices := &dpuservicev1.DPUServiceList{}
				g.Expect(testClient.List(ctx, gotDPUServices)).To(Succeed())
				g.Expect(gotDPUServices.Items).To(BeEmpty())
			}).WithTimeout(30 * time.Second).Should(BeNil())
		})

		It("should successfully update the resources in ServiceDaemonSet based on resourceType from customNAD in DPUServiceInterface", func() {
			clusters := []provisioningv1.DPUCluster{
				testutils.GetTestDPUCluster(testDPU1NS.Name, "cluster-one"),
			}
			cleanupObjs = createDPUClusters(clusters, cleanupObjs)

			By("creating the DPUService with serviceID and interfaces")
			dpuService := getDPUServiceWithoutHelmchartValues(testNS.Name)
			dpuServiceInterface := getMinimalDPUServiceInterfacewithCustomNAD(testNS.Name)
			dpuService[0].Spec.Interfaces[0] = dpuServiceInterface.Name
			dpuServiceNAD := getDPUServiceNADWithSpec(testNS.Name)
			Expect(testClient.Create(ctx, dpuServiceNAD)).To(Succeed())
			cleanupObjs = append(cleanupObjs, dpuServiceNAD)
			Expect(testClient.Create(ctx, dpuServiceInterface)).To(Succeed())
			cleanupObjs = append(cleanupObjs, dpuServiceInterface)
			Expect(testClient.Create(ctx, dpuService[0])).To(Succeed())
			cleanupObjs = append(cleanupObjs, dpuService[0])

			Eventually(func(g Gomega) {
				assertDPUService(g, testClient, []*dpuservicev1.DPUService{dpuService[0]})
			}).WithTimeout(30 * time.Second).Should(BeNil())

			// Check that argo secrets have been created correctly.
			Eventually(func(g Gomega) {
				assertArgoCDSecrets(g, testClient, clusters, &cleanupObjs, []string{testDPU1NS.Name, testDPU2NS.Name, testDPU3NS.Name})
			}).WithTimeout(30 * time.Second).Should(BeNil())

			// Check that the argo AppProject has been created correctly
			Eventually(func(g Gomega) {
				assertAppProject(g, testClient, clusters)
			}).WithTimeout(30 * time.Second).Should(BeNil())

			Eventually(func(g Gomega) {
				// Ensure Resources is not nil
				values := getApplicationServiceDaemonSetValues(g, dpuService[0])
				g.Expect(values).ToNot(BeNil(), "Expected ApplicationValues to not be nil")
				resList := values.Resources
				g.Expect(resList).ToNot(BeNil(), "Expected Resources to not be nil")

				// Verify the resource values
				expectedResourceName := corev1.ResourceName("nvidia.com/bf_sf")
				expectedResourceValue := resource.MustParse("1")

				actualResourceValue, exists := resList[expectedResourceName]
				g.Expect(exists).To(BeTrue(), "Expected Resources to have key 'nvidia.com/bf_sf'")
				g.Expect(actualResourceValue.Equal(expectedResourceValue)).To(BeTrue(), "Expected 'nvidia.com/bf_sf' to have value 1")
			}).WithTimeout(30 * time.Second).Should(Succeed())

			// Update/Delete DPUServiceNAD object, which should update the resources in helmchart values
			origNAD := dpuServiceNAD.DeepCopy()
			dpuServiceNAD.Spec.ResourceType = "vf"
			Expect(testClient.Patch(ctx, dpuServiceNAD, client.MergeFrom(origNAD))).To(Succeed())
			// verify merged values are updated
			Eventually(func(g Gomega) {
				// Ensure Resources is not nil
				values := getApplicationServiceDaemonSetValues(g, dpuService[0])
				g.Expect(values).ToNot(BeNil(), "Expected ApplicationValues to not be nil")
				resList := values.Resources
				g.Expect(resList).ToNot(BeNil(), "Expected Resources to not be nil")

				// Verify the resource values
				expectedResourceName := corev1.ResourceName("nvidia.com/bf_vf")
				expectedResourceValue := resource.MustParse("1")

				actualResourceValue, exists := resList[expectedResourceName]
				g.Expect(exists).To(BeTrue(), "Expected Resources to have key 'nvidia.com/bf_vf'")
				g.Expect(actualResourceValue.Equal(expectedResourceValue)).To(BeTrue(), "Expected 'nvidia.com/bf_vf' to have value 1")

			}).WithTimeout(30 * time.Second).Should(Succeed())

			Expect(testClient.Delete(ctx, dpuService[0])).To(Succeed())
			// Ensure the applications are deleted.
			Eventually(func(g Gomega) {
				applications := &argov1.ApplicationList{}
				g.Expect(testClient.List(ctx, applications)).To(Succeed())
				// We're not running the ArgoCD controllers in this test so the finalizers must be removed here.
				// Do this in each loop as there's a race condition where the Application is patched again
				// by the DPUService controller.
				for i := range applications.Items {
					err := testClient.Patch(ctx, &applications.Items[i], client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"finalizers":[]}}`)))
					if err != nil && !apierrors.IsNotFound(err) {
						g.Expect(err).ToNot(HaveOccurred())
					}
				}
				g.Expect(applications.Items).To(BeEmpty())

			}).WithTimeout(30 * time.Second).Should(BeNil())

			// Ensure the DPUService finalizer is removed and they are deleted.
			Eventually(func(g Gomega) {
				gotDPUServices := &dpuservicev1.DPUServiceList{}
				g.Expect(testClient.List(ctx, gotDPUServices)).To(Succeed())
				g.Expect(gotDPUServices.Items).To(BeEmpty())
			}).WithTimeout(30 * time.Second).Should(BeNil())
		})

		It("should only inject the resources in ServiceDaemonSet for sf/vf resourcetype in customNAD", func() {
			clusters := []provisioningv1.DPUCluster{
				testutils.GetTestDPUCluster(testDPU1NS.Name, "cluster-one"),
			}
			cleanupObjs = createDPUClusters(clusters, cleanupObjs)

			By("creating the DPUService with serviceID and interfaces")
			dpuService := getMinimalDPUServices(testNS.Name)
			dpuService[0].Spec.ServiceDaemonSet.Resources = corev1.ResourceList{
				"cpu":    resource.MustParse("1"),
				"memory": resource.MustParse("3Gi"),
			}
			dpuServiceInterface := getMinimalDPUServiceInterfacewithCustomNAD(testNS.Name)
			dpuService[0].Spec.Interfaces[0] = dpuServiceInterface.Name
			dpuServiceNAD := getDPUServiceNADWithSpec(testNS.Name)
			dpuServiceNAD.Spec.ResourceType = "veth"
			Expect(testClient.Create(ctx, dpuServiceNAD)).To(Succeed())
			cleanupObjs = append(cleanupObjs, dpuServiceNAD)
			Expect(testClient.Create(ctx, dpuServiceInterface)).To(Succeed())
			cleanupObjs = append(cleanupObjs, dpuServiceInterface)
			Expect(testClient.Create(ctx, dpuService[0])).To(Succeed())
			cleanupObjs = append(cleanupObjs, dpuService[0])

			Eventually(func(g Gomega) {
				assertDPUService(g, testClient, []*dpuservicev1.DPUService{dpuService[0]})
			}).WithTimeout(30 * time.Second).Should(BeNil())

			// Check that argo secrets have been created correctly.
			Eventually(func(g Gomega) {
				assertArgoCDSecrets(g, testClient, clusters, &cleanupObjs, []string{testDPU1NS.Name, testDPU2NS.Name, testDPU3NS.Name})
			}).WithTimeout(30 * time.Second).Should(BeNil())

			// Check that the argo AppProject has been created correctly
			Eventually(func(g Gomega) {
				assertAppProject(g, testClient, clusters)
			}).WithTimeout(30 * time.Second).Should(BeNil())

			Eventually(func(g Gomega) {
				// Ensure Resources is not nil
				values := getApplicationServiceDaemonSetValues(g, dpuService[0])
				g.Expect(values).ToNot(BeNil(), "Expected ApplicationValues to not be nil")
				resList := values.Resources
				g.Expect(resList).ToNot(BeNil(), "Expected Resources to not be nil")
				g.Expect(resList).To(HaveLen(2), "Existing resources should remain unchanged")
			}).WithTimeout(30 * time.Second).Should(Succeed())

			Expect(testClient.Delete(ctx, dpuService[0])).To(Succeed())
			// Ensure the applications are deleted.
			Eventually(func(g Gomega) {
				applications := &argov1.ApplicationList{}
				g.Expect(testClient.List(ctx, applications)).To(Succeed())
				// We're not running the ArgoCD controllers in this test so the finalizers must be removed here.
				// Do this in each loop as there's a race condition where the Application is patched again
				// by the DPUService controller.
				for i := range applications.Items {
					err := testClient.Patch(ctx, &applications.Items[i], client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"finalizers":[]}}`)))
					if err != nil && !apierrors.IsNotFound(err) {
						g.Expect(err).ToNot(HaveOccurred())
					}
				}
				g.Expect(applications.Items).To(BeEmpty())

			}).WithTimeout(30 * time.Second).Should(BeNil())

			// Ensure the DPUService finalizer is removed and they are deleted.
			Eventually(func(g Gomega) {
				gotDPUServices := &dpuservicev1.DPUServiceList{}
				g.Expect(testClient.List(ctx, gotDPUServices)).To(Succeed())
				g.Expect(gotDPUServices.Items).To(BeEmpty())
			}).WithTimeout(30 * time.Second).Should(BeNil())
		})

		It("should merge resources in servicedaemonset", func() {
			clusters := []provisioningv1.DPUCluster{
				testutils.GetTestDPUCluster(testDPU1NS.Name, "cluster-one"),
			}
			cleanupObjs = createDPUClusters(clusters, cleanupObjs)

			By("creating the DPUService with serviceID and interfaces")
			dpuService := getMinimalDPUServices(testNS.Name)
			dpuService[0].Spec.ServiceDaemonSet.Resources = corev1.ResourceList{
				"cpu":    resource.MustParse("1"),
				"memory": resource.MustParse("3Gi"),
			}
			dpuServiceInterface := getMinimalDPUServiceInterfacewithCustomNAD(testNS.Name)
			dpuService[0].Spec.Interfaces[0] = dpuServiceInterface.Name
			dpuServiceNAD := getDPUServiceNADWithSpec(testNS.Name)
			dpuServiceNAD.Spec.ResourceType = "sf"
			Expect(testClient.Create(ctx, dpuServiceNAD)).To(Succeed())
			cleanupObjs = append(cleanupObjs, dpuServiceNAD)
			Expect(testClient.Create(ctx, dpuServiceInterface)).To(Succeed())
			cleanupObjs = append(cleanupObjs, dpuServiceInterface)
			Expect(testClient.Create(ctx, dpuService[0])).To(Succeed())
			cleanupObjs = append(cleanupObjs, dpuService[0])

			Eventually(func(g Gomega) {
				assertDPUService(g, testClient, []*dpuservicev1.DPUService{dpuService[0]})
			}).WithTimeout(30 * time.Second).Should(BeNil())

			// Check that argo secrets have been created correctly.
			Eventually(func(g Gomega) {
				assertArgoCDSecrets(g, testClient, clusters, &cleanupObjs, []string{testDPU1NS.Name, testDPU2NS.Name, testDPU3NS.Name})
			}).WithTimeout(30 * time.Second).Should(BeNil())

			// Check that the argo AppProject has been created correctly
			Eventually(func(g Gomega) {
				assertAppProject(g, testClient, clusters)
			}).WithTimeout(30 * time.Second).Should(BeNil())

			// verify resources merged correctly
			Eventually(func(g Gomega) {
				// Ensure Resources is not nil
				values := getApplicationServiceDaemonSetValues(g, dpuService[0])
				g.Expect(values).ToNot(BeNil(), "Expected ApplicationValues to not be nil")
				resList := values.Resources
				g.Expect(resList).ToNot(BeNil(), "Expected Resources to not be nil")
				g.Expect(resList).To(HaveLen(3), "Expected 3 resources in servicedaemonset")
				// Verify the resource values
				expectedResourceName := corev1.ResourceName("nvidia.com/bf_sf")
				expectedResourceValue := resource.MustParse("1")

				actualResourceValue, exists := resList[expectedResourceName]
				g.Expect(exists).To(BeTrue(), "Expected Resources to have key 'nvidia.com/bf_sf'")
				g.Expect(actualResourceValue.Equal(expectedResourceValue)).To(BeTrue(), "Expected 'nvidia.com/bf_sf' to have value 1")

				expectedResourceName = corev1.ResourceName("cpu")
				expectedResourceValue = resource.MustParse("1")
				actualResourceValue, exists = resList[expectedResourceName]
				g.Expect(exists).To(BeTrue(), "Expected existing resources(cpu) to be present")
				g.Expect(actualResourceValue.Equal(expectedResourceValue)).To(BeTrue(), "Expected existing resources(cpu) to be present")
				expectedResourceName = corev1.ResourceName("memory")
				expectedResourceValue = resource.MustParse("3Gi")
				actualResourceValue, exists = resList[expectedResourceName]
				g.Expect(exists).To(BeTrue(), "Expected existing resources(memory) to be present")
				g.Expect(actualResourceValue.Equal(expectedResourceValue)).To(BeTrue(), "Expected existing resources(memory) to be present")
			}).WithTimeout(30 * time.Second).Should(Succeed())

			Expect(testClient.Delete(ctx, dpuService[0])).To(Succeed())
			// Ensure the applications are deleted.
			Eventually(func(g Gomega) {
				applications := &argov1.ApplicationList{}
				g.Expect(testClient.List(ctx, applications)).To(Succeed())
				// We're not running the ArgoCD controllers in this test so the finalizers must be removed here.
				// Do this in each loop as there's a race condition where the Application is patched again
				// by the DPUService controller.
				for i := range applications.Items {
					err := testClient.Patch(ctx, &applications.Items[i], client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"finalizers":[]}}`)))
					if err != nil && !apierrors.IsNotFound(err) {
						g.Expect(err).ToNot(HaveOccurred())
					}
				}
				g.Expect(applications.Items).To(BeEmpty())

			}).WithTimeout(30 * time.Second).Should(BeNil())

			// Ensure the DPUService finalizer is removed and they are deleted.
			Eventually(func(g Gomega) {
				gotDPUServices := &dpuservicev1.DPUServiceList{}
				g.Expect(testClient.List(ctx, gotDPUServices)).To(Succeed())
				g.Expect(gotDPUServices.Items).To(BeEmpty())
			}).WithTimeout(30 * time.Second).Should(BeNil())
		})
	})
})

func createDPUClusters(clusters []provisioningv1.DPUCluster, cleanupObjs []client.Object) []client.Object {
	if len(cleanupObjs) == 0 {
		cleanupObjs = []client.Object{}
	}
	for i := range clusters {
		kamajiSecret, err := testutils.GetFakeKamajiClusterSecretFromEnvtest(clusters[i], cfg)
		Expect(err).NotTo(HaveOccurred())
		Expect(testClient.Create(ctx, kamajiSecret)).To(Succeed())
		cleanupObjs = append(cleanupObjs, kamajiSecret)
	}

	for i, cl := range clusters {
		Expect(testClient.Create(ctx, &cl)).To(Succeed())
		cleanupObjs = append(cleanupObjs, &clusters[i])
		patcher := patch.NewSerialPatcher(&cl, testClient)

		// mark the cluster as ready so that the remoteCache treats it as ready
		cl.Status.Phase = provisioningv1.PhaseReady
		Expect(patcher.Patch(ctx, &cl, patch.WithFieldOwner(dpuServiceControllerName))).To(Succeed())
	}

	return cleanupObjs
}

var _ = Describe("DPUService Controller reconcile interfaces", func() {
	var (
		testNS              *corev1.Namespace
		dpuServiceInterface *dpuservicev1.DPUServiceInterface
		cleanupObjs         []client.Object
	)
	BeforeEach(func() {
		By("creating the namespaces")
		testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "testns-"}}
		Expect(testClient.Create(ctx, testNS)).To(Succeed())

		dpuServiceInterface = getMinimalDPUServiceInterface(testNS.Name)
		Expect(client.IgnoreAlreadyExists(testClient.Create(ctx, dpuServiceInterface))).To(Succeed())
		cleanupObjs = append(cleanupObjs, dpuServiceInterface)
	})
	AfterEach(func() {
		By("Cleanup the Namespace and Secrets")
		Expect(testutils.CleanupAndWait(ctx, testClient, cleanupObjs...)).To(Succeed())
		Expect(testClient.Delete(ctx, testNS)).To(Succeed())
	})

	DescribeTable("reconcile serviceDaemonSet values",
		func(dpuService *dpuservicev1.DPUService, interfaceName string, expected *dpuservicev1.ServiceDaemonSetValues) {
			if interfaceName != "" {
				dpuService.Spec.Interfaces = []string{interfaceName}
				dpuService.Namespace = testNS.Name
			}
			r := &DPUServiceReconciler{Client: testClient, Scheme: testClient.Scheme()}
			serviceID := ""
			if dpuService.Spec.ServiceID != nil {
				serviceID = *dpuService.Spec.ServiceID
			}
			values, _, err := r.reconcileInterfaces(ctx, dpuService, serviceID)
			Expect(err).ToNot(HaveOccurred())
			Expect(values).To(Equal(expected))
		},
		Entry("empty values", &dpuservicev1.DPUService{
			Spec: dpuservicev1.DPUServiceSpec{
				ServiceDaemonSet: &dpuservicev1.ServiceDaemonSetValues{},
			},
		}, "", &dpuservicev1.ServiceDaemonSetValues{
			Annotations: nil,
			Labels:      nil,
		}),
		Entry("values with annotations", &dpuservicev1.DPUService{
			Spec: dpuservicev1.DPUServiceSpec{
				ServiceDaemonSet: &dpuservicev1.ServiceDaemonSetValues{
					Annotations: map[string]string{
						networkAnnotationKey: `[{"name":"mybrsfc","namespace":"my-namespace","interface":"net1","cni-args":null}]`,
					},
				},
			},
		}, "", &dpuservicev1.ServiceDaemonSetValues{
			Annotations: map[string]string{
				networkAnnotationKey: `[{"name":"mybrsfc","namespace":"my-namespace","interface":"net1","cni-args":null}]`,
			},
			Labels: nil,
		}),
		Entry("values with labels", &dpuservicev1.DPUService{
			Spec: dpuservicev1.DPUServiceSpec{
				ServiceDaemonSet: &dpuservicev1.ServiceDaemonSetValues{
					Labels: map[string]string{
						dpuservicev1.DPFServiceIDLabelKey: "service-one",
					},
				},
			},
		}, "", &dpuservicev1.ServiceDaemonSetValues{
			Annotations: nil,
			Labels: map[string]string{
				dpuservicev1.DPFServiceIDLabelKey: "service-one",
			},
		}),
		Entry("DPUService with interfaces", &dpuservicev1.DPUService{
			Spec: dpuservicev1.DPUServiceSpec{
				ServiceDaemonSet: &dpuservicev1.ServiceDaemonSetValues{
					Annotations: map[string]string{
						networkAnnotationKey: `[{"name":"iprequest","interface":"myip1","cni-args":{"allocateDefaultGateway":true,"poolNames":["pool1"],"poolType":"cidrpool"}}]`,
					},
					Labels: map[string]string{
						dpuservicev1.DPFServiceIDLabelKey: "service-one",
					},
				},
			},
		}, "dpu-service-interface", &dpuservicev1.ServiceDaemonSetValues{
			Annotations: map[string]string{
				networkAnnotationKey: mergeWithInvalidNetwork(`[{"name":"iprequest","interface":"myip1","cni-args":{"allocateDefaultGateway":true,"poolNames":["pool1"],"poolType":"cidrpool"}},` +
					`{"name":"mybrsfc","namespace":"my-namespace","interface":"net1","cni-args":null}]`),
			},
			Labels: map[string]string{
				dpuservicev1.DPFServiceIDLabelKey: "service-one",
			},
		}),
	)
})

func assertDPUService(g Gomega, testClient client.Client, dpuServices []*dpuservicev1.DPUService) {
	for i := range dpuServices {
		gotDPUService := &dpuservicev1.DPUService{}
		g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuServices[i]), gotDPUService)).To(Succeed())
		g.Expect(gotDPUService.Finalizers).To(ConsistOf([]string{dpuservicev1.DPUServiceFinalizer}))
	}
}

func assertDPUServiceConfigPorts(g Gomega, testClient client.Client, dpuServices []*dpuservicev1.DPUService, expectedConfigPorts *dpuservicev1.ConfigPorts, expectedEndpointSliceEndpoint []discoveryv1.Endpoint, clusterName string) {
	for i := range dpuServices {
		gotDPUService := &dpuservicev1.DPUService{}
		g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuServices[i]), gotDPUService)).To(Succeed())
		g.Expect(gotDPUService.Spec.ConfigPorts).To(Equal(expectedConfigPorts))
		if expectedConfigPorts == nil {
			assertConfigPortsDontExist(g, testClient, dpuServices[i], clusterName)
			return
		}

		// Validate the status of the ConfigPorts.
		g.Expect(gotDPUService.Status.ConfigPorts).To(HaveKey(clusterName))
		clusterConfigPortStatus := gotDPUService.Status.ConfigPorts[clusterName][0]
		g.Expect(clusterConfigPortStatus.Name).To(Equal(expectedConfigPorts.Ports[0].Name))
		g.Expect(clusterConfigPortStatus.Port).To(Equal(expectedConfigPorts.Ports[0].Port))
		g.Expect(clusterConfigPortStatus.Protocol).To(Equal(expectedConfigPorts.Ports[0].Protocol))
		if expectedConfigPorts.ServiceType == corev1.ServiceTypeNodePort {
			g.Expect(clusterConfigPortStatus.NodePort).NotTo(BeNil())
		}

		// Validate that Service, EndpointSlices and Endpoints are created correctly
		assertConfigPortService(g, testClient, dpuServices[i], expectedConfigPorts, clusterName)
		assertConfigPortEndpointSlice(g, testClient, dpuServices[i], expectedEndpointSliceEndpoint, clusterName)
		assertConfigPortEndpoints(g, testClient, dpuServices[i], clusterName)
	}
}

func assertConfigPortsDontExist(g Gomega, testClient client.Client, dpuService *dpuservicev1.DPUService, clusterName string) {
	serviceName := getConfigPortName(clusterName, dpuService.GetName())

	// Verify Service is deleted
	service := &corev1.Service{}
	err := testClient.Get(ctx, types.NamespacedName{
		Name:      serviceName,
		Namespace: dpuService.GetNamespace(),
	}, service)
	g.Expect(apierrors.IsNotFound(err)).To(BeTrue(), "Service should be deleted")

	// Verify EndpointSlice is deleted
	endpointSlice := &discoveryv1.EndpointSlice{}
	err = testClient.Get(ctx, types.NamespacedName{
		Name:      serviceName,
		Namespace: dpuService.GetNamespace(),
	}, endpointSlice)
	g.Expect(apierrors.IsNotFound(err)).To(BeTrue(), "EndpointSlice should be deleted")

	// Verify Endpoints is deleted
	endpoints := &corev1.Endpoints{}
	err = testClient.Get(ctx, types.NamespacedName{
		Name:      serviceName,
		Namespace: dpuService.GetNamespace(),
	}, endpoints)
	g.Expect(apierrors.IsNotFound(err)).To(BeTrue(), "Endpoints should be deleted")
}

func assertConfigPortService(g Gomega, testClient client.Client, dpuService *dpuservicev1.DPUService, expectedConfigPorts *dpuservicev1.ConfigPorts, clusterName string) {
	serviceName := getConfigPortName(clusterName, dpuService.GetName())
	service := &corev1.Service{}
	g.Expect(testClient.Get(ctx, types.NamespacedName{
		Name:      serviceName,
		Namespace: dpuService.GetNamespace(),
	}, service)).To(Succeed())

	// Validate metadata
	g.Expect(service.Name).To(Equal(serviceName))
	g.Expect(service.Namespace).To(Equal(dpuService.GetNamespace()))

	// Validate labels
	expectedLabels := map[string]string{
		dpuservicev1.DPUServiceNameLabelKey:                     dpuService.GetName(),
		dpuservicev1.DPUServiceNamespaceLabelKey:                dpuService.GetNamespace(),
		dpuservicev1.DPUServiceExposedPortForDPUClusterLabelKey: clusterName,
	}

	for key, expectedValue := range expectedLabels {
		g.Expect(service.Labels).To(HaveKeyWithValue(key, expectedValue))
	}

	// Validate owner reference
	g.Expect(service.OwnerReferences).To(HaveLen(1))
	ownerRef := service.OwnerReferences[0]
	g.Expect(ownerRef.APIVersion).To(Equal(dpuservicev1.DPUServiceGroupVersionKind.GroupVersion().String()))
	g.Expect(ownerRef.Kind).To(Equal(dpuservicev1.DPUServiceGroupVersionKind.Kind))
	g.Expect(ownerRef.Name).To(Equal(dpuService.GetName()))
	g.Expect(ownerRef.UID).To(Equal(dpuService.GetUID()))

	// Validate service type
	g.Expect(service.Spec.Type).To(Equal(expectedConfigPorts.ServiceType))

	// Validate ports
	g.Expect(service.Spec.Ports).To(HaveLen(len(expectedConfigPorts.Ports)))
	for i, expectedPort := range expectedConfigPorts.Ports {
		servicePort := service.Spec.Ports[i]
		g.Expect(servicePort.Name).To(Equal(expectedPort.Name))
		g.Expect(servicePort.Port).To(Equal(int32(expectedPort.Port)))
		g.Expect(servicePort.Protocol).To(Equal(expectedPort.Protocol))

		// Validate NodePort if ServiceType is NodePort
		if expectedConfigPorts.ServiceType == corev1.ServiceTypeNodePort {
			if expectedPort.NodePort != nil {
				g.Expect(servicePort.NodePort).To(Equal(int32(*expectedPort.NodePort)))
			} else {
				// If NodePort wasn't specified, it should be assigned automatically
				g.Expect(servicePort.NodePort).To(BeNumerically(">", 0))
			}
		}
	}

	// Validate selector (should be empty for headless services or when manually managing endpoints)
	if expectedConfigPorts.ServiceType == corev1.ClusterIPNone {
		g.Expect(service.Spec.ClusterIP).To(Equal("None"))
	}
}

func assertConfigPortEndpointSlice(g Gomega, testClient client.Client, dpuService *dpuservicev1.DPUService, expectedEndpointSliceEndpoint []discoveryv1.Endpoint, clusterName string) {
	endpointSliceName := getConfigPortName(clusterName, dpuService.GetName())
	endpointSlice := &discoveryv1.EndpointSlice{}
	g.Expect(testClient.Get(ctx, types.NamespacedName{
		Name:      endpointSliceName,
		Namespace: dpuService.GetNamespace(),
	}, endpointSlice)).To(Succeed())

	// Validate metadata
	g.Expect(endpointSlice.Name).To(Equal(endpointSliceName))
	g.Expect(endpointSlice.Namespace).To(Equal(dpuService.GetNamespace()))

	// Validate labels
	expectedLabels := map[string]string{
		dpuservicev1.DPUServiceNameLabelKey:                     dpuService.GetName(),
		dpuservicev1.DPUServiceNamespaceLabelKey:                dpuService.GetNamespace(),
		dpuservicev1.DPUServiceExposedPortForDPUClusterLabelKey: clusterName,
		discoveryv1.LabelManagedBy:                              dpuServiceControllerName,
		discoveryv1.LabelServiceName:                            endpointSliceName,
	}

	for key, expectedValue := range expectedLabels {
		g.Expect(endpointSlice.Labels).To(HaveKeyWithValue(key, expectedValue))
	}

	// Validate DPUService labels are copied
	for key, value := range dpuService.GetLabels() {
		g.Expect(endpointSlice.Labels).To(HaveKeyWithValue(key, value))
	}

	// Validate address type
	g.Expect(endpointSlice.AddressType).To(Equal(discoveryv1.AddressTypeIPv4))

	// Validate ports
	g.Expect(endpointSlice.Ports).To(HaveLen(len(dpuService.Spec.ConfigPorts.Ports)))
	for i, expectedPort := range dpuService.Spec.ConfigPorts.Ports {
		port := endpointSlice.Ports[i]
		g.Expect(*port.Name).To(Equal(expectedPort.Name))
		g.Expect(*port.Protocol).To(Equal(expectedPort.Protocol))
	}

	// Validate owner reference
	g.Expect(endpointSlice.OwnerReferences).To(HaveLen(1))
	ownerRef := endpointSlice.OwnerReferences[0]
	g.Expect(ownerRef.APIVersion).To(Equal(dpuservicev1.DPUServiceGroupVersionKind.GroupVersion().String()))
	g.Expect(ownerRef.Kind).To(Equal(dpuservicev1.DPUServiceGroupVersionKind.Kind))
	g.Expect(ownerRef.Name).To(Equal(dpuService.GetName()))
	g.Expect(ownerRef.UID).To(Equal(dpuService.GetUID()))

	// Validate endpoints
	g.Expect(endpointSlice.Endpoints).To(HaveLen(len(expectedEndpointSliceEndpoint)))
	g.Expect(endpointSlice.Endpoints).To(ConsistOf(expectedEndpointSliceEndpoint))
}

func assertConfigPortEndpoints(g Gomega, testClient client.Client, dpuService *dpuservicev1.DPUService, clusterName string) {
	endpointSliceName := getConfigPortName(clusterName, dpuService.GetName())
	endpoints := &corev1.Endpoints{}
	g.Expect(testClient.Get(ctx, types.NamespacedName{
		Name:      endpointSliceName,
		Namespace: dpuService.GetNamespace(),
	}, endpoints)).To(Succeed())

	// Validate metadata
	g.Expect(endpoints.Name).To(Equal(endpointSliceName))
	g.Expect(endpoints.Namespace).To(Equal(dpuService.GetNamespace()))

	// Get the corresponding EndpointSlice to compare
	endpointSlice := &discoveryv1.EndpointSlice{}
	g.Expect(testClient.Get(ctx, types.NamespacedName{
		Name:      endpointSliceName,
		Namespace: dpuService.GetNamespace(),
	}, endpointSlice)).To(Succeed())

	// Validate labels match EndpointSlice
	for key, value := range endpointSlice.Labels {
		// this label is not set by the controller on endpoints.
		if key == discoveryv1.LabelManagedBy {
			continue
		}
		g.Expect(endpoints.Labels).To(HaveKeyWithValue(key, value))
	}

	// Validate skip-mirror label is set.
	g.Expect(endpoints.Labels).To(HaveKeyWithValue(discoveryv1.LabelSkipMirror, "true"))

	// Validate owner reference
	g.Expect(endpoints.OwnerReferences).To(HaveLen(1))
	ownerRef := endpoints.OwnerReferences[0]
	g.Expect(ownerRef.APIVersion).To(Equal(dpuservicev1.DPUServiceGroupVersionKind.GroupVersion().String()))
	g.Expect(ownerRef.Kind).To(Equal(dpuservicev1.DPUServiceGroupVersionKind.Kind))
	g.Expect(ownerRef.Name).To(Equal(dpuService.GetName()))
	g.Expect(ownerRef.UID).To(Equal(dpuService.GetUID()))

	// Validate subsets structure
	if len(endpointSlice.Endpoints) > 0 && len(endpointSlice.Ports) > 0 {
		g.Expect(endpoints.Subsets).To(HaveLen(1))
		subset := endpoints.Subsets[0]

		// Validate ports conversion
		g.Expect(subset.Ports).To(HaveLen(len(endpointSlice.Ports)))
		for i, endpointSlicePort := range endpointSlice.Ports {
			endpointsPort := subset.Ports[i]
			g.Expect(endpointsPort.Port).To(Equal(*endpointSlicePort.Port))
			g.Expect(endpointsPort.Protocol).To(Equal(*endpointSlicePort.Protocol))
			if endpointSlicePort.Name != nil {
				g.Expect(endpointsPort.Name).To(Equal(*endpointSlicePort.Name))
			}
		}

		// Validate addresses conversion
		totalAddressCount := len(subset.Addresses) + len(subset.NotReadyAddresses)
		expectedAddressCount := 0
		for _, endpoint := range endpointSlice.Endpoints {
			expectedAddressCount += len(endpoint.Addresses)
		}
		g.Expect(totalAddressCount).To(Equal(expectedAddressCount))

		// Validate ready/not-ready address separation
		for _, endpoint := range endpointSlice.Endpoints {
			isReady := endpoint.Conditions.Ready != nil && *endpoint.Conditions.Ready
			for _, address := range endpoint.Addresses {
				if isReady {
					g.Expect(subset.Addresses).To(ContainElement(HaveField("IP", address)))
				} else {
					g.Expect(subset.NotReadyAddresses).To(ContainElement(HaveField("IP", address)))
				}
			}
		}
	} else {
		// If EndpointSlice has no endpoints or ports, Endpoints should have no subsets
		g.Expect(endpoints.Subsets).To(BeEmpty())
	}
}

func assertDPUServiceCondition(g Gomega, testClient client.Client, dpuServices []*dpuservicev1.DPUService) {
	for i := range dpuServices {
		gotDPUService := &dpuservicev1.DPUService{}
		g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuServices[i]), gotDPUService)).To(Succeed())
		g.Expect(gotDPUService.Status.Conditions).NotTo(BeNil())
		g.Expect(gotDPUService.Status.Conditions).To(ConsistOf(
			And(
				HaveField("Type", string(conditions.TypeReady)),
				HaveField("Status", metav1.ConditionFalse),
				HaveField("Reason", string(conditions.ReasonPending)),
				HaveField("Message", conditions.ReadyConditionMessage(conditions.MessageNotReady, []string{"ApplicationsReady"})),
			),
			And(
				HaveField("Type", string(dpuservicev1.ConditionApplicationPrereqsReconciled)),
				HaveField("Status", metav1.ConditionTrue),
				HaveField("Reason", string(conditions.ReasonSuccess)),
			),
			And(
				HaveField("Type", string(dpuservicev1.ConditionApplicationsReconciled)),
				HaveField("Status", metav1.ConditionTrue),
				HaveField("Reason", string(conditions.ReasonSuccess)),
			),
			// Argo can not deploy anything on the DPUs during unit tests.
			And(
				HaveField("Type", string(dpuservicev1.ConditionApplicationsReady)),
				HaveField("Status", metav1.ConditionFalse),
				HaveField("Reason", string(conditions.ReasonPending)),
			),
			And(
				HaveField("Type", string(dpuservicev1.ConditionDPUServiceInterfaceReconciled)),
				HaveField("Status", metav1.ConditionTrue),
				HaveField("Reason", string(conditions.ReasonSuccess)),
			),
			And(
				HaveField("Type", string(dpuservicev1.ConditionConfigPortsReconciled)),
				HaveField("Status", metav1.ConditionTrue),
				HaveField("Reason", string(conditions.ReasonSuccess)),
			),
		))
	}
}

func assertArgoCDSecrets(g Gomega, testClient client.Client, clusters []provisioningv1.DPUCluster, cleanupObjs *[]client.Object, testNamespaces []string) {
	selector := labels.NewSelector()
	req1, _ := labels.NewRequirement(argoCDSecretLabelKey, selection.Equals, []string{"cluster"})
	selector = selector.Add(*req1)
	req2, _ := labels.NewRequirement(operatorv1.DPFComponentLabelKey, selection.Equals, []string{"dpuservice-manager"})
	selector = selector.Add(*req2)
	req3, _ := labels.NewRequirement(provisioningv1.DPUClusterNamespaceLabelKey, selection.In, testNamespaces)
	selector = selector.Add(*req3)

	argoSecrets := &corev1.SecretList{}
	err := testClient.List(ctx, argoSecrets,
		client.InNamespace("dpf-operator-system"),
		client.MatchingLabelsSelector{Selector: selector},
	)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(argoSecrets.Items).To(HaveLen(len(clusters)))
	for _, s := range argoSecrets.Items {
		// Assert each secret contains the required keys in Data.
		for _, key := range []string{"config", "name", "server"} {
			if _, ok := s.Data[key]; !ok {
				g.Expect(s.Data).To(HaveKey(key))
			}
		}
		g.Expect(s.OwnerReferences).To(HaveLen(1))
		g.Expect(s.OwnerReferences[0].Name).To(Equal(operatorcontroller.DefaultDPFOperatorConfigSingletonName))
		g.Expect(s.OwnerReferences[0].Kind).To(Equal(operatorv1.DPFOperatorConfigKind))
		*cleanupObjs = append(*cleanupObjs, s.DeepCopy())
	}

}

func assertAppProject(g Gomega, testClient client.Client, clusters []provisioningv1.DPUCluster) {
	// Check that the DPU cluster argo project has been created.
	appProject := &argov1.AppProject{}
	g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace, Name: dpuAppProjectName}, appProject)).To(Succeed())
	g.Expect(appProject.OwnerReferences).To(HaveLen(1))
	g.Expect(appProject.OwnerReferences[0].Name).To(Equal(operatorcontroller.DefaultDPFOperatorConfigSingletonName))
	g.Expect(appProject.OwnerReferences[0].Kind).To(Equal(operatorv1.DPFOperatorConfigKind))

	gotDestinations := appProject.Spec.Destinations
	g.Expect(gotDestinations).To(HaveLen(len(clusters)))
	expectedDestinations := []argov1.ApplicationDestination{}
	for _, c := range clusters {
		expectedDestinations = append(expectedDestinations, argov1.ApplicationDestination{
			Name:      c.Name,
			Namespace: "*",
		})
	}
	g.Expect(gotDestinations).To(ConsistOf(expectedDestinations))
	g.Expect(appProject.GetOwnerReferences()[0].Name).To(Equal(operatorcontroller.DefaultDPFOperatorConfigSingletonName))

	// Check that the host argo project has been created.
	g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace, Name: hostAppProjectName}, appProject)).To(Succeed())
	gotDestinations = appProject.Spec.Destinations
	expectedDestinations = []argov1.ApplicationDestination{
		{
			Name:      "in-cluster",
			Namespace: "*",
		}}
	g.Expect(gotDestinations).To(ConsistOf(expectedDestinations))
}

func assertDPUServiceAnnotationsClean(g Gomega, testClient client.Client, dpuServices []*dpuservicev1.DPUService) {
	for _, dpuService := range dpuServices {
		interfaces := dpuService.Spec.Interfaces
		for _, name := range interfaces {
			dsi := &dpuservicev1.DPUServiceInterface{}
			g.Expect(testClient.Get(ctx, types.NamespacedName{Name: name, Namespace: dpuService.Namespace}, dsi)).To(Succeed())
			g.Expect(dsi.GetAnnotations()[dpuservicev1.DPUServiceInterfaceAnnotationKey]).To(Not(Equal(dpuService.Name)))
		}
	}
}

func assertApplicationPaused(g Gomega, testClient client.Client, dpuServices []*dpuservicev1.DPUService, clusters []provisioningv1.DPUCluster) {
	applications := &argov1.ApplicationList{}
	g.Expect(testClient.List(ctx, applications)).To(Succeed())

	// Check that self-heal is disabled for all applications.
	g.Expect(applications.Items).To(HaveLen(len(clusters)*len(dpuServices) + 1))
	for _, app := range applications.Items {
		skipVal, ok := app.Annotations[annotationKeyAppSkipReconcile]
		g.Expect(ok).To(BeTrue())
		g.Expect(skipVal).To(Equal("true"))
	}
}

func getApplicationServiceDaemonSetValues(g Gomega, dpuService *dpuservicev1.DPUService) *dpuservicev1.ServiceDaemonSetValues {
	applications := &argov1.ApplicationList{}
	g.Expect(testClient.List(ctx, applications, client.MatchingLabels{
		dpuservicev1.DPUServiceNameLabelKey:      dpuService.Name,
		dpuservicev1.DPUServiceNamespaceLabelKey: dpuService.Namespace,
	})).To(Succeed())
	g.Expect(applications.Items).To(HaveLen(1))
	for _, app := range applications.Items {
		g.Expect(app.Labels).To(HaveKey(dpuservicev1.DPUServiceNameLabelKey))
		g.Expect(app.Labels).To(HaveKey(dpuservicev1.DPUServiceNamespaceLabelKey))
		dpuServiceName := app.Labels[dpuservicev1.DPUServiceNameLabelKey]
		dpuServiceNS := app.Labels[dpuservicev1.DPUServiceNamespaceLabelKey]
		g.Expect(dpuServiceName).To(Equal(dpuService.Name))
		g.Expect(dpuServiceNS).To(Equal(dpuService.Namespace))
		// Check that the values fields are set as expected.
		var appValuesMap map[string]any
		g.Expect(json.Unmarshal(app.Spec.Source.Helm.ValuesObject.Raw, &appValuesMap)).To(Succeed())
		// Expect values to be correctly transposed from the DPUService serviceDaemonSet values.
		Expect(appValuesMap).To(HaveKey("serviceDaemonSet"))
		var appServiceDaemonSet = struct {
			ServiceDaemonSet dpuservicev1.ServiceDaemonSetValues `json:"serviceDaemonSet"`
		}{}
		Expect(json.Unmarshal(app.Spec.Source.Helm.ValuesObject.Raw, &appServiceDaemonSet)).To(Succeed())
		appService := appServiceDaemonSet.ServiceDaemonSet
		return &appService
	}
	return nil
}

func assertApplication(g Gomega, testClient client.Client, testNS string,
	dpuServices []*dpuservicev1.DPUService, dpuServiceInterfaces []*dpuservicev1.DPUServiceInterface, clusters []provisioningv1.DPUCluster) {
	// Check that argoApplications are created for each of the clusters.
	applications := &argov1.ApplicationList{}
	g.Expect(testClient.List(ctx, applications, client.MatchingLabels{
		dpuservicev1.DPUServiceNamespaceLabelKey: testNS,
	})).To(Succeed())

	// Check that we have one application for each cluster and dpuService and an additional application for the hostDPUService.
	g.Expect(applications.Items).To(HaveLen(len(clusters)*len(dpuServices) + 1))
	for _, app := range applications.Items {
		g.Expect(app.Labels).To(HaveKey(dpuservicev1.DPUServiceNameLabelKey))
		g.Expect(app.Labels).To(HaveKey(dpuservicev1.DPUServiceNamespaceLabelKey))
		dpuServiceName := app.Labels[dpuservicev1.DPUServiceNameLabelKey]
		dpuServiceNS := app.Labels[dpuservicev1.DPUServiceNamespaceLabelKey]
		for _, service := range dpuServices {
			if service.Name != dpuServiceName || service.Namespace != dpuServiceNS {
				continue
			}
			// Check the helm fields are set as expected.
			g.Expect(app.Spec.Source.Chart).To(Equal(service.Spec.HelmChart.Source.Chart))
			g.Expect(app.Spec.Source.Path).To(Equal(service.Spec.HelmChart.Source.Path))
			g.Expect(app.Spec.Source.RepoURL).To(Equal(service.Spec.HelmChart.Source.GetArgoRepoURL()))
			g.Expect(app.Spec.Source.TargetRevision).To(Equal(service.Spec.HelmChart.Source.Version))
			g.Expect(app.Spec.Source.Helm.ReleaseName).To(Equal(service.Spec.HelmChart.Source.ReleaseName))

			// check application reconciliation isn't skipped
			_, ok := app.Annotations[annotationKeyAppSkipReconcile]
			g.Expect(ok).To(BeFalse())

			// If the DPUService doesn't define a ServiceDaemonSet the below assertions are not applicable.
			if service.Spec.ServiceDaemonSet == nil {
				return
			}

			// Check that the values fields are set as expected.
			var appValuesMap, serviceValuesMap map[string]interface{}
			g.Expect(json.Unmarshal(app.Spec.Source.Helm.ValuesObject.Raw, &appValuesMap)).To(Succeed())

			// Expect values to be correctly transposed from the DPUService serviceDaemonSet values.
			Expect(appValuesMap).To(HaveKey("serviceDaemonSet"))
			var appServiceDaemonSet = struct {
				ServiceDaemonSet dpuservicev1.ServiceDaemonSetValues `json:"serviceDaemonSet"`
			}{}
			Expect(json.Unmarshal(app.Spec.Source.Helm.ValuesObject.Raw, &appServiceDaemonSet)).To(Succeed())
			appService := appServiceDaemonSet.ServiceDaemonSet
			for k, v := range service.Spec.ServiceDaemonSet.Labels {
				Expect(appService.Labels).To(HaveKeyWithValue(k, v))
			}
			Expect(appService.Labels).To(HaveKeyWithValue(dpuservicev1.DPFServiceIDLabelKey, *service.Spec.ServiceID))

			m := map[string]*dpuservicev1.DPUServiceInterface{}
			for _, dpuServiceInterface := range dpuServiceInterfaces {
				m[dpuServiceInterface.Name] = dpuServiceInterface
			}
			annotations, err := updateAnnotationsWithNetworks(service, m)
			Expect(err).ToNot(HaveOccurred())
			annotations[networkAnnotationKey] = mergeWithInvalidNetwork(annotations[networkAnnotationKey])
			Expect(appService.Annotations).To(Equal(annotations))

			Expect(appService.NodeSelector).To(Equal(service.Spec.ServiceDaemonSet.NodeSelector))
			Expect(appService.UpdateStrategy).To(Equal(service.Spec.ServiceDaemonSet.UpdateStrategy))

			// If this field is unset skip this assertion.
			if service.Spec.HelmChart.Values == nil {
				continue
			}

			// Expect every value passed in the service spec `.values` to be set in the application helm valuesObject.
			g.Expect(json.Unmarshal(service.Spec.HelmChart.Values.Raw, &serviceValuesMap)).To(Succeed())
			for k, v := range serviceValuesMap {
				g.Expect(appValuesMap).To(HaveKeyWithValue(k, v))
			}
		}
	}
}

func getDPUServiceWithoutHelmchartValues(testNamespace string) []*dpuservicev1.DPUService {
	return []*dpuservicev1.DPUService{
		{ObjectMeta: metav1.ObjectMeta{GenerateName: "dpu-one-", Namespace: testNamespace},
			Spec: dpuservicev1.DPUServiceSpec{
				HelmChart: dpuservicev1.HelmChart{
					Source: dpuservicev1.ApplicationSource{
						RepoURL:     "oci://repository.com",
						Version:     "v1.1",
						Chart:       "first-chart",
						ReleaseName: "release-one",
					},
				},
				ServiceID: ptr.To("service-one"),
				ServiceDaemonSet: &dpuservicev1.ServiceDaemonSetValues{
					NodeSelector: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							{
								MatchExpressions: []corev1.NodeSelectorRequirement{
									{
										Key:      "key",
										Operator: "Exists",
									},
								},
							},
						},
					},
					UpdateStrategy: &appsv1.DaemonSetUpdateStrategy{
						Type: appsv1.RollingUpdateDaemonSetStrategyType,
						RollingUpdate: &appsv1.RollingUpdateDaemonSet{
							MaxUnavailable: &intstr.IntOrString{
								Type:   0,
								IntVal: 0,
								StrVal: "",
							},
							MaxSurge: &intstr.IntOrString{
								Type:   0,
								IntVal: 0,
								StrVal: "",
							},
						},
					},
					Labels: map[string]string{
						"label-one": "label-value",
					},
					Annotations: map[string]string{
						"annotation-one": "annotation",
					},
				},
				Interfaces: []string{"dpu-service-interface"},
			},
		},
	}
}

func getMinimalDPUServices(testNamespace string) []*dpuservicev1.DPUService {
	return []*dpuservicev1.DPUService{
		{ObjectMeta: metav1.ObjectMeta{GenerateName: "dpu-one-", Namespace: testNamespace},
			Spec: dpuservicev1.DPUServiceSpec{
				HelmChart: dpuservicev1.HelmChart{
					Source: dpuservicev1.ApplicationSource{
						RepoURL:     "oci://repository.com",
						Version:     "v1.1",
						Chart:       "first-chart",
						ReleaseName: "release-one",
					},
					Values: &runtime.RawExtension{
						Object: &unstructured.Unstructured{
							Object: map[string]interface{}{
								"value": "one",
								"other": "two",
							},
						},
					},
				},
				ServiceID: ptr.To("service-one"),
				ServiceDaemonSet: &dpuservicev1.ServiceDaemonSetValues{
					NodeSelector: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							{
								MatchExpressions: []corev1.NodeSelectorRequirement{
									{
										Key:      "key",
										Operator: corev1.NodeSelectorOpExists,
									},
								},
							},
						},
					},
					UpdateStrategy: &appsv1.DaemonSetUpdateStrategy{
						Type: appsv1.RollingUpdateDaemonSetStrategyType,
						RollingUpdate: &appsv1.RollingUpdateDaemonSet{
							MaxUnavailable: &intstr.IntOrString{
								Type:   0,
								IntVal: 0,
								StrVal: "",
							},
							MaxSurge: &intstr.IntOrString{
								Type:   0,
								IntVal: 0,
								StrVal: "",
							},
						},
					},
					Labels: map[string]string{
						"label-one": "label-value",
					},
					Annotations: map[string]string{
						"annotation-one": "annotation",
					},
				},
				Interfaces: []string{"dpu-service-interface"},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "dpu-two", Namespace: testNamespace},
			Spec: dpuservicev1.DPUServiceSpec{
				HelmChart: dpuservicev1.HelmChart{
					Source: dpuservicev1.ApplicationSource{
						RepoURL:     "oci://repository.com",
						Version:     "v1.2",
						Chart:       "second-chart",
						ReleaseName: "release-two",
					},
				},
			},
		},
	}
}

var _ = Describe("test DPUService reconciler step-by-step", func() {
	Context("When reconciling", func() {
		var (
			testConfig *operatorv1.DPFOperatorConfig
			testNS     *corev1.Namespace
		)
		BeforeEach(func() {
			By("creating the namespaces")
			testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "testns-"}}
			Expect(testClient.Create(ctx, testNS)).To(Succeed())
			// Create the DPF System Namespace
			err := testClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace}})
			if !apierrors.IsAlreadyExists(err) {
				Expect(err).ToNot(HaveOccurred())
			}
			// Apply and get the DPFOperatorConfig. There is a race condition between the separate test runs why we have to fetch the config.
			// A real config is necessary to run our reconcileArgoSecrets tests.
			if testConfig == nil {
				testConfig = getMinimalDPFOperatorConfig()
				Expect(client.IgnoreAlreadyExists(testClient.Create(ctx, testConfig))).To(Succeed())
			}
			Expect(testClient.Get(ctx, client.ObjectKeyFromObject(testConfig), testConfig)).To(Succeed())

			By("Cleanup the control plane and argoCD secrets")
			secretList := &corev1.SecretList{}
			objs := []client.Object{}

			// Delete all the secrets.
			Expect(testClient.List(ctx, secretList)).To(Succeed())
			for _, s := range secretList.Items {
				objs = append(objs, s.DeepCopy())
			}
			Expect(testutils.CleanupAndWait(ctx, testClient, objs...)).To(Succeed())
		})
		AfterEach(func() {
			By("Cleanup the test Namespace")
			Expect(testClient.Delete(ctx, testNS)).To(Succeed())
			By("Cleanup the control plane and argoCD secrets")
			secretList := &corev1.SecretList{}
			objs := []client.Object{}

			// Delete all the secrets.
			Expect(testClient.List(ctx, secretList)).To(Succeed())
			for _, s := range secretList.Items {
				objs = append(objs, s.DeepCopy())
			}
			Expect(testutils.CleanupAndWait(ctx, testClient, objs...)).To(Succeed())
		})

		// reconcileArgoSecrets
		It("should create an Argo secret based on the admin-kubeconfig for each cluster", func() {
			clusters := []provisioningv1.DPUCluster{
				testutils.GetTestDPUCluster(testNS.Name, "cluster-one"),
				testutils.GetTestDPUCluster(testNS.Name, "cluster-two"),
				testutils.GetTestDPUCluster(testNS.Name, "cluster-three"),
			}

			secrets := []*corev1.Secret{}
			for _, cluster := range clusters {
				kamajiSecret, err := testutils.GetFakeKamajiClusterSecretFromEnvtest(cluster, cfg)
				Expect(err).ToNot(HaveOccurred())
				secrets = append(secrets, kamajiSecret)
			}
			for _, s := range secrets {
				Expect(testClient.Create(ctx, s)).To(Succeed())
			}

			dpuClusterConfigs := clusterConfigs(testClient, clusters)
			r := &DPUServiceReconciler{Client: testClient, Scheme: testClient.Scheme()}
			err := r.reconcileArgoSecrets(ctx, dpuClusterConfigs, testConfig)
			Expect(err).NotTo(HaveOccurred())
			secretList := &corev1.SecretList{}
			Expect(testClient.List(ctx, secretList, client.HasLabels{argoCDSecretLabelKey, provisioningv1.DPUClusterNameLabelKey, provisioningv1.DPUClusterNamespaceLabelKey})).To(Succeed())
			Expect(secretList.Items).To(HaveLen(3))
			for _, s := range secretList.Items {
				Expect(s.Data).To(HaveKey("config"))
				Expect(s.Data).To(HaveKey("name"))
				Expect(s.Data).To(HaveKey("server"))
			}
		})
		It("should create secrets for existing clusters when one cluster does not exist", func() {
			clusters := []provisioningv1.DPUCluster{
				testutils.GetTestDPUCluster(testNS.Name, "cluster-four"),
				testutils.GetTestDPUCluster(testNS.Name, "cluster-five"),
				testutils.GetTestDPUCluster(testNS.Name, "cluster-six"),
			}
			secrets := []*corev1.Secret{}
			for _, cluster := range clusters {
				// Not creating a kamaji secret for this cluster.
				if cluster.Name == "cluster-six" {
					continue
				}
				kamajiSecret, err := testutils.GetFakeKamajiClusterSecretFromEnvtest(cluster, cfg)
				Expect(err).ToNot(HaveOccurred())
				secrets = append(secrets, kamajiSecret)
			}
			for _, s := range secrets {
				Expect(testClient.Create(ctx, s)).To(Succeed())
			}

			dpuClusterConfigs := clusterConfigs(testClient, clusters)
			r := &DPUServiceReconciler{Client: testClient, Scheme: testClient.Scheme()}
			err := r.reconcileArgoSecrets(ctx, dpuClusterConfigs, testConfig)
			// Expect an error to be reported.
			Expect(err).To(HaveOccurred())

			// Expect reconciliation to have continued and created the other secrets.
			secretList := &corev1.SecretList{}
			Expect(testClient.List(ctx, secretList, client.HasLabels{argoCDSecretLabelKey, provisioningv1.DPUClusterNameLabelKey, provisioningv1.DPUClusterNamespaceLabelKey})).To(Succeed())
			Expect(secretList.Items).To(HaveLen(2))
			for _, s := range secretList.Items {
				Expect(s.Data).To(HaveKey("config"))
				Expect(s.Data).To(HaveKey("name"))
				Expect(s.Data).To(HaveKey("server"))
			}
		})
		It("should create secrets for existing clusters when one cluster secret is malformed", func() {
			clusters := []provisioningv1.DPUCluster{
				testutils.GetTestDPUCluster(testNS.Name, "cluster-seven"),
				testutils.GetTestDPUCluster(testNS.Name, "cluster-eight"),
				testutils.GetTestDPUCluster(testNS.Name, "cluster-nine"),
			}
			secrets := []*corev1.Secret{}
			for _, cluster := range clusters {
				kamajiSecret, err := testutils.GetFakeKamajiClusterSecretFromEnvtest(cluster, cfg)
				Expect(err).ToNot(HaveOccurred())
				// the third secret is malformed.
				if cluster.Name == "cluster-nine" {
					kamajiSecret.Data["super-admin.conf"] = []byte("just-a-field")
				}
				secrets = append(secrets, kamajiSecret)
			}
			for _, s := range secrets {
				Expect(testClient.Create(ctx, s)).To(Succeed())
			}

			dpuClusterConfigs := clusterConfigs(testClient, clusters)
			r := &DPUServiceReconciler{Client: testClient, Scheme: testClient.Scheme()}
			err := r.reconcileArgoSecrets(ctx, dpuClusterConfigs, testConfig)
			// Expect an error to be reported.
			Expect(err).To(HaveOccurred())

			// Expect reconciliation to have continued and created the other secrets.
			secretList := &corev1.SecretList{}
			Expect(testClient.List(ctx, secretList, client.HasLabels{argoCDSecretLabelKey, provisioningv1.DPUClusterNameLabelKey, provisioningv1.DPUClusterNamespaceLabelKey})).To(Succeed())
			Expect(secretList.Items).To(HaveLen(2))
			for _, s := range secretList.Items {
				Expect(s.Data).To(HaveKey("config"))
				Expect(s.Data).To(HaveKey("name"))
				Expect(s.Data).To(HaveKey("server"))
			}
		})
		It("should reconcile image pull secrets created with the correct labels", func() {
			// Create a fake Kamaji cluster using the envtest cluster
			dpuCluster := testutils.GetTestDPUCluster(testNS.Name, "cluster-seven")
			secret, err := testutils.GetFakeKamajiClusterSecretFromEnvtest(dpuCluster, cfg)
			Expect(err).NotTo(HaveOccurred())
			Expect(testClient.Create(ctx, secret)).To(Succeed())

			// Create some secrets that should be mirrored to the DPUCluster.
			labels := map[string]string{dpuservicev1.DPFImagePullSecretLabelKey: ""}
			Expect(testClient.Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "dpf-secret-one", Namespace: testNS.Name, Labels: labels}})).To(Succeed())
			Expect(testClient.Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "dpf-secret-two", Namespace: testNS.Name, Labels: labels}})).To(Succeed())

			// Create a secret that should not be mirrored as it does not have the correct labels.
			Expect(testClient.Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "not-a-dpf-secret", Namespace: testNS.Name}})).To(Succeed())

			// Create a secret that should not be mirrored as it does not have the correct namespace.
			Expect(testClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "anothernamespace"}})).To(Succeed())
			Expect(testClient.Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "another-not-a-dpf-secret", Namespace: "anothernamespace"}})).To(Succeed())

			// Reconcile a DPUService in a different namespace and check to see that it has been cloned.
			cloningNamespace := "namespace-to-clone-to"
			Expect(testClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: cloningNamespace}})).To(Succeed())
			dpuService := &dpuservicev1.DPUService{ObjectMeta: metav1.ObjectMeta{Name: "name", Namespace: cloningNamespace}}
			r := &DPUServiceReconciler{Client: testClient, Scheme: testClient.Scheme()}
			dpuClusterConfig := dpucluster.NewConfig(testClient, &dpuCluster)
			Expect(r.reconcileImagePullSecrets(ctx, []*dpucluster.Config{dpuClusterConfig}, dpuService)).To(Succeed())

			// Check we have the correct secrets cloned to the intended namespace
			gotSecrets := &corev1.SecretList{}
			Expect(testClient.List(ctx, gotSecrets, client.InNamespace(cloningNamespace))).To(Succeed())
			for _, gotSecret := range gotSecrets.Items {
				Expect(gotSecret.Name).To(BeElementOf([]string{"dpf-secret-one", "dpf-secret-two"}))
			}

			// ImagePullSecrets should be cleaned up when all DPUServices have been deleted.
			Eventually(func(g Gomega) {
				_, err := r.reconcileDelete(ctx, &dpuservicev1.DPUService{ObjectMeta: metav1.ObjectMeta{Name: "name", Namespace: cloningNamespace}})
				g.Expect(err).NotTo(HaveOccurred())

				g.Expect(testClient.List(ctx, gotSecrets, client.InNamespace(cloningNamespace))).To(Succeed())
				g.Expect(gotSecrets.Items).To(BeEmpty())
			}).WithTimeout(10 * time.Second).Should(Succeed())
		})
	})
})

var _ = Describe("unit test DPUService functions", func() {
	Context("When testing argoCDValuesFromDPUService", func() {
		DescribeTable("behaves as expected", func(serviceID *string, values string, serviceDaemonSetValues *dpuservicev1.ServiceDaemonSetValues, expectedValues string) {
			dpuService := &dpuservicev1.DPUService{
				Spec: dpuservicev1.DPUServiceSpec{
					ServiceID: serviceID,
					HelmChart: dpuservicev1.HelmChart{
						Values: &runtime.RawExtension{Raw: []byte(values)},
					},
					ServiceDaemonSet: serviceDaemonSetValues,
				},
			}

			o, err := argoCDValuesFromDPUService(serviceDaemonSetValues, dpuService, *serviceID)
			Expect(err).ToNot(HaveOccurred())
			Expect(o.Raw).To(BeEquivalentTo([]byte(expectedValues)))
		},
			Entry("no values no servicedaemonset",
				ptr.To("someservice"),
				`{}`,
				nil,
				`{"serviceDaemonSet":{"labels":{"svc.dpu.nvidia.com/service":"someservice"}}}`,
			),
			Entry("values but no serviceDaemonSet specified",
				ptr.To("someservice"),
				`{"key":"value"}`,
				nil,
				`{"key":"value","serviceDaemonSet":{"labels":{"svc.dpu.nvidia.com/service":"someservice"}}}`,
			),
			Entry("values with serviceDaemonSet specified",
				ptr.To("someservice"),
				`{"key":"value"}`,
				&dpuservicev1.ServiceDaemonSetValues{
					Annotations: map[string]string{
						"some": "annotation",
					},
				},
				`{"key":"value","serviceDaemonSet":{"annotations":{"some":"annotation"},"labels":{"svc.dpu.nvidia.com/service":"someservice"}}}`,
			),
			Entry("values that have serviceDaemonSet overrides with serviceDaemonSet specified",
				ptr.To("someservice"),
				`{"serviceDaemonSet":{"annotations":{"diff":"annotation"},"labels":{"some":"label"},"updateStrategy":{"type":"RollingUpdate"}}}`,
				&dpuservicev1.ServiceDaemonSetValues{
					Annotations: map[string]string{
						"some": "annotation",
					},
					UpdateStrategy: &appsv1.DaemonSetUpdateStrategy{
						Type: appsv1.OnDeleteDaemonSetStrategyType,
					},
				},
				`{"serviceDaemonSet":{"annotations":{"diff":"annotation","some":"annotation"},"labels":{"some":"label","svc.dpu.nvidia.com/service":"someservice"},"updateStrategy":{"type":"OnDelete"}}}`,
			),
		)
	})
})

func getMinimalDPFOperatorConfig() *operatorv1.DPFOperatorConfig {
	return &operatorv1.DPFOperatorConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      operatorcontroller.DefaultDPFOperatorConfigSingletonName,
			Namespace: operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace,
		},
		Spec: operatorv1.DPFOperatorConfigSpec{
			ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{
				BFBPersistentVolumeClaimName: "name",
			},
		},
	}
}

func getDPUServiceNADWithSpec(namespace string) *dpuservicev1.DPUServiceNAD {
	return &dpuservicev1.DPUServiceNAD{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "mynad",
			Namespace:   namespace,
			Labels:      map[string]string{"labelTest": "labelTestValue"},
			Annotations: map[string]string{"annotTest": "annotTestValue"},
		},
		Spec: dpuservicev1.DPUServiceNADSpec{
			ResourceType: "sf",
			Bridge:       "test-ovsbridge",
			ServiceMTU:   1500,
			IPAM:         true,
		},
	}
}

func getMinimalDPUServiceInterfacewithCustomNAD(namespace string) *dpuservicev1.DPUServiceInterface {
	return &dpuservicev1.DPUServiceInterface{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dpu-service-interface-with-custom-nad",
			Namespace: namespace,
		},
		Spec: dpuservicev1.DPUServiceInterfaceSpec{
			Template: dpuservicev1.ServiceInterfaceSetSpecTemplate{
				Spec: dpuservicev1.ServiceInterfaceSetSpec{
					Template: dpuservicev1.ServiceInterfaceSpecTemplate{
						Spec: dpuservicev1.ServiceInterfaceSpec{
							InterfaceType: dpuservicev1.InterfaceTypeService,
							Service: &dpuservicev1.ServiceDef{
								ServiceID:     "service-one",
								Network:       "mynad",
								InterfaceName: "net1",
							},
						},
					},
				},
			},
		},
	}
}

func getMinimalDPUServiceInterface(namespace string) *dpuservicev1.DPUServiceInterface {
	return &dpuservicev1.DPUServiceInterface{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dpu-service-interface",
			Namespace: namespace,
		},
		Spec: dpuservicev1.DPUServiceInterfaceSpec{
			Template: dpuservicev1.ServiceInterfaceSetSpecTemplate{
				Spec: dpuservicev1.ServiceInterfaceSetSpec{
					Template: dpuservicev1.ServiceInterfaceSpecTemplate{
						Spec: dpuservicev1.ServiceInterfaceSpec{
							InterfaceType: dpuservicev1.InterfaceTypeService,
							Service: &dpuservicev1.ServiceDef{
								ServiceID:     "service-one",
								Network:       "my-namespace/mybrsfc",
								InterfaceName: "net1",
							},
						},
					},
				},
			},
		},
	}
}

func getExposedService(labels map[string]string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:         "test-service",
			Namespace:    "default",
			GenerateName: "svc-",
			Labels:       labels,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:     "port-one",
					Port:     8080,
					Protocol: corev1.ProtocolTCP,
				},
			},
			Type: corev1.ServiceTypeNodePort,
		},
	}
}

func clusterConfigs(c client.Client, clusters []provisioningv1.DPUCluster) []*dpucluster.Config {
	clusterConfigs := make([]*dpucluster.Config, 0, len(clusters))
	for _, cluster := range clusters {
		clusterConfig := dpucluster.NewConfig(c, &cluster)
		clusterConfigs = append(clusterConfigs, clusterConfig)
	}
	return clusterConfigs
}

func Test_dpuNodePortsToMap(t *testing.T) {
	type args struct {
		dpuService  *dpuservicev1.DPUService
		serviceList *corev1.ServiceList
	}
	tests := []struct {
		name    string
		args    args
		want    map[string]int32
		wantErr bool
	}{
		{
			name: "Expect 1 service with 1 port, but got 0",
			args: args{
				dpuService: &dpuservicev1.DPUService{
					Spec: dpuservicev1.DPUServiceSpec{
						ConfigPorts: &dpuservicev1.ConfigPorts{
							ServiceType: corev1.ServiceTypeNodePort,
							Ports: []dpuservicev1.ConfigPort{
								{Name: "port1", Port: 80},
							},
						},
					},
				},
				serviceList: &corev1.ServiceList{},
			},
			wantErr: true,
		},
		{
			name: "Expect 1 service with 1 port, got 1",
			args: args{
				dpuService: &dpuservicev1.DPUService{
					Spec: dpuservicev1.DPUServiceSpec{
						ConfigPorts: &dpuservicev1.ConfigPorts{
							ServiceType: corev1.ServiceTypeNodePort,
							Ports: []dpuservicev1.ConfigPort{
								{Name: "port1", Port: 80},
							},
						},
					},
				},
				serviceList: &corev1.ServiceList{
					Items: []corev1.Service{
						{
							Spec: corev1.ServiceSpec{
								Type: corev1.ServiceTypeNodePort,
								Ports: []corev1.ServicePort{
									{Name: "port1", NodePort: 30000},
								},
							},
						},
					},
				},
			},
			want: map[string]int32{
				"port1": 30000,
			},
			wantErr: false,
		},
		{
			name: "2 ports configured, expect 2",
			args: args{
				dpuService: &dpuservicev1.DPUService{
					Spec: dpuservicev1.DPUServiceSpec{
						ConfigPorts: &dpuservicev1.ConfigPorts{
							ServiceType: corev1.ServiceTypeNodePort,
							Ports: []dpuservicev1.ConfigPort{
								{Name: "port1", Port: 80},
								{Name: "port2", Port: 81},
							},
						},
					},
				},
				serviceList: &corev1.ServiceList{
					Items: []corev1.Service{
						{
							Spec: corev1.ServiceSpec{
								Type: corev1.ServiceTypeNodePort,
								Ports: []corev1.ServicePort{
									{Name: "port1", NodePort: 30000},
									{Name: "port2", NodePort: 30001},
								},
							},
						},
					},
				},
			},
			want: map[string]int32{
				"port1": 30000,
				"port2": 30001,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := dpuNodePortsToMap(tt.args.dpuService, tt.args.serviceList)
			if (err != nil) != tt.wantErr {
				t.Errorf("dpuNodePortsToMap() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("dpuNodePortsToMap() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func newTestDPUService(name, namespace string) *dpuservicev1.DPUService {
	return &dpuservicev1.DPUService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: dpuservicev1.DPUServiceSpec{
			HelmChart: dpuservicev1.HelmChart{
				Source: dpuservicev1.ApplicationSource{
					RepoURL: "oci://repository.com",
					Version: "v1.1",
					Chart:   "chart",
				},
			},
			ConfigPorts: &dpuservicev1.ConfigPorts{
				ServiceType: corev1.ServiceTypeNodePort,
				Ports: []dpuservicev1.ConfigPort{
					{
						Name:     "port1",
						Protocol: corev1.ProtocolTCP,
						Port:     80,
					},
				},
			},
		},
	}
}

func newTestNode(name, hostNodeName string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				provisioningv1.DPUNodeNameLabel:      hostNodeName,
				provisioningv1.DPUNodeNamespaceLabel: "test-namespace",
			},
		},
		Status: corev1.NodeStatus{
			Addresses: []corev1.NodeAddress{
				{Type: corev1.NodeInternalIP, Address: "1.1.1.1"},
			},
		},
	}
}

var _ = Describe("nodeEventHandler", func() {
	var (
		handler      *nodeEventHandler
		queue        workqueue.TypedRateLimitingInterface[ctrl.Request]
		hostNodeName string
	)

	// createAndWait creates the given object and waits until it can be retrieved.
	// This is necessary to avoid race conditions between the creation and the nodeEventHandler processing.
	createAndWait := func(ctx context.Context, c client.Client, obj client.Object) {
		Expect(c.Create(ctx, obj)).To(Succeed())
		Eventually(func(g Gomega) {
			err := c.Get(ctx, client.ObjectKeyFromObject(obj), obj)
			g.Expect(err).NotTo(HaveOccurred())
		}).WithTimeout(10 * time.Second).Should(Succeed())
	}

	BeforeEach(func() {
		testNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace}}
		Expect(client.IgnoreAlreadyExists(testClient.Create(ctx, testNS))).To(Succeed())
		dpfOperatorConfig := getMinimalDPFOperatorConfig()
		Expect(client.IgnoreAlreadyExists(testClient.Create(ctx, dpfOperatorConfig))).To(Succeed())
		DeferCleanup(testClient.Delete, ctx, dpfOperatorConfig)

		hostNodeName = "test-namespace_host-node" // nolint:goconst
		handler = &nodeEventHandler{
			hostClient: testManager.GetClient(), // Use the manager's client which has the indexers registered
		}

		// Create a new workqueue for each test
		queue = workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[ctrl.Request]())

		DeferCleanup(func() {
			queue.ShutDown()
		})
	})

	Describe("handleNodeEventHelper", func() {
		var testNamespace string

		BeforeEach(func() {
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "test-node-handler-"}}
			Expect(testClient.Create(ctx, ns)).To(Succeed())
			testNamespace = ns.Name
			DeferCleanup(testClient.Delete, ctx, ns)
		})

		It("should enqueue the DPUService when node has the host label and matches NodeSelector", func() {
			dpuService := newTestDPUService("flannel", testNamespace)
			createAndWait(ctx, testClient, dpuService)
			DeferCleanup(testClient.Delete, ctx, dpuService)

			node := newTestNode("dpu-node", hostNodeName)
			node.Labels["key"] = "value"

			Eventually(func() bool {
				handler.handleNodeEventHelper(ctx, node, queue)
				if queue.Len() == 0 {
					return false
				}
				item, shutdown := queue.Get()
				if shutdown {
					return false
				}
				defer queue.Done(item)
				return item.Name == "flannel" && item.Namespace == testNamespace
			}, 1*time.Second).Should(BeTrue())
		})

		It("should not enqueue DPUService when node lacks host label", func() {
			dpuService := newTestDPUService("test-service-no-label", testNamespace)
			createAndWait(ctx, testClient, dpuService)
			DeferCleanup(testClient.Delete, ctx, dpuService)

			node := newTestNode("dpu-node-no-label", hostNodeName)
			delete(node.Labels, provisioningv1.DPUNodeNameLabel)
			delete(node.Labels, provisioningv1.DPUNodeNamespaceLabel)
			node.Labels["other-key"] = "value"

			Consistently(func() int {
				handler.handleNodeEventHelper(ctx, node, queue)
				return queue.Len()
			}, 1*time.Second).Should(Equal(0))
		})

		It("should not enqueue DPUService when node has no addresses", func() {
			dpuService := newTestDPUService("test-service-no-addr", testNamespace)
			createAndWait(ctx, testClient, dpuService)
			DeferCleanup(testClient.Delete, ctx, dpuService)

			node := newTestNode("dpu-node-no-addresses", hostNodeName)
			node.Status.Addresses = []corev1.NodeAddress{}

			Consistently(func() int {
				handler.handleNodeEventHelper(ctx, node, queue)
				return queue.Len()
			}, 1*time.Second).Should(Equal(0))
		})

		It("should not enqueue DPUService without ConfigPorts", func() {
			dpuService := newTestDPUService("no-config-ports", testNamespace)
			dpuService.Spec.ConfigPorts = nil
			createAndWait(ctx, testClient, dpuService)
			DeferCleanup(testClient.Delete, ctx, dpuService)

			node := newTestNode("dpu-node", hostNodeName)

			Consistently(func() bool {
				handler.handleNodeEventHelper(ctx, node, queue)
				for queue.Len() > 0 {
					item, shutdown := queue.Get()
					if shutdown {
						return false
					}
					defer queue.Done(item)
					if item.Name == "no-config-ports" && item.Namespace == testNamespace {
						return false
					}
				}
				return true
			}, 500*time.Millisecond).Should(BeTrue())
		})

		It("should enqueue DPUService when node matches NodeSelector", func() {
			dpuService := newTestDPUService("selective-service", testNamespace)
			dpuService.Spec.ServiceDaemonSet = &dpuservicev1.ServiceDaemonSetValues{
				NodeSelector: &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{
						{
							MatchExpressions: []corev1.NodeSelectorRequirement{
								{Key: "region", Operator: corev1.NodeSelectorOpIn, Values: []string{"us-west"}},
							},
						},
					},
				},
			}
			createAndWait(ctx, testClient, dpuService)
			DeferCleanup(testClient.Delete, ctx, dpuService)

			node := newTestNode("dpu-node-west", hostNodeName)
			node.Labels["region"] = "us-west"

			Eventually(func() bool {
				handler.handleNodeEventHelper(ctx, node, queue)
				if queue.Len() == 0 {
					return false
				}
				item, shutdown := queue.Get()
				if shutdown {
					return false
				}
				defer queue.Done(item)
				return item.Name == "selective-service" && item.Namespace == testNamespace
			}, 1*time.Second).Should(BeTrue())
		})

		It("should not enqueue DPUService when node does not match NodeSelector", func() {
			dpuService := newTestDPUService("random-service", testNamespace)
			dpuService.Spec.ServiceDaemonSet = &dpuservicev1.ServiceDaemonSetValues{
				NodeSelector: &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{
						{
							MatchExpressions: []corev1.NodeSelectorRequirement{
								{Key: "region", Operator: corev1.NodeSelectorOpIn, Values: []string{"us-west"}},
							},
						},
					},
				},
			}
			createAndWait(ctx, testClient, dpuService)
			DeferCleanup(testClient.Delete, ctx, dpuService)

			node := newTestNode("dpu-node-east", hostNodeName)
			node.Labels["region"] = "us-east"

			Consistently(func() bool {
				handler.handleNodeEventHelper(ctx, node, queue)
				for queue.Len() > 0 {
					item, shutdown := queue.Get()
					if shutdown {
						return false
					}
					defer queue.Done(item)
					if item.Name == "random-service" && item.Namespace == testNamespace {
						return false
					}
				}
				return true
			}, 500*time.Millisecond).Should(BeTrue())
		})

		It("should enqueue multiple matching DPUServices", func() {
			dpuService1 := newTestDPUService("service-1", testNamespace)
			dpuService2 := newTestDPUService("service-2", testNamespace)
			createAndWait(ctx, testClient, dpuService1)
			createAndWait(ctx, testClient, dpuService2)
			DeferCleanup(testClient.Delete, ctx, dpuService1)
			DeferCleanup(testClient.Delete, ctx, dpuService2)

			node := newTestNode("dpu-node", hostNodeName)

			enqueuedServices := make(map[string]bool)
			Eventually(func() int {
				handler.handleNodeEventHelper(ctx, node, queue)
				for queue.Len() > 0 {
					item, shutdown := queue.Get()
					if shutdown {
						break
					}
					defer queue.Done(item)
					enqueuedServices[item.Name] = true
				}
				return len(enqueuedServices)
			}, 1*time.Second).Should(Equal(2))
			Expect(enqueuedServices["service-1"]).To(BeTrue())
			Expect(enqueuedServices["service-2"]).To(BeTrue())
		})

		It("should deduplicate requests when enqueueing", func() {
			dpuService := newTestDPUService("test-service-dedup", testNamespace)
			createAndWait(ctx, testClient, dpuService)
			DeferCleanup(testClient.Delete, ctx, dpuService)

			node := newTestNode("dpu-node", hostNodeName)

			count := 0
			Eventually(func() int {
				handler.handleNodeEventHelper(ctx, node, queue)
				handler.handleNodeEventHelper(ctx, node, queue)
				for queue.Len() > 0 {
					item, shutdown := queue.Get()
					if shutdown {
						break
					}
					defer queue.Done(item)
					if item.Name == "test-service-dedup" && item.Namespace == testNamespace {
						count++
					}
				}
				return count
			}, 1*time.Second).Should(Equal(1))
		})
	})

	Describe("nodeEventHandler Create", func() {
		var testNamespace string

		BeforeEach(func() {
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "test-create-"}}
			Expect(testClient.Create(ctx, ns)).To(Succeed())
			testNamespace = ns.Name
			DeferCleanup(testClient.Delete, ctx, ns)
		})

		It("should call handleNodeEventHelper on create event", func() {
			dpuService := newTestDPUService("test-service-create", testNamespace)
			createAndWait(ctx, testClient, dpuService)
			DeferCleanup(testClient.Delete, ctx, dpuService)

			node := newTestNode("new-node", hostNodeName)

			Eventually(func() bool {
				handler.Create(ctx, event.CreateEvent{Object: node}, queue)
				if queue.Len() == 0 {
					return false
				}
				item, shutdown := queue.Get()
				if shutdown {
					return false
				}
				defer queue.Done(item)
				return item.Name == "test-service-create" && item.Namespace == testNamespace
			}, 1*time.Second).Should(BeTrue())
		})
	})

	Describe("nodeEventHandler Update", func() {
		var testNamespace string

		BeforeEach(func() {
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "test-update-"}}
			Expect(testClient.Create(ctx, ns)).To(Succeed())
			testNamespace = ns.Name
			DeferCleanup(testClient.Delete, ctx, ns)
		})

		It("should call handleNodeEventHelper on update event", func() {
			dpuService := newTestDPUService("test-service-update", testNamespace)
			createAndWait(ctx, testClient, dpuService)
			DeferCleanup(testClient.Delete, ctx, dpuService)

			oldNode := newTestNode("updated-node", hostNodeName)
			newNode := oldNode.DeepCopy()
			newNode.Status.Addresses = []corev1.NodeAddress{
				{Type: corev1.NodeInternalIP, Address: "2.2.2.2"},
			}

			Eventually(func() bool {
				handler.Update(ctx, event.UpdateEvent{ObjectOld: oldNode, ObjectNew: newNode}, queue)
				if queue.Len() == 0 {
					return false
				}
				item, shutdown := queue.Get()
				if shutdown {
					return false
				}
				defer queue.Done(item)
				return item.Name == "test-service-update" && item.Namespace == testNamespace
			}, 1*time.Second).Should(BeTrue())
		})
	})

	Describe("nodeEventHandler Delete", func() {
		var testNamespace string

		BeforeEach(func() {
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "test-delete-"}}
			Expect(testClient.Create(ctx, ns)).To(Succeed())
			testNamespace = ns.Name
			DeferCleanup(testClient.Delete, ctx, ns)
		})

		It("should call handleNodeEventHelper on delete event", func() {
			dpuService := newTestDPUService("test-service-delete", testNamespace)
			createAndWait(ctx, testClient, dpuService)
			DeferCleanup(testClient.Delete, ctx, dpuService)

			node := newTestNode("deleted-node", hostNodeName)

			Eventually(func() bool {
				handler.Delete(ctx, event.DeleteEvent{Object: node}, queue)
				if queue.Len() == 0 {
					return false
				}
				item, shutdown := queue.Get()
				if shutdown {
					return false
				}
				defer queue.Done(item)
				return item.Name == "test-service-delete" && item.Namespace == testNamespace
			}, 1*time.Second).Should(BeTrue())
		})
	})

	Describe("nodeEventHandler Generic", func() {
		It("should not process generic events", func() {
			node := newTestNode("generic-node", hostNodeName)

			handler.Generic(ctx, event.GenericEvent{Object: node}, queue)

			Consistently(func() int {
				return queue.Len()
			}, 1*time.Second).Should(Equal(0))
		})
	})

	Describe("nodeAddressPredicate", func() {
		var predicate predicate.Funcs

		BeforeEach(func() {
			predicate = nodeAddressPredicate()
		})

		Context("CreateFunc", func() {
			It("should return true for all create events", func() {
				node := newTestNode("new-node", hostNodeName)
				Expect(predicate.CreateFunc(event.CreateEvent{Object: node})).To(BeTrue())
			})
		})

		Context("UpdateFunc", func() {
			It("should return true when addresses change", func() {
				oldNode := newTestNode("test-node", hostNodeName)
				newNode := oldNode.DeepCopy()
				newNode.Status.Addresses = []corev1.NodeAddress{
					{Type: corev1.NodeInternalIP, Address: "2.2.2.2"},
				}

				Expect(predicate.UpdateFunc(event.UpdateEvent{
					ObjectOld: oldNode,
					ObjectNew: newNode,
				})).To(BeTrue())
			})

			It("should return true when address is added", func() {
				oldNode := newTestNode("test-node", hostNodeName)
				newNode := oldNode.DeepCopy()
				newNode.Status.Addresses = append(newNode.Status.Addresses,
					corev1.NodeAddress{Type: corev1.NodeExternalIP, Address: "2.2.2.2"})

				Expect(predicate.UpdateFunc(event.UpdateEvent{
					ObjectOld: oldNode,
					ObjectNew: newNode,
				})).To(BeTrue())
			})

			It("should return true when address is removed", func() {
				oldNode := newTestNode("test-node", hostNodeName)
				oldNode.Status.Addresses = append(oldNode.Status.Addresses,
					corev1.NodeAddress{Type: corev1.NodeExternalIP, Address: "2.2.2.2"})
				newNode := oldNode.DeepCopy()
				newNode.Status.Addresses = []corev1.NodeAddress{
					{Type: corev1.NodeInternalIP, Address: "1.1.1.1"},
				}

				Expect(predicate.UpdateFunc(event.UpdateEvent{
					ObjectOld: oldNode,
					ObjectNew: newNode,
				})).To(BeTrue())
			})

			It("should return false when addresses do not change", func() {
				oldNode := newTestNode("test-node", hostNodeName)
				newNode := oldNode.DeepCopy()

				Expect(predicate.UpdateFunc(event.UpdateEvent{
					ObjectOld: oldNode,
					ObjectNew: newNode,
				})).To(BeFalse())
			})

			It("should return false when only labels change", func() {
				oldNode := newTestNode("test-node", hostNodeName)
				oldNode.Labels["key"] = "old-value"
				newNode := oldNode.DeepCopy()
				newNode.Labels["key"] = "new-value"

				Expect(predicate.UpdateFunc(event.UpdateEvent{
					ObjectOld: oldNode,
					ObjectNew: newNode,
				})).To(BeFalse())
			})
		})

		Context("DeleteFunc", func() {
			It("should return true for all delete events", func() {
				node := newTestNode("deleted-node", hostNodeName)
				Expect(predicate.DeleteFunc(event.DeleteEvent{Object: node})).To(BeTrue())
			})
		})

		Context("GenericFunc", func() {
			It("should return false for all generic events", func() {
				node := newTestNode("generic-node", hostNodeName)
				Expect(predicate.GenericFunc(event.GenericEvent{Object: node})).To(BeFalse())
			})
		})
	})
})
