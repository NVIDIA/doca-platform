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

package volumeprovisioner

import (
	"context"
	"errors"
	"strings"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	storagev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"
	"github.com/nvidia/doca-platform/internal/storage/snap/host-controller/dpuclusterhelper"
	"github.com/nvidia/doca-platform/internal/storage/snap/host-controller/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

const (
	testPVName = "test-pv"
)

var (
	errTest = errors.New("test error")
)

func getTestDPUVolume() *storagev1.DPUVolume {
	storageClassName := testStorageClass
	csiDriverName := testCSIDriver
	vendorName := testVendorName
	pluginName := testPluginName
	return &storagev1.DPUVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-volume",
			Namespace: "default",
		},
		Spec: storagev1.DPUVolumeSpec{
			DPUStoragePolicyName: "test-policy",
			VolumeMode:           ptr.To(corev1.PersistentVolumeFilesystem),
			AccessModes:          []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("1Gi"),
				},
			},
		},
		Status: storagev1.DPUVolumeStatus{
			State: &storagev1.DPUVolumeState{
				StorageClassName:             &storageClassName,
				CSIDriverName:                &csiDriverName,
				SelectedDPUStorageVendorName: &vendorName,
				StorageVendorPluginName:      &pluginName,
			},
		},
	}
}

func getTestDPUCluster(name string) provisioningv1.DPUCluster {
	return provisioningv1.DPUCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
	}
}

func getTestPVC(name string) *corev1.PersistentVolumeClaim {
	storageClassName := testStorageClass
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testTargetNamespace,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: &storageClassName,
			VolumeMode:       ptr.To(corev1.PersistentVolumeFilesystem),
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("1Gi"),
				},
			},
		},
		Status: corev1.PersistentVolumeClaimStatus{
			Phase: corev1.ClaimBound,
		},
	}
}

func getTestPV(name, pvcName string) *corev1.PersistentVolume {
	return &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: corev1.PersistentVolumeSpec{
			Capacity: corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("1Gi"),
			},
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			VolumeMode:  ptr.To(corev1.PersistentVolumeFilesystem),
			PersistentVolumeSource: corev1.PersistentVolumeSource{
				CSI: &corev1.CSIPersistentVolumeSource{
					Driver:       testCSIDriver,
					VolumeHandle: "test-handle",
					VolumeAttributes: map[string]string{
						"attr1": "value1",
					},
				},
			},
			ClaimRef: &corev1.ObjectReference{
				Name:      pvcName,
				Namespace: testTargetNamespace,
			},
		},
	}
}

func getTestVolumeCR(name string) *storagev1.Volume {
	return &storagev1.Volume{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testTargetNamespace,
		},
		Spec: storagev1.VolumeSpec{
			Request: storagev1.VolumeRequest{
				CapacityRange: storagev1.CapacityRange{
					Request: resource.MustParse("1Gi"),
				},
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				VolumeMode:  ptr.To(corev1.PersistentVolumeFilesystem),
			},
			VolumeSpecDPU: storagev1.VolumeSpecDPU{
				ID:                      name,
				AccessModes:             []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				ReclaimPolicy:           corev1.PersistentVolumeReclaimDelete,
				StorageVendorName:       testVendorName,
				StorageVendorPluginName: testPluginName,
				CSIReference: storagev1.CSIReference{
					CSIDriverName:    testCSIDriver,
					StorageClassName: testStorageClass,
				},
			},
		},
		Status: storagev1.VolumeStatus{
			State: storagev1.VolumeStateAvailable,
		},
	}
}

// Helper function to create objects with deletion timestamp
func getTestPVCWithDeletionTimestamp(name string) *corev1.PersistentVolumeClaim {
	pvc := getTestPVC(name)
	now := metav1.Now()
	pvc.DeletionTimestamp = &now
	pvc.Finalizers = []string{"test-finalizer"}
	return pvc
}

func getTestVolumeCRWithDeletionTimestamp(name string) *storagev1.Volume {
	volume := getTestVolumeCR(name)
	now := metav1.Now()
	volume.DeletionTimestamp = &now
	volume.Finalizers = []string{"test-finalizer"}
	return volume
}

// Helper function to create PVC with wrong spec
func getTestPVCWithWrongSpec(name string) *corev1.PersistentVolumeClaim {
	pvc := getTestPVC(name)
	wrongStorageClass := "wrong-storage-class"
	pvc.Spec.StorageClassName = &wrongStorageClass
	return pvc
}

// Helper function to create Volume CR with wrong spec
func getTestVolumeCRWithWrongSpec(name string) *storagev1.Volume {
	volume := getTestVolumeCR(name)
	volume.Spec.Request.CapacityRange.Request = resource.MustParse("2Gi") // Different size
	return volume
}

// Helper to create interceptor that fails on PVC creation
func createPVCFailureInterceptor() interceptor.Funcs {
	return interceptor.Funcs{
		Create: func(ctx context.Context, client client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			if _, ok := obj.(*corev1.PersistentVolumeClaim); ok {
				return errTest
			}
			return client.Create(ctx, obj, opts...)
		},
	}
}

// Helper to create interceptor that fails on Volume CR creation
func createVolumeCRFailureInterceptor() interceptor.Funcs {
	return interceptor.Funcs{
		Create: func(ctx context.Context, client client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			if _, ok := obj.(*storagev1.Volume); ok {
				return errTest
			}
			return client.Create(ctx, obj, opts...)
		},
	}
}

// Helper to create interceptor that fails on update operations
func createUpdateFailureInterceptor() interceptor.Funcs {
	return interceptor.Funcs{
		Update: func(ctx context.Context, client client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
			return errTest
		},
	}
}

// Helper to setup cluster clients with optional interceptors
func setupClusterClients(primaryObjects, secondaryObjects []client.Object) (dpuclusterhelper.ClientForDPUCluster, []dpuclusterhelper.ClientForDPUCluster) {
	return setupClusterClientsWithInterceptors(primaryObjects, secondaryObjects, nil, nil)
}

func setupClusterClientsWithInterceptors(primaryObjects, secondaryObjects []client.Object,
	primaryInterceptor, secondaryInterceptor *interceptor.Funcs) (dpuclusterhelper.ClientForDPUCluster, []dpuclusterhelper.ClientForDPUCluster) {

	primaryCluster := getTestDPUCluster("primary-cluster")
	secondaryCluster := getTestDPUCluster("secondary-cluster")

	primaryBuilder := getFakeClientBuilder().WithObjects(primaryObjects...)
	if primaryInterceptor != nil {
		primaryBuilder = primaryBuilder.WithInterceptorFuncs(*primaryInterceptor)
	}
	primaryFakeClient := primaryBuilder.Build()

	secondaryBuilder := getFakeClientBuilder().WithObjects(secondaryObjects...)
	if secondaryInterceptor != nil {
		secondaryBuilder = secondaryBuilder.WithInterceptorFuncs(*secondaryInterceptor)
	}
	secondaryFakeClient := secondaryBuilder.Build()

	clientForPrimary := dpuclusterhelper.ClientForDPUCluster{
		Client:     primaryFakeClient,
		DPUCluster: &primaryCluster,
	}

	targetClusters := []dpuclusterhelper.ClientForDPUCluster{
		{
			Client:     primaryFakeClient,
			DPUCluster: &primaryCluster,
		},
		{
			Client:     secondaryFakeClient,
			DPUCluster: &secondaryCluster,
		},
	}

	return clientForPrimary, targetClusters
}

var _ = Describe("VolumeProvisioner", func() {
	var (
		ctx         context.Context
		provisioner VolumeProvisioner
		dpuVolume   *storagev1.DPUVolume
	)

	BeforeEach(func() {
		ctx = context.Background()
		provisioner = New(testTargetNamespace, utils.New("test-annotation"))
		dpuVolume = getTestDPUVolume()
	})

	Context("Successful Provisioning", func() {
		It("should create PVC when starting from scratch", func() {
			// Start with completely empty clusters
			clientForPrimary, targetClusters := setupClusterClients(
				[]client.Object{}, // Primary cluster is empty
				[]client.Object{}, // Secondary cluster is empty
			)

			result, err := provisioner.Provision(ctx, clientForPrimary, targetClusters, dpuVolume)

			Expect(err).ToNot(HaveOccurred())
			// Result should not be ready because PVC won't be bound without a PV
			Expect(result.Ready).To(BeFalse())

			// Verify PVC was created in primary cluster
			pvcList := &corev1.PersistentVolumeClaimList{}
			Expect(clientForPrimary.Client.List(ctx, pvcList)).To(Succeed())
			Expect(pvcList.Items).To(HaveLen(1))

			createdPVC := pvcList.Items[0]
			Expect(createdPVC.Name).To(Equal(dpuVolume.Name + "-pvc"))
			Expect(createdPVC.Namespace).To(Equal(testTargetNamespace))
			Expect(createdPVC.Spec.StorageClassName).To(Equal(dpuVolume.Status.State.StorageClassName))
			Expect(createdPVC.Spec.VolumeMode).To(Equal(dpuVolume.Spec.VolumeMode))
			Expect(createdPVC.Spec.AccessModes).To(Equal(dpuVolume.Spec.AccessModes))
			Expect(createdPVC.Spec.Resources).To(Equal(dpuVolume.Spec.Resources))
			Expect(createdPVC.Annotations).To(HaveKey("test-annotation"))

			// Volume CRs should not be created yet since PVC is not bound
			for _, targetCluster := range targetClusters {
				volumeList := &storagev1.VolumeList{}
				Expect(targetCluster.Client.List(ctx, volumeList)).To(Succeed())
				Expect(volumeList.Items).To(BeEmpty())
			}
		})
		It("should complete successfully when PVC and PV are bound and create Volume CRs", func() {
			existingPVC := getTestPVC(dpuVolume.Name + "-pvc")
			existingPVC.Spec.VolumeName = "existing-pv"
			existingPVC.Status.Phase = corev1.ClaimBound
			existingPV := getTestPV("existing-pv", dpuVolume.Name)

			clientForPrimary, targetClusters := setupClusterClients(
				[]client.Object{existingPVC, existingPV},
				[]client.Object{},
			)

			result, err := provisioner.Provision(ctx, clientForPrimary, targetClusters, dpuVolume)

			Expect(err).ToNot(HaveOccurred())
			Expect(result.Ready).To(BeTrue())
			Expect(result.Data).ToNot(BeNil())
			Expect(result.Data.PVCName).To(Equal(dpuVolume.Name + "-pvc"))
			Expect(result.Data.VolumeName).To(Equal("existing-pv"))

			// Verify existing PVC was not recreated
			pvcList := &corev1.PersistentVolumeClaimList{}
			Expect(clientForPrimary.Client.List(ctx, pvcList)).To(Succeed())
			Expect(pvcList.Items).To(HaveLen(1))

			// Verify Volume CRs were created in both target clusters
			for i, targetCluster := range targetClusters {
				volumeList := &storagev1.VolumeList{}
				Expect(targetCluster.Client.List(ctx, volumeList)).To(Succeed())
				Expect(volumeList.Items).To(HaveLen(1), "Volume CR should exist in target cluster %d", i)

				volumeCR := volumeList.Items[0]
				Expect(volumeCR.Name).To(Equal(dpuVolume.Name))
				Expect(volumeCR.Namespace).To(Equal(testTargetNamespace))
				Expect(volumeCR.Status.State).To(Equal(storagev1.VolumeStateAvailable))
			}
		})
		It("should update existing Volume CR with new spec", func() {
			pvc := getTestPVC(dpuVolume.Name + "-pvc")
			pvc.Spec.VolumeName = testPVName
			pvc.Status.Phase = corev1.ClaimBound
			pv := getTestPV(testPVName, dpuVolume.Name)

			// Create existing Volume CR with wrong spec
			existingVolume := getTestVolumeCRWithWrongSpec(dpuVolume.Name)

			clientForPrimary, targetClusters := setupClusterClients(
				[]client.Object{pvc, pv},
				[]client.Object{existingVolume},
			)

			result, err := provisioner.Provision(ctx, clientForPrimary, targetClusters, dpuVolume)

			Expect(err).ToNot(HaveOccurred())
			Expect(result.Ready).To(BeTrue())

			// Verify Volume CR was updated
			updatedVolume := &storagev1.Volume{}
			err = targetClusters[1].Client.Get(ctx, client.ObjectKey{
				Namespace: testTargetNamespace,
				Name:      dpuVolume.Name,
			}, updatedVolume)
			Expect(err).ToNot(HaveOccurred())
			Expect(updatedVolume.Spec.Request.CapacityRange.Request).To(Equal(resource.MustParse("1Gi")))
		})
	})

	Context("PVC Failures", func() {
		It("should return error when PVC creation fails", func() {
			pvcFailure := createPVCFailureInterceptor()
			clientForPrimary, targetClusters := setupClusterClientsWithInterceptors(
				[]client.Object{},
				[]client.Object{},
				&pvcFailure,
				nil,
			)

			result, err := provisioner.Provision(ctx, clientForPrimary, targetClusters, dpuVolume)
			Expect(err).To(MatchError(errTest))
			Expect(result.Ready).To(BeFalse())
		})
		It("should wait when PVC is being deleted", func() {
			pvcWithDeletion := getTestPVCWithDeletionTimestamp(dpuVolume.Name + "-pvc")

			clientForPrimary, targetClusters := setupClusterClients(
				[]client.Object{pvcWithDeletion},
				[]client.Object{},
			)

			result, err := provisioner.Provision(ctx, clientForPrimary, targetClusters, dpuVolume)

			Expect(err).ToNot(HaveOccurred())
			Expect(result.Ready).To(BeFalse())
			Expect(result.Reason).To(ContainSubstring("is being deleted"))
		})
		It("should recreate PVC when spec is incorrect", func() {
			wrongPVC := getTestPVCWithWrongSpec(dpuVolume.Name + "-pvc")

			clientForPrimary, targetClusters := setupClusterClients(
				[]client.Object{wrongPVC},
				[]client.Object{},
			)

			result, err := provisioner.Provision(ctx, clientForPrimary, targetClusters, dpuVolume)

			Expect(err).ToNot(HaveOccurred())
			Expect(result.Ready).To(BeFalse())
			Expect(result.Reason).To(ContainSubstring("has incorrect spec"))

			// Verify PVC was deleted
			pvc := &corev1.PersistentVolumeClaim{}
			err = clientForPrimary.Client.Get(ctx, client.ObjectKey{
				Namespace: testTargetNamespace,
				Name:      dpuVolume.Name + "-pvc",
			}, pvc)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})
		It("should wait when PVC is not bound", func() {
			unboundPVC := getTestPVC(dpuVolume.Name + "-pvc")
			unboundPVC.Status.Phase = corev1.ClaimPending

			clientForPrimary, targetClusters := setupClusterClients(
				[]client.Object{unboundPVC},
				[]client.Object{},
			)

			result, err := provisioner.Provision(ctx, clientForPrimary, targetClusters, dpuVolume)

			Expect(err).ToNot(HaveOccurred())
			Expect(result.Ready).To(BeFalse())
			Expect(result.Reason).To(ContainSubstring("is not bound"))
		})
	})

	Context("Volume CR Failures", func() {
		It("should return error when Volume CR creation fails", func() {
			pvc := getTestPVC(dpuVolume.Name + "-pvc")
			pvc.Spec.VolumeName = testPVName
			pvc.Status.Phase = corev1.ClaimBound
			pv := getTestPV(testPVName, dpuVolume.Name)

			volumeFailure := createVolumeCRFailureInterceptor()
			clientForPrimary, targetClusters := setupClusterClientsWithInterceptors(
				[]client.Object{pvc, pv},
				[]client.Object{},
				nil,
				&volumeFailure,
			)

			result, err := provisioner.Provision(ctx, clientForPrimary, targetClusters, dpuVolume)
			Expect(err).To(MatchError(errTest))
			Expect(result.Ready).To(BeFalse())
		})
		It("should wait when Volume CR is being deleted", func() {
			pvc := getTestPVC(dpuVolume.Name + "-pvc")
			pvc.Spec.VolumeName = testPVName
			pvc.Status.Phase = corev1.ClaimBound
			pv := getTestPV(testPVName, dpuVolume.Name)
			volumeWithDeletion := getTestVolumeCRWithDeletionTimestamp(dpuVolume.Name)

			clientForPrimary, targetClusters := setupClusterClients(
				[]client.Object{pvc, pv},
				[]client.Object{volumeWithDeletion},
			)

			result, err := provisioner.Provision(ctx, clientForPrimary, targetClusters, dpuVolume)

			Expect(err).ToNot(HaveOccurred())
			Expect(result.Ready).To(BeFalse())
			Expect(result.Reason).To(ContainSubstring("is being deleted"))
		})
		It("should return error when Volume CR update fails", func() {
			pvc := getTestPVC(dpuVolume.Name + "-pvc")
			pvc.Spec.VolumeName = testPVName
			pvc.Status.Phase = corev1.ClaimBound
			pv := getTestPV(testPVName, dpuVolume.Name)
			existingVolume := getTestVolumeCRWithWrongSpec(dpuVolume.Name)

			updateFailure := createUpdateFailureInterceptor()
			clientForPrimary, targetClusters := setupClusterClientsWithInterceptors(
				[]client.Object{pvc, pv},
				[]client.Object{existingVolume},
				nil,
				&updateFailure,
			)

			result, err := provisioner.Provision(ctx, clientForPrimary, targetClusters, dpuVolume)
			Expect(err).To(MatchError(errTest))
			Expect(result.Ready).To(BeFalse())
		})
	})

	Context("Remove Operations", func() {
		It("should initiate removal of Volume CRs and PVCs from all clusters", func() {
			volume1 := getTestVolumeCR(dpuVolume.Name)
			volume2 := getTestVolumeCR(dpuVolume.Name)
			pvc1 := getTestPVC(dpuVolume.Name + "-pvc")
			pvc2 := getTestPVC(dpuVolume.Name + "-pvc")

			_, targetClusters := setupClusterClients(
				[]client.Object{volume1, pvc1},
				[]client.Object{volume2, pvc2},
			)

			result, err := provisioner.Remove(ctx, targetClusters, client.ObjectKeyFromObject(dpuVolume))

			Expect(err).ToNot(HaveOccurred())
			// The removal process should be initiated successfully.
			// The exact completion status depends on fake client behavior,
			// but it should report appropriately about Volume CR removal state.
			if !result.Completed {
				Expect(result.Reason).To(ContainSubstring("Volume"))
				Expect(result.Reason).To(ContainSubstring("is marked for removal"))
			}
		})
		It("should handle resources that don't exist", func() {
			_, targetClusters := setupClusterClients(
				[]client.Object{},
				[]client.Object{},
			)

			result, err := provisioner.Remove(ctx, targetClusters, client.ObjectKeyFromObject(dpuVolume))

			Expect(err).ToNot(HaveOccurred())
			Expect(result.Completed).To(BeTrue())
		})
		It("should wait when Volume CRs are in deletion state", func() {
			volumeWithDeletion := getTestVolumeCRWithDeletionTimestamp(dpuVolume.Name)

			_, targetClusters := setupClusterClients(
				[]client.Object{volumeWithDeletion},
				[]client.Object{},
			)

			result, err := provisioner.Remove(ctx, targetClusters, client.ObjectKeyFromObject(dpuVolume))

			Expect(err).ToNot(HaveOccurred())
			Expect(result.Completed).To(BeFalse())
			Expect(result.Reason).To(ContainSubstring("Volume"))
			Expect(result.Reason).To(ContainSubstring("is not removed yet"))
		})
		It("should wait when PVCs are in deletion state", func() {
			pvcWithDeletion := getTestPVCWithDeletionTimestamp(dpuVolume.Name + "-pvc")

			_, targetClusters := setupClusterClients(
				[]client.Object{pvcWithDeletion},
				[]client.Object{},
			)

			result, err := provisioner.Remove(ctx, targetClusters, client.ObjectKeyFromObject(dpuVolume))

			Expect(err).ToNot(HaveOccurred())
			Expect(result.Completed).To(BeFalse())
			Expect(result.Reason).To(ContainSubstring("PersistentVolumeClaim"))
			Expect(result.Reason).To(ContainSubstring("is not removed yet"))
		})
	})

	Context("Long Volume Names", func() {
		It("should create PVC with hashed name when DPUVolume name exceeds DNS length limit", func() {
			longName := strings.Repeat("long-name", 30)
			dpuVolume := getTestDPUVolume()
			dpuVolume.Name = longName
			clientForPrimary, targetClusters := setupClusterClients(
				[]client.Object{}, // Primary cluster is empty
				[]client.Object{}, // Secondary cluster is empty
			)

			result, err := provisioner.Provision(ctx, clientForPrimary, targetClusters, dpuVolume)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.Ready).To(BeFalse())

			// Verify PVC was created with hashed name
			pvcList := &corev1.PersistentVolumeClaimList{}
			Expect(clientForPrimary.Client.List(ctx, pvcList)).To(Succeed())
			Expect(pvcList.Items).To(HaveLen(1))
			createdPVC := pvcList.Items[0]
			Expect(createdPVC.Name).To(HaveSuffix("-pvc"))
			Expect(createdPVC.Name).To(HaveLen(36)) // 32 hex characters + 4 for "-pvc"
			Expect(createdPVC.Namespace).To(Equal(testTargetNamespace))
		})
	})
})
