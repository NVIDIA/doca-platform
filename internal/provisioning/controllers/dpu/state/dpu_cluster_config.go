/*
Copyright 2024 NVIDIA

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

package state

import (
	"context"
	"fmt"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	rfclient "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/client"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	dpucluster "github.com/nvidia/doca-platform/pkg/dpucluster"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func ClusterConfig(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) (provisioningv1.DPUStatus, error) {
	logger := log.FromContext(ctx)

	state := dpu.Status.DeepCopy()
	if !dpu.DeletionTimestamp.IsZero() {
		state.Phase = provisioningv1.DPUDeleting
		return *state, nil
	}

	if err := checkFwVersion(ctx, dpu, ctrlCtx); err != nil {
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondDPUClusterReady.String(), err, "CheckFwVersionError", err.Error()))
		return *state, err
	}

	dpuName := dpu.Name
	tenantNamespace := dpu.Spec.Cluster.Namespace
	tenantName := dpu.Spec.Cluster.Name

	dpuCluster := &provisioningv1.DPUCluster{}
	err := ctrlCtx.Get(ctx, types.NamespacedName{Namespace: tenantNamespace, Name: tenantName}, dpuCluster)
	if err != nil {
		if apierrors.IsNotFound(err) {
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondDPUClusterReady.String(), err, "DPUClusterNotFound", err.Error()))
			return *state, err
		}
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondDPUClusterReady.String(), err, "GetDPUClusterError", err.Error()))
		return *state, err
	}

	newClient, err := dpucluster.NewConfig(ctrlCtx.Client, dpuCluster).Client(ctx)
	if err != nil {
		err = fmt.Errorf("failed to create new config client: %w", err)
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondDPUClusterReady.String(), err, "NewConfigError", err.Error()))
		return *state, err
	}

	node := &corev1.Node{}
	if err := newClient.Get(ctx, types.NamespacedName{Namespace: tenantNamespace, Name: dpuName}, node); err != nil {
		if apierrors.IsNotFound(err) {
			err = fmt.Errorf("node %s not found", dpuName)
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondDPUClusterReady.String(), err, "NodeNotFound", err.Error()))
			return *state, err
		}
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondDPUClusterReady.String(), err, "GetNodeError", err.Error()))
		return *state, err
	}

	if !cutil.IsNodeReady(node) {
		err = fmt.Errorf("node is not ready")
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondDPUClusterReady.String(), err, "NodeNotReady", err.Error()))
		return *state, err
	}

	ann := dpu.Spec.Cluster.NodeAnnotations
	if ann == nil {
		ann = map[string]string{}
	}
	if err = cutil.UpdateLabelsAndAnnotationsToNode(ctx, newClient, node, dpu.Spec.Cluster.NodeLabels, ann); err != nil {
		err = fmt.Errorf("failed to update node labels and annotations: %w", err)
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondDPUClusterReady.String(), err, "AddLabelsOrAnnotationsToObjectError", err.Error()))
		return *state, err
	}

	logger.V(3).Info(fmt.Sprintf("DPU %s joins the %s DPU Cluster", dpuName, dpuCluster.Name))

	cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondDPUClusterReady, "", "cluster configured"))
	state.Phase = provisioningv1.DPUNodeEffectRemoval
	return *state, nil
}

func checkFwVersion(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) error {
	if dpu.Status.DPUInstallInterface == nil || *dpu.Status.DPUInstallInterface != string(provisioningv1.InstallViaRedFish) {
		return nil
	}

	if dpu.Status.DPUType == provisioningv1.DPUTypeBlueField4 {
		return nil
	}

	dpuDevice := &provisioningv1.DPUDevice{}
	if err := ctrlCtx.Get(ctx, types.NamespacedName{Namespace: dpu.Namespace, Name: dpu.Spec.DPUDeviceName}, dpuDevice); err != nil {
		return err
	}

	bfb := &provisioningv1.BFB{}
	if err := ctrlCtx.Get(ctx, types.NamespacedName{Namespace: dpu.Namespace, Name: dpu.Spec.BFB}, bfb); err != nil {
		return err
	}

	var rfClient *rfclient.Client
	var err error
	var versionInfo *rfclient.VersionInfo

	if rfClient, err = rfclient.NewTLSClient(ctx, dpuDevice.BMCAddress(), dpu.Namespace, ctrlCtx.Client); err != nil {
		return err
	}

	if _, versionInfo, err = rfClient.CheckDPUUEFI(); err != nil {
		return err
	}

	bfbUEFIVersion := bfb.Status.Versions.UEFI

	if versionInfo.Version != bfbUEFIVersion {
		return fmt.Errorf("DPU UEFI version %s was not updated, expected %s", versionInfo.Version, bfbUEFIVersion)
	}

	if _, versionInfo, err = rfClient.CheckDPUBSP(); err != nil {
		return err
	}

	bfbBSPVersion := bfb.Status.Versions.BSP

	if versionInfo.Version != bfbBSPVersion {
		return fmt.Errorf("DPU BSP version %s was not updated, expected %s", versionInfo.Version, bfbBSPVersion)
	}
	return nil
}
