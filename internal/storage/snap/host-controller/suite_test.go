/*
COPYRIGHT 2025 NVIDIA

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

package hostcontroller

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	storagev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/dpucluster"
	testutils "github.com/nvidia/doca-platform/test/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

const (
	timeout                 = time.Second * 15
	interval                = time.Millisecond * 250
	testNsNameHost          = "test-ns"
	testNsNameDPU           = "test-ns-dpu"
	testDPUClusterName      = "testenv"
	testDPUClusterNamespace = testNsNameHost
)

var (
	cfg *rest.Config
	// testClient is the client for the host cluster
	testClient client.Client
	// testClientDPU is the client for the DPU cluster. While it points to the same cluster as testClient,
	// keeping them separate improves test readability by clearly distinguishing between host and DPU cluster operations
	testClientDPU              client.Client
	testEnv                    *envtest.Environment
	ctx, testManagerCancelFunc = context.WithCancel(ctrl.SetupSignalHandler())
	remoteCache                *dpucluster.RemoteCache
	setupObjects               []client.Object
)

func TestHostController(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Storage HOST controller")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "..", "..", "config", "provisioning", "crd", "bases"),
			filepath.Join("..", "..", "..", "..", "config", "storage", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
		// The BinaryAssetsDirectory is only required if you want to run the tests directly
		// without call the makefile target test. If not informed it will look for the
		// default path defined in controller-runtime which is /usr/local/kubebuilder/.
		// Note that you must have the required binaries setup under the bin directory to perform
		// the tests directly. When we run make test it will be setup and used automatically.
		BinaryAssetsDirectory: filepath.Join("..", "..", "..", "hack", "tools", "bin", "k8s",
			fmt.Sprintf("1.33.0-%s-%s", runtime.GOOS, runtime.GOARCH)),
	}

	var err error
	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	Expect(provisioningv1.AddToScheme(scheme.Scheme)).To(Succeed())
	Expect(storagev1.AddToScheme(scheme.Scheme)).To(Succeed())

	s := scheme.Scheme
	// +kubebuilder:scaffold:scheme

	testClient, err = client.New(cfg, client.Options{Scheme: s})
	Expect(err).NotTo(HaveOccurred())
	Expect(testClient).NotTo(BeNil())

	testClientDPU = testClient

	testNsHost := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNsNameHost}}
	Expect(testClient.Create(ctx, testNsHost)).To(Succeed())

	testNsDPU := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNsNameDPU}}
	Expect(testClient.Create(ctx, testNsDPU)).To(Succeed())

	By("setting up and running the test reconciler")
	testManager, err := ctrl.NewManager(cfg,
		ctrl.Options{
			Scheme: scheme.Scheme,
			Cache: cache.Options{
				ByObject: map[client.Object]cache.ByObject{
					// watch DPUVolume and DPUVolumeAttachment only in namespace where the controller runs
					&storagev1.DPUVolume{}:           {Namespaces: map[string]cache.Config{testNsNameHost: {}}},
					&storagev1.DPUVolumeAttachment{}: {Namespaces: map[string]cache.Config{testNsNameHost: {}}},
					&storagev1.DPUStorageVendor{}:    {Namespaces: map[string]cache.Config{testNsNameHost: {}}},
					&storagev1.DPUStoragePolicy{}:    {Namespaces: map[string]cache.Config{testNsNameHost: {}}},
				},
			},
			// Set metrics server bind address to 0 to disable it.
			Metrics: server.Options{
				BindAddress: "0",
			}})
	Expect(err).ToNot(HaveOccurred())

	// Setup field indexers
	Expect(SetupIndexers(ctx, testManager)).To(Succeed())

	// new remote cache
	remoteCache, err = dpucluster.SetupRemoteCacheWithManager(ctx, testManager,
		dpucluster.OptionTimeout{Timeout: time.Second * 30},
		dpucluster.OptionHostClient{Client: testManager.GetClient()},
		dpucluster.OptionScheme{Scheme: testManager.GetScheme()},
		dpucluster.OptionUserAgent{UserAgent: "snap-host-controller"})
	Expect(err).ToNot(HaveOccurred())

	reconcileOptions := Options{
		Namespace:       testNsNameHost,
		TargetNamespace: testNsNameDPU,
		DPUCluster: types.NamespacedName{
			Name:      testDPUClusterName,
			Namespace: testDPUClusterNamespace,
		},
	}

	err = (&DPUVolumeReconciler{
		Client:      testManager.GetClient(),
		Scheme:      testManager.GetScheme(),
		RemoteCache: remoteCache,
		Options:     reconcileOptions,
	}).SetupWithManager(testManager)
	Expect(err).NotTo(HaveOccurred())

	err = (&DPUVolumeAttachmentReconciler{
		Client:      testManager.GetClient(),
		Scheme:      testManager.GetScheme(),
		RemoteCache: remoteCache,
		Options:     reconcileOptions,
	}).SetupWithManager(testManager)
	Expect(err).NotTo(HaveOccurred())

	err = (&DPUStorageVendorReconciler{
		Client:      testManager.GetClient(),
		Scheme:      testManager.GetScheme(),
		RemoteCache: remoteCache,
		Options:     reconcileOptions,
	}).SetupWithManager(testManager)
	Expect(err).NotTo(HaveOccurred())

	err = (&DPUStoragePolicyReconciler{
		Client:      testManager.GetClient(),
		Scheme:      testManager.GetScheme(),
		RemoteCache: remoteCache,
		Options:     reconcileOptions,
	}).SetupWithManager(testManager)
	Expect(err).NotTo(HaveOccurred())

	go func() {
		defer GinkgoRecover()
		err = testManager.Start(ctx)
		Expect(err).ToNot(HaveOccurred(), "failed to run manager")
	}()

	By("Faking GetdpuClusters to use the envtest cluster instead of a separate one")
	dpuCluster := testutils.GetTestDPUCluster(testDPUClusterNamespace, testDPUClusterName)
	kamajiSecret, err := testutils.GetFakeKamajiClusterSecretFromEnvtest(dpuCluster, cfg)
	Expect(err).NotTo(HaveOccurred())
	Expect(testClient.Create(ctx, kamajiSecret)).To(Succeed())
	setupObjects = append(setupObjects, kamajiSecret)

	Expect(testClient.Create(ctx, &dpuCluster)).To(Succeed())
	Eventually(func(g Gomega) {
		g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(&dpuCluster), &dpuCluster)).NotTo(HaveOccurred())
		dpuCluster.Status.Phase = provisioningv1.PhaseReady
		g.Expect(testClient.Status().Update(ctx, &dpuCluster)).To(Succeed())
	}).Should(Succeed())
	setupObjects = append(setupObjects, &dpuCluster)
})

var _ = AfterSuite(func() {
	By("remove Fake DPU cluster objects")
	Expect(testutils.CleanupAndWait(ctx, testClient, setupObjects...)).To(Succeed())

	By("tearing down the test environment")
	if testManagerCancelFunc != nil {
		testManagerCancelFunc()
	}
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})

func createObjects(objs ...client.Object) {
	for _, o := range objs {
		ExpectWithOffset(1, testClient.Create(ctx, o)).NotTo(HaveOccurred())
	}
}

func createObjectsDPU(objs ...client.Object) {
	for _, o := range objs {
		ExpectWithOffset(1, testClientDPU.Create(ctx, o)).NotTo(HaveOccurred())
	}
}

func getDPUVolume() *storagev1.DPUVolume {
	return &storagev1.DPUVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-vol1",
			Namespace: testNsNameHost,
		},
		Spec: storagev1.DPUVolumeSpec{
			DPUStoragePolicyName: "test-policy",
			Parameters: map[string]string{
				"param1": "value1",
				"param2": "value2",
			},
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: *resource.NewQuantity(1073741824, resource.BinarySI),
				},
			},
		},
	}
}

func getVolume() *storagev1.Volume {
	return &storagev1.Volume{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-vol1",
			Namespace: testNsNameDPU,
		},
		Spec: storagev1.VolumeSpec{
			StorageParameters: map[string]string{
				"param1": "value1",
				"param2": "value2",
				"policy": "test-policy",
			},
			Request: storagev1.VolumeRequest{
				CapacityRange: storagev1.CapacityRange{
					Request: *resource.NewQuantity(1073741824, resource.BinarySI),
				},
			},
		},
	}
}

func getDPUVolumeAttachment() *storagev1.DPUVolumeAttachment {
	return &storagev1.DPUVolumeAttachment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-vol1-attach1",
			Namespace: testNsNameHost,
		},
		Spec: storagev1.DPUVolumeAttachmentSpec{
			DPUNodeName:   "test-node",
			DPUVolumeName: "test-vol1",
			FunctionTypeConfig: storagev1.FunctionTypeConfig{
				FunctionType:    "vf",
				HotplugFunction: false,
			},
		},
	}
}

func getVolumeAttachment() *storagev1.VolumeAttachment {
	return &storagev1.VolumeAttachment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-vol1-attach1",
			Namespace: testNsNameDPU,
		},
		Spec: storagev1.VolumeAttachmentSpec{
			NodeName: "test-node-dpu",
			Source: storagev1.VolumeSource{
				VolumeRef: &storagev1.ObjectRef{
					Name:      "test-vol1",
					Namespace: testNsNameDPU,
				},
			},
			FunctionTypeConfig: storagev1.FunctionTypeConfig{
				FunctionType:    "vf",
				HotplugFunction: false,
			},
		},
	}
}

func getDPUNode() *provisioningv1.DPUNode {
	return &provisioningv1.DPUNode{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-node",
			Namespace: testNsNameHost,
		},
	}
}

func getDPU() *provisioningv1.DPU {
	return &provisioningv1.DPU{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-node-dpu",
			Namespace: testNsNameHost,
		},
		Spec: provisioningv1.DPUSpec{
			SerialNumber:  "MT25066004C7",
			DPUNodeName:   "test-node",
			DPUDeviceName: "test-device",
			BFB:           "test-bfb",
		},
	}
}

func updateVolumeStatusToAvailable(name string) {
	EventuallyWithOffset(1, func(g Gomega) {
		vol := &storagev1.Volume{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNsNameDPU}}
		volKey := client.ObjectKeyFromObject(vol)
		g.Expect(testClientDPU.Get(ctx, volKey, vol)).NotTo(HaveOccurred())

		// Set VolumeSpecDPU fields to simulate controller behavior and make DPUVolume ready
		vol.Spec.VolumeSpecDPU = storagev1.VolumeSpecDPU{
			ID:                      "test-vol-id-123",
			Capacity:                *resource.NewQuantity(1073741824, resource.BinarySI),
			AccessModes:             []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			ReclaimPolicy:           corev1.PersistentVolumeReclaimDelete,
			StorageVendorName:       "test-vendor",
			StorageVendorPluginName: "test-plugin",
			VolumeAttributes: map[string]string{
				"test-attr1": "value1",
				"test-attr2": "value2",
			},
			CSIReference: storagev1.CSIReference{
				CSIDriverName:    "test-csi-driver",
				StorageClassName: "test-storage-class",
				PVCRef: &storagev1.ObjectRef{
					Name:      "test-pvc",
					Namespace: testNsNameDPU,
				},
			},
		}
		g.Expect(testClientDPU.Update(ctx, vol)).NotTo(HaveOccurred())
		vol.Status.State = storagev1.VolumeStateAvailable
		g.Expect(testClientDPU.Status().Update(ctx, vol)).NotTo(HaveOccurred())
	}, timeout, interval).Should(Succeed())
}
