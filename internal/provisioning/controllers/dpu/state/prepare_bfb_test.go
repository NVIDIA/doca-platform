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

package state_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	operatorcontroller "github.com/nvidia/doca-platform/internal/operator/controllers"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// mockJoinCommandGenerator is a mock implementation of NodeJoinCommandGenerator
type mockJoinCommandGenerator struct {
	returnError bool
	errorMsg    string
}

func (m *mockJoinCommandGenerator) GenerateJoinCommand(ctx context.Context, dc *provisioningv1.DPUCluster) (string, error) {
	if m.returnError {
		return "", fmt.Errorf("%s", m.errorMsg)
	}
	return "kubeadm join 10.0.0.1:6443 --token abc123.xyz789", nil
}

var _ = Describe("DPU: PrepareBFB", func() {
	var (
		defaultDPUName     = "dpu-prepare-bfb-test"
		defaultNodeName    = "node-prepare-bfb-test"
		defaultClusterName = "cluster-prepare-bfb-test"
		defaultFlavorName  = "flavor-prepare-bfb-test"
		defaultDeviceName  = "device-prepare-bfb-test"
	)

	// Helper to create DPFOperatorConfig
	createDPFOperatorConfig := func() {
		// Create the namespace first if it doesn't exist
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace}}
		_ = k8sClient.Create(ctx, ns)

		dpfConfig := &operatorv1.DPFOperatorConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      operatorcontroller.DefaultDPFOperatorConfigSingletonName,
				Namespace: operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace,
			},
			Spec: operatorv1.DPFOperatorConfigSpec{
				DeploymentMode: operatorv1.DeploymentModeHostTrusted,
				ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{
					BFBPersistentVolumeClaimName: ptr.To("bfb-pvc"),
				},
			},
		}
		Expect(client.IgnoreAlreadyExists(k8sClient.Create(ctx, dpfConfig))).To(Succeed())
	}

	It("should return error with DPUFlavorNotFound when DPUFlavor does not exist", func() {
		dpu := dpuObj(defaultDPUName)
		dpu.Spec.DPUFlavor = "non-existent-flavor"
		dpu.Status.Phase = provisioningv1.DPUPrepareBFB

		status, err := state.PrepareBFB(ctx, dpu,
			&dutil.ControllerContext{
				Client: k8sClient,
			},
		)
		Expect(err).To(HaveOccurred())
		Expect(status.Conditions).Should(ContainElements(
			And(
				HaveField("Type", provisioningv1.DPUCondBFBPrepared.String()),
				HaveField("Status", metav1.ConditionFalse),
				HaveField("Reason", "DPUFlavorNotFound"),
			),
		))
	})

	It("should return error with DPUClusterNotFound when DPUCluster does not exist", func() {
		flavor := dpuFlavorObj(defaultFlavorName)
		createObject(flavor)

		dpu := dpuObj(defaultDPUName)
		dpu.Spec.DPUFlavor = defaultFlavorName
		dpu.Spec.Cluster = provisioningv1.K8sCluster{
			Name:      "non-existent-cluster",
			Namespace: testNS.Name,
		}
		dpu.Status.Phase = provisioningv1.DPUPrepareBFB

		status, err := state.PrepareBFB(ctx, dpu,
			&dutil.ControllerContext{
				Client: k8sClient,
			},
		)
		Expect(err).To(HaveOccurred())
		Expect(status.Conditions).Should(ContainElements(
			And(
				HaveField("Type", provisioningv1.DPUCondBFBPrepared.String()),
				HaveField("Status", metav1.ConditionFalse),
				HaveField("Reason", "DPUClusterNotFound"),
			),
		))
	})

	It("should return error with DPUNodeNotFound when DPUNode does not exist", func() {
		flavor := dpuFlavorObj(defaultFlavorName)
		createObject(flavor)

		cluster := dpuClusterObj(defaultClusterName, string(provisioningv1.StaticCluster))
		createObject(cluster)

		dpu := dpuObj(defaultDPUName)
		dpu.Spec.DPUFlavor = defaultFlavorName
		dpu.Spec.Cluster = provisioningv1.K8sCluster{
			Name:      defaultClusterName,
			Namespace: testNS.Name,
		}
		dpu.Spec.DPUNodeName = "non-existent-dpunode"
		dpu.Status.Phase = provisioningv1.DPUPrepareBFB

		status, err := state.PrepareBFB(ctx, dpu,
			&dutil.ControllerContext{
				Client: k8sClient,
			},
		)
		Expect(err).To(HaveOccurred())
		Expect(status.Conditions).Should(ContainElements(
			And(
				HaveField("Type", provisioningv1.DPUCondBFBPrepared.String()),
				HaveField("Status", metav1.ConditionFalse),
				HaveField("Reason", "DPUNodeNotFound"),
			),
		))
	})

	It("should return error with FailedToCreateDirectory when directory creation fails", func() {
		node := nodeObj(defaultNodeName)
		createObject(node)

		flavor := dpuFlavorObj(defaultFlavorName)
		createObject(flavor)

		cluster := dpuClusterObj(defaultClusterName, string(provisioningv1.StaticCluster))
		createObject(cluster)

		dpuNode := dpuNodeObj(defaultNodeName)
		createObject(dpuNode)
		patch := client.MergeFrom(dpuNode.DeepCopy())
		dpuNode.Status.KubeNodeRef = ptr.To(node.Name)
		Expect(k8sClient.Status().Patch(ctx, dpuNode, patch)).To(Succeed())

		dpuDevice := dpuDeviceObj(defaultDeviceName)
		createObject(dpuDevice)

		dpu := dpuObj(defaultDPUName)
		dpu.Spec.DPUFlavor = defaultFlavorName
		dpu.Spec.Cluster = provisioningv1.K8sCluster{
			Name:      defaultClusterName,
			Namespace: testNS.Name,
		}
		dpu.Spec.DPUNodeName = dpuNode.Name
		dpu.Spec.DPUDeviceName = defaultDeviceName
		dpu.Status.Phase = provisioningv1.DPUPrepareBFB

		// Create a file at the BFBBaseDir path to cause MkdirAll to fail
		tempFile, err := os.CreateTemp("", "bfb-test-file")
		Expect(err).NotTo(HaveOccurred())
		tempFilePath := tempFile.Name()
		_ = tempFile.Close()
		defer func() { _ = os.Remove(tempFilePath) }()

		originalBFBBaseDir := cutil.BFBBaseDir
		cutil.BFBBaseDir = filepath.Join(tempFilePath, "subdir")
		defer func() { cutil.BFBBaseDir = originalBFBBaseDir }()

		status, err := state.PrepareBFB(ctx, dpu,
			&dutil.ControllerContext{
				Client: k8sClient,
				JoinCommandGenerator: &mockJoinCommandGenerator{
					returnError: false,
				},
			},
		)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to create directory"))
		Expect(status.Conditions).Should(ContainElements(
			And(
				HaveField("Type", provisioningv1.DPUCondBFBPrepared.String()),
				HaveField("Status", metav1.ConditionFalse),
				HaveField("Reason", "FailedToCreateDirectory"),
			),
		))
	})

	It("should return error with FailedToGenerateJoinCommand when join command generation fails", func() {
		node := nodeObj(defaultNodeName)
		createObject(node)

		flavor := dpuFlavorObj(defaultFlavorName)
		createObject(flavor)

		cluster := dpuClusterObj(defaultClusterName, string(provisioningv1.StaticCluster))
		createObject(cluster)

		dpuNode := dpuNodeObj(defaultNodeName)
		createObject(dpuNode)
		patch := client.MergeFrom(dpuNode.DeepCopy())
		dpuNode.Status.KubeNodeRef = ptr.To(node.Name)
		Expect(k8sClient.Status().Patch(ctx, dpuNode, patch)).To(Succeed())

		dpu := dpuObj(defaultDPUName)
		dpu.Spec.DPUFlavor = defaultFlavorName
		dpu.Spec.Cluster = provisioningv1.K8sCluster{
			Name:      defaultClusterName,
			Namespace: testNS.Name,
		}
		dpu.Spec.DPUNodeName = dpuNode.Name
		dpu.Status.Phase = provisioningv1.DPUPrepareBFB

		// Create temp directory for BFBBaseDir so MkdirAll succeeds
		tempDir, err := os.MkdirTemp("", "bfb-test")
		Expect(err).NotTo(HaveOccurred())
		defer func() { _ = os.RemoveAll(tempDir) }()

		originalBFBBaseDir := cutil.BFBBaseDir
		cutil.BFBBaseDir = tempDir
		defer func() { cutil.BFBBaseDir = originalBFBBaseDir }()

		status, err := state.PrepareBFB(ctx, dpu,
			&dutil.ControllerContext{
				Client: k8sClient,
				JoinCommandGenerator: &mockJoinCommandGenerator{
					returnError: true,
					errorMsg:    "mock join command error",
				},
			},
		)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to generate join command"))
		Expect(status.Conditions).Should(ContainElements(
			And(
				HaveField("Type", provisioningv1.DPUCondBFBPrepared.String()),
				HaveField("Status", metav1.ConditionFalse),
				HaveField("Reason", "FailedToGenerateJoinCommand"),
			),
		))
	})

	It("should return error with DPUDeviceNotFound when DPUDevice does not exist", func() {
		node := nodeObj(defaultNodeName)
		createObject(node)

		flavor := dpuFlavorObj(defaultFlavorName)
		createObject(flavor)

		cluster := dpuClusterObj(defaultClusterName, string(provisioningv1.StaticCluster))
		createObject(cluster)

		dpuNode := dpuNodeObj(defaultNodeName)
		createObject(dpuNode)
		patch := client.MergeFrom(dpuNode.DeepCopy())
		dpuNode.Status.KubeNodeRef = ptr.To(node.Name)
		Expect(k8sClient.Status().Patch(ctx, dpuNode, patch)).To(Succeed())

		dpu := dpuObj(defaultDPUName)
		dpu.Spec.DPUFlavor = defaultFlavorName
		dpu.Spec.Cluster = provisioningv1.K8sCluster{
			Name:      defaultClusterName,
			Namespace: testNS.Name,
		}
		dpu.Spec.DPUNodeName = dpuNode.Name
		dpu.Spec.DPUDeviceName = "non-existent-device"
		dpu.Status.Phase = provisioningv1.DPUPrepareBFB

		// Create temp directory for BFBBaseDir so MkdirAll succeeds
		tempDir, err := os.MkdirTemp("", "bfb-test")
		Expect(err).NotTo(HaveOccurred())
		defer func() { _ = os.RemoveAll(tempDir) }()

		originalBFBBaseDir := cutil.BFBBaseDir
		cutil.BFBBaseDir = tempDir
		defer func() { cutil.BFBBaseDir = originalBFBBaseDir }()

		status, err := state.PrepareBFB(ctx, dpu,
			&dutil.ControllerContext{
				Client: k8sClient,
				JoinCommandGenerator: &mockJoinCommandGenerator{
					returnError: false,
				},
			},
		)
		Expect(err).To(HaveOccurred())
		Expect(status.Conditions).Should(ContainElements(
			And(
				HaveField("Type", provisioningv1.DPUCondBFBPrepared.String()),
				HaveField("Status", metav1.ConditionFalse),
				HaveField("Reason", "DPUDeviceNotFound"),
			),
		))
	})

	It("should return error with FailedToGenerateBFConfig when bf.cfg generation fails", func() {
		node := nodeObj(defaultNodeName)
		createObject(node)

		flavor := dpuFlavorObj(defaultFlavorName)
		createObject(flavor)

		cluster := dpuClusterObj(defaultClusterName, string(provisioningv1.StaticCluster))
		createObject(cluster)

		dpuNode := dpuNodeObj(defaultNodeName)
		createObject(dpuNode)
		patch := client.MergeFrom(dpuNode.DeepCopy())
		dpuNode.Status.KubeNodeRef = ptr.To(node.Name)
		Expect(k8sClient.Status().Patch(ctx, dpuNode, patch)).To(Succeed())

		dpuDevice := dpuDeviceObj(defaultDeviceName)
		createObject(dpuDevice)

		dpu := dpuObj(defaultDPUName)
		dpu.Spec.DPUFlavor = defaultFlavorName
		dpu.Spec.Cluster = provisioningv1.K8sCluster{
			Name:      defaultClusterName,
			Namespace: testNS.Name,
		}
		dpu.Spec.DPUNodeName = dpuNode.Name
		dpu.Spec.DPUDeviceName = defaultDeviceName
		dpu.Status.Phase = provisioningv1.DPUPrepareBFB

		// Create temp directory for BFBBaseDir so MkdirAll succeeds
		tempDir, err := os.MkdirTemp("", "bfb-test")
		Expect(err).NotTo(HaveOccurred())
		defer func() { _ = os.RemoveAll(tempDir) }()

		originalBFBBaseDir := cutil.BFBBaseDir
		cutil.BFBBaseDir = tempDir
		defer func() { cutil.BFBBaseDir = originalBFBBaseDir }()

		status, err := state.PrepareBFB(ctx, dpu,
			&dutil.ControllerContext{
				Client: k8sClient,
				JoinCommandGenerator: &mockJoinCommandGenerator{
					returnError: false,
				},
				Options: dutil.DPUOptions{
					// Use a non-existent template file to trigger GenerateBFConfig error
					BFCFGTemplateFile: "/non/existent/template/file.cfg",
				},
			},
		)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to generate bf.cfg"))
		Expect(status.Conditions).Should(ContainElements(
			And(
				HaveField("Type", provisioningv1.DPUCondBFBPrepared.String()),
				HaveField("Status", metav1.ConditionFalse),
				HaveField("Reason", "FailedToGenerateBFConfig"),
			),
		))
	})

	// TODO: Re-enable after refactoring ResolveParams to not read ServiceAccountCAPath directly.
	// Tracked in follow-up MR to extract bootstrap kubeconfig generation behind an interface.
	PIt("should return error with FailedToPushBFCFG when file write fails", func() {
		createDPFOperatorConfig()

		node := nodeObj(defaultNodeName)
		createObject(node)

		flavor := dpuFlavorObj(defaultFlavorName)
		createObject(flavor)

		cluster := dpuClusterObj(defaultClusterName, string(provisioningv1.StaticCluster))
		createObject(cluster)

		dpuNode := dpuNodeObj(defaultNodeName)
		createObject(dpuNode)
		patch := client.MergeFrom(dpuNode.DeepCopy())
		dpuNode.Status.KubeNodeRef = ptr.To(node.Name)
		Expect(k8sClient.Status().Patch(ctx, dpuNode, patch)).To(Succeed())

		dpuDevice := dpuDeviceObj(defaultDeviceName)
		createObject(dpuDevice)

		dpu := dpuObj(defaultDPUName)
		dpu.Spec.DPUFlavor = defaultFlavorName
		dpu.Spec.Cluster = provisioningv1.K8sCluster{
			Name:      defaultClusterName,
			Namespace: testNS.Name,
		}
		dpu.Spec.DPUNodeName = dpuNode.Name
		dpu.Spec.DPUDeviceName = defaultDeviceName
		dpu.Status.Phase = provisioningv1.DPUPrepareBFB

		tempDir, err := os.MkdirTemp("", "bfb-test")
		Expect(err).NotTo(HaveOccurred())
		defer func() { _ = os.RemoveAll(tempDir) }()

		// Create the bfcfg directory but make it read-only to cause write to fail
		bfcfgDir := filepath.Join(tempDir, "bfcfg")
		err = os.MkdirAll(bfcfgDir, 0555)
		Expect(err).NotTo(HaveOccurred())

		originalBFBBaseDir := cutil.BFBBaseDir
		cutil.BFBBaseDir = tempDir
		defer func() { cutil.BFBBaseDir = originalBFBBaseDir }()

		status, err := state.PrepareBFB(ctx, dpu,
			&dutil.ControllerContext{
				Client: k8sClient,
				JoinCommandGenerator: &mockJoinCommandGenerator{
					returnError: false,
				},
				Options: dutil.DPUOptions{
					BFCFGTemplateFile:   "",
					DPUInstallInterface: string(provisioningv1.InstallViaGNOI),
				},
			},
		)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to write bf.cfg"))
		Expect(status.Conditions).Should(ContainElements(
			And(
				HaveField("Type", provisioningv1.DPUCondBFBPrepared.String()),
				HaveField("Status", metav1.ConditionFalse),
				HaveField("Reason", "FailedToPushBFCFG"),
			),
		))
	})

	// TODO: Re-enable after refactoring ResolveParams to not read ServiceAccountCAPath directly.
	PIt("should successfully prepare BFB and transition to DPUOSInstalling", func() {
		createDPFOperatorConfig()

		node := nodeObj(defaultNodeName)
		createObject(node)

		flavor := dpuFlavorObj(defaultFlavorName)
		createObject(flavor)

		cluster := dpuClusterObj(defaultClusterName, string(provisioningv1.StaticCluster))
		createObject(cluster)

		dpuNode := dpuNodeObj(defaultNodeName)
		createObject(dpuNode)
		patch := client.MergeFrom(dpuNode.DeepCopy())
		dpuNode.Status.KubeNodeRef = ptr.To(node.Name)
		Expect(k8sClient.Status().Patch(ctx, dpuNode, patch)).To(Succeed())

		dpuDevice := dpuDeviceObj(defaultDeviceName)
		createObject(dpuDevice)

		dpu := dpuObj(defaultDPUName)
		dpu.Spec.DPUFlavor = defaultFlavorName
		dpu.Spec.Cluster = provisioningv1.K8sCluster{
			Name:      defaultClusterName,
			Namespace: testNS.Name,
		}
		dpu.Spec.DPUNodeName = dpuNode.Name
		dpu.Spec.DPUDeviceName = defaultDeviceName
		dpu.Status.Phase = provisioningv1.DPUPrepareBFB

		tempDir, err := os.MkdirTemp("", "bfb-test")
		Expect(err).NotTo(HaveOccurred())
		defer func() { _ = os.RemoveAll(tempDir) }()

		originalBFBBaseDir := cutil.BFBBaseDir
		cutil.BFBBaseDir = tempDir
		defer func() { cutil.BFBBaseDir = originalBFBBaseDir }()

		status, err := state.PrepareBFB(ctx, dpu,
			&dutil.ControllerContext{
				Client: k8sClient,
				JoinCommandGenerator: &mockJoinCommandGenerator{
					returnError: false,
				},
				Options: dutil.DPUOptions{
					BFCFGTemplateFile:   "",
					DPUInstallInterface: string(provisioningv1.InstallViaGNOI),
				},
			},
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(status.Phase).To(Equal(provisioningv1.DPUOSInstalling))
		Expect(status.BFCFGFile).NotTo(BeEmpty())
		Expect(status.Conditions).Should(ContainElements(
			And(
				HaveField("Type", provisioningv1.DPUCondBFBPrepared.String()),
				HaveField("Status", metav1.ConditionTrue),
			),
		))

		_, err = os.Stat(status.BFCFGFile)
		Expect(err).NotTo(HaveOccurred())
	})

	Describe("BlueField 4 ISO path", func() {
		// TODO: Re-enable after refactoring ResolveParams to not read ServiceAccountCAPath directly.
		PIt("should create ISO image and set BFCFGFile to iso path when DPUDevice is BlueField 4", func() {
			createDPFOperatorConfig()

			node := nodeObj(defaultNodeName)
			createObject(node)

			flavor := dpuFlavorObj(defaultFlavorName)
			createObject(flavor)

			cluster := dpuClusterObj(defaultClusterName, string(provisioningv1.StaticCluster))
			createObject(cluster)

			dpuNode := dpuNodeObj(defaultNodeName)
			createObject(dpuNode)
			patch := client.MergeFrom(dpuNode.DeepCopy())
			dpuNode.Status.KubeNodeRef = ptr.To(node.Name)
			Expect(k8sClient.Status().Patch(ctx, dpuNode, patch)).To(Succeed())

			dpuDevice := dpuDeviceObj(defaultDeviceName)
			createObject(dpuDevice)
			deviceStatusPatch := client.MergeFrom(dpuDevice.DeepCopy())
			dpuDevice.Status.DPUType = provisioningv1.DPUTypeBlueField4
			Expect(k8sClient.Status().Patch(ctx, dpuDevice, deviceStatusPatch)).To(Succeed())

			dpu := dpuObj(defaultDPUName)
			dpu.Spec.DPUFlavor = defaultFlavorName
			dpu.Spec.Cluster = provisioningv1.K8sCluster{
				Name:      defaultClusterName,
				Namespace: testNS.Name,
			}
			dpu.Spec.DPUNodeName = dpuNode.Name
			dpu.Spec.DPUDeviceName = defaultDeviceName
			dpu.Status.Phase = provisioningv1.DPUPrepareBFB

			tempDir, err := os.MkdirTemp("", "bfb-bf4-test")
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = os.RemoveAll(tempDir) }()

			originalBFBBaseDir := cutil.BFBBaseDir
			cutil.BFBBaseDir = tempDir
			defer func() { cutil.BFBBaseDir = originalBFBBaseDir }()

			status, err := state.PrepareBFB(ctx, dpu,
				&dutil.ControllerContext{
					Client: k8sClient,
					JoinCommandGenerator: &mockJoinCommandGenerator{
						returnError: false,
					},
					Options: dutil.DPUOptions{
						BFCFGTemplateFile:   "",
						DPUInstallInterface: string(provisioningv1.InstallViaGNOI),
					},
				},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUOSInstalling))
			Expect(status.BFCFGFile).To(HaveSuffix("seed.iso"))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondBFBPrepared.String()),
					HaveField("Status", metav1.ConditionTrue),
				),
			))

			_, err = os.Stat(status.BFCFGFile)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
