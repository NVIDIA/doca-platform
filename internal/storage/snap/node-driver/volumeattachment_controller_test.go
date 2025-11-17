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
	"context"

	pb "github.com/nvidia/doca-platform/api/grpc/nvidia/storage/plugins/v1"
	snapstoragev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"
	mock "github.com/nvidia/doca-platform/internal/storage/snap/node-driver/mock"
	rpcclient "github.com/nvidia/doca-platform/internal/storage/snap/node-driver/snap-rpc"
	testutils "github.com/nvidia/doca-platform/test/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/types/known/wrapperspb"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

const (
	testNodeName      = "test-node"
	testVolumeAttName = "test-volume-attachment"
	testVolumeName    = "test-volume"
	testPluginName    = "test-plugin"
	testProviderName  = "test-provider"
	testDeviceName    = "test-device"
	testPCIAddr       = "0000:85:00.0"
)

var _ = Describe("VolumeAttachment Controller", func() {
	var (
		testCtx            context.Context
		volumeAttachment   *snapstoragev1.VolumeAttachment
		volume             *snapstoragev1.Volume
		testCtrl           *gomock.Controller
		mockPluginClient   *mock.MockStoragePluginServiceClient
		mockIdentityClient *mock.MockIdentityServiceClient
		mockSNAPClient     *mock.MockClient
		cleanupObjects     []client.Object
		k8sManager         ctrl.Manager
		controllerCtx      context.Context
		controllerCancel   context.CancelFunc
		controllerFinished chan struct{}
		reconciler         *VolumeAttachmentReconciler
	)

	BeforeEach(func() {
		testCtx = context.Background()
		testCtrl = gomock.NewController(GinkgoT())
		mockPluginClient = mock.NewMockStoragePluginServiceClient(testCtrl)
		mockIdentityClient = mock.NewMockIdentityServiceClient(testCtrl)
		mockSNAPClient = mock.NewMockClient(testCtrl)
		cleanupObjects = []client.Object{}

		// Create controller manager for each test
		var err error
		k8sManager, err = ctrl.NewManager(cfg, ctrl.Options{
			Scheme: k8sClient.Scheme(),
			Metrics: server.Options{
				BindAddress: "0",
			},
			LeaderElection: false,
			Controller: config.Controller{
				SkipNameValidation: ptr.To(true),
			},
		})
		Expect(err).ToNot(HaveOccurred())

		// Create reconciler with mocked dependencies
		reconciler = &VolumeAttachmentReconciler{
			Client:   k8sManager.GetClient(),
			Scheme:   k8sManager.GetScheme(),
			NodeName: testNodeName,
		}

		// Configure mock functions
		reconciler.dialPluginClientFunc = func(ctx context.Context, pluginName string) (pb.StoragePluginServiceClient, pb.IdentityServiceClient, func(), error) {
			return mockPluginClient, mockIdentityClient, func() {}, nil
		}
		reconciler.createSNAPClientFunc = func(snapProvider string) (rpcclient.Client, error) {
			return mockSNAPClient, nil
		}

		// Set up controller with manager
		err = reconciler.SetupWithManager(k8sManager)
		Expect(err).ToNot(HaveOccurred())

		controllerFinished = make(chan struct{})
		// Start controller manager
		controllerCtx, controllerCancel = context.WithCancel(context.Background())
		go func() {
			defer GinkgoRecover()
			defer close(controllerFinished)
			err := k8sManager.Start(controllerCtx)
			Expect(err).ToNot(HaveOccurred(), "failed to run manager")
		}()
		// Wait for manager to be ready
		Eventually(func(g Gomega) {
			g.Expect(k8sManager.GetCache().WaitForCacheSync(controllerCtx)).To(BeTrue())
		}, testTimeout, testInterval).Should(Succeed())
	})

	AfterEach(func() {
		By("Cleaning up the objects")
		Expect(testutils.CleanupAndWait(testCtx, k8sClient, cleanupObjects...)).To(Succeed())
		cleanupObjects = nil
		// Stop controller manager
		if controllerCancel != nil {
			controllerCancel()
			Eventually(controllerFinished, testTimeout, testInterval).Should(BeClosed())
		}
		testCtrl.Finish()
	})

	getVolumeAttachment := func(storageAttached bool, dpuAttached bool) *snapstoragev1.VolumeAttachment {
		va := &snapstoragev1.VolumeAttachment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testVolumeAttName,
				Namespace: testNamespace,
			},
			Spec: snapstoragev1.VolumeAttachmentSpec{
				NodeName: testNodeName,
				Source: snapstoragev1.VolumeSource{
					VolumeRef: &snapstoragev1.ObjectRef{
						Name:      testVolumeName,
						Namespace: testNamespace,
					},
				},
				FunctionTypeConfig: snapstoragev1.FunctionTypeConfig{
					FunctionType:    "vf",
					HotplugFunction: false,
				},
			},
		}
		va.Status.StorageAttached = storageAttached
		va.Status.DPU.Attached = dpuAttached
		return va
	}

	getVolume := func(volumeMode corev1.PersistentVolumeMode) *snapstoragev1.Volume {
		v := &snapstoragev1.Volume{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testVolumeName,
				Namespace: testNamespace,
			},
			Spec: snapstoragev1.VolumeSpec{
				Request: snapstoragev1.VolumeRequest{
					VolumeMode: &volumeMode,
					CapacityRange: snapstoragev1.CapacityRange{
						Request: resource.MustParse("1Gi"),
					},
				},
				VolumeSpecDPU: snapstoragev1.VolumeSpecDPU{
					StorageVendorPluginName: testPluginName,
					CSIReference: snapstoragev1.CSIReference{
						PVCRef: &snapstoragev1.ObjectRef{
							Name: testVolumeName,
						},
					},
				},
			},
		}
		return v
	}

	createVolumeAttachment := func(cr *snapstoragev1.VolumeAttachment) {
		orig := cr.DeepCopy()
		Expect(k8sClient.Create(testCtx, cr)).To(Succeed())
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(testCtx, client.ObjectKeyFromObject(cr), cr)).NotTo(HaveOccurred())
			cr.Status = orig.Status
			g.Expect(k8sClient.Status().Update(testCtx, cr)).To(Succeed())
		}, testTimeout, testInterval).Should(Succeed())
	}

	createVolume := func(cr *snapstoragev1.Volume) {
		orig := cr.DeepCopy()
		Expect(k8sClient.Create(testCtx, cr)).To(Succeed())
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(testCtx, client.ObjectKeyFromObject(cr), cr)).NotTo(HaveOccurred())
			cr.Status = orig.Status
			g.Expect(k8sClient.Status().Update(testCtx, cr)).To(Succeed())
		}, testTimeout, testInterval).Should(Succeed())
	}

	setupMockExpectations := func(volumeMode corev1.PersistentVolumeMode) {
		// Mock Identity Service calls
		mockIdentityClient.EXPECT().
			GetPluginInfo(gomock.Any(), gomock.Any()).
			Return(&pb.GetPluginInfoResponse{
				Name:          testPluginName,
				VendorVersion: "1.0.0",
			}, nil).
			AnyTimes()

		mockIdentityClient.EXPECT().
			Probe(gomock.Any(), gomock.Any()).
			Return(&pb.ProbeResponse{Ready: &wrapperspb.BoolValue{Value: true}}, nil).
			AnyTimes()

		// Mock Storage Plugin Service calls
		mockPluginClient.EXPECT().
			StoragePluginGetCapabilities(gomock.Any(), gomock.Any()).
			Return(&pb.StoragePluginGetCapabilitiesResponse{
				Capabilities: []*pb.StoragePluginServiceCapability{
					{Type: &pb.StoragePluginServiceCapability_Rpc{
						Rpc: &pb.StoragePluginServiceCapability_RPC{
							Type: pb.StoragePluginServiceCapability_RPC_TYPE_CREATE_DELETE_BLOCK_DEVICE,
						},
					}},
					{Type: &pb.StoragePluginServiceCapability_Rpc{
						Rpc: &pb.StoragePluginServiceCapability_RPC{
							Type: pb.StoragePluginServiceCapability_RPC_TYPE_CREATE_DELETE_FS_DEVICE,
						},
					}},
				},
			}, nil).
			AnyTimes()

		mockPluginClient.EXPECT().
			GetSNAPProvider(gomock.Any(), gomock.Any()).
			Return(&pb.GetSNAPProviderResponse{
				ProviderName: testProviderName,
			}, nil).
			AnyTimes()

		mockPluginClient.EXPECT().
			CreateDevice(gomock.Any(), gomock.Any()).
			Return(&pb.CreateDeviceResponse{
				DeviceName: testDeviceName,
			}, nil).
			AnyTimes()

		mockPluginClient.EXPECT().
			GetDevice(gomock.Any(), gomock.Any()).
			Return(&pb.GetDeviceResponse{
				VolumeMode: string(volumeMode),
			}, nil).
			AnyTimes()

		// Mock SNAP client calls based on volume mode
		if volumeMode == corev1.PersistentVolumeBlock {
			mockSNAPClient.EXPECT().
				ExposeBlockDevice(gomock.Any(), gomock.Any(), gomock.Any()).
				Return(1, testPCIAddr, "test-uuid", nil).
				AnyTimes()
		} else {
			mockSNAPClient.EXPECT().
				ExposeFSDevice(gomock.Any(), gomock.Any(), gomock.Any()).
				Return("test-tag", testPCIAddr, nil).
				AnyTimes()
		}
		// Mock detachment calls
		mockPluginClient.EXPECT().
			DeleteDevice(gomock.Any(), gomock.Any()).
			Return(&pb.DeleteDeviceResponse{}, nil).
			AnyTimes()

		mockSNAPClient.EXPECT().
			DestroyBlockDevice(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(nil).
			AnyTimes()

		mockSNAPClient.EXPECT().
			Close().
			Return(nil).
			AnyTimes()
	}

	Context("When VolumeAttachment is created", func() {
		It("should skip reconciliation if NodeName doesn't match", func() {
			By("Creating VolumeAttachment with different NodeName")
			volumeAttachment = getVolumeAttachment(true, false)
			volumeAttachment.Spec.NodeName = "different-node"
			createVolumeAttachment(volumeAttachment)
			cleanupObjects = append(cleanupObjects, volumeAttachment)

			By("Verifying finalizer is not added and VolumeAttachment is not attached")
			Consistently(func(g Gomega) {
				g.Expect(k8sClient.Get(testCtx, client.ObjectKeyFromObject(volumeAttachment), volumeAttachment)).NotTo(HaveOccurred())
				g.Expect(controllerutil.ContainsFinalizer(volumeAttachment, dpuFinalizer)).To(BeFalse())
				g.Expect(volumeAttachment.Status.DPU.Attached).To(BeFalse())
			}, testConsistentlyTimeout, testInterval).Should(Succeed())
		})

		It("should skip attachment if storageAttached is false", func() {
			By("Creating VolumeAttachment with storageAttached=false")
			volumeAttachment = getVolumeAttachment(false, false)
			createVolumeAttachment(volumeAttachment)
			cleanupObjects = append(cleanupObjects, volumeAttachment)

			By("Verifying finalizer is not added and VolumeAttachment is not attached")
			Consistently(func(g Gomega) {
				g.Expect(k8sClient.Get(testCtx, client.ObjectKeyFromObject(volumeAttachment), volumeAttachment)).NotTo(HaveOccurred())
				g.Expect(controllerutil.ContainsFinalizer(volumeAttachment, dpuFinalizer)).To(BeFalse())
				g.Expect(volumeAttachment.Status.DPU.Attached).To(BeFalse())
			}, testConsistentlyTimeout, testInterval).Should(Succeed())
		})
	})

	Context("When handling attachment with Volume reference", func() {
		It("should successfully attach a block volume", func() {
			By("Setting up mock expectations for block volume")
			setupMockExpectations(corev1.PersistentVolumeBlock)

			By("Creating Volume with block mode")
			volume = getVolume(corev1.PersistentVolumeBlock)
			createVolume(volume)
			cleanupObjects = append(cleanupObjects, volume)

			By("Creating VolumeAttachment with storageAttached=true")
			volumeAttachment = getVolumeAttachment(true, false)
			createVolumeAttachment(volumeAttachment)
			cleanupObjects = append(cleanupObjects, volumeAttachment)

			By("Verifying VolumeAttachment gets attached")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(testCtx, client.ObjectKeyFromObject(volumeAttachment), volumeAttachment)).NotTo(HaveOccurred())
				g.Expect(controllerutil.ContainsFinalizer(volumeAttachment, dpuFinalizer)).To(BeTrue())
				g.Expect(volumeAttachment.Status.DPU.Attached).To(BeTrue())
				g.Expect(volumeAttachment.Status.DPU.DeviceName).NotTo(BeEmpty())
				g.Expect(volumeAttachment.Status.DPU.PCIDeviceAddress).NotTo(BeEmpty())
			}, testTimeout, testInterval).Should(Succeed())
		})

		It("should successfully attach a filesystem volume", func() {
			By("Setting up mock expectations for filesystem volume")
			setupMockExpectations(corev1.PersistentVolumeFilesystem)

			By("Creating Volume with filesystem mode")
			volume = getVolume(corev1.PersistentVolumeFilesystem)
			createVolume(volume)
			cleanupObjects = append(cleanupObjects, volume)

			By("Creating VolumeAttachment with storageAttached=true")
			volumeAttachment = getVolumeAttachment(true, false)
			createVolumeAttachment(volumeAttachment)
			cleanupObjects = append(cleanupObjects, volumeAttachment)

			By("Verifying VolumeAttachment gets attached")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(testCtx, client.ObjectKeyFromObject(volumeAttachment), volumeAttachment)).NotTo(HaveOccurred())
				g.Expect(controllerutil.ContainsFinalizer(volumeAttachment, dpuFinalizer)).To(BeTrue())
				g.Expect(volumeAttachment.Status.DPU.Attached).To(BeTrue())
				g.Expect(volumeAttachment.Status.DPU.DeviceName).NotTo(BeEmpty())
				g.Expect(volumeAttachment.Status.DPU.PCIDeviceAddress).NotTo(BeEmpty())
			}, testTimeout, testInterval).Should(Succeed())
		})

		It("should not attach if Volume is not found", func() {
			By("Creating VolumeAttachment without corresponding Volume")
			volumeAttachment = getVolumeAttachment(true, false)
			createVolumeAttachment(volumeAttachment)
			cleanupObjects = append(cleanupObjects, volumeAttachment)

			By("Verifying VolumeAttachment is not attached")
			Consistently(func(g Gomega) {
				g.Expect(k8sClient.Get(testCtx, client.ObjectKeyFromObject(volumeAttachment), volumeAttachment)).NotTo(HaveOccurred())
				g.Expect(volumeAttachment.Status.DPU.Attached).To(BeFalse())
			}, testConsistentlyTimeout, testInterval).Should(Succeed())
		})
	})

	Context("When handling detachment", func() {
		It("should successfully detach a volume", func() {
			By("Setting up mock expectations for detachment")
			setupMockExpectations(corev1.PersistentVolumeBlock)

			By("Creating Volume")
			volume = getVolume(corev1.PersistentVolumeBlock)
			createVolume(volume)
			cleanupObjects = append(cleanupObjects, volume)

			By("Creating attached VolumeAttachment")
			volumeAttachment = getVolumeAttachment(true, true)
			volumeAttachment.Finalizers = []string{dpuFinalizer}
			createVolumeAttachment(volumeAttachment)

			// Update status fields separately using Eventually for retry logic
			Eventually(func() error {
				// Get obj object to avoid stale resourceVersion
				obj := &snapstoragev1.VolumeAttachment{}
				if err := k8sClient.Get(testCtx, client.ObjectKeyFromObject(volumeAttachment), obj); err != nil {
					return err
				}
				// Update status fields
				obj.Status.StorageAttached = true
				obj.Status.DPU.Attached = true
				obj.Status.DPU.DeviceName = testDeviceName
				obj.Status.DPU.PCIDeviceAddress = testPCIAddr
				obj.Status.DPU.BdevAttrs = snapstoragev1.BdevAttrs{
					NVMeNsID: 1,
					NVMeUUID: "test-uuid",
				}
				return k8sClient.Status().Update(testCtx, obj)
			}, testTimeout, testInterval).Should(Succeed())
			cleanupObjects = append(cleanupObjects, volumeAttachment)

			By("Deleting VolumeAttachment to trigger detachment")
			Expect(k8sClient.Delete(testCtx, volumeAttachment)).To(Succeed())

			By("Verifying VolumeAttachment is detached and finalizer removed")
			Eventually(func(g Gomega) {
				err := k8sClient.Get(testCtx, client.ObjectKeyFromObject(volumeAttachment), volumeAttachment)
				g.Expect(err).To(HaveOccurred())
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}, testTimeout, testInterval).Should(Succeed())
		})
	})

	Context("When VolumeAttachment has no VolumeRef", func() {
		It("should skip processing when VolumeRef is empty", func() {
			By("Creating VolumeAttachment without VolumeRef")
			volumeAttachment = getVolumeAttachment(true, false)
			volumeAttachment.Spec.Source.VolumeRef = &snapstoragev1.ObjectRef{
				Name: "", // Empty name
			}
			createVolumeAttachment(volumeAttachment)
			cleanupObjects = append(cleanupObjects, volumeAttachment)

			By("Verifying VolumeAttachment remains unattached")
			Consistently(func(g Gomega) {
				g.Expect(k8sClient.Get(testCtx, client.ObjectKeyFromObject(volumeAttachment), volumeAttachment)).NotTo(HaveOccurred())
				g.Expect(volumeAttachment.Status.DPU.Attached).To(BeFalse())
			}, testConsistentlyTimeout, testInterval).Should(Succeed())
		})
	})
})
