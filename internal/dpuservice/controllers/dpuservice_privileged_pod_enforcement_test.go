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

package controllers

import (
	"fmt"
	"strings"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("DPUService privileged pod enforcement VAP", func() {
	It("parses the embedded manifest with CRLF line endings", func() {
		crlfManifest := []byte(strings.ReplaceAll(string(rawVAPManifest), "\n", "\r\n"))
		docs, err := readYAMLDocuments(crlfManifest)
		Expect(err).ToNot(HaveOccurred())
		Expect(docs).To(HaveLen(2))

		var policy *admissionregistrationv1.ValidatingAdmissionPolicy
		Expect(decodeYAMLDocument(docs[0], &policy)).To(Succeed())
		Expect(policy.GetName()).To(Equal(privilegedVAPName))

		var binding *admissionregistrationv1.ValidatingAdmissionPolicyBinding
		Expect(decodeYAMLDocument(docs[1], &binding)).To(Succeed())
		Expect(binding.Spec.PolicyName).To(Equal(policy.GetName()))
	})

	It("is accepted by the API server and enforces privileged workload CEL", func() {
		const (
			allowedServiceID = "allowed-service"
			deniedServiceID  = "denied-service"
		)

		By("creating the VAP parameter ConfigMap and scoped namespace")
		applyPrivilegedPodEnforcementPrereqs(map[string]string{
			allowedServiceID: "svc-ns/allowed",
		})

		workloadNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
			GenerateName: "vap-cel-",
			Labels:       map[string]string{NamespaceScopeLabelKey: ""},
		}}
		Expect(testClient.Create(ctx, workloadNS)).To(Succeed())
		DeferCleanup(testClient.Delete, ctx, workloadNS)

		By("applying the ValidatingAdmissionPolicy and binding in Deny mode")
		applyPrivilegedPodEnforcementVAP(admissionregistrationv1.Deny)
		// Reset to a non-enforcing state with an empty allowlist afterwards, without
		// deleting the objects — production never deletes them, and a delete+recreate
		// would trigger the paramRef informer bug these tests must avoid.
		DeferCleanup(func() {
			applyPrivilegedPodEnforcementVAP(admissionregistrationv1.Audit)
			applyPrivilegedPodEnforcementPrereqs(nil)
		})

		// The API server needs some time to observe the ConfigMap, VAP and binding;
		// there is no readiness signal, so probe (dry-run) until the policy both denies
		// a non-allowlisted privileged pod and admits the allowlisted one before
		// running the assertions below.
		By("waiting for the VAP to enforce with the current allowlist")
		Eventually(func() error {
			if err := validatePrivilegedPodEnforcement(ctx, testClient); err != nil {
				return err
			}
			return validateAllowlistedPrivilegedPodAdmission(ctx, testClient, allowedServiceID)
		}).WithTimeout(30 * time.Second).WithPolling(200 * time.Millisecond).Should(Succeed())

		By("allowing unprivileged workloads for a denied service ID")
		Expect(testClient.Create(ctx, testPrivilegedPod(workloadNS.Name, "unprivileged", deniedServiceID, false))).To(Succeed())

		By("allowing privileged workloads for an allowlisted service ID")
		allowedPod := testPrivilegedPod(workloadNS.Name, "allowed", allowedServiceID, true)
		Expect(testClient.Create(ctx, allowedPod)).To(Succeed())
		Expect(client.IgnoreNotFound(testClient.Delete(ctx, allowedPod))).To(Succeed())

		By("rejecting privileged Pods for a non-allowlisted service ID")
		err := testClient.Create(ctx, testPrivilegedPod(workloadNS.Name, "denied-pod", deniedServiceID, true))
		Expect(err).To(HaveOccurred())
		Expect(apierrors.IsInvalid(err)).To(BeTrue(), "expected invalid error, got %v", err)
		Expect(err.Error()).To(ContainSubstring("Privileged containers are not allowed"))

		By("rejecting privileged controller workloads using pod template labels")
		err = testClient.Create(ctx, testPrivilegedDeployment(workloadNS.Name, "denied-deploy", deniedServiceID))
		Expect(err).To(HaveOccurred())
		Expect(apierrors.IsInvalid(err)).To(BeTrue(), "expected invalid error, got %v", err)
		Expect(err.Error()).To(ContainSubstring("Privileged containers are not allowed"))

		By("rejecting privileged CronJobs using pod template labels")
		err = testClient.Create(ctx, testPrivilegedCronJob(workloadNS.Name, "denied-cronjob", deniedServiceID))
		Expect(err).To(HaveOccurred())
		Expect(apierrors.IsInvalid(err)).To(BeTrue(), "expected invalid error, got %v", err)
		Expect(err.Error()).To(ContainSubstring("Privileged containers are not allowed"))

		By("allowing privileged Pods that omit the service-id label")
		unlabeledPod := testPrivilegedPod(workloadNS.Name, "unlabeled", "", true)
		unlabeledPod.Labels = nil
		Expect(testClient.Create(ctx, unlabeledPod)).To(Succeed())
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(testClient.Delete(ctx, unlabeledPod))).To(Succeed())
		})

		By("allowing privileged Deployments where neither the parent nor the pod template carry the service label")
		unlabeledDeploy := testPrivilegedDeployment(workloadNS.Name, "unlabeled-deploy", "")
		unlabeledDeploy.Labels = nil
		unlabeledDeploy.Spec.Template.Labels = map[string]string{"app": "test"}
		Expect(testClient.Create(ctx, unlabeledDeploy)).To(Succeed())
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(testClient.Delete(ctx, unlabeledDeploy))).To(Succeed())
		})

		By("admitting an unprivileged Pod that uses hostNetwork (scope: only securityContext.privileged is gated)")
		hostNetPod := testPrivilegedPod(workloadNS.Name, "host-network", deniedServiceID, false)
		hostNetPod.Spec.HostNetwork = true
		Expect(testClient.Create(ctx, hostNetPod)).To(Succeed())
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(testClient.Delete(ctx, hostNetPod))).To(Succeed())
		})
	})
})

// privilegedPodEnforcementTestFieldOwner is the single server-side-apply field
// owner used for all VAP/binding/ConfigMap mutations in these tests, so repeated
// applies consistently replace previously-owned fields (e.g. emptying the allowlist).
const privilegedPodEnforcementTestFieldOwner = "privileged-pod-enforcement-test"

func applyPrivilegedPodEnforcementPrereqs(data map[string]string) {
	// The probe pods are created in privilegedAllowlistConfigMapNamespace, which must
	// carry NamespaceScopeLabelKey for the VAP's namespaceSelector to match.
	ns := &corev1.Namespace{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Namespace"},
		ObjectMeta: metav1.ObjectMeta{
			Name:   privilegedAllowlistConfigMapNamespace,
			Labels: map[string]string{NamespaceScopeLabelKey: privilegedAllowlistConfigMapNamespace},
		},
	}
	Expect(testClient.Patch(ctx, ns, client.Apply, client.ForceOwnership, client.FieldOwner(privilegedPodEnforcementTestFieldOwner))).To(Succeed())

	cm := buildPrivilegedDPUServiceConfigMap()
	for key, value := range data {
		cm.Data[key] = value
	}
	Expect(testClient.Patch(ctx, cm, client.Apply, client.ForceOwnership, client.FieldOwner(privilegedPodEnforcementTestFieldOwner))).To(Succeed())
}

// applyPrivilegedPodEnforcementVAP creates or updates (server-side apply) the VAP
// and its binding, setting the binding's validationActions to action. It mirrors
// production by never deleting/recreating these objects: enforcement is toggled
// between Deny and Audit by updating the binding in place. A delete+recreate would
// trigger the Kubernetes paramRef informer bug
// (https://github.com/kubernetes/kubernetes/issues/133827) where the recreated
// binding serves a stale allowlist, which made these specs flake under the full suite.
func applyPrivilegedPodEnforcementVAP(action admissionregistrationv1.ValidationAction) {
	policy := privilegedPodsVAP.DeepCopy()
	Expect(testClient.Patch(ctx, policy, client.Apply, client.ForceOwnership, client.FieldOwner(privilegedPodEnforcementTestFieldOwner))).To(Succeed())

	binding := privilegedPodsVAPBinding.DeepCopy()
	binding.Spec.ValidationActions = []admissionregistrationv1.ValidationAction{action}
	Expect(testClient.Patch(ctx, binding, client.Apply, client.ForceOwnership, client.FieldOwner(privilegedPodEnforcementTestFieldOwner))).To(Succeed())
}

func testPrivilegedPod(namespace, name, serviceID string, privileged bool) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-pod", name),
			Namespace: namespace,
			Labels:    map[string]string{dpuservicev1.DPFServiceIDLabelKey: serviceID},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "test",
					Image: "example.com/test:latest",
					SecurityContext: &corev1.SecurityContext{
						Privileged: ptr.To(privileged),
					},
				},
			},
		},
	}
}

func testPrivilegedDeployment(namespace, name, serviceID string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "test"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":                             "test",
						dpuservicev1.DPFServiceIDLabelKey: serviceID,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test",
							Image: "example.com/test:latest",
							SecurityContext: &corev1.SecurityContext{
								Privileged: ptr.To(true),
							},
						},
					},
				},
			},
		},
	}
}

func testPrivilegedCronJob(namespace, name, serviceID string) *batchv1.CronJob {
	return &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: batchv1.CronJobSpec{
			Schedule: "* * * * *",
			JobTemplate: batchv1.JobTemplateSpec{
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app":                             "test",
								dpuservicev1.DPFServiceIDLabelKey: serviceID,
							},
						},
						Spec: corev1.PodSpec{
							RestartPolicy: corev1.RestartPolicyNever,
							Containers: []corev1.Container{
								{
									Name:  "test",
									Image: "example.com/test:latest",
									SecurityContext: &corev1.SecurityContext{
										Privileged: ptr.To(true),
									},
								},
							},
						},
					},
				},
			},
		},
	}
}
