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

package controller

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"math"
	"sync"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/internal/features"
	"github.com/nvidia/doca-platform/internal/utils"
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
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	kexec "k8s.io/utils/exec"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
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

	// resyncCh is the watch source TriggerResync uses to force a reconcile of every ServiceChain.
	resyncCh chan event.GenericEvent

	// resyncMu guards resyncActive/resyncPending, which coalesce concurrent TriggerResync callers.
	resyncMu      sync.Mutex
	resyncActive  bool
	resyncPending bool
}

const (
	BridgeSFC                  = "br-sfc" // Default OVS bridge name for Service Function Chaining
	podNodeNameKey             = "spec.nodeName"
	serviceChainNodeNameKey    = "spec.node"
	ServiceChainFinalizer      = dpuservicev1.SvcDpuGroupName + "/ServiceChain-finalizer"
	serviceChainControllerName = "servicechaincontroller"

	LearningTable  = 0
	SwitchingTable = 1

	MulticastPriority     = 2
	UnicastLearntPriority = 1
	UnicastPriority       = 0

	PriorityCustomFlows = 50

	BroadcastMulticastMask = "01:00:00:00:00:00/01:00:00:00:00:00"

	ForwardablePTPMulticastMac    = "01:1b:19:00:00:00" // forwardable PTP multicast MAC address
	NonForwardablePTPMulticastMac = "01:80:c2:00:00:0e" // unforwardable PTP multicast MAC address
)

var errPodNotFound = errors.New("pod not found")

// TriggerResync forces a reconcile of every ServiceChain on this node, e.g. after an ovs-vswitchd
// restart or an API-server reconnect. The vswitchd-restart and reconnect callbacks can fire
// concurrently and each enqueue blocks until consumed, so a second caller doesn't block behind the
// first: if a resync is already running it just marks another pass pending and returns nil, and the
// running resync repeats so it always covers the latest trigger. A coalesced caller's nil therefore
// means "accepted, will run", not "completed": every pass logs its own failure so those errors are
// surfaced even though only the first caller gets one returned.
func (r *ServiceChainReconciler) TriggerResync(ctx context.Context) error {
	r.resyncMu.Lock()
	if r.resyncActive {
		r.resyncPending = true
		r.resyncMu.Unlock()
		return nil
	}
	r.resyncActive = true
	r.resyncMu.Unlock()

	for {
		err := r.resyncAllChains(ctx)
		if err != nil {
			ctrllog.FromContext(ctx).Error(err, "ServiceChain resync pass failed")
		}

		r.resyncMu.Lock()
		// Keep looping while a request is pending, even on error: that caller returned nil
		// expecting this loop to cover it, so dropping it on error would silently lose the pass.
		if !r.resyncPending {
			r.resyncActive = false
			r.resyncMu.Unlock()
			return err
		}
		r.resyncPending = false
		r.resyncMu.Unlock()
	}
}

// resyncAllChains enqueues a reconcile for every ServiceChain on this node.
func (r *ServiceChainReconciler) resyncAllChains(ctx context.Context) error {
	scList := &dpuservicev1.ServiceChainList{}
	if err := r.List(ctx, scList, client.MatchingFields{serviceChainNodeNameKey: r.NodeName}); err != nil {
		return err
	}

	for i := range scList.Items {
		select {
		case r.resyncCh <- event.GenericEvent{Object: &scList.Items[i]}:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (r *ServiceChainReconciler) getPodServiceInterfaceLabels(ctx context.Context, pod *corev1.Pod) ([]labels.Set, error) {
	serviceID, ok := pod.Labels[dpuservicev1.DPFServiceIDLabelKey]
	if !ok {
		return nil, nil
	}

	labelSets, err := r.legacyServiceInterfaceLabels(ctx, pod.Namespace, serviceID)
	if err != nil {
		return nil, err
	}

	// On the NSI path there are no legacy ServiceInterface objects, so also match the node's NSI entries;
	// otherwise a service Pod restart would not re-trigger the chains that resolve through those entries.
	if features.Gates.Enabled(features.NSIPathForSFC) {
		nsiLabels, err := r.nsiEntryLabels(ctx, pod.Namespace, serviceID)
		if err != nil {
			return nil, err
		}
		labelSets = append(labelSets, nsiLabels...)
	}

	return labelSets, nil
}

// legacyServiceInterfaceLabels returns the labels of this node's legacy ServiceInterfaces backed by serviceID.
func (r *ServiceChainReconciler) legacyServiceInterfaceLabels(ctx context.Context, namespace, serviceID string) ([]labels.Set, error) {
	siList := &dpuservicev1.ServiceInterfaceList{}
	if err := r.List(ctx, siList,
		client.InNamespace(namespace),
		client.MatchingFields{utils.ServiceInterfaceNodeFieldKey: r.NodeName},
	); err != nil {
		return nil, err
	}

	labelSets := make([]labels.Set, 0, len(siList.Items))
	for i := range siList.Items {
		si := &siList.Items[i]
		if si.Spec.Service == nil || si.Spec.Service.ServiceID != serviceID {
			continue
		}
		labelSets = append(labelSets, labels.Set(si.Labels))
	}
	return labelSets, nil
}

// nsiEntryLabels returns the labels of this node's SFC NSI entries backed by serviceID.
func (r *ServiceChainReconciler) nsiEntryLabels(ctx context.Context, namespace, serviceID string) ([]labels.Set, error) {
	nsi, err := r.getSFCNodeServiceInterfaces(ctx)
	if err != nil {
		return nil, err
	}
	if nsi == nil {
		return nil, nil
	}

	labelSets := make([]labels.Set, 0, len(nsi.Spec.Interfaces))
	for i := range nsi.Spec.Interfaces {
		entry := &nsi.Spec.Interfaces[i]
		entryNamespace, _ := entry.GetNamespacedName()
		svc := entry.GetService()
		if entryNamespace != namespace || svc == nil || svc.ServiceID != serviceID {
			continue
		}
		labelSets = append(labelSets, labels.Set(entry.Labels))
	}
	return labelSets, nil
}

func serviceChainUsesInterfaces(sc *dpuservicev1.ServiceChain, interfaceLabels []labels.Set) bool {
	for _, sw := range sc.Spec.Switches {
		for _, port := range sw.Ports {
			selector := labels.SelectorFromSet(port.ServiceInterface.MatchLabels)
			for _, labelSet := range interfaceLabels {
				if selector.Matches(labelSet) {
					return true
				}
			}
		}
	}
	return false
}

// serviceChainsForPod enqueues ServiceChains using the changed service Pod.
func (r *ServiceChainReconciler) serviceChainsForPod(ctx context.Context, o client.Object) []reconcile.Request {
	pod, ok := o.(*corev1.Pod)
	if !ok {
		return nil
	}

	interfaceLabels, err := r.getPodServiceInterfaceLabels(ctx, pod)
	if err != nil {
		ctrllog.FromContext(ctx).Error(err, "failed to list interfaces for pod-triggered resync")
		return nil
	}
	if len(interfaceLabels) == 0 {
		return nil
	}

	scList := &dpuservicev1.ServiceChainList{}
	if err := r.List(ctx, scList,
		client.InNamespace(pod.Namespace),
		client.MatchingFields{serviceChainNodeNameKey: r.NodeName},
	); err != nil {
		ctrllog.FromContext(ctx).Error(err, "failed to list ServiceChains for pod-triggered resync")
		return nil
	}

	reqs := make([]reconcile.Request, 0, len(scList.Items))
	for i := range scList.Items {
		sc := &scList.Items[i]
		if !serviceChainUsesInterfaces(sc, interfaceLabels) {
			continue
		}
		reqs = append(reqs, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(sc)})
	}
	return reqs
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
		ExternalIDs: map[string]string{ovsutils.DPFIDKey: condition},
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

	if len(ifaces) == 0 {
		return "", fmt.Errorf("failed to find matching interface with external_ids: %s", iface.ExternalIDs)
	}

	if len(ifaces) > 2 {
		return "", fmt.Errorf("found %d interfaces with external_ids %s, expected at most 2", len(ifaces), iface.ExternalIDs)
	}

	// patch port should have two interfaces with the same dpf-id, other ports should have one interface
	for i := range ifaces {
		found, err := ovs.IsIfaceInBr(ctx, SFCBridge, ifaces[i].Name)
		if err != nil {
			return "", err
		}
		if found {
			if ifaces[i].Ofport == nil {
				return "", fmt.Errorf("interface %s Ofport is nil", ifaces[i].Name)
			}
			return fmt.Sprintf("%d", *ifaces[i].Ofport), nil
		}
	}

	// More than one interface found is patch port case, expected one of the interfaces to be in br-sfc
	if len(ifaces) > 1 {
		return "", fmt.Errorf("neither interface with dpf-id %s found in %s", condition, SFCBridge)
	}

	// check if this is a port created for hbn by ovs cni
	// naming is generated in the format: `p<interface_name>brsfc`
	iface = &ifaces[0]
	portName := "p" + iface.Name + "brsfc"
	found, err := ovs.IsIfaceInBr(ctx, SFCBridge, portName)
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("port %s or %s not found in %s", iface.Name, portName, SFCBridge)
	}

	iface = &ovsmodel.Interface{Name: portName}
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
		return nil, fmt.Errorf("%w: no pod in namespace(%s) matching labels(%v) on node(%s) found", errPodNotFound, namespace, lbls, r.NodeName)
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

// reconcileDelete handles the delete reconciliation loop
func (r *ServiceChainReconciler) reconcileDelete(ctx context.Context, sc *dpuservicev1.ServiceChain, hashedName uint64) error {
	log := ctrllog.FromContext(ctx)
	log.Info("Reconciling delete")

	if err := r.OFBridge.DeleteFlowsByCookie(hashedName, math.MaxUint64); err != nil {
		log.Error(err, "failed to delete flows")
		return err
	}

	// If there are no associated applications remove the finalizer
	log.Info("Removing finalizer")
	controllerutil.RemoveFinalizer(sc, ServiceChainFinalizer)
	return nil
}

// resolveSwitchPort resolves port to a validated interface's OVS interface name, legacy or NSI.
func (r *ServiceChainReconciler) resolveSwitchPort(ctx context.Context, sc *dpuservicev1.ServiceChain, nsi *dpuservicev1.NodeServiceInterfaces, port dpuservicev1.Port) (string, error) {
	candidate, err := r.getSingleInterfaceCandidate(ctx, sc.Namespace, nsi, port.ServiceInterface.MatchLabels)
	if err != nil {
		return "", err
	}
	if isValid, reason := candidate.ready(); !isValid {
		return "", fmt.Errorf("invalid service interface: %s", reason)
	}
	return r.getPortNameForInterfaceEntry(ctx, sc.Namespace, candidate.spec, candidate.condition)
}

// buildChainPorts resolves every switch's ports to OVS interface names, best-effort.
func (r *ServiceChainReconciler) buildChainPorts(ctx context.Context, sc *dpuservicev1.ServiceChain, nsi *dpuservicev1.NodeServiceInterfaces) ([][]string, []error) {
	var errs []error
	ports := make([][]string, 0, len(sc.Spec.Switches))
	for swPos, sw := range sc.Spec.Switches {
		ports = append(ports, []string{})
		for _, port := range sw.Ports {
			intfName, err := r.resolveSwitchPort(ctx, sc, nsi, port)
			if err != nil {
				errs = append(errs, err)
			}
			if intfName != "" {
				ports[swPos] = append(ports[swPos], intfName)
			}
		}
	}
	return ports, errs
}

// reconcileFlows builds and applies this chain's ports as OpenFlow and custom flows.
func (r *ServiceChainReconciler) reconcileFlows(ctx context.Context, sc *dpuservicev1.ServiceChain, nsi *dpuservicev1.NodeServiceInterfaces, hashedName uint64) error {
	// don't fail immediately, operate on best effort basis to enable partial chains
	// to enable some of the traffic to pass
	ports, errs := r.buildChainPorts(ctx, sc, nsi)
	// Applied fire-and-forget every reconcile: ovs-ofctl is idempotent, so this is harmless.
	if err := r.SC.GenerateAndApplyOpenFlows(ctx, ports, hashedName); err != nil {
		errs = append(errs, err)
	}

	if err := r.EnsureCustomFlowsForChain(ctx, sc, nsi); err != nil {
		errs = append(errs, err)
	}

	return kerrors.NewAggregate(errs)
}

// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch;update
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=servicechains,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=servicechains/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=servicechains/finalizers,verbs=update
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=serviceinterfaces,verbs=get;list;watch
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=nodeserviceinterfaces,verbs=get;list;watch
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
			// Object gone: remove its flows anyway before returning.
			if flowErrors := r.OFBridge.DeleteFlowsByCookie(hashedName, math.MaxUint64); flowErrors != nil {
				log.Error(flowErrors, "failed to delete flows")
				return ctrl.Result{}, flowErrors
			}
			return requeueDone()
		}
		log.Error(err, "failed to get ServiceChain")
		return ctrl.Result{}, err
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
		); err != nil && !isOnlyNotFoundErr(err) {
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
	}()

	// Handle deletion reconciliation loop.
	if !sc.ObjectMeta.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, r.reconcileDelete(ctx, sc, hashedName)
	}

	// Add finalizer if not set.
	if !controllerutil.ContainsFinalizer(sc, ServiceChainFinalizer) {
		controllerutil.AddFinalizer(sc, ServiceChainFinalizer)
		return ctrl.Result{}, nil
	}

	// On updates delete all OpenFlow flows with the same service chain name
	if sc.Generation != sc.Status.ObservedGeneration {
		if flowErrors := r.OFBridge.DeleteFlowsByCookie(hashedName, math.MaxUint64); flowErrors != nil {
			log.Error(flowErrors, "failed to delete flows")
			return ctrl.Result{}, flowErrors
		}
	}

	var nsi *dpuservicev1.NodeServiceInterfaces
	if features.Gates.Enabled(features.NSIPathForSFC) {
		nsi, err = r.getSFCNodeServiceInterfaces(ctx)
		if err != nil {
			log.Error(err, "failed to get NodeServiceInterfaces")
			return ctrl.Result{}, err
		}
	}

	if err := r.reconcileFlows(ctx, sc, nsi, hashedName); err != nil {
		setFalseServiceChainReconciledCondition(err, sc)
		return ctrl.Result{}, err
	}

	setTrueServiceChainReconciledCondition(sc)
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ServiceChainReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	if err := mgr.GetCache().IndexField(ctx, &dpuservicev1.ServiceChain{}, serviceChainNodeNameKey, func(o client.Object) []string {
		sc := o.(*dpuservicev1.ServiceChain)
		if sc.Spec.Node == nil {
			return nil
		}
		return []string{*sc.Spec.Node}
	}); err != nil {
		return err
	}

	if err := mgr.GetCache().IndexField(ctx, &corev1.Pod{}, podNodeNameKey, func(o client.Object) []string {
		return []string{o.(*corev1.Pod).Spec.NodeName}
	}); err != nil {
		return err
	}

	p := predicate.NewPredicateFuncs(func(o client.Object) bool {
		if o.(*dpuservicev1.ServiceChain).Spec.Node == nil { // NodeName may not be set
			return false
		}
		return *o.(*dpuservicev1.ServiceChain).Spec.Node == r.NodeName
	})

	// Only Pods on this node carrying a serviceID label can back a "service" interface port.
	podPredicate := predicate.NewPredicateFuncs(func(o client.Object) bool {
		pod := o.(*corev1.Pod)
		if pod.Spec.NodeName != r.NodeName {
			return false
		}
		_, ok := pod.Labels[dpuservicev1.DPFServiceIDLabelKey]
		return ok
	})

	// Only this node's "sfc"-typed NodeServiceInterfaces can affect ServiceChains here.
	nsiPredicate := predicate.NewPredicateFuncs(func(o client.Object) bool {
		return isSFCNodeShard(o.(*dpuservicev1.NodeServiceInterfaces), r.NodeName)
	})

	r.resyncCh = make(chan event.GenericEvent)

	controllerBuilder := ctrl.NewControllerManagedBy(mgr).
		For(&dpuservicev1.ServiceChain{}, builder.WithPredicates(p)).
		Watches(&corev1.Pod{}, handler.EnqueueRequestsFromMapFunc(r.serviceChainsForPod), builder.WithPredicates(podPredicate)).
		WatchesRawSource(source.Channel(r.resyncCh, &handler.EnqueueRequestForObject{}))
	if features.Gates.Enabled(features.NSIPathForSFC) {
		controllerBuilder = controllerBuilder.Watches(
			&dpuservicev1.NodeServiceInterfaces{},
			handler.EnqueueRequestsFromMapFunc(r.mapNSIToServiceChains),
			builder.WithPredicates(nsiPredicate),
		)
	}
	return controllerBuilder.Complete(r)
}

// mapNSIToServiceChains enqueues this node's ServiceChains whose ports select an interface entry in the
// changed SFC NSI shard. EnqueueRequestsFromMapFunc runs this on both the old and new objects for updates,
// so a chain losing an entry is still enqueued via the old object and its stale port gets torn down.
func (r *ServiceChainReconciler) mapNSIToServiceChains(ctx context.Context, obj client.Object) []reconcile.Request {
	nsi, ok := obj.(*dpuservicev1.NodeServiceInterfaces)
	if !ok {
		return nil
	}

	scList := &dpuservicev1.ServiceChainList{}
	if err := r.List(ctx, scList, client.MatchingFields{serviceChainNodeNameKey: r.NodeName}); err != nil {
		ctrllog.FromContext(ctx).Error(err, "failed to list ServiceChains for NSI-triggered resync")
		return nil
	}

	reqs := make([]reconcile.Request, 0, len(scList.Items))
	for i := range scList.Items {
		sc := &scList.Items[i]
		if sc.Spec.Node == nil || *sc.Spec.Node != r.NodeName {
			continue
		}
		if serviceChainSelectsNSIEntry(sc, nsi) {
			reqs = append(reqs, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(sc)})
		}
	}
	return reqs
}

// serviceChainSelectsNSIEntry reports whether any of sc's port selectors match an interface entry in nsi
// within the chain's namespace.
func serviceChainSelectsNSIEntry(sc *dpuservicev1.ServiceChain, nsi *dpuservicev1.NodeServiceInterfaces) bool {
	for i := range nsi.Spec.Interfaces {
		entry := &nsi.Spec.Interfaces[i]
		entryNamespace, _ := entry.GetNamespacedName()
		if entryNamespace != sc.Namespace {
			continue
		}
		entryLabels := labels.Set(entry.Labels)
		for _, sw := range sc.Spec.Switches {
			for _, port := range sw.Ports {
				if labels.SelectorFromSet(port.ServiceInterface.MatchLabels).Matches(entryLabels) {
					return true
				}
			}
		}
	}
	return false
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
