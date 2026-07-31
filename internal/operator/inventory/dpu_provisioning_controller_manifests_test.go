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

package inventory

import (
	"fmt"
	"strings"
	"testing"
	"time"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/operator/utils"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/bfcfg"
	"github.com/nvidia/doca-platform/internal/release"

	. "github.com/onsi/gomega"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
)

const (
	TestPVC = "test-pvc"
)

func TestDPFProvisioningControllerObjects_Parse(t *testing.T) {
	g := NewGomegaWithT(t)
	originalObjects, err := utils.BytesToUnstructured(provisioningControllerData)
	g.Expect(err).NotTo(HaveOccurred())

	iterate := func(op func(*unstructured.Unstructured) bool) []byte {
		ret := []*unstructured.Unstructured{}
		for _, obj := range originalObjects {
			cpy := obj.DeepCopy()
			include := op(cpy)
			if include {
				ret = append(ret, cpy)
			}
		}
		b, err := utils.UnstructuredToBytes(ret)
		g.Expect(err).NotTo(HaveOccurred())
		return b
	}

	correct := iterate(func(u *unstructured.Unstructured) bool { return true })
	missingDeployment := iterate(func(u *unstructured.Unstructured) bool {
		return u.GetKind() != string(DeploymentKind)
	})
	wrongName := iterate(func(u *unstructured.Unstructured) bool {
		if u.GetKind() == string(DeploymentKind) {
			u.SetName("wrong-name")
		}
		return true
	})
	volumeMissing := iterate(func(u *unstructured.Unstructured) bool {
		if u.GetKind() == string(DeploymentKind) {
			deploy := &appsv1.Deployment{}
			err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.UnstructuredContent(), deploy)
			g.Expect(err).NotTo(HaveOccurred())
			deploy.Spec.Template.Spec.Volumes = []corev1.Volume{
				{
					Name: "some-other-volume",
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{
							Path: "/",
						},
					},
				},
			}
			un, err := runtime.DefaultUnstructuredConverter.ToUnstructured(deploy)
			g.Expect(err).NotTo(HaveOccurred())
			*u = unstructured.Unstructured{Object: un}
		}
		return true
	})

	volumeWrongName := iterate(func(u *unstructured.Unstructured) bool {
		if u.GetKind() == string(DeploymentKind) {
			deploy := &appsv1.Deployment{}
			err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.UnstructuredContent(), deploy)
			g.Expect(err).NotTo(HaveOccurred())
			deploy.Spec.Template.Spec.Volumes = []corev1.Volume{
				{
					Name: "some-other-volume",
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{
							Path: "/",
						},
					},
				},
				{
					Name: "wrong-name",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: bfbVolumeName,
						},
					},
				},
			}
			un, err := runtime.DefaultUnstructuredConverter.ToUnstructured(deploy)
			g.Expect(err).NotTo(HaveOccurred())
			*u = unstructured.Unstructured{Object: un}
		}
		return true
	})

	tests := []struct {
		name      string
		data      []byte
		expectErr bool
	}{
		{
			name:      "should succeed",
			data:      correct,
			expectErr: false,
		},
		{
			name:      "fail if no Deployment in manifests",
			data:      missingDeployment,
			expectErr: true,
		},
		{
			name:      "fail if wrong Deployment name in manifests",
			data:      wrongName,
			expectErr: true,
		},
		{
			name:      "fail if PVC volume is missing",
			data:      volumeMissing,
			expectErr: true,
		},
		{
			name:      "fail if PVC volume has different name",
			data:      volumeWrongName,
			expectErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := provisioningControllerObjects{
				data:            provisioningControllerData,
				bfbRegistryData: bfbRegistryData,
			}
			p.data = tc.data
			if tc.expectErr {
				NewGomegaWithT(t).Expect(p.Parse()).To(HaveOccurred())
			} else {
				NewGomegaWithT(t).Expect(p.Parse()).NotTo(HaveOccurred())
			}
		})
	}
}

func TestProvisioningControllerObjects_GenerateManifests(t *testing.T) {
	g := NewWithT(t)
	originalObjs, err := utils.BytesToUnstructured(provisioningControllerData)
	g.Expect(err).NotTo(HaveOccurred())
	provCtrl := provisioningControllerObjects{
		data:            provisioningControllerData,
		bfbRegistryData: bfbRegistryData,
	}
	g.Expect(provCtrl.Parse()).NotTo(HaveOccurred())
	defaults := &release.Defaults{}
	g.Expect(defaults.Parse()).To(Succeed())

	t.Run("no objects if disable is set", func(t *testing.T) {
		vars := newDefaultVariables(defaults)
		vars.DisableSystemComponents = map[string]bool{
			provCtrl.Name(): true,
		}
		objs, err := provCtrl.GenerateManifests(vars, skipApplySetCreationOption{})
		if err != nil {
			t.Fatalf("failed to generate manifests: %v", err)
		}
		if len(objs) != 0 {
			t.Fatalf("manifests should not be generated when disabled: %v", objs)
		}
	})

	t.Run("fail if empty pvc", func(t *testing.T) {
		vars := newDefaultVariables(defaults)

		vars.DPFProvisioningController = DPFProvisioningVariables{
			BFBPersistentVolumeClaimName: " ",
		}
		_, err := provCtrl.GenerateManifests(vars, skipApplySetCreationOption{})
		NewGomegaWithT(t).Expect(err).To(HaveOccurred())
	})

	t.Run("test setting namespaces", func(t *testing.T) {
		g := NewWithT(t)
		testNS := "foop"
		vars := newDefaultVariables(defaults)

		vars.Namespace = testNS
		vars.DPFProvisioningController = DPFProvisioningVariables{
			BFBPersistentVolumeClaimName: "pvc",
		}
		objs, err := provCtrl.GenerateManifests(vars, skipApplySetCreationOption{})
		g.Expect(err).NotTo(HaveOccurred())
		for _, obj := range objs {
			// Check the cert manager annotation is updated
			annotations := obj.GetAnnotations()
			if value, ok := annotations["cert-manager.io/inject-ca-from"]; ok {
				parts := strings.Split(value, "/")
				g.Expect(parts[0]).To(Equal(testNS))
			}
			switch ObjectKind(obj.GetObjectKind().GroupVersionKind().Kind) {
			// Skip unnamespaced objects that don't have nested namespaces.
			case NamespaceKind, ClusterRoleKind, CustomResourceDefinitionKind:
				continue
			case ClusterRoleBindingKind:
				crb := &v1.ClusterRoleBinding{}
				uns, ok := obj.(*unstructured.Unstructured)
				g.Expect(ok).To(BeTrue())
				g.Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(uns.UnstructuredContent(), crb)).To(Succeed())
				for _, subject := range crb.Subjects {
					g.Expect(subject.Namespace).To(Equal(testNS))
				}
			case RoleBindingKind:
				g.Expect(obj.GetNamespace()).To(Equal(testNS))
				rb := &v1.RoleBinding{}
				uns, ok := obj.(*unstructured.Unstructured)
				g.Expect(ok).To(BeTrue())
				g.Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(uns.UnstructuredContent(), rb)).To(Succeed())
				for _, subject := range rb.Subjects {
					g.Expect(subject.Namespace).To(Equal(testNS))
				}
			case ValidatingWebhookConfigurationKind:
				vwc := &admissionregistrationv1.ValidatingWebhookConfiguration{}
				uns, ok := obj.(*unstructured.Unstructured)
				g.Expect(ok).To(BeTrue())
				g.Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(uns.UnstructuredContent(), vwc)).To(Succeed())
				g.Expect(ok).To(BeTrue())
				for _, webhook := range vwc.Webhooks {
					g.Expect(webhook.ClientConfig.Service.Namespace).To(Equal(testNS))
				}
			case MutatingWebhookConfigurationKind:
				typedObject := &admissionregistrationv1.MutatingWebhookConfiguration{}
				uns, ok := obj.(*unstructured.Unstructured)
				g.Expect(ok).To(BeTrue())
				g.Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(uns.UnstructuredContent(), typedObject)).To(Succeed())
				g.Expect(ok).To(BeTrue())
				for _, webhook := range typedObject.Webhooks {
					g.Expect(webhook.ClientConfig.Service.Namespace).To(Equal(testNS))
				}
			case CertificateKind:
				g.Expect(obj.GetNamespace()).To(Equal(testNS))
				uns, ok := obj.(*unstructured.Unstructured)
				g.Expect(ok).To(BeTrue())
				certs, ok, _ := unstructured.NestedSlice(uns.UnstructuredContent(), "spec", "dnsNames")
				g.Expect(ok).To(BeTrue())
				for i := range certs {
					s, ok := certs[i].(string)
					g.Expect(ok).To(BeTrue())
					// services take the form ${SERVICE_NAME}.${SERVICE_NAMESPACE}.${SERVICE_DOMAIN}.svc
					parts := strings.Split(s, ".")
					// Set the second part as the namespace and reset the string and field.
					g.Expect(parts[1]).To(Equal(testNS))
				}
			default:
				g.Expect(obj.GetNamespace()).To(Equal(testNS))
			}

		}
	})

	// This test is customized for the current Provisioning manifest, internal/operator/inventory/manifests/provisioning-controller.yaml.
	// These tests should be reviewed every time the manifest is updated
	t.Run("test field modification", func(t *testing.T) {
		ns := "namespace-one"
		g := NewGomegaWithT(t)
		expectedPVC := TestPVC
		expectedImagePullSecret1 := "test-image-pull-secret"
		expectedImagePullSecret2 := "test-image-pull-secret-2"
		expectedKubernetesAPIServerVIP := "192.168.1.1"
		expectedKubernetesAPIServerPort := 6443
		expectedDmsTimeout := 20
		expectedMultiDPUOperationsSyncWaitTime := "30s"
		vars := newDefaultVariables(defaults)
		vars.Namespace = ns
		vars.DPFProvisioningController = DPFProvisioningVariables{
			BFBPersistentVolumeClaimName:   expectedPVC,
			DMSTimeout:                     &expectedDmsTimeout,
			MultiDPUOperationsSyncWaitTime: 30 * time.Second,
		}
		vars.ImagePullSecrets = []string{expectedImagePullSecret1, expectedImagePullSecret2}
		vars.KubernetesAPIServerVIP = &expectedKubernetesAPIServerVIP
		vars.KubernetesAPIServerPort = &expectedKubernetesAPIServerPort
		generatedObjs, err := provCtrl.GenerateManifests(vars, skipApplySetCreationOption{})
		g.Expect(err).NotTo(HaveOccurred())

		g.Expect(generatedObjs).To(HaveLen(len(originalObjs)))

		// Expect the namespaces for the namespace scoped objects to equal the namespace in variables.
		for _, obj := range generatedObjs {
			if !isClusterScoped(obj.GetObjectKind().GroupVersionKind().Kind) {
				g.Expect(obj.GetNamespace()).To(Equal(ns), obj.GetObjectKind().GroupVersionKind().String())
			}
		}
		gotDeployment := &appsv1.Deployment{}
		for i, obj := range generatedObjs {
			if obj.GetObjectKind().GroupVersionKind().Kind == string(DeploymentKind) {
				deployment, ok := generatedObjs[i].(*unstructured.Unstructured)
				g.Expect(ok).To(BeTrue())
				g.Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(deployment.UnstructuredContent(), gotDeployment)).ToNot(HaveOccurred())
				continue
			}
			if obj.GetObjectKind().GroupVersionKind().Kind == "Service" && obj.GetName() == webhookServiceName {
				uns := obj.(*unstructured.Unstructured)
				selector, found, err := unstructured.NestedMap(uns.UnstructuredContent(), "spec", "selector")
				g.Expect(found).To(BeTrue())
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(selector[operatorv1.DPFComponentLabelKey]).To(Equal(DPFProvisioningControllerName))
			}
		}
		// * ensure deployment contains NodeAffinity
		g.Expect(*gotDeployment.Spec.Template.Spec.Affinity.NodeAffinity).To(Equal(controlPlaneNodeAffinity))

		// * ensure the component label is set
		g.Expect(gotDeployment.Spec.Template.Labels[operatorv1.DPFComponentLabelKey]).To(Equal(DPFProvisioningControllerName))
		// * ensure that the expected modifications have been made to the deployment.
		g.Expect(gotDeployment).NotTo(BeNil())
		g.Expect(gotDeployment.Spec.Template.Spec.ImagePullSecrets).To(HaveLen(2))
		g.Expect(gotDeployment.Spec.Template.Spec.ImagePullSecrets[0].Name).To(Equal(expectedImagePullSecret1))
		g.Expect(gotDeployment.Spec.Template.Spec.ImagePullSecrets[1].Name).To(Equal(expectedImagePullSecret2))
		// * check bfb pvc
		g.Expect(gotDeployment.Spec.Template.Spec.Volumes).To(HaveLen(2))
		g.Expect(gotDeployment.Spec.Template.Spec.Volumes[1].PersistentVolumeClaim).NotTo(BeNil())
		g.Expect(gotDeployment.Spec.Template.Spec.Volumes[1].PersistentVolumeClaim.ClaimName).To(Equal(expectedPVC))
		// * check args of the manager container
		var container *corev1.Container
		for _, c := range gotDeployment.Spec.Template.Spec.Containers {
			if c.Name == dpfProvisioningControllerContainerName {
				container = c.DeepCopy()
				break
			}
		}
		g.Expect(container).NotTo(BeNil())
		expectedArgs := []string{
			"--leader-elect",
			"--v=3",
			fmt.Sprintf("--dms-image=%s", defaults.DMSImage),
			fmt.Sprintf("--bfb-pvc=%s", expectedPVC),
			fmt.Sprintf("--image-pull-secrets=%s", strings.Join([]string{expectedImagePullSecret1, expectedImagePullSecret2}, ",")),
			fmt.Sprintf("--dms-timeout=%d", expectedDmsTimeout),
			fmt.Sprintf("--dpu-install-interface=%s", provisioningv1.InstallViaGNOI),
			fmt.Sprintf("--dms-pod-envs=KUBERNETES_SERVICE_HOST=%s,KUBERNETES_SERVICE_PORT=%d", expectedKubernetesAPIServerVIP, expectedKubernetesAPIServerPort),
			fmt.Sprintf("--multi-dpu-operations-sync-wait-time=%s", expectedMultiDPUOperationsSyncWaitTime),
		}
		g.Expect(gotDeployment.Spec.Template.Spec.Containers).To(HaveLen(1))
		g.Expect(gotDeployment.Spec.Template.Spec.Containers[0].Args).To(HaveLen(len(expectedArgs)))
		for i, ea := range expectedArgs {
			g.Expect(container.Args[i]).To(Equal(ea))
		}
	})
	t.Run("test adding a custom bfb cfg configmap", func(t *testing.T) {
		g := NewGomegaWithT(t)

		expectedPVC := TestPVC
		vars := newDefaultVariables(defaults)
		vars.DPFProvisioningController = DPFProvisioningVariables{
			BFBPersistentVolumeClaimName: expectedPVC,
			BFCFGTemplateConfig:          ptr.To("configmap"),
		}
		generatedObjs, err := provCtrl.GenerateManifests(vars, skipApplySetCreationOption{})
		g.Expect(err).NotTo(HaveOccurred())
		gotDeployment := &appsv1.Deployment{}
		for i, obj := range generatedObjs {
			if obj.GetObjectKind().GroupVersionKind().Kind == string(DeploymentKind) {
				deployment, ok := generatedObjs[i].(*unstructured.Unstructured)
				g.Expect(ok).To(BeTrue())
				g.Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(deployment.UnstructuredContent(), gotDeployment)).ToNot(HaveOccurred())
				continue
			}
		}
		podTemplate := gotDeployment.Spec.Template
		// * ensure that the expected modifications have been made to the deployment.
		g.Expect(gotDeployment).NotTo(BeNil())
		g.Expect(podTemplate.Spec.Containers).To(HaveLen(1))
		// * ensure the arg is added to the container
		g.Expect(podTemplate.Spec.Containers[0].Args).To(ContainElement(fmt.Sprintf("--bf-cfg-template-file=%s", "/bfb-config/bf.cfg.template")))
		// * ensure the volume is added to the pod spec
		g.Expect(podTemplate.Spec.Volumes).To(ContainElement(corev1.Volume{
			Name: customBFConfigVolumeName,
			VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: *vars.DPFProvisioningController.BFCFGTemplateConfig},
				Items: []corev1.KeyToPath{
					{
						Key:  bfcfg.ConfigMapDataKey,
						Path: customBFConfigFileName,
					},
				},
			}},
		}))

		// *ensure the volumemount is added ot the pod spec
		g.Expect(podTemplate.Spec.Containers[0].VolumeMounts).To(
			ContainElement(corev1.VolumeMount{
				Name:      customBFConfigVolumeName,
				MountPath: "/bfb-config",
			}))
	})

	t.Run("test setting resources", func(t *testing.T) {
		g := NewGomegaWithT(t)
		vars := newDefaultVariables(defaults)
		vars.DPFProvisioningController = DPFProvisioningVariables{
			BFBPersistentVolumeClaimName: "pvc",
		}

		// Set resources
		vars.Resources[provCtrl.Name()] = corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("500m"),
				corev1.ResourceMemory: resource.MustParse("512Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("500m"),
				corev1.ResourceMemory: resource.MustParse("512Mi"),
			},
		}

		objs, err := provCtrl.GenerateManifests(vars, skipApplySetCreationOption{})
		g.Expect(err).NotTo(HaveOccurred())

		for _, obj := range objs {
			if obj.GetObjectKind().GroupVersionKind().Kind != string(DeploymentKind) {
				continue
			}

			deployment := &appsv1.Deployment{}
			uns, ok := obj.(*unstructured.Unstructured)
			g.Expect(ok).To(BeTrue())
			g.Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(uns.UnstructuredContent(), deployment)).To(Succeed())

			// Check resources
			expectedResources := &corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("500m"),
					corev1.ResourceMemory: resource.MustParse("512Mi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("500m"),
					corev1.ResourceMemory: resource.MustParse("512Mi"),
				},
			}
			g.Expect(deployment.Spec.Template.Spec.Containers[0].Resources).To(Equal(*expectedResources))
		}
	})

}

func TestDPFProvisioningControllerObjects_GenerateBFBRegistryManifests(t *testing.T) {
	g := NewGomegaWithT(t)

	defaults := release.NewDefaults()
	err := defaults.Parse()
	g.Expect(err).NotTo(HaveOccurred())

	provCtrl := &provisioningControllerObjects{
		data:            provisioningControllerData,
		bfbRegistryData: bfbRegistryData,
	}
	err = provCtrl.Parse()
	g.Expect(err).NotTo(HaveOccurred())

	t.Run("test generating bfb-registry manifests with redfish install interface", func(t *testing.T) {
		g := NewGomegaWithT(t)

		expectedPVC := TestPVC
		vars := newDefaultVariables(defaults)
		vars.DPFProvisioningController = DPFProvisioningVariables{
			BFBPersistentVolumeClaimName: expectedPVC,
			InstallInterface: &operatorv1.ProvisioningInstallInterface{
				InstallViaRedfish: &operatorv1.InstallViaRedfish{
					BFBRegistryAddress: "registry-address",
					BFBRegistry: &operatorv1.BFBRegistryConfiguration{
						Port: ptr.To(9090),
					},
				},
			},
		}
		vars.Images[operatorv1.BFBRegistryName] = "registry-image:latest"

		generatedObjs, err := provCtrl.GenerateManifests(vars, skipApplySetCreationOption{})
		g.Expect(err).NotTo(HaveOccurred())

		// Find the DaemonSet for the BFB registry
		var daemonSet *appsv1.DaemonSet
		for _, obj := range generatedObjs {
			if obj.GetObjectKind().GroupVersionKind().Kind == string(DaemonSetKind) {
				ds := &appsv1.DaemonSet{}
				unstructuredObj, ok := obj.(*unstructured.Unstructured)
				g.Expect(ok).To(BeTrue())
				err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.UnstructuredContent(), ds)
				g.Expect(err).NotTo(HaveOccurred())
				daemonSet = ds
				break
			}
		}

		// Verify the DaemonSet exists and has expected properties
		g.Expect(daemonSet).NotTo(BeNil())
		g.Expect(daemonSet.Namespace).To(Equal(vars.Namespace))
		g.Expect(daemonSet.Labels).To(HaveKeyWithValue(operatorv1.DPFComponentLabelKey, operatorv1.BFBRegistryName))

		// Verify container configuration
		g.Expect(daemonSet.Spec.Template.Spec.Containers).To(HaveLen(1))
		container := daemonSet.Spec.Template.Spec.Containers[0]
		g.Expect(container.Name).To(Equal("nginx"))
		g.Expect(container.Image).To(Equal("registry-image:latest"))

		// Verify environment variables
		g.Expect(container.Env).To(ContainElement(corev1.EnvVar{
			Name:  "NGINX_PORT",
			Value: "9090",
		}))

		// Verify node affinity and tolerations
		g.Expect(daemonSet.Spec.Template.Spec.Affinity).NotTo(BeNil())
		g.Expect(daemonSet.Spec.Template.Spec.Affinity.NodeAffinity).NotTo(BeNil())
		g.Expect(daemonSet.Spec.Template.Spec.Tolerations).NotTo(BeEmpty())

		// Verify deployment has the correct flag for BFB registry
		var deployment *appsv1.Deployment
		for _, obj := range generatedObjs {
			if obj.GetObjectKind().GroupVersionKind().Kind == string(DeploymentKind) {
				deploy := &appsv1.Deployment{}
				unstructuredObj, ok := obj.(*unstructured.Unstructured)
				g.Expect(ok).To(BeTrue())
				err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.UnstructuredContent(), deploy)
				g.Expect(err).NotTo(HaveOccurred())
				deployment = deploy
				break
			}
		}

		g.Expect(deployment).NotTo(BeNil())
		g.Expect(deployment.Spec.Template.Spec.Containers).To(HaveLen(1))
		g.Expect(deployment.Spec.Template.Spec.Containers[0].Args).To(ContainElement("--dpu-install-interface=redfish"))
		g.Expect(deployment.Spec.Template.Spec.Containers[0].Args).To(ContainElement("--bfb-registry=registry-address"))
	})

	t.Run("test generating bfb-registry manifests with default port when BFBRegistry is nil", func(t *testing.T) {
		g := NewGomegaWithT(t)

		expectedPVC := TestPVC
		vars := newDefaultVariables(defaults)
		vars.DPFProvisioningController = DPFProvisioningVariables{
			BFBPersistentVolumeClaimName: expectedPVC,
			InstallInterface: &operatorv1.ProvisioningInstallInterface{
				InstallViaRedfish: &operatorv1.InstallViaRedfish{
					BFBRegistryAddress: "registry-address",
					// BFBRegistry is nil
				},
			},
		}
		vars.Images[operatorv1.BFBRegistryName] = "registry-image:latest"

		generatedObjs, err := provCtrl.GenerateManifests(vars, skipApplySetCreationOption{})
		g.Expect(err).NotTo(HaveOccurred())

		// Find the DaemonSet for the BFB registry
		var daemonSet *appsv1.DaemonSet
		for _, obj := range generatedObjs {
			if obj.GetObjectKind().GroupVersionKind().Kind == string(DaemonSetKind) {
				ds := &appsv1.DaemonSet{}
				unstructuredObj, ok := obj.(*unstructured.Unstructured)
				g.Expect(ok).To(BeTrue())
				err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.UnstructuredContent(), ds)
				g.Expect(err).NotTo(HaveOccurred())
				daemonSet = ds
				break
			}
		}

		// Verify the DaemonSet exists
		g.Expect(daemonSet).NotTo(BeNil())

		// Verify container configuration
		g.Expect(daemonSet.Spec.Template.Spec.Containers).To(HaveLen(1))
		container := daemonSet.Spec.Template.Spec.Containers[0]

		// Verify environment variables - should use default port 8080
		g.Expect(container.Env).To(ContainElement(corev1.EnvVar{
			Name:  "NGINX_PORT",
			Value: "8080",
		}))

		// Verify deployment has the correct flags
		var deployment *appsv1.Deployment
		for _, obj := range generatedObjs {
			if obj.GetObjectKind().GroupVersionKind().Kind == string(DeploymentKind) {
				deploy := &appsv1.Deployment{}
				unstructuredObj, ok := obj.(*unstructured.Unstructured)
				g.Expect(ok).To(BeTrue())
				err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.UnstructuredContent(), deploy)
				g.Expect(err).NotTo(HaveOccurred())
				deployment = deploy
				break
			}
		}

		g.Expect(deployment).NotTo(BeNil())
		g.Expect(deployment.Spec.Template.Spec.Containers[0].Args).To(ContainElement("--dpu-install-interface=redfish"))
		g.Expect(deployment.Spec.Template.Spec.Containers[0].Args).To(ContainElement("--bfb-registry=registry-address"))
	})

	t.Run("test no bfb-registry manifests with gnoi install interface", func(t *testing.T) {
		g := NewGomegaWithT(t)

		expectedPVC := TestPVC
		vars := newDefaultVariables(defaults)
		vars.DPFProvisioningController = DPFProvisioningVariables{
			BFBPersistentVolumeClaimName: expectedPVC,
			InstallInterface: &operatorv1.ProvisioningInstallInterface{
				InstallViaGNOI: &operatorv1.InstallViaGNOI{},
			},
		}

		generatedObjs, err := provCtrl.GenerateManifests(vars, skipApplySetCreationOption{})
		g.Expect(err).NotTo(HaveOccurred())

		// Verify no DaemonSet is created
		for _, obj := range generatedObjs {
			if obj.GetObjectKind().GroupVersionKind().Kind == string(DaemonSetKind) {
				t.Errorf("Found unexpected DaemonSet when using GNOI install interface")
			}
		}

		// Verify deployment has the correct flag for GNOI
		var deployment *appsv1.Deployment
		for _, obj := range generatedObjs {
			if obj.GetObjectKind().GroupVersionKind().Kind == string(DeploymentKind) {
				deploy := &appsv1.Deployment{}
				unstructuredObj, ok := obj.(*unstructured.Unstructured)
				g.Expect(ok).To(BeTrue())
				err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.UnstructuredContent(), deploy)
				g.Expect(err).NotTo(HaveOccurred())
				deployment = deploy
				break
			}
		}

		g.Expect(deployment).NotTo(BeNil())
		g.Expect(deployment.Spec.Template.Spec.Containers).To(HaveLen(1))
		g.Expect(deployment.Spec.Template.Spec.Containers[0].Args).To(ContainElement("--dpu-install-interface=gNOI"))
	})
}

func TestDPFProvisioningControllerObjects_setMaxDPUParallelInstallations(t *testing.T) {
	g := NewGomegaWithT(t)

	defaults := release.NewDefaults()
	err := defaults.Parse()
	g.Expect(err).NotTo(HaveOccurred())

	provCtrl := &provisioningControllerObjects{
		data:            provisioningControllerData,
		bfbRegistryData: bfbRegistryData,
	}
	err = provCtrl.Parse()
	g.Expect(err).NotTo(HaveOccurred())

	t.Run("test setting max DPU parallel installations", func(t *testing.T) {
		g := NewGomegaWithT(t)

		expectedPVC := TestPVC
		vars := newDefaultVariables(defaults)
		vars.DPFProvisioningController = DPFProvisioningVariables{
			BFBPersistentVolumeClaimName: expectedPVC,
			MaxDPUParallelInstallations:  ptr.To(int32(10)),
		}

		generatedObjs, err := provCtrl.GenerateManifests(vars, skipApplySetCreationOption{})
		g.Expect(err).NotTo(HaveOccurred())

		// Verify deployment has the correct flags
		var deployment *appsv1.Deployment
		for _, obj := range generatedObjs {
			if obj.GetObjectKind().GroupVersionKind().Kind == string(DeploymentKind) {
				deploy := &appsv1.Deployment{}
				unstructuredObj, ok := obj.(*unstructured.Unstructured)
				g.Expect(ok).To(BeTrue())
				err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.UnstructuredContent(), deploy)
				g.Expect(err).NotTo(HaveOccurred())
				deployment = deploy
				break
			}
		}

		g.Expect(deployment).NotTo(BeNil())
		g.Expect(deployment.Spec.Template.Spec.Containers).To(HaveLen(1))
		g.Expect(deployment.Spec.Template.Spec.Containers[0].Args).To(ContainElement("--max-dpu-parallel-installations=10"))
	})

	t.Run("test default (unset) max DPU parallel installations", func(t *testing.T) {
		g := NewGomegaWithT(t)

		expectedPVC := TestPVC
		vars := newDefaultVariables(defaults)
		vars.DPFProvisioningController = DPFProvisioningVariables{
			BFBPersistentVolumeClaimName: expectedPVC,
		}

		generatedObjs, err := provCtrl.GenerateManifests(vars, skipApplySetCreationOption{})
		g.Expect(err).NotTo(HaveOccurred())

		var deployment *appsv1.Deployment
		for _, obj := range generatedObjs {
			if obj.GetObjectKind().GroupVersionKind().Kind == string(DeploymentKind) {
				deploy := &appsv1.Deployment{}
				unstructuredObj, ok := obj.(*unstructured.Unstructured)
				g.Expect(ok).To(BeTrue())
				err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.UnstructuredContent(), deploy)
				g.Expect(err).NotTo(HaveOccurred())
				deployment = deploy
				break
			}
		}

		g.Expect(deployment).NotTo(BeNil())
		g.Expect(deployment.Spec.Template.Spec.Containers).To(HaveLen(1))
		for _, arg := range deployment.Spec.Template.Spec.Containers[0].Args {
			g.Expect(arg).NotTo(ContainSubstring("--max-dpu-parallel-installations"))
		}
	})

	t.Run("test 1 max DPU parallel installations", func(t *testing.T) {
		g := NewGomegaWithT(t)

		expectedPVC := TestPVC
		vars := newDefaultVariables(defaults)
		vars.DPFProvisioningController = DPFProvisioningVariables{
			BFBPersistentVolumeClaimName: expectedPVC,
			MaxDPUParallelInstallations:  ptr.To(int32(1)),
		}

		generatedObjs, err := provCtrl.GenerateManifests(vars, skipApplySetCreationOption{})
		g.Expect(err).NotTo(HaveOccurred())

		// Verify deployment has the correct flags
		var deployment *appsv1.Deployment
		for _, obj := range generatedObjs {
			if obj.GetObjectKind().GroupVersionKind().Kind == string(DeploymentKind) {
				deploy := &appsv1.Deployment{}
				unstructuredObj, ok := obj.(*unstructured.Unstructured)
				g.Expect(ok).To(BeTrue())
				err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.UnstructuredContent(), deploy)
				g.Expect(err).NotTo(HaveOccurred())
				deployment = deploy
				break
			}
		}

		g.Expect(deployment).NotTo(BeNil())
		g.Expect(deployment.Spec.Template.Spec.Containers).To(HaveLen(1))
		g.Expect(deployment.Spec.Template.Spec.Containers[0].Args).To(ContainElement("--max-dpu-parallel-installations=1"))
	})

	t.Run("test negative max DPU parallel installations", func(t *testing.T) {
		g := NewGomegaWithT(t)

		expectedPVC := TestPVC
		vars := newDefaultVariables(defaults)
		vars.DPFProvisioningController = DPFProvisioningVariables{
			BFBPersistentVolumeClaimName: expectedPVC,
			MaxDPUParallelInstallations:  ptr.To(int32(-1)),
		}

		generatedObjs, err := provCtrl.GenerateManifests(vars, skipApplySetCreationOption{})
		g.Expect(err).NotTo(HaveOccurred())

		// Verify deployment has the correct flags
		var deployment *appsv1.Deployment
		for _, obj := range generatedObjs {
			if obj.GetObjectKind().GroupVersionKind().Kind == string(DeploymentKind) {
				deploy := &appsv1.Deployment{}
				unstructuredObj, ok := obj.(*unstructured.Unstructured)
				g.Expect(ok).To(BeTrue())
				err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.UnstructuredContent(), deploy)
				g.Expect(err).NotTo(HaveOccurred())
				deployment = deploy
				break
			}
		}

		g.Expect(deployment).NotTo(BeNil())
		g.Expect(deployment.Spec.Template.Spec.Containers).To(HaveLen(1))
		g.Expect(deployment.Spec.Template.Spec.Containers[0].Args).To(ContainElement("--max-dpu-parallel-installations=-1"))
	})

	t.Run("test large max DPU parallel installations", func(t *testing.T) {
		g := NewGomegaWithT(t)

		expectedPVC := TestPVC
		vars := newDefaultVariables(defaults)
		vars.DPFProvisioningController = DPFProvisioningVariables{
			BFBPersistentVolumeClaimName: expectedPVC,
			MaxDPUParallelInstallations:  ptr.To(int32(1000)),
		}

		generatedObjs, err := provCtrl.GenerateManifests(vars, skipApplySetCreationOption{})
		g.Expect(err).NotTo(HaveOccurred())

		// Verify deployment has the correct flags
		var deployment *appsv1.Deployment
		for _, obj := range generatedObjs {
			if obj.GetObjectKind().GroupVersionKind().Kind == string(DeploymentKind) {
				deploy := &appsv1.Deployment{}
				unstructuredObj, ok := obj.(*unstructured.Unstructured)
				g.Expect(ok).To(BeTrue())
				err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.UnstructuredContent(), deploy)
				g.Expect(err).NotTo(HaveOccurred())
				deployment = deploy
				break
			}
		}

		g.Expect(deployment).NotTo(BeNil())
		g.Expect(deployment.Spec.Template.Spec.Containers).To(HaveLen(1))
		g.Expect(deployment.Spec.Template.Spec.Containers[0].Args).To(ContainElement("--max-dpu-parallel-installations=1000"))
	})
}
