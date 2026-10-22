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

package controllers

import (
	"context"
	"testing"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/internal/utils/tracing"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const testTraceparent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"

// tracedReconcileContext turns tracing on for the test and returns the context of a reconcile whose
// object arrived with testTraceparent.
func tracedReconcileContext(t *testing.T) context.Context {
	t.Helper()
	t.Cleanup(tracing.EnableForTest())
	parent := &dpuservicev1.DPUServiceChain{
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

func tracingTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	g := NewWithT(t)
	s := runtime.NewScheme()
	g.Expect(dpuservicev1.AddToScheme(s)).To(Succeed())
	return s
}

func TestDPUServiceChainCreateOrUpdateObjectsInDPUClusterPropagatesTraceContext(t *testing.T) {
	g := NewWithT(t)
	scheme := tracingTestScheme(t)
	hostClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	dpuClusterClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &DPUServiceChainReconciler{Client: hostClient}
	dpuServiceChain := &dpuservicev1.DPUServiceChain{ObjectMeta: metav1.ObjectMeta{Name: "chain", Namespace: "ns"}}

	g.Expect(r.createOrUpdateObjectsInDPUCluster(tracedReconcileContext(t), dpuClusterClient, types.NamespacedName{}, dpuServiceChain)).To(Succeed())

	scs := &dpuservicev1.ServiceChainSet{}
	g.Expect(dpuClusterClient.Get(context.Background(), types.NamespacedName{Namespace: "ns", Name: "chain"}, scs)).To(Succeed())
	g.Expect(scs.Annotations).To(HaveKeyWithValue(tracing.GeneratedTraceParentAnnotation, testTraceparent))
}

func TestDPUServiceChainCreateOrUpdateObjectsInDPUClusterWritesNoTraceContextWithoutUpstream(t *testing.T) {
	t.Cleanup(tracing.EnableForTest())
	g := NewWithT(t)
	scheme := tracingTestScheme(t)
	hostClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	dpuClusterClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &DPUServiceChainReconciler{Client: hostClient}
	dpuServiceChain := &dpuservicev1.DPUServiceChain{ObjectMeta: metav1.ObjectMeta{Name: "chain", Namespace: "ns"}}

	g.Expect(r.createOrUpdateObjectsInDPUCluster(context.Background(), dpuClusterClient, types.NamespacedName{}, dpuServiceChain)).To(Succeed())

	scs := &dpuservicev1.ServiceChainSet{}
	g.Expect(dpuClusterClient.Get(context.Background(), types.NamespacedName{Namespace: "ns", Name: "chain"}, scs)).To(Succeed())
	g.Expect(scs.Annotations).ToNot(HaveKey(tracing.GeneratedTraceParentAnnotation))
}

func TestDPUServiceInterfaceCreateOrUpdateObjectsInDPUClusterPropagatesTraceContext(t *testing.T) {
	g := NewWithT(t)
	scheme := tracingTestScheme(t)
	hostClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	dpuClusterClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &DPUServiceInterfaceReconciler{Client: hostClient}
	dpuServiceInterface := &dpuservicev1.DPUServiceInterface{ObjectMeta: metav1.ObjectMeta{Name: "iface", Namespace: "ns"}}

	g.Expect(r.createOrUpdateObjectsInDPUCluster(tracedReconcileContext(t), dpuClusterClient, types.NamespacedName{}, dpuServiceInterface)).To(Succeed())

	sis := &dpuservicev1.ServiceInterfaceSet{}
	g.Expect(dpuClusterClient.Get(context.Background(), types.NamespacedName{Namespace: "ns", Name: "iface"}, sis)).To(Succeed())
	g.Expect(sis.Annotations).To(HaveKeyWithValue(tracing.GeneratedTraceParentAnnotation, testTraceparent))
}
