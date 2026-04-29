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

package controller

import (
	"context"
	"fmt"
	"time"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	operatorcontroller "github.com/nvidia/doca-platform/internal/operator/controllers"
	dnutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpunode/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util/dms"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Node Controller", func() {

	Describe("Node", func() {
		var createNode = func(ctx context.Context, name string, labels map[string]string) *corev1.Node {
			node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
					},
					Addresses: []corev1.NodeAddress{
						{
							Type:    corev1.NodeInternalIP,
							Address: "127.0.0.1",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, node)).NotTo(HaveOccurred())
			return node
		}

		Context("obj test context", func() {
			ctx := context.Background()
			It("Node: create a DPUNode before creating Node - validate No DMS Pod is created", func() {
				dpuNode := &provisioningv1.DPUNode{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "dpf-provisioning-node-controller-test",
						Namespace: operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace,
					},
					Spec: provisioningv1.DPUNodeSpec{
						NodeRebootMethod: &provisioningv1.NodeRebootMethod{
							GNOI: &provisioningv1.GNOI{},
						},
						NodeDMSAddress: &provisioningv1.DMSAddress{IP: "1.1.1.1", Port: 1234},
					},
				}
				Expect(k8sClient.Create(ctx, dpuNode)).NotTo(HaveOccurred())

				By("creating the node")
				node := createNode(ctx, "dpf-provisioning-node-controller-test", map[string]string{
					"feature.node.kubernetes.io/dpudevice-pciAddress": "0000-04-00",
					cutil.NodeSelectorLabel:                           "true"})

				By("Checking DMS Pod is not created")
				Consistently(func(g Gomega) {
					podFetched := &corev1.Pod{}
					g.Expect(k8sClient.Get(ctx, client.ObjectKey{
						Namespace: operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace,
						Name:      cutil.GenerateDMSPodName(node)},
						podFetched)).To(HaveOccurred())
				}, 10*time.Second, 1*time.Second).Should(Succeed())
			})
			It("Node: create a k8s Node with dpu-enabled label and validate DMS Pod is created", func() {
				By("Creating DPFOperatorConfig")
				dpfOperatorConfig := &operatorv1.DPFOperatorConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name:      operatorcontroller.DefaultDPFOperatorConfigSingletonName,
						Namespace: operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace,
					},
					Spec: operatorv1.DPFOperatorConfigSpec{
						DeploymentMode: operatorv1.DeploymentModeHostTrusted,
						ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{
							BFBPersistentVolumeClaimName: ptr.To("bfb-pvc"),
						},
					},
				}
				Expect(k8sClient.Create(ctx, dpfOperatorConfig)).NotTo(HaveOccurred())

				By("creating the node")
				node := createNode(ctx, "dpf-provisioning-node-controller-test-2", map[string]string{
					"feature.node.kubernetes.io/dpudevice-pciAddress": "0000-04-00",
					cutil.NodeSelectorLabel:                           "true"})

				By("Checking DMS Pod is created")
				Eventually(func(g Gomega) {
					podFetched := &corev1.Pod{}
					g.Expect(k8sClient.Get(ctx, client.ObjectKey{
						Namespace: operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace,
						Name:      cutil.GenerateDMSPodName(node)},
						podFetched)).To(Succeed())
				}).WithTimeout(60 * time.Second).WithPolling(2 * time.Second).Should(Succeed())
			})
		})
		AfterEach(func() {
			By("deleting the Pod")
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace, Name: "dpf-provisioning-node-controller-test-2-dms"}}))).To(Succeed())
			By("deleting the DPUNode")
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &provisioningv1.DPUNode{ObjectMeta: metav1.ObjectMeta{Namespace: operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace, Name: "dpf-provisioning-node-controller-test"}}))).To(Succeed())
			By("deleting the DPFOperatorConfig")
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &operatorv1.DPFOperatorConfig{ObjectMeta: metav1.ObjectMeta{Namespace: operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace, Name: operatorcontroller.DefaultDPFOperatorConfigSingletonName}}))).To(Succeed())
			By("deleting the node")
			Expect(client.IgnoreNotFound(k8sClient.DeleteAllOf(ctx, &corev1.Node{}))).To(Succeed())
		})
	})
	var _ = Describe("DMS Pod", func() {

		const (
			DefaultNS                    = "dpf-provisioning-test"
			DefaultNodeName              = "dpf-node"
			DefaultDPUName               = "dpf-dpu"
			DefaultDPFOperatorConfigName = "dpf-operator-config"
			DefaultDPFOperatorConfigUID  = "dpf-operator-config-uid"
			DefaultDPFOperatorConfig     = "dpf-operator-config"
		)

		var (
			testNS                        *corev1.Namespace
			testNode                      *corev1.Node
			testDPFOperatorConfigOwnerRef *metav1.OwnerReference
			testDPFOperatorConfig         *operatorv1.DPFOperatorConfig
		)

		var createNode = func(ctx context.Context, name string) *corev1.Node {
			obj := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: testNS.Name,
					Labels: map[string]string{
						cutil.NodeFeatureDiscoveryLabelPrefix + "dpu.features-dpudevice-pciAddress": "0000-90-00",
						cutil.NodeFeatureDiscoveryLabelPrefix + "dpu.features-dpu-pf-name":          "ens1f0np0",
						cutil.NodeFeatureDiscoveryLabelPrefix + cutil.DPUOOBBridgeConfiguredLabel:   "",
					}},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
					},
					Addresses: []corev1.NodeAddress{
						{
							Type:    corev1.NodeInternalIP,
							Address: "127.0.0.1",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, obj)).NotTo(HaveOccurred())
			return obj
		}

		var createDPFOperatorConfigOwnerRef = func() *metav1.OwnerReference {
			return &metav1.OwnerReference{
				APIVersion: operatorv1.GroupVersion.String(),
				Kind:       operatorv1.DPFOperatorConfigKind,
				Name:       DefaultDPFOperatorConfigName,
				UID:        DefaultDPFOperatorConfigUID,
			}
		}

		var createDPFOperatorConfig = func(name string, namespace string) *operatorv1.DPFOperatorConfig {
			return &operatorv1.DPFOperatorConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
				},
				Spec: operatorv1.DPFOperatorConfigSpec{
					DeploymentMode: operatorv1.DeploymentModeHostTrusted,
					ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{
						BFBPersistentVolumeClaimName: ptr.To("foo-pvc"),
					},
				},
			}
		}

		BeforeEach(func() {
			By("creating the namespace")
			testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: DefaultNS}}
			Expect(k8sClient.Create(ctx, testNS)).To(Succeed())
			// Wait for the namespace to be created and get its name
			Eventually(func() error {
				return k8sClient.Get(ctx, client.ObjectKey{Name: testNS.Name}, testNS)
			}).WithTimeout(10 * time.Second).Should(Succeed())

			By("creating the node")
			fmt.Printf("Debug creating the node testNS: %+v\n", testNS)
			testNode = createNode(ctx, DefaultNodeName)

			By("creating the dpfoperatorconfig")
			testDPFOperatorConfig = createDPFOperatorConfig(DefaultDPFOperatorConfig, testNS.Name)
			Expect(k8sClient.Create(ctx, testDPFOperatorConfig)).To(Succeed())

			testDPFOperatorConfigOwnerRef = createDPFOperatorConfigOwnerRef()
		})

		AfterEach(func() {
			By("deleting the dpfoperatorconfig")
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &operatorv1.DPFOperatorConfig{ObjectMeta: metav1.ObjectMeta{Namespace: testNS.Name, Name: DefaultDPFOperatorConfig}}))).To(Succeed())

			By("deleting the namespace")
			Expect(k8sClient.Delete(ctx, testNS)).To(Succeed())

			By("Cleaning the node")
			Expect(k8sClient.Delete(ctx, testNode)).To(Succeed())
		})

		Context("obj test context", func() {
			ctx := context.Background()
			It("DMS Pod: creating DMS Pod with minimal options", func() {
				By("creating Issuer")
				createIssuer(ctx, "dpf-provisioning-selfsigned-issuer", testNS.Name)

				By("creating DMS Pod")
				option := dnutil.HostAgentPodOptions{
					HostAgentImageWithTag: "example.com/doca-platform-foundation/dpf-provisioning-controller/hostdriver:v0.1.0",
					BFBRegistryAddress:    "bfb-registry:8082",
				}
				err := dms.CreateHostAgentPod(ctx, k8sClient, testNode, option, testNS.Name, testDPFOperatorConfigOwnerRef)
				Expect(err).NotTo(HaveOccurred())
			})

			It("DMS Pod: creating DMS Pod with env", func() {
				By("creating Issuer")
				createIssuer(ctx, "dpf-provisioning-selfsigned-issuer", testNS.Name)

				By("creating DMS Pod")
				option := dnutil.HostAgentPodOptions{
					HostAgentImageWithTag: "example.com/doca-platform-foundation/dpf-provisioning-controller/hostdriver:v0.1.0",
					BFBRegistryAddress:    "bfb-registry:8082",
					DMSPodEnvs:            []string{"k1=v1", "k2=v2"},
				}
				err := dms.CreateHostAgentPod(ctx, k8sClient, testNode, option, testNS.Name, testDPFOperatorConfigOwnerRef)
				Expect(err).NotTo(HaveOccurred())

				By("checking Pod env")
				pod := &corev1.Pod{}
				Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: testNS.Name, Name: cutil.GenerateDMSPodName(testNode)}, pod)).To(Succeed())
				for _, c := range pod.Spec.Containers {
					Expect(c.Env).To(ContainElements(corev1.EnvVar{Name: "k1", Value: "v1"}, corev1.EnvVar{Name: "k2", Value: "v2"}))
				}
			})

			It("DMS Pod: create DMS Pod w/o options", func() {
				By("creating Issuer")
				createIssuer(ctx, "dpf-provisioning-selfsigned-issuer", testNS.Name)

				By("creating DMS Pod w/o options")
				option := dnutil.HostAgentPodOptions{}
				err := dms.CreateHostAgentPod(ctx, k8sClient, testNode, option, testNS.Name, testDPFOperatorConfigOwnerRef)
				Expect(err).To(HaveOccurred())
			})
		})
	})
})

var createIssuer = func(ctx context.Context, name string, namespace string) {
	issuer := &unstructured.Unstructured{}
	issuer.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "cert-manager.io",
		Version: "v1",
		Kind:    "Issuer",
	})
	issuer.SetName(name)
	issuer.SetNamespace(namespace)
	Expect(unstructured.SetNestedMap(issuer.Object, map[string]interface{}{}, "spec")).ToNot(HaveOccurred())
	Expect(k8sClient.Create(ctx, issuer)).NotTo(HaveOccurred())
}
