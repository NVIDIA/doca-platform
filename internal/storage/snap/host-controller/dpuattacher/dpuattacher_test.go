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

package dpuattacher

import (
	"context"
	"errors"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	storagev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"
	"github.com/nvidia/doca-platform/internal/storage/snap/host-controller/dpuclusterhelper"
	"github.com/nvidia/doca-platform/internal/storage/snap/host-controller/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

const (
	testNamespace     = "test-namespace"
	testDPUNodeName   = "test-dpu-node"
	testDPUVolumeName = "test-dpu-volume"
	testPCIAddress    = "0000:00:01.0"
	testFuncVUID      = "test-function-vuid"
	testDeviceName    = "test-device"
	testNVMeNsID      = int64(1)
	testNVMeUUID      = "test-uuid"
	testFilesystemTag = "test-fs-tag"
)

var (
	errTest = errors.New("test error")
)

// returns fake client builder with required schemes
func getFakeClientBuilder() *fake.ClientBuilder {
	return fake.NewClientBuilder().WithScheme(scheme.Scheme).WithStatusSubresource(&storagev1.VolumeAttachment{})
}

// createClientForDPUCluster creates a ClientForDPUCluster with the given client
func createClientForDPUCluster(client client.Client) dpuclusterhelper.ClientForDPUCluster {
	return dpuclusterhelper.ClientForDPUCluster{
		DPUCluster: &provisioningv1.DPUCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: testNamespace,
			},
		},
		Client: client,
	}
}

// createTestDPU creates a test DPU object for testing
func createTestDPU() *provisioningv1.DPU {
	return &provisioningv1.DPU{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testDPUNodeName,
			Namespace: testNamespace,
		},
	}
}

// createVolumeAttachment creates a basic VolumeAttachment with standard spec
func createVolumeAttachment(name, namespace string, dpuVolumeAttachment *storagev1.DPUVolumeAttachment) *storagev1.VolumeAttachment {
	return &storagev1.VolumeAttachment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: storagev1.VolumeAttachmentSpec{
			NodeName: dpuVolumeAttachment.Spec.DPUNodeName,
			Source: storagev1.VolumeSource{
				VolumeRef: &storagev1.ObjectRef{
					Name:      dpuVolumeAttachment.Spec.DPUVolumeName,
					Namespace: namespace,
				},
			},
			FunctionTypeConfig: dpuVolumeAttachment.Spec.FunctionTypeConfig,
		},
	}
}

// createVolumeAttachmentWithStatus creates a VolumeAttachment with specified status
func createVolumeAttachmentWithStatus(name string, dpuVolumeAttachment *storagev1.DPUVolumeAttachment, status storagev1.VolumeAttachmentStatus) *storagev1.VolumeAttachment {
	va := createVolumeAttachment(name, testNamespace, dpuVolumeAttachment)
	va.Status = status
	return va
}

// createVolumeAttachmentBeingDeleted creates a VolumeAttachment that is being deleted
func createVolumeAttachmentBeingDeleted(name, namespace string, dpuVolumeAttachment *storagev1.DPUVolumeAttachment, finalizers []string) *storagev1.VolumeAttachment {
	va := createVolumeAttachment(name, namespace, dpuVolumeAttachment)
	va.Finalizers = finalizers
	va.DeletionTimestamp = &metav1.Time{Time: metav1.Now().Time}
	return va
}

// createVolumeAttachmentWithIncorrectSpec creates a VolumeAttachment with wrong spec
func createVolumeAttachmentWithIncorrectSpec(name, namespace string) *storagev1.VolumeAttachment {
	return &storagev1.VolumeAttachment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: storagev1.VolumeAttachmentSpec{
			NodeName: "wrong-node",
			Source: storagev1.VolumeSource{
				VolumeRef: &storagev1.ObjectRef{
					Name:      "wrong-volume",
					Namespace: namespace,
				},
			},
			FunctionTypeConfig: storagev1.FunctionTypeConfig{
				FunctionType:    storagev1.FunctionTypeVF,
				HotplugFunction: true,
			},
		},
	}
}

// createVolumeAttachmentWithFinalizers creates a VolumeAttachment with specified finalizers
func createVolumeAttachmentWithFinalizers(name, namespace string, dpuVolumeAttachment *storagev1.DPUVolumeAttachment, finalizers []string) *storagev1.VolumeAttachment {
	va := createVolumeAttachment(name, namespace, dpuVolumeAttachment)
	va.Finalizers = finalizers
	return va
}

// createFullyAttachedVolumeAttachment creates a VolumeAttachment that is fully attached with all data
func createFullyAttachedVolumeAttachment(name, namespace string, dpuVolumeAttachment *storagev1.DPUVolumeAttachment) *storagev1.VolumeAttachment {
	va := createVolumeAttachment(name, namespace, dpuVolumeAttachment)
	va.Status = storagev1.VolumeAttachmentStatus{
		StorageAttached: true,
		DPU: storagev1.VolumeAttachmentStatusDPU{
			Attached:         true,
			PCIDeviceAddress: testPCIAddress,
			FuncVUID:         testFuncVUID,
			DeviceName:       testDeviceName,
			BdevAttrs: storagev1.BdevAttrs{
				NVMeNsID: testNVMeNsID,
				NVMeUUID: testNVMeUUID,
			},
			FSdevAttrs: storagev1.FSdevAttrs{
				FilesystemTag: testFilesystemTag,
			},
		},
	}
	return va
}

// createPartiallyAttachedVolumeAttachment creates a VolumeAttachment with minimal attachment data
func createPartiallyAttachedVolumeAttachment(name, namespace string, dpuVolumeAttachment *storagev1.DPUVolumeAttachment) *storagev1.VolumeAttachment {
	va := createVolumeAttachment(name, namespace, dpuVolumeAttachment)
	va.Status = storagev1.VolumeAttachmentStatus{
		StorageAttached: true,
		DPU: storagev1.VolumeAttachmentStatusDPU{
			Attached:         true,
			PCIDeviceAddress: testPCIAddress,
		},
	}
	return va
}

var _ = Describe("DPUAttacher", func() {
	var (
		ctx                 context.Context
		attacher            DPUAttacher
		fakeClient          client.Client
		dpuVolumeAttachment *storagev1.DPUVolumeAttachment
		testDPU             *provisioningv1.DPU
		ownedByHelper       utils.OwnedByHelper
	)

	BeforeEach(func() {
		ctx = context.Background()
		ownedByHelper = utils.New("test-annotation")
		attacher = NewDPUAttacher(testNamespace, ownedByHelper)
		fakeClient = getFakeClientBuilder().Build()
		testDPU = createTestDPU()

		dpuVolumeAttachment = &storagev1.DPUVolumeAttachment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-attachment",
				Namespace: testNamespace,
			},
			Spec: storagev1.DPUVolumeAttachmentSpec{
				DPUNodeName:   testDPUNodeName,
				DPUVolumeName: testDPUVolumeName,
				FunctionTypeConfig: storagev1.FunctionTypeConfig{
					FunctionType:    storagev1.FunctionTypePF,
					HotplugFunction: false,
				},
			},
		}
	})

	Context("DPUAttach", func() {
		Context("when VolumeAttachment does not exist", func() {
			It("should create VolumeAttachment and return not ready", func() {
				result, err := attacher.DPUAttach(ctx, createClientForDPUCluster(fakeClient), dpuVolumeAttachment, testDPU)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Ready).To(BeFalse())
				Expect(result.Reason).To(BeEmpty())
				Expect(result.Data).To(BeNil())

				// Verify VolumeAttachment was created
				volumeAttachment := &storagev1.VolumeAttachment{}
				err = fakeClient.Get(ctx, client.ObjectKey{
					Name:      dpuVolumeAttachment.Name,
					Namespace: testNamespace,
				}, volumeAttachment)
				Expect(err).NotTo(HaveOccurred())
				Expect(volumeAttachment.Spec.NodeName).To(Equal(testDPUNodeName))
				Expect(volumeAttachment.Spec.Source.VolumeRef.Name).To(Equal(testDPUVolumeName))
				Expect(volumeAttachment.Spec.Source.VolumeRef.Namespace).To(Equal(testNamespace))
				Expect(volumeAttachment.Spec.FunctionTypeConfig).To(Equal(dpuVolumeAttachment.Spec.FunctionTypeConfig))
				Expect(volumeAttachment.Annotations).To(HaveKey("test-annotation"))
				Expect(volumeAttachment.Status.StorageAttached).To(BeTrue())
				ownedBy, err := ownedByHelper.GetOwnedBy(volumeAttachment)
				Expect(err).NotTo(HaveOccurred())
				Expect(ownedBy).To(Equal(client.ObjectKeyFromObject(dpuVolumeAttachment)))
			})
		})
		Context("when VolumeAttachment exists and is being deleted", func() {
			It("should return not ready with deletion message", func() {
				volumeAttachment := createVolumeAttachmentBeingDeleted(dpuVolumeAttachment.Name, testNamespace, dpuVolumeAttachment, []string{"test-finalizer"})
				testClient := getFakeClientBuilder().WithObjects(volumeAttachment).Build()
				result, err := attacher.DPUAttach(ctx, createClientForDPUCluster(testClient), dpuVolumeAttachment, testDPU)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Ready).To(BeFalse())
				Expect(result.Reason).To(ContainSubstring("VolumeAttachment"))
				Expect(result.Reason).To(ContainSubstring("is being deleted"))
			})
		})
		Context("when VolumeAttachment exists but has incorrect spec", func() {
			It("should delete the existing VolumeAttachment and return not ready", func() {
				volumeAttachment := createVolumeAttachmentWithIncorrectSpec(dpuVolumeAttachment.Name, testNamespace)
				Expect(fakeClient.Create(ctx, volumeAttachment)).To(Succeed())

				result, err := attacher.DPUAttach(ctx, createClientForDPUCluster(fakeClient), dpuVolumeAttachment, testDPU)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Ready).To(BeFalse())
				Expect(result.Reason).To(ContainSubstring("VolumeAttachment"))
				Expect(result.Reason).To(ContainSubstring("has incorrect spec, removed it"))

				// Verify VolumeAttachment was deleted
				updatedVolumeAttachment := &storagev1.VolumeAttachment{}
				err = fakeClient.Get(ctx, client.ObjectKey{
					Name:      dpuVolumeAttachment.Name,
					Namespace: testNamespace,
				}, updatedVolumeAttachment)
				Expect(apierrors.IsNotFound(err)).To(BeTrue())
			})
		})
		Context("when VolumeAttachment exists with correct spec but incorrect owner reference", func() {
			It("should update the owner reference", func() {
				status := storagev1.VolumeAttachmentStatus{StorageAttached: false}
				volumeAttachment := createVolumeAttachmentWithStatus(dpuVolumeAttachment.Name, dpuVolumeAttachment, status)
				Expect(fakeClient.Create(ctx, volumeAttachment)).To(Succeed())

				result, err := attacher.DPUAttach(ctx, createClientForDPUCluster(fakeClient), dpuVolumeAttachment, testDPU)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Ready).To(BeFalse())

				// Verify owner reference was updated and status was set to StorageAttached
				updatedVolumeAttachment := &storagev1.VolumeAttachment{}
				err = fakeClient.Get(ctx, client.ObjectKey{
					Name:      dpuVolumeAttachment.Name,
					Namespace: testNamespace,
				}, updatedVolumeAttachment)
				Expect(err).NotTo(HaveOccurred())
				Expect(updatedVolumeAttachment.Annotations).To(HaveKey("test-annotation"))
				Expect(updatedVolumeAttachment.Status.StorageAttached).To(BeTrue())
				ownedBy, err := ownedByHelper.GetOwnedBy(updatedVolumeAttachment)
				Expect(err).NotTo(HaveOccurred())
				Expect(ownedBy).To(Equal(client.ObjectKeyFromObject(dpuVolumeAttachment)))
			})
		})
		Context("when VolumeAttachment exists and storage is not attached", func() {
			It("should update StorageAttached status to true", func() {
				status := storagev1.VolumeAttachmentStatus{StorageAttached: false}
				volumeAttachment := createVolumeAttachmentWithStatus(dpuVolumeAttachment.Name, dpuVolumeAttachment, status)
				ownedByHelper.SetOwnedBy(volumeAttachment, client.ObjectKeyFromObject(dpuVolumeAttachment))
				Expect(fakeClient.Create(ctx, volumeAttachment)).To(Succeed())

				result, err := attacher.DPUAttach(ctx, createClientForDPUCluster(fakeClient), dpuVolumeAttachment, testDPU)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Ready).To(BeFalse())

				// Verify StorageAttached was set to true
				updatedVolumeAttachment := &storagev1.VolumeAttachment{}
				err = fakeClient.Get(ctx, client.ObjectKey{
					Name:      dpuVolumeAttachment.Name,
					Namespace: testNamespace,
				}, updatedVolumeAttachment)
				Expect(err).NotTo(HaveOccurred())
				Expect(updatedVolumeAttachment.Status.StorageAttached).To(BeTrue())
			})
		})
		Context("when VolumeAttachment exists but DPU is not attached", func() {
			It("should return not ready with status message", func() {
				status := storagev1.VolumeAttachmentStatus{
					StorageAttached: true,
					DPU: storagev1.VolumeAttachmentStatusDPU{
						Attached: false,
					},
					Message: "DPU attachment in progress",
				}
				volumeAttachment := createVolumeAttachmentWithStatus(dpuVolumeAttachment.Name, dpuVolumeAttachment, status)
				ownedByHelper.SetOwnedBy(volumeAttachment, client.ObjectKeyFromObject(dpuVolumeAttachment))
				Expect(fakeClient.Create(ctx, volumeAttachment)).To(Succeed())

				result, err := attacher.DPUAttach(ctx, createClientForDPUCluster(fakeClient), dpuVolumeAttachment, testDPU)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Ready).To(BeFalse())
				Expect(result.Reason).To(Equal("DPU attachment in progress"))
			})
		})
		Context("when VolumeAttachment exists and DPU is attached", func() {
			Context("with all attachment details", func() {
				It("should return ready with complete attachment data", func() {
					volumeAttachment := createFullyAttachedVolumeAttachment(dpuVolumeAttachment.Name, testNamespace, dpuVolumeAttachment)
					ownedByHelper.SetOwnedBy(volumeAttachment, client.ObjectKeyFromObject(dpuVolumeAttachment))
					Expect(fakeClient.Create(ctx, volumeAttachment)).To(Succeed())

					result, err := attacher.DPUAttach(ctx, createClientForDPUCluster(fakeClient), dpuVolumeAttachment, testDPU)
					Expect(err).NotTo(HaveOccurred())
					Expect(result.Ready).To(BeTrue())
					Expect(result.Data).NotTo(BeNil())
					Expect(result.Data.PCIAddress).To(Equal(testPCIAddress))
					Expect(result.Data.FuncVUID).To(Equal(testFuncVUID))
					Expect(result.Data.DeviceName).To(Equal(testDeviceName))
					Expect(result.Data.NVMEAttrs).NotTo(BeNil())
					Expect(*result.Data.NVMEAttrs.NamespaceID).To(Equal(testNVMeNsID))
					Expect(*result.Data.NVMEAttrs.NamespaceUUID).To(Equal(testNVMeUUID))
					Expect(result.Data.VirtioFSAttrs).NotTo(BeNil())
					Expect(*result.Data.VirtioFSAttrs.FilesystemTag).To(Equal(testFilesystemTag))
				})
			})
			Context("with minimal attachment details", func() {
				It("should return ready with partial attachment data", func() {
					volumeAttachment := createPartiallyAttachedVolumeAttachment(dpuVolumeAttachment.Name, testNamespace, dpuVolumeAttachment)
					ownedByHelper.SetOwnedBy(volumeAttachment, client.ObjectKeyFromObject(dpuVolumeAttachment))
					Expect(fakeClient.Create(ctx, volumeAttachment)).To(Succeed())

					result, err := attacher.DPUAttach(ctx, createClientForDPUCluster(fakeClient), dpuVolumeAttachment, testDPU)
					Expect(err).NotTo(HaveOccurred())
					Expect(result.Ready).To(BeTrue())
					Expect(result.Data).NotTo(BeNil())
					Expect(result.Data.PCIAddress).To(Equal(testPCIAddress))
					Expect(result.Data.FuncVUID).To(BeEmpty())
					Expect(result.Data.DeviceName).To(BeEmpty())
					Expect(result.Data.NVMEAttrs).To(BeNil())
					Expect(result.Data.VirtioFSAttrs).To(BeNil())
				})
			})
		})
		Context("when client operations fail", func() {
			It("should return error when Get VolumeAttachment fails", func() {
				failingClient := getFakeClientBuilder().
					WithInterceptorFuncs(interceptor.Funcs{
						Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
							if _, ok := obj.(*storagev1.VolumeAttachment); ok {
								return errTest
							}
							return client.Get(ctx, key, obj, opts...)
						},
					}).
					Build()

				result, err := attacher.DPUAttach(ctx, createClientForDPUCluster(failingClient), dpuVolumeAttachment, testDPU)
				Expect(err).To(MatchError(errTest))
				Expect(result).To(Equal(DPUAttachResult{}))
			})
			It("should return error when Create VolumeAttachment fails", func() {
				failingClient := getFakeClientBuilder().
					WithInterceptorFuncs(interceptor.Funcs{
						Create: func(ctx context.Context, client client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
							if _, ok := obj.(*storagev1.VolumeAttachment); ok {
								return errTest
							}
							return client.Create(ctx, obj, opts...)
						},
					}).
					Build()

				result, err := attacher.DPUAttach(ctx, createClientForDPUCluster(failingClient), dpuVolumeAttachment, testDPU)
				Expect(err).To(MatchError(errTest))
				Expect(result).To(Equal(DPUAttachResult{}))
			})
			It("should return error when Delete VolumeAttachment fails", func() {
				// Create VolumeAttachment with incorrect spec to trigger deletion
				volumeAttachment := createVolumeAttachmentWithIncorrectSpec(dpuVolumeAttachment.Name, testNamespace)

				failingClient := getFakeClientBuilder().
					WithObjects(volumeAttachment).
					WithInterceptorFuncs(interceptor.Funcs{
						Delete: func(ctx context.Context, client client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
							if _, ok := obj.(*storagev1.VolumeAttachment); ok {
								return errTest
							}
							return client.Delete(ctx, obj, opts...)
						},
					}).
					Build()

				result, err := attacher.DPUAttach(ctx, createClientForDPUCluster(failingClient), dpuVolumeAttachment, testDPU)
				Expect(err).To(MatchError(errTest))
				Expect(result).To(Equal(DPUAttachResult{}))
			})
			It("should return error when Update VolumeAttachment fails", func() {
				volumeAttachment := createVolumeAttachment(dpuVolumeAttachment.Name, testNamespace, dpuVolumeAttachment)

				failingClient := getFakeClientBuilder().
					WithObjects(volumeAttachment).
					WithInterceptorFuncs(interceptor.Funcs{
						Update: func(ctx context.Context, client client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
							if _, ok := obj.(*storagev1.VolumeAttachment); ok {
								return errTest
							}
							return client.Update(ctx, obj, opts...)
						},
					}).
					Build()

				result, err := attacher.DPUAttach(ctx, createClientForDPUCluster(failingClient), dpuVolumeAttachment, testDPU)
				Expect(err).To(MatchError(errTest))
				Expect(result).To(Equal(DPUAttachResult{}))
			})
			It("should return error when Status Update VolumeAttachment fails", func() {
				status := storagev1.VolumeAttachmentStatus{StorageAttached: false}
				volumeAttachment := createVolumeAttachmentWithStatus(dpuVolumeAttachment.Name, dpuVolumeAttachment, status)
				ownedByHelper.SetOwnedBy(volumeAttachment, client.ObjectKeyFromObject(dpuVolumeAttachment))

				failingClient := getFakeClientBuilder().
					WithObjects(volumeAttachment).
					WithInterceptorFuncs(interceptor.Funcs{
						SubResourceUpdate: func(ctx context.Context, client client.Client, subResourceName string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
							if subResourceName == "status" {
								if _, ok := obj.(*storagev1.VolumeAttachment); ok {
									return errTest
								}
							}
							return client.SubResource(subResourceName).Update(ctx, obj, opts...)
						},
					}).
					Build()

				result, err := attacher.DPUAttach(ctx, createClientForDPUCluster(failingClient), dpuVolumeAttachment, testDPU)
				Expect(err).To(MatchError(errTest))
				Expect(result).To(Equal(DPUAttachResult{}))
			})
		})
	})

	Context("DPUDetach", func() {
		var (
			objectKey client.ObjectKey
		)
		BeforeEach(func() {
			objectKey = client.ObjectKey{
				Name:      "test-attachment",
				Namespace: testNamespace,
			}
		})
		Context("when no VolumeAttachment exists", func() {
			It("should return completed result", func() {
				result, err := attacher.DPUDetach(ctx, []dpuclusterhelper.ClientForDPUCluster{createClientForDPUCluster(fakeClient)}, objectKey)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Completed).To(BeTrue())
				Expect(result.Reason).To(BeEmpty())
			})
		})
		Context("when VolumeAttachment exists without finalizers", func() {
			BeforeEach(func() {
				volumeAttachment := createVolumeAttachment(objectKey.Name, testNamespace, dpuVolumeAttachment)
				Expect(fakeClient.Create(ctx, volumeAttachment)).To(Succeed())
			})
			It("should delete VolumeAttachment and return not completed result", func() {
				result, err := attacher.DPUDetach(ctx, []dpuclusterhelper.ClientForDPUCluster{createClientForDPUCluster(fakeClient)}, objectKey)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Completed).To(BeFalse())
				Expect(result.Reason).To(ContainSubstring("VolumeAttachment"))
				Expect(result.Reason).To(ContainSubstring("is marked for removal"))

				// Verify VolumeAttachment was deleted
				volumeAttachment := &storagev1.VolumeAttachment{}
				err = fakeClient.Get(ctx, client.ObjectKey{
					Name:      objectKey.Name,
					Namespace: testNamespace,
				}, volumeAttachment)
				Expect(apierrors.IsNotFound(err)).To(BeTrue())
			})
		})
		Context("when VolumeAttachment exists with snap controller finalizer", func() {
			BeforeEach(func() {
				volumeAttachment := createVolumeAttachmentWithFinalizers(objectKey.Name, testNamespace, dpuVolumeAttachment, []string{snapControllerFinalizer})
				Expect(fakeClient.Create(ctx, volumeAttachment)).To(Succeed())
			})
			It("should remove finalizer and delete VolumeAttachment", func() {
				result, err := attacher.DPUDetach(ctx, []dpuclusterhelper.ClientForDPUCluster{createClientForDPUCluster(fakeClient)}, objectKey)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Completed).To(BeFalse())
				Expect(result.Reason).To(ContainSubstring("VolumeAttachment"))
				Expect(result.Reason).To(ContainSubstring("is marked for removal"))

				// Verify VolumeAttachment was deleted
				volumeAttachment := &storagev1.VolumeAttachment{}
				err = fakeClient.Get(ctx, client.ObjectKey{
					Name:      objectKey.Name,
					Namespace: testNamespace,
				}, volumeAttachment)
				Expect(apierrors.IsNotFound(err)).To(BeTrue())
			})
		})
		Context("when VolumeAttachment exists with other finalizers", func() {
			BeforeEach(func() {
				volumeAttachment := createVolumeAttachmentWithFinalizers(objectKey.Name, testNamespace, dpuVolumeAttachment, []string{snapControllerFinalizer, "other-finalizer"})
				Expect(fakeClient.Create(ctx, volumeAttachment)).To(Succeed())
			})
			It("should remove snap controller finalizer but keep other finalizers", func() {
				result, err := attacher.DPUDetach(ctx, []dpuclusterhelper.ClientForDPUCluster{createClientForDPUCluster(fakeClient)}, objectKey)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Completed).To(BeFalse())
				Expect(result.Reason).To(ContainSubstring("VolumeAttachment"))
				Expect(result.Reason).To(ContainSubstring("is marked for removal"))

				// Verify VolumeAttachment still exists but with snap controller finalizer removed
				volumeAttachment := &storagev1.VolumeAttachment{}
				err = fakeClient.Get(ctx, client.ObjectKey{
					Name:      objectKey.Name,
					Namespace: testNamespace,
				}, volumeAttachment)
				// With fake client, the object should still exist (not deleted yet due to other finalizers)
				// but the snap controller finalizer should be removed
				Expect(err).NotTo(HaveOccurred())
				Expect(volumeAttachment.Finalizers).NotTo(ContainElement(snapControllerFinalizer))
				Expect(volumeAttachment.Finalizers).To(ContainElement("other-finalizer"))
			})
		})
		Context("when multiple DPU clusters are provided", func() {
			var dpuClustersClients []dpuclusterhelper.ClientForDPUCluster
			BeforeEach(func() {
				secondFakeClient := getFakeClientBuilder().Build()
				dpuClustersClients = []dpuclusterhelper.ClientForDPUCluster{createClientForDPUCluster(fakeClient), {
					DPUCluster: &provisioningv1.DPUCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-cluster-2",
							Namespace: testNamespace,
						},
					},
					Client: secondFakeClient,
				}}

				// Create VolumeAttachment in first cluster
				volumeAttachment1 := createVolumeAttachment(objectKey.Name, testNamespace, dpuVolumeAttachment)
				Expect(fakeClient.Create(ctx, volumeAttachment1)).To(Succeed())

				// Create VolumeAttachment in second cluster
				volumeAttachment2 := createVolumeAttachment(objectKey.Name, testNamespace, dpuVolumeAttachment)
				Expect(secondFakeClient.Create(ctx, volumeAttachment2)).To(Succeed())
			})
			It("should delete VolumeAttachment from all clusters", func() {
				result, err := attacher.DPUDetach(ctx, dpuClustersClients, objectKey)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Completed).To(BeFalse())
				Expect(result.Reason).To(ContainSubstring("VolumeAttachment"))

				// Verify VolumeAttachment was deleted from both clusters
				volumeAttachment1 := &storagev1.VolumeAttachment{}
				err = fakeClient.Get(ctx, client.ObjectKey{
					Name:      objectKey.Name,
					Namespace: testNamespace,
				}, volumeAttachment1)
				Expect(apierrors.IsNotFound(err)).To(BeTrue())

				volumeAttachment2 := &storagev1.VolumeAttachment{}
				err = dpuClustersClients[1].Client.Get(ctx, client.ObjectKey{
					Name:      objectKey.Name,
					Namespace: testNamespace,
				}, volumeAttachment2)
				Expect(apierrors.IsNotFound(err)).To(BeTrue())
			})
		})
		Context("when Get VolumeAttachment fails", func() {
			It("should return the error", func() {
				failingClient := getFakeClientBuilder().
					WithInterceptorFuncs(interceptor.Funcs{
						Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
							if _, ok := obj.(*storagev1.VolumeAttachment); ok {
								return errTest
							}
							return client.Get(ctx, key, obj, opts...)
						},
					}).
					Build()

				result, err := attacher.DPUDetach(ctx, []dpuclusterhelper.ClientForDPUCluster{createClientForDPUCluster(failingClient)}, objectKey)
				Expect(err).To(MatchError(errTest))
				Expect(result).To(Equal(DPUDetachResult{}))
			})
		})
		Context("when Delete VolumeAttachment fails", func() {
			BeforeEach(func() {
				volumeAttachment := createVolumeAttachment(objectKey.Name, testNamespace, dpuVolumeAttachment)
				Expect(fakeClient.Create(ctx, volumeAttachment)).To(Succeed())
			})
			It("should return the error", func() {
				volumeAttachment := createVolumeAttachment(objectKey.Name, testNamespace, dpuVolumeAttachment)
				failingClient := getFakeClientBuilder().
					WithObjects(volumeAttachment).
					WithInterceptorFuncs(interceptor.Funcs{
						Delete: func(ctx context.Context, client client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
							if _, ok := obj.(*storagev1.VolumeAttachment); ok {
								return errTest
							}
							return client.Delete(ctx, obj, opts...)
						},
					}).
					Build()
				result, err := attacher.DPUDetach(ctx, []dpuclusterhelper.ClientForDPUCluster{createClientForDPUCluster(failingClient)}, objectKey)
				Expect(err).To(MatchError(errTest))
				Expect(result).To(Equal(DPUDetachResult{}))
			})
		})
		Context("when empty DPU clusters list is provided", func() {
			It("should return completed result", func() {
				result, err := attacher.DPUDetach(ctx, []dpuclusterhelper.ClientForDPUCluster{}, objectKey)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Completed).To(BeTrue())
				Expect(result.Reason).To(BeEmpty())
			})
		})
	})
})
