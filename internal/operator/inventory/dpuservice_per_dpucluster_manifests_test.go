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

package inventory

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/release"
	"github.com/nvidia/doca-platform/pkg/dpucluster"

	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

//nolint:goconst
func Test_serviceChainSetControllerObjects_GenerateManifests(t *testing.T) {
	g := NewWithT(t)

	tests := []struct {
		name                              string
		inputYAML                         string
		vars                              Variables
		wantDPUServices                   []dpuservicev1.DPUService
		wantDPUServiceCredentialsRequests []dpuservicev1.DPUServiceCredentialRequest
	}{
		{
			name: "Only in-cluster DPUService is generated if no DPUCluster is created",
			inputYAML: `apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUService
metadata:
  name: servicefunctionchainset-controller
  namespace: dpf-operator-system
spec:
  helmChart:
    source:
      repoURL: oci://example.com
      chart: servicechain-chart
      version: v0.1.0
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceCredentialRequest
metadata:
  name: servicesetcontroller
  namespace: dpf-operator-system
spec:
  duration: 24h
  serviceAccount:
    name: example-serviceaccount-name
    namespace: example-serviceaccount-namespace
  targetCluster:
    name: example-cluster-name 
    namespace: example-cluster-namespace 
  type: tokenFile
  secret:
    name: example-secret-name
    namespace: example-secret-namespace`,
			vars: func() Variables {
				defaults := &release.Defaults{}
				g.Expect(defaults.Parse()).To(Succeed())
				vars := newDefaultVariables(defaults)
				vars.Namespace = "test-namespace"
				vars.HelmCharts[operatorv1.ServiceSetControllerName] = testDPUNetworkingHelmChart
				vars.Images[operatorv1.ServiceSetControllerName.WithContainer(operatorv1.ControllerManagerContainer)] = "example.com/dpf-system:v0.1.0"
				vars.DPUClusters = []*dpucluster.Config{} // No DPU clusters
				return vars
			}(),
			wantDPUServices: []dpuservicev1.DPUService{
				{
					TypeMeta: metav1.TypeMeta{
						Kind:       "DPUService",
						APIVersion: "svc.dpu.nvidia.com/v1alpha1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      operatorv1.ServiceChainSetCRDsName.String(),
						Namespace: "test-namespace",
						Labels: map[string]string{
							operatorv1.DPFComponentLabelKey: operatorv1.ServiceSetControllerName.String(),
							release.DPFVersionLabelKey:      release.DPFVersion(),
						},
					},
					Spec: dpuservicev1.DPUServiceSpec{
						ServiceDaemonSet: &dpuservicev1.ServiceDaemonSetValues{
							Labels: map[string]string{
								operatorv1.DPFComponentLabelKey: operatorv1.ServiceSetControllerName.String(),
							},
						},
						DeployInCluster: ptr.To(false),
						HelmChart: dpuservicev1.HelmChart{
							Source: dpuservicev1.ApplicationSource{
								RepoURL: "oci://example.com",
								Chart:   "dpu-networking",
								Version: "v0.1.0",
							},
							Values: func() *runtime.RawExtension {
								values := map[string]interface{}{
									"servicechainset-controller": map[string]interface{}{
										"controllerManager": map[string]interface{}{
											"manager": map[string]interface{}{
												"image": map[string]interface{}{
													"repository": "example.com/dpf-system",
													"tag":        "v0.1.0",
												},
											},
										},
										"deployDPUManifests": true,
										"enabled":            true,
										"rbac": map[string]interface{}{
											"serviceAccounts": []interface{}{},
										},
									},
								}
								raw, err := json.Marshal(values)
								g.Expect(err).ToNot(HaveOccurred())
								return &runtime.RawExtension{Raw: raw}
							}(),
						},
					},
				},
			},
			wantDPUServiceCredentialsRequests: nil,
		},
		{
			name: "DPUServices and DPUServiceCredentialsRequests are generated for one cluster",
			inputYAML: `apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUService
metadata:
  name: servicefunctionchainset-controller
  namespace: dpf-operator-system
spec:
  helmChart:
    source:
      repoURL: oci://example.com
      chart: servicechain-chart
      version: v0.1.0
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceCredentialRequest
metadata:
  name: servicesetcontroller
  namespace: dpf-operator-system
spec:
  duration: 24h
  serviceAccount:
    name: example-serviceaccount-name
    namespace: example-serviceaccount-namespace
  targetCluster:
    name: example-cluster-name 
    namespace: example-cluster-namespace 
  type: tokenFile
  secret:
    name: example-secret-name
    namespace: example-secret-namespace`,
			vars: func() Variables {
				defaults := &release.Defaults{}
				g.Expect(defaults.Parse()).To(Succeed())
				vars := newDefaultVariables(defaults)
				vars.Namespace = "test-namespace"
				vars.HelmCharts[operatorv1.ServiceSetControllerName] = testDPUNetworkingHelmChart
				vars.Images[operatorv1.ServiceSetControllerName.WithContainer(operatorv1.ControllerManagerContainer)] = "example.com/dpf-system:v0.1.0"
				vars.DPUClusters = []*dpucluster.Config{
					{
						Cluster: &provisioningv1.DPUCluster{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "cluster-1",
								Namespace: "dpf-provisioning-system",
							},
						},
					},
				}
				return vars
			}(),
			wantDPUServices: []dpuservicev1.DPUService{
				{
					TypeMeta: metav1.TypeMeta{
						Kind:       "DPUService",
						APIVersion: "svc.dpu.nvidia.com/v1alpha1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "servicechainset-controller-73923044a5",
						Namespace: "test-namespace",
						Labels: map[string]string{
							operatorv1.DPFComponentLabelKey:            operatorv1.ServiceSetControllerName.String(),
							release.DPFVersionLabelKey:                 release.DPFVersion(),
							provisioningv1.DPUClusterNameLabelKey:      "cluster-1",
							provisioningv1.DPUClusterNamespaceLabelKey: "dpf-provisioning-system",
						},
					},
					Spec: dpuservicev1.DPUServiceSpec{
						ServiceDaemonSet: &dpuservicev1.ServiceDaemonSetValues{
							Labels: map[string]string{
								operatorv1.DPFComponentLabelKey:            operatorv1.ServiceSetControllerName.String(),
								provisioningv1.DPUClusterNameLabelKey:      "cluster-1",
								provisioningv1.DPUClusterNamespaceLabelKey: "dpf-provisioning-system",
							},
						},
						DeployInCluster: ptr.To(true),
						HelmChart: dpuservicev1.HelmChart{
							Source: dpuservicev1.ApplicationSource{
								RepoURL: "oci://example.com",
								Chart:   "dpu-networking",
								Version: "v0.1.0",
							},
							Values: func() *runtime.RawExtension {
								values := map[string]interface{}{
									"servicechainset-controller": map[string]interface{}{
										"controllerManager": map[string]interface{}{
											"manager": map[string]interface{}{
												"image": map[string]interface{}{
													"repository": "example.com/dpf-system",
													"tag":        "v0.1.0",
												},
											},
										},
										"serviceAccount": map[string]interface{}{
											"name": "servicechainset-controller-73923044a5",
											"labels": map[string]interface{}{
												dpuservicev1.CredentialRequestNameLabelKey:                "servicesetcontroller-73923044a5",
												dpuservicev1.CredentialRequestNamespaceLabelKey:           "test-namespace",
												dpuservicev1.DPUServiceCredentialRequestManagedByLabelKey: dpuservicev1.DPUServiceCredentialRequestManagedByLabelValue,
											},
										},
										"deployHostManifests": true,
										"enabled":             true,
										"env": []interface{}{
											map[string]interface{}{
												"name": "KUBERNETES_SERVICE_HOST",
												"valueFrom": map[string]interface{}{
													"secretKeyRef": map[string]interface{}{
														"name": "servicechainset-controller-credentials-73923044a5",
														"key":  "KUBERNETES_SERVICE_HOST",
													},
												},
											},
											map[string]interface{}{
												"name": "KUBERNETES_SERVICE_PORT",
												"valueFrom": map[string]interface{}{
													"secretKeyRef": map[string]interface{}{
														"name": "servicechainset-controller-credentials-73923044a5",
														"key":  "KUBERNETES_SERVICE_PORT",
													},
												},
											},
										},
										"volumeMounts": []interface{}{
											map[string]interface{}{
												"name":      "tokenfile",
												"mountPath": "/var/run/secrets/kubernetes.io/serviceaccount",
												"readOnly":  true,
											},
										},
										"volumes": []interface{}{
											map[string]interface{}{
												"name": "tokenfile",
												"projected": map[string]interface{}{
													"sources": []interface{}{
														map[string]interface{}{
															"secret": map[string]interface{}{
																"name": "servicechainset-controller-credentials-73923044a5",
																"items": []interface{}{
																	map[string]interface{}{
																		"key":  "TOKEN_FILE",
																		"path": "token",
																	},
																},
															},
														},
														map[string]interface{}{
															"secret": map[string]interface{}{
																"name": "servicechainset-controller-credentials-73923044a5",
																"items": []interface{}{
																	map[string]interface{}{
																		"key":  "KUBERNETES_CA_DATA",
																		"path": "ca.crt",
																	},
																},
															},
														},
													},
												},
											},
										},
									},
								}
								raw, err := json.Marshal(values)
								g.Expect(err).ToNot(HaveOccurred())
								return &runtime.RawExtension{Raw: raw}
							}(),
						},
					},
				},
				{
					TypeMeta: metav1.TypeMeta{
						Kind:       "DPUService",
						APIVersion: "svc.dpu.nvidia.com/v1alpha1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      operatorv1.ServiceChainSetCRDsName.String(),
						Namespace: "test-namespace",
						Labels: map[string]string{
							operatorv1.DPFComponentLabelKey: operatorv1.ServiceSetControllerName.String(),
							release.DPFVersionLabelKey:      release.DPFVersion(),
						},
					},
					Spec: dpuservicev1.DPUServiceSpec{
						ServiceDaemonSet: &dpuservicev1.ServiceDaemonSetValues{
							Labels: map[string]string{
								operatorv1.DPFComponentLabelKey: operatorv1.ServiceSetControllerName.String(),
							},
						},
						DeployInCluster: ptr.To(false),
						HelmChart: dpuservicev1.HelmChart{
							Source: dpuservicev1.ApplicationSource{
								RepoURL: "oci://example.com",
								Chart:   "dpu-networking",
								Version: "v0.1.0",
							},
							Values: func() *runtime.RawExtension {
								values := map[string]interface{}{
									"servicechainset-controller": map[string]interface{}{
										"controllerManager": map[string]interface{}{
											"manager": map[string]interface{}{
												"image": map[string]interface{}{
													"repository": "example.com/dpf-system",
													"tag":        "v0.1.0",
												},
											},
										},
										"deployDPUManifests": true,
										"enabled":            true,
										"rbac": map[string]interface{}{
											"serviceAccounts": []interface{}{
												map[string]interface{}{
													"name":      "servicechainset-controller-73923044a5",
													"namespace": "test-namespace",
												},
											},
										},
									},
								}
								raw, err := json.Marshal(values)
								g.Expect(err).ToNot(HaveOccurred())
								return &runtime.RawExtension{Raw: raw}
							}(),
						},
					},
				},
			},
			wantDPUServiceCredentialsRequests: []dpuservicev1.DPUServiceCredentialRequest{
				{
					TypeMeta: metav1.TypeMeta{
						Kind:       "DPUServiceCredentialRequest",
						APIVersion: "svc.dpu.nvidia.com/v1alpha1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "servicesetcontroller-73923044a5",
						Namespace: "test-namespace",
						Labels: map[string]string{
							operatorv1.DPFComponentLabelKey:            operatorv1.ServiceSetControllerName.String(),
							release.DPFVersionLabelKey:                 release.DPFVersion(),
							provisioningv1.DPUClusterNameLabelKey:      "cluster-1",
							provisioningv1.DPUClusterNamespaceLabelKey: "dpf-provisioning-system",
						},
					},
					Spec: dpuservicev1.DPUServiceCredentialRequestSpec{
						ServiceAccount: dpuservicev1.NamespacedName{
							Name:      "servicechainset-controller-73923044a5",
							Namespace: ptr.To("test-namespace"),
						},
						Duration: &metav1.Duration{Duration: 24 * time.Hour},
						TargetCluster: &dpuservicev1.NamespacedName{
							Name:      "cluster-1",
							Namespace: ptr.To("dpf-provisioning-system"),
						},
						Type: "tokenFile",
						Secret: dpuservicev1.NamespacedName{
							Name:      "servicechainset-controller-credentials-73923044a5",
							Namespace: ptr.To("test-namespace"),
						},
					},
				},
			},
		},
		{
			name: "DPUServices and DPUServiceCredentialsRequests are generated for more than one cluster",
			inputYAML: `apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUService
metadata:
  name: servicefunctionchainset-controller
  namespace: dpf-operator-system
spec:
  helmChart:
    source:
      repoURL: oci://example.com
      chart: servicechain-chart
      version: v0.1.0
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceCredentialRequest
metadata:
  name: servicesetcontroller
  namespace: dpf-operator-system
spec:
  duration: 24h
  serviceAccount:
    name: example-serviceaccount-name
    namespace: example-serviceaccount-namespace
  targetCluster:
    name: example-cluster-name 
    namespace: example-cluster-namespace 
  type: tokenFile
  secret:
    name: example-secret-name
    namespace: example-secret-namespace`,
			vars: func() Variables {
				defaults := &release.Defaults{}
				g.Expect(defaults.Parse()).To(Succeed())
				vars := newDefaultVariables(defaults)
				vars.Namespace = "test-namespace"
				vars.HelmCharts[operatorv1.ServiceSetControllerName] = testDPUNetworkingHelmChart
				vars.Images[operatorv1.ServiceSetControllerName.WithContainer(operatorv1.ControllerManagerContainer)] = "example.com/dpf-system:v0.1.0"
				vars.DPUClusters = []*dpucluster.Config{
					{
						Cluster: &provisioningv1.DPUCluster{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "cluster-1",
								Namespace: "dpf-provisioning-system",
							},
						},
					},
					{
						Cluster: &provisioningv1.DPUCluster{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "cluster-2",
								Namespace: "dpf-provisioning-system",
							},
						},
					},
				}
				return vars
			}(),
			wantDPUServices: []dpuservicev1.DPUService{
				{
					TypeMeta: metav1.TypeMeta{
						Kind:       "DPUService",
						APIVersion: "svc.dpu.nvidia.com/v1alpha1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "servicechainset-controller-73923044a5",
						Namespace: "test-namespace",
						Labels: map[string]string{
							operatorv1.DPFComponentLabelKey:            operatorv1.ServiceSetControllerName.String(),
							release.DPFVersionLabelKey:                 release.DPFVersion(),
							provisioningv1.DPUClusterNameLabelKey:      "cluster-1",
							provisioningv1.DPUClusterNamespaceLabelKey: "dpf-provisioning-system",
						},
					},
					Spec: dpuservicev1.DPUServiceSpec{
						ServiceDaemonSet: &dpuservicev1.ServiceDaemonSetValues{
							Labels: map[string]string{
								operatorv1.DPFComponentLabelKey:            operatorv1.ServiceSetControllerName.String(),
								provisioningv1.DPUClusterNameLabelKey:      "cluster-1",
								provisioningv1.DPUClusterNamespaceLabelKey: "dpf-provisioning-system",
							},
						},
						DeployInCluster: ptr.To(true),
						HelmChart: dpuservicev1.HelmChart{
							Source: dpuservicev1.ApplicationSource{
								RepoURL: "oci://example.com",
								Chart:   "dpu-networking",
								Version: "v0.1.0",
							},
							Values: func() *runtime.RawExtension {
								values := map[string]interface{}{
									"servicechainset-controller": map[string]interface{}{
										"controllerManager": map[string]interface{}{
											"manager": map[string]interface{}{
												"image": map[string]interface{}{
													"repository": "example.com/dpf-system",
													"tag":        "v0.1.0",
												},
											},
										},
										"serviceAccount": map[string]interface{}{
											"name": "servicechainset-controller-73923044a5",
											"labels": map[string]interface{}{
												dpuservicev1.CredentialRequestNameLabelKey:                "servicesetcontroller-73923044a5",
												dpuservicev1.CredentialRequestNamespaceLabelKey:           "test-namespace",
												dpuservicev1.DPUServiceCredentialRequestManagedByLabelKey: dpuservicev1.DPUServiceCredentialRequestManagedByLabelValue,
											},
										},
										"deployHostManifests": true,
										"enabled":             true,
										"env": []interface{}{
											map[string]interface{}{
												"name": "KUBERNETES_SERVICE_HOST",
												"valueFrom": map[string]interface{}{
													"secretKeyRef": map[string]interface{}{
														"name": "servicechainset-controller-credentials-73923044a5",
														"key":  "KUBERNETES_SERVICE_HOST",
													},
												},
											},
											map[string]interface{}{
												"name": "KUBERNETES_SERVICE_PORT",
												"valueFrom": map[string]interface{}{
													"secretKeyRef": map[string]interface{}{
														"name": "servicechainset-controller-credentials-73923044a5",
														"key":  "KUBERNETES_SERVICE_PORT",
													},
												},
											},
										},
										"volumeMounts": []interface{}{
											map[string]interface{}{
												"name":      "tokenfile",
												"mountPath": "/var/run/secrets/kubernetes.io/serviceaccount",
												"readOnly":  true,
											},
										},
										"volumes": []interface{}{
											map[string]interface{}{
												"name": "tokenfile",
												"projected": map[string]interface{}{
													"sources": []interface{}{
														map[string]interface{}{
															"secret": map[string]interface{}{
																"name": "servicechainset-controller-credentials-73923044a5",
																"items": []interface{}{
																	map[string]interface{}{
																		"key":  "TOKEN_FILE",
																		"path": "token",
																	},
																},
															},
														},
														map[string]interface{}{
															"secret": map[string]interface{}{
																"name": "servicechainset-controller-credentials-73923044a5",
																"items": []interface{}{
																	map[string]interface{}{
																		"key":  "KUBERNETES_CA_DATA",
																		"path": "ca.crt",
																	},
																},
															},
														},
													},
												},
											},
										},
									},
								}
								raw, err := json.Marshal(values)
								g.Expect(err).ToNot(HaveOccurred())
								return &runtime.RawExtension{Raw: raw}
							}(),
						},
					},
				},
				{
					TypeMeta: metav1.TypeMeta{
						Kind:       "DPUService",
						APIVersion: "svc.dpu.nvidia.com/v1alpha1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "servicechainset-controller-1bdc412401",
						Namespace: "test-namespace",
						Labels: map[string]string{
							operatorv1.DPFComponentLabelKey:            operatorv1.ServiceSetControllerName.String(),
							release.DPFVersionLabelKey:                 release.DPFVersion(),
							provisioningv1.DPUClusterNameLabelKey:      "cluster-2",
							provisioningv1.DPUClusterNamespaceLabelKey: "dpf-provisioning-system",
						},
					},
					Spec: dpuservicev1.DPUServiceSpec{
						ServiceDaemonSet: &dpuservicev1.ServiceDaemonSetValues{
							Labels: map[string]string{
								operatorv1.DPFComponentLabelKey:            operatorv1.ServiceSetControllerName.String(),
								provisioningv1.DPUClusterNameLabelKey:      "cluster-2",
								provisioningv1.DPUClusterNamespaceLabelKey: "dpf-provisioning-system",
							},
						},
						DeployInCluster: ptr.To(true),
						HelmChart: dpuservicev1.HelmChart{
							Source: dpuservicev1.ApplicationSource{
								RepoURL: "oci://example.com",
								Chart:   "dpu-networking",
								Version: "v0.1.0",
							},
							Values: func() *runtime.RawExtension {
								values := map[string]interface{}{
									"servicechainset-controller": map[string]interface{}{
										"controllerManager": map[string]interface{}{
											"manager": map[string]interface{}{
												"image": map[string]interface{}{
													"repository": "example.com/dpf-system",
													"tag":        "v0.1.0",
												},
											},
										},
										"serviceAccount": map[string]interface{}{
											"name": "servicechainset-controller-1bdc412401",
											"labels": map[string]interface{}{
												dpuservicev1.CredentialRequestNameLabelKey:                "servicesetcontroller-1bdc412401",
												dpuservicev1.CredentialRequestNamespaceLabelKey:           "test-namespace",
												dpuservicev1.DPUServiceCredentialRequestManagedByLabelKey: dpuservicev1.DPUServiceCredentialRequestManagedByLabelValue,
											},
										},
										"deployHostManifests": true,
										"enabled":             true,
										"env": []interface{}{
											map[string]interface{}{
												"name": "KUBERNETES_SERVICE_HOST",
												"valueFrom": map[string]interface{}{
													"secretKeyRef": map[string]interface{}{
														"name": "servicechainset-controller-credentials-1bdc412401",
														"key":  "KUBERNETES_SERVICE_HOST",
													},
												},
											},
											map[string]interface{}{
												"name": "KUBERNETES_SERVICE_PORT",
												"valueFrom": map[string]interface{}{
													"secretKeyRef": map[string]interface{}{
														"name": "servicechainset-controller-credentials-1bdc412401",
														"key":  "KUBERNETES_SERVICE_PORT",
													},
												},
											},
										},
										"volumeMounts": []interface{}{
											map[string]interface{}{
												"name":      "tokenfile",
												"mountPath": "/var/run/secrets/kubernetes.io/serviceaccount",
												"readOnly":  true,
											},
										},
										"volumes": []interface{}{
											map[string]interface{}{
												"name": "tokenfile",
												"projected": map[string]interface{}{
													"sources": []interface{}{
														map[string]interface{}{
															"secret": map[string]interface{}{
																"name": "servicechainset-controller-credentials-1bdc412401",
																"items": []interface{}{
																	map[string]interface{}{
																		"key":  "TOKEN_FILE",
																		"path": "token",
																	},
																},
															},
														},
														map[string]interface{}{
															"secret": map[string]interface{}{
																"name": "servicechainset-controller-credentials-1bdc412401",
																"items": []interface{}{
																	map[string]interface{}{
																		"key":  "KUBERNETES_CA_DATA",
																		"path": "ca.crt",
																	},
																},
															},
														},
													},
												},
											},
										},
									},
								}
								raw, err := json.Marshal(values)
								g.Expect(err).ToNot(HaveOccurred())
								return &runtime.RawExtension{Raw: raw}
							}(),
						},
					},
				},
				{
					TypeMeta: metav1.TypeMeta{
						Kind:       "DPUService",
						APIVersion: "svc.dpu.nvidia.com/v1alpha1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      operatorv1.ServiceChainSetCRDsName.String(),
						Namespace: "test-namespace",
						Labels: map[string]string{
							operatorv1.DPFComponentLabelKey: operatorv1.ServiceSetControllerName.String(),
							release.DPFVersionLabelKey:      release.DPFVersion(),
						},
					},
					Spec: dpuservicev1.DPUServiceSpec{
						ServiceDaemonSet: &dpuservicev1.ServiceDaemonSetValues{
							Labels: map[string]string{
								operatorv1.DPFComponentLabelKey: operatorv1.ServiceSetControllerName.String(),
							},
						},
						DeployInCluster: ptr.To(false),
						HelmChart: dpuservicev1.HelmChart{
							Source: dpuservicev1.ApplicationSource{
								RepoURL: "oci://example.com",
								Chart:   "dpu-networking",
								Version: "v0.1.0",
							},
							Values: func() *runtime.RawExtension {
								values := map[string]interface{}{
									"servicechainset-controller": map[string]interface{}{
										"controllerManager": map[string]interface{}{
											"manager": map[string]interface{}{
												"image": map[string]interface{}{
													"repository": "example.com/dpf-system",
													"tag":        "v0.1.0",
												},
											},
										},
										"deployDPUManifests": true,
										"enabled":            true,
										"rbac": map[string]interface{}{
											"serviceAccounts": []interface{}{
												map[string]interface{}{
													"name":      "servicechainset-controller-73923044a5",
													"namespace": "test-namespace",
												},
												map[string]interface{}{
													"name":      "servicechainset-controller-1bdc412401",
													"namespace": "test-namespace",
												},
											},
										},
									},
								}
								raw, err := json.Marshal(values)
								g.Expect(err).ToNot(HaveOccurred())
								return &runtime.RawExtension{Raw: raw}
							}(),
						},
					},
				},
			},
			wantDPUServiceCredentialsRequests: []dpuservicev1.DPUServiceCredentialRequest{
				{
					TypeMeta: metav1.TypeMeta{
						Kind:       "DPUServiceCredentialRequest",
						APIVersion: "svc.dpu.nvidia.com/v1alpha1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "servicesetcontroller-73923044a5",
						Namespace: "test-namespace",
						Labels: map[string]string{
							operatorv1.DPFComponentLabelKey:            operatorv1.ServiceSetControllerName.String(),
							release.DPFVersionLabelKey:                 release.DPFVersion(),
							provisioningv1.DPUClusterNameLabelKey:      "cluster-1",
							provisioningv1.DPUClusterNamespaceLabelKey: "dpf-provisioning-system",
						},
					},
					Spec: dpuservicev1.DPUServiceCredentialRequestSpec{
						ServiceAccount: dpuservicev1.NamespacedName{
							Name:      "servicechainset-controller-73923044a5",
							Namespace: ptr.To("test-namespace"),
						},
						Duration: &metav1.Duration{Duration: 24 * time.Hour},
						TargetCluster: &dpuservicev1.NamespacedName{
							Name:      "cluster-1",
							Namespace: ptr.To("dpf-provisioning-system"),
						},
						Type: "tokenFile",
						Secret: dpuservicev1.NamespacedName{
							Name:      "servicechainset-controller-credentials-73923044a5",
							Namespace: ptr.To("test-namespace"),
						},
					},
				},
				{
					TypeMeta: metav1.TypeMeta{
						Kind:       "DPUServiceCredentialRequest",
						APIVersion: "svc.dpu.nvidia.com/v1alpha1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "servicesetcontroller-1bdc412401",
						Namespace: "test-namespace",
						Labels: map[string]string{
							operatorv1.DPFComponentLabelKey:            operatorv1.ServiceSetControllerName.String(),
							release.DPFVersionLabelKey:                 release.DPFVersion(),
							provisioningv1.DPUClusterNameLabelKey:      "cluster-2",
							provisioningv1.DPUClusterNamespaceLabelKey: "dpf-provisioning-system",
						},
					},
					Spec: dpuservicev1.DPUServiceCredentialRequestSpec{
						ServiceAccount: dpuservicev1.NamespacedName{
							Name:      "servicechainset-controller-1bdc412401",
							Namespace: ptr.To("test-namespace"),
						},
						Duration: &metav1.Duration{Duration: 24 * time.Hour},
						TargetCluster: &dpuservicev1.NamespacedName{
							Name:      "cluster-2",
							Namespace: ptr.To("dpf-provisioning-system"),
						},
						Type: "tokenFile",
						Secret: dpuservicev1.NamespacedName{
							Name:      "servicechainset-controller-credentials-1bdc412401",
							Namespace: ptr.To("test-namespace"),
						},
					},
				},
			},
		},
		{
			name: "DPUServices are mutated with servicechainset controller specific values",
			inputYAML: `apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUService
metadata:
  name: servicefunctionchainset-controller
  namespace: dpf-operator-system
spec:
  helmChart:
    source:
      repoURL: oci://example.com
      chart: servicechain-chart
      version: v0.1.0
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceCredentialRequest
metadata:
  name: servicesetcontroller
  namespace: dpf-operator-system
spec:
  duration: 24h
  serviceAccount:
    name: example-serviceaccount-name
    namespace: example-serviceaccount-namespace
  targetCluster:
    name: example-cluster-name 
    namespace: example-cluster-namespace 
  type: tokenFile
  secret:
    name: example-secret-name
    namespace: example-secret-namespace`,
			vars: func() Variables {
				defaults := &release.Defaults{}
				g.Expect(defaults.Parse()).To(Succeed())
				vars := newDefaultVariables(defaults)
				vars.Namespace = "test-namespace"
				vars.HelmCharts[operatorv1.ServiceSetControllerName] = "oci://custom-registry.com/custom-chart:v1.2.3"
				vars.Images[operatorv1.ServiceSetControllerName.WithContainer(operatorv1.ControllerManagerContainer)] = "custom-registry.com/custom-image:v1.2.3"
				vars.DPUClusters = []*dpucluster.Config{
					{
						Cluster: &provisioningv1.DPUCluster{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "cluster-1",
								Namespace: "dpf-provisioning-system",
							},
						},
					},
				}
				return vars
			}(),
			wantDPUServices: []dpuservicev1.DPUService{
				{
					TypeMeta: metav1.TypeMeta{
						Kind:       "DPUService",
						APIVersion: "svc.dpu.nvidia.com/v1alpha1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "servicechainset-controller-73923044a5",
						Namespace: "test-namespace",
						Labels: map[string]string{
							operatorv1.DPFComponentLabelKey:            operatorv1.ServiceSetControllerName.String(),
							release.DPFVersionLabelKey:                 release.DPFVersion(),
							provisioningv1.DPUClusterNameLabelKey:      "cluster-1",
							provisioningv1.DPUClusterNamespaceLabelKey: "dpf-provisioning-system",
						},
					},
					Spec: dpuservicev1.DPUServiceSpec{
						ServiceDaemonSet: &dpuservicev1.ServiceDaemonSetValues{
							Labels: map[string]string{
								operatorv1.DPFComponentLabelKey:            operatorv1.ServiceSetControllerName.String(),
								provisioningv1.DPUClusterNameLabelKey:      "cluster-1",
								provisioningv1.DPUClusterNamespaceLabelKey: "dpf-provisioning-system",
							},
						},
						DeployInCluster: ptr.To(true),
						HelmChart: dpuservicev1.HelmChart{
							Source: dpuservicev1.ApplicationSource{
								RepoURL: "oci://custom-registry.com",
								Chart:   "custom-chart",
								Version: "v1.2.3",
							},
							Values: func() *runtime.RawExtension {
								values := map[string]interface{}{
									"servicechainset-controller": map[string]interface{}{
										"controllerManager": map[string]interface{}{
											"manager": map[string]interface{}{
												"image": map[string]interface{}{
													"repository": "custom-registry.com/custom-image",
													"tag":        "v1.2.3",
												},
											},
										},
										"serviceAccount": map[string]interface{}{
											"name": "servicechainset-controller-73923044a5",
											"labels": map[string]interface{}{
												dpuservicev1.CredentialRequestNameLabelKey:                "servicesetcontroller-73923044a5",
												dpuservicev1.CredentialRequestNamespaceLabelKey:           "test-namespace",
												dpuservicev1.DPUServiceCredentialRequestManagedByLabelKey: dpuservicev1.DPUServiceCredentialRequestManagedByLabelValue,
											},
										},
										"deployHostManifests": true,
										"enabled":             true,
										"env": []interface{}{
											map[string]interface{}{
												"name": "KUBERNETES_SERVICE_HOST",
												"valueFrom": map[string]interface{}{
													"secretKeyRef": map[string]interface{}{
														"name": "servicechainset-controller-credentials-73923044a5",
														"key":  "KUBERNETES_SERVICE_HOST",
													},
												},
											},
											map[string]interface{}{
												"name": "KUBERNETES_SERVICE_PORT",
												"valueFrom": map[string]interface{}{
													"secretKeyRef": map[string]interface{}{
														"name": "servicechainset-controller-credentials-73923044a5",
														"key":  "KUBERNETES_SERVICE_PORT",
													},
												},
											},
										},
										"volumeMounts": []interface{}{
											map[string]interface{}{
												"name":      "tokenfile",
												"mountPath": "/var/run/secrets/kubernetes.io/serviceaccount",
												"readOnly":  true,
											},
										},
										"volumes": []interface{}{
											map[string]interface{}{
												"name": "tokenfile",
												"projected": map[string]interface{}{
													"sources": []interface{}{
														map[string]interface{}{
															"secret": map[string]interface{}{
																"name": "servicechainset-controller-credentials-73923044a5",
																"items": []interface{}{
																	map[string]interface{}{
																		"key":  "TOKEN_FILE",
																		"path": "token",
																	},
																},
															},
														},
														map[string]interface{}{
															"secret": map[string]interface{}{
																"name": "servicechainset-controller-credentials-73923044a5",
																"items": []interface{}{
																	map[string]interface{}{
																		"key":  "KUBERNETES_CA_DATA",
																		"path": "ca.crt",
																	},
																},
															},
														},
													},
												},
											},
										},
									},
								}
								raw, err := json.Marshal(values)
								g.Expect(err).ToNot(HaveOccurred())
								return &runtime.RawExtension{Raw: raw}
							}(),
						},
					},
				},
				{
					TypeMeta: metav1.TypeMeta{
						Kind:       "DPUService",
						APIVersion: "svc.dpu.nvidia.com/v1alpha1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      operatorv1.ServiceChainSetCRDsName.String(),
						Namespace: "test-namespace",
						Labels: map[string]string{
							operatorv1.DPFComponentLabelKey: operatorv1.ServiceSetControllerName.String(),
							release.DPFVersionLabelKey:      release.DPFVersion(),
						},
					},
					Spec: dpuservicev1.DPUServiceSpec{
						ServiceDaemonSet: &dpuservicev1.ServiceDaemonSetValues{
							Labels: map[string]string{
								operatorv1.DPFComponentLabelKey: operatorv1.ServiceSetControllerName.String(),
							},
						},
						DeployInCluster: ptr.To(false),
						HelmChart: dpuservicev1.HelmChart{
							Source: dpuservicev1.ApplicationSource{
								RepoURL: "oci://custom-registry.com",
								Chart:   "custom-chart",
								Version: "v1.2.3",
							},
							Values: func() *runtime.RawExtension {
								values := map[string]interface{}{
									"servicechainset-controller": map[string]interface{}{
										"controllerManager": map[string]interface{}{
											"manager": map[string]interface{}{
												"image": map[string]interface{}{
													"repository": "custom-registry.com/custom-image",
													"tag":        "v1.2.3",
												},
											},
										},
										"deployDPUManifests": true,
										"enabled":            true,
										"rbac": map[string]interface{}{
											"serviceAccounts": []interface{}{
												map[string]interface{}{
													"name":      "servicechainset-controller-73923044a5",
													"namespace": "test-namespace",
												},
											},
										},
									},
								}
								raw, err := json.Marshal(values)
								g.Expect(err).ToNot(HaveOccurred())
								return &runtime.RawExtension{Raw: raw}
							}(),
						},
					},
				},
			},
			wantDPUServiceCredentialsRequests: []dpuservicev1.DPUServiceCredentialRequest{
				{
					TypeMeta: metav1.TypeMeta{
						Kind:       "DPUServiceCredentialRequest",
						APIVersion: "svc.dpu.nvidia.com/v1alpha1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "servicesetcontroller-73923044a5",
						Namespace: "test-namespace",
						Labels: map[string]string{
							operatorv1.DPFComponentLabelKey:            operatorv1.ServiceSetControllerName.String(),
							release.DPFVersionLabelKey:                 release.DPFVersion(),
							provisioningv1.DPUClusterNameLabelKey:      "cluster-1",
							provisioningv1.DPUClusterNamespaceLabelKey: "dpf-provisioning-system",
						},
					},
					Spec: dpuservicev1.DPUServiceCredentialRequestSpec{
						ServiceAccount: dpuservicev1.NamespacedName{
							Name:      "servicechainset-controller-73923044a5",
							Namespace: ptr.To("test-namespace"),
						},
						Duration: &metav1.Duration{Duration: 24 * time.Hour},
						TargetCluster: &dpuservicev1.NamespacedName{
							Name:      "cluster-1",
							Namespace: ptr.To("dpf-provisioning-system"),
						},
						Type: "tokenFile",
						Secret: dpuservicev1.NamespacedName{
							Name:      "servicechainset-controller-credentials-73923044a5",
							Namespace: ptr.To("test-namespace"),
						},
					},
				},
			},
		},
		{
			name: "component is disabled",
			inputYAML: `apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUService
metadata:
  name: servicefunctionchainset-controller
  namespace: dpf-operator-system
spec:
  helmChart:
    source:
      repoURL: oci://example.com
      chart: servicechain-chart
      version: v0.1.0
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceCredentialRequest
metadata:
  name: servicesetcontroller
  namespace: dpf-operator-system
spec:
  duration: 24h
  serviceAccount:
    name: example-serviceaccount-name
    namespace: example-serviceaccount-namespace
  targetCluster:
    name: example-cluster-name 
    namespace: example-cluster-namespace 
  type: tokenFile
  secret:
    name: example-secret-name
    namespace: example-secret-namespace`,
			vars: func() Variables {
				defaults := &release.Defaults{}
				g.Expect(defaults.Parse()).To(Succeed())
				vars := newDefaultVariables(defaults)
				vars.Namespace = "test-namespace"
				vars.HelmCharts[operatorv1.ServiceSetControllerName] = testDPUNetworkingHelmChart
				vars.Images[operatorv1.ServiceSetControllerName.WithContainer(operatorv1.ControllerManagerContainer)] = "example.com/dpf-system:v0.1.0"
				vars.DisableSystemComponents[operatorv1.ServiceSetControllerName] = true
				return vars
			}(),
			wantDPUServices:                   nil,
			wantDPUServiceCredentialsRequests: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create serviceChainSetControllerObjects using the test YAML data
			scs := newServiceChainSetControllerObjects([]byte(tt.inputYAML))

			// Parse the YAML data to populate the internal structure
			err := scs.Parse()
			g.Expect(err).NotTo(HaveOccurred())

			got, err := scs.GenerateManifests(context.Background(), tt.vars)
			g.Expect(err).NotTo(HaveOccurred())

			// Find DPUServices and DPUServiceCredentialsRequests in results
			var gotDPUServices []dpuservicev1.DPUService
			var gotDPUServiceCredentialsRequests []dpuservicev1.DPUServiceCredentialRequest

			for _, obj := range got {
				if unstructuredObj, ok := obj.(*unstructured.Unstructured); ok {
					t.Logf("Processing unstructured object: %s", unstructuredObj.GetKind())
					if unstructuredObj.GetKind() == "DPUService" {
						dpuService := &dpuservicev1.DPUService{}
						err = runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.UnstructuredContent(), dpuService)
						g.Expect(err).ToNot(HaveOccurred())
						gotDPUServices = append(gotDPUServices, *dpuService)
						t.Logf("Found DPUService: %s", dpuService.Name)
					} else if unstructuredObj.GetKind() == "DPUServiceCredentialRequest" {
						credRequest := &dpuservicev1.DPUServiceCredentialRequest{}
						err = runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.UnstructuredContent(), credRequest)
						g.Expect(err).ToNot(HaveOccurred())
						gotDPUServiceCredentialsRequests = append(gotDPUServiceCredentialsRequests, *credRequest)
						t.Logf("Found DPUServiceCredentialRequest: %s", credRequest.Name)
					}
				}
			}

			// Verify DPUServices
			ignoreApplySetLabel := cmpopts.IgnoreMapEntries(func(k, _ string) bool {
				return k == applysetPartOfLabel
			})
			g.Expect(gotDPUServices).To(BeComparableTo(tt.wantDPUServices, ignoreApplySetLabel))

			// Verify DPUServiceCredentialsRequests
			g.Expect(gotDPUServiceCredentialsRequests).To(BeComparableTo(tt.wantDPUServiceCredentialsRequests, ignoreApplySetLabel))
		})
	}
}

func Test_serviceChainSetControllerObjects_ReadyCheck(t *testing.T) {
	g := NewWithT(t)

	s := scheme.Scheme
	g.Expect(dpuservicev1.AddToScheme(s)).To(Succeed())
	g.Expect(operatorv1.AddToScheme(s)).To(Succeed())
	g.Expect(provisioningv1.AddToScheme(s)).To(Succeed())

	tests := []struct {
		name                          string
		dpuClusters                   []provisioningv1.DPUCluster
		dpuServices                   []dpuservicev1.DPUService
		dpuServiceCredentialsRequests []dpuservicev1.DPUServiceCredentialRequest
		upgradeFromVersion            *string
		wantErr                       bool
		expectedErrorMsg              string
	}{
		{
			name: "ServiceChainSet Controller is ready when all DPUServices and DPUServiceCredentialsRequests are ready",
			dpuClusters: []provisioningv1.DPUCluster{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cluster-1",
						Namespace: "dpf-provisioning-system",
					},
				},
			},
			dpuServices: []dpuservicev1.DPUService{
				{
					TypeMeta: metav1.TypeMeta{Kind: "DPUService"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      operatorv1.ServiceChainSetCRDsName.String(),
						Namespace: "test-namespace",
						Labels: map[string]string{
							operatorv1.DPFComponentLabelKey: operatorv1.ServiceSetControllerName.String(),
							release.DPFVersionLabelKey:      release.DPFVersion(),
						},
					},
					Spec: dpuservicev1.DPUServiceSpec{
						DeployInCluster: ptr.To(false),
					},
					Status: dpuservicev1.DPUServiceStatus{
						Conditions: []metav1.Condition{
							{
								Type:   "Ready",
								Status: "True",
							},
						},
					},
				},
				{
					TypeMeta: metav1.TypeMeta{Kind: "DPUService"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "servicechainset-controller-73923044a5",
						Namespace: "test-namespace",
						Labels: map[string]string{
							operatorv1.DPFComponentLabelKey:            operatorv1.ServiceSetControllerName.String(),
							release.DPFVersionLabelKey:                 release.DPFVersion(),
							provisioningv1.DPUClusterNameLabelKey:      "cluster-1",
							provisioningv1.DPUClusterNamespaceLabelKey: "dpf-provisioning-system",
						},
					},
					Spec: dpuservicev1.DPUServiceSpec{
						DeployInCluster: ptr.To(true),
					},
					Status: dpuservicev1.DPUServiceStatus{
						Conditions: []metav1.Condition{
							{
								Type:   "Ready",
								Status: "True",
							},
						},
					},
				},
			},
			dpuServiceCredentialsRequests: []dpuservicev1.DPUServiceCredentialRequest{
				{
					TypeMeta: metav1.TypeMeta{Kind: "DPUServiceCredentialRequest"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "servicesetcontroller-73923044a5",
						Namespace: "test-namespace",
						Labels: map[string]string{
							operatorv1.DPFComponentLabelKey: operatorv1.ServiceSetControllerName.String(),
							release.DPFVersionLabelKey:      release.DPFVersion(),
						},
					},
					Status: dpuservicev1.DPUServiceCredentialRequestStatus{
						Conditions: []metav1.Condition{
							{
								Type:   "Ready",
								Status: "True",
							},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "ServiceChainSet Controller is not ready when one of the DPUServices is missing",
			dpuClusters: []provisioningv1.DPUCluster{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cluster-1",
						Namespace: "dpf-provisioning-system",
					},
				},
			},
			dpuServices: []dpuservicev1.DPUService{
				{
					TypeMeta: metav1.TypeMeta{Kind: "DPUService"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      operatorv1.ServiceChainSetCRDsName.String(),
						Namespace: "test-namespace",
						Labels: map[string]string{
							operatorv1.DPFComponentLabelKey: operatorv1.ServiceSetControllerName.String(),
							release.DPFVersionLabelKey:      release.DPFVersion(),
						},
					},
					Spec: dpuservicev1.DPUServiceSpec{
						DeployInCluster: ptr.To(false),
					},
					Status: dpuservicev1.DPUServiceStatus{
						Conditions: []metav1.Condition{
							{
								Type:   "Ready",
								Status: "True",
							},
						},
					},
				},
				// Missing the per-cluster DPUService
			},
			dpuServiceCredentialsRequests: []dpuservicev1.DPUServiceCredentialRequest{
				{
					TypeMeta: metav1.TypeMeta{Kind: "DPUServiceCredentialRequest"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "servicesetcontroller-73923044a5",
						Namespace: "test-namespace",
						Labels: map[string]string{
							operatorv1.DPFComponentLabelKey: operatorv1.ServiceSetControllerName.String(),
							release.DPFVersionLabelKey:      release.DPFVersion(),
						},
					},
					Status: dpuservicev1.DPUServiceCredentialRequestStatus{
						Conditions: []metav1.Condition{
							{
								Type:   "Ready",
								Status: "True",
							},
						},
					},
				},
			},
			wantErr:          true,
			expectedErrorMsg: "expected 2 DPUServices (1 RBAC/CRDs + 1 per-cluster), found 1",
		},
		{
			name: "ServiceChainSet Controller is not ready when one of the DPUServiceCredentialsRequests is missing",
			dpuClusters: []provisioningv1.DPUCluster{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cluster-1",
						Namespace: "dpf-provisioning-system",
					},
				},
			},
			dpuServices: []dpuservicev1.DPUService{
				{
					TypeMeta: metav1.TypeMeta{Kind: "DPUService"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      operatorv1.ServiceChainSetCRDsName.String(),
						Namespace: "test-namespace",
						Labels: map[string]string{
							operatorv1.DPFComponentLabelKey: operatorv1.ServiceSetControllerName.String(),
							release.DPFVersionLabelKey:      release.DPFVersion(),
						},
					},
					Spec: dpuservicev1.DPUServiceSpec{
						DeployInCluster: ptr.To(false),
					},
					Status: dpuservicev1.DPUServiceStatus{
						Conditions: []metav1.Condition{
							{
								Type:   "Ready",
								Status: "True",
							},
						},
					},
				},
				{
					TypeMeta: metav1.TypeMeta{Kind: "DPUService"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "servicechainset-controller-73923044a5",
						Namespace: "test-namespace",
						Labels: map[string]string{
							operatorv1.DPFComponentLabelKey:            operatorv1.ServiceSetControllerName.String(),
							release.DPFVersionLabelKey:                 release.DPFVersion(),
							provisioningv1.DPUClusterNameLabelKey:      "cluster-1",
							provisioningv1.DPUClusterNamespaceLabelKey: "dpf-provisioning-system",
						},
					},
					Spec: dpuservicev1.DPUServiceSpec{
						DeployInCluster: ptr.To(true),
					},
					Status: dpuservicev1.DPUServiceStatus{
						Conditions: []metav1.Condition{
							{
								Type:   "Ready",
								Status: "True",
							},
						},
					},
				},
			},
			dpuServiceCredentialsRequests: []dpuservicev1.DPUServiceCredentialRequest{
				// Missing the DPUServiceCredentialRequest
			},
			wantErr:          true,
			expectedErrorMsg: "expected 1 DPUServiceCredentialRequests, found 0",
		},
		{
			name: "ServiceChainSet Controller is not ready when one of the DPUServices is not ready",
			dpuClusters: []provisioningv1.DPUCluster{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cluster-1",
						Namespace: "dpf-provisioning-system",
					},
				},
			},
			dpuServices: []dpuservicev1.DPUService{
				{
					TypeMeta: metav1.TypeMeta{Kind: "DPUService"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      operatorv1.ServiceChainSetCRDsName.String(),
						Namespace: "test-namespace",
						Labels: map[string]string{
							operatorv1.DPFComponentLabelKey: operatorv1.ServiceSetControllerName.String(),
							release.DPFVersionLabelKey:      release.DPFVersion(),
						},
					},
					Spec: dpuservicev1.DPUServiceSpec{
						DeployInCluster: ptr.To(false),
					},
					Status: dpuservicev1.DPUServiceStatus{
						Conditions: []metav1.Condition{
							{
								Type:   "Ready",
								Status: "False", // Not ready
							},
						},
					},
				},
				{
					TypeMeta: metav1.TypeMeta{Kind: "DPUService"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "servicechainset-controller-73923044a5",
						Namespace: "test-namespace",
						Labels: map[string]string{
							operatorv1.DPFComponentLabelKey:            operatorv1.ServiceSetControllerName.String(),
							release.DPFVersionLabelKey:                 release.DPFVersion(),
							provisioningv1.DPUClusterNameLabelKey:      "cluster-1",
							provisioningv1.DPUClusterNamespaceLabelKey: "dpf-provisioning-system",
						},
					},
					Spec: dpuservicev1.DPUServiceSpec{
						DeployInCluster: ptr.To(true),
					},
					Status: dpuservicev1.DPUServiceStatus{
						Conditions: []metav1.Condition{
							{
								Type:   "Ready",
								Status: "True",
							},
						},
					},
				},
			},
			dpuServiceCredentialsRequests: []dpuservicev1.DPUServiceCredentialRequest{
				{
					TypeMeta: metav1.TypeMeta{Kind: "DPUServiceCredentialRequest"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "servicesetcontroller-73923044a5",
						Namespace: "test-namespace",
						Labels: map[string]string{
							operatorv1.DPFComponentLabelKey: operatorv1.ServiceSetControllerName.String(),
							release.DPFVersionLabelKey:      release.DPFVersion(),
						},
					},
					Status: dpuservicev1.DPUServiceCredentialRequestStatus{
						Conditions: []metav1.Condition{
							{
								Type:   "Ready",
								Status: "True",
							},
						},
					},
				},
			},
			wantErr:          true,
			expectedErrorMsg: "servicechainset-controller related DPUService test-namespace/servicechainset-rbac-and-crds is not ready",
		},
		{
			name: "ServiceChainSet Controller is not ready when one of the DPUServiceCredentialsRequests is not ready",
			dpuClusters: []provisioningv1.DPUCluster{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cluster-1",
						Namespace: "dpf-provisioning-system",
					},
				},
			},
			dpuServices: []dpuservicev1.DPUService{
				{
					TypeMeta: metav1.TypeMeta{Kind: "DPUService"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      operatorv1.ServiceChainSetCRDsName.String(),
						Namespace: "test-namespace",
						Labels: map[string]string{
							operatorv1.DPFComponentLabelKey: operatorv1.ServiceSetControllerName.String(),
							release.DPFVersionLabelKey:      release.DPFVersion(),
						},
					},
					Spec: dpuservicev1.DPUServiceSpec{
						DeployInCluster: ptr.To(false),
					},
					Status: dpuservicev1.DPUServiceStatus{
						Conditions: []metav1.Condition{
							{
								Type:   "Ready",
								Status: "True",
							},
						},
					},
				},
				{
					TypeMeta: metav1.TypeMeta{Kind: "DPUService"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "servicechainset-controller-73923044a5",
						Namespace: "test-namespace",
						Labels: map[string]string{
							operatorv1.DPFComponentLabelKey:            operatorv1.ServiceSetControllerName.String(),
							release.DPFVersionLabelKey:                 release.DPFVersion(),
							provisioningv1.DPUClusterNameLabelKey:      "cluster-1",
							provisioningv1.DPUClusterNamespaceLabelKey: "dpf-provisioning-system",
						},
					},
					Spec: dpuservicev1.DPUServiceSpec{
						DeployInCluster: ptr.To(true),
					},
					Status: dpuservicev1.DPUServiceStatus{
						Conditions: []metav1.Condition{
							{
								Type:   "Ready",
								Status: "True",
							},
						},
					},
				},
			},
			dpuServiceCredentialsRequests: []dpuservicev1.DPUServiceCredentialRequest{
				{
					TypeMeta: metav1.TypeMeta{Kind: "DPUServiceCredentialRequest"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "servicesetcontroller-73923044a5",
						Namespace: "test-namespace",
						Labels: map[string]string{
							operatorv1.DPFComponentLabelKey: operatorv1.ServiceSetControllerName.String(),
							release.DPFVersionLabelKey:      release.DPFVersion(),
						},
					},
					Status: dpuservicev1.DPUServiceCredentialRequestStatus{
						Conditions: []metav1.Condition{
							{
								Type:   "Ready",
								Status: "False", // Not ready
							},
						},
					},
				},
			},
			wantErr:          true,
			expectedErrorMsg: "servicechainset-controller related DPUServiceCredentialRequest test-namespace/servicesetcontroller-73923044a5 is not ready",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var clusterObjects []client.Object
			for i := range tt.dpuClusters {
				clusterObjects = append(clusterObjects, &tt.dpuClusters[i])
			}
			for i := range tt.dpuServices {
				clusterObjects = append(clusterObjects, &tt.dpuServices[i])
			}
			for i := range tt.dpuServiceCredentialsRequests {
				clusterObjects = append(clusterObjects, &tt.dpuServiceCredentialsRequests[i])
			}
			testClient := fake.NewClientBuilder().WithScheme(s).WithObjects(clusterObjects...).Build()

			scs := &dpuServicePerDPUClusterObjects{
				templateDPUService: fromDPUService{
					name: operatorv1.ServiceSetControllerName,
				},
				componentName:   operatorv1.ServiceSetControllerName,
				rbacAndCRDsName: operatorv1.ServiceChainSetCRDsName,
			}

			var err error
			if tt.upgradeFromVersion != nil {
				config := &operatorv1.DPFOperatorConfig{}
				config.SetNamespace("test-namespace")
				config.Status.Version = tt.upgradeFromVersion
				err = scs.IsReadyForUpgrade(context.Background(), testClient, config)
			} else {
				err = scs.IsReady(context.Background(), testClient, "test-namespace")
			}

			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
				if tt.expectedErrorMsg != "" {
					g.Expect(err.Error()).To(ContainSubstring(tt.expectedErrorMsg))
				}
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}
		})
	}
}

func Test_serviceChainSetControllerObjects_ReadyAndVersionUpdatedCheck(t *testing.T) {
	g := NewWithT(t)

	s := scheme.Scheme
	g.Expect(dpuservicev1.AddToScheme(s)).To(Succeed())
	g.Expect(operatorv1.AddToScheme(s)).To(Succeed())
	g.Expect(provisioningv1.AddToScheme(s)).To(Succeed())

	tests := []struct {
		name                          string
		dpuClusters                   []provisioningv1.DPUCluster
		dpuServices                   []dpuservicev1.DPUService
		dpuServiceCredentialsRequests []dpuservicev1.DPUServiceCredentialRequest
		upgradeFromVersion            *string
		wantErr                       bool
		expectedErrorMsg              string
	}{
		{
			name: "ServiceChainSet Controller is ready when all DPUServices and DPUServiceCredentialsRequests are ready with correct versions",
			dpuClusters: []provisioningv1.DPUCluster{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cluster-1",
						Namespace: "dpf-provisioning-system",
					},
				},
			},
			dpuServices: []dpuservicev1.DPUService{
				{
					TypeMeta: metav1.TypeMeta{Kind: "DPUService"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      operatorv1.ServiceChainSetCRDsName.String(),
						Namespace: "test-namespace",
						Labels: map[string]string{
							operatorv1.DPFComponentLabelKey: operatorv1.ServiceSetControllerName.String(),
							release.DPFVersionLabelKey:      release.DPFVersion(),
						},
					},
					Spec: dpuservicev1.DPUServiceSpec{
						DeployInCluster: ptr.To(false),
					},
					Status: dpuservicev1.DPUServiceStatus{
						Conditions: []metav1.Condition{
							{
								Type:   "Ready",
								Status: "True",
							},
						},
					},
				},
				{
					TypeMeta: metav1.TypeMeta{Kind: "DPUService"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "servicechainset-controller-73923044a5",
						Namespace: "test-namespace",
						Labels: map[string]string{
							operatorv1.DPFComponentLabelKey:            operatorv1.ServiceSetControllerName.String(),
							release.DPFVersionLabelKey:                 release.DPFVersion(),
							provisioningv1.DPUClusterNameLabelKey:      "cluster-1",
							provisioningv1.DPUClusterNamespaceLabelKey: "dpf-provisioning-system",
						},
					},
					Spec: dpuservicev1.DPUServiceSpec{
						DeployInCluster: ptr.To(true),
					},
					Status: dpuservicev1.DPUServiceStatus{
						Conditions: []metav1.Condition{
							{
								Type:   "Ready",
								Status: "True",
							},
						},
					},
				},
			},
			dpuServiceCredentialsRequests: []dpuservicev1.DPUServiceCredentialRequest{
				{
					TypeMeta: metav1.TypeMeta{Kind: "DPUServiceCredentialRequest"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "servicesetcontroller-73923044a5",
						Namespace: "test-namespace",
						Labels: map[string]string{
							operatorv1.DPFComponentLabelKey: operatorv1.ServiceSetControllerName.String(),
							release.DPFVersionLabelKey:      release.DPFVersion(),
						},
					},
					Status: dpuservicev1.DPUServiceCredentialRequestStatus{
						Conditions: []metav1.Condition{
							{
								Type:   "Ready",
								Status: "True",
							},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "ServiceChainSet Controller is not ready when all DPUServices and DPUServiceCredentials are ready but one DPUService has incorrect version",
			dpuClusters: []provisioningv1.DPUCluster{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cluster-1",
						Namespace: "dpf-provisioning-system",
					},
				},
			},
			dpuServices: []dpuservicev1.DPUService{
				{
					TypeMeta: metav1.TypeMeta{Kind: "DPUService"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      operatorv1.ServiceChainSetCRDsName.String(),
						Namespace: "test-namespace",
						Labels: map[string]string{
							operatorv1.DPFComponentLabelKey: operatorv1.ServiceSetControllerName.String(),
							release.DPFVersionLabelKey:      release.DPFVersion(),
						},
					},
					Spec: dpuservicev1.DPUServiceSpec{
						DeployInCluster: ptr.To(false),
					},
					Status: dpuservicev1.DPUServiceStatus{
						Conditions: []metav1.Condition{
							{
								Type:   "Ready",
								Status: "True",
							},
						},
					},
				},
				{
					TypeMeta: metav1.TypeMeta{Kind: "DPUService"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "servicechainset-controller-73923044a5",
						Namespace: "test-namespace",
						Labels: map[string]string{
							operatorv1.DPFComponentLabelKey: operatorv1.ServiceSetControllerName.String(),
							release.DPFVersionLabelKey:      "v0.0.9", // Wrong version
						},
					},
					Spec: dpuservicev1.DPUServiceSpec{
						DeployInCluster: ptr.To(true),
					},
					Status: dpuservicev1.DPUServiceStatus{
						Conditions: []metav1.Condition{
							{
								Type:   "Ready",
								Status: "True",
							},
						},
					},
				},
			},
			dpuServiceCredentialsRequests: []dpuservicev1.DPUServiceCredentialRequest{
				{
					TypeMeta: metav1.TypeMeta{Kind: "DPUServiceCredentialRequest"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "servicesetcontroller-73923044a5",
						Namespace: "test-namespace",
						Labels: map[string]string{
							operatorv1.DPFComponentLabelKey: operatorv1.ServiceSetControllerName.String(),
							release.DPFVersionLabelKey:      release.DPFVersion(),
						},
					},
					Status: dpuservicev1.DPUServiceCredentialRequestStatus{
						Conditions: []metav1.Condition{
							{
								Type:   "Ready",
								Status: "True",
							},
						},
					},
				},
			},
			wantErr:          true,
			expectedErrorMsg: "DPUService test-namespace/servicechainset-controller-73923044a5 has version v0.0.9, want",
		},
		{
			name: "ServiceChainSet Controller is not ready when all DPUServices and DPUServiceCredentials are ready but one DPUServiceCredentialRequest has incorrect version",
			dpuClusters: []provisioningv1.DPUCluster{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cluster-1",
						Namespace: "dpf-provisioning-system",
					},
				},
			},
			dpuServices: []dpuservicev1.DPUService{
				{
					TypeMeta: metav1.TypeMeta{Kind: "DPUService"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      operatorv1.ServiceChainSetCRDsName.String(),
						Namespace: "test-namespace",
						Labels: map[string]string{
							operatorv1.DPFComponentLabelKey: operatorv1.ServiceSetControllerName.String(),
							release.DPFVersionLabelKey:      release.DPFVersion(),
						},
					},
					Spec: dpuservicev1.DPUServiceSpec{
						DeployInCluster: ptr.To(false),
					},
					Status: dpuservicev1.DPUServiceStatus{
						Conditions: []metav1.Condition{
							{
								Type:   "Ready",
								Status: "True",
							},
						},
					},
				},
				{
					TypeMeta: metav1.TypeMeta{Kind: "DPUService"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "servicechainset-controller-73923044a5",
						Namespace: "test-namespace",
						Labels: map[string]string{
							operatorv1.DPFComponentLabelKey:            operatorv1.ServiceSetControllerName.String(),
							release.DPFVersionLabelKey:                 release.DPFVersion(),
							provisioningv1.DPUClusterNameLabelKey:      "cluster-1",
							provisioningv1.DPUClusterNamespaceLabelKey: "dpf-provisioning-system",
						},
					},
					Spec: dpuservicev1.DPUServiceSpec{
						DeployInCluster: ptr.To(true),
					},
					Status: dpuservicev1.DPUServiceStatus{
						Conditions: []metav1.Condition{
							{
								Type:   "Ready",
								Status: "True",
							},
						},
					},
				},
			},
			dpuServiceCredentialsRequests: []dpuservicev1.DPUServiceCredentialRequest{
				{
					TypeMeta: metav1.TypeMeta{Kind: "DPUServiceCredentialRequest"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "servicesetcontroller-73923044a5",
						Namespace: "test-namespace",
						Labels: map[string]string{
							operatorv1.DPFComponentLabelKey: operatorv1.ServiceSetControllerName.String(),
							release.DPFVersionLabelKey:      "v0.0.9", // Wrong version
						},
					},
					Status: dpuservicev1.DPUServiceCredentialRequestStatus{
						Conditions: []metav1.Condition{
							{
								Type:   "Ready",
								Status: "True",
							},
						},
					},
				},
			},
			wantErr:          true,
			expectedErrorMsg: "DPUServiceCredentialRequest test-namespace/servicesetcontroller-73923044a5 has version v0.0.9, want",
		},
		{
			name: "ServiceChainSet Controller is not ready when a DPUService is not ready",
			dpuClusters: []provisioningv1.DPUCluster{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cluster-1",
						Namespace: "dpf-provisioning-system",
					},
				},
			},
			dpuServices: []dpuservicev1.DPUService{
				{
					TypeMeta: metav1.TypeMeta{Kind: "DPUService"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      operatorv1.ServiceChainSetCRDsName.String(),
						Namespace: "test-namespace",
						Labels: map[string]string{
							operatorv1.DPFComponentLabelKey: operatorv1.ServiceSetControllerName.String(),
							release.DPFVersionLabelKey:      release.DPFVersion(),
						},
					},
					Spec: dpuservicev1.DPUServiceSpec{
						DeployInCluster: ptr.To(false),
					},
					Status: dpuservicev1.DPUServiceStatus{
						Conditions: []metav1.Condition{
							{
								Type:   "Ready",
								Status: "False", // Not ready
							},
						},
					},
				},
				{
					TypeMeta: metav1.TypeMeta{Kind: "DPUService"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "servicechainset-controller-73923044a5",
						Namespace: "test-namespace",
						Labels: map[string]string{
							operatorv1.DPFComponentLabelKey:            operatorv1.ServiceSetControllerName.String(),
							release.DPFVersionLabelKey:                 release.DPFVersion(),
							provisioningv1.DPUClusterNameLabelKey:      "cluster-1",
							provisioningv1.DPUClusterNamespaceLabelKey: "dpf-provisioning-system",
						},
					},
					Spec: dpuservicev1.DPUServiceSpec{
						DeployInCluster: ptr.To(true),
					},
					Status: dpuservicev1.DPUServiceStatus{
						Conditions: []metav1.Condition{
							{
								Type:   "Ready",
								Status: "True",
							},
						},
					},
				},
			},
			dpuServiceCredentialsRequests: []dpuservicev1.DPUServiceCredentialRequest{
				{
					TypeMeta: metav1.TypeMeta{Kind: "DPUServiceCredentialRequest"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "servicesetcontroller-73923044a5",
						Namespace: "test-namespace",
						Labels: map[string]string{
							operatorv1.DPFComponentLabelKey: operatorv1.ServiceSetControllerName.String(),
							release.DPFVersionLabelKey:      release.DPFVersion(),
						},
					},
					Status: dpuservicev1.DPUServiceCredentialRequestStatus{
						Conditions: []metav1.Condition{
							{
								Type:   "Ready",
								Status: "True",
							},
						},
					},
				},
			},
			wantErr:          true,
			expectedErrorMsg: "servicechainset-controller related DPUService test-namespace/servicechainset-rbac-and-crds is not ready",
		},
		{
			name: "ServiceChainSet Controller is not ready when a DPUServiceCredentialRequest is not ready",
			dpuClusters: []provisioningv1.DPUCluster{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cluster-1",
						Namespace: "dpf-provisioning-system",
					},
				},
			},
			dpuServices: []dpuservicev1.DPUService{
				{
					TypeMeta: metav1.TypeMeta{Kind: "DPUService"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      operatorv1.ServiceChainSetCRDsName.String(),
						Namespace: "test-namespace",
						Labels: map[string]string{
							operatorv1.DPFComponentLabelKey: operatorv1.ServiceSetControllerName.String(),
							release.DPFVersionLabelKey:      release.DPFVersion(),
						},
					},
					Spec: dpuservicev1.DPUServiceSpec{
						DeployInCluster: ptr.To(false),
					},
					Status: dpuservicev1.DPUServiceStatus{
						Conditions: []metav1.Condition{
							{
								Type:   "Ready",
								Status: "True",
							},
						},
					},
				},
				{
					TypeMeta: metav1.TypeMeta{Kind: "DPUService"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "servicechainset-controller-73923044a5",
						Namespace: "test-namespace",
						Labels: map[string]string{
							operatorv1.DPFComponentLabelKey:            operatorv1.ServiceSetControllerName.String(),
							release.DPFVersionLabelKey:                 release.DPFVersion(),
							provisioningv1.DPUClusterNameLabelKey:      "cluster-1",
							provisioningv1.DPUClusterNamespaceLabelKey: "dpf-provisioning-system",
						},
					},
					Spec: dpuservicev1.DPUServiceSpec{
						DeployInCluster: ptr.To(true),
					},
					Status: dpuservicev1.DPUServiceStatus{
						Conditions: []metav1.Condition{
							{
								Type:   "Ready",
								Status: "True",
							},
						},
					},
				},
			},
			dpuServiceCredentialsRequests: []dpuservicev1.DPUServiceCredentialRequest{
				{
					TypeMeta: metav1.TypeMeta{Kind: "DPUServiceCredentialRequest"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "servicesetcontroller-73923044a5",
						Namespace: "test-namespace",
						Labels: map[string]string{
							operatorv1.DPFComponentLabelKey: operatorv1.ServiceSetControllerName.String(),
							release.DPFVersionLabelKey:      release.DPFVersion(),
						},
					},
					Status: dpuservicev1.DPUServiceCredentialRequestStatus{
						Conditions: []metav1.Condition{
							{
								Type:   "Ready",
								Status: "False", // Not ready
							},
						},
					},
				},
			},
			wantErr:          true,
			expectedErrorMsg: "servicechainset-controller related DPUServiceCredentialRequest test-namespace/servicesetcontroller-73923044a5 is not ready",
		},
		{
			name: "ServiceChainSet Controller is not ready when a DPUService is missing",
			dpuClusters: []provisioningv1.DPUCluster{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cluster-1",
						Namespace: "dpf-provisioning-system",
					},
				},
			},
			dpuServices: []dpuservicev1.DPUService{
				{
					TypeMeta: metav1.TypeMeta{Kind: "DPUService"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      operatorv1.ServiceChainSetCRDsName.String(),
						Namespace: "test-namespace",
						Labels: map[string]string{
							operatorv1.DPFComponentLabelKey: operatorv1.ServiceSetControllerName.String(),
							release.DPFVersionLabelKey:      release.DPFVersion(),
						},
					},
					Spec: dpuservicev1.DPUServiceSpec{
						DeployInCluster: ptr.To(false),
					},
					Status: dpuservicev1.DPUServiceStatus{
						Conditions: []metav1.Condition{
							{
								Type:   "Ready",
								Status: "True",
							},
						},
					},
				},
				// Missing the per-cluster DPUService
			},
			dpuServiceCredentialsRequests: []dpuservicev1.DPUServiceCredentialRequest{
				{
					TypeMeta: metav1.TypeMeta{Kind: "DPUServiceCredentialRequest"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "servicesetcontroller-73923044a5",
						Namespace: "test-namespace",
						Labels: map[string]string{
							operatorv1.DPFComponentLabelKey: operatorv1.ServiceSetControllerName.String(),
							release.DPFVersionLabelKey:      release.DPFVersion(),
						},
					},
					Status: dpuservicev1.DPUServiceCredentialRequestStatus{
						Conditions: []metav1.Condition{
							{
								Type:   "Ready",
								Status: "True",
							},
						},
					},
				},
			},
			wantErr:          true,
			expectedErrorMsg: "expected 2 DPUServices (1 RBAC/CRDs + 1 per-cluster), found 1",
		},
		{
			name: "ServiceChainSet Controller is not ready when a DPUServiceCredentialRequest is missing",
			dpuClusters: []provisioningv1.DPUCluster{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cluster-1",
						Namespace: "dpf-provisioning-system",
					},
				},
			},
			dpuServices: []dpuservicev1.DPUService{
				{
					TypeMeta: metav1.TypeMeta{Kind: "DPUService"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      operatorv1.ServiceChainSetCRDsName.String(),
						Namespace: "test-namespace",
						Labels: map[string]string{
							operatorv1.DPFComponentLabelKey: operatorv1.ServiceSetControllerName.String(),
							release.DPFVersionLabelKey:      release.DPFVersion(),
						},
					},
					Spec: dpuservicev1.DPUServiceSpec{
						DeployInCluster: ptr.To(false),
					},
					Status: dpuservicev1.DPUServiceStatus{
						Conditions: []metav1.Condition{
							{
								Type:   "Ready",
								Status: "True",
							},
						},
					},
				},
				{
					TypeMeta: metav1.TypeMeta{Kind: "DPUService"},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "servicechainset-controller-73923044a5",
						Namespace: "test-namespace",
						Labels: map[string]string{
							operatorv1.DPFComponentLabelKey:            operatorv1.ServiceSetControllerName.String(),
							release.DPFVersionLabelKey:                 release.DPFVersion(),
							provisioningv1.DPUClusterNameLabelKey:      "cluster-1",
							provisioningv1.DPUClusterNamespaceLabelKey: "dpf-provisioning-system",
						},
					},
					Spec: dpuservicev1.DPUServiceSpec{
						DeployInCluster: ptr.To(true),
					},
					Status: dpuservicev1.DPUServiceStatus{
						Conditions: []metav1.Condition{
							{
								Type:   "Ready",
								Status: "True",
							},
						},
					},
				},
			},
			dpuServiceCredentialsRequests: []dpuservicev1.DPUServiceCredentialRequest{
				// Missing the DPUServiceCredentialRequest
			},
			wantErr:          true,
			expectedErrorMsg: "expected 1 DPUServiceCredentialRequests, found 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var clusterObjects []client.Object
			for i := range tt.dpuClusters {
				clusterObjects = append(clusterObjects, &tt.dpuClusters[i])
			}
			for i := range tt.dpuServices {
				clusterObjects = append(clusterObjects, &tt.dpuServices[i])
			}
			for i := range tt.dpuServiceCredentialsRequests {
				clusterObjects = append(clusterObjects, &tt.dpuServiceCredentialsRequests[i])
			}
			testClient := fake.NewClientBuilder().WithScheme(s).WithObjects(clusterObjects...).Build()

			scs := &dpuServicePerDPUClusterObjects{
				templateDPUService: fromDPUService{
					name: operatorv1.ServiceSetControllerName,
				},
				componentName:   operatorv1.ServiceSetControllerName,
				rbacAndCRDsName: operatorv1.ServiceChainSetCRDsName,
			}

			var err error
			if tt.upgradeFromVersion != nil {
				config := &operatorv1.DPFOperatorConfig{}
				config.SetNamespace("test-namespace")
				config.Status.Version = tt.upgradeFromVersion
				err = scs.IsReadyForUpgrade(context.Background(), testClient, config)
			} else {
				err = scs.IsReady(context.Background(), testClient, "test-namespace")
			}

			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
				if tt.expectedErrorMsg != "" {
					g.Expect(err.Error()).To(ContainSubstring(tt.expectedErrorMsg))
				}
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}
		})
	}
}
