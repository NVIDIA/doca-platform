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

package controllers //nolint:dupl

import (
	"context"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	operatorcontroller "github.com/nvidia/doca-platform/internal/operator/controllers"
	"github.com/nvidia/doca-platform/pkg/conditions"
	dpucluster "github.com/nvidia/doca-platform/pkg/dpucluster"
	testutils "github.com/nvidia/doca-platform/test/utils"
	"github.com/nvidia/doca-platform/test/utils/informer"

	"github.com/fluxcd/pkg/runtime/patch"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	dscResourceName = "test-dpu-service-chain"
)

//nolint:dupl
var _ = Describe("DPUServiceChain Controller", func() {
	Context("When reconciling a resource", func() {
		var (
			testNS           *corev1.Namespace
			dpuCluster       provisioningv1.DPUCluster
			dpuClusterClient client.Client
		)
		BeforeEach(func() {
			By("Creating the namespaces")
			testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "dpudeployment-testns-"}}
			Expect(testClient.Create(ctx, testNS)).To(Succeed())
			DeferCleanup(testClient.Delete, ctx, testNS)

			By("Adding DPFOperatorConfig")
			dpfOperatorConfig := getMinimalDPFOperatorConfig()
			dpfOperatorConfig.Namespace = testNS.Name
			Expect(testClient.Create(ctx, dpfOperatorConfig)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpfOperatorConfig)

			By("Faking GetDPUClusters to use the envtest cluster instead of a separate one")
			dpuCluster = testutils.GetTestDPUCluster("default", "envtest")
			kamajiSecret, err := testutils.GetFakeKamajiClusterSecretFromEnvtest(dpuCluster, cfg)
			Expect(err).NotTo(HaveOccurred())
			Expect(testClient.Create(ctx, kamajiSecret)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, kamajiSecret)

			// Create the DPUCluster object.
			Expect(testClient.Create(ctx, &dpuCluster)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, &dpuCluster)
			patcher := patch.NewSerialPatcher(&dpuCluster, testClient)

			// mark the cluster as ready so that the remoteCache treats it as ready
			dpuCluster.Status.Phase = provisioningv1.PhaseReady
			Expect(patcher.Patch(ctx, &dpuCluster, patch.WithFieldOwner("dpuservicechain-controller"))).To(Succeed())

			// create a client for the DPUCluster
			dpuClusterClient, err = dpucluster.NewConfig(testClient, &dpuCluster).Client(ctx)
			Expect(err).ToNot(HaveOccurred())
		})
		It("should successfully reconcile the DPUServiceChain", func() {
			By("Create DPUServiceChain")
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, createDPUServiceChain(ctx, dscResourceName, testNS.Name, &metav1.LabelSelector{}))
			By("Verify ServiceChainSet is created")
			Eventually(func(g Gomega) {
				scs := &dpuservicev1.ServiceChainSet{ObjectMeta: metav1.ObjectMeta{Name: dscResourceName, Namespace: testNS.Name}}
				g.Expect(dpuClusterClient.Get(ctx, client.ObjectKeyFromObject(scs), scs)).NotTo(HaveOccurred())
			}, timeout*30, interval).Should(Succeed())
			By("Verify ServiceChainSet")
			scs := &dpuservicev1.ServiceChainSet{ObjectMeta: metav1.ObjectMeta{Name: dscResourceName, Namespace: testNS.Name}}
			Expect(dpuClusterClient.Get(ctx, client.ObjectKeyFromObject(scs), scs)).NotTo(HaveOccurred())
			for k, v := range testutils.GetTestLabels() {
				Expect(scs.Labels[k]).To(Equal(v))
			}
			Expect(scs.Spec).To(BeEquivalentTo(getExpectedTestServiceChainSetSpec(&metav1.LabelSelector{})))
			By("Update DPUServiceChain")
			labelSelector := &metav1.LabelSelector{MatchLabels: map[string]string{"role": "firewall"}}
			Eventually(func(g Gomega) {
				dsc := &dpuservicev1.DPUServiceChain{ObjectMeta: metav1.ObjectMeta{Name: dscResourceName, Namespace: testNS.Name}}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dsc), dsc)).NotTo(HaveOccurred())
				updatedSpec := getTestServiceChainSetSpec(labelSelector)
				dsc.Spec.Template.Spec = *updatedSpec
				g.Expect(testClient.Update(ctx, dsc)).To(Succeed())
			}).Should(Succeed())
			By("Verify ServiceChainSet is updated")
			Eventually(func(g Gomega) {
				scs := &dpuservicev1.ServiceChainSet{ObjectMeta: metav1.ObjectMeta{Name: dscResourceName, Namespace: testNS.Name}}
				g.Expect(dpuClusterClient.Get(ctx, client.ObjectKeyFromObject(scs), scs)).NotTo(HaveOccurred())
				g.Expect(scs.Spec).To(BeEquivalentTo(getExpectedTestServiceChainSetSpec(&metav1.LabelSelector{})))
			}, timeout*30, interval).Should(Succeed())
		})
		It("should successfully reconcile a DPUServiceChain with maximum name length", func() {
			By("Create DPUServiceChain")
			dsc := getMinimalDPUServiceChain(utilrand.String(63), testNS.Name, &metav1.LabelSelector{})
			Expect(testClient.Create(ctx, dsc)).NotTo(HaveOccurred())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dsc)
			By("Verify ServiceChainSet is created")
			Eventually(func(g Gomega) {
				scs := &dpuservicev1.ServiceChainSet{ObjectMeta: metav1.ObjectMeta{Name: dsc.Name, Namespace: testNS.Name}}
				g.Expect(dpuClusterClient.Get(ctx, client.ObjectKeyFromObject(scs), scs)).NotTo(HaveOccurred())
			}, timeout*30, interval).Should(Succeed())
		})
		It("should fail to create a DPUServiceChain with name exceeding the maximum length", func() {
			By("Create DPUServiceChain")
			dsc := getMinimalDPUServiceChain(utilrand.String(64), testNS.Name, &metav1.LabelSelector{})
			Expect(testClient.Create(ctx, dsc)).To(HaveOccurred())
		})
		It("should successfully reconcile the DPUServiceChain with valid specified MTU", func() {
			By("Create DPUServiceChain")
			dpuServiceChain := getMinimalDPUServiceChain(dscResourceName, testNS.Name, &metav1.LabelSelector{})
			dpuServiceChain.Spec.Template.Spec.Template.Spec.Switches[0].ServiceMTU = ptr.To(1280)
			Expect(testClient.Create(ctx, dpuServiceChain)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceChain)
			By("Verify ServiceChainSet is created")
			Eventually(func(g Gomega) {
				scs := &dpuservicev1.ServiceChainSet{ObjectMeta: metav1.ObjectMeta{Name: dscResourceName, Namespace: testNS.Name}}
				g.Expect(dpuClusterClient.Get(ctx, client.ObjectKeyFromObject(scs), scs)).NotTo(HaveOccurred())
			}, timeout*30, interval).Should(Succeed())
			By("Verify ServiceChainSet")
			scs := &dpuservicev1.ServiceChainSet{ObjectMeta: metav1.ObjectMeta{Name: dscResourceName, Namespace: testNS.Name}}
			Expect(dpuClusterClient.Get(ctx, client.ObjectKeyFromObject(scs), scs)).NotTo(HaveOccurred())
			for k, v := range testutils.GetTestLabels() {
				Expect(scs.Labels[k]).To(Equal(v))
			}
			expectedSpec := getExpectedTestServiceChainSetSpec(&metav1.LabelSelector{})
			expectedSpec.Template.Spec.Switches[0].ServiceMTU = ptr.To(1280)
			Expect(scs.Spec).To(BeEquivalentTo(expectedSpec))
		})
		It("should fail to reconcile the DPUServiceChain - should not create a ServiceChainSet if invalid MTU is specified", func() {
			By("Create DPUServiceChain")
			dpuServiceChain := getMinimalDPUServiceChain(dscResourceName, testNS.Name, &metav1.LabelSelector{})
			dpuServiceChain.Spec.Template.Spec.Template.Spec.Switches[0].ServiceMTU = ptr.To(9000)
			Expect(testClient.Create(ctx, dpuServiceChain)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceChain)
			By("Verify ServiceChainSet is not created")
			Consistently(func(g Gomega) {
				scs := &dpuservicev1.ServiceChainSet{ObjectMeta: metav1.ObjectMeta{Name: dscResourceName, Namespace: testNS.Name}}
				g.Expect(dpuClusterClient.Get(ctx, client.ObjectKeyFromObject(scs), scs)).To(HaveOccurred())
			}, timeout, interval).Should(Succeed())
		})
		It("should successfully detect update on the ServiceChainSet", func() {
			By("Create DPUServiceChain")
			dpuServiceChain := createDPUServiceChain(ctx, dscResourceName, testNS.Name, &metav1.LabelSelector{})
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceChain)
			By("Verify ServiceChainSet is created")
			Eventually(func(g Gomega) {
				scs := &dpuservicev1.ServiceChainSet{ObjectMeta: metav1.ObjectMeta{Name: dscResourceName, Namespace: testNS.Name}}
				g.Expect(dpuClusterClient.Get(ctx, client.ObjectKeyFromObject(scs), scs)).NotTo(HaveOccurred())
			}, timeout, interval).Should(Succeed())

			By("Verify ServiceChainSet")
			scs := &dpuservicev1.ServiceChainSet{ObjectMeta: metav1.ObjectMeta{Name: dscResourceName, Namespace: testNS.Name}}
			Expect(dpuClusterClient.Get(ctx, client.ObjectKeyFromObject(scs), scs)).NotTo(HaveOccurred())
			for k, v := range testutils.GetTestLabels() {
				Expect(scs.Labels[k]).To(Equal(v))
			}

			By("Verify watcher is created")
			Eventually(func(g Gomega) {
				remoteClient, err := remoteCache.GetClient(client.ObjectKeyFromObject(&dpuCluster))
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(remoteClient).NotTo(BeNil())
			}, timeout, interval).Should(Succeed())

			// now that we know the watcher is created, we can test the service chain set update
			// detection logic. For that purpose we create a fresh DPUServiceChain. This is needed
			// because the remote cache detecting a new dpuCluster and our reconciler creating a new watcher
			// for serviceChainSet kind is an operation that is subject to race conditions. With the new
			// DPUServiceChain we can reduce unwanted reconciliation loops to a minimum.
			By("Recreate DPUServiceChain")
			Expect(testutils.CleanupAndWait(ctx, testClient, dpuServiceChain)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, createDPUServiceChain(ctx, dscResourceName, testNS.Name, &metav1.LabelSelector{}))
			By("Verify ServiceChainSet is created")
			scs = &dpuservicev1.ServiceChainSet{ObjectMeta: metav1.ObjectMeta{Name: dscResourceName, Namespace: testNS.Name}}
			Eventually(func(g Gomega) {
				g.Expect(dpuClusterClient.Get(ctx, client.ObjectKeyFromObject(scs), scs)).NotTo(HaveOccurred())
				g.Expect(scs.Spec).To(BeEquivalentTo(getExpectedTestServiceChainSetSpec(&metav1.LabelSelector{})))
			}, timeout*30, interval).Should(Succeed())

			Eventually(func(g Gomega) {
				By("Update ServiceChainSet")
				gotScs := &dpuservicev1.ServiceChainSet{}
				g.Expect(dpuClusterClient.Get(ctx, client.ObjectKeyFromObject(scs), gotScs)).To(Succeed())
				gotScs.Spec.NodeSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"role": "firewall"}}
				gotScs.SetManagedFields(nil)
				gotScs.SetGroupVersionKind(dpuservicev1.ServiceChainSetGroupVersionKind)
				g.Expect(dpuClusterClient.Patch(ctx, gotScs, client.Apply, client.ForceOwnership, client.FieldOwner("dpuservicechain-controller"))).To(Succeed())

				By("Verify ServiceChainSet is reverted")
				g.Eventually(func(g Gomega) {
					scs := &dpuservicev1.ServiceChainSet{ObjectMeta: metav1.ObjectMeta{Name: dscResourceName, Namespace: testNS.Name}}
					g.Expect(dpuClusterClient.Get(ctx, client.ObjectKeyFromObject(scs), scs)).NotTo(HaveOccurred())
					g.Expect(scs.Spec).To(BeEquivalentTo(getExpectedTestServiceChainSetSpec(&metav1.LabelSelector{})))
				}, timeout, interval).Should(Succeed())
			}, timeout, interval).MustPassRepeatedly(10).Should(Succeed())
		})
		It("should successfully delete the DPUServiceChain and ServiceChainSet", func() {
			By("Create DPUServiceChain")
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, createDPUServiceChain(ctx, dscResourceName, testNS.Name, &metav1.LabelSelector{}))
			By("Verify ServiceChainSet is created")
			Eventually(func(g Gomega) {
				scs := &dpuservicev1.ServiceChainSet{ObjectMeta: metav1.ObjectMeta{Name: dscResourceName, Namespace: testNS.Name}}
				g.Expect(dpuClusterClient.Get(ctx, client.ObjectKeyFromObject(scs), scs)).NotTo(HaveOccurred())
			}, timeout*30, interval).Should(Succeed())
			By("Delete DPUServiceChain")
			dsc := &dpuservicev1.DPUServiceChain{ObjectMeta: metav1.ObjectMeta{Name: dscResourceName, Namespace: testNS.Name}}
			Expect(testClient.Delete(ctx, dsc)).NotTo(HaveOccurred())
			By("Verify ServiceChainSet is deleted")
			Eventually(func(g Gomega) {
				scs := &dpuservicev1.ServiceChainSet{ObjectMeta: metav1.ObjectMeta{Name: dscResourceName, Namespace: testNS.Name}}
				err := dpuClusterClient.Get(ctx, client.ObjectKeyFromObject(scs), scs)
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}, timeout*30, interval).Should(Succeed())
			By("Verify DPUServiceChain is deleted")
			Eventually(func(g Gomega) {
				scs := &dpuservicev1.DPUServiceChain{ObjectMeta: metav1.ObjectMeta{Name: dscResourceName, Namespace: testNS.Name}}
				err := testClient.Get(ctx, client.ObjectKeyFromObject(scs), scs)
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}, timeout*30, interval).Should(Succeed())
		})
	})
	Context("When checking the behavior on multiple DPU clusters", func() {
		var (
			testNS            *corev1.Namespace
			dpuCluster1       provisioningv1.DPUCluster
			dpuCluster2       provisioningv1.DPUCluster
			dpuCluster1Client client.Client
			dpuCluster2Client client.Client
		)

		BeforeEach(func() {
			testNS = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "testns-",
				},
			}
			Expect(testClient.Create(ctx, testNS)).To(Succeed())
			DeferCleanup(testClient.Delete, ctx, testNS)

			By("Adding DPFOperatorConfig")
			dpfOperatorConfig := getMinimalDPFOperatorConfig()
			dpfOperatorConfig.Namespace = testNS.Name
			Expect(testClient.Create(ctx, dpfOperatorConfig)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpfOperatorConfig)

			By("Adding fake kamaji cluster 1 with label dpucluster=cluster1 using testEnv1")
			dpuCluster1 = testutils.GetTestDPUCluster(testNS.Name, "cluster1")
			dpuCluster1.Labels = map[string]string{"dpucluster": "cluster1"}
			kamajiSecret1, err := testutils.GetFakeKamajiClusterSecretFromEnvtest(dpuCluster1, cfg1)
			Expect(err).NotTo(HaveOccurred())
			Expect(testClient.Create(ctx, kamajiSecret1)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, kamajiSecret1)

			Expect(testClient.Create(ctx, &dpuCluster1)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, &dpuCluster1)
			patcher1 := patch.NewSerialPatcher(&dpuCluster1, testClient)

			// mark the cluster as ready so that the remoteCache treats it as ready
			dpuCluster1.Status.Phase = provisioningv1.PhaseReady
			Expect(patcher1.Patch(ctx, &dpuCluster1, patch.WithFieldOwner("test"))).To(Succeed())

			dpuCluster1Client, err = dpucluster.NewConfig(testClient, &dpuCluster1).Client(ctx)
			Expect(err).ToNot(HaveOccurred())

			By("Adding fake kamaji cluster 2 with label dpucluster=cluster2 using testEnv2")
			dpuCluster2 = testutils.GetTestDPUCluster(testNS.Name, "cluster2")
			dpuCluster2.Labels = map[string]string{"dpucluster": "cluster2"}
			kamajiSecret2, err := testutils.GetFakeKamajiClusterSecretFromEnvtest(dpuCluster2, cfg2)
			Expect(err).NotTo(HaveOccurred())
			Expect(testClient.Create(ctx, kamajiSecret2)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, kamajiSecret2)

			Expect(testClient.Create(ctx, &dpuCluster2)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, &dpuCluster2)
			patcher2 := patch.NewSerialPatcher(&dpuCluster2, testClient)

			// mark the cluster as ready so that the remoteCache treats it as ready
			dpuCluster2.Status.Phase = provisioningv1.PhaseReady
			Expect(patcher2.Patch(ctx, &dpuCluster2, patch.WithFieldOwner("test"))).To(Succeed())

			dpuCluster2Client, err = dpucluster.NewConfig(testClient, &dpuCluster2).Client(ctx)
			Expect(err).ToNot(HaveOccurred())
		})
		It("should reconcile resources in all DPU Clusters when DPUClusterSelector is not set", func() {
			By("Creating DPUServiceChain without DPUClusterSelector")
			dpuServiceChain := getMinimalDPUServiceChain("no-selector-chain", testNS.Name, nil)
			Expect(testClient.Create(ctx, dpuServiceChain)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceChain)

			By("Verifying ServiceChainSet is created in cluster1")
			Eventually(func(g Gomega) {
				got := &dpuservicev1.ServiceChainSet{}
				g.Expect(dpuCluster1Client.Get(ctx, client.ObjectKey{Namespace: testNS.Name, Name: "no-selector-chain"}, got)).To(Succeed())
			}).WithTimeout(10 * time.Second).Should(Succeed())

			By("Verifying ServiceChainSet is created in cluster2")
			Eventually(func(g Gomega) {
				got := &dpuservicev1.ServiceChainSet{}
				g.Expect(dpuCluster2Client.Get(ctx, client.ObjectKey{Namespace: testNS.Name, Name: "no-selector-chain"}, got)).To(Succeed())
			}).WithTimeout(10 * time.Second).Should(Succeed())
		})

		It("should reconcile resources in all matching DPU Clusters", func() {
			By("Creating DPUServiceChain with DPUClusterSelector matching dpucluster=cluster1")
			dpuServiceChain := getMinimalDPUServiceChain("multi-cluster-chain", testNS.Name, nil)
			dpuServiceChain.Spec.DPUClusterSelector = &metav1.LabelSelector{
				MatchLabels: map[string]string{"dpucluster": "cluster1"},
			}
			Expect(testClient.Create(ctx, dpuServiceChain)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceChain)

			By("Verifying ServiceChainSet is created in cluster1")
			Eventually(func(g Gomega) {
				got := &dpuservicev1.ServiceChainSet{}
				g.Expect(dpuCluster1Client.Get(ctx, client.ObjectKey{Namespace: testNS.Name, Name: "multi-cluster-chain"}, got)).To(Succeed())
			}).WithTimeout(10 * time.Second).Should(Succeed())

			By("Verifying ServiceChainSet is NOT created in cluster2")
			Consistently(func(g Gomega) {
				got := &dpuservicev1.ServiceChainSet{}
				err := dpuCluster2Client.Get(ctx, client.ObjectKey{Namespace: testNS.Name, Name: "multi-cluster-chain"}, got)
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}).WithTimeout(5 * time.Second).Should(Succeed())
		})

		It("should remove resources from non matching DPU Clusters", func() {
			By("Creating DPUServiceChain without DPUClusterSelector (matches all clusters)")
			dpuServiceChain := getMinimalDPUServiceChain("all-clusters-chain", testNS.Name, nil)
			Expect(testClient.Create(ctx, dpuServiceChain)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceChain)

			By("Verifying ServiceChainSet is created in both clusters")
			Eventually(func(g Gomega) {
				got1 := &dpuservicev1.ServiceChainSet{}
				g.Expect(dpuCluster1Client.Get(ctx, client.ObjectKey{Namespace: testNS.Name, Name: "all-clusters-chain"}, got1)).To(Succeed())
				got2 := &dpuservicev1.ServiceChainSet{}
				g.Expect(dpuCluster2Client.Get(ctx, client.ObjectKey{Namespace: testNS.Name, Name: "all-clusters-chain"}, got2)).To(Succeed())
			}).WithTimeout(10 * time.Second).Should(Succeed())

			By("Updating DPUServiceChain to only match dpucluster=cluster1")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuServiceChain), dpuServiceChain)).To(Succeed())
				dpuServiceChain.Spec.DPUClusterSelector = &metav1.LabelSelector{
					MatchLabels: map[string]string{"dpucluster": "cluster1"},
				}
				dpuServiceChain.SetManagedFields(nil)
				dpuServiceChain.SetGroupVersionKind(dpuservicev1.DPUServiceChainGroupVersionKind)
				g.Expect(testClient.Patch(ctx, dpuServiceChain, client.Apply, client.FieldOwner("test"))).To(Succeed())
			}).WithTimeout(10 * time.Second).Should(Succeed())

			By("Verifying ServiceChainSet is still in cluster1")
			Eventually(func(g Gomega) {
				got := &dpuservicev1.ServiceChainSet{}
				g.Expect(dpuCluster1Client.Get(ctx, client.ObjectKey{Namespace: testNS.Name, Name: "all-clusters-chain"}, got)).To(Succeed())
			}).WithTimeout(10 * time.Second).Should(Succeed())

			By("Verifying ServiceChainSet is removed from cluster2")
			Eventually(func(g Gomega) {
				got := &dpuservicev1.ServiceChainSet{}
				err := dpuCluster2Client.Get(ctx, client.ObjectKey{Namespace: testNS.Name, Name: "all-clusters-chain"}, got)
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}).WithTimeout(10 * time.Second).Should(Succeed())
		})
	})
	Context("When checking the status transitions", func() {
		var (
			testNS           *corev1.Namespace
			dpuServiceChain  *dpuservicev1.DPUServiceChain
			dpuCluster       provisioningv1.DPUCluster
			kamajiSecret     *corev1.Secret
			dpuClusterClient client.Client
			i                *informer.TestInformer
		)

		BeforeEach(func() {
			By("Creating the namespaces")
			testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "testns-"}}
			Expect(testClient.Create(ctx, testNS)).To(Succeed())
			DeferCleanup(testClient.Delete, ctx, testNS)

			By("Adding fake kamaji cluster")
			dpuCluster = testutils.GetTestDPUCluster(testNS.Name, "envtest")
			var err error
			kamajiSecret, err = testutils.GetFakeKamajiClusterSecretFromEnvtest(dpuCluster, cfg)
			Expect(err).NotTo(HaveOccurred())
			Expect(testClient.Create(ctx, kamajiSecret)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, kamajiSecret)
			Expect(testClient.Create(ctx, &dpuCluster)).To(Succeed())
			DeferCleanup(testClient.Delete, ctx, &dpuCluster)
			dpuClusterClient, err = dpucluster.NewConfig(testClient, &dpuCluster).Client(ctx)
			Expect(err).ToNot(HaveOccurred())
			// mark the cluster as ready so that the remoteCache treats it as ready
			dpuCluster.Status.Phase = provisioningv1.PhaseReady
			Expect(testClient.Status().Update(ctx, &dpuCluster)).To(Succeed())

			By("Creating the DPFOperatorConfig for MTU validation")
			dpfOperatorConfig := getMinimalDPFOperatorConfig()
			dpfOperatorConfig.Namespace = testNS.Name
			Expect(testClient.Create(ctx, dpfOperatorConfig)).To(Succeed())
			DeferCleanup(testClient.Delete, ctx, dpfOperatorConfig)

			By("Creating the informer infrastructure for DPUServiceChain")
			i = informer.NewInformer(cfg, dpuservicev1.DPUServiceChainGroupVersionKind, testNS.Name, "dpuservicechains")
			DeferCleanup(i.Cleanup)
			go i.Run()
			Eventually(i.HasSynced).WithTimeout(5 * time.Second).WithPolling(100 * time.Millisecond).Should(BeTrue())

			By("Creating a DPUServiceChain")
			dpuServiceChain = createDPUServiceChain(ctx, "chain", testNS.Name, &metav1.LabelSelector{})
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceChain)
		})
		It("DPUServiceChain has most conditions with Pending Reason at start of the reconciliation loop", func() {
			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				oldObj := &dpuservicev1.DPUServiceChain{}
				newObj := &dpuservicev1.DPUServiceChain{}
				g.Expect(testClient.Scheme().Convert(ev.OldObj, oldObj, nil)).ToNot(HaveOccurred())
				g.Expect(testClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())

				g.Expect(oldObj.Status.Conditions).To(BeEmpty())
				g.Expect(newObj.Status.Conditions).ToNot(BeEmpty())
				return newObj.Status.Conditions
			}).WithTimeout(10 * time.Second).Should(ConsistOf(
				And(
					HaveField("Type", string(conditions.TypeReady)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionServiceChainSetReconciled)),
					HaveField("Status", metav1.ConditionUnknown),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
				// Ideally this should have been unknown, but we update this status on defer
				And(
					HaveField("Type", string(dpuservicev1.ConditionServiceChainSetReady)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
			))
		})
		It("DPUServiceChain has condition ServiceChainSetReconciled with Success Reason at end of successful reconciliation loop but ServiceChainSetReady with Pending reason on underlying object not ready", func() {
			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				oldObj := &dpuservicev1.DPUServiceChain{}
				newObj := &dpuservicev1.DPUServiceChain{}
				g.Expect(testClient.Scheme().Convert(ev.OldObj, oldObj, nil)).ToNot(HaveOccurred())
				g.Expect(testClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())

				g.Expect(oldObj.Status.Conditions).To(ContainElement(
					And(
						HaveField("Type", string(dpuservicev1.ConditionServiceChainSetReconciled)),
						HaveField("Status", metav1.ConditionUnknown),
						HaveField("Reason", string(conditions.ReasonPending)),
					),
				))
				return newObj.Status.Conditions
			}).WithTimeout(10 * time.Second).Should(ConsistOf(
				And(
					HaveField("Type", string(conditions.TypeReady)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionServiceChainSetReconciled)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionServiceChainSetReady)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
			))
		})
		It("DPUServiceChain has all conditions with Success Reason at end of successful reconciliation loop and underlying object ready", func() {
			By("Patching ServiceChainSet status to ready")
			scs := &dpuservicev1.ServiceChainSet{}
			Eventually(func(g Gomega) {
				g.Expect(dpuClusterClient.Get(ctx, client.ObjectKey{
					Name:      dpuServiceChain.Name,
					Namespace: dpuServiceChain.Namespace,
				}, scs)).To(Succeed())
			}).WithTimeout(10 * time.Second).Should(Succeed())

			scs.Status.Conditions = []metav1.Condition{
				{
					Type:               string(conditions.TypeReady),
					Status:             metav1.ConditionTrue,
					Reason:             string(conditions.ReasonSuccess),
					LastTransitionTime: metav1.NewTime(time.Now()),
					ObservedGeneration: scs.Generation,
				},
			}
			scs.SetGroupVersionKind(dpuservicev1.ServiceChainSetGroupVersionKind)
			scs.SetManagedFields(nil)
			Expect(dpuClusterClient.Status().Patch(ctx, scs, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())

			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				oldObj := &dpuservicev1.DPUServiceChain{}
				newObj := &dpuservicev1.DPUServiceChain{}
				g.Expect(testClient.Scheme().Convert(ev.OldObj, oldObj, nil)).ToNot(HaveOccurred())
				g.Expect(testClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())

				return newObj.Status.Conditions
			}).WithTimeout(10 * time.Second).Should(ConsistOf(
				And(
					HaveField("Type", string(conditions.TypeReady)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionServiceChainSetReconciled)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionServiceChainSetReady)),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", string(conditions.ReasonSuccess)),
				),
			))
		})
		It("DPUServiceChain has condition ServiceChainSetReconciled with Error Reason at the end of a reconciliation loop that failed", func() {
			By("Setting the DPUCluster to an invalid state")
			Expect(testClient.Delete(ctx, kamajiSecret)).To(Succeed())

			DeferCleanup(func() {
				By("Reverting the DPUCluster to ready to ensure DPUServiceChain deletion can be done")
				kamajiSecret.ResourceVersion = ""
				Expect(testClient.Create(ctx, kamajiSecret)).To(Succeed())
			})

			By("Checking condition")
			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				oldObj := &dpuservicev1.DPUServiceChain{}
				newObj := &dpuservicev1.DPUServiceChain{}
				g.Expect(testClient.Scheme().Convert(ev.OldObj, oldObj, nil)).ToNot(HaveOccurred())
				g.Expect(testClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())

				g.Expect(oldObj.Status.Conditions).To(ContainElement(
					And(
						HaveField("Type", string(dpuservicev1.ConditionServiceChainSetReconciled)),
						HaveField("Status", metav1.ConditionUnknown),
						HaveField("Reason", string(conditions.ReasonPending)),
					),
				))
				return newObj.Status.Conditions
			}).WithTimeout(10 * time.Second).Should(ConsistOf(
				And(
					HaveField("Type", string(conditions.TypeReady)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionServiceChainSetReconciled)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonError)),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionServiceChainSetReady)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
			))
		})
		It("DPUServiceChain has condition ServiceChainSetReconciled with AwaitingDeletion Reason when there are still objects in the DPUCluster", func() {
			By("Ensuring that the DPUServiceChain has been reconciled successfully")
			Eventually(func(g Gomega) []metav1.Condition {
				got := &dpuservicev1.DPUServiceChain{}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuServiceChain), got)).To(Succeed())
				return got.Status.Conditions
			}).WithTimeout(10 * time.Second).Should(ContainElement(
				And(
					HaveField("Type", string(dpuservicev1.ConditionServiceChainSetReconciled)),
					HaveField("Status", metav1.ConditionTrue),
				),
			))

			By("Adding finalizer to the underlying object")
			gotChainSet := &dpuservicev1.ServiceChainSet{}
			Eventually(dpuClusterClient.Get).WithArguments(ctx, client.ObjectKey{Namespace: testNS.Name, Name: "chain"}, gotChainSet).Should(Succeed())
			gotChainSet.SetFinalizers([]string{"test.dpu.nvidia.com/test"})
			gotChainSet.SetGroupVersionKind(dpuservicev1.ServiceChainSetGroupVersionKind)
			gotChainSet.SetManagedFields(nil)
			Expect(testClient.Patch(ctx, gotChainSet, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())

			By("Deleting the DPUServiceChain")
			Expect(testClient.Delete(ctx, dpuServiceChain)).To(Succeed())

			By("Checking the deleted condition is added")
			Eventually(func(g Gomega) []metav1.Condition {
				ev := &informer.Event{}
				g.Eventually(i.UpdateEvents).Should(Receive(ev))
				oldObj := &dpuservicev1.DPUServiceChain{}
				newObj := &dpuservicev1.DPUServiceChain{}
				g.Expect(testClient.Scheme().Convert(ev.OldObj, oldObj, nil)).ToNot(HaveOccurred())
				g.Expect(testClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())

				g.Expect(oldObj.Status.Conditions).To(ContainElement(
					And(
						HaveField("Type", string(dpuservicev1.ConditionServiceChainSetReconciled)),
					),
				))
				return newObj.Status.Conditions
			}).WithTimeout(10 * time.Second).Should(ConsistOf(
				And(
					HaveField("Type", string(conditions.TypeReady)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonAwaitingDeletion)),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionServiceChainSetReconciled)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonAwaitingDeletion)),
					HaveField("Message", ContainSubstring("1")),
				),
				And(
					HaveField("Type", string(dpuservicev1.ConditionServiceChainSetReady)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
			))

			By("Removing finalizer from the underlying object to ensure deletion")
			gotChainSet = &dpuservicev1.ServiceChainSet{}
			Eventually(dpuClusterClient.Get).WithArguments(ctx, client.ObjectKey{Namespace: testNS.Name, Name: "chain"}, gotChainSet).Should(Succeed())
			gotChainSet.SetFinalizers([]string{})
			gotChainSet.SetGroupVersionKind(dpuservicev1.ServiceChainSetGroupVersionKind)
			gotChainSet.SetManagedFields(nil)
			Expect(testClient.Patch(ctx, gotChainSet, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())

			// Trigger reconcile to avoid waiting the duration we have specified when objects are not yet deleted in the
			// underlying cluster.
			// TODO: consider if there's ways to speed up this reconcile.
			Eventually(func(g Gomega) {
				g.Expect(testutils.ForceObjectReconcileWithAnnotation(ctx, testClient, dpuServiceChain)).To(Succeed())
			}).Should(Succeed())
		})
	})
})

func getMinimalDPUServiceChain(name string, namespace string, labelSelector *metav1.LabelSelector) *dpuservicev1.DPUServiceChain {
	return &dpuservicev1.DPUServiceChain{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: dpuservicev1.DPUServiceChainSpec{
			Template: dpuservicev1.ServiceChainSetSpecTemplate{
				ObjectMeta: dpuservicev1.ObjectMeta{
					Labels: testutils.GetTestLabels(),
				},
				Spec: *getTestServiceChainSetSpec(labelSelector),
			},
		},
	}
}

func createDPUServiceChain(ctx context.Context, name string, namespace string, labelSelector *metav1.LabelSelector) *dpuservicev1.DPUServiceChain {
	dsc := getMinimalDPUServiceChain(name, namespace, labelSelector)
	Expect(testClient.Create(ctx, dsc)).NotTo(HaveOccurred())
	return dsc
}

func getTestServiceChainSetSpec(labelSelector *metav1.LabelSelector) *dpuservicev1.ServiceChainSetSpec {
	return &dpuservicev1.ServiceChainSetSpec{
		NodeSelector: labelSelector,
		Template: dpuservicev1.ServiceChainSpecTemplate{
			Spec: *getTestServiceChainSpec(),
			ObjectMeta: dpuservicev1.ObjectMeta{
				Labels: testutils.GetTestLabels(),
			},
		},
	}
}

func getExpectedTestServiceChainSetSpec(labelSelector *metav1.LabelSelector) dpuservicev1.ServiceChainSetSpec {
	return dpuservicev1.ServiceChainSetSpec{
		NodeSelector: labelSelector,
		Template: dpuservicev1.ServiceChainSpecTemplate{
			Spec: *getExpectedTestServiceChainSpec(),
			ObjectMeta: dpuservicev1.ObjectMeta{
				Labels: testutils.GetTestLabels(),
			},
		},
	}
}

func getTestServiceChainSpec() *dpuservicev1.ServiceChainSpec {
	return &dpuservicev1.ServiceChainSpec{
		Switches: []dpuservicev1.Switch{
			{
				Ports: []dpuservicev1.Port{
					{
						ServiceInterface: dpuservicev1.ServiceIfc{
							MatchLabels: map[string]string{
								"svc.dpu.nvidia.com/interface": "p0",
							},
						},
					},
				},
			},
		},
	}
}

func getExpectedTestServiceChainSpec() *dpuservicev1.ServiceChainSpec {
	return &dpuservicev1.ServiceChainSpec{
		Switches: []dpuservicev1.Switch{
			{
				Ports: []dpuservicev1.Port{
					{
						ServiceInterface: dpuservicev1.ServiceIfc{
							MatchLabels: map[string]string{
								"svc.dpu.nvidia.com/interface": "p0",
							},
						},
					},
				},
				ServiceMTU: ptr.To(1500),
			},
		},
	}
}

func getMinimalDPFOperatorConfig() *operatorv1.DPFOperatorConfig {
	return &operatorv1.DPFOperatorConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      operatorcontroller.DefaultDPFOperatorConfigSingletonName,
			Namespace: operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace,
		},
		Spec: operatorv1.DPFOperatorConfigSpec{
			DeploymentMode: operatorv1.DeploymentModeHostTrusted,
			ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{
				BFBPersistentVolumeClaimName: ptr.To("name"),
			},
		},
	}
}
