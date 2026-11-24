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

package controller

import (
	"testing"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	"github.com/nvidia/doca-platform/internal/operator/certmanagement"
	"github.com/nvidia/doca-platform/pkg/certmanager"

	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestAnchorIssuerToDPFOperatorConfig checks that an issuer event enqueues the DPFOperatorConfig only
// when it is for the issuer the anchor Certificate names.
func TestAnchorIssuerToDPFOperatorConfig(t *testing.T) {
	ns := "anchor-issuer-enqueue"
	configKey := types.NamespacedName{
		Namespace: ns,
		Name:      "dpfoperatorconfig",
	}

	clusterAnchor := certmanager.IssuerReference{
		Name:  "enterprise-ca",
		Kind:  operatorv1.CertManagerClusterIssuerKind,
		Group: operatorv1.CertManagerGroup,
	}

	// A scheme of its own rather than scheme.Scheme: the fake client registers the unstructured
	// kinds it reads into the scheme it is given, and the manager the suite runs reads scheme.Scheme
	// concurrently.
	newReconciler := func(objs ...client.Object) *DPFOperatorConfigReconciler {
		s := runtime.NewScheme()
		return &DPFOperatorConfigReconciler{
			Client:   fake.NewClientBuilder().WithScheme(s).WithObjects(objs...).Build(),
			Scheme:   s,
			Settings: &DPFOperatorConfigReconcilerSettings{ConfigSingletonNamespaceName: &configKey},
		}
	}

	issuer := func(gvk schema.GroupVersionKind, namespace, name string) *unstructured.Unstructured {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(gvk)
		obj.SetNamespace(namespace)
		obj.SetName(name)
		return obj
	}

	// The ClusterIssuer the anchor names being created is the event that has to wake the reconcile:
	// while it does not exist, nothing else the controller watches changes.
	t.Run("enqueues for the ClusterIssuer the anchor names", func(t *testing.T) {
		g := NewWithT(t)
		r := newReconciler(newCACertificate(operatorv1.WebhookIntermediateCAName, clusterAnchor, ns))
		requests := r.AnchorIssuerToDPFOperatorConfig(ctx, issuer(certmanagement.ClusterIssuerGVK, "", clusterAnchor.Name))
		g.Expect(requests).To(ConsistOf(ctrl.Request{NamespacedName: configKey}))
	})

	t.Run("enqueues for the namespaced Issuer the anchor names", func(t *testing.T) {
		g := NewWithT(t)
		r := newReconciler(newCACertificate(operatorv1.WebhookIntermediateCAName, externalCAIssuerRef(), ns))
		requests := r.AnchorIssuerToDPFOperatorConfig(ctx, issuer(certmanagement.IssuerGVK, ns, externalCAIssuerRef().Name))
		g.Expect(requests).To(ConsistOf(ctrl.Request{NamespacedName: configKey}))
	})

	t.Run("ignores an issuer the anchor does not name", func(t *testing.T) {
		g := NewWithT(t)
		r := newReconciler(newCACertificate(operatorv1.WebhookIntermediateCAName, clusterAnchor, ns))
		requests := r.AnchorIssuerToDPFOperatorConfig(ctx, issuer(certmanagement.ClusterIssuerGVK, "", "other-ca"))
		g.Expect(requests).To(BeEmpty())
	})

	t.Run("ignores an issuer of the other kind with the same name", func(t *testing.T) {
		g := NewWithT(t)
		r := newReconciler(newCACertificate(operatorv1.WebhookIntermediateCAName, clusterAnchor, ns))
		requests := r.AnchorIssuerToDPFOperatorConfig(ctx, issuer(certmanagement.IssuerGVK, ns, clusterAnchor.Name))
		g.Expect(requests).To(BeEmpty())
	})

	t.Run("ignores a namespaced Issuer of the same name in another namespace", func(t *testing.T) {
		g := NewWithT(t)
		r := newReconciler(newCACertificate(operatorv1.WebhookIntermediateCAName, externalCAIssuerRef(), ns))
		requests := r.AnchorIssuerToDPFOperatorConfig(ctx, issuer(certmanagement.IssuerGVK, "elsewhere", externalCAIssuerRef().Name))
		g.Expect(requests).To(BeEmpty())
	})

	// The watch on the anchor Certificate wakes the reconcile once the chart creates it.
	t.Run("ignores every issuer while the anchor Certificate does not exist", func(t *testing.T) {
		g := NewWithT(t)
		requests := newReconciler().AnchorIssuerToDPFOperatorConfig(ctx, issuer(certmanagement.ClusterIssuerGVK, "", clusterAnchor.Name))
		g.Expect(requests).To(BeEmpty())
	})

	t.Run("ignores everything when no singleton config is configured", func(t *testing.T) {
		g := NewWithT(t)
		r := newReconciler(newCACertificate(operatorv1.WebhookIntermediateCAName, clusterAnchor, ns))
		r.Settings = &DPFOperatorConfigReconcilerSettings{}
		requests := r.AnchorIssuerToDPFOperatorConfig(ctx, issuer(certmanagement.ClusterIssuerGVK, "", clusterAnchor.Name))
		g.Expect(requests).To(BeEmpty())
	})
}
