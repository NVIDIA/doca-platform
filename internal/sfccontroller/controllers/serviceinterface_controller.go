/*
COPYRIGHT 2024 NVIDIA

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

package controller

import (
	"context"
	"fmt"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/internal/conditions"
	"github.com/nvidia/doca-platform/internal/ovsutils"

	"github.com/fluxcd/pkg/runtime/patch"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	ServiceInterfaceController = "serviceinterfacecontroller"
	ServiceInterfaceFinalizer  = dpuservicev1.SvcDpuGroupName + "/ServiceInterface-finalizer"
	RequeueIntervalSuccess     = 20 * time.Second
	RequeueIntervalError       = 5 * time.Second
	OvnPatch                   = "puplinkbrovn"
	OvnPatchPeer               = "puplinkbrsfc"
	SFCBridge                  = "br-sfc"
	OVNBridge                  = "br-ovn"
)

//nolint:unparam
func requeueDone() (ctrl.Result, error) {
	return ctrl.Result{}, nil
}

//nolint:unparam
func requeueSuccess() (ctrl.Result, error) {
	return ctrl.Result{RequeueAfter: RequeueIntervalSuccess}, nil
}

//nolint:unparam
func requeueError() (ctrl.Result, error) {
	return ctrl.Result{RequeueAfter: RequeueIntervalError}, nil
}

// ServiceInterfaceReconciler reconciles a ServiceInterface object
type ServiceInterfaceReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	NodeName string
	OVS      ovsutils.API
}

// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=ServiceInterfaces,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=ServiceInterfaces/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=ServiceInterfaces/finalizers,verbs=update
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=serviceinterfaces,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=serviceinterfaces/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch

func AddPort(ctx context.Context, ovs ovsutils.API, portName string, ifaceExternalIDs, portExternalIDs map[string]string) error {
	if err := ovs.AddPort(ctx, SFCBridge, portName, "dpdk"); err != nil {
		return err
	}

	if err := ovs.SetIfaceExternalIDs(ctx, portName, ifaceExternalIDs); err != nil {
		return err
	}

	if err := ovs.SetPortExternalIDs(ctx, portName, portExternalIDs); err != nil {
		return err
	}

	return nil
}

func AddPatchPort(ctx context.Context, ovs ovsutils.API, brA, brB string, ifaceExternalIDs map[string]string) error {
	if err := ovs.AddPort(ctx, brA, OvnPatch, "patch"); err != nil {
		return err
	}

	if err := ovs.SetIfaceOptions(ctx, OvnPatch, map[string]string{"peer": OvnPatchPeer}); err != nil {
		return err
	}

	if err := ovs.AddPort(ctx, brB, OvnPatchPeer, "patch"); err != nil {
		return err
	}

	if err := ovs.SetIfaceExternalIDs(ctx, OvnPatchPeer, ifaceExternalIDs); err != nil {
		return err
	}

	if err := ovs.SetIfaceOptions(ctx, OvnPatchPeer, map[string]string{"peer": OvnPatch}); err != nil {
		return err
	}

	return nil
}

func FigureOutName(ctx context.Context, serviceInterface *dpuservicev1.ServiceInterface) string {
	log := ctrllog.FromContext(ctx)
	portName := ""

	switch serviceInterface.Spec.InterfaceType {
	case dpuservicev1.InterfaceTypePhysical:
		log.Info("matched on physical")
		portName = serviceInterface.Spec.Physical.InterfaceName
	case dpuservicev1.InterfaceTypePF:
		log.Info("matched on pf")
		portName = fmt.Sprintf("pf%dhpf", serviceInterface.Spec.PF.ID)
	case dpuservicev1.InterfaceTypeVF:
		log.Info("matched on vf")
		portName = fmt.Sprintf("pf%dvf%d", serviceInterface.Spec.VF.PFID, serviceInterface.Spec.VF.VFID)
	case dpuservicev1.InterfaceTypeVLAN:
		log.Info("matched on vlan skipping")
		// TODO for MVP it is out of scope
		// revisit this once we do not have collisions on patches.
	case dpuservicev1.InterfaceTypeService:
		// service interfaces add/remove from ovs is handled part of CNI ADD/DEL flow
		log.Info("matched on service skipping")
	}

	return portName
}

func AddInterfacesToOvs(ctx context.Context, ovs ovsutils.API, serviceInterface *dpuservicev1.ServiceInterface, metadata string) error {
	log := ctrllog.FromContext(ctx)
	var err error

	if serviceInterface.Spec.InterfaceType == dpuservicev1.InterfaceTypeOVN {
		log.Info("matched on ovn")
		err = AddPatchPort(ctx, ovs, OVNBridge, SFCBridge, map[string]string{"dpf-id": metadata})
		if err != nil {
			log.Info(fmt.Sprintf("failed to add port: %s", err.Error()))
			return err
		}
		return nil
	}

	portName := FigureOutName(ctx, serviceInterface)

	if portName != "" {
		if serviceInterface.Spec.InterfaceType == dpuservicev1.InterfaceTypePhysical {
			err = AddPort(ctx, ovs, portName, map[string]string{"dpf-id": metadata}, map[string]string{"dpf-type": "physical"})
		} else {
			err = AddPort(ctx, ovs, portName, map[string]string{"dpf-id": metadata}, nil)
		}
		if err != nil {
			log.Info(fmt.Sprintf("failed to add port: %s", err.Error()))
			return err
		}
	}

	return nil
}

func DeleteInterfacesFromOvs(ctx context.Context, ovs ovsutils.API, serviceInterface *dpuservicev1.ServiceInterface) error {
	log := ctrllog.FromContext(ctx)
	log.Info("deleteInterfacesFromOvs")

	if serviceInterface.Spec.InterfaceType == dpuservicev1.InterfaceTypeOVN {
		log.Info("matched on ovn")
		// match on ovs-cni naming
		err := ovs.DelPort(ctx, SFCBridge, OvnPatch)
		if err != nil {
			log.Info(fmt.Sprintf("failed to delete port %s", err.Error()))
			return err
		}
		err = ovs.DelPort(ctx, SFCBridge, OvnPatchPeer)
		if err != nil {
			log.Info(fmt.Sprintf("failed to delete port %s", err.Error()))
			return err
		}
		return nil
	}

	portName := FigureOutName(ctx, serviceInterface)

	if portName != "" {
		err := ovs.DelPort(ctx, SFCBridge, portName)
		if err != nil {
			log.Info(fmt.Sprintf("failed to delete port: %s", err.Error()))
			return err
		}
	}
	return nil
}

// reconcileDelete handles the delete reconciliation loop
//
//nolint:unparam
func (r *ServiceInterfaceReconciler) reconcileDelete(ctx context.Context, serviceInterface *dpuservicev1.ServiceInterface, req ctrl.Request) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx)
	log.Info("Reconciling delete")
	err := DeleteInterfacesFromOvs(ctx, r.OVS, serviceInterface)
	if err != nil {
		log.Error(err, "failed to delete DeleteInterfacesFromOvs")
		return requeueError()
	}

	// If there are no associated applications remove the finalizer
	log.Info("Removing finalizer")
	controllerutil.RemoveFinalizer(serviceInterface, ServiceInterfaceFinalizer)
	return requeueSuccess()
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *ServiceInterfaceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	log := ctrllog.FromContext(ctx)
	log.Info("reconciling")
	serviceInterface := &dpuservicev1.ServiceInterface{}

	if err := r.Client.Get(ctx, req.NamespacedName, serviceInterface); err != nil {
		if apierrors.IsNotFound(err) {
			// Return early if the object is not found.
			log.Info("object not found")
			return requeueDone()
		}
		log.Error(err, "failed to get ServiceInterface")
		return requeueError()
	}

	if serviceInterface.Spec.Node == nil {
		log.Info("si.Spec.Node: not set, skip the object")
		return requeueSuccess()
	}

	if *serviceInterface.Spec.Node != r.NodeName {
		// this object was not intended for this nodes
		// skip
		log.Info(fmt.Sprintf("serviceInterface.Spec.Node: %s != node: %s", *serviceInterface.Spec.Node, r.NodeName))
		return ctrl.Result{}, nil
	}

	patcher := patch.NewSerialPatcher(serviceInterface, r.Client)

	conditions.EnsureConditions(serviceInterface, dpuservicev1.ServiceInterfaceConditions)
	// Defer a patch call to always patch the object when Reconcile exits.
	defer func() {
		log.Info("Patching")

		conditions.SetSummary(serviceInterface)
		if err := patcher.Patch(ctx, serviceInterface,
			patch.WithFieldOwner(ServiceInterfaceController),
			patch.WithStatusObservedGeneration{},
			patch.WithOwnedConditions{Conditions: conditions.TypesAsStrings(dpuservicev1.ServiceInterfaceConditions)},
		); err != nil {
			reterr = kerrors.NewAggregate([]error{reterr, nil})
		}
	}()

	if !serviceInterface.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, serviceInterface, req)
	}

	// Add finalizer if not set.
	if !controllerutil.ContainsFinalizer(serviceInterface, ServiceInterfaceFinalizer) {
		controllerutil.AddFinalizer(serviceInterface, ServiceInterfaceFinalizer)
		return ctrl.Result{Requeue: true}, nil
	}

	err := AddInterfacesToOvs(ctx, r.OVS, serviceInterface, req.NamespacedName.String())
	if err != nil {
		log.Info("failed to add AddInterfacesToOvs")
		conditions.AddFalse(
			serviceInterface,
			dpuservicev1.ServiceInterfaceReconciled,
			conditions.ReasonError,
			conditions.ConditionMessage(fmt.Sprintf("Error occurred: %v", err)),
		)
		return requeueError()
	}

	return requeueSuccess()
}

// SetupWithManager sets up the controller with the Manager.
func (r *ServiceInterfaceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dpuservicev1.ServiceInterface{}).
		Complete(r)
}
