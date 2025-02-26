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
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	dpucluster "github.com/nvidia/doca-platform/internal/dpucluster"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// RequeueInterval is the interval to requeue the request.
	RequeueInterval = 5 * time.Second
	// CFGExtension is the extension of the BFB configuration file.
	CFGExtension = ".cfg"
	// DPUSetNameLabel is the label that indicates the name of the DPUSet.
	DPUSetNameLabel = "provisioning.dpu.nvidia.com/dpuset-name"
	// DPUSetNamespaceLabel is the label that indicates the namespace of the DPUSet.
	DPUSetNamespaceLabel = "provisioning.dpu.nvidia.com/dpuset-namespace"
	// DPUPCIAddress is the label that indicates the PCI address of the DPU.
	DPUPCIAddress = "dpu-%d-pci-address"
	// DPUPFName is the label that indicates the PF name of the DPU.
	DPUPFName = "dpu-%d-pf0-name"
	// DPUPCIAddressLabel is the label that indicates the PCI address of the DPU.
	DPUPCIAddressLabel = "provisioning.dpu.nvidia.com/dpu-pciAddress"
	// DPUPFNameLabel is the label that indicates the PF name of the DPU.
	DPUPFNameLabel = "provisioning.dpu.nvidia.com/dpu-pf-name"
	// DPUHostIPLabel is the label that indicates the host IP of the DPU.
	DPUHostIPLabel = "provisioning.dpu.nvidia.com/dpu-host-ip"
	// TolerationNotReadyKey is the key for the NotReady taint.
	TolerationNotReadyKey = "node.kubernetes.io/not-ready"
	// TolerationUnreachableyKey is the key for the Unreachable taint.
	TolerationUnreachableyKey = "node.kubernetes.io/unreachable"
	// TolerationUnschedulableKey is the key for the Unschedulable taint.
	TolerationUnschedulableKey = "node.kubernetes.io/unschedulable"
	// DPUOOBBridgeConfiguredLabel is the label that indicates that the DPU OOB bridge is configured.
	DPUOOBBridgeConfiguredLabel = "dpu-oob-bridge-configured"
	// NodeFeatureDiscoveryLabelPrefix is the prefix for all NodeFeatureDiscovery labels.
	NodeFeatureDiscoveryLabelPrefix = "feature.node.kubernetes.io/"
	// NodeMaintenanceRequestorID is the requestor ID used for NodeMaintenance CRs
	NodeMaintenanceRequestorID = "dpu.nvidia.com"
	// ProvisioningGroupName is the provisioning group, used to identify provisioning as
	// additional Requestors in NodeMaintenance CR.
	ProvisioningGroupName = "provisioning.dpu.nvidia.com"
)

var (
	// Location of BFB binary files
	BFBBaseDir = "bfb"
)

func GenerateBFCFGFileName(dpuName string) string {
	return fmt.Sprintf("%s%s", dpuName, CFGExtension)
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

func GenerateBFBVersionFromURL(bfbURL string) string {
	base := path.Base(bfbURL)
	version := strings.TrimSuffix(base, path.Ext(base))
	return version
}

func GenerateDMSPodName(dpuName string) string {
	return fmt.Sprintf("%s-%s", dpuName, "dms")
}

func GenerateCACertName(dpuNamespace string) string {
	return fmt.Sprintf("%s-%s", dpuNamespace, "ca-cert")
}

func GenerateDMSServerCertName(dpuName string) string {
	return fmt.Sprintf("%s-%s", dpuName, "dms-server-cert")
}

func GenerateCASecretName(dpuNamespace string) string {
	return fmt.Sprintf("%s-%s", dpuNamespace, "ca-secret")
}

func GenerateHostnetworkPodName(dpuName string) string {
	return fmt.Sprintf("%s-%s", dpuName, "hostnetwork")
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

func GetPCIAddrFromLabel(labels map[string]string, removePrefix bool) (string, error) {
	// the value of pci address from the node label likes: 0000_4b_00
	if pciAddress, ok := labels[DPUPCIAddressLabel]; ok {
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

	return "", fmt.Errorf("not found pci address")
}

func IsNodeReady(node *corev1.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

func AddLabelsToNode(ctx context.Context, client crclient.Client, node *corev1.Node, labels map[string]string) error {

	if node.Labels == nil {
		node.Labels = make(map[string]string)
	}
	for key, value := range labels {
		node.Labels[key] = value
	}
	if err := client.Update(ctx, node); err != nil {
		return err
	}
	return nil
}

func DeleteObject(client crclient.Client, obj crclient.Object) error {
	err := client.Delete(context.TODO(), obj)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}

func GetObjects(client crclient.Client, objects []crclient.Object) (existObjects []crclient.Object, err error) {
	for _, obj := range objects {
		nn := types.NamespacedName{
			Namespace: obj.GetNamespace(),
			Name:      obj.GetName(),
		}
		if err := client.Get(context.TODO(), nn, obj); err != nil {
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

func ComputeMD5(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close() //nolint: errcheck

	hash := md5.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
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

func GeneratePodToleration(nodeEffect provisioningv1.NodeEffect) []corev1.Toleration {
	tolerations := []corev1.Toleration{
		{
			Key:      TolerationNotReadyKey,
			Operator: corev1.TolerationOpExists,
			Effect:   corev1.TaintEffectNoExecute,
		},
		{
			Key:      TolerationNotReadyKey,
			Operator: corev1.TolerationOpExists,
			Effect:   corev1.TaintEffectNoSchedule,
		},
		{
			Key:      TolerationUnreachableyKey,
			Operator: corev1.TolerationOpExists,
			Effect:   corev1.TaintEffectNoExecute,
		},
		{
			Key:      TolerationUnschedulableKey,
			Operator: corev1.TolerationOpExists,
			Effect:   corev1.TaintEffectNoSchedule,
		},
	}

	if nodeEffect.Taint != nil {
		t := corev1.Toleration{
			Key:      nodeEffect.Taint.Key,
			Operator: corev1.TolerationOpEqual,
			Value:    nodeEffect.Taint.Value,
			Effect:   nodeEffect.Taint.Effect,
		}
		tolerations = append(tolerations, t)
	}

	return tolerations
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
		Type:   condType,
		Reason: reason,
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

func AdminKubeConfigPath(dc provisioningv1.DPUCluster) string {
	return filepath.Join("/kubeconfig", fmt.Sprintf("%s_%s_%s", dc.Name, dc.Namespace, dc.UID))
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
