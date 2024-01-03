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
	"testing"

	. "github.com/onsi/gomega"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestDPUServiceControllerManifestsParse(t *testing.T) {
	g := NewWithT(t)
	dpuServiceMainfests := &dpuServiceControllerObjects{data: dpuServiceData}
	g.Expect(dpuServiceMainfests.Parse()).ToNot(HaveOccurred())

	// make sure we found expected objects
	foundByKind := map[ObjectKind]*unstructured.Unstructured{}
	for _, o := range dpuServiceMainfests.objects {
		foundByKind[ObjectKind(o.GetKind())] = o
	}

	g.Expect(foundByKind).To(HaveKey(DeploymentKind))
	g.Expect(foundByKind).To(HaveKey(RoleKind))
	g.Expect(foundByKind).To(HaveKey(RoleBindingKind))
	g.Expect(foundByKind).To(HaveKey(ServiceAccountKind))
	g.Expect(foundByKind).To(HaveKey(ClusterRoleKind))
	g.Expect(foundByKind).To(HaveKey(ClusterRoleBindingKind))
	g.Expect(foundByKind).To(HaveKey(ValidatingWebhookConfigurationKind))
	g.Expect(foundByKind).To(HaveKey(ServiceKind))
	g.Expect(foundByKind).To(HaveKey(CertificateKind))
	g.Expect(foundByKind).To(HaveKey(IssuerKind))

	// make sure no namespace and crd obj
	g.Expect(foundByKind).ToNot(HaveKey(CustomResourceDefinitionKind))
	g.Expect(foundByKind).ToNot(HaveKey(NamespaceKind))

	// ensure no additional object kinds
	g.Expect(foundByKind).To(HaveLen(10))

	// ensure objects parse to concrete type
	g.Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(foundByKind[DeploymentKind].UnstructuredContent(), &appsv1.Deployment{})).ToNot(HaveOccurred())
	g.Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(foundByKind[RoleKind].UnstructuredContent(), &rbacv1.Role{})).ToNot(HaveOccurred())
	g.Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(foundByKind[RoleBindingKind].UnstructuredContent(), &rbacv1.RoleBinding{})).ToNot(HaveOccurred())
	g.Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(foundByKind[ServiceAccountKind].UnstructuredContent(), &corev1.ServiceAccount{})).ToNot(HaveOccurred())
	g.Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(foundByKind[ClusterRoleKind].UnstructuredContent(), &rbacv1.ClusterRole{})).ToNot(HaveOccurred())
	g.Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(foundByKind[ClusterRoleBindingKind].UnstructuredContent(), &rbacv1.ClusterRoleBinding{})).ToNot(HaveOccurred())
	g.Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(foundByKind[ValidatingWebhookConfigurationKind].UnstructuredContent(), &admissionregistrationv1.ValidatingWebhookConfiguration{})).ToNot(HaveOccurred())
	g.Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(foundByKind[ServiceKind].UnstructuredContent(), &corev1.Service{})).ToNot(HaveOccurred())
}
