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

package bom

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
)

func loadServiceInstances(t *testing.T) []ServiceInstance {
	t.Helper()
	g := NewWithT(t)
	objs, err := LoadObjects("testdata/services")
	g.Expect(err).NotTo(HaveOccurred())
	instances, err := ServiceInstancesFromObjects(objs, nil)
	g.Expect(err).NotTo(HaveOccurred())
	return instances
}

// TestServiceInstancesFromObjectsLogsOrphans checks that a DPUServiceConfiguration without a
// DPUServiceTemplate of its deploymentServiceName is dropped and logged.
func TestServiceInstancesFromObjectsLogsOrphans(t *testing.T) {
	g := NewWithT(t)
	objs, err := DecodeObjects([]byte(`apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: orphan
spec:
  deploymentServiceName: missing
`))
	g.Expect(err).NotTo(HaveOccurred())
	var logged []string
	logf := func(format string, args ...any) { logged = append(logged, fmt.Sprintf(format, args...)) }
	instances, err := ServiceInstancesFromObjects([]SourceObject{{File: "orphan.yaml", Object: objs[0]}}, logf)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(instances).To(BeEmpty())
	g.Expect(logged).To(ConsistOf(`ignoring orphan.yaml DPUServiceConfiguration/orphan: no DPUServiceTemplate has deploymentServiceName "missing"`))
}

func TestServiceInstancesFromObjects(t *testing.T) {
	g := NewWithT(t)
	instances := loadServiceInstances(t)
	g.Expect(instances).To(HaveLen(3))

	byOrigin := map[string]ServiceInstance{}
	for _, inst := range instances {
		switch {
		case strings.Contains(inst.Origin, "DPUServiceTemplate/svc-a"):
			byOrigin["a"] = inst
		case strings.Contains(inst.Origin, "DPUServiceTemplate/svc-b"):
			byOrigin["b"] = inst
		case strings.Contains(inst.Origin, "DPUService/plain"):
			byOrigin["plain"] = inst
		}
	}
	g.Expect(byOrigin).To(HaveLen(3))

	// The configuration of the template wins over the template values and deploys in cluster.
	a := byOrigin["a"]
	g.Expect(a.Chart).To(Equal(ChartRef{RepoURL: "https://charts.example.com", Name: "app", Version: "1.0.0"}))
	g.Expect(a.Scope).To(Equal(ScopeHost))
	g.Expect(a.Namespace).To(Equal("dpf-operator-system"))
	g.Expect(a.Values).To(HaveKeyWithValue("image", map[string]any{"tag": "from-configuration"}))
	g.Expect(a.Values).To(HaveKeyWithValue("keep", "template"))

	g.Expect(byOrigin["b"].Values).To(HaveKeyWithValue("peer", "svc-c.ns"))

	plain := byOrigin["plain"]
	g.Expect(plain.Chart).To(Equal(ChartRef{RepoURL: "oci://ghcr.io/example/charts", Name: "plain", Version: "2.0.0"}))
	g.Expect(plain.Scope).To(Equal(ScopeDPU))
	g.Expect(plain.Namespace).To(Equal("ns"))
}

// TestServiceInstanceFromDPUService checks the chart, the values as the manifest sets them (the
// values the controller adds are not computed), the scope and the namespace.
func TestServiceInstanceFromDPUService(t *testing.T) {
	g := NewWithT(t)
	svc := &dpuservicev1.DPUService{
		ObjectMeta: metav1.ObjectMeta{Name: "multus"},
		Spec: dpuservicev1.DPUServiceSpec{
			HelmChart: dpuservicev1.HelmChart{
				Source: dpuservicev1.ApplicationSource{RepoURL: "oci://example.com", Chart: "dpu-networking", Version: "v0.1.0", ReleaseName: "net"},
				Values: &runtime.RawExtension{Raw: []byte(`{"multus":{"enabled":true}}`)},
			},
			ServiceDaemonSet: &dpuservicev1.ServiceDaemonSetValues{Labels: map[string]string{"a": "b"}},
			DeployInCluster:  ptr.To(true),
		},
	}
	inst, err := ServiceInstanceFromDPUService(svc)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(inst.Chart).To(Equal(ChartRef{RepoURL: "oci://example.com", Name: "dpu-networking", Version: "v0.1.0"}))
	g.Expect(inst.ReleaseName).To(Equal("net"))
	g.Expect(inst.Namespace).To(Equal("dpf-operator-system"))
	g.Expect(inst.Scope).To(Equal(ScopeHost))
	g.Expect(inst.Values).To(Equal(map[string]any{"multus": map[string]any{"enabled": true}}))

	svc.Spec.HelmChart.Values = nil
	svc.Spec.DeployInCluster = nil
	inst, err = ServiceInstanceFromDPUService(svc)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(inst.Values).To(BeEmpty())
	g.Expect(inst.Scope).To(Equal(ScopeDPU))

	svc.Spec.HelmChart.Source.Chart = ""
	_, err = ServiceInstanceFromDPUService(svc)
	g.Expect(err).To(HaveOccurred())
}

func TestEngineServices(t *testing.T) {
	g := NewWithT(t)
	instances := loadServiceInstances(t)
	// Give every instance images through the fake renderer.
	for i := range instances {
		instances[i].Values["images"] = []any{"ghcr.io/example/common:v1", fmt.Sprintf("example.com/first-%d:v0.1.0", i%2)}
	}
	renderer := &fakeRenderer{}
	e := &Engine{Renderer: renderer, FirstPartyRegistries: []string{"example.com"}}

	services, err := e.Services(context.Background(), instances, ServicesOptions{})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(renderer.requests).To(HaveLen(3))
	g.Expect(services).To(HaveLen(2))

	app := services[0]
	g.Expect(app.Name).To(Equal("app"))
	g.Expect(app.Chart).To(Equal(Chart{RepoURL: "https://charts.example.com", Name: "app", Version: "1.0.0"}))
	common := slices.IndexFunc(app.Images, func(i Image) bool { return i.Ref != "" })
	g.Expect(app.Images[common]).To(Equal(Image{Ref: "ghcr.io/example/common:v1", Scope: []Scope{ScopeDPU, ScopeHost}}))
	g.Expect((&BOM{Services: services}).Names()).To(Equal([]string{"first-0", "first-1"}))

	// Unresolved variables in a chart fail.
	instances[0].Chart.Version = "$TAG"
	_, err = e.Services(context.Background(), instances[:1], ServicesOptions{})
	g.Expect(err).To(MatchError(ContainSubstring("unresolved variable")))
	// The error names the variables.
	instances[0].Chart.RepoURL = "${CHART_REPO}"
	_, err = e.Services(context.Background(), instances[:1], ServicesOptions{})
	g.Expect(err).To(MatchError(ContainSubstring("unresolved variables $TAG, ${CHART_REPO}")))
}
