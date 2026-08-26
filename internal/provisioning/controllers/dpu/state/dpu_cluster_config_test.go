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

package state_test

import (
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	dpucluster "github.com/nvidia/doca-platform/pkg/dpucluster"
	testutils "github.com/nvidia/doca-platform/test/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("DPU: cluster config", func() {
	var (
		defaultDPUName        = "dpu-cluster-config-test"
		defaultDPUNodeName    = "dpu-node-cluster-config-test"
		defaultDPUDeviceName  = "dpu-device-cluster-config-test"
		defaultDPUClusterName = "dpu-cluster-cluster-config-test"
		strTrue               = "true"
	)

	Context("successful cases", func() {
		It("Update labels", func() {
			By("prepare DPUDevice CR")
			dpuDevice := dpuDeviceObj(defaultDPUDeviceName)
			createObject(dpuDevice)

			By("prepare DPUNode CR")
			dpuNode := dpuNodeObj(defaultDPUNodeName)
			dpuNode.Finalizers = []string{provisioningv1.DPUNodeFinalizer}
			dpuNode.Labels[cutil.NodeFeatureDiscoveryLabelPrefix+cutil.DPUOOBBridgeConfiguredLabel] = strTrue
			dpuNode.Spec.DPUs = []provisioningv1.DPURef{
				{
					Name: dpuDevice.Name,
				},
			}
			createObject(dpuNode)
			patch := client.MergeFrom(dpuNode.DeepCopy())
			dpuNode.Status.DPUInstallInterface = ptr.To(string(provisioningv1.InstallViaGNOI))
			Expect(k8sClient.Status().Patch(ctx, dpuNode, patch)).To(Succeed())

			By("prepare the DPUCluster")
			dpuCluster := dpuClusterObj(defaultDPUClusterName, "kamaji")
			kamajiSecret, err := testutils.GetFakeKamajiClusterSecretFromEnvtest(*dpuCluster, cfg)
			Expect(err).ToNot(HaveOccurred())
			createObject(kamajiSecret)
			createObject(dpuCluster)
			dpuClusterClient, err := dpucluster.NewConfig(k8sClient, dpuCluster).Client(ctx)
			Expect(err).ToNot(HaveOccurred())

			By("prepare DPU CR")
			dpu := dpuObj(defaultDPUName)
			dpu.Name = defaultDPUName
			dpu.Spec.PCIAddress = ptr.To("0000-00-00")
			dpu.Spec.DPUNodeName = dpuNode.Name
			dpu.Spec.DPUDeviceName = dpuDevice.Name
			dpu.Spec.Cluster.Namespace = dpuCluster.Namespace
			dpu.Spec.Cluster.Name = dpuCluster.Name
			dpu.Spec.Cluster.NodeLabels = map[string]string{
				"key1": "value1",
				"key2": "value2",
			}
			dpu.Status.Phase = provisioningv1.DPUClusterConfig
			dpu.Status.DPUInstallInterface = ptr.To(string(provisioningv1.InstallViaGNOI))

			By("creating a Node in the DPUCluster")
			nodeInDPUCluster := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: dpu.Name,
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:   corev1.NodeReady,
							Status: corev1.ConditionTrue,
						},
					},
				},
			}
			Expect(dpuClusterClient.Create(ctx, nodeInDPUCluster)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, dpuClusterClient, nodeInDPUCluster)

			By("creating a node-join bootstrap token and kubeadm join secret")
			bootstrapToken := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "bootstrap-token-abc123",
					Namespace: "kube-system",
					Labels: map[string]string{
						cutil.LabelDPUName:      dpu.Name,
						cutil.LabelDPUNamespace: dpu.Namespace,
					},
				},
				Type: corev1.SecretTypeBootstrapToken,
			}
			Expect(dpuClusterClient.Create(ctx, bootstrapToken)).To(Succeed())
			kubeadmJoinSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cutil.KubeadmJoinSecretName(dpu.Name),
					Namespace: dpu.Namespace,
				},
				Data: map[string][]byte{
					"join": []byte("kubeadm join example --token abc123.xyz789"),
				},
			}
			createObject(kubeadmJoinSecret)

			By("creating a DPU agent bootstrap token in the management cluster")
			Expect(client.IgnoreAlreadyExists(k8sClient.Create(ctx, &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: "kube-system"},
			}))).To(Succeed())
			agentBootstrapToken := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "bootstrap-token-ec277e",
					Namespace: "kube-system",
					Labels: map[string]string{
						cutil.LabelDPUName:      dpu.Name,
						cutil.LabelDPUNamespace: dpu.Namespace,
					},
				},
				Type: corev1.SecretTypeBootstrapToken,
			}
			createObject(agentBootstrapToken)

			readyRun := func(installInterface provisioningv1.DPUInstallInterfaceType) {
				status, err := state.ClusterConfig(ctx, dpu,
					&dutil.ControllerContext{
						Client: k8sClient,
						Options: dutil.DPUOptions{
							DPUInstallInterface: string(installInterface),
						},
					},
				)
				Expect(err).To(Succeed())
				Expect(status.Phase).To(Equal(provisioningv1.DPUServiceReadiness))

				nodeInDPUCluster := &corev1.Node{}
				Expect(dpuClusterClient.Get(ctx, client.ObjectKey{Namespace: dpu.Namespace, Name: dpu.Name}, nodeInDPUCluster)).To(Succeed())
				Expect(nodeInDPUCluster.Labels).To(HaveKeyWithValue("key1", "value1"))
				Expect(nodeInDPUCluster.Labels).To(HaveKeyWithValue("key2", "value2"))

				tokenList := &corev1.SecretList{}
				Expect(dpuClusterClient.List(ctx, tokenList,
					client.InNamespace("kube-system"),
					client.MatchingLabels{
						cutil.LabelDPUName:      dpu.Name,
						cutil.LabelDPUNamespace: dpu.Namespace,
					},
				)).To(Succeed())
				Expect(tokenList.Items).To(BeEmpty())

				deletedJoinSecret := &corev1.Secret{}
				err = k8sClient.Get(ctx, client.ObjectKey{Namespace: dpu.Namespace, Name: cutil.KubeadmJoinSecretName(dpu.Name)}, deletedJoinSecret)
				Expect(apierrors.IsNotFound(err)).To(BeTrue())

				agentTokenList := &corev1.SecretList{}
				Expect(k8sClient.List(ctx, agentTokenList,
					client.InNamespace("kube-system"),
					client.MatchingLabels{
						cutil.LabelDPUName:      dpu.Name,
						cutil.LabelDPUNamespace: dpu.Namespace,
					},
				)).To(Succeed())
				Expect(agentTokenList.Items).To(BeEmpty())
			}
			runForEachInterface(readyRun)

			By("remove labels")
			dpu.Status.Phase = provisioningv1.DPUClusterConfig
			delete(dpu.Spec.Cluster.NodeLabels, "key1")
			removeLabelsRun := func(installInterface provisioningv1.DPUInstallInterfaceType) {
				status, err := state.ClusterConfig(ctx, dpu,
					&dutil.ControllerContext{
						Client: k8sClient,
						Options: dutil.DPUOptions{
							DPUInstallInterface: string(installInterface),
						},
					},
				)
				Expect(err).To(Succeed())
				Expect(status.Phase).To(Equal(provisioningv1.DPUServiceReadiness))

				nodeInDPUCluster := &corev1.Node{}
				Expect(dpuClusterClient.Get(ctx, client.ObjectKey{Namespace: dpu.Namespace, Name: dpu.Name}, nodeInDPUCluster)).To(Succeed())
				Expect(nodeInDPUCluster.Labels).To(Not(HaveKey("key1")))
				Expect(nodeInDPUCluster.Labels).To(HaveKeyWithValue("key2", "value2"))
			}
			runForEachInterface(removeLabelsRun)
		})
	})
})
