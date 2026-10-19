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

package utils

import (
	"testing"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"

	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestMergeServiceTemplateAndConfigurationValues(t *testing.T) {
	g := NewWithT(t)
	tpl := &dpuservicev1.DPUServiceTemplate{}
	got, err := MergeServiceTemplateAndConfigurationValues(tpl, nil)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(got).To(BeNil())

	tpl.Spec.HelmChart.Values = &runtime.RawExtension{Raw: []byte(`{"a":{"x":1,"y":1},"t":true}`)}
	cfg := &dpuservicev1.DPUServiceConfiguration{}
	cfg.Spec.ServiceConfiguration.HelmChart.Values = &runtime.RawExtension{Raw: []byte(`{"a":{"x":2},"c":true}`)}
	got, err = MergeServiceTemplateAndConfigurationValues(tpl, cfg)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(got.Raw)).To(MatchJSON(`{"a":{"x":2,"y":1},"t":true,"c":true}`))

	cfg.Spec.ServiceConfiguration.HelmChart.Values = &runtime.RawExtension{Raw: []byte(`{`)}
	_, err = MergeServiceTemplateAndConfigurationValues(tpl, cfg)
	g.Expect(err).To(HaveOccurred())
}
