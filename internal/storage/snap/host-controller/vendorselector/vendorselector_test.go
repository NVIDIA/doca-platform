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

package vendorselector

import (
	"context"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	storagev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"
	"github.com/nvidia/doca-platform/internal/storage/snap/host-controller/dpuclusterhelper"
	"github.com/nvidia/doca-platform/internal/storage/snap/host-controller/indexers"
	"github.com/nvidia/doca-platform/pkg/conditions"
	"github.com/nvidia/doca-platform/pkg/dpucluster"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corestoragev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	testNamespace  = "test-namespace"
	testDPUCluster = "test-dpu-cluster"
)

func getDPUVolume(name string) *storagev1.DPUVolume {
	return &storagev1.DPUVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: storagev1.DPUVolumeSpec{
			DPUStoragePolicyName: "test-policy",
		},
		Status: storagev1.DPUVolumeStatus{
			State: &storagev1.DPUVolumeState{
				Parameters: make(map[string]string),
			},
		},
	}
}

func getDPUStoragePolicy(vendors []string) *storagev1.DPUStoragePolicy {
	policy := &storagev1.DPUStoragePolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-policy",
			Namespace: testNamespace,
		},
		Spec: storagev1.DPUStoragePolicySpec{
			DPUStorageVendors: vendors,
		},
	}
	conditions.AddTrue(policy, conditions.TypeReady)
	return policy
}

func getDPUStorageVendor(name string, clusters []storagev1.ObjectReference) *storagev1.DPUStorageVendor {
	return &storagev1.DPUStorageVendor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: storagev1.DPUStorageVendorSpec{
			StorageClassName: "test-storage-class",
			PluginName:       "test-plugin",
		},
		Status: storagev1.DPUStorageVendorStatus{
			DPUClusters: clusters,
		},
	}
}

func getDPUCluster() *provisioningv1.DPUCluster {
	return &provisioningv1.DPUCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testDPUCluster,
			Namespace: testNamespace,
		},
	}
}

func getStorageClass(provisioner string) *corestoragev1.StorageClass {
	return &corestoragev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-storage-class",
		},
		Provisioner: provisioner,
	}
}

func getCSIDriver() *corestoragev1.CSIDriver {
	return &corestoragev1.CSIDriver{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-csi-driver",
		},
		Spec: corestoragev1.CSIDriverSpec{
			AttachRequired: ptr.To(true),
		},
	}
}

var _ = Describe("DPUVolume Vendor Selector", func() {
	var (
		ctx            context.Context
		fakeClient     client.Client
		fakeDPUClient  client.Client
		vendorSelector VendorSelector
		dpuVolume      *storagev1.DPUVolume
		clusterKey     client.ObjectKey
	)

	BeforeEach(func() {
		ctx = context.Background()
		// Setup fake clients with scheme
		s := scheme.Scheme
		Expect(provisioningv1.AddToScheme(s)).To(Succeed())
		Expect(storagev1.AddToScheme(s)).To(Succeed())
		// Create fake clients
		fakeClient = fake.NewClientBuilder().
			WithScheme(s).
			WithIndex(&storagev1.DPUVolume{}, indexers.DPUVolumeStatusStateSelectedDPUStorageVendorName, func(obj client.Object) []string {
				dpuVolume := obj.(*storagev1.DPUVolume)
				if dpuVolume.Status.State != nil && dpuVolume.Status.State.SelectedDPUStorageVendorName != nil {
					return []string{*dpuVolume.Status.State.SelectedDPUStorageVendorName}
				}
				return []string{}
			}).
			Build()
		fakeDPUClient = fake.NewClientBuilder().WithScheme(s).Build()
		clusterKey = client.ObjectKey{Name: testDPUCluster, Namespace: testNamespace}
		clusterClientProvider := dpucluster.NewStaticClusterClientProvider(map[client.ObjectKey]client.Client{
			clusterKey: fakeDPUClient,
		})
		dpuHelper := dpuclusterhelper.New(fakeClient, clusterClientProvider)
		vendorSelector = New(fakeClient, dpuHelper, Options{Namespace: testNamespace})
		dpuVolume = getDPUVolume("test-volume")
	})

	Context("SelectVendorForDPUVolume", func() {
		It("should return not selected when DPUStoragePolicy is not found", func() {
			dpuVolume.Spec.DPUStoragePolicyName = "non-existent-policy"

			result, err := vendorSelector.SelectVendorForDPUVolume(ctx, dpuVolume)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Selected).To(BeFalse())
			Expect(result.Reason).To(ContainSubstring("DPUStoragePolicy"))
			Expect(result.Reason).To(ContainSubstring("not found"))
			Expect(result.SelectedVendorInfo).To(BeNil())
		})
		It("should return not selected when DPUStoragePolicy is not ready", func() {
			policy := getDPUStoragePolicy([]string{"test-vendor"})
			// Remove the ready condition
			policy.Status.Conditions = nil

			Expect(fakeClient.Create(ctx, policy)).To(Succeed())

			result, err := vendorSelector.SelectVendorForDPUVolume(ctx, dpuVolume)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Selected).To(BeFalse())
			Expect(result.Reason).To(ContainSubstring("DPUStoragePolicy"))
			Expect(result.Reason).To(ContainSubstring("is not ready"))
			Expect(result.SelectedVendorInfo).To(BeNil())
		})
		It("should return error when DPUStorageVendor is not found", func() {
			policy := getDPUStoragePolicy([]string{"non-existent-vendor"})
			Expect(fakeClient.Create(ctx, policy)).To(Succeed())

			_, err := vendorSelector.SelectVendorForDPUVolume(ctx, dpuVolume)
			Expect(err).To(MatchError(ContainSubstring("not found")))
		})
		It("should return error when DPUStorageVendor has no available DPUClusters", func() {
			vendor := getDPUStorageVendor("test-vendor", []storagev1.ObjectReference{})
			Expect(fakeClient.Create(ctx, vendor)).To(Succeed())

			policy := getDPUStoragePolicy([]string{"test-vendor"})
			Expect(fakeClient.Create(ctx, policy)).To(Succeed())

			_, err := vendorSelector.SelectVendorForDPUVolume(ctx, dpuVolume)
			Expect(err).To(MatchError(ContainSubstring("no DPUClusters available")))
		})
		It("should return error when DPUCluster is not found", func() {
			vendor := getDPUStorageVendor("test-vendor", []storagev1.ObjectReference{
				{Name: "non-existent-cluster", Namespace: testNamespace},
			})
			Expect(fakeClient.Create(ctx, vendor)).To(Succeed())

			policy := getDPUStoragePolicy([]string{"test-vendor"})
			Expect(fakeClient.Create(ctx, policy)).To(Succeed())

			_, err := vendorSelector.SelectVendorForDPUVolume(ctx, dpuVolume)
			Expect(err).To(MatchError(ContainSubstring("not found")))
		})
		It("should return error when StorageClass is not found in DPU cluster", func() {
			cluster := getDPUCluster()
			Expect(fakeClient.Create(ctx, cluster)).To(Succeed())

			vendor := getDPUStorageVendor("test-vendor", []storagev1.ObjectReference{
				{Name: testDPUCluster, Namespace: testNamespace},
			})
			vendor.Spec.StorageClassName = "non-existent-storage-class"
			Expect(fakeClient.Create(ctx, vendor)).To(Succeed())

			policy := getDPUStoragePolicy([]string{"test-vendor"})
			Expect(fakeClient.Create(ctx, policy)).To(Succeed())

			_, err := vendorSelector.SelectVendorForDPUVolume(ctx, dpuVolume)
			Expect(err).To(MatchError(ContainSubstring("storageclasses")))
		})
		It("should return error when CSIDriver is not found in DPU cluster", func() {
			storageClass := getStorageClass("non-existent-csi-driver")
			Expect(fakeDPUClient.Create(ctx, storageClass)).To(Succeed())

			cluster := getDPUCluster()
			Expect(fakeClient.Create(ctx, cluster)).To(Succeed())

			vendor := getDPUStorageVendor("test-vendor", []storagev1.ObjectReference{
				{Name: testDPUCluster, Namespace: testNamespace},
			})
			Expect(fakeClient.Create(ctx, vendor)).To(Succeed())

			policy := getDPUStoragePolicy([]string{"test-vendor"})
			Expect(fakeClient.Create(ctx, policy)).To(Succeed())

			_, err := vendorSelector.SelectVendorForDPUVolume(ctx, dpuVolume)
			Expect(err).To(MatchError(ContainSubstring("csidrivers")))
		})
		It("should successfully select vendor for DPUVolume with all required resources", func() {
			csiDriver := getCSIDriver()
			Expect(fakeDPUClient.Create(ctx, csiDriver)).To(Succeed())

			storageClass := getStorageClass("test-csi-driver")
			Expect(fakeDPUClient.Create(ctx, storageClass)).To(Succeed())

			cluster := getDPUCluster()
			Expect(fakeClient.Create(ctx, cluster)).To(Succeed())

			vendor := getDPUStorageVendor("test-vendor", []storagev1.ObjectReference{
				{Name: testDPUCluster, Namespace: testNamespace},
			})
			Expect(fakeClient.Create(ctx, vendor)).To(Succeed())

			policy := getDPUStoragePolicy([]string{"test-vendor"})
			policy.Spec.Parameters = map[string]string{
				"policy-param": "policy-value",
			}
			Expect(fakeClient.Create(ctx, policy)).To(Succeed())

			dpuVolume.Spec.Parameters = map[string]string{
				"volume-param": "volume-value",
			}

			result, err := vendorSelector.SelectVendorForDPUVolume(ctx, dpuVolume)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Selected).To(BeTrue())
			Expect(result.SelectedVendorInfo).NotTo(BeNil())

			// Verify all vendor selection fields are set correctly
			Expect(result.SelectedVendorInfo.DPUClusterName).To(Equal(testDPUCluster))
			Expect(result.SelectedVendorInfo.DPUClusterNamespace).To(Equal(testNamespace))
			Expect(result.SelectedVendorInfo.SelectedDPUStorageVendorName).To(Equal("test-vendor"))
			Expect(result.SelectedVendorInfo.StorageVendorPluginName).To(Equal("test-plugin"))
			Expect(result.SelectedVendorInfo.StorageClassName).To(Equal("test-storage-class"))
			Expect(result.SelectedVendorInfo.CSIDriverName).To(Equal("test-csi-driver"))

			// Verify parameters are merged correctly (volume parameters take precedence)
			Expect(result.SelectedVendorInfo.Parameters).To(HaveKeyWithValue("policy-param", "policy-value"))
			Expect(result.SelectedVendorInfo.Parameters).To(HaveKeyWithValue("volume-param", "volume-value"))
		})
	})
	Context("Storage vendor selection algorithms", func() {
		It("should select vendor with fewest volumes when using NumberVolumes algorithm", func() {
			// Setup common resources
			csiDriver := getCSIDriver()
			Expect(fakeDPUClient.Create(ctx, csiDriver)).To(Succeed())

			storageClass := getStorageClass("test-csi-driver")
			Expect(fakeDPUClient.Create(ctx, storageClass)).To(Succeed())

			cluster := getDPUCluster()
			Expect(fakeClient.Create(ctx, cluster)).To(Succeed())

			// Create vendors
			vendor1 := getDPUStorageVendor("vendor1", []storagev1.ObjectReference{
				{Name: testDPUCluster, Namespace: testNamespace},
			})
			Expect(fakeClient.Create(ctx, vendor1)).To(Succeed())
			vendor2 := getDPUStorageVendor("vendor2", []storagev1.ObjectReference{
				{Name: testDPUCluster, Namespace: testNamespace},
			})
			Expect(fakeClient.Create(ctx, vendor2)).To(Succeed())

			// Create existing volumes for vendor2 to make it have more volumes
			existingVolume1 := getDPUVolume("existing-volume1")
			existingVolume1.Status.State.SelectedDPUStorageVendorName = ptr.To("vendor2")
			Expect(fakeClient.Create(ctx, existingVolume1)).To(Succeed())

			existingVolume2 := getDPUVolume("existing-volume2")
			existingVolume2.Status.State.SelectedDPUStorageVendorName = ptr.To("vendor2")
			Expect(fakeClient.Create(ctx, existingVolume2)).To(Succeed())

			policy := getDPUStoragePolicy([]string{"vendor1", "vendor2"})
			policy.Spec.SelectionAlgorithm = ptr.To(storagev1.SelectionAlgorithmNumberVolumes)
			Expect(fakeClient.Create(ctx, policy)).To(Succeed())

			result, err := vendorSelector.SelectVendorForDPUVolume(ctx, dpuVolume)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Selected).To(BeTrue())
			Expect(result.SelectedVendorInfo).NotTo(BeNil())
			// Should select vendor1 as it has fewer volumes (0) compared to vendor2 (2)
			Expect(result.SelectedVendorInfo.SelectedDPUStorageVendorName).To(Equal("vendor1"))
		})
		It("should select alphabetically first vendor when multiple vendors have same minimum number of volumes", func() {
			// Setup common resources
			csiDriver := getCSIDriver()
			Expect(fakeDPUClient.Create(ctx, csiDriver)).To(Succeed())

			storageClass := getStorageClass("test-csi-driver")
			Expect(fakeDPUClient.Create(ctx, storageClass)).To(Succeed())

			cluster := getDPUCluster()
			Expect(fakeClient.Create(ctx, cluster)).To(Succeed())

			// Create two vendors with names that test alphabetical ordering
			vendorB := getDPUStorageVendor("vendor-b", []storagev1.ObjectReference{
				{Name: testDPUCluster, Namespace: testNamespace},
			})
			Expect(fakeClient.Create(ctx, vendorB)).To(Succeed())
			vendorA := getDPUStorageVendor("vendor-a", []storagev1.ObjectReference{
				{Name: testDPUCluster, Namespace: testNamespace},
			})
			Expect(fakeClient.Create(ctx, vendorA)).To(Succeed())

			// Create same number of existing volumes for both vendors (1 each)
			// Both will have the minimum count, triggering the selected code path
			existingVolume1 := getDPUVolume("existing-volume1")
			existingVolume1.Status.State.SelectedDPUStorageVendorName = ptr.To("vendor-a")
			Expect(fakeClient.Create(ctx, existingVolume1)).To(Succeed())

			existingVolume2 := getDPUVolume("existing-volume2")
			existingVolume2.Status.State.SelectedDPUStorageVendorName = ptr.To("vendor-b")
			Expect(fakeClient.Create(ctx, existingVolume2)).To(Succeed())

			policy := getDPUStoragePolicy([]string{"vendor-b", "vendor-a"})
			policy.Spec.SelectionAlgorithm = ptr.To(storagev1.SelectionAlgorithmNumberVolumes)
			Expect(fakeClient.Create(ctx, policy)).To(Succeed())

			result, err := vendorSelector.SelectVendorForDPUVolume(ctx, dpuVolume)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Selected).To(BeTrue())
			Expect(result.SelectedVendorInfo).NotTo(BeNil())
			// Should select vendor-a as it's alphabetically first among vendors with minimum volumes (1 each)
			Expect(result.SelectedVendorInfo.SelectedDPUStorageVendorName).To(Equal("vendor-a"))
		})
		It("should use random selection when algorithm is Random", func() {
			// Setup common resources
			csiDriver := getCSIDriver()
			Expect(fakeDPUClient.Create(ctx, csiDriver)).To(Succeed())

			storageClass := getStorageClass("test-csi-driver")
			Expect(fakeDPUClient.Create(ctx, storageClass)).To(Succeed())

			cluster := getDPUCluster()
			Expect(fakeClient.Create(ctx, cluster)).To(Succeed())

			vendor1 := getDPUStorageVendor("vendor1", []storagev1.ObjectReference{
				{Name: testDPUCluster, Namespace: testNamespace},
			})
			Expect(fakeClient.Create(ctx, vendor1)).To(Succeed())

			vendor2 := getDPUStorageVendor("vendor2", []storagev1.ObjectReference{
				{Name: testDPUCluster, Namespace: testNamespace},
			})
			Expect(fakeClient.Create(ctx, vendor2)).To(Succeed())

			policy := getDPUStoragePolicy([]string{"vendor1", "vendor2"})
			policy.Spec.SelectionAlgorithm = ptr.To(storagev1.SelectionAlgorithmRandom)
			Expect(fakeClient.Create(ctx, policy)).To(Succeed())

			result, err := vendorSelector.SelectVendorForDPUVolume(ctx, dpuVolume)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Selected).To(BeTrue())
			Expect(result.SelectedVendorInfo).NotTo(BeNil())
			// Should select one of the vendors
			Expect(result.SelectedVendorInfo.SelectedDPUStorageVendorName).To(BeElementOf("vendor1", "vendor2"))
		})
	})
})
