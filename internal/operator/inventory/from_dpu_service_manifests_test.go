/*
Copyright 2024 NVIDIA

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
	"reflect"
	"testing"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	"github.com/nvidia/doca-platform/internal/release"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func Test_fromDPUService_GenerateManifests(t *testing.T) {
	g := NewWithT(t)
	serviceName := "testService"
	componentName := operatorv1.ComponentName(serviceName)
	initialValuesObject := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"value-one": "value-again",
			"ovs-cni": map[string]interface{}{
				"enabled": false,
			},
			"multus": map[string]interface{}{
				"enabled": true,
			},
		},
	}
	initialValuesData, err := json.Marshal(initialValuesObject)
	g.Expect(err).NotTo(HaveOccurred())
	valuesObjectWithPullSecrets := initialValuesObject.DeepCopy()
	valuesObjectWithPullSecrets.Object[serviceName] = map[string]interface{}{
		"imagePullSecrets": []corev1.LocalObjectReference{
			{Name: "secret-one"},
			{Name: "secret-two"},
		},
	}

	valuesDataWithImagePullSecrets, err := json.Marshal(valuesObjectWithPullSecrets)
	g.Expect(err).NotTo(HaveOccurred())

	defaults := &release.Defaults{}
	g.Expect(defaults.Parse()).To(Succeed())
	imagePullSecretsVars := newDefaultVariables(defaults)
	imagePullSecretsVars.ImagePullSecrets = []string{"secret-one", "secret-two"}

	disabledTestServiceVars := newDefaultVariables(defaults)
	imagePullSecretsVars.ImagePullSecrets = []string{"secret-one", "secret-two"}
	disabledTestServiceVars.DisableSystemComponents = map[operatorv1.ComponentName]bool{
		componentName: true,
	}

	flannelImageVariables := newDefaultVariables(defaults)
	flannelImageVariables.Images[operatorv1.FlannelName.String()] = "registry.com/image:v1.1.1,registry.com/image-cni:v1.1.1"
	valuesWithFlannelVariables := initialValuesObject.DeepCopy()

	valuesWithFlannelVariables.Object["flannel"] = map[string]interface{}{
		"enabled": true,
		"podCidr": "10.244.0.0/14",
		"flannel": map[string]interface{}{
			"image": map[string]interface{}{
				"repository": "registry.com/image",
				"tag":        "v1.1.1",
			},
			"cniBinDir":                 "/opt/cni/bin",
			"cniConfDir":                "/etc/cni/net.d/",
			"mtu":                       "0",
			"skipCNIConfigInstallation": true,
			"image_cni": map[string]interface{}{
				"repository": "registry.com/image-cni",
				"tag":        "v1.1.1",
			},
		},
	}
	valuesDataWithFlannelVariables, err := json.Marshal(valuesWithFlannelVariables)
	g.Expect(err).NotTo(HaveOccurred())

	flannelPodCIDRVariables := newDefaultVariables(defaults)
	flannelPodCIDRVariables.FlannelPodCIDR = "10.255.0.0/14"
	valuesWithFlannelPodCIDRVariables := initialValuesObject.DeepCopy()

	valuesWithFlannelPodCIDRVariables.Object["flannel"] = map[string]interface{}{
		"enabled": true,
		"podCidr": "10.255.0.0/14",
		"flannel": map[string]interface{}{
			"mtu":                       "0",
			"cniBinDir":                 "/opt/cni/bin",
			"cniConfDir":                "/etc/cni/net.d/",
			"skipCNIConfigInstallation": true,
		},
	}
	valuesDataWithFlannelPodCIDR, err := json.Marshal(valuesWithFlannelPodCIDRVariables)
	g.Expect(err).NotTo(HaveOccurred())

	withCNIBinDirVariables := newDefaultVariables(defaults)
	withCNIBinDirVariables.DPUCNIBinPath = "/some/cni/bin/dir/path"
	cniInstallerWithCNIBinDirValues := initialValuesObject.DeepCopy()
	cniInstallerWithCNIBinDirValues.Object[operatorv1.CNIInstallerName.String()] = map[string]interface{}{
		"cniInstaller": map[string]interface{}{
			"image": map[string]interface{}{
				"repository": "example.com/dpf-cni-installer",
				"tag":        "v0.1.0",
			},
		},
		"cniBinDir": "/some/cni/bin/dir/path",
		"enabled":   true,
	}
	cniInstallerWithCNIBinDirValuesData, err := json.Marshal(cniInstallerWithCNIBinDirValues)
	g.Expect(err).NotTo(HaveOccurred())

	tests := []struct {
		name    string
		in      *dpuservicev1.DPUService
		vars    Variables
		want    *dpuservicev1.DPUService
		wantErr bool
	}{
		{
			name: "Preserve values from the template",
			in: &dpuservicev1.DPUService{
				TypeMeta: metav1.TypeMeta{Kind: "DPUService"},
				ObjectMeta: metav1.ObjectMeta{
					Name: serviceName,
				},
				Spec: dpuservicev1.DPUServiceSpec{
					HelmChart: dpuservicev1.HelmChart{
						Values: &runtime.RawExtension{
							Raw: initialValuesData,
						},
					},
				},
			},
			vars: newDefaultVariables(defaults),
			want: &dpuservicev1.DPUService{
				TypeMeta: metav1.TypeMeta{Kind: "DPUService"},
				ObjectMeta: metav1.ObjectMeta{
					Name: serviceName,
					Labels: map[string]string{
						operatorv1.DPFComponentLabelKey: serviceName,
						release.DPFVersionLabelKey:      release.DPFVersion(),
					},
				},
				Spec: dpuservicev1.DPUServiceSpec{
					ServiceDaemonSet: &dpuservicev1.ServiceDaemonSetValues{
						Labels: map[string]string{
							operatorv1.DPFComponentLabelKey: serviceName,
						},
					},
					HelmChart: dpuservicev1.HelmChart{
						Source: dpuservicev1.ApplicationSource{
							RepoURL: "helmchart.com",
							Path:    "",
							Version: "v1",
							Chart:   "chart",
						},
						Values: &runtime.RawExtension{
							Raw: initialValuesData,
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "Merge imagepullsecrets into the the template",
			in: &dpuservicev1.DPUService{
				TypeMeta: metav1.TypeMeta{Kind: "DPUService"},
				ObjectMeta: metav1.ObjectMeta{
					Name: serviceName,
				},
				Spec: dpuservicev1.DPUServiceSpec{
					HelmChart: dpuservicev1.HelmChart{
						Values: &runtime.RawExtension{
							Raw: initialValuesData,
						},
					},
				},
			},
			vars: imagePullSecretsVars,
			want: &dpuservicev1.DPUService{
				TypeMeta: metav1.TypeMeta{Kind: "DPUService"},
				ObjectMeta: metav1.ObjectMeta{
					Name: serviceName,
					Labels: map[string]string{
						operatorv1.DPFComponentLabelKey: serviceName,
						release.DPFVersionLabelKey:      release.DPFVersion(),
					},
				},
				Spec: dpuservicev1.DPUServiceSpec{
					ServiceDaemonSet: &dpuservicev1.ServiceDaemonSetValues{
						Labels: map[string]string{
							operatorv1.DPFComponentLabelKey: serviceName,
						},
					},
					HelmChart: dpuservicev1.HelmChart{
						Source: dpuservicev1.ApplicationSource{
							RepoURL: "helmchart.com",
							Path:    "",
							Version: "v1",
							Chart:   "chart",
						},
						Values: &runtime.RawExtension{
							Raw: valuesDataWithImagePullSecrets,
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "Disable component by setting enabled: false in the helm values",
			in: &dpuservicev1.DPUService{
				ObjectMeta: metav1.ObjectMeta{
					Name: serviceName,
				},
				TypeMeta: metav1.TypeMeta{Kind: "DPUService"},
				Spec: dpuservicev1.DPUServiceSpec{
					HelmChart: dpuservicev1.HelmChart{
						Values: &runtime.RawExtension{
							Raw: initialValuesData,
						},
					},
				},
			},
			vars: imagePullSecretsVars,
			want: &dpuservicev1.DPUService{
				TypeMeta: metav1.TypeMeta{Kind: "DPUService"},
				ObjectMeta: metav1.ObjectMeta{
					Name: serviceName,
					Labels: map[string]string{
						operatorv1.DPFComponentLabelKey: serviceName,
						release.DPFVersionLabelKey:      release.DPFVersion(),
					},
				},
				Spec: dpuservicev1.DPUServiceSpec{
					ServiceDaemonSet: &dpuservicev1.ServiceDaemonSetValues{
						Labels: map[string]string{
							operatorv1.DPFComponentLabelKey: serviceName,
						},
					},
					HelmChart: dpuservicev1.HelmChart{
						Source: dpuservicev1.ApplicationSource{
							RepoURL: "helmchart.com",
							Path:    "",
							Version: "v1",
							Chart:   "chart",
						},
						Values: &runtime.RawExtension{
							Raw: valuesDataWithImagePullSecrets,
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "Substitute images in flannel DPUService",
			in: &dpuservicev1.DPUService{
				TypeMeta: metav1.TypeMeta{Kind: "DPUService"},
				ObjectMeta: metav1.ObjectMeta{
					Name: operatorv1.FlannelName.String(),
				},
				Spec: dpuservicev1.DPUServiceSpec{
					HelmChart: dpuservicev1.HelmChart{
						Values: &runtime.RawExtension{
							Raw: initialValuesData,
						},
					},
				},
			},
			vars: flannelImageVariables,
			want: &dpuservicev1.DPUService{
				TypeMeta: metav1.TypeMeta{Kind: "DPUService"},
				ObjectMeta: metav1.ObjectMeta{
					Name: operatorv1.FlannelName.String(),
					Labels: map[string]string{
						operatorv1.DPFComponentLabelKey: operatorv1.FlannelName.String(),
						release.DPFVersionLabelKey:      release.DPFVersion(),
					},
				},
				Spec: dpuservicev1.DPUServiceSpec{
					ServiceDaemonSet: &dpuservicev1.ServiceDaemonSetValues{
						Labels: map[string]string{
							operatorv1.DPFComponentLabelKey: operatorv1.FlannelName.String(),
						},
					},
					HelmChart: dpuservicev1.HelmChart{
						Source: dpuservicev1.ApplicationSource{
							RepoURL: "oci://example.com",
							Path:    "",
							Version: "v0.1.0",
							Chart:   "dpu-networking",
						},
						Values: &runtime.RawExtension{
							Raw: valuesDataWithFlannelVariables,
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "Substitute podCIDR in flannel DPUService",
			in: &dpuservicev1.DPUService{
				TypeMeta: metav1.TypeMeta{Kind: "DPUService"},
				ObjectMeta: metav1.ObjectMeta{
					Name: operatorv1.FlannelName.String(),
				},
				Spec: dpuservicev1.DPUServiceSpec{
					HelmChart: dpuservicev1.HelmChart{
						Values: &runtime.RawExtension{
							Raw: initialValuesData,
						},
					},
				},
			},
			vars: flannelPodCIDRVariables,
			want: &dpuservicev1.DPUService{
				TypeMeta: metav1.TypeMeta{Kind: "DPUService"},
				ObjectMeta: metav1.ObjectMeta{
					Name: operatorv1.FlannelName.String(),
					Labels: map[string]string{
						operatorv1.DPFComponentLabelKey: operatorv1.FlannelName.String(),
						release.DPFVersionLabelKey:      release.DPFVersion(),
					},
				},
				Spec: dpuservicev1.DPUServiceSpec{
					ServiceDaemonSet: &dpuservicev1.ServiceDaemonSetValues{
						Labels: map[string]string{
							operatorv1.DPFComponentLabelKey: operatorv1.FlannelName.String(),
						},
					},
					HelmChart: dpuservicev1.HelmChart{
						Source: dpuservicev1.ApplicationSource{
							RepoURL: "oci://example.com",
							Path:    "",
							Version: "v0.1.0",
							Chart:   "dpu-networking",
						},
						Values: &runtime.RawExtension{
							Raw: valuesDataWithFlannelPodCIDR,
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "cni installer sets cniBinDir",
			in: &dpuservicev1.DPUService{
				TypeMeta: metav1.TypeMeta{Kind: "DPUService"},
				ObjectMeta: metav1.ObjectMeta{
					Name: operatorv1.CNIInstallerName.String(),
				},
				Spec: dpuservicev1.DPUServiceSpec{
					HelmChart: dpuservicev1.HelmChart{
						Values: &runtime.RawExtension{
							Raw: initialValuesData,
						},
					},
				},
			},
			vars: withCNIBinDirVariables,
			want: &dpuservicev1.DPUService{
				TypeMeta: metav1.TypeMeta{Kind: "DPUService"},
				ObjectMeta: metav1.ObjectMeta{
					Name: operatorv1.CNIInstallerName.String(),
					Labels: map[string]string{
						operatorv1.DPFComponentLabelKey: operatorv1.CNIInstallerName.String(),
						release.DPFVersionLabelKey:      release.DPFVersion(),
					},
				},
				Spec: dpuservicev1.DPUServiceSpec{
					ServiceDaemonSet: &dpuservicev1.ServiceDaemonSetValues{
						Labels: map[string]string{
							operatorv1.DPFComponentLabelKey: operatorv1.CNIInstallerName.String(),
						},
					},
					HelmChart: dpuservicev1.HelmChart{
						Source: dpuservicev1.ApplicationSource{
							RepoURL: "oci://example.com",
							Path:    "",
							Version: "v0.1.0",
							Chart:   "dpu-networking",
						},
						Values: &runtime.RawExtension{
							Raw: cniInstallerWithCNIBinDirValuesData,
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "DPUService with resources",
			in: &dpuservicev1.DPUService{
				TypeMeta: metav1.TypeMeta{Kind: "DPUService"},
				ObjectMeta: metav1.ObjectMeta{
					Name: operatorv1.SRIOVDevicePluginName.String(),
				},
				Spec: dpuservicev1.DPUServiceSpec{
					HelmChart: dpuservicev1.HelmChart{
						Values: &runtime.RawExtension{
							Raw: initialValuesData,
						},
					},
				},
			},
			vars: func() Variables {
				vars := newDefaultVariables(defaults)
				vars.Resources[operatorv1.SRIOVDevicePluginName.WithContainer(operatorv1.SRIOVDevicePluginContainer)] = corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("500m"),
						corev1.ResourceMemory: resource.MustParse("512Mi"),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("500m"),
						corev1.ResourceMemory: resource.MustParse("512Mi"),
					},
				}
				return vars
			}(),
			want: &dpuservicev1.DPUService{
				TypeMeta: metav1.TypeMeta{Kind: "DPUService"},
				ObjectMeta: metav1.ObjectMeta{
					Name: operatorv1.SRIOVDevicePluginName.String(),
					Labels: map[string]string{
						operatorv1.DPFComponentLabelKey: operatorv1.SRIOVDevicePluginName.String(),
						release.DPFVersionLabelKey:      release.DPFVersion(),
					},
				},
				Spec: dpuservicev1.DPUServiceSpec{
					ServiceDaemonSet: &dpuservicev1.ServiceDaemonSetValues{
						Labels: map[string]string{
							operatorv1.DPFComponentLabelKey: operatorv1.SRIOVDevicePluginName.String(),
						},
					},
					HelmChart: dpuservicev1.HelmChart{
						Source: dpuservicev1.ApplicationSource{
							RepoURL: "oci://example.com",
							Path:    "",
							Version: "v0.1.0",
							Chart:   "dpu-networking",
						},
						Values: &runtime.RawExtension{
							Raw: func() []byte {
								valuesWithResources := initialValuesObject.DeepCopy()
								valuesWithResources.Object[operatorv1.SRIOVDevicePluginName.String()] = map[string]interface{}{
									"enabled": true,
									"kubeSriovDevicePlugin": map[string]interface{}{
										"kubeSriovdp": map[string]interface{}{
											"resources": map[string]interface{}{
												"limits": map[string]interface{}{
													"cpu":    "500m",
													"memory": "512Mi",
												},
												"requests": map[string]interface{}{
													"cpu":    "500m",
													"memory": "512Mi",
												},
											},
										},
									},
								}
								data, _ := json.Marshal(valuesWithResources)
								return data
							}(),
						},
					},
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			un, err := runtime.DefaultUnstructuredConverter.ToUnstructured(tt.in)
			g.Expect(err).ToNot(HaveOccurred())

			f := &fromDPUService{
				name:       operatorv1.ComponentName(tt.in.Name),
				dpuService: &unstructured.Unstructured{Object: un},
			}
			tt.vars.HelmCharts[componentName] = "helmchart.com/chart:v1"

			got, err := f.GenerateManifests(context.Background(), tt.vars, skipApplySetCreationOption{})
			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
			}
			if !tt.wantErr {
				g.Expect(err).NotTo(HaveOccurred())
			}

			// If a DPUService shouldn't be generated expect the list of objects to be empty.
			if tt.want == nil {
				g.Expect(got).To(BeEmpty())
				return
			}
			// convert to concrete type so we can compare to tt.want
			gotUnstructured, ok := got[0].(*unstructured.Unstructured)
			g.Expect(ok).To(BeTrue())
			gott := &dpuservicev1.DPUService{}
			err = runtime.DefaultUnstructuredConverter.FromUnstructured(gotUnstructured.UnstructuredContent(), gott)
			g.Expect(err).ToNot(HaveOccurred())

			g.Expect(gott).To(BeComparableTo(tt.want))
		})
	}
}

func Test_fromDPUService_ReadyCheck(t *testing.T) {
	g := NewWithT(t)

	s := scheme.Scheme
	g.Expect(dpuservicev1.AddToScheme(s)).To(Succeed())

	dpuService := &dpuservicev1.DPUService{
		TypeMeta: metav1.TypeMeta{Kind: "DPUService"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testService",
			Namespace: "testNamespace",
		},
		Spec:   dpuservicev1.DPUServiceSpec{},
		Status: dpuservicev1.DPUServiceStatus{},
	}
	tests := []struct {
		name       string
		conditions []metav1.Condition
		wantErr    bool
	}{
		{
			name:       "error if object has nil conditions",
			conditions: nil,
			wantErr:    true,
		},
		{
			name:       "error if object has no conditions",
			conditions: []metav1.Condition{},
			wantErr:    true,
		},
		{
			name: "error if object has no Ready condition",
			conditions: []metav1.Condition{
				{
					Type:   "UnrelatedCondition",
					Status: "True",
				},
			},
			wantErr: true,
		},
		{
			name: "error if object has Ready condition with status: False",
			conditions: []metav1.Condition{
				{
					Type:   "UnrelatedCondition",
					Status: "False",
				},
			},
			wantErr: true,
		},
		{
			name: "error if object has Ready condition with status: Unknown",
			conditions: []metav1.Condition{
				{
					Type:   "UnrelatedCondition",
					Status: "Unknown",
				},
			},
			wantErr: true,
		},
		{
			name: "succeed if has condition Ready with status True",
			conditions: []metav1.Condition{
				{
					Type:   "Ready",
					Status: "True",
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dpuService.Status.Conditions = tt.conditions
			testClient := fake.NewClientBuilder().WithScheme(s).WithObjects(dpuService).Build()
			f := &fromDPUService{
				name: operatorv1.ComponentName(dpuService.Name),
				dpuService: &unstructured.Unstructured{
					Object: map[string]interface{}{
						"metadata": map[string]interface{}{
							"name":      dpuService.Name,
							"namespace": dpuService.Namespace,
						},
					},
				},
			}
			config := &operatorv1.DPFOperatorConfig{}
			config.SetNamespace(dpuService.Namespace)
			config.Status.Version = ptr.To("1.2.3")
			err := f.IsReadyForUpgrade(context.Background(), testClient, config)
			g.Expect(err != nil).To(Equal(tt.wantErr))

		})
	}
}

func Test_parseHelmChartString(t *testing.T) {

	tests := []struct {
		name    string
		input   string
		want    *HelmChartSource
		wantErr bool
	}{
		{
			name:  "correctly parse a valid helmChart string",
			input: "oci://example.com/ovs-cni:v0.1.0",
			want: &HelmChartSource{
				Repo:    "oci://example.com",
				Version: "v0.1.0",
				Chart:   "ovs-cni",
			},
			wantErr: false,
		},

		{
			name:    "error if version not present",
			input:   "oci://example.com/ovs-cni",
			wantErr: true,
		},
		{
			name:    "error if repo prefix is missing",
			input:   "ovs-cni:v0.1.0",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseHelmChartString(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseHelmChartString() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseHelmChartString() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_parseImageString(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    *image
		wantErr bool
	}{
		{
			name: "correctly parse image reference with tag",
			in:   "docker.com/image:v1.0",
			want: &image{
				repoImage: "docker.com/image",
				tag:       "v1.0",
			},
			wantErr: false,
		},
		{
			name: "correctly parse image reference with tag and digest",
			in:   "docker.com/image:v1.0@sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
			want: &image{
				repoImage: "docker.com/image",
				tag:       "v1.0@sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
			},
			wantErr: false,
		},
		{
			name: "correctly parse image reference with digest only",
			in:   "docker.com/image@sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
			want: &image{
				repoImage: "docker.com/image@sha256",
				tag:       "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseImageString(tt.in)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseImageString() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseImageString() got = %v, want %v", got, tt.want)
			}
		})
	}
}
