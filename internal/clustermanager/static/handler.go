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

package static

import (
	"context"
	"fmt"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/clustermanager/controller"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type clusterHandler struct {
}

func NewHandler() controller.ClusterHandler {
	return &clusterHandler{}
}

func (ch *clusterHandler) ReconcileCluster(ctx context.Context, dc *provisioningv1.DPUCluster) (string, []metav1.Condition, error) {
	var cond *metav1.Condition
	if dc.Spec.Kubeconfig != "" {
		cond = cutil.NewCondition(string(provisioningv1.ConditionCreated), nil, "Created", "")
	} else {
		cond = cutil.NewCondition(string(provisioningv1.ConditionCreated), fmt.Errorf("spec.kubeconfig is empty"), "EmptyKubeconfig", "")
	}
	return dc.Spec.Kubeconfig, []metav1.Condition{*cond}, nil

}

func (ch *clusterHandler) CleanUpCluster(context.Context, *provisioningv1.DPUCluster) (bool, error) {
	// static cluster manager does not clean up anything
	return true, nil
}

func (ch clusterHandler) Type() string {
	return string(provisioningv1.StaticCluster)
}
