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

// Package snap holds assertions for SNAP storage objects and consumer I/O.
//
// Deploying the stack stays with the e2e suite because it is tied to its config, manifest handling, and
// DPU pinning.
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

// VerifyDPUVolumesBound waits for all workload DPUVolumes in namespace to reach the Bound phase.
func VerifyDPUVolumesBound(ctx context.Context, c client.Client, namespace string, expectedCount int) {
	By("Verifying all workload DPUVolumes are Bound")
	Eventually(func(g Gomega) {
		volumes := &storagev1.DPUVolumeList{}
		g.Expect(c.List(ctx, volumes, client.InNamespace(namespace))).To(Succeed())
		g.Expect(volumes.Items).To(HaveLen(expectedCount), "expected exactly %d DPUVolumes for the workloads", expectedCount)
		for i := range volumes.Items {
			volume := &volumes.Items[i]
			g.Expect(volume.Status.Phase).ToNot(BeNil(), "DPUVolume %s has no phase yet", volume.Name)
			g.Expect(*volume.Status.Phase).To(Equal(storagev1.DPUVolumePhaseBound), "DPUVolume %s is not Bound", volume.Name)
		}
	}).WithTimeout(VolumeReadyTimeout).WithPolling(PollInterval).Should(Succeed())
}

// VerifySVVolumeAttachmentsAttached waits for all backend attachments to become attached.
func VerifySVVolumeAttachmentsAttached(ctx context.Context, dpuClusterClient client.Client, namespace string, expectedCount int) {
	By("Verifying all backend SVVolumeAttachments are attached in the DPU cluster")
	Eventually(func(g Gomega) {
		attachments := &storagev1.SVVolumeAttachmentList{}
		g.Expect(dpuClusterClient.List(ctx, attachments, client.InNamespace(namespace))).To(Succeed())
		g.Expect(attachments.Items).To(HaveLen(expectedCount), "expected exactly %d SVVolumeAttachments for the workloads", expectedCount)
		for i := range attachments.Items {
			attachment := &attachments.Items[i]
			if attachment.Status.AttachError != nil {
				g.Expect(attachment.Status.AttachError.Message).To(BeEmpty(), "SVVolumeAttachment %s failed to attach", attachment.Name)
			}
			g.Expect(attachment.Status.Attached).To(BeTrue(), "SVVolumeAttachment %s is not attached", attachment.Name)
		}
	}).WithTimeout(VolumeReadyTimeout).WithPolling(PollInterval).Should(Succeed())
}

// VerifyVirtioFSAttachmentsReady waits for all attachments and checks their VirtioFS tags.
func VerifyVirtioFSAttachmentsReady(ctx context.Context, c client.Client, namespace string, expectedCount int) {
	By("Verifying all DPUVolumeAttachments are Ready with VirtioFS filesystem tags")
	Eventually(func(g Gomega) {
		attachments := &storagev1.DPUVolumeAttachmentList{}
		g.Expect(c.List(ctx, attachments, client.InNamespace(namespace))).To(Succeed())
		g.Expect(attachments.Items).To(HaveLen(expectedCount), "expected exactly %d DPUVolumeAttachments for the workloads", expectedCount)
		for i := range attachments.Items {
			attachment := &attachments.Items[i]
			g.Expect(meta.IsStatusConditionTrue(attachment.Status.Conditions, string(conditions.TypeReady))).To(BeTrue(),
				"DPUVolumeAttachment %s is not Ready", attachment.Name)
			g.Expect(attachment.Status.DPU).ToNot(BeNil(), "DPUVolumeAttachment %s has no DPU status", attachment.Name)
			g.Expect(attachment.Status.DPU.VirtioFSAttrs).ToNot(BeNil(),
				"DPUVolumeAttachment %s has no VirtioFS attributes", attachment.Name)
			g.Expect(attachment.Status.DPU.VirtioFSAttrs.FilesystemTag).ToNot(BeNil(),
				"DPUVolumeAttachment %s has no VirtioFS filesystem tag", attachment.Name)
			g.Expect(*attachment.Status.DPU.VirtioFSAttrs.FilesystemTag).ToNot(BeEmpty(),
				"DPUVolumeAttachment %s has an empty VirtioFS filesystem tag", attachment.Name)
		}
	}).WithTimeout(VolumeReadyTimeout).WithPolling(PollInterval).Should(Succeed())
}

// VerifyNVMeAttachmentsReady waits for all attachments and checks their NVMe namespace IDs.
func VerifyNVMeAttachmentsReady(ctx context.Context, c client.Client, namespace string, expectedCount int) {
	By("Verifying all DPUVolumeAttachments are Ready with NVMe namespace IDs")
	Eventually(func(g Gomega) {
		attachments := &storagev1.DPUVolumeAttachmentList{}
		g.Expect(c.List(ctx, attachments, client.InNamespace(namespace))).To(Succeed())
		g.Expect(attachments.Items).To(HaveLen(expectedCount), "expected exactly %d DPUVolumeAttachments for the workloads", expectedCount)
		for i := range attachments.Items {
			attachment := &attachments.Items[i]
			g.Expect(meta.IsStatusConditionTrue(attachment.Status.Conditions, string(conditions.TypeReady))).To(BeTrue(),
				"DPUVolumeAttachment %s is not Ready", attachment.Name)
			g.Expect(attachment.Status.DPU).ToNot(BeNil(), "DPUVolumeAttachment %s has no DPU status", attachment.Name)
			g.Expect(attachment.Status.DPU.NVMEAttrs).ToNot(BeNil(),
				"DPUVolumeAttachment %s has no NVMe attributes", attachment.Name)
			g.Expect(attachment.Status.DPU.NVMEAttrs.NamespaceID).ToNot(BeNil(),
				"DPUVolumeAttachment %s has no NVMe namespace ID", attachment.Name)
			g.Expect(*attachment.Status.DPU.NVMEAttrs.NamespaceID).To(BeNumerically(">", 0),
				"DPUVolumeAttachment %s has an invalid NVMe namespace ID", attachment.Name)
		}
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

// VerifyNVMeRawBlockIO writes a unique marker to a raw block device and reads it back.
func VerifyNVMeRawBlockIO(restClient *rest.RESTClient, restConfig *rest.Config, namespace, podName, devicePath string) {
	By(fmt.Sprintf("Verifying raw block I/O on %s in pod %s/%s", devicePath, namespace, podName))
	marker := fmt.Sprintf("snap-nvme-e2e-%d", time.Now().UnixNano())
	command := fmt.Sprintf(
		"set -e; err_file=$(mktemp); read_file=$(mktemp); "+
			"trap 'rm -f \"$err_file\" \"$read_file\"' EXIT; test -b %[1]s; "+
			"if ! printf '%[2]s' | dd of=%[1]s bs=1 seek=4096 conv=notrunc 2>\"$err_file\"; then "+
			"cat \"$err_file\" >&2; exit 1; fi; sync; "+
			"if ! dd if=%[1]s of=\"$read_file\" bs=1 skip=4096 count=%[3]d 2>\"$err_file\"; then "+
			"cat \"$err_file\" >&2; exit 1; fi; actual=\"$(cat \"$read_file\")\"; "+
			"test \"$actual\" = '%[2]s'; printf '%%s' \"$actual\"",
		devicePath, marker, len(marker))
	Eventually(func(g Gomega) {
		out, err := netshoot.ExecInPodOnce(restClient, restConfig, namespace, podName, []string{"sh", "-c", command})
		g.Expect(err).ToNot(HaveOccurred(), "raw block I/O failed in pod %s: %s", podName, out)
		g.Expect(strings.TrimSpace(out)).To(Equal(marker))
	}).WithTimeout(MountTimeout).WithPolling(PollInterval).Should(Succeed())
}
