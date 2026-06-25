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
	"strconv"
	"testing"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"

	. "github.com/onsi/gomega"
	apiextensionsinternal "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiservervalidation "k8s.io/apiextensions-apiserver/pkg/apiserver/validation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// testSchema builds a simple v1 CRD validation schema with required fields at spec level.
func testSchema(requiredFields ...string) *apiextensionsv1.CustomResourceValidation {
	specProps := apiextensionsv1.JSONSchemaProps{
		Type:     "object",
		Required: requiredFields,
	}
	return &apiextensionsv1.CustomResourceValidation{
		OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
			Type: "object",
			Properties: map[string]apiextensionsv1.JSONSchemaProps{
				"spec": specProps,
			},
		},
	}
}

// buildValidator converts a v1 schema to an internal validator, panicking on error (test helper only).
func buildValidator(t *testing.T, v1Schema *apiextensionsv1.CustomResourceValidation) apiservervalidation.SchemaCreateValidator {
	t.Helper()
	internalValidation := &apiextensionsinternal.CustomResourceValidation{}
	if err := apiextensionsv1.Convert_v1_CustomResourceValidation_To_apiextensions_CustomResourceValidation(
		v1Schema, internalValidation, nil); err != nil {
		t.Fatalf("converting schema: %v", err)
	}
	validator, _, err := apiservervalidation.NewSchemaValidator(internalValidation.OpenAPIV3Schema)
	if err != nil {
		t.Fatalf("building validator: %v", err)
	}
	return validator
}

func TestValidateObjectSchemas(t *testing.T) {
	g := NewWithT(t)

	gvk := schema.GroupVersionKind{Group: "svc.dpu.nvidia.com", Version: "v1alpha1", Kind: "DPUDeployment"}

	scheme := runtime.NewScheme()
	utilruntime.Must(apiextensionsv1.AddToScheme(scheme))
	scheme.AddKnownTypeWithName(gvk, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(gvk.GroupVersion().WithKind("DPUDeploymentList"), &unstructured.UnstructuredList{})

	crd := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "dpudeployments.svc.dpu.nvidia.com",
			Labels: map[string]string{operatorv1.DPFComponentLabelKey: "dpf-operator-controller-manager"},
		},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "svc.dpu.nvidia.com",
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Kind:     "DPUDeployment",
				ListKind: "DPUDeploymentList",
			},
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{
				Name:    "v1alpha1",
				Storage: true,
				Schema:  testSchema("dpuSetStrategy", "nodeEffect"),
			}},
		},
	}

	invalid := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "svc.dpu.nvidia.com/v1alpha1",
		"kind":       "DPUDeployment",
		"metadata":   map[string]interface{}{"name": "invalid", "namespace": "target"},
		"spec":       map[string]interface{}{},
	}}
	valid := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "svc.dpu.nvidia.com/v1alpha1",
		"kind":       "DPUDeployment",
		"metadata":   map[string]interface{}{"name": "valid", "namespace": "target"},
		"spec":       map[string]interface{}{"dpuSetStrategy": "x", "nodeEffect": "y"},
	}}

	r := &DPFOperatorConfigReconciler{
		UncachedClient: fake.NewClientBuilder().WithScheme(scheme).WithObjects(crd, invalid, valid).Build(),
	}

	err := r.validateObjectSchemas(context.Background(), nil, nil)
	g.Expect(err).To(HaveOccurred())
	msg := err.Error()
	g.Expect(msg).To(ContainSubstring("svc.dpu.nvidia.com/v1alpha1, Kind=DPUDeployment:"))
	g.Expect(msg).To(ContainSubstring("target/invalid has schema validation errors: [spec.dpuSetStrategy: Required value; spec.nodeEffect: Required value]"))
	g.Expect(msg).NotTo(ContainSubstring("target/valid"))
}

func TestValidateObjectSchemasSkipsNonDPFGroups(t *testing.T) {
	g := NewWithT(t)

	scheme := runtime.NewScheme()
	utilruntime.Must(apiextensionsv1.AddToScheme(scheme))

	// CRD with a non-DPF group — should be silently skipped even if objects would fail.
	crd := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "foos.example.com"},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "example.com",
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Kind:     "Foo",
				ListKind: "FooList",
			},
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{
				Name:    "v1",
				Storage: true,
				Schema:  testSchema("required"),
			}},
		},
	}

	r := &DPFOperatorConfigReconciler{
		UncachedClient: fake.NewClientBuilder().WithScheme(scheme).WithObjects(crd).Build(),
	}

	err := r.validateObjectSchemas(context.Background(), nil, nil)
	g.Expect(err).NotTo(HaveOccurred())
}

func TestValidateObjectSchemasLimitsObjectsPerMessage(t *testing.T) {
	g := NewWithT(t)

	gvk := schema.GroupVersionKind{Group: "svc.dpu.nvidia.com", Version: "v1alpha1", Kind: "DPUDeployment"}
	validator := buildValidator(t, testSchema("required"))

	objects := make([]unstructured.Unstructured, maxItemsToReportOnValidationMessage+1)
	for i := range objects {
		objects[i] = unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "svc.dpu.nvidia.com/v1alpha1",
			"kind":       "DPUDeployment",
			"metadata":   map[string]interface{}{"name": fmt.Sprintf("obj-%02d", i), "namespace": "target"},
			"spec":       map[string]interface{}{},
		}}
	}

	r := &DPFOperatorConfigReconciler{
		UncachedClient: &paginatedValidationClient{items: objects},
	}

	msgs, err := r.listAndValidateObjects(context.Background(), gvk, "DPUDeploymentList", validator)
	g.Expect(err).NotTo(HaveOccurred())

	formatted := objectSchemaValidationMessage(gvk, msgs)
	g.Expect(formatted).To(ContainSubstring("target/obj-00"))
	g.Expect(formatted).To(ContainSubstring("target/obj-04"))
	g.Expect(formatted).NotTo(ContainSubstring("target/obj-05"))
	g.Expect(formatted).To(ContainSubstring("and 1 more"))
}

func TestValidateObjectSchemasUsesPaginatedLists(t *testing.T) {
	g := NewWithT(t)

	gvk := schema.GroupVersionKind{Group: "svc.dpu.nvidia.com", Version: "v1alpha1", Kind: "DPUDeployment"}
	validator := buildValidator(t, testSchema("required"))

	// All objects valid except the last one, which is on page 2.
	objects := make([]unstructured.Unstructured, int(objectSchemaValidationListPageSize)+1)
	for i := range objects {
		spec := map[string]interface{}{"required": "ok"}
		if i == int(objectSchemaValidationListPageSize) {
			spec = map[string]interface{}{}
		}
		objects[i] = unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "svc.dpu.nvidia.com/v1alpha1",
			"kind":       "DPUDeployment",
			"metadata":   map[string]interface{}{"name": "obj-" + strconv.Itoa(i), "namespace": "target"},
			"spec":       spec,
		}}
	}

	pc := &paginatedValidationClient{items: objects}
	r := &DPFOperatorConfigReconciler{UncachedClient: pc}

	msgs, err := r.listAndValidateObjects(context.Background(), gvk, "DPUDeploymentList", validator)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(msgs).To(HaveLen(1))
	g.Expect(msgs[0].Error()).To(ContainSubstring("obj-500"))

	g.Expect(pc.calls).To(HaveLen(2))
	g.Expect(pc.calls[0].Limit).To(Equal(objectSchemaValidationListPageSize))
	g.Expect(pc.calls[0].Continue).To(BeEmpty())
	g.Expect(pc.calls[1].Limit).To(Equal(objectSchemaValidationListPageSize))
	g.Expect(pc.calls[1].Continue).To(Equal(strconv.FormatInt(objectSchemaValidationListPageSize, 10)))
}

// paginatedValidationClient is a minimal client.Client that serves a fixed slice of unstructured
// objects in pages, recording each List call's options for assertion.
type paginatedValidationClient struct {
	client.Client
	items []unstructured.Unstructured
	calls []client.ListOptions
}

func (c *paginatedValidationClient) List(_ context.Context, list client.ObjectList, opts ...client.ListOption) error {
	lo := client.ListOptions{}
	lo.ApplyOptions(opts)
	c.calls = append(c.calls, lo)

	ul, ok := list.(*unstructured.UnstructuredList)
	if !ok {
		return fmt.Errorf("expected *unstructured.UnstructuredList, got %T", list)
	}

	start := 0
	if lo.Continue != "" {
		n, err := strconv.Atoi(lo.Continue)
		if err != nil {
			return err
		}
		start = n
	}

	end := len(c.items)
	if lo.Limit > 0 && start+int(lo.Limit) < end {
		end = start + int(lo.Limit)
	}

	ul.Items = append([]unstructured.Unstructured(nil), c.items[start:end]...)
	ul.SetContinue("")
	if end < len(c.items) {
		ul.SetContinue(strconv.Itoa(end))
	}
	return nil
}
