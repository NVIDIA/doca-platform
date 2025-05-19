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
	"path/filepath"
	"testing"

	snapstoragev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

var (
	cfg       *rest.Config
	k8sClient client.Client
	testEnv   *envtest.Environment
	ctx       context.Context
	cancel    context.CancelFunc
)

const (
	Provisioner  = "csi.snap.nvidia.com"
	TenantNSName = "tenant"
)

var (
	snapNS   *corev1.Namespace
	tenantNS *corev1.Namespace
)

var getObjKey = func(meta metav1.ObjectMeta) types.NamespacedName {
	return types.NamespacedName{
		Name:      meta.Name,
		Namespace: meta.Namespace,
	}
}

var reclaimPolicy = corev1.PersistentVolumeReclaimRetain
var volumeBindingMode = storagev1.VolumeBindingWaitForFirstConsumer
var createStorageClassObj = func(name string) *storagev1.StorageClass {
	return &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: snapNS.Name,
		},
		Provisioner:          Provisioner,
		ReclaimPolicy:        &reclaimPolicy,
		AllowVolumeExpansion: ptr.To(false),
		VolumeBindingMode:    &volumeBindingMode,
	}
}

var createStorageVendorObj = func(name string, storageClassName string, pluginName string) *snapstoragev1.StorageVendor {
	return &snapstoragev1.StorageVendor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: snapNS.Name,
		},
		Spec: snapstoragev1.StorageVendorSpec{
			StorageClassName: storageClassName,
			PluginName:       pluginName,
		},
	}
}

var createStoragePolicyObj = func(name string, storageVendorList []string) *snapstoragev1.StoragePolicy {
	return &snapstoragev1.StoragePolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: snapNS.Name,
		},
		Spec: snapstoragev1.StoragePolicySpec{
			StorageVendors:      storageVendorList,
			StorageSelectionAlg: snapstoragev1.Random,
		},
	}
}

var volumeMode = corev1.PersistentVolumeBlock
var createNVVolumeObj = func(name string, policy string, request resource.Quantity) *snapstoragev1.Volume {
	return &snapstoragev1.Volume{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: tenantNS.Name,
		},
		Spec: snapstoragev1.VolumeSpec{
			StorageParameters: map[string]string{
				"policy": policy,
			},
			Request: snapstoragev1.VolumeRequest{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				VolumeMode:  &volumeMode,
				CapacityRange: snapstoragev1.CapacityRange{
					Request: request,
				},
			},
			// TODO: should remove?
			VolumeSpecDPU: snapstoragev1.VolumeSpecDPU{
				CSIReference: snapstoragev1.CSIReference{
					CSIDriverName: Provisioner,
				},
			},
		},
	}
}

var createNVVolumeAttachmentObj = func(name string, volumeName string) *snapstoragev1.VolumeAttachment {
	return &snapstoragev1.VolumeAttachment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: tenantNS.Name,
		},
		Spec: snapstoragev1.VolumeAttachmentSpec{
			NodeName: "node1",
			Source: snapstoragev1.VolumeSource{
				VolumeRef: &snapstoragev1.ObjectRef{
					APIVersion: snapstoragev1.GroupVersion.String(),
					Kind:       snapstoragev1.VolumeKind,
					Name:       volumeName,
					Namespace:  tenantNS.Name,
				},
			},
		},
	}
}

var createCSIDrivertObj = func(name string) *storagev1.CSIDriver {
	return &storagev1.CSIDriver{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"provisioner": Provisioner,
			},
		},
		Spec: storagev1.CSIDriverSpec{
			AttachRequired: ptr.To(false),
		},
	}
}

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Snap Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "..", "..", "config", "snap", "crd"),
		},
		ErrorIfCRDPathMissing: true,
	}

	var err error
	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	scheme := scheme.Scheme
	err = snapstoragev1.AddToScheme(scheme)
	Expect(err).NotTo(HaveOccurred())

	// +kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	ctx, cancel = context.WithCancel(ctrl.SetupSignalHandler())

	k8sManager, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:         scheme,
		LeaderElection: false,
		Metrics: server.Options{
			BindAddress: "0",
		}})
	Expect(err).ToNot(HaveOccurred())

	storagePolicyReconciler := &StoragePolicy{
		Client:   k8sManager.GetClient(),
		Scheme:   k8sManager.GetScheme(),
		Recorder: k8sManager.GetEventRecorderFor(storagePolicyControllerName),
	}
	err = storagePolicyReconciler.SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	nvVolumeReconciler := &NVVolume{
		Client:   k8sManager.GetClient(),
		Scheme:   k8sManager.GetScheme(),
		Recorder: k8sManager.GetEventRecorderFor(nvVolumeControllerName),
	}
	err = nvVolumeReconciler.SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	nvVolumeAttachmentReconciler := &NVVolumeAttachment{
		Client:   k8sManager.GetClient(),
		Scheme:   k8sManager.GetScheme(),
		Recorder: k8sManager.GetEventRecorderFor(nvVolumeAttachmentControllerName),
	}
	err = nvVolumeAttachmentReconciler.SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	go func() {
		defer GinkgoRecover()
		err = k8sManager.Start(ctx)
		Expect(err).ToNot(HaveOccurred(), "failed to run manager")
	}()
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	if cancel != nil {
		cancel()
	}
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})
