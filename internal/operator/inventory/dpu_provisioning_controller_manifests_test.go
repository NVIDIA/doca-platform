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

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	"github.com/nvidia/doca-platform/internal/operator/utils"
	"github.com/nvidia/doca-platform/internal/release"

	. "github.com/onsi/gomega"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
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
				data: provisioningControllerData,
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
		data: provisioningControllerData,
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

	// This test is customized for the current Provisioning manifest, internal/operator/inventory/manifests/provisioningctrl.yaml.
	// These tests should be reviewed every time the manifest is updated
	t.Run("test field modification", func(t *testing.T) {
		ns := "namespace-one"
		g := NewGomegaWithT(t)
		expectedPVC := "test-pvc"
		expectedImagePullSecret1 := "test-image-pull-secret"
		expectedImagePullSecret2 := "test-image-pull-secret-2"
		expectedDmsTimeout := 20
		vars := newDefaultVariables(defaults)
		vars.Namespace = ns
		vars.DPFProvisioningController = DPFProvisioningVariables{
			BFBPersistentVolumeClaimName: expectedPVC,
			DMSTimeout:                   &expectedDmsTimeout,
		}
		vars.ImagePullSecrets = []string{expectedImagePullSecret1, expectedImagePullSecret2}
		generatedObjs, err := provCtrl.GenerateManifests(vars, skipApplySetCreationOption{})
		g.Expect(err).NotTo(HaveOccurred())

		// Expect the CRD and Namespace to have been removed. There are 5 CRDs and 1 Namespace in the manifest file.
		g.Expect(generatedObjs).To(HaveLen(len(originalObjs) - 6))

		// Expect the namespaces for all of the namespace scoped objects to equal the namespace in variables.
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
				g.Expect(selector[operatorv1.DPFComponentLabelKey]).To(Equal(dpfProvisioningControllerName))
			}
		}
		// * ensure deployment contains NodeAffinity
		g.Expect(*gotDeployment.Spec.Template.Spec.Affinity.NodeAffinity).To(Equal(controlPlaneNodeAffinity))

		// * ensure the component label is set
		g.Expect(gotDeployment.Spec.Template.Labels[operatorv1.DPFComponentLabelKey]).To(Equal(dpfProvisioningControllerName))
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
			if c.Name == "manager" {
				container = c.DeepCopy()
				break
			}
		}
		g.Expect(container).NotTo(BeNil())
		expectedArgs := []string{
			"--leader-elect",
			"--v=3",
			fmt.Sprintf("--dms-image=%s", defaults.DMSImage),
			fmt.Sprintf("--hostnetwork-image=%s", defaults.HostNetworkSetupImage),
			fmt.Sprintf("--bfb-pvc=%s", expectedPVC),
			fmt.Sprintf("--image-pull-secrets=%s", strings.Join([]string{expectedImagePullSecret1, expectedImagePullSecret2}, ",")),
			fmt.Sprintf("--dms-timeout=%d", expectedDmsTimeout),
		}
		g.Expect(gotDeployment.Spec.Template.Spec.Containers).To(HaveLen(1))
		g.Expect(gotDeployment.Spec.Template.Spec.Containers[0].Args).To(HaveLen(len(expectedArgs)))
		for i, ea := range expectedArgs {
			g.Expect(container.Args[i]).To(Equal(ea))
		}
	})
}
