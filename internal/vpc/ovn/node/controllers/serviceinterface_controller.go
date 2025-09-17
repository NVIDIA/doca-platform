/*
SPDX-FileCopyrightText: Copyright (c) 2025 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
SPDX-License-Identifier: LicenseRef-NvidiaProprietary

NVIDIA CORPORATION, its affiliates and licensors retain all intellectual
property and proprietary rights in and to this material, related
documentation and any modifications thereto. Any use, reproduction,
disclosure or distribution of this material and related documentation
 without an express license agreement from NVIDIA CORPORATION or
its affiliates is strictly prohibited.
*/

//nolint:dupl
package controllers

import (
	"context"
	"fmt"
	"net"
	"time"

	vpcovncommon "gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/common"
	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/node/nodeutils"

	"github.com/fluxcd/pkg/runtime/patch"
	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/conditions"
	"github.com/nvidia/doca-platform/pkg/ovsutils"
	"github.com/nvidia/doca-platform/pkg/utils/networkhelper"
	"github.com/nvidia/doca-platform/pkg/vfmac"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=serviceinterfaces,verbs=get;list;watch;update;patch;delete
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=serviceinterfaces/finalizers,verbs=update
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=serviceinterfaces/status,verbs=get;update;patch

const (
	ServiceInterfaceControllerName = "vpc-ovn-node-serviceinterface-controller"
	ServiceInterfaceFinalizer      = "ovn.vpc.dpu.nvidia.com/serviceinterface-node-finalizer"
	UnknownMACAddress              = "unknown"
	RequeueIntervalError           = 5 * time.Second
	DefaultMTU                     = 9216
	PodNodeNameKey                 = "spec.nodeName"
	DPDKPortType                   = "dpdk"
)

// ServiceInterfaceReconciler reconciles ServiceInterface objects in dpu clusters
type ServiceInterfaceReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	NodeName      string
	OVS           ovsutils.API
	VFMapping     *vfmac.VFMapping
	NetworkHelper networkhelper.NetworkHelper
}

//nolint:unparam
func requeueError() (ctrl.Result, error) {
	return ctrl.Result{RequeueAfter: RequeueIntervalError}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ServiceInterfaceReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	// Add index for Pod's spec.nodeName field
	if err := mgr.GetCache().IndexField(ctx, &corev1.Pod{}, PodNodeNameKey, func(o client.Object) []string {
		return []string{o.(*corev1.Pod).Spec.NodeName}
	}); err != nil {
		return err
	}

	siVirtualNetworkPredicate := predicate.NewPredicateFuncs(func(o client.Object) bool {
		si, ok := o.(*dpuservicev1.ServiceInterface)
		if !ok {
			return false
		}
		return si.HasVirtualNetwork()
	})

	nodePredicate := predicate.NewPredicateFuncs(func(o client.Object) bool {
		si, ok := o.(*dpuservicev1.ServiceInterface)
		if !ok {
			return false
		}
		if si.Spec.Node == nil { // NodeName may not be set
			return false
		}
		return *si.Spec.Node == r.NodeName
	})

	podPredicate := predicate.NewPredicateFuncs(func(o client.Object) bool {
		pod, ok := o.(*corev1.Pod)
		if !ok {
			return false
		}
		return pod.Spec.NodeName == r.NodeName && pod.Status.Phase != corev1.PodRunning && pod.Labels[dpuservicev1.DPFServiceIDLabelKey] != ""
	})

	return ctrl.NewControllerManagedBy(mgr).
		Named(ServiceInterfaceControllerName).
		For(&dpuservicev1.ServiceInterface{},
			builder.WithPredicates(nodePredicate, siVirtualNetworkPredicate)).
		// Watch Pods and trigger reconcile for ServiceInterfaces of type Service
		Watches(&corev1.Pod{}, handler.EnqueueRequestsFromMapFunc(r.requestsForPods),
			builder.WithPredicates(podPredicate),
		).Complete(r)
}

// Reconcile reconciles a ServiceInterface object when virtualNetwork is set
func (r *ServiceInterfaceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	log := ctrllog.FromContext(ctx)

	log.Info("Reconciling")
	si := &dpuservicev1.ServiceInterface{}
	if err := r.Client.Get(ctx, req.NamespacedName, si); err != nil {
		if apierrors.IsNotFound(err) {
			// Return early if the object is not found.
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	patcher := patch.NewSerialPatcher(si, r.Client)
	conditions.EnsureConditions(si, dpuservicev1.ServiceInterfaceVPCConditions)
	// Defer a patch call to always patch the object when Reconcile exits.
	defer func() {
		log.Info("Patching")

		conditions.SetSummary(si)
		if err := patcher.Patch(ctx, si,
			patch.WithFieldOwner(ServiceInterfaceControllerName),
			patch.WithStatusObservedGeneration{},
			patch.WithOwnedConditions{Conditions: conditions.TypesAsStrings(dpuservicev1.ServiceInterfaceVPCConditions)},
		); err != nil {
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
	}()

	// Handle deletion reconciliation loop.
	if !si.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, si)
	}

	// Add finalizer if not set.
	if !controllerutil.ContainsFinalizer(si, ServiceInterfaceFinalizer) {
		controllerutil.AddFinalizer(si, ServiceInterfaceFinalizer)
		return ctrl.Result{}, nil
	}

	return r.reconcile(ctx, si)
}

//nolint:unparam
func (r *ServiceInterfaceReconciler) reconcile(ctx context.Context, si *dpuservicev1.ServiceInterface) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx)
	log.Info("Reconcile internal")

	if err := r.AddMacAdressAnnotation(ctx, si); err != nil {
		log.Error(err, "Failed to add MAC address annotation, requeue")
		conditions.AddFalse(
			si,
			dpuservicev1.ServiceInterfaceReconciled,
			conditions.ReasonError,
			conditions.ConditionMessage(fmt.Sprintf("MAC address annotation error: %v", err)),
		)
		return requeueError()
	}

	if si.ObjectMeta.Annotations[vpcovncommon.LSPConnectedAnnotationKey] != vpcovncommon.AnnotationValueTrue {
		// not ready yet,wait for the vpc-ovn-controller on the management cluster to connect the LSP
		log.Info("LSP is not connected")
		conditions.AddFalse(
			si,
			dpuservicev1.ServiceInterfaceReconciled,
			conditions.ReasonPending,
			conditions.ConditionMessage("Waiting for the vpc-ovn-controller to connect the LSP"),
		)
		return requeueError()
	}

	ifaceID := vpcovncommon.ServiceInterfacePortName(si)
	switch si.Spec.InterfaceType {
	case dpuservicev1.InterfaceTypePF, dpuservicev1.InterfaceTypeVF:
		// Add the interface to the br-int bridge
		if err := r.AddInterfaceToOvs(ctx, si, ifaceID); err != nil {
			log.Error(err, "Failed to add interface", "ifaceID", ifaceID)
			conditions.AddFalse(
				si,
				dpuservicev1.ServiceInterfaceReconciled,
				conditions.ReasonError,
				conditions.ConditionMessage(fmt.Sprintf("Failed to add interface: %v", err)),
			)
			return requeueError()
		}
	case dpuservicev1.InterfaceTypeService:
		// ServiceInterface of type service will be plugged into the bridge by ovs-cni
		portName, err := nodeutils.GetPortNameForInterface(ctx, r.Client, r.OVS, r.NetworkHelper, si, r.NodeName)
		if err != nil {
			log.Error(err, "Failed to get port name for service interface")
			return requeueError()
		}

		ifaceExternalIDs := map[string]string{nodeutils.IfaceIDKey: ifaceID}
		log.Info("Setting interface ExternalIDs", "interface", portName, "externalIDs", ifaceExternalIDs)
		if err := r.OVS.SetIfaceExternalIDs(ctx, portName, ifaceExternalIDs); err != nil {
			log.Error(err, "Failed to add iface-id to interface", "interface name", portName)
			conditions.AddFalse(
				si,
				dpuservicev1.ServiceInterfaceReconciled,
				conditions.ReasonError,
				conditions.ConditionMessage(fmt.Sprintf("Failed to add iface-id to interface: %v", err)),
			)
			return requeueError()
		}
	default:
		log.Error(nil, "Unsupported interface type", "type", si.Spec.InterfaceType)
		conditions.AddFalse(
			si,
			dpuservicev1.ServiceInterfaceReconciled,
			conditions.ReasonError,
			conditions.ConditionMessage(fmt.Sprintf("Unsupported interface type: %v", si.Spec.InterfaceType)),
		)
		return requeueError()
	}

	log.Info("ServiceInterface node reconciled successfully")
	conditions.AddTrue(
		si,
		dpuservicev1.ServiceInterfaceReconciled,
	)
	return ctrl.Result{}, nil
}

func (r *ServiceInterfaceReconciler) getSFMacAddress(ctx context.Context, serviceInterface *dpuservicev1.ServiceInterface) (string, error) {
	log := ctrllog.FromContext(ctx)
	log.Info("Getting SF service interface MAC address", "service interface", serviceInterface)

	iface, err := nodeutils.GetPortForServiceInterfaceTypeService(ctx, r.Client, r.OVS, serviceInterface, r.NodeName)
	if err != nil {
		return "", fmt.Errorf("failed to get port name for service interface: %v", err)
	}

	macAddr, found := iface.ExternalIDs[nodeutils.IfaceMacKey]
	if !found {
		return "", fmt.Errorf("mac address not found for service interface")
	}
	if _, err := net.ParseMAC(macAddr); err != nil {
		return "", fmt.Errorf("invalid MAC address %q in iface external-ids: %v", macAddr, err)
	}

	log.Info("Service interface mac address", "mac address", macAddr)

	return macAddr, nil
}

func hasUnknownMACAnnotation(si *dpuservicev1.ServiceInterface) bool {
	return si.Annotations[vpcovncommon.LSPUnknownMACAnnotationKey] == vpcovncommon.AnnotationValueTrue
}

func (r *ServiceInterfaceReconciler) AddMacAdressAnnotation(ctx context.Context, si *dpuservicev1.ServiceInterface) error {
	log := ctrllog.FromContext(ctx)
	if si.ObjectMeta.Annotations == nil {
		si.ObjectMeta.Annotations = make(map[string]string)
	}
	macAddrVal := UnknownMACAddress

	unknownMACAnnotation := hasUnknownMACAnnotation(si)
	if unknownMACAnnotation {
		si.ObjectMeta.Annotations[vpcovncommon.LSPMACAddressAnnotationKey] = macAddrVal
		log.Info("Set serviceinterface annotation lsp-mac-address (unknown MAC)", "lsp-mac-address", macAddrVal)
		return nil
	}

	var err error
	switch si.Spec.InterfaceType {
	case dpuservicev1.InterfaceTypeVF:
		macAddrVal, err = r.VFMapping.GetVFMacAddressFromVFMapping(si.Spec.VF.PFID, si.Spec.VF.VFID)
		if err != nil {
			return err
		}
	case dpuservicev1.InterfaceTypeService:
		// Currently, only SFs are supported as InterfaceTypeService.
		macAddrVal, err = r.getSFMacAddress(ctx, si)
		if err != nil {
			return err
		}
	case dpuservicev1.InterfaceTypePF:
		macAddrVal, err = r.VFMapping.GetPFMacAddressFromVFMapping(si.Spec.PF.ID)
		if err != nil {
			return err
		}
	default:
		// Any other interface types don't add MAC address annotation
		return nil
	}

	// Just set the annotation in-memory, let the deferred patcher handle the PATCH
	si.ObjectMeta.Annotations[vpcovncommon.LSPMACAddressAnnotationKey] = macAddrVal

	log.Info("Set serviceinterface annotation lsp-mac-address", "lsp-mac-address", macAddrVal)
	return nil
}

//nolint:unparam
func (r *ServiceInterfaceReconciler) reconcileDelete(ctx context.Context, si *dpuservicev1.ServiceInterface) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx)
	log.Info("Reconcile delete")

	switch si.Spec.InterfaceType {
	case dpuservicev1.InterfaceTypePF, dpuservicev1.InterfaceTypeVF:
		// Delete the interface from ovs
		if err := r.DeleteInterfaceFromOvs(ctx, si); err != nil {
			log.Error(err, "Failed to delete interface from ovs")
			conditions.AddFalse(
				si,
				dpuservicev1.ServiceInterfaceReconciled,
				conditions.ReasonError,
				conditions.ConditionMessage(fmt.Sprintf("Failed to delete interface from ovs: %v", err)),
			)
			return requeueError()
		}
	case dpuservicev1.InterfaceTypeService:
		// ServiceInterface of type service will be deleted by ovs-cni
		// No action needed from this controller
	default:
		// No action needed for unsupported interface types
		log.Info("Unsupported interface type, no delete action needed", "type", si.Spec.InterfaceType)
	}

	// If there are no associated applications remove the finalizer
	if controllerutil.ContainsFinalizer(si, ServiceInterfaceFinalizer) {
		log.Info("Removing finalizer")
		controllerutil.RemoveFinalizer(si, ServiceInterfaceFinalizer)
	}

	return ctrl.Result{}, nil
}

// requestsForPods returns requests for the pod containing ServiceInterface of type Service
func (r *ServiceInterfaceReconciler) requestsForPods(ctx context.Context, o client.Object) []ctrl.Request {
	log := ctrllog.FromContext(ctx)
	pod, ok := o.(*corev1.Pod)
	if !ok {
		return nil
	}
	serviceID := pod.Labels[dpuservicev1.DPFServiceIDLabelKey]
	serviceInterfaceList := &dpuservicev1.ServiceInterfaceList{}
	if err := r.Client.List(ctx, serviceInterfaceList, client.MatchingLabels{dpuservicev1.DPFServiceIDLabelKey: serviceID}, client.InNamespace(pod.Namespace)); err != nil {
		log.Error(err, "Failed to list service interfaces", "pod", pod.Name, "serviceID", serviceID)
		return nil
	}

	if len(serviceInterfaceList.Items) == 0 {
		log.Info("No service interfaces with serviceID found for pod", "pod", pod.Name, "serviceID", serviceID)
		return nil
	}

	requests := []ctrl.Request{}
	for _, si := range serviceInterfaceList.Items {
		if si.HasVirtualNetwork() {
			requests = append(requests, ctrl.Request{NamespacedName: types.NamespacedName{Name: si.Name, Namespace: si.Namespace}})
		}
	}

	return requests
}

func (r *ServiceInterfaceReconciler) AddInterfaceToOvs(ctx context.Context, serviceInterface *dpuservicev1.ServiceInterface, ifaceID string) error {
	log := ctrllog.FromContext(ctx)
	log.Info("Adding interface to ovs")

	// Skip if interface type is Service as it's handled by ovs-cni
	if serviceInterface.Spec.InterfaceType == dpuservicev1.InterfaceTypeService {
		return nil
	}

	portName, err := nodeutils.GetPortNameForInterface(ctx, r.Client, r.OVS, r.NetworkHelper, serviceInterface, r.NodeName)
	if err != nil {
		log.Error(err, "Failed to get port name for interface",
			"serviceInterface", client.ObjectKeyFromObject(serviceInterface))
		return fmt.Errorf("failed to get port name for service interface: %w", err)
	}

	log.Info("Adding interface to ovs", "interface", portName, "ifaceID", ifaceID)
	if err := r.AddPort(ctx, portName, map[string]string{nodeutils.IfaceIDKey: ifaceID}); err != nil {
		log.Error(err, "Failed to add interface", "ifaceID", ifaceID, "portName", portName)
		return err
	}
	log.Info("Interface added to ovs", "interface", portName)

	return nil
}

func (r *ServiceInterfaceReconciler) DeleteInterfaceFromOvs(ctx context.Context, serviceInterface *dpuservicev1.ServiceInterface) error {
	log := ctrllog.FromContext(ctx)
	log.Info("Deleting interface from ovs")

	// Skip if interface type is Service as it's handled by ovs-cni
	if serviceInterface.Spec.InterfaceType == dpuservicev1.InterfaceTypeService {
		return nil
	}

	portName, err := nodeutils.GetPortNameForInterface(ctx, r.Client, r.OVS, r.NetworkHelper, serviceInterface, r.NodeName)
	if err != nil {
		log.Error(err, "Failed to get port name for interface, port will not be deleted from OVS.",
			"serviceInterface", client.ObjectKeyFromObject(serviceInterface))
		return nil
	}

	log.Info("Deleting interface", "interface", portName)
	err = r.OVS.DelPort(ctx, nodeutils.IntegrationBridge, portName)
	if err != nil {
		log.Error(err, "Failed to delete interface", "interface name", portName)
		return err
	}

	return nil
}

// AddPort adds a port to the integration bridge with the given name and external IDs
func (r *ServiceInterfaceReconciler) AddPort(ctx context.Context, portName string, ifaceExternalIDs map[string]string) error {
	log := ctrllog.FromContext(ctx)
	mtu := DefaultMTU
	log.Info("Adding port to bridge", "port", portName, "bridge", nodeutils.IntegrationBridge)
	if err := r.OVS.AddPort(ctx, nodeutils.IntegrationBridge, portName, DPDKPortType, &mtu); err != nil {
		return err
	}

	log.Info("Setting interface externalIDs", "interface", portName, "externalIDs", ifaceExternalIDs)
	if err := r.OVS.SetIfaceExternalIDs(ctx, portName, ifaceExternalIDs); err != nil {
		return err
	}

	return nil
}
