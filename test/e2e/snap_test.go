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

package e2e

import (
	"github.com/nvidia/doca-platform/test/e2e/cleanup"
	snaputils "github.com/nvidia/doca-platform/test/utils/snap"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("DPF System tests - SNAP", Labels{Domain.DPFSystem, Domain.SNAP}, Ordered, func() {
	BeforeAll(func() {
		snapDeploymentScope = cleanupTracker.RegisterScope(cleanup.NamedScopeManual("snap-deployment"))
		snapControlPlaneScope = cleanupTracker.RegisterScope(cleanup.NamedScopeManual("snap-control-plane"))
		snapVirtioFSWorkloadScope = cleanupTracker.RegisterScope(cleanup.NamedScopeManual("snap-virtiofs-workload"))
		snapNVMeWorkloadScope = cleanupTracker.RegisterScope(cleanup.NamedScopeManual("snap-nvme-workload"))

		if !input.hasDpuNodes() {
			Skip("Skip test as there are no DPU nodes")
		}

		snapNVMeWorkloadScope.CleanupBefore()
		snapVirtioFSWorkloadScope.CleanupBefore()
		snapControlPlaneScope.CleanupBefore()
		snapDeploymentScope.CleanupBefore()

		provInput := getProvisionDPUClustersInput()

		By("Creating DPU cluster client for verification")
		getDPUClusterClients(ctx, provInput)
		Expect(dpuClusterClient).ToNot(BeEmpty(), "no DPU cluster clients were created")

		// The DPUDeployment drives DPU provisioning, so the stack is applied before the provisioning wait.
		By("Deploying the SNAP storage stack")
		deploySNAPStorageStack(ctx, input, *conf)

		By("Waiting for provisioning")
		VerifyDPUClusterWithNodes(ctx, provInput)

		By("Waiting for DPU cluster pods to be ready")
		VerifyClusterPods(ctx, dpuClusterClient[0], systemPodsToVerify)

		By("Waiting for DPFOperatorConfig to be ready")
		VerifyDPFOperatorConfigReady(ctx, input.client, snapProvisioningTimeout)

	})

	AfterAll(func() {
		snapControlPlaneScope.CleanupAfter()
		snapDeploymentScope.CleanupAfter()
		if !snapDeploymentScope.ResourcesExist() {
			clearPinnedDPUDeviceLabel(ctx, input.client, dpfOperatorSystemNamespace)
		}
	})

	Context("SNAP services", Labels{Domain.RequiresNodes}, Ordered, func() {
		It("should run the shared DPU-side SNAP services", func() {
			VerifyClusterPods(ctx, dpuClusterClient[0], []string{"doca-snap", "fs-storage-dpu-plugin", "block-storage-dpu-plugin", "snap-node-driver"})
		})
		It("should run both host-side CSI services and the host controller", func() {
			VerifyClusterPods(ctx, testClient, []string{"snap-csi-plugin-virtiofs", "snap-csi-plugin-nvme", "snap-host-controller"})
		})
		It("should run the csi-hostpath backend on the DPU cluster", func() {
			VerifyClusterPods(ctx, dpuClusterClient[0], []string{"csi-hostpathplugin"})
		})
	})

	Context("Storage control plane", Labels{Domain.RequiresNodes}, Ordered, func() {
		It("should have Ready filesystem and block DPUStorageVendors", func() {
			snaputils.VerifyDPUStorageVendorReady(ctx, input.client, dpfOperatorSystemNamespace, snapVirtioFSStorageVendorName)
			snaputils.VerifyDPUStorageVendorReady(ctx, input.client, dpfOperatorSystemNamespace, snapNVMeStorageVendorName)
		})
		It("should have Ready filesystem and block DPUStoragePolicies", func() {
			snaputils.VerifyDPUStoragePolicyReady(ctx, input.client, dpfOperatorSystemNamespace, snapVirtioFSStoragePolicyName)
			snaputils.VerifyDPUStoragePolicyReady(ctx, input.client, dpfOperatorSystemNamespace, snapNVMeStoragePolicyName)
		})
	})

	Context("VirtioFS volume on a hot-plugged PF (trusted)", Labels{Domain.RequiresNodes}, Ordered, func() {
		BeforeAll(func() {
			snapVirtioFSWorkloadScope.CleanupBefore()
		})

		AfterAll(func() {
			snapVirtioFSWorkloadScope.CleanupAfter()
		})

		It("should create the workload StorageClass and four-replica StatefulSet", func() {
			applySNAPWorkload(ctx, input, snapVirtioFSWorkloadScope, snapVirtioFSNamespace,
				conf.SNAPVirtioFSStorageClassPath, conf.SNAPVirtioFSWorkloadPath)
		})
		It("should bind a DPUVolume for each workload pod", func() {
			snaputils.VerifyDPUVolumesBound(ctx, input.client, dpfOperatorSystemNamespace, snapWorkloadReplicas)
		})
		It("should attach every backend volume to the DPU", func() {
			snaputils.VerifySVVolumeAttachmentsAttached(ctx, dpuClusterClient[0], dpfOperatorSystemNamespace, snapWorkloadReplicas)
		})
		It("should have Ready DPUVolumeAttachments with VirtioFS filesystem tags", func() {
			snaputils.VerifyVirtioFSAttachmentsReady(ctx, input.client, dpfOperatorSystemNamespace, snapWorkloadReplicas)
		})
		It("should run every workload pod and mount its VirtioFS volume", func() {
			for _, podName := range snapWorkloadPodNames(snapVirtioFSStatefulSetName) {
				snaputils.WaitForWorkloadPodRunning(ctx, input.client, snapVirtioFSNamespace, podName)
				snaputils.VerifyVirtioFSMount(hostClusterRESTClient, input.restConfig, snapVirtioFSNamespace, podName, snapVirtioFSMountPath)
			}
		})
		It("should keep every workload writing and reading its own file on the mount", func() {
			for _, podName := range snapWorkloadPodNames(snapVirtioFSStatefulSetName) {
				snaputils.VerifyWorkloadHeartbeat(hostClusterRESTClient, input.restConfig, snapVirtioFSNamespace, podName, snapVirtioFSHeartbeatFile)
			}
		})
	})

	Context("NVMe volume on a hot-plugged PF (trusted)", Labels{Domain.RequiresNodes}, Ordered, func() {
		BeforeAll(func() {
			snapNVMeWorkloadScope.CleanupBefore()
		})

		AfterAll(func() {
			snapNVMeWorkloadScope.CleanupAfter()
		})

		It("should create the workload StorageClass and four-replica StatefulSet", func() {
			applySNAPWorkload(ctx, input, snapNVMeWorkloadScope, snapNVMeNamespace,
				conf.SNAPNVMeStorageClassPath, conf.SNAPNVMeWorkloadPath)
		})
		It("should bind a block DPUVolume for each workload pod", func() {
			snaputils.VerifyDPUVolumesBound(ctx, input.client, dpfOperatorSystemNamespace, snapWorkloadReplicas)
		})
		It("should attach every backend volume to the DPU", func() {
			snaputils.VerifySVVolumeAttachmentsAttached(ctx, dpuClusterClient[0], dpfOperatorSystemNamespace, snapWorkloadReplicas)
		})
		It("should have Ready NVMe hot-plugged PF attachments", func() {
			snaputils.VerifyNVMeAttachmentsReady(ctx, input.client, dpfOperatorSystemNamespace, snapWorkloadReplicas)
		})
		It("should run every workload pod and perform raw block I/O", func() {
			for _, podName := range snapWorkloadPodNames(snapNVMeStatefulSetName) {
				snaputils.WaitForWorkloadPodRunning(ctx, input.client, snapNVMeNamespace, podName)
				snaputils.VerifyNVMeRawBlockIO(hostClusterRESTClient, input.restConfig, snapNVMeNamespace, podName, snapNVMeDevicePath)
			}
		})
	})
})
