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

package controllerattacher

import (
	"context"
	"errors"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	storagev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"
	"github.com/nvidia/doca-platform/internal/storage/snap/host-controller/dpuclusterhelper"
	"github.com/nvidia/doca-platform/internal/storage/snap/host-controller/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corestoragev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

const (
	testNamespace     = "test-namespace"
	testCSIDriverName = "test-csi-driver"
)

var (
	errTest = errors.New("test error")
)

// returns fake client builder with required schemes
func getFakeClientBuilder() *fake.ClientBuilder {
	return fake.NewClientBuilder().WithScheme(scheme.Scheme)
}

var _ = Describe("ControllerAttacher", func() {
	var (
		ctx                 context.Context
		helper              ControllerAttacher
		fakeClient          client.Client
		dpuClusterClient    dpuclusterhelper.ClientForDPUCluster
		dpuVolumeAttachment *storagev1.DPUVolumeAttachment
		dpuVolume           *storagev1.DPUVolume
		dpu                 *provisioningv1.DPU
		csiDriver           *corestoragev1.CSIDriver
	)

	BeforeEach(func() {
		ctx = context.Background()
		helper = NewControllerAttacher(testNamespace, utils.New("test-annotation"))
		fakeClient = getFakeClientBuilder().Build()

		dpuClusterClient = dpuclusterhelper.ClientForDPUCluster{
			Client: fakeClient,
			DPUCluster: &provisioningv1.DPUCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: testNamespace,
				},
			},
		}

		dpuVolumeAttachment = &storagev1.DPUVolumeAttachment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-attachment",
				Namespace: testNamespace,
			},
			Spec: storagev1.DPUVolumeAttachmentSpec{
				DPUNodeName:   "test-node",
				DPUVolumeName: "test-volume",
			},
		}

		dpuVolume = &storagev1.DPUVolume{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-volume",
				Namespace: testNamespace,
			},
			Status: storagev1.DPUVolumeStatus{
				State: &storagev1.DPUVolumeState{
					CSIDriverName: ptr.To(testCSIDriverName),
					VolumeInfo: &storagev1.VolumeInfo{
						VolumeName: ptr.To("test-pv"),
					},
				},
			},
		}

		dpu = &provisioningv1.DPU{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-dpu",
				Namespace: testNamespace,
			},
		}

		csiDriver = &corestoragev1.CSIDriver{
			ObjectMeta: metav1.ObjectMeta{
				Name: testCSIDriverName,
			},
			Spec: corestoragev1.CSIDriverSpec{
				AttachRequired: ptr.To(true),
			},
		}
	})

	Context("ControllerAttach", func() {
		BeforeEach(func() {
			Expect(fakeClient.Create(ctx, csiDriver)).To(Succeed())
		})
		Context("when CSIDriverName is not set in DPUVolume", func() {
			It("should return an error", func() {
				dpuVolume.Status.State.CSIDriverName = nil
				result, err := helper.ControllerAttach(ctx, dpuClusterClient, dpuVolumeAttachment, dpuVolume, dpu)
				Expect(err).To(MatchError(And(ContainSubstring("status.state of DPUVolume"), ContainSubstring("is missing required fields"))))
				Expect(result).To(Equal(ControllerAttachResult{}))
			})
		})
		Context("when CSIDriver does not exist", func() {
			It("should return an error", func() {
				dpuVolume.Status.State.CSIDriverName = ptr.To("non-existent-driver")
				result, err := helper.ControllerAttach(ctx, dpuClusterClient, dpuVolumeAttachment, dpuVolume, dpu)
				Expect(err).To(MatchError(ContainSubstring("failed to get CSIDriver")))
				Expect(result).To(Equal(ControllerAttachResult{}))
			})
		})
		Context("when CSIDriver does not require attachment", func() {
			It("should return attached=true and set ControllerAttached status", func() {
				csiDriver.Spec.AttachRequired = ptr.To(false)
				Expect(fakeClient.Update(ctx, csiDriver)).To(Succeed())
				result, err := helper.ControllerAttach(ctx, dpuClusterClient, dpuVolumeAttachment, dpuVolume, dpu)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Ready).To(BeTrue())
			})
		})
		Context("when CSIDriver requires attachment", func() {
			Context("and VolumeInfo is not set", func() {
				It("should return an error", func() {
					dpuVolume.Status.State.VolumeInfo = nil
					result, err := helper.ControllerAttach(ctx, dpuClusterClient, dpuVolumeAttachment, dpuVolume, dpu)
					Expect(err).To(MatchError(And(ContainSubstring("status.state of DPUVolume"), ContainSubstring("is missing required fields"))))
					Expect(result).To(Equal(ControllerAttachResult{}))
				})
			})
			Context("and SVVolumeAttachment does not exist", func() {
				It("should create SVVolumeAttachment and return not attached", func() {
					result, err := helper.ControllerAttach(ctx, dpuClusterClient, dpuVolumeAttachment, dpuVolume, dpu)
					Expect(err).NotTo(HaveOccurred())
					Expect(result.Ready).To(BeFalse())

					// Verify SVVolumeAttachment was created
					svVolumeAttachment := &storagev1.SVVolumeAttachment{}
					err = fakeClient.Get(ctx, client.ObjectKey{
						Name:      dpuVolumeAttachment.Name,
						Namespace: testNamespace,
					}, svVolumeAttachment)
					Expect(err).NotTo(HaveOccurred())
					Expect(svVolumeAttachment.Spec.NodeName).To(Equal(dpu.Name))
					Expect(svVolumeAttachment.Spec.Source.PersistentVolumeName).To(Equal(ptr.To("test-pv")))
					Expect(svVolumeAttachment.Annotations).To(HaveKey("test-annotation"))
				})
			})
			Context("and SVVolumeAttachment exists and is being deleted", func() {
				It("should return IsDeleting=true", func() {
					svVolumeAttachment := &storagev1.SVVolumeAttachment{
						ObjectMeta: metav1.ObjectMeta{
							Name:              dpuVolumeAttachment.Name,
							Namespace:         testNamespace,
							Finalizers:        []string{"test-finalizer"},
							DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
						},
						Spec: corestoragev1.VolumeAttachmentSpec{
							NodeName: dpu.Name,
							Source: corestoragev1.VolumeAttachmentSource{
								PersistentVolumeName: ptr.To("test-pv"),
							},
						},
					}
					testClient := getFakeClientBuilder().WithObjects(csiDriver, svVolumeAttachment).Build()
					testDPUClusterClient := dpuclusterhelper.ClientForDPUCluster{
						Client:     testClient,
						DPUCluster: dpuClusterClient.DPUCluster,
					}
					result, err := helper.ControllerAttach(ctx, testDPUClusterClient, dpuVolumeAttachment, dpuVolume, dpu)
					Expect(err).NotTo(HaveOccurred())
					Expect(result.Ready).To(BeFalse())
					Expect(result.Reason).To(ContainSubstring("SVVolumeAttachment"))
					Expect(result.Reason).To(ContainSubstring("is being deleted"))
				})
			})
			Context("and SVVolumeAttachment exists but has incorrect spec", func() {
				It("should delete the existing SVVolumeAttachment and return IsDeleting=true", func() {
					svVolumeAttachment := &storagev1.SVVolumeAttachment{
						ObjectMeta: metav1.ObjectMeta{
							Name:      dpuVolumeAttachment.Name,
							Namespace: testNamespace,
						},
						Spec: corestoragev1.VolumeAttachmentSpec{
							Attacher: testCSIDriverName,
							NodeName: "wrong-node",
							Source: corestoragev1.VolumeAttachmentSource{
								PersistentVolumeName: ptr.To("wrong-pv"),
							},
						},
					}
					Expect(fakeClient.Create(ctx, svVolumeAttachment)).To(Succeed())

					result, err := helper.ControllerAttach(ctx, dpuClusterClient, dpuVolumeAttachment, dpuVolume, dpu)
					Expect(err).NotTo(HaveOccurred())
					Expect(result.Ready).To(BeFalse())
					Expect(result.Reason).To(ContainSubstring("SVVolumeAttachment"))
					Expect(result.Reason).To(ContainSubstring("is being deleted"))
				})
			})
			Context("and SVVolumeAttachment exists and is attached", func() {
				It("should return IsAttached=true and set status fields", func() {
					svVolumeAttachment := &storagev1.SVVolumeAttachment{
						ObjectMeta: metav1.ObjectMeta{
							Name:      dpuVolumeAttachment.Name,
							Namespace: testNamespace,
						},
						Spec: corestoragev1.VolumeAttachmentSpec{
							Attacher: testCSIDriverName,
							NodeName: dpu.Name,
							Source: corestoragev1.VolumeAttachmentSource{
								PersistentVolumeName: ptr.To("test-pv"),
							},
						},
						Status: corestoragev1.VolumeAttachmentStatus{
							Attached: true,
							AttachmentMetadata: map[string]string{
								"test-key": "test-value",
							},
						},
					}
					Expect(fakeClient.Create(ctx, svVolumeAttachment)).To(Succeed())

					result, err := helper.ControllerAttach(ctx, dpuClusterClient, dpuVolumeAttachment, dpuVolume, dpu)
					Expect(err).NotTo(HaveOccurred())
					Expect(result.Ready).To(BeTrue())
					Expect(result.AttachmentMetadata).To(Equal(map[string]string{
						"test-key": "test-value",
					}))
				})
			})
			Context("and SVVolumeAttachment exists but is not attached", func() {
				Context("and has an attach error", func() {
					It("should return error message", func() {
						svVolumeAttachment := &storagev1.SVVolumeAttachment{
							ObjectMeta: metav1.ObjectMeta{
								Name:      dpuVolumeAttachment.Name,
								Namespace: testNamespace,
							},
							Spec: corestoragev1.VolumeAttachmentSpec{
								Attacher: testCSIDriverName,
								NodeName: dpu.Name,
								Source: corestoragev1.VolumeAttachmentSource{
									PersistentVolumeName: ptr.To("test-pv"),
								},
							},
							Status: corestoragev1.VolumeAttachmentStatus{
								Attached: false,
								AttachError: &corestoragev1.VolumeError{
									Message: "attachment failed",
								},
							},
						}
						Expect(fakeClient.Create(ctx, svVolumeAttachment)).To(Succeed())

						result, err := helper.ControllerAttach(ctx, dpuClusterClient, dpuVolumeAttachment, dpuVolume, dpu)
						Expect(err).NotTo(HaveOccurred())
						Expect(result.Ready).To(BeFalse())
						Expect(result.Reason).To(ContainSubstring("attachment failed"))
					})
				})
				Context("and has no attach error", func() {
					It("should return no error message", func() {
						svVolumeAttachment := &storagev1.SVVolumeAttachment{
							ObjectMeta: metav1.ObjectMeta{
								Name:      dpuVolumeAttachment.Name,
								Namespace: testNamespace,
							},
							Spec: corestoragev1.VolumeAttachmentSpec{
								Attacher: testCSIDriverName,
								NodeName: dpu.Name,
								Source: corestoragev1.VolumeAttachmentSource{
									PersistentVolumeName: ptr.To("test-pv"),
								},
							},
							Status: corestoragev1.VolumeAttachmentStatus{
								Attached: false,
							},
						}
						Expect(fakeClient.Create(ctx, svVolumeAttachment)).To(Succeed())

						result, err := helper.ControllerAttach(ctx, dpuClusterClient, dpuVolumeAttachment, dpuVolume, dpu)
						Expect(err).NotTo(HaveOccurred())
						Expect(result.Ready).To(BeFalse())
						Expect(result.Reason).To(BeEmpty())
					})
				})
			})
			Context("and Get SVVolumeAttachment fails", func() {
				It("should return the error", func() {
					failingClient := getFakeClientBuilder().
						WithObjects(csiDriver).
						WithInterceptorFuncs(interceptor.Funcs{
							Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
								if _, ok := obj.(*storagev1.SVVolumeAttachment); ok {
									return errTest
								}
								return client.Get(ctx, key, obj, opts...)
							},
						}).
						Build()
					failingDPUClusterClient := dpuclusterhelper.ClientForDPUCluster{
						Client:     failingClient,
						DPUCluster: dpuClusterClient.DPUCluster,
					}

					result, err := helper.ControllerAttach(ctx, failingDPUClusterClient, dpuVolumeAttachment, dpuVolume, dpu)
					Expect(err).To(MatchError(errTest))
					Expect(result).To(Equal(ControllerAttachResult{}))
				})
			})
			Context("and Delete SVVolumeAttachment fails", func() {
				It("should return the error", func() {
					// Create SVVolumeAttachment with incorrect spec to trigger deletion
					svVolumeAttachment := &storagev1.SVVolumeAttachment{
						ObjectMeta: metav1.ObjectMeta{
							Name:      dpuVolumeAttachment.Name,
							Namespace: testNamespace,
						},
						Spec: corestoragev1.VolumeAttachmentSpec{
							Attacher: testCSIDriverName,
							NodeName: "wrong-node",
							Source: corestoragev1.VolumeAttachmentSource{
								PersistentVolumeName: ptr.To("wrong-pv"),
							},
						},
					}

					failingClient := getFakeClientBuilder().
						WithObjects(csiDriver, svVolumeAttachment).
						WithInterceptorFuncs(interceptor.Funcs{
							Delete: func(ctx context.Context, client client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
								if _, ok := obj.(*storagev1.SVVolumeAttachment); ok {
									return errTest
								}
								return client.Delete(ctx, obj, opts...)
							},
						}).
						Build()
					failingDPUClusterClient := dpuclusterhelper.ClientForDPUCluster{
						Client:     failingClient,
						DPUCluster: dpuClusterClient.DPUCluster,
					}

					result, err := helper.ControllerAttach(ctx, failingDPUClusterClient, dpuVolumeAttachment, dpuVolume, dpu)
					Expect(err).To(MatchError(errTest))
					Expect(result).To(Equal(ControllerAttachResult{}))
				})
			})
		})
		Context("when client operations fail", func() {
			It("should return error when Get CSIDriver fails", func() {
				failingClient := getFakeClientBuilder().
					WithInterceptorFuncs(interceptor.Funcs{
						Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
							if _, ok := obj.(*corestoragev1.CSIDriver); ok {
								return errTest
							}
							return client.Get(ctx, key, obj, opts...)
						},
					}).
					Build()
				failingDPUClusterClient := dpuclusterhelper.ClientForDPUCluster{
					Client:     failingClient,
					DPUCluster: dpuClusterClient.DPUCluster,
				}

				result, err := helper.ControllerAttach(ctx, failingDPUClusterClient, dpuVolumeAttachment, dpuVolume, dpu)
				Expect(err).To(HaveOccurred())
				Expect(errors.Is(err, errTest)).To(BeTrue())
				Expect(result).To(Equal(ControllerAttachResult{}))
			})
			It("should return error when Create SVVolumeAttachment fails", func() {
				failingClient := getFakeClientBuilder().
					WithObjects(csiDriver).
					WithInterceptorFuncs(interceptor.Funcs{
						Create: func(ctx context.Context, client client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
							if _, ok := obj.(*storagev1.SVVolumeAttachment); ok {
								return errTest
							}
							return client.Create(ctx, obj, opts...)
						},
					}).
					Build()
				failingDPUClusterClient := dpuclusterhelper.ClientForDPUCluster{
					Client:     failingClient,
					DPUCluster: dpuClusterClient.DPUCluster,
				}

				result, err := helper.ControllerAttach(ctx, failingDPUClusterClient, dpuVolumeAttachment, dpuVolume, dpu)
				Expect(err).To(HaveOccurred())
				Expect(errors.Is(err, errTest)).To(BeTrue())
				Expect(result).To(Equal(ControllerAttachResult{}))
			})
		})
	})

	Context("ControllerDetach", func() {
		var (
			dpuClustersClients []dpuclusterhelper.ClientForDPUCluster
			objectKey          client.ObjectKey
		)
		BeforeEach(func() {
			objectKey = client.ObjectKey{
				Name:      "test-attachment",
				Namespace: testNamespace,
			}
			dpuClustersClients = []dpuclusterhelper.ClientForDPUCluster{
				{
					DPUCluster: &provisioningv1.DPUCluster{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-cluster",
							Namespace: testNamespace,
						},
					},
					Client: fakeClient,
				},
			}
		})
		Context("when no SVVolumeAttachment exists", func() {
			It("should return not found result", func() {
				result, err := helper.ControllerDetach(ctx, dpuClustersClients, objectKey)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Completed).To(BeTrue())
				Expect(result.Reason).To(BeEmpty())
			})
		})
		Context("when SVVolumeAttachment exists", func() {
			BeforeEach(func() {
				svVolumeAttachment := &storagev1.SVVolumeAttachment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      objectKey.Name,
						Namespace: testNamespace,
					},
				}
				Expect(fakeClient.Create(ctx, svVolumeAttachment)).To(Succeed())
			})
			It("should delete SVVolumeAttachment and return deleted result", func() {
				result, err := helper.ControllerDetach(ctx, dpuClustersClients, objectKey)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Completed).To(BeFalse())
				Expect(result.Reason).To(ContainSubstring("SVVolumeAttachment"))
				Expect(result.Reason).To(ContainSubstring("is marked for removal"))

				// Verify SVVolumeAttachment was deleted
				svVolumeAttachment := &storagev1.SVVolumeAttachment{}
				err = fakeClient.Get(ctx, client.ObjectKey{
					Name:      objectKey.Name,
					Namespace: testNamespace,
				}, svVolumeAttachment)
				Expect(apierrors.IsNotFound(err)).To(BeTrue())
			})
		})
	})
})
