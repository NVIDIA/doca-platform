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

package controller

import (
	"context"
	"testing"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/internal/utils"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestServiceChainsOfPod checks that a pod gets the ServiceChains of its node that have a port for its
// service, the newest first, and none without a service ID. It uses a fake client with the node
// indexes, as the envtest suite runs controllers that would reconcile these objects.
func TestServiceChainsOfPod(t *testing.T) {
	g := NewWithT(t)
	s := runtime.NewScheme()
	g.Expect(dpuservicev1.AddToScheme(s)).To(Succeed())

	now := time.Now()
	serviceInterface := func(name, serviceID string) *dpuservicev1.ServiceInterface {
		return &dpuservicev1.ServiceInterface{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: "ns",
				Labels:    map[string]string{"port": name},
			},
			Spec: dpuservicev1.ServiceInterfaceSpec{
				Node:          ptr.To("node1"),
				InterfaceType: dpuservicev1.InterfaceTypeService,
				Service: &dpuservicev1.ServiceDef{
					ServiceID:     serviceID,
					InterfaceName: name,
				},
			},
		}
	}
	chain := func(name, node, port string, age time.Duration) *dpuservicev1.ServiceChain {
		return &dpuservicev1.ServiceChain{
			ObjectMeta: metav1.ObjectMeta{
				Name:              name,
				Namespace:         "ns",
				CreationTimestamp: metav1.NewTime(now.Add(-age)),
			},
			Spec: dpuservicev1.ServiceChainSpec{
				Node: ptr.To(node),
				Switches: []dpuservicev1.Switch{
					{
						Ports: []dpuservicev1.Port{
							{
								ServiceInterface: dpuservicev1.ServiceIfc{
									MatchLabels: map[string]string{"port": port},
								},
							},
						},
					},
				},
			},
		}
	}
	c := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(
			serviceInterface("mine", "svc"),
			serviceInterface("theirs", "other"),
			chain("old", "node1", "mine", time.Hour),
			chain("new", "node1", "mine", time.Minute),
			chain("other-service", "node1", "theirs", time.Second),
			chain("other-node", "node2", "mine", time.Second),
		).
		WithIndex(&dpuservicev1.ServiceChain{}, utils.ServiceChainNodeFieldKey, utils.ServiceChainNodeIndexFunc).
		WithIndex(&dpuservicev1.ServiceInterface{}, utils.ServiceInterfaceNodeFieldKey, utils.ServiceInterfaceNodeIndexFunc).
		WithIndex(&dpuservicev1.NodeServiceInterfaces{}, utils.NSINodeFieldKey, utils.NSINodeIndexFunc).
		Build()
	r := &PodIpamReconciler{
		Client: c,
		Scheme: s,
	}

	names := func(objs []client.Object) []string {
		out := make([]string, 0, len(objs))
		for _, o := range objs {
			out = append(out, o.GetName())
		}
		return out
	}
	pod := func(labels map[string]string) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod",
				Namespace: "ns",
				Labels:    labels,
			},
			Spec: corev1.PodSpec{
				NodeName: "node1",
			},
		}
	}

	g.Expect(names(r.serviceChainsOfPod(context.Background(), pod(map[string]string{dpuservicev1.DPFServiceIDLabelKey: "svc"})))).
		To(Equal([]string{"new", "old"}), "only the chains with a port of the pod's service on its node, the newest first")
	g.Expect(r.serviceChainsOfPod(context.Background(), pod(nil))).To(BeEmpty(), "a pod without a service ID has no workflow to join")
}

// TestServiceInterfacesOfPod checks that a pod without a ServiceChain, such as one that only uses
// virtual networks, gets the ServiceInterfaces of its node that belong to its service, newest first,
// and no NodeServiceInterfaces shard. It uses a fake client for the reason TestServiceChainsOfPod does.
func TestServiceInterfacesOfPod(t *testing.T) {
	g := NewWithT(t)
	s := runtime.NewScheme()
	g.Expect(dpuservicev1.AddToScheme(s)).To(Succeed())

	now := time.Now()
	serviceInterface := func(name, node, serviceID string, age time.Duration) *dpuservicev1.ServiceInterface {
		return &dpuservicev1.ServiceInterface{
			ObjectMeta: metav1.ObjectMeta{
				Name:              name,
				Namespace:         "ns",
				CreationTimestamp: metav1.NewTime(now.Add(-age)),
			},
			Spec: dpuservicev1.ServiceInterfaceSpec{
				Node:          ptr.To(node),
				InterfaceType: dpuservicev1.InterfaceTypeService,
				Service: &dpuservicev1.ServiceDef{
					ServiceID:     serviceID,
					InterfaceName: "p0_if",
				},
			},
		}
	}
	shard := func(name, node string, age time.Duration, entries ...dpuservicev1.InterfaceEntry) *dpuservicev1.NodeServiceInterfaces {
		return &dpuservicev1.NodeServiceInterfaces{
			ObjectMeta: metav1.ObjectMeta{
				Name:              name,
				Namespace:         utils.NSIObjectsNamespace,
				CreationTimestamp: metav1.NewTime(now.Add(-age)),
			},
			Spec: dpuservicev1.NodeServiceInterfacesSpec{
				Node:       node,
				Type:       "sfc",
				Interfaces: entries,
			},
		}
	}
	entry := func(set, serviceID string) dpuservicev1.InterfaceEntry {
		return dpuservicev1.InterfaceEntry{
			Name:          "ns_" + set,
			InterfaceType: dpuservicev1.InterfaceTypeService,
			Service: &dpuservicev1.ServiceDef{
				ServiceID:     serviceID,
				InterfaceName: "p0_if",
			},
		}
	}
	c := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(
			serviceInterface("old", "node1", "vnet", time.Hour),
			serviceInterface("new", "node1", "vnet", time.Minute),
			serviceInterface("other-service", "node1", "other", time.Second),
			serviceInterface("other-node", "node2", "vnet", time.Second),
			shard("shard-with-entry-of-service", "node1", time.Second, entry("set", "vnet")),
		).
		WithIndex(&dpuservicev1.ServiceInterface{}, utils.ServiceInterfaceNodeFieldKey, utils.ServiceInterfaceNodeIndexFunc).
		WithIndex(&dpuservicev1.NodeServiceInterfaces{}, utils.NSINodeFieldKey, utils.NSINodeIndexFunc).
		Build()
	r := &PodIpamReconciler{
		Client: c,
		Scheme: s,
	}

	names := func(objs []client.Object) []string {
		out := make([]string, 0, len(objs))
		for _, o := range objs {
			out = append(out, o.GetName())
		}
		return out
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod",
			Namespace: "ns",
			Labels:    map[string]string{dpuservicev1.DPFServiceIDLabelKey: "vnet"},
		},
		Spec: corev1.PodSpec{
			NodeName: "node1",
		},
	}

	g.Expect(names(r.serviceInterfacesOfPod(context.Background(), pod))).
		To(Equal([]string{"new", "old"}), "the interfaces of the service on the node, and no shard, which carries the trace of its last writer")
	g.Expect(names(r.workflowCandidatesOfPod(context.Background(), pod))).
		To(Equal([]string{"new", "old"}), "a pod without a chain has the interfaces as its candidates")

	pod.Labels = nil
	g.Expect(r.serviceInterfacesOfPod(context.Background(), pod)).To(BeEmpty(), "a pod without a service ID has no workflow to join")
}
