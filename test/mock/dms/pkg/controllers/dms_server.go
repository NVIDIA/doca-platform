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
	"fmt"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/pkg/dpucluster"
	"github.com/nvidia/doca-platform/test/mock/dms/pkg/server"

	"github.com/fluxcd/pkg/runtime/patch"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	fakeNodeLabel              = "node-role.dpf.nvidia.com/fake"
	dpfOperatorSystemNamespace = "dpf-operator-system"
)

// DMSServerReconciler reconciles a DPU object
type DMSServerReconciler struct {
	Client client.Client
	server.ListenerManager
	server.Server

	// PodName is used to advertise this pod as the DMS server for DPUs.
	PodName string
}

// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpus;dpunodes;dpudevices,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpus/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpus/finalizers,verbs=update
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpuclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpuclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpuclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=pods;pods/exec;nodes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=patch;update;delete;create
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpudevices/status,verbs=get;update;patch

func (r *DMSServerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	log := ctrllog.FromContext(ctx)
	log.Info("Reconciling")
	node := &corev1.Node{}
	if err := r.Client.Get(ctx, req.NamespacedName, node); err != nil {
		if apierrors.IsNotFound(err) {
			// Return early if the object is not found.
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	// Return early if this node does not have the NodeSelectorLabel
	if _, ok := node.GetLabels()[cutil.NodeSelectorLabel]; !ok {
		return ctrl.Result{}, nil
	}

	if !node.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	dpuNode := r.dpuNodeForNode(ctx, node)

	l, err := r.AllocateListener()
	if err != nil {
		return ctrl.Result{}, err
	}

	if err := r.ServeForDPUNode(dpuNode, l); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.createDPUDeviceForDPUNode(ctx, dpuNode); err != nil {
		return ctrl.Result{}, err
	}
	// Create a DPUNode for the node if it doesn't exist.
	if err := r.Client.Create(ctx, dpuNode); client.IgnoreAlreadyExists(err) != nil {
		return ctrl.Result{}, err
	}

	// Create a Node for the DPU once it has been created.
	if err := r.createNodeForDPU(ctx, dpuNode); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *DMSServerReconciler) createDPUDeviceForDPUNode(ctx context.Context, dpuNode *provisioningv1.DPUNode) error {
	dpuDevice := &provisioningv1.DPUDevice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-device", dpuNode.Name),
			Namespace: dpfOperatorSystemNamespace,
			Labels: map[string]string{
				cutil.DPUNodeNameLabel:         dpuNode.Name,
				cutil.DPUDevicePCIAddressLabel: "0000-00-00",
			},
		},
		Spec: provisioningv1.DPUDeviceSpec{
			SerialNumber: "MT25066004C7",
			PSID:         nil,
			OPN:          nil,
			BMCIP:        nil,
			NumberOfPFs:  ptr.To(2),
			PF0Name:      ptr.To("pf1"),
		},
	}
	err := client.IgnoreAlreadyExists(r.Client.Create(ctx, dpuDevice))
	if err != nil {
		return err
	}
	patcher := patch.NewSerialPatcher(dpuDevice, r.Client)
	dpuDevice.Status.PCIAddress = ptr.To("0000-00-00")
	return patcher.Patch(ctx, dpuDevice)
}

// createDPUNodeForNode creates a DPUNode for the DPU. When using real DMS creating this object is done by a bash
// script inside an init container for DMS.
func (r *DMSServerReconciler) dpuNodeForNode(ctx context.Context, node *corev1.Node) *provisioningv1.DPUNode {
	// If the dpuNode is already created this is a no-op.
	dpuNode := &provisioningv1.DPUNode{}
	if err := r.Client.Get(ctx, client.ObjectKey{Namespace: dpfOperatorSystemNamespace, Name: fmt.Sprintf("%s-dpu", node.Name)}, dpuNode); err == nil {
		return nil
	}
	dpuNode = &provisioningv1.DPUNode{
		ObjectMeta: metav1.ObjectMeta{
			Name:      node.Name,
			Namespace: dpfOperatorSystemNamespace,
			Labels: map[string]string{
				// DPUs require a node with this label to advance past the "Initializing" phase
				cutil.NodeFeatureDiscoveryLabelPrefix + cutil.DPUOOBBridgeConfiguredLabel: "true",
				cutil.DPUOOBBridgeConfiguredLabel:                                         "true",
			},
			Annotations: map[string]string{},
			// The OwnerReference needs to be set as this is the way the DPUNode controller sets status.KubeNodeRef.
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: corev1.SchemeGroupVersion.String(),
					Kind:       "Node",
					Name:       node.Name,
					UID:        node.UID,
				},
			},
		},
		Spec: provisioningv1.DPUNodeSpec{
			NodeRebootMethod: &provisioningv1.NodeRebootMethod{
				GNOI: &provisioningv1.GNOI{},
			},
			DPUs: []provisioningv1.DPURef{
				{
					Name: fmt.Sprintf("%s-device", node.Name),
				},
			},
		},
	}
	return dpuNode
}

// createNodeForDPU creates a Kubernetes node with a Ready conditions for the DPU.
func (r *DMSServerReconciler) createNodeForDPU(ctx context.Context, dpuNode *provisioningv1.DPUNode) error {
	log := ctrllog.FromContext(ctx)
	// Find the DPUDevices associated with the DPUNode
	dpus := &provisioningv1.DPUList{}
	if err := r.Client.List(ctx, dpus, client.MatchingLabels(map[string]string{cutil.DPUNodeNameLabel: dpuNode.Name})); err != nil {
		return err
	}
	// Find the DPU associated with the DPUDevices.
	for _, dpu := range dpus.Items {
		// Only create the node in the DPUClusterConfig phase.
		if dpu.Status.Phase != provisioningv1.DPUClusterConfig {
			return nil
		}
		log.Info("Ensuring node is up to date for DPU")
		dpuCluster := &provisioningv1.DPUCluster{}
		err := r.Client.Get(ctx, types.NamespacedName{Namespace: dpu.Spec.Cluster.Namespace, Name: dpu.Spec.Cluster.Name}, dpuCluster)
		if err != nil {
			return err
		}

		dpuClient, err := dpucluster.NewConfig(r.Client, dpuCluster).Client(ctx)
		if err != nil {
			return err
		}

		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: dpu.Name,
				Annotations: map[string]string{
					"kwok.x-k8s.io/node": "fake",
				},
				Labels: map[string]string{
					fakeNodeLabel: "true",
				},
			},
			TypeMeta: metav1.TypeMeta{
				Kind:       "Node",
				APIVersion: "v1",
			},
			Spec: corev1.NodeSpec{
				PodCIDR:    "",
				PodCIDRs:   nil,
				ProviderID: "",
			},
		}
		// Return early if the node already exists. Do not repeatedly reconcile the object to avoid conflicts with kwok.
		if err := dpuClient.Get(ctx, client.ObjectKeyFromObject(node), &corev1.Node{}); err == nil {
			return nil
		}
		node.ManagedFields = nil
		if err := dpuClient.Patch(ctx, node, client.Apply, client.ForceOwnership, client.FieldOwner("mock-dms")); err != nil {
			return err
		}

		node.Status = corev1.NodeStatus{
			NodeInfo: corev1.NodeSystemInfo{
				Architecture: "arm64",
			},
			Allocatable: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1000"),
				corev1.ResourceMemory: resource.MustParse("2Ti"),
				corev1.ResourcePods:   resource.MustParse("1000"),
			},
			Capacity: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1000"),
				corev1.ResourceMemory: resource.MustParse("2Ti"),
				corev1.ResourcePods:   resource.MustParse("1000"),
			},
		}

		node.ManagedFields = nil
		if err := dpuClient.Status().Patch(ctx, node, client.Apply, client.ForceOwnership, client.FieldOwner("mock-dms")); err != nil {
			return err
		}
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DMSServerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Node{}).
		Watches(&provisioningv1.DPU{}, handler.EnqueueRequestsFromMapFunc(dpuToNode)).
		Named("dms-server").
		Complete(r)
}

func dpuToNode(_ context.Context, obj client.Object) []ctrl.Request {
	dpu, ok := obj.(*provisioningv1.DPU)
	if !ok {
		return nil
	}
	return []ctrl.Request{{NamespacedName: types.NamespacedName{Namespace: "", Name: dpu.Spec.DPUNodeName}}}
}
