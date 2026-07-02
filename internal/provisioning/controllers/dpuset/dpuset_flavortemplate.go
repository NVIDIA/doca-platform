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

package dpuset

import (
	"context"
	"fmt"
	"reflect"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuflavortemplate"

	"github.com/fluxcd/pkg/runtime/patch"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// isTemplateMode reports whether the DPUSet renders a DPUFlavorTemplate per DPU
// instead of referencing a static DPUFlavor.
func isTemplateMode(dpuSet *provisioningv1.DPUSet) bool {
	return dpuSet.Spec.DPUTemplate.Spec.DPUFlavorTemplate != ""
}

// isTemplateModeDPU reports whether the DPU was created from a DPUFlavorTemplate
// (it carries the template-name label stamped at creation).
func isTemplateModeDPU(dpu *provisioningv1.DPU) bool {
	return dpu.Labels[cutil.DPUFlavorTemplateNameLabel] != ""
}

// templateEval is the read-only result of evaluating an existing template-mode DPU
// against its DPUFlavorTemplate and DPUDevice.spec.values.
type templateEval struct {
	// disrupt is true when the DPU must be reprovisioned (delete + recreate).
	disrupt bool
	// renderErr is set when rendering/admission failed for an existing DPU. The DPU
	// is NOT disrupted; the failure is surfaced as a non-disruptive annotation.
	renderErr error
	// equalButStale is true when the render matches the existing generated DPUFlavor
	// but the DPU's hash labels are out of date and should be patched.
	equalButStale bool
	// live label values, valid only when equalButStale is true.
	liveTemplateHash string
	liveValuesHash   string
}

// inputHashes returns the template-body hash and the values hash that together identify
// the render inputs of a template-mode DPU. The two hashes are recorded as labels at
// creation and compared on every reconcile to detect input changes.
func inputHashes(spec provisioningv1.DPUFlavorTemplateSpec, values *runtime.RawExtension) (templateHash, valuesHash string, err error) {
	templateHash, err = dpuflavortemplate.Hash(spec)
	if err != nil {
		return "", "", fmt.Errorf("failed to hash DPUFlavorTemplate: %w", err)
	}
	valuesHash, err = dpuflavortemplate.ValuesHash(values)
	if err != nil {
		return "", "", fmt.Errorf("failed to hash DPUDevice values: %w", err)
	}
	return templateHash, valuesHash, nil
}

// renderGeneratedFlavor renders the given DPUFlavorTemplate against the DPUDevice values
// and returns the concrete generated DPUFlavor object (without ownerReference/finalizer).
// It is a pure function of its inputs: callers fetch the template/device and own all I/O.
func renderGeneratedFlavor(template *provisioningv1.DPUFlavorTemplate, dpuDevice *provisioningv1.DPUDevice,
	dpuSet *provisioningv1.DPUSet, generatedName string) (*provisioningv1.DPUFlavor, error) {
	flavor, err := dpuflavortemplate.Render(template.Spec, dpuDevice.Spec.Values)
	if err != nil {
		return nil, err
	}
	flavor.Name = generatedName
	flavor.Namespace = dpuSet.Namespace
	if flavor.Labels == nil {
		flavor.Labels = map[string]string{}
	}
	flavor.Labels[cutil.GeneratedByLabel] = cutil.GeneratedByDPUFlavorTemplate
	flavor.Labels[cutil.DPUFlavorTemplateNameLabel] = template.Name
	flavor.Labels[cutil.DPUSetNameLabel] = dpuSet.Name
	flavor.Labels[cutil.DPUSetNamespaceLabel] = dpuSet.Namespace
	return flavor, nil
}

// createTemplateModeDPU implements the flavor-first creation order for template mode:
//
//	render -> create generated DPUFlavor -> create DPU.
//
// The generated flavor is created bare; its ownerReference and protective finalizer are set by
// the DPU controller (adoptGeneratedFlavor) once the DPU reconciles, so the controller that
// releases the finalizer on deletion is also the one that adds it.
//
// On render/admission failure it creates the DPU with render-failed annotations and the
// expected generated flavor name, so the DPU controller surfaces the failure (Error /
// DPUFlavorRendered=False) rather than silently stalling.
func (r *DPUSetReconciler) createTemplateModeDPU(ctx context.Context, dpuSet *provisioningv1.DPUSet,
	dpuDevice *provisioningv1.DPUDevice, dpu *provisioningv1.DPU) error {
	logger := log.FromContext(ctx)
	generatedName := dpu.Name
	templateName := dpuSet.Spec.DPUTemplate.Spec.DPUFlavorTemplate

	template := &provisioningv1.DPUFlavorTemplate{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: dpuSet.Namespace, Name: templateName}, template); err != nil {
		return fmt.Errorf("failed to get DPUFlavorTemplate %s: %w", templateName, err)
	}

	// Record the inputs on the DPU before rendering: the generated flavor name, the
	// template name, and the input hashes. Stamping the hashes up front (even when the
	// render below fails) keeps a parked render-failed DPU from being re-rendered on
	// every reconcile; it is re-evaluated only once the user changes the template or
	// values, which changes the hashes. Render is a pure function of these inputs.
	templateHash, valuesHash, err := inputHashes(template.Spec, dpuDevice.Spec.Values)
	if err != nil {
		return fmt.Errorf("failed to hash render inputs for %s: %w", generatedName, err)
	}
	dpu.Spec.DPUFlavor = generatedName
	dpu.Labels[cutil.DPUFlavorTemplateNameLabel] = templateName
	dpu.Labels[cutil.DPUFlavorTemplateHashLabel] = templateHash
	dpu.Labels[cutil.DPUDeviceValuesHashLabel] = valuesHash

	// Render the flavor and create it, then create the DPU once at the end. On render or
	// admission failure we fall through with render-failed annotations so the DPU still surfaces
	// the failure (Error / DPUFlavorRendered=False) rather than silently stalling.
	flavor, err := renderGeneratedFlavor(template, dpuDevice, dpuSet, generatedName)
	if err != nil {
		logger.Error(err, "Failed to render DPUFlavorTemplate; creating DPU in render-failed state",
			"DPU", dpu.Name)
		setRenderFailedAnnotations(dpu, cutil.RenderFailedOnCreate, err.Error())
	} else if createErr := r.Create(ctx, flavor); createErr != nil && !apierrors.IsAlreadyExists(createErr) {
		// A deterministic, per-flavor admission rejection means this rendered flavor can never be
		// created as-is, so surface it like a render failure.
		//   - Invalid (422): CRD OpenAPI bounds / CEL, e.g. an MTU outside 1280..9216.
		//   - BadRequest (400): the DPUFlavor ValidateCreate webhook, e.g. duplicate portNumber or
		//     systemReservedResources exceeding dpuResources.
		if apierrors.IsInvalid(createErr) || apierrors.IsBadRequest(createErr) {
			logger.Error(createErr, "Generated DPUFlavor rejected by admission; creating DPU in render-failed state",
				"DPU", dpu.Name)
			setRenderFailedAnnotations(dpu, cutil.RenderFailedOnCreate, createErr.Error())
		} else {
			return fmt.Errorf("failed to create generated DPUFlavor %s: %w", generatedName, createErr)
		}
	}

	// Ownership (ownerReference + protective finalizer) of the generated flavor is established by
	// the DPU controller (adoptGeneratedFlavor) once the DPU reconciles, keeping a single
	// controller as the only writer of the flavor's ownerRef/finalizer.
	return r.Create(ctx, dpu)
}

// evalTemplateDPUs evaluates every DPU of a template-mode DPUSet once per reconcile and returns
// the results keyed by DPU name. Each evaluation does reads plus a render, and the result is
// consumed by reconcileTemplateDPUs, the strategy (computeDPUDrift via needDisruptDPU), and
// reconcileDPUOutdatedStatus (detectOutdated); precomputing it here means those three paths
// share one consistent decision instead of each re-rendering the same inputs. Returns an empty
// map for static-flavor DPUSets, whose drift checks ignore the templateEval.
//
// All DPUs in the set are evaluated, not only those already carrying the template label: a
// DPUSet can be switched from a static dpuFlavor to a dpuFlavorTemplate (the spec is not
// immutable), leaving pre-existing DPUs without the label. evalTemplateDPU treats that missing
// label as a template/mode swap (disrupt=true) so those DPUs migrate instead of being silently
// skipped here.
//
// Invariant: the returned map is the single source of truth for template-mode decisions in a
// reconcile. The caller's dpuMap is a read-only snapshot - reconcileTemplateDPUs patches DPU
// hash labels/annotations on the API server but does not refresh dpuMap, so those values may be
// stale afterwards. Consumers must decide from this map, never from dpuMap labels.
func (r *DPUSetReconciler) evalTemplateDPUs(ctx context.Context, dpuSet *provisioningv1.DPUSet, dpuMap map[string]provisioningv1.DPU) map[string]templateEval {
	evals := make(map[string]templateEval)
	if !isTemplateMode(dpuSet) {
		return evals
	}
	for _, dpu := range dpuMap {
		evals[dpu.Name] = r.evalTemplateDPU(ctx, *dpuSet, dpu)
	}
	return evals
}

// evalTemplateDPU renders the current template/values for an existing template-mode DPU
// and compares the result against the DPU's recorded labels and its generated DPUFlavor.
// It performs only reads; callers act on the returned decision. Prefer evalTemplateDPUs,
// which evaluates all DPUs once and shares the result across the reconcile.
func (r *DPUSetReconciler) evalTemplateDPU(ctx context.Context, dpuSet provisioningv1.DPUSet, dpu provisioningv1.DPU) templateEval {
	logger := log.FromContext(ctx)

	liveName := dpuSet.Spec.DPUTemplate.Spec.DPUFlavorTemplate
	recordedName := dpu.Labels[cutil.DPUFlavorTemplateNameLabel]
	// Mode/template swap is structural and always disruptive: the DPU's flavor reference,
	// finalizer, and labels cannot be transitioned in place.
	if liveName != recordedName {
		return templateEval{disrupt: true}
	}

	// Read/hash failures below deliberately return the zero templateEval (no disruption): a flaky
	// or transient read must not tear down a healthy DPU ("don't disrupt on uncertainty"). A
	// persistent failure (e.g. a deleted user-managed template) keeps the DPU at its last good
	// render and is visible via these logs.
	dpuDevice := &provisioningv1.DPUDevice{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: dpu.Namespace, Name: dpu.Spec.DPUDeviceName}, dpuDevice); err != nil {
		logger.Error(err, "Failed to get DPUDevice for template evaluation", "DPU", dpu.Name)
		return templateEval{}
	}

	template := &provisioningv1.DPUFlavorTemplate{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: dpuSet.Namespace, Name: liveName}, template); err != nil {
		logger.Error(err, "Failed to get DPUFlavorTemplate for evaluation", "template", liveName)
		return templateEval{}
	}

	liveHash, liveValuesHash, err := inputHashes(template.Spec, dpuDevice.Spec.Values)
	if err != nil {
		logger.Error(err, "Failed to hash render inputs for template evaluation", "DPU", dpu.Name)
		return templateEval{}
	}

	// Fast path: inputs unchanged since the DPU was provisioned, nothing to do.
	// We deliberately do NOT verify the generated DPUFlavor still exists here: it is
	// finalizer-protected and created before the DPU, and a Ready DPU whose already
	// consumed snapshot vanished must not be torn down.
	if liveHash == dpu.Labels[cutil.DPUFlavorTemplateHashLabel] &&
		liveValuesHash == dpu.Labels[cutil.DPUDeviceValuesHashLabel] {
		return templateEval{}
	}

	// Inputs changed: render the current template/values before deciding anything.
	// Render is evaluated BEFORE the existence check so a render failure is surfaced
	// as a non-disruptive annotation rather than misread as a missing flavor, which
	// would tear the DPU down and loop on every reconcile.
	rendered, err := dpuflavortemplate.Render(template.Spec, dpuDevice.Spec.Values)
	if err != nil {
		return templateEval{renderErr: err, liveTemplateHash: liveHash, liveValuesHash: liveValuesHash}
	}

	// Render succeeded: only now is a missing generated flavor a genuine drift that
	// warrants reprovisioning (delete + recreate), per the REL1 flavor lifecycle.
	generated := &provisioningv1.DPUFlavor{}
	flavorErr := r.Get(ctx, types.NamespacedName{Namespace: dpu.Namespace, Name: dpu.Spec.DPUFlavor}, generated)
	if apierrors.IsNotFound(flavorErr) {
		return templateEval{disrupt: true}
	}
	if flavorErr != nil {
		// Transient read error: don't disrupt on uncertainty.
		logger.Error(flavorErr, "Failed to get generated DPUFlavor", "DPU", dpu.Name)
		return templateEval{}
	}

	if reflect.DeepEqual(rendered.Spec, generated.Spec) {
		return templateEval{equalButStale: true, liveTemplateHash: liveHash, liveValuesHash: liveValuesHash}
	}
	return templateEval{disrupt: true}
}

// reconcileTemplateDPUs handles the non-disruptive side effects of template evaluation for
// existing template-mode DPUs: patching stale hash labels when the render is unchanged, and
// recording render-failure annotations when an update render fails. Disruptive divergence is
// left to the strategy (rolloutRolling / onDelete) via computeDPUDrift.
//
// The label/annotation patches here are persisted for the NEXT reconcile; they intentionally do
// not refresh the caller's dpuMap snapshot. In-reconcile decisions use templateEvals, never the
// (now possibly stale) dpuMap labels - see the invariant note in Handle.
func (r *DPUSetReconciler) reconcileTemplateDPUs(ctx context.Context, dpuSet *provisioningv1.DPUSet, dpuMap map[string]provisioningv1.DPU, templateEvals map[string]templateEval) error {
	if !isTemplateMode(dpuSet) {
		return nil
	}
	logger := log.FromContext(ctx)
	for _, dpu := range dpuMap {
		if !dpu.DeletionTimestamp.IsZero() || !isTemplateModeDPU(&dpu) {
			continue
		}
		eval := templateEvals[dpu.Name]
		switch {
		case eval.renderErr != nil:
			// An update-time render failure is non-disruptive: the existing DPU and its generated
			// DPUFlavor are left untouched. It is recorded as an annotation here; the DPU controller
			// translates it into the DPUFlavorRendered condition in any phase (state-agnostic), so the
			// failure is visible even for a Ready DPU.
			logger.Error(eval.renderErr, "Failed to render DPUFlavorTemplate for existing DPU; recording render-failed annotation",
				"DPU", fmt.Sprintf("%s/%s", dpu.Namespace, dpu.Name))
			if err := r.patchDPURenderFailed(ctx, &dpu, cutil.RenderFailedOnUpdate, eval.renderErr.Error()); err != nil {
				return err
			}
		case eval.equalButStale:
			if err := r.patchDPUTemplateLabels(ctx, &dpu, eval.liveTemplateHash, eval.liveValuesHash); err != nil {
				return err
			}
		}
	}
	return nil
}

// patchDPUTemplateLabels updates the template/values hash labels on a DPU and clears any
// stale render-failed annotation.
func (r *DPUSetReconciler) patchDPUTemplateLabels(ctx context.Context, dpu *provisioningv1.DPU, templateHash, valuesHash string) error {
	patcher := patch.NewSerialPatcher(dpu, r.Client)
	if dpu.Labels == nil {
		dpu.Labels = map[string]string{}
	}
	dpu.Labels[cutil.DPUFlavorTemplateHashLabel] = templateHash
	dpu.Labels[cutil.DPUDeviceValuesHashLabel] = valuesHash
	clearRenderFailedAnnotations(dpu)
	return patcher.Patch(ctx, dpu)
}

// patchDPURenderFailed records a render-failure annotation on a DPU.
func (r *DPUSetReconciler) patchDPURenderFailed(ctx context.Context, dpu *provisioningv1.DPU, reason, message string) error {
	if dpu.Annotations[cutil.RenderFailedReasonAnnotation] == reason &&
		dpu.Annotations[cutil.RenderFailedMessageAnnotation] == message {
		return nil
	}
	patcher := patch.NewSerialPatcher(dpu, r.Client)
	setRenderFailedAnnotations(dpu, reason, message)
	return patcher.Patch(ctx, dpu)
}

// setRenderFailedAnnotations stamps the render-failure reason/message annotations.
func setRenderFailedAnnotations(dpu *provisioningv1.DPU, reason, message string) {
	if dpu.Annotations == nil {
		dpu.Annotations = map[string]string{}
	}
	dpu.Annotations[cutil.RenderFailedReasonAnnotation] = reason
	dpu.Annotations[cutil.RenderFailedMessageAnnotation] = message
}

// clearRenderFailedAnnotations removes the render-failure annotations if present.
func clearRenderFailedAnnotations(dpu *provisioningv1.DPU) {
	delete(dpu.Annotations, cutil.RenderFailedReasonAnnotation)
	delete(dpu.Annotations, cutil.RenderFailedMessageAnnotation)
}

// templateToDPUSetReq maps a DPUFlavorTemplate change to the DPUSets that reference it.
func (r *DPUSetReconciler) templateToDPUSetReq(ctx context.Context, resource client.Object) []reconcile.Request {
	template := resource.(*provisioningv1.DPUFlavorTemplate)
	dpuSetList := &provisioningv1.DPUSetList{}
	// A DPUSet only references a DPUFlavorTemplate in its own namespace, so scope the
	// List to the template's namespace instead of paying for a cluster-wide list.
	if r.List(ctx, dpuSetList, client.InNamespace(template.Namespace)) != nil {
		return nil
	}
	requests := []reconcile.Request{}
	for _, item := range dpuSetList.Items {
		if item.Spec.DPUTemplate.Spec.DPUFlavorTemplate != template.Name {
			continue
		}
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: item.Name, Namespace: item.Namespace},
		})
	}
	return requests
}
