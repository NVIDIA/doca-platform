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

package discovery

import (
	"context"
	"testing"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/utils/tracing"

	. "github.com/onsi/gomega"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const testTraceparent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"

// tracedDiscoveryContext returns the context of a DPUDiscovery reconcile that arrived with
// testTraceparent.
func tracedDiscoveryContext() context.Context {
	parent := &provisioningv1.DPUDiscovery{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "discovery",
			Namespace:   "ns",
			Annotations: map[string]string{tracing.TraceParentAnnotation: testTraceparent},
		},
	}
	ctx, endSpan := tracing.ReconcileSpan(context.Background(), DPUDiscoveryControllerName, parent)
	endSpan(nil)
	return ctx
}

// TestCreateDPUDeviceAndNodePropagatesTraceContext checks that the crawler stamps the reconcile's
// trace on the DPUDevice and DPUNode it creates for a discovered DPU.
func TestCreateDPUDeviceAndNodePropagatesTraceContext(t *testing.T) {
	t.Cleanup(tracing.EnableForTest())
	g := NewWithT(t)
	scheme := runtime.NewScheme()
	g.Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	crawler := NewCrawlerService(c, "ns", 1, false, provisioningv1.BMCFactoryResetPolicyNever)

	g.Expect(crawler.createDPUDeviceAndNode(tracedDiscoveryContext(), CrawlResult{
		IPAddress:    "10.0.0.8",
		Port:         443,
		SerialNumber: "MT1234",
		OPN:          "opn",
	})).To(Succeed())

	device := &provisioningv1.DPUDevice{}
	g.Expect(c.Get(context.Background(), types.NamespacedName{Namespace: "ns", Name: "mt1234"}, device)).To(Succeed())
	g.Expect(device.Annotations).To(HaveKeyWithValue(tracing.GeneratedTraceParentAnnotation, testTraceparent))

	node := &provisioningv1.DPUNode{}
	g.Expect(c.Get(context.Background(), types.NamespacedName{Namespace: "ns", Name: "dpu-node-mt1234"}, node)).To(Succeed())
	g.Expect(node.Annotations).To(HaveKeyWithValue(tracing.GeneratedTraceParentAnnotation, testTraceparent))
}

// TestDiscoverySpanContinuesChangeTrace checks that the reconcile scanning a new generation passes
// the DPUDiscovery's trace on to the DPUDevices it creates, and that a periodic rescan does not.
func TestDiscoverySpanContinuesChangeTrace(t *testing.T) {
	t.Cleanup(tracing.EnableForTest())
	for name, tc := range map[string]struct {
		observedGeneration int64
		wantTrace          bool
	}{
		"spec change":     {observedGeneration: 1, wantTrace: true},
		"periodic rescan": {observedGeneration: 2, wantTrace: false},
	} {
		t.Run(name, func(t *testing.T) {
			g := NewWithT(t)
			dpuDiscovery := &provisioningv1.DPUDiscovery{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "discovery",
					Namespace:   "ns",
					UID:         "discovery-uid",
					Generation:  2,
					Annotations: map[string]string{tracing.TraceParentAnnotation: testTraceparent},
				},
				Status: provisioningv1.DPUDiscoveryStatus{ObservedGeneration: tc.observedGeneration},
			}

			ctx, endSpan := discoverySpan(context.Background(), dpuDiscovery)
			endSpan(nil)
			device := &provisioningv1.DPUDevice{}
			tracing.InjectIntoObject(ctx, device)

			if tc.wantTrace {
				g.Expect(device.Annotations).To(HaveKeyWithValue(tracing.GeneratedTraceParentAnnotation, testTraceparent))
			} else {
				g.Expect(device.Annotations).NotTo(HaveKey(tracing.GeneratedTraceParentAnnotation))
			}
		})
	}
}

// TestDiscoveryScanSpanIdentifiesObject checks that the span of a periodic rescan carries the same
// attributes that identify the DPUDiscovery on its reconcile spans.
func TestDiscoveryScanSpanIdentifiesObject(t *testing.T) {
	g := NewWithT(t)
	exporter := tracetest.NewInMemoryExporter()
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter)))
	t.Cleanup(func() { otel.SetTracerProvider(previous) })
	dpuDiscovery := &provisioningv1.DPUDiscovery{
		ObjectMeta: metav1.ObjectMeta{Name: "discovery", Namespace: "ns", UID: "discovery-uid", Generation: 2},
		Status:     provisioningv1.DPUDiscoveryStatus{ObservedGeneration: 2},
	}

	_, endSpan := discoverySpan(context.Background(), dpuDiscovery)
	endSpan(nil)

	spans := exporter.GetSpans()
	g.Expect(spans).To(HaveLen(1))
	g.Expect(spans[0].Name).To(Equal(DPUDiscoveryControllerName + ".scan"))
	g.Expect(spans[0].Attributes).To(ConsistOf(tracing.ObjectAttributes(DPUDiscoveryControllerName, dpuDiscovery)))
	g.Expect(spans[0].Attributes).To(ContainElement(attribute.String("k8s.resource.kind", "DPUDiscovery")))
}
