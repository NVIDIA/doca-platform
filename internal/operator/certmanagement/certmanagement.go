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

// Package certmanagement reports the state of the certificate authority that anchors the
// provisioning PKI on the DPFOperatorConfig, and resolves the anchor the system components are
// applied with.
package certmanagement

import (
	"context"
	"fmt"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/certmanager"
	"github.com/nvidia/doca-platform/pkg/conditions"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// errGetCertificate is shared by every read of the Certificates below. They fail the same way and
// the key in the message is the only thing that distinguishes them, so the wording is kept in one
// place rather than repeated per caller.
const errGetCertificate = "failed to get Certificate %s: %w"

// The cert-manager kinds the operator reads to report on the PKI. They are read as unstructured to
// avoid taking a dependency on the cert-manager API types, as the rest of DPF does.
var (
	CertificateGVK = schema.GroupVersionKind{
		Group:   operatorv1.CertManagerGroup,
		Version: "v1",
		Kind:    "Certificate",
	}
	IssuerGVK = schema.GroupVersionKind{
		Group:   operatorv1.CertManagerGroup,
		Version: "v1",
		Kind:    operatorv1.CertManagerIssuerKind,
	}
	clusterIssuerGVK = schema.GroupVersionKind{
		Group:   operatorv1.CertManagerGroup,
		Version: "v1",
		Kind:    operatorv1.CertManagerClusterIssuerKind,
	}
)

// ResolveAnchor returns the issuer that anchors the whole DPF PKI, read from the webhook
// intermediate CA the dpf-operator chart creates.
//
// The chart stamps its certificateAuthority.issuerRef onto that Certificate, or the issuer over its
// own self-signed root when it was given none. Reading it back is what keeps the platform chain
// anchored to the same authority as the webhook chain: with the choice made once, in the chart, the
// two cannot be configured to disagree. A missing Certificate means the chart is not installed or
// not reconciled yet, which is reported rather than guessed at, since guessing the self-signed root
// is exactly the mistake that would anchor the platform CA to an issuer nobody created.
func ResolveAnchor(ctx context.Context, c client.Client, namespace string) (certmanager.IssuerReference, error) {
	webhookCA := &unstructured.Unstructured{}
	webhookCA.SetGroupVersionKind(CertificateGVK)
	key := client.ObjectKey{Namespace: namespace, Name: operatorv1.WebhookIntermediateCAName}
	if err := c.Get(ctx, key, webhookCA); err != nil {
		return certmanager.IssuerReference{}, fmt.Errorf(errGetCertificate, key, err)
	}

	ref, found, err := unstructured.NestedStringMap(webhookCA.Object, "spec", "issuerRef")
	if err != nil {
		return certmanager.IssuerReference{}, fmt.Errorf("failed to read the issuerRef of Certificate %s: %w", key, err)
	}
	if !found || ref["name"] == "" {
		return certmanager.IssuerReference{}, fmt.Errorf("certificate %s has no issuerRef to anchor the platform CA to", key)
	}

	return certmanager.IssuerReference{
		Name:  ref["name"],
		Kind:  ref["kind"],
		Group: ref["group"],
	}.WithIssuerDefaults(), nil
}

// AnchorPhase is where the authority anchoring the PKI stands, which decides what the caller does
// with this reconcile before it applies anything.
type AnchorPhase string

const (
	// AnchorPhaseRolledOut is the steady state: the authority is resolved and already recorded as what
	// the PKI is rolled out against, so the caller applies the system components under it.
	AnchorPhaseRolledOut AnchorPhase = "RolledOut"

	// AnchorPhasePending is the Certificate the anchor is read from not existing yet, which is the
	// window between the chart applying the operator and applying that Certificate. There is no
	// authority to anchor anything to, so the caller waits, but nothing is misconfigured either.
	AnchorPhasePending AnchorPhase = "Pending"

	// AnchorPhaseRecorded is the authority the PKI is currently rolled out against having had to be
	// recorded on the status first. The caller persists it and applies the system components on its
	// next reconcile, since this apply is what repoints the Certificate it was read from.
	AnchorPhaseRecorded AnchorPhase = "Recorded"

	// AnchorPhaseRotationRequired is anchoring the PKI to the resolved authority while the peers
	// already provisioned trust only the previous one. The platform CA keeps its key and subject
	// across the re-anchor, so nothing it signed stops validating, but a CA rotation is what carries
	// the new authority to those peers before the previous one can be retired.
	AnchorPhaseRotationRequired AnchorPhase = "RotationRequired"
)

// AnchorState is what the operator has to know about the authority of the PKI before it applies the
// system components.
type AnchorState struct {
	// IssuerRef is the authority resolved from the chart, the one the platform CA is applied under.
	// Empty while the Certificate it is read from does not exist.
	IssuerRef certmanager.IssuerReference

	// Phase is where that authority stands. One phase rather than a flag per state, because no two of
	// them can hold at once and the caller acts on exactly one. Empty only alongside an error, where
	// there is no state to act on.
	Phase AnchorPhase
}

// RotationRequired reports whether anchoring the PKI to the resolved authority calls for a CA
// rotation. Read often enough, and in enough places, to be worth naming rather than comparing.
func (s AnchorState) RotationRequired() bool {
	return s.Phase == AnchorPhaseRotationRequired
}

// ResolveAnchorState resolves the authority anchoring the PKI and reports what anchoring the
// platform CA to it means for the peers already provisioned.
//
// The three steps belong together: the rotation is judged against the anchor resolved here, and the
// record it is judged through is read from the same PKI. What each of them reports is set on
// CertManagementReadyCondition, and an unexpected failure in any of them is returned, since nothing
// can be applied without knowing what to anchor it to.
//
// The Certificate the anchor is read from not existing yet is the exception. It is reported as
// pending rather than returned, because it is the ordering of a chart install and not a state anyone
// has to act on, and an error there puts the components that have nothing to do with the PKI behind
// the backoff of a condition that clears itself.
func ResolveAnchorState(ctx context.Context, c client.Client, config *operatorv1.DPFOperatorConfig) (AnchorState, error) {
	issuerRef, err := ResolveAnchor(ctx, c, config.Namespace)
	if apierrors.IsNotFound(err) {
		conditions.AddFalse(
			config,
			operatorv1.CertManagementReadyCondition,
			conditions.ReasonPending,
			conditions.ConditionMessage(fmt.Sprintf(
				"Waiting for the DPF Operator chart to create Certificate %q, which the authority anchoring "+
					"the PKI is read from.", operatorv1.WebhookIntermediateCAName)))
		return AnchorState{Phase: AnchorPhasePending}, nil
	}
	if err != nil {
		setError(config, err)
		return AnchorState{}, err
	}

	// Recording it is the whole reconcile: what was read here is only worth anything once it is on the
	// API server, and it is read from the Certificate the caller is about to repoint.
	recorded, err := ensureAnchorRecorded(ctx, c, config)
	if err != nil {
		setError(config, err)
		return AnchorState{}, err
	}
	if recorded {
		return AnchorState{IssuerRef: issuerRef, Phase: AnchorPhaseRecorded}, nil
	}

	rotationRequired, err := detectRotationRequired(ctx, c, config, issuerRef)
	if err != nil {
		setError(config, err)
		return AnchorState{}, err
	}
	if rotationRequired {
		setRotationRequired(config, issuerRef)
		return AnchorState{IssuerRef: issuerRef, Phase: AnchorPhaseRotationRequired}, nil
	}

	return AnchorState{IssuerRef: issuerRef, Phase: AnchorPhaseRolledOut}, nil
}

// setError reports an unexpected failure of either cert management step on
// CertManagementReadyCondition. Reading the anchor before the apply and inspecting the chain after
// it fail the same way as far as a reader of the status is concerned, so they report it identically.
//
// Every entry point of this package reports its own failures through this, rather than leaving the
// condition to whoever called it, so there is no state a caller has to remember to report.
func setError(config *operatorv1.DPFOperatorConfig, err error) {
	conditions.AddFalse(
		config,
		operatorv1.CertManagementReadyCondition,
		conditions.ReasonError,
		conditions.ConditionMessage(fmt.Sprintf(
			"Certificate management must be reconciled for DPF Operator to continue:\n%v",
			conditions.JoinErrors(err, 1))))
}

// setRotationRequired reports an anchor change that calls for a CA rotation on
// CertManagementReadyCondition.
//
// It reports rather than records: what outlives the reconcile is the anchor recorded in status, which
// detectRotationRequired compares the chosen one against, so the condition can be rebuilt by any
// later reconcile instead of having to survive this one.
func setRotationRequired(config *operatorv1.DPFOperatorConfig, issuerRef certmanager.IssuerReference) {
	conditions.AddFalse(
		config,
		operatorv1.CertManagementReadyCondition,
		operatorv1.CertManagementReasonCARotationRequired,
		conditions.ConditionMessage(fmt.Sprintf(
			"The platform intermediate CA is being re-anchored to %s %q while DPUs are already provisioned. "+
				"It keeps its key and its subject, so the certificates it has already signed stay valid, but the "+
				"peers provisioned under the previous authority trust only that one, and a CA rotation is what "+
				"carries the new authority to them before the previous one is retired.",
			issuerRef.Kind, issuerRef.Name)))
}

// Reconcile reports the state of the certificate authority that anchors the provisioning PKI on
// CertManagementReadyCondition.
//
// It only observes: a chain that is not ready yet, an anchoring issuer that does not exist, or an
// anchor change that calls for a CA rotation are all reported on the condition without stopping the
// reconcile. Nothing else the operator does depends on the chain being ready, and a mode switch is
// deliberately not gated, so failing the reconcile here would only stall unrelated work. An
// unexpected API failure is reported on the condition as well, and returned on top of it, since it
// is worth retrying.
//
// rotationRequired comes from detectRotationRequired, which has to run before the system components
// are applied. The caller reports it as soon as it observes it, so this only has to leave the
// condition alone rather than report it again.
func Reconcile(ctx context.Context, c client.Client, config *operatorv1.DPFOperatorConfig, issuerRef certmanager.IssuerReference, rotationRequired bool) error {
	// The anchor is a prerequisite in either mode rather than something DPF creates, so a missing one
	// is called out separately from a chain that is merely still being issued. Checking it for the
	// self-signed root as well is what distinguishes a chart that was never told to bring a root from
	// a chain cert-manager has not caught up with yet.
	issuer, err := getIssuer(ctx, c, config.Namespace, issuerRef)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			err = fmt.Errorf("failed to get %s %q: %w", issuerRef.Kind, issuerRef.Name, err)
			setError(config, err)
			return err
		}
		conditions.AddFalse(
			config,
			operatorv1.CertManagementReadyCondition,
			operatorv1.CertManagementReasonIssuerNotFound,
			conditions.ConditionMessage(missingIssuerMessage(issuerRef)))
		return nil
	}
	if ready, message := certManagerObjectReady(issuer); !ready {
		conditions.AddFalse(
			config,
			operatorv1.CertManagementReadyCondition,
			conditions.ReasonPending,
			conditions.ConditionMessage(fmt.Sprintf("Waiting for %s %q to become ready%s",
				issuerRef.Kind, issuerRef.Name, formatCertManagerMessage(message))))
		return nil
	}

	// A rotation is reported ahead of the chain below by the caller: that chain is reissued under the
	// new anchor on its own, so it goes ready again within the same rotation and reporting it would
	// only mask the more actionable state with a transient one.
	//
	// The anchor itself is checked first, above, rather than masked the same way. A rotation reported
	// on its own reads as a procedure waiting to be run, but an anchor that does not exist or is not
	// ready is one the chain can never be reissued under, so the rotation would never complete and
	// the condition would sit on CARotationRequired indefinitely with nothing pointing at the reason.
	// That is the state a re-anchor to a misspelled issuer lands in, and it is the anchor that has to
	// be reported for it to be diagnosable at all.
	if rotationRequired {
		return nil
	}

	intermediateCA := &unstructured.Unstructured{}
	intermediateCA.SetGroupVersionKind(CertificateGVK)
	key := client.ObjectKey{Namespace: config.Namespace, Name: operatorv1.PlatformIntermediateCAName}
	if err := c.Get(ctx, key, intermediateCA); err != nil {
		if !apierrors.IsNotFound(err) {
			err = fmt.Errorf(errGetCertificate, key, err)
			setError(config, err)
			return err
		}
		conditions.AddFalse(
			config,
			operatorv1.CertManagementReadyCondition,
			conditions.ReasonPending,
			conditions.ConditionMessage(fmt.Sprintf("Waiting for the platform intermediate CA Certificate %s to be created", key)))
		return nil
	}

	if ready, message := certManagerObjectReady(intermediateCA); !ready {
		// The most common cause in external issuer mode is a PKI backend whose policy refuses to sign a
		// CA certificate, and cert-manager reports that refusal on the condition, so it is passed on.
		conditions.AddFalse(
			config,
			operatorv1.CertManagementReadyCondition,
			conditions.ReasonPending,
			conditions.ConditionMessage(fmt.Sprintf("Waiting for the platform intermediate CA %q to be issued by %s %q%s",
				operatorv1.PlatformIntermediateCAName, issuerRef.Kind, issuerRef.Name, formatCertManagerMessage(message))))
		return nil
	}

	conditions.AddTrue(config, operatorv1.CertManagementReadyCondition)
	return nil
}

// missingIssuerMessage explains a missing anchor in terms of whoever was supposed to create it. An
// external issuer is a prerequisite of the configuration that names it, while the self-signed root
// issuer comes from the DPF Operator chart, which creates none when it is pointed at a certificate
// authority of its own instead. That second case is a configuration the two surfaces disagree on
// rather than anything DPF can resolve, so it is worth naming.
func missingIssuerMessage(issuerRef certmanager.IssuerReference) string {
	if issuerRef.IsExternalIssuer() {
		return fmt.Sprintf("%s %q from certificateAuthority.issuerRef of the DPF Operator chart does not exist. "+
			"It must exist and be ready before DPF can request its intermediate CA through it.",
			issuerRef.Kind, issuerRef.Name)
	}
	return fmt.Sprintf("%s %q does not exist. The DPF Operator chart creates it together with the self-signed "+
		"root it anchors, so a PKI anchored here with the issuer absent means that chart is not installed or "+
		"not reconciled yet.", issuerRef.Kind, issuerRef.Name)
}

// detectRotationRequired reports whether the issuer anchoring the provisioning PKI is about to
// change, or has already changed, on a cluster that has provisioned DPUs. Those DPUs were
// provisioned to trust the chain of the previous anchor and DPF cannot update their BMC truststores,
// so the operator has to drive a CA rotation for them.
//
// It compares the chosen anchor against the one recorded on the status, not against the live
// platform CA Certificate: that Certificate is repointed by the very apply this has to outlive, and
// the apply can still fail afterwards on a component that has nothing to do with the PKI. Comparing
// against what the operator has recorded as rolled out is what keeps such a reconcile, or a restart
// in the middle of one, from retiring a rotation that is still outstanding. It also keeps the steady
// state off the API server, since a Certificate is read as unstructured and those reads are not
// served from the cache of the manager.
//
// It stops reporting when the caller records the new anchor, which it does once the rollout of it is
// no longer outstanding.
func detectRotationRequired(ctx context.Context, c client.Client, config *operatorv1.DPFOperatorConfig, desiredRef certmanager.IssuerReference) (bool, error) {
	recordedRef := recordedAnchor(config)
	// Nothing rolled out yet: a fresh install picks its mode without rotating anything.
	if recordedRef == nil || *recordedRef == desiredRef {
		return false, nil
	}

	// Without provisioned DPUs there is no truststore holding the previous chain, so the anchor can be
	// swapped freely and cert-manager reissuing under it is all that is needed.
	dpus := &provisioningv1.DPUList{}
	if err := c.List(ctx, dpus); err != nil {
		return false, fmt.Errorf("failed to list DPUs: %w", err)
	}
	return len(dpus.Items) > 0, nil
}

// RecordAnchor records the certificate authority the provisioning PKI has been rolled out against.
//
// The caller records it after the apply that anchors the platform CA to it has succeeded, and only
// while no rotation is outstanding, because the difference between this and the chosen anchor is
// precisely what reports that rotation. Recording it during one is what would retire a rotation the
// peers have not caught up with.
func RecordAnchor(config *operatorv1.DPFOperatorConfig, issuerRef certmanager.IssuerReference) {
	if config.Status.Security == nil {
		config.Status.Security = &operatorv1.SecurityStatus{}
	}
	if config.Status.Security.CertManagement == nil {
		config.Status.Security.CertManagement = &operatorv1.CertManagementStatus{}
	}
	config.Status.Security.CertManagement.Anchor = &operatorv1.CertManagementAnchor{
		Name:  issuerRef.Name,
		Kind:  issuerRef.Kind,
		Group: issuerRef.Group,
	}
}

// ensureAnchorRecorded records what the provisioning PKI is anchored to on a config that does not
// carry it yet, reading it from the live platform CA Certificate, and reports whether it did.
//
// This is the one read of that Certificate left, and it exists for the cluster DPF is upgraded onto:
// one that has been anchored for a while, by a version that recorded nothing, where taking the
// absence of a record to mean nothing was rolled out would miss the rotation of an anchor that did
// change. The caller reports it and leaves the apply to its next reconcile, because that apply
// repoints the Certificate this was read from and a reconcile that recorded it only in memory is one
// restart away from having read it for nothing.
//
// It can go once no supported upgrade path starts before the release this lands in, since from then
// on every cluster records the anchor as it rolls it out. That is 27.1 at the earliest, and with it
// AnchorPhaseRecorded and the reconcile that only records.
func ensureAnchorRecorded(ctx context.Context, c client.Client, config *operatorv1.DPFOperatorConfig) (bool, error) {
	if recordedAnchor(config) != nil {
		return false, nil
	}

	intermediateCA := &unstructured.Unstructured{}
	intermediateCA.SetGroupVersionKind(CertificateGVK)
	key := client.ObjectKey{Namespace: config.Namespace, Name: operatorv1.PlatformIntermediateCAName}
	if err := c.Get(ctx, key, intermediateCA); err != nil {
		if apierrors.IsNotFound(err) {
			// A fresh install: nothing is anchored yet, so there is nothing to have rolled out.
			return false, nil
		}
		return false, fmt.Errorf(errGetCertificate, key, err)
	}

	currentRef, found, err := unstructured.NestedStringMap(intermediateCA.Object, "spec", "issuerRef")
	if err != nil {
		return false, fmt.Errorf("failed to read the issuerRef of Certificate %s: %w", key, err)
	}
	if !found || currentRef["name"] == "" {
		return false, nil
	}

	// Defaulted the way the anchor read from the chart is, since the two are compared for equality to
	// detect a re-anchor and cert-manager lets a Certificate leave either field out.
	RecordAnchor(config, certmanager.IssuerReference{
		Name:  currentRef["name"],
		Kind:  currentRef["kind"],
		Group: currentRef["group"],
	}.WithIssuerDefaults())
	return true, nil
}

// recordedAnchor returns the anchor the provisioning PKI has been rolled out against, or nil when
// none has been recorded yet.
func recordedAnchor(config *operatorv1.DPFOperatorConfig) *certmanager.IssuerReference {
	if config.Status.Security == nil {
		return nil
	}
	certManagement := config.Status.Security.CertManagement
	if certManagement == nil || certManagement.Anchor == nil {
		return nil
	}
	return &certmanager.IssuerReference{
		Name:  certManagement.Anchor.Name,
		Kind:  certManagement.Anchor.Kind,
		Group: certManagement.Anchor.Group,
	}
}

// getIssuer reads the referenced issuer, resolving a namespaced Issuer in the namespace of the
// DPFOperatorConfig and a ClusterIssuer cluster wide.
func getIssuer(ctx context.Context, c client.Client, namespace string, ref certmanager.IssuerReference) (*unstructured.Unstructured, error) {
	issuer := &unstructured.Unstructured{}
	key := client.ObjectKey{Name: ref.Name}
	if ref.Kind == operatorv1.CertManagerClusterIssuerKind {
		issuer.SetGroupVersionKind(clusterIssuerGVK)
	} else {
		issuer.SetGroupVersionKind(IssuerGVK)
		key.Namespace = namespace
	}

	if err := c.Get(ctx, key, issuer); err != nil {
		return nil, err
	}
	return issuer, nil
}

// certManagerObjectReady reports whether a cert-manager object carries a Ready condition with status
// True, along with the message on that condition. cert-manager explains why it could not issue a
// certificate there, so the message is worth passing on to whoever reads the DPFOperatorConfig.
func certManagerObjectReady(obj *unstructured.Unstructured) (bool, string) {
	objConditions, found, err := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if err != nil || !found {
		return false, ""
	}

	for _, objCondition := range objConditions {
		condition, ok := objCondition.(map[string]interface{})
		if !ok {
			continue
		}
		if conditionType, ok := condition["type"].(string); !ok || conditionType != string(conditions.TypeReady) {
			continue
		}
		message, _ := condition["message"].(string)
		status, _ := condition["status"].(string)
		return status == string(metav1.ConditionTrue), message
	}
	return false, ""
}

// formatCertManagerMessage renders a cert-manager condition message as a suffix, dropping it when
// cert-manager has not reported one yet.
func formatCertManagerMessage(message string) string {
	if message == "" {
		return ""
	}
	return fmt.Sprintf(": %s", message)
}
