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

package dpuselector

import (
	"context"
	"errors"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

var (
	errTest = errors.New("test error")
)

// getDPU returns a DPU object with default test values that can be modified as needed
func getDPU(name string) *provisioningv1.DPU {
	return &provisioningv1.DPU{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "test-namespace",
		},
		Spec: provisioningv1.DPUSpec{
			DPUNodeName: "test-dpunode",
			Cluster: provisioningv1.K8sCluster{
				Namespace: "cluster-namespace",
				Name:      "test-cluster",
			},
		},
	}
}

// returns partially configured fake client builder
func getFakeClientBuilder() *fake.ClientBuilder {
	return fake.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithIndex(&provisioningv1.DPU{}, "spec.dpuNodeName", func(obj client.Object) []string {
			dpu, ok := obj.(*provisioningv1.DPU)
			if !ok {
				return nil
			}
			return []string{dpu.Spec.DPUNodeName}
		})
}

//nolint:goconst
var _ = Describe("DPUSelector", func() {
	var (
		ctx     context.Context
		dpuNode *provisioningv1.DPUNode
	)

	BeforeEach(func() {
		ctx = context.Background()
		// Setup test data
		dpuNode = &provisioningv1.DPUNode{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-dpunode",
				Namespace: "test-namespace",
			},
		}
	})

	Context("GetDPUForNode", func() {
		Context("with indexer field option", func() {
			It("should return the correct DPU when a single DPU exists", func() {
				dpuSelector := New(WithIndexerField{FieldName: "spec.dpuNodeName"})

				matchingDPU := getDPU("test-dpu")
				nonMatchingDPU := getDPU("other-dpu")
				nonMatchingDPU.Spec.DPUNodeName = "different-dpunode"

				fakeClient := getFakeClientBuilder().WithObjects(matchingDPU, nonMatchingDPU).Build()

				result, err := dpuSelector.GetDPUForNode(ctx, fakeClient, dpuNode)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Name).To(Equal("test-dpu"))
				Expect(result.Spec.DPUNodeName).To(Equal(dpuNode.Name))
			})
			It("should return an error when no DPU exists", func() {
				dpuSelector := New(WithIndexerField{FieldName: "spec.dpuNodeName"})

				fakeClient := getFakeClientBuilder().Build()

				result, err := dpuSelector.GetDPUForNode(ctx, fakeClient, dpuNode)
				Expect(err).To(MatchError(ContainSubstring("no DPU found for DPUNode")))
				Expect(result).To(BeNil())
			})
			It("should return an error when multiple DPUs exist", func() {
				dpuSelector := New(WithIndexerField{FieldName: "spec.dpuNodeName"})

				matchingDPU1 := getDPU("test-dpu-1")
				matchingDPU2 := getDPU("test-dpu-2")
				nonMatchingDPU := getDPU("other-dpu")
				nonMatchingDPU.Spec.DPUNodeName = "different-dpunode"

				fakeClient := getFakeClientBuilder().WithObjects(matchingDPU1, matchingDPU2, nonMatchingDPU).Build()

				result, err := dpuSelector.GetDPUForNode(ctx, fakeClient, dpuNode)
				Expect(err).To(MatchError(ContainSubstring("2 DPUs found for DPUNode")))
				Expect(result).To(BeNil())
			})
		})
		Context("with default options (no indexer field)", func() {
			It("should return the correct DPU when a single DPU exists", func() {
				dpuSelector := New()

				matchingDPU := getDPU("test-dpu")
				nonMatchingDPU := getDPU("other-dpu")
				nonMatchingDPU.Spec.DPUNodeName = "different-dpunode"

				fakeClient := getFakeClientBuilder().WithObjects(matchingDPU, nonMatchingDPU).Build()

				result, err := dpuSelector.GetDPUForNode(ctx, fakeClient, dpuNode)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Name).To(Equal("test-dpu"))
				Expect(result.Spec.DPUNodeName).To(Equal(dpuNode.Name))
			})
			It("should return an error when no DPU exists", func() {
				dpuSelector := New()

				fakeClient := getFakeClientBuilder().Build()

				result, err := dpuSelector.GetDPUForNode(ctx, fakeClient, dpuNode)
				Expect(err).To(MatchError(ContainSubstring("no DPU found for DPUNode")))
				Expect(result).To(BeNil())
			})
			It("should return an error when no matching DPU exists", func() {
				dpuSelector := New()

				nonMatchingDPU := getDPU("test-dpu")
				nonMatchingDPU.Spec.DPUNodeName = "different-dpunode"

				fakeClient := getFakeClientBuilder().WithObjects(nonMatchingDPU).Build()

				result, err := dpuSelector.GetDPUForNode(ctx, fakeClient, dpuNode)
				Expect(err).To(MatchError(ContainSubstring("no DPU found for DPUNode")))
				Expect(result).To(BeNil())
			})
			It("should return an error when multiple DPUs exist for the same DPUNode", func() {
				dpuSelector := New()

				matchingDPU1 := getDPU("test-dpu-1")
				matchingDPU2 := getDPU("test-dpu-2")
				nonMatchingDPU := getDPU("other-dpu")
				nonMatchingDPU.Spec.DPUNodeName = "different-dpunode"

				fakeClient := getFakeClientBuilder().WithObjects(matchingDPU1, matchingDPU2, nonMatchingDPU).Build()

				result, err := dpuSelector.GetDPUForNode(ctx, fakeClient, dpuNode)
				Expect(err).To(MatchError(ContainSubstring("2 DPUs found for DPUNode")))
				Expect(result).To(BeNil())
			})
		})
		Context("with label selector option", func() {
			It("should return DPU matching label selector", func() {
				labelSelector, err := labels.Parse("app=storage")
				Expect(err).NotTo(HaveOccurred())
				dpuSelector := New(
					WithIndexerField{FieldName: "spec.dpuNodeName"},
					WithLabelSelector{Selector: labelSelector},
				)

				matchingDPU := getDPU("test-dpu")
				matchingDPU.Labels = map[string]string{"app": "storage"}

				nonMatchingDPU := getDPU("other-dpu")
				nonMatchingDPU.Labels = map[string]string{"app": "compute"}
				nonMatchingDPU.Spec.DPUNodeName = "different-dpunode"

				fakeClient := getFakeClientBuilder().WithObjects(matchingDPU, nonMatchingDPU).Build()

				result, err := dpuSelector.GetDPUForNode(ctx, fakeClient, dpuNode)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Name).To(Equal("test-dpu"))
			})
			It("should not return DPU not matching label selector", func() {
				labelSelector, err := labels.Parse("app=storage")
				Expect(err).NotTo(HaveOccurred())
				dpuSelector := New(
					WithIndexerField{FieldName: "spec.dpuNodeName"},
					WithLabelSelector{Selector: labelSelector},
				)

				matchingNameButWrongLabel := getDPU("test-dpu")
				matchingNameButWrongLabel.Labels = map[string]string{"app": "compute"}

				fakeClient := getFakeClientBuilder().WithObjects(matchingNameButWrongLabel).Build()

				result, err := dpuSelector.GetDPUForNode(ctx, fakeClient, dpuNode)
				Expect(err).To(MatchError(ContainSubstring("no DPU found for DPUNode")))
				Expect(result).To(BeNil())
			})
		})
		Context("with namespace restriction option", func() {
			It("should return DPU from specified namespace", func() {
				targetNamespace := "target-namespace"
				dpuSelector := New(
					WithIndexerField{FieldName: "spec.dpuNodeName"},
					WithInNamespace{Namespace: targetNamespace},
				)

				targetNamespaceDPU := getDPU("test-dpu")
				targetNamespaceDPU.Namespace = targetNamespace

				wrongNamespaceDPU := getDPU("other-dpu")
				wrongNamespaceDPU.Namespace = "different-namespace"

				fakeClient := getFakeClientBuilder().WithObjects(targetNamespaceDPU, wrongNamespaceDPU).Build()

				result, err := dpuSelector.GetDPUForNode(ctx, fakeClient, dpuNode)
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Name).To(Equal("test-dpu"))
				Expect(result.Namespace).To(Equal(targetNamespace))
			})
			It("should not return DPU from different namespace", func() {
				targetNamespace := "target-namespace"
				dpuSelector := New(
					WithIndexerField{FieldName: "spec.dpuNodeName"},
					WithInNamespace{Namespace: targetNamespace},
				)

				differentNamespaceDPU := getDPU("test-dpu")
				differentNamespaceDPU.Namespace = "different-namespace"

				fakeClient := getFakeClientBuilder().WithObjects(differentNamespaceDPU).Build()

				result, err := dpuSelector.GetDPUForNode(ctx, fakeClient, dpuNode)
				Expect(err).To(MatchError(ContainSubstring("no DPU found for DPUNode")))
				Expect(result).To(BeNil())
			})
		})
		Context("when client list call fails", func() {
			It("should return the client error", func() {
				dpuSelector := New(WithIndexerField{FieldName: "spec.dpuNodeName"})

				failingClient := getFakeClientBuilder().
					WithInterceptorFuncs(interceptor.Funcs{
						List: func(ctx context.Context, client client.WithWatch,
							list client.ObjectList, opts ...client.ListOption) error {
							return errTest
						},
					}).
					Build()

				result, err := dpuSelector.GetDPUForNode(ctx, failingClient, dpuNode)
				Expect(err).To(MatchError(ContainSubstring("failed to list DPUs")))
				Expect(errors.Is(err, errTest)).To(BeTrue())
				Expect(result).To(BeNil())
			})
		})
	})
})
