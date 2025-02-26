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

package dpfctl

import (
	"testing"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/internal/conditions"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes/scheme"
)

func TestUnstructuredGetConditions(t *testing.T) {
	g := NewWithT(t)

	// Register the DPUService API to the scheme
	g.Expect(dpuservicev1.AddToScheme(scheme.Scheme)).To(Succeed())

	// GetConditions should return conditions from an unstructured object
	d := &dpuservicev1.DPUService{}
	conditions.AddTrue(d, conditions.TypeReady)
	u := &unstructured.Unstructured{}
	g.Expect(scheme.Scheme.Convert(d, u, nil)).To(Succeed())

	got := unstructuredGetSet(u).GetConditions()
	g.Expect(got).To(HaveLen(1))
	g.Expect(got[0].Type).To(Equal(string(conditions.TypeReady)))
	g.Expect(got[0].Status).To(Equal(metav1.ConditionTrue))

	// GetConditions should return nil for an unstructured object with empty conditions
	d = &dpuservicev1.DPUService{}
	u = &unstructured.Unstructured{}
	g.Expect(scheme.Scheme.Convert(d, u, nil)).To(Succeed())

	g.Expect(unstructuredGetSet(u).GetConditions()).To(BeNil())

	// GetConditions should return nil for an unstructured object without conditions
	e := &corev1.Endpoints{}
	u = &unstructured.Unstructured{}
	g.Expect(scheme.Scheme.Convert(e, u, nil)).To(Succeed())

	g.Expect(unstructuredGetSet(u).GetConditions()).To(BeNil())

	// GetConditions should return conditions from an unstructured object with a different type of conditions.
	p := &corev1.Pod{Status: corev1.PodStatus{
		Conditions: []corev1.PodCondition{
			{
				Type:               "foo",
				Status:             "foo",
				LastProbeTime:      metav1.Time{},
				LastTransitionTime: metav1.Time{},
				Reason:             "foo",
				Message:            "foo",
			},
		},
	}}
	u = &unstructured.Unstructured{}
	g.Expect(scheme.Scheme.Convert(p, u, nil)).To(Succeed())

	g.Expect(unstructuredGetSet(u).GetConditions()).To(HaveLen(1))
}

func TestUnstructuredSetConditions(t *testing.T) {
	g := NewWithT(t)

	// Register the DPUService API to the scheme
	g.Expect(dpuservicev1.AddToScheme(scheme.Scheme)).To(Succeed())

	d := &dpuservicev1.DPUService{}
	u := &unstructured.Unstructured{}
	g.Expect(scheme.Scheme.Convert(d, u, nil)).To(Succeed())

	// set conditions
	c := []metav1.Condition{
		{
			Type:               "foo",
			Status:             "foo",
			LastTransitionTime: metav1.Time{},
			Reason:             "foo",
		},
		{
			Type:               "bar",
			Status:             "bar",
			LastTransitionTime: metav1.Time{},
			Reason:             "bar",
		},
	}

	s := unstructuredGetSet(u)
	s.SetConditions(c)
	g.Expect(s.GetConditions()).To(BeComparableTo(c))
}
