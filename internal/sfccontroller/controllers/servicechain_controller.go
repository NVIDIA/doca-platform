/*
COPYRIGHT 2026 NVIDIA

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
	"errors"
	"fmt"
	"hash/fnv"
	"math"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/conditions"
	oflow "github.com/nvidia/doca-platform/pkg/openflow"
	"github.com/nvidia/doca-platform/pkg/ovsmodel"
	"github.com/nvidia/doca-platform/pkg/ovsutils"

	"antrea.io/antrea/pkg/ovs/openflow"
	"github.com/fluxcd/pkg/runtime/patch"
	model "github.com/ovn-org/libovsdb/model"
	ovsdb "github.com/ovn-org/libovsdb/ovsdb"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	kexec "k8s.io/utils/exec"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// ServiceChainReconciler reconciles a ServiceChain object
type ServiceChainReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	NodeName   string
	BridgeName string
	OFTable    openflow.Table
	OFBridge   openflow.Bridge
	OVS        ovsutils.API
	Exec       kexec.Interface
	SC         ServiceChainAPI
	OPFlow     oflow.OpenFlowAPI
}

const (
	BridgeSFC                  = "br-sfc" // Default OVS bridge name for Service Function Chaining
	RequeueIntervalFlows       = 5 * time.Second
	podNodeNameKey             = "spec.nodeName"
	ServiceChainFinalizer      = dpuservicev1.SvcDpuGroupName + "/ServiceChain-finalizer"
	ServiceChainLabel          = dpuservicev1.SvcDpuGroupName + "/service-chain"
	serviceChainControllerName = "servicechaincontroller"

	PriorityDropFlows         = 100
	PriorityCustomFlows       = 50
	PriorityLearntFlows       = 30
	PriorityDynamicLearnFlows = 20
	PriorityDefaultFlows      = 10

	ForwardablePTPMulticastMac    = "01:1b:19:00:00:00" // forwardable PTP multicast MAC address
	NonForwardablePTPMulticastMac = "01:80:c2:00:00:0e" // unforwardable PTP multicast MAC address
)

//nolint:unparam
func requeueFlows() (ctrl.Result, error) {
	return ctrl.Result{RequeueAfter: RequeueIntervalFlows}, nil
}

func setFalseServiceChainReconciledCondition(err error, sc *dpuservicev1.ServiceChain) {
	conditions.AddFalse(
		sc,
		dpuservicev1.ServiceChainReconciled,
		conditions.ReasonError,
		conditions.ConditionMessage(fmt.Sprintf("Error occurred: %v", err)),
	)
}

func setTrueServiceChainReconciledCondition(sc *dpuservicev1.ServiceChain) {
	conditions.AddTrue(
		sc,
		dpuservicev1.ServiceChainReconciled,
	)
}

// Hashing function, will be used when adding and removing OpenFlow flows
// This hash will take in the service chain name and return the corresponding hash
func hash(s string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s)) // Ignoring error
	return h.Sum64()
}

// Utility function to find an OVS interface based on the condition that the
// external_id:dpf-id=condition which is sent as an input
func findInterface(ctx context.Context, ovs ovsutils.API, condition string) (string, error) {
	// Get doesn't work for ExternalIDs field
	var ifaces []ovsmodel.Interface
	iface := &ovsmodel.Interface{
		ExternalIDs: map[string]string{"dpf-id": condition},
	}
	err := ovs.WhereAll(
		iface,
		model.Condition{
			Field:    &iface.ExternalIDs,
			Function: ovsdb.ConditionIncludes,
			Value:    iface.ExternalIDs,
		},
	).List(ctx, &ifaces)

	if err != nil {
		return "", fmt.Errorf("failed to get interface with external_ids: %s: %v", iface.ExternalIDs, err)
	}

	if len(ifaces) != 1 {
		return "", fmt.Errorf("failed to find matching interface with external_ids :%s", iface.ExternalIDs)
	}

	iface = &ifaces[0]

	found, err := ovs.IsIfaceInBr(ctx, SFCBridge, iface.Name)
	if err != nil {
		return "", err
	}

	if found {
		if iface.Ofport == nil {
			return "", fmt.Errorf("interface %s Ofport is nil", iface.Name)
		}
		return fmt.Sprintf("%d", *iface.Ofport), nil
	}

	// check if port is a patch port for hbn

	// Build br-hbn patch p + intfname + brsfc
	portName := "p" + iface.Name + "brsfc"
	found, err = ovs.IsIfaceInBr(ctx, SFCBridge, portName)
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("port %s or %s not found in %s", iface.Name, portName, SFCBridge)
	}

	iface = &ovsmodel.Interface{
		Name: portName,
	}
	err = ovs.Get(ctx, iface)
	if err != nil {
		return "", fmt.Errorf("failed to get interface %s: %v", portName, err)
	}

	if iface.Ofport == nil {
		return "", fmt.Errorf("interface %s Ofport is nil", portName)
	}
	return fmt.Sprintf("%d", *iface.Ofport), nil
}

// getPodWithLabels returns pod in namespace that is scheduled on current node with given labels. if more than one or none matches, error out.
func (r *ServiceChainReconciler) getPodWithLabels(ctx context.Context, namespace string, lbls map[string]string) (*corev1.Pod, error) {
	podList := &corev1.PodList{}
	listOpts := []client.ListOption{}
	listOpts = append(listOpts, client.MatchingLabels(lbls))
	listOpts = append(listOpts, client.MatchingFieldsSelector{Selector: fields.OneTermEqualSelector(podNodeNameKey, r.NodeName)})
	if namespace != "" {
		listOpts = append(listOpts, client.InNamespace(namespace))
	}

	if err := r.List(ctx, podList, listOpts...); err != nil {
		return nil, err
	}

	if len(podList.Items) == 0 {
		return nil, fmt.Errorf("no pod in namespace(%s) matching labels(%v) on node(%s) found", namespace, lbls, r.NodeName)
	}

	if len(podList.Items) > 1 {
		return nil, fmt.Errorf("expected only one pod in namespace(%s) to match labels(%v) on node(%s). found %d",
			namespace, lbls, r.NodeName, len(podList.Items))
	}

	return &podList.Items[0], nil
}

// getServiceInterfaceListWithLabels returns ServiceInterface in namespace that belongs to current node with given labels.
func (r *ServiceChainReconciler) getServiceInterfaceListWithLabels(ctx context.Context, namespace string, lbls map[string]string) ([]*dpuservicev1.ServiceInterface, error) {
	sil := &dpuservicev1.ServiceInterfaceList{}
	listOpts := []client.ListOption{}

	listOpts = append(listOpts, client.MatchingLabels(lbls))
	if namespace != "" {
		listOpts = append(listOpts, client.InNamespace(namespace))
	}

	if err := r.List(ctx, sil, listOpts...); err != nil {
		return nil, err
	}

	// filter out serviceInterfaces not on this node
	matching := make([]*dpuservicev1.ServiceInterface, 0, len(sil.Items))
	for i := range sil.Items {
		if sil.Items[i].Spec.Node == nil ||
			*sil.Items[i].Spec.Node != r.NodeName {
			continue
		}
		matching = append(matching, &sil.Items[i])
	}
	return matching, nil
}

// getServiceInterfaceWithLabels returns ServiceInterface in namespace that belongs to current node with given labels. if more than one or none matches, error out.
func (r *ServiceChainReconciler) getServiceInterfaceWithLabels(ctx context.Context, namespace string, lbls map[string]string) (*dpuservicev1.ServiceInterface, error) {
	sil, err := r.getServiceInterfaceListWithLabels(ctx, namespace, lbls)
	if err != nil {
		return nil, err
	}

	if len(sil) == 0 {
		return nil, fmt.Errorf("no serviceInterface in namespace(%s) matching labels(%v) on node(%s) found", namespace, lbls, r.NodeName)
	}

	if len(sil) > 1 {
		return nil, fmt.Errorf("expected only one serviceInterface in namespace(%s) to match labels(%v) on node(%s). found %d",
			namespace, lbls, r.NodeName, len(sil))
	}

	return sil[0], nil
}

// getPortNameForServiceInterface returns the ovs port name matching the given service interface
func (r *ServiceChainReconciler) getPortNameForServiceInterface(ctx context.Context, namespace string, si *dpuservicev1.ServiceInterface) (string, error) {
	condition := si.Namespace + "/" + si.Name
	if si.Spec.InterfaceType == dpuservicev1.InterfaceTypeService {
		// get pod matching serviceID
		pod, err := r.getPodWithLabels(ctx, namespace, map[string]string{dpuservicev1.DPFServiceIDLabelKey: si.Spec.Service.ServiceID})
		if err != nil {
			return "", err
		}
		if si.Spec.Service.InterfaceName == "" {
			return "", errors.New("empty interface name for serviceInterface of type service")
		}
		// construct condition which identifies ovs port for the interface that is associated
		// with service and serviceInterface.
		condition = pod.Namespace + "/" + pod.Name + "/" + si.Spec.Service.InterfaceName
	}

	port, err := findInterface(ctx, r.OVS, condition)
	if err != nil {
		return "", err
	}

	if port == "" {
		return "", fmt.Errorf("port with condition %s not found", condition)
	}

	return port, nil
}

func (r *ServiceChainReconciler) addServiceInterfaceChainLabel(ctx context.Context, si *dpuservicev1.ServiceInterface, serviceChainName string) error {
	patcher := patch.NewSerialPatcher(si, r.Client)
	si.Labels[ServiceChainLabel] = serviceChainName
	return patcher.Patch(ctx, si)
}

func (r *ServiceChainReconciler) deleteServiceInterfaceChainLabel(ctx context.Context, serviceChainName, namespace string) error {
	siLabelsToDelete := map[string]string{
		ServiceChainLabel: serviceChainName,
	}
	sil, err := r.getServiceInterfaceListWithLabels(ctx, namespace, siLabelsToDelete)
	if err != nil {
		return fmt.Errorf("failed to get interface %v", err)
	}
	for _, si := range sil {
		patcher := patch.NewSerialPatcher(si, r.Client)
		delete(si.Labels, ServiceChainLabel)
		if err := patcher.Patch(ctx, si); err != nil {
			return fmt.Errorf("failed to patch ServiceInterface %v", err)
		}
	}
	return nil
}

// reconcileDelete handles the delete reconciliation loop
//
//nolint:unparam
func (r *ServiceChainReconciler) reconcileDelete(ctx context.Context, sc *dpuservicev1.ServiceChain, req ctrl.Request, hashedName uint64) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx)
	log.Info("Reconciling delete")
	if err := r.deleteServiceInterfaceChainLabel(ctx, req.Name, req.Namespace); err != nil {
		log.Error(err, "failed to delete ServiceInterface chain label")
		return requeueFlows()
	}

	flowErrors := r.OFBridge.DeleteFlowsByCookie(hashedName, math.MaxUint64)
	if flowErrors != nil {
		log.Error(flowErrors, "failed to delete flows")
		return requeueFlows()
	}

	// If there are no associated applications remove the finalizer
	log.Info("Removing finalizer")
	controllerutil.RemoveFinalizer(sc, ServiceChainFinalizer)
	return requeueFlows()
}

// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch;update
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=servicechains,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=servicechains/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=servicechains/finalizers,verbs=update
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=serviceinterfaces,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups="",resources=nodes;s,verbs=get;list;watch

func (r *ServiceChainReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	log := ctrllog.FromContext(ctx)
	log.Info("reconciling")
	var err error
	var hashedName uint64
	sc := &dpuservicev1.ServiceChain{}
	hashedName = hash(req.NamespacedName.String())

	if err = r.Client.Get(ctx, req.NamespacedName, sc); err != nil {
		if apierrors.IsNotFound(err) {
			// Return early if the object is not found.
			if err := r.deleteServiceInterfaceChainLabel(ctx, req.Name, req.Namespace); err != nil {
				log.Error(err, "failed to delete ServiceInterface chain label")
				return requeueFlows()
			}

			// Always ensure delete operation in case of errors
			flowErrors := r.OFBridge.DeleteFlowsByCookie(hashedName, math.MaxUint64)
			if flowErrors != nil {
				log.Error(flowErrors, "failed to delete flows")
				return requeueFlows()
			}
			return requeueDone()
		}
		log.Error(err, "failed to get ServiceChain")
		return requeueFlows()
	}

	patcher := patch.NewSerialPatcher(sc, r.Client)
	conditions.EnsureConditions(sc, dpuservicev1.ServiceChainConditions)
	// Defer a patch call to always patch the object when Reconcile exits.
	defer func() {
		log.Info("Patching")

		conditions.SetSummary(sc)
		if err := patcher.Patch(ctx, sc,
			patch.WithFieldOwner(serviceChainControllerName),
			patch.WithStatusObservedGeneration{},
			patch.WithOwnedConditions{Conditions: conditions.TypesAsStrings(dpuservicev1.ServiceChainConditions)},
		); err != nil {
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
	}()

	// Handle deletion reconciliation loop.
	if !sc.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, sc, req, hashedName)
	}

	// Add finalizer if not set.
	if !controllerutil.ContainsFinalizer(sc, ServiceChainFinalizer) {
		controllerutil.AddFinalizer(sc, ServiceChainFinalizer)
		return requeueFlows()
	}

	// On updates delete all OpenFlow flows with the same service chain name
	if sc.Generation != sc.Status.ObservedGeneration {
		flowErrors := r.OFBridge.DeleteFlowsByCookie(hashedName, math.MaxUint64)
		if flowErrors != nil {
			log.Error(flowErrors, "failed to delete flows")
			return requeueFlows()
		}
	}

	var errs []error

	// Construct an array of arrays of ports in order to traverse it
	// and construct the flows
	// don't fail immediately, operate on best effort basis to enable partial chains
	// to enable some of the traffic to pass
	var ports [][]string
	for swPos, sw := range sc.Spec.Switches {
		ports = append(ports, [][]string{{}}...)
		for _, port := range sw.Ports {
			si, err := r.getServiceInterfaceWithLabels(ctx, sc.Namespace, port.ServiceInterface.MatchLabels)
			if err != nil {
				log.Error(err, "failed to get interface")
				errs = append(errs, err)
				continue
			}
			if isValid, reason := isValidateServiceInterface(si); !isValid {
				log.Error(fmt.Errorf("invalid service interface"), reason, "serviceinterface labels", port.ServiceInterface.MatchLabels)
				errs = append(errs, fmt.Errorf("invalid service interface: %s", reason))
				continue
			}
			intfName, err := r.getPortNameForServiceInterface(ctx, sc.Namespace, si)
			if err != nil {
				log.Error(err, "failed to get interface")
				errs = append(errs, err)
				continue
			}
			if intfName != "" {
				ports[swPos] = append(ports[swPos], intfName)
			}

			if err := r.addServiceInterfaceChainLabel(ctx, si, sc.Name); err != nil {
				log.Error(err, "failed to add ServiceInterface chain label")
				errs = append(errs, err)
				continue
			}
		}
	}

	err = r.SC.GenerateAndApplyOpenFlows(ctx, ports, hashedName)
	if err != nil {
		log.Error(err, "failed to generate and apply flows")
		errs = append(errs, err)
	}

	// Apply any custom flows for this chain
	err = r.EnsureCustomFlowsForChain(ctx, sc)
	if err != nil {
		log.Error(err, "failed to add custom flows")
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		err := kerrors.NewAggregate(errs)
		log.Error(err, "failed to reconcile service chain")
		setFalseServiceChainReconciledCondition(err, sc)
		return requeueFlows()
	}

	setTrueServiceChainReconciledCondition(sc)
	return requeueFlows()
}

// SetupWithManager sets up the controller with the Manager.
func (r *ServiceChainReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	err := mgr.GetCache().IndexField(ctx, &corev1.Pod{}, podNodeNameKey, func(o client.Object) []string {
		return []string{o.(*corev1.Pod).Spec.NodeName}
	})
	if err != nil {
		return err
	}

	p := predicate.NewPredicateFuncs(func(o client.Object) bool {
		if o.(*dpuservicev1.ServiceChain).Spec.Node == nil { // NodeName may not be set
			return false
		}
		return *o.(*dpuservicev1.ServiceChain).Spec.Node == r.NodeName
	})

	return ctrl.NewControllerManagedBy(mgr).
		For(&dpuservicev1.ServiceChain{}, builder.WithPredicates(p)).
		Complete(r)
}

func isValidateServiceInterface(si *dpuservicev1.ServiceInterface) (bool, string) {
	if si.HasVirtualNetwork() {
		return false, fmt.Sprintf("serviceInterface %s in namespace (%s) has a virtual network, cannot be chained on br-sfc bridge", si.Name, si.Namespace)
	}

	if !conditions.IsTrue(si, conditions.TypeReady) {
		errorMessage := ""
		for _, condition := range si.Status.Conditions {
			if condition.Type == string(conditions.TypeReady) {
				continue
			}
			if condition.Status == metav1.ConditionFalse {
				errorMessage = fmt.Sprintf("%s%s: %s\n", errorMessage, condition.Type, condition.Message)
			}
		}
		return false, fmt.Sprintf("serviceInterface %s in namespace (%s) is not ready: %s", si.Name, si.Namespace, errorMessage)
	}

	return true, ""
}
