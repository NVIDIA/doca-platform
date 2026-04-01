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

	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	certmanager "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const (
	hostAgentGroup = "system:bootstrappers:dpf:host-agent"
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
	csr.Status.Conditions = append(csr.Status.Conditions, certmanager.CertificateSigningRequestCondition{
		Type:               certmanager.CertificateApproved,
		Status:             corev1.ConditionTrue,
		Reason:             "HostAgentApproved",
		Message:            "approved by controller",
		LastUpdateTime:     metav1.Now(),
		LastTransitionTime: metav1.Now(),
	})
	_, err := r.ClientSet.CertificatesV1().CertificateSigningRequests().UpdateApproval(ctx, csr.Name, csr, metav1.UpdateOptions{})
	if err != nil {
		return ctrl.Result{}, err
	}
	log.Info("CSR approved", "name", csr.Name)
	return ctrl.Result{}, nil
}

func (r *CSRReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&certmanager.CertificateSigningRequest{}, builder.WithPredicates(predicate.NewPredicateFuncs(func(o client.Object) bool {
			csr, ok := o.(*certmanager.CertificateSigningRequest)
			if !ok {
				return false
			}
			for _, group := range csr.Spec.Groups {
				if group == hostAgentGroup || group == cutil.DPUAgentBootstrapGroup {
					return true
				}
			}
			return false
		}))).
		Complete(r)
}
