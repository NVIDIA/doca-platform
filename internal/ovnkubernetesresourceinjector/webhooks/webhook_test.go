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

package webhooks

import (
	"context"
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestNetworkInjector_Default(t *testing.T) {
	g := NewWithT(t)
	controlPlaneNodeName := "control-plane-node"
	workerNodeName := "worker-node"
	resourceName := corev1.ResourceName("test-resource")

	objects := []client.Object{
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: controlPlaneNodeName,
				Labels: map[string]string{
					"node-role.kubernetes.io/master": "",
				},
			},
		},
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:   workerNodeName,
				Labels: map[string]string{},
			},
		},
		&unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "k8s.cni.cncf.io/v1",
				"kind":       "NetworkAttachmentDefinition",
				"metadata": map[string]interface{}{
					"name":      "dpf-ovn-kubernetes",
					"namespace": "ovn-kubernetes",
					"annotations": map[string]interface{}{
						"k8s.v1.cni.cncf.io/resourceName": resourceName.String(),
					},
				},
			},
		},
	}

	controlPlaneMatchExpressionsExists := []corev1.NodeSelectorTerm{
		{
			MatchExpressions: []corev1.NodeSelectorRequirement{
				{
					Key:      "node-role.kubernetes.io/master",
					Operator: corev1.NodeSelectorOpExists,
				},
				{
					Key:      "node-role.kubernetes.io/control-plane",
					Operator: corev1.NodeSelectorOpExists,
				},
			},
		},
	}

	controlPlaneMatchExpressionsIn := []corev1.NodeSelectorTerm{
		{
			MatchExpressions: []corev1.NodeSelectorRequirement{
				{
					Key:      "node-role.kubernetes.io/master",
					Operator: corev1.NodeSelectorOpIn,
					Values:   []string{""},
				},
				{
					Key:      "node-role.kubernetes.io/control-plane",
					Operator: corev1.NodeSelectorOpIn,
					Values:   []string{""},
				},
			},
		},
	}

	basePod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "nginx",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{},
						Limits:   corev1.ResourceList{},
					},
				},
			},
		},
	}
	hostNetworkPod := basePod.DeepCopy()
	hostNetworkPod.Spec.HostNetwork = true

	podWithControlPlaneNodeSelector := basePod.DeepCopy()
	podWithControlPlaneNodeSelector.Spec.NodeSelector = map[string]string{"node-role.kubernetes.io/master": ""}

	podWithControlPlaneNodeSelectorMatchExpressionsExists := basePod.DeepCopy()
	setSelectorTerms(podWithControlPlaneNodeSelectorMatchExpressionsExists, controlPlaneMatchExpressionsExists)

	podWithControlPlaneNodeSelectorMatchExpressionsIn := basePod.DeepCopy()
	setSelectorTerms(podWithControlPlaneNodeSelectorMatchExpressionsIn, controlPlaneMatchExpressionsIn)

	podWithControlPlaneNodeNameSelectorTerms := basePod.DeepCopy()
	setSelectorTermsToNodeName(podWithControlPlaneNodeNameSelectorTerms, controlPlaneNodeName)

	podWithWorkerNodeSelectorTerms := basePod.DeepCopy()
	setSelectorTermsToNodeName(podWithWorkerNodeSelectorTerms, workerNodeName)

	podWithExistingVFResources := basePod.DeepCopy()

	podWithExistingVFResources.Spec.Containers[0].Resources.Requests = corev1.ResourceList{
		resourceName: resource.MustParse("1"),
	}
	podWithExistingVFResources.Spec.Containers[0].Resources.Limits = corev1.ResourceList{
		resourceName: resource.MustParse("1"),
	}

	tests := []struct {
		name                  string
		pod                   *corev1.Pod
		expectedResourceCount string
		expectAnnotation      bool
	}{
		{
			name:                  "don't inject resource into pod that has hostNetwork == true",
			pod:                   hostNetworkPod,
			expectedResourceCount: "0",
		},
		{
			name:                  "don't inject resource into pod that has a node selector for a control plane machine",
			pod:                   podWithControlPlaneNodeSelector,
			expectedResourceCount: "0",
		},
		{
			name:                  "don't inject resource into pod that explicitly targets a control plane node with NodeSelectorTerms",
			pod:                   podWithControlPlaneNodeNameSelectorTerms,
			expectedResourceCount: "0",
		},
		{
			name:                  "don't inject resource into pod that explicitly targets a control plane node with NodeSelectorTerms with Exists operator",
			pod:                   podWithControlPlaneNodeSelectorMatchExpressionsExists,
			expectedResourceCount: "0",
		},
		{
			name:                  "don't inject resource into pod that explicitly targets a control plane node with NodeSelectorTerms with In operator",
			pod:                   podWithControlPlaneNodeSelectorMatchExpressionsIn,
			expectedResourceCount: "0",
		},
		{
			name:                  "inject resource into pod that explicitly targets a worker node with NodeSelectorTerms",
			pod:                   podWithWorkerNodeSelectorTerms,
			expectedResourceCount: "1",
			expectAnnotation:      true,
		},
		{
			name:                  "inject resource into pod that has no clear scheduling target",
			pod:                   basePod,
			expectedResourceCount: "1",
			expectAnnotation:      true,
		},
		{
			name:                  "inject resources into pod with existing resource claims",
			pod:                   podWithExistingVFResources,
			expectedResourceCount: "2",
			expectAnnotation:      true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := scheme.Scheme
			fakeclient := fake.NewClientBuilder().WithObjects(objects...).WithScheme(s).Build()
			webhook := &NetworkInjector{
				Client: fakeclient,
				Settings: NetworkInjectorSettings{
					NADName:      "dpf-ovn-kubernetes",
					NADNamespace: "ovn-kubernetes",
				},
			}
			err := webhook.Default(context.Background(), tt.pod)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(tt.pod.Spec.Containers[0].Resources.Limits[resourceName].Equal(resource.MustParse(tt.expectedResourceCount))).To(BeTrue())
			g.Expect(tt.pod.Spec.Containers[0].Resources.Requests[resourceName].Equal(resource.MustParse(tt.expectedResourceCount))).To(BeTrue())
			//nolint:ginkgolinter
			g.Expect(tt.pod.Annotations[annotationKeyToBeInjected] == "ovn-kubernetes/dpf-ovn-kubernetes").To(Equal(tt.expectAnnotation))
		})
	}
}

func TestNetworkInjector_PreReqObjects(t *testing.T) {
	g := NewWithT(t)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "nginx",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{},
						Limits:   corev1.ResourceList{},
					},
				},
			},
		},
	}

	networkAttachDefWithoutAnnotation := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "k8s.cni.cncf.io/v1",
			"kind":       "NetworkAttachmentDefinition",
			"metadata": map[string]interface{}{
				"name":      "dpf-ovn-kubernetes",
				"namespace": "ovn-kubernetes",
			},
		},
	}

	networkAttachDefWithAnnotation := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "k8s.cni.cncf.io/v1",
			"kind":       "NetworkAttachmentDefinition",
			"metadata": map[string]interface{}{
				"name":      "dpf-ovn-kubernetes",
				"namespace": "ovn-kubernetes",
				"annotations": map[string]interface{}{
					"k8s.v1.cni.cncf.io/resourceName": "some-resource",
				},
			},
		},
	}

	tests := []struct {
		name            string
		existingObjects []client.Object
		expectError     bool
	}{
		{
			name:            "no NetworkAttachmentDefinition",
			existingObjects: nil,
			expectError:     true,
		},
		{
			name:            "no annotation on NetworkAttachmentDefinition",
			existingObjects: []client.Object{networkAttachDefWithoutAnnotation},
			expectError:     true,
		},
		{
			name:            "all prereq objects exist",
			existingObjects: []client.Object{networkAttachDefWithAnnotation},
			expectError:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := scheme.Scheme
			fakeclient := fake.NewClientBuilder().WithObjects(tt.existingObjects...).WithScheme(s).Build()
			webhook := &NetworkInjector{
				Client: fakeclient,
				Settings: NetworkInjectorSettings{
					NADName:      "dpf-ovn-kubernetes",
					NADNamespace: "ovn-kubernetes",
				},
			}
			err := webhook.Default(context.Background(), pod)
			if tt.expectError {
				g.Expect(err).To(HaveOccurred())
			} else {
				g.Expect(err).ToNot(HaveOccurred())
			}
		})
	}
}

func setSelectorTermsToNodeName(pod *corev1.Pod, nodeName string) {
	setSelectorTerms(pod, []corev1.NodeSelectorTerm{
		{
			MatchFields: []corev1.NodeSelectorRequirement{
				{
					Key:    "metadata.name",
					Values: []string{nodeName},
				},
			},
		},
	})
}

func setSelectorTerms(pod *corev1.Pod, terms []corev1.NodeSelectorTerm) {
	if pod.Spec.Affinity == nil {
		pod.Spec.Affinity = &corev1.Affinity{}
	}
	if pod.Spec.Affinity.NodeAffinity == nil {
		pod.Spec.Affinity.NodeAffinity = &corev1.NodeAffinity{}
	}
	if pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
		pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution = &corev1.NodeSelector{}
	}
	pod.Spec.Affinity.NodeAffinity.
		RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms = terms
}
