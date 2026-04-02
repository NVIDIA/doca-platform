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

package inventory

import (
	"context"
	"fmt"
	"testing"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	"github.com/nvidia/doca-platform/internal/operator/utils"
	"github.com/nvidia/doca-platform/internal/release"

	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
)

func TestNodeSRIOVDevicePluginControllerObjects_Parse(t *testing.T) {
	g := NewGomegaWithT(t)
	originalObjects, err := utils.BytesToUnstructured(nodeSRIOVDevicePluginControllerData)
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
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			n := newNodeSRIOVDevicePluginControllerObjects(nodeSRIOVDevicePluginControllerData)
			n.data = tc.data
			if tc.expectErr {
				NewGomegaWithT(t).Expect(n.Parse()).To(HaveOccurred())
			} else {
				NewGomegaWithT(t).Expect(n.Parse()).NotTo(HaveOccurred())
			}
		})
	}
}

func TestNodeSRIOVDevicePluginControllerObjects_GenerateManifests(t *testing.T) {
	var (
		testNamespace       = "nodesriovdeviceplugincontroller-test"
		testImagePullSecret = "test-image-pull-secret"
	)

	g := NewWithT(t)
	nodeSRIOVDevicePluginCtrl := newNodeSRIOVDevicePluginControllerObjects(nodeSRIOVDevicePluginControllerData)
	g.Expect(nodeSRIOVDevicePluginCtrl.Parse()).NotTo(HaveOccurred())
	defaults := &release.Defaults{}
	g.Expect(defaults.Parse()).To(Succeed())

	t.Run("no objects if disable (by default)", func(t *testing.T) {
		vars := newDefaultVariables(defaults)
		objs, err := nodeSRIOVDevicePluginCtrl.GenerateManifests(context.Background(), vars, skipApplySetCreationOption{})
		if err != nil {
			t.Fatalf("failed to generate manifests: %v", err)
		}
		if len(objs) != 0 {
			t.Fatalf("manifests should not be generated when disabled: %v", objs)
		}
	})
	t.Run("test setting namespaces", func(t *testing.T) {
		g := NewWithT(t)
		vars := newDefaultVariables(defaults)
		vars.DisableSystemComponents[nodeSRIOVDevicePluginCtrl.Name()] = false
		vars.Namespace = testNamespace
		objs, err := nodeSRIOVDevicePluginCtrl.GenerateManifests(context.Background(), vars, skipApplySetCreationOption{})
		g.Expect(err).NotTo(HaveOccurred())
		for _, obj := range objs {
			if !isClusterScoped(obj.GetObjectKind().GroupVersionKind().Kind) {
				g.Expect(obj.GetNamespace()).To(Equal(testNamespace))
			}
		}
	})
	t.Run("test setting image pull secrets", func(t *testing.T) {
		g := NewGomegaWithT(t)
		vars := newDefaultVariables(defaults)
		vars.DisableSystemComponents[nodeSRIOVDevicePluginCtrl.Name()] = false
		vars.Namespace = testNamespace
		vars.ImagePullSecrets = []string{testImagePullSecret}
		generatedObjs, err := nodeSRIOVDevicePluginCtrl.GenerateManifests(context.Background(), vars, skipApplySetCreationOption{})
		g.Expect(err).NotTo(HaveOccurred())
		gotDeployment := &appsv1.Deployment{}
		for i, obj := range generatedObjs {
			if obj.GetObjectKind().GroupVersionKind().Kind == string(DeploymentKind) {
				deployment, ok := generatedObjs[i].(*unstructured.Unstructured)
				g.Expect(ok).To(BeTrue())
				g.Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(
					deployment.UnstructuredContent(), gotDeployment)).ToNot(HaveOccurred())
				break
			}
		}
		g.Expect(gotDeployment).NotTo(BeNil())
		g.Expect(gotDeployment.Spec.Template.Spec.ImagePullSecrets).To(HaveLen(1))
		g.Expect(gotDeployment.Spec.Template.Spec.ImagePullSecrets[0].Name).To(Equal(testImagePullSecret))
	})
	t.Run("test setting tolerations", func(t *testing.T) {
		g := NewGomegaWithT(t)
		vars := newDefaultVariables(defaults)
		vars.DisableSystemComponents[nodeSRIOVDevicePluginCtrl.Name()] = false
		generatedObjs, err := nodeSRIOVDevicePluginCtrl.GenerateManifests(context.Background(), vars, skipApplySetCreationOption{})
		g.Expect(err).NotTo(HaveOccurred())
		gotDeployment := &appsv1.Deployment{}
		for i, obj := range generatedObjs {
			if obj.GetObjectKind().GroupVersionKind().Kind == string(DeploymentKind) {
				deployment, ok := generatedObjs[i].(*unstructured.Unstructured)
				g.Expect(ok).To(BeTrue())
				g.Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(
					deployment.UnstructuredContent(), gotDeployment)).ToNot(HaveOccurred())
				break
			}
		}
		g.Expect(gotDeployment).NotTo(BeNil())
		g.Expect(gotDeployment.Spec.Template.Spec.Tolerations).To(ConsistOf(controlPlaneTolerations))
	})
	t.Run("test setting node affinity", func(t *testing.T) {
		g := NewGomegaWithT(t)
		vars := newDefaultVariables(defaults)
		vars.DisableSystemComponents[nodeSRIOVDevicePluginCtrl.Name()] = false
		generatedObjs, err := nodeSRIOVDevicePluginCtrl.GenerateManifests(context.Background(), vars, skipApplySetCreationOption{})
		g.Expect(err).NotTo(HaveOccurred())
		gotDeployment := &appsv1.Deployment{}
		for i, obj := range generatedObjs {
			if obj.GetObjectKind().GroupVersionKind().Kind == string(DeploymentKind) {
				deployment, ok := generatedObjs[i].(*unstructured.Unstructured)
				g.Expect(ok).To(BeTrue())
				g.Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(
					deployment.UnstructuredContent(), gotDeployment)).ToNot(HaveOccurred())
				break
			}
		}
		g.Expect(gotDeployment).NotTo(BeNil())
		g.Expect(gotDeployment.Spec.Template.Spec.Affinity).NotTo(BeNil())
		g.Expect(*gotDeployment.Spec.Template.Spec.Affinity.NodeAffinity).To(Equal(controlPlaneNodeAffinity))
	})
	t.Run("test setting image", func(t *testing.T) {
		g := NewGomegaWithT(t)
		vars := newDefaultVariables(defaults)
		vars.DisableSystemComponents[nodeSRIOVDevicePluginCtrl.Name()] = false
		setImage := "image-name"
		vars.Images[operatorv1.NodeSRIOVDevicePluginControllerName.WithContainer(
			operatorv1.ControllerManagerContainer)] = setImage
		generatedObjs, err := nodeSRIOVDevicePluginCtrl.GenerateManifests(context.Background(), vars, skipApplySetCreationOption{})
		g.Expect(err).NotTo(HaveOccurred())
		gotDeployment := &appsv1.Deployment{}
		for i, obj := range generatedObjs {
			if obj.GetObjectKind().GroupVersionKind().Kind == string(DeploymentKind) {
				deployment, ok := generatedObjs[i].(*unstructured.Unstructured)
				g.Expect(ok).To(BeTrue())
				g.Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(
					deployment.UnstructuredContent(), gotDeployment)).ToNot(HaveOccurred())
				break
			}
		}
		g.Expect(gotDeployment).NotTo(BeNil())
		g.Expect(gotDeployment.Spec.Template.Spec.Containers[0].Image).To(Equal(setImage))
	})
	t.Run("test setting args", func(t *testing.T) {
		g := NewGomegaWithT(t)
		vars := newDefaultVariables(defaults)
		vars.DisableSystemComponents[nodeSRIOVDevicePluginCtrl.Name()] = false
		vars.Namespace = testNamespace
		vars.NodeSRIOVDevicePluginController = NodeSRIOVDevicePluginControllerVariables{
			DevicePluginImage:     "device-plugin-image:v1",
			DevicePluginInitImage: "device-plugin-init-image:v1",
			DefaultResourcePrefix: "custom.prefix.io",
		}
		vars.ImagePullSecrets = []string{"secret1", "secret2"}
		generatedObjs, err := nodeSRIOVDevicePluginCtrl.GenerateManifests(context.Background(), vars, skipApplySetCreationOption{})
		g.Expect(err).NotTo(HaveOccurred())
		gotDeployment := &appsv1.Deployment{}
		for i, obj := range generatedObjs {
			if obj.GetObjectKind().GroupVersionKind().Kind == string(DeploymentKind) {
				deployment, ok := generatedObjs[i].(*unstructured.Unstructured)
				g.Expect(ok).To(BeTrue())
				g.Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(
					deployment.UnstructuredContent(), gotDeployment)).ToNot(HaveOccurred())
				break
			}
		}
		g.Expect(gotDeployment).NotTo(BeNil())
		g.Expect(gotDeployment.Spec.Template.Spec.Containers).To(HaveLen(1))
		args := gotDeployment.Spec.Template.Spec.Containers[0].Args
		g.Expect(args).To(ContainElement(fmt.Sprintf("--namespace=%s", testNamespace)))
		g.Expect(args).To(ContainElement("--device-plugin-image=device-plugin-image:v1"))
		g.Expect(args).To(ContainElement("--device-plugin-init-image=device-plugin-init-image:v1"))
		g.Expect(args).To(ContainElement("--default-resource-prefix=custom.prefix.io"))
		g.Expect(args).To(ContainElement("--image-pull-secrets=secret1,secret2"))
		expectedOwnerConfigMapName := ""
		for _, obj := range generatedObjs {
			if obj.GetObjectKind().GroupVersionKind().Kind != "ConfigMap" {
				continue
			}
			expectedOwnerConfigMapName = obj.GetName()
			break
		}
		g.Expect(expectedOwnerConfigMapName).NotTo(BeEmpty())
		g.Expect(args).To(ContainElement(fmt.Sprintf("--owner-configmap-name=%s", expectedOwnerConfigMapName)))
	})
	t.Run("test setting resources", func(t *testing.T) {
		g := NewGomegaWithT(t)
		vars := newDefaultVariables(defaults)
		vars.DisableSystemComponents[nodeSRIOVDevicePluginCtrl.Name()] = false
		vars.Resources[nodeSRIOVDevicePluginCtrl.Name().WithContainer(
			operatorv1.ControllerManagerContainer)] = corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("500m"),
				corev1.ResourceMemory: resource.MustParse("512Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("500m"),
				corev1.ResourceMemory: resource.MustParse("512Mi"),
			},
		}
		objs, err := nodeSRIOVDevicePluginCtrl.GenerateManifests(context.Background(), vars, skipApplySetCreationOption{})
		g.Expect(err).NotTo(HaveOccurred())
		for _, obj := range objs {
			if obj.GetObjectKind().GroupVersionKind().Kind != string(DeploymentKind) {
				continue
			}
			deployment := &appsv1.Deployment{}
			uns, ok := obj.(*unstructured.Unstructured)
			g.Expect(ok).To(BeTrue())
			g.Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(
				uns.UnstructuredContent(), deployment)).To(Succeed())
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
	t.Run("test setting replicas", func(t *testing.T) {
		g := NewGomegaWithT(t)
		vars := newDefaultVariables(defaults)
		vars.DisableSystemComponents[nodeSRIOVDevicePluginCtrl.Name()] = false
		vars.Replicas[operatorv1.NodeSRIOVDevicePluginControllerName] = ptr.To[int32](3)
		objs, err := nodeSRIOVDevicePluginCtrl.GenerateManifests(context.Background(), vars, skipApplySetCreationOption{})
		g.Expect(err).NotTo(HaveOccurred())
		for _, obj := range objs {
			if obj.GetObjectKind().GroupVersionKind().Kind != string(DeploymentKind) {
				continue
			}
			deployment := &appsv1.Deployment{}
			uns, ok := obj.(*unstructured.Unstructured)
			g.Expect(ok).To(BeTrue())
			g.Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(
				uns.UnstructuredContent(), deployment)).To(Succeed())
			g.Expect(*deployment.Spec.Replicas).To(Equal(int32(3)))
		}
	})
}
