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

package csr

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	hostutil "github.com/nvidia/doca-platform/internal/provisioning/hostagent/util"
	providentity "github.com/nvidia/doca-platform/internal/provisioning/utils/certificate/identity"

	certmanager "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const (
	hostAgentBootstrapGroup = "system:bootstrappers:dpf:host-agent"
	dpuAgentApprovedReason  = "DPUAgentApproved"
	hostAgentApprovedReason = "HostAgentApproved"
)

type CSRReconciler struct {
	RuntimeClient client.Client
	ClientSet     *clientset.Clientset
}

// +kubebuilder:rbac:groups=certificates.k8s.io,resources=certificatesigningrequests,verbs=get;list;watch
// +kubebuilder:rbac:groups=certificates.k8s.io,resources=certificatesigningrequests/approval;certificatesigningrequests/status,verbs=update
// +kubebuilder:rbac:groups=certificates.k8s.io,resources=signers,resourceNames="kubernetes.io/kube-apiserver-client",verbs=approve

func (r *CSRReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx)

	csr := &certmanager.CertificateSigningRequest{}
	if err := r.RuntimeClient.Get(ctx, req.NamespacedName, csr); err != nil {
		return ctrl.Result{}, err
	}
	needApprove := true
	var approvalStatus certmanager.RequestConditionType
	for _, cond := range csr.Status.Conditions {
		switch cond.Type {
		case certmanager.CertificateApproved, certmanager.CertificateDenied:
			needApprove = false
			approvalStatus = cond.Type
		}
	}
	if !needApprove {
		log.V(3).Info("CSR already approved or denied, skipped", "name", csr.Name, "approval status", approvalStatus)
		return ctrl.Result{}, nil
	}

	approvalReason := hostAgentApprovedReason
	approvalLogMessage := "Host agent CSR approved"
	if isDPUAgentCSR(csr) {
		if err := r.validateDPUAgentCSR(ctx, csr); err != nil {
			var validationErr *csrValidationError
			if errors.As(err, &validationErr) {
				// Keep invalid DPU agent CSRs pending so the certificate manager does
				// not immediately create a replacement CSR and flood the cluster.
				log.Info("DPU agent CSR validation failed, leaving pending", "name", csr.Name, "error", validationErr.Error())
				return ctrl.Result{}, nil
			}
			return ctrl.Result{}, err
		}
		approvalReason = dpuAgentApprovedReason
		approvalLogMessage = "DPU agent CSR approved"
	}

	if err := r.updateApprovalCondition(ctx, csr, certmanager.CertificateApproved, approvalReason, "approved by controller"); err != nil {
		return ctrl.Result{}, err
	}
	log.Info(approvalLogMessage, "name", csr.Name)
	return ctrl.Result{}, nil
}

func (r *CSRReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&certmanager.CertificateSigningRequest{}, builder.WithPredicates(predicate.NewPredicateFuncs(shouldProcessCSRObject))).
		Complete(r)
}

func shouldProcessCSRObject(o client.Object) bool {
	csr, ok := o.(*certmanager.CertificateSigningRequest)
	if !ok {
		return false
	}
	for _, group := range csr.Spec.Groups {
		if group == hostAgentBootstrapGroup || group == cutil.DPUAgentBootstrapGroup || group == providentity.DPUAgentOrganization {
			return true
		}
	}
	return false
}

type csrValidationError struct {
	message string
}

func (e *csrValidationError) Error() string {
	return e.message
}

func newCSRValidationError(format string, args ...any) error {
	return &csrValidationError{
		message: fmt.Sprintf(format, args...),
	}
}

func isDPUAgentCSR(csr *certmanager.CertificateSigningRequest) bool {
	for _, group := range csr.Spec.Groups {
		if group == cutil.DPUAgentBootstrapGroup || group == providentity.DPUAgentOrganization {
			return true
		}
	}
	return false
}

func (r *CSRReconciler) validateDPUAgentCSR(ctx context.Context, csr *certmanager.CertificateSigningRequest) error {
	if csr.Spec.SignerName != certmanager.KubeAPIServerClientSignerName {
		return newCSRValidationError("unsupported signer %q", csr.Spec.SignerName)
	}

	block, _ := pem.Decode(csr.Spec.Request)
	if block == nil {
		return newCSRValidationError("invalid CSR PEM payload")
	}

	request, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return newCSRValidationError("parsing CSR request: %v", err)
	}
	if err := request.CheckSignature(); err != nil {
		return newCSRValidationError("invalid CSR signature: %v", err)
	}

	dpuName, ok := providentity.DPUNameFromAgentUsername(request.Subject.CommonName)
	if !ok {
		return newCSRValidationError("common name %q must match da-{dpu-name}", request.Subject.CommonName)
	}
	if len(request.Subject.Organization) != 1 || request.Subject.Organization[0] != providentity.DPUAgentOrganization {
		return newCSRValidationError("organization must be %q", providentity.DPUAgentOrganization)
	}

	dpu := &provisioningv1.DPU{}
	if err := r.RuntimeClient.Get(ctx, client.ObjectKey{Namespace: hostutil.DPFNamespace, Name: dpuName}, dpu); err != nil {
		if apierrors.IsNotFound(err) {
			return newCSRValidationError("DPU %s/%s not found", hostutil.DPFNamespace, dpuName)
		}
		return fmt.Errorf("getting DPU %s/%s: %w", hostutil.DPFNamespace, dpuName, err)
	}

	return nil
}

func (r *CSRReconciler) updateApprovalCondition(
	ctx context.Context,
	csr *certmanager.CertificateSigningRequest,
	conditionType certmanager.RequestConditionType,
	reason string,
	message string,
) error {
	csr = csr.DeepCopy()
	csr.Status.Conditions = append(csr.Status.Conditions, certmanager.CertificateSigningRequestCondition{
		Type:               conditionType,
		Status:             corev1.ConditionTrue,
		Reason:             reason,
		Message:            message,
		LastUpdateTime:     metav1.Now(),
		LastTransitionTime: metav1.Now(),
	})
	_, err := r.ClientSet.CertificatesV1().CertificateSigningRequests().UpdateApproval(ctx, csr.Name, csr, metav1.UpdateOptions{})
	return err
}
