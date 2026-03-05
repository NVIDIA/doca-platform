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

package templates

import (
	"os"
	"testing"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/third_party/forked/nvidia-external-attacher/client/clientset/versioned/scheme"

	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
)

func Test_ProcessTestConfig(t *testing.T) {
	g := NewWithT(t)

	sch := runtime.NewScheme()
	_ = scheme.AddToScheme(sch)
	_ = dpuservicev1.AddToScheme(sch)
	decode := serializer.NewCodecFactory(sch).UniversalDeserializer().Decode
	testConfig, err := os.ReadFile("testdata/dpuserviceconfig_hbn.yaml")
	g.Expect(err).ToNot(HaveOccurred())
	obj, gKV, err := decode(testConfig, nil, nil)
	g.Expect(err).ToNot(HaveOccurred())
	config := obj.(*dpuservicev1.DPUServiceConfiguration)
	g.Expect(gKV).ToNot(BeNil())
	g.Expect(config).ToNot(BeNil())

	params := map[string]any{
		"enabled_bgp": "on",
	}

	// process the configuration
	rendered, err := Render(config.Spec.ServiceConfiguration.HelmChart.Values, params, config.Annotations)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(rendered).ToNot(BeNil())
	g.Expect(rendered).To(Equal(&runtime.RawExtension{
		Raw: []byte("{\"configuration\":{\"startupYAMLJ2\":\"- set:\\n    interface:\\n      pf2dpu2_if:\\n        ip:\\n          address:\\n            {{ ipaddresses.ip_pf2dpu2.cidr }}: {}\\n        type: swp\\n        link:\\n          mtu: 9000\\n    router:\\n      bgp:\\n        autonomous-system: {{ config.bgp_autonomous_system }}\\n        enable: on\\n\"}}"),
	}))
}

func Test_ProcessDPUServiceConfiguration(t *testing.T) {
	cases := []struct {
		name        string
		input       dpuservicev1.ServiceConfiguration
		params      map[string]any
		expected    *runtime.RawExtension
		expectedErr bool
	}{
		{
			name: "basic test",
			input: dpuservicev1.ServiceConfiguration{
				HelmChart: dpuservicev1.ServiceConfigurationHelmChart{
					Values: &runtime.RawExtension{
						Raw: []byte(`{"param1":"{{.param1}}","param2":"{{.param2}}"}`),
					},
				},
			},
			params: map[string]any{
				"param1": "value1",
				"param2": "value2",
			},
			expected: &runtime.RawExtension{
				Raw: []byte(`{"param1":"value1","param2":"value2"}`),
			},
		},
		{
			name: "with numbers",
			input: dpuservicev1.ServiceConfiguration{
				HelmChart: dpuservicev1.ServiceConfigurationHelmChart{
					Values: &runtime.RawExtension{
						Raw: []byte(`{"param1":"{{.param1}}","param2":"{{.param2}}"}`),
					},
				},
			},
			params: map[string]any{
				"param1": 123,
				"param2": 456,
			},
			expected: &runtime.RawExtension{
				Raw: []byte(`{"param1":"123","param2":"456"}`),
			},
		},
		{
			name: "with missing params",
			input: dpuservicev1.ServiceConfiguration{
				HelmChart: dpuservicev1.ServiceConfigurationHelmChart{
					Values: &runtime.RawExtension{
						Raw: []byte(`{"param1":"{{.param1}}","param2":"{{.param2}}"}`),
					},
				},
			},
			params: map[string]any{
				"param1": "value1",
			},
			expectedErr: true,
		},
		{
			name: "with bad delimiters",
			input: dpuservicev1.ServiceConfiguration{
				HelmChart: dpuservicev1.ServiceConfigurationHelmChart{
					Values: &runtime.RawExtension{
						Raw: []byte(`{"param1":"{{.param1}}","param2":"{{{{.param2}}}}"}`),
					},
				},
			},
			params: map[string]any{
				"param1": "value1",
				"param2": "value2",
			},
			expectedErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			rendered, err := Render(tc.input.HelmChart.Values, tc.params, nil)
			if tc.expectedErr {
				g.Expect(err).To(HaveOccurred())
				g.Expect(rendered).To(BeNil())
				return
			}
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(rendered).ToNot(BeNil())
			g.Expect(rendered).To(Equal(tc.expected))
		})
	}
}
