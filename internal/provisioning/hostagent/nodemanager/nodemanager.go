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

package nodemanager

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	hostutil "github.com/nvidia/doca-platform/internal/provisioning/hostagent/util"

	"github.com/vishvananda/netlink"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	DPFNamespace              = "dpf-operator-system"
	CurrentRebootIDFile       = "/proc/sys/kernel/random/boot_id"
	LatestRebootIDFile        = "/var/lib/dpf/hostagent/boot_id/latest"
	SkipDefaultRouteCheckFile = "/var/lib/dpf/dms/hostnetwork-skip-default-route-check"
)

type Interface interface {
	Start() error
	GetNodeName() string
}
type NodeManager struct {
	client.Client
	nodeName         string
	rebootMethod     string
	customScriptName string
}

func NewNodeManager(client client.Client, rebootMethod string, customScriptName string) *NodeManager {
	return &NodeManager{
		Client:           client,
		rebootMethod:     rebootMethod,
		customScriptName: customScriptName,
	}
}

func (n *NodeManager) Start() error {
	n.registerWithAPIServer()
	go func() {
		_ = wait.PollUntilContextCancel(context.TODO(), 30*time.Second, true, func(ctx context.Context) (bool, error) {
			n.updateStatus()
			return false, nil
		})
	}()
	return nil
}

func (n *NodeManager) GetNodeName() string {
	return n.nodeName
}

func (n *NodeManager) registerWithAPIServer() {
	step := 100 * time.Millisecond
	for {
		time.Sleep(step)
		step = step * 2
		if step >= 7*time.Second {
			step = 7 * time.Second
		}
		klog.Info("Attempting to register DPUDevices")
		dpuDevices, err := n.initDPUDevices()
		if err != nil {
			klog.Errorf("failed to init DPU devices: %v", err)
			continue
		}
		klog.Infof("Successfully registered DPUDevices")

		klog.Info("Attempting to register DPUNode")
		err = n.initDPUNode(dpuDevices)
		if err != nil {
			klog.Errorf("failed to init DPUNode: %v", err)
			continue
		}
		klog.Infof("Successfully registered DPUNode")
		return
	}
}

func (n *NodeManager) initDPUDevices() ([]*provisioningv1.DPUDevice, error) {
	ret := []*provisioningv1.DPUDevice{}
	devices, err := hostutil.DiscoverDPUs()
	if err != nil {
		return nil, fmt.Errorf("failed to discover DPUs: %w", err)
	}
	nodeName, _, err := hostutil.GetNodeName()
	if err != nil {
		return nil, fmt.Errorf("failed to get node name: %w", err)
	}
	for _, device := range devices {
		dpuDevice := &provisioningv1.DPUDevice{
			ObjectMeta: metav1.ObjectMeta{
				Name:      dpuDeviceCRName(device),
				Namespace: DPFNamespace,
				Labels: map[string]string{
					cutil.DPUNodeNameLabel: nodeName,
				},
			},
			Spec: provisioningv1.DPUDeviceSpec{
				SerialNumber: device.SerialNumber,
				NumberOfPFs:  ptr.To(device.NumOfPFs),
			},
		}
		if err := n.Create(context.TODO(), dpuDevice); err != nil {
			if !apierrors.IsAlreadyExists(err) {
				return nil, fmt.Errorf("failed to create DPU device %s: %w", dpuDevice.Name, err)
			}
			if err := n.Get(context.TODO(), client.ObjectKey{Namespace: DPFNamespace, Name: dpuDevice.Name}, dpuDevice); err != nil {
				return nil, fmt.Errorf("failed to get DPU device %s: %w", dpuDevice.Name, err)
			}
		}
		dpuDevice.Status.PCIAddress = ptr.To(strings.ReplaceAll(device.Address, ":", "-"))
		if err := n.Status().Update(context.TODO(), dpuDevice); err != nil {
			return nil, fmt.Errorf("failed to update DPU device %s: %w", dpuDevice.Name, err)
		}
		ret = append(ret, dpuDevice)
	}
	return ret, nil
}

func (n *NodeManager) initDPUNode(dpuDevices []*provisioningv1.DPUDevice) error {
	dpuNode := &provisioningv1.DPUNode{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: DPFNamespace,
		},
	}

	kubeNodeRef := ""
	k8sNodeName := os.Getenv(hostutil.K8sNodeNameEnv)
	if k8sNodeName != "" {
		node := &corev1.Node{}
		if err := n.Get(context.TODO(), client.ObjectKey{Name: k8sNodeName}, node); err != nil {
			return fmt.Errorf("in a K8s env, but failed to get K8s node %s: %w", k8sNodeName, err)
		}
		n.nodeName = node.Name
		kubeNodeRef = node.Name
		ref := metav1.OwnerReference{
			APIVersion: "v1",
			Kind:       "Node",
			Name:       node.Name,
			UID:        node.UID,
			Controller: ptr.To(true),
		}
		dpuNode.ObjectMeta.OwnerReferences = []metav1.OwnerReference{ref}
	} else {
		nodeName, truncated, err := hostutil.GetNodeName()
		if err != nil {
			return fmt.Errorf("failed to init DPUNode CR: %w", err)
		}
		if truncated {
			klog.Warningf("hostname length is greater than %d, truncating to %s", hostutil.MaximumHostNameLength, nodeName)
		}
		n.nodeName = nodeName
	}
	dpuNode.Name = n.nodeName

	devices, err := hostutil.DiscoverDPUs()
	if err != nil {
		return fmt.Errorf("failed to discover DPUs: %w", err)
	}
	if len(devices) > 0 {
		dpuNode.Labels = map[string]string{
			cutil.NodeSelectorLabel: "true",
		}
	}

	switch n.rebootMethod {
	case "gNOI", "hostAgent":
		dpuNode.Spec.NodeRebootMethod = &provisioningv1.NodeRebootMethod{
			HostAgent: &provisioningv1.HostAgent{},
		}
	case "external":
		dpuNode.Spec.NodeRebootMethod = &provisioningv1.NodeRebootMethod{
			External: &provisioningv1.External{},
		}
	case "script":
		dpuNode.Spec.NodeRebootMethod = &provisioningv1.NodeRebootMethod{
			Script: &provisioningv1.Script{
				Name: n.customScriptName,
			},
		}
	}

	for _, dpuDevice := range dpuDevices {
		dpuNode.Spec.DPUs = append(dpuNode.Spec.DPUs, provisioningv1.DPURef{
			Name: dpuDevice.Name,
		})
	}
	if err := n.Create(context.TODO(), dpuNode); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("failed to create DPUNode: %w", err)
		}
		existingDPUNode := &provisioningv1.DPUNode{}
		if err := n.Get(context.TODO(), client.ObjectKey{Namespace: DPFNamespace, Name: dpuNode.Name}, existingDPUNode); err != nil {
			return fmt.Errorf("failed to get DPUNode: %w", err)
		}
		equal := equality.Semantic.DeepEqual(dpuNode.Spec.DPUs, existingDPUNode.Spec.DPUs)
		if !equal {
			existingDPUNode.Spec.DPUs = dpuNode.Spec.DPUs
			if err := n.Update(context.TODO(), existingDPUNode); err != nil {
				return fmt.Errorf("failed to update DPUNode: %w", err)
			}
		}
		dpuNode = existingDPUNode
	}
	rebootOccurred, err := n.rebootOccurred()
	if err != nil {
		return fmt.Errorf("failed to check if reboot occurred: %w", err)
	}
	switch rebootOccurred {
	case rebootOccurred, firstRun:
		dpuNode.Status.RebootInProgress = ptr.To(false)
	}
	dpuNode.Status.DPUInstallInterface = ptr.To(string(provisioningv1.InstallViaHostAgent))
	if kubeNodeRef != "" {
		dpuNode.Status.KubeNodeRef = ptr.To(kubeNodeRef)
	}
	if err := n.Status().Update(context.TODO(), dpuNode); err != nil {
		return fmt.Errorf("failed to update DPUNode: %w", err)
	}
	return nil
}

func (n *NodeManager) bridgeCondition() metav1.Condition {
	cond := metav1.Condition{
		Type: string(provisioningv1.DPUNodeConditionBridgeConfigured),
	}
	bridge, err := netlink.LinkByName("br-dpu")
	if err != nil {
		cond.Status = metav1.ConditionFalse
		cond.Reason = "BridgeNotFound"
		cond.Message = err.Error()
		return cond
	}

	// Skip default route check if the file exists. This is mainly used for testing.
	if _, err := os.Stat(SkipDefaultRouteCheckFile); err == nil {
		cond.Status = metav1.ConditionTrue
		cond.Reason = cond.Type
		return cond
	}

	routes, err := netlink.RouteList(bridge, netlink.FAMILY_V4)
	if err != nil {
		cond.Status = metav1.ConditionFalse
		cond.Reason = "FailedToListRoutes"
		cond.Message = err.Error()
		return cond
	}
	for _, route := range routes {
		if route.Dst != nil && route.Dst.IP.Equal(net.ParseIP("0.0.0.0")) {
			cond.Status = metav1.ConditionTrue
			cond.Reason = cond.Type
			return cond
		}
	}
	cond.Status = metav1.ConditionFalse
	cond.Reason = "NoDefaultRouteFound"
	cond.Message = "No default route found for bridge br-dpu"
	return cond
}

type RebootResult int

const (
	rebootOccurred RebootResult = iota
	rebootNotOccurred
	firstRun
	unknown
)

// rebootOccurred returns true if the reboot occurred.
// False is returned if the reboot did not occur or it is unknown if the reboot occurred.
func (n *NodeManager) rebootOccurred() (RebootResult, error) {
	currentRebootID, err := os.ReadFile(CurrentRebootIDFile)
	if err != nil {
		return unknown, err
	}
	latestRebootID, err := os.ReadFile(LatestRebootIDFile)
	if err != nil {
		if !os.IsNotExist(err) {
			return unknown, err
		}
		if err := os.MkdirAll(filepath.Dir(LatestRebootIDFile), 0755); err != nil {
			return firstRun, err
		}
		if err := hostutil.AtomicWrite(LatestRebootIDFile, currentRebootID, 0644); err != nil {
			return firstRun, err
		}
		return firstRun, nil
	}
	if string(latestRebootID) != string(currentRebootID) {
		return rebootOccurred, nil
	}
	return rebootNotOccurred, nil
}

func (n *NodeManager) updateStatus() {
	if err := n.updateDPUDeviceStatus(); err != nil {
		klog.Errorf("failed to update DPU device status: %v", err)
	}
	if err := n.updateDPUNodeStatus(); err != nil {
		klog.Errorf("failed to update DPUNode status: %v", err)
	}
}

func (n *NodeManager) updateDPUDeviceStatus() error {
	devices, err := hostutil.DiscoverDPUs()
	if err != nil {
		return fmt.Errorf("failed to discover DPUs: %w", err)
	}
	for _, device := range devices {
		dpuDevice := &provisioningv1.DPUDevice{}
		if err := n.Get(context.TODO(), client.ObjectKey{Namespace: DPFNamespace, Name: dpuDeviceCRName(device)}, dpuDevice); err != nil {
			if apierrors.IsNotFound(err) {
				klog.Warningf("DPUDevice CR for DPU %s does not exist", dpuDeviceCRName(device))
				continue
			}
			return fmt.Errorf("failed to get DPU device %s: %w", dpuDeviceCRName(device), err)
		}
		pn0Name, err := hostutil.NewPCIHelper(device.Address).PF(0).InterfaceName()
		if err != nil {
			klog.Warningf("failed to get PF0 name for DPU %s: %v", dpuDeviceCRName(device), err)
			continue
		}
		dpuDevice.Status.PF0Name = ptr.To(pn0Name)
		if err := n.Status().Update(context.TODO(), dpuDevice); err != nil {
			return fmt.Errorf("failed to update DPU device %s: %w", dpuDeviceCRName(device), err)
		}
	}
	return nil
}

func (n *NodeManager) updateDPUNodeStatus() error {
	nodeName, _, err := hostutil.GetNodeName()
	if err != nil {
		return fmt.Errorf("failed to get node name: %w", err)
	}
	dpuNode := &provisioningv1.DPUNode{}
	if err := n.Get(context.TODO(), client.ObjectKey{Namespace: DPFNamespace, Name: nodeName}, dpuNode); err != nil {
		return fmt.Errorf("failed to get DPUNode: %w", err)
	}
	cond := n.bridgeCondition()
	needUpdate := SetDPUNodeCondition(&dpuNode.Status, &cond)
	if !needUpdate {
		return nil
	}
	klog.V(3).Infof("need update bridge condition for DPUNode status. New: %+v", cond)
	if err := n.Status().Update(context.TODO(), dpuNode); err != nil {
		return fmt.Errorf("failed to update DPUNode: %w", err)
	}
	klog.V(3).Infof("updated bridge condition for DPUNode status. New: %+v", cond)
	return nil
}

func SetDPUNodeCondition(status *provisioningv1.DPUNodeStatus, condition *metav1.Condition) bool {
	condition.LastTransitionTime = metav1.Now()
	// Try to find this condition.
	conditionIndex, oldCondition := GetDPUNodeCondition(status, condition.Type)

	if oldCondition == nil {
		// We are adding new condition.
		status.Conditions = append(status.Conditions, *condition)
		return true
	}
	// We are updating an existing condition, so we need to check if it has changed.
	if condition.Status == oldCondition.Status {
		condition.LastTransitionTime = oldCondition.LastTransitionTime
	}

	isEqual := condition.Status == oldCondition.Status &&
		condition.Reason == oldCondition.Reason &&
		condition.Message == oldCondition.Message &&
		condition.LastTransitionTime.Equal(&oldCondition.LastTransitionTime)

	status.Conditions[conditionIndex] = *condition
	// Return true if one of the fields have changed.
	return !isEqual
}

func GetDPUNodeCondition(status *provisioningv1.DPUNodeStatus, conditionType string) (int, *metav1.Condition) {
	if status == nil {
		return -1, nil
	}
	conditions := status.Conditions
	if conditions == nil {
		return -1, nil
	}
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return i, &conditions[i]
		}
	}
	return -1, nil
}

func GetCurrentRebootID() (string, error) {
	rebootID, err := os.ReadFile(CurrentRebootIDFile)
	if err != nil {
		return "", err
	}
	return string(rebootID), nil
}

func GetLatestRebootID() (string, error) {
	rebootID, err := os.ReadFile(LatestRebootIDFile)
	if err != nil {
		return "", err
	}
	return string(rebootID), nil
}

func dpuDeviceCRName(dpuDevice hostutil.Device) string {
	return strings.ToLower(dpuDevice.SerialNumber)
}
