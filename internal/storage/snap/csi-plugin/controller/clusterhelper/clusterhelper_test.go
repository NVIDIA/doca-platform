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

package clusterhelper

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	storagev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"
	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/config"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	testNamespaceName      = "test-ns"
	testClusterRoleBinding = "test-rb"
)

var _ = Describe("Cluster helper", func() {
	It("Run", func(ctx context.Context) {

		By("Create test namespace")
		Expect(testClient.Create(ctx,
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNamespaceName}})).NotTo(HaveOccurred())

		By("Configure KUBECONFIG for the host cluster")
		origKubeconfig := os.Getenv("KUBECONFIG")
		DeferCleanup(func() {
			_ = os.Setenv("KUBECONFIG", origKubeconfig)
		})
		_ = os.Setenv("KUBECONFIG", filepath.Join(tmpDir, "kubeconfig"))

		By("Configure RBAC for the token for DPU cluster")
		Expect(testClient.Create(ctx, &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: testClusterRoleBinding},
			Subjects: []rbacv1.Subject{{
				Kind: "User",
				Name: testUser,
			}},
			RoleRef: rbacv1.RoleRef{
				Kind:     "ClusterRole",
				Name:     "cluster-admin",
				APIGroup: "rbac.authorization.k8s.io",
			},
		})).NotTo(HaveOccurred())

		By("Create test objects")

		Expect(testClient.Create(ctx, &provisioningv1.DPU{
			ObjectMeta: metav1.ObjectMeta{Name: "dpu1", Namespace: testNamespaceName},
			Spec:       provisioningv1.DPUSpec{DPUNodeName: "node1", BFB: "test", DPUDeviceName: "dpudevice1-test"}})).NotTo(HaveOccurred())
		Expect(testClient.Create(ctx, &provisioningv1.DPU{
			ObjectMeta: metav1.ObjectMeta{Name: "dpu2", Namespace: testNamespaceName},
			Spec:       provisioningv1.DPUSpec{DPUNodeName: "node2", BFB: "test", DPUDeviceName: "dpudevice1-test"}})).NotTo(HaveOccurred())
		vol1 := &storagev1.Volume{
			ObjectMeta: metav1.ObjectMeta{Name: "vol1", Namespace: testNamespaceName},
			Spec:       storagev1.VolumeSpec{Request: storagev1.VolumeRequest{AccessModes: []corev1.PersistentVolumeAccessMode{}}}}
		Expect(testClient.Create(ctx, vol1)).NotTo(HaveOccurred())
		vol2 := &storagev1.Volume{
			ObjectMeta: metav1.ObjectMeta{Name: "vol2", Namespace: testNamespaceName},
			Spec:       storagev1.VolumeSpec{Request: storagev1.VolumeRequest{AccessModes: []corev1.PersistentVolumeAccessMode{}}}}
		Expect(testClient.Create(ctx, vol2)).NotTo(HaveOccurred())

		By("Start cluster helper")
		urlData, err := url.Parse(cfg.Host)
		Expect(err).NotTo(HaveOccurred())
		hostPort := strings.Split(urlData.Host, ":")

		h := New(config.Controller{
			TargetNamespace:     testNamespaceName,
			DPUClusterAPIHost:   hostPort[0],
			DPUClusterAPIPort:   hostPort[1],
			DPUClusterTokenFile: filepath.Join(tmpDir, "token"),
			DPUClusterCAFile:    filepath.Join(tmpDir, "ca"),
		})
		startCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		done := make(chan struct{})
		go func() {
			defer GinkgoRecover()
			defer close(done)
			Expect(h.Run(startCtx)).NotTo(HaveOccurred())
		}()

		By("Check clients are available")
		Expect(h.Wait(startCtx)).NotTo(HaveOccurred())
		hostClusterClient, err := h.GetHostClusterClient(ctx)
		Expect(hostClusterClient).NotTo(BeNil())
		Expect(err).NotTo(HaveOccurred())
		dpuClusterClient, err := h.GetDPUClusterClient(ctx)
		Expect(dpuClusterClient).NotTo(BeNil())
		Expect(err).NotTo(HaveOccurred())

		By("Validate indexers")

		// dpu nodeName indexer
		dpuList := &provisioningv1.DPUList{}
		Expect(hostClusterClient.List(ctx, dpuList, client.MatchingFieldsSelector{
			Selector: fields.OneTermEqualSelector("spec.dpuNodeName", "node2")})).NotTo(HaveOccurred())
		Expect(dpuList.Items).To(HaveLen(1))
		Expect(dpuList.Items[0].GetName()).To(Equal("dpu2"))

		// volume ID indexer
		volumeList := &storagev1.VolumeList{}
		Expect(dpuClusterClient.List(ctx, volumeList, client.MatchingFieldsSelector{
			Selector: fields.OneTermEqualSelector("metadata.uid", string(vol1.GetUID()))})).NotTo(HaveOccurred())
		Expect(volumeList.Items).To(HaveLen(1))
		Expect(volumeList.Items[0].GetName()).To(Equal("vol1"))

		cancel()
		By("Wait for cluster helper to stop")
		select {
		case <-done:
		case <-ctx.Done():
		}
	}, SpecTimeout(time.Second*15))
})
