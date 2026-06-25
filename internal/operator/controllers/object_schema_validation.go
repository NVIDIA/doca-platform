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
	"slices"
	"sort"
	"strings"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/conditions"
	"github.com/nvidia/doca-platform/pkg/dpucluster"

	apiextensionsinternal "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiservervalidation "k8s.io/apiextensions-apiserver/pkg/apiserver/validation"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// objectSchemaValidationListPageSize is the page size for uncached list calls to the kube-apiserver.
	objectSchemaValidationListPageSize int64 = 500
	// objectSchemaValidationGroupSuffix is the API group suffix used to filter DPF CRDs.
	objectSchemaValidationGroupSuffix = ".dpu.nvidia.com"
)

// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list
// +kubebuilder:rbac:groups=noderesources.dpu.nvidia.com;operator.dpu.nvidia.com;provisioning.dpu.nvidia.com;storage.dpu.nvidia.com;svc.dpu.nvidia.com;vpc.dpu.nvidia.com,resources=*,verbs=list

func (r *DPFOperatorConfigReconciler) validateObjectSchemas(ctx context.Context, _ *operatorv1.DPFOperatorConfig, _ []*dpucluster.Config) error {
	crdList := &apiextensionsv1.CustomResourceDefinitionList{}
	crds, err := listObjectsFromAPIReader[*apiextensionsv1.CustomResourceDefinition](ctx, r.UncachedClient, crdList,
		client.MatchingLabels{operatorv1.DPFComponentLabelKey: "dpf-operator-controller-manager"},
	)
	if err != nil {
		return fmt.Errorf("listing CRDs: %w", err)
	}

	var errs []error
	for _, crd := range crds {
		if !strings.HasSuffix(crd.Spec.Group, objectSchemaValidationGroupSuffix) {
			continue
		}
		r.validateObjectSchemaForCRD(ctx, crd, &errs)
	}

	sort.Slice(errs, func(i, j int) bool { return errs[i].Error() < errs[j].Error() })
	return kerrors.NewAggregate(errs)
}

// validateObjectSchemaForCRD builds a schema validator from the CRD's OpenAPI schema, lists all
// objects of that type, and appends an error to errs for each object that has schema violations.
func (r *DPFOperatorConfigReconciler) validateObjectSchemaForCRD(ctx context.Context, crd *apiextensionsv1.CustomResourceDefinition, errs *[]error) {
	for _, version := range crd.Spec.Versions {
		if !version.Storage {
			continue
		}
		if version.Schema == nil || version.Schema.OpenAPIV3Schema == nil {
			continue
		}

		internalValidation := &apiextensionsinternal.CustomResourceValidation{}
		if err := apiextensionsv1.Convert_v1_CustomResourceValidation_To_apiextensions_CustomResourceValidation(
			version.Schema, internalValidation, nil); err != nil {
			*errs = append(*errs, fmt.Errorf("converting schema for %s/%s: %w", crd.Name, version.Name, err))
			continue
		}

		validator, _, err := apiservervalidation.NewSchemaValidator(internalValidation.OpenAPIV3Schema)
		if err != nil {
			*errs = append(*errs, fmt.Errorf("building validator for %s/%s: %w", crd.Name, version.Name, err))
			continue
		}

		gvk := schema.GroupVersionKind{
			Group:   crd.Spec.Group,
			Version: version.Name,
			Kind:    crd.Spec.Names.Kind,
		}

		objectValidationErrs, listErr := r.listAndValidateObjects(ctx, gvk, crd.Spec.Names.ListKind, validator)
		if listErr != nil {
			*errs = append(*errs, listErr)
			continue
		}
		if len(objectValidationErrs) > 0 {
			*errs = append(*errs, fmt.Errorf("%s", objectSchemaValidationMessage(gvk, objectValidationErrs)))
		}
	}
}

// listAndValidateObjects fetches all objects of the given GVK, validates each against the schema,
// and returns one error per object that has schema violations.
func (r *DPFOperatorConfigReconciler) listAndValidateObjects(ctx context.Context, gvk schema.GroupVersionKind, listKind string, validator apiservervalidation.SchemaCreateValidator) ([]error, error) {
	objList := &unstructured.UnstructuredList{}
	objList.SetGroupVersionKind(gvk.GroupVersion().WithKind(listKind))
	items, err := listObjectsFromAPIReader[*unstructured.Unstructured](ctx, r.UncachedClient, objList)
	if err != nil {
		return nil, fmt.Errorf("failed to list %s: %w", gvk.String(), err)
	}

	var objectErrs []error
	for _, item := range items {
		violations := apiservervalidation.ValidateCustomResource(nil, item.Object, validator)
		if len(violations) > 0 {
			sortedViolations := make([]string, 0, len(violations))
			for _, v := range violations {
				sortedViolations = append(sortedViolations, v.Error())
			}
			sort.Strings(sortedViolations)
			objectErrs = append(objectErrs, fmt.Errorf("%s has schema validation errors: [%s]",
				formatObjectRef(item), strings.Join(sortedViolations, "; ")))
		}
	}

	return objectErrs, nil
}

// listObjectsFromAPIReader pages through all results and returns the full slice.
// objectList is reused across pages — c.List replaces Items on each call.
// The continue token is read from objectList after each page and passed into the next.
func listObjectsFromAPIReader[T client.Object](ctx context.Context, c client.Reader, objectList client.ObjectList, opts ...client.ListOption) ([]T, error) {
	var objs []T

	for {
		la, err := apimeta.ListAccessor(objectList)
		if err != nil {
			return nil, fmt.Errorf("getting list accessor: %w", err)
		}

		listOpts := append([]client.ListOption{
			client.Continue(la.GetContinue()),
			client.Limit(objectSchemaValidationListPageSize),
		}, opts...)

		if err := c.List(ctx, objectList, listOpts...); err != nil {
			return nil, err
		}

		items, err := apimeta.ExtractList(objectList)
		if err != nil {
			return nil, fmt.Errorf("extracting list items: %w", err)
		}
		for _, item := range items {
			objs = append(objs, item.(T))
		}

		if la.GetContinue() == "" {
			break
		}
	}

	return objs, nil
}

func objectSchemaValidationMessage(gvk schema.GroupVersionKind, objectErrs []error) string {
	reportedObjectErrs := slices.Clone(objectErrs)
	sort.Slice(reportedObjectErrs, func(i, j int) bool { return reportedObjectErrs[i].Error() < reportedObjectErrs[j].Error() })

	// Limit the number of objects reported to prevent condition messages from growing unbounded.
	if len(reportedObjectErrs) > maxItemsToReportOnValidationMessage {
		reportedObjectErrs = reportedObjectErrs[:maxItemsToReportOnValidationMessage]
	}

	// Add a message for the number of objects that were not reported.
	if moreObjects := len(objectErrs) - len(reportedObjectErrs); moreObjects > 0 {
		reportedObjectErrs = append(reportedObjectErrs, fmt.Errorf("... and %d more", moreObjects))
	}

	joinedErr := conditions.JoinErrors(kerrors.NewAggregate(reportedObjectErrs), 3)
	if joinedErr == nil {
		return fmt.Sprintf("%s:", gvk.String())
	}
	return fmt.Sprintf("%s:\n%s", gvk.String(), joinedErr.Error())
}

func formatObjectRef(obj *unstructured.Unstructured) string {
	if obj.GetNamespace() == "" {
		return obj.GetName()
	}
	return fmt.Sprintf("%s/%s", obj.GetNamespace(), obj.GetName())
}
