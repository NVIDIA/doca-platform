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

package inventory

import (
	"context"
	_ "embed"
	"fmt"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	"github.com/nvidia/doca-platform/internal/operator/utils"
	"github.com/nvidia/doca-platform/internal/release"
	"github.com/nvidia/doca-platform/pkg/conditions"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ Component = &sfcControllerObjects{}

// sfcControllerObjects contains objects that are used to generate sfc controller manifests.
// sfcControllerObjects objects should be immutable after Parse()
type sfcControllerObjects struct {
	data []byte
	// fromDPUService contains information relevant to the DPUService we expect to exist in the manifests
	fromDPUService fromDPUService
	dpuServiceNADs []*unstructured.Unstructured
}

func newSFCControllerObjects(data []byte) *sfcControllerObjects {
	return &sfcControllerObjects{
		data:           data,
		fromDPUService: fromDPUService{name: operatorv1.SFCControllerName},
	}
}

func (p *sfcControllerObjects) Name() string {
	return operatorv1.SFCControllerName
}

// Parse parses the sfc controller data into the relevant fields of the struct and performs some basic validations.
func (p *sfcControllerObjects) Parse() (err error) {
	if p.data == nil {
		return fmt.Errorf("sfcControllerObjects.data can not be empty")
	}

	objs, err := utils.BytesToUnstructured(p.data)
	if err != nil {
		return fmt.Errorf("error while converting SFC controller manifests to objects: %w", err)
	} else if len(objs) == 0 {
		return fmt.Errorf("no objects found in SFC controller manifests")
	}

	for _, obj := range objs {
		switch obj.GetKind() {
		// Exclude Namespace and CustomResourceDefinition as the operator should not deploy these resources.
		case string(NamespaceKind), string(CustomResourceDefinitionKind):
			continue
		case string(dpuservicev1.DPUServiceNADKind):
			p.dpuServiceNADs = append(p.dpuServiceNADs, obj)
		case string(dpuservicev1.DPUServiceKind):
			if p.fromDPUService.dpuService != nil {
				return fmt.Errorf("manifests should contain exactly one DPUService, found more than 1")
			}
			p.fromDPUService.dpuService = obj
		default:
			return fmt.Errorf("unexpected type of object detected %v", obj.GetKind())
		}
	}

	return nil
}

// GenerateManifests applies edits and returns objects
func (p *sfcControllerObjects) GenerateManifests(vars Variables, options ...GenerateManifestOption) ([]client.Object, error) {
	if ok := vars.DisableSystemComponents[p.Name()]; ok {
		return []client.Object{}, nil
	}

	opts := &GenerateManifestOptions{}
	for _, option := range options {
		option.Apply(opts)
	}

	labelsToAdd := map[string]string{
		operatorv1.DPFComponentLabelKey: p.Name(),
		release.DPFVersionLabelKey:      release.DPFVersion(),
	}
	applySetID := ApplySetID(vars.Namespace, p)
	// Add the ApplySet to the manifests if this hasn't been disabled.
	if !opts.skipApplySet {
		labelsToAdd[applysetPartOfLabel] = applySetID
	}

	// make a copy of the objects
	nadsCopy := make([]*unstructured.Unstructured, 0, len(p.dpuServiceNADs))
	for i := range p.dpuServiceNADs {
		nadsCopy = append(nadsCopy, p.dpuServiceNADs[i].DeepCopy())
	}

	// apply edits
	if err := NewEdits().
		AddForAll(NamespaceEdit(vars.Namespace)).
		AddForAll(LabelsEdit(labelsToAdd)).
		AddForKind(dpuservicev1.DPUServiceNADKind, DPUServiceNADMTUEdit(vars.Networking.HighSpeedMTU)).
		Apply(nadsCopy); err != nil {
		return nil, err
	}

	dpuServiceCopy, err := p.fromDPUService.applyDPUServiceEdits(vars, labelsToAdd)
	if err != nil {
		return nil, fmt.Errorf("failed to apply DPUService edits: %w", err)
	}

	objs := make([]*unstructured.Unstructured, 0, len(nadsCopy)+1)
	objs = append(objs, nadsCopy...)
	objs = append(objs, dpuServiceCopy)

	// return as Objects
	ret := []client.Object{}
	if !opts.skipApplySet {
		ret = append(ret, applySetParentForComponent(p, applySetID, vars, applySetInventoryString(objs...)))
	}

	for i := range objs {
		ret = append(ret, objs[i])
	}

	return ret, nil
}

// IsReadyForUpgrade reports the readiness of the sfc controller objects. It returns an error when any of the resources is not
// ready.
func (p *sfcControllerObjects) IsReadyForUpgrade(ctx context.Context, c client.Client, config *operatorv1.DPFOperatorConfig) error {
	var errs []error
	if !utils.IsUpgradeFrom25Dot7(*config.Status.Version) {
		errs = append(errs, p.areDPUServiceNADsReady(ctx, c, config.GetNamespace(), false)...)
	}
	errs = append(errs, p.fromDPUService.isReady(ctx, c, config.GetNamespace(), false))
	return kerrors.NewAggregate(errs)
}

// areDPUServiceNADsReady checks whether the DPUServiceNADs for the sfc controller manifests are ready. Based on the
// versionValidation input passed, this function also checks if the NADs are matching the DPF version.
func (p *sfcControllerObjects) areDPUServiceNADsReady(ctx context.Context, c client.Client, namespace string, versionValidation bool) []error {
	var errs []error
	for _, obj := range p.dpuServiceNADs {
		gotDPUServiceNAD := &dpuservicev1.DPUServiceNAD{}
		key := client.ObjectKey{Name: obj.GetName(), Namespace: namespace}
		if err := c.Get(ctx, key, gotDPUServiceNAD); err != nil {
			errs = append(errs, fmt.Errorf("failed to get DPUServiceNAD %s: %w", key, err))
			continue
		}

		if versionValidation {
			if gotDPUServiceNAD.GetLabels()[release.DPFVersionLabelKey] != "" && gotDPUServiceNAD.GetLabels()[release.DPFVersionLabelKey] != release.DPFVersion() {
				errs = append(errs, fmt.Errorf("DPUServiceNAD %s/%s has version %s, want %s",
					gotDPUServiceNAD.GetNamespace(), gotDPUServiceNAD.GetName(), gotDPUServiceNAD.GetLabels()[release.DPFVersionLabelKey], release.DPFVersion()))
				continue
			}
		}

		//TODO: Add extra check for observedGeneration once observedGeneration is added at the DPUServiceNAD conditions

		if !conditions.IsTrue(gotDPUServiceNAD, conditions.TypeReady) {
			errs = append(errs, fmt.Errorf("SFC Controller related DPUServiceNAD %s is not ready", key))
			continue
		}
	}

	return errs
}

// IsReady reports the readiness of the sfc controller objects as well as the version state. It returns
// an error when any of the resources is not ready.
func (p *sfcControllerObjects) IsReady(ctx context.Context, c client.Client, namespace string) error {
	var errs []error
	errs = append(errs, p.areDPUServiceNADsReady(ctx, c, namespace, true)...)
	errs = append(errs, p.fromDPUService.isReady(ctx, c, namespace, true))
	return kerrors.NewAggregate(errs)
}

// DPUServiceNADMTUEdit sets the MTU for a given DPUServiceNAD
func DPUServiceNADMTUEdit(mtu int) UnstructuredEdit {
	return func(obj *unstructured.Unstructured) error {
		// do the conversion to ensure we're dealing with the correct type, but deal with unstructured for the patch.
		err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.UnstructuredContent(), &dpuservicev1.DPUServiceNAD{})
		if err != nil {
			return fmt.Errorf("error while converting object to DPUServiceNAD to objects: %w", err)
		}
		err = unstructured.SetNestedField(obj.UnstructuredContent(), int64(mtu), "spec", "serviceMTU")
		if err != nil {
			return fmt.Errorf("error while setting MTU to unstructured DPUServiceNAD: %w", err)
		}
		return nil
	}
}
