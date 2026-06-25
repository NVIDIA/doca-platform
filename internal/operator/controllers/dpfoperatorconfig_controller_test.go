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
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	argocdpkg "github.com/nvidia/doca-platform/internal/argocd"
	"github.com/nvidia/doca-platform/internal/digest"
	"github.com/nvidia/doca-platform/internal/operator/inventory"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/release"
	"github.com/nvidia/doca-platform/pkg/conditions"
	"github.com/nvidia/doca-platform/pkg/dpucluster"
	testutils "github.com/nvidia/doca-platform/test/utils"
	argov1 "github.com/nvidia/doca-platform/third_party/forked/argoproj/argo-cd/pkg/apis/application/v1alpha1"

	"github.com/Masterminds/semver/v3"
	"github.com/fluxcd/pkg/runtime/patch"
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

const legacyDPFVersionWithoutKubeletSupport = "v25.10.1"

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

			DeploymentMode: operatorv1.DeploymentModeHostTrusted,
			// This secret name is wrong - this prevents ImagePullSecretsReconciled from becoming true.
			ImagePullSecrets: []string{"wrong-secret-name"},
			ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{
				BFBPersistentVolumeClaimName: ptr.To("{\"school\":\"EFG\", \"standard\": \"2\", \"name\": \"abc\", \"city\": \"miami\"}'"),
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
				"PreUpgradeValidationReady":  "Success",
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
				"PreUpgradeValidationReady":  "Success",
			})
		}).WithTimeout(5*time.Second).Should(Succeed(), fmt.Sprintf("test failed with %v", config))
	})

	t.Run("SystemComponentsReady AwaitingDeletion when DPFConfig is being deleted", func(t *testing.T) {
		dpuservice := &dpuservicev1.DPUService{}

		// Add a finalizer to a DPUService to prevent deletion from succeeding.
		g.Eventually(func(g Gomega) {
			g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: config.Namespace, Name: operatorv1.MultusName.String()}, dpuservice)).To(Succeed())
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
				"PreUpgradeValidationReady":  "Success",
			})
		}).WithTimeout(10 * time.Second).Should(Succeed())

		g.Eventually(func(g Gomega) {
			g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: config.Namespace, Name: operatorv1.MultusName.String()}, dpuservice)).To(Succeed())
			// Remove the finalizers from the DPUService to enable deletion.
			dpuservice.ObjectMeta.SetFinalizers([]string{})
			g.Expect(testClient.Update(ctx, dpuservice)).To(Succeed())
		}).WithTimeout(10 * time.Second).Should(Succeed())

		// Wait for DPFOperatorConfig to be deleted
		g.Eventually(func(g Gomega) {
			conf := &operatorv1.DPFOperatorConfig{}
			g.Expect(apierrors.IsNotFound(testClient.Get(ctx, client.ObjectKeyFromObject(config), conf))).To(BeTrue())
		}).WithTimeout(10 * time.Second).Should(Succeed())
	})
}

func TestDPFOperatorConfigReconciler_Validation(t *testing.T) {
	g := NewWithT(t)

	testNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "testns-"}}
	// Create the namespace for the test.
	g.Expect(testClient.Create(ctx, testNS)).To(Succeed())

	config := &operatorv1.DPFOperatorConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "config",
			Namespace: testNS.Name,
		},
		Spec: operatorv1.DPFOperatorConfigSpec{
			DeploymentMode: operatorv1.DeploymentModeHostTrusted,
			ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{
				BFBPersistentVolumeClaimName: ptr.To("foo"),
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
		}).WithTimeout(10 * time.Second).Should(Succeed())
	})

	g.Expect(testutils.CleanupAndWait(ctx, testClient, config)).To(Succeed())
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
			name: "succeed for valid image, helm chart name, and MaxDPUParallelInstallations",
			config: &operatorv1.DPFOperatorConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "config-max-dpu-parallel-installations",
					Namespace: testNS.Name,
				},
				Spec: operatorv1.DPFOperatorConfigSpec{
					DeploymentMode: operatorv1.DeploymentModeHostTrusted,
					ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{
						Controller: &operatorv1.DefaultOverridesConfiguration{
							ImageComponentConfig: operatorv1.ImageComponentConfig{
								Image: ptr.To("example.com/dpu-provisioning-controller:v1.0.0"),
							},
						},
						BFBPersistentVolumeClaimName: ptr.To("name"),
						MaxDPUParallelInstallations:  ptr.To(int32(10)),
					},
					Flannel: &operatorv1.FlannelConfiguration{
						HelmComponentConfig: operatorv1.HelmComponentConfig{
							HelmChart: ptr.To("oci://example.com/dpu-provisioning-controller:v1.0.0"),
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "fail for MaxDPUParallelInstallations below minimum",
			config: &operatorv1.DPFOperatorConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "config-max-dpu-parallel-installations-below-minimum",
					Namespace: testNS.Name,
				},
				Spec: operatorv1.DPFOperatorConfigSpec{
					DeploymentMode: operatorv1.DeploymentModeHostTrusted,
					ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{
						BFBPersistentVolumeClaimName: ptr.To("name"),
						MaxDPUParallelInstallations:  ptr.To(int32(0)),
					},
				},
			},
			wantErr: true,
		},
		{
			name: "succeed for nil MaxDPUParallelInstallations (uses default)",
			config: &operatorv1.DPFOperatorConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "config-max-dpu-parallel-installations-nil",
					Namespace: testNS.Name,
				},
				Spec: operatorv1.DPFOperatorConfigSpec{
					DeploymentMode: operatorv1.DeploymentModeHostTrusted,
					ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{
						BFBPersistentVolumeClaimName: ptr.To("name"),
					},
				},
			},
			wantErr: false,
		},
		{
			name: "succeed for valid image and helm chart name",
			config: &operatorv1.DPFOperatorConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "config-valid-image-helm-chart",
					Namespace: testNS.Name,
				},
				Spec: operatorv1.DPFOperatorConfigSpec{
					DeploymentMode: operatorv1.DeploymentModeHostTrusted,
					ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{
						Controller: &operatorv1.DefaultOverridesConfiguration{
							ImageComponentConfig: operatorv1.ImageComponentConfig{
								// Invalid image name.
								Image: ptr.To("example.com/dpu-provisioning-controller:v1.0.0"),
							},
						},
						BFBPersistentVolumeClaimName: ptr.To("name"),
					},
					Flannel: &operatorv1.FlannelConfiguration{
						HelmComponentConfig: operatorv1.HelmComponentConfig{
							HelmChart: ptr.To("oci://example.com/dpu-provisioning-controller:v1.0.0"),
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "fail for image with invalid name",
			config: &operatorv1.DPFOperatorConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "config-invalid-image",
					Namespace: testNS.Name,
				},
				Spec: operatorv1.DPFOperatorConfigSpec{
					DeploymentMode: operatorv1.DeploymentModeHostTrusted,
					ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{
						Controller: &operatorv1.DefaultOverridesConfiguration{
							ImageComponentConfig: operatorv1.ImageComponentConfig{
								// Invalid image name.
								Image: ptr.To("--"),
							},
						},
						BFBPersistentVolumeClaimName: ptr.To("name"),
					},
				},
			},
			wantErr: true,
		},
		{
			name: "fail for helm chart with invalid name",
			config: &operatorv1.DPFOperatorConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "config-invalid-helm-chart",
					Namespace: testNS.Name,
				},
				Spec: operatorv1.DPFOperatorConfigSpec{
					DeploymentMode: operatorv1.DeploymentModeHostTrusted,
					ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{
						Controller: &operatorv1.DefaultOverridesConfiguration{
							ImageComponentConfig: operatorv1.ImageComponentConfig{
								// Invalid image name.
								Image: ptr.To("example.com/dpu-provisioning-controller:v1.0.0"),
							},
						},
						BFBPersistentVolumeClaimName: ptr.To("name"),
					},
					Flannel: &operatorv1.FlannelConfiguration{
						HelmComponentConfig: operatorv1.HelmComponentConfig{
							// Helm chart missing prefix is invalid.
							HelmChart: ptr.To("example.com/dpu-provisioning-controller:v1.0.0"),
						},
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
			g.Expect(testutils.CleanupAndWait(ctx, testClient, tt.config)).To(Succeed())
		})
	}
}

// assertCondition takes a map of Condition type to Condition reasons and asserts it against the conditions of the passed config.
func assertConditions(g Gomega, config *operatorv1.DPFOperatorConfig, assertion map[string]string) {
	g.Expect(config.Status.Conditions).To(HaveLen(len(operatorv1.Conditions)))
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
			DeploymentMode:   operatorv1.DeploymentModeHostTrusted,
			ImagePullSecrets: initialImagePullSecrets,
			Overrides: &operatorv1.Overrides{
				Paused: ptr.To(true),
			},
			ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{
				BFBPersistentVolumeClaimName: ptr.To("foo-pvc"),
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
		waitForDPUService(g, config.Namespace, operatorv1.ServiceSetControllerName, operatorv1.ServiceChainSetCRDsName.String(), initialImagePullSecrets)
		waitForDPUService(g, config.Namespace, operatorv1.MultusName, operatorv1.MultusName.String(), initialImagePullSecrets)
		waitForDPUService(g, config.Namespace, operatorv1.SRIOVDevicePluginName, operatorv1.SRIOVDevicePluginName.String(), initialImagePullSecrets)
		waitForDPUService(g, config.Namespace, operatorv1.FlannelName, operatorv1.FlannelName.String(), initialImagePullSecrets)
		waitForDPUService(g, config.Namespace, operatorv1.NVIPAMControllerName, operatorv1.NVIPAMNodeName.String(), initialImagePullSecrets)
		waitForDPUService(g, config.Namespace, operatorv1.SFCControllerName, operatorv1.SFCControllerName.String(), initialImagePullSecrets)

		// Check that DPUServiceNADs are deployed
		waitForDPUServiceNAD(g, config.Namespace, "mybrsfc")
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

	t.Run("update images and helm charts for objects deployed by the DPF Operator", func(t *testing.T) {
		// Set the image and helm chart for each component deployed by DPF.
		g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(config), config)).To(Succeed())
		configCopy := config.DeepCopy()

		imageTemplate := "release-artifacts.com/%s:v1.0"
		helmTemplate := "oci://release-artifacts.com/%s:v1.0"
		// Update the config with
		config.Spec = operatorv1.DPFOperatorConfigSpec{
			DeploymentMode:   operatorv1.DeploymentModeHostTrusted,
			ImagePullSecrets: initialImagePullSecrets,

			// For objects which are deployed as raw manifests set the image field in configuration.
			ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{
				BFBPersistentVolumeClaimName: ptr.To("foo-pvc"),
				Controller: &operatorv1.DefaultOverridesConfiguration{
					ImageComponentConfig: operatorv1.ImageComponentConfig{
						Image: ptr.To(fmt.Sprintf(imageTemplate, operatorv1.ProvisioningControllerName)),
					},
				},
			},
			DPUServiceController: &operatorv1.DPUServiceControllerConfiguration{
				Controller: &operatorv1.DefaultOverridesConfiguration{
					ImageComponentConfig: operatorv1.ImageComponentConfig{
						Image: ptr.To(fmt.Sprintf(imageTemplate, operatorv1.DPUServiceControllerName)),
					},
				},
			},
			KamajiClusterManager: &operatorv1.KamajiClusterManagerConfiguration{
				Controller: &operatorv1.DefaultOverridesConfiguration{
					ImageComponentConfig: operatorv1.ImageComponentConfig{
						Image: ptr.To(fmt.Sprintf(imageTemplate, operatorv1.KamajiClusterManagerName)),
					},
				},
				BaseComponentConfig: operatorv1.BaseComponentConfig{
					Disable: ptr.To(false),
				},
			},
			StaticClusterManager: &operatorv1.StaticClusterManagerConfiguration{
				Controller: &operatorv1.DefaultOverridesConfiguration{
					ImageComponentConfig: operatorv1.ImageComponentConfig{
						Image: ptr.To(fmt.Sprintf(imageTemplate, operatorv1.StaticClusterManagerName)),
					},
				},
				BaseComponentConfig: operatorv1.BaseComponentConfig{
					Disable: ptr.To(false),
				},
			},

			// For objects which are deployed as DPUServices set the helm chart field in configuration.
			ServiceSetController: &operatorv1.ServiceSetControllerConfiguration{
				HelmComponentConfig: operatorv1.HelmComponentConfig{
					HelmChart: ptr.To(fmt.Sprintf(helmTemplate, operatorv1.ServiceSetControllerName)),
				},
			},
			Multus: &operatorv1.MultusConfiguration{
				HelmComponentConfig: operatorv1.HelmComponentConfig{
					HelmChart: ptr.To(fmt.Sprintf(helmTemplate, operatorv1.MultusName)),
				},
			},
			SRIOVDevicePlugin: &operatorv1.SRIOVDevicePluginConfiguration{
				HelmComponentConfig: operatorv1.HelmComponentConfig{
					HelmChart: ptr.To(fmt.Sprintf(helmTemplate, operatorv1.SRIOVDevicePluginName)),
				},
			},
			Flannel: &operatorv1.FlannelConfiguration{
				HelmComponentConfig: operatorv1.HelmComponentConfig{
					HelmChart: ptr.To(fmt.Sprintf(helmTemplate, operatorv1.FlannelName)),
				},
			},
			NVIPAM: &operatorv1.NVIPAMConfiguration{
				HelmComponentConfig: operatorv1.HelmComponentConfig{
					HelmChart: ptr.To(fmt.Sprintf(helmTemplate, operatorv1.NVIPAMControllerName)),
				},
			},
			SFCController: &operatorv1.SFCControllerConfiguration{
				HelmComponentConfig: operatorv1.HelmComponentConfig{
					HelmChart: ptr.To(fmt.Sprintf(helmTemplate, operatorv1.SFCControllerName)),
				},
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
				waitForDPUService(g, config.Namespace, operatorv1.ServiceSetControllerName, operatorv1.ServiceChainSetCRDsName.String(), initialImagePullSecrets),
				fmt.Sprintf(helmTemplate, operatorv1.ServiceSetControllerName),
			)).To(BeTrue())

			g.Expect(dpuServiceReferencesHelmChart(
				waitForDPUService(g, config.Namespace, operatorv1.SRIOVDevicePluginName, operatorv1.SRIOVDevicePluginName.String(), initialImagePullSecrets),
				fmt.Sprintf(helmTemplate, operatorv1.SRIOVDevicePluginName),
			)).To(BeTrue())

			g.Expect(dpuServiceReferencesHelmChart(
				waitForDPUService(g, config.Namespace, operatorv1.FlannelName, operatorv1.FlannelName.String(), initialImagePullSecrets),
				fmt.Sprintf(helmTemplate, operatorv1.FlannelName),
			)).To(BeTrue())

			g.Expect(dpuServiceReferencesHelmChart(
				waitForDPUService(g, config.Namespace, operatorv1.MultusName, operatorv1.MultusName.String(), initialImagePullSecrets),
				fmt.Sprintf(helmTemplate, operatorv1.MultusName),
			)).To(BeTrue())

			g.Expect(dpuServiceReferencesHelmChart(
				waitForDPUService(g, config.Namespace, operatorv1.SFCControllerName, operatorv1.SFCControllerName.String(), initialImagePullSecrets),
				fmt.Sprintf(helmTemplate, operatorv1.SFCControllerName),
			)).To(BeTrue())

			g.Expect(dpuServiceReferencesHelmChart(
				waitForDPUService(g, config.Namespace, operatorv1.NVIPAMControllerName, operatorv1.NVIPAMNodeName.String(), initialImagePullSecrets),
				fmt.Sprintf(helmTemplate, operatorv1.NVIPAMControllerName),
			)).To(BeTrue())

		}).WithTimeout(20 * time.Second).Should(Succeed())

	})

	t.Run("Delete system components when they are disabled in the DPFOperatorConfig", func(t *testing.T) {
		// Patch the DPFOperatorConfig to disable multus deployment.
		g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(config), config)).To(Succeed())
		configCopy := config.DeepCopy()
		config.Spec.Multus = &operatorv1.MultusConfiguration{
			BaseComponentConfig: operatorv1.BaseComponentConfig{Disable: ptr.To(true)},
		}
		g.Expect(testClient.Patch(ctx, config, client.MergeFrom(configCopy))).To(Succeed())

		// Expect the DPUService and Provisioning controller managers to be deployed.
		waitForDeployment(g, config.Namespace, "dpuservice-controller-manager")
		waitForDeployment(g, config.Namespace, "dpf-provisioning-controller-manager")

		// Check the system components deployed as DPUServices are created as expected.
		waitForDPUService(g, config.Namespace, operatorv1.ServiceSetControllerName, operatorv1.ServiceChainSetCRDsName.String(), initialImagePullSecrets)
		waitForDPUService(g, config.Namespace, operatorv1.SRIOVDevicePluginName, operatorv1.SRIOVDevicePluginName.String(), initialImagePullSecrets)
		waitForDPUService(g, config.Namespace, operatorv1.FlannelName, operatorv1.FlannelName.String(), initialImagePullSecrets)
		waitForDPUService(g, config.Namespace, operatorv1.NVIPAMControllerName, operatorv1.NVIPAMNodeName.String(), initialImagePullSecrets)
		waitForDPUService(g, config.Namespace, operatorv1.SFCControllerName, operatorv1.SFCControllerName.String(), initialImagePullSecrets)
		waitForDPUService(g, config.Namespace, operatorv1.CNIInstallerName, operatorv1.CNIInstallerName.String(), initialImagePullSecrets)
		g.Eventually(func(g Gomega) {
			dpuservices := &dpuservicev1.DPUServiceList{}
			g.Expect(testClient.List(ctx, dpuservices)).To(Succeed())
			err := testClient.Get(ctx, client.ObjectKey{
				Namespace: config.Namespace,
				Name:      operatorv1.MultusName.String()},
				&dpuservicev1.DPUService{})
			g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
		}).WithTimeout(10 * time.Second).Should(Succeed())
	})
	t.Run("Delete Operator config", func(t *testing.T) {
		g.Expect(testClient.Delete(ctx, config)).To(Succeed())
		g.Eventually(func(g Gomega) {
			g.Expect(apierrors.IsNotFound(testClient.Get(ctx, client.ObjectKeyFromObject(config), config))).To(BeTrue())
		}).WithTimeout(60 * time.Second).Should(Succeed())
	})
}

func TestDPFOperatorConfigReconciler_ReconcileWithTwoDPUClusters(t *testing.T) {
	g := NewWithT(t)
	testNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "testns-"}}
	// Create the namespace for the test.
	g.Expect(testClient.Create(ctx, testNS)).To(Succeed())

	initialImagePullSecrets := []string{"secret-one", "secret-two"}
	// Create the DPF ImagePullSecrets
	for _, imagePullSecret := range initialImagePullSecrets {
		g.Expect(testClient.Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: imagePullSecret, Namespace: testNS.Name}})).To(Succeed())
	}

	// Create two DPUClusters
	dpuCluster1 := testutils.GetTestDPUCluster(testNS.Name, "cluster-1")
	kamajiSecret1, err := testutils.GetFakeKamajiClusterSecretFromEnvtest(dpuCluster1, cfg)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(testClient.Create(ctx, kamajiSecret1)).To(Succeed())
	g.Expect(testClient.Create(ctx, &dpuCluster1)).To(Succeed())
	// Mark DPUCluster1 as Ready
	dpuCluster1.Status.Phase = provisioningv1.PhaseReady
	g.Expect(testClient.Status().Update(ctx, &dpuCluster1)).To(Succeed())

	dpuCluster2 := testutils.GetTestDPUCluster(testNS.Name, "cluster-2")
	kamajiSecret2, err := testutils.GetFakeKamajiClusterSecretFromEnvtest(dpuCluster2, cfg)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(testClient.Create(ctx, kamajiSecret2)).To(Succeed())
	g.Expect(testClient.Create(ctx, &dpuCluster2)).To(Succeed())
	// Mark DPUCluster2 as Ready
	dpuCluster2.Status.Phase = provisioningv1.PhaseReady
	g.Expect(testClient.Status().Update(ctx, &dpuCluster2)).To(Succeed())

	config := &operatorv1.DPFOperatorConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "config",
			Namespace: testNS.Name,
		},
		Spec: operatorv1.DPFOperatorConfigSpec{
			DeploymentMode:   operatorv1.DeploymentModeHostTrusted,
			ImagePullSecrets: initialImagePullSecrets,
			ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{
				BFBPersistentVolumeClaimName: ptr.To("foo-pvc"),
			},
		},
	}

	g.Expect(testClient.Create(ctx, config)).To(Succeed())

	t.Run("Reconcile Secrets and system components", func(t *testing.T) {
		// Expect the DPUService and Provisioning controller managers to be deployed.
		waitForDeployment(g, config.Namespace, "dpuservice-controller-manager")
		waitForDeployment(g, config.Namespace, "dpf-provisioning-controller-manager")

		// Check the system components deployed as DPUServices are created as expected.
		// For ServiceChainSetController with 2 DPU clusters, we should have:
		// - 1 RBAC/CRDs DPUService
		// - 2 per-cluster DPUServices
		// Verify the RBAC/CRDs DPUService exists
		waitForDPUService(g, config.Namespace, operatorv1.ServiceSetControllerName, operatorv1.ServiceChainSetCRDsName.String(), initialImagePullSecrets)

		// Compute per-cluster DPUService names using the digest function
		hash1 := digest.Short(digest.FromObjects(dpuCluster1.Name, dpuCluster1.Namespace), 10)
		perClusterServiceSetControllerDPUServiceName1 := fmt.Sprintf("%s-%s", operatorv1.ServiceSetControllerName.String(), hash1)
		waitForDPUService(g, config.Namespace, operatorv1.ServiceSetControllerName, perClusterServiceSetControllerDPUServiceName1, initialImagePullSecrets)

		hash2 := digest.Short(digest.FromObjects(dpuCluster2.Name, dpuCluster2.Namespace), 10)
		perClusterServiceSetControllerDPUServiceName2 := fmt.Sprintf("%s-%s", operatorv1.ServiceSetControllerName.String(), hash2)
		waitForDPUService(g, config.Namespace, operatorv1.ServiceSetControllerName, perClusterServiceSetControllerDPUServiceName2, initialImagePullSecrets)

		// For NVIPAM with 2 DPU clusters, we should have:
		// - 1 RBAC/CRDs/Node component DPUService
		// - 2 per-cluster DPUServices
		// Verify the RBAC/CRDs DPUService exists
		waitForDPUService(g, config.Namespace, operatorv1.NVIPAMControllerName, operatorv1.NVIPAMNodeName.String(), initialImagePullSecrets)

		// Compute per-cluster DPUService names using the digest function
		perClusterNVIPAMDPUServiceName1 := fmt.Sprintf("%s-%s", operatorv1.NVIPAMControllerName.String(), hash1)
		waitForDPUService(g, config.Namespace, operatorv1.NVIPAMControllerName, perClusterNVIPAMDPUServiceName1, initialImagePullSecrets)

		perClusterNVIPAMDPUServiceName2 := fmt.Sprintf("%s-%s", operatorv1.NVIPAMControllerName.String(), hash2)
		waitForDPUService(g, config.Namespace, operatorv1.NVIPAMControllerName, perClusterNVIPAMDPUServiceName2, initialImagePullSecrets)

		// Check other system DPUServices
		waitForDPUService(g, config.Namespace, operatorv1.MultusName, operatorv1.MultusName.String(), initialImagePullSecrets)
		waitForDPUService(g, config.Namespace, operatorv1.SRIOVDevicePluginName, operatorv1.SRIOVDevicePluginName.String(), initialImagePullSecrets)
		waitForDPUService(g, config.Namespace, operatorv1.FlannelName, operatorv1.FlannelName.String(), initialImagePullSecrets)
		waitForDPUService(g, config.Namespace, operatorv1.SFCControllerName, operatorv1.SFCControllerName.String(), initialImagePullSecrets)
	})

	t.Run("Reconcile ArgoCD AppProjects and cluster secrets", func(t *testing.T) {
		// Since no ArgoCDNamespace override is set, ArgoCD objects land in config.Namespace.
		argoCDNamespace := testNS.Name

		// --- AppProjects ---
		// DPU AppProject must exist with destinations for both clusters.
		dpuProject := &argov1.AppProject{}
		g.Eventually(func(g Gomega) {
			g.Expect(testClient.Get(ctx, client.ObjectKey{
				Namespace: argoCDNamespace,
				Name:      argocdpkg.AppProjectNameDPU,
			}, dpuProject)).To(Succeed())
			destinationNames := make([]string, len(dpuProject.Spec.Destinations))
			for i, d := range dpuProject.Spec.Destinations {
				destinationNames[i] = d.Name
			}
			g.Expect(destinationNames).To(ConsistOf(dpuCluster1.Name, dpuCluster2.Name))
		}).WithTimeout(30 * time.Second).Should(Succeed())

		// Host AppProject must exist with the in-cluster destination.
		hostProject := &argov1.AppProject{}
		g.Eventually(func(g Gomega) {
			g.Expect(testClient.Get(ctx, client.ObjectKey{
				Namespace: argoCDNamespace,
				Name:      argocdpkg.AppProjectNameHost,
			}, hostProject)).To(Succeed())
			g.Expect(hostProject.Spec.Destinations).To(HaveLen(1))
			g.Expect(hostProject.Spec.Destinations[0].Name).To(Equal("in-cluster"))
		}).WithTimeout(30 * time.Second).Should(Succeed())

		// --- ArgoCD cluster secrets ---
		// One secret per ready DPUCluster, identified by the argocd secret-type label.
		secretList := &corev1.SecretList{}
		g.Eventually(func(g Gomega) {
			g.Expect(testClient.List(ctx, secretList,
				client.InNamespace(argoCDNamespace),
				client.MatchingLabels{argocdpkg.ArgoCDSecretLabelKey: argocdpkg.ArgoCDSecretLabelValue},
			)).To(Succeed())
			g.Expect(secretList.Items).To(HaveLen(2))
			secretNames := make([]string, len(secretList.Items))
			for i, s := range secretList.Items {
				secretNames[i] = s.Name
			}
			// Secrets are named "<namespace>-<clustername>"
			g.Expect(secretNames).To(ConsistOf(
				fmt.Sprintf("%s-%s", dpuCluster1.Namespace, dpuCluster1.Name),
				fmt.Sprintf("%s-%s", dpuCluster2.Namespace, dpuCluster2.Name),
			))
			for _, s := range secretList.Items {
				g.Expect(s.Data).To(HaveKey("name"))
				g.Expect(s.Data).To(HaveKey("server"))
				g.Expect(s.Data).To(HaveKey("config"))
			}
		}).WithTimeout(30 * time.Second).Should(Succeed())
	})

	t.Run("Delete Operator config", func(t *testing.T) {
		g.Expect(testClient.Delete(ctx, config)).To(Succeed())
		g.Eventually(func(g Gomega) {
			g.Expect(apierrors.IsNotFound(testClient.Get(ctx, client.ObjectKeyFromObject(config), config))).To(BeTrue())
		}).WithTimeout(60 * time.Second).Should(Succeed())
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

func waitForDPUService(g Gomega, ns string, componentName operatorv1.ComponentName, dpuServiceName string, imagePullSecrets []string) *dpuservicev1.DPUService {
	dpuservice := &dpuservicev1.DPUService{}
	g.Eventually(func(g Gomega) {
		g.Expect(testClient.Get(ctx, client.ObjectKey{
			Namespace: ns,
			Name:      dpuServiceName,
		}, dpuservice)).To(Succeed())
		var result map[string]interface{}
		g.Expect(json.Unmarshal(dpuservice.Spec.HelmChart.Values.Raw, &result)).To(Succeed())

		// Each system DPUService should have specific values under values.$COMPONENT_NAME
		serviceValues, ok := result[componentName.String()].(map[string]interface{})
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

func waitForDPUServiceNAD(g Gomega, ns, name string) *dpuservicev1.DPUServiceNAD {
	dpuServiceNAD := &dpuservicev1.DPUServiceNAD{}
	g.Eventually(func(g Gomega) {
		g.Expect(testClient.Get(ctx, client.ObjectKey{
			Namespace: ns,
			Name:      name},
			dpuServiceNAD)).To(Succeed())
	}).WithTimeout(30 * time.Second).Should(Succeed())
	return dpuServiceNAD
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

func TestDPFOperatorConfigValidation(t *testing.T) {
	g := NewWithT(t)
	testNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "testns-"}}
	// Create the namespace for the test.
	g.Expect(testClient.Create(ctx, testNS)).To(Succeed())

	tests := []struct {
		name    string
		wantErr bool
		input   *operatorv1.DPFOperatorConfig
	}{
		{
			name:    "spec provisioningController.BFBPersistentVolumeClaimName is always required",
			wantErr: true,
			input: &operatorv1.DPFOperatorConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "config",
					Namespace: testNS.Name,
				},
			},
		},
		{
			name:    "spec flannel.Images.KubeFlannel is required if spec flannel.Images is set",
			wantErr: true,
			input: &operatorv1.DPFOperatorConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "config",
					Namespace: testNS.Name,
				},
				Spec: operatorv1.DPFOperatorConfigSpec{
					DeploymentMode: operatorv1.DeploymentModeHostTrusted,
					ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{
						BFBPersistentVolumeClaimName: ptr.To("something"),
					},
					Flannel: &operatorv1.FlannelConfiguration{
						Images: &operatorv1.FlannelImages{
							FlannelCNI: "something",
							//KubeFlannel is missing
						},
					},
				},
			},
		},
		{
			name:    "spec flannel.Images.FlannelCNI is required if spec flannel.Images is set",
			wantErr: true,
			input: &operatorv1.DPFOperatorConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "config",
					Namespace: testNS.Name,
				},
				Spec: operatorv1.DPFOperatorConfigSpec{
					DeploymentMode: operatorv1.DeploymentModeHostTrusted,
					ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{
						BFBPersistentVolumeClaimName: ptr.To("something"),
					},
					Flannel: &operatorv1.FlannelConfiguration{
						Images: &operatorv1.FlannelImages{
							KubeFlannel: "something",
							//FlannelCNI is missing
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := testClient.Create(ctx, tt.input)
			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
			} else {
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(testutils.CleanupAndWait(ctx, testClient, tt.input)).To(Succeed())
			}
		})
	}
}

func TestApplySetCreationUpgradeDeletion(t *testing.T) {
	g := NewWithT(t)
	ns := "test-generateandpatchobjects"
	testComponentName := operatorv1.ComponentName("test-component")
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

func TestDPFOperatorConfigReconciler_ReconcilePreUpgradeValidations(t *testing.T) {
	g := NewWithT(t)

	testNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "testns-"}}
	g.Expect(testClient.Create(ctx, testNS)).To(Succeed())

	// Create a proper inventory and defaults for the reconciler
	mockInventory := inventory.New()
	g.Expect(mockInventory.ParseAll()).To(Succeed())
	mockDefaults := &release.Defaults{}
	g.Expect(mockDefaults.Parse()).To(Succeed())

	newReconciler := func() *DPFOperatorConfigReconciler {
		return &DPFOperatorConfigReconciler{
			Client:         testClient,
			UncachedClient: testClient,
			Scheme:         scheme.Scheme,
			Inventory:      mockInventory,
			Defaults:       mockDefaults,
			Settings: &DPFOperatorConfigReconcilerSettings{
				SkipWebhook: true,
			},
		}
	}

	t.Run("returns nil for new config", func(t *testing.T) {
		config := &operatorv1.DPFOperatorConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "config-new",
				Namespace: testNS.Name,
			},
			Spec: operatorv1.DPFOperatorConfigSpec{
				DeploymentMode: operatorv1.DeploymentModeHostTrusted,
				ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{
					BFBPersistentVolumeClaimName: ptr.To("test-pvc"),
				},
			},
			Status: operatorv1.DPFOperatorConfigStatus{
				ObservedGeneration: 0, // New config has ObservedGeneration of 0
			},
		}

		r := newReconciler()
		err := r.reconcilePreUpgradeValidations(ctx, config, []*dpucluster.Config{})
		g.Expect(err).ToNot(HaveOccurred())
	})

	t.Run("returns nil when no upgrade is in progress", func(t *testing.T) {
		currentVersion := release.DPFVersion()
		config := &operatorv1.DPFOperatorConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "config-no-upgrade",
				Namespace: testNS.Name,
			},
			Spec: operatorv1.DPFOperatorConfigSpec{
				DeploymentMode: operatorv1.DeploymentModeHostTrusted,
				ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{
					BFBPersistentVolumeClaimName: ptr.To("test-pvc"),
				},
			},
			Status: operatorv1.DPFOperatorConfigStatus{
				Version:            &currentVersion, // Same version as current
				ObservedGeneration: 1,               // Not a new config
			},
		}

		r := newReconciler()
		err := r.reconcilePreUpgradeValidations(ctx, config, []*dpucluster.Config{})
		g.Expect(err).ToNot(HaveOccurred())
	})

	t.Run("returns error for invalid DPF version", func(t *testing.T) {
		invalidVersion := "invalid-version"
		config := &operatorv1.DPFOperatorConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "config-invalid-version",
				Namespace: testNS.Name,
			},
			Spec: operatorv1.DPFOperatorConfigSpec{
				DeploymentMode: operatorv1.DeploymentModeHostTrusted,
				ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{
					BFBPersistentVolumeClaimName: ptr.To("test-pvc"),
				},
			},
			Status: operatorv1.DPFOperatorConfigStatus{
				Version:            &invalidVersion, // Invalid version to trigger upgrade validation
				ObservedGeneration: 1,
			},
		}

		r := newReconciler()
		err := r.reconcilePreUpgradeValidations(ctx, config, []*dpucluster.Config{})
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("DPF version validation"))
		g.Expect(err.Error()).To(ContainSubstring("invalid version"))
	})

	t.Run("succeeds with valid DPUs during upgrade", func(t *testing.T) {
		config := &operatorv1.DPFOperatorConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "config-upgrade-valid",
				Namespace: testNS.Name,
			},
			Spec: operatorv1.DPFOperatorConfigSpec{
				DeploymentMode: operatorv1.DeploymentModeHostTrusted,
				ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{
					BFBPersistentVolumeClaimName: ptr.To("test-pvc"),
				},
			},
			Status: operatorv1.DPFOperatorConfigStatus{
				Version:            ptr.To("v25.7.10"), // Same as current, so no upgrade validation
				ObservedGeneration: 2,
			},
		}

		r := newReconciler()
		err := r.reconcilePreUpgradeValidations(ctx, config, []*dpucluster.Config{})
		// System Components are not installed, so we expect an error.
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).NotTo(ContainSubstring("invalid version v25.7.10"))
	})

	t.Run("formats multiple validation errors correctly", func(t *testing.T) {
		config := &operatorv1.DPFOperatorConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "config-multiple-errors",
				Namespace: testNS.Name,
			},
			Spec: operatorv1.DPFOperatorConfigSpec{
				DeploymentMode: operatorv1.DeploymentModeHostTrusted,
				ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{
					BFBPersistentVolumeClaimName: ptr.To("test-pvc"),
				},
			},
			Status: operatorv1.DPFOperatorConfigStatus{
				Version:            ptr.To(release.LastReleasedDPFGAVersion),
				ObservedGeneration: 1,
			},
		}

		// Create multiple DPUs that are not in a terminal state so DPU state validation reports both.
		dpu1 := &provisioningv1.DPU{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "dpu-error-1",
				Namespace: testNS.Name,
			},
			Spec: provisioningv1.DPUSpec{
				SerialNumber:  "MT25066004C7",
				DPUNodeName:   "test-node-1",
				DPUDeviceName: "test-device-1",
				BFB:           ptr.To("test-bfb"),
				DPUFlavor:     "test-flavor",
				NodeEffect: provisioningv1.NodeEffect{
					Action: provisioningv1.Action{NoEffect: ptr.To(true)},
				},
			},
		}

		dpu2 := &provisioningv1.DPU{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "dpu-error-2",
				Namespace: testNS.Name,
			},
			Spec: provisioningv1.DPUSpec{
				SerialNumber:  "MT25066004C7",
				DPUNodeName:   "test-node-2",
				DPUDeviceName: "test-device-2",
				BFB:           ptr.To("test-bfb"),
				DPUFlavor:     "test-flavor",
				NodeEffect: provisioningv1.NodeEffect{
					Action: provisioningv1.Action{NoEffect: ptr.To(true)},
				},
			},
		}

		g.Expect(testClient.Create(ctx, dpu1)).To(Succeed())
		g.Expect(testClient.Create(ctx, dpu2)).To(Succeed())

		// Update the status separately using the status subresource
		dpu1.Status = provisioningv1.DPUStatus{
			Phase:      provisioningv1.DPUInitializing,
			DPFVersion: ptr.To("invalid-version-1"),
			Conditions: []metav1.Condition{
				{
					Type:               string(conditions.TypeReady),
					Status:             metav1.ConditionFalse,
					Reason:             "NotReady",
					Message:            "DPU is not ready",
					LastTransitionTime: metav1.NewTime(time.Now()),
				},
			},
		}
		g.Expect(testClient.Status().Update(ctx, dpu1)).To(Succeed())

		dpu2.Status = provisioningv1.DPUStatus{
			Phase:      provisioningv1.DPUInitializing,
			DPFVersion: ptr.To("invalid-version-2"),
			Conditions: []metav1.Condition{
				{
					Type:               string(conditions.TypeReady),
					Status:             metav1.ConditionFalse,
					Reason:             "NotReady",
					Message:            "DPU is not ready",
					LastTransitionTime: metav1.NewTime(time.Now()),
				},
			},
		}
		g.Expect(testClient.Status().Update(ctx, dpu2)).To(Succeed())

		r := newReconciler()
		err := r.reconcilePreUpgradeValidations(ctx, config, []*dpucluster.Config{})
		g.Expect(err).To(HaveOccurred())

		// Check that both DPU state errors are included in the aggregated validation message.

		// Use case-insensitive matching since semver error message format may vary between semver versions
		errMsg := strings.ToLower(err.Error())
		g.Expect(errMsg).To(ContainSubstring("dpu dpu-error-1 is not ready"))
		g.Expect(errMsg).To(ContainSubstring("dpu dpu-error-2 is not ready"))
		g.Expect(errMsg).To(ContainSubstring("dpu state"))
		g.Expect(errMsg).NotTo(ContainSubstring("dpf version validation:"))

		// Clean up the DPUs
		g.Expect(testClient.Delete(ctx, dpu1)).To(Succeed())
		g.Expect(testClient.Delete(ctx, dpu2)).To(Succeed())
	})

	t.Run("simulate simple update and verify it passes", func(t *testing.T) {
		// Use a valid but different version to trigger upgrade validation
		config := &operatorv1.DPFOperatorConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "upgrade-test",
				Namespace: testNS.Name,
			},
			Spec: operatorv1.DPFOperatorConfigSpec{
				DeploymentMode: operatorv1.DeploymentModeHostTrusted,
				ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{
					BFBPersistentVolumeClaimName: ptr.To("test-pvc"),
					BaseComponentConfig: operatorv1.BaseComponentConfig{
						Disable: ptr.To(true), // Disable the controller to avoid complexity
					},
				},
				// Enable one system component to test dynamic validation
				Multus: &operatorv1.MultusConfiguration{
					BaseComponentConfig: operatorv1.BaseComponentConfig{Disable: ptr.To(false)}},
				// Disable other system components to avoid complexity
				Monitoring: &operatorv1.MonitoringConfiguration{Disable: ptr.To(true)},
				ServiceSetController: &operatorv1.ServiceSetControllerConfiguration{
					BaseComponentConfig: operatorv1.BaseComponentConfig{Disable: ptr.To(true)}},
				SRIOVDevicePlugin: &operatorv1.SRIOVDevicePluginConfiguration{
					BaseComponentConfig: operatorv1.BaseComponentConfig{Disable: ptr.To(true)}},
				Flannel: &operatorv1.FlannelConfiguration{
					BaseComponentConfig: operatorv1.BaseComponentConfig{Disable: ptr.To(true)}},
				NVIPAM: &operatorv1.NVIPAMConfiguration{
					BaseComponentConfig: operatorv1.BaseComponentConfig{Disable: ptr.To(true)}},
				SFCController: &operatorv1.SFCControllerConfiguration{
					BaseComponentConfig: operatorv1.BaseComponentConfig{Disable: ptr.To(true)}},
				DPUServiceController: &operatorv1.DPUServiceControllerConfiguration{
					BaseComponentConfig: operatorv1.BaseComponentConfig{Disable: ptr.To(true)}},
				DPUDetector: &operatorv1.DPUDetectorConfiguration{
					BaseComponentConfig: operatorv1.BaseComponentConfig{Disable: ptr.To(true)}},
				KamajiClusterManager: &operatorv1.KamajiClusterManagerConfiguration{
					BaseComponentConfig: operatorv1.BaseComponentConfig{Disable: ptr.To(true)}},
				StaticClusterManager: &operatorv1.StaticClusterManagerConfiguration{
					BaseComponentConfig: operatorv1.BaseComponentConfig{Disable: ptr.To(true)}},
				CNIInstaller: &operatorv1.CNIInstallerConfiguration{
					BaseComponentConfig: operatorv1.BaseComponentConfig{Disable: ptr.To(true)}},
			},
		}
		g.Expect(testClient.Create(ctx, config)).To(Succeed())

		patcher := patch.NewSerialPatcher(config, testClient)
		config.Status.Version = ptr.To(release.DPFVersion())
		config.Status.ObservedGeneration = 1
		g.Expect(patcher.Patch(ctx, config, patch.WithFieldOwner("test"))).To(Succeed())

		dpuService := &dpuservicev1.DPUService{}
		dpuService.SetName(operatorv1.MultusName.String())
		dpuService.SetNamespace(testNS.Name)
		g.Eventually(func(g Gomega) {
			g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuService), dpuService)).To(Succeed())
			g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(config), config)).To(Succeed())
			upgradeCondition := conditions.Get(config, operatorv1.PreUpgradeValidationReadyCondition)
			g.Expect(upgradeCondition).ToNot(BeNil())
			g.Expect(upgradeCondition.Status).To(Equal(metav1.ConditionTrue))
		}).WithTimeout(10 * time.Second).WithPolling(time.Second).Should(Succeed())

		g.Consistently(func(g Gomega) {
			g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(config), config)).To(Succeed())
			upgradeCondition := conditions.Get(config, operatorv1.PreUpgradeValidationReadyCondition)
			g.Expect(upgradeCondition).ToNot(BeNil())
			g.Expect(upgradeCondition.Status).To(Equal(metav1.ConditionTrue))
		}).WithTimeout(10 * time.Second).WithPolling(time.Second).Should(Succeed())

		patcher = patch.NewSerialPatcher(config, testClient)
		config.Status.Version = ptr.To(release.LastReleasedDPFGAVersion)
		g.Expect(patcher.Patch(ctx, config, patch.WithFieldOwner("test"))).To(Succeed())

		g.Eventually(func(g Gomega) {
			g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(config), config)).To(Succeed())
			upgradeCondition := conditions.Get(config, operatorv1.PreUpgradeValidationReadyCondition)
			g.Expect(upgradeCondition).ToNot(BeNil())
			g.Expect(upgradeCondition.Status).To(Equal(metav1.ConditionFalse))
			g.Expect(upgradeCondition.Reason).To(Equal(string(conditions.ReasonError)))
			g.Expect(upgradeCondition.Message).To(ContainSubstring("Validation must pass for DPF upgrade to continue"))
			g.Expect(upgradeCondition.Message).To(ContainSubstring(fmt.Sprintf("DPUService %s/%s is not ready", dpuService.GetNamespace(), dpuService.GetName())))
		}).WithTimeout(5 * time.Second).WithPolling(time.Second).Should(Succeed())

		// Update the DPU service to ready status
		g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuService), dpuService)).To(Succeed())
		patcher = patch.NewSerialPatcher(dpuService, testClient)
		conditions.AddTrue(dpuService, conditions.TypeReady)
		g.Expect(patcher.Patch(ctx, dpuService, patch.WithFieldOwner("test"), patch.WithStatusObservedGeneration{})).To(Succeed())

		g.Eventually(func(g Gomega) {
			g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(config), config)).To(Succeed())
			upgradeCondition := conditions.Get(config, operatorv1.PreUpgradeValidationReadyCondition)
			g.Expect(upgradeCondition).ToNot(BeNil())
			g.Expect(upgradeCondition.Status).To(Equal(metav1.ConditionTrue))
		}).WithTimeout(10 * time.Second).WithPolling(time.Second).Should(Succeed())

		g.Expect(testutils.CleanupAndWait(ctx, testClient, config)).To(Succeed())
	})
}

func TestValidateKubernetesVersionSkew(t *testing.T) {
	g := NewWithT(t)

	testNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "testns-"}}
	g.Expect(testClient.Create(ctx, testNS)).To(Succeed())

	mockInventory := inventory.New()
	g.Expect(mockInventory.ParseAll()).To(Succeed())

	newReconciler := func() *DPFOperatorConfigReconciler {
		return &DPFOperatorConfigReconciler{
			Client:    testClient,
			Scheme:    scheme.Scheme,
			Inventory: mockInventory,
		}
	}

	// currentDPFVersion is newer than the legacy v25.10.x releases, simulating
	// a DPU provisioned by the current operator that supports KubeletVersion reporting.
	currentDPFVersion := "v26.4.0"

	// Helper to create a DPU with required fields.
	// Sets DPFVersion to currentDPFVersion so the DPU is expected to report KubeletVersion.
	createDPU := func(name string, cluster *provisioningv1.DPUCluster, kubeletVersion *string) *provisioningv1.DPU {
		dpu := &provisioningv1.DPU{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: testNS.Name,
			},
			Spec: provisioningv1.DPUSpec{
				SerialNumber:  "MT25066004C7",
				DPUNodeName:   "test-node",
				DPUDeviceName: "test-device",
				BFB:           ptr.To("test-bfb"),
				DPUFlavor:     "test-flavor",
				Cluster: provisioningv1.K8sCluster{
					Name:      cluster.Name,
					Namespace: cluster.Namespace,
				},
				NodeEffect: provisioningv1.NodeEffect{
					Action: provisioningv1.Action{NoEffect: ptr.To(true)},
				},
			},
		}
		g.Expect(testClient.Create(ctx, dpu)).To(Succeed())

		patcher := patch.NewSerialPatcher(dpu, testClient)
		dpu.Status = provisioningv1.DPUStatus{
			Phase:      provisioningv1.DPUReady,
			DPFVersion: ptr.To(currentDPFVersion),
			AgentStatus: &provisioningv1.AgentStatus{
				KubeletVersion: kubeletVersion,
			},
		}
		g.Expect(patcher.Patch(ctx, dpu, patch.WithFieldOwner("test"))).To(Succeed())

		return dpu
	}

	t.Run("validates static clusters against their actual kube-apiserver version", func(t *testing.T) {
		staticCluster := &provisioningv1.DPUCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "static-cluster-versioned",
				Namespace: testNS.Name,
			},
			Spec: provisioningv1.DPUClusterSpec{
				Type: string(provisioningv1.StaticCluster),
			},
		}
		g.Expect(testClient.Create(ctx, staticCluster)).To(Succeed())
		// Set the actual kube-apiserver version on the static cluster
		patcher := patch.NewSerialPatcher(staticCluster, testClient)
		staticCluster.Status.Version = "v1.30.0"
		staticCluster.Status.Phase = provisioningv1.PhaseReady
		g.Expect(patcher.Patch(ctx, staticCluster)).To(Succeed())

		// Create a DPU with kubelet v1.30.0 — compatible with static cluster's v1.30.0
		dpuOk := createDPU("dpu-static-ok", staticCluster, ptr.To("v1.30.0"))
		// Create a DPU with kubelet v1.26.0 — 4 minor versions behind v1.30.0, too old
		dpuOld := createDPU("dpu-static-old", staticCluster, ptr.To("v1.26.0"))

		dpuClusters := []*dpucluster.Config{
			dpucluster.NewConfig(testClient, staticCluster),
		}

		r := newReconciler()
		err := r.validateKubernetesVersionSkew(ctx, &operatorv1.DPFOperatorConfig{}, dpuClusters)
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("dpu-static-old"))
		g.Expect(err.Error()).To(ContainSubstring("more than 3 minor versions behind"))
		g.Expect(err.Error()).To(ContainSubstring("v1.30.0"))
		g.Expect(err.Error()).NotTo(ContainSubstring("dpu-static-ok"))

		g.Expect(testClient.Delete(ctx, dpuOk)).To(Succeed())
		g.Expect(testClient.Delete(ctx, dpuOld)).To(Succeed())
		g.Expect(testClient.Delete(ctx, staticCluster)).To(Succeed())
	})

	t.Run("passes validation when no DPUs are assigned to kamaji cluster", func(t *testing.T) {
		// Create a kamaji cluster without any DPUs
		kamajiCluster := &provisioningv1.DPUCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "kamaji-cluster-empty",
				Namespace: testNS.Name,
			},
			Spec: provisioningv1.DPUClusterSpec{
				Type: string(provisioningv1.KamajiCluster),
			},
		}
		g.Expect(testClient.Create(ctx, kamajiCluster)).To(Succeed())

		dpuClusters := []*dpucluster.Config{
			dpucluster.NewConfig(testClient, kamajiCluster),
		}

		r := newReconciler()
		err := r.validateKubernetesVersionSkew(ctx, &operatorv1.DPFOperatorConfig{}, dpuClusters)
		g.Expect(err).ToNot(HaveOccurred())

		g.Expect(testClient.Delete(ctx, kamajiCluster)).To(Succeed())
	})

	t.Run("passes validation when DPUs have compatible kubelet versions", func(t *testing.T) {
		// Create a kamaji cluster
		kamajiCluster := &provisioningv1.DPUCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "kamaji-cluster-compatible",
				Namespace: testNS.Name,
			},
			Spec: provisioningv1.DPUClusterSpec{
				Type: string(provisioningv1.KamajiCluster),
			},
		}
		g.Expect(testClient.Create(ctx, kamajiCluster)).To(Succeed())

		semV, err := semver.NewVersion(util.KubernetesVersion)
		g.Expect(err).ToNot(HaveOccurred())
		sameVersion := fmt.Sprintf("%d.%d.0", semV.Major(), semV.Minor())
		vMinusOne := fmt.Sprintf("%d.%d", semV.Major(), semV.Minor()-1)
		vMinusTwo := fmt.Sprintf("%d.%d", semV.Major(), semV.Minor()-2)
		vMinusThree := fmt.Sprintf("%d.%d", semV.Major(), semV.Minor()-3)

		// Create DPUs with compatible kubelet versions (within 3 minor versions)
		dpu1 := createDPU("dpu-compatible-1", kamajiCluster, &sameVersion)
		dpu2 := createDPU("dpu-compatible-2", kamajiCluster, &vMinusOne)
		dpu3 := createDPU("dpu-compatible-3", kamajiCluster, &vMinusTwo)
		dpu4 := createDPU("dpu-compatible-4", kamajiCluster, &vMinusThree)

		dpuClusters := []*dpucluster.Config{
			dpucluster.NewConfig(testClient, kamajiCluster),
		}

		r := newReconciler()
		err = r.validateKubernetesVersionSkew(ctx, &operatorv1.DPFOperatorConfig{}, dpuClusters)
		g.Expect(err).ToNot(HaveOccurred())

		vMinusFour := fmt.Sprintf("%d.%d", semV.Major(), semV.Minor()-4)
		dpu5 := createDPU("dpu-incompatible", kamajiCluster, &vMinusFour)

		err = r.validateKubernetesVersionSkew(ctx, &operatorv1.DPFOperatorConfig{}, dpuClusters)
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("/dpu-incompatible: kubelet version"))
		g.Expect(err.Error()).To(ContainSubstring("is more than 3 minor versions behind kube-apiserver version"))
		g.Expect(testClient.Delete(ctx, dpu5)).To(Succeed())

		vPlusOne := fmt.Sprintf("%d.%d", semV.Major(), semV.Minor()+1)
		dpu6 := createDPU("dpu-incompatible-too-new", kamajiCluster, &vPlusOne)
		err = r.validateKubernetesVersionSkew(ctx, &operatorv1.DPFOperatorConfig{}, dpuClusters)
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("/dpu-incompatible-too-new: kubelet version"))
		g.Expect(err.Error()).To(ContainSubstring("cannot be newer than kube-apiserver version"))

		// Cleanup
		g.Expect(testClient.Delete(ctx, dpu1)).To(Succeed())
		g.Expect(testClient.Delete(ctx, dpu2)).To(Succeed())
		g.Expect(testClient.Delete(ctx, dpu3)).To(Succeed())
		g.Expect(testClient.Delete(ctx, dpu4)).To(Succeed())
		g.Expect(testClient.Delete(ctx, dpu6)).To(Succeed())
		g.Expect(testClient.Delete(ctx, kamajiCluster)).To(Succeed())
	})

	t.Run("fails validation when DPU kubelet has different major version", func(t *testing.T) {
		// Create a kamaji cluster
		kamajiCluster := &provisioningv1.DPUCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "kamaji-cluster-major-mismatch",
				Namespace: testNS.Name,
			},
			Spec: provisioningv1.DPUClusterSpec{
				Type: string(provisioningv1.KamajiCluster),
			},
		}
		g.Expect(testClient.Create(ctx, kamajiCluster)).To(Succeed())

		dpu := createDPU("dpu-major-mismatch", kamajiCluster, ptr.To("v2.0.0"))

		dpuClusters := []*dpucluster.Config{
			dpucluster.NewConfig(testClient, kamajiCluster),
		}

		r := newReconciler()
		err := r.validateKubernetesVersionSkew(ctx, &operatorv1.DPFOperatorConfig{}, dpuClusters)
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("major version"))
		g.Expect(err.Error()).To(ContainSubstring("must match"))

		// Cleanup
		g.Expect(testClient.Delete(ctx, dpu)).To(Succeed())
		g.Expect(testClient.Delete(ctx, kamajiCluster)).To(Succeed())
	})

	t.Run("skips legacy DPUs without KubeletVersion (DPFVersion predates support)", func(t *testing.T) {
		// Create a kamaji cluster
		kamajiCluster := &provisioningv1.DPUCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "kamaji-cluster-legacy",
				Namespace: testNS.Name,
			},
			Spec: provisioningv1.DPUClusterSpec{
				Type: string(provisioningv1.KamajiCluster),
			},
		}
		g.Expect(testClient.Create(ctx, kamajiCluster)).To(Succeed())

		// Create DPU with old DPFVersion (predates KubeletVersion support) and no KubeletVersion
		dpuLegacy := &provisioningv1.DPU{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "dpu-legacy",
				Namespace: testNS.Name,
			},
			Spec: provisioningv1.DPUSpec{
				SerialNumber:  "MT25066004C7",
				DPUNodeName:   "test-node",
				DPUDeviceName: "test-device",
				BFB:           ptr.To("test-bfb"),
				DPUFlavor:     "test-flavor",
				Cluster: provisioningv1.K8sCluster{
					Name:      kamajiCluster.Name,
					Namespace: kamajiCluster.Namespace,
				},
				NodeEffect: provisioningv1.NodeEffect{
					Action: provisioningv1.Action{NoEffect: ptr.To(true)},
				},
			},
		}
		g.Expect(testClient.Create(ctx, dpuLegacy)).To(Succeed())
		patcher := patch.NewSerialPatcher(dpuLegacy, testClient)
		dpuLegacy.Status = provisioningv1.DPUStatus{
			Phase:      provisioningv1.DPUReady,
			DPFVersion: ptr.To(legacyDPFVersionWithoutKubeletSupport),
		}
		g.Expect(patcher.Patch(ctx, dpuLegacy)).To(Succeed())

		dpuClusters := []*dpucluster.Config{
			dpucluster.NewConfig(testClient, kamajiCluster),
		}

		r := newReconciler()
		err := r.validateKubernetesVersionSkew(ctx, &operatorv1.DPFOperatorConfig{}, dpuClusters)
		// Expect dpuNoDPFVer to fail
		g.Expect(err).ToNot(HaveOccurred())

		// Create DPU with no DPFVersion at all (very old)
		dpuNoDPFVer := &provisioningv1.DPU{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "dpu-no-dpfversion",
				Namespace: testNS.Name,
			},
			Spec: provisioningv1.DPUSpec{
				SerialNumber:  "MT25066004C8",
				DPUNodeName:   "test-node",
				DPUDeviceName: "test-device",
				BFB:           ptr.To("test-bfb"),
				DPUFlavor:     "test-flavor",
				Cluster: provisioningv1.K8sCluster{
					Name:      kamajiCluster.Name,
					Namespace: kamajiCluster.Namespace,
				},
				NodeEffect: provisioningv1.NodeEffect{
					Action: provisioningv1.Action{NoEffect: ptr.To(true)},
				},
			},
		}
		g.Expect(testClient.Create(ctx, dpuNoDPFVer)).To(Succeed())
		err = r.validateKubernetesVersionSkew(ctx, &operatorv1.DPFOperatorConfig{}, dpuClusters)
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("dpu-no-dpfversion: no KubeletVersion"))
		g.Expect(testClient.Delete(ctx, dpuNoDPFVer)).To(Succeed())

		// Create DPU with old DPFVersion (predates KubeletVersion support) and no KubeletVersion
		dpuFailedWithoutKubelet := &provisioningv1.DPU{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "dpu-failed-without-kubelet",
				Namespace: testNS.Name,
			},
			Spec: provisioningv1.DPUSpec{
				SerialNumber:  "MT25066004C7",
				DPUNodeName:   "test-node",
				DPUDeviceName: "test-device",
				BFB:           ptr.To("test-bfb"),
				DPUFlavor:     "test-flavor",
				Cluster: provisioningv1.K8sCluster{
					Name:      kamajiCluster.Name,
					Namespace: kamajiCluster.Namespace,
				},
				NodeEffect: provisioningv1.NodeEffect{
					Action: provisioningv1.Action{NoEffect: ptr.To(true)},
				},
			},
		}
		g.Expect(testClient.Create(ctx, dpuFailedWithoutKubelet)).To(Succeed())
		patcher = patch.NewSerialPatcher(dpuFailedWithoutKubelet, testClient)
		dpuFailedWithoutKubelet.Status = provisioningv1.DPUStatus{
			Phase:      provisioningv1.DPUError,
			DPFVersion: ptr.To("v26.4.0"),
		}
		g.Expect(patcher.Patch(ctx, dpuFailedWithoutKubelet)).To(Succeed())
		err = r.validateKubernetesVersionSkew(ctx, &operatorv1.DPFOperatorConfig{}, dpuClusters)
		g.Expect(err).ToNot(HaveOccurred())

		// Cleanup
		g.Expect(testClient.Delete(ctx, dpuFailedWithoutKubelet)).To(Succeed())
		g.Expect(testClient.Delete(ctx, dpuLegacy)).To(Succeed())
		g.Expect(testClient.Delete(ctx, kamajiCluster)).To(Succeed())
	})

	t.Run("errors when current DPU has no KubeletVersion reported", func(t *testing.T) {
		// Create a kamaji cluster
		kamajiCluster := &provisioningv1.DPUCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "kamaji-cluster-missing-version",
				Namespace: testNS.Name,
			},
			Spec: provisioningv1.DPUClusterSpec{
				Type: string(provisioningv1.KamajiCluster),
			},
		}
		g.Expect(testClient.Create(ctx, kamajiCluster)).To(Succeed())

		// Create DPU with current DPFVersion but no KubeletVersion — should error
		dpu := createDPU("dpu-missing-kubelet", kamajiCluster, nil)

		dpuClusters := []*dpucluster.Config{
			dpucluster.NewConfig(testClient, kamajiCluster),
		}

		r := newReconciler()
		err := r.validateKubernetesVersionSkew(ctx, &operatorv1.DPFOperatorConfig{}, dpuClusters)
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("kubelet version not reported"))
		g.Expect(err.Error()).To(ContainSubstring("should support it"))

		// Cleanup
		g.Expect(testClient.Delete(ctx, dpu)).To(Succeed())
		g.Expect(testClient.Delete(ctx, kamajiCluster)).To(Succeed())
	})

	t.Run("returns error for invalid kubelet version format", func(t *testing.T) {
		// Create a kamaji cluster
		kamajiCluster := &provisioningv1.DPUCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "kamaji-cluster-invalid-version",
				Namespace: testNS.Name,
			},
			Spec: provisioningv1.DPUClusterSpec{
				Type: string(provisioningv1.KamajiCluster),
			},
		}
		g.Expect(testClient.Create(ctx, kamajiCluster)).To(Succeed())

		// Create DPU with invalid version format
		dpu := createDPU("dpu-invalid-version", kamajiCluster, ptr.To("invalid-version"))

		dpuClusters := []*dpucluster.Config{
			dpucluster.NewConfig(testClient, kamajiCluster),
		}

		r := newReconciler()
		err := r.validateKubernetesVersionSkew(ctx, &operatorv1.DPFOperatorConfig{}, dpuClusters)
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("invalid kubelet version"))

		// Cleanup
		g.Expect(testClient.Delete(ctx, dpu)).To(Succeed())
		g.Expect(testClient.Delete(ctx, kamajiCluster)).To(Succeed())
	})

	t.Run("aggregates multiple validation errors", func(t *testing.T) {
		// Create a kamaji cluster
		kamajiCluster := &provisioningv1.DPUCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "kamaji-cluster-multi-errors",
				Namespace: testNS.Name,
			},
			Spec: provisioningv1.DPUClusterSpec{
				Type: string(provisioningv1.KamajiCluster),
			},
		}
		g.Expect(testClient.Create(ctx, kamajiCluster)).To(Succeed())

		// Create DPU with kubelet too old
		dpu1 := createDPU("dpu-multi-error-1", kamajiCluster, ptr.To("v1.30.0"))

		// Create DPU with kubelet too new
		dpu2 := createDPU("dpu-multi-error-2", kamajiCluster, ptr.To("v1.36.0"))

		dpuClusters := []*dpucluster.Config{
			dpucluster.NewConfig(testClient, kamajiCluster),
		}

		r := newReconciler()
		err := r.validateKubernetesVersionSkew(ctx, &operatorv1.DPFOperatorConfig{}, dpuClusters)
		g.Expect(err).To(HaveOccurred())
		// Both errors should be included
		g.Expect(err.Error()).To(ContainSubstring("dpu-multi-error-1"))
		g.Expect(err.Error()).To(ContainSubstring("dpu-multi-error-2"))

		// Cleanup
		g.Expect(testClient.Delete(ctx, dpu1)).To(Succeed())
		g.Expect(testClient.Delete(ctx, dpu2)).To(Succeed())
		g.Expect(testClient.Delete(ctx, kamajiCluster)).To(Succeed())
	})
}

func TestGetDPUKubeletVersion(t *testing.T) {
	tests := []struct {
		name        string
		agentStatus *provisioningv1.AgentStatus
		dpfVersion  *string
		wantVersion string
		wantErr     string
	}{
		{
			name:        "returns kubelet version when present",
			agentStatus: &provisioningv1.AgentStatus{KubeletVersion: ptr.To("v1.33.3")},
			dpfVersion:  ptr.To("v26.4.0"),
			wantVersion: "v1.33.3",
		},
		{
			name:        "errors when DPU has no AgentStatus and no DPFVersion (very old DPU)",
			agentStatus: nil,
			dpfVersion:  nil,
			wantErr:     "no KubeletVersion",
		},
		{
			name:        "skips DPU with legacy DPFVersion v25.10.1",
			agentStatus: nil,
			dpfVersion:  ptr.To(legacyDPFVersionWithoutKubeletSupport),
			wantVersion: "",
		},
		{
			name:        "skips DPU with DPFVersion v25.10.0 (same minor as supported)",
			agentStatus: nil,
			dpfVersion:  ptr.To("v25.10.0"),
			wantVersion: "",
		},
		{
			name:        "skips DPU with DPFVersion v25.10.1-rc.1 prerelease (same minor)",
			agentStatus: nil,
			dpfVersion:  ptr.To("v25.10.1-rc.1"),
			wantVersion: "",
		},
		{
			name:        "errors when DPFVersion is older than v25.10.x",
			agentStatus: nil,
			dpfVersion:  ptr.To("v25.7.0"),
			wantErr:     "unsupported DPF version",
		},
		{
			name:        "skips DPU with DPFVersion v25.10.2 (newer patch but same v25.10.x minor)",
			agentStatus: nil,
			dpfVersion:  ptr.To("v25.10.2"),
			wantVersion: "",
		},
		{
			name:        "errors when DPFVersion is newer minor than legacy kubelet support threshold",
			agentStatus: nil,
			dpfVersion:  ptr.To("v26.4.0"),
			wantErr:     "kubelet version not reported",
		},
		{
			name:        "errors when DPFVersion is unparseable",
			agentStatus: nil,
			dpfVersion:  ptr.To("not-a-version"),
			wantErr:     "failed to parse DPF version",
		},
		{
			name:        "returns kubelet version even when DPFVersion is nil",
			agentStatus: &provisioningv1.AgentStatus{KubeletVersion: ptr.To("v1.32.0")},
			dpfVersion:  nil,
			wantVersion: "v1.32.0",
		},
		{
			name:        "AgentStatus present but KubeletVersion nil with old DPFVersion",
			agentStatus: &provisioningv1.AgentStatus{KubeletVersion: nil},
			dpfVersion:  ptr.To("v25.10.0"),
			wantVersion: "",
		},
		{
			name:        "AgentStatus present but KubeletVersion nil with new DPFVersion",
			agentStatus: &provisioningv1.AgentStatus{KubeletVersion: nil},
			dpfVersion:  ptr.To("v26.4.0"),
			wantErr:     "kubelet version not reported",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dpu := &provisioningv1.DPU{
				Status: provisioningv1.DPUStatus{
					DPFVersion:  tt.dpfVersion,
					AgentStatus: tt.agentStatus,
				},
			}
			got, err := getDPUKubeletVersion(dpu)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.wantVersion {
				t.Fatalf("expected version %q, got %q", tt.wantVersion, got)
			}
		})
	}
}
