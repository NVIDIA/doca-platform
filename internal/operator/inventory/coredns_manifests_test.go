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

package inventory

import (
	"testing"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestDPUClusterServedByHostDNS(t *testing.T) {
	dpuCluster := func(clusterType provisioningv1.ClusterType, endpoint *provisioningv1.ClusterEndpointSpec) *provisioningv1.DPUCluster {
		return &provisioningv1.DPUCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster", Namespace: "ns"},
			Spec:       provisioningv1.DPUClusterSpec{Type: string(clusterType), ClusterEndpoint: endpoint},
		}
	}
	keepalived := &provisioningv1.ClusterEndpointSpec{Keepalived: &provisioningv1.KeepalivedSpec{VIP: "10.0.110.200"}}

	tests := []struct {
		name        string
		clusterType provisioningv1.ClusterType
		endpoint    *provisioningv1.ClusterEndpointSpec
		want        bool
	}{
		{
			name:        "kamaji with a keepalived VIP is served from the host cluster",
			clusterType: provisioningv1.KamajiCluster,
			endpoint:    keepalived,
			want:        true,
		},
		{
			// Nothing points a static cluster's DNS Service at the host, so a CoreDNS deployed for
			// it would never be resolved against.
			name:        "static with a keepalived VIP",
			clusterType: provisioningv1.StaticCluster,
			endpoint:    keepalived,
			want:        false,
		},
		{
			name:        "no cluster endpoint",
			clusterType: provisioningv1.KamajiCluster,
			endpoint:    nil,
			want:        false,
		},
		{
			name:        "cluster endpoint without keepalived",
			clusterType: provisioningv1.KamajiCluster,
			endpoint:    &provisioningv1.ClusterEndpointSpec{},
			want:        false,
		},
		{
			name:        "keepalived without a VIP",
			clusterType: provisioningv1.KamajiCluster,
			endpoint:    &provisioningv1.ClusterEndpointSpec{Keepalived: &provisioningv1.KeepalivedSpec{}},
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			g.Expect(IsDPUClusterServedByHostDNS(dpuCluster(tt.clusterType, tt.endpoint))).To(Equal(tt.want))
		})
	}
}

func TestCoreDNSObjectsSkipClustersWithoutVIP(t *testing.T) {
	g := NewWithT(t)

	component := newCoreDNSObjects([]byte(coreDNSInputYAML))
	g.Expect(component.Parse()).To(Succeed())

	withVIP := &provisioningv1.DPUCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "kamaji", Namespace: "ns"},
		Spec: provisioningv1.DPUClusterSpec{
			Type: string(provisioningv1.KamajiCluster),
			ClusterEndpoint: &provisioningv1.ClusterEndpointSpec{
				Keepalived: &provisioningv1.KeepalivedSpec{VIP: "10.0.110.200"},
			},
		},
	}
	static := &provisioningv1.DPUCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "static", Namespace: "ns"},
		Spec:       provisioningv1.DPUClusterSpec{Type: string(provisioningv1.StaticCluster)},
	}

	g.Expect(component.matchesCluster(withVIP)).To(BeTrue())
	g.Expect(component.matchesCluster(static)).To(BeFalse())
	g.Expect(component.matchingClusterCount([]provisioningv1.DPUCluster{*withVIP, *static})).To(Equal(1))
}

func TestCoreDNSFollowsTheKeepalivedNodeSelector(t *testing.T) {
	coreDNSValues := func(cluster *provisioningv1.DPUCluster) map[string]interface{} {
		g := NewWithT(t)

		component := newCoreDNSObjects([]byte(coreDNSInputYAML))
		g.Expect(component.Parse()).To(Succeed())

		dpuService := &dpuservicev1.DPUService{}
		for _, edit := range component.extraPerDPUClusterEdits(cluster) {
			g.Expect(edit(dpuService)).To(Succeed())
		}
		if dpuService.Spec.HelmChart.Values == nil {
			return nil
		}
		values, ok := dpuService.Spec.HelmChart.Values.Object.(*unstructured.Unstructured)
		g.Expect(ok).To(BeTrue())
		return values.UnstructuredContent()
	}

	dpuCluster := func(nodeSelector map[string]string) *provisioningv1.DPUCluster {
		return &provisioningv1.DPUCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "kamaji", Namespace: "ns"},
			Spec: provisioningv1.DPUClusterSpec{
				Type: string(provisioningv1.KamajiCluster),
				ClusterEndpoint: &provisioningv1.ClusterEndpointSpec{
					Keepalived: &provisioningv1.KeepalivedSpec{VIP: "10.0.110.200", NodeSelector: nodeSelector},
				},
			},
		}
	}

	t.Run("runs where keepalived runs", func(t *testing.T) {
		g := NewWithT(t)
		// DPU nodes reach CoreDNS through the VIP, so it has to follow the nodes the VIP may
		// live on.
		nodeSelector := map[string]string{"kubernetes.io/os": "linux", "dpf/dpu-facing": "true"}

		values := coreDNSValues(dpuCluster(nodeSelector))

		got, found, err := unstructured.NestedStringMap(values, operatorv1.CoreDNSName.String(), "nodeSelector")
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(found).To(BeTrue())
		g.Expect(got).To(Equal(nodeSelector))
	})

	t.Run("runs on any control plane node when keepalived does", func(t *testing.T) {
		g := NewWithT(t)

		values := coreDNSValues(dpuCluster(nil))

		_, found, err := unstructured.NestedStringMap(values, operatorv1.CoreDNSName.String(), "nodeSelector")
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(found).To(BeFalse())
	})
}

const coreDNSInputYAML = `apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUService
metadata:
  name: coredns
  namespace: dpf-operator-system
spec:
  security:
    privileged: false
  helmChart:
    source:
      repoURL: oci://example.com
      chart: coredns-chart
      version: v0.1.0
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceCredentialRequest
metadata:
  name: coredns
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
    namespace: example-secret-namespace
`
