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
	"strings"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	nvipamv1 "github.com/nvidia/doca-platform/internal/nvipam/api/v1alpha1"

	multusclient "gopkg.in/k8snetworkplumbingwg/multus-cni.v4/pkg/k8sclient"
	multustypes "gopkg.in/k8snetworkplumbingwg/multus-cni.v4/pkg/types"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// PodIpamReconciler reconciles a Pod object
type PodIpamReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

type poolConfig struct {
	PoolName string
	PoolType string
	AssignGW bool
}

const (
	ipamPoolNamesParam     = "poolNames"
	podIpamControllerName  = "podIpamcontroller"
	networkAttachmentAnnot = "k8s.v1.cni.cncf.io/networks"
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
	if !pod.ObjectMeta.DeletionTimestamp.IsZero() {
		// Return early, the object is deleting.
		return ctrl.Result{}, nil
	}
	if pod.Spec.NodeName == "" {
		// Pod not scheduled on a node yet
		return ctrl.Result{}, nil
	}
	if pod.Status.Phase != corev1.PodPending {
		// Pod is not pending
		return ctrl.Result{}, nil
	}
	podNets, err := getPodNetworks(ctx, pod)
	if err != nil {
		return ctrl.Result{}, err
	}
	if len(podNets) == 0 {
		log.Info("Pod without secondary network")
		return ctrl.Result{}, nil
	}
	podIfcToSvcIfc := make(map[string]*dpuservicev1.ServiceIfc)
	for _, net := range podNets {
		// No Interface requested
		if net.InterfaceRequest == "" {
			log.Info("Pod with secondary network, but without interface requested")
			return ctrl.Result{RequeueAfter: reconcileRetryTime}, nil
		}
		podIfcToSvcIfc[net.InterfaceRequest] = nil
	}
	err = r.mutateMapWithServicesForPod(ctx, podIfcToSvcIfc, pod.Spec.NodeName, pod)
	if err != nil {
		log.Error(err, "Fail to map pod network interfaces to Chain Port, requeue.")
		return ctrl.Result{RequeueAfter: reconcileRetryTime}, nil
	}

	requeue, err := validateSvc(ctx, podIfcToSvcIfc)
	if err != nil {
		return ctrl.Result{}, err
	}

	ifcToPoolCfg, err := r.getPoolsConfig(ctx, podIfcToSvcIfc)
	if err != nil {
		// No IpPool/CidrPool found requeuing
		return ctrl.Result{RequeueAfter: reconcileRetryTime}, nil
	}
	err = r.updatePodAnnotation(ctx, pod, ifcToPoolCfg)
	return requeue, err
}

func (r *PodIpamReconciler) updatePodAnnotation(ctx context.Context, pod *corev1.Pod,
	ifcToPoolCfg map[string]*poolConfig) error {
	log := log.FromContext(ctx)
	networks, err := multusclient.GetPodNetwork(pod)
	if err != nil {
		log.Error(err, "failed to get networks from pod")
		return err
	}
	changed := false
	for _, net := range networks {
		if pollCfg, ok := ifcToPoolCfg[net.InterfaceRequest]; ok {
			cniArgs := make(map[string]interface{})
			cniArgs["allocateDefaultGateway"] = pollCfg.AssignGW
			cniArgs["poolNames"] = []string{pollCfg.PoolName}
			cniArgs["poolType"] = pollCfg.PoolType
			net.CNIArgs = &cniArgs
			changed = true
		}
	}
	if !changed {
		return nil
	}
	j, err := json.Marshal(networks)
	if err != nil {
		log.Error(err, "failed to marshal networks to json")
		return err
	}
	pod.Annotations[networkAttachmentAnnot] = string(j)
	pod.ObjectMeta.ManagedFields = nil
	pod.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Pod"))
	return r.Client.Patch(ctx, pod, client.Apply, client.ForceOwnership, client.FieldOwner(podIpamControllerName))
}

// mutateMapWithServicesForPod returns a map of the pod's network interfaces to serviceIfc entries.
// The map is populated with serviceIfc entries which refer to ServiceInterfaces that are associated with the pod's node.
// The serviceIfc entries are populated based on the serviceChain CRs in the pod's namespace.
func (r *PodIpamReconciler) mutateMapWithServicesForPod(ctx context.Context, podIfcToSvcIfc map[string]*dpuservicev1.ServiceIfc, node string, pod *corev1.Pod) error {
	scList := &dpuservicev1.ServiceChainList{}
	if err := r.Client.List(ctx, scList, client.InNamespace(pod.Namespace)); err != nil {
		return err
	}
	if len(scList.Items) == 0 {
		return fmt.Errorf("no serviceChains in namespace %s found", pod.Namespace)
	}
	//TODO(adrianc): need to re-write the below...
	for _, serviceChain := range scList.Items {
		if serviceChain.Spec.Node == nil || *serviceChain.Spec.Node != node {
			continue
		}

		for _, sw := range serviceChain.Spec.Switches {
			for _, port := range sw.Ports {
				svcIfc, err := r.getServiceInterfaceWithLabels(ctx, node, pod.Namespace, port.ServiceInterface.MatchLabels)
				if err != nil {
					return fmt.Errorf("failed to get serviceInterface for chain. %w", err)
				}
				if svcIfc.Spec.InterfaceType == dpuservicev1.InterfaceTypeService {
					// no support for object reference
					// we add entry if the pod matched serviceID label AND the interface name matches.
					if podMatchLabels(pod, map[string]string{dpuservicev1.DPFServiceIDLabelKey: svcIfc.Spec.Service.ServiceID}) {
						if _, ok := podIfcToSvcIfc[svcIfc.Spec.Service.InterfaceName]; ok {
							podIfcToSvcIfc[svcIfc.Spec.Service.InterfaceName] = &port.ServiceInterface
						}
					}
				}
			}
		}
	}
	return nil
}

// getServiceInterfaceWithLabels returns ServiceInterface in given namespace that belongs to current node with given labels. if more than one or none matches, error out.
func (r *PodIpamReconciler) getServiceInterfaceWithLabels(ctx context.Context, nodeName string, namespace string, lbls map[string]string) (*dpuservicev1.ServiceInterface, error) {
	//TODO(adrianc): this needs to be moved to a common place as we need the same thing in sfc-controller
	sil := &dpuservicev1.ServiceInterfaceList{}
	listOpts := []client.ListOption{}
	listOpts = append(listOpts, client.MatchingLabelsSelector{Selector: labels.SelectorFromSet(labels.Set(lbls))})
	if namespace != "" {
		listOpts = append(listOpts, client.InNamespace(namespace))
	}
	if err := r.List(ctx, sil, listOpts...); err != nil {
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

// SetupWithManager sets up the controller with the Manager.
func (r *PodIpamReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Pod{}).
		Complete(r)
}

func getPodNetworks(ctx context.Context, pod *corev1.Pod) ([]*multustypes.NetworkSelectionElement, error) {
	log := log.FromContext(ctx)
	networks, err := multusclient.GetPodNetwork(pod)
	if err != nil {
		if _, ok := err.(*multusclient.NoK8sNetworkError); ok {
			log.Info("No networks found in pod annotation")
			return nil, nil
		}
		log.Error(err, "failed to get networks from pod")
		return nil, err
	}
	return networks, nil
}

func (r *PodIpamReconciler) getPoolsConfig(ctx context.Context, podIfcToSvcIfc map[string]*dpuservicev1.ServiceIfc) (map[string]*poolConfig, error) {
	ifcToPoolCfg := make(map[string]*poolConfig)
	for ifc, svcIfc := range podIfcToSvcIfc {
		if svcIfc.IPAM != nil {
			poolCfg, err := r.getPoolConfig(ctx, svcIfc.IPAM)
			if err != nil {
				return nil, err
			}
			ifcToPoolCfg[ifc] = poolCfg
		}
	}
	return ifcToPoolCfg, nil
}

func (r *PodIpamReconciler) getPoolConfig(ctx context.Context, ipam *dpuservicev1.IPAM) (*poolConfig, error) {
	poolCfg := &poolConfig{}

	if ipam.DefaultGateway != nil {
		poolCfg.AssignGW = *ipam.DefaultGateway
	}

	pool, poolType, err := r.getPoolByMatchLabel(ctx, ipam)
	if err != nil {
		return nil, err
	}
	poolCfg.PoolType = poolType
	poolCfg.PoolName = pool
	return poolCfg, nil
}

func (r *PodIpamReconciler) getPoolByMatchLabel(ctx context.Context, ipam *dpuservicev1.IPAM) (string, string, error) {
	log := log.FromContext(ctx)
	listOptions := client.MatchingLabels(ipam.MatchLabels)
	ipPoolList := &nvipamv1.IPPoolList{}
	if err := r.List(ctx, ipPoolList, &listOptions); err != nil {
		return "", "", err
	}
	if len(ipPoolList.Items) > 0 {
		if len(ipPoolList.Items) > 1 {
			log.Info("DPUDeploymentService IPAM MatchLabels matched more than one IPPool", "labels", ipam.MatchLabels)
		}
		return ipPoolList.Items[0].Name, strings.ToLower(nvipamv1.IPPoolKind), nil
	}
	cidrPoolList := &nvipamv1.CIDRPoolList{}
	if err := r.List(ctx, cidrPoolList, &listOptions); err != nil {
		return "", "", err
	}
	if len(cidrPoolList.Items) > 0 {
		if len(ipPoolList.Items) > 1 {
			log.Info("DPUDeploymentService IPAM MatchLabels matched more than one CIDRPool", "labels", ipam.MatchLabels)
		}
		return cidrPoolList.Items[0].Name, strings.ToLower(nvipamv1.CIDRPoolKind), nil
	}
	// No IpPool/CidrPool found requeuing
	return "", "", fmt.Errorf("no IPPool or CidrPool found for Labels %v", ipam.MatchLabels)
}

func validateSvc(ctx context.Context, podIfcToSvcIfc map[string]*dpuservicev1.ServiceIfc) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	requeue := ctrl.Result{}
	for ifc, svcIfc := range podIfcToSvcIfc {
		if svcIfc == nil {
			// No service found for interface, register a requeue for next retry
			// In case the service is added later
			requeue = ctrl.Result{RequeueAfter: reconcileRetryTime}
			// Remove the interface from the map as it has no service
			delete(podIfcToSvcIfc, ifc)
			continue
		}
		if svcIfc.IPAM == nil {
			log.Info("No IPAM requested for interface", "interface", ifc)
			continue
		}
		if len(svcIfc.IPAM.MatchLabels) < 1 {
			return requeue, fmt.Errorf("service IPAM should have MatchLabels. Interface:%s", ifc)
		}
	}
	return requeue, nil
}
