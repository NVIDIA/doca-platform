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

package controller

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"maps"
	"strings"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/internal/utils"
	"github.com/nvidia/doca-platform/pkg/conditions"
	"github.com/nvidia/doca-platform/pkg/ecpf"
	"github.com/nvidia/doca-platform/pkg/ovsutils"

	"github.com/fluxcd/pkg/runtime/patch"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const (
	ServiceInterfaceController = "serviceinterfacecontroller"
	ServiceInterfaceFinalizer  = dpuservicev1.SvcDpuGroupName + "/ServiceInterface-finalizer"
	RequeueIntervalSuccess     = 20 * time.Second
	RequeueIntervalError       = 5 * time.Second
	SFCBridge                  = "br-sfc"
	OVNBridge                  = "br-ovn"
	InterfaceTypeDPDK          = "dpdk"
	InterfaceTypePatch         = "patch"

	// Patch port naming formats
	peerPatchNameFormat   = "p_%s_to_%s"      // used when PeerPatchName is specified
	patchPortHashedFormat = "p_%s_to_%s_%08x" // used for auto-generated names with hash
	patchPortUplinkFormat = "puplink%sto%s"   // used for OVN uplink patch ports

	// noopRemovalPhysicalAnnotationKey is the annotation key that controls whether a Physical Interface should be removed
	// from OVS or not. This is a workaround and must not be used in production.
	// TODO: Remove this once OVS issue is fixed
	noopRemovalPhysicalAnnotationKey = "svc.dpu.nvidia.com/noop-physical-removal"
)

// patchPortOptions holds the configuration options for creating a patch port.
type patchPortOptions struct {
	portName    string
	externalIDs map[string]string
}

// interfaceEntrySpec lets ServiceInterface and InterfaceEntry share the OVS primitives below.
type interfaceEntrySpec interface {
	GetInterfaceType() string
	GetPhysical() *dpuservicev1.Physical
	GetVF() *dpuservicev1.VF
	GetPF() *dpuservicev1.PF
	GetOVN() *dpuservicev1.OVN
	GetPatch() *dpuservicev1.PatchDef
	GetService() *dpuservicev1.ServiceDef
	GetAnnotations() map[string]string
}

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
	Scheme      *runtime.Scheme
	NodeName    string
	OVS         ovsutils.API
	ECPFManager ecpf.ECPFManager
}

// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=ServiceInterfaces,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=ServiceInterfaces/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=ServiceInterfaces/finalizers,verbs=update
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=serviceinterfaces,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=serviceinterfaces/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch

func AddPort(ctx context.Context, ovs ovsutils.API, portName string, ifaceExternalIDs, portExternalIDs map[string]string) error {
	if err := ovs.AddPort(ctx, ovsutils.PortConfig{
		BridgeName:    SFCBridge,
		Name:          portName,
		InterfaceType: InterfaceTypeDPDK,
	}); err != nil {
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

// getPatchPortNames generates OVS patch port names for connecting two bridges.
// Uses PeerPatchName when specified, otherwise derives a deterministic name from metadata.
// Returns the patch port name for brA and the corresponding peer port name for brB.
//
// Examples:
//   - With PeerPatchName "mypatch", brA="br-sfc", brB="br-ovn":
//     returns ("p_brsfc_to_mypatch", "mypatch")
//   - Without PeerPatchName, brA="br-sfc", brB="br-ovn", metadata hash 0xabcd1234:
//     returns ("p_brsfc_to_brovn_abcd1234", "p_brovn_to_brsfc_abcd1234")
func getPatchPortNames(spec interfaceEntrySpec, metadata, brA, brB string) (string, string) {
	strippedBrA := strings.ReplaceAll(brA, "-", "")
	strippedBrB := strings.ReplaceAll(brB, "-", "")

	if patch := spec.GetPatch(); patch != nil && patch.PeerPatchName != nil {
		peerPatchName := *patch.PeerPatchName
		patchName := fmt.Sprintf(peerPatchNameFormat, strippedBrA, peerPatchName)
		return patchName, peerPatchName
	}

	h := fnv.New32a()
	h.Write([]byte(metadata))
	interfaceNameHash := h.Sum32()

	brAPatchPortName := fmt.Sprintf(patchPortHashedFormat, strippedBrA, strippedBrB, interfaceNameHash)
	brBPatchPortName := fmt.Sprintf(patchPortHashedFormat, strippedBrB, strippedBrA, interfaceNameHash)
	return brAPatchPortName, brBPatchPortName
}

// getOVNPatchPortNames generates OVN patch port names for connecting two bridges.
// Returns the patch port name for brA and the corresponding peer port name for brB.
//
// Example:
//   - brA="br-sfc", brB="br-ovn":
//     returns ("puplinkbrsfctobrovn", "puplinkbrovntobrsfc")
func getOVNPatchPortNames(brA, brB string) (string, string) {
	strippedBrA := strings.ReplaceAll(brA, "-", "")
	strippedBrB := strings.ReplaceAll(brB, "-", "")

	brAPatchPortName := fmt.Sprintf(patchPortUplinkFormat, strippedBrA, strippedBrB)
	brBPatchPortName := fmt.Sprintf(patchPortUplinkFormat, strippedBrB, strippedBrA)
	return brAPatchPortName, brBPatchPortName
}

// AddPatchPort creates a bidirectional OVS patch port connection between two bridges.
// The peer patch port is added first to ensure the operation fails if the brB bridge does not exist.
//
// The function performs the following steps:
//  1. Creates a patch port on brB with the given peerPatchPort (without peer option)
//  2. Creates a patch port on brA with the given patchPort (without peer option)
//  3. Sets the peer option on peerPatchPort to reference patchPort
//  4. Sets the peer option on patchPort to reference peerPatchPort
//  5. Sets patchExternalIDs to patchPort for identification
//  6. Sets peerExternalIDs to peerPatchPort for identification
//
// Returns an error if any OVS operation fails; partial state may remain on failure.
func AddPatchPort(ctx context.Context, ovs ovsutils.API, brA, brB string, patchPort, peerPatchPort patchPortOptions) error {
	log := ctrllog.FromContext(ctx)
	log.Info("adding patch port", "brA", brA, "brB", brB, "patchPort", patchPort.portName, "peerPatchPort", peerPatchPort.portName)

	// Create patch port on brB first to fail fast if the peer bridge doesn't exist.
	if err := ovs.AddPort(ctx, ovsutils.PortConfig{
		BridgeName:    brB,
		Name:          peerPatchPort.portName,
		InterfaceType: InterfaceTypePatch,
	}); err != nil {
		return err
	}

	if err := ovs.AddPort(ctx, ovsutils.PortConfig{
		BridgeName:    brA,
		Name:          patchPort.portName,
		InterfaceType: InterfaceTypePatch,
	}); err != nil {
		return err
	}

	// Configure peer relationship after both ports exist to avoid netdev datapath errors.
	if err := ovs.SetIfaceOptions(ctx, peerPatchPort.portName, map[string]string{"peer": patchPort.portName}); err != nil {
		return err
	}

	if err := ovs.SetIfaceOptions(ctx, patchPort.portName, map[string]string{"peer": peerPatchPort.portName}); err != nil {
		return err
	}

	if err := ovs.SetIfaceExternalIDs(ctx, patchPort.portName, patchPort.externalIDs); err != nil {
		return err
	}

	if peerPatchPort.externalIDs != nil {
		if err := ovs.SetIfaceExternalIDs(ctx, peerPatchPort.portName, peerPatchPort.externalIDs); err != nil {
			return err
		}
	}

	return nil
}

// DeletePatchPorts removes a bidirectional OVS patch port connection between two bridges.
// This is the inverse operation of AddPatchPort.
//
// The function performs the following steps:
//  1. Deletes the patch port from brA with the given patchPort
//  2. Deletes the peer patch port from brB with the given patchPortPeer
//
// Returns an error if any OVS operation fails; partial state may remain on failure.
func DeletePatchPorts(ctx context.Context, ovs ovsutils.API, brA, brB string, patchPort, peerPatchPort string) error {
	log := ctrllog.FromContext(ctx)
	err := ovs.DelPort(ctx, brA, patchPort, nil)
	if err != nil {
		log.Info(fmt.Sprintf("failed to delete port %s", err.Error()))
		return err
	}
	err = ovs.DelPort(ctx, brB, peerPatchPort, nil)
	if err != nil {
		log.Info(fmt.Sprintf("failed to delete port %s", err.Error()))
		return err
	}
	return nil
}

func setFalseServiceInterfaceReconciledCondition(err error, si *dpuservicev1.ServiceInterface) {
	conditions.AddFalse(
		si,
		dpuservicev1.ServiceInterfaceReconciled,
		conditions.ReasonError,
		conditions.ConditionMessage(fmt.Sprintf("Error occurred: %v", err)),
	)
}

func setTrueServiceInterfaceReconciledCondition(si *dpuservicev1.ServiceInterface) {
	conditions.AddTrue(
		si,
		dpuservicev1.ServiceInterfaceReconciled,
	)
}

// isOnlyNotFoundErr reports whether err is nil or consists solely of NotFound errors, unwrapping any nested aggregate.
func isOnlyNotFoundErr(err error) bool {
	if err == nil {
		return true
	}
	if agg, ok := err.(kerrors.Aggregate); ok {
		for _, e := range agg.Errors() {
			if !isOnlyNotFoundErr(e) {
				return false
			}
		}
		return true
	}
	return apierrors.IsNotFound(err)
}

func FigureOutName(ctx context.Context, ecpfManager ecpf.ECPFManager, spec interfaceEntrySpec) (string, error) {
	var err error
	log := ctrllog.FromContext(ctx)
	portName := ""

	switch spec.GetInterfaceType() {
	case dpuservicev1.InterfaceTypePhysical:
		log.V(4).Info("matched on physical")
		portName = spec.GetPhysical().InterfaceName
	case dpuservicev1.InterfaceTypePF:
		log.V(4).Info("matched on pf")
		portName, err = ecpfManager.GetRepresentorForPFServiceInterface(spec.GetPF())
	case dpuservicev1.InterfaceTypeVF:
		log.V(4).Info("matched on vf")
		portName, err = ecpfManager.GetRepresentorForVFServiceInterface(spec.GetVF())
	case dpuservicev1.InterfaceTypeVLAN:
		log.V(4).Info("matched on vlan skipping")
		// TODO for MVP it is out of scope
		// revisit this once we do not have collisions on patches.
	case dpuservicev1.InterfaceTypeService:
		// service interfaces add/remove from ovs is handled part of CNI ADD/DEL flow
		log.V(4).Info("matched on service skipping")
	}

	if err != nil {
		return "", err
	}

	return portName, nil
}

func getOVNBridge(spec interfaceEntrySpec) string {
	ovnBridge := OVNBridge // set default value to "br-ovn"
	if ovn := spec.GetOVN(); ovn != nil {
		ovnBridge = ptr.Deref(ovn.ExternalBridge, OVNBridge)
	}
	return ovnBridge
}

func AddInterfacesToOvs(
	ctx context.Context,
	ovs ovsutils.API,
	ecpfManager ecpf.ECPFManager,
	spec interfaceEntrySpec,
	metadata string,
) error {
	switch spec.GetInterfaceType() {
	case dpuservicev1.InterfaceTypeOVN:
		return addOVNPatchPort(ctx, ovs, spec, metadata)
	case dpuservicev1.InterfaceTypePatch:
		return addPeerPatchPort(ctx, ovs, spec, metadata)
	}

	log := ctrllog.FromContext(ctx)
	portName, err := FigureOutName(ctx, ecpfManager, spec)
	if err != nil {
		log.Error(err, "failed to get port name")
		return err
	}
	if portName == "" {
		return nil
	}

	var typeExternalIDs map[string]string
	if spec.GetInterfaceType() == dpuservicev1.InterfaceTypePhysical {
		typeExternalIDs = map[string]string{ovsutils.DPFTypeKey: ovsutils.DPFTypePhysical}
	}
	if err := AddPort(ctx, ovs, portName, map[string]string{ovsutils.DPFIDKey: metadata}, typeExternalIDs); err != nil {
		log.Info(fmt.Sprintf("failed to add port: %s", err.Error()))
		return err
	}
	return nil
}

// addOVNPatchPort creates the SFC<->OVN bridge patch port pair for an OVN interface entry.
func addOVNPatchPort(ctx context.Context, ovs ovsutils.API, spec interfaceEntrySpec, metadata string) error {
	log := ctrllog.FromContext(ctx)
	log.V(4).Info("matched on interfaceType ovn", "metadata", metadata)
	externalBridge := getOVNBridge(spec)
	log.V(4).Info("ovn bridge", "name", externalBridge)

	patchPort := patchPortOptions{
		externalIDs: map[string]string{ovsutils.DPFIDKey: metadata},
	}
	peerPatchPort := patchPortOptions{
		externalIDs: nil,
	}
	patchPort.portName, peerPatchPort.portName = getOVNPatchPortNames(SFCBridge, externalBridge)
	if err := AddPatchPort(ctx, ovs, SFCBridge, externalBridge, patchPort, peerPatchPort); err != nil {
		log.Error(err, "failed to add patch port between bridges", "brA", SFCBridge, "brB", externalBridge, "patchPort", patchPort.portName, "patchPortPeer", peerPatchPort.portName)
		return err
	}
	return nil
}

// addPeerPatchPort creates the SFC<->peer bridge patch port pair for a Patch interface entry.
func addPeerPatchPort(ctx context.Context, ovs ovsutils.API, spec interfaceEntrySpec, metadata string) error {
	log := ctrllog.FromContext(ctx)
	log.V(4).Info("matched on interfaceType patch", "metadata", metadata)
	patch := spec.GetPatch()
	if patch == nil || patch.PeerBridge == "" {
		log.Error(fmt.Errorf("peer bridge is not set or is empty"), "failed to add patch port between bridges", "brA", SFCBridge)
		return fmt.Errorf("peer bridge is not set or is empty")
	}

	peerBridge := patch.PeerBridge
	log.V(4).Info("peer bridge", "name", peerBridge)

	// User should not set the reserved dpf-id
	if _, exists := patch.PeerExternalIDs[ovsutils.DPFIDKey]; exists {
		log.Info("External ID is reserved and managed internally, it cannot be set by users", "key", ovsutils.DPFIDKey)
		return fmt.Errorf("external ID %s is a reserved and managed internally, it cannot be set by users", ovsutils.DPFIDKey)
	}

	patchPort := patchPortOptions{
		externalIDs: map[string]string{ovsutils.DPFIDKey: metadata},
	}
	peerPatchPort := patchPortOptions{
		externalIDs: make(map[string]string, len(patch.PeerExternalIDs)+1),
	}
	maps.Copy(peerPatchPort.externalIDs, patch.PeerExternalIDs)
	peerPatchPort.externalIDs[ovsutils.DPFIDKey] = metadata

	patchPort.portName, peerPatchPort.portName = getPatchPortNames(spec, metadata, SFCBridge, peerBridge)
	if err := AddPatchPort(ctx, ovs, SFCBridge, peerBridge, patchPort, peerPatchPort); err != nil {
		log.Error(err, "failed to add patch port between bridges", "patchPort", patchPort.portName, "peerPatchPort", peerPatchPort.portName)
		return err
	}
	return nil
}

func DeleteInterfacesFromOvs(
	ctx context.Context,
	ovs ovsutils.API,
	ecpfManager ecpf.ECPFManager,
	spec interfaceEntrySpec,
	metadata string,
) error {
	log := ctrllog.FromContext(ctx)
	log.V(4).Info("deleteInterfacesFromOvs")

	// Skip Physical Interface removal from OVS if annotation is set
	if spec.GetInterfaceType() == dpuservicev1.InterfaceTypePhysical {
		if _, ok := spec.GetAnnotations()[noopRemovalPhysicalAnnotationKey]; ok {
			log.Info("skipping physical interface removal from OVS since annotation exists")
			return nil
		}
	}

	switch spec.GetInterfaceType() {
	case dpuservicev1.InterfaceTypeOVN:
		return deleteOVNPatchPort(ctx, ovs, spec, metadata)
	case dpuservicev1.InterfaceTypePatch:
		return deletePeerPatchPort(ctx, ovs, spec, metadata)
	}

	portName, err := FigureOutName(ctx, ecpfManager, spec)
	if err != nil {
		// The device representor may have disappeared or may never have been added
		// (for example, when creation referenced a missing device representor).
		// Fall back to the stable dpf-id stored in OVS so an unresolved representor
		// does not block finalizer removal.
		portName, err = recoverPortNameForDeletion(ctx, ovs, metadata, err)
		if err != nil {
			log.Error(err, "failed to recover port name for deletion")
			return err
		}
	}
	if portName == "" {
		return nil
	}
	if err := ovs.DelPort(ctx, SFCBridge, portName, nil); err != nil {
		log.Error(err, "failed to delete port")
		return err
	}
	return nil
}

// recoverPortNameForDeletion attempts to identify an OVS interface by its ownership metadata after
// live representor resolution has failed. An empty name with no error means no owned OVS interface
// exists and cleanup is already complete.
func recoverPortNameForDeletion(ctx context.Context, ovs ovsutils.API, metadata string, resolveErr error) (string, error) {
	iface, lookupErr := ovs.GetIfaceWithExternalIDs(ctx, map[string]string{
		ovsutils.DPFIDKey: metadata,
	})

	switch {
	case lookupErr == nil:
		ctrllog.FromContext(ctx).Info(
			"using owned OVS interface after port name resolution failed",
			"portName", iface.Name,
			"dpfID", metadata,
			"resolveError", resolveErr,
		)
		return iface.Name, nil
	case errors.Is(lookupErr, ovsutils.ErrIfaceNotFound):
		ctrllog.FromContext(ctx).Info(
			"owned OVS interface is already absent",
			"dpfID", metadata,
			"resolveError", resolveErr,
		)
		return "", nil
	default:
		return "", errors.Join(
			fmt.Errorf("resolve interface name: %w", resolveErr),
			fmt.Errorf("find interface by dpf-id: %w", lookupErr),
		)
	}
}

// deleteOVNPatchPort removes the SFC<->OVN bridge patch port pair for an OVN interface entry.
func deleteOVNPatchPort(ctx context.Context, ovs ovsutils.API, spec interfaceEntrySpec, metadata string) error {
	log := ctrllog.FromContext(ctx)
	log.V(4).Info("matched on interfaceType ovn", "metadata", metadata)
	externalBridge := getOVNBridge(spec)
	log.V(4).Info("ovn bridge", "name", externalBridge)

	patchPortName, patchPortPeerName := getOVNPatchPortNames(SFCBridge, externalBridge)
	if err := DeletePatchPorts(ctx, ovs, SFCBridge, externalBridge, patchPortName, patchPortPeerName); err != nil {
		log.Error(err, "failed to delete patch port between bridges", "patchPortName", patchPortName, "patchPortPeerName", patchPortPeerName)
		return err
	}
	return nil
}

// deletePeerPatchPort removes the SFC<->peer bridge patch port pair for a Patch interface entry.
func deletePeerPatchPort(ctx context.Context, ovs ovsutils.API, spec interfaceEntrySpec, metadata string) error {
	log := ctrllog.FromContext(ctx)
	log.V(4).Info("matched on interfaceType patch", "metadata", metadata)
	patch := spec.GetPatch()
	if patch == nil || patch.PeerBridge == "" {
		log.Error(fmt.Errorf("peer bridge is not set or is empty"), "failed to delete patch port between bridges", "brA", SFCBridge)
		return fmt.Errorf("peer bridge is not set or is empty")
	}

	peerBridge := patch.PeerBridge
	log.V(4).Info("peer bridge", "name", peerBridge)
	patchPortName, patchPortPeerName := getPatchPortNames(spec, metadata, SFCBridge, peerBridge)
	if err := DeletePatchPorts(ctx, ovs, SFCBridge, peerBridge, patchPortName, patchPortPeerName); err != nil {
		log.Error(err, "failed to delete patch port between bridges", "patchPortName", patchPortName, "patchPortPeerName", patchPortPeerName)
		return err
	}
	return nil
}

// reconcileDelete handles the delete reconciliation loop
func (r *ServiceInterfaceReconciler) reconcileDelete(ctx context.Context, serviceInterface *dpuservicev1.ServiceInterface, req ctrl.Request) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx)
	log.Info("Reconciling delete")
	err := DeleteInterfacesFromOvs(ctx, r.OVS, r.ECPFManager, serviceInterface, req.NamespacedName.String())
	if err != nil {
		log.Error(err, "failed to delete DeleteInterfacesFromOvs")
		setFalseServiceInterfaceReconciledCondition(err, serviceInterface)
		return requeueError()
	}

	// If there are no associated applications remove the finalizer
	log.Info("Removing finalizer")
	controllerutil.RemoveFinalizer(serviceInterface, ServiceInterfaceFinalizer)
	return requeueDone()
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
		); err != nil && !isOnlyNotFoundErr(err) {
			// Removing the last finalizer above can race the object's deletion, so a NotFound here is expected, not a failure.
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
	}()

	if !serviceInterface.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, serviceInterface, req)
	}

	// Add finalizer if not set.
	if !controllerutil.ContainsFinalizer(serviceInterface, ServiceInterfaceFinalizer) {
		controllerutil.AddFinalizer(serviceInterface, ServiceInterfaceFinalizer)
		return ctrl.Result{}, nil
	}

	err := AddInterfacesToOvs(ctx, r.OVS, r.ECPFManager, serviceInterface, req.NamespacedName.String())
	if err != nil {
		log.Info("failed to add AddInterfacesToOvs")
		setFalseServiceInterfaceReconciledCondition(err, serviceInterface)
		return requeueError()
	}

	setTrueServiceInterfaceReconciledCondition(serviceInterface)
	return requeueSuccess()
}

// SetupWithManager sets up the controller with the Manager.
func (r *ServiceInterfaceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := utils.SetupServiceInterfaceNodeIndexer(context.Background(), mgr); err != nil {
		return err
	}

	siWithoutVirtualNetworkPredicate := predicate.NewPredicateFuncs(func(o client.Object) bool {
		si, ok := o.(*dpuservicev1.ServiceInterface)
		if !ok {
			return false
		}

		// controller reconcile only for local node
		if si.Spec.Node == nil || *si.Spec.Node != r.NodeName {
			return false
		}

		return !si.HasVirtualNetwork()
	})

	return ctrl.NewControllerManagedBy(mgr).
		For(
			&dpuservicev1.ServiceInterface{},
			builder.WithPredicates(siWithoutVirtualNetworkPredicate),
		).
		Complete(r)
}
