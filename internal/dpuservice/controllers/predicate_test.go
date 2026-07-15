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

package controllers

import (
	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/conditions"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

var _ = Describe("privilegedPodEnforcementChangedPredicate", func() {
	var p predicate.Predicate

	configWith := func(enforce *bool) *operatorv1.DPFOperatorConfig {
		return &operatorv1.DPFOperatorConfig{
			Spec: operatorv1.DPFOperatorConfigSpec{
				Security: &operatorv1.SecurityConfiguration{PrivilegedPodEnforcement: enforce},
			},
		}
	}

	BeforeEach(func() {
		p = privilegedPodEnforcementChangedPredicate()
	})

	It("ignores create, delete, and generic events", func() {
		// DPUServices reconcile their own lifecycle and pick up state via resync.
		Expect(p.Create(event.CreateEvent{Object: configWith(ptr.To(false))})).To(BeFalse())
		Expect(p.Delete(event.DeleteEvent{Object: configWith(ptr.To(false))})).To(BeFalse())
		Expect(p.Generic(event.GenericEvent{Object: configWith(ptr.To(false))})).To(BeFalse())
	})

	It("returns true when enforcement flips true to false", func() {
		Expect(p.Update(event.UpdateEvent{
			ObjectOld: configWith(ptr.To(true)),
			ObjectNew: configWith(ptr.To(false)),
		})).To(BeTrue())
	})

	It("returns true when enforcement changes from unset to explicit false", func() {
		Expect(p.Update(event.UpdateEvent{
			ObjectOld: configWith(nil),
			ObjectNew: configWith(ptr.To(false)),
		})).To(BeTrue())
	})

	It("returns false when enforcement is unchanged and only unrelated spec changes", func() {
		oldCfg := configWith(nil)
		newCfg := configWith(ptr.To(true))
		newCfg.Spec.Networking = &operatorv1.Networking{ControlPlaneMTU: ptr.To(1500)}
		Expect(p.Update(event.UpdateEvent{ObjectOld: oldCfg, ObjectNew: newCfg})).To(BeFalse())
	})
})

var _ = Describe("nodeOVNEncapIPPredicate", func() {
	var (
		p                       predicate.Funcs
		ovnEncapIPValue         string
		ovnEncapIPValueModified string
	)

	getTestNode := func() *corev1.Node {
		return &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "host-node",
				Annotations: map[string]string{},
			},
		}
	}

	BeforeEach(func() {
		p = nodeOVNEncapIPPredicate()
		ovnEncapIPValue = `["10.0.120.1"]`
		ovnEncapIPValueModified = `["10.0.120.2"]`
	})

	Context("UpdateFunc", func() {
		It("returns true when OVN encap IPs annotation changes", func() {
			oldNode := getTestNode()
			oldNode.Annotations[ovnNodeEncapIPsAnnotation] = ovnEncapIPValue

			newNode := oldNode.DeepCopy()
			newNode.Annotations[ovnNodeEncapIPsAnnotation] = ovnEncapIPValueModified

			Expect(p.UpdateFunc(event.UpdateEvent{ObjectOld: oldNode, ObjectNew: newNode})).To(BeTrue())
		})

		It("returns false when unrelated node annotation changes", func() {
			oldNode := getTestNode()
			oldNode.Annotations[ovnNodeEncapIPsAnnotation] = ovnEncapIPValue

			newNode := oldNode.DeepCopy()
			newNode.Annotations["key"] = "new"

			Expect(p.UpdateFunc(event.UpdateEvent{ObjectOld: oldNode, ObjectNew: newNode})).To(BeFalse())
		})
	})

	Context("CreateFunc", func() {
		It("returns true when OVN encap IPs annotation is present", func() {
			node := getTestNode()
			node.Annotations[ovnNodeEncapIPsAnnotation] = ovnEncapIPValue

			Expect(p.CreateFunc(event.CreateEvent{Object: node})).To(BeTrue())
		})

		It("returns false when OVN encap IPs annotation is not present", func() {
			node := getTestNode()

			Expect(p.CreateFunc(event.CreateEvent{Object: node})).To(BeFalse())
		})
	})

	Context("DeleteFunc", func() {
		It("returns true when annotation was present", func() {
			node := getTestNode()
			node.Annotations[ovnNodeEncapIPsAnnotation] = ovnEncapIPValue

			Expect(p.DeleteFunc(event.DeleteEvent{Object: node})).To(BeTrue())
		})

		It("returns false when annotation was not present", func() {
			node := getTestNode()

			Expect(p.DeleteFunc(event.DeleteEvent{Object: node})).To(BeFalse())
		})
	})

	Context("GenericFunc", func() {
		It("returns false for generic events", func() {
			node := getTestNode()
			Expect(p.GenericFunc(event.GenericEvent{Object: node})).To(BeFalse())
		})
	})
})

var _ = Describe("newDPUServiceIDLabelPredicate", func() {
	var labelPredicate predicate.Predicate

	BeforeEach(func() {
		labelPredicate = newDPUServiceIDLabelPredicate()
	})

	It("should accept pods with DPFServiceIDLabelKey", func() {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pod",
				Namespace: "default",
				Labels: map[string]string{
					dpuservicev1.DPFServiceIDLabelKey: "service-123",
				},
			},
		}
		Expect(labelPredicate.Create(event.CreateEvent{Object: pod})).To(BeTrue())
		Expect(labelPredicate.Update(event.UpdateEvent{ObjectOld: pod, ObjectNew: pod})).To(BeTrue())
		Expect(labelPredicate.Delete(event.DeleteEvent{Object: pod})).To(BeTrue())
		Expect(labelPredicate.Generic(event.GenericEvent{Object: pod})).To(BeTrue())
	})

	It("should reject pods without DPFServiceIDLabelKey", func() {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pod",
				Namespace: "default",
				Labels: map[string]string{
					"some-other-label": "value",
				},
			},
		}

		Expect(labelPredicate.Create(event.CreateEvent{Object: pod})).To(BeFalse())
		Expect(labelPredicate.Update(event.UpdateEvent{ObjectOld: pod, ObjectNew: pod})).To(BeFalse())
		Expect(labelPredicate.Delete(event.DeleteEvent{Object: pod})).To(BeFalse())
		Expect(labelPredicate.Generic(event.GenericEvent{Object: pod})).To(BeFalse())
	})

	It("should reject pods with no labels", func() {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pod",
				Namespace: "default",
			},
		}

		Expect(labelPredicate.Create(event.CreateEvent{Object: pod})).To(BeFalse())
		Expect(labelPredicate.Update(event.UpdateEvent{ObjectOld: pod, ObjectNew: pod})).To(BeFalse())
		Expect(labelPredicate.Delete(event.DeleteEvent{Object: pod})).To(BeFalse())
		Expect(labelPredicate.Generic(event.GenericEvent{Object: pod})).To(BeFalse())
	})
})

var _ = Describe("Pod readiness predicate", func() {
	var readinessPredicate predicate.Predicate

	BeforeEach(func() {
		readinessPredicate = newPodReadinessPredicate()
	})

	Context("CreateFunc", func() {
		It("should accept create events for Ready pods", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodReady,
							Status: corev1.ConditionTrue,
						},
					},
				},
			}

			Expect(readinessPredicate.Create(event.CreateEvent{Object: pod})).To(BeTrue())
		})

		It("should reject create events for non-Ready pods", func() {
			testCases := []struct {
				name      string
				condition corev1.ConditionStatus
			}{
				{"Not Ready", corev1.ConditionFalse},
				{"Unknown", corev1.ConditionUnknown},
				{"No Condition", ""},
			}

			for _, tc := range testCases {
				pod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod",
						Namespace: "default",
					},
					Status: corev1.PodStatus{},
				}

				if tc.condition != "" {
					pod.Status.Conditions = []corev1.PodCondition{
						{
							Type:   corev1.PodReady,
							Status: tc.condition,
						},
					}
				}

				Expect(readinessPredicate.Create(event.CreateEvent{Object: pod})).To(BeFalse(),
					"Expected to reject create event for pod with readiness: %s", tc.name)
			}
		})
	})

	Context("UpdateFunc", func() {
		It("should accept transition from not Ready to Ready", func() {
			oldPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodReady,
							Status: corev1.ConditionFalse,
						},
					},
				},
			}

			newPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodReady,
							Status: corev1.ConditionTrue,
						},
					},
				},
			}

			Expect(readinessPredicate.Update(event.UpdateEvent{
				ObjectOld: oldPod,
				ObjectNew: newPod,
			})).To(BeTrue())
		})

		It("should accept transition from Ready to not Ready", func() {
			oldPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodReady,
							Status: corev1.ConditionTrue,
						},
					},
				},
			}

			newPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodReady,
							Status: corev1.ConditionFalse,
						},
					},
				},
			}

			Expect(readinessPredicate.Update(event.UpdateEvent{
				ObjectOld: oldPod,
				ObjectNew: newPod,
			})).To(BeTrue())
		})

		It("should accept when deletion timestamp is set", func() {
			now := metav1.Now()
			oldPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
				},
			}

			newPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-pod",
					Namespace:         "default",
					DeletionTimestamp: &now,
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
				},
			}

			Expect(readinessPredicate.Update(event.UpdateEvent{
				ObjectOld: oldPod,
				ObjectNew: newPod,
			})).To(BeTrue())
		})

		It("should reject when readiness doesn't change", func() {
			oldPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodReady,
							Status: corev1.ConditionFalse,
						},
					},
				},
			}

			newPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodReady,
							Status: corev1.ConditionFalse,
						},
					},
				},
			}

			Expect(readinessPredicate.Update(event.UpdateEvent{
				ObjectOld: oldPod,
				ObjectNew: newPod,
			})).To(BeFalse())
		})

		It("should reject when readiness stays True and other fields changed", func() {
			oldPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodReady,
							Status: corev1.ConditionTrue,
						},
					},
				},
			}

			newPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Labels: map[string]string{
						"new-label": "value",
					},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodReady,
							Status: corev1.ConditionTrue,
						},
					},
				},
			}

			Expect(readinessPredicate.Update(event.UpdateEvent{
				ObjectOld: oldPod,
				ObjectNew: newPod,
			})).To(BeFalse())
		})

		It("should accept when readiness changes from False to True", func() {
			oldPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodReady,
							Status: corev1.ConditionFalse,
						},
					},
				},
			}

			newPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodReady,
							Status: corev1.ConditionTrue,
						},
					},
				},
			}

			Expect(readinessPredicate.Update(event.UpdateEvent{
				ObjectOld: oldPod,
				ObjectNew: newPod,
			})).To(BeTrue())
		})

		It("should accept when readiness changes from True to False", func() {
			oldPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodReady,
							Status: corev1.ConditionTrue,
						},
					},
				},
			}

			newPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodReady,
							Status: corev1.ConditionFalse,
						},
					},
				},
			}

			Expect(readinessPredicate.Update(event.UpdateEvent{
				ObjectOld: oldPod,
				ObjectNew: newPod,
			})).To(BeTrue())
		})

		It("should reject when deletion timestamp was already set", func() {
			now := metav1.Now()
			oldPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-pod",
					Namespace:         "default",
					DeletionTimestamp: &now,
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
				},
			}

			newPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-pod",
					Namespace:         "default",
					DeletionTimestamp: &now,
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
				},
			}

			Expect(readinessPredicate.Update(event.UpdateEvent{
				ObjectOld: oldPod,
				ObjectNew: newPod,
			})).To(BeFalse())
		})
	})

	Context("DeleteFunc", func() {
		It("should accept all delete events", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
			}

			Expect(readinessPredicate.Delete(event.DeleteEvent{Object: pod})).To(BeTrue())
		})
	})

	Context("GenericFunc", func() {
		It("should reject all generic events", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
			}

			Expect(readinessPredicate.Generic(event.GenericEvent{Object: pod})).To(BeFalse())
		})
	})
})

var _ = Describe("nodeAddressPredicate", func() {
	var (
		predicate    predicate.Funcs
		hostNodeName string
	)

	BeforeEach(func() {
		hostNodeName = "test-namespace_host-node"
		predicate = nodeAddressPredicate()
	})

	Context("CreateFunc", func() {
		It("should return true for all create events", func() {
			node := newTestNode("new-node", hostNodeName)
			Expect(predicate.CreateFunc(event.CreateEvent{Object: node})).To(BeTrue())
		})
	})

	Context("UpdateFunc", func() {
		It("should return true when addresses change", func() {
			oldNode := newTestNode("test-node", hostNodeName)
			newNode := oldNode.DeepCopy()
			newNode.Status.Addresses = []corev1.NodeAddress{
				{Type: corev1.NodeInternalIP, Address: "2.2.2.2"},
			}

			Expect(predicate.UpdateFunc(event.UpdateEvent{
				ObjectOld: oldNode,
				ObjectNew: newNode,
			})).To(BeTrue())
		})

		It("should return true when address is added", func() {
			oldNode := newTestNode("test-node", hostNodeName)
			newNode := oldNode.DeepCopy()
			newNode.Status.Addresses = append(newNode.Status.Addresses,
				corev1.NodeAddress{Type: corev1.NodeExternalIP, Address: "2.2.2.2"})

			Expect(predicate.UpdateFunc(event.UpdateEvent{
				ObjectOld: oldNode,
				ObjectNew: newNode,
			})).To(BeTrue())
		})

		It("should return true when address is removed", func() {
			oldNode := newTestNode("test-node", hostNodeName)
			oldNode.Status.Addresses = append(oldNode.Status.Addresses,
				corev1.NodeAddress{Type: corev1.NodeExternalIP, Address: "2.2.2.2"})
			newNode := oldNode.DeepCopy()
			newNode.Status.Addresses = []corev1.NodeAddress{
				{Type: corev1.NodeInternalIP, Address: "1.1.1.1"},
			}

			Expect(predicate.UpdateFunc(event.UpdateEvent{
				ObjectOld: oldNode,
				ObjectNew: newNode,
			})).To(BeTrue())
		})

		It("should return false when addresses do not change", func() {
			oldNode := newTestNode("test-node", hostNodeName)
			newNode := oldNode.DeepCopy()

			Expect(predicate.UpdateFunc(event.UpdateEvent{
				ObjectOld: oldNode,
				ObjectNew: newNode,
			})).To(BeFalse())
		})

		It("should return false when only labels change", func() {
			oldNode := newTestNode("test-node", hostNodeName)
			oldNode.Labels["key"] = "old-value"
			newNode := oldNode.DeepCopy()
			newNode.Labels["key"] = "new-value"

			Expect(predicate.UpdateFunc(event.UpdateEvent{
				ObjectOld: oldNode,
				ObjectNew: newNode,
			})).To(BeFalse())
		})
	})

	Context("DeleteFunc", func() {
		It("should return true for all delete events", func() {
			node := newTestNode("deleted-node", hostNodeName)
			Expect(predicate.DeleteFunc(event.DeleteEvent{Object: node})).To(BeTrue())
		})
	})

	Context("GenericFunc", func() {
		It("should return false for all generic events", func() {
			node := newTestNode("generic-node", hostNodeName)
			Expect(predicate.GenericFunc(event.GenericEvent{Object: node})).To(BeFalse())
		})
	})
})

var _ = Describe("nsiInterfaceReadinessChanged", func() {
	readyCondition := func(ready bool) []metav1.Condition {
		status := metav1.ConditionFalse
		if ready {
			status = metav1.ConditionTrue
		}
		return []metav1.Condition{
			{
				Type:               string(conditions.TypeReady),
				Status:             status,
				Reason:             "Test",
				LastTransitionTime: metav1.Now(),
			},
		}
	}

	It("returns false when both slices are empty", func() {
		Expect(nsiInterfaceReadinessChanged(nil, nil)).To(BeFalse())
	})

	It("returns false when readiness is unchanged", func() {
		old := []dpuservicev1.InterfaceEntryStatus{
			{Name: "ns1_set1", Conditions: readyCondition(true)},
			{Name: "ns1_set2", Conditions: readyCondition(false)},
		}
		Expect(nsiInterfaceReadinessChanged(old, old)).To(BeFalse())
	})

	It("returns true when an entry transitions not-ready → ready", func() {
		old := []dpuservicev1.InterfaceEntryStatus{
			{Name: "ns1_set1", Conditions: readyCondition(false)},
		}
		new := []dpuservicev1.InterfaceEntryStatus{
			{Name: "ns1_set1", Conditions: readyCondition(true)},
		}
		Expect(nsiInterfaceReadinessChanged(old, new)).To(BeTrue())
	})

	It("returns true when an entry transitions ready → not-ready", func() {
		old := []dpuservicev1.InterfaceEntryStatus{
			{Name: "ns1_set1", Conditions: readyCondition(true)},
		}
		new := []dpuservicev1.InterfaceEntryStatus{
			{Name: "ns1_set1", Conditions: readyCondition(false)},
		}
		Expect(nsiInterfaceReadinessChanged(old, new)).To(BeTrue())
	})

	It("returns true when a new entry appears", func() {
		old := []dpuservicev1.InterfaceEntryStatus{
			{Name: "ns1_set1", Conditions: readyCondition(true)},
		}
		new := []dpuservicev1.InterfaceEntryStatus{
			{Name: "ns1_set1", Conditions: readyCondition(true)},
			{Name: "ns1_set2", Conditions: readyCondition(false)},
		}
		Expect(nsiInterfaceReadinessChanged(old, new)).To(BeTrue())
	})

	It("returns true when an entry is removed", func() {
		old := []dpuservicev1.InterfaceEntryStatus{
			{Name: "ns1_set1", Conditions: readyCondition(true)},
			{Name: "ns1_set2", Conditions: readyCondition(true)},
		}
		new := []dpuservicev1.InterfaceEntryStatus{
			{Name: "ns1_set1", Conditions: readyCondition(true)},
		}
		Expect(nsiInterfaceReadinessChanged(old, new)).To(BeTrue())
	})

	It("returns true when only one of multiple entries changes", func() {
		old := []dpuservicev1.InterfaceEntryStatus{
			{Name: "ns1_set1", Conditions: readyCondition(true)},
			{Name: "ns1_set2", Conditions: readyCondition(true)},
		}
		new := []dpuservicev1.InterfaceEntryStatus{
			{Name: "ns1_set1", Conditions: readyCondition(true)},
			{Name: "ns1_set2", Conditions: readyCondition(false)},
		}
		Expect(nsiInterfaceReadinessChanged(old, new)).To(BeTrue())
	})

	It("returns false when multiple entries are all unchanged", func() {
		statuses := []dpuservicev1.InterfaceEntryStatus{
			{Name: "ns1_set1", Conditions: readyCondition(true)},
			{Name: "ns1_set2", Conditions: readyCondition(false)},
			{Name: "ns2_set1", Conditions: readyCondition(true)},
		}
		Expect(nsiInterfaceReadinessChanged(statuses, statuses)).To(BeFalse())
	})
})

var _ = Describe("newNodeServiceInterfacesReadyPredicate", func() {
	var p predicate.Predicate

	readyNSI := func(ready bool) *dpuservicev1.NodeServiceInterfaces {
		status := metav1.ConditionFalse
		if ready {
			status = metav1.ConditionTrue
		}
		return &dpuservicev1.NodeServiceInterfaces{
			ObjectMeta: metav1.ObjectMeta{Name: "test-nsi", Namespace: "default"},
			Spec:       dpuservicev1.NodeServiceInterfacesSpec{Node: "dpu-node", Type: "sfc"},
			Status: dpuservicev1.NodeServiceInterfacesStatus{
				InterfaceStatuses: []dpuservicev1.InterfaceEntryStatus{
					{
						Name: "ns1_set1",
						Conditions: []metav1.Condition{
							{
								Type:               string(conditions.TypeReady),
								Status:             status,
								Reason:             "Test",
								LastTransitionTime: metav1.Now(),
							},
						},
					},
				},
			},
		}
	}

	BeforeEach(func() {
		p = newNodeServiceInterfacesReadyPredicate()
	})

	Context("Create", func() {
		It("always returns false to avoid reconciliation burst on cache sync", func() {
			Expect(p.Create(event.CreateEvent{Object: readyNSI(true)})).To(BeFalse())
			Expect(p.Create(event.CreateEvent{Object: readyNSI(false)})).To(BeFalse())
		})
	})

	Context("Update", func() {
		It("returns false when no entry readiness changed", func() {
			nsi := readyNSI(true)
			Expect(p.Update(event.UpdateEvent{ObjectOld: nsi, ObjectNew: nsi})).To(BeFalse())
		})

		It("returns true when an entry transitions not-ready → ready", func() {
			Expect(p.Update(event.UpdateEvent{
				ObjectOld: readyNSI(false),
				ObjectNew: readyNSI(true),
			})).To(BeTrue())
		})

		It("returns true when an entry transitions ready → not-ready", func() {
			Expect(p.Update(event.UpdateEvent{
				ObjectOld: readyNSI(true),
				ObjectNew: readyNSI(false),
			})).To(BeTrue())
		})
	})

	Context("Delete", func() {
		It("always returns true", func() {
			Expect(p.Delete(event.DeleteEvent{Object: readyNSI(true)})).To(BeTrue())
			Expect(p.Delete(event.DeleteEvent{Object: readyNSI(false)})).To(BeTrue())
		})
	})

	Context("Generic", func() {
		It("always returns false", func() {
			Expect(p.Generic(event.GenericEvent{Object: readyNSI(true)})).To(BeFalse())
		})
	})
})

var _ = Describe("newServiceChainReadyPredicate", func() {
	var p predicate.Predicate

	chainWith := func(ready bool) *dpuservicev1.ServiceChain {
		status := metav1.ConditionFalse
		if ready {
			status = metav1.ConditionTrue
		}
		return &dpuservicev1.ServiceChain{
			ObjectMeta: metav1.ObjectMeta{Name: "test-chain", Namespace: "default"},
			Status: dpuservicev1.ServiceChainStatus{
				Conditions: []metav1.Condition{
					{
						Type:               string(conditions.TypeReady),
						Status:             status,
						Reason:             "Test",
						LastTransitionTime: metav1.Now(),
					},
				},
			},
		}
	}

	BeforeEach(func() {
		p = newServiceChainReadyPredicate()
	})

	Context("Create", func() {
		It("always returns false to avoid reconciliation burst on cache sync", func() {
			Expect(p.Create(event.CreateEvent{Object: chainWith(true)})).To(BeFalse())
			Expect(p.Create(event.CreateEvent{Object: chainWith(false)})).To(BeFalse())
		})
	})

	Context("Update", func() {
		It("returns false when the Ready condition is unchanged", func() {
			chain := chainWith(true)
			Expect(p.Update(event.UpdateEvent{ObjectOld: chain, ObjectNew: chain})).To(BeFalse())
		})

		It("returns true when Ready transitions not-ready → ready", func() {
			Expect(p.Update(event.UpdateEvent{
				ObjectOld: chainWith(false),
				ObjectNew: chainWith(true),
			})).To(BeTrue())
		})

		It("returns true when Ready transitions ready → not-ready", func() {
			Expect(p.Update(event.UpdateEvent{
				ObjectOld: chainWith(true),
				ObjectNew: chainWith(false),
			})).To(BeTrue())
		})
	})

	Context("Delete", func() {
		It("always returns true", func() {
			Expect(p.Delete(event.DeleteEvent{Object: chainWith(true)})).To(BeTrue())
			Expect(p.Delete(event.DeleteEvent{Object: chainWith(false)})).To(BeTrue())
		})
	})

	Context("Generic", func() {
		It("always returns false", func() {
			Expect(p.Generic(event.GenericEvent{Object: chainWith(true)})).To(BeFalse())
		})
	})
})

var _ = Describe("newServiceInterfaceReadyPredicate", func() {
	var p predicate.Predicate

	siWithReadyCondition := func(ready bool) *dpuservicev1.ServiceInterface {
		status := metav1.ConditionFalse
		if ready {
			status = metav1.ConditionTrue
		}
		return &dpuservicev1.ServiceInterface{
			ObjectMeta: metav1.ObjectMeta{Name: "test-interface", Namespace: "default"},
			Status: dpuservicev1.ServiceInterfaceStatus{
				Conditions: []metav1.Condition{
					{
						Type:               string(conditions.TypeReady),
						Status:             status,
						Reason:             "Test",
						LastTransitionTime: metav1.Now(),
					},
				},
			},
		}
	}

	BeforeEach(func() {
		p = newServiceInterfaceReadyPredicate()
	})

	Context("Create", func() {
		It("always returns false to avoid reconciliation burst on cache sync", func() {
			Expect(p.Create(event.CreateEvent{Object: siWithReadyCondition(true)})).To(BeFalse())
			Expect(p.Create(event.CreateEvent{Object: siWithReadyCondition(false)})).To(BeFalse())
		})
	})

	Context("Update", func() {
		It("returns false when the Ready condition is unchanged", func() {
			iface := siWithReadyCondition(true)
			Expect(p.Update(event.UpdateEvent{ObjectOld: iface, ObjectNew: iface})).To(BeFalse())
		})

		It("returns true when Ready transitions not-ready → ready", func() {
			Expect(p.Update(event.UpdateEvent{
				ObjectOld: siWithReadyCondition(false),
				ObjectNew: siWithReadyCondition(true),
			})).To(BeTrue())
		})

		It("returns true when Ready transitions ready → not-ready", func() {
			Expect(p.Update(event.UpdateEvent{
				ObjectOld: siWithReadyCondition(true),
				ObjectNew: siWithReadyCondition(false),
			})).To(BeTrue())
		})
	})

	Context("Delete", func() {
		It("always returns true", func() {
			Expect(p.Delete(event.DeleteEvent{Object: siWithReadyCondition(true)})).To(BeTrue())
			Expect(p.Delete(event.DeleteEvent{Object: siWithReadyCondition(false)})).To(BeTrue())
		})
	})

	Context("Generic", func() {
		It("always returns false", func() {
			Expect(p.Generic(event.GenericEvent{Object: siWithReadyCondition(true)})).To(BeFalse())
		})
	})
})

var _ = Describe("newNodeConditionPredicate", func() {
	var p predicate.Funcs

	nodeWithConditions := func(conds ...corev1.NodeCondition) *corev1.Node {
		return &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "test-node"},
			Status:     corev1.NodeStatus{Conditions: conds},
		}
	}

	readyCondition := func(status corev1.ConditionStatus) corev1.NodeCondition {
		return corev1.NodeCondition{Type: corev1.NodeReady, Status: status}
	}

	BeforeEach(func() {
		p = newNodeConditionPredicate()
	})

	Context("CreateFunc", func() {
		It("returns true for nodes that have conditions", func() {
			node := nodeWithConditions(readyCondition(corev1.ConditionTrue))
			Expect(p.CreateFunc(event.CreateEvent{Object: node})).To(BeTrue())
		})

		It("returns false for nodes without conditions", func() {
			node := nodeWithConditions()
			Expect(p.CreateFunc(event.CreateEvent{Object: node})).To(BeFalse())
		})
	})

	Context("UpdateFunc", func() {
		It("returns false when conditions are unchanged", func() {
			oldNode := nodeWithConditions(readyCondition(corev1.ConditionTrue))
			newNode := oldNode.DeepCopy()
			Expect(p.UpdateFunc(event.UpdateEvent{ObjectOld: oldNode, ObjectNew: newNode})).To(BeFalse())
		})

		It("returns true when a condition status changes", func() {
			oldNode := nodeWithConditions(readyCondition(corev1.ConditionTrue))
			newNode := nodeWithConditions(readyCondition(corev1.ConditionFalse))
			Expect(p.UpdateFunc(event.UpdateEvent{ObjectOld: oldNode, ObjectNew: newNode})).To(BeTrue())
		})

		It("returns true when a condition is added", func() {
			oldNode := nodeWithConditions(readyCondition(corev1.ConditionTrue))
			newNode := nodeWithConditions(
				readyCondition(corev1.ConditionTrue),
				corev1.NodeCondition{Type: corev1.NodeMemoryPressure, Status: corev1.ConditionFalse},
			)
			Expect(p.UpdateFunc(event.UpdateEvent{ObjectOld: oldNode, ObjectNew: newNode})).To(BeTrue())
		})
	})

	Context("DeleteFunc", func() {
		It("always returns true", func() {
			Expect(p.DeleteFunc(event.DeleteEvent{Object: nodeWithConditions(readyCondition(corev1.ConditionTrue))})).To(BeTrue())
			Expect(p.DeleteFunc(event.DeleteEvent{Object: nodeWithConditions()})).To(BeTrue())
		})
	})

	Context("GenericFunc", func() {
		It("always returns false", func() {
			Expect(p.GenericFunc(event.GenericEvent{Object: nodeWithConditions(readyCondition(corev1.ConditionTrue))})).To(BeFalse())
		})
	})
})
