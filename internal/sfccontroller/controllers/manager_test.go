/*
Copyright 2025 NVIDIA

Licensed under the Apache License, Version 2.0 (the License);
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an AS IS BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package controller

import (
	"context"
	"reflect"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/internal/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

//nolint:goconst
var _ = Describe("manager test", func() {

	var ctx = context.Background()
	var ns *corev1.Namespace
	var hostPod, otherHostPod *corev1.Pod

	BeforeEach(func() {
		hostPod = nil
		otherHostPod = nil
		ns = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "test-ns-",
			},
		}
		Expect(testClient.Create(ctx, ns)).To(Succeed())
	})

	AfterEach(func() {
		if hostPod != nil {
			Expect(client.IgnoreNotFound(testClient.Delete(ctx, hostPod))).To(Succeed())
		}
		if otherHostPod != nil {
			Expect(client.IgnoreNotFound(testClient.Delete(ctx, otherHostPod))).To(Succeed())
		}
		Expect(testClient.Delete(ctx, ns)).To(Succeed())
	})

	It("cahce only pods on the same node", func() {
		hostPod = &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pod",
				Namespace: ns.Name,
			},
			Spec: corev1.PodSpec{
				NodeName: testNodeName,
				Containers: []corev1.Container{
					{
						Name:  "test-container",
						Image: "test-image",
					},
				},
			},
		}
		Expect(testClient.Create(ctx, hostPod)).To(Succeed())

		otherHostPod = &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "other-test-pod",
				Namespace: ns.Name,
			},
			Spec: corev1.PodSpec{
				NodeName: "other-node",
				Containers: []corev1.Container{
					{
						Name:  "test-container",
						Image: "test-image",
					},
				},
			},
		}
		Expect(testClient.Create(ctx, otherHostPod)).To(Succeed())

		listPods := &corev1.PodList{}
		Expect(mgrClient.List(ctx, listPods)).To(Succeed())
		Expect(listPods.Items).To(HaveLen(1))
		Expect(listPods.Items[0].Name).To(Equal(hostPod.Name))

		By("pod is cahced and expected to be found")
		err := mgrClient.Get(ctx, types.NamespacedName{Name: hostPod.Name, Namespace: ns.Name}, &corev1.Pod{})
		Expect(err).To(Succeed())

		By("pod is not cached and expected to be not found")
		err = mgrClient.Get(ctx, types.NamespacedName{Name: otherHostPod.Name, Namespace: ns.Name}, &corev1.Pod{})
		Expect(err).To(HaveOccurred())
	})

	It("leaves SyncPeriod unset so the controller-runtime default resync applies", func() {
		options := GetMgrCache(testNodeName)
		Expect(options.SyncPeriod).To(BeNil())
	})

	It("selects service chains, service interfaces, and node service interfaces for this node", func() {
		options := GetMgrCache(testNodeName)
		Expect(fieldSelectorFor(options, &dpuservicev1.ServiceChain{})).To(Equal("spec.node=" + testNodeName))
		Expect(fieldSelectorFor(options, &dpuservicev1.ServiceInterface{})).To(Equal("spec.node=" + testNodeName))

		nsiConfig, ok := byObjectFor(options, &dpuservicev1.NodeServiceInterfaces{})
		Expect(ok).To(BeTrue())
		Expect(nsiConfig.Field.String()).To(Equal("spec.node=" + testNodeName))
		Expect(nsiConfig.Namespaces).To(HaveKey(utils.NSIObjectsNamespace))
	})

	It("caches only this node's service chains, service interfaces, and node service interfaces", func() {
		localChain := newTestServiceChain("local-chain", ns.Name, &testNodeName)
		otherChain := newTestServiceChain("other-chain", ns.Name, ptr.To("other-node"))
		unsetChain := newTestServiceChain("unset-chain", ns.Name, nil)
		localSI := newTestServiceInterface("local-si", ns.Name, &testNodeName)
		otherSI := newTestServiceInterface("other-si", ns.Name, ptr.To("other-node"))
		localNSI := newTestNSI("local-nsi", utils.NSIObjectsNamespace, testNodeName)
		otherNSI := newTestNSI("other-nsi", utils.NSIObjectsNamespace, "other-node")
		strayNSI := newTestNSI("stray-nsi", ns.Name, testNodeName)

		for _, obj := range []client.Object{localChain, otherChain, unsetChain, localSI, otherSI, localNSI, otherNSI, strayNSI} {
			Expect(testClient.Create(ctx, obj)).To(Succeed())
		}
		DeferCleanup(func() {
			for _, obj := range []client.Object{localNSI, otherNSI} {
				Expect(client.IgnoreNotFound(testClient.Delete(ctx, obj))).To(Succeed())
			}
		})

		Eventually(func(g Gomega) {
			chains := &dpuservicev1.ServiceChainList{}
			g.Expect(mgrClient.List(ctx, chains, client.InNamespace(ns.Name))).To(Succeed())
			g.Expect(namesOf(chains.Items)).To(ConsistOf("local-chain"))

			sis := &dpuservicev1.ServiceInterfaceList{}
			g.Expect(mgrClient.List(ctx, sis, client.InNamespace(ns.Name))).To(Succeed())
			g.Expect(namesOf(sis.Items)).To(ConsistOf("local-si"))

			nsis := &dpuservicev1.NodeServiceInterfacesList{}
			g.Expect(mgrClient.List(ctx, nsis)).To(Succeed())
			g.Expect(namesOf(nsis.Items)).To(ConsistOf("local-nsi"))
		}).Should(Succeed())

		Expect(apierrors.IsNotFound(mgrClient.Get(ctx, client.ObjectKeyFromObject(otherChain), &dpuservicev1.ServiceChain{}))).To(BeTrue())
		Expect(apierrors.IsNotFound(mgrClient.Get(ctx, client.ObjectKeyFromObject(unsetChain), &dpuservicev1.ServiceChain{}))).To(BeTrue())
		Expect(apierrors.IsNotFound(mgrClient.Get(ctx, client.ObjectKeyFromObject(otherSI), &dpuservicev1.ServiceInterface{}))).To(BeTrue())
		Expect(apierrors.IsNotFound(mgrClient.Get(ctx, client.ObjectKeyFromObject(otherNSI), &dpuservicev1.NodeServiceInterfaces{}))).To(BeTrue())
		// A namespace outside the NSI cache is not watched. Get fails closed instead of reading the API server.
		Expect(mgrClient.Get(ctx, client.ObjectKeyFromObject(strayNSI), &dpuservicev1.NodeServiceInterfaces{})).To(MatchError(ContainSubstring("unknown namespace for the cache")))
		Expect(mgrClient.Get(ctx, client.ObjectKeyFromObject(localChain), &dpuservicev1.ServiceChain{})).To(Succeed())
	})
})

func fieldSelectorFor(options cache.Options, obj client.Object) string {
	by, ok := byObjectFor(options, obj)
	Expect(ok).To(BeTrue())
	Expect(by.Field).NotTo(BeNil())
	return by.Field.String()
}

func byObjectFor(options cache.Options, sample client.Object) (cache.ByObject, bool) {
	sampleType := reflect.TypeOf(sample)
	for obj, by := range options.ByObject {
		if reflect.TypeOf(obj) == sampleType {
			return by, true
		}
	}
	return cache.ByObject{}, false
}

func namesOf[T any](items []T) []string {
	names := make([]string, len(items))
	for i := range items {
		names[i] = any(&items[i]).(interface{ GetName() string }).GetName()
	}
	return names
}

func newTestServiceChain(name, namespace string, node *string) *dpuservicev1.ServiceChain {
	return &dpuservicev1.ServiceChain{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: dpuservicev1.ServiceChainSpec{
			Node: node,
			Switches: []dpuservicev1.Switch{{
				Ports: []dpuservicev1.Port{{
					ServiceInterface: dpuservicev1.ServiceIfc{MatchLabels: map[string]string{"app": "firewall"}},
				}},
			}},
		},
	}
}

func newTestServiceInterface(name, namespace string, node *string) *dpuservicev1.ServiceInterface {
	return &dpuservicev1.ServiceInterface{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: dpuservicev1.ServiceInterfaceSpec{
			Node:          node,
			InterfaceType: dpuservicev1.InterfaceTypePhysical,
			Physical:      &dpuservicev1.Physical{InterfaceName: "p0"},
		},
	}
}

func newTestNSI(name, namespace, node string) *dpuservicev1.NodeServiceInterfaces {
	return &dpuservicev1.NodeServiceInterfaces{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: dpuservicev1.NodeServiceInterfacesSpec{
			Node: node,
			Type: dpuservicev1.NSITypeSFC,
		},
	}
}
