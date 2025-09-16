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

package hostagent

import (
	"context"
	"fmt"
	"strings"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/hostagent/networkmanager"
	"github.com/nvidia/doca-platform/internal/provisioning/hostagent/nodemanager"
	"github.com/nvidia/doca-platform/internal/provisioning/hostagent/phase/checkreboot"
	"github.com/nvidia/doca-platform/internal/provisioning/hostagent/phase/configfw"
	"github.com/nvidia/doca-platform/internal/provisioning/hostagent/phase/install"
	"github.com/nvidia/doca-platform/internal/provisioning/hostagent/phase/interfaceinit"
	"github.com/nvidia/doca-platform/internal/provisioning/hostagent/phase/network"
	"github.com/nvidia/doca-platform/internal/provisioning/hostagent/phase/reboot"

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

type PhaseHandler interface {
	Handle(context.Context, *provisioningv1.DPU) (provisioningv1.DPUStatus, ctrl.Result, error)
}

type HostAgentReconciler struct {
	client.Client
	BFBRegistryAddress string
	handlers           map[provisioningv1.DPUPhase]PhaseHandler
	NodeManager        nodemanager.Interface
	NetworkManager     networkmanager.Interface
}

func NewHostAgentReconciler(client client.Client,
	bfbRegistryAddress string,
	nodeManager nodemanager.Interface,
	networkManager networkmanager.Interface) *HostAgentReconciler {
	r := &HostAgentReconciler{
		Client:         client,
		NodeManager:    nodeManager,
		NetworkManager: networkManager,
	}
	bfbRegistry := bfbRegistryAddress + "/bfb"
	if !strings.HasPrefix(bfbRegistry, "http") && !strings.HasPrefix(bfbRegistry, "https") {
		bfbRegistry = "http://" + bfbRegistry
	}
	r.handlers = map[provisioningv1.DPUPhase]PhaseHandler{
		provisioningv1.DPUInitializeInterface:      interfaceinit.NewHandler(client, r.NetworkManager.GetDevice),
		provisioningv1.DPUConfigFWParameters:       configfw.NewHandler(client, r.NetworkManager.GetDevice),
		provisioningv1.DPUOSInstalling:             install.NewHandler(client, bfbRegistry, r.NetworkManager.GetDevice),
		provisioningv1.DPUCheckingHostRebootNeed:   checkreboot.NewHandler(client, r.NetworkManager.GetDevice),
		provisioningv1.DPURebooting:                reboot.NewHandler(client, r.NodeManager.GetNodeName, r.NetworkManager.GetDevice),
		provisioningv1.DPUHostNetworkConfiguration: network.NewHandler(r.NetworkManager.AddNetworkRequest),
	}
	return r
}

func (r *HostAgentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx)

	dpu := &provisioningv1.DPU{}
	if err := r.Get(ctx, req.NamespacedName, dpu); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	log.V(3).Info("Reconciling", "phase", dpu.Status.Phase)
	if dpu.Status.DPUInstallInterface != nil && *dpu.Status.DPUInstallInterface == string(provisioningv1.InstallViaRedFish) {
		return ctrl.Result{}, nil
	}

	h, ok := r.handlers[dpu.Status.Phase]
	if !ok {
		// skip phases that are not handled by host agent
		log.V(3).Info("Skipping phase", "phase", dpu.Status.Phase)
		return ctrl.Result{}, nil
	}
	newStatus, result, err := h.Handle(ctx, dpu.DeepCopy())
	if equality.Semantic.DeepEqual(dpu.Status, newStatus) {
		log.V(3).Info("No change in DPU status", "phase", dpu.Status.Phase)
		return result, err
	}
	log.Info("Update DPU status", "phase", dpu.Status.Phase)
	dpu.Status = newStatus
	if err := r.Status().Update(ctx, dpu); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update DPU status, err: %v", err)
	}
	return result, err
}

func (r *HostAgentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&provisioningv1.DPU{}, builder.WithPredicates(predicate.NewPredicateFuncs(func(o client.Object) bool {
			dpu, ok := o.(*provisioningv1.DPU)
			if !ok {
				return false
			}
			return dpu.Spec.DPUNodeName == r.NodeManager.GetNodeName()
		}))).
		Complete(r)
}
