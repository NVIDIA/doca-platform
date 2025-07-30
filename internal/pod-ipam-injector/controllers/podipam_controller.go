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
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
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
	ipamPoolNamesParam     = "poolNames"
	podIpamControllerName  = "podIpamcontroller"
	networkAttachmentAnnot = "k8s.v1.cni.cncf.io/networks"

	// The invalid network is used to hold the readiness of the pod until the MTU is injected in the CNI args correctly.
	// This network is added by the DPUService controller on all DPUServices that have an interface. Depending on the
	// order of object creation and in conjunction with the pod restarter, converging to the correct annotation may take
	// several pod recreations.
	invalidNetworkName      = "invalid-network"
	invalidNetworkNamespace = "invalid-namespace"
	invalidNetworkInterface = "invalid-interface"
	invalidNetwork          = invalidNetworkNamespace + "/" + invalidNetworkName
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

	networks, err := getPodNetworks(pod)
	if err != nil {
		return ctrl.Result{}, err
	}

	if shouldSkipPod(pod, networks) {
		return ctrl.Result{}, nil
	}

	// Get the settings for each of the network interfaces
	settings, err := getSettingsForNetworkInterface(ctx, r.Client, pod, networks)
	if err != nil {
		return ctrl.Result{}, err
	}

	// update pod network annotation
	populatedNetworks, changed := mutateNetworksWithSettings(networks, settings)
	if !changed {
		// We need to enqueue again because it might be that the Pod is still in Pending due to IPAM missing. We do not
		// explicitly return error if we can't find IPAM Pools because we don't know if the Pod should use IPAM (we would
		// need to implement logic to understand that via the NAD).
		return reconcile.Result{RequeueAfter: reconcileRetryTime}, nil
	}

	// patch the pod
	if err := patchPodWithoutInvalidNetworkAnnotation(ctx, r.Client, pod, populatedNetworks); err != nil {
		return reconcile.Result{}, fmt.Errorf("error while patching pod without invalid network annotation: %w", err)
	}

	// We need to enqueue again because it might be that the Pod is still in Pending due to IPAM missing. We do not
	// explicitly return error if we can't find IPAM Pools because we don't know if the Pod should use IPAM (we would
	// need to implement logic to understand that via the NAD).
	return reconcile.Result{RequeueAfter: reconcileRetryTime}, nil
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
func getSettingsForNetworkInterface(ctx context.Context, c client.Client, pod *corev1.Pod, networks []*multustypes.NetworkSelectionElement) (map[string]*networkSettings, error) {
	// Get relevant interfaces
	networkSettingsForInterface := initializeNetworkSettingsForInterface(networks)

	// In case no network has a valid interface requested, we can just skip
	if len(networkSettingsForInterface) == 0 {
		return networkSettingsForInterface, nil
	}

	// Gather all the information needed from ServiceChains
	serviceChainSettingsForInterface, err := getServiceChainSettingsForInterface(ctx, c, pod, networkSettingsForInterface)
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
		if network.InterfaceRequest == invalidNetworkInterface {
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
func getServiceChainSettingsForInterface(ctx context.Context, c client.Client, pod *corev1.Pod, networkSettingsForInterface map[string]*networkSettings) (map[string]serviceChainSettings, error) {
	serviceChainSettingsForInterface := make(map[string]serviceChainSettings)
	serviceChains := &dpuservicev1.ServiceChainList{}
	if err := c.List(ctx, serviceChains, client.InNamespace(pod.Namespace)); err != nil {
		return nil, fmt.Errorf("failed to list servicechains: %w", err)
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
func shouldSkipPod(pod *corev1.Pod, networks []*multustypes.NetworkSelectionElement) bool {
	if !pod.ObjectMeta.DeletionTimestamp.IsZero() ||
		pod.Spec.NodeName == "" ||
		pod.Status.Phase != corev1.PodPending ||
		len(networks) == 0 {
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

func patchPodWithoutInvalidNetworkAnnotation(ctx context.Context, c client.Client, pod *corev1.Pod, networks []*multustypes.NetworkSelectionElement) error {
	filteredNetworks := slices.Collect(func(yield func(*multustypes.NetworkSelectionElement) bool) {
		for _, network := range networks {
			if network.Name != invalidNetworkName {
				if !yield(network) {
					return
				}
			}
		}

	})

	j, err := json.Marshal(filteredNetworks)
	if err != nil {
		return fmt.Errorf("error while marshaling json: %w", err)
	}
	pod.Annotations[networkAttachmentAnnot] = string(j)
	pod.ObjectMeta.ManagedFields = nil
	pod.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Pod"))
	return c.Patch(ctx, pod, client.Apply, client.ForceOwnership, client.FieldOwner(podIpamControllerName))
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

func getPodNetworks(pod *corev1.Pod) ([]*multustypes.NetworkSelectionElement, error) {
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
