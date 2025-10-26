/*
COPYRIGHT 2025 NVIDIA

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
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/internal/digest"
	nvipamv1 "github.com/nvidia/doca-platform/third_party/api/nvipam/api/v1alpha1"

	multusclient "gopkg.in/k8snetworkplumbingwg/multus-cni.v4/pkg/k8sclient"
	multustypes "gopkg.in/k8snetworkplumbingwg/multus-cni.v4/pkg/types"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// PodIpamReconciler reconciles a Pod object
type PodIpamReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

const (
	podIpamControllerName   = "podIpamcontroller"
	NetworkAttachmentAnnot  = "k8s.v1.cni.cncf.io/networks"
	NetworkDigestAnnotation = "dpu.nvidia.com/network-digest"
	ServiceInterfaceNodeKey = "spec.node"

	// The invalid network is used to hold the readiness of the pod until the MTU is injected in the CNI args correctly.
	// This network is added by the DPUService controller on all DPUServices that have an interface. Depending on the
	// order of object creation and in conjunction with the pod restarter, converging to the correct annotation may take
	// several pod recreations.
	InvalidNetworkName      = "invalid-network"
	InvalidNetworkNamespace = "invalid-namespace"
	InvalidNetworkInterface = "invalid-interface"
	InvalidNetwork          = InvalidNetworkNamespace + "/" + InvalidNetworkName
)

var (
	reconcileRetryTime = 30 * time.Second
)

// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch;update
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=servicechains,verbs=get;list;watch
// +kubebuilder:rbac:groups=nv-ipam.nvidia.com,resources=ippools,verbs=get;list;watch
// +kubebuilder:rbac:groups=nv-ipam.nvidia.com,resources=cidrpools,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;update;patch

func (r *PodIpamReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("Reconciling")
	pod := &corev1.Pod{}
	if err := r.Client.Get(ctx, req.NamespacedName, pod); err != nil {
		if apierrors.IsNotFound(err) {
			// Return early if the object is not found.
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	networks, err := GetPodNetworks(pod)
	if err != nil {
		return ctrl.Result{}, err
	}

	if shouldSkipPod(pod, networks) {
		return ctrl.Result{}, nil
	}

	populatedNetworks, changed, err := getPopulatedNetworks(ctx, r.Client, pod, networks, nil)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get populated networks: %w", err)
	}

	podHasOnlyVirtualNetworks, err := isPodUsingOnlyVirtualNetworks(ctx, r.Client, pod)
	if err != nil {
		return ctrl.Result{}, err
	}

	if !changed && !podHasOnlyVirtualNetworks {
		// We need to enqueue again because it might be that the Pod is still in Pending due to IPAM missing. We do not
		// explicitly return error if we can't find IPAM Pools because we don't know if the Pod should use IPAM (we would
		// need to implement logic to understand that via the NAD).
		return reconcile.Result{RequeueAfter: reconcileRetryTime}, nil
	}

	// patch the pod with network annotation and digest
	if err := patchPodWithoutInvalidNetworkAnnotationAndWithDigestAnnotation(ctx, r.Client, pod, populatedNetworks); err != nil {
		log.Error(err, "Error patching pod without invalid network annotation and digest", "pod", req.NamespacedName)
		return reconcile.Result{}, fmt.Errorf("error while patching pod without invalid network annotation and digest: %w", err)
	}

	// Since this pod is connected to a virtual network, we can return early and not requeue since
	// we do not expect it to use NVIPAM.
	if podHasOnlyVirtualNetworks {
		log.Info("Pod is using virtual network, removing invalid network annotation", "pod", pod.Name)
		return reconcile.Result{}, nil
	}

	// We need to enqueue again because it might be that the Pod is still in Pending due to IPAM missing. We do not
	// explicitly return error if we can't find IPAM Pools because we don't know if the Pod should use IPAM (we would
	// need to implement logic to understand that via the NAD).
	return reconcile.Result{RequeueAfter: reconcileRetryTime}, nil
}

// getPopulatedNetworks returns the populated networks with the settings the Pod IPAM Injector applies.
// If serviceChain is specified, then the networks are calculated for the given serviceChain, otherwise the networks are
// populated using all the serviceChains available in the cluster and relevant to this Pod.
func getPopulatedNetworks(ctx context.Context,
	c client.Client,
	pod *corev1.Pod,
	networks []*multustypes.NetworkSelectionElement,
	serviceChain *dpuservicev1.ServiceChain) ([]*multustypes.NetworkSelectionElement, bool, error) {

	// Get the settings for each of the network interfaces
	settings, err := getSettingsForNetworkInterface(ctx, c, pod, networks, serviceChain)
	if err != nil {
		return networks, false, err
	}

	// mutateNetworksWithSettings mutates the networks with relevant information for each interface
	populatedNetworks, changed := mutateNetworksWithSettings(networks, settings)
	return populatedNetworks, changed, nil
}

// networkSettings contains the settings needed to be added in the network annotation after discovering them
type networkSettings struct {
	// IPAMPoolName is the NVIPAM Pool name associated with this network
	IPAMPoolName *string
	// IPAMPoolType is the NVIPAM Pool type associated with this network
	IPAMPoolType *string
	// IPAMAssignDefaultGateway controls whether the allocateDefaultGateway should be set on the network
	IPAMAssignDefaultGateway *bool
	// MTU is the MTU that should be configured on the network
	MTU *int
}

// getSettingsForNetworkInterface returns a map that contains the information needed so that the annotation can be updated
// for each interface.
func getSettingsForNetworkInterface(ctx context.Context, c client.Client, pod *corev1.Pod, networks []*multustypes.NetworkSelectionElement, serviceChain *dpuservicev1.ServiceChain) (map[string]*networkSettings, error) {
	// Get relevant interfaces
	networkSettingsForInterface := initializeNetworkSettingsForInterface(networks)

	// In case no network has a valid interface requested, we can just skip
	if len(networkSettingsForInterface) == 0 {
		return networkSettingsForInterface, nil
	}

	// Gather all the information needed from ServiceChains
	serviceChainSettingsForInterface, err := getServiceChainSettingsForInterface(ctx, c, pod, networkSettingsForInterface, serviceChain)
	if err != nil {
		return nil, fmt.Errorf("failed to get information from servicechains related to the pod interfaces: %w", err)
	}

	// Gather all the information needed from NVIPAM Pool objects
	poolSettingsForInterface, err := getPoolSettingsForInterface(ctx, c, serviceChainSettingsForInterface)
	if err != nil {
		return nil, fmt.Errorf("failed to get information from NVIPAM pools related to the pod interfaces: %w", err)
	}

	// Populate the settings for each network interface
	for interfaceName := range networkSettingsForInterface {
		// We should not populate the networkSettings if no info found in any of the underlying resources
		var s *networkSettings
		if v, ok := serviceChainSettingsForInterface[interfaceName]; ok {
			s = &networkSettings{}
			s.MTU = &v.ServiceMTU
			if v.IPAM != nil {
				s.IPAMAssignDefaultGateway = v.IPAM.DefaultGateway
			}
		}
		if v, ok := poolSettingsForInterface[interfaceName]; ok {
			if s == nil {
				s = &networkSettings{}
			}
			s.IPAMPoolName = &v.PoolName
			s.IPAMPoolType = &v.PoolType
		}
		networkSettingsForInterface[interfaceName] = s
	}

	return networkSettingsForInterface, nil
}

// initializeNetworkSettingsForInterface returns a map that has as keys the interfaces we need to process and nil values.
// Values must be populated from another function after the necessary info is discovered.
func initializeNetworkSettingsForInterface(networks []*multustypes.NetworkSelectionElement) map[string]*networkSettings {
	settingsForNetworkInterface := make(map[string]*networkSettings)
	for _, network := range networks {
		if network.InterfaceRequest == InvalidNetworkInterface {
			continue
		}
		if network.InterfaceRequest == "" {
			continue
		}
		settingsForNetworkInterface[network.InterfaceRequest] = nil
	}
	return settingsForNetworkInterface
}

// serviceChainSettings contains information gathered from a ServiceChain object and is needed to process the network
// annotation for an interface
type serviceChainSettings struct {
	// IPAM is the IPAM object that is part of the port referencing the interface in a ServiceChain switch
	IPAM *dpuservicev1.IPAM
	// ServiceMTU is the MTU defined in the ServiceChain switch which the interface is part of
	ServiceMTU int
}

// getServiceChainSettingsForInterface returns a map that has as keys the interface requested by the pod and as value info
// that is needed to populate the network annotation from the servicechain object
// If serviceChain is specified, then the settings are calculated for the given serviceChain, otherwise all of the serviceChains
// in the cluster are analyzed.
func getServiceChainSettingsForInterface(ctx context.Context, c client.Client, pod *corev1.Pod, networkSettingsForInterface map[string]*networkSettings, serviceChain *dpuservicev1.ServiceChain) (map[string]serviceChainSettings, error) {
	serviceChainSettingsForInterface := make(map[string]serviceChainSettings)
	serviceChains := &dpuservicev1.ServiceChainList{}
	if serviceChain != nil {
		serviceChains.Items = []dpuservicev1.ServiceChain{*serviceChain}
	} else {
		if err := c.List(ctx, serviceChains, client.InNamespace(pod.Namespace)); err != nil {
			return nil, fmt.Errorf("failed to list servicechains: %w", err)
		}
	}

	for _, serviceChain := range serviceChains.Items {
		if serviceChain.Spec.Node == nil || *serviceChain.Spec.Node != pod.Spec.NodeName {
			continue
		}

		for _, sw := range serviceChain.Spec.Switches {
			for _, port := range sw.Ports {
				svcIfc, err := getServiceInterfaceWithLabels(ctx, c, pod.Spec.NodeName, pod.Namespace, port.ServiceInterface.MatchLabels)
				if err != nil {
					return nil, fmt.Errorf("failed to get serviceInterface for chain. %w", err)
				}

				// We only care about interfaces of type service since such are attached to the Pods
				if svcIfc.Spec.InterfaceType != dpuservicev1.InterfaceTypeService {
					continue
				}

				// If the pod labels do not have a label that matches the serviceID of the interface in question, we
				// continue as the interface is not relevant to this pod.
				if !podMatchLabels(pod, map[string]string{dpuservicev1.DPFServiceIDLabelKey: svcIfc.Spec.Service.ServiceID}) {
					continue
				}

				// If the interface cannot be found in the settingsForNetworkInterface map which is the source of truth
				// for the interfaces we need to take action for, then we continue as the interface is not relevant to
				// this pod.
				if _, ok := networkSettingsForInterface[svcIfc.Spec.Service.InterfaceName]; !ok {
					continue
				}

				// TODO: check if this is correct and should be checked here instead of higher level controllers
				if _, ok := serviceChainSettingsForInterface[svcIfc.Spec.Service.InterfaceName]; ok {
					return nil, fmt.Errorf("interface %s is part of 2 switches, invalid configuration", svcIfc.Spec.Service.InterfaceName)
				}

				serviceChainSettingsForInterface[svcIfc.Spec.Service.InterfaceName] = serviceChainSettings{
					IPAM:       port.ServiceInterface.IPAM,
					ServiceMTU: *sw.ServiceMTU,
				}
			}
		}
	}

	return serviceChainSettingsForInterface, nil
}

// poolSettings contains information gathered from a NVIPAM Pool object and is needed to process the network
// annotation for an interface
type poolSettings struct {
	// PoolName is the name of the NVIPAM object
	PoolName string
	// PoolType is the type of the NVIPAM object
	PoolType string
}

// getpoolSettingsForInterface returns a map that has as keys the interface requested by the pod and as value info
// that is needed to populate the network annotation from the NVIPAM related object
func getPoolSettingsForInterface(ctx context.Context, c client.Client, serviceChainSettingsForInterface map[string]serviceChainSettings) (map[string]poolSettings, error) {
	poolSettingsForInterface := make(map[string]poolSettings)
	for interfaceName, serviceChainSettings := range serviceChainSettingsForInterface {
		// If no IPAM is specified, then this interface doesn't require IPAM so we can skip it
		if serviceChainSettings.IPAM == nil {
			continue
		}

		poolName, poolType, err := getNVIPAMPoolByMatchLabels(ctx, c, serviceChainSettings.IPAM)
		if err != nil {
			return nil, err
		}

		poolSettingsForInterface[interfaceName] = poolSettings{
			PoolName: poolName,
			PoolType: poolType,
		}

	}
	return poolSettingsForInterface, nil
}

// shouldSkipPod determines if a pod should be skipped during reconciliation.
// It returns true if any of the following conditions are met:
// - The pod has no network annotation
// - The pod is being deleted (DeletionTimestamp is not zero)
// - The pod has not been assigned to a node (NodeName is empty)
// - The pod is not in Pending phase
// - The pod does not have an invalid network annotation
func shouldSkipPod(pod *corev1.Pod, networks []*multustypes.NetworkSelectionElement) bool {
	// Check pod state conditions
	isPodMarkedForDeletion := pod.ObjectMeta.DeletionTimestamp != nil && !pod.ObjectMeta.DeletionTimestamp.IsZero()
	if isPodMarkedForDeletion || pod.Spec.NodeName == "" || pod.Status.Phase != corev1.PodPending {
		return true
	}

	// Check network conditions
	if len(networks) == 0 || !HasInvalidNetwork(networks) {
		return true
	}

	return false
}

// mutateNetworksWithSettings mutates the networks with relevant information for each interface
func mutateNetworksWithSettings(networks []*multustypes.NetworkSelectionElement, settings map[string]*networkSettings) ([]*multustypes.NetworkSelectionElement, bool) {
	var changed bool
	for i, net := range networks {
		if s, ok := settings[net.InterfaceRequest]; ok && s != nil {
			cniArgs := make(map[string]interface{})
			if s.IPAMAssignDefaultGateway != nil {
				cniArgs["allocateDefaultGateway"] = *s.IPAMAssignDefaultGateway
			}
			if s.IPAMPoolName != nil {
				cniArgs["poolNames"] = []string{*s.IPAMPoolName}
			}
			if s.IPAMPoolType != nil {
				cniArgs["poolType"] = *s.IPAMPoolType
			}
			if s.MTU != nil {
				cniArgs["mtu"] = *s.MTU
			}
			networks[i].CNIArgs = &cniArgs
			changed = true
		}
	}
	return networks, changed
}

// patchPodWithoutInvalidNetworkAnnotationAndWithDigestAnnotation updates a Pod's network and digest annotations with the given networks.
func patchPodWithoutInvalidNetworkAnnotationAndWithDigestAnnotation(ctx context.Context, c client.Client, pod *corev1.Pod, networks []*multustypes.NetworkSelectionElement) error {
	filteredNetworks := filterInvalidNetwork(networks)

	// Calculate digest from filtered networks (same as restart controller)
	digestValue := calculateNetworkDigest(filteredNetworks)

	// Marshal the filtered networks for the annotation
	j, err := json.Marshal(filteredNetworks)
	if err != nil {
		return fmt.Errorf("error while marshaling json: %w", err)
	}

	// Prepare the pod for patching
	if pod.Annotations == nil {
		pod.Annotations = map[string]string{}
	}
	pod.Annotations[NetworkAttachmentAnnot] = string(j)
	pod.Annotations[NetworkDigestAnnotation] = digestValue
	pod.ObjectMeta.ManagedFields = nil
	pod.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Pod"))

	return c.Patch(ctx, pod, client.Apply, client.ForceOwnership, client.FieldOwner(podIpamControllerName))
}

// HasInvalidNetwork returns true if the pod has an invalid network annotation
// It checks if the pod has an invalid network annotation by comparing the length of the filtered networks to the length of the original networks.
// If the length of the filtered networks is not equal to the length of the original networks, then the pod has an invalid network annotation.
func HasInvalidNetwork(networks []*multustypes.NetworkSelectionElement) bool {
	return len(filterInvalidNetwork(networks)) != len(networks)
}

func filterInvalidNetwork(networks []*multustypes.NetworkSelectionElement) []*multustypes.NetworkSelectionElement {
	return slices.Collect(func(yield func(*multustypes.NetworkSelectionElement) bool) {
		for _, network := range networks {
			if network.Name != InvalidNetworkName {
				if !yield(network) {
					return
				}
			}
		}
	})
}

// getServiceInterfaceWithLabels returns ServiceInterface in given namespace that belongs to current node with given labels. if more than one or none matches, error out.
func getServiceInterfaceWithLabels(ctx context.Context, c client.Client, nodeName string, namespace string, lbls map[string]string) (*dpuservicev1.ServiceInterface, error) {
	//TODO(adrianc): this needs to be moved to a common place as we need the same thing in sfc-controller
	sil := &dpuservicev1.ServiceInterfaceList{}
	listOpts := []client.ListOption{
		client.MatchingLabelsSelector{Selector: labels.SelectorFromSet(labels.Set(lbls))},
		client.InNamespace(namespace),
	}
	if err := c.List(ctx, sil, listOpts...); err != nil {
		return nil, err
	}

	// filter out serviceInterfaces not on this node
	matching := make([]*dpuservicev1.ServiceInterface, 0, len(sil.Items))
	for i := range sil.Items {
		if sil.Items[i].Spec.Node == nil || *sil.Items[i].Spec.Node != nodeName {
			continue
		}
		matching = append(matching, &sil.Items[i])
	}

	if len(matching) == 0 {
		return nil, fmt.Errorf("no serviceInterface in namespace(%s) matching labels(%v) on node(%s) found", namespace, lbls, nodeName)
	}

	if len(matching) > 1 {
		return nil, fmt.Errorf("expected only one serviceInterface in namespace(%s) to match labels(%v) on node(%s). found %d",
			namespace, lbls, nodeName, len(matching))
	}

	return matching[0], nil
}

// podMatchLabels returns true if non empty lbls match non empty pod.Labels. returns false otherwise
func podMatchLabels(pod *corev1.Pod, lbls map[string]string) bool {
	if len(lbls) == 0 || len(pod.Labels) == 0 {
		return false
	}

	selector := labels.SelectorFromSet(labels.Set(lbls))
	return selector.Matches(labels.Set(pod.Labels))
}

// GetPodNetworks returns the secondary networks for the given pods
func GetPodNetworks(pod *corev1.Pod) ([]*multustypes.NetworkSelectionElement, error) {
	networks, err := multusclient.GetPodNetwork(pod)
	if err != nil {
		if _, ok := err.(*multusclient.NoK8sNetworkError); ok {
			return nil, nil
		}
		return nil, err
	}
	return networks, nil
}

func getNVIPAMPoolByMatchLabels(ctx context.Context, c client.Client, ipam *dpuservicev1.IPAM) (string, string, error) {
	log := log.FromContext(ctx)
	listOptions := client.MatchingLabels(ipam.MatchLabels)
	ipPoolList := &nvipamv1.IPPoolList{}
	if err := c.List(ctx, ipPoolList, &listOptions); err != nil {
		return "", "", err
	}
	if len(ipPoolList.Items) > 0 {
		if len(ipPoolList.Items) > 1 {
			log.Info("Service IPAM MatchLabels matched more than one IPPool", "labels", ipam.MatchLabels)
		}
		return ipPoolList.Items[0].Name, strings.ToLower(nvipamv1.IPPoolKind), nil
	}
	cidrPoolList := &nvipamv1.CIDRPoolList{}
	if err := c.List(ctx, cidrPoolList, &listOptions); err != nil {
		return "", "", err
	}
	if len(cidrPoolList.Items) > 0 {
		if len(cidrPoolList.Items) > 1 {
			log.Info("Service IPAM MatchLabels matched more than one CIDRPool", "labels", ipam.MatchLabels)
		}
		return cidrPoolList.Items[0].Name, strings.ToLower(nvipamv1.CIDRPoolKind), nil
	}
	// No IpPool/CidrPool found requeuing
	return "", "", fmt.Errorf("no IPPool or CIDRPool found for labels %v", ipam.MatchLabels)
}

// SetupWithManager sets up the controller with the Manager.
func (r *PodIpamReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Pod{}).
		Complete(r)
}

// CalculatePodNetworkDigest calculates the digest of the current network configuration for the given pod
// not including the invalid network annotation that might exist in the Pod networks
func CalculatePodNetworkDigest(ctx context.Context, c client.Client, pod *corev1.Pod, currentNetworks []*multustypes.NetworkSelectionElement) (string, error) {
	if len(currentNetworks) == 0 {
		// No networks to process
		return "", nil
	}

	// Get the expected populated networks based on current ServiceChain configuration
	expectedNetworks, _, err := getPopulatedNetworks(ctx, c, pod, currentNetworks, nil)
	if err != nil {
		return "", fmt.Errorf("failed to get populated networks: %w", err)
	}

	return calculateNetworkDigest(expectedNetworks), nil
}

func calculateNetworkDigest(networks []*multustypes.NetworkSelectionElement) string {
	filteredNetworks := filterInvalidNetwork(networks)
	return digest.FromObjects(filteredNetworks).String()
}

// isPodUsingOnlyVirtualNetworks checks if the pod is using a virtual network.
// Note: A pod can have multiple ServiceInterfaces with the same serviceID (one per interface),
// so we need to check if ANY of them has a virtual network.
func isPodUsingOnlyVirtualNetworks(ctx context.Context, c client.Client, pod *corev1.Pod) (bool, error) {
	serviceInterfaceList := &dpuservicev1.ServiceInterfaceList{}

	// Not using the getServiceInterfaceWithLabels function here because we need to check all the service interfaces
	if err := c.List(ctx, serviceInterfaceList, client.InNamespace(pod.Namespace), client.MatchingFields{ServiceInterfaceNodeKey: pod.Spec.NodeName}); err != nil {
		return false, err
	}
	if len(serviceInterfaceList.Items) == 0 {
		return false, nil
	}

	isUsingVirtualNetwork := false

	// Check if any of the service interfaces has a virtual network
	// if it uses only interfaces with virtual network, then it is using a virtual network.
	for i := range serviceInterfaceList.Items {
		if serviceInterfaceList.Items[i].HasVirtualNetwork() {
			isUsingVirtualNetwork = true
		} else {
			return false, nil
		}
	}

	return isUsingVirtualNetwork, nil
}
