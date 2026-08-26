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

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	operatorcontroller "github.com/nvidia/doca-platform/internal/operator/controllers"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/allocator"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

var _ = Describe("Phase Initializing", func() {
	var (
		defaultDPUName        = "dpu-initializing-test"
		defaultDPUNodeName    = "dpu-node-initializing-test"
		defaultDPUDeviceName  = "dpu-device-initializing-test"
		defaultDPUClusterName = "dpu-cluster-initializing-test"
		strTrue               = "true"
	)

	// deleteAllDPFOperatorConfigs removes every DPFOperatorConfig so a test can establish
	// an exact precondition (zero or one). GetDPFOperatorConfig requires exactly one config
	// cluster-wide, and other specs leave a persistent singleton, so the identity-mode stamp
	// tests reset this shared state explicitly to stay deterministic regardless of spec order.
	deleteAllDPFOperatorConfigs := func() {
		list := &operatorv1.DPFOperatorConfigList{}
		Expect(k8sClient.List(ctx, list)).To(Succeed())
		for i := range list.Items {
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &list.Items[i]))).To(Succeed())
		}
	}

	// setDPUDeviceType stamps status.dpuType so Initializing can advance to Pending.
	setDPUDeviceType := func(dpuDevice *provisioningv1.DPUDevice, dpuType provisioningv1.DPUType) {
		patch := client.MergeFrom(dpuDevice.DeepCopy())
		dpuDevice.Status.DPUType = dpuType
		Expect(k8sClient.Status().Patch(ctx, dpuDevice, patch)).To(Succeed())
	}

	// ensureSingleDPFOperatorConfig deletes any existing configs and creates exactly one in
	// the singleton namespace, optionally enabling SPIFFE. The DPU identity-mode stamp reads
	// this config to decide between bootstrap-token and spiffe.
	ensureSingleDPFOperatorConfig := func(spiffe bool) {
		deleteAllDPFOperatorConfigs()
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace}}
		Expect(client.IgnoreAlreadyExists(k8sClient.Create(ctx, ns))).To(Succeed())
		// SPIFFE is only valid with deploymentMode=zero-trust (enforced by a CRD CEL rule).
		deploymentMode := operatorv1.DeploymentModeHostTrusted
		if spiffe {
			deploymentMode = operatorv1.DeploymentModeZeroTrust
		}
		cfg := &operatorv1.DPFOperatorConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      operatorcontroller.DefaultDPFOperatorConfigSingletonName,
				Namespace: operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace,
			},
		}
		// CreateOrUpdate reconciles the singleton's spec in place so identity-mode tests are
		// deterministic even if a prior config lingers (delete is asynchronous in envtest).
		// Swallowing AlreadyExists on a bare Create could otherwise keep stale SPIFFE settings.
		_, err := controllerutil.CreateOrUpdate(ctx, k8sClient, cfg, func() error {
			cfg.Spec = operatorv1.DPFOperatorConfigSpec{
				DeploymentMode: deploymentMode,
				ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{
					BFBPersistentVolumeClaimName: ptr.To("bfb-pvc"),
				},
			}
			if spiffe {
				// zero-trust requires the Redfish install interface (CRD CEL rule).
				cfg.Spec.ProvisioningController.InstallInterface = &operatorv1.ProvisioningInstallInterface{
					InstallViaRedfish: &operatorv1.InstallViaRedfish{},
				}
				cfg.Spec.Security = &operatorv1.SecurityConfiguration{
					SPIFFE: &operatorv1.SPIFFEConfiguration{
						SPIREServerAddress:                "spire-server.spire-system.svc:8081",
						SPIRETrustDomain:                  "cs.internal",
						DPUAgentSPIFFEIDTemplate:          "spiffe://{{ .TrustDomain }}/tenant/operator/service/dsx/dpu/{{ .SerialNumber }}/process/dpu-agent",
						DPUAgentExchangedSPIFFEIDTemplate: "spiffe://operator.example.test/dpu/{{ .SerialNumber }}/process/dpu-agent",
						KubeAPIAudience:                   "spire-dpu",
						SPIREOIDCURL:                      "https://spire-oidc.spire-system.svc",
						SPIREControllerManagerClassName:   "spire-mgmt-spire",
						TrustBundle: operatorv1.SPIFFETrustBundleConfigMapReference{
							Name:      "spire-bundle",
							Namespace: "spire-system",
						},
					},
				}
			}
			return nil
		})
		Expect(err).NotTo(HaveOccurred())
	}

	Context("successful cases", func() {
		BeforeEach(func() {
			ensureSingleDPFOperatorConfig(false)
		})
		It("should transition to the next phase", func() {
			By("prepare DPUDevice CR")
			dpuDevice := dpuDeviceObj(defaultDPUDeviceName)
			createObject(dpuDevice)
			setDPUDeviceType(dpuDevice, provisioningv1.DPUTypeBlueField3)

			By("prepare DPUNode CR")
			dpuNode := dpuNodeObj(defaultDPUNodeName)
			dpuNode.Finalizers = []string{provisioningv1.DPUNodeFinalizer}
			dpuNode.Labels[cutil.NodeFeatureDiscoveryLabelPrefix+cutil.DPUOOBBridgeConfiguredLabel] = strTrue
			dpuNode.Spec.DPUs = []provisioningv1.DPURef{
				{
					Name: dpuDevice.Name,
				},
			}
			createObject(dpuNode)
			patch := client.MergeFrom(dpuNode.DeepCopy())
			dpuNode.Status = provisioningv1.DPUNodeStatus{
				DPUInstallInterface: ptr.To(string(provisioningv1.InstallViaHostAgent)),
				Conditions: []metav1.Condition{
					{
						Type:               string(provisioningv1.DPUNodeConditionBridgeConfigured),
						Status:             metav1.ConditionTrue,
						Reason:             "BridgeConfigured",
						Message:            "Bridge configured",
						LastTransitionTime: metav1.Now(),
					},
				},
			}
			Expect(k8sClient.Status().Patch(ctx, dpuNode, patch)).To(Succeed())

			By("prepare DPUCluster CR")
			dpuCluster := dpuClusterObj(defaultDPUClusterName, "static")
			createObject(dpuCluster)

			By("prepare DPU CR")
			dpu := dpuObj(defaultDPUName)
			// redfish does not need PCI address. Here, it is assigned to simplify the test
			dpu.Spec.PCIAddress = ptr.To("0000-00-00")
			dpu.Spec.DPUNodeName = dpuNode.Name
			dpu.Spec.DPUDeviceName = dpuDevice.Name
			dpu.Spec.Cluster.Namespace = dpuCluster.Namespace
			dpu.Spec.Cluster.Name = dpuCluster.Name
			dpu.Status.Phase = provisioningv1.DPUInitializing
			dpu.Status.DPUInstallInterface = ptr.To(string(provisioningv1.InstallViaGNOI))
			reportedAt := metav1.Now()
			dpu.Status.AgentStatus = &provisioningv1.AgentStatus{
				PreInstall: &provisioningv1.AgentPreInstallStatus{
					AgentReported: &reportedAt,
				},
			}

			run := func(installInterface provisioningv1.DPUInstallInterfaceType) {
				status, err := state.Initializing(ctx, dpu,
					&dutil.ControllerContext{
						Client: k8sClient,
						Options: dutil.DPUOptions{
							DPUInstallInterface: string(installInterface),
						},
					},
				)
				Expect(err).To(Succeed())
				Expect(status.Phase).To(Equal(provisioningv1.DPUPending))
				Expect(status.Conditions).Should(ContainElements(
					And(
						HaveField("Type", provisioningv1.DPUCondInitialized.String()),
						HaveField("Status", metav1.ConditionTrue),
						HaveField("Reason", provisioningv1.DPUCondInitialized.String()),
					),
				))
				// DPUDevice has no SecureBoot status, so DPU should not have it either
				Expect(status.SecureBoot).To(BeNil())
			}
			runForEachInterface(run)
		})

		It("should sync SecureBoot status from DPUDevice to DPU", func() {
			By("prepare DPUDevice CR with SecureBoot status")
			dpuDevice := dpuDeviceObj(defaultDPUDeviceName)
			createObject(dpuDevice)

			patchDevice := client.MergeFrom(dpuDevice.DeepCopy())
			dpuDevice.Status.DPUType = provisioningv1.DPUTypeBlueField3
			dpuDevice.Status.SecureBoot = &provisioningv1.SecureBootStatus{Enabled: ptr.To(true)}
			Expect(k8sClient.Status().Patch(ctx, dpuDevice, patchDevice)).To(Succeed())

			By("prepare DPUNode CR")
			dpuNode := dpuNodeObj(defaultDPUNodeName)
			dpuNode.Finalizers = []string{provisioningv1.DPUNodeFinalizer}
			dpuNode.Labels[cutil.NodeFeatureDiscoveryLabelPrefix+cutil.DPUOOBBridgeConfiguredLabel] = strTrue
			dpuNode.Spec.DPUs = []provisioningv1.DPURef{{Name: dpuDevice.Name}}
			createObject(dpuNode)
			patchNode := client.MergeFrom(dpuNode.DeepCopy())
			dpuNode.Status = provisioningv1.DPUNodeStatus{
				DPUInstallInterface: ptr.To(string(provisioningv1.InstallViaHostAgent)),
				Conditions: []metav1.Condition{{
					Type:               string(provisioningv1.DPUNodeConditionBridgeConfigured),
					Status:             metav1.ConditionTrue,
					Reason:             "BridgeConfigured",
					Message:            "Bridge configured",
					LastTransitionTime: metav1.Now(),
				}},
			}
			Expect(k8sClient.Status().Patch(ctx, dpuNode, patchNode)).To(Succeed())

			By("prepare DPUCluster CR")
			dpuCluster := dpuClusterObj(defaultDPUClusterName, "static")
			createObject(dpuCluster)

			By("prepare DPU CR")
			dpu := dpuObj(defaultDPUName)
			dpu.Spec.PCIAddress = ptr.To("0000-00-00")
			dpu.Spec.DPUNodeName = dpuNode.Name
			dpu.Spec.DPUDeviceName = dpuDevice.Name
			dpu.Spec.Cluster.Namespace = dpuCluster.Namespace
			dpu.Spec.Cluster.Name = dpuCluster.Name
			dpu.Status.Phase = provisioningv1.DPUInitializing

			status, err := state.Initializing(ctx, dpu,
				&dutil.ControllerContext{
					Client: k8sClient,
					Options: dutil.DPUOptions{
						DPUInstallInterface: string(provisioningv1.InstallViaHostAgent),
					},
				})
			Expect(err).To(Succeed())
			Expect(status.SecureBoot).NotTo(BeNil())
			Expect(*status.SecureBoot.Enabled).To(BeTrue())
		})

		It("should propagate hostless status from DPUDevice label", func() {
			dpuDevice := dpuDeviceObj(defaultDPUDeviceName)
			dpuDevice.Labels[cutil.DPUDeviceHostlessLabel] = strTrue
			dpuDevice.Spec.BMCIP = ptr.To("192.0.2.10")
			createObject(dpuDevice)
			setDPUDeviceType(dpuDevice, provisioningv1.DPUTypeBlueField3)

			dpuNode := dpuNodeObj(defaultDPUNodeName)
			dpuNode.Spec.DPUs = []provisioningv1.DPURef{{Name: dpuDevice.Name}}
			createObject(dpuNode)
			patchNode := client.MergeFrom(dpuNode.DeepCopy())
			dpuNode.Status.DPUInstallInterface = ptr.To(string(provisioningv1.InstallViaRedFish))
			Expect(k8sClient.Status().Patch(ctx, dpuNode, patchNode)).To(Succeed())

			dpuCluster := dpuClusterObj(defaultDPUClusterName, "static")
			createObject(dpuCluster)

			dpu := dpuObj(defaultDPUName)
			dpu.Spec.DPUNodeName = dpuNode.Name
			dpu.Spec.DPUDeviceName = dpuDevice.Name
			dpu.Spec.Cluster.Namespace = dpuCluster.Namespace
			dpu.Spec.Cluster.Name = dpuCluster.Name
			dpu.Status.Phase = provisioningv1.DPUInitializing
			dpu.Status.DPUInstallInterface = ptr.To(string(provisioningv1.InstallViaRedFish))
			reportedAt := metav1.Now()
			dpu.Status.AgentStatus = &provisioningv1.AgentStatus{
				PreInstall: &provisioningv1.AgentPreInstallStatus{
					AgentReported: &reportedAt,
				},
			}

			status, err := state.Initializing(ctx, dpu, &dutil.ControllerContext{
				Client: k8sClient,
				Options: dutil.DPUOptions{
					DPUInstallInterface: string(provisioningv1.InstallViaRedFish),
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Hostless).To(BeTrue())
			Expect(status.Phase).To(Equal(provisioningv1.DPUPending))
		})

		It("should stay in Initializing until a Redfish DPUDevice reports a known DPU type", func() {
			dpuDevice := dpuDeviceObj(defaultDPUDeviceName)
			dpuDevice.Spec.BMCIP = ptr.To("192.0.2.10")
			createObject(dpuDevice)
			// Intentionally leave status.dpuType unset (Unknown after copy).

			dpuNode := dpuNodeObj(defaultDPUNodeName)
			dpuNode.Spec.DPUs = []provisioningv1.DPURef{{Name: dpuDevice.Name}}
			createObject(dpuNode)
			patchNode := client.MergeFrom(dpuNode.DeepCopy())
			dpuNode.Status.DPUInstallInterface = ptr.To(string(provisioningv1.InstallViaRedFish))
			Expect(k8sClient.Status().Patch(ctx, dpuNode, patchNode)).To(Succeed())

			dpuCluster := dpuClusterObj(defaultDPUClusterName, "static")
			createObject(dpuCluster)

			dpu := dpuObj(defaultDPUName)
			dpu.Spec.DPUNodeName = dpuNode.Name
			dpu.Spec.DPUDeviceName = dpuDevice.Name
			dpu.Spec.Cluster.Namespace = dpuCluster.Namespace
			dpu.Spec.Cluster.Name = dpuCluster.Name
			dpu.Status.Phase = provisioningv1.DPUInitializing
			dpu.Status.DPUInstallInterface = ptr.To(string(provisioningv1.InstallViaRedFish))
			reportedAt := metav1.Now()
			dpu.Status.AgentStatus = &provisioningv1.AgentStatus{
				PreInstall: &provisioningv1.AgentPreInstallStatus{
					AgentReported: &reportedAt,
				},
			}
			ctrlCtx := &dutil.ControllerContext{
				Client: k8sClient,
				Options: dutil.DPUOptions{
					DPUInstallInterface: string(provisioningv1.InstallViaRedFish),
				},
			}

			status, err := state.Initializing(ctx, dpu, ctrlCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUInitializing))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondInitialized.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "DPUTypeUnknown"),
				),
			))

			setDPUDeviceType(dpuDevice, provisioningv1.DPUTypeBlueField4)
			status, err = state.Initializing(ctx, dpu, ctrlCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUPending))
			Expect(status.DPUType).To(Equal(provisioningv1.DPUTypeBlueField4))
		})

		It("should leave Initializing on HostAgent when DPUDevice dpuType is still Unknown", func() {
			dpuDevice := dpuDeviceObj(defaultDPUDeviceName)
			createObject(dpuDevice)
			// Host-trusted never stamps dpuType; CRD default remains Unknown.

			dpuNode := dpuNodeObj(defaultDPUNodeName)
			dpuNode.Finalizers = []string{provisioningv1.DPUNodeFinalizer}
			dpuNode.Labels[cutil.NodeFeatureDiscoveryLabelPrefix+cutil.DPUOOBBridgeConfiguredLabel] = strTrue
			dpuNode.Spec.DPUs = []provisioningv1.DPURef{{Name: dpuDevice.Name}}
			createObject(dpuNode)
			patchNode := client.MergeFrom(dpuNode.DeepCopy())
			dpuNode.Status = provisioningv1.DPUNodeStatus{
				DPUInstallInterface: ptr.To(string(provisioningv1.InstallViaHostAgent)),
				Conditions: []metav1.Condition{{
					Type:               string(provisioningv1.DPUNodeConditionBridgeConfigured),
					Status:             metav1.ConditionTrue,
					Reason:             "BridgeConfigured",
					Message:            "Bridge configured",
					LastTransitionTime: metav1.Now(),
				}},
			}
			Expect(k8sClient.Status().Patch(ctx, dpuNode, patchNode)).To(Succeed())

			dpuCluster := dpuClusterObj(defaultDPUClusterName, "static")
			createObject(dpuCluster)

			dpu := dpuObj(defaultDPUName)
			dpu.Spec.PCIAddress = ptr.To("0000-00-00")
			dpu.Spec.DPUNodeName = dpuNode.Name
			dpu.Spec.DPUDeviceName = dpuDevice.Name
			dpu.Spec.Cluster.Namespace = dpuCluster.Namespace
			dpu.Spec.Cluster.Name = dpuCluster.Name
			dpu.Status.Phase = provisioningv1.DPUInitializing
			dpu.Status.DPUInstallInterface = ptr.To(string(provisioningv1.InstallViaHostAgent))
			reportedAt := metav1.Now()
			dpu.Status.AgentStatus = &provisioningv1.AgentStatus{
				PreInstall: &provisioningv1.AgentPreInstallStatus{
					AgentReported: &reportedAt,
				},
			}
			ctrlCtx := &dutil.ControllerContext{
				Client: k8sClient,
				Options: dutil.DPUOptions{
					DPUInstallInterface: string(provisioningv1.InstallViaHostAgent),
				},
			}

			status, err := state.Initializing(ctx, dpu, ctrlCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUPending))
		})

	})

	Context("identity mode stamp", func() {
		// newReadyDPU builds the DPUDevice/DPUNode/DPUCluster/DPU set required to reach the
		// identity-mode stamp (the last step before transitioning to Pending) and returns the
		// DPU plus the ControllerContext to run Initializing with.
		newReadyDPU := func() (*provisioningv1.DPU, *dutil.ControllerContext) {
			dpuDevice := dpuDeviceObj(defaultDPUDeviceName)
			createObject(dpuDevice)
			setDPUDeviceType(dpuDevice, provisioningv1.DPUTypeBlueField3)

			dpuNode := dpuNodeObj(defaultDPUNodeName)
			dpuNode.Finalizers = []string{provisioningv1.DPUNodeFinalizer}
			dpuNode.Labels[cutil.NodeFeatureDiscoveryLabelPrefix+cutil.DPUOOBBridgeConfiguredLabel] = strTrue
			dpuNode.Spec.DPUs = []provisioningv1.DPURef{{Name: dpuDevice.Name}}
			createObject(dpuNode)
			patchNode := client.MergeFrom(dpuNode.DeepCopy())
			dpuNode.Status = provisioningv1.DPUNodeStatus{
				DPUInstallInterface: ptr.To(string(provisioningv1.InstallViaHostAgent)),
				Conditions: []metav1.Condition{{
					Type:               string(provisioningv1.DPUNodeConditionBridgeConfigured),
					Status:             metav1.ConditionTrue,
					Reason:             "BridgeConfigured",
					Message:            "Bridge configured",
					LastTransitionTime: metav1.Now(),
				}},
			}
			Expect(k8sClient.Status().Patch(ctx, dpuNode, patchNode)).To(Succeed())

			dpuCluster := dpuClusterObj(defaultDPUClusterName, "static")
			createObject(dpuCluster)

			dpu := dpuObj(defaultDPUName)
			dpu.Spec.PCIAddress = ptr.To("0000-00-00")
			dpu.Spec.DPUNodeName = dpuNode.Name
			dpu.Spec.DPUDeviceName = dpuDevice.Name
			dpu.Spec.Cluster.Namespace = dpuCluster.Namespace
			dpu.Spec.Cluster.Name = dpuCluster.Name
			dpu.Status.Phase = provisioningv1.DPUInitializing
			dpu.Status.DPUInstallInterface = ptr.To(string(provisioningv1.InstallViaHostAgent))
			reportedAt := metav1.Now()
			dpu.Status.AgentStatus = &provisioningv1.AgentStatus{
				PreInstall: &provisioningv1.AgentPreInstallStatus{
					AgentReported: &reportedAt,
				},
			}

			ctrlCtx := &dutil.ControllerContext{
				Client:  k8sClient,
				Options: dutil.DPUOptions{DPUInstallInterface: string(provisioningv1.InstallViaHostAgent)},
			}
			return dpu, ctrlCtx
		}

		It("stamps bootstrap-token when SPIFFE is disabled", func() {
			ensureSingleDPFOperatorConfig(false)
			dpu, ctrlCtx := newReadyDPU()

			status, err := state.Initializing(ctx, dpu, ctrlCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUPending))
			Expect(status.IdentityMode).NotTo(BeNil())
			Expect(*status.IdentityMode).To(Equal(provisioningv1.IdentityModeBootstrapToken))
		})

		It("stamps spiffe when SPIFFE is enabled", func() {
			ensureSingleDPFOperatorConfig(true)
			dpu, ctrlCtx := newReadyDPU()

			status, err := state.Initializing(ctx, dpu, ctrlCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUPending))
			Expect(status.IdentityMode).NotTo(BeNil())
			Expect(*status.IdentityMode).To(Equal(provisioningv1.IdentityModeSpiffe))
		})

		It("requeues without stamping or advancing when no DPFOperatorConfig exists", func() {
			deleteAllDPFOperatorConfigs()
			dpu, ctrlCtx := newReadyDPU()

			status, err := state.Initializing(ctx, dpu, ctrlCtx)
			Expect(err).To(HaveOccurred())
			Expect(status.IdentityMode).To(BeNil())
			Expect(status.Phase).NotTo(Equal(provisioningv1.DPUPending))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondInitialized.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "IdentityModeStampDeferred"),
				),
			))
		})

		It("does not restamp a DPU that already has an identity mode", func() {
			// Config says SPIFFE, but the DPU is already stamped bootstrap-token; the stamp is
			// immutable once set, so the existing mode must be preserved.
			ensureSingleDPFOperatorConfig(true)
			dpu, ctrlCtx := newReadyDPU()
			dpu.Status.IdentityMode = ptr.To(provisioningv1.IdentityModeBootstrapToken)

			status, err := state.Initializing(ctx, dpu, ctrlCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUPending))
			Expect(status.IdentityMode).NotTo(BeNil())
			Expect(*status.IdentityMode).To(Equal(provisioningv1.IdentityModeBootstrapToken))
		})

		It("keeps bootstrap-token DPUs when SPIFFE is enabled for new DPUs", func() {
			ensureSingleDPFOperatorConfig(false)
			legacyDPU, ctrlCtx := newReadyDPU()
			legacyStatus, err := state.Initializing(ctx, legacyDPU, ctrlCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(*legacyStatus.IdentityMode).To(Equal(provisioningv1.IdentityModeBootstrapToken))

			ensureSingleDPFOperatorConfig(true)
			secondDevice := dpuDeviceObj("second-device")
			createObject(secondDevice)
			setDPUDeviceType(secondDevice, provisioningv1.DPUTypeBlueField3)

			node := &provisioningv1.DPUNode{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: defaultDPUNodeName, Namespace: testNS.Name}, node)).To(Succeed())
			node.Spec.DPUs = append(node.Spec.DPUs, provisioningv1.DPURef{Name: secondDevice.Name})
			Expect(k8sClient.Update(ctx, node)).To(Succeed())

			newDPU := dpuObj("second-dpu")
			newDPU.Spec.PCIAddress = ptr.To("0000-00-01")
			newDPU.Spec.DPUNodeName = node.Name
			newDPU.Spec.DPUDeviceName = secondDevice.Name
			newDPU.Spec.Cluster.Namespace = legacyDPU.Spec.Cluster.Namespace
			newDPU.Spec.Cluster.Name = legacyDPU.Spec.Cluster.Name
			newDPU.Status.Phase = provisioningv1.DPUInitializing
			newDPU.Status.DPUInstallInterface = ptr.To(string(provisioningv1.InstallViaHostAgent))

			newStatus, err := state.Initializing(ctx, newDPU, ctrlCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(*newStatus.IdentityMode).To(Equal(provisioningv1.IdentityModeSpiffe))

			legacyDPU.Status = legacyStatus
			legacyStatus, err = state.Initializing(ctx, legacyDPU, ctrlCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(*legacyStatus.IdentityMode).To(Equal(provisioningv1.IdentityModeBootstrapToken))
		})
	})

	Context("error handling", func() {
		It("should retry if DPUNode is not found", func() {
			dpu := dpuObj(defaultDPUName)
			dpu.Spec.DPUNodeName = "non-existent-dpu-node"
			dpu.Status.Phase = provisioningv1.DPUInitializing
			run := func(installInterface provisioningv1.DPUInstallInterfaceType) {
				status, err := state.Initializing(ctx, dpu,
					&dutil.ControllerContext{
						Client: k8sClient,
						Options: dutil.DPUOptions{
							DPUInstallInterface: string(installInterface),
						},
					})
				Expect(err).To(HaveOccurred())
				Expect(status.Phase).To(Equal(provisioningv1.DPUInitializing))
				Expect(status.Conditions).Should(ContainElements(
					And(
						HaveField("Type", provisioningv1.DPUCondInitialized.String()),
						HaveField("Status", metav1.ConditionFalse),
						HaveField("Reason", "DPUNodeNotFound"),
					),
				))
			}
			runForEachInterface(run)
		})
		It("should retry if DPUDevice is not found", func() {
			dpuNode := dpuNodeObj(defaultDPUNodeName)
			createObject(dpuNode)
			dpu := dpuObj(defaultDPUName)
			dpu.Spec.DPUNodeName = dpuNode.Name
			dpu.Spec.DPUDeviceName = "non-existent-dpu-device"
			dpu.Status.Phase = provisioningv1.DPUInitializing
			run := func(installInterface provisioningv1.DPUInstallInterfaceType) {
				status, err := state.Initializing(ctx, dpu,
					&dutil.ControllerContext{
						Client: k8sClient,
						Options: dutil.DPUOptions{
							DPUInstallInterface: string(installInterface),
						},
					})
				Expect(err).To(HaveOccurred())
				Expect(status.Phase).To(Equal(provisioningv1.DPUInitializing))
				Expect(status.Conditions).Should(ContainElements(
					And(
						HaveField("Type", provisioningv1.DPUCondInitialized.String()),
						HaveField("Status", metav1.ConditionFalse),
						HaveField("Reason", "DPUDeviceNotFound"),
					),
				))
			}
			runForEachInterface(run)
		})
		It("should requeue if PCI address is not specified - exclusive for gNOI", func() {
			By("prepare DPUDevice CR")
			dpuDevice := dpuDeviceObj(defaultDPUDeviceName)
			createObject(dpuDevice)

			By("prepare DPUNode CR")
			dpuNode := dpuNodeObj(defaultDPUNodeName)
			dpuNode.Finalizers = []string{provisioningv1.DPUNodeFinalizer}
			dpuNode.Labels[cutil.NodeFeatureDiscoveryLabelPrefix+cutil.DPUOOBBridgeConfiguredLabel] = strTrue
			dpuNode.Spec.DPUs = []provisioningv1.DPURef{
				{
					Name: dpuDevice.Name,
				},
			}
			createObject(dpuNode)
			patch := client.MergeFrom(dpuNode.DeepCopy())
			dpuNode.Status = provisioningv1.DPUNodeStatus{
				DPUInstallInterface: ptr.To(string(provisioningv1.InstallViaHostAgent)),
				Conditions: []metav1.Condition{
					{
						Type:               string(provisioningv1.DPUNodeConditionBridgeConfigured),
						Status:             metav1.ConditionTrue,
						Reason:             "BridgeConfigured",
						Message:            "Bridge configured",
						LastTransitionTime: metav1.Now(),
					},
				},
			}
			Expect(k8sClient.Status().Patch(ctx, dpuNode, patch)).To(Succeed())

			By("prepare DPU CR")
			dpu := dpuObj(defaultDPUName)
			dpu.Spec.DPUNodeName = dpuNode.Name
			dpu.Spec.DPUDeviceName = dpuDevice.Name
			dpu.Status.Phase = provisioningv1.DPUInitializing
			dpu.Status.DPUInstallInterface = ptr.To(string(provisioningv1.InstallViaGNOI))
			status, err := state.Initializing(ctx, dpu,
				&dutil.ControllerContext{
					Client: k8sClient,
					Options: dutil.DPUOptions{
						DPUInstallInterface: string(provisioningv1.InstallViaGNOI),
					},
				})
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUInitializing))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondInitialized.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "PCIAddressNotProvided"),
				),
			))
		})
		It("should retry if OOB bridge label is missing", func() {
			By("prepare DPUDevice CR")
			dpuDevice := dpuDeviceObj(defaultDPUDeviceName)
			createObject(dpuDevice)

			By("prepare DPUNode CR")
			dpuNode := dpuNodeObj(defaultDPUNodeName)
			dpuNode.Finalizers = []string{provisioningv1.DPUNodeFinalizer}
			delete(dpuNode.Labels, cutil.NodeFeatureDiscoveryLabelPrefix+cutil.DPUOOBBridgeConfiguredLabel)
			dpuNode.Spec.DPUs = []provisioningv1.DPURef{
				{
					Name: dpuDevice.Name,
				},
			}
			createObject(dpuNode)
			patch := client.MergeFrom(dpuNode.DeepCopy())
			dpuNode.Status.DPUInstallInterface = ptr.To(string(provisioningv1.InstallViaGNOI))
			Expect(k8sClient.Status().Patch(ctx, dpuNode, patch)).To(Succeed())

			By("prepare DPU CR")
			dpu := dpuObj(defaultDPUName)
			dpu.Spec.DPUNodeName = dpuNode.Name
			dpu.Spec.DPUDeviceName = dpuDevice.Name
			dpu.Status.Phase = provisioningv1.DPUInitializing
			dpu.Status.DPUInstallInterface = ptr.To(string(provisioningv1.InstallViaGNOI))

			run := func(installInterface provisioningv1.DPUInstallInterfaceType) {
				status, err := state.Initializing(ctx, dpu,
					&dutil.ControllerContext{
						Client: k8sClient,
						Options: dutil.DPUOptions{
							DPUInstallInterface: string(installInterface),
						},
					},
				)
				Expect(err).To(HaveOccurred())
				Expect(status.Phase).To(Equal(provisioningv1.DPUInitializing))
				Expect(status.Conditions).Should(ContainElements(
					And(
						HaveField("Type", provisioningv1.DPUCondInitialized.String()),
						HaveField("Status", metav1.ConditionFalse),
						HaveField("Reason", "DPUOOBBridgeNotConfigured"),
					),
				))
			}
			runForEachInterface(run)
		})
		It("should retry if fails to allocate DPUCluster", func() {
			By("prepare DPUDevice CR")
			dpuDevice := dpuDeviceObj(defaultDPUDeviceName)
			createObject(dpuDevice)

			By("prepare DPUNode CR")
			dpuNode := dpuNodeObj(defaultDPUNodeName)
			dpuNode.Finalizers = []string{provisioningv1.DPUNodeFinalizer}
			dpuNode.Labels[cutil.NodeFeatureDiscoveryLabelPrefix+cutil.DPUOOBBridgeConfiguredLabel] = strTrue
			dpuNode.Spec.DPUs = []provisioningv1.DPURef{
				{
					Name: dpuDevice.Name,
				},
			}
			createObject(dpuNode)
			patch := client.MergeFrom(dpuNode.DeepCopy())
			dpuNode.Status = provisioningv1.DPUNodeStatus{
				DPUInstallInterface: ptr.To(string(provisioningv1.InstallViaHostAgent)),
				Conditions: []metav1.Condition{
					{
						Type:               string(provisioningv1.DPUNodeConditionBridgeConfigured),
						Status:             metav1.ConditionTrue,
						Reason:             "BridgeConfigured",
						Message:            "Bridge configured",
						LastTransitionTime: metav1.Now(),
					},
				},
			}
			Expect(k8sClient.Status().Patch(ctx, dpuNode, patch)).To(Succeed())

			By("prepare DPU CR")
			dpu := dpuObj(defaultDPUName)
			// redfish does not need PCI address. Here, it is assigned to simplify the test
			dpu.Spec.PCIAddress = ptr.To("0000-00-00")
			dpu.Spec.DPUNodeName = dpuNode.Name
			dpu.Spec.DPUDeviceName = dpuDevice.Name
			dpu.Status.Phase = provisioningv1.DPUInitializing
			dpu.Status.DPUInstallInterface = ptr.To(string(provisioningv1.InstallViaGNOI))

			run := func(installInterface provisioningv1.DPUInstallInterfaceType) {
				status, err := state.Initializing(ctx, dpu,
					&dutil.ControllerContext{
						Client:           k8sClient,
						ClusterAllocator: &allocateFail{},
						Options: dutil.DPUOptions{
							DPUInstallInterface: string(installInterface),
						},
					},
				)
				Expect(err).To(HaveOccurred())
				Expect(status.Phase).To(Equal(provisioningv1.DPUInitializing))
				Expect(status.Conditions).Should(ContainElements(
					And(
						HaveField("Type", provisioningv1.DPUCondInitialized.String()),
						HaveField("Status", metav1.ConditionFalse),
						HaveField("Reason", "DPUClusterNotAllocated"),
					),
				))
			}
			runForEachInterface(run)
		})
		It("should retry if DPUCluster is not found", func() {
			By("prepare DPUDevice CR")
			dpuDevice := dpuDeviceObj(defaultDPUDeviceName)
			createObject(dpuDevice)

			By("prepare DPUNode CR")
			dpuNode := dpuNodeObj(defaultDPUNodeName)
			dpuNode.Finalizers = []string{provisioningv1.DPUNodeFinalizer}
			dpuNode.Labels[cutil.NodeFeatureDiscoveryLabelPrefix+cutil.DPUOOBBridgeConfiguredLabel] = strTrue
			dpuNode.Spec.DPUs = []provisioningv1.DPURef{
				{
					Name: dpuDevice.Name,
				},
			}
			createObject(dpuNode)
			patch := client.MergeFrom(dpuNode.DeepCopy())
			dpuNode.Status = provisioningv1.DPUNodeStatus{
				DPUInstallInterface: ptr.To(string(provisioningv1.InstallViaHostAgent)),
				Conditions: []metav1.Condition{
					{
						Type:               string(provisioningv1.DPUNodeConditionBridgeConfigured),
						Status:             metav1.ConditionTrue,
						Reason:             "BridgeConfigured",
						Message:            "Bridge configured",
						LastTransitionTime: metav1.Now(),
					},
				},
			}
			Expect(k8sClient.Status().Patch(ctx, dpuNode, patch)).To(Succeed())

			By("prepare DPU CR")
			dpu := dpuObj(defaultDPUName)
			// redfish does not need PCI address. Here, it is assigned to simplify the test
			dpu.Spec.PCIAddress = ptr.To("0000-00-00")
			dpu.Spec.DPUNodeName = dpuNode.Name
			dpu.Spec.DPUDeviceName = dpuDevice.Name
			dpu.Spec.Cluster.Namespace = "non-existent-namespace"
			dpu.Spec.Cluster.Name = "non-existent-dpu-cluster"
			dpu.Status.Phase = provisioningv1.DPUInitializing
			dpu.Status.DPUInstallInterface = ptr.To(string(provisioningv1.InstallViaGNOI))

			run := func(installInterface provisioningv1.DPUInstallInterfaceType) {
				status, err := state.Initializing(ctx, dpu,
					&dutil.ControllerContext{
						Client:           k8sClient,
						ClusterAllocator: &allocateFail{},
						Options: dutil.DPUOptions{
							DPUInstallInterface: string(installInterface),
						},
					},
				)
				Expect(err).To(HaveOccurred())
				Expect(status.Phase).To(Equal(provisioningv1.DPUInitializing))
				Expect(status.Conditions).Should(ContainElements(
					And(
						HaveField("Type", provisioningv1.DPUCondInitialized.String()),
						HaveField("Status", metav1.ConditionFalse),
						HaveField("Reason", "DPUClusterNotFound"),
					),
				))
			}
			runForEachInterface(run)

		})
		It("should fail with DPUClusterDeleting when DPUCluster is terminating", func() {
			By("prepare DPUDevice CR")
			dpuDevice := dpuDeviceObj(defaultDPUDeviceName)
			createObject(dpuDevice)

			By("prepare DPUNode CR")
			dpuNode := dpuNodeObj(defaultDPUNodeName)
			dpuNode.Finalizers = []string{provisioningv1.DPUNodeFinalizer}
			dpuNode.Labels[cutil.NodeFeatureDiscoveryLabelPrefix+cutil.DPUOOBBridgeConfiguredLabel] = strTrue
			dpuNode.Spec.DPUs = []provisioningv1.DPURef{
				{Name: dpuDevice.Name},
			}
			createObject(dpuNode)
			patch := client.MergeFrom(dpuNode.DeepCopy())
			dpuNode.Status = provisioningv1.DPUNodeStatus{
				DPUInstallInterface: ptr.To(string(provisioningv1.InstallViaHostAgent)),
				Conditions: []metav1.Condition{
					{
						Type:               string(provisioningv1.DPUNodeConditionBridgeConfigured),
						Status:             metav1.ConditionTrue,
						Reason:             "BridgeConfigured",
						Message:            "Bridge configured",
						LastTransitionTime: metav1.Now(),
					},
				},
			}
			Expect(k8sClient.Status().Patch(ctx, dpuNode, patch)).To(Succeed())

			By("prepare DPUCluster CR that is deleting")
			const blockDeletionFinalizer = "state.test.dpu.nvidia.com/block-deletion"
			dpuCluster := dpuClusterObj(defaultDPUClusterName, "static")
			dpuCluster.Finalizers = []string{blockDeletionFinalizer}
			Expect(k8sClient.Create(ctx, dpuCluster)).To(Succeed())

			DeferCleanup(func() {
				fetched := &provisioningv1.DPUCluster{}
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dpuCluster), fetched); err != nil {
					return
				}
				fetched.Finalizers = nil
				_ = k8sClient.Update(ctx, fetched)
				_ = client.IgnoreNotFound(k8sClient.Delete(ctx, fetched))
			})
			Expect(k8sClient.Delete(ctx, dpuCluster)).To(Succeed())
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(dpuCluster), dpuCluster)).To(Succeed())
			Expect(dpuCluster.DeletionTimestamp).NotTo(BeNil())

			By("prepare DPU CR referencing the deleting cluster")
			dpu := dpuObj(defaultDPUName)
			dpu.Spec.PCIAddress = ptr.To("0000-00-00")
			dpu.Spec.DPUNodeName = dpuNode.Name
			dpu.Spec.DPUDeviceName = dpuDevice.Name
			dpu.Spec.Cluster.Namespace = dpuCluster.Namespace
			dpu.Spec.Cluster.Name = dpuCluster.Name
			dpu.Status.Phase = provisioningv1.DPUInitializing
			dpu.Status.DPUInstallInterface = ptr.To(string(provisioningv1.InstallViaGNOI))

			run := func(installInterface provisioningv1.DPUInstallInterfaceType) {
				status, err := state.Initializing(ctx, dpu,
					&dutil.ControllerContext{
						Client:           k8sClient,
						ClusterAllocator: &allocateFail{},
						Options: dutil.DPUOptions{
							DPUInstallInterface: string(installInterface),
						},
					},
				)
				Expect(err).To(HaveOccurred())
				Expect(status.Phase).To(Equal(provisioningv1.DPUInitializing))
				Expect(status.Conditions).Should(ContainElements(
					And(
						HaveField("Type", provisioningv1.DPUCondInitialized.String()),
						HaveField("Status", metav1.ConditionFalse),
						HaveField("Reason", "DPUClusterDeleting"),
					),
				))
			}
			runForEachInterface(run)
		})
		It("should transition to the next phase if SkipDpuProvisioningLabel is set", func() {
			By("prepare DPUDevice CR")
			dpuDevice := dpuDeviceObj(defaultDPUDeviceName)
			dpuDevice.Labels[cutil.SkipDpuProvisioningLabel] = strTrue
			createObject(dpuDevice)

			By("prepare DPUNode CR")
			dpuNode := dpuNodeObj(defaultDPUNodeName)
			dpuNode.Finalizers = []string{provisioningv1.DPUNodeFinalizer}
			dpuNode.Labels[cutil.NodeFeatureDiscoveryLabelPrefix+cutil.DPUOOBBridgeConfiguredLabel] = strTrue
			dpuNode.Spec.DPUs = []provisioningv1.DPURef{
				{
					Name: dpuDevice.Name,
				},
			}
			createObject(dpuNode)
			patch := client.MergeFrom(dpuNode.DeepCopy())
			dpuNode.Status = provisioningv1.DPUNodeStatus{
				DPUInstallInterface: ptr.To(string(provisioningv1.InstallViaGNOI)),
				Conditions: []metav1.Condition{
					{
						Type:               string(provisioningv1.DPUNodeConditionBridgeConfigured),
						Status:             metav1.ConditionTrue,
						Reason:             "BridgeConfigured",
						Message:            "Bridge configured",
						LastTransitionTime: metav1.Now(),
					},
				},
			}
			Expect(k8sClient.Status().Patch(ctx, dpuNode, patch)).To(Succeed())

			By("prepare DPUCluster CR")
			dpuCluster := dpuClusterObj(defaultDPUClusterName, "static")
			createObject(dpuCluster)

			By("prepare DPU CR")
			dpu := dpuObj(defaultDPUName)
			dpu.Spec.PCIAddress = ptr.To("0000-00-00")
			dpu.Spec.DPUNodeName = dpuNode.Name
			dpu.Spec.DPUDeviceName = dpuDevice.Name
			dpu.Spec.Cluster.Namespace = dpuCluster.Namespace
			dpu.Spec.Cluster.Name = dpuCluster.Name
			dpu.Status.Phase = provisioningv1.DPUInitializing
			dpu.Status.DPUInstallInterface = ptr.To(string(provisioningv1.InstallViaRedFish))

			status, err := state.Initializing(ctx, dpu,
				&dutil.ControllerContext{
					Client: k8sClient,
				})
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUReady))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondInitialized.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", "Skipped"),
				),
			))
		})
	})
})

type allocateFail struct{}

func (a *allocateFail) Allocate(ctx context.Context, dpu *provisioningv1.DPU) (allocator.AllocateResult, error) {
	return types.NamespacedName{}, fmt.Errorf("allocate failed")
}

func (a *allocateFail) SaveAssignedDPU(*provisioningv1.DPU) {
}

func (a *allocateFail) SaveCluster(*provisioningv1.DPUCluster) {
}

func (a *allocateFail) ReleaseDPU(*provisioningv1.DPU) {
}

func (a *allocateFail) RemoveCluster(*provisioningv1.DPUCluster) {
}

func (a *allocateFail) GetDPUsCount(*provisioningv1.DPUCluster) int {
	return 0
}
