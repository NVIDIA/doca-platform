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
			fieldOwner       = "dpuservice-privileged-pod-enforcement-vap-test"
		)

		By("creating the VAP parameter ConfigMap and scoped namespace")
		paramNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: privilegedAllowlistConfigMapNamespace}}
		err := testClient.Create(ctx, paramNS)
		Expect(client.IgnoreAlreadyExists(err)).To(Succeed())

		paramConfigMap := buildPrivilegedDPUServiceConfigMap()
		paramConfigMap.Data[allowedServiceID] = "svc-ns/allowed"
		Expect(testClient.Patch(ctx, paramConfigMap, client.Apply, client.ForceOwnership, client.FieldOwner(fieldOwner))).To(Succeed())
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(testClient.Delete(ctx, paramConfigMap))).To(Succeed())
		})

		workloadNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
			GenerateName: "vap-cel-",
			Labels:       map[string]string{NamespaceScopeLabelKey: ""},
		}}
		Expect(testClient.Create(ctx, workloadNS)).To(Succeed())
		DeferCleanup(testClient.Delete, ctx, workloadNS)

		By("creating the ValidatingAdmissionPolicy and binding")
		policy := privilegedPodsVAP.DeepCopy()
		Expect(testClient.Patch(ctx, policy, client.Apply, client.ForceOwnership, client.FieldOwner(fieldOwner))).To(Succeed())
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(testClient.Delete(ctx, &admissionregistrationv1.ValidatingAdmissionPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: privilegedVAPName},
			}))).To(Succeed())
		})

		binding := privilegedPodsVAPBinding.DeepCopy()
		Expect(testClient.Patch(ctx, binding, client.Apply, client.ForceOwnership, client.FieldOwner(fieldOwner))).To(Succeed())
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(testClient.Delete(ctx, &admissionregistrationv1.ValidatingAdmissionPolicyBinding{
				ObjectMeta: metav1.ObjectMeta{Name: privilegedVAPName},
			}))).To(Succeed())
		})

		By("allowing unprivileged workloads for a denied service ID")
		Expect(testClient.Create(ctx, testPrivilegedPod(workloadNS.Name, "unprivileged", deniedServiceID, false))).To(Succeed())

		By("allowing privileged workloads for an allowlisted service ID")
		Expect(testClient.Create(ctx, testPrivilegedPod(workloadNS.Name, "allowed", allowedServiceID, true))).To(Succeed())

		By("rejecting privileged Pods for a non-allowlisted service ID")
		Eventually(func(g Gomega) {
			pod := testPrivilegedPod(workloadNS.Name, fmt.Sprintf("denied-%d", time.Now().UnixNano()), deniedServiceID, true)
			err := testClient.Create(ctx, pod)
			if err == nil {
				Expect(client.IgnoreNotFound(testClient.Delete(ctx, pod))).To(Succeed())
			}
			g.Expect(err).To(HaveOccurred())
			g.Expect(apierrors.IsInvalid(err)).To(BeTrue(), "expected invalid error, got %v", err)
			g.Expect(err.Error()).To(ContainSubstring("Privileged containers are not allowed"))
		}).WithTimeout(10 * time.Second).WithPolling(200 * time.Millisecond).Should(Succeed())

		By("rejecting privileged controller workloads using pod template labels")
		Eventually(func(g Gomega) {
			deploy := testPrivilegedDeployment(workloadNS.Name, fmt.Sprintf("denied-%d", time.Now().UnixNano()), deniedServiceID)
			err := testClient.Create(ctx, deploy)
			if err == nil {
				Expect(client.IgnoreNotFound(testClient.Delete(ctx, deploy))).To(Succeed())
			}
			g.Expect(err).To(HaveOccurred())
			g.Expect(apierrors.IsInvalid(err)).To(BeTrue(), "expected invalid error, got %v", err)
			g.Expect(err.Error()).To(ContainSubstring("Privileged containers are not allowed"))
		}).WithTimeout(10 * time.Second).WithPolling(200 * time.Millisecond).Should(Succeed())

		By("rejecting privileged CronJobs using pod template labels")
		Eventually(func(g Gomega) {
			cronJob := testPrivilegedCronJob(workloadNS.Name, fmt.Sprintf("denied-%d", time.Now().UnixNano()), deniedServiceID)
			err := testClient.Create(ctx, cronJob)
			if err == nil {
				Expect(client.IgnoreNotFound(testClient.Delete(ctx, cronJob))).To(Succeed())
			}
			g.Expect(err).To(HaveOccurred())
			g.Expect(apierrors.IsInvalid(err)).To(BeTrue(), "expected invalid error, got %v", err)
			g.Expect(err.Error()).To(ContainSubstring("Privileged containers are not allowed"))
		}).WithTimeout(10 * time.Second).WithPolling(200 * time.Millisecond).Should(Succeed())

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

func testPrivilegedPod(namespace, name, serviceID string, privileged bool) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-pod", name),
			Namespace: namespace,
			Labels:    map[string]string{"svc.dpu.nvidia.com/service": serviceID},
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
						"app":                        "test",
						"svc.dpu.nvidia.com/service": serviceID,
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
								"app":                        "test",
								"svc.dpu.nvidia.com/service": serviceID,
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
