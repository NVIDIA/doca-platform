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

package controllers

import (
	"encoding/json"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	dpucluster "github.com/nvidia/doca-platform/pkg/dpucluster"
	testutils "github.com/nvidia/doca-platform/test/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("DPUServiceNAD Controller", func() {
	Context("When reconciling a resource", func() {
		var testNS *corev1.Namespace
		BeforeEach(func() {
			By("Creating the namespaces")
			testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "testns-"}}
			Expect(testClient.Create(ctx, testNS)).To(Succeed())
			DeferCleanup(testClient.Delete, ctx, testNS)
		})
		It("should successfully reconcile the DPUServiceNAD", func() {
			By("Reconciling the created resource")
			dpuServiceNAD := getMinimalDPUServiceNAD(testNS.Name)
			Expect(testClient.Create(ctx, dpuServiceNAD)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceNAD)
			By("checking that finalizer is added")
			Eventually(func(g Gomega) []string {
				got := &dpuservicev1.DPUServiceNAD{}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuServiceNAD), got)).To(Succeed())
				return got.Finalizers
			}).WithTimeout(10 * time.Second).Should(ConsistOf([]string{dpuServiceNADFinalizer}))
		})
	})

	Context("When checking the behavior on the DPU cluster", func() {
		var testNS *corev1.Namespace
		var dpuClusterClient client.Client
		BeforeEach(func() {
			By("Creating the namespaces")
			testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "testns-"}}
			Expect(testClient.Create(ctx, testNS)).To(Succeed())
			DeferCleanup(testClient.Delete, ctx, testNS)
			By("Adding fake kamaji cluster")
			dpuCluster := testutils.GetTestDPUCluster(testNS.Name, "envtest")
			kamajiSecret, err := testutils.GetFakeKamajiClusterSecretFromEnvtest(dpuCluster, cfg)
			Expect(err).NotTo(HaveOccurred())
			Expect(testClient.Create(ctx, kamajiSecret)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, kamajiSecret)
			Expect(testClient.Create(ctx, &dpuCluster)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, &dpuCluster)
			dpuClusterClient, err = dpucluster.NewConfig(testClient, &dpuCluster).Client(ctx)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should reconcile NAD in DPU cluster", func() {
			By(" Creating the DPUServiceNAD resource ")
			DPUServiceNAD := getDPUServiceNADWithSpec(testNS.Name)

			Expect(testClient.Create(ctx, DPUServiceNAD)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, DPUServiceNAD)

			Eventually(func(g Gomega) {
				got := &unstructured.Unstructured{}
				got.SetGroupVersionKind(schema.GroupVersionKind{
					Group:   "k8s.cni.cncf.io",
					Version: "v1",
					Kind:    "NetworkAttachmentDefinition",
				})
				g.Expect(dpuClusterClient.Get(ctx, client.ObjectKey{Namespace: testNS.Name, Name: "mynad"}, got)).To(Succeed())
				g.Expect(got.GetLabels()).To(HaveKeyWithValue("dpu.nvidia.com/dpuservicenad-name", "mynad"))
				g.Expect(got.GetLabels()).To(HaveKeyWithValue("dpu.nvidia.com/dpuservicenad-namespace", testNS.Name))
				g.Expect(got.GetLabels()).To(HaveKeyWithValue("labelTest", "labelTestValue"))
				g.Expect(got.GetAnnotations()).To(HaveKeyWithValue("annotTest", "annotTestValue"))

				var data map[string]interface{}
				spec, found, err := unstructured.NestedString(got.Object, "spec", "config")
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(found).To(BeTrue())
				err = json.Unmarshal([]byte(spec), &data)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(data["cniVersion"].(string)).To(Equal("0.3.1"))
				g.Expect(data["type"].(string)).To(Equal("ovs"))
				g.Expect(data["bridge"].(string)).To(Equal("test-ovsbridge"))
				g.Expect(data["interface_type"].(string)).To(Equal("dpdk"))
				g.Expect(int(data["mtu"].(float64))).To(Equal(1500))
			}).WithTimeout(60 * time.Second).Should(Succeed())

			By("Removing the DPUServiceNAD resource")
			Expect(testClient.Delete(ctx, DPUServiceNAD)).To(Succeed())

			Eventually(func(g Gomega) {
				got := &unstructured.UnstructuredList{}
				got.SetGroupVersionKind(schema.GroupVersionKind{
					Group:   "k8s.cni.cncf.io",
					Version: "v1",
					Kind:    "NetworkAttachmentDefinitionList",
				})
				g.Expect(dpuClusterClient.List(ctx, got)).To(Succeed())
				g.Expect(got.Items).To(BeEmpty())
			}).WithTimeout(60 * time.Second).Should(Succeed())

		})

		It("should reconcile NAD with IPAM in DPU cluster", func() {
			By("Creating the DPUServiceNAD resource ")
			DPUServiceNAD := getDPUServiceNADWithSpec(testNS.Name)
			DPUServiceNAD.Spec.IPAM = true
			Expect(testClient.Create(ctx, DPUServiceNAD)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, DPUServiceNAD)

			Eventually(func(g Gomega) {
				got := &unstructured.Unstructured{}
				got.SetGroupVersionKind(schema.GroupVersionKind{
					Group:   "k8s.cni.cncf.io",
					Version: "v1",
					Kind:    "NetworkAttachmentDefinition",
				})
				g.Expect(dpuClusterClient.Get(ctx, client.ObjectKey{Namespace: testNS.Name, Name: "mynad"}, got)).To(Succeed())
				g.Expect(got.GetLabels()).To(HaveKeyWithValue("dpu.nvidia.com/dpuservicenad-name", "mynad"))
				g.Expect(got.GetLabels()).To(HaveKeyWithValue("dpu.nvidia.com/dpuservicenad-namespace", testNS.Name))
				g.Expect(got.GetLabels()).To(HaveKeyWithValue("labelTest", "labelTestValue"))
				g.Expect(got.GetAnnotations()).To(HaveKeyWithValue("annotTest", "annotTestValue"))

				var data map[string]interface{}
				spec, found, err := unstructured.NestedString(got.Object, "spec", "config")
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(found).To(BeTrue())
				err = json.Unmarshal([]byte(spec), &data)
				g.Expect(err).ToNot(HaveOccurred())
				ipamData, ok := data["ipam"].(map[string]interface{})
				g.Expect(ok).To(BeTrue())
				g.Expect(ipamData["type"].(string)).To(Equal("nv-ipam"))
				g.Expect(data["cniVersion"].(string)).To(Equal("0.3.1"))
				g.Expect(data["type"].(string)).To(Equal("ovs"))
				g.Expect(data["bridge"].(string)).To(Equal("test-ovsbridge"))
				g.Expect(data["interface_type"].(string)).To(Equal("dpdk"))
				g.Expect(int(data["mtu"].(float64))).To(Equal(1500))

			}).WithTimeout(60 * time.Second).Should(Succeed())

			By("Removing the DPUServiceNAD resource")
			Expect(testClient.Delete(ctx, DPUServiceNAD)).To(Succeed())

			Eventually(func(g Gomega) {
				got := &unstructured.UnstructuredList{}
				got.SetGroupVersionKind(schema.GroupVersionKind{
					Group:   "k8s.cni.cncf.io",
					Version: "v1",
					Kind:    "NetworkAttachmentDefinitionList",
				})
				g.Expect(dpuClusterClient.List(ctx, got)).To(Succeed())
				g.Expect(got.Items).To(BeEmpty())
			}).WithTimeout(60 * time.Second).Should(Succeed())
		})

		It("should reconcile NAD with ResourceName(vf) annotation in DPU cluster", func() {
			By("Creating the DPUServiceNAD resource ")
			DPUServiceNAD := getDPUServiceNADWithSpec(testNS.Name)
			DPUServiceNAD.Spec.ResourceType = "vf"
			Expect(testClient.Create(ctx, DPUServiceNAD)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, DPUServiceNAD)

			// refer this NAD in dpuserviceinterface and dpuservice
			Eventually(func(g Gomega) {
				got := &unstructured.Unstructured{}
				got.SetGroupVersionKind(schema.GroupVersionKind{
					Group:   "k8s.cni.cncf.io",
					Version: "v1",
					Kind:    "NetworkAttachmentDefinition",
				})
				g.Expect(dpuClusterClient.Get(ctx, client.ObjectKey{Namespace: testNS.Name, Name: "mynad"}, got)).To(Succeed())
				g.Expect(got.GetLabels()).To(HaveKeyWithValue("dpu.nvidia.com/dpuservicenad-name", "mynad"))
				g.Expect(got.GetLabels()).To(HaveKeyWithValue("dpu.nvidia.com/dpuservicenad-namespace", testNS.Name))
				g.Expect(got.GetLabels()).To(HaveKeyWithValue("labelTest", "labelTestValue"))
				g.Expect(got.GetAnnotations()).To(HaveKeyWithValue("annotTest", "annotTestValue"))
				g.Expect(got.GetAnnotations()).To(HaveKeyWithValue("k8s.v1.cni.cncf.io/resourceName", "nvidia.com/bf_vf"))

			}).WithTimeout(60 * time.Second).Should(Succeed())

			By("Removing the DPUServiceNAD resource")
			Expect(testClient.Delete(ctx, DPUServiceNAD)).To(Succeed())

			Eventually(func(g Gomega) {
				got := &unstructured.UnstructuredList{}
				got.SetGroupVersionKind(schema.GroupVersionKind{
					Group:   "k8s.cni.cncf.io",
					Version: "v1",
					Kind:    "NetworkAttachmentDefinitionList",
				})
				g.Expect(dpuClusterClient.List(ctx, got)).To(Succeed())
				g.Expect(got.Items).To(BeEmpty())
			}).WithTimeout(60 * time.Second).Should(Succeed())
		})

		It("should reconcile NAD in DPU cluster with maximum name length", func() {
			By("Creating the DPUServiceNAD resource")
			dpuServiceNAD := getDPUServiceNADWithSpec(testNS.Name)
			dpuServiceNAD.Name = utilrand.String(63)

			Expect(testClient.Create(ctx, dpuServiceNAD)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceNAD)

			Eventually(func(g Gomega) {
				got := &unstructured.Unstructured{}
				got.SetGroupVersionKind(schema.GroupVersionKind{
					Group:   "k8s.cni.cncf.io",
					Version: "v1",
					Kind:    "NetworkAttachmentDefinition",
				})
				g.Expect(dpuClusterClient.Get(ctx, client.ObjectKey{Namespace: testNS.Name, Name: dpuServiceNAD.Name}, got)).To(Succeed())
			}).WithTimeout(60 * time.Second).Should(Succeed())
		})

		It("should fail to create a DPUServiceNAD with name exceeding the maximum length", func() {
			By("Create ServiceInterfaceSet, without Node Selector")
			dpuServiceNAD := getDPUServiceNADWithSpec(testNS.Name)
			dpuServiceNAD.Name = utilrand.String(64)
			Expect(testClient.Create(ctx, dpuServiceNAD)).To(HaveOccurred())
		})
	})
})

func getMinimalDPUServiceNAD(namespace string) *dpuservicev1.DPUServiceNAD {
	return &dpuservicev1.DPUServiceNAD{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dpuservicenad",
			Namespace: namespace,
		},
		Spec: dpuservicev1.DPUServiceNADSpec{
			ResourceType: "sf",
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
