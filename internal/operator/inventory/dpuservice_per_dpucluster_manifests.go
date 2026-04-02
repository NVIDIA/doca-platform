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
	"maps"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/digest"
	operatorutils "github.com/nvidia/doca-platform/internal/operator/utils"
	"github.com/nvidia/doca-platform/internal/release"
	"github.com/nvidia/doca-platform/pkg/conditions"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ Component = &dpuServicePerDPUClusterObjects{}

// dpuServicePerDPUClusterObjects contains objects that are used to generate per dpucluster manifests.
// dpuServicePerDPUClusterObjects objects should be immutable after Parse()
type dpuServicePerDPUClusterObjects struct {
	data []byte
	// templateDPUService is the template DPUService that will be used to generate:
	// 1. A RBAC/CRDs DPUService (non-in-cluster) for deploying RBAC and CRDs
	// 2. Per-cluster DPUServices (in-cluster) for each DPUCluster
	templateDPUService fromDPUService
	// dpuServiceCredentialsRequest is the template credentials request for the DPUService that will be instantiated per DPUCluster.
	dpuServiceCredentialsRequest *unstructured.Unstructured
	// componentName is the name used for the component label
	componentName operatorv1.ComponentName
	// rbacAndCRDsName is the name for the RBAC/CRDs DPUService
	rbacAndCRDsName operatorv1.ComponentName
}

func newServiceChainSetControllerObjects(data []byte) *dpuServicePerDPUClusterObjects {
	return &dpuServicePerDPUClusterObjects{
		data:               data,
		templateDPUService: fromDPUService{name: operatorv1.ServiceSetControllerName},
		componentName:      operatorv1.ServiceSetControllerName,
		rbacAndCRDsName:    operatorv1.ServiceChainSetCRDsName,
	}
}

func newKubeStateMetricsObjects(data []byte) *dpuServicePerDPUClusterObjects {
	return &dpuServicePerDPUClusterObjects{
		data:               data,
		templateDPUService: fromDPUService{name: operatorv1.KubeStateMetricsName},
		componentName:      operatorv1.KubeStateMetricsName,
		rbacAndCRDsName:    operatorv1.KubeStateMetricsRBACName,
	}
}

func newNVIPAMObjects(data []byte) *dpuServicePerDPUClusterObjects {
	return &dpuServicePerDPUClusterObjects{
		data:               data,
		templateDPUService: fromDPUService{name: operatorv1.NVIPAMControllerName},
		componentName:      operatorv1.NVIPAMControllerName,
		rbacAndCRDsName:    operatorv1.NVIPAMNodeName,
	}
}

func (p *dpuServicePerDPUClusterObjects) Name() operatorv1.ComponentName {
	return p.componentName
}

// Parse parses the data into the relevant fields of the struct and performs some basic validations.
func (p *dpuServicePerDPUClusterObjects) Parse() (err error) {
	if p.data == nil {
		return fmt.Errorf("dpuServicePerDPUClusterObjects.data can not be empty")
	}

	objs, err := operatorutils.BytesToUnstructured(p.data)
	if err != nil {
		return fmt.Errorf("error while converting %s manifests to objects: %w", p.componentName.String(), err)
	} else if len(objs) == 0 {
		return fmt.Errorf("no objects found in %s manifests", p.componentName.String())
	}

	for _, obj := range objs {
		switch obj.GetKind() {
		// Exclude Namespace and CustomResourceDefinition as the operator should not deploy these resources.
		case string(NamespaceKind), string(CustomResourceDefinitionKind):
			continue
		case dpuservicev1.DPUServiceKind:
			if p.templateDPUService.dpuService != nil {
				return fmt.Errorf("manifests should contain exactly one DPUService, found more than 1")
			}
			p.templateDPUService.dpuService = obj
		case dpuservicev1.DPUServiceCredentialRequestKind:
			if p.dpuServiceCredentialsRequest != nil {
				return fmt.Errorf("manifests should contain exactly one DPUServiceCredentialsRequest, found more than 1")
			}
			p.dpuServiceCredentialsRequest = obj
		default:
			return fmt.Errorf("unexpected type of object detected %v", obj.GetKind())
		}
	}

	if p.templateDPUService.dpuService == nil {
		return fmt.Errorf("manifests must contain a DPUService")
	}

	if p.dpuServiceCredentialsRequest == nil {
		return fmt.Errorf("manifests must contain a DPUServiceCredentialsRequest")
	}

	return nil
}

// GenerateManifests applies edits and returns objects
func (p *dpuServicePerDPUClusterObjects) GenerateManifests(_ context.Context, vars Variables, options ...GenerateManifestOption) ([]client.Object, error) {
	if ok := vars.DisableSystemComponents[p.Name()]; ok {
		return []client.Object{}, nil
	}

	opts := &GenerateManifestOptions{}
	for _, option := range options {
		option.Apply(opts)
	}

	labelsToAdd := map[string]string{
		operatorv1.DPFComponentLabelKey: p.Name().String(),
		release.DPFVersionLabelKey:      release.DPFVersion(),
	}
	applySetID := ApplySetID(vars.Namespace, p)
	// Add the ApplySet to the manifests if this hasn't been disabled.
	if !opts.skipApplySet {
		labelsToAdd[applysetPartOfLabel] = applySetID
	}

	objs := make([]*unstructured.Unstructured, 0)

	// Build the list of service accounts for all DPUClusters that the RBACAndCRDs DPUService should grant access to.
	// This is in the form of what e.g. the ServiceChainSetController chart expects. These service accounts are created by
	// the DPUServiceCredentialRequest we create below for each DPUCluster.
	serviceAccounts := make([]types.NamespacedName, 0, len(vars.DPUClusters))

	// Generate an in-cluster DPUService and DPUServiceCredentialsRequest for each DPUCluster
	for _, cluster := range vars.DPUClusters {
		// Create copy the labels
		labelsToAddCopy := make(map[string]string)
		maps.Copy(labelsToAddCopy, labelsToAdd)
		labelsToAddCopy[provisioningv1.DPUClusterNameLabelKey] = cluster.Cluster.Name
		labelsToAddCopy[provisioningv1.DPUClusterNamespaceLabelKey] = cluster.Cluster.Namespace
		// We hash the DPUCluster name and namespace to keep the name short and within the 63 character limit
		hashedClusterNameNamespace := digest.Short(digest.FromObjects(cluster.Cluster.Name, cluster.Cluster.Namespace), 10)
		secretName := fmt.Sprintf("%s-credentials-%s", p.componentName.String(), hashedClusterNameNamespace)
		serviceAccountName := fmt.Sprintf("%s-%s", p.componentName.String(), hashedClusterNameNamespace)

		// Create a DPUService per cluster
		dpuServicePerClusterCopy, err := p.generatePerClusterDPUService(vars, labelsToAddCopy, hashedClusterNameNamespace, secretName, serviceAccountName)
		if err != nil {
			return nil, err
		}

		objs = append(objs, dpuServicePerClusterCopy)

		// Create a DPUServiceCredentialRequest per cluster
		clusterCredReqCopy, err := p.generatePerClusterDPUServiceCredentialRequest(vars.Namespace, labelsToAddCopy, hashedClusterNameNamespace, cluster.Cluster.Name, cluster.Cluster.Namespace, secretName, serviceAccountName)
		if err != nil {
			return nil, err
		}

		objs = append(objs, clusterCredReqCopy)

		// Add service account for this cluster to the list
		serviceAccounts = append(serviceAccounts, types.NamespacedName{
			Name:      serviceAccountName,
			Namespace: vars.Namespace,
		})
	}

	// Generate the RBAC and CRDs DPUService after all per-cluster services are created
	rbacAndCRDsDPUServiceCopy, err := p.generateRBACAndCRDsDPUService(vars, labelsToAdd, serviceAccounts)
	if err != nil {
		return nil, err
	}

	objs = append(objs, rbacAndCRDsDPUServiceCopy)

	// return as Objects
	ret := []client.Object{}
	for i := range objs {
		ret = append(ret, objs[i])
	}

	return ret, nil
}

// generateRBACAndCRDsDPUService generates the RBAC and CRDs DPUService with appropriate edits applied
func (p *dpuServicePerDPUClusterObjects) generateRBACAndCRDsDPUService(vars Variables, labelsToAdd map[string]string, serviceAccounts []types.NamespacedName) (*unstructured.Unstructured, error) {
	// Generate the RBAC and CRDs DPUService
	rbacAndCRDsDPUServiceCopy, err := p.templateDPUService.applyDPUServiceEdits(vars, labelsToAdd)
	if err != nil {
		return nil, fmt.Errorf("failed to apply RBAC and CRDs related DPUService edits: %w", err)
	}

	// Adjust the name of the DPUService to match the RBAC and CRDs DPUService name
	rbacAndCRDsDPUServiceCopy.SetName(p.rbacAndCRDsName.String())

	// Add additional values required by the RBAC and CRDs DPUService
	dpuServiceEdits := NewEdits()
	edits := rbacAndCRDEdits(p.componentName.String(), serviceAccounts)

	for _, edit := range edits {
		dpuServiceEdits.AddForKindS(DPUServiceKind, edit)
	}

	// Apply the edits.
	if err := dpuServiceEdits.Apply([]*unstructured.Unstructured{rbacAndCRDsDPUServiceCopy}); err != nil {
		return nil, err
	}

	return rbacAndCRDsDPUServiceCopy, nil
}

// generatePerClusterDPUService generates a per-cluster DPUService with appropriate edits applied
func (p *dpuServicePerDPUClusterObjects) generatePerClusterDPUService(vars Variables, labelsToAdd map[string]string, hashedClusterNameNamespace string, secretName string, serviceAccountName string) (*unstructured.Unstructured, error) {
	// Create a DPUService per cluster and apply the standard edits
	dpuServicePerClusterCopy, err := p.templateDPUService.applyDPUServiceEdits(vars, labelsToAdd)
	if err != nil {
		return nil, fmt.Errorf("failed to apply controller related DPUService edits: %w", err)
	}

	// Adjust the name of the DPUService to include the hashed DPUCluster name and namespace.
	dpuServicePerClusterCopy.SetName(fmt.Sprintf("%s-%s", dpuServicePerClusterCopy.GetName(), hashedClusterNameNamespace))

	saLabels := map[string]string{}
	if p.dpuServiceCredentialsRequest != nil {
		saLabels = map[string]string{
			dpuservicev1.CredentialRequestNameLabelKey:                fmt.Sprintf("%s-%s", p.dpuServiceCredentialsRequest.GetName(), hashedClusterNameNamespace),
			dpuservicev1.CredentialRequestNamespaceLabelKey:           vars.Namespace,
			dpuservicev1.DPUServiceCredentialRequestManagedByLabelKey: dpuservicev1.DPUServiceCredentialRequestManagedByLabelValue,
		}
	}

	dpuServiceEdits := NewEdits()
	edits := perClusterEdits(p.componentName.String(), secretName, serviceAccountName, saLabels)

	for _, edit := range edits {
		dpuServiceEdits.AddForKindS(DPUServiceKind, edit)
	}

	// Apply the edits.
	if err := dpuServiceEdits.Apply([]*unstructured.Unstructured{dpuServicePerClusterCopy}); err != nil {
		return nil, err
	}

	return dpuServicePerClusterCopy, nil
}

// generatePerClusterDPUServiceCredentialRequest generates a per-cluster DPUServiceCredentialRequest with appropriate edits applied
func (p *dpuServicePerDPUClusterObjects) generatePerClusterDPUServiceCredentialRequest(namespace string, labelsToAdd map[string]string, hashedClusterNameNamespace string, clusterName string, clusterNamespace string, secretName string, serviceAccountName string) (*unstructured.Unstructured, error) {
	// Create DPUServiceCredentialsRequest copy
	clusterCredReqCopy := p.dpuServiceCredentialsRequest.DeepCopy()
	originalCredReqName := clusterCredReqCopy.GetName()
	clusterCredReqCopy.SetName(fmt.Sprintf("%s-%s", originalCredReqName, hashedClusterNameNamespace))

	// Apply edits to DPUServiceCredentialsRequest
	if err := NewEdits().
		AddForAll(NamespaceEdit(namespace)).
		AddForAll(LabelsEdit(labelsToAdd)).
		AddForAll(dpuServiceCredentialsRequestSetServiceAccountEdit(serviceAccountName, namespace)).
		AddForAll(dpuServiceCredentialsRequestSetTargetClusterEdit(clusterName, clusterNamespace)).
		AddForAll(dpuServiceCredentialsRequestSetSecretEdit(secretName, namespace)).
		Apply([]*unstructured.Unstructured{clusterCredReqCopy}); err != nil {
		return nil, fmt.Errorf("failed to apply edits for cluster %s DPUServiceCredentialsRequest: %w", clusterName, err)
	}

	return clusterCredReqCopy, nil
}

// dpuServiceCredentialsRequestSetServiceAccountEdit sets the serviceAccount field in the DPUServiceCredentialsRequest spec
func dpuServiceCredentialsRequestSetServiceAccountEdit(name, namespace string) UnstructuredEdit {
	return func(obj *unstructured.Unstructured) error {
		// Validate we're dealing with the correct type
		if obj.GetKind() != dpuservicev1.DPUServiceCredentialRequestKind {
			return fmt.Errorf("unexpected object kind %s, expected DPUServiceCredentialRequest", obj.GetKind())
		}
		if err := unstructured.SetNestedField(obj.UnstructuredContent(), name, "spec", "serviceAccount", "name"); err != nil {
			return fmt.Errorf("error while setting serviceAccount name: %w", err)
		}
		if err := unstructured.SetNestedField(obj.UnstructuredContent(), namespace, "spec", "serviceAccount", "namespace"); err != nil {
			return fmt.Errorf("error while setting serviceAccount namespace: %w", err)
		}
		return nil
	}
}

// dpuServiceCredentialsRequestSetTargetClusterEdit sets the targetCluster field in the DPUServiceCredentialsRequest spec
func dpuServiceCredentialsRequestSetTargetClusterEdit(name, namespace string) UnstructuredEdit {
	return func(obj *unstructured.Unstructured) error {
		// Validate we're dealing with the correct type
		if obj.GetKind() != dpuservicev1.DPUServiceCredentialRequestKind {
			return fmt.Errorf("unexpected object kind %s, expected DPUServiceCredentialRequest", obj.GetKind())
		}
		if err := unstructured.SetNestedField(obj.UnstructuredContent(), name, "spec", "targetCluster", "name"); err != nil {
			return fmt.Errorf("error while setting targetCluster name: %w", err)
		}
		if err := unstructured.SetNestedField(obj.UnstructuredContent(), namespace, "spec", "targetCluster", "namespace"); err != nil {
			return fmt.Errorf("error while setting targetCluster namespace: %w", err)
		}
		return nil
	}
}

// dpuServiceCredentialsRequestSetSecretEdit sets the secret field in the DPUServiceCredentialsRequest spec
func dpuServiceCredentialsRequestSetSecretEdit(name, namespace string) UnstructuredEdit {
	return func(obj *unstructured.Unstructured) error {
		// Validate we're dealing with the correct type
		if obj.GetKind() != dpuservicev1.DPUServiceCredentialRequestKind {
			return fmt.Errorf("unexpected object kind %s, expected DPUServiceCredentialRequest", obj.GetKind())
		}
		if err := unstructured.SetNestedField(obj.UnstructuredContent(), name, "spec", "secret", "name"); err != nil {
			return fmt.Errorf("error while setting secret name: %w", err)
		}
		if err := unstructured.SetNestedField(obj.UnstructuredContent(), namespace, "spec", "secret", "namespace"); err != nil {
			return fmt.Errorf("error while setting secret namespace: %w", err)
		}
		return nil
	}
}

// areDPUServicesReady checks whether the DPUServices are ready. Based on the versionValidation input passed,
// this function also checks if the DPUServices are matching the DPF version.
// It also verifies that the expected count of DPUServices (1 RBAC/CRDs + N per-cluster) matches the actual count.
func (p *dpuServicePerDPUClusterObjects) areDPUServicesReady(ctx context.Context, c client.Client, namespace string, dpuClusterCount int, versionValidation bool) []error {
	var errs []error

	// List all DPUServices with our component label
	dpuServiceList := &dpuservicev1.DPUServiceList{}
	if err := c.List(ctx, dpuServiceList,
		client.InNamespace(namespace),
		client.MatchingLabels{operatorv1.DPFComponentLabelKey: p.Name().String()}); err != nil {
		errs = append(errs, fmt.Errorf("failed to list DPUServices: %w", err))
		return errs
	}

	// Verify we have the expected number of DPUServices (1 RBAC/CRDs + N per-cluster)
	expectedDPUServiceCount := 1 + dpuClusterCount
	if len(dpuServiceList.Items) != expectedDPUServiceCount {
		errs = append(errs, fmt.Errorf("expected %d DPUServices (1 RBAC/CRDs + %d per-cluster), found %d",
			expectedDPUServiceCount, dpuClusterCount, len(dpuServiceList.Items)))
	}

	// Check all DPUServices are ready and optionally validate version
	for _, dpuService := range dpuServiceList.Items {
		if versionValidation {
			if dpuService.GetLabels()[release.DPFVersionLabelKey] != "" && dpuService.GetLabels()[release.DPFVersionLabelKey] != release.DPFVersion() {
				errs = append(errs, fmt.Errorf("DPUService %s/%s has version %s, want %s",
					dpuService.GetNamespace(), dpuService.GetName(), dpuService.GetLabels()[release.DPFVersionLabelKey], release.DPFVersion()))
				continue
			}
		}

		if !conditions.IsTrue(&dpuService, conditions.TypeReady) {
			errs = append(errs, fmt.Errorf("%s related DPUService %s/%s is not ready", p.componentName.String(), namespace, dpuService.GetName()))
		}
	}

	return errs
}

// areDPUServiceCredentialRequestsReady checks whether the DPUServiceCredentialRequests for the DPUService are ready.
// Based on the versionValidation input passed, this function also checks if the DPUServiceCredentialRequests are matching the DPF version.
// It also verifies that the expected count of DPUServiceCredentialRequests (N per-cluster) matches the actual count.
func (p *dpuServicePerDPUClusterObjects) areDPUServiceCredentialRequestsReady(ctx context.Context, c client.Client, namespace string, expectedClusterCount int, versionValidation bool) []error {
	var errs []error

	// List all DPUServiceCredentialRequests with our component label
	dpuServiceCredentialRequestList := &dpuservicev1.DPUServiceCredentialRequestList{}
	if err := c.List(ctx, dpuServiceCredentialRequestList,
		client.InNamespace(namespace),
		client.MatchingLabels{operatorv1.DPFComponentLabelKey: p.Name().String()}); err != nil {
		errs = append(errs, fmt.Errorf("failed to list DPUServiceCredentialRequests: %w", err))
		return errs
	}

	// Verify we have the expected number of DPUServiceCredentialRequests
	if len(dpuServiceCredentialRequestList.Items) != expectedClusterCount {
		errs = append(errs, fmt.Errorf("expected %d DPUServiceCredentialRequests, found %d", expectedClusterCount, len(dpuServiceCredentialRequestList.Items)))
	}

	// Check all DPUServiceCredentialRequests are ready and optionally validate version
	for _, credReq := range dpuServiceCredentialRequestList.Items {
		if versionValidation {
			if credReq.GetLabels()[release.DPFVersionLabelKey] != "" && credReq.GetLabels()[release.DPFVersionLabelKey] != release.DPFVersion() {
				errs = append(errs, fmt.Errorf("DPUServiceCredentialRequest %s/%s has version %s, want %s",
					credReq.GetNamespace(), credReq.GetName(), credReq.GetLabels()[release.DPFVersionLabelKey], release.DPFVersion()))
				continue
			}
		}

		if !conditions.IsTrue(&credReq, conditions.TypeReady) {
			errs = append(errs, fmt.Errorf("%s related DPUServiceCredentialRequest %s/%s is not ready", p.componentName.String(), namespace, credReq.GetName()))
		}
	}

	return errs
}

// IsReadyForUpgrade reports the readiness of the objects. It returns an error when any of the resources is not
// ready.
func (p *dpuServicePerDPUClusterObjects) IsReadyForUpgrade(ctx context.Context, c client.Client, config *operatorv1.DPFOperatorConfig) error {
	shouldSkip, err := ShouldSkipUpgradeCheck(p.Name(), *config.Status.Version)
	if err != nil {
		return fmt.Errorf("determine if component %s should skip upgrade check: %w", p.Name(), err)
	}
	if shouldSkip {
		return nil
	}

	var errs []error

	// List DPUClusters to determine expected count
	dpuClusterList := &provisioningv1.DPUClusterList{}
	if err := c.List(ctx, dpuClusterList); err != nil {
		return fmt.Errorf("failed to list DPUClusters: %w", err)
	}
	dpuClusterCount := len(dpuClusterList.Items)

	isUpgradeFromLastReleasedGA := operatorutils.IsUpgradeFromLastReleasedGA(*config.Status.Version)

	// TODO: Remove after v26.4.0. This conditional exists because the ServiceChainSet controller
	// changed from a single DPUService to a per-DPUCluster DPUService.
	if isUpgradeFromLastReleasedGA {
		// if we're upgrading from the last released GA, we only need to check the legacy DPUService. This works
		// because the templateDPUService has the same component name as the legacy DPUService.
		return p.templateDPUService.isReady(ctx, c, config.GetNamespace(), false)
	} else {
		errs = append(errs, p.areDPUServicesReady(ctx, c, config.GetNamespace(), dpuClusterCount, false)...)
		errs = append(errs, p.areDPUServiceCredentialRequestsReady(ctx, c, config.GetNamespace(), dpuClusterCount, false)...)
	}

	return kerrors.NewAggregate(errs)
}

// IsReady reports the readiness of the objects as well as the version state. It returns
// an error when any of the resources is not ready.
func (p *dpuServicePerDPUClusterObjects) IsReady(ctx context.Context, c client.Client, namespace string) error {
	var errs []error

	// List DPUClusters to determine expected count
	dpuClusterList := &provisioningv1.DPUClusterList{}
	if err := c.List(ctx, dpuClusterList); err != nil {
		return fmt.Errorf("failed to list DPUClusters: %w", err)
	}
	dpuClusterCount := len(dpuClusterList.Items)

	errs = append(errs, p.areDPUServicesReady(ctx, c, namespace, dpuClusterCount, true)...)
	errs = append(errs, p.areDPUServiceCredentialRequestsReady(ctx, c, namespace, dpuClusterCount, true)...)

	return kerrors.NewAggregate(errs)
}
