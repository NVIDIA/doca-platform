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

package controllers

import (
	"context"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	hostutil "github.com/nvidia/doca-platform/internal/provisioning/hostagent/util"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

type HostAgentServerReconciler struct {
	Client client.Client
}

// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpus,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpus/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpus/finalizers,verbs=update

func (r *HostAgentServerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx)
	log.Info("Reconciling")
	dpu := &provisioningv1.DPU{}
	if err := r.Client.Get(ctx, req.NamespacedName, dpu); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	var newCond string
	switch dpu.Status.Phase {
	case provisioningv1.DPUInitializeInterface:
		newCond = string(provisioningv1.DPUCondInterfaceInitialized)
	case provisioningv1.DPUConfigFWParameters:
		newCond = string(provisioningv1.DPUCondFWConfigured)
	case provisioningv1.DPUOSInstalling:
		newCond = string(provisioningv1.DPUCondOSInstalled)
	case provisioningv1.DPURebooting:
		newCond = string(provisioningv1.DPUCondRebooted)
	case provisioningv1.DPUConfig:
		// In production this is reported by the DPU agent after it boots.
		if dpu.Status.AgentStatus == nil {
			dpu.Status.AgentStatus = &provisioningv1.AgentStatus{}
		}
		dpu.Status.AgentStatus.LastStartupTime = ptr.To(metav1.Now())
		dpu.Status.AgentStatus.RebootMethod = ptr.To(provisioningv1.RebootMethodSystemLevelReset)
		if err := r.Client.Status().Update(ctx, dpu); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	case provisioningv1.DPUHostNetworkConfiguration:
		newCond = string(provisioningv1.DPUCondHostNetworkReady)
	}
	if newCond == "" {
		return ctrl.Result{}, nil
	}
	hostutil.NewCondition(newCond).Success("").Set(&dpu.Status.Conditions)
	if err := r.Client.Status().Update(ctx, dpu); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *HostAgentServerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&provisioningv1.DPU{}).
		Named("host-agent-server").
		Complete(r)
}
