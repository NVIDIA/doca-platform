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

package nvidia

import (
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	testutils "github.com/nvidia/doca-platform/test/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
)

var _ = Describe("Kamaji Handler - Reconciliation Functions", func() {
	var (
		testNS     *corev1.Namespace
		handler    *clusterHandler
		dpuCluster *provisioningv1.DPUCluster
	)

	BeforeEach(func() {
		By("creating the namespace")
		testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "testns-"}}
		Expect(k8sClient.Create(ctx, testNS)).To(Succeed())
		DeferCleanup(k8sClient.Delete, ctx, testNS)

		By("creating handler")
		handler = &clusterHandler{
			Client: k8sClient,
			Scheme: scheme.Scheme,
		}

		By("creating DPUCluster")
		dpuCluster = &provisioningv1.DPUCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: testNS.Name,
			},
			Spec: provisioningv1.DPUClusterSpec{
				Type:     string(provisioningv1.KamajiCluster),
				MaxNodes: 100,
			},
		}
		Expect(k8sClient.Create(ctx, dpuCluster)).To(Succeed())
		DeferCleanup(testutils.CleanupAndWait, ctx, k8sClient, dpuCluster)
	})

	Context("reconcileMonitoringService", func() {
		It("should create metrics Service with correct configuration", func() {
			nodePort := int32(30443)

			By("Reconciling monitoring service")
			err := handler.reconcileMonitoringService(ctx, dpuCluster, nodePort)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying metrics Service is created")
			svc := &corev1.Service{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      dpuCluster.Name + "-metrics",
					Namespace: dpuCluster.Namespace,
				}, svc)
			}).Should(Succeed())

			Expect(svc.Labels["kamaji.clastix.io/name"]).To(Equal(dpuCluster.Name + "-metrics"))
			Expect(svc.Spec.Selector["kamaji.clastix.io/name"]).To(Equal(dpuCluster.Name))
			Expect(svc.Spec.Ports).To(HaveLen(3))
			Expect(svc.Spec.Ports[0].TargetPort).To(Equal(intstr.FromInt32(nodePort)))
		})
	})

	Context("reconcileDeleteMonitoringService", func() {
		It("should delete metrics Service if it exists and is owned by DPUCluster", func() {
			nodePort := int32(30443)

			By("Creating the metrics Service")
			err := handler.reconcileMonitoringService(ctx, dpuCluster, nodePort)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying Service exists")
			svc := &corev1.Service{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      dpuCluster.Name + "-metrics",
					Namespace: dpuCluster.Namespace,
				}, svc)
			}).Should(Succeed())

			By("Deleting the metrics Service")
			err = handler.reconcileDeleteMonitoringService(ctx, dpuCluster)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying Service is deleted")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      dpuCluster.Name + "-metrics",
					Namespace: dpuCluster.Namespace,
				}, svc)
				return apierrors.IsNotFound(err)
			}).Should(BeTrue())
		})

		It("should not delete Service if it is not owned by DPUCluster", func() {
			By("Creating a Service without proper owner reference")
			svc := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      dpuCluster.Name + "-metrics",
					Namespace: dpuCluster.Namespace,
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "apps/v1",
							Kind:       "Deployment",
							Name:       "some-other-owner",
							UID:        "different-uid",
							Controller: ptr.To(true),
						},
					},
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Name:     "test",
							Port:     8080,
							Protocol: corev1.ProtocolTCP,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, svc)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, svc)

			By("Attempting to delete the Service")
			err := handler.reconcileDeleteMonitoringService(ctx, dpuCluster)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying Service still exists")
			existingSvc := &corev1.Service{}
			Consistently(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      dpuCluster.Name + "-metrics",
					Namespace: dpuCluster.Namespace,
				}, existingSvc)
			}).Should(Succeed())
		})
	})

	Context("reconcileDeleteServiceMonitor", func() {
		It("should delete ServiceMonitor if it exists and is owned by DPUCluster", func() {
			By("Creating the ServiceMonitor")
			err := handler.reconcileServiceMonitor(ctx, dpuCluster)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying ServiceMonitor exists")
			sm := &unstructured.Unstructured{}
			sm.SetGroupVersionKind(serviceMonitorGVK)
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      dpuCluster.Name,
					Namespace: dpuCluster.Namespace,
				}, sm)
			}).Should(Succeed())

			By("Deleting the ServiceMonitor")
			err = handler.reconcileDeleteServiceMonitor(ctx, dpuCluster)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying ServiceMonitor is deleted")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      dpuCluster.Name,
					Namespace: dpuCluster.Namespace,
				}, sm)
				return apierrors.IsNotFound(err)
			}).Should(BeTrue())
		})

		It("should not delete ServiceMonitor if it is not owned by DPUCluster", func() {
			By("Creating a ServiceMonitor without proper owner reference")
			sm := &unstructured.Unstructured{}
			sm.SetGroupVersionKind(serviceMonitorGVK)
			sm.SetName(dpuCluster.Name)
			sm.SetNamespace(dpuCluster.Namespace)
			sm.SetOwnerReferences([]metav1.OwnerReference{
				{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       "some-other-owner",
					UID:        "different-uid",
					Controller: ptr.To(true),
				},
			})
			sm.Object["spec"] = map[string]any{
				"selector": map[string]any{
					"matchLabels": map[string]any{
						"test": "label",
					},
				},
				"endpoints": []any{
					map[string]any{
						"port": "metrics",
					},
				},
			}
			Expect(k8sClient.Create(ctx, sm)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, sm)

			By("Attempting to delete the ServiceMonitor")
			err := handler.reconcileDeleteServiceMonitor(ctx, dpuCluster)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying ServiceMonitor still exists")
			existingSM := &unstructured.Unstructured{}
			existingSM.SetGroupVersionKind(serviceMonitorGVK)
			Consistently(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      dpuCluster.Name,
					Namespace: dpuCluster.Namespace,
				}, existingSM)
			}).Should(Succeed())
		})
	})

	Context("reconcileDeleteMonitoring", func() {
		It("should delete both Service and ServiceMonitor", func() {
			nodePort := int32(30443)

			By("Creating the resources")
			err := handler.reconcileMonitoringService(ctx, dpuCluster, nodePort)
			Expect(err).NotTo(HaveOccurred())
			err = handler.reconcileServiceMonitor(ctx, dpuCluster)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying resources exist")
			svc := &corev1.Service{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      dpuCluster.Name + "-metrics",
					Namespace: dpuCluster.Namespace,
				}, svc)
			}).Should(Succeed())

			sm := &unstructured.Unstructured{}
			sm.SetGroupVersionKind(serviceMonitorGVK)
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      dpuCluster.Name,
					Namespace: dpuCluster.Namespace,
				}, sm)
			}).Should(Succeed())

			By("Deleting both resources")
			err = handler.reconcileDeleteMonitoring(ctx, dpuCluster)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying Service is deleted")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      dpuCluster.Name + "-metrics",
					Namespace: dpuCluster.Namespace,
				}, svc)
				return apierrors.IsNotFound(err)
			}).Should(BeTrue())

			By("Verifying ServiceMonitor is deleted")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      dpuCluster.Name,
					Namespace: dpuCluster.Namespace,
				}, sm)
				return apierrors.IsNotFound(err)
			}).Should(BeTrue())
		})
	})
})

var _ = Describe("Kamaji Handler - Helper Functions", func() {
	Context("When calling getMetricsService", func() {
		It("should create a properly configured Service", func() {
			dpuCluster := &provisioningv1.DPUCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-ns",
				},
			}
			nodePort := int32(30443)

			svc := getMetricsService(dpuCluster, nodePort)

			By("Verifying Service metadata")
			Expect(svc.Name).To(Equal("test-cluster-metrics"))
			Expect(svc.Namespace).To(Equal("test-ns"))
			Expect(svc.Labels["kamaji.clastix.io/name"]).To(Equal("test-cluster-metrics"))

			By("Verifying Service selector")
			Expect(svc.Spec.Selector["kamaji.clastix.io/name"]).To(Equal("test-cluster"))

			By("Verifying Service ports")
			Expect(svc.Spec.Ports).To(HaveLen(3))

			expectedPorts := []struct {
				name       string
				port       int32
				targetPort int32
			}{
				{"kube-apiserver-metrics", 6443, nodePort},
				{"kube-controller-manager-metrics", 10257, 10257},
				{"kube-scheduler-metrics", 10259, 10259},
			}

			for i, expected := range expectedPorts {
				Expect(svc.Spec.Ports[i].Name).To(Equal(expected.name))
				Expect(svc.Spec.Ports[i].Port).To(Equal(expected.port))
				Expect(svc.Spec.Ports[i].Protocol).To(Equal(corev1.ProtocolTCP))
				Expect(svc.Spec.Ports[i].TargetPort).To(Equal(intstr.FromInt32(expected.targetPort)))
			}
		})
	})

	Context("When calling getServiceMonitorResource", func() {
		It("should create a properly configured ServiceMonitor", func() {
			dpuCluster := &provisioningv1.DPUCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-ns",
				},
			}

			sm := getServiceMonitorResource(dpuCluster, serviceMonitorGVK)

			By("Verifying ServiceMonitor metadata")
			Expect(sm.GetName()).To(Equal("test-cluster"))
			Expect(sm.GetNamespace()).To(Equal("test-ns"))
			Expect(sm.GetKind()).To(Equal("ServiceMonitor"))
			Expect(sm.GetAPIVersion()).To(Equal("monitoring.coreos.com/v1"))

			By("Verifying ServiceMonitor labels")
			labels := sm.GetLabels()
			Expect(labels["kamaji.clastix.io/name"]).To(Equal("test-cluster-metrics"))
			Expect(labels[provisioningv1.DPUClusterNameLabelKey]).To(Equal("test-cluster"))
			Expect(labels[provisioningv1.DPUClusterNamespaceLabelKey]).To(Equal("test-ns"))

			By("Verifying ServiceMonitor spec")
			spec, found, err := unstructured.NestedMap(sm.Object, "spec")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())

			By("Verifying selector")
			selector, found, err := unstructured.NestedMap(spec, "selector")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())

			matchLabels, found, err := unstructured.NestedStringMap(selector, "matchLabels")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(matchLabels["kamaji.clastix.io/name"]).To(Equal("test-cluster-metrics"))

			By("Verifying endpoints")
			endpoints, found, err := unstructured.NestedSlice(spec, "endpoints")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(endpoints).To(HaveLen(3))

			expectedEndpoints := []struct {
				name             string
				port             string
				relabelingsCount int
			}{
				{"kube-apiserver", "kube-apiserver-metrics", 2},
				{"kube-controller-manager", "kube-controller-manager-metrics", 2},
				{"kube-scheduler", "kube-scheduler-metrics", 2},
			}

			for i, expected := range expectedEndpoints {
				By("Verifying endpoint for " + expected.name)
				endpoint, ok := endpoints[i].(map[string]any)
				Expect(ok).To(BeTrue())
				Expect(endpoint["port"]).To(Equal(expected.port))
				Expect(endpoint["scheme"]).To(Equal("https"))
				Expect(endpoint["interval"]).To(Equal("15s"))
				Expect(endpoint["scrapeTimeout"]).To(Equal("10s"))

				tlsConfig, found, err := unstructured.NestedMap(endpoint, "tlsConfig")
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())
				Expect(tlsConfig["insecureSkipVerify"]).To(BeTrue())

				relabelings, found, err := unstructured.NestedSlice(endpoint, "relabelings")
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())
				Expect(relabelings).To(HaveLen(expected.relabelingsCount))
			}
		})
	})

	Context("When calling hasGVK", func() {
		It("should return true for ServiceMonitor GVK when CRD is installed", func() {
			handler := &clusterHandler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}

			hasGVK := handler.hasGVK(serviceMonitorGVK)
			Expect(hasGVK).To(BeTrue())
		})

		It("should return false for non-existent GVK", func() {
			handler := &clusterHandler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}

			nonExistentGVK := serviceMonitorGVK
			nonExistentGVK.Group = "nonexistent.example.com"
			hasGVK := handler.hasGVK(nonExistentGVK)
			Expect(hasGVK).To(BeFalse())
		})
	})

	DescribeTable("When calling ownedBy",
		func(ownerRefs []metav1.OwnerReference, expected bool) {
			owner := &provisioningv1.DPUCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "owner-cluster",
					Namespace: "test-ns",
					UID:       "owner-uid-123",
				},
			}

			obj := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "service",
					Namespace:       "test-ns",
					OwnerReferences: ownerRefs,
				},
			}

			result := ownedBy(obj, owner)
			Expect(result).To(Equal(expected))
		},
		Entry("should return true when object is owned by the specified owner",
			[]metav1.OwnerReference{
				{
					APIVersion: provisioningv1.GroupVersion.String(),
					Kind:       "DPUCluster",
					Name:       "owner-cluster",
					UID:        "owner-uid-123",
					Controller: ptr.To(true),
				},
			},
			true,
		),
		Entry("should return false when object has no owner references",
			nil,
			false,
		),
		Entry("should return false when object is owned by a different owner",
			[]metav1.OwnerReference{
				{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       "different-owner",
					UID:        "different-uid-456",
					Controller: ptr.To(true),
				},
			},
			false,
		),
		Entry("should return false when controller field is false",
			[]metav1.OwnerReference{
				{
					APIVersion: provisioningv1.GroupVersion.String(),
					Kind:       "DPUCluster",
					Name:       "owner-cluster",
					UID:        "owner-uid-123",
					Controller: ptr.To(false),
				},
			},
			false,
		),
		Entry("should return false when controller field is nil",
			[]metav1.OwnerReference{
				{
					APIVersion: provisioningv1.GroupVersion.String(),
					Kind:       "DPUCluster",
					Name:       "owner-cluster",
					UID:        "owner-uid-123",
					Controller: nil,
				},
			},
			false,
		),
	)
})
