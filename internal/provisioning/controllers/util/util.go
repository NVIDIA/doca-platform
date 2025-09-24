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

package util

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/dpucluster"

	"github.com/fluxcd/pkg/runtime/patch"
	"google.golang.org/grpc/metadata"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// RequeueInterval is the interval to requeue the request.
	RequeueInterval = 5 * time.Second

	// RebootSyncInterval is the interval to requeue the request for waiting all DPUs get into non-provisioning phase.
	RebootSyncInterval = 30 * time.Second

	// CFGExtension is the extension of the BFB configuration file.
	CFGExtension = "cfg"
	// DPUProvisioningLabelPrefix is the prefix for all DPU provisioning labels.
	DPUProvisioningLabelPrefix = "provisioning.dpu.nvidia.com/"
	// DPUNodeRebootMethodLabel is the label that specify the reboot method
	DPUNodeRebootMethodLabel = DPUProvisioningLabelPrefix + "reboot-method"
	// DPUNodeAdditionalDPURebootLabel is the label that should be added to a DPUNode to trigger an additional DPU reboot
	// after the BFB is installed.
	DPUNodeAdditionalDPURebootLabel = DPUProvisioningLabelPrefix + "dpu-reboot-after-install"
	// DPUNodeScriptNameLabel is the label that specify the script name for the custom script reboot method.
	DPUNodeScriptNameLabel = DPUProvisioningLabelPrefix + "script-name"
	// DPUSetNameLabel is the label that indicates the name of the DPUSet.
	DPUSetNameLabel = DPUProvisioningLabelPrefix + "dpuset-name"
	// DPUSetNamespaceLabel is the label that indicates the namespace of the DPUSet.
	DPUSetNamespaceLabel = DPUProvisioningLabelPrefix + "dpuset-namespace"
	// DPUNodeNameLabel is the label that indicates the name of the DPUNode the DPU is associated with.
	DPUNodeNameLabel = DPUProvisioningLabelPrefix + "dpunode-name"
	// DPUDeviceNameLabel is the label that indicates the name of the DPUDevice the DPU is associated with.
	DPUDeviceNameLabel = DPUProvisioningLabelPrefix + "dpudevice-name"
	// DPUDevicePCIAddressLabel is the label that indicates the PCI address of the DPU device.
	DPUDevicePCIAddressLabel = DPUProvisioningLabelPrefix + "dpudevice-pciAddress"
	// DPUDevicePSIDLabel is the label that indicates the PSID of the DPU device.
	DPUDevicePSIDLabel = DPUProvisioningLabelPrefix + "dpudevice-psid"
	// DPUDeviceOPNLabel is the label that indicates the OPN of the DPU device.
	DPUDeviceOPNLabel = DPUProvisioningLabelPrefix + "dpudevice-opn"
	// DPUDeviceNumOfPFsLabel is the label that indicates the number of PFs on the DPU device.
	DPUDeviceNumOfPFsLabel = DPUProvisioningLabelPrefix + "dpudevice-num-of-pfs"
	// DPUDevicePF0NameLabel is the label that indicates the name of the PF0 on the DPU device.
	DPUDevicePF0NameLabel = DPUProvisioningLabelPrefix + "dpudevice-pf0-name"
	// DPUDeviceBMCIPLabel is the label that indicates the BMC IP of the DPU device.
	DPUDeviceBMCIPLabel = DPUProvisioningLabelPrefix + "dpudevice-bmc-ip"
	// TolerationNotReadyKey is the key for the NotReady taint.
	TolerationNotReadyKey = "node.kubernetes.io/not-ready"
	// TolerationUnreachableKey is the key for the Unreachable taint.
	TolerationUnreachableKey = "node.kubernetes.io/unreachable"
	// TolerationUnschedulableKey is the key for the Unschedulable taint.
	TolerationUnschedulableKey = "node.kubernetes.io/unschedulable"
	// TolerationOVNKubernetesNetworkUnavailableKey is the key for the OVN Kubernetes NotReady taint.
	TolerationOVNKubernetesNetworkUnavailableKey = "k8s.ovn.org/network-unavailable"
	// DPUOOBBridgeConfiguredLabel is the label that indicates that the DPU OOB bridge is configured.
	DPUOOBBridgeConfiguredLabel = "dpu-oob-bridge-configured"
	// NodeFeatureDiscoveryLabelPrefix is the prefix for all NodeFeatureDiscovery labels.
	NodeFeatureDiscoveryLabelPrefix = "feature.node.kubernetes.io/"
	// NodeSelectorLabel is a label for linking Node with DPU.
	NodeSelectorLabel = NodeFeatureDiscoveryLabelPrefix + "dpu-enabled"
	// NodeMaintenanceRequestorID is the requestor ID used for NodeMaintenance CRs
	NodeMaintenanceRequestorID = "dpu.nvidia.com"
	// ProvisioningGroupName is the provisioning group, used to identify provisioning as
	// additional Requestors in NodeMaintenance CR.
	ProvisioningGroupName = "provisioning.dpu.nvidia.com"
	// PCIAddressTargetKey is the key for the PCI address in the metadata context.
	PCIAddressTargetKey = "target"

	OverrideDMSPodNameAnnotationKey = "provisioning.dpu.nvidia.com/override-dms-pod-name"

	// LastAppliedLabelsOnDPUKey is the key for the last applied labels.
	LastAppliedLabelsOnDPUKey = DPUProvisioningLabelPrefix + "last-applied-labels-on-dpu"

	// LastAppliedNodeMaintenanceAdditionalRequestorsOnDPUKey is the key for the last applied node maintenance additional requestors.
	LastAppliedNodeMaintenanceAdditionalRequestorsOnDPUKey = DPUProvisioningLabelPrefix + "last-applied-node-maintenance-additional-requestors-on-dpu"

	HoldNodeEffectKey             = DPUProvisioningLabelPrefix + "wait-for-external-nodeeffect"
	TrustedSFCount                = DPUProvisioningLabelPrefix + "num-of-trusted-sfs"
	ProvisioningComponentLabelKey = DPUProvisioningLabelPrefix + "component"

	// KubernetesVersion is the version used by the DPUCluster.
	// It has a one-to-one relationship with the DPF Operator version and needs to be updated with each minor release.
	KubernetesVersion = "v1.34.0"
	// MaxNameLength is the maximum length of the name of the K8s resource.
	MaxNameLength = validation.DNS1123SubdomainMaxLength // 253

	// HostNameDPULabelKey is the label added to the DPU Kubernetes Node that indicates the hostname of the host that
	// this DPU belongs to.
	HostNameDPULabelKey = "provisioning.dpu.nvidia.com/host"
)

var (
	// Location of BFB binary files
	BFBBaseDir        = "bfb"
	KubeconfigBaseDir = "kubeconfig"
)

func GenerateBmcFwFilePath(filename string) string {
	return string(os.PathSeparator) + BFBBaseDir + string(os.PathSeparator) + filename
}

func GenerateBFCFGFileName(dpuName string, uid string) string {
	return fmt.Sprintf("%s-%s.%s", dpuName, uid, CFGExtension)
}

func GenerateBFBCFGFilePath(filename string) string {
	return string(os.PathSeparator) + BFBBaseDir + string(os.PathSeparator) + filename
}

func GenerateBFBTaskName(bfb provisioningv1.BFB) string {
	return fmt.Sprintf("%s-%s", bfb.Namespace, bfb.Name)
}

func GenerateBFBFilePath(filename string) string {
	return string(os.PathSeparator) + BFBBaseDir + string(os.PathSeparator) + filename
}

func GenerateBFBTMPFilePath(uid string) string {
	return string(os.PathSeparator) + BFBBaseDir + string(os.PathSeparator) + fmt.Sprintf("bfb-%s", uid)
}

// GenerateDMSPodName creates a name for the DMS pod based off the name of the passed object.
// This allows the passed object to be either a DPUNode or a corev1.Node.
func GenerateDMSPodName(dpuNode crclient.Object) string {
	if v, ok := dpuNode.GetAnnotations()[OverrideDMSPodNameAnnotationKey]; ok {
		return v
	}
	return fmt.Sprintf("%s-%s", dpuNode.GetName(), "dms")
}

func GenerateHostAgentPodName(dpuNode crclient.Object) string {
	return GenerateDMSPodName(dpuNode)
}

func GenerateDMSServerCertName(dpuName string) string {
	return fmt.Sprintf("%s-%s", dpuName, "dms-server-cert")
}

func GenerateDMSServerSecretName(dpuName string) string {
	return fmt.Sprintf("%s-%s", dpuName, "server-secret")
}

func GenerateNodeName(dpu *provisioningv1.DPU) string {
	return dpu.Name
}

func RemoteExec(ns, name, container, cmd string) (string, string, error) {
	config, err := restclient.InClusterConfig()
	if err != nil {
		return "", "", nil
	}

	// Create a Kubernetes client
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	req := clientset.CoreV1().RESTClient().
		Post().
		Resource("pods").
		Name(name).
		Namespace(ns).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: container,
			Command:   strings.Fields(cmd),
			Stdin:     false,
			Stdout:    true,
			Stderr:    true,
			TTY:       false,
		}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
	if err != nil {
		return "", "", err
	}

	outBuf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	cmdErr := executor.StreamWithContext(context.Background(), remotecommand.StreamOptions{
		Stdin:  nil,
		Stdout: bufio.NewWriter(outBuf),
		Stderr: bufio.NewWriter(errBuf),
		Tty:    false,
	})
	return outBuf.String(), errBuf.String(), cmdErr
}

func GetPCIAddrFromDPU(dpu *provisioningv1.DPU, removePrefix bool) (string, error) {
	// the value of pci address from the node label likes: 0000_4b_00
	if dpu.Spec.PCIAddress == nil {
		return "", fmt.Errorf("empty PCI address")
	}
	pciAddress := *dpu.Spec.PCIAddress
	if removePrefix {
		// remove 0000- prefix
		underscoreIndex := strings.Index(pciAddress, "-")
		if underscoreIndex != -1 {
			pciAddress = pciAddress[underscoreIndex+1:]
		}
	}
	// replace - to :
	result := strings.ReplaceAll(pciAddress, "-", ":")
	// 4b:00
	return result, nil
}

func IsNodeReady(node *corev1.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

// AddLabelsToObject adds the given labels to any Kubernetes object implementing metav1.Object
func AddLabelsToObject(ctx context.Context, client crclient.Client, obj metav1.Object, labels map[string]string) error {
	patcher := patch.NewSerialPatcher(obj.(crclient.Object), client)
	if obj.GetLabels() == nil {
		obj.SetLabels(make(map[string]string))
	}

	for key, value := range labels {
		obj.GetLabels()[key] = value
	}

	if err := patcher.Patch(ctx, obj.(crclient.Object)); err != nil {
		return err
	}

	return nil
}

// UpdateLabelsToNode updates the labels of the given node.
// It will add the new labels and remove the labels that are not in the new labels.
// It will also update the last applied labels annotation.
func UpdateLabelsToNode(ctx context.Context, client crclient.Client, node *corev1.Node, labels map[string]string) error {
	if node.Labels == nil {
		node.Labels = make(map[string]string)
	}

	if node.Annotations == nil {
		node.Annotations = make(map[string]string)
	}

	oldLabels := make(map[string]string)
	if lastAppliedLabelsStr, ok := node.Annotations[LastAppliedLabelsOnDPUKey]; ok {
		if err := json.Unmarshal([]byte(lastAppliedLabelsStr), &oldLabels); err != nil {
			return err
		}
	}

	removedLabels := GetRemovedLabels(oldLabels, labels)

	// Add labels
	for key, value := range labels {
		node.GetLabels()[key] = value
	}

	// Remove labels
	for _, key := range removedLabels {
		delete(node.GetLabels(), key)
	}

	// Update last applied labels
	if jsonStr, err := MarshalJSON(labels); err != nil {
		return fmt.Errorf("failed to marshal labels: %w", err)
	} else {
		node.Annotations[LastAppliedLabelsOnDPUKey] = jsonStr
	}

	if err := client.Update(ctx, node); err != nil {
		return err
	}

	return nil
}

// GetRemovedLabels returns the labels that are present in oldLabels but not in newLabels
func GetRemovedLabels(oldLabels, newLabels map[string]string) []string {
	var removedLabels []string
	for key := range oldLabels {
		if _, ok := newLabels[key]; !ok {
			removedLabels = append(removedLabels, key)
		}
	}
	return removedLabels
}

func DeleteObjects(ctx context.Context, client crclient.Client, objs ...crclient.Object) error {
	var errs []error
	for _, obj := range objs {
		if err := client.Delete(ctx, obj); crclient.IgnoreNotFound(err) != nil {
			errs = append(errs, fmt.Errorf("deleting error %s %s : %s err: %w",
				obj.GetObjectKind().GroupVersionKind().String(), obj.GetNamespace(), obj.GetName(), err))
			continue
		}
	}
	// Return nil in case error array is empty
	return kerrors.NewAggregate(errs)
}

func GetObjects(ctx context.Context, client crclient.Client, objects []crclient.Object) (existObjects []crclient.Object, err error) {
	for _, obj := range objects {
		nn := types.NamespacedName{
			Namespace: obj.GetNamespace(),
			Name:      obj.GetName(),
		}
		if err := client.Get(ctx, nn, obj); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return existObjects, err
		}
		existObjects = append(existObjects, obj)
	}

	return existObjects, nil
}

func GetDPUCondition(status *provisioningv1.DPUStatus, conditionType string) (int, *metav1.Condition) {
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

func DPUCondition(condType provisioningv1.DPUConditionType, reason, message string) *metav1.Condition {
	cond := &metav1.Condition{
		Type:    condType.String(),
		Status:  metav1.ConditionTrue,
		Message: message,
	}
	if reason != "" {
		cond.Reason = reason
	} else {
		cond.Reason = condType.String()
	}
	return cond
}

func GetDPUDeviceCondition(dpuDevice *provisioningv1.DPUDevice, conditionType string) (int, *metav1.Condition) {
	conditions := dpuDevice.GetConditions()

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

func SetDPUDeviceCondition(dpuDevice *provisioningv1.DPUDevice, condition *metav1.Condition) bool {

	condition.LastTransitionTime = metav1.Now()
	// Try to find this condition.
	conditionIndex, oldCondition := GetDPUDeviceCondition(dpuDevice, condition.Type)

	if oldCondition == nil {
		// We are adding new condition.
		dpuDevice.SetConditions(append(dpuDevice.GetConditions(), *condition))
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

	if isEqual {
		return false
	}

	conditions := dpuDevice.GetConditions()
	conditions[conditionIndex] = *condition
	dpuDevice.SetConditions(conditions)
	// Return true if one of the fields have changed.
	return !isEqual
}

func SetDPUCondition(status *provisioningv1.DPUStatus, condition *metav1.Condition) bool {
	condition.LastTransitionTime = metav1.Now()
	// Try to find this condition.
	conditionIndex, oldCondition := GetDPUCondition(status, condition.Type)

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

// ReplaceDaemonSetPodNodeNameNodeAffinity replaces the RequiredDuringSchedulingIgnoredDuringExecution
// NodeAffinity of the given affinity with a new NodeAffinity that selects the given nodeName.
// Note that this function assumes that no NodeAffinity conflicts with the selected nodeName.
//
// This method is copied from https://github.com/kubernetes/kubernetes/blob/dbc2b0a5c7acc349ea71a14e49913661eaf708d2/pkg/controller/daemon/util/daemonset_util.go#L176
func ReplaceDaemonSetPodNodeNameNodeAffinity(affinity *corev1.Affinity, nodename string) *corev1.Affinity {
	nodeSelReq := corev1.NodeSelectorRequirement{
		Key:      metav1.ObjectNameField,
		Operator: corev1.NodeSelectorOpIn,
		Values:   []string{nodename},
	}

	nodeSelector := &corev1.NodeSelector{
		NodeSelectorTerms: []corev1.NodeSelectorTerm{
			{
				MatchFields: []corev1.NodeSelectorRequirement{nodeSelReq},
			},
		},
	}

	if affinity == nil {
		return &corev1.Affinity{
			NodeAffinity: &corev1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: nodeSelector,
			},
		}
	}

	if affinity.NodeAffinity == nil {
		affinity.NodeAffinity = &corev1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: nodeSelector,
		}
		return affinity
	}

	nodeAffinity := affinity.NodeAffinity

	if nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
		nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution = nodeSelector
		return affinity
	}

	// Replace node selector with the new one.
	nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms = []corev1.NodeSelectorTerm{
		{
			MatchFields: []corev1.NodeSelectorRequirement{nodeSelReq},
		},
	}

	return affinity
}

func GetNamespacedName(obj metav1.Object) types.NamespacedName {
	return types.NamespacedName{
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
	}
}

func IsClusterCreated(conditions []metav1.Condition) bool {
	cond := meta.FindStatusCondition(conditions, string(provisioningv1.ConditionCreated))
	if cond == nil {
		return false
	}
	return cond.Status == metav1.ConditionTrue
}

// NewCondition creates a new metav1.Condition with the given parameters.
// todo: merge with DPUCondition()
func NewCondition(condType string, err error, reason, message string) *metav1.Condition {
	cond := &metav1.Condition{
		Type: condType,
	}
	if reason != "" {
		cond.Reason = reason
	} else {
		cond.Reason = condType
	}
	if err != nil {
		cond.Status = metav1.ConditionFalse
		cond.Message = err.Error()
	} else {
		cond.Status = metav1.ConditionTrue
		cond.Message = message
	}
	return cond
}

// NeedUpdateLabels compares two labels.
// If label 2 does not contain all the key-value pairs of label 1, then return true.
// otherwise return false
func NeedUpdateLabels(label1 map[string]string, label2 map[string]string) bool {
	for k, v := range label1 {
		if w, ok := label2[k]; !ok || w != v {
			return true
		}
	}
	return false
}

func GetClientset(ctx context.Context, client crclient.Client, dc *provisioningv1.DPUCluster) (*kubernetes.Clientset, []byte, error) {
	scrtCtx, scrtCancel := context.WithTimeout(ctx, 10*time.Second)
	defer scrtCancel()

	clientSet, kubeConfig, err := dpucluster.NewConfig(client, dc).Clientset(scrtCtx)
	if err != nil {
		return nil, nil, err
	}
	return clientSet, kubeConfig, nil
}

// CopyLabelsOrAnnotations merges source labels/annotations into target. If target is nil, it will be initialized.
func CopyLabelsOrAnnotations(target, source map[string]string) map[string]string {
	if target == nil {
		target = make(map[string]string)
	}
	for key, value := range source {
		target[key] = value
	}
	return target
}

// IsDPUInProvisioningPhase returns true if the phase is between Pending (exclusive) and OSInstalling
func IsDPUInProvisioningPhase(phase provisioningv1.DPUPhase) bool {
	switch phase {
	case provisioningv1.DPUNodeEffect,
		provisioningv1.DPUInitializeInterface,
		provisioningv1.DPUConfigFWParameters,
		provisioningv1.DPUPrepareBFB,
		provisioningv1.DPUOSInstalling:
		return true
	default:
		return false
	}
}

func IsDPUBeforeProvisioningPhase(phase provisioningv1.DPUPhase) bool {
	switch phase {
	case "",
		provisioningv1.DPUInitializing,
		provisioningv1.DPUPending:
		return true
	default:
		return false
	}
}

func IsDPUAfterProvisioningPhase(phase provisioningv1.DPUPhase) bool {
	switch phase {
	case provisioningv1.DPURebooting,
		provisioningv1.DPUCheckingHostRebootNeed,
		provisioningv1.DPUReady,
		provisioningv1.DPUError,
		provisioningv1.DPUClusterConfig,
		provisioningv1.DPUHostNetworkConfiguration,
		provisioningv1.DPUDeleting:
		return true
	default:
		return false
	}
}

func GenerateDPUName(dpuNodeName string, dpuDeviceName string) string {
	maxPrefix := MaxNameLength - (len(dpuDeviceName) + 1) // +1 for the hyphen
	if len(dpuNodeName) > maxPrefix {
		dpuNodeName = dpuNodeName[:maxPrefix]
	}
	return fmt.Sprintf("%s-%s", dpuNodeName, dpuDeviceName)
}

func MarshalJSON(obj interface{}) (string, error) {
	json, err := json.Marshal(obj)
	if err != nil {
		return "", err
	}
	return string(json), nil
}

func GenerateDPUNodeMaintenanceObjectName(dpuNodeName string, nodeEffect *provisioningv1.NodeEffect) (string, error) {
	if nodeEffect == nil {
		return "", fmt.Errorf("nodeEffect is nil")
	}

	switch {
	case nodeEffect.IsCustomAction():
		return fmt.Sprintf("%s-custom-action-%s", dpuNodeName, *nodeEffect.CustomAction), nil
	case nodeEffect.IsDrain():
		return fmt.Sprintf("%s-k8sdrain", dpuNodeName), nil
	case nodeEffect.IsHold():
		return fmt.Sprintf("%s-hold", dpuNodeName), nil
	case nodeEffect.IsTaint():
		data, err := json.Marshal(nodeEffect.Taint)
		if err != nil {
			return "", fmt.Errorf("failed to marshal taint: %w", err)
		}
		hash := sha256.Sum256(data)
		return fmt.Sprintf("%s-taint-%s", dpuNodeName, fmt.Sprintf("%x", hash)[:8]), nil
	case nodeEffect.IsCustomLabel():
		data, err := json.Marshal(nodeEffect.CustomLabel)
		if err != nil {
			return "", fmt.Errorf("failed to marshal custom label: %w", err)
		}
		hash := sha256.Sum256(data)
		return fmt.Sprintf("%s-cl-%s", dpuNodeName, fmt.Sprintf("%x", hash)[:8]), nil
	}
	return "", fmt.Errorf("invalid node effect type: %v", nodeEffect.String())
}

func IsNodeEffectApplied(dpunodemaintenance *provisioningv1.DPUNodeMaintenance) bool {
	if condition := meta.FindStatusCondition(dpunodemaintenance.Status.Conditions, string(provisioningv1.ConditionNodeEffectApplied)); condition != nil {
		return condition.Status == metav1.ConditionTrue
	}
	return false
}

func IsDPUNodeReady(dpuNode *provisioningv1.DPUNode) bool {
	if condition := meta.FindStatusCondition(dpuNode.Status.Conditions, provisioningv1.DPUNodeConditionReady.String()); condition != nil {
		return condition.Status == metav1.ConditionTrue
	}
	return false
}

func GetDPUPhases(ctx context.Context, client crclient.Client, dpuNode *provisioningv1.DPUNode, dpuPhases map[string]struct{}) (err error) {
	for _, dpuDevice := range dpuNode.Spec.DPUs {
		dpu := &provisioningv1.DPU{}
		if err := client.Get(ctx, types.NamespacedName{Namespace: dpuNode.Namespace, Name: GenerateDPUName(dpuNode.Name, dpuDevice.Name)}, dpu); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return err
		}
		dpuPhases[string(dpu.Status.Phase)] = struct{}{}
	}
	return nil
}

func GetDPUsWithPhase(ctx context.Context, client crclient.Client, dpuNode *provisioningv1.DPUNode, phase provisioningv1.DPUPhase) ([]*provisioningv1.DPU, error) {
	dpus := make([]*provisioningv1.DPU, 0)
	for _, dpuDevice := range dpuNode.Spec.DPUs {
		dpu := &provisioningv1.DPU{}
		if err := client.Get(ctx, types.NamespacedName{Namespace: dpuNode.Namespace, Name: GenerateDPUName(dpuNode.Name, dpuDevice.Name)}, dpu); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return nil, err
		}
		if string(dpu.Status.Phase) == string(phase) {
			dpus = append(dpus, dpu.DeepCopy())
		}
	}
	return dpus, nil
}

func ContainsDPUPhase(phases map[string]struct{}, phase provisioningv1.DPUPhase) bool {
	_, exist := phases[string(phase)]
	return exist
}

func ContainsDPUPhases(phases map[string]struct{}, subPhases map[string]struct{}) bool {
	for phase := range subPhases {
		if _, exist := phases[phase]; exist {
			return true
		}
	}
	return false
}

func BuildContextWithTargetPCIAddress(ctx context.Context, pciAddressFromDPUObject string) context.Context {
	md := metadata.Pairs(
		PCIAddressTargetKey, strings.ReplaceAll(pciAddressFromDPUObject, "-", ":"),
	)
	return metadata.NewOutgoingContext(ctx, md)
}
