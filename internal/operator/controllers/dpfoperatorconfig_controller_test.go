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

package controller

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	"github.com/nvidia/doca-platform/internal/operator/inventory"
	testutils "github.com/nvidia/doca-platform/test/utils"

	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestDPFOperatorConfigSettings(t *testing.T) {
	g := NewWithT(t)
	t.Run("ConfigSingletonNamespaceName restricts reconciliation to a config with the specified name /namespace", func(t *testing.T) {
		singletonReconciler := &DPFOperatorConfigReconciler{
			Client: testClient,
			Scheme: scheme.Scheme,
			Settings: &DPFOperatorConfigReconcilerSettings{
				ConfigSingletonNamespaceName: &types.NamespacedName{
					Namespace: "one-namespace",
					Name:      "one-name",
				},
			},
		}

		_, err := singletonReconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: types.NamespacedName{
				Namespace: "one-namespace",
				Name:      "one-name",
			},
		})
		g.Expect(err).ToNot(HaveOccurred())

		// Fail with a different name.
		_, err = singletonReconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: types.NamespacedName{
				Namespace: "different-namespace",
				Name:      "different-name",
			},
		})
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("only one object"))

	})

	t.Run("Unrestricted reconciler reconciles config of any name and namespace", func(t *testing.T) {
		unrestrictedReconciler := &DPFOperatorConfigReconciler{
			Client: testClient,
			Scheme: scheme.Scheme,
			Settings: &DPFOperatorConfigReconcilerSettings{
				ConfigSingletonNamespaceName: nil,
			},
		}

		_, err := unrestrictedReconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: types.NamespacedName{
				Namespace: "one-namespace",
				Name:      "one-name",
			},
		})
		g.Expect(err).ToNot(HaveOccurred())

		_, err = unrestrictedReconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: types.NamespacedName{
				Namespace: "different-namespace",
				Name:      "different-name",
			},
		})
		g.Expect(err).NotTo(HaveOccurred())
	})
}

func TestDPFOperatorConfigReconciler_Conditions(t *testing.T) {
	g := NewWithT(t)

	testNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "testns-"}}
	// Create the namespace for the test.
	g.Expect(testClient.Create(ctx, testNS)).To(Succeed())

	// This DPFOperatorConfig as various problems which will be fixed during the flow of the test code.
	config := &operatorv1.DPFOperatorConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "config",
			Namespace: testNS.Name,
		},
		Spec: operatorv1.DPFOperatorConfigSpec{

			// This secret name is wrong - this prevents ImagePullSecretsReconciled from becoming true.
			ImagePullSecrets: []string{"wrong-secret-name"},
			ProvisioningController: operatorv1.ProvisioningControllerConfiguration{
				BFBPersistentVolumeClaimName: "{\"school\":\"EFG\", \"standard\": \"2\", \"name\": \"abc\", \"city\": \"miami\"}'",
			},
		},
	}

	// Create a secret which marks envtest as a DPUCluster.
	dpuCluster := testutils.GetTestDPUCluster(testNS.Name, "envtest")
	kamajiSecret, err := testutils.GetFakeKamajiClusterSecretFromEnvtest(dpuCluster, cfg)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(testClient.Create(ctx, kamajiSecret)).To(Succeed())
	// Create a pull secret to be used by the DPFOperatorConfig.
	pullSecret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "secret-one", Namespace: testNS.Name}}
	g.Expect(testClient.Create(ctx, pullSecret)).To(Succeed())
	// Create the DPFOperatorConfig.
	g.Expect(testClient.Create(ctx, config)).To(Succeed())

	t.Run("ImagePullSecretsReconciled Error when secret can not be found", func(t *testing.T) {
		g.Eventually(func(g Gomega) {
			g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(config), config)).To(Succeed())
			assertConditions(g, config, map[string]string{
				"Ready":                      "Pending",
				"SystemComponentsReady":      "Error",
				"SystemComponentsReconciled": "Pending",
				"ImagePullSecretsReconciled": "Error",
			})
		}).WithTimeout(10 * time.Second).Should(Succeed())
	})
	t.Run("ImagePullSecretsReconciled Success after secret name is fixed", func(t *testing.T) {
		conf := &operatorv1.DPFOperatorConfig{}
		g.Eventually(func(g Gomega) {
			g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(config), conf)).To(Succeed())
			conf.Spec.ImagePullSecrets = []string{"secret-one"}
			g.Expect(testClient.Update(ctx, conf)).To(Succeed())
		}).WithTimeout(10 * time.Second).Should(Succeed())

		g.Eventually(func(g Gomega) {
			g.Expect(testutils.ForceObjectReconcileWithAnnotation(ctx, testClient, config)).To(Succeed())
			g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(config), conf)).To(Succeed())
			assertConditions(g, conf, map[string]string{
				"Ready":                      "Pending",
				"SystemComponentsReady":      "Error",
				"SystemComponentsReconciled": "Success",
				"ImagePullSecretsReconciled": "Success",
			})
		}).WithTimeout(5*time.Second).Should(Succeed(), fmt.Sprintf("test failed with %v", config))
	})

	t.Run("SystemComponentsReady AwaitingDeletion when DPFConfig is being deleted", func(t *testing.T) {
		dpuservice := &dpuservicev1.DPUService{}

		// Add a finalizer to a DPUService to prevent deletion from succeeding.
		g.Eventually(func(g Gomega) {
			g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: config.Namespace, Name: operatorv1.MultusName}, dpuservice)).To(Succeed())
			dpuservice.ObjectMeta.SetFinalizers(append(dpuservice.ObjectMeta.GetFinalizers(), "another"))
			g.Expect(testClient.Update(ctx, dpuservice)).To(Succeed())
		}).WithTimeout(10 * time.Second).Should(Succeed())

		// Delete the DPFOperatorConfig
		g.Expect(testClient.Delete(ctx, config)).To(Succeed())

		g.Eventually(func(g Gomega) {
			conf := &operatorv1.DPFOperatorConfig{}
			g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(config), conf)).To(Succeed())
			assertConditions(g, conf, map[string]string{
				"Ready":                      "AwaitingDeletion",
				"SystemComponentsReady":      "Error",
				"SystemComponentsReconciled": "AwaitingDeletion",
				"ImagePullSecretsReconciled": "Success",
			})
		}).WithTimeout(10 * time.Second).Should(Succeed())
	})

}

func TestDPFOperatorConfig_Validation(t *testing.T) {
	g := NewWithT(t)

	testNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "testns-"}}
	// Create the namespace for the test.
	g.Expect(testClient.Create(ctx, testNS)).To(Succeed())

	tests := []struct {
		name    string
		config  *operatorv1.DPFOperatorConfig
		wantErr bool
	}{
		{
			name: "succeed for valid image and helm chart name",
			config: &operatorv1.DPFOperatorConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "config",
					Namespace: testNS.Name,
				},
				Spec: operatorv1.DPFOperatorConfigSpec{
					ProvisioningController: operatorv1.ProvisioningControllerConfiguration{
						// Invalid image name.
						Image:                        ptr.To("example.com/dpu-provisioning-controller:v1.0.0"),
						BFBPersistentVolumeClaimName: "name",
					},
					Flannel: &operatorv1.FlannelConfiguration{
						HelmChart: ptr.To("oci://example.com/dpu-provisioning-controller:v1.0.0"),
					},
				},
			},
			wantErr: false,
		},
		{
			name: "fail for image with invalid name",
			config: &operatorv1.DPFOperatorConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "config",
					Namespace: testNS.Name,
				},
				Spec: operatorv1.DPFOperatorConfigSpec{
					ProvisioningController: operatorv1.ProvisioningControllerConfiguration{
						// Invalid image name.
						Image:                        ptr.To("--"),
						BFBPersistentVolumeClaimName: "name",
					},
				},
			},
			wantErr: true,
		},
		{
			name: "fail for helm chart with invalid name",
			config: &operatorv1.DPFOperatorConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "config",
					Namespace: testNS.Name,
				},
				Spec: operatorv1.DPFOperatorConfigSpec{
					ProvisioningController: operatorv1.ProvisioningControllerConfiguration{
						// Invalid image name.
						Image:                        ptr.To("example.com/dpu-provisioning-controller:v1.0.0"),
						BFBPersistentVolumeClaimName: "name",
					},
					Flannel: &operatorv1.FlannelConfiguration{
						// Helm chart missing prefix is invalid.
						HelmChart: ptr.To("example.com/dpu-provisioning-controller:v1.0.0"),
					},
				},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := testClient.Create(ctx, tt.config)
			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
				return
			}
			g.Expect(err).NotTo(HaveOccurred())
		})
	}

}

// assertCondition takes a map of Condition type to Condition reasons and asserts it against the conditions of the passed config.
func assertConditions(g Gomega, config *operatorv1.DPFOperatorConfig, assertion map[string]string) {
	g.Expect(config.Status.Conditions).To(HaveLen(4))
	for _, condition := range config.Status.Conditions {
		g.Expect(assertion[condition.Type]).To(Equal(condition.Reason),
			fmt.Sprintf("Expected condition %s to equal %s, actual %s. Message is %s", condition.Type, assertion[condition.Type], condition.Reason, condition.Message))
	}
}

func TestDPFOperatorConfigReconciler_Reconcile(t *testing.T) {
	g := NewWithT(t)
	testNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "testns-"}}
	// Create the namespace for the test.
	g.Expect(testClient.Create(ctx, testNS)).To(Succeed())

	initialImagePullSecrets := []string{"secret-one", "secret-two"}
	updatedImagePullSecrets := []string{"secret-two"}
	// Create the DPF ImagePullSecrets
	for _, imagePullSecret := range initialImagePullSecrets {
		g.Expect(testClient.Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: imagePullSecret, Namespace: testNS.Name}})).To(Succeed())
	}

	config := &operatorv1.DPFOperatorConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "config",
			Namespace: testNS.Name,
		},
		Spec: operatorv1.DPFOperatorConfigSpec{
			ImagePullSecrets: initialImagePullSecrets,
			Overrides: &operatorv1.Overrides{
				Paused: ptr.To(true),
			},
			ProvisioningController: operatorv1.ProvisioningControllerConfiguration{
				BFBPersistentVolumeClaimName: "foo-pvc",
			},
		},
	}

	t.Run("No reconcile when DPFOperatorConfig is paused", func(t *testing.T) {
		g.Expect(testClient.Create(ctx, config)).To(Succeed())
		g.Consistently(func(g Gomega) {
			gotConfig := &operatorv1.DPFOperatorConfig{}
			// Expect the config finalizers to not have been reconciled.
			g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(config), gotConfig)).To(Succeed())
			g.Expect(gotConfig.Finalizers).To(BeEmpty())

			// Expect secrets to not have been labeled.
			secrets := &corev1.SecretList{}
			g.Expect(testClient.List(ctx, secrets, client.HasLabels{dpuservicev1.DPFImagePullSecretLabelKey}, client.InNamespace(testNS.Name))).To(Succeed())
			g.Expect(secrets.Items).To(BeEmpty())

			// Expect no DPUServices to have been created.
			dpuServices := &dpuservicev1.DPUServiceList{}
			g.Expect(testClient.List(ctx, dpuServices, client.InNamespace(testNS.Name))).To(Succeed())
			g.Expect(dpuServices.Items).To(BeEmpty())
		}).WithTimeout(5 * time.Second).Should(Succeed())
	})

	t.Run("Reconcile Secrets and system components when DPFOperatorConfig is unpaused", func(t *testing.T) {
		// Patch the DPFOperatorConfig to remove `spec.paused`
		g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(config), config)).To(Succeed())
		patch := client.RawPatch(types.MergePatchType, []byte(fmt.Sprintf("{\"spec\": {\"overrides\": {\"paused\":%t}}}", false)))
		g.Expect(testClient.Patch(ctx, config, patch)).To(Succeed())

		// Expect Finalizers to be reconciled.
		g.Eventually(func(g Gomega) []string {
			gotConfig := &operatorv1.DPFOperatorConfig{}
			g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(config), gotConfig)).To(Succeed())
			return gotConfig.Finalizers
		}).WithTimeout(30 * time.Second).Should(ConsistOf([]string{operatorv1.DPFOperatorConfigFinalizer}))

		// Expect the secrets to have been correctly labeled.
		g.Eventually(func(g Gomega) {
			secrets := &corev1.SecretList{}
			g.Expect(testClient.List(ctx, secrets, client.HasLabels{dpuservicev1.DPFImagePullSecretLabelKey}, client.InNamespace(testNS.Name))).To(Succeed())
			g.Expect(secrets.Items).To(HaveLen(2))
		}).WithTimeout(30 * time.Second).Should(Succeed())

		// Expect the DPUService and Provisioning controller managers to be deployed.
		waitForDeployment(g, config.Namespace, "dpuservice-controller-manager")
		deployment := waitForDeployment(g, config.Namespace, "dpf-provisioning-controller-manager")
		verifyPVC(g, deployment, "foo-pvc")

		// Check the system components deployed as DPUServices are created as expected.
		waitForDPUService(g, config.Namespace, operatorv1.ServiceSetControllerName, initialImagePullSecrets)
		waitForDPUService(g, config.Namespace, operatorv1.MultusName, initialImagePullSecrets)
		waitForDPUService(g, config.Namespace, operatorv1.SRIOVDevicePluginName, initialImagePullSecrets)
		waitForDPUService(g, config.Namespace, operatorv1.FlannelName, initialImagePullSecrets)
		waitForDPUService(g, config.Namespace, operatorv1.NVIPAMName, initialImagePullSecrets)
		waitForDPUService(g, config.Namespace, operatorv1.OVSCNIName, initialImagePullSecrets)
		waitForDPUService(g, config.Namespace, operatorv1.SFCControllerName, initialImagePullSecrets)
		waitForDPUService(g, config.Namespace, operatorv1.OVSHelperName, initialImagePullSecrets)
	})

	t.Run("Remove label from Secrets when they are removed from the DPFOperatorConfig", func(t *testing.T) {
		// Patch the DPFOperatorConfig to remove "secret-one" from the image pull secrets.
		g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(config), config)).To(Succeed())
		patch := client.MergeFrom(config.DeepCopy())
		config.Spec.ImagePullSecrets = updatedImagePullSecrets
		g.Expect(testClient.Patch(ctx, config, patch)).To(Succeed())
		// Expect the label to have been removed from secret-one.
		g.Eventually(func(g Gomega) {
			secrets := &corev1.SecretList{}
			g.Expect(testClient.List(ctx, secrets, client.HasLabels{dpuservicev1.DPFImagePullSecretLabelKey}, client.InNamespace(testNS.Name))).To(Succeed())
			g.Expect(secrets.Items).To(HaveLen(1))
		}).WithTimeout(30 * time.Second).Should(Succeed())
	})

	t.Run("update images and helm charts for objects deployed by the DPF Operator ", func(t *testing.T) {
		// Set the image and helm chart for each component deployed by DPF.
		g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(config), config)).To(Succeed())
		configCopy := config.DeepCopy()

		imageTemplate := "release-artifacts.com/%s:v1.0"
		helmTemplate := "oci://release-artifacts.com/%s:v1.0"
		// Update the config with
		config.Spec = operatorv1.DPFOperatorConfigSpec{
			ImagePullSecrets: initialImagePullSecrets,

			// For objects which are deployed as raw manifests set the image field in configuration.
			ProvisioningController: operatorv1.ProvisioningControllerConfiguration{
				BFBPersistentVolumeClaimName: "foo-pvc",
				Image:                        ptr.To(fmt.Sprintf(imageTemplate, operatorv1.ProvisioningControllerName)),
			},
			DPUServiceController: &operatorv1.DPUServiceControllerConfiguration{
				Image: ptr.To(fmt.Sprintf(imageTemplate, operatorv1.DPUServiceControllerName)),
			},
			KamajiClusterManager: &operatorv1.KamajiClusterManagerConfiguration{
				Image:   ptr.To(fmt.Sprintf(imageTemplate, operatorv1.KamajiClusterManagerName)),
				Disable: ptr.To(false),
			},
			StaticClusterManager: &operatorv1.StaticClusterManagerConfiguration{
				Image:   ptr.To(fmt.Sprintf(imageTemplate, operatorv1.StaticClusterManagerName)),
				Disable: ptr.To(false),
			},

			// For objects which are deployed as DPUServices set the helm chart field in configuration.
			ServiceSetController: &operatorv1.ServiceSetControllerConfiguration{
				HelmChart: ptr.To(fmt.Sprintf(helmTemplate, operatorv1.ServiceSetControllerName)),
			},
			Multus: &operatorv1.MultusConfiguration{
				HelmChart: ptr.To(fmt.Sprintf(helmTemplate, operatorv1.MultusName)),
			},
			SRIOVDevicePlugin: &operatorv1.SRIOVDevicePluginConfiguration{
				HelmChart: ptr.To(fmt.Sprintf(helmTemplate, operatorv1.SRIOVDevicePluginName)),
			},
			Flannel: &operatorv1.FlannelConfiguration{
				HelmChart: ptr.To(fmt.Sprintf(helmTemplate, operatorv1.FlannelName)),
			},
			OVSCNI: &operatorv1.OVSCNIConfiguration{
				HelmChart: ptr.To(fmt.Sprintf(helmTemplate, operatorv1.OVSCNIName)),
			},
			NVIPAM: &operatorv1.NVIPAMConfiguration{
				HelmChart: ptr.To(fmt.Sprintf(helmTemplate, operatorv1.NVIPAMName)),
			},
			SFCController: &operatorv1.SFCControllerConfiguration{
				HelmChart: ptr.To(fmt.Sprintf(helmTemplate, operatorv1.SFCControllerName)),
			},
			OVSHelper: &operatorv1.OVSHelperConfiguration{
				HelmChart: ptr.To(fmt.Sprintf(helmTemplate, operatorv1.OVSHelperName)),
			},
		}
		g.Expect(testClient.Patch(ctx, config, client.MergeFrom(configCopy))).To(Succeed())
		g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(config), config)).To(Succeed())

		g.Eventually(func(g Gomega) {
			g.Expect(firstContainerHasImageWithName(
				waitForDeployment(g, config.Namespace, "dpf-provisioning-controller-manager"),
				fmt.Sprintf(imageTemplate, operatorv1.ProvisioningControllerName),
			)).To(BeTrue())

			g.Expect(firstContainerHasImageWithName(
				waitForDeployment(g, config.Namespace, "dpuservice-controller-manager"),
				fmt.Sprintf(imageTemplate, operatorv1.DPUServiceControllerName),
			)).To(BeTrue())

			g.Expect(firstContainerHasImageWithName(
				waitForDeployment(g, config.Namespace, "static-cm-controller-manager"),
				fmt.Sprintf(imageTemplate, operatorv1.StaticClusterManagerName),
			)).To(BeTrue())

			g.Expect(firstContainerHasImageWithName(
				waitForDeployment(g, config.Namespace, "kamaji-cm-controller-manager"),
				fmt.Sprintf(imageTemplate, operatorv1.KamajiClusterManagerName),
			)).To(BeTrue())

			g.Expect(dpuServiceReferencesHelmChart(
				waitForDPUService(g, config.Namespace, operatorv1.ServiceSetControllerName, initialImagePullSecrets),
				fmt.Sprintf(helmTemplate, operatorv1.ServiceSetControllerName),
			)).To(BeTrue())

			g.Expect(dpuServiceReferencesHelmChart(
				waitForDPUService(g, config.Namespace, operatorv1.OVSCNIName, initialImagePullSecrets),
				fmt.Sprintf(helmTemplate, operatorv1.OVSCNIName),
			)).To(BeTrue())

			g.Expect(dpuServiceReferencesHelmChart(
				waitForDPUService(g, config.Namespace, operatorv1.SRIOVDevicePluginName, initialImagePullSecrets),
				fmt.Sprintf(helmTemplate, operatorv1.SRIOVDevicePluginName),
			)).To(BeTrue())

			g.Expect(dpuServiceReferencesHelmChart(
				waitForDPUService(g, config.Namespace, operatorv1.FlannelName, initialImagePullSecrets),
				fmt.Sprintf(helmTemplate, operatorv1.FlannelName),
			)).To(BeTrue())

			g.Expect(dpuServiceReferencesHelmChart(
				waitForDPUService(g, config.Namespace, operatorv1.MultusName, initialImagePullSecrets),
				fmt.Sprintf(helmTemplate, operatorv1.MultusName),
			)).To(BeTrue())

			g.Expect(dpuServiceReferencesHelmChart(
				waitForDPUService(g, config.Namespace, operatorv1.SFCControllerName, initialImagePullSecrets),
				fmt.Sprintf(helmTemplate, operatorv1.SFCControllerName),
			)).To(BeTrue())

			g.Expect(dpuServiceReferencesHelmChart(
				waitForDPUService(g, config.Namespace, operatorv1.NVIPAMName, initialImagePullSecrets),
				fmt.Sprintf(helmTemplate, operatorv1.NVIPAMName),
			)).To(BeTrue())

			g.Expect(dpuServiceReferencesHelmChart(
				waitForDPUService(g, config.Namespace, operatorv1.OVSHelperName, initialImagePullSecrets),
				fmt.Sprintf(helmTemplate, operatorv1.OVSHelperName),
			)).To(BeTrue())

		}).WithTimeout(20 * time.Second).Should(Succeed())

	})
	t.Run("Delete system components when they are disabled in the DPFOperatorConfig", func(t *testing.T) {
		// Patch the DPFOperatorConfig to disable multus deployment.
		g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(config), config)).To(Succeed())
		configCopy := config.DeepCopy()
		config.Spec.Multus = &operatorv1.MultusConfiguration{Disable: ptr.To(true)}
		g.Expect(testClient.Patch(ctx, config, client.MergeFrom(configCopy))).To(Succeed())

		// Expect the DPUService and Provisioning controller managers to be deployed.
		waitForDeployment(g, config.Namespace, "dpuservice-controller-manager")
		waitForDeployment(g, config.Namespace, "dpf-provisioning-controller-manager")

		// Check the system components deployed as DPUServices are created as expected.
		waitForDPUService(g, config.Namespace, operatorv1.ServiceSetControllerName, initialImagePullSecrets)
		waitForDPUService(g, config.Namespace, operatorv1.SRIOVDevicePluginName, initialImagePullSecrets)
		waitForDPUService(g, config.Namespace, operatorv1.FlannelName, initialImagePullSecrets)
		waitForDPUService(g, config.Namespace, operatorv1.NVIPAMName, initialImagePullSecrets)
		waitForDPUService(g, config.Namespace, operatorv1.OVSCNIName, initialImagePullSecrets)
		waitForDPUService(g, config.Namespace, operatorv1.SFCControllerName, initialImagePullSecrets)
		waitForDPUService(g, config.Namespace, operatorv1.OVSHelperName, initialImagePullSecrets)
		g.Eventually(func(g Gomega) {
			dpuservices := &dpuservicev1.DPUServiceList{}
			g.Expect(testClient.List(ctx, dpuservices)).To(Succeed())
			err := testClient.Get(ctx, client.ObjectKey{
				Namespace: config.Namespace,
				Name:      operatorv1.MultusName},
				&dpuservicev1.DPUService{})
			g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
		}).WithTimeout(10 * time.Second).Should(Succeed())
	})
	t.Run("Delete Operator config", func(t *testing.T) {
		g.Expect(testClient.Delete(ctx, config)).To(Succeed())
		g.Eventually(func(g Gomega) {
			g.Expect(apierrors.IsNotFound(testClient.Get(ctx, client.ObjectKeyFromObject(config), config))).To(BeTrue())
		}).WithTimeout(30 * time.Second).Should(Succeed())
	})
}

func dpuServiceReferencesHelmChart(dpuService *dpuservicev1.DPUService, chart string) bool {
	helmSource, err := inventory.ParseHelmChartString(chart)
	if err != nil {
		return false
	}
	dpuServiceHelmSource := dpuService.Spec.HelmChart.Source
	return helmSource.Chart == dpuServiceHelmSource.Chart &&
		helmSource.Repo == dpuServiceHelmSource.RepoURL &&
		helmSource.Version == dpuServiceHelmSource.Version
}
func firstContainerHasImageWithName(deployment *appsv1.Deployment, imageName string) bool {
	return deployment.Spec.Template.Spec.Containers[0].Image == imageName
}

func waitForDPUService(g Gomega, ns, name string, imagePullSecrets []string) *dpuservicev1.DPUService {
	dpuservice := &dpuservicev1.DPUService{}
	g.Eventually(func(g Gomega) {
		g.Expect(testClient.Get(ctx, client.ObjectKey{
			Namespace: ns,
			Name:      name},
			dpuservice)).To(Succeed())
		var result map[string]interface{}
		g.Expect(json.Unmarshal(dpuservice.Spec.HelmChart.Values.Raw, &result)).To(Succeed())

		// Each system DPUService should have specific values under values.$SERVICE_NAME
		serviceValues, ok := result[name].(map[string]interface{})
		g.Expect(ok).To(BeTrue())
		g.Expect(serviceValues).To(HaveKey("imagePullSecrets"))
		secrets, ok := serviceValues["imagePullSecrets"].([]interface{})
		g.Expect(ok).To(BeTrue())
		g.Expect(secrets).To(HaveLen(len(imagePullSecrets)))
		for i := range secrets {
			secret, ok := secrets[i].(map[string]interface{})
			g.Expect(ok).To(BeTrue())
			g.Expect(secret["name"]).To(Equal(imagePullSecrets[i]))
		}
	}).WithTimeout(30 * time.Second).Should(Succeed())
	return dpuservice
}

func waitForDeployment(g Gomega, ns, name string) *appsv1.Deployment {
	deployment := &appsv1.Deployment{}
	g.Eventually(func(g Gomega) {
		g.Expect(testClient.Get(ctx, client.ObjectKey{
			Namespace: ns,
			Name:      name},
			deployment)).To(Succeed())
	}).WithTimeout(30 * time.Second).Should(Succeed())
	return deployment
}

func verifyPVC(g Gomega, deployment *appsv1.Deployment, expected string) {
	var bfbPVC *corev1.PersistentVolumeClaimVolumeSource
	for _, vol := range deployment.Spec.Template.Spec.Volumes {
		if vol.Name == "bfb-volume" && vol.PersistentVolumeClaim != nil {
			bfbPVC = vol.PersistentVolumeClaim
			break
		}
	}
	g.Expect(bfbPVC).NotTo(BeNil())
	g.Expect(bfbPVC.ClaimName).To(Equal(expected))
}

func TestApplySetCreationUpgradeDeletion(t *testing.T) {
	g := NewWithT(t)
	ns := "test-generateandpatchobjects"
	testComponentName := "test-component"
	objOne := testObject(ns, "obj-one")
	objTwo := testObject(ns, "obj-two")
	applySet := testApplySet(ns, inventory.StubComponentWithObjs(testComponentName, []*unstructured.Unstructured{}))
	vars := inventory.Variables{Namespace: ns}
	g.Expect(testClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}})).To(Succeed())
	t.Run("test component initial creation with two objects", func(t *testing.T) {
		// This test calls the reconciler method directly, but the test component is not being reconciled
		// by other tests and we do not create a DPFOperatorConfig.
		r := &DPFOperatorConfigReconciler{
			Inventory: &inventory.SystemComponents{},
			Client:    testClient,
			Settings:  &DPFOperatorConfigReconcilerSettings{},
		}

		component := inventory.StubComponentWithObjs(testComponentName, []*unstructured.Unstructured{objOne, objTwo})

		// This test calls the reconciler method directly, but the test component is not being reconciled
		// by other tests and we do not create a DPFOperatorConfig.
		err := r.generateAndPatchObjects(ctx, component, vars)
		g.Expect(err).NotTo(HaveOccurred())

		// Expect both objects and the ApplySet parent object to be created.
		g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(objOne), objOne)).To(Succeed())
		g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(objTwo), objTwo)).To(Succeed())
		g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(applySet), applySet)).To(Succeed())

		// Expect the inventory annotation to have two items.
		gknnList, ok := applySet.GetAnnotations()[inventory.ApplySetInventoryAnnotationKey]
		g.Expect(ok).To(BeTrue())
		g.Expect(strings.Split(gknnList, ",")).To(HaveLen(2))
	})

	t.Run("test object is deleted and removed from ApplySet when removed from component inventory", func(t *testing.T) {
		// This test calls the reconciler method directly, but the test component is not being reconciled
		// by other tests and we do not create a DPFOperatorConfig.
		r := &DPFOperatorConfigReconciler{
			Inventory: &inventory.SystemComponents{},
			Client:    testClient,
			Settings:  &DPFOperatorConfigReconcilerSettings{},
		}

		component := inventory.StubComponentWithObjs(testComponentName, []*unstructured.Unstructured{
			// obj-one is deleted here.
			//objOne,
			objTwo,
		})

		// This test calls the reconciler method directly, but the test component is not being reconciled
		// by other tests and we do not create a DPFOperatorConfig.
		err := r.generateAndPatchObjects(ctx, component, vars)
		g.Expect(err).NotTo(HaveOccurred())

		// Expect obj-two and the ApplySet parent object to be created.
		g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(objTwo), objTwo)).To(Succeed())
		g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(applySet), applySet)).To(Succeed())

		// Expect obj-one to be deleted.
		g.Expect(apierrors.IsNotFound(testClient.Get(ctx, client.ObjectKeyFromObject(objOne), objOne))).To(BeTrue())

		gknnList, ok := applySet.GetAnnotations()[inventory.ApplySetInventoryAnnotationKey]
		g.Expect(ok).To(BeTrue())
		g.Expect(strings.Split(gknnList, ",")).To(HaveLen(1))

	})

	t.Run("test objects and ApplySet when component returns no inventory", func(t *testing.T) {
		r := &DPFOperatorConfigReconciler{
			Inventory: &inventory.SystemComponents{},
			Client:    testClient,
			Settings:  &DPFOperatorConfigReconcilerSettings{},
		}

		component := inventory.StubComponentWithObjs(testComponentName, []*unstructured.Unstructured{})

		// This test calls the reconciler method directly, but the test component is not being reconciled
		// by other tests and we do not create a DPFOperatorConfig.
		err := r.generateAndPatchObjects(ctx, component, vars)
		g.Expect(err).NotTo(HaveOccurred())

		// Expect objects to be deleted.
		g.Expect(apierrors.IsNotFound(testClient.Get(ctx, client.ObjectKeyFromObject(objOne), objOne))).To(BeTrue())
		g.Expect(apierrors.IsNotFound(testClient.Get(ctx, client.ObjectKeyFromObject(objTwo), objTwo))).To(BeTrue())

		// Expect the ApplySet to be deleted.
		g.Expect(apierrors.IsNotFound(testClient.Get(ctx, client.ObjectKeyFromObject(applySet), applySet))).To(BeTrue())

	})

}

func testApplySet(namespace string, component inventory.Component) *corev1.Secret {
	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      inventory.ApplySetName(component),
			Namespace: namespace,
		},
	}
}
func testObject(namespace, name string) *unstructured.Unstructured {
	uns := &unstructured.Unstructured{}
	uns.SetKind("ConfigMap")
	uns.SetAPIVersion("v1")
	uns.SetNamespace(namespace)
	uns.SetName(name)
	return uns
}
