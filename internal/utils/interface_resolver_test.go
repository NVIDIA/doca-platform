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
	"context"
	"testing"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	testNode = "node-1"
	testNS   = "default"
)

// fakeScheme returns a scheme with only the dpuservice types registered.
func fakeScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	g := NewWithT(t)
	s := runtime.NewScheme()
	g.Expect(dpuservicev1.AddToScheme(s)).To(Succeed())
	return s
}

// fakeClient builds a fake client with a spec.node field index for NSI.
func fakeClient(t *testing.T, objects ...client.Object) client.Client {
	t.Helper()
	return fake.NewClientBuilder().
		WithScheme(fakeScheme(t)).
		WithObjects(objects...).
		WithIndex(&dpuservicev1.ServiceInterface{}, ServiceInterfaceNodeFieldKey, func(o client.Object) []string {
			return []string{ptr.Deref(o.(*dpuservicev1.ServiceInterface).Spec.Node, "")}
		}).
		WithIndex(&dpuservicev1.NodeServiceInterfaces{}, NSINodeFieldKey, NSINodeIndexFunc).
		Build()
}

// nsiObject creates a NodeServiceInterfaces with the given entries.
func nsiObject(name, nsiType string, entries ...dpuservicev1.InterfaceEntry) *dpuservicev1.NodeServiceInterfaces {
	return &dpuservicev1.NodeServiceInterfaces{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: NSIObjectsNamespace},
		Spec: dpuservicev1.NodeServiceInterfacesSpec{
			Node:       testNode,
			Type:       nsiType,
			Interfaces: entries,
		},
	}
}

// siEntry builds an InterfaceEntry owned by a given set namespace/name.
// The name format mirrors interfaceEntryName in the servicechainset controller.
func siEntry(setNS, setName string, labels map[string]string, ifType string) dpuservicev1.InterfaceEntry {
	return dpuservicev1.InterfaceEntry{
		Name:          setNS + "_" + setName,
		Labels:        labels,
		InterfaceType: ifType,
	}
}

// terminatingSIEntry builds an InterfaceEntry marked for removal.
func terminatingSIEntry(setNS, setName string, labels map[string]string, ifType string) dpuservicev1.InterfaceEntry {
	entry := siEntry(setNS, setName, labels, ifType)
	entry.Terminating = true
	return entry
}

// siObject creates a legacy ServiceInterface.
func siObject(name, node string, labels map[string]string, ifType string) *dpuservicev1.ServiceInterface {
	return &dpuservicev1.ServiceInterface{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNS, Labels: labels},
		Spec: dpuservicev1.ServiceInterfaceSpec{
			Node:          ptr.To(node),
			InterfaceType: ifType,
		},
	}
}

func TestResolveServiceInterfaceByLabels(t *testing.T) {
	matchLabels := map[string]string{"role": "firewall"}

	tests := []struct {
		name             string
		objects          []client.Object
		matchLabels      map[string]string
		nsiTypes         []string
		wantName         string
		wantNamespace    string
		wantNode         string
		wantInterfaceTyp string
		wantErr          bool
		errContains      string
	}{
		{
			name: "NSI path resolves matching entry",
			objects: []client.Object{
				nsiObject("node-1-sfc", dpuservicev1.NSITypeSFC,
					siEntry(testNS, "my-set", matchLabels, dpuservicev1.InterfaceTypeVF)),
			},
			matchLabels:      matchLabels,
			wantName:         "my-set",
			wantNamespace:    testNS,
			wantNode:         testNode,
			wantInterfaceTyp: dpuservicev1.InterfaceTypeVF,
		},
		{
			name: "NSI type filter selects matching shard",
			objects: []client.Object{
				nsiObject("node-1-sfc", dpuservicev1.NSITypeSFC,
					siEntry(testNS, "sfc-set", matchLabels, dpuservicev1.InterfaceTypeVF)),
				nsiObject("node-1-vpc", "vpc-provisioner",
					siEntry(testNS, "vpc-set", matchLabels, dpuservicev1.InterfaceTypeService)),
			},
			matchLabels: matchLabels,
			nsiTypes:    []string{dpuservicev1.NSITypeSFC},
			wantName:    "sfc-set",
		},
		{
			name: "NSI without type filter errors on multiple matches",
			objects: []client.Object{
				nsiObject("node-1-sfc", dpuservicev1.NSITypeSFC,
					siEntry(testNS, "sfc-set", matchLabels, dpuservicev1.InterfaceTypeVF)),
				nsiObject("node-1-vpc", "vpc-provisioner",
					siEntry(testNS, "vpc-set", matchLabels, dpuservicev1.InterfaceTypeService)),
			},
			matchLabels: matchLabels,
			wantErr:     true,
			errContains: "found 2",
		},
		{
			name: "NSI terminating entry is skipped and falls back to legacy",
			objects: []client.Object{
				nsiObject("node-1-sfc", dpuservicev1.NSITypeSFC,
					terminatingSIEntry(testNS, "my-set", matchLabels, dpuservicev1.InterfaceTypeVF)),
				siObject("legacy-si", testNode, matchLabels, dpuservicev1.InterfaceTypeVF),
			},
			matchLabels: matchLabels,
			wantName:    "legacy-si",
		},
		{
			name: "NSI resolves live entry while another set with the same labels terminates",
			objects: []client.Object{
				nsiObject("node-1-sfc", dpuservicev1.NSITypeSFC,
					terminatingSIEntry(testNS, "old-set", matchLabels, dpuservicev1.InterfaceTypePF),
					siEntry(testNS, "new-set", matchLabels, dpuservicev1.InterfaceTypeVF)),
			},
			matchLabels:      matchLabels,
			wantName:         "new-set",
			wantInterfaceTyp: dpuservicev1.InterfaceTypeVF,
		},
		{
			name: "NSI namespace isolation falls through to legacy",
			objects: []client.Object{
				nsiObject("node-1-sfc", dpuservicev1.NSITypeSFC,
					siEntry("other-ns", "cross-ns-set", matchLabels, dpuservicev1.InterfaceTypeVF)),
			},
			matchLabels: matchLabels,
			wantErr:     true,
			errContains: "no serviceInterface",
		},
		{
			name: "NSI no match falls back to legacy",
			objects: []client.Object{
				nsiObject("node-1-sfc", dpuservicev1.NSITypeSFC,
					siEntry(testNS, "my-set", map[string]string{"role": "other"}, dpuservicev1.InterfaceTypeVF)),
				siObject("legacy-si", testNode, matchLabels, dpuservicev1.InterfaceTypeVF),
			},
			matchLabels: matchLabels,
			wantName:    "legacy-si",
		},
		{
			name: "legacy path resolves matching entry",
			objects: []client.Object{
				siObject("my-si", testNode, matchLabels, dpuservicev1.InterfaceTypeVF),
			},
			matchLabels: matchLabels,
			wantName:    "my-si",
		},
		{
			name: "legacy path filters by node",
			objects: []client.Object{
				siObject("wrong-node-si", "node-2", matchLabels, dpuservicev1.InterfaceTypeVF),
			},
			matchLabels: matchLabels,
			wantErr:     true,
			errContains: "no serviceInterface",
		},
		{
			name: "legacy path ambiguous entries",
			objects: []client.Object{
				siObject("si-a", testNode, matchLabels, dpuservicev1.InterfaceTypeVF),
				siObject("si-b", testNode, matchLabels, dpuservicev1.InterfaceTypePF),
			},
			matchLabels: matchLabels,
			wantErr:     true,
			errContains: "expected only one serviceInterface",
		},
		{
			name:        "legacy path not found",
			objects:     nil,
			matchLabels: matchLabels,
			wantErr:     true,
			errContains: "no serviceInterface",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			c := fakeClient(t, tt.objects...)

			result, err := ResolveServiceInterfaceByLabels(
				context.Background(), c, testNode, testNS, tt.matchLabels, tt.nsiTypes...)

			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
				if tt.errContains != "" {
					g.Expect(err.Error()).To(ContainSubstring(tt.errContains))
				}
				return
			}

			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(result).NotTo(BeNil())
			g.Expect(result.Name).To(Equal(tt.wantName))
			if tt.wantNamespace != "" {
				g.Expect(result.Namespace).To(Equal(tt.wantNamespace))
			}
			if tt.wantNode != "" {
				g.Expect(result.Spec.Node).NotTo(BeNil())
				g.Expect(*result.Spec.Node).To(Equal(tt.wantNode))
			}
			if tt.wantInterfaceTyp != "" {
				g.Expect(result.Spec.InterfaceType).To(Equal(tt.wantInterfaceTyp))
			}
		})
	}
}

func TestListInterfacesForNode(t *testing.T) {
	g := NewWithT(t)
	legacy := siObject("legacy", testNode, map[string]string{"source": "legacy"}, dpuservicev1.InterfaceTypePF)
	active := siEntry(testNS, "vpc-set", map[string]string{"source": "nsi"}, dpuservicev1.InterfaceTypeService)
	active.Annotations = map[string]string{"example": "value"}
	c := fakeClient(t,
		legacy,
		siObject("other-node", "node-2", nil, dpuservicev1.InterfaceTypePF),
		nsiObject("node-1-sfc", dpuservicev1.NSITypeSFC,
			terminatingSIEntry(testNS, "terminating", nil, dpuservicev1.InterfaceTypeVF)),
		nsiObject("node-1-vpc", dpuservicev1.NSITypeVPC,
			active,
			siEntry("other-ns", "other-namespace", nil, dpuservicev1.InterfaceTypeService)),
	)

	interfaces, err := ListInterfacesForNode(context.Background(), c, testNode, testNS)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(interfaces).To(HaveLen(2))
	g.Expect(interfaces).To(ContainElement(legacy))
	g.Expect(interfaces).To(ContainElement(And(
		HaveField("Name", "vpc-set"),
		HaveField("Annotations", map[string]string{"example": "value"}),
	)))
}

func TestListInterfacesForNodeDeduplicates(t *testing.T) {
	g := NewWithT(t)
	legacy := siObject("shared", testNode, map[string]string{"source": "legacy"}, dpuservicev1.InterfaceTypePF)
	c := fakeClient(t,
		legacy,
		nsiObject("node-1-sfc", dpuservicev1.NSITypeSFC,
			siEntry(testNS, "shared", map[string]string{"source": "nsi"}, dpuservicev1.InterfaceTypeService)),
	)

	interfaces, err := ListInterfacesForNode(context.Background(), c, testNode, testNS)
	g.Expect(err).NotTo(HaveOccurred())
	// The legacy and NSI interfaces share a name; the NSI entry wins.
	g.Expect(interfaces).To(HaveLen(1))
	g.Expect(interfaces[0].Name).To(Equal("shared"))
	g.Expect(interfaces[0].Labels).To(HaveKeyWithValue("source", "nsi"))
}
