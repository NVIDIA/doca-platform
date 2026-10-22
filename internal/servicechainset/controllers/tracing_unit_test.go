/*
Copyright 2026 NVIDIA

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

package controller

import (
	"context"
	"testing"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/internal/utils/tracing"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const testTraceparent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"

// tracedReconcileContext turns tracing on for the test and returns the context of a reconcile whose
// object arrived with testTraceparent.
func tracedReconcileContext(t *testing.T) context.Context {
	t.Helper()
	t.Cleanup(tracing.EnableForTest())
	parent := &dpuservicev1.ServiceChainSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "parent",
			Namespace:   "ns",
			Annotations: map[string]string{tracing.TraceParentAnnotation: testTraceparent},
		},
	}
	ctx, endSpan := tracing.ReconcileSpan(context.Background(), "test", parent)
	endSpan(nil)
	return ctx
}

// tracingTestClient returns a fake client with the node index the ServiceChainSet reconciler lists by.
func tracingTestClient(t *testing.T) client.Client {
	t.Helper()
	g := NewWithT(t)
	scheme := runtime.NewScheme()
	g.Expect(dpuservicev1.AddToScheme(scheme)).To(Succeed())
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithIndex(&dpuservicev1.ServiceChain{}, nodeFieldKey, func(o client.Object) []string {
			return []string{*o.(*dpuservicev1.ServiceChain).Spec.Node}
		}).
		Build()
}

func TestServiceChainSetCreateOrUpdateChildPropagatesTraceContext(t *testing.T) {
	g := NewWithT(t)
	c := tracingTestClient(t)
	r := &ServiceChainSetReconciler{Client: c}
	set := &dpuservicev1.ServiceChainSet{ObjectMeta: metav1.ObjectMeta{Name: "set", Namespace: "ns"}}

	g.Expect(r.createOrUpdateChild(tracedReconcileContext(t), set, "node-1")).To(Succeed())

	serviceChains := &dpuservicev1.ServiceChainList{}
	g.Expect(c.List(context.Background(), serviceChains, client.InNamespace("ns"))).To(Succeed())
	g.Expect(serviceChains.Items).To(HaveLen(1))
	g.Expect(serviceChains.Items[0].Annotations).To(HaveKeyWithValue(tracing.GeneratedTraceParentAnnotation, testTraceparent))
}

func TestServiceChainSetCreateOrUpdateChildWritesNoTraceContextWithoutUpstream(t *testing.T) {
	t.Cleanup(tracing.EnableForTest())
	g := NewWithT(t)
	c := tracingTestClient(t)
	r := &ServiceChainSetReconciler{Client: c}
	set := &dpuservicev1.ServiceChainSet{ObjectMeta: metav1.ObjectMeta{Name: "set", Namespace: "ns"}}

	g.Expect(r.createOrUpdateChild(context.Background(), set, "node-1")).To(Succeed())

	serviceChains := &dpuservicev1.ServiceChainList{}
	g.Expect(c.List(context.Background(), serviceChains, client.InNamespace("ns"))).To(Succeed())
	g.Expect(serviceChains.Items).To(HaveLen(1))
	g.Expect(serviceChains.Items[0].Annotations).ToNot(HaveKey(tracing.GeneratedTraceParentAnnotation))
}
