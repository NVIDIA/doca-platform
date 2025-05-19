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
	"context"
	"testing"

	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func Test_deploymentReadyCheck(t *testing.T) {
	g := NewWithT(t)

	s := scheme.Scheme

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testService",
			Namespace: "testNamespace",
		},
		Spec: appsv1.DeploymentSpec{},
		Status: appsv1.DeploymentStatus{
			Replicas: 5,
		},
	}
	tests := []struct {
		name    string
		status  appsv1.DeploymentStatus
		wantErr bool
	}{
		{
			name:    "error if deployment has empty status",
			status:  appsv1.DeploymentStatus{},
			wantErr: true,
		},
		{
			name: "error if deployment doesn't define readyReplicas",
			status: appsv1.DeploymentStatus{
				Replicas: 1,
			},
			wantErr: true,
		},
		{
			name: "error if deployment replicas != readyReplicas",
			status: appsv1.DeploymentStatus{
				Replicas:      3,
				ReadyReplicas: 1,
			},
			wantErr: true,
		},
		{
			name: "succeed if deployment has replicas == readyReplicas",
			status: appsv1.DeploymentStatus{
				Replicas:      5,
				ReadyReplicas: 5,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deployment.Status = tt.status
			testClient := fake.NewClientBuilder().WithScheme(s).WithObjects(deployment).Build()

			objects := []*unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"apiVersion": "apps/v1",
						"kind":       "Deployment",
						"metadata": map[string]interface{}{
							"name":      deployment.Name,
							"namespace": deployment.Namespace,
						},
					},
				},
			}
			err := deploymentReadyCheck(context.Background(), testClient, deployment.Namespace, objects)
			g.Expect(err != nil).To(Equal(tt.wantErr), err)
		})
	}
}

func Test_deamonsetReadyCheck(t *testing.T) {
	g := NewWithT(t)

	s := scheme.Scheme

	daemonset := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testService",
			Namespace: "testNamespace",
		},
		Spec: appsv1.DaemonSetSpec{},
		Status: appsv1.DaemonSetStatus{
			NumberReady: 5,
		},
	}
	tests := []struct {
		name    string
		status  appsv1.DaemonSetStatus
		wantErr bool
	}{
		{
			name:    "error if daemonset has empty status",
			status:  appsv1.DaemonSetStatus{},
			wantErr: true,
		},
		{
			name: "error if daemonset doesn't define DesiredNumberScheduled",
			status: appsv1.DaemonSetStatus{
				NumberReady: 1,
			},
			wantErr: true,
		},
		{
			name: "error if daemonset NumberReady != DesiredNumberScheduled",
			status: appsv1.DaemonSetStatus{
				NumberReady:            3,
				DesiredNumberScheduled: 1,
			},
			wantErr: true,
		},
		{
			name: "succeed if daemonset has NumberReady == DesiredNumberScheduled",
			status: appsv1.DaemonSetStatus{
				NumberReady:            5,
				DesiredNumberScheduled: 5,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			daemonset.Status = tt.status
			testClient := fake.NewClientBuilder().WithScheme(s).WithObjects(daemonset).Build()

			objects := []*unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"apiVersion": "apps/v1",
						"kind":       "DaemonSet",
						"metadata": map[string]interface{}{
							"name":      daemonset.Name,
							"namespace": daemonset.Namespace,
						},
					},
				},
			}
			err := daemonsetReadyCheck(context.Background(), testClient, daemonset.Namespace, objects)
			g.Expect(err != nil).To(Equal(tt.wantErr), err)
		})
	}
}
