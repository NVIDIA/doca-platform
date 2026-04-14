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

package hostcontroller

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	storagev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/conditions"
	testutils "github.com/nvidia/doca-platform/test/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	corestoragev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

const (
	timeout                 = time.Second * 30
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
	testCtx, testCtxCancelFunc = context.WithCancel(ctrl.SetupSignalHandler())
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
			fmt.Sprintf("1.32.0-%s-%s", runtime.GOOS, runtime.GOARCH)),
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
	Expect(testClient.Create(testCtx, testNsHost)).To(Succeed())

	testNsDPU := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNsNameDPU}}
	Expect(testClient.Create(testCtx, testNsDPU)).To(Succeed())

	By("Faking GetDPUClusters to use the envtest cluster instead of a separate cluster")
	dpuCluster := testutils.GetTestDPUCluster(testDPUClusterNamespace, testDPUClusterName)
	kamajiSecret, err := testutils.GetFakeKamajiClusterSecretFromEnvtest(dpuCluster, cfg)
	Expect(err).NotTo(HaveOccurred())
	Expect(testClient.Create(testCtx, kamajiSecret)).To(Succeed())
	setupObjects = append(setupObjects, kamajiSecret)

	Expect(testClient.Create(testCtx, &dpuCluster)).To(Succeed())
	Eventually(func(g Gomega) {
		g.Expect(testClient.Get(testCtx, client.ObjectKeyFromObject(&dpuCluster), &dpuCluster)).NotTo(HaveOccurred())
		dpuCluster.Status.Phase = provisioningv1.PhaseReady
		g.Expect(testClient.Status().Update(testCtx, &dpuCluster)).To(Succeed())
	}).Should(Succeed())
	setupObjects = append(setupObjects, &dpuCluster)
})

var _ = AfterSuite(func() {
	By("remove Fake DPU cluster objects")
	Expect(testutils.CleanupAndWait(testCtx, testClient, setupObjects...)).To(Succeed())

	By("tearing down the test environment")
	if testCtxCancelFunc != nil {
		testCtxCancelFunc()
	}
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})

func createObjects(objs ...client.Object) {
	for _, o := range objs {
		ExpectWithOffset(1, testClient.Create(testCtx, o)).NotTo(HaveOccurred())
	}
}

func createObjectsDPU(objs ...client.Object) {
	for _, o := range objs {
		ExpectWithOffset(1, testClientDPU.Create(testCtx, o)).NotTo(HaveOccurred())
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
			Cluster: provisioningv1.K8sCluster{
				Namespace: testDPUClusterNamespace,
				Name:      testDPUClusterName,
			},
			SerialNumber:  "MT25066004C7",
			DPUNodeName:   "test-node",
			DPUDeviceName: "test-device",
			BFB:           "test-bfb",
			DPUFlavor:     "test-flavor",
			NodeEffect:    provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}},
		},
	}
}

func setDPUReady(dpu *provisioningv1.DPU) {
	EventuallyWithOffset(1, func(g Gomega) {
		g.Expect(testClient.Get(testCtx, client.ObjectKeyFromObject(dpu), dpu)).NotTo(HaveOccurred())
		conditions.AddTrue(dpu, conditions.TypeReady)
		g.Expect(testClient.Status().Update(testCtx, dpu)).NotTo(HaveOccurred())
	}, timeout, interval).Should(Succeed())
}

func getDPUStoragePolicy() *storagev1.DPUStoragePolicy {
	policy := &storagev1.DPUStoragePolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-policy",
			Namespace: testNsNameHost,
		},
		Spec: storagev1.DPUStoragePolicySpec{
			DPUStorageVendors:  []string{"test-storage-vendor"},
			SelectionAlgorithm: ptr.To(storagev1.SelectionAlgorithmNumberVolumes),
		},
	}
	return policy
}

func getDPUStorageVendor() *storagev1.DPUStorageVendor {
	vendor := &storagev1.DPUStorageVendor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-storage-vendor",
			Namespace: testNsNameHost,
		},
		Spec: storagev1.DPUStorageVendorSpec{
			StorageClassName: "test-storage-class",
			PluginName:       "test-csi-driver",
		},
	}
	return vendor
}

func setDPUStorageVendorReady(vendor *storagev1.DPUStorageVendor, c client.Client) {
	EventuallyWithOffset(1, func(g Gomega) {
		g.Expect(c.Get(testCtx, client.ObjectKeyFromObject(vendor), vendor)).NotTo(HaveOccurred())
		conditions.AddTrue(vendor, conditions.TypeReady)
		conditions.AddTrue(vendor, storagev1.ConditionDPUStorageVendorValid)
		conditions.AddTrue(vendor, storagev1.ConditionDPUStorageVendorReconciled)
		vendor.Status.DPUClusters = []storagev1.ObjectReference{
			{Name: testDPUClusterName, Namespace: testDPUClusterNamespace},
		}
		g.Expect(c.Status().Update(testCtx, vendor)).NotTo(HaveOccurred())
	}, timeout, interval).Should(Succeed())
}

func setDPUStoragePolicyReady(policy *storagev1.DPUStoragePolicy, c client.Client) {
	EventuallyWithOffset(1, func(g Gomega) {
		g.Expect(c.Get(testCtx, client.ObjectKeyFromObject(policy), policy)).NotTo(HaveOccurred())
		conditions.AddTrue(policy, conditions.TypeReady)
		conditions.AddTrue(policy, storagev1.ConditionDPUStoragePolicyValid)
		conditions.AddTrue(policy, storagev1.ConditionDPUStoragePolicyReconciled)
		g.Expect(c.Status().Update(testCtx, policy)).NotTo(HaveOccurred())
	}, timeout, interval).Should(Succeed())
}

func getStorageClass() *corestoragev1.StorageClass {
	return &corestoragev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-storage-class",
		},
		Provisioner: "test-csi-driver",
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

func createAndBindPV(pvc *corev1.PersistentVolumeClaim) *corev1.PersistentVolume {
	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pv-" + pvc.Name,
		},
		Spec: corev1.PersistentVolumeSpec{
			Capacity:                      pvc.Spec.Resources.Requests,
			AccessModes:                   pvc.Spec.AccessModes,
			VolumeMode:                    pvc.Spec.VolumeMode,
			PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimDelete,
			StorageClassName:              *pvc.Spec.StorageClassName,
			ClaimRef: &corev1.ObjectReference{
				Namespace: pvc.Namespace,
				Name:      pvc.Name,
				UID:       pvc.UID,
			},
			PersistentVolumeSource: corev1.PersistentVolumeSource{
				CSI: &corev1.CSIPersistentVolumeSource{
					Driver:       "test-csi-driver",
					VolumeHandle: "test-volume-handle",
					VolumeAttributes: map[string]string{
						"test-attr1": "value1",
						"test-attr2": "value2",
					},
				},
			},
		},
	}

	EventuallyWithOffset(1, func(g Gomega) {
		g.Expect(testClientDPU.Create(testCtx, pv)).NotTo(HaveOccurred())
	}, timeout, interval).Should(Succeed())

	EventuallyWithOffset(1, func(g Gomega) {
		g.Expect(testClientDPU.Get(testCtx, client.ObjectKeyFromObject(pvc), pvc)).NotTo(HaveOccurred())
		pvc.Spec.VolumeName = pv.Name
		g.Expect(testClientDPU.Update(testCtx, pvc)).NotTo(HaveOccurred())
		pvc.Status.Phase = corev1.ClaimBound
		g.Expect(testClientDPU.Status().Update(testCtx, pvc)).NotTo(HaveOccurred())
	}, timeout, interval).Should(Succeed())

	return pv
}

func setDPUVolumeReadyWithVolumeInfo(dpuVolume *storagev1.DPUVolume) {
	EventuallyWithOffset(1, func(g Gomega) {
		g.Expect(testClient.Get(testCtx, client.ObjectKeyFromObject(dpuVolume), dpuVolume)).NotTo(HaveOccurred())
		conditions.AddTrue(dpuVolume, storagev1.ConditionDPUVolumeReconciled)
		conditions.AddTrue(dpuVolume, storagev1.ConditionDPUVolumeScheduled)
		conditions.AddTrue(dpuVolume, storagev1.ConditionDPUVolumeBound)
		conditions.AddTrue(dpuVolume, conditions.TypeReady)
		if dpuVolume.Status.State == nil {
			dpuVolume.Status.State = &storagev1.DPUVolumeState{}
		}
		dpuVolume.Status.State.DPUCluster = &storagev1.ObjectReference{
			Name:      testDPUClusterName,
			Namespace: testDPUClusterNamespace,
		}
		dpuVolume.Status.State.SelectedDPUStorageVendorName = ptr.To("test-storage-vendor")
		dpuVolume.Status.State.CSIDriverName = ptr.To("test-csi-driver")
		dpuVolume.Status.State.VolumeInfo = &storagev1.VolumeInfo{
			VolumeName: ptr.To("test-pv"),
		}
		dpuVolume.Status.Phase = ptr.To(storagev1.DPUVolumePhaseBound)
		g.Expect(testClient.Status().Update(testCtx, dpuVolume)).NotTo(HaveOccurred())
	}, timeout, interval).Should(Succeed())
}

// cleanupTestObjects removes all test objects from the cluster
func cleanupTestObjects(ctx context.Context, c client.Client) {
	// Define all object list types to cleanup
	objectLists := []client.ObjectList{
		&storagev1.DPUVolumeList{},
		&storagev1.VolumeList{},
		&storagev1.DPUVolumeAttachmentList{},
		&storagev1.VolumeAttachmentList{},
		&storagev1.DPUStorageVendorList{},
		&storagev1.DPUStoragePolicyList{},
		&provisioningv1.DPUNodeList{},
		&provisioningv1.DPUList{},
		&corev1.PersistentVolumeClaimList{},
		&corev1.PersistentVolumeList{},
		&corestoragev1.StorageClassList{},
		&corestoragev1.CSIDriverList{},
	}
	cleanupObjects := []client.Object{}
	for _, objList := range objectLists {
		ExpectWithOffset(1, c.List(ctx, objList)).NotTo(HaveOccurred())
		cleanupObjects = append(cleanupObjects, extractObjectsFromList(objList)...)
	}
	ExpectWithOffset(1, testutils.CleanupWithFinalizerRemovalAndWait(ctx, c, cleanupObjects...)).To(Succeed())
}

// extractObjectsFromList extracts objects from a list using reflection
func extractObjectsFromList(objList client.ObjectList) []client.Object {
	var objects []client.Object
	v := reflect.ValueOf(objList).Elem()
	itemsField := v.FieldByName("Items")
	if !itemsField.IsValid() || itemsField.Kind() != reflect.Slice {
		return objects
	}
	for i := 0; i < itemsField.Len(); i++ {
		item := itemsField.Index(i)
		itemPtr := item.Addr().Interface()
		if obj, ok := itemPtr.(client.Object); ok {
			objects = append(objects, obj)
		}
	}
	return objects
}
