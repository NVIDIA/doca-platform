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
		snapWorkloadScope = cleanupTracker.RegisterScope(cleanup.NamedScopeManual("snap-workload"))

		if !input.hasDpuNodes() {
			Skip("Skip test as there are no DPU nodes")
		}

		// Workload first, as in the AfterAll: its namespace only finishes terminating once the CSI node
		// plugin, which the deployment scope owns, has unmounted the volume from the workload pod.
		snapWorkloadScope.CleanupBefore()
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
		// Cleanup workload first: detaching its volume needs the SNAP services the deployment scope owns.
		snapWorkloadScope.CleanupAfter()
		snapDeploymentScope.CleanupAfter()
		// The DPUSet selects its DPU by this label, so the label can only go once the DPUSet has. A kept
		// deployment scope leaves the DPUSet standing, and a DPUSet that selects nothing loses its DPU.
		if !snapDeploymentScope.ResourcesExist() {
			clearPinnedDPUDeviceLabel(ctx, input.client, dpfOperatorSystemNamespace)
		}
	})

	Context("SNAP services", Labels{Domain.RequiresNodes}, Ordered, func() {
		It("should run the DPU-side SNAP services (doca-snap, fs-storage-dpu-plugin, snap-node-driver)", func() {
			VerifyClusterPods(ctx, dpuClusterClient[0], []string{"doca-snap", "fs-storage-dpu-plugin", "snap-node-driver"})
		})
		It("should run the host-side SNAP services (snap-csi-plugin, snap-host-controller)", func() {
			VerifyClusterPods(ctx, testClient, []string{"snap-csi-plugin", "snap-host-controller"})
		})
		It("should run the csi-hostpath backend on the DPU cluster", func() {
			VerifyClusterPods(ctx, dpuClusterClient[0], []string{"csi-hostpathplugin"})
		})
	})

	Context("Storage control plane", Labels{Domain.RequiresNodes}, Ordered, func() {
		It("should have a Ready DPUStorageVendor", func() {
			snaputils.VerifyDPUStorageVendorReady(ctx, input.client, dpfOperatorSystemNamespace, snapStorageVendorName)
		})
		It("should have a Ready DPUStoragePolicy", func() {
			snaputils.VerifyDPUStoragePolicyReady(ctx, input.client, dpfOperatorSystemNamespace, snapStoragePolicyName)
		})
	})

	Context("VirtioFS volume on a hot-plugged PF (trusted)", Labels{Domain.RequiresNodes}, Ordered, func() {
		It("should create the workload StorageClass and StatefulSet", func() {
			applySNAPWorkload(ctx, input, *conf)
		})
		It("should bind a DPUVolume", func() {
			snaputils.VerifyDPUVolumeBound(ctx, input.client, dpfOperatorSystemNamespace)
		})
		It("should attach the backend volume to the DPU", func() {
			snaputils.VerifySVVolumeAttachmentAttached(ctx, dpuClusterClient[0], dpfOperatorSystemNamespace)
		})
		It("should have a Ready DPUVolumeAttachment with a VirtioFS filesystem tag", func() {
			snaputils.VerifyDPUVolumeAttachmentReady(ctx, input.client, dpfOperatorSystemNamespace)
		})
		It("should run the workload pod and mount the VirtioFS volume", func() {
			snaputils.WaitForWorkloadPodRunning(ctx, input.client, snapWorkloadNamespace, snapWorkloadPodName)
			snaputils.VerifyVirtioFSMount(hostClusterRESTClient, input.restConfig, snapWorkloadNamespace, snapWorkloadPodName, snapVolumeMountPath)
		})
		It("should keep the workload writing and reading its own file on the mount", func() {
			snaputils.VerifyWorkloadHeartbeat(hostClusterRESTClient, input.restConfig, snapWorkloadNamespace, snapWorkloadPodName, snapHeartbeatFile)
		})
	})
})
