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

package dpunode

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	operatorcontroller "github.com/nvidia/doca-platform/internal/operator/controllers"
	dnutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpunode/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/release"
	dpfutils "github.com/nvidia/doca-platform/internal/utils"
	"github.com/nvidia/doca-platform/pkg/conditions"

	"github.com/fluxcd/pkg/runtime/patch"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	// controller name that will be used when
	DPUNodeControllerName = "dpunode"
)

// DPUNodeReconciler reconciles a DPUNode object
type DPUNodeReconciler struct {
	client.Client
	DPUInstallInterface *string
	// Options are the Options used to configure the DMS Pods created by the controller.
	Options dnutil.DMSPodOptions
	// Recorder is an event recorder that is used to record events that occur during the execution of the controller.
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpunodes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpunodes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpunodes/finalizers,verbs=update
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpus,verbs=get;list;watch
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpudevices,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpudevices/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpudevices/finalizers,verbs=update
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpus/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpus/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;create;delete

func (r *DPUNodeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	log := log.FromContext(ctx)
	log.Info("Reconcile")

	dpuNode := &provisioningv1.DPUNode{}
	pod := &corev1.Pod{}
	if err := r.Get(ctx, req.NamespacedName, dpuNode); err != nil {
		if apierrors.IsNotFound(err) {
			// Return early if the object is not found.
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	patcher := patch.NewSerialPatcher(dpuNode, r.Client)
	// Defer a patch call to always patch the object when Reconcile exits.
	defer func() {
		log.Info("Patching")
		if err := patcher.Patch(ctx, dpuNode,
			patch.WithFieldOwner(DPUNodeControllerName),
			patch.WithStatusObservedGeneration{},
			patch.WithOwnedConditions{Conditions: conditions.TypesAsStrings(dpuservicev1.Conditions)},
		); err != nil {
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
	}()

	// TODO: once KubeNodeRef is moved from Status to Spec, change this to check Spec.KubeNodeRef
	var nodeRef *metav1.OwnerReference
	for i := range dpuNode.ObjectMeta.OwnerReferences {
		if dpuNode.ObjectMeta.OwnerReferences[i].Kind == "Node" {
			nodeRef = &dpuNode.ObjectMeta.OwnerReferences[i]
			break
		}
	}

	if nodeRef != nil {
		node := &corev1.Node{}
		if err := r.Client.Get(ctx, client.ObjectKey{Namespace: "", Name: nodeRef.Name}, node); err != nil {
			if apierrors.IsNotFound(err) {
				if err := r.Client.Delete(ctx, dpuNode); err != nil {
					return ctrl.Result{}, err
				}
				return ctrl.Result{}, nil
			}
			return ctrl.Result{}, err
		}

		// update DPUNode status - KubeNodeRef
		dpuNode.Status.KubeNodeRef = &node.Name

		// Copy labels and annotations from node to dpuNode
		dpuNode.Labels = cutil.CopyLabelsOrAnnotations(dpuNode.Labels, node.Labels)
		dpuNode.Annotations = cutil.CopyLabelsOrAnnotations(dpuNode.Annotations, node.Annotations)

		dmsPodName := cutil.GenerateDMSPodName(dpuNode)
		nn := types.NamespacedName{
			Namespace: operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace,
			Name:      dmsPodName,
		}

		if err := r.Get(ctx, nn, pod); err != nil {
			if apierrors.IsNotFound(err) {
				return ctrl.Result{}, nil
			}
			return ctrl.Result{}, err
		}

		conditionMessage, err := r.isPodRunning(ctx, pod)
		if err != nil {
			r.updateDPUNodeStatusConditions(dpuNode, provisioningv1.DPUNodeConditionReady, metav1.ConditionFalse, "ErrorOccurred", err.Error())
			return ctrl.Result{}, err
		}

		if conditionMessage != nil {
			r.updateDPUNodeStatusConditions(dpuNode, provisioningv1.DPUNodeConditionReady, metav1.ConditionFalse, "NotReady", *conditionMessage)
			return ctrl.Result{RequeueAfter: cutil.RequeueInterval}, nil
		}
	}

	// Handle host agent upgrade
	if nodeRef != nil {
		if result, err := r.handleHostAgentUpgrade(ctx, dpuNode, true, pod); err != nil || !result.IsZero() {
			return result, err
		}
	} else {
		if result, err := r.handleHostAgentUpgrade(ctx, dpuNode, false, nil); err != nil || !result.IsZero() {
			return result, err
		}
	}

	// TODO: handle DPU modified

	// Update DPUNode status - DPUInstallInterface
	if r.DPUInstallInterface == nil {
		return ctrl.Result{}, errors.New("DPUInstallInterface is not set")
	}
	if dpuNode.Status.DPUInstallInterface == nil {
		dpuNode.Status.DPUInstallInterface = r.DPUInstallInterface
		return ctrl.Result{}, nil
	}

	if err := r.reconcileDPUDevices(ctx, dpuNode); err != nil {
		r.updateDPUNodeStatusConditions(dpuNode, provisioningv1.DPUNodeConditionInvalidDPUDetails, metav1.ConditionTrue, string(provisioningv1.DPUNodeConditionInvalidDPUDetails), err.Error())
		return ctrl.Result{}, err
	}

	// Check if the DMS server is ready
	if *dpuNode.Status.DPUInstallInterface == string(provisioningv1.DPUNodeInstallInterfaceGNOI) {
		if dpuNode.Spec.NodeDMSAddress == nil {
			msg := fmt.Sprintf("DPUNode %s NodeDMSAddress is not set", dpuNode.Name)
			r.updateDPUNodeStatusConditions(dpuNode, provisioningv1.DPUNodeConditionReady, metav1.ConditionFalse, "NoNodeDMSAddress", msg)
			return ctrl.Result{}, errors.New(msg)
		}
		addr := dpuNode.Spec.NodeDMSAddress.String()
		conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
		if err != nil {
			msg := fmt.Sprintf("the DMS server %s is not ready yet, err: %v", addr, err)
			r.updateDPUNodeStatusConditions(dpuNode, provisioningv1.DPUNodeConditionReady, metav1.ConditionFalse, "DMSServerNotReady", msg)
			return ctrl.Result{RequeueAfter: cutil.RequeueInterval}, nil
		}

		defer func() {
			if err := conn.Close(); err != nil {
				log.Error(fmt.Errorf("failed to close connection of %s, err: %v", addr, err), "")
			}
		}()

	}

	r.updateDPUNodeStatusConditions(dpuNode, provisioningv1.DPUNodeConditionReady, metav1.ConditionTrue, "", "")

	// TODO: add health check for DMS pod
	return ctrl.Result{}, nil
}

func (r *DPUNodeReconciler) handleHostAgentUpgrade(ctx context.Context, dpuNode *provisioningv1.DPUNode, isKubernetes bool, pod *corev1.Pod) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	// Check whether the DMS Pod is out of date, upgrade it if necessary
	dpfOperatorConfig, err := dpfutils.GetDPFOperatorConfig(ctx, r.Client)
	if err != nil {
		log.Error(fmt.Errorf("getting DPFOperatorConfig, err: %v", err), "")
		return ctrl.Result{}, nil
	}
	if !dpfOperatorConfig.UpgradeInProgress() {
		return ctrl.Result{}, nil
	}
	if isKubernetes {
		dpfVersion, exist := pod.Labels[release.DPFVersionLabelKey]
		if !exist || dpfVersion != release.DPFVersion() {
			// Upgrade the Host Agent
			if err := r.Delete(ctx, pod); err != nil {
				log.Info("failed to delete the old host agent pod for upgrade, err: %v", err)
				return ctrl.Result{}, err
			}
			log.Info("upgrading host agent to version " + release.DPFVersion())
			return ctrl.Result{RequeueAfter: cutil.RequeueInterval}, nil
		}
		return ctrl.Result{}, nil
	} else {
		dpuNodeUpgradeConditionExists, needHostAgentUpgradeValue := r.getDPUNodeUpgradeCondition(dpuNode)
		if !dpuNodeUpgradeConditionExists {
			// Update the DPUNode condition to true and wait for the user to upgrade DMS
			msg := "Need user to upgrade host agent during the dpf upgrade."
			r.updateDPUNodeStatusConditions(dpuNode, provisioningv1.DPUNodeConditionNeedHostAgentUpgrade, metav1.ConditionTrue, "", msg)
			return ctrl.Result{RequeueAfter: cutil.RequeueInterval}, nil
		} else if !needHostAgentUpgradeValue {
			// User has completed the DMS upgrade
			log.Info("Host agent upgrade is completed.")
			return ctrl.Result{}, nil
		} else {
			log.Info("Waiting for the user to upgrade host agent.")
			return ctrl.Result{RequeueAfter: cutil.RequeueInterval}, nil
		}
	}
}

func (r *DPUNodeReconciler) updateDPUNodeStatusConditions(dpuNode *provisioningv1.DPUNode, condType provisioningv1.DPUNodeConditionType, status metav1.ConditionStatus, reason string, message string) {
	cond := &metav1.Condition{
		Type:    condType.String(),
		Status:  status,
		Message: message,
	}
	if reason != "" {
		cond.Reason = reason
	} else {
		cond.Reason = condType.String()
	}
	meta.SetStatusCondition(&dpuNode.Status.Conditions, *cond)
}

func (r *DPUNodeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&provisioningv1.DPUNode{}).
		Watches(&corev1.Node{},
			handler.EnqueueRequestsFromMapFunc(r.nodeToDPUNodeReq),
			builder.WithPredicates(predicate.LabelChangedPredicate{})).
		Watches(&provisioningv1.DPU{},
			handler.EnqueueRequestsFromMapFunc(r.dpuToDPUNodeReq)).
		Watches(&operatorv1.DPFOperatorConfig{},
			handler.EnqueueRequestsFromMapFunc(r.dpfOperatorConfigToDPUNodeReq)).
		Complete(r)
}

func (r *DPUNodeReconciler) dpfOperatorConfigToDPUNodeReq(ctx context.Context, resource client.Object) []reconcile.Request {
	requests := make([]reconcile.Request, 0)
	dpuNodeList := &provisioningv1.DPUNodeList{}
	dpfOperatorConfig, ok := resource.(*operatorv1.DPFOperatorConfig)
	if !ok {
		return nil
	}
	if !dpfOperatorConfig.UpgradeInProgress() {
		return nil
	}

	if err := r.List(ctx, dpuNodeList); err == nil {
		for _, item := range dpuNodeList.Items {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      item.GetName(),
					Namespace: item.GetNamespace(),
				}})
		}
	}
	return requests
}

func (r *DPUNodeReconciler) nodeToDPUNodeReq(ctx context.Context, resource client.Object) []reconcile.Request {
	// Get the node that changed
	node := resource.(*corev1.Node)
	requests := make([]reconcile.Request, 0)

	// List all DPUNodes
	dpuNodeList := &provisioningv1.DPUNodeList{}
	if err := r.List(ctx, dpuNodeList); err != nil {
		return nil
	}

	// Find DPUNodes that reference this node and add requests for them
	for _, dpuNode := range dpuNodeList.Items {
		if dpuNode.Status.KubeNodeRef != nil && *dpuNode.Status.KubeNodeRef == node.GetName() {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      dpuNode.GetName(),
					Namespace: dpuNode.GetNamespace(),
				},
			})
		}
	}

	return requests
}

func (r *DPUNodeReconciler) dpuToDPUNodeReq(ctx context.Context, resource client.Object) []reconcile.Request {
	// Logic for handling changes to DPU objects
	dpu := resource.(*provisioningv1.DPU)
	requests := make([]reconcile.Request, 0)
	dpuNodeList := &provisioningv1.DPUNodeList{}
	if err := r.List(ctx, dpuNodeList); err != nil {
		return nil
	}
	for _, item := range dpuNodeList.Items {
		if item.GetName() == dpu.Spec.DPUNodeName {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      item.GetName(),
					Namespace: item.GetNamespace(),
				},
			})
		}
	}
	return requests
}

func isTimeout(pod *corev1.Pod, timeoutDuration time.Duration) bool {
	return time.Since(pod.CreationTimestamp.Time) > timeoutDuration
}

func (r *DPUNodeReconciler) isPodRunning(ctx context.Context, pod *corev1.Pod) (*string, error) {
	// TODO: verifiy all returned conditions are OK.
	logger := log.FromContext(ctx)
	if !pod.DeletionTimestamp.IsZero() {
		message := fmt.Sprintf("DMS pod %s is in terminating state", pod.Name)
		return &message, nil
	}
	switch pod.Status.Phase {
	// TODO: fix the case when pod is in Pending state, check all containers and all initContainers and return proper message
	case corev1.PodPending:
		// Verify NFS server connection using the DMS container startup probe.
		if len(pod.Status.ContainerStatuses) == 0 || pod.Status.ContainerStatuses[0].State.Waiting != nil {
			for _, condition := range pod.Status.Conditions {
				if condition.Type != corev1.PodReadyToStartContainers || condition.Status != corev1.ConditionFalse {
					continue
				}
				message := fmt.Sprintf("the DMS server %s is not ready yet, wait for the NFS server to become available", pod.Name)
				return &message, nil
			}
		}
		message := fmt.Sprintf("the DMS server %s is not ready yet", pod.Name)
		return &message, nil

	case corev1.PodRunning:
		// a simple probe to check if the DMS server is ready
		logger.Info("DMS pod is running")
	case corev1.PodFailed:
		return nil, fmt.Errorf("DMS Pod Failed")

	default:
		if isTimeout(pod, r.Options.DMSPodTimeout) {
			return nil, fmt.Errorf("DMS Pod didn't run and timed out")
		}
	}
	return nil, nil
}

func (r *DPUNodeReconciler) reconcileDPUDevices(ctx context.Context, dpuNode *provisioningv1.DPUNode) error {
	if dpuNode.Status.DPUInstallInterface == nil {
		return fmt.Errorf("DPUInstallInterface is not provided")
	}
	dpuInstallInterface := *dpuNode.Status.DPUInstallInterface
	labels := map[string]string{
		cutil.DPUNodeNameLabel: dpuNode.Name,
	}
	for _, dpu := range dpuNode.Spec.DPUs {
		dpuDevice := &provisioningv1.DPUDevice{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: dpuNode.Namespace, Name: dpu.Name}, dpuDevice); err != nil {
			return err
		}
		switch dpuInstallInterface {
		case string(provisioningv1.DPUNodeInstallInterfaceGNOI):
			if dpuDevice.Status.PCIAddress == nil {
				return fmt.Errorf("DPUDevice %s does not have a PCI address", dpuDevice.Name)
			}
		case string(provisioningv1.DPUNodeInstallIntrefaceRedfish):
			if dpuDevice.Spec.BMCIP == nil {
				return fmt.Errorf("DPUDevice %s does not have a BMC IP address", dpuDevice.Name)
			}
		default:
			return fmt.Errorf("DPUInstallInterface %s is not supported", dpuInstallInterface)
		}
		if dpuDevice.Status.PCIAddress != nil {
			labels[cutil.DPUDevicePCIAddressLabel] = *dpuDevice.Status.PCIAddress
		}
		if dpuDevice.Spec.PSID != nil {
			labels[cutil.DPUDevicePSIDLabel] = *dpuDevice.Spec.PSID
		}
		if dpuDevice.Spec.OPN != nil {
			labels[cutil.DPUDeviceOPNLabel] = *dpuDevice.Spec.OPN
		}
		if dpuDevice.Spec.NumberOfPFs != nil {
			labels[cutil.DPUDeviceNumOfPFsLabel] = fmt.Sprintf("%d", *dpuDevice.Spec.NumberOfPFs)
		}
		if dpuDevice.Spec.PF0Name != nil {
			labels[cutil.DPUDevicePF0NameLabel] = *dpuDevice.Spec.PF0Name
		}
		if dpuDevice.Spec.BMCIP != nil {
			labels[cutil.DPUDeviceBMCIPLabel] = *dpuDevice.Spec.BMCIP
		}

		// add labels to DPUDevice CR
		patcher := patch.NewSerialPatcher(dpuDevice, r.Client)
		dpuDevice.Labels = cutil.CopyLabelsOrAnnotations(dpuDevice.Labels, labels)
		if err := patcher.Patch(ctx, dpuDevice); err != nil {
			return err
		}
	}
	return nil
}

func (r *DPUNodeReconciler) getDPUNodeUpgradeCondition(dpuNode *provisioningv1.DPUNode) (bool, bool) {
	upgradeConditionExists, needDMSUpgrade := false, false
	for _, condition := range dpuNode.Status.Conditions {
		if condition.Type == provisioningv1.DPUNodeConditionNeedHostAgentUpgrade.String() {
			upgradeConditionExists = true
			needDMSUpgrade = condition.Status == metav1.ConditionTrue
			break
		}
	}
	return upgradeConditionExists, needDMSUpgrade
}
