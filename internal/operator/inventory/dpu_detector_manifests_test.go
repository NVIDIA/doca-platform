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
	_ "embed"
	"fmt"
	"testing"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	"github.com/nvidia/doca-platform/internal/operator/utils"
	"github.com/nvidia/doca-platform/internal/release"

	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestDPUDetectorObjects_Parse(t *testing.T) {
	g := NewGomegaWithT(t)
	originalObjects, err := utils.BytesToUnstructured(dpuDetectorData)
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
	missingDaemonSet := iterate(func(u *unstructured.Unstructured) bool {
		return u.GetKind() != string(DaemonSetKind)
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
			name:      "fail if no Daemonset in manifests",
			data:      missingDaemonSet,
			expectErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := dpuDetectorObjects{
				data: dpuDetectorData,
			}
			d.data = tc.data
			if tc.expectErr {
				NewGomegaWithT(t).Expect(d.Parse()).To(HaveOccurred())
			} else {
				NewGomegaWithT(t).Expect(d.Parse()).NotTo(HaveOccurred())
			}
		})
	}

}
func TestDPUDetectorObjects_GenerateManifests(t *testing.T) {
	g := NewWithT(t)
	dpuDetectorCtrl := dpuDetectorObjects{
		data: dpuDetectorData,
	}
	g.Expect(dpuDetectorCtrl.Parse()).NotTo(HaveOccurred())
	defaults := &release.Defaults{}
	g.Expect(defaults.Parse()).To(Succeed())

	t.Run("no objects if disable is set", func(t *testing.T) {
		vars := newDefaultVariables(defaults)
		vars.DisableSystemComponents = map[string]bool{
			dpuDetectorCtrl.Name(): true,
		}
		objs, err := dpuDetectorCtrl.GenerateManifests(vars, skipApplySetCreationOption{})
		if err != nil {
			t.Fatalf("failed to generate manifests: %v", err)
		}
		if len(objs) != 0 {
			t.Fatalf("manifests should not be generated when disabled: %v", objs)
		}
	})

	t.Run("test setting namespaces", func(t *testing.T) {
		g := NewWithT(t)
		testNS := "foop"
		vars := newDefaultVariables(defaults)

		vars.Namespace = testNS
		objs, err := dpuDetectorCtrl.GenerateManifests(vars, skipApplySetCreationOption{})
		g.Expect(err).NotTo(HaveOccurred())
		for _, obj := range objs {
			g.Expect(obj.GetNamespace()).To(Equal(testNS))
		}
	})

	t.Run("test setting image pull secrets", func(t *testing.T) {
		ns := "namespace-one"
		g := NewGomegaWithT(t)
		expectedImagePullSecret1 := "test-image-pull-secret"
		vars := newDefaultVariables(defaults)
		vars.Namespace = ns
		vars.ImagePullSecrets = []string{expectedImagePullSecret1}
		generatedObjs, err := dpuDetectorCtrl.GenerateManifests(vars, skipApplySetCreationOption{})
		g.Expect(err).NotTo(HaveOccurred())
		gotDaemonSet := &appsv1.DaemonSet{}
		for i, obj := range generatedObjs {
			if obj.GetObjectKind().GroupVersionKind().Kind == string(DaemonSetKind) {
				ds, ok := generatedObjs[i].(*unstructured.Unstructured)
				g.Expect(ok).To(BeTrue())
				g.Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(ds.UnstructuredContent(), gotDaemonSet)).ToNot(HaveOccurred())
				break
			}
		}
		g.Expect(gotDaemonSet).NotTo(BeNil())
		g.Expect(gotDaemonSet.Spec.Template.Spec.ImagePullSecrets).To(HaveLen(1))
		g.Expect(gotDaemonSet.Spec.Template.Spec.ImagePullSecrets[0].Name).To(Equal(expectedImagePullSecret1))
	})

	t.Run("test setting tolerations", func(t *testing.T) {
		g := NewGomegaWithT(t)
		vars := newDefaultVariables(defaults)
		generatedObjs, err := dpuDetectorCtrl.GenerateManifests(vars, skipApplySetCreationOption{})
		g.Expect(err).NotTo(HaveOccurred())
		gotDaemonSet := &appsv1.DaemonSet{}
		for i, obj := range generatedObjs {
			if obj.GetObjectKind().GroupVersionKind().Kind == string(DaemonSetKind) {
				ds, ok := generatedObjs[i].(*unstructured.Unstructured)
				g.Expect(ok).To(BeTrue())
				g.Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(ds.UnstructuredContent(), gotDaemonSet)).ToNot(HaveOccurred())
				break
			}
		}
		g.Expect(gotDaemonSet).NotTo(BeNil())
		g.Expect(gotDaemonSet.Spec.Template.Spec.Tolerations).To(ConsistOf([]corev1.Toleration{
			{
				Key:      "node.kubernetes.io/not-ready",
				Operator: corev1.TolerationOpExists,
				Effect:   corev1.TaintEffectNoSchedule,
			},
		},
		))
	})
	t.Run("test setting image and collector args", func(t *testing.T) {
		g := NewGomegaWithT(t)
		vars := newDefaultVariables(defaults)
		setArg := "collectors.PSID"
		setImage := "image-name"
		vars.Images[operatorv1.DPUDetectorName] = setImage
		vars.DPUDetectorCollectors = map[string]bool{
			setArg: true,
		}

		generatedObjs, err := dpuDetectorCtrl.GenerateManifests(vars, skipApplySetCreationOption{})
		g.Expect(err).NotTo(HaveOccurred())
		gotDaemonSet := &appsv1.DaemonSet{}
		for i, obj := range generatedObjs {
			if obj.GetObjectKind().GroupVersionKind().Kind == string(DaemonSetKind) {
				ds, ok := generatedObjs[i].(*unstructured.Unstructured)
				g.Expect(ok).To(BeTrue())
				g.Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(ds.UnstructuredContent(), gotDaemonSet)).ToNot(HaveOccurred())
				break
			}
		}
		g.Expect(gotDaemonSet).NotTo(BeNil())
		g.Expect(gotDaemonSet.Spec.Template.Spec.Containers[0].Image).To(Equal(setImage))
		g.Expect(gotDaemonSet.Spec.Template.Spec.Containers[0].Args).To(ConsistOf([]string{fmt.Sprintf("--%s=true", setArg)}))
	})

}
