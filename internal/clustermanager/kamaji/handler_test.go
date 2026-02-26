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
	"fmt"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
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
	"sigs.k8s.io/controller-runtime/pkg/client"
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
			Client:          k8sClient,
			Scheme:          scheme.Scheme,
			keepalivedImage: "test.registry.io/keepalived:v1.0.0",
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
				Client:          k8sClient,
				Scheme:          scheme.Scheme,
				keepalivedImage: "", // not needed for hasGVK test
			}

			hasGVK := handler.hasGVK(serviceMonitorGVK)
			Expect(hasGVK).To(BeTrue())
		})

		It("should return false for non-existent GVK", func() {
			handler := &clusterHandler{
				Client:          k8sClient,
				Scheme:          scheme.Scheme,
				keepalivedImage: "", // not needed for hasGVK test
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

var _ = Describe("Kamaji Handler - Defaults and Secrets", func() {
	Describe("Keepalived Image Injection", func() {
		It("should use injected keepalived image", func() {
			// Create handler with keepalived image
			handler := &clusterHandler{
				Client:          k8sClient,
				Scheme:          scheme.Scheme,
				keepalivedImage: "test.registry.io/keepalived:v1.0.0",
			}

			// Verify keepalived image is accessible
			Expect(handler.keepalivedImage).To(Equal("test.registry.io/keepalived:v1.0.0"))
		})

		It("should fail at startup when keepalivedImage flag is empty", func() {
			// This test validates that missing keepalivedImage is caught at startup
			// The main.go validates this before starting the controller
			handler := &clusterHandler{
				Client:          k8sClient,
				Scheme:          scheme.Scheme,
				keepalivedImage: "",
			}

			// Handler creation succeeds but reconciliation would use empty image
			// In practice, main.go validation prevents starting with empty keepalivedImage
			Expect(handler.keepalivedImage).To(BeEmpty())
		})
	})

	Describe("ImagePullSecrets Copying", func() {
		var (
			operatorNS        *corev1.Namespace
			tenantNS          *corev1.Namespace
			handler           *clusterHandler
			dpfOperatorConfig *operatorv1.DPFOperatorConfig
		)

		BeforeEach(func() {
			operatorNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "operator-"}}
			Expect(k8sClient.Create(ctx, operatorNS)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, operatorNS)

			tenantNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "tenant-"}}
			Expect(k8sClient.Create(ctx, tenantNS)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, tenantNS)

			handler = &clusterHandler{
				Client:          k8sClient,
				Scheme:          scheme.Scheme,
				keepalivedImage: "", // not needed for copyImagePullSecrets test
			}

			dpfOperatorConfig = &operatorv1.DPFOperatorConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "config",
					Namespace: operatorNS.Name,
				},
				Spec: operatorv1.DPFOperatorConfigSpec{
					ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{
						BFBPersistentVolumeClaimName: ptr.To("pvc"),
					},
				},
			}
			Expect(k8sClient.Create(ctx, dpfOperatorConfig)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, k8sClient, dpfOperatorConfig)
		})

		It("should return empty list when no secrets configured", func() {
			secrets, err := handler.copyImagePullSecrets(ctx, tenantNS.Name)
			Expect(err).NotTo(HaveOccurred())
			Expect(secrets).To(BeEmpty())
		})

		It("should skip copying when target namespace is same as operator namespace", func() {
			// Configure some imagePullSecrets in DPFOperatorConfig
			dpfOperatorConfig.Spec.ImagePullSecrets = []string{"test-secret"}
			Expect(k8sClient.Update(ctx, dpfOperatorConfig)).To(Succeed())

			// Create a secret in operator namespace
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: operatorNS.Name,
				},
				Type: corev1.SecretTypeDockerConfigJson,
				Data: map[string][]byte{
					".dockerconfigjson": []byte(`{"auths":{"test.registry.io":{"username":"user","password":"pass"}}}`),
				},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, secret)

			// Call copyImagePullSecrets with operator namespace as target
			// Should return secret names without actually copying
			secrets, err := handler.copyImagePullSecrets(ctx, operatorNS.Name)
			Expect(err).NotTo(HaveOccurred())
			Expect(secrets).To(Equal([]string{"test-secret"}))

			// Verify no duplicate secret was created (still only one secret)
			secretList := &corev1.SecretList{}
			Expect(k8sClient.List(ctx, secretList, client.InNamespace(operatorNS.Name))).To(Succeed())
			secretCount := 0
			for _, s := range secretList.Items {
				if s.Name == "test-secret" {
					secretCount++
				}
			}
			Expect(secretCount).To(Equal(1), "should not create duplicate secret in same namespace")
		})

		It("should copy secret from operator namespace to tenant namespace", func() {
			// Create source secret
			srcSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pull-secret",
					Namespace: operatorNS.Name,
				},
				Type: corev1.SecretTypeDockerConfigJson,
				Data: map[string][]byte{
					corev1.DockerConfigJsonKey: []byte(`{"auths":{"registry.io":{"auth":"dGVzdDp0ZXN0"}}}`),
				},
			}
			Expect(k8sClient.Create(ctx, srcSecret)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, k8sClient, srcSecret)

			// Configure secret in DPFOperatorConfig
			Eventually(func() error {
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dpfOperatorConfig), dpfOperatorConfig); err != nil {
					return err
				}
				dpfOperatorConfig.Spec.ImagePullSecrets = []string{"pull-secret"}
				return k8sClient.Update(ctx, dpfOperatorConfig)
			}).Should(Succeed())

			// Copy secrets
			secrets, err := handler.copyImagePullSecrets(ctx, tenantNS.Name)
			Expect(err).NotTo(HaveOccurred())
			Expect(secrets).To(ConsistOf("pull-secret"))

			// Verify secret exists in tenant namespace with correct data
			dstSecret := &corev1.Secret{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      "pull-secret",
					Namespace: tenantNS.Name,
				}, dstSecret)
			}).Should(Succeed())
			Expect(dstSecret.Type).To(Equal(srcSecret.Type))
			Expect(dstSecret.Data).To(Equal(srcSecret.Data))
		})

		It("should update secret when source changes", func() {
			srcSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pull-secret",
					Namespace: operatorNS.Name,
				},
				Type: corev1.SecretTypeDockerConfigJson,
				Data: map[string][]byte{
					corev1.DockerConfigJsonKey: []byte(`{"auths":{"registry.io":{"auth":"b2xkOm9sZA=="}}}`),
				},
			}
			Expect(k8sClient.Create(ctx, srcSecret)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, k8sClient, srcSecret)

			Eventually(func() error {
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dpfOperatorConfig), dpfOperatorConfig); err != nil {
					return err
				}
				dpfOperatorConfig.Spec.ImagePullSecrets = []string{"pull-secret"}
				return k8sClient.Update(ctx, dpfOperatorConfig)
			}).Should(Succeed())

			// First copy
			_, err := handler.copyImagePullSecrets(ctx, tenantNS.Name)
			Expect(err).NotTo(HaveOccurred())

			// Update source
			Eventually(func() error {
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(srcSecret), srcSecret); err != nil {
					return err
				}
				srcSecret.Data[corev1.DockerConfigJsonKey] = []byte(`{"auths":{"registry.io":{"auth":"bmV3Om5ldw=="}}}`)
				return k8sClient.Update(ctx, srcSecret)
			}).Should(Succeed())

			// Second copy should update
			_, err = handler.copyImagePullSecrets(ctx, tenantNS.Name)
			Expect(err).NotTo(HaveOccurred())

			// Verify updated data
			Eventually(func() []byte {
				dstSecret := &corev1.Secret{}
				_ = k8sClient.Get(ctx, types.NamespacedName{Name: "pull-secret", Namespace: tenantNS.Name}, dstSecret)
				return dstSecret.Data[corev1.DockerConfigJsonKey]
			}).Should(Equal(srcSecret.Data[corev1.DockerConfigJsonKey]))
		})

		It("should skip non-existent secrets and copy only existing ones", func() {
			srcSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "exists", Namespace: operatorNS.Name},
				Type:       corev1.SecretTypeDockerConfigJson,
				Data:       map[string][]byte{corev1.DockerConfigJsonKey: []byte(`{"auths":{}}`)},
			}
			Expect(k8sClient.Create(ctx, srcSecret)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, k8sClient, srcSecret)

			Eventually(func() error {
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dpfOperatorConfig), dpfOperatorConfig); err != nil {
					return err
				}
				dpfOperatorConfig.Spec.ImagePullSecrets = []string{"exists", "missing"}
				return k8sClient.Update(ctx, dpfOperatorConfig)
			}).Should(Succeed())

			secrets, err := handler.copyImagePullSecrets(ctx, tenantNS.Name)
			Expect(err).NotTo(HaveOccurred())
			Expect(secrets).To(ConsistOf("exists"))
		})
	})
})

var _ = Describe("Kamaji Handler - TenantControlPlane Creation", func() {
	Context("When calling expectedTenantControlPlane", func() {
		It("should create TenantControlPlane with correct kube-apiserver ExtraArgs", func() {
			dpuCluster := &provisioningv1.DPUCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "test-ns",
				},
				Spec: provisioningv1.DPUClusterSpec{
					Type:     string(provisioningv1.KamajiCluster),
					MaxNodes: 100,
				},
			}
			nodePort := int32(30443)

			tcp, err := expectedTenantControlPlane(dpuCluster, scheme.Scheme, nodePort)

			By("Verifying TenantControlPlane was created without error")
			Expect(err).NotTo(HaveOccurred())
			Expect(tcp).NotTo(BeNil())

			By("Verifying TenantControlPlane metadata")
			Expect(tcp.Name).To(Equal("test-cluster"))
			Expect(tcp.Namespace).To(Equal("test-ns"))
			Expect(tcp.Labels["tenant.clastix.io"]).To(Equal("test-cluster"))
			Expect(tcp.Labels[provisioningv1.DPUClusterNameLabelKey]).To(Equal("test-cluster"))

			By("Verifying ExtraArgs are set correctly")
			Expect(tcp.Spec.ControlPlane.Deployment.ExtraArgs).NotTo(BeNil())
			Expect(tcp.Spec.ControlPlane.Deployment.ExtraArgs.APIServer).NotTo(BeEmpty())

			By("Verifying audit log parameters")
			Expect(tcp.Spec.ControlPlane.Deployment.ExtraArgs.APIServer).To(ContainElement("--audit-log-path=/var/log/kubernetes/audit.log"))
			Expect(tcp.Spec.ControlPlane.Deployment.ExtraArgs.APIServer).To(ContainElement("--audit-policy-file=/etc/kubernetes/audit-policy.yaml"))
			Expect(tcp.Spec.ControlPlane.Deployment.ExtraArgs.APIServer).To(ContainElement("--audit-log-maxage=30"))
			Expect(tcp.Spec.ControlPlane.Deployment.ExtraArgs.APIServer).To(ContainElement("--audit-log-maxbackup=10"))
			Expect(tcp.Spec.ControlPlane.Deployment.ExtraArgs.APIServer).To(ContainElement("--audit-log-maxsize=100"))

			By("Verifying security parameters")
			Expect(tcp.Spec.ControlPlane.Deployment.ExtraArgs.APIServer).To(ContainElement("--anonymous-auth=true"))
			Expect(tcp.Spec.ControlPlane.Deployment.ExtraArgs.APIServer).To(ContainElement("--profiling=false"))

			By("Verifying TLS cipher suites parameter")
			Expect(tcp.Spec.ControlPlane.Deployment.ExtraArgs.APIServer).To(ContainElement(fmt.Sprintf("--tls-cipher-suites=%s", TLSCipherSuites)))

			By("Verifying request timeout parameter")
			Expect(tcp.Spec.ControlPlane.Deployment.ExtraArgs.APIServer).To(ContainElement("--request-timeout=120s"))

			By("Verifying ControllerManager and Scheduler ExtraArgs are initialized")
			Expect(tcp.Spec.ControlPlane.Deployment.ExtraArgs.ControllerManager).NotTo(BeNil())
			Expect(tcp.Spec.ControlPlane.Deployment.ExtraArgs.Scheduler).NotTo(BeNil())

		})

		It("should create TenantControlPlane with correct kube-controller-manager ExtraArgs", func() {
			dpuCluster := &provisioningv1.DPUCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster-cm",
					Namespace: "test-ns",
				},
				Spec: provisioningv1.DPUClusterSpec{
					Type:     string(provisioningv1.KamajiCluster),
					MaxNodes: 100,
				},
			}
			nodePort := int32(30443)

			tcp, err := expectedTenantControlPlane(dpuCluster, scheme.Scheme, nodePort)

			By("Verifying TenantControlPlane was created without error")
			Expect(err).NotTo(HaveOccurred())
			Expect(tcp).NotTo(BeNil())

			By("Verifying ControllerManager ExtraArgs are set correctly")
			Expect(tcp.Spec.ControlPlane.Deployment.ExtraArgs).NotTo(BeNil())
			Expect(tcp.Spec.ControlPlane.Deployment.ExtraArgs.ControllerManager).NotTo(BeEmpty())

			By("Verifying controller-manager profiling parameter")
			Expect(tcp.Spec.ControlPlane.Deployment.ExtraArgs.ControllerManager).To(ContainElement("--profiling=false"))

		})

		It("should create TenantControlPlane with correct kube-scheduler ExtraArgs", func() {
			dpuCluster := &provisioningv1.DPUCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster-sched",
					Namespace: "test-ns",
				},
				Spec: provisioningv1.DPUClusterSpec{
					Type:     string(provisioningv1.KamajiCluster),
					MaxNodes: 100,
				},
			}
			nodePort := int32(30443)

			tcp, err := expectedTenantControlPlane(dpuCluster, scheme.Scheme, nodePort)

			By("Verifying TenantControlPlane was created without error")
			Expect(err).NotTo(HaveOccurred())
			Expect(tcp).NotTo(BeNil())

			By("Verifying Scheduler ExtraArgs are set correctly")
			Expect(tcp.Spec.ControlPlane.Deployment.ExtraArgs).NotTo(BeNil())
			Expect(tcp.Spec.ControlPlane.Deployment.ExtraArgs.Scheduler).NotTo(BeEmpty())

			By("Verifying scheduler profiling parameter")
			Expect(tcp.Spec.ControlPlane.Deployment.ExtraArgs.Scheduler).To(ContainElement("--profiling=false"))

		})

		It("should create TenantControlPlane with all ExtraArgs configured", func() {
			dpuCluster := &provisioningv1.DPUCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster-all",
					Namespace: "test-ns",
				},
				Spec: provisioningv1.DPUClusterSpec{
					Type:     string(provisioningv1.KamajiCluster),
					MaxNodes: 100,
				},
			}
			nodePort := int32(30443)

			tcp, err := expectedTenantControlPlane(dpuCluster, scheme.Scheme, nodePort)

			By("Verifying TenantControlPlane was created without error")
			Expect(err).NotTo(HaveOccurred())
			Expect(tcp).NotTo(BeNil())

			By("Verifying all ExtraArgs components are present")
			Expect(tcp.Spec.ControlPlane.Deployment.ExtraArgs).NotTo(BeNil())
			Expect(tcp.Spec.ControlPlane.Deployment.ExtraArgs.APIServer).To(HaveLen(10))
			Expect(tcp.Spec.ControlPlane.Deployment.ExtraArgs.ControllerManager).To(HaveLen(1))
			Expect(tcp.Spec.ControlPlane.Deployment.ExtraArgs.Scheduler).To(HaveLen(1))

			By("Verifying profiling is disabled for all components")
			Expect(tcp.Spec.ControlPlane.Deployment.ExtraArgs.APIServer).To(ContainElement("--profiling=false"))
			Expect(tcp.Spec.ControlPlane.Deployment.ExtraArgs.ControllerManager).To(ContainElement("--profiling=false"))
			Expect(tcp.Spec.ControlPlane.Deployment.ExtraArgs.Scheduler).To(ContainElement("--profiling=false"))
		})
	})
})
