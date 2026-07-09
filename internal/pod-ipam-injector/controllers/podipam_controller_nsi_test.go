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

// This file replicates podipam_controller_test.go with NodeServiceInterfaces (NSI)
// objects as the backing for service interface lookup instead of standalone
// ServiceInterface objects. Tests that exercise functions which do not go through
// the shared resolver (isPodUsingOnlyVirtualNetworks, getNVIPAMPoolByMatchLabels)
// are kept identical to the legacy versions.

package controller //nolint:dupl

import (
	"context"
	"fmt"
	"strings"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/internal/utils"
	testutils "github.com/nvidia/doca-platform/test/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// nsiShardName returns the NSI object name for the test node's sfc shard.
func nsiShardName() string { return nodeName + "-" + dpuservicev1.NSITypeSFC }

// entryName returns the NSI entry name for the given set name in the test namespace.
func entryName(setName string) string { return defaultNS + "_" + setName }

// nsiApply creates or updates the NSI shard for the test node via SSA, adding a
// single service-type entry with the given labels and serviceID/interfaceName.
// fieldManagerSuffix makes the field manager unique per entry so multiple entries
// can coexist in the same shard.
func nsiApply(ctx context.Context, fieldManagerSuffix string, entry dpuservicev1.InterfaceEntry) {
	nsi := &dpuservicev1.NodeServiceInterfaces{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nsiShardName(),
			Namespace: utils.NSIObjectsNamespace,
		},
		Spec: dpuservicev1.NodeServiceInterfacesSpec{
			Node:       nodeName,
			Type:       dpuservicev1.NSITypeSFC,
			Interfaces: []dpuservicev1.InterfaceEntry{entry},
		},
	}
	nsi.SetManagedFields(nil)
	nsi.SetGroupVersionKind(dpuservicev1.NodeServiceInterfacesGroupVersionKind)
	Expect(testClient.Patch(ctx, nsi, client.Apply, client.ForceOwnership,
		client.FieldOwner("test-nsi-"+fieldManagerSuffix))).To(Succeed())
}

// createNSIForService creates an NSI entry equivalent to createServiceInterfaceForService.
func createNSIForService(ctx context.Context, entrySetName, ifc string) {
	entry := dpuservicev1.InterfaceEntry{
		Name: entryName(entrySetName),
		Labels: map[string]string{
			dpuservicev1.DPFServiceIDLabelKey: serviceName,
			serviceInterfaceAnnotKey:          ifc,
		},
		InterfaceType: dpuservicev1.InterfaceTypeService,
		Service: &dpuservicev1.ServiceDef{
			ServiceID:     serviceName,
			InterfaceName: ifc,
		},
	}
	nsiApply(ctx, entrySetName, entry)
}

// deleteNSIShard deletes the test NSI shard if it exists.
func deleteNSIShard(ctx context.Context) {
	nsi := &dpuservicev1.NodeServiceInterfaces{}
	err := testClient.Get(ctx, client.ObjectKey{Namespace: utils.NSIObjectsNamespace, Name: nsiShardName()}, nsi)
	if apierrors.IsNotFound(err) {
		return
	}
	Expect(err).NotTo(HaveOccurred())
	Expect(testClient.Delete(ctx, nsi)).To(Succeed())
	Eventually(func() bool {
		err := testClient.Get(ctx, client.ObjectKey{Namespace: utils.NSIObjectsNamespace, Name: nsiShardName()}, &dpuservicev1.NodeServiceInterfaces{})
		return apierrors.IsNotFound(err)
	}).WithTimeout(5 * time.Second).Should(BeTrue())
}

//nolint:dupl
var _ = Describe("PodIpam Controller — NSI path", func() {
	Context("When reconciling a resource", func() {
		var cleanupObjects []client.Object

		BeforeEach(func() {
			cleanupObjects = []client.Object{}
		})

		AfterEach(func() {
			By("Cleaning up the objects")
			Expect(testutils.CleanupAndWait(ctx, testClient, cleanupObjects...)).To(Succeed())
			deleteNSIShard(ctx)
		})

		It("should successfully update Network annotation on Pod - IPAM match labels IPPool", func() {
			By("Create NSI entry for Service")
			createNSIForService(ctx, strings.Join([]string{serviceName, ifcName}, "-"), ifcName)
			By("Create ServiceChain with IPAM MatchLabels")
			defaultGateway := false
			ipam := &dpuservicev1.IPAM{
				DefaultGateway: ptr.To(defaultGateway),
				MatchLabels:    ipamLabels,
			}
			cleanupObjects = append(cleanupObjects, createServiceChainWithServiceInterface(ctx, svcName1, ipam, serviceName, ifcName, ptr.To(4000)))
			By("Create IPPool")
			cleanupObjects = append(cleanupObjects, createIPPool(ctx, ipamName, ipamLabels))
			By("Create Pod with Network Annotation")
			cleanupObjects = append(cleanupObjects, createPodWithNetworkAnnotation(ctx, singleNetAnnotationWithInvalid(ifcName)))
			By("Check that Pod annotation has been updated")
			Eventually(func(g Gomega) {
				pod := &corev1.Pod{}
				g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: defaultNS, Name: podName}, pod)).To(Succeed())
				g.Expect(pod.Annotations[multusKey]).To(BeEquivalentTo(expectedSingleNetAnnotationWithIPAM(ifcName, "ippool", defaultGateway, 4000)))
			}).WithTimeout(2 * time.Second).Should(BeNil())
			By("Turning the Pod State to Succeed")
			changePodState(ctx, corev1.PodSucceeded)
		})

		It("should successfully update Network annotation on Pod - IPAM match labels CIDRPool", func() {
			By("Create NSI entry for Service")
			createNSIForService(ctx, strings.Join([]string{serviceName, ifcName}, "-"), ifcName)
			By("Create ServiceChain with IPAM MatchLabels")
			defaultGateway := true
			ipam := &dpuservicev1.IPAM{
				DefaultGateway: ptr.To(defaultGateway),
				MatchLabels:    ipamLabels,
			}
			cleanupObjects = append(cleanupObjects, createServiceChainWithServiceInterface(ctx, svcName1, ipam, serviceName, ifcName, nil))
			By("Create CIDRPool")
			cleanupObjects = append(cleanupObjects, createCidrPool(ctx, ipamName, ipamLabels))
			By("Create Pod with Network Annotation")
			cleanupObjects = append(cleanupObjects, createPodWithNetworkAnnotation(ctx, singleNetAnnotationWithInvalid(ifcName)))
			By("Check that Pod annotation has been updated")
			Eventually(func(g Gomega) {
				pod := &corev1.Pod{}
				g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: defaultNS, Name: podName}, pod)).To(Succeed())
				g.Expect(pod.Annotations[multusKey]).To(BeEquivalentTo(expectedSingleNetAnnotationWithIPAM(ifcName, "cidrpool", defaultGateway, 1500)))
			}).WithTimeout(2 * time.Second).Should(BeNil())
			By("Turning the Pod State to Succeed")
			changePodState(ctx, corev1.PodSucceeded)
		})

		It("should successfully update Network annotation on Pod - Multiple networks, Multiple IPAM, Multiple MTU", func() {
			By("Create NSI entries for Service")
			createNSIForService(ctx, strings.Join([]string{serviceName, ifcName}, "-"), ifcName)
			createNSIForService(ctx, strings.Join([]string{serviceName, ifcName2}, "-"), ifcName2)
			By("Create ServiceChain1 with IPAM MatchLabels")
			defaultGateway := false
			ipam := &dpuservicev1.IPAM{
				DefaultGateway: ptr.To(defaultGateway),
				MatchLabels:    ipamLabels,
			}
			cleanupObjects = append(cleanupObjects, createServiceChainWithServiceInterface(ctx, svcName1, ipam, serviceName, ifcName, ptr.To(3000)))
			By("Create ServiceChain2 with IPAM ref")
			ipam2 := &dpuservicev1.IPAM{
				DefaultGateway: ptr.To(defaultGateway),
				MatchLabels:    ipamLabels2,
			}
			cleanupObjects = append(cleanupObjects, createServiceChainWithServiceInterface(ctx, svcName2, ipam2, serviceName, ifcName2, nil))
			By("Create IpPool1")
			cleanupObjects = append(cleanupObjects, createIPPool(ctx, ipamName, ipamLabels))
			By("Create IpPool2")
			cleanupObjects = append(cleanupObjects, createIPPool(ctx, ipamName2, ipamLabels2))
			By("Create Pod with Network Annotation - multiple networks")
			cleanupObjects = append(cleanupObjects, createPodWithNetworkAnnotation(ctx, multipleNetAnnotation()))
			By("Check that Pod annotation has been updated")
			Eventually(func(g Gomega) {
				pod := &corev1.Pod{}
				g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: defaultNS, Name: podName}, pod)).To(Succeed())
				g.Expect(pod.Annotations[multusKey]).To(BeEquivalentTo(expectedMultipleNetAnnotation("ippool", defaultGateway, 3000, 1500)))
			}).WithTimeout(2 * time.Second).Should(BeNil())
			By("Turning the Pod State to Succeed")
			changePodState(ctx, corev1.PodSucceeded)
		})

		It("should successfully update Network annotation on Pod - Pod Created before Chain", func() {
			defaultGateway := false
			By("Create Pod with Network Annotation")
			cleanupObjects = append(cleanupObjects, createPodWithNetworkAnnotation(ctx, singleNetAnnotationWithInvalid(ifcName)))
			By("Verify Pod annotations is not updated")
			Consistently(func(g Gomega) {
				pod := &corev1.Pod{}
				g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: defaultNS, Name: podName}, pod)).To(Succeed())
				g.Expect(pod.Annotations[multusKey]).To(BeEquivalentTo(singleNetAnnotationWithInvalid(ifcName)))
			}).WithTimeout(2 * time.Second).Should(Succeed())
			By("Create ServiceChain with IPAM MatchLabels")
			ipam := &dpuservicev1.IPAM{
				DefaultGateway: ptr.To(defaultGateway),
				MatchLabels:    ipamLabels,
			}
			cleanupObjects = append(cleanupObjects, createServiceChainWithServiceInterface(ctx, svcName1, ipam, serviceName, ifcName, nil))
			By("Verify Pod annotations is not updated")
			Consistently(func(g Gomega) {
				pod := &corev1.Pod{}
				g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: defaultNS, Name: podName}, pod)).To(Succeed())
				g.Expect(pod.Annotations[multusKey]).To(BeEquivalentTo(singleNetAnnotationWithInvalid(ifcName)))
			}).WithTimeout(2 * time.Second).Should(Succeed())
			By("Create NSI entry for Service")
			createNSIForService(ctx, strings.Join([]string{serviceName, ifcName}, "-"), ifcName)
			By("Verify Pod annotations is not updated")
			Consistently(func(g Gomega) {
				pod := &corev1.Pod{}
				g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: defaultNS, Name: podName}, pod)).To(Succeed())
				g.Expect(pod.Annotations[multusKey]).To(BeEquivalentTo(singleNetAnnotationWithInvalid(ifcName)))
			}).WithTimeout(2 * time.Second).Should(Succeed())
			By("Create IpPool")
			cleanupObjects = append(cleanupObjects, createIPPool(ctx, ipamName, ipamLabels))
			By("Check that Pod annotation has been updated")
			Eventually(func(g Gomega) {
				pod := &corev1.Pod{}
				g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: defaultNS, Name: podName}, pod)).To(Succeed())
				g.Expect(pod.Annotations[multusKey]).To(BeEquivalentTo(expectedSingleNetAnnotationWithIPAM(ifcName, "ippool", defaultGateway, 1500)))
			}).WithTimeout(30 * time.Second).Should(Succeed())
			By("Turning the Pod State to Succeed")
			changePodState(ctx, corev1.PodSucceeded)
		})

		It("should not update Network annotation on Pod - no IPAM requested", func() {
			By("Create NSI entry for Service")
			createNSIForService(ctx, strings.Join([]string{serviceName, ifcName}, "-"), ifcName)
			By("Create ServiceChain without IPAM")
			cleanupObjects = append(cleanupObjects, createServiceChainWithServiceInterface(ctx, svcName1, nil, serviceName, ifcName, nil))
			By("Create Pod with Network Annotation")
			cleanupObjects = append(cleanupObjects, createPodWithNetworkAnnotation(ctx, singleNetAnnotationWithInvalid(ifcName)))
			By("Check that Pod annotation has been updated")
			Eventually(func(g Gomega) {
				pod := &corev1.Pod{}
				g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: defaultNS, Name: podName}, pod)).To(Succeed())
				g.Expect(pod.Annotations[multusKey]).To(BeEquivalentTo(expectedSingleNetAnnotationWithoutIPAM(1500)))
			}).WithTimeout(2 * time.Second).Should(BeNil())
			By("Turning the Pod State to Succeed")
			changePodState(ctx, corev1.PodSucceeded)
		})

		It("should update network annotation with correct mtu and no IPAM", func() {
			By("Create NSI entry for Service")
			createNSIForService(ctx, strings.Join([]string{serviceName, ifcName}, "-"), ifcName)
			By("Create ServiceChain without IPAM but with MTU 3000")
			cleanupObjects = append(cleanupObjects, createServiceChainWithServiceInterface(ctx, svcName1, nil, serviceName, ifcName, ptr.To(3000)))
			By("Create Pod with Network Annotation")
			cleanupObjects = append(cleanupObjects, createPodWithNetworkAnnotation(ctx, singleNetAnnotationWithInvalid(ifcName)))
			By("Check that Pod annotation has been updated")
			Eventually(func(g Gomega) {
				pod := &corev1.Pod{}
				g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: defaultNS, Name: podName}, pod)).To(Succeed())
				g.Expect(pod.Annotations[multusKey]).To(BeEquivalentTo(expectedSingleNetAnnotationWithoutIPAM(3000)))
			}).WithTimeout(2 * time.Second).Should(BeNil())
			By("Turning the Pod State to Succeed")
			changePodState(ctx, corev1.PodSucceeded)
		})

		It("should skip ServiceChains and NSI entries that don't match pod requirements", func() {
			By("Create NSI entry for the correct service")
			createNSIForService(ctx, strings.Join([]string{serviceName, ifcName}, "-"), ifcName)

			By("Create NSI entry on a different namespace (should be invisible to default-ns lookup)")
			nsiApply(ctx, "other-ns-entry", dpuservicev1.InterfaceEntry{
				Name: "other-ns_other-set",
				Labels: map[string]string{
					dpuservicev1.DPFServiceIDLabelKey: "other-service",
				},
				InterfaceType: dpuservicev1.InterfaceTypeService,
				Service:       &dpuservicev1.ServiceDef{ServiceID: "other-service", InterfaceName: "other-ifc"},
			})

			By("Create ServiceChain on wrong node (should be skipped)")
			wrongNodeChain := &dpuservicev1.ServiceChain{
				ObjectMeta: metav1.ObjectMeta{Name: "wrong-node-chain", Namespace: defaultNS},
				Spec: dpuservicev1.ServiceChainSpec{
					Node: ptr.To("other-node"),
					Switches: []dpuservicev1.Switch{
						{
							ServiceMTU: ptr.To(1500),
							Ports: []dpuservicev1.Port{
								{ServiceInterface: dpuservicev1.ServiceIfc{
									MatchLabels: map[string]string{dpuservicev1.DPFServiceIDLabelKey: "other-service"},
								}},
							},
						},
					},
				},
			}
			Expect(testClient.Create(ctx, wrongNodeChain)).To(Succeed())
			cleanupObjects = append(cleanupObjects, wrongNodeChain)

			By("Create correct ServiceChain with MTU")
			cleanupObjects = append(cleanupObjects, createServiceChainWithServiceInterface(ctx, svcName1, nil, serviceName, ifcName, ptr.To(4000)))
			By("Create Pod with Network Annotation")
			cleanupObjects = append(cleanupObjects, createPodWithNetworkAnnotation(ctx, singleNetAnnotationWithInvalid(ifcName)))
			By("Check that Pod annotation has been updated with correct ServiceChain only")
			Eventually(func(g Gomega) {
				pod := &corev1.Pod{}
				g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: defaultNS, Name: podName}, pod)).To(Succeed())
				g.Expect(pod.Annotations[multusKey]).To(BeEquivalentTo(expectedSingleNetAnnotationWithoutIPAM(4000)))
			}).WithTimeout(5 * time.Second).Should(Succeed())
			By("Turning the Pod State to Succeed")
			changePodState(ctx, corev1.PodSucceeded)
		})

		It("should not fail when unrelated ServiceChain has faulty selector", func() {
			By("Create NSI entry for the pod's service")
			createNSIForService(ctx, strings.Join([]string{serviceName, ifcName}, "-"), ifcName)
			By("Create unrelated ServiceChain with faulty selector")
			faultyChain := &dpuservicev1.ServiceChain{
				ObjectMeta: metav1.ObjectMeta{Name: "faulty-chain-nsi", Namespace: defaultNS},
				Spec: dpuservicev1.ServiceChainSpec{
					Node: ptr.To(nodeName),
					Switches: []dpuservicev1.Switch{
						{
							ServiceMTU: ptr.To(2000),
							Ports: []dpuservicev1.Port{
								{ServiceInterface: dpuservicev1.ServiceIfc{
									MatchLabels: map[string]string{"uplink": "p0"},
								}},
							},
						},
					},
				},
			}
			Expect(testClient.Create(ctx, faultyChain)).To(Succeed())
			cleanupObjects = append(cleanupObjects, faultyChain)

			By("Create correct ServiceChain with IPAM")
			defaultGateway := true
			ipam := &dpuservicev1.IPAM{DefaultGateway: ptr.To(defaultGateway), MatchLabels: ipamLabels}
			cleanupObjects = append(cleanupObjects, createServiceChainWithServiceInterface(ctx, svcName1, ipam, serviceName, ifcName, ptr.To(4000)))
			By("Create IPPool")
			cleanupObjects = append(cleanupObjects, createIPPool(ctx, ipamName, ipamLabels))
			By("Create Pod with Network Annotation")
			cleanupObjects = append(cleanupObjects, createPodWithNetworkAnnotation(ctx, singleNetAnnotationWithInvalid(ifcName)))
			By("Check that Pod annotation has been updated correctly despite faulty ServiceChain")
			Eventually(func(g Gomega) {
				pod := &corev1.Pod{}
				g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: defaultNS, Name: podName}, pod)).To(Succeed())
				g.Expect(pod.Annotations[multusKey]).To(BeEquivalentTo(expectedSingleNetAnnotationWithIPAM(ifcName, "ippool", defaultGateway, 4000)))
			}).WithTimeout(5 * time.Second).Should(Succeed())
			By("Turning the Pod State to Succeed")
			changePodState(ctx, corev1.PodSucceeded)
		})

		It("should not error for interfaces with pre-populated CNI args when ServiceChain is missing (HBN case)", func() {
			By("Create NSI entry for the physical interface")
			createNSIForService(ctx, strings.Join([]string{serviceName, ifcName}, "-"), ifcName)
			By("Create ServiceChain only for the physical interface")
			cleanupObjects = append(cleanupObjects, createServiceChainWithServiceInterface(ctx, svcName1, nil, serviceName, ifcName, ptr.To(1500)))
			By("Create Pod with both IPAM interface (with CNI args) and physical interface (without CNI args)")
			hbnAnnotation := fmt.Sprintf(`[{"name":"iprequest","interface":"ip_pf0vf3","cni-args":{"poolNames":["pool1"],"poolType":"cidrpool"}},{"name":"mybrsfc","interface":"%s"},{"name":"invalid-network","namespace":"invalid-namespace","interface":"invalid-interface"}]`, ifcName)
			cleanupObjects = append(cleanupObjects, createPodWithNetworkAnnotation(ctx, hbnAnnotation))
			By("Check that Pod annotation has been updated correctly")
			Eventually(func(g Gomega) {
				pod := &corev1.Pod{}
				g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: defaultNS, Name: podName}, pod)).To(Succeed())
				expectedAnnot := fmt.Sprintf(`[{"name":"iprequest","namespace":"default","interface":"ip_pf0vf3","cni-args":{"poolNames":["pool1"],"poolType":"cidrpool"}},{"name":"mybrsfc","namespace":"default","interface":"%s","cni-args":{"mtu":1500}}]`, ifcName)
				g.Expect(pod.Annotations[multusKey]).To(BeEquivalentTo(expectedAnnot))
			}).WithTimeout(5 * time.Second).Should(Succeed())
			By("Turning the Pod State to Succeed")
			changePodState(ctx, corev1.PodSucceeded)
		})

		It("should merge MTU into pre-populated CNI args when ServiceChain exists for IPAM interface", func() {
			By("Create NSI entry for the IPAM interface")
			nsiApply(ctx, "ipam-ifc", dpuservicev1.InterfaceEntry{
				Name: entryName("ipam-interface"),
				Labels: map[string]string{
					dpuservicev1.DPFServiceIDLabelKey: serviceName,
					serviceInterfaceAnnotKey:          "ip_pf0vf3",
				},
				InterfaceType: dpuservicev1.InterfaceTypeService,
				Service: &dpuservicev1.ServiceDef{
					ServiceID:     serviceName,
					InterfaceName: "ip_pf0vf3",
				},
			})
			By("Create NSI entry for the physical interface")
			createNSIForService(ctx, strings.Join([]string{serviceName, ifcName}, "-"), ifcName)
			By("Create ServiceChain for IPAM interface with MTU 9000")
			ipamChain := &dpuservicev1.ServiceChain{
				ObjectMeta: metav1.ObjectMeta{Name: "ipam-chain-nsi", Namespace: defaultNS},
				Spec: dpuservicev1.ServiceChainSpec{
					Node: ptr.To(nodeName),
					Switches: []dpuservicev1.Switch{{
						ServiceMTU: ptr.To(9000),
						Ports: []dpuservicev1.Port{{ServiceInterface: dpuservicev1.ServiceIfc{
							MatchLabels: map[string]string{
								dpuservicev1.DPFServiceIDLabelKey: serviceName,
								serviceInterfaceAnnotKey:          "ip_pf0vf3",
							},
						}}},
					}},
				},
			}
			Expect(testClient.Create(ctx, ipamChain)).To(Succeed())
			cleanupObjects = append(cleanupObjects, ipamChain)
			cleanupObjects = append(cleanupObjects, createServiceChainWithServiceInterface(ctx, svcName1, nil, serviceName, ifcName, ptr.To(1500)))
			By("Create Pod with IPAM interface that has pre-populated CNI args")
			hbnAnnotation := fmt.Sprintf(`[{"name":"iprequest","interface":"ip_pf0vf3","cni-args":{"poolNames":["pool1"],"poolType":"cidrpool","allocateDefaultGateway":true}},{"name":"mybrsfc","interface":"%s"},{"name":"invalid-network","namespace":"invalid-namespace","interface":"invalid-interface"}]`, ifcName)
			cleanupObjects = append(cleanupObjects, createPodWithNetworkAnnotation(ctx, hbnAnnotation))
			By("Check that Pod annotation has been updated with MTU merged into existing CNI args")
			Eventually(func(g Gomega) {
				pod := &corev1.Pod{}
				g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: defaultNS, Name: podName}, pod)).To(Succeed())
				expectedAnnot := fmt.Sprintf(`[{"name":"iprequest","namespace":"default","interface":"ip_pf0vf3","cni-args":{"allocateDefaultGateway":true,"mtu":9000,"poolNames":["pool1"],"poolType":"cidrpool"}},{"name":"mybrsfc","namespace":"default","interface":"%s","cni-args":{"mtu":1500}}]`, ifcName)
				g.Expect(pod.Annotations[multusKey]).To(BeEquivalentTo(expectedAnnot))
			}).WithTimeout(5 * time.Second).Should(Succeed())
			By("Turning the Pod State to Succeed")
			changePodState(ctx, corev1.PodSucceeded)
		})
	})

	Context("When calculating network digests — NSI path", func() {
		var cleanupObjects []client.Object

		BeforeEach(func() {
			cleanupObjects = []client.Object{}
		})

		AfterEach(func() {
			Expect(testutils.CleanupAndWait(ctx, testClient, cleanupObjects...)).To(Succeed())
			deleteNSIShard(ctx)
		})

		It("should calculate digest for networks with IPAM and MTU", func() {
			createNSIForService(ctx, strings.Join([]string{serviceName, ifcName}, "-"), ifcName)
			defaultGateway := true
			ipam := &dpuservicev1.IPAM{DefaultGateway: ptr.To(defaultGateway), MatchLabels: ipamLabels}
			cleanupObjects = append(cleanupObjects, createServiceChainWithServiceInterface(ctx, svcName1, ipam, serviceName, ifcName, ptr.To(4000)))
			cleanupObjects = append(cleanupObjects, createIPPool(ctx, ipamName, ipamLabels))
			pod := createTestPodInMemory(
				map[string]string{multusKey: singleNetAnnotationWithInvalid(ifcName)},
				map[string]string{dpuservicev1.DPFServiceIDLabelKey: serviceName})
			networks, _ := GetPodNetworks(pod)
			digest := calculateDigestEventually(ctx, pod, networks)
			networks2, err := GetPodNetworks(pod)
			Expect(err).NotTo(HaveOccurred())
			digest2, _ := CalculatePodNetworkDigest(ctx, testClient, pod, networks2)
			Expect(digest2).To(Equal(digest))
		})

		It("should calculate digest for networks without IPAM", func() {
			createNSIForService(ctx, strings.Join([]string{serviceName, ifcName}, "-"), ifcName)
			cleanupObjects = append(cleanupObjects, createServiceChainWithServiceInterface(ctx, svcName1, nil, serviceName, ifcName, ptr.To(3000)))
			pod := createTestPodInMemory(
				map[string]string{multusKey: singleNetAnnotationWithInvalid(ifcName)},
				map[string]string{dpuservicev1.DPFServiceIDLabelKey: serviceName})
			networks, err := GetPodNetworks(pod)
			Expect(err).NotTo(HaveOccurred())
			digest := calculateDigestEventually(ctx, pod, networks)
			Expect(digest).NotTo(BeEmpty())
		})
	})

	Context("getServiceInterfaceWithLabels — NSI path", func() {
		var cleanupObjects []client.Object

		BeforeEach(func() { cleanupObjects = []client.Object{} })
		AfterEach(func() {
			Expect(testutils.CleanupAndWait(ctx, testClient, cleanupObjects...)).To(Succeed())
			deleteNSIShard(ctx)
		})

		It("should return error when no matching NSI entry is found", func() {
			_, err := getServiceInterfaceWithLabels(ctx, testClient, nodeName, defaultNS, map[string]string{
				dpuservicev1.DPFServiceIDLabelKey: "non-existent-service",
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no serviceInterface"))
		})

		It("should return error when multiple matching NSI entries are found", func() {
			nsiApply(ctx, "dup-1", dpuservicev1.InterfaceEntry{
				Name:          entryName("dup-set-1"),
				Labels:        map[string]string{dpuservicev1.DPFServiceIDLabelKey: "duplicate-nsi-service"},
				InterfaceType: dpuservicev1.InterfaceTypeService,
				Service:       &dpuservicev1.ServiceDef{ServiceID: "duplicate-nsi-service", InterfaceName: "ifc1"},
			})
			nsiApply(ctx, "dup-2", dpuservicev1.InterfaceEntry{
				Name:          entryName("dup-set-2"),
				Labels:        map[string]string{dpuservicev1.DPFServiceIDLabelKey: "duplicate-nsi-service"},
				InterfaceType: dpuservicev1.InterfaceTypeService,
				Service:       &dpuservicev1.ServiceDef{ServiceID: "duplicate-nsi-service", InterfaceName: "ifc2"},
			})
			Eventually(func(g Gomega) {
				_, err := getServiceInterfaceWithLabels(ctx, testClient, nodeName, defaultNS, map[string]string{
					dpuservicev1.DPFServiceIDLabelKey: "duplicate-nsi-service",
				})
				g.Expect(err).To(HaveOccurred())
				// The NSI path returns "found 2"; legacy path would return "expected only one"
				g.Expect(err.Error()).To(SatisfyAny(
					ContainSubstring("found 2"),
					ContainSubstring("expected only one"),
				))
			}).WithTimeout(2 * time.Second).Should(Succeed())
		})

		It("should filter out NSI entries belonging to a different namespace", func() {
			// Entry owned by "other-ns" is invisible to a lookup in defaultNS.
			nsiApply(ctx, "cross-ns", dpuservicev1.InterfaceEntry{
				Name:          "other-ns_nsi-cross-ns-set",
				Labels:        map[string]string{dpuservicev1.DPFServiceIDLabelKey: "nsi-test-service"},
				InterfaceType: dpuservicev1.InterfaceTypeService,
				Service:       &dpuservicev1.ServiceDef{ServiceID: "nsi-test-service", InterfaceName: "ifc1"},
			})
			Eventually(func(g Gomega) {
				_, err := getServiceInterfaceWithLabels(ctx, testClient, nodeName, defaultNS, map[string]string{
					dpuservicev1.DPFServiceIDLabelKey: "nsi-test-service",
				})
				g.Expect(err).To(HaveOccurred())
				g.Expect(err.Error()).To(ContainSubstring("no serviceInterface"))
			}).WithTimeout(2 * time.Second).Should(Succeed())
		})

		It("should successfully return NSI-backed interface when single match exists", func() {
			nsiApply(ctx, "correct-set", dpuservicev1.InterfaceEntry{
				Name:          entryName("nsi-correct-set"),
				Labels:        map[string]string{dpuservicev1.DPFServiceIDLabelKey: "nsi-correct-service"},
				InterfaceType: dpuservicev1.InterfaceTypeService,
				Service:       &dpuservicev1.ServiceDef{ServiceID: "nsi-correct-service", InterfaceName: "ifc1"},
			})
			Eventually(func(g Gomega) {
				result, err := getServiceInterfaceWithLabels(ctx, testClient, nodeName, defaultNS, map[string]string{
					dpuservicev1.DPFServiceIDLabelKey: "nsi-correct-service",
				})
				g.Expect(err).NotTo(HaveOccurred())
				// The synthetic SI name is the set name decoded from the entry name.
				g.Expect(result.Name).To(Equal("nsi-correct-set"))
				g.Expect(*result.Spec.Node).To(Equal(nodeName))
				g.Expect(result.Spec.InterfaceType).To(Equal(dpuservicev1.InterfaceTypeService))
			}).WithTimeout(2 * time.Second).Should(Succeed())
		})

		It("should fall back to legacy ServiceInterface when NSI has no matching entry", func() {
			// NSI shard exists but has a non-matching entry.
			nsiApply(ctx, "non-match", dpuservicev1.InterfaceEntry{
				Name:          entryName("other-set"),
				Labels:        map[string]string{dpuservicev1.DPFServiceIDLabelKey: "other-service"},
				InterfaceType: dpuservicev1.InterfaceTypeService,
				Service:       &dpuservicev1.ServiceDef{ServiceID: "other-service", InterfaceName: "ifc1"},
			})
			// Legacy ServiceInterface with the label we're looking for.
			si := &dpuservicev1.ServiceInterface{
				ObjectMeta: metav1.ObjectMeta{
					Name: "legacy-fallback-si", Namespace: defaultNS,
					Labels: map[string]string{dpuservicev1.DPFServiceIDLabelKey: "legacy-fallback-service"},
				},
				Spec: dpuservicev1.ServiceInterfaceSpec{
					InterfaceType: dpuservicev1.InterfaceTypeService,
					Node:          ptr.To(nodeName),
					Service:       &dpuservicev1.ServiceDef{ServiceID: "legacy-fallback-service", InterfaceName: "ifc1"},
				},
			}
			Expect(testClient.Create(ctx, si)).To(Succeed())
			cleanupObjects = append(cleanupObjects, si)
			Eventually(func(g Gomega) {
				result, err := getServiceInterfaceWithLabels(ctx, testClient, nodeName, defaultNS, map[string]string{
					dpuservicev1.DPFServiceIDLabelKey: "legacy-fallback-service",
				})
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(result.Name).To(Equal("legacy-fallback-si"))
			}).WithTimeout(2 * time.Second).Should(Succeed())
		})
	})

})
