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
	"sort"
	"strings"
	"time"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/conditions"
	"github.com/nvidia/doca-platform/pkg/deprecation"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// deprecatedFieldsScanInterval throttles the scan below, since this controller watches DPUs
// directly and would otherwise re-scan on every routine DPU status update. The condition is
// informational (excluded from Ready), so the resulting staleness is acceptable; unwatched CR
// kinds (e.g. DPUSet, DPUDevice) only refresh on the manager's normal resync cadence regardless.
const deprecatedFieldsScanInterval = 5 * time.Minute

// deprecatedFieldsCacheEntry is the last known result for one object, valid for the UID and
// generation it was computed from, so an unchanged object is never re-read. The UID is needed
// because a deleted and recreated object restarts at generation 1 under the same name.
type deprecatedFieldsCacheEntry struct {
	uid        types.UID
	generation int64
	fieldPaths []string
}

// reconcileDeprecatedFieldsUsage scans DPF custom resources for deprecated fields that are
// currently set and reports the result on DeprecatedFieldsNotInUseCondition.
func (r *DPFOperatorConfigReconciler) reconcileDeprecatedFieldsUsage(ctx context.Context, config *operatorv1.DPFOperatorConfig) {
	if time.Since(r.lastDeprecatedFieldsScan) < deprecatedFieldsScanInterval {
		return
	}
	r.lastDeprecatedFieldsScan = time.Now()

	log := ctrllog.FromContext(ctx)

	findingsByObject := map[deprecatedFieldFindingKey]*deprecatedFieldFinding{}
	var scanErrs []error
	for _, gvkDep := range r.KnownDeprecations {
		findings, err := r.deprecatedFieldsForGVK(ctx, gvkDep)
		if err != nil {
			scanErrs = append(scanErrs, fmt.Errorf("scanning %s: %w", gvkDep.GVK.String(), err))
			continue
		}
		for _, finding := range findings {
			findingsByObject[deprecatedFieldFindingKey{kind: finding.kind, objectRef: finding.objectRef}] = finding
		}
	}

	// This condition is informational and excluded from the Ready summary, so scan failures or
	// deprecated usage found here never block Ready or upgrades.
	if len(scanErrs) > 0 {
		log.Error(kerrors.NewAggregate(scanErrs), "Deprecated fields scan: failed to scan one or more resource kinds")
		conditions.AddUnknown(config, operatorv1.DeprecatedFieldsNotInUseCondition, operatorv1.ReasonInspectionFailed, conditions.ConditionMessage(kerrors.NewAggregate(scanErrs).Error()))
		return
	}

	if len(findingsByObject) == 0 {
		conditions.AddTrue(config, operatorv1.DeprecatedFieldsNotInUseCondition)
		return
	}

	findings := make([]*deprecatedFieldFinding, 0, len(findingsByObject))
	for _, finding := range findingsByObject {
		findings = append(findings, finding)
	}

	conditions.AddFalse(config, operatorv1.DeprecatedFieldsNotInUseCondition, operatorv1.ReasonDeprecatedFieldsInUse,
		conditions.ConditionMessage(deprecatedFieldsUsageMessage(findings)))
}

// deprecatedFieldsBulkReadThreshold is the number of changed objects above which they are read with
// one paginated List instead of one Get each, e.g. on the first scan after an operator restart when
// the cache is empty.
const deprecatedFieldsBulkReadThreshold = 10

// deprecatedFieldsForGVK scans all live objects of gvkDep.GVK for deprecated field usage, returning
// one finding per object that has at least one deprecated field set.
func (r *DPFOperatorConfigReconciler) deprecatedFieldsForGVK(ctx context.Context, gvkDep deprecation.GVKDeprecations) ([]*deprecatedFieldFinding, error) {
	// 1. Do a cheap metadata-only list.
	metaList := &metav1.PartialObjectMetadataList{}
	metaList.SetGroupVersionKind(gvkDep.GVK.GroupVersion().WithKind(gvkDep.ListKind))
	metas, err := listObjectsFromAPIReader[*metav1.PartialObjectMetadata](ctx, r.UncachedClient, metaList)
	if err != nil {
		return nil, fmt.Errorf("listing metadata: %w", err)
	}

	// 2. Diff the list against the cache, skip unchanged objects.
	oldCache := r.deprecatedFieldsCache[gvkDep.GVK]
	var changed []types.NamespacedName
	for _, meta := range metas {
		key := types.NamespacedName{Namespace: meta.GetNamespace(), Name: meta.GetName()}
		entry, ok := oldCache[key]
		if !ok || entry.uid != meta.GetUID() || entry.generation != meta.GetGeneration() {
			changed = append(changed, key)
		}
	}

	// 3. Fetch the new or changed objects in full and find deprecated fields.
	fieldPathsByKey, err := r.readDeprecatedFieldPaths(ctx, gvkDep, changed)
	if err != nil {
		return nil, err
	}

	// 4. Prepare the cache value for the GVK and add findings for each object.
	newCache := make(map[types.NamespacedName]deprecatedFieldsCacheEntry, len(metas))
	var findings []*deprecatedFieldFinding
	for _, meta := range metas {
		key := types.NamespacedName{Namespace: meta.GetNamespace(), Name: meta.GetName()}
		entry, ok := oldCache[key]
		if !ok || entry.uid != meta.GetUID() || entry.generation != meta.GetGeneration() {
			fieldPaths, found := fieldPathsByKey[key]
			if !found {
				// Deleted between the metadata list and the read; simply drop it.
				continue
			}
			entry = deprecatedFieldsCacheEntry{uid: meta.GetUID(), generation: meta.GetGeneration(), fieldPaths: fieldPaths}
		}

		newCache[key] = entry
		if len(entry.fieldPaths) > 0 {
			findings = append(findings, &deprecatedFieldFinding{
				kind:       gvkDep.GVK.Kind,
				objectRef:  formatObjectRef(meta),
				fieldPaths: entry.fieldPaths,
			})
		}
	}

	if r.deprecatedFieldsCache == nil {
		r.deprecatedFieldsCache = map[schema.GroupVersionKind]map[types.NamespacedName]deprecatedFieldsCacheEntry{}
	}
	// 5. Update the cache.
	r.deprecatedFieldsCache[gvkDep.GVK] = newCache

	return findings, nil
}

// readDeprecatedFieldPaths reads the objects identified by keys in full and returns, per object, the
// JSON paths of every field gvkDep.Fields declares that is actually set on it. Objects that no longer
// exist are absent from the result. Many keys are read with one paginated List, few with a Get each.
func (r *DPFOperatorConfigReconciler) readDeprecatedFieldPaths(ctx context.Context, gvkDep deprecation.GVKDeprecations, keys []types.NamespacedName) (map[types.NamespacedName][]string, error) {
	result := make(map[types.NamespacedName][]string, len(keys))
	if len(keys) == 0 {
		return result, nil
	}

	if len(keys) > deprecatedFieldsBulkReadThreshold {
		wanted := make(map[types.NamespacedName]struct{}, len(keys))
		for _, key := range keys {
			wanted[key] = struct{}{}
		}

		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(gvkDep.GVK.GroupVersion().WithKind(gvkDep.ListKind))
		objs, err := listObjectsFromAPIReader[*unstructured.Unstructured](ctx, r.UncachedClient, list)
		if err != nil {
			return nil, fmt.Errorf("listing %s: %w", gvkDep.GVK.Kind, err)
		}
		for _, obj := range objs {
			key := types.NamespacedName{Namespace: obj.GetNamespace(), Name: obj.GetName()}
			if _, ok := wanted[key]; ok {
				result[key] = deprecatedFieldPathsSetIn(gvkDep, obj)
			}
		}
		return result, nil
	}

	for _, key := range keys {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(gvkDep.GVK)
		if err := r.UncachedClient.Get(ctx, key, obj); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return nil, fmt.Errorf("getting %s %s: %w", gvkDep.GVK.Kind, key, err)
		}
		result[key] = deprecatedFieldPathsSetIn(gvkDep, obj)
	}
	return result, nil
}

func deprecatedFieldPathsSetIn(gvkDep deprecation.GVKDeprecations, obj *unstructured.Unstructured) []string {
	var fieldPaths []string
	for _, field := range gvkDep.Fields {
		if field.IsSetIn(obj.Object) {
			fieldPaths = append(fieldPaths, field.JSONPath())
		}
	}
	return fieldPaths
}

// deprecatedFieldFinding groups every deprecated field path found set on a single object.
type deprecatedFieldFinding struct {
	kind string
	// objectRef is "<namespace>/<name>" (or just "<name>" for cluster-scoped objects), matching
	// formatObjectRef's convention.
	objectRef  string
	fieldPaths []string
}

// deprecatedFieldFindingKey identifies a finding's object, used to dedupe findings across scans.
type deprecatedFieldFindingKey struct {
	kind      string
	objectRef string
}

// maxDeprecatedFieldsFindingsPerKind caps how many objects are listed under each Kind's heading in
// the condition message. Independent of maxItemsToReportOnValidationMessage, which bounds
// unrelated condition messages.
const maxDeprecatedFieldsFindingsPerKind = 10

// maxDeprecatedFieldsMessageObjectLinesLength bounds the total length of the per-object bullets in
// the condition message. Object names and field paths are unbounded in practice, so without it the
// message could exceed the condition message's 32768 character schema limit and cause the whole
// status patch to be rejected. Kind headings and "... and N more" lines are always written; with a
// handful of Kinds they add well under the remaining headroom.
const maxDeprecatedFieldsMessageObjectLinesLength = 8192

// deprecatedFieldsUsageMessage formats findings grouped by Kind, one heading per Kind (with its
// total object count) followed by one bullet per object listing its deprecated field paths.
func deprecatedFieldsUsageMessage(findings []*deprecatedFieldFinding) string {
	findingsByKind := map[string][]*deprecatedFieldFinding{}
	for _, finding := range findings {
		findingsByKind[finding.kind] = append(findingsByKind[finding.kind], finding)
	}

	kinds := make([]string, 0, len(findingsByKind))
	for kind := range findingsByKind {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)

	var b strings.Builder
	b.WriteString("CRs are still using deprecated fields:")
	objectLinesBudget := maxDeprecatedFieldsMessageObjectLinesLength
	for _, kind := range kinds {
		group := findingsByKind[kind]
		sort.Slice(group, func(i, j int) bool { return group[i].objectRef < group[j].objectRef })

		fmt.Fprintf(&b, "\n%s (%d):", kind, len(group))

		// Cap per Kind so a single busy Kind does not starve the message's visibility into every
		// other Kind that also has deprecated usage.
		shown := group
		if len(shown) > maxDeprecatedFieldsFindingsPerKind {
			shown = shown[:maxDeprecatedFieldsFindingsPerKind]
		}
		listed := 0
		for _, finding := range shown {
			fieldPaths := make([]string, len(finding.fieldPaths))
			copy(fieldPaths, finding.fieldPaths)
			sort.Strings(fieldPaths)
			line := fmt.Sprintf("\n* %s: %s", finding.objectRef, strings.Join(fieldPaths, ", "))
			if len(line) > objectLinesBudget {
				// Stop listing for good once the budget is exhausted, so later (shorter) lines
				// don't sneak in and make the output depend on name lengths.
				objectLinesBudget = 0
				break
			}
			objectLinesBudget -= len(line)
			b.WriteString(line)
			listed++
		}
		if more := len(group) - listed; more > 0 {
			fmt.Fprintf(&b, "\n* ... and %d more", more)
		}
	}

	return b.String()
}
