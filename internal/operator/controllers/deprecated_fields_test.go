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
	"fmt"
	"strings"
	"testing"
	"time"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/conditions"
	"github.com/nvidia/doca-platform/pkg/deprecation"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// widgetGVK/widgetListGVK/widgetGVKDep are a synthetic GVK used to test the scanner's scan/cache
// logic in isolation from the real, generated deprecation.KnownDeprecations list.
var (
	widgetGVK     = schema.GroupVersionKind{Group: "example.dpu.nvidia.com", Version: "v1alpha1", Kind: "Widget"}
	widgetListGVK = schema.GroupVersionKind{Group: "example.dpu.nvidia.com", Version: "v1alpha1", Kind: "WidgetList"}
	widgetGVKDep  = deprecation.GVKDeprecations{
		GVK:      widgetGVK,
		ListKind: "WidgetList",
		Fields: []deprecation.DeprecatedField{
			{Segments: []deprecation.PathSegment{{Name: "spec"}, {Name: "oldField"}}, Description: "Deprecated: use newField instead."},
		},
	}
)

func newWidgetScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	utilruntime.Must(metav1.AddMetaToScheme(scheme))
	scheme.AddKnownTypeWithName(widgetGVK, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(widgetListGVK, &unstructured.UnstructuredList{})
	return scheme
}

func newWidget(name string, generation int64, oldFieldSet bool) *unstructured.Unstructured {
	spec := map[string]interface{}{}
	if oldFieldSet {
		spec["oldField"] = "x"
	}
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "example.dpu.nvidia.com/v1alpha1",
		"kind":       "Widget",
		"metadata": map[string]interface{}{
			"name":       name,
			"namespace":  "dpf-operator-system",
			"generation": generation,
		},
		"spec": spec,
	}}
}

func newWidgetReconciler(t *testing.T, objs ...client.Object) *DPFOperatorConfigReconciler {
	t.Helper()
	return &DPFOperatorConfigReconciler{
		UncachedClient:    fake.NewClientBuilder().WithScheme(newWidgetScheme(t)).WithObjects(objs...).Build(),
		KnownDeprecations: []deprecation.GVKDeprecations{widgetGVKDep},
	}
}

func TestReconcileDeprecatedFieldsUsage_NoDeprecatedUsage(t *testing.T) {
	g := NewWithT(t)

	r := newWidgetReconciler(t, newWidget("clean", 1, false))
	config := &operatorv1.DPFOperatorConfig{}

	r.reconcileDeprecatedFieldsUsage(context.Background(), config)

	cond := conditions.Get(config, operatorv1.DeprecatedFieldsNotInUseCondition)
	g.Expect(cond).NotTo(BeNil())
	g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
	g.Expect(cond.Reason).To(Equal(string(conditions.ReasonSuccess)))
	g.Expect(cond.Message).To(BeEmpty())
}

func TestReconcileDeprecatedFieldsUsage_DeprecatedFieldSet(t *testing.T) {
	g := NewWithT(t)

	r := newWidgetReconciler(t, newWidget("legacy", 1, true))
	config := &operatorv1.DPFOperatorConfig{}

	r.reconcileDeprecatedFieldsUsage(context.Background(), config)

	cond := conditions.Get(config, operatorv1.DeprecatedFieldsNotInUseCondition)
	g.Expect(cond).NotTo(BeNil())
	g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
	g.Expect(cond.Reason).To(Equal(string(operatorv1.ReasonDeprecatedFieldsInUse)))
	g.Expect(cond.Message).To(ContainSubstring("CRs are still using deprecated fields:"))
	g.Expect(cond.Message).To(ContainSubstring("Widget"))
	g.Expect(cond.Message).To(ContainSubstring("dpf-operator-system/legacy"))
	g.Expect(cond.Message).To(ContainSubstring("spec.oldField"))
}

func TestReconcileDeprecatedFieldsUsage_NoKnownDeprecations(t *testing.T) {
	g := NewWithT(t)

	r := &DPFOperatorConfigReconciler{
		UncachedClient: fake.NewClientBuilder().WithScheme(newWidgetScheme(t)).Build(),
	}
	config := &operatorv1.DPFOperatorConfig{}

	r.reconcileDeprecatedFieldsUsage(context.Background(), config)

	cond := conditions.Get(config, operatorv1.DeprecatedFieldsNotInUseCondition)
	g.Expect(cond).NotTo(BeNil())
	g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
}

func TestReconcileDeprecatedFieldsUsage_MetadataListErrorSetsUnknown(t *testing.T) {
	g := NewWithT(t)

	base := fake.NewClientBuilder().WithScheme(newWidgetScheme(t)).Build()
	r := &DPFOperatorConfigReconciler{
		UncachedClient:    &erroringListClient{Client: base, errOnKind: "WidgetList"},
		KnownDeprecations: []deprecation.GVKDeprecations{widgetGVKDep},
	}
	config := &operatorv1.DPFOperatorConfig{}

	r.reconcileDeprecatedFieldsUsage(context.Background(), config)

	cond := conditions.Get(config, operatorv1.DeprecatedFieldsNotInUseCondition)
	g.Expect(cond).NotTo(BeNil())
	g.Expect(cond.Status).To(Equal(metav1.ConditionUnknown))
	g.Expect(cond.Reason).To(Equal(string(operatorv1.ReasonInspectionFailed)))
}

func TestReconcileDeprecatedFieldsUsage_GetErrorSetsUnknown(t *testing.T) {
	g := NewWithT(t)

	base := fake.NewClientBuilder().WithScheme(newWidgetScheme(t)).WithObjects(newWidget("legacy", 1, true)).Build()
	r := &DPFOperatorConfigReconciler{
		UncachedClient:    &erroringGetClient{Client: base, errOnKind: "Widget"},
		KnownDeprecations: []deprecation.GVKDeprecations{widgetGVKDep},
	}
	config := &operatorv1.DPFOperatorConfig{}

	r.reconcileDeprecatedFieldsUsage(context.Background(), config)

	cond := conditions.Get(config, operatorv1.DeprecatedFieldsNotInUseCondition)
	g.Expect(cond).NotTo(BeNil())
	g.Expect(cond.Status).To(Equal(metav1.ConditionUnknown))
	g.Expect(cond.Reason).To(Equal(string(operatorv1.ReasonInspectionFailed)))
}

func TestReconcileDeprecatedFieldsUsage_Throttled(t *testing.T) {
	g := NewWithT(t)

	countingClient := &listCountingClient{Client: fake.NewClientBuilder().WithScheme(newWidgetScheme(t)).WithObjects(newWidget("legacy", 1, true)).Build()}

	r := &DPFOperatorConfigReconciler{
		UncachedClient:           countingClient,
		KnownDeprecations:        []deprecation.GVKDeprecations{widgetGVKDep},
		lastDeprecatedFieldsScan: time.Now(),
	}
	config := &operatorv1.DPFOperatorConfig{}

	r.reconcileDeprecatedFieldsUsage(context.Background(), config)

	g.Expect(countingClient.calls).To(Equal(0))
	g.Expect(conditions.Get(config, operatorv1.DeprecatedFieldsNotInUseCondition)).To(BeNil())
}

func TestDeprecatedFieldsForGVK_CacheHitSkipsGet(t *testing.T) {
	g := NewWithT(t)

	base := fake.NewClientBuilder().WithScheme(newWidgetScheme(t)).WithObjects(newWidget("legacy", 5, true)).Build()
	getCounting := &getCountingClient{Client: base}
	r := &DPFOperatorConfigReconciler{UncachedClient: getCounting}

	// First scan: generation 5 is new to the cache, so it must Get the full object.
	findings, err := r.deprecatedFieldsForGVK(context.Background(), widgetGVKDep)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(findings).To(HaveLen(1))
	g.Expect(getCounting.calls).To(Equal(1))

	// Second scan, same generation: must reuse the cached result without another Get.
	findings, err = r.deprecatedFieldsForGVK(context.Background(), widgetGVKDep)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(findings).To(HaveLen(1))
	g.Expect(findings[0].fieldPaths).To(Equal([]string{"spec.oldField"}))
	g.Expect(getCounting.calls).To(Equal(1), "generation unchanged: should not re-fetch the object")
}

func TestDeprecatedFieldsForGVK_GenerationChangeTriggersGet(t *testing.T) {
	g := NewWithT(t)

	widget := newWidget("legacy", 5, true)
	base := fake.NewClientBuilder().WithScheme(newWidgetScheme(t)).WithObjects(widget).Build()
	getCounting := &getCountingClient{Client: base}
	r := &DPFOperatorConfigReconciler{UncachedClient: getCounting}

	_, err := r.deprecatedFieldsForGVK(context.Background(), widgetGVKDep)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(getCounting.calls).To(Equal(1))

	// Simulate the field being cleared and the generation bumped, as a real API server would on a
	// spec change.
	g.Expect(base.Delete(context.Background(), widget)).To(Succeed())
	g.Expect(base.Create(context.Background(), newWidget("legacy", 6, false))).To(Succeed())

	findings, err := r.deprecatedFieldsForGVK(context.Background(), widgetGVKDep)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(findings).To(BeEmpty())
	g.Expect(getCounting.calls).To(Equal(2), "generation changed: should re-fetch the object")
}

func TestDeprecatedFieldsForGVK_RecreatedObjectWithSameGenerationTriggersGet(t *testing.T) {
	g := NewWithT(t)

	widget := newWidget("legacy", 1, true)
	widget.SetUID("uid-1")
	base := fake.NewClientBuilder().WithScheme(newWidgetScheme(t)).WithObjects(widget).Build()
	getCounting := &getCountingClient{Client: base}
	r := &DPFOperatorConfigReconciler{UncachedClient: getCounting}

	findings, err := r.deprecatedFieldsForGVK(context.Background(), widgetGVKDep)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(findings).To(HaveLen(1))

	// Delete and recreate under the same name and generation, without the deprecated field.
	g.Expect(base.Delete(context.Background(), widget)).To(Succeed())
	recreated := newWidget("legacy", 1, false)
	recreated.SetUID("uid-2")
	g.Expect(base.Create(context.Background(), recreated)).To(Succeed())

	findings, err = r.deprecatedFieldsForGVK(context.Background(), widgetGVKDep)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(findings).To(BeEmpty())
	g.Expect(getCounting.calls).To(Equal(2), "UID changed: should re-fetch the object")
}

func TestDeprecatedFieldsForGVK_ManyChangedObjectsUseBulkList(t *testing.T) {
	g := NewWithT(t)

	var objs []client.Object
	for i := 0; i < deprecatedFieldsBulkReadThreshold+5; i++ {
		objs = append(objs, newWidget(fmt.Sprintf("widget-%d", i), 1, i%2 == 0))
	}
	base := fake.NewClientBuilder().WithScheme(newWidgetScheme(t)).WithObjects(objs...).Build()
	getCounting := &getCountingClient{Client: base}
	r := &DPFOperatorConfigReconciler{UncachedClient: getCounting}

	findings, err := r.deprecatedFieldsForGVK(context.Background(), widgetGVKDep)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(findings).To(HaveLen((len(objs) + 1) / 2))
	g.Expect(getCounting.calls).To(Equal(0), "cold cache with many objects: should list, not Get each object")

	findings, err = r.deprecatedFieldsForGVK(context.Background(), widgetGVKDep)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(findings).To(HaveLen((len(objs) + 1) / 2))
	g.Expect(getCounting.calls).To(Equal(0))
}

func TestDeprecatedFieldsForGVK_DeletedObjectDroppedFromCache(t *testing.T) {
	g := NewWithT(t)

	widget := newWidget("legacy", 1, true)
	base := fake.NewClientBuilder().WithScheme(newWidgetScheme(t)).WithObjects(widget).Build()
	r := &DPFOperatorConfigReconciler{UncachedClient: base}

	findings, err := r.deprecatedFieldsForGVK(context.Background(), widgetGVKDep)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(findings).To(HaveLen(1))
	g.Expect(r.deprecatedFieldsCache[widgetGVK]).To(HaveKey(types.NamespacedName{Namespace: "dpf-operator-system", Name: "legacy"}))

	g.Expect(base.Delete(context.Background(), widget)).To(Succeed())

	findings, err = r.deprecatedFieldsForGVK(context.Background(), widgetGVKDep)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(findings).To(BeEmpty())
	g.Expect(r.deprecatedFieldsCache[widgetGVK]).NotTo(HaveKey(types.NamespacedName{Namespace: "dpf-operator-system", Name: "legacy"}))
}

func TestDeprecatedFieldsForGVK_NotFoundDuringGetIsNotAnError(t *testing.T) {
	g := NewWithT(t)

	// The metadata list reports an object that a concurrent delete has already removed by the time
	// the full Get runs.
	base := fake.NewClientBuilder().WithScheme(newWidgetScheme(t)).Build()
	r := &DPFOperatorConfigReconciler{UncachedClient: &raceyGetClient{Client: base, metas: []metav1.PartialObjectMetadata{
		{
			TypeMeta:   metav1.TypeMeta{APIVersion: "example.dpu.nvidia.com/v1alpha1", Kind: "Widget"},
			ObjectMeta: metav1.ObjectMeta{Name: "ghost", Namespace: "dpf-operator-system", Generation: 1},
		},
	}}}

	findings, err := r.deprecatedFieldsForGVK(context.Background(), widgetGVKDep)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(findings).To(BeEmpty())
}

func TestDeprecatedFieldsUsageMessage_GroupsByKindWithCountsAndFieldsPerObject(t *testing.T) {
	g := NewWithT(t)

	findings := []*deprecatedFieldFinding{
		{kind: "DPFOperatorConfig", objectRef: "dpf-operator-system/dpfoperatorconfig", fieldPaths: []string{"spec.nvipam.image", "spec.multus.image"}},
		{kind: "DPUDevice", objectRef: "dpf-operator-system/legacy", fieldPaths: []string{"spec.opn"}},
		{kind: "DPUDevice", objectRef: "dpf-operator-system/other", fieldPaths: []string{"spec.psid"}},
	}

	msg := deprecatedFieldsUsageMessage(findings)
	g.Expect(msg).To(Equal("CRs are still using deprecated fields:\n" +
		"DPFOperatorConfig (1):\n" +
		"* dpf-operator-system/dpfoperatorconfig: spec.multus.image, spec.nvipam.image\n" +
		"DPUDevice (2):\n" +
		"* dpf-operator-system/legacy: spec.opn\n" +
		"* dpf-operator-system/other: spec.psid"))
}

func TestDeprecatedFieldsUsageMessage_CapsPerKindIndependently(t *testing.T) {
	g := NewWithT(t)

	var findings []*deprecatedFieldFinding
	for i := 0; i < maxDeprecatedFieldsFindingsPerKind+1; i++ {
		findings = append(findings, &deprecatedFieldFinding{
			kind:       "DPU",
			objectRef:  fmt.Sprintf("dpf-operator-system/obj-%03d", i),
			fieldPaths: []string{"spec.bmcIP"},
		})
	}
	// A second, small Kind must not be truncated or crowded out by DPU's cap.
	findings = append(findings, &deprecatedFieldFinding{
		kind:       "DPFOperatorConfig",
		objectRef:  "dpf-operator-system/dpfoperatorconfig",
		fieldPaths: []string{"spec.multus.image"},
	})

	msg := deprecatedFieldsUsageMessage(findings)
	g.Expect(msg).To(ContainSubstring(fmt.Sprintf("DPU (%d):", maxDeprecatedFieldsFindingsPerKind+1)))
	g.Expect(msg).To(ContainSubstring(fmt.Sprintf("obj-%03d", 0)))
	g.Expect(msg).To(ContainSubstring(fmt.Sprintf("obj-%03d", maxDeprecatedFieldsFindingsPerKind-1)))
	g.Expect(msg).NotTo(ContainSubstring(fmt.Sprintf("obj-%03d", maxDeprecatedFieldsFindingsPerKind)))
	g.Expect(msg).To(ContainSubstring("* ... and 1 more"))
	g.Expect(msg).To(ContainSubstring("DPFOperatorConfig (1):\n* dpf-operator-system/dpfoperatorconfig: spec.multus.image"))
}

func TestDeprecatedFieldsUsageMessage_BoundsTotalLength(t *testing.T) {
	g := NewWithT(t)

	// Max-length namespaces and names with many field paths: without a global bound, the per-Kind
	// cap alone would produce a message far beyond the condition message's schema limit.
	longNamespace := strings.Repeat("n", 63)
	var fieldPaths []string
	for i := 0; i < 10; i++ {
		fieldPaths = append(fieldPaths, fmt.Sprintf("spec.some.deeply.nested.deprecated.field%02d", i))
	}
	kinds := []string{"DPFOperatorConfig", "DPU", "DPUDeployment", "DPUDevice", "DPUNode", "DPUServiceChain", "DPUServiceIPAM", "DPUServiceInterface", "DPUSet"}
	var findings []*deprecatedFieldFinding
	for _, kind := range kinds {
		for i := 0; i < 50; i++ {
			findings = append(findings, &deprecatedFieldFinding{
				kind:       kind,
				objectRef:  fmt.Sprintf("%s/%s-%03d", longNamespace, strings.Repeat("o", 249), i),
				fieldPaths: fieldPaths,
			})
		}
	}

	msg := deprecatedFieldsUsageMessage(findings)
	g.Expect(len(msg)).To(BeNumerically("<=", 32768))
	// Every Kind heading with its total count survives truncation.
	for _, kind := range kinds {
		g.Expect(msg).To(ContainSubstring(fmt.Sprintf("\n%s (50):", kind)))
	}
	// The last Kind has no budget left for any object, but still reports how many were omitted.
	g.Expect(msg).To(HaveSuffix("\nDPUSet (50):\n* ... and 50 more"))
}

// erroringListClient wraps a client.Client and fails List calls for objects whose list-type GVK
// Kind matches errOnKind, succeeding for everything else.
type erroringListClient struct {
	client.Client
	errOnKind string
}

func (c *erroringListClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	if pl, ok := list.(*metav1.PartialObjectMetadataList); ok && pl.GetObjectKind().GroupVersionKind().Kind == c.errOnKind {
		return fmt.Errorf("simulated list error for %s", c.errOnKind)
	}
	if list.GetObjectKind().GroupVersionKind().Kind == c.errOnKind {
		return fmt.Errorf("simulated list error for %s", c.errOnKind)
	}
	return c.Client.List(ctx, list, opts...)
}

// erroringGetClient wraps a client.Client and fails Get calls for objects whose Kind matches
// errOnKind, succeeding for everything else (including metadata Lists, so the scan reaches the Get
// step).
type erroringGetClient struct {
	client.Client
	errOnKind string
}

func (c *erroringGetClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if obj.GetObjectKind().GroupVersionKind().Kind == c.errOnKind {
		return fmt.Errorf("simulated get error for %s", c.errOnKind)
	}
	return c.Client.Get(ctx, key, obj, opts...)
}

// raceyGetClient serves a fixed, caller-supplied metadata list (simulating objects that may no
// longer exist by the time a full Get is attempted) while delegating Get to the wrapped client.
type raceyGetClient struct {
	client.Client
	metas []metav1.PartialObjectMetadata
}

func (c *raceyGetClient) List(_ context.Context, list client.ObjectList, _ ...client.ListOption) error {
	pl, ok := list.(*metav1.PartialObjectMetadataList)
	if !ok {
		return fmt.Errorf("raceyGetClient only supports PartialObjectMetadataList, got %T", list)
	}
	pl.Items = append([]metav1.PartialObjectMetadata(nil), c.metas...)
	return nil
}

// listCountingClient wraps a client.Client, counting List calls to assert the scan-interval
// throttle actually skips work rather than merely leaving the condition unchanged by coincidence.
type listCountingClient struct {
	client.Client
	calls int
}

func (c *listCountingClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	c.calls++
	return c.Client.List(ctx, list, opts...)
}

// getCountingClient wraps a client.Client, counting Get calls to assert the metadata-generation
// cache actually skips full reads for unchanged objects rather than merely returning the same
// answer by coincidence.
type getCountingClient struct {
	client.Client
	calls int
}

func (c *getCountingClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	c.calls++
	return c.Client.Get(ctx, key, obj, opts...)
}
