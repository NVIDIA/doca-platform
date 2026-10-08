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

package certmanagement

import (
	"context"
	"testing"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/certmanager"
	"github.com/nvidia/doca-platform/pkg/conditions"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const testNamespace = "dpf-operator-system"

// testScheme registers the cert-manager kinds as unstructured so the fake client can serve them
// without the CRDs. An external issuer is most commonly a ClusterIssuer, so both scopes are covered.
func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	NewWithT(t).Expect(operatorv1.AddToScheme(scheme)).To(Succeed())
	NewWithT(t).Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())
	for _, gvk := range []schema.GroupVersionKind{CertificateGVK, IssuerGVK, clusterIssuerGVK} {
		scheme.AddKnownTypeWithName(gvk, &unstructured.Unstructured{})
		scheme.AddKnownTypeWithName(gvk.GroupVersion().WithKind(gvk.Kind+"List"), &unstructured.UnstructuredList{})
	}
	return scheme
}

// readyStatus builds the cert-manager Ready condition the operator keys off.
func readyStatus(ready bool, message string) map[string]interface{} {
	status := "False"
	if ready {
		status = string(metav1.ConditionTrue)
	}
	return map[string]interface{}{
		"conditions": []interface{}{
			map[string]interface{}{
				"type":    string(conditions.TypeReady),
				"status":  status,
				"message": message,
			},
		},
	}
}

// newPlatformCACertificate builds the platform intermediate CA Certificate as it exists in the
// cluster, anchored to the given issuer. A nil status stands for a Certificate cert-manager has not
// reported on yet.
func newPlatformCACertificate(issuerRef certmanager.IssuerReference, status map[string]interface{}) *unstructured.Unstructured {
	certificate := &unstructured.Unstructured{Object: map[string]interface{}{
		"spec": map[string]interface{}{
			"isCA":       true,
			"secretName": operatorv1.PlatformIntermediateCAName,
			"issuerRef": map[string]interface{}{
				"name":  issuerRef.Name,
				"kind":  issuerRef.Kind,
				"group": issuerRef.Group,
			},
		},
	}}
	certificate.SetGroupVersionKind(CertificateGVK)
	certificate.SetNamespace(testNamespace)
	certificate.SetName(operatorv1.PlatformIntermediateCAName)
	if status != nil {
		certificate.Object["status"] = status
	}
	return certificate
}

// newIssuer builds a cert-manager issuer of the given kind, namespaced for an Issuer and cluster
// scoped for a ClusterIssuer, so both resolution paths of getIssuer can be exercised.
func newIssuer(gvk schema.GroupVersionKind, name string, status map[string]interface{}) *unstructured.Unstructured {
	issuer := &unstructured.Unstructured{Object: map[string]interface{}{
		"spec": map[string]interface{}{"ca": map[string]interface{}{"secretName": "root-ca"}},
	}}
	issuer.SetGroupVersionKind(gvk)
	issuer.SetName(name)
	if gvk.Kind == operatorv1.CertManagerIssuerKind {
		issuer.SetNamespace(testNamespace)
	}
	if status != nil {
		issuer.Object["status"] = status
	}
	return issuer
}

// newConfig builds a DPFOperatorConfig in the namespace the objects above are created in.
func newConfig() *operatorv1.DPFOperatorConfig {
	return &operatorv1.DPFOperatorConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dpfoperatorconfig",
			Namespace: testNamespace,
		},
	}
}

// selfSignedRef is the anchor a chart that was given no authority of its own yields.
func selfSignedRef() certmanager.IssuerReference {
	return certmanager.IssuerReference{
		Name:  operatorv1.GlobalRootIssuerName,
		Kind:  operatorv1.CertManagerIssuerKind,
		Group: operatorv1.CertManagerGroup,
	}
}

// externalIssuerRef references an issuer of the given kind that stands in for an enterprise PKI.
func externalIssuerRef(kind string) certmanager.IssuerReference {
	return certmanager.IssuerReference{
		Name:  "openbao-issuer",
		Kind:  kind,
		Group: operatorv1.CertManagerGroup,
	}
}

// selfSignedRootIssuer builds the root issuer the DPF Operator chart creates, which anchors the
// platform CA whenever no external issuer is configured.
func selfSignedRootIssuer() *unstructured.Unstructured {
	return newIssuer(IssuerGVK, operatorv1.GlobalRootIssuerName, readyStatus(true, ""))
}

// TestReconcile covers what CertManagementReadyCondition reports for each state of the PKI, which
// is the only way an operator sees a misconfiguration without reading cert-manager objects directly.
func TestReconcile(t *testing.T) {
	clusterIssuerRef := externalIssuerRef(operatorv1.CertManagerClusterIssuerKind)

	tests := []struct {
		name string
		// anchor is the authority read from the webhook intermediate CA. The zero value stands for
		// the self-signed root, the anchor a chart that was given no authority of its own yields.
		anchor           certmanager.IssuerReference
		objs             []client.Object
		rotationRequired bool
		wantStatus       metav1.ConditionStatus
		wantReason       conditions.ConditionReason
		wantMessage      string
	}{
		{
			name:       "self-signed with an issued intermediate CA is ready",
			objs:       []client.Object{selfSignedRootIssuer(), newPlatformCACertificate(selfSignedRef(), readyStatus(true, ""))},
			wantStatus: metav1.ConditionTrue,
			wantReason: conditions.ReasonSuccess,
		},
		{
			name:        "self-signed waits for the intermediate CA to be created",
			objs:        []client.Object{selfSignedRootIssuer()},
			wantStatus:  metav1.ConditionFalse,
			wantReason:  conditions.ReasonPending,
			wantMessage: "to be created",
		},
		{
			name: "self-signed waits for the intermediate CA to be issued",
			objs: []client.Object{
				selfSignedRootIssuer(),
				newPlatformCACertificate(selfSignedRef(), readyStatus(false, "Issuing certificate as Secret does not exist")),
			},
			wantStatus:  metav1.ConditionFalse,
			wantReason:  conditions.ReasonPending,
			wantMessage: "Issuing certificate as Secret does not exist",
		},
		{
			name:        "self-signed waits for an intermediate CA that has no conditions yet",
			objs:        []client.Object{selfSignedRootIssuer(), newPlatformCACertificate(selfSignedRef(), nil)},
			wantStatus:  metav1.ConditionFalse,
			wantReason:  conditions.ReasonPending,
			wantMessage: "to be issued",
		},
		{
			// The chart creates this issuer together with the root it anchors, so a PKI anchored here
			// with the issuer absent means that chart is not installed or not reconciled yet. Naming
			// it is the only way to tell that apart from a chain cert-manager is still working on.
			name:        "a missing self-signed root issuer names the chart that creates it",
			objs:        []client.Object{newPlatformCACertificate(selfSignedRef(), readyStatus(true, ""))},
			wantStatus:  metav1.ConditionFalse,
			wantReason:  operatorv1.CertManagementReasonIssuerNotFound,
			wantMessage: "creates it together with the self-signed root it anchors",
		},
		{
			name:        "a self-signed root issuer that is not ready is waited on",
			objs:        []client.Object{newIssuer(IssuerGVK, operatorv1.GlobalRootIssuerName, readyStatus(false, "Secret does not have a field named tls.key"))},
			wantStatus:  metav1.ConditionFalse,
			wantReason:  conditions.ReasonPending,
			wantMessage: "Secret does not have a field named tls.key",
		},
		{
			name:        "a missing external ClusterIssuer is reported as not found",
			anchor:      clusterIssuerRef,
			objs:        []client.Object{newPlatformCACertificate(clusterIssuerRef, readyStatus(true, ""))},
			wantStatus:  metav1.ConditionFalse,
			wantReason:  operatorv1.CertManagementReasonIssuerNotFound,
			wantMessage: "does not exist",
		},
		{
			name:        "a missing external Issuer is reported as not found",
			anchor:      externalIssuerRef(operatorv1.CertManagerIssuerKind),
			wantStatus:  metav1.ConditionFalse,
			wantReason:  operatorv1.CertManagementReasonIssuerNotFound,
			wantMessage: "does not exist",
		},
		{
			name:   "an external ClusterIssuer that is not ready is waited on",
			anchor: clusterIssuerRef,
			objs: []client.Object{
				newIssuer(clusterIssuerGVK, clusterIssuerRef.Name, readyStatus(false, "Failed to initialize the Vault client")),
				newPlatformCACertificate(clusterIssuerRef, readyStatus(true, "")),
			},
			wantStatus:  metav1.ConditionFalse,
			wantReason:  conditions.ReasonPending,
			wantMessage: "Failed to initialize the Vault client",
		},
		{
			name:   "an external ClusterIssuer with an issued intermediate CA is ready",
			anchor: clusterIssuerRef,
			objs: []client.Object{
				newIssuer(clusterIssuerGVK, clusterIssuerRef.Name, readyStatus(true, "")),
				newPlatformCACertificate(clusterIssuerRef, readyStatus(true, "")),
			},
			wantStatus: metav1.ConditionTrue,
			wantReason: conditions.ReasonSuccess,
		},
		{
			name:   "an external Issuer is resolved in the namespace of the config",
			anchor: externalIssuerRef(operatorv1.CertManagerIssuerKind),
			objs: []client.Object{
				newIssuer(IssuerGVK, "openbao-issuer", readyStatus(true, "")),
				newPlatformCACertificate(externalIssuerRef(operatorv1.CertManagerIssuerKind), readyStatus(true, "")),
			},
			wantStatus: metav1.ConditionTrue,
			wantReason: conditions.ReasonSuccess,
		},
		{
			// A backend that only issues leaves rejects the CA request, which is the failure the
			// prerequisite about signing CA certificates is there to prevent.
			name:   "an external issuer refusing to sign a CA certificate is surfaced",
			anchor: clusterIssuerRef,
			objs: []client.Object{
				newIssuer(clusterIssuerGVK, clusterIssuerRef.Name, readyStatus(true, "")),
				newPlatformCACertificate(clusterIssuerRef, readyStatus(false, "not allowed to sign CA certificates")),
			},
			wantStatus:  metav1.ConditionFalse,
			wantReason:  conditions.ReasonPending,
			wantMessage: "not allowed to sign CA certificates",
		},
		{
			// Reported by the caller ahead of the chain below, which the reissue under the new anchor
			// brings back to ready on its own, so this step has to leave that report standing rather
			// than replace it with a state that clears itself.
			name:   "a required CA rotation takes precedence over a chain that is still reissuing",
			anchor: clusterIssuerRef,
			objs: []client.Object{
				newIssuer(clusterIssuerGVK, clusterIssuerRef.Name, readyStatus(true, "")),
				newPlatformCACertificate(clusterIssuerRef, readyStatus(false, "Issuing certificate as Secret was previously issued by another issuer")),
			},
			rotationRequired: true,
			wantStatus:       metav1.ConditionFalse,
			wantReason:       operatorv1.CertManagementReasonCARotationRequired,
			wantMessage:      "a CA rotation is what carries the new authority to them",
		},
		{
			// The state a re-anchor to a misspelled issuer name lands in. The chain can never be
			// reissued under an anchor that does not exist, so the rotation never completes: leaving
			// the rotation reported on its own would hold the condition on CARotationRequired
			// indefinitely with nothing naming the cause.
			name:             "a missing anchor is reported even while a CA rotation is required",
			anchor:           clusterIssuerRef,
			objs:             []client.Object{newPlatformCACertificate(clusterIssuerRef, readyStatus(true, ""))},
			rotationRequired: true,
			wantStatus:       metav1.ConditionFalse,
			wantReason:       operatorv1.CertManagementReasonIssuerNotFound,
			wantMessage:      "does not exist",
		},
		{
			// Same reasoning as the missing anchor above: one that is not ready cannot reissue the
			// chain either, so the rotation it is holding up is not the part to act on.
			name:   "an anchor that is not ready is reported even while a CA rotation is required",
			anchor: clusterIssuerRef,
			objs: []client.Object{
				newIssuer(clusterIssuerGVK, clusterIssuerRef.Name, readyStatus(false, "Failed to initialize the Vault client")),
				newPlatformCACertificate(clusterIssuerRef, readyStatus(true, "")),
			},
			rotationRequired: true,
			wantStatus:       metav1.ConditionFalse,
			wantReason:       conditions.ReasonPending,
			wantMessage:      "Failed to initialize the Vault client",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			config := newConfig()
			anchor := tt.anchor
			if anchor.Name == "" {
				anchor = selfSignedRef()
			}
			c := fake.NewClientBuilder().
				WithScheme(testScheme(t)).
				WithObjects(tt.objs...).
				Build()

			// Mirrors the caller, which reports the rotation as soon as it observes it rather than
			// after the apply that makes it unobservable.
			if tt.rotationRequired {
				setRotationRequired(config, anchor)
			}

			g.Expect(Reconcile(context.Background(), c, config, anchor, tt.rotationRequired)).To(Succeed())

			condition := conditions.Get(config, operatorv1.CertManagementReadyCondition)
			g.Expect(condition).NotTo(BeNil())
			g.Expect(condition.Status).To(Equal(tt.wantStatus))
			g.Expect(condition.Reason).To(Equal(string(tt.wantReason)))
			if tt.wantMessage != "" {
				g.Expect(condition.Message).To(ContainSubstring(tt.wantMessage))
			}
		})
	}
}

// TestDetectRotationRequired covers when re-anchoring the PKI needs a CA rotation: only once DPUs
// exist, because it is their BMC truststores that DPF cannot update. It also covers the recorded
// rotation outliving the difference it was derived from, which the apply destroys.
func TestDetectRotationRequired(t *testing.T) {
	clusterIssuerRef := externalIssuerRef(operatorv1.CertManagerClusterIssuerKind)

	dpu := &provisioningv1.DPU{
		ObjectMeta: metav1.ObjectMeta{Name: "dpu-one", Namespace: testNamespace},
	}

	tests := []struct {
		name string
		// recordedAnchor stages the anchor a config already carries as rolled out. Left unset, the
		// config carries none and the anchor is seeded from the platform CA Certificate below, which
		// is the sequence the reconcile follows.
		recordedAnchor *certmanager.IssuerReference
		anchor         certmanager.IssuerReference
		objs           []client.Object
		want           bool
	}{
		{
			name: "a fresh install has nothing anchored to rotate",
			objs: []client.Object{dpu},
			want: false,
		},
		{
			name: "an unchanged self-signed anchor needs no rotation",
			objs: []client.Object{newPlatformCACertificate(selfSignedRef(), readyStatus(true, "")), dpu},
			want: false,
		},
		{
			name:   "an unchanged external anchor needs no rotation",
			anchor: clusterIssuerRef,
			objs:   []client.Object{newPlatformCACertificate(clusterIssuerRef, readyStatus(true, "")), dpu},
			want:   false,
		},
		{
			name:   "switching to an external issuer without DPUs needs no rotation",
			anchor: clusterIssuerRef,
			objs:   []client.Object{newPlatformCACertificate(selfSignedRef(), readyStatus(true, ""))},
			want:   false,
		},
		{
			name:   "switching to an external issuer with DPUs needs a rotation",
			anchor: clusterIssuerRef,
			objs:   []client.Object{newPlatformCACertificate(selfSignedRef(), readyStatus(true, "")), dpu},
			want:   true,
		},
		{
			name: "reverting to the self-signed root with DPUs needs a rotation",
			objs: []client.Object{newPlatformCACertificate(clusterIssuerRef, readyStatus(true, "")), dpu},
			want: true,
		},
		{
			name:   "swapping one external issuer for another with DPUs needs a rotation",
			anchor: clusterIssuerRef,
			objs: []client.Object{
				newPlatformCACertificate(certmanager.IssuerReference{
					Name:  "other-issuer",
					Kind:  operatorv1.CertManagerClusterIssuerKind,
					Group: operatorv1.CertManagerGroup,
				}, readyStatus(true, "")),
				dpu,
			},
			want: true,
		},
		{
			// What every reconcile after the one that applied the rotation sees: the Certificate now
			// carries the new anchor, while the truststores the rotation is about do not. The record
			// is deliberately left on the previous anchor until they do.
			name:           "a rotation is still required once the Certificate has been repointed",
			recordedAnchor: ptr.To(selfSignedRef()),
			anchor:         clusterIssuerRef,
			objs:           []client.Object{newPlatformCACertificate(clusterIssuerRef, readyStatus(true, "")), dpu},
			want:           true,
		},
		{
			name:           "a rotation is retired once the new anchor is recorded as rolled out",
			recordedAnchor: ptr.To(clusterIssuerRef),
			anchor:         clusterIssuerRef,
			objs:           []client.Object{newPlatformCACertificate(clusterIssuerRef, readyStatus(true, "")), dpu},
			want:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			c := fake.NewClientBuilder().
				WithScheme(testScheme(t)).
				WithObjects(tt.objs...).
				Build()

			anchor := tt.anchor
			if anchor.Name == "" {
				anchor = selfSignedRef()
			}

			config := newConfig()
			if tt.recordedAnchor != nil {
				RecordAnchor(config, *tt.recordedAnchor)
			}
			_, err := ensureAnchorRecorded(context.Background(), c, config)
			g.Expect(err).NotTo(HaveOccurred())

			got, err := detectRotationRequired(context.Background(), c, config, anchor)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(got).To(Equal(tt.want))
		})
	}
}

// TestEnsureAnchorRecorded covers seeding the anchor of a cluster that was anchored before DPF
// recorded one, which is what an upgrade lands on, and leaving a recorded anchor alone once there
// is one. The second part is what keeps a rotation in flight from being retired by the Certificate
// the apply has already repointed.
func TestEnsureAnchorRecorded(t *testing.T) {
	clusterIssuerRef := externalIssuerRef(operatorv1.CertManagerClusterIssuerKind)

	t.Run("seeds the anchor from the platform CA Certificate", func(t *testing.T) {
		g := NewWithT(t)
		c := fake.NewClientBuilder().
			WithScheme(testScheme(t)).
			WithObjects(newPlatformCACertificate(clusterIssuerRef, readyStatus(true, ""))).
			Build()
		config := newConfig()

		seeded, err := ensureAnchorRecorded(context.Background(), c, config)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(seeded).To(BeTrue())
		g.Expect(config.Status.Security.CertManagement.Anchor).To(HaveValue(Equal(operatorv1.CertManagementAnchor{
			Name:  clusterIssuerRef.Name,
			Kind:  clusterIssuerRef.Kind,
			Group: clusterIssuerRef.Group,
		})))
	})

	// cert-manager defaults an issuerRef that leaves out the kind or the group, and the anchor read
	// from the chart is defaulted to match. A record taken straight off the Certificate would not be,
	// and the two are compared for equality, so an upgrade would report a rotation of an anchor that
	// never changed.
	t.Run("defaults the kind and group the Certificate left out", func(t *testing.T) {
		g := NewWithT(t)
		partialRef := certmanager.IssuerReference{Name: operatorv1.GlobalRootIssuerName}
		c := fake.NewClientBuilder().
			WithScheme(testScheme(t)).
			WithObjects(
				newPlatformCACertificate(partialRef, readyStatus(true, "")),
				&provisioningv1.DPU{ObjectMeta: metav1.ObjectMeta{Name: "dpu-one", Namespace: testNamespace}},
			).
			Build()
		config := newConfig()

		seeded, err := ensureAnchorRecorded(context.Background(), c, config)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(seeded).To(BeTrue())
		g.Expect(config.Status.Security.CertManagement.Anchor).To(HaveValue(Equal(operatorv1.CertManagementAnchor{
			Name:  operatorv1.GlobalRootIssuerName,
			Kind:  operatorv1.CertManagerIssuerKind,
			Group: operatorv1.CertManagerGroup,
		})))

		rotationRequired, err := detectRotationRequired(context.Background(), c, config, selfSignedRef())
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(rotationRequired).To(BeFalse())
	})

	t.Run("records nothing on a fresh install", func(t *testing.T) {
		g := NewWithT(t)
		c := fake.NewClientBuilder().WithScheme(testScheme(t)).Build()
		config := newConfig()

		seeded, err := ensureAnchorRecorded(context.Background(), c, config)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(seeded).To(BeFalse())
		g.Expect(config.Status.Security).To(BeNil())
	})

	t.Run("leaves a recorded anchor alone", func(t *testing.T) {
		g := NewWithT(t)
		c := fake.NewClientBuilder().
			WithScheme(testScheme(t)).
			WithObjects(newPlatformCACertificate(clusterIssuerRef, readyStatus(true, ""))).
			Build()
		config := newConfig()
		RecordAnchor(config, selfSignedRef())

		seeded, err := ensureAnchorRecorded(context.Background(), c, config)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(seeded).To(BeFalse())
		g.Expect(config.Status.Security.CertManagement.Anchor.Name).To(Equal(selfSignedRef().Name))
	})
}

// TestResolveAnchorState covers the order the three steps have to run in: the anchor of a cluster
// that carries no record is recorded first and reported on its own, because the caller applies the
// platform CA, and so repoints what that record was read from, only on the reconcile after it.
func TestResolveAnchorState(t *testing.T) {
	clusterIssuerRef := externalIssuerRef(operatorv1.CertManagerClusterIssuerKind)
	webhookCA := &unstructured.Unstructured{Object: map[string]interface{}{
		"spec": map[string]interface{}{
			"isCA": true,
			"issuerRef": map[string]interface{}{
				"name":  clusterIssuerRef.Name,
				"kind":  clusterIssuerRef.Kind,
				"group": clusterIssuerRef.Group,
			},
		},
	}}
	webhookCA.SetGroupVersionKind(CertificateGVK)
	webhookCA.SetNamespace(testNamespace)
	webhookCA.SetName(operatorv1.WebhookIntermediateCAName)

	dpu := &provisioningv1.DPU{ObjectMeta: metav1.ObjectMeta{Name: "dpu-one", Namespace: testNamespace}}
	anchoredCluster := []client.Object{
		webhookCA,
		newPlatformCACertificate(selfSignedRef(), readyStatus(true, "")),
		dpu,
	}

	t.Run("records the anchor in effect before reporting on the change of it", func(t *testing.T) {
		g := NewWithT(t)
		c := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(anchoredCluster...).Build()
		config := newConfig()

		state, err := ResolveAnchorState(context.Background(), c, config)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(state.Phase).To(Equal(AnchorPhaseRecorded))
		g.Expect(state.IssuerRef).To(Equal(clusterIssuerRef))
		// Judging the rotation is left to the reconcile that finds this record on the API server.
		g.Expect(state.RotationRequired()).To(BeFalse())
		g.Expect(config.Status.Security.CertManagement.Anchor.Name).To(Equal(selfSignedRef().Name))
	})

	t.Run("reports the rotation once the anchor in effect is recorded", func(t *testing.T) {
		g := NewWithT(t)
		c := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(anchoredCluster...).Build()
		config := newConfig()
		RecordAnchor(config, selfSignedRef())

		state, err := ResolveAnchorState(context.Background(), c, config)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(state.Phase).To(Equal(AnchorPhaseRotationRequired))
		g.Expect(state.RotationRequired()).To(BeTrue())
		g.Expect(conditions.Get(config, operatorv1.CertManagementReadyCondition).Reason).
			To(Equal(string(operatorv1.CertManagementReasonCARotationRequired)))
	})

	t.Run("reports nothing while the recorded anchor is the one in effect", func(t *testing.T) {
		g := NewWithT(t)
		c := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(anchoredCluster...).Build()
		config := newConfig()
		RecordAnchor(config, clusterIssuerRef)

		state, err := ResolveAnchorState(context.Background(), c, config)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(state.Phase).To(Equal(AnchorPhaseRolledOut))
		g.Expect(conditions.Get(config, operatorv1.CertManagementReadyCondition)).To(BeNil())
	})

	// The window between the chart applying the operator and applying the Certificate the anchor is
	// read from. Returning an error there would put every system component, including the ones with
	// nothing to do with the PKI, behind the backoff of a state that clears itself.
	t.Run("waits without failing while the Certificate the anchor is read from is absent", func(t *testing.T) {
		g := NewWithT(t)
		c := fake.NewClientBuilder().WithScheme(testScheme(t)).Build()
		config := newConfig()

		state, err := ResolveAnchorState(context.Background(), c, config)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(state.Phase).To(Equal(AnchorPhasePending))
		g.Expect(state.IssuerRef).To(Equal(certmanager.IssuerReference{}))
		condition := conditions.Get(config, operatorv1.CertManagementReadyCondition)
		g.Expect(condition).NotTo(BeNil())
		g.Expect(condition.Reason).To(Equal(string(conditions.ReasonPending)))
		g.Expect(condition.Message).To(ContainSubstring(operatorv1.WebhookIntermediateCAName))
	})

	// A Certificate the chart produced without an issuerRef is not something that resolves on its
	// own, so it stays an error rather than joining the pending case above.
	t.Run("fails on a Certificate that carries no issuerRef", func(t *testing.T) {
		g := NewWithT(t)
		emptyWebhookCA := &unstructured.Unstructured{Object: map[string]interface{}{"spec": map[string]interface{}{"isCA": true}}}
		emptyWebhookCA.SetGroupVersionKind(CertificateGVK)
		emptyWebhookCA.SetNamespace(testNamespace)
		emptyWebhookCA.SetName(operatorv1.WebhookIntermediateCAName)
		c := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(emptyWebhookCA).Build()
		config := newConfig()

		state, err := ResolveAnchorState(context.Background(), c, config)
		g.Expect(err).To(HaveOccurred())
		// No phase to act on: the error is what the caller has to handle.
		g.Expect(state.Phase).To(BeEmpty())
		g.Expect(conditions.Get(config, operatorv1.CertManagementReadyCondition).Reason).
			To(Equal(string(conditions.ReasonError)))
	})
}

// TestResolveAnchor covers reading the anchor of the whole PKI back from the webhook intermediate
// CA, which is the object the dpf-operator chart stamps its choice of authority onto.
func TestResolveAnchor(t *testing.T) {
	newWebhookCA := func(issuerRef map[string]interface{}) *unstructured.Unstructured {
		webhookCA := &unstructured.Unstructured{Object: map[string]interface{}{
			"spec": map[string]interface{}{"isCA": true},
		}}
		if issuerRef != nil {
			webhookCA.Object["spec"].(map[string]interface{})["issuerRef"] = issuerRef
		}
		webhookCA.SetGroupVersionKind(CertificateGVK)
		webhookCA.SetNamespace(testNamespace)
		webhookCA.SetName(operatorv1.WebhookIntermediateCAName)
		return webhookCA
	}

	tests := []struct {
		name      string
		objs      []client.Object
		want      certmanager.IssuerReference
		wantError string
	}{
		{
			name: "the self-signed root of the chart is read back as the anchor",
			objs: []client.Object{newWebhookCA(map[string]interface{}{
				"name":  operatorv1.GlobalRootIssuerName,
				"kind":  operatorv1.CertManagerIssuerKind,
				"group": operatorv1.CertManagerGroup,
			})},
			want: selfSignedRef(),
		},
		{
			// cert-manager defaults a reference without a kind to a namespaced Issuer, and DPF has to
			// resolve the anchor the same way to report on the issuer the chain is actually built on.
			name: "an anchor without a kind and group is defaulted like cert-manager does",
			objs: []client.Object{newWebhookCA(map[string]interface{}{"name": "openbao-issuer"})},
			want: externalIssuerRef(operatorv1.CertManagerIssuerKind),
		},
		{
			name:      "a chart that has not been installed is reported rather than guessed at",
			wantError: "failed to get Certificate",
		},
		{
			name:      "a webhook intermediate CA without an issuerRef is reported",
			objs:      []client.Object{newWebhookCA(nil)},
			wantError: "has no issuerRef",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			c := fake.NewClientBuilder().
				WithScheme(testScheme(t)).
				WithObjects(tt.objs...).
				Build()

			got, err := ResolveAnchor(context.Background(), c, testNamespace)
			if tt.wantError != "" {
				g.Expect(err).To(HaveOccurred())
				g.Expect(err.Error()).To(ContainSubstring(tt.wantError))
				return
			}
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(got).To(Equal(tt.want))
		})
	}
}
