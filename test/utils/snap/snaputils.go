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

// Package snap holds the assertions the SNAP storage tests make: on the DPF storage objects the stack
// produces (DPUStorageVendor through DPUVolumeAttachment) and on the VirtioFS mount the workload ends up
// with. They take a client and plain names, so any suite can call them.
//
// Deploying the stack stays with the suite (test/e2e/snap.go): that part is tied to the e2e config, its
// manifest handling and the DPU pinning.
package snap

import (
	"context"
	"fmt"
	"strings"
	"time"

	storagev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/conditions"
	"github.com/nvidia/doca-platform/test/utils/netshoot"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// ServiceReadyTimeout covers the storage control plane reconciling its own objects, which waits on the
	// DPU-side services coming up.
	ServiceReadyTimeout = 20 * time.Minute
	// VolumeReadyTimeout covers provisioning a volume on the backend and attaching it to the DPU.
	VolumeReadyTimeout = 10 * time.Minute
	// MountTimeout covers the host mounting an already attached volume.
	MountTimeout = 5 * time.Minute
	// PollInterval is the polling interval for all of the waits above.
	PollInterval = 1 * time.Second
)

// VerifyDPUStorageVendorReady waits for the named DPUStorageVendor to become Ready.
func VerifyDPUStorageVendorReady(ctx context.Context, c client.Client, namespace, name string) {
	By(fmt.Sprintf("Verifying DPUStorageVendor %s is Ready", name))
	Eventually(func(g Gomega) {
		vendor := &storagev1.DPUStorageVendor{}
		g.Expect(c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, vendor)).To(Succeed())
		g.Expect(meta.IsStatusConditionTrue(vendor.Status.Conditions, string(conditions.TypeReady))).To(BeTrue(), "DPUStorageVendor %s is not Ready", name)
	}).WithTimeout(ServiceReadyTimeout).WithPolling(PollInterval).Should(Succeed())
}

// VerifyDPUStoragePolicyReady waits for the named DPUStoragePolicy to become Ready.
func VerifyDPUStoragePolicyReady(ctx context.Context, c client.Client, namespace, name string) {
	By(fmt.Sprintf("Verifying DPUStoragePolicy %s is Ready", name))
	Eventually(func(g Gomega) {
		policy := &storagev1.DPUStoragePolicy{}
		g.Expect(c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, policy)).To(Succeed())
		g.Expect(meta.IsStatusConditionTrue(policy.Status.Conditions, string(conditions.TypeReady))).To(BeTrue(), "DPUStoragePolicy %s is not Ready", name)
	}).WithTimeout(ServiceReadyTimeout).WithPolling(PollInterval).Should(Succeed())
}

// VerifyDPUVolumeBound waits for the single workload DPUVolume in namespace to reach the Bound phase.
func VerifyDPUVolumeBound(ctx context.Context, c client.Client, namespace string) {
	By("Verifying the workload DPUVolume is Bound")
	Eventually(func(g Gomega) {
		volumes := &storagev1.DPUVolumeList{}
		g.Expect(c.List(ctx, volumes, client.InNamespace(namespace))).To(Succeed())
		// Single-replica workload -> exactly one DPUVolume.
		g.Expect(volumes.Items).To(HaveLen(1), "expected exactly one DPUVolume for the workload")
		v := &volumes.Items[0]
		g.Expect(v.Status.Phase).ToNot(BeNil(), "DPUVolume %s has no phase yet", v.Name)
		g.Expect(*v.Status.Phase).To(Equal(storagev1.DPUVolumePhaseBound), "DPUVolume %s is not Bound", v.Name)
	}).WithTimeout(VolumeReadyTimeout).WithPolling(PollInterval).Should(Succeed())
}

// VerifySVVolumeAttachmentAttached waits, in the DPU cluster, for the backend volume to be attached to the
// DPU. The host controller creates the SVVolumeAttachment because the backend CSIDriver sets
// attachRequired, and the nvidia-external-attacher next to that driver marks it attached once
// ControllerPublishVolume succeeds.
func VerifySVVolumeAttachmentAttached(ctx context.Context, dpuClusterClient client.Client, namespace string) {
	By("Verifying the backend SVVolumeAttachment is attached in the DPU cluster")
	Eventually(func(g Gomega) {
		attachments := &storagev1.SVVolumeAttachmentList{}
		g.Expect(dpuClusterClient.List(ctx, attachments, client.InNamespace(namespace))).To(Succeed())
		// Single-replica workload -> exactly one SVVolumeAttachment.
		g.Expect(attachments.Items).To(HaveLen(1), "expected exactly one SVVolumeAttachment for the workload")
		a := &attachments.Items[0]
		if a.Status.AttachError != nil {
			// Surfaces the CSI error instead of a bare timeout: it is the reason the attach never lands.
			g.Expect(a.Status.AttachError.Message).To(BeEmpty(), "SVVolumeAttachment %s failed to attach", a.Name)
		}
		g.Expect(a.Status.Attached).To(BeTrue(), "SVVolumeAttachment %s is not attached", a.Name)
	}).WithTimeout(VolumeReadyTimeout).WithPolling(PollInterval).Should(Succeed())
}

// VerifyDPUVolumeAttachmentReady waits for a Ready DPUVolumeAttachment and sanity-checks that SNAP
// reported a VirtioFS filesystem tag (used by the host to mount the volume).
func VerifyDPUVolumeAttachmentReady(ctx context.Context, c client.Client, namespace string) {
	By("Verifying a DPUVolumeAttachment is Ready with a VirtioFS filesystem tag")
	Eventually(func(g Gomega) {
		attachments := &storagev1.DPUVolumeAttachmentList{}
		g.Expect(c.List(ctx, attachments, client.InNamespace(namespace))).To(Succeed())
		// Single-replica workload -> exactly one DPUVolumeAttachment. Waiting for a Ready one among
		// several would accept a leftover from an earlier run and hide that this one never became Ready.
		g.Expect(attachments.Items).To(HaveLen(1), "expected exactly one DPUVolumeAttachment for the workload")
		a := &attachments.Items[0]
		g.Expect(meta.IsStatusConditionTrue(a.Status.Conditions, string(conditions.TypeReady))).To(BeTrue(), "DPUVolumeAttachment %s is not Ready", a.Name)
		g.Expect(a.Status.DPU).ToNot(BeNil(), "DPUVolumeAttachment %s has no DPU status", a.Name)
		g.Expect(a.Status.DPU.VirtioFSAttrs).ToNot(BeNil(), "DPUVolumeAttachment %s has no VirtioFSAttrs", a.Name)
		g.Expect(a.Status.DPU.VirtioFSAttrs.FilesystemTag).ToNot(BeNil(), "DPUVolumeAttachment %s has no VirtioFS filesystem tag", a.Name)
		g.Expect(*a.Status.DPU.VirtioFSAttrs.FilesystemTag).ToNot(BeEmpty(), "DPUVolumeAttachment %s has an empty VirtioFS filesystem tag", a.Name)
	}).WithTimeout(VolumeReadyTimeout).WithPolling(PollInterval).Should(Succeed())
}

// WaitForWorkloadPodRunning waits for the workload pod to be Running.
func WaitForWorkloadPodRunning(ctx context.Context, c client.Client, namespace, podName string) {
	By(fmt.Sprintf("Waiting for workload pod %s/%s to be Running", namespace, podName))
	Eventually(func(g Gomega) {
		pod := &corev1.Pod{}
		g.Expect(c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: podName}, pod)).To(Succeed())
		g.Expect(pod.Status.Phase).To(Equal(corev1.PodRunning), "workload pod %s is not Running", podName)
	}).WithTimeout(VolumeReadyTimeout).WithPolling(PollInterval).Should(Succeed())
}

// VerifyVirtioFSMount asserts that mountPath is a virtiofs mount inside the pod and that a write followed
// by a read round-trips, via an exec on the cluster the pod runs in.
func VerifyVirtioFSMount(restClient *rest.RESTClient, restConfig *rest.Config, namespace, podName, mountPath string) {
	By(fmt.Sprintf("Verifying %s is a virtiofs mount in pod %s/%s", mountPath, namespace, podName))
	Eventually(func(g Gomega) {
		out, err := netshoot.ExecInPodOnce(restClient, restConfig, namespace, podName, []string{"sh", "-c", fmt.Sprintf("df -T %s", mountPath)})
		g.Expect(err).ToNot(HaveOccurred(), "df -T %s failed in pod %s: %s", mountPath, podName, out)
		g.Expect(out).To(ContainSubstring("virtiofs"), "%s is not a virtiofs mount in pod %s: %s", mountPath, podName, out)
	}).WithTimeout(MountTimeout).WithPolling(PollInterval).Should(Succeed())

	By(fmt.Sprintf("Verifying read/write on %s in pod %s/%s", mountPath, namespace, podName))
	Eventually(func(g Gomega) {
		out, err := netshoot.ExecInPodOnce(restClient, restConfig, namespace, podName, []string{"sh", "-c", fmt.Sprintf("echo e2e > %s/e2e && cat %s/e2e", mountPath, mountPath)})
		g.Expect(err).ToNot(HaveOccurred(), "read/write on %s failed in pod %s: %s", mountPath, podName, out)
		g.Expect(strings.TrimSpace(out)).To(Equal("e2e"), "unexpected read/write output in pod %s: %s", podName, out)
	}).WithTimeout(MountTimeout).WithPolling(PollInterval).Should(Succeed())
}

// VerifyWorkloadHeartbeat verifies the workload container's own write loop lands on the mount by reading
// the heartbeat file it maintains in a loop (see test/objects/storage/sts-fs.yaml).
func VerifyWorkloadHeartbeat(restClient *rest.RESTClient, restConfig *rest.Config, namespace, podName, heartbeatFile string) {
	By(fmt.Sprintf("Verifying the workload heartbeat file %s in pod %s/%s", heartbeatFile, namespace, podName))
	Eventually(func(g Gomega) {
		out, err := netshoot.ExecInPodOnce(restClient, restConfig, namespace, podName, []string{"sh", "-c", "cat " + heartbeatFile})
		g.Expect(err).ToNot(HaveOccurred(), "reading %s failed in pod %s: %s", heartbeatFile, podName, out)
		g.Expect(strings.TrimSpace(out)).ToNot(BeEmpty(), "workload heartbeat file %s is empty in pod %s", heartbeatFile, podName)
	}).WithTimeout(MountTimeout).WithPolling(PollInterval).Should(Succeed())
}
