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

package redfish

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	nvidiaNodeMaintenancev1 "github.com/Mellanox/maintenance-operator/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	cfg         *rest.Config
	k8sClient   client.Client
	testEnv     *envtest.Environment
	ctx         context.Context
	cancel      context.CancelFunc
	testNS      *corev1.Namespace
	testObjects []client.Object
)

func TestRedfish(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Redfish Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "..", "..", "..", "..", "config", "provisioning", "crd", "bases"),
			filepath.Join("..", "..", "..", "..", "..", "..", "deploy", "charts", "dpf-operator", "templates", "crds"),
			filepath.Join("..", "..", "..", "..", "..", "..", "test", "objects", "crd", "cert-manager"),
			filepath.Join("..", "..", "..", "..", "..", "..", "test", "objects", "crd", "nodemaintenances"),
		},
		ErrorIfCRDPathMissing: true,
		BinaryAssetsDirectory: filepath.Join("..", "..", "..", "..", "..", "..", "hack", "tools", "bin", "k8s",
			fmt.Sprintf("1.32.0-%s-%s", runtime.GOOS, runtime.GOARCH)),
	}

	var err error
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	testScheme := scheme.Scheme
	err = provisioningv1.AddToScheme(testScheme)
	Expect(err).NotTo(HaveOccurred())
	err = operatorv1.AddToScheme(testScheme)
	Expect(err).NotTo(HaveOccurred())
	err = nvidiaNodeMaintenancev1.AddToScheme(testScheme)
	Expect(err).NotTo(HaveOccurred())

	ctx, cancel = context.WithCancel(context.TODO())
	k8sClient, err = client.New(cfg, client.Options{Scheme: testScheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())
})

var _ = BeforeEach(func() {
	testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "redfish-test-"}}
	Eventually(func() error {
		return k8sClient.Create(ctx, testNS)
	}).WithTimeout(10 * time.Second).Should(Succeed())
	testObjects = []client.Object{}
})

var _ = AfterEach(func() {
	for _, obj := range testObjects {
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, obj))).To(Succeed())
	}
	if testNS != nil {
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, testNS))).To(Succeed())
	}
})

var _ = AfterSuite(func() {
	cancel()
	By("tearing down the test environment")
	err := (func() (err error) {
		sleepTime := 1 * time.Millisecond
		for i := 0; i < 12; i++ {
			if err = testEnv.Stop(); err == nil {
				return
			}
			sleepTime *= 2
			time.Sleep(sleepTime)
		}
		return
	})()
	Expect(err).NotTo(HaveOccurred())
})

func createObject(obj client.Object) {
	Expect(k8sClient.Create(ctx, obj)).To(Succeed())
	testObjects = append(testObjects, obj)
}

func dpuFlavorObj(name string) *provisioningv1.DPUFlavor {
	return &provisioningv1.DPUFlavor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNS.Name,
		},
		Spec: provisioningv1.DPUFlavorSpec{},
	}
}

func dpuObj(name string) *provisioningv1.DPU {
	return &provisioningv1.DPU{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNS.Name,
			Labels:    make(map[string]string),
		},
		Spec: provisioningv1.DPUSpec{
			SerialNumber: "MT25066004C" + utilrand.String(5),
			DPUFlavor:    "dpu-flavor",
			NodeEffect:   provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}},
		},
	}
}

func dpuNodeObj(name string) *provisioningv1.DPUNode {
	return &provisioningv1.DPUNode{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNS.Name,
			Labels: map[string]string{
				cutil.NodeFeatureDiscoveryLabelPrefix + cutil.DPUOOBBridgeConfiguredLabel: "true",
			},
		},
		Spec: provisioningv1.DPUNodeSpec{
			NodeRebootMethod: &provisioningv1.NodeRebootMethod{
				GNOI: &provisioningv1.GNOI{},
			},
			NodeDMSAddress: &provisioningv1.DMSAddress{IP: "127.0.0.1", Port: 57400},
		},
	}
}

func dpuDeviceObj(name string) *provisioningv1.DPUDevice {
	return &provisioningv1.DPUDevice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNS.Name,
		},
		Spec: provisioningv1.DPUDeviceSpec{
			SerialNumber: "MT25066004C" + utilrand.String(5),
			NumberOfPFs:  ptr.To(2),
		},
	}
}

func dpuClusterObj(name string, dpuType string) *provisioningv1.DPUCluster {
	return &provisioningv1.DPUCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNS.Name,
		},
		Spec: provisioningv1.DPUClusterSpec{
			Type:       dpuType,
			Kubeconfig: fmt.Sprintf("%v-admin-kubeconfig", name),
		},
	}
}
