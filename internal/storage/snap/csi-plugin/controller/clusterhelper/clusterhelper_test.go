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
	"time"

	storagev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"
	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/config"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	testNamespaceName = "test-ns"
)

var _ = Describe("Cluster helper", func() {
	It("Run", func(ctx context.Context) {

		By("Create test namespace")
		Expect(testClient.Create(ctx,
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNamespaceName}})).NotTo(HaveOccurred())

		By("Create test objects")
		testVendorName := "test-vendor"
		testVendor := &storagev1.DPUStorageVendor{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testVendorName,
				Namespace: testNamespaceName,
			},
			Spec: storagev1.DPUStorageVendorSpec{
				StorageClassName: "test-storage-class",
				PluginName:       "test-plugin",
			},
		}
		Expect(testClient.Create(ctx, testVendor)).NotTo(HaveOccurred())

		By("Start cluster helper")
		h := New(cfg, config.Controller{
			Namespace: testNamespaceName,
		})
		startCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		done := make(chan struct{})
		go func() {
			defer GinkgoRecover()
			defer close(done)
			Expect(h.Run(startCtx)).NotTo(HaveOccurred())
		}()

		By("Check client is available")
		Expect(h.Wait(startCtx)).NotTo(HaveOccurred())
		c, err := h.GetClient(ctx)
		Expect(c).NotTo(BeNil())
		Expect(err).NotTo(HaveOccurred())

		By("Validate client")
		var foundVendor storagev1.DPUStorageVendor
		err = c.Get(ctx, client.ObjectKey{Name: testVendorName, Namespace: testNamespaceName}, &foundVendor)
		Expect(err).NotTo(HaveOccurred())
		Expect(foundVendor.Spec.StorageClassName).To(Equal("test-storage-class"))
		Expect(foundVendor.Spec.PluginName).To(Equal("test-plugin"))

		cancel()
		By("Wait for cluster helper to stop")
		select {
		case <-done:
		case <-ctx.Done():
		}
	}, SpecTimeout(time.Second*15))
})
