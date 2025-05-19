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

package webhooks

import (
	"context"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
)

var _ = Describe("DPUCluster", func() {

	const (
		DefaultObjName = "obj-dpucluster"
		DefaultObjType = string(provisioningv1.StaticCluster)
	)

	var getObjKey = func(obj *provisioningv1.DPUCluster) types.NamespacedName {
		return types.NamespacedName{
			Name:      obj.Name,
			Namespace: obj.Namespace,
		}
	}

	var createObj = func(name string) *provisioningv1.DPUCluster {
		return &provisioningv1.DPUCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: "default",
			},
			Spec:   provisioningv1.DPUClusterSpec{},
			Status: provisioningv1.DPUClusterStatus{},
		}
	}

	BeforeEach(func() {
		// Add any setup steps that needs to be executed before each test
	})

	AfterEach(func() {
		// Add any teardown steps that needs to be executed after each test
	})

	Context("obj test context", func() {
		ctx := context.Background()

		It("create and get object", func() {
			obj := createObj(DefaultObjName)
			obj.Spec.Type = DefaultObjType
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			objFetched := &provisioningv1.DPUCluster{}
			err = k8sClient.Get(ctx, getObjKey(obj), objFetched)
			Expect(err).NotTo(HaveOccurred())
			Expect(objFetched).To(Equal(obj))
		})

		It("delete object", func() {
			obj := createObj(DefaultObjName)
			obj.Spec.Type = DefaultObjType
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Delete(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Get(ctx, getObjKey(obj), obj)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})

		It("update object", func() {
			obj := createObj(DefaultObjName)
			obj.Spec.Type = DefaultObjType
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			err = k8sClient.Update(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			objFetched := &provisioningv1.DPUCluster{}
			err = k8sClient.Get(ctx, getObjKey(obj), objFetched)
			Expect(err).NotTo(HaveOccurred())
			Expect(objFetched).To(Equal(obj))
		})

		It("spec.type validation", func() {
			obj := createObj(DefaultObjName)
			obj.Spec.Type = "dummy.com/type"
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			obj = createObj(DefaultObjName)
			obj.Spec.Type = "dummy.com"
			err = k8sClient.Create(ctx, obj)
			Expect(err).To(HaveOccurred())

			obj = createObj(DefaultObjName)
			obj.Spec.Type = "dummy"
			err = k8sClient.Create(ctx, obj)
			Expect(err).To(HaveOccurred())

			obj = createObj(DefaultObjName)
			obj.Spec.Type = ""
			err = k8sClient.Create(ctx, obj)
			Expect(err).To(HaveOccurred())
		})

		It("spec.maxNodes default", func() {
			obj := createObj(DefaultObjName)
			obj.Spec.Type = DefaultObjType
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(k8sClient.Delete, ctx, obj)
			Expect(obj.Spec.MaxNodes).To(BeEquivalentTo(1000))

			objFetched := &provisioningv1.DPUCluster{}
			err = k8sClient.Get(ctx, getObjKey(obj), objFetched)
			Expect(err).NotTo(HaveOccurred())
			Expect(objFetched.Spec.MaxNodes).To(BeEquivalentTo(1000))
		})

		It("spec.maxNodes validation", func() {
			obj := createObj(DefaultObjName)
			obj.Spec.Type = DefaultObjType
			obj.Spec.MaxNodes = 1
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			obj = createObj(DefaultObjName)
			obj.Spec.Type = DefaultObjType
			obj.Spec.MaxNodes = 0
			err = k8sClient.Create(ctx, obj)
			Expect(err).To(HaveOccurred())

			obj = createObj(DefaultObjName)
			obj.Spec.Type = DefaultObjType
			obj.Spec.MaxNodes = -1
			err = k8sClient.Create(ctx, obj)
			Expect(err).To(HaveOccurred())

			obj = createObj(DefaultObjName)
			obj.Spec.Type = DefaultObjType
			obj.Spec.MaxNodes = 1001
			err = k8sClient.Create(ctx, obj)
			Expect(err).To(HaveOccurred())

			obj = createObj(DefaultObjName)
			obj.Spec.Type = DefaultObjType
			obj.Spec.MaxNodes = 100000
			err = k8sClient.Create(ctx, obj)
			Expect(err).To(HaveOccurred())
		})

		It("spec.kubeconfig can be updated from unassigned value", func() {
			newValue := `dummy_new_kubeconfig`

			obj := createObj(DefaultObjName)
			obj.Spec.Type = DefaultObjType
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			objFetched := &provisioningv1.DPUCluster{}
			err = k8sClient.Get(ctx, getObjKey(obj), objFetched)
			Expect(err).NotTo(HaveOccurred())
			Expect(objFetched.Spec.Kubeconfig).To(Equal(""))

			obj.Spec.Kubeconfig = newValue
			err = k8sClient.Update(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Get(ctx, getObjKey(obj), objFetched)
			Expect(err).NotTo(HaveOccurred())
			Expect(objFetched.Spec.Kubeconfig).To(Equal(newValue))
		})

		It("spec.kubeconfig can be updated from assigned empty value", func() {
			refValue := ``
			newValue := `dummy_new_kubeconfig`

			obj := createObj(DefaultObjName)
			obj.Spec.Type = DefaultObjType
			obj.Spec.Kubeconfig = refValue
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			objFetched := &provisioningv1.DPUCluster{}
			err = k8sClient.Get(ctx, getObjKey(obj), objFetched)
			Expect(err).NotTo(HaveOccurred())
			Expect(objFetched.Spec.Kubeconfig).To(Equal(""))

			obj.Spec.Kubeconfig = newValue
			err = k8sClient.Update(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Get(ctx, getObjKey(obj), objFetched)
			Expect(err).NotTo(HaveOccurred())
			Expect(objFetched.Spec.Kubeconfig).To(Equal(newValue))
		})

		It("spec.kubeconfig is immutable", func() {
			refValue := `kubeconfig-test`
			newValue := `kubeconfig-test-new`

			obj := createObj(DefaultObjName)
			obj.Spec.Type = DefaultObjType
			obj.Spec.Kubeconfig = refValue
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			obj.Spec.Kubeconfig = newValue
			err = k8sClient.Update(ctx, obj)
			Expect(err).To(HaveOccurred())

			objFetched := &provisioningv1.DPUCluster{}
			err = k8sClient.Get(ctx, getObjKey(obj), objFetched)
			Expect(err).NotTo(HaveOccurred())
			Expect(objFetched.Spec.Kubeconfig).To(Equal(refValue))
		})

		It("create from yaml", func() {
			yml := []byte(`
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUCluster
metadata:
  name: dpucluster-1
  namespace: default
spec:
  maxNodes: 10
  version: v1.31.0
  type: static
  clusterEndpoint:
    keepalived:
      vip: 10.10.10.10
      virtualRouterID: 1
      interface: br-dpu
`)
			obj := &provisioningv1.DPUCluster{}
			err := yaml.UnmarshalStrict(yml, obj)
			Expect(err).To(Succeed())
			err = k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			objFetched := &provisioningv1.DPUCluster{}
			err = k8sClient.Get(ctx, getObjKey(obj), objFetched)
			Expect(err).NotTo(HaveOccurred())
			Expect(objFetched.Spec.Type).To(BeEquivalentTo(string(provisioningv1.StaticCluster)))
			Expect(objFetched.Spec.MaxNodes).To(BeEquivalentTo(10))
			Expect(objFetched.Spec.Version).To(BeEquivalentTo("v1.31.0"))
		})

		It("status.phase default", func() {
			obj := createObj(DefaultObjName)
			obj.Spec.Type = DefaultObjType
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(k8sClient.Delete, ctx, obj)
			Expect(obj.Status.Phase).To(BeEquivalentTo(provisioningv1.PhasePending))

			objFetched := &provisioningv1.DPUCluster{}
			err = k8sClient.Get(ctx, getObjKey(obj), objFetched)
			Expect(err).NotTo(HaveOccurred())
			Expect(objFetched.Status.Phase).To(BeEquivalentTo(provisioningv1.PhasePending))
		})
	})
})
