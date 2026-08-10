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

package inventory

import (
	"context"
	_ "embed"
	"fmt"
	"maps"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/digest"
	"github.com/nvidia/doca-platform/internal/release"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ Component = &dpuMonitoringObjects{}

// DPUMonitoringSecretName returns the name of the Secret holding the ServiceAccount
// token used to scrape the control plane of the given DPU cluster.
//
// The Secret is created in the namespace of the DPUCluster rather than in the DPF
// operator namespace, because Prometheus resolves the credentials referenced by a
// ServiceMonitor in the namespace of that ServiceMonitor, and the cluster manager
// creates the control plane ServiceMonitors alongside the DPUCluster.
//
// The cluster manager uses this to reference the Secret from the ServiceMonitor it
// creates, so this is the single source of truth for the name.
func DPUMonitoringSecretName(clusterName, clusterNamespace string) string {
	// The DPUCluster name and namespace are hashed to keep the name short and within
	// the 63 character limit.
	hashed := digest.Short(digest.FromObjects(clusterName, clusterNamespace), 10)
	return fmt.Sprintf("%s-credentials-%s", operatorv1.DPUMonitoringName.String(), hashed)
}

// dpuMonitoringObjects generates the objects the DPF Operator needs in order to monitor the
// DPU clusters themselves, as opposed to the workloads running on them.
//
// It currently provides the credentials Prometheus uses to scrape the control plane of every
// DPU cluster. For each DPUCluster it generates a DPUServiceCredentialRequest, which creates a
// ServiceAccount in that DPU cluster and stores a token for it in a Secret in the DPUCluster
// namespace. It additionally generates a single DPUService which deploys the RBAC allowing
// those ServiceAccounts to read the metrics endpoints of their DPU cluster.
//
// Unlike dpuServicePerDPUClusterObjects this component deploys no workload of its own:
// the consumer of the credentials is the Prometheus instance running in the management
// cluster, scraping through the ServiceMonitors created by the cluster manager. It
// therefore generates one DPUService in total rather than one per DPUCluster.
type dpuMonitoringObjects struct {
	data []byte
	// templateDPUService is the template DPUService used to generate the DPUService
	// which deploys RBAC to the DPU clusters.
	templateDPUService fromDPUService
	// dpuServiceCredentialsRequest is the template credentials request that is instantiated per DPUCluster.
	dpuServiceCredentialsRequest *unstructured.Unstructured
	// componentName is the name used for the component label and for the generated DPUService.
	componentName operatorv1.ComponentName
}

func newDPUMonitoringObjects(data []byte) *dpuMonitoringObjects {
	return &dpuMonitoringObjects{
		data:               data,
		templateDPUService: fromDPUService{name: operatorv1.DPUMonitoringName},
		componentName:      operatorv1.DPUMonitoringName,
	}
}

func (p *dpuMonitoringObjects) Name() operatorv1.ComponentName {
	return p.componentName
}

// Parse parses the data into the relevant fields of the struct and performs some basic validations.
func (p *dpuMonitoringObjects) Parse() error {
	if p.data == nil {
		return fmt.Errorf("dpuMonitoringObjects.data can not be empty")
	}

	dpuService, credentialRequest, err := parseDPUServiceAndCredentialRequest(p.data, p.componentName)
	if err != nil {
		return err
	}

	p.templateDPUService.dpuService = dpuService
	p.dpuServiceCredentialsRequest = credentialRequest

	return nil
}

// GenerateManifests applies edits and returns objects.
func (p *dpuMonitoringObjects) GenerateManifests(_ context.Context, vars Variables) ([]client.Object, error) {
	if ok := vars.DisableSystemComponents[p.Name()]; ok {
		return []client.Object{}, nil
	}

	labelsToAdd := map[string]string{
		operatorv1.DPFComponentLabelKey: p.Name().String(),
		release.DPFVersionLabelKey:      release.DPFVersion(),
		applysetPartOfLabel:             ApplySetID(vars.Namespace, p),
	}

	objs := make([]*unstructured.Unstructured, 0)

	// The ServiceAccount granted access to the metrics endpoints is created by the
	// DPUServiceCredentialRequests below, inside the DPU cluster each one targets. A single
	// name is therefore used for every cluster: they are distinct objects in distinct clusters
	// and cannot collide. The DPUServiceCredentialRequests and the Secrets they produce do
	// need per-cluster names, because those all live in the management cluster.
	//
	// The dpu-monitoring chart hardcodes the same name as the subject of the ClusterRoleBinding
	// it deploys, so the two must agree. TestDPUMonitoringChartServiceAccountName guards that.
	serviceAccountName := operatorv1.DPUMonitoringName.String()

	// Generate a DPUServiceCredentialRequest for each DPUCluster.
	for _, cluster := range vars.DPUClusters {
		labelsToAddCopy := make(map[string]string)
		maps.Copy(labelsToAddCopy, labelsToAdd)
		labelsToAddCopy[provisioningv1.DPUClusterNameLabelKey] = cluster.Cluster.Name
		labelsToAddCopy[provisioningv1.DPUClusterNamespaceLabelKey] = cluster.Cluster.Namespace

		clusterCredReq, err := p.credentialRequestForCluster(vars, cluster.Cluster.Name, cluster.Cluster.Namespace, serviceAccountName, labelsToAddCopy)
		if err != nil {
			return nil, err
		}

		objs = append(objs, clusterCredReq)
	}

	rbacDPUServiceCopy, err := p.generateRBACDPUService(vars, labelsToAdd)
	if err != nil {
		return nil, err
	}

	objs = append(objs, rbacDPUServiceCopy)

	// return as Objects
	ret := []client.Object{}
	for i := range objs {
		ret = append(ret, objs[i])
	}

	return ret, nil
}

// credentialRequestForCluster builds the DPUServiceCredentialRequest which creates the metrics
// ServiceAccount inside the given DPU cluster and stores a token for it in a Secret.
//
// The request itself lives in the DPF operator namespace alongside the other components'
// requests, but the Secret it produces is created in the DPUCluster namespace, so that the
// ServiceMonitor the cluster manager creates there can reference it.
func (p *dpuMonitoringObjects) credentialRequestForCluster(vars Variables, clusterName, clusterNamespace, serviceAccountName string, labels map[string]string) (*unstructured.Unstructured, error) {
	// The DPUCluster name and namespace are hashed to keep the name short and within the 63
	// character limit.
	hashedClusterNameNamespace := digest.Short(digest.FromObjects(clusterName, clusterNamespace), 10)

	return generateCredentialRequestForCluster(p.dpuServiceCredentialsRequest, credentialRequestTarget{
		namespace:                  vars.Namespace,
		hashedClusterNameNamespace: hashedClusterNameNamespace,
		clusterName:                clusterName,
		clusterNamespace:           clusterNamespace,
		// The ServiceAccount is created in the target DPU cluster, in the namespace the
		// DPUService deploying the RBAC is applied to.
		serviceAccountName: serviceAccountName,
		secretName:         DPUMonitoringSecretName(clusterName, clusterNamespace),
		secretNamespace:    clusterNamespace,
	}, labels)
}

// generateRBACDPUService generates the DPUService which deploys the metrics RBAC to the DPU clusters.
func (p *dpuMonitoringObjects) generateRBACDPUService(vars Variables, labelsToAdd map[string]string) (*unstructured.Unstructured, error) {
	rbacDPUServiceCopy, err := p.templateDPUService.applyDPUServiceEdits(vars, labelsToAdd)
	if err != nil {
		return nil, fmt.Errorf("failed to apply RBAC related DPUService edits: %w", err)
	}

	// The DPUService keeps the name applyDPUServiceEdits gave it, which is the component
	// name: this component generates a single DPUService, so unlike the per-DPUCluster
	// components there is nothing for it to collide with.

	// Unlike the per-DPUCluster components, the subject of the ClusterRoleBinding does not have
	// to be passed to the chart: the ServiceAccount carries the same name in every DPU cluster,
	// and its namespace is the namespace this DPUService is deployed into. So only the
	// deployment mode is set here, placing the RBAC in the DPU clusters rather than in the
	// management cluster.
	dpuServiceEdits := NewEdits()
	dpuServiceEdits.AddForKindS(DPUServiceKind, dpuServiceAddValueEdit(true, p.componentName.String(), "deployDPUManifests"))
	dpuServiceEdits.AddForKindS(DPUServiceKind, dpuServiceInClusterEdit(false))

	// Apply the edits.
	if err := dpuServiceEdits.Apply([]*unstructured.Unstructured{rbacDPUServiceCopy}); err != nil {
		return nil, err
	}

	return rbacDPUServiceCopy, nil
}

// isReady checks the single DPUService and the per-DPUCluster DPUServiceCredentialRequests.
// Based on the versionValidation input passed, it also checks that they match the DPF version.
//
// Unlike the per-DPUCluster components there is exactly one DPUService, named after the component,
// so the readiness check of the embedded fromDPUService applies unchanged.
func (p *dpuMonitoringObjects) isReady(ctx context.Context, c client.Client, namespace string, versionValidation bool) error {
	var errs []error

	if err := p.templateDPUService.isReady(ctx, c, namespace, versionValidation); err != nil {
		errs = append(errs, err)
	}

	// List DPUClusters to determine the expected number of credential requests.
	dpuClusterList := &provisioningv1.DPUClusterList{}
	if err := c.List(ctx, dpuClusterList); err != nil {
		return fmt.Errorf("failed to list DPUClusters: %w", err)
	}

	errs = append(errs, areDPUServiceCredentialRequestsReady(ctx, c, p.Name(), namespace, len(dpuClusterList.Items), versionValidation)...)

	return kerrors.NewAggregate(errs)
}

// IsReadyForUpgrade reports the readiness of the objects. It returns an error when any of the
// resources is not ready.
func (p *dpuMonitoringObjects) IsReadyForUpgrade(ctx context.Context, c client.Client, config *operatorv1.DPFOperatorConfig) error {
	shouldSkip, err := ShouldSkipUpgradeCheck(p.Name(), *config.Status.Version)
	if err != nil {
		return fmt.Errorf("determine if component %s should skip upgrade check: %w", p.Name(), err)
	}
	if shouldSkip {
		return nil
	}

	return p.isReady(ctx, c, config.GetNamespace(), false)
}

// IsReady reports the readiness of the objects as well as the version state. It returns
// an error when any of the resources is not ready.
func (p *dpuMonitoringObjects) IsReady(ctx context.Context, c client.Client, namespace string) error {
	return p.isReady(ctx, c, namespace, true)
}
