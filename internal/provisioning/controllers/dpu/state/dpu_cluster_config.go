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
	dpucluster "github.com/nvidia/doca-platform/internal/dpucluster"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type dpuClusterConfig struct {
	dpu *provisioningv1.DPU
}

func (st *dpuClusterConfig) Handle(ctx context.Context, client crclient.Client, _ dutil.DPUOptions) (provisioningv1.DPUStatus, error) {
	logger := log.FromContext(ctx)

	state := st.dpu.Status.DeepCopy()
	if isDeleting(st.dpu) {
		state.Phase = provisioningv1.DPUDeleting
		return *state, nil
	}

	dpuName := st.dpu.Name
	tenantNamespace := st.dpu.Spec.Cluster.Namespace
	tenantName := st.dpu.Spec.Cluster.Name

	dpuCluster := &provisioningv1.DPUCluster{}
	err := client.Get(ctx, types.NamespacedName{Namespace: tenantNamespace, Name: tenantName}, dpuCluster)
	if err != nil {
		return *state, err
	}

	newClient, err := dpucluster.NewConfig(client, dpuCluster).Client(ctx)
	if err != nil {
		return *state, err
	}

	node := &corev1.Node{}
	if err := newClient.Get(ctx, types.NamespacedName{Namespace: tenantNamespace, Name: dpuName}, node); err != nil {
		return *state, err
	}

	if !util.IsNodeReady(node) {
		return *state, nil
	}

	// Patch DPU labels to Kamaji Node
	if err = util.AddLabelsToNode(ctx, newClient, node, st.dpu.Spec.Cluster.NodeLabels); err != nil {
		return *state, err
	}

	logger.V(3).Info(fmt.Sprintf("DPU %s joined Kamaji Cluster", dpuName))

	state.Phase = provisioningv1.DPUReady
	util.SetDPUCondition(state, util.DPUCondition(provisioningv1.DPUCondReady, "", "cluster configured"))
	return *state, nil
}
