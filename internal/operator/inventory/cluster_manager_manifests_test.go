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
	"strings"
	"testing"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	"github.com/nvidia/doca-platform/internal/release"

	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestClusterManagerObjects_ComparisonTable(t *testing.T) {
	g := NewWithT(t)
	defaults := &release.Defaults{}
	g.Expect(defaults.Parse()).To(Succeed())

	tests := []struct {
		name                     string
		clusterManager           *clusterManagerObjects
		expectKeepalivedFlag     bool
		expectCommonEdits        bool
		clusterManagerObjectName string
	}{
		{
			name:                     "Kamaji has keepalived flag and common edits",
			clusterManager:           newKamajiClusterManagerObjects(kamajiCMData),
			expectKeepalivedFlag:     true,
			expectCommonEdits:        true,
			clusterManagerObjectName: operatorv1.KamajiClusterManagerName.String(),
		},
		{
			name:                     "Static has NO keepalived flag but has common edits",
			clusterManager:           newStaticClusterManagerObjects(staticCMData),
			expectKeepalivedFlag:     false,
			expectCommonEdits:        true,
			clusterManagerObjectName: operatorv1.StaticClusterManagerName.String(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			g.Expect(tc.clusterManager.Parse()).NotTo(HaveOccurred())

			testNS := testNamespace
			vars := newDefaultVariables(defaults)
			// Static cluster manager is disabled by default, so we need to enable it for the test
			if tc.clusterManagerObjectName == operatorv1.StaticClusterManagerName.String() {
				vars.DisableSystemComponents[operatorv1.StaticClusterManagerName] = false
			}
			vars.Namespace = testNS

			objs, err := tc.clusterManager.GenerateManifests(vars, skipApplySetCreationOption{})
			g.Expect(err).NotTo(HaveOccurred())

			// Find the Deployment
			var deployment *appsv1.Deployment
			for _, obj := range objs {
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
			container := deployment.Spec.Template.Spec.Containers[0]

			// Check keepalived flag
			hasKeepalivedFlag := false
			for _, arg := range container.Args {
				if strings.HasPrefix(arg, "--keepalived-image=") {
					hasKeepalivedFlag = true
					break
				}
			}
			g.Expect(hasKeepalivedFlag).To(Equal(tc.expectKeepalivedFlag),
				"Cluster manager %s should have keepalived flag: %v", tc.clusterManagerObjectName, tc.expectKeepalivedFlag)

			// Check common edits if expected
			if tc.expectCommonEdits {
				g.Expect(deployment.Namespace).To(Equal(testNS))
				g.Expect(deployment.Spec.Template.Spec.Tolerations).To(Equal(controlPlaneTolerations))
				g.Expect(deployment.Spec.Template.Spec.Affinity).NotTo(BeNil())
				g.Expect(deployment.Spec.Template.Spec.Affinity.NodeAffinity).NotTo(BeNil())
			}
		})
	}
}

func TestClusterManagerObjects_ResourcesAndReplicas(t *testing.T) {
	g := NewWithT(t)
	defaults := &release.Defaults{}
	g.Expect(defaults.Parse()).To(Succeed())

	t.Run("Kamaji respects resources configuration", func(t *testing.T) {
		g := NewWithT(t)
		kamajiCM := newKamajiClusterManagerObjects(kamajiCMData)
		g.Expect(kamajiCM.Parse()).NotTo(HaveOccurred())

		vars := newDefaultVariables(defaults)
		vars.Resources[operatorv1.KamajiClusterManagerName.WithContainer(operatorv1.ControllerManagerContainer)] = corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("128Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("200m"),
				corev1.ResourceMemory: resource.MustParse("256Mi"),
			},
		}

		objs, err := kamajiCM.GenerateManifests(vars, skipApplySetCreationOption{})
		g.Expect(err).NotTo(HaveOccurred())

		var deployment *appsv1.Deployment
		for _, obj := range objs {
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
		container := deployment.Spec.Template.Spec.Containers[0]
		g.Expect(container.Resources.Requests.Cpu().String()).To(Equal("100m"))
		g.Expect(container.Resources.Requests.Memory().String()).To(Equal("128Mi"))
		g.Expect(container.Resources.Limits.Cpu().String()).To(Equal("200m"))
		g.Expect(container.Resources.Limits.Memory().String()).To(Equal("256Mi"))
	})

	t.Run("Static respects resources configuration", func(t *testing.T) {
		g := NewWithT(t)
		staticCM := newStaticClusterManagerObjects(staticCMData)
		g.Expect(staticCM.Parse()).NotTo(HaveOccurred())

		vars := newDefaultVariables(defaults)
		// Static cluster manager is disabled by default, so we need to enable it
		vars.DisableSystemComponents[operatorv1.StaticClusterManagerName] = false
		vars.Resources[operatorv1.StaticClusterManagerName.WithContainer(operatorv1.ControllerManagerContainer)] = corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("128Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("200m"),
				corev1.ResourceMemory: resource.MustParse("256Mi"),
			},
		}

		objs, err := staticCM.GenerateManifests(vars, skipApplySetCreationOption{})
		g.Expect(err).NotTo(HaveOccurred())

		var deployment *appsv1.Deployment
		for _, obj := range objs {
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
		container := deployment.Spec.Template.Spec.Containers[0]
		g.Expect(container.Resources.Requests.Cpu().String()).To(Equal("100m"))
		g.Expect(container.Resources.Requests.Memory().String()).To(Equal("128Mi"))
		g.Expect(container.Resources.Limits.Cpu().String()).To(Equal("200m"))
		g.Expect(container.Resources.Limits.Memory().String()).To(Equal("256Mi"))
	})
}
