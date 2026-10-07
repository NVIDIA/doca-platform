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
	"time"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/operator/certmanagement"
	"github.com/nvidia/doca-platform/pkg/certmanager"
	"github.com/nvidia/doca-platform/pkg/conditions"
	"github.com/nvidia/doca-platform/pkg/dpucluster"

	"github.com/fluxcd/pkg/runtime/patch"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

// externalCAIssuerRef references the namespaced issuer that stands in for an enterprise PKI in the
// tests below.
func externalCAIssuerRef() certmanager.IssuerReference {
	return certmanager.IssuerReference{
		Name:  "openbao-issuer",
		Kind:  operatorv1.CertManagerIssuerKind,
		Group: operatorv1.CertManagerGroup,
	}
}

// selfSignedCAIssuerRef is the anchor the DPF Operator chart yields when it was given no authority
// of its own.
func selfSignedCAIssuerRef() certmanager.IssuerReference {
	return certmanager.IssuerReference{
		Name:  operatorv1.GlobalRootIssuerName,
		Kind:  operatorv1.CertManagerIssuerKind,
		Group: operatorv1.CertManagerGroup,
	}
}

// newCACertificate builds a cert-manager Certificate for an intermediate CA anchored to the given
// issuer, named after the Secret it writes as the manifests of DPF name theirs.
func newCACertificate(name string, issuerRef certmanager.IssuerReference, namespace string) *unstructured.Unstructured {
	certificate := &unstructured.Unstructured{Object: map[string]interface{}{
		"spec": map[string]interface{}{
			"isCA":       true,
			"secretName": name,
			"issuerRef": map[string]interface{}{
				"name":  issuerRef.Name,
				"kind":  issuerRef.Kind,
				"group": issuerRef.Group,
			},
		},
	}}
	certificate.SetGroupVersionKind(certmanagement.CertificateGVK)
	certificate.SetNamespace(namespace)
	certificate.SetName(name)
	return certificate
}

// createWebhookIntermediateCA creates the webhook intermediate CA anchored to the self-signed root
// of DPF, which is what the chart creates when it was given no authority of its own. The operator
// reads the anchor of the PKI from this object, so any test that reconciles has to stand it up: the
// chart that owns it does not run in envtest.
func createWebhookIntermediateCA(g Gomega, namespace string) {
	webhookCA := newCACertificate(operatorv1.WebhookIntermediateCAName, selfSignedCAIssuerRef(), namespace)
	g.Expect(testClient.Create(ctx, webhookCA)).To(Succeed())
}

// createReadyIssuer creates a cert-manager Issuer in the given namespace and reports it as ready.
// Neither the chart that owns the self-signed root nor the enterprise PKI behind an external issuer
// runs in envtest, so a test that expects the operator to look past the anchor and on to the chain
// below it has to stand the anchor up itself.
func createReadyIssuer(g Gomega, name, namespace string) {
	issuer := &unstructured.Unstructured{Object: map[string]interface{}{
		"spec": map[string]interface{}{"ca": map[string]interface{}{"secretName": "root-ca"}},
	}}
	issuer.SetGroupVersionKind(certmanagement.IssuerGVK)
	issuer.SetName(name)
	issuer.SetNamespace(namespace)
	g.Expect(testClient.Create(ctx, issuer)).To(Succeed())

	issuer.Object["status"] = map[string]interface{}{
		"conditions": []interface{}{
			map[string]interface{}{
				"type":   string(conditions.TypeReady),
				"status": string(metav1.ConditionTrue),
			},
		},
	}
	g.Expect(testClient.Status().Patch(ctx, issuer, client.Merge)).To(Succeed())
}

// markPlatformIntermediateCAIssued reports the platform intermediate CA the operator applied as
// issued, standing in for cert-manager, which does not run in envtest. It waits for the Certificate
// first: the operator creates it while applying the system components, so it exists only once that
// apply has reached it.
func markPlatformIntermediateCAIssued(g *WithT, namespace string) {
	intermediateCA := &unstructured.Unstructured{}
	intermediateCA.SetGroupVersionKind(certmanagement.CertificateGVK)
	key := client.ObjectKey{Namespace: namespace, Name: operatorv1.PlatformIntermediateCAName}

	g.Eventually(func(g Gomega) {
		g.Expect(testClient.Get(ctx, key, intermediateCA)).To(Succeed())
	}).WithTimeout(10 * time.Second).Should(Succeed())

	intermediateCA.Object["status"] = map[string]interface{}{
		"conditions": []interface{}{
			map[string]interface{}{
				"type":   string(conditions.TypeReady),
				"status": string(metav1.ConditionTrue),
			},
		},
	}
	g.Expect(testClient.Status().Patch(ctx, intermediateCA, client.Merge)).To(Succeed())
}

// TestCertManagementReadyOnceTheChainIsIssued covers the ready path of the condition: the operator
// resolves the anchor of the chart, applies the platform intermediate CA under it, and then reports
// on the chain it asked for rather than on the anchor alone. cert-manager does not run in envtest,
// so issuing that chain is done here in its place, which is what separates the pending report below
// from the successful one after it.
func TestCertManagementReadyOnceTheChainIsIssued(t *testing.T) {
	g := NewWithT(t)

	testNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "certmgmt-ready-"}}
	g.Expect(testClient.Create(ctx, testNS)).To(Succeed())

	// The chart is anchored to the self-signed root it creates itself, both of which are staged here
	// because that chart does not run in envtest.
	createReadyIssuer(g, operatorv1.GlobalRootIssuerName, testNS.Name)
	createWebhookIntermediateCA(g, testNS.Name)

	config := newPausedConfig(testNS.Name)
	g.Expect(testClient.Create(ctx, config)).To(Succeed())

	_, err := reconcileConfigOnce(g, config)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(caRotationReason(config)).To(Equal(string(conditions.ReasonPending)))

	// Nothing is recorded as rolled out while the chain is not issued, so a change of authority is
	// still judged against the anchor this reconcile applied once it is.
	markPlatformIntermediateCAIssued(g, testNS.Name)

	_, err = reconcileConfigOnce(g, config)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(conditions.Get(config, operatorv1.CertManagementReadyCondition).Status).To(Equal(metav1.ConditionTrue))
	g.Expect(recordedCAAnchor(config)).To(HaveValue(Equal(operatorv1.CertManagementAnchor{
		Name:  selfSignedCAIssuerRef().Name,
		Kind:  selfSignedCAIssuerRef().Kind,
		Group: selfSignedCAIssuerRef().Group,
	})))
}

// TestCARotationSignalSurvivesTheApply covers what the rotation signal rests on: it is derived from
// the anchor the operator recorded as rolled out, not from the one on the live platform CA
// Certificate, which the apply repoints in the same reconcile that reports the rotation.
//
// The second reconcile is what makes the signal worth anything. The Certificate now carries the new
// anchor, so there is no difference left in the cluster to observe, and the rotation is still
// reported because the recorded anchor is deliberately left on the previous one while the peers
// provisioned under it have yet to be brought over.
func TestCARotationSignalSurvivesTheApply(t *testing.T) {
	g := NewWithT(t)

	testNS, intermediateCA := newCARotationFixture(t, g, "certmgmt-rotation-")

	config := newPausedConfig(testNS.Name)
	g.Expect(testClient.Create(ctx, config)).To(Succeed())

	// The first reconcile only records what the PKI is anchored to today and hands the apply to the
	// next one, so that the record is on the API server before the Certificate it was read from is
	// repointed.
	_, err := reconcileConfigOnce(g, config)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(recordedCAAnchor(config)).To(HaveValue(Equal(operatorv1.CertManagementAnchor{
		Name:  selfSignedCAIssuerRef().Name,
		Kind:  selfSignedCAIssuerRef().Kind,
		Group: selfSignedCAIssuerRef().Group,
	})))

	_, err = reconcileConfigOnce(g, config)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(caRotationReason(config)).To(Equal(string(operatorv1.CertManagementReasonCARotationRequired)))
	// Not advanced to the new anchor: a rotation is the one case where the apply succeeding is not
	// enough to call it rolled out.
	g.Expect(recordedCAAnchor(config)).To(HaveValue(Equal(operatorv1.CertManagementAnchor{
		Name:  selfSignedCAIssuerRef().Name,
		Kind:  selfSignedCAIssuerRef().Kind,
		Group: selfSignedCAIssuerRef().Group,
	})))

	// The apply has repointed the Certificate, so there is no longer a difference to observe.
	g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(intermediateCA), intermediateCA)).To(Succeed())
	currentRef, found, err := unstructured.NestedStringMap(intermediateCA.Object, "spec", "issuerRef")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(found).To(BeTrue())
	g.Expect(currentRef["name"]).To(Equal(externalCAIssuerRef().Name))

	_, err = reconcileConfigOnce(g, config)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(caRotationReason(config)).To(Equal(string(operatorv1.CertManagementReasonCARotationRequired)))
}

// TestCARotationTowardsAMissingAnchorReportsTheAnchor covers the one state a reported rotation must
// not hide: an anchor that does not exist.
//
// A rotation is reported ahead of the chain below it, which is reissued under the new anchor and
// comes back ready on its own. An anchor that is absent is never going to reissue anything, so the
// rotation it is holding up cannot complete either, and reporting only the rotation would leave the
// condition on CARotationRequired indefinitely with nothing naming the misspelled issuer behind it.
func TestCARotationTowardsAMissingAnchorReportsTheAnchor(t *testing.T) {
	g := NewWithT(t)

	testNS, _ := newCARotationFixture(t, g, "certmgmt-rotation-missing-")

	// Re-anchored to an issuer nobody created, which is what a typo in the value of the chart looks
	// like from here.
	reanchorWebhookCA(g, testNS.Name, "openbao-issuer-typo")

	config := newPausedConfig(testNS.Name)
	g.Expect(testClient.Create(ctx, config)).To(Succeed())

	// Records the anchor in effect and hands the apply to the next reconcile.
	_, err := reconcileConfigOnce(g, config)
	g.Expect(err).NotTo(HaveOccurred())

	_, err = reconcileConfigOnce(g, config)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(caRotationReason(config)).To(Equal(string(operatorv1.CertManagementReasonIssuerNotFound)))
	g.Expect(conditions.Get(config, operatorv1.CertManagementReadyCondition).Message).
		To(ContainSubstring("openbao-issuer-typo"))
}

// TestCARotationSignalSurvivesAMissingAnchor covers a rotation that falls into a failure state
// while it is still outstanding. The condition cannot carry it across one: IssuerNotFound takes the
// place of CARotationRequired on it, so a rotation read back from the condition is one the operator
// forgets the moment the anchor it is rotating towards is misspelled.
//
// It is derived from the anchor recorded as rolled out instead, which no failure state writes,
// because the record is reached only on the reconciles that report no rotation. The sequence below
// is what holds the two apart: a rotation, a failure over it, and the same rotation reported again
// once that failure clears, with nothing in between having re-entered it.
func TestCARotationSignalSurvivesAMissingAnchor(t *testing.T) {
	g := NewWithT(t)

	testNS, _ := newCARotationFixture(t, g, "certmgmt-rotation-lost-anchor-")

	config := newPausedConfig(testNS.Name)
	g.Expect(testClient.Create(ctx, config)).To(Succeed())

	// Records the anchor in effect and hands the apply to the next reconcile, which is the one that
	// reports the rotation.
	_, err := reconcileConfigOnce(g, config)
	g.Expect(err).NotTo(HaveOccurred())

	_, err = reconcileConfigOnce(g, config)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(caRotationReason(config)).To(Equal(string(operatorv1.CertManagementReasonCARotationRequired)))

	// The outstanding rotation is retargeted at an issuer nobody created, so the condition leaves
	// CARotationRequired for the misconfiguration that has to be named ahead of it.
	reanchorWebhookCA(g, testNS.Name, "openbao-issuer-typo")

	_, err = reconcileConfigOnce(g, config)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(caRotationReason(config)).To(Equal(string(operatorv1.CertManagementReasonIssuerNotFound)))

	// Read back from the API server rather than from the object above, since what the rotation has
	// to survive on is what was persisted: a different pod resuming after the failure has only this.
	persisted := &operatorv1.DPFOperatorConfig{}
	g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(config), persisted)).To(Succeed())
	g.Expect(recordedCAAnchor(persisted)).To(HaveValue(Equal(operatorv1.CertManagementAnchor{
		Name:  selfSignedCAIssuerRef().Name,
		Kind:  selfSignedCAIssuerRef().Kind,
		Group: selfSignedCAIssuerRef().Group,
	})))

	// Corrected to the issuer that does exist, and the rotation nobody re-entered is reported again
	// because the record it is measured against never moved.
	reanchorWebhookCA(g, testNS.Name, externalCAIssuerRef().Name)

	_, err = reconcileConfigOnce(g, persisted)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(caRotationReason(persisted)).To(Equal(string(operatorv1.CertManagementReasonCARotationRequired)))
	g.Expect(recordedCAAnchor(persisted)).To(HaveValue(Equal(operatorv1.CertManagementAnchor{
		Name:  selfSignedCAIssuerRef().Name,
		Kind:  selfSignedCAIssuerRef().Kind,
		Group: selfSignedCAIssuerRef().Group,
	})))
}

// TestCARotationSignalSurvivesAFailedApply covers the re-entrancy of the above. The apply can
// repoint the platform CA Certificate and still fail, on a component that has nothing to do with
// the PKI, which makes the reconcile return before it reports on the chain. Every reconcile after
// that one finds a cluster whose anchor already matches, so a rotation derived from the cluster
// rather than from what the operator recorded is a rotation it forgets.
//
// The apply is failed by pointing the opentelemetry-collector at a CA Secret that does not exist,
// which is resolved up front but only returned as an error once the rest of the components, the
// provisioning one among them, have been applied.
func TestCARotationSignalSurvivesAFailedApply(t *testing.T) {
	g := NewWithT(t)

	testNS, intermediateCA := newCARotationFixture(t, g, "certmgmt-rotation-failed-")

	config := newPausedConfig(testNS.Name)
	config.Spec.Monitoring = &operatorv1.MonitoringConfiguration{
		OpenTelemetryCollector: &operatorv1.OpenTelemetryCollectorConfiguration{
			Logging: &operatorv1.OpenTelemetryCollectorLoggingConfiguration{
				Endpoint:    "https://collector.example.com:4318",
				CASecretRef: &operatorv1.OpenTelemetryCollectorCASecretReference{Name: "absent-ca"},
			},
		},
	}
	g.Expect(testClient.Create(ctx, config)).To(Succeed())

	// Records the anchor in effect and leaves the apply to the reconcile below, which is the one that
	// has to fail with the rotation already reported.
	_, err := reconcileConfigOnce(g, config)
	g.Expect(err).NotTo(HaveOccurred())

	_, err = reconcileConfigOnce(g, config)
	g.Expect(err).To(HaveOccurred())
	g.Expect(caRotationReason(config)).To(Equal(string(operatorv1.CertManagementReasonCARotationRequired)))

	// Asserted so the test keeps covering re-entrancy rather than a failure that happened to abort
	// the apply first: the Certificate is repointed, which is what leaves the next reconcile nothing
	// to observe.
	g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(intermediateCA), intermediateCA)).To(Succeed())
	currentRef, found, err := unstructured.NestedStringMap(intermediateCA.Object, "spec", "issuerRef")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(found).To(BeTrue())
	g.Expect(currentRef["name"]).To(Equal(externalCAIssuerRef().Name))

	// Read back from the cluster rather than from the object above, since a reconcile that fails
	// after the apply is exactly the one whose record has to be on the API server for the next one,
	// or a different pod, to pick the rotation up again.
	persisted := &operatorv1.DPFOperatorConfig{}
	g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(config), persisted)).To(Succeed())
	g.Expect(caRotationReason(persisted)).To(Equal(string(operatorv1.CertManagementReasonCARotationRequired)))
	g.Expect(recordedCAAnchor(persisted)).To(HaveValue(Equal(operatorv1.CertManagementAnchor{
		Name:  selfSignedCAIssuerRef().Name,
		Kind:  selfSignedCAIssuerRef().Kind,
		Group: selfSignedCAIssuerRef().Group,
	})))

	_, err = reconcileConfigOnce(g, persisted)
	g.Expect(err).To(HaveOccurred())
	g.Expect(caRotationReason(persisted)).To(Equal(string(operatorv1.CertManagementReasonCARotationRequired)))
}

// reconcileConfigOnce drives one reconcile of the given config and then patches it, the way
// Reconcile does with its deferred patch, so that what the reconcile recorded is persisted even when
// it failed partway through.
func reconcileConfigOnce(g Gomega, config *operatorv1.DPFOperatorConfig) (ctrl.Result, error) {
	patcher := patch.NewSerialPatcher(config, testClient)
	conditions.EnsureConditions(config, operatorv1.Conditions)
	result, err := reconciler.reconcile(ctx, config, []*dpucluster.Config{})
	g.Expect(patcher.Patch(ctx, config,
		patch.WithFieldOwner(dpfOperatorConfigControllerName),
		patch.WithOwnedConditions{Conditions: conditions.TypesAsStrings(operatorv1.Conditions)},
	)).To(Succeed())
	return result, err
}

// TestAnchorCertificateEnqueuesOnlyTheAnchor covers the watch that makes a change of authority
// observable at all. The chart repoints one Certificate and leaves everything else the controller
// watches untouched, so an event for that one has to reach the config, while the certificates DPF
// issues below the anchor must not reconcile the whole system every time cert-manager renews one.
func TestAnchorCertificateEnqueuesOnlyTheAnchor(t *testing.T) {
	g := NewWithT(t)

	namespace := DefaultDPFOperatorConfigSingletonNamespace
	watch := isAnchorCertificate()

	anchor := newCACertificate(operatorv1.WebhookIntermediateCAName, selfSignedCAIssuerRef(), namespace)
	g.Expect(watch.Update(event.UpdateEvent{ObjectOld: anchor, ObjectNew: anchor})).To(BeTrue())
	g.Expect(watch.Create(event.CreateEvent{Object: anchor})).To(BeTrue())

	below := newCACertificate(operatorv1.PlatformIntermediateCAName, selfSignedCAIssuerRef(), namespace)
	g.Expect(watch.Update(event.UpdateEvent{ObjectOld: below, ObjectNew: below})).To(BeFalse())
	g.Expect(watch.Create(event.CreateEvent{Object: below})).To(BeFalse())
}

// reanchorWebhookCA repoints the Certificate the anchor of the PKI is read from, which is what the
// chart does when certificateAuthority.issuerRef is changed. That chart does not run in envtest, so
// the edit it would make is applied here.
func reanchorWebhookCA(g Gomega, namespace, issuerName string) {
	webhookCA := &unstructured.Unstructured{}
	webhookCA.SetGroupVersionKind(certmanagement.CertificateGVK)
	g.Expect(testClient.Get(ctx,
		client.ObjectKey{Namespace: namespace, Name: operatorv1.WebhookIntermediateCAName}, webhookCA)).To(Succeed())
	g.Expect(unstructured.SetNestedField(webhookCA.Object, issuerName, "spec", "issuerRef", "name")).To(Succeed())
	g.Expect(testClient.Update(ctx, webhookCA)).To(Succeed())
}

// recordedCAAnchor returns the anchor recorded as rolled out on the config, or nil when none is.
func recordedCAAnchor(config *operatorv1.DPFOperatorConfig) *operatorv1.CertManagementAnchor {
	if config.Status.Security == nil || config.Status.Security.CertManagement == nil {
		return nil
	}
	return config.Status.Security.CertManagement.Anchor
}

// caRotationReason returns the reason on CertManagementReadyCondition, or the empty string when the
// condition is absent.
func caRotationReason(config *operatorv1.DPFOperatorConfig) string {
	condition := conditions.Get(config, operatorv1.CertManagementReadyCondition)
	if condition == nil {
		return ""
	}
	return condition.Reason
}

// newCARotationFixture stages a cluster that is anchored to the self-signed root and has a DPU
// provisioned under it, which is what turns a re-anchor into a rotation: the DPU trusts the chain of
// the previous anchor through a BMC truststore DPF cannot update. It returns the namespace and the
// platform CA Certificate holding the anchor in effect.
func newCARotationFixture(t *testing.T, g *WithT, namespacePrefix string) (*corev1.Namespace, *unstructured.Unstructured) {
	t.Helper()

	testNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: namespacePrefix}}
	g.Expect(testClient.Create(ctx, testNS)).To(Succeed())

	dpu := &provisioningv1.DPU{
		ObjectMeta: metav1.ObjectMeta{Name: "dpu-one", Namespace: testNS.Name},
		Spec: provisioningv1.DPUSpec{
			SerialNumber:  "MT25066004C7",
			DPUNodeName:   "rotation-node",
			DPUDeviceName: "rotation-device",
			DPUFlavor:     "rotation-flavor",
			BFB:           ptr.To("rotation-bfb"),
			NodeEffect:    provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}},
		},
	}
	g.Expect(testClient.Create(ctx, dpu)).To(Succeed())
	// Deleted rather than left to the namespace, because the rotation detection lists DPUs across all
	// of them, so one left behind here decides the outcome of the next test.
	t.Cleanup(func() {
		g.Expect(client.IgnoreNotFound(testClient.Delete(ctx, dpu))).To(Succeed())
	})

	intermediateCA := newCACertificate(operatorv1.PlatformIntermediateCAName, selfSignedCAIssuerRef(), testNS.Name)
	g.Expect(testClient.Create(ctx, intermediateCA)).To(Succeed())

	// The chart is anchored to an external issuer while the platform CA still carries the previous
	// self-signed anchor, which is the re-anchor the signal is about. The chart does not run in
	// envtest, so the object the anchor is read from is staged here, and so is the issuer it names:
	// a rotation towards an anchor that does not exist reports that anchor instead, which is the
	// misconfiguration TestCARotationTowardsAMissingAnchorReportsTheAnchor covers.
	createReadyIssuer(g, externalCAIssuerRef().Name, testNS.Name)
	webhookCA := newCACertificate(operatorv1.WebhookIntermediateCAName, externalCAIssuerRef(), testNS.Name)
	g.Expect(testClient.Create(ctx, webhookCA)).To(Succeed())

	return testNS, intermediateCA
}

// newPausedConfig is a config the tests above reconcile by hand.
//
// It is paused so that the reconciler the suite runs leaves it alone, since reconcile is called
// directly and what these tests assert on is the outcome of a specific reconcile. It is still meant
// to be created rather than assembled in memory, so that it carries the defaults the manifests it
// renders are validated against.
func newPausedConfig(namespace string) *operatorv1.DPFOperatorConfig {
	return &operatorv1.DPFOperatorConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "config", Namespace: namespace},
		Spec: operatorv1.DPFOperatorConfigSpec{
			DeploymentMode: operatorv1.DeploymentModeHostTrusted,
			Overrides:      &operatorv1.Overrides{Paused: ptr.To(true)},
			ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{
				BFBPersistentVolumeClaimName: ptr.To("rotation-pvc"),
			},
		},
	}
}
