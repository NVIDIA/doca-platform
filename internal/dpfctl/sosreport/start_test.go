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

package sosreport

import (
	"context"
	"strings"
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestValidateStartOptions(t *testing.T) {
	tests := []struct {
		name    string
		opts    StartOptions
		wantErr bool
	}{
		{
			name:    "valid host cluster",
			opts:    StartOptions{Cluster: "host", Output: OutputLocal},
			wantErr: false,
		},
		{
			name:    "valid dpu cluster",
			opts:    StartOptions{Cluster: "dpu", Output: OutputLocal},
			wantErr: false,
		},
		{
			name:    "valid all clusters",
			opts:    StartOptions{Cluster: "all", Output: OutputLocal},
			wantErr: false,
		},
		{
			name:    "invalid cluster value",
			opts:    StartOptions{Cluster: "invalid", Output: OutputLocal},
			wantErr: true,
		},
		{
			name:    "NFS without path",
			opts:    StartOptions{Cluster: "host", Output: OutputNFS, NFSServer: "10.0.0.1"},
			wantErr: true,
		},
		{
			name:    "valid NFS with path",
			opts:    StartOptions{Cluster: "host", Output: OutputNFS, NFSServer: "10.0.0.1", NFSPath: "/exports/sos"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			err := ValidateStartOptions(&tt.opts)
			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}
		})
	}
}

func TestValidateStartOptions_DefaultCaseID(t *testing.T) {
	g := NewWithT(t)
	opts := StartOptions{Cluster: "host", Output: OutputLocal}
	g.Expect(ValidateStartOptions(&opts)).To(Succeed())
	g.Expect(opts.CaseID).NotTo(BeEmpty())
	g.Expect(opts.CaseID).To(HavePrefix("dpf-"))
}

func TestValidateStartOptions_NFSSubDir(t *testing.T) {
	g := NewWithT(t)
	opts := StartOptions{
		Cluster:   "host",
		Output:    OutputNFS,
		NFSServer: "10.0.0.1",
		NFSPath:   "/exports",
	}
	g.Expect(ValidateStartOptions(&opts)).To(Succeed())
	g.Expect(opts.NFSSubDir).NotTo(BeEmpty())
}

func TestValidateStartOptions_NFSNoSub(t *testing.T) {
	g := NewWithT(t)
	opts := StartOptions{
		Cluster:   "host",
		Output:    OutputNFS,
		NFSServer: "10.0.0.1",
		NFSPath:   "/exports",
		NFSNoSub:  true,
	}
	g.Expect(ValidateStartOptions(&opts)).To(Succeed())
	g.Expect(opts.NFSSubDir).To(BeEmpty())
}

func TestValidateStartOptions_PreserveExistingCaseID(t *testing.T) {
	g := NewWithT(t)
	opts := StartOptions{Cluster: "host", Output: OutputLocal, CaseID: "my-case"}
	g.Expect(ValidateStartOptions(&opts)).To(Succeed())
	g.Expect(opts.CaseID).To(Equal("my-case"))
}

func TestValidateCaseID(t *testing.T) {
	tests := []struct {
		name    string
		caseID  string
		wantErr bool
	}{
		{name: "empty case ID", caseID: "", wantErr: false},
		{name: "valid case ID", caseID: "CASE-12345", wantErr: false},
		{name: "valid single character", caseID: "a", wantErr: false},
		{name: "valid label characters", caseID: "case_123.alpha", wantErr: false},
		{name: "valid max length", caseID: "case-" + strings.Repeat("a", 58), wantErr: false},
		{name: "forward slash", caseID: "../../etc", wantErr: true},
		{name: "backslash", caseID: `case\id`, wantErr: true},
		{name: "dot-dot", caseID: "case..id", wantErr: true},
		{name: "embedded slash", caseID: "case/id", wantErr: true},
		{name: "too long", caseID: "case-" + strings.Repeat("a", 59), wantErr: true},
		{name: "space", caseID: "case id", wantErr: true},
		{name: "starts with dash", caseID: "-case", wantErr: true},
		{name: "ends with dash", caseID: "case-", wantErr: true},
		{name: "starts with dot", caseID: ".case", wantErr: true},
		{name: "ends with dot", caseID: "case.", wantErr: true},
		{name: "starts with underscore", caseID: "_case", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			err := ValidateCaseID(tt.caseID)
			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}
		})
	}
}

func TestValidateStartOptions_ArchiveOnlyImpliesArchive(t *testing.T) {
	g := NewWithT(t)
	opts := StartOptions{
		Cluster:     "host",
		Output:      OutputNFS,
		NFSServer:   "10.0.0.1",
		NFSPath:     "/exports",
		ArchiveOnly: true,
	}
	g.Expect(ValidateStartOptions(&opts)).To(Succeed())
	g.Expect(opts.Archive).To(BeTrue())
}

// newFakeTarget creates a ClusterTarget with a fake client containing the given nodes.
func newFakeTarget(name string, nodes ...*corev1.Node) ClusterTarget {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	objs := make([]runtime.Object, len(nodes))
	for i, n := range nodes {
		objs[i] = n
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objs...).Build()
	return ClusterTarget{Name: name, Client: c}
}

func testNode(name string, labels map[string]string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
	}
}

func TestGetTargetNodes(t *testing.T) {
	allNodes := []*corev1.Node{
		testNode("master1", map[string]string{"node-role.kubernetes.io/control-plane": ""}),
		testNode("worker1", map[string]string{"node-role.kubernetes.io/worker": ""}),
		testNode("worker2", map[string]string{"node-role.kubernetes.io/worker": ""}),
	}

	tests := []struct {
		name         string
		clusterNodes []*corev1.Node
		filterNodes  []string
		nodeSelector string
		wantNodes    []string
		wantErr      bool
	}{
		{
			name:         "all nodes",
			clusterNodes: allNodes,
			wantNodes:    []string{"master1", "worker1", "worker2"},
		},
		{
			name:         "filter by names",
			clusterNodes: allNodes,
			filterNodes:  []string{"worker1", "worker2"},
			wantNodes:    []string{"worker1", "worker2"},
		},
		{
			name:         "filter by names - non-existent",
			clusterNodes: allNodes,
			filterNodes:  []string{"worker99"},
			wantNodes:    nil,
		},
		{
			name:         "filter by names - partial match",
			clusterNodes: allNodes,
			filterNodes:  []string{"worker1", "worker99"},
			wantNodes:    []string{"worker1"},
		},
		{
			name:         "node selector - workers only",
			clusterNodes: allNodes,
			nodeSelector: "node-role.kubernetes.io/worker=",
			wantNodes:    []string{"worker1", "worker2"},
		},
		{
			name:         "node selector - no match",
			clusterNodes: allNodes[:1], // only master1
			nodeSelector: "node-role.kubernetes.io/worker=",
			wantErr:      true,
		},
		{
			name:         "nodes and selector combined",
			clusterNodes: allNodes,
			filterNodes:  []string{"worker1"},
			nodeSelector: "node-role.kubernetes.io/worker=",
			wantNodes:    []string{"worker1"},
		},
		{
			name:         "nodes and selector - no overlap",
			clusterNodes: allNodes,
			filterNodes:  []string{"master1"},
			nodeSelector: "node-role.kubernetes.io/worker=",
			wantNodes:    nil,
		},
		{
			name:    "empty cluster",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			target := newFakeTarget("test", tt.clusterNodes...)
			nodes, err := getTargetNodes(context.Background(), target, tt.filterNodes, tt.nodeSelector)
			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
				return
			}
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(nodes).To(Equal(tt.wantNodes))
		})
	}
}

func TestListNodes(t *testing.T) {
	tests := []struct {
		name      string
		nodes     []*corev1.Node
		selector  string
		wantCount int
		wantErr   bool
	}{
		{
			name: "with selector",
			nodes: []*corev1.Node{
				testNode("master1", map[string]string{"role": "control-plane"}),
				testNode("worker1", map[string]string{"role": "worker"}),
				testNode("worker2", map[string]string{"role": "worker"}),
			},
			selector:  "role=worker",
			wantCount: 2,
		},
		{
			name:     "invalid selector",
			selector: "!!!invalid",
			wantErr:  true,
		},
		{
			name: "empty selector returns all",
			nodes: []*corev1.Node{
				testNode("node1", nil),
				testNode("node2", nil),
			},
			wantCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			scheme := runtime.NewScheme()
			_ = corev1.AddToScheme(scheme)
			objs := make([]runtime.Object, len(tt.nodes))
			for i, n := range tt.nodes {
				objs[i] = n
			}
			c := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objs...).Build()

			nodes, err := ListNodes(context.Background(), c, tt.selector)
			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
				return
			}
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(nodes).To(HaveLen(tt.wantCount))
		})
	}
}
