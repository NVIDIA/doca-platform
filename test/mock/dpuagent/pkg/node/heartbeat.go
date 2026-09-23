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

package node

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
)

const (
	// FakeNodeLabel marks the Node as a stand-in so e2e tooling can tell it from real DPUs.
	FakeNodeLabel = "e2e.test.io/fake-node"

	leaseNamespace       = "kube-node-lease"
	leaseDurationSeconds = 40
	heartbeatInterval    = 10 * time.Second
)

// RegisterNode creates the Node object the way a kubelet does on first start. An existing Node
// with the same name (reprovisioning) is adopted, which NodeRestriction allows for the node itself.
func (j *Joiner) RegisterNode(ctx context.Context, kubeletVersion string) error {
	if j.clientset == nil {
		return fmt.Errorf("node credentials not ready")
	}
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: j.nodeName,
			Labels: map[string]string{
				corev1.LabelHostname:   j.nodeName,
				corev1.LabelArchStable: "arm64",
				corev1.LabelOSStable:   "linux",
				FakeNodeLabel:          "true",
			},
		},
		Spec: corev1.NodeSpec{},
	}
	_, err := j.clientset.CoreV1().Nodes().Create(ctx, node, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create Node %s: %w", j.nodeName, err)
	}
	if apierrors.IsAlreadyExists(err) {
		klog.InfoS("DPU cluster Node already exists, taking it over", "node", j.nodeName)
	} else {
		klog.InfoS("DPU cluster Node registered", "node", j.nodeName)
	}
	return j.patchStatus(ctx, kubeletVersion)
}

// RunHeartbeat renews the node Lease and re-posts a Ready status every 10 seconds until ctx is
// canceled, which is what keeps cutil.IsNodeReady true for the Cluster Config phase.
func (j *Joiner) RunHeartbeat(ctx context.Context, kubeletVersion string) {
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()
	for {
		if err := j.renewLease(ctx); err != nil && ctx.Err() == nil {
			klog.ErrorS(err, "node lease renewal failed", "node", j.nodeName)
		}
		if err := j.patchStatus(ctx, kubeletVersion); err != nil && ctx.Err() == nil {
			klog.ErrorS(err, "node status heartbeat failed", "node", j.nodeName)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (j *Joiner) renewLease(ctx context.Context) error {
	leases := j.clientset.CoordinationV1().Leases(leaseNamespace)
	now := metav1.NewMicroTime(time.Now())
	lease, err := leases.Get(ctx, j.nodeName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		node, err := j.clientset.CoreV1().Nodes().Get(ctx, j.nodeName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("get Node for lease owner: %w", err)
		}
		lease = &coordinationv1.Lease{
			ObjectMeta: metav1.ObjectMeta{
				Name:      j.nodeName,
				Namespace: leaseNamespace,
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: "v1",
					Kind:       "Node",
					Name:       node.Name,
					UID:        node.UID,
				}},
			},
			Spec: coordinationv1.LeaseSpec{
				HolderIdentity:       ptr.To(j.nodeName),
				LeaseDurationSeconds: ptr.To(int32(leaseDurationSeconds)),
				RenewTime:            &now,
			},
		}
		_, err = leases.Create(ctx, lease, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	lease.Spec.HolderIdentity = ptr.To(j.nodeName)
	lease.Spec.LeaseDurationSeconds = ptr.To(int32(leaseDurationSeconds))
	lease.Spec.RenewTime = &now
	_, err = leases.Update(ctx, lease, metav1.UpdateOptions{})
	return err
}

func (j *Joiner) patchStatus(ctx context.Context, kubeletVersion string) error {
	now := metav1.Now()
	condition := func(t corev1.NodeConditionType, status corev1.ConditionStatus, reason, message string) corev1.NodeCondition {
		return corev1.NodeCondition{Type: t, Status: status, Reason: reason, Message: message, LastHeartbeatTime: now, LastTransitionTime: now}
	}
	status := corev1.NodeStatus{
		Conditions: []corev1.NodeCondition{
			condition(corev1.NodeReady, corev1.ConditionTrue, "KubeletReady", "mock-dpuagent kubelet is posting ready status"),
			condition(corev1.NodeMemoryPressure, corev1.ConditionFalse, "KubeletHasSufficientMemory", "kubelet has sufficient memory available"),
			condition(corev1.NodeDiskPressure, corev1.ConditionFalse, "KubeletHasNoDiskPressure", "kubelet has no disk pressure"),
			condition(corev1.NodePIDPressure, corev1.ConditionFalse, "KubeletHasSufficientPID", "kubelet has sufficient PID available"),
		},
		Capacity: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("16"),
			corev1.ResourceMemory: resource.MustParse("32Gi"),
			corev1.ResourcePods:   resource.MustParse("110"),
		},
		Allocatable: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("16"),
			corev1.ResourceMemory: resource.MustParse("31Gi"),
			corev1.ResourcePods:   resource.MustParse("110"),
		},
		NodeInfo: corev1.NodeSystemInfo{
			Architecture:            "arm64",
			OperatingSystem:         "linux",
			OSImage:                 "Ubuntu 24.04 LTS (mock-dpuagent)",
			KernelVersion:           "6.8.0-mock",
			ContainerRuntimeVersion: "containerd://mock",
			KubeletVersion:          kubeletVersion,
		},
		Addresses: []corev1.NodeAddress{{Type: corev1.NodeHostName, Address: j.nodeName}},
	}
	// Strategic merge on conditions (merge key "type") keeps conditions other writers set.
	patch, err := json.Marshal(map[string]interface{}{"status": status})
	if err != nil {
		return err
	}
	_, err = j.clientset.CoreV1().Nodes().Patch(ctx, j.nodeName, types.StrategicMergePatchType, patch, metav1.PatchOptions{}, "status")
	if err != nil {
		return fmt.Errorf("patch Node %s status: %w", j.nodeName, err)
	}
	return nil
}
