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

package controller //nolint:dupl

import (
	"context"
	"fmt"
	"strings"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/internal/utils"
	testutils "github.com/nvidia/doca-platform/test/utils"
	nvipamv1 "github.com/nvidia/doca-platform/third_party/api/nvipam/api/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	multustypes "gopkg.in/k8snetworkplumbingwg/multus-cni.v4/pkg/types"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	svcName1                 = "chain1"
	svcName2                 = "chain2"
	defaultNS                = "default"
	ifcName                  = "sfceth1"
	ifcName2                 = "sfceth2"
	ipamName                 = "pool-1"
	ipamName2                = "pool-2"
	podName                  = "test-pod"
	multusKey                = "k8s.v1.cni.cncf.io/networks"
	nodeName                 = "worker-1"
	serviceName              = "firewall"
	serviceInterfaceAnnotKey = "svc.dpu.nvidia.com/interface"
)

var (
	ipamLabels = map[string]string{
		"svc.dpu.nvidia.com/pool": ipamName,
	}
	ipamLabels2 = map[string]string{
		"svc.dpu.nvidia.com/pool": ipamName2,
	}
)

//nolint:dupl
var _ = Describe("PodIpam Controller", func() {
	Context("When reconciling a resource", func() {
		var cleanupObjects []client.Object

		BeforeEach(func() {
			cleanupObjects = []client.Object{}
		})

		AfterEach(func() {
			By("Cleaning up the objects")
			Expect(testutils.CleanupAndWait(ctx, testClient, cleanupObjects...)).To(Succeed())
		})

		It("should successfully update Network annotation on Pod - IPAM match labels IPPool", func() {
			By("Create ServiceInterface for Service")
			cleanupObjects = append(cleanupObjects, createServiceInterfaceForService(
				ctx, strings.Join([]string{serviceName, ifcName}, "-"), serviceName, ifcName))
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
			By("Create ServiceInterface for Service")
			cleanupObjects = append(cleanupObjects, createServiceInterfaceForService(
				ctx, strings.Join([]string{serviceName, ifcName}, "-"), serviceName, ifcName))
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
			By("Create ServiceInterfaces for Service")
			cleanupObjects = append(cleanupObjects, createServiceInterfaceForService(
				ctx, strings.Join([]string{serviceName, ifcName}, "-"), serviceName, ifcName))
			cleanupObjects = append(cleanupObjects, createServiceInterfaceForService(
				ctx, strings.Join([]string{serviceName, ifcName2}, "-"), serviceName, ifcName2))
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
			By("Create ServiceInterface for Service")
			cleanupObjects = append(cleanupObjects, createServiceInterfaceForService(
				ctx, strings.Join([]string{serviceName, ifcName}, "-"), serviceName, ifcName))
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

		It("should not update Network annotation on Pod - no network", func() {
			By("Create Pod without Network Annotation")
			cleanupObjects = append(cleanupObjects, createPodWithNetworkAnnotation(ctx, ""))
			By("Check that Pod annotation has not been updated")
			Consistently(func(g Gomega) {
				pod := &corev1.Pod{}
				g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: defaultNS, Name: podName}, pod)).To(Succeed())
				g.Expect(pod.Annotations[multusKey]).To(Equal(""))
			}).WithTimeout(2 * time.Second).Should(BeNil())
			By("Turning the Pod State to Succeed")
			changePodState(ctx, corev1.PodSucceeded)
		})

		It("should not update Network annotation on Pod - no interface requested", func() {
			By("Create Pod with Network Annotation, no interface requested")
			annotation := `[{"name":"mybrsfc"}]`
			cleanupObjects = append(cleanupObjects, createPodWithNetworkAnnotation(ctx, annotation))
			By("Check that Pod annotation has not been updated")
			Consistently(func(g Gomega) {
				pod := &corev1.Pod{}
				g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: defaultNS, Name: podName}, pod)).To(Succeed())
				g.Expect(pod.Annotations[multusKey]).To(BeEquivalentTo(annotation))
			}).WithTimeout(2 * time.Second).Should(BeNil())
			By("Turning the Pod State to Succeed")
			changePodState(ctx, corev1.PodSucceeded)
		})

		It("should not update Network annotation on Pod - no IPAM requested", func() {
			By("Create ServiceInterface for Service")
			cleanupObjects = append(cleanupObjects, createServiceInterfaceForService(
				ctx, strings.Join([]string{serviceName, ifcName}, "-"), serviceName, ifcName))
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

		It("should not update network annotation if all of the settings can't be collected and no invalid annotation", func() {
			By("Create Pod with Network Annotation")
			cleanupObjects = append(cleanupObjects, createPodWithNetworkAnnotation(ctx, singleNetAnnotation()))
			By("Check that Pod annotation has not been updated")
			Consistently(func(g Gomega) {
				pod := &corev1.Pod{}
				g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: defaultNS, Name: podName}, pod)).To(Succeed())
				g.Expect(pod.Annotations[multusKey]).To(BeEquivalentTo(singleNetAnnotation()))
			}).WithTimeout(5 * time.Second).Should(BeNil())
			By("Turning the Pod State to Succeed")
			changePodState(ctx, corev1.PodSucceeded)
		})

		It("should not update network annotation if all of the settings can't be collected and invalid annotation exists", func() {
			By("Create Pod with Network Annotation")
			cleanupObjects = append(cleanupObjects, createPodWithNetworkAnnotation(ctx, singleNetAnnotationWithInvalid(ifcName)))
			By("Check that Pod annotation has not been updated")
			Consistently(func(g Gomega) {
				pod := &corev1.Pod{}
				g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: defaultNS, Name: podName}, pod)).To(Succeed())
				g.Expect(pod.Annotations[multusKey]).To(BeEquivalentTo(singleNetAnnotationWithInvalid(ifcName)))
			}).WithTimeout(5 * time.Second).Should(BeNil())
			By("Turning the Pod State to Succeed")
			changePodState(ctx, corev1.PodSucceeded)
		})

		// TODO: Check if this is ok or change logic
		It("should not update network annotation if interface referenced in the chain is missing and invalid annotation exists on pod", func() {
			By("Create ServiceChain that references interface not related to the pod")
			cleanupObjects = append(cleanupObjects, createServiceChainWithServiceInterface(ctx, svcName1, nil, serviceName, "randomifc", nil))
			By("Create Pod with Network Annotation")
			cleanupObjects = append(cleanupObjects, createPodWithNetworkAnnotation(ctx, singleNetAnnotationWithInvalid(ifcName)))
			By("Check that Pod annotation has not been updated")
			Consistently(func(g Gomega) {
				pod := &corev1.Pod{}
				g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: defaultNS, Name: podName}, pod)).To(Succeed())
				g.Expect(pod.Annotations[multusKey]).To(BeEquivalentTo(singleNetAnnotationWithInvalid(ifcName)))
			}).WithTimeout(5 * time.Second).Should(BeNil())
			By("Turning the Pod State to Succeed")
			changePodState(ctx, corev1.PodSucceeded)
		})

		It("should update network annotation with correct mtu and no IPAM", func() {
			By("Create ServiceInterface for Service")
			cleanupObjects = append(cleanupObjects, createServiceInterfaceForService(
				ctx, strings.Join([]string{serviceName, ifcName}, "-"), serviceName, ifcName))
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

		It("should remove invalid network for Pod using virtual network", func() {
			By("Create ServiceInterface for Service with virtual network")
			si := &dpuservicev1.ServiceInterface{
				ObjectMeta: metav1.ObjectMeta{
					Name:      strings.Join([]string{serviceName, ifcName}, "-"),
					Namespace: defaultNS,
					Labels: map[string]string{
						dpuservicev1.DPFServiceIDLabelKey: serviceName,
						serviceInterfaceAnnotKey:          ifcName,
					},
				},
				Spec: dpuservicev1.ServiceInterfaceSpec{
					InterfaceType: dpuservicev1.InterfaceTypeService,
					Node:          ptr.To(nodeName),
					Service: &dpuservicev1.ServiceDef{
						ServiceID:     serviceName,
						InterfaceName: ifcName,
						Network:       "virtual-network",
					},
				},
			}
			Expect(testClient.Create(ctx, si)).To(Succeed())
			cleanupObjects = append(cleanupObjects, si)

			By("Create ServiceChain for the virtual network interface with MTU")
			cleanupObjects = append(cleanupObjects, createServiceChainWithServiceInterface(ctx, svcName1, nil, serviceName, ifcName, ptr.To(2000)))

			By("Create Pod with Network Annotation including invalid network")
			cleanupObjects = append(cleanupObjects, createPodWithNetworkAnnotation(ctx, singleNetAnnotationWithInvalid(ifcName)))

			By("Check that invalid network has been removed from Pod annotation")
			Eventually(func(g Gomega) {
				pod := &corev1.Pod{}
				g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: defaultNS, Name: podName}, pod)).To(Succeed())
				// Virtual network gets MTU from ServiceChain but no IPAM
				g.Expect(pod.Annotations[multusKey]).To(BeEquivalentTo(expectedSingleNetAnnotationWithoutIPAM(2000)))
				// Verify digest is set
				g.Expect(pod.Annotations[NetworkDigestAnnotation]).NotTo(BeEmpty())
			}).WithTimeout(5 * time.Second).Should(Succeed())

			By("Turning the Pod State to Succeed")
			changePodState(ctx, corev1.PodSucceeded)
		})

		It("should update Pod with two service interfaces - one with virtual network and one without (with IPAM)", func() {
			By("Create ServiceInterface 1 with virtual network")
			siVirtual := &dpuservicev1.ServiceInterface{
				ObjectMeta: metav1.ObjectMeta{
					Name:      strings.Join([]string{serviceName, ifcName}, "-"),
					Namespace: defaultNS,
					Labels: map[string]string{
						dpuservicev1.DPFServiceIDLabelKey: serviceName,
						serviceInterfaceAnnotKey:          ifcName,
					},
				},
				Spec: dpuservicev1.ServiceInterfaceSpec{
					InterfaceType: dpuservicev1.InterfaceTypeService,
					Node:          ptr.To(nodeName),
					Service: &dpuservicev1.ServiceDef{
						ServiceID:      serviceName,
						InterfaceName:  ifcName,
						Network:        "mybrsfc",
						VirtualNetwork: ptr.To("virtual-network"),
					},
				},
			}
			Expect(testClient.Create(ctx, siVirtual)).To(Succeed())
			cleanupObjects = append(cleanupObjects, siVirtual)

			By("Create ServiceInterface 2 without virtual network")
			siRegular := &dpuservicev1.ServiceInterface{
				ObjectMeta: metav1.ObjectMeta{
					Name:      strings.Join([]string{serviceName, ifcName2}, "-"),
					Namespace: defaultNS,
					Labels: map[string]string{
						dpuservicev1.DPFServiceIDLabelKey: serviceName,
						serviceInterfaceAnnotKey:          ifcName2,
					},
				},
				Spec: dpuservicev1.ServiceInterfaceSpec{
					InterfaceType: dpuservicev1.InterfaceTypeService,
					Node:          ptr.To(nodeName),
					Service: &dpuservicev1.ServiceDef{
						ServiceID:     serviceName,
						InterfaceName: ifcName2,
						Network:       "second-network",
					},
				},
			}
			Expect(testClient.Create(ctx, siRegular)).To(Succeed())
			cleanupObjects = append(cleanupObjects, siRegular)

			By("Create ServiceChain1 for virtual network interface with MTU (no IPAM)")
			cleanupObjects = append(cleanupObjects, createServiceChainWithServiceInterface(ctx, svcName1, nil, serviceName, ifcName, ptr.To(2000)))

			By("Create ServiceChain2 for regular interface with IPAM and MTU")
			defaultGateway := true
			ipam := &dpuservicev1.IPAM{
				DefaultGateway: ptr.To(defaultGateway),
				MatchLabels:    ipamLabels,
			}
			cleanupObjects = append(cleanupObjects, createServiceChainWithServiceInterface(ctx, svcName2, ipam, serviceName, ifcName2, ptr.To(3000)))

			By("Create IPPool for regular interface")
			cleanupObjects = append(cleanupObjects, createIPPool(ctx, ipamName, ipamLabels))

			By("Create Pod with both network interfaces")
			multiNetAnnot := fmt.Sprintf(`[{"name":"mybrsfc","interface":"%s"},{"name":"second-network","interface":"%s"},{"name":"invalid-network","namespace":"invalid-namespace","interface":"invalid-interface"}]`, ifcName, ifcName2)
			cleanupObjects = append(cleanupObjects, createPodWithNetworkAnnotation(ctx, multiNetAnnot))

			By("Check that Pod annotation has been updated correctly")
			Eventually(func(g Gomega) {
				pod := &corev1.Pod{}
				g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: defaultNS, Name: podName}, pod)).To(Succeed())
				// Verify annotation contains both interfaces:
				// - Virtual network: MTU 2000, no IPAM
				// - Regular network: MTU 3000, with IPAM
				expectedAnnot := fmt.Sprintf(
					`[{"name":"mybrsfc","namespace":"default","interface":"%s","cni-args":{"mtu":2000}},`+
						`{"name":"second-network","namespace":"default","interface":"%s","cni-args":{"allocateDefaultGateway":%v,"mtu":3000,"poolNames":["pool-1"],"poolType":"ippool"}}]`,
					ifcName, ifcName2, defaultGateway)
				g.Expect(pod.Annotations[multusKey]).To(BeEquivalentTo(expectedAnnot))
			}).WithTimeout(5 * time.Second).Should(Succeed())

			By("Turning the Pod State to Succeed")
			changePodState(ctx, corev1.PodSucceeded)
		})

		It("should skip ServiceChains and ServiceInterfaces that don't match pod requirements", func() {
			By("Create ServiceInterface for the correct service")
			correctSI := createServiceInterfaceForService(
				ctx, strings.Join([]string{serviceName, ifcName}, "-"), serviceName, ifcName)
			cleanupObjects = append(cleanupObjects, correctSI)

			By("Create ServiceInterface on wrong node (should be skipped)")
			wrongNodeSI := &dpuservicev1.ServiceInterface{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "wrong-node-si",
					Namespace: defaultNS,
					Labels: map[string]string{
						dpuservicev1.DPFServiceIDLabelKey: "other-service",
					},
				},
				Spec: dpuservicev1.ServiceInterfaceSpec{
					InterfaceType: dpuservicev1.InterfaceTypeService,
					Node:          ptr.To("other-node"),
					Service: &dpuservicev1.ServiceDef{
						ServiceID:     "other-service",
						InterfaceName: "other-ifc",
					},
				},
			}
			Expect(testClient.Create(ctx, wrongNodeSI)).To(Succeed())
			cleanupObjects = append(cleanupObjects, wrongNodeSI)

			By("Create ServiceInterface with wrong type (should be skipped)")
			wrongTypeSI := &dpuservicev1.ServiceInterface{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "wrong-type-si",
					Namespace: defaultNS,
					Labels: map[string]string{
						dpuservicev1.DPFServiceIDLabelKey: "pf-service",
					},
				},
				Spec: dpuservicev1.ServiceInterfaceSpec{
					InterfaceType: dpuservicev1.InterfaceTypePF, // Not InterfaceTypeService
					Node:          ptr.To(nodeName),
					PF: &dpuservicev1.PF{
						ID: 0,
					},
				},
			}
			Expect(testClient.Create(ctx, wrongTypeSI)).To(Succeed())
			cleanupObjects = append(cleanupObjects, wrongTypeSI)

			By("Create ServiceChain on wrong node (should be skipped)")
			wrongNodeChain := &dpuservicev1.ServiceChain{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "wrong-node-chain",
					Namespace: defaultNS,
				},
				Spec: dpuservicev1.ServiceChainSpec{
					Node: ptr.To("other-node"), // Different node
					Switches: []dpuservicev1.Switch{
						{
							ServiceMTU: ptr.To(1500),
							Ports: []dpuservicev1.Port{
								{
									ServiceInterface: dpuservicev1.ServiceIfc{
										MatchLabels: map[string]string{
											dpuservicev1.DPFServiceIDLabelKey: "other-service",
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(testClient.Create(ctx, wrongNodeChain)).To(Succeed())
			cleanupObjects = append(cleanupObjects, wrongNodeChain)

			By("Create ServiceChain with PF interface type (should be skipped)")
			pfChain := &dpuservicev1.ServiceChain{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pf-chain",
					Namespace: defaultNS,
				},
				Spec: dpuservicev1.ServiceChainSpec{
					Node: ptr.To(nodeName),
					Switches: []dpuservicev1.Switch{
						{
							ServiceMTU: ptr.To(1500),
							Ports: []dpuservicev1.Port{
								{
									ServiceInterface: dpuservicev1.ServiceIfc{
										MatchLabels: map[string]string{
											dpuservicev1.DPFServiceIDLabelKey: "pf-service",
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(testClient.Create(ctx, pfChain)).To(Succeed())
			cleanupObjects = append(cleanupObjects, pfChain)

			By("Create correct ServiceChain with MTU")
			cleanupObjects = append(cleanupObjects, createServiceChainWithServiceInterface(ctx, svcName1, nil, serviceName, ifcName, ptr.To(4000)))

			By("Create Pod with Network Annotation")
			cleanupObjects = append(cleanupObjects, createPodWithNetworkAnnotation(ctx, singleNetAnnotationWithInvalid(ifcName)))

			By("Check that Pod annotation has been updated with correct ServiceChain only")
			Eventually(func(g Gomega) {
				pod := &corev1.Pod{}
				g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: defaultNS, Name: podName}, pod)).To(Succeed())
				// Should only get settings from the correct ServiceChain (MTU 4000), not the others
				g.Expect(pod.Annotations[multusKey]).To(BeEquivalentTo(expectedSingleNetAnnotationWithoutIPAM(4000)))
			}).WithTimeout(5 * time.Second).Should(Succeed())

			By("Turning the Pod State to Succeed")
			changePodState(ctx, corev1.PodSucceeded)
		})

		It("should not fail when unrelated ServiceChain has faulty selector", func() {
			By("Create ServiceInterface for the pod's service")
			cleanupObjects = append(cleanupObjects, createServiceInterfaceForService(
				ctx, strings.Join([]string{serviceName, ifcName}, "-"), serviceName, ifcName))

			By("Create unrelated ServiceChain with faulty selector (no matching ServiceInterface)")
			faultyChain := &dpuservicev1.ServiceChain{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "faulty-chain",
					Namespace: defaultNS,
				},
				Spec: dpuservicev1.ServiceChainSpec{
					Node: ptr.To(nodeName),
					Switches: []dpuservicev1.Switch{
						{
							ServiceMTU: ptr.To(2000),
							Ports: []dpuservicev1.Port{
								{
									ServiceInterface: dpuservicev1.ServiceIfc{
										MatchLabels: map[string]string{
											"uplink": "p0", // This label doesn't match any ServiceInterface
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(testClient.Create(ctx, faultyChain)).To(Succeed())
			cleanupObjects = append(cleanupObjects, faultyChain)

			By("Verify that getServiceInterfaceWithLabels would fail for the faulty selector")
			// This demonstrates that the faulty ServiceChain would cause an error if not handled properly
			_, err := getServiceInterfaceWithLabels(ctx, testClient, nodeName, defaultNS, map[string]string{"uplink": "p0"})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no serviceInterface"))

			By("Create correct ServiceChain for the pod with IPAM")
			defaultGateway := true
			ipam := &dpuservicev1.IPAM{
				DefaultGateway: ptr.To(defaultGateway),
				MatchLabels:    ipamLabels,
			}
			cleanupObjects = append(cleanupObjects, createServiceChainWithServiceInterface(ctx, svcName1, ipam, serviceName, ifcName, ptr.To(4000)))

			By("Create IPPool for the pod")
			cleanupObjects = append(cleanupObjects, createIPPool(ctx, ipamName, ipamLabels))

			By("Create Pod with Network Annotation")
			cleanupObjects = append(cleanupObjects, createPodWithNetworkAnnotation(ctx, singleNetAnnotationWithInvalid(ifcName)))

			By("Check that Pod annotation has been updated correctly despite faulty ServiceChain")
			Eventually(func(g Gomega) {
				pod := &corev1.Pod{}
				g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: defaultNS, Name: podName}, pod)).To(Succeed())
				// Should get settings from the correct ServiceChain only (MTU 4000, IPAM)
				// The faulty ServiceChain's error should be collected but not cause reconciliation to fail
				// because the pod doesn't actually need that interface
				g.Expect(pod.Annotations[multusKey]).To(BeEquivalentTo(expectedSingleNetAnnotationWithIPAM(ifcName, "ippool", defaultGateway, 4000)))
			}).WithTimeout(5 * time.Second).Should(Succeed())

			By("Turning the Pod State to Succeed")
			changePodState(ctx, corev1.PodSucceeded)
		})

		It("should return error when pod needs interface but ServiceChain has faulty selector", func() {
			By("Create ServiceChain with faulty selector for interface the pod needs")
			// This ServiceChain references an interface the pod will request, but the selector is wrong
			faultyChain := &dpuservicev1.ServiceChain{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "faulty-needed-chain",
					Namespace: defaultNS,
				},
				Spec: dpuservicev1.ServiceChainSpec{
					Node: ptr.To(nodeName),
					Switches: []dpuservicev1.Switch{
						{
							ServiceMTU: ptr.To(2000),
							Ports: []dpuservicev1.Port{
								{
									ServiceInterface: dpuservicev1.ServiceIfc{
										MatchLabels: map[string]string{
											dpuservicev1.DPFServiceIDLabelKey: serviceName,
											serviceInterfaceAnnotKey:          ifcName,
											"nonexistent":                     "label", // Added label that doesn't exist
										},
									},
								},
							},
						},
					},
				},
			}
			Expect(testClient.Create(ctx, faultyChain)).To(Succeed())
			cleanupObjects = append(cleanupObjects, faultyChain)

			By("Create Pod with Network Annotation requesting interface from faulty chain")
			cleanupObjects = append(cleanupObjects, createPodWithNetworkAnnotation(ctx, singleNetAnnotationWithInvalid(ifcName)))

			By("Verify Pod annotation is NOT updated because reconciliation fails with error")
			Consistently(func(g Gomega) {
				pod := &corev1.Pod{}
				g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: defaultNS, Name: podName}, pod)).To(Succeed())
				// Annotation should still have the invalid network because an error is returned
				// Controller will requeue automatically and retry until settings are available
				g.Expect(pod.Annotations[multusKey]).To(BeEquivalentTo(singleNetAnnotationWithInvalid(ifcName)))
			}).WithTimeout(5 * time.Second).Should(Succeed())

			By("Turning the Pod State to Succeed")
			changePodState(ctx, corev1.PodSucceeded)
		})

		It("should not error for interfaces with pre-populated CNI args when ServiceChain is missing (HBN case)", func() {
			By("Create ServiceInterface for the physical interface")
			cleanupObjects = append(cleanupObjects, createServiceInterfaceForService(
				ctx, strings.Join([]string{serviceName, ifcName}, "-"), serviceName, ifcName))

			By("Create ServiceChain only for the physical interface")
			cleanupObjects = append(cleanupObjects, createServiceChainWithServiceInterface(ctx, svcName1, nil, serviceName, ifcName, ptr.To(1500)))

			By("Create Pod with both IPAM interface (with CNI args) and physical interface (without CNI args)")
			// The IPAM interface "ip_pf0vf3" has pre-populated CNI args with IPAM pools
			// The physical interface "sfceth1" does not have CNI args and needs ServiceChain
			hbnAnnotation := fmt.Sprintf(`[{"name":"iprequest","interface":"ip_pf0vf3","cni-args":{"poolNames":["pool1"],"poolType":"cidrpool"}},{"name":"mybrsfc","interface":"%s"},{"name":"invalid-network","namespace":"invalid-namespace","interface":"invalid-interface"}]`, ifcName)
			cleanupObjects = append(cleanupObjects, createPodWithNetworkAnnotation(ctx, hbnAnnotation))

			By("Check that Pod annotation has been updated correctly")
			Eventually(func(g Gomega) {
				pod := &corev1.Pod{}
				g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: defaultNS, Name: podName}, pod)).To(Succeed())
				// The IPAM interface should keep its existing CNI args (no ServiceChain exists for it, but that's OK)
				// The physical interface should get MTU from ServiceChain
				expectedAnnot := fmt.Sprintf(`[{"name":"iprequest","namespace":"default","interface":"ip_pf0vf3","cni-args":{"poolNames":["pool1"],"poolType":"cidrpool"}},{"name":"mybrsfc","namespace":"default","interface":"%s","cni-args":{"mtu":1500}}]`, ifcName)
				g.Expect(pod.Annotations[multusKey]).To(BeEquivalentTo(expectedAnnot))
			}).WithTimeout(5 * time.Second).Should(Succeed())

			By("Turning the Pod State to Succeed")
			changePodState(ctx, corev1.PodSucceeded)
		})

		It("should merge MTU into pre-populated CNI args when ServiceChain exists for IPAM interface", func() {
			By("Create ServiceInterface for both the IPAM interface and physical interface")
			// Note: This is a special case where a ServiceInterface exists matching the IPAM interface name
			ipamSI := &dpuservicev1.ServiceInterface{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ipam-interface-si",
					Namespace: defaultNS,
					Labels: map[string]string{
						dpuservicev1.DPFServiceIDLabelKey: serviceName,
						serviceInterfaceAnnotKey:          "ip_pf0vf3",
					},
				},
				Spec: dpuservicev1.ServiceInterfaceSpec{
					InterfaceType: dpuservicev1.InterfaceTypeService,
					Node:          ptr.To(nodeName),
					Service: &dpuservicev1.ServiceDef{
						ServiceID:     serviceName,
						InterfaceName: "ip_pf0vf3",
					},
				},
			}
			Expect(testClient.Create(ctx, ipamSI)).To(Succeed())
			cleanupObjects = append(cleanupObjects, ipamSI)

			cleanupObjects = append(cleanupObjects, createServiceInterfaceForService(
				ctx, strings.Join([]string{serviceName, ifcName}, "-"), serviceName, ifcName))

			By("Create ServiceChains for both interfaces with different MTUs")
			ipamChain := &dpuservicev1.ServiceChain{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ipam-chain",
					Namespace: defaultNS,
				},
				Spec: dpuservicev1.ServiceChainSpec{
					Node: ptr.To(nodeName),
					Switches: []dpuservicev1.Switch{
						{
							ServiceMTU: ptr.To(9000),
							Ports: []dpuservicev1.Port{
								{
									ServiceInterface: dpuservicev1.ServiceIfc{
										MatchLabels: map[string]string{
											dpuservicev1.DPFServiceIDLabelKey: serviceName,
											serviceInterfaceAnnotKey:          "ip_pf0vf3",
										},
									},
								},
							},
						},
					},
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
				// The IPAM interface should have its original CNI args PLUS the MTU from ServiceChain
				// The physical interface should get MTU from ServiceChain
				expectedAnnot := fmt.Sprintf(`[{"name":"iprequest","namespace":"default","interface":"ip_pf0vf3","cni-args":{"allocateDefaultGateway":true,"mtu":9000,"poolNames":["pool1"],"poolType":"cidrpool"}},{"name":"mybrsfc","namespace":"default","interface":"%s","cni-args":{"mtu":1500}}]`, ifcName)
				g.Expect(pod.Annotations[multusKey]).To(BeEquivalentTo(expectedAnnot))
			}).WithTimeout(5 * time.Second).Should(Succeed())

			By("Turning the Pod State to Succeed")
			changePodState(ctx, corev1.PodSucceeded)
		})

		It("should calculate digest for valid networks", func() {
			By("Create networks with different configurations")
			networks1 := []*multustypes.NetworkSelectionElement{
				{
					Name:             "network1",
					Namespace:        "default",
					InterfaceRequest: "eth1",
					CNIArgs: &map[string]interface{}{
						"mtu": 1500,
					},
				},
			}

			networks2 := []*multustypes.NetworkSelectionElement{
				{
					Name:             "network1",
					Namespace:        "default",
					InterfaceRequest: "eth1",
					CNIArgs: &map[string]interface{}{
						"mtu": 3000,
					},
				},
			}

			By("Calculate digests")
			digest1 := calculateNetworkDigest(networks1)
			digest2 := calculateNetworkDigest(networks2)

			By("Verify digests are different for different configurations")
			Expect(digest1).NotTo(BeEmpty())
			Expect(digest2).NotTo(BeEmpty())
			Expect(digest1).NotTo(Equal(digest2))
		})

		It("should filter invalid networks from digest calculation", func() {
			By("Create networks including invalid network")
			networks := []*multustypes.NetworkSelectionElement{
				{
					Name:             "network1",
					Namespace:        "default",
					InterfaceRequest: "eth1",
					CNIArgs: &map[string]interface{}{
						"mtu": 1500,
					},
				},
				{
					Name:             "invalid-network",
					Namespace:        "invalid-namespace",
					InterfaceRequest: "invalid-interface",
				},
			}

			By("Calculate digest")
			digest := calculateNetworkDigest(networks)
			Expect(digest).NotTo(BeEmpty())

			By("Verify digest matches when invalid network is not present")
			networksFiltered := []*multustypes.NetworkSelectionElement{
				{
					Name:             "network1",
					Namespace:        "default",
					InterfaceRequest: "eth1",
					CNIArgs: &map[string]interface{}{
						"mtu": 1500,
					},
				},
			}
			digestFiltered := calculateNetworkDigest(networksFiltered)
			Expect(digestFiltered).To(Equal(digest))
		})

		It("should handle empty networks list", func() {
			By("Calculate digest for empty networks")
			networks := []*multustypes.NetworkSelectionElement{}
			digest := calculateNetworkDigest(networks)
			Expect(digest).NotTo(BeEmpty())
		})

		It("should handle networks with only invalid networks", func() {
			By("Create networks with only invalid network")
			networks := []*multustypes.NetworkSelectionElement{
				{
					Name:             "invalid-network",
					Namespace:        "invalid-namespace",
					InterfaceRequest: "invalid-interface",
				},
			}

			By("Calculate digest")
			digest := calculateNetworkDigest(networks)
			Expect(digest).NotTo(BeEmpty())
		})

		It("should handle pod with malformed network annotation", func() {
			By("Create Pod with malformed network annotation")
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "malformed-pod",
					Namespace: defaultNS,
					Annotations: map[string]string{
						multusKey: "this-is-not-valid-json",
					},
					Labels: map[string]string{dpuservicev1.DPFServiceIDLabelKey: serviceName},
				},
				Spec: corev1.PodSpec{
					NodeName: nodeName,
					Containers: []corev1.Container{
						{Name: "ctr1", Image: "image"},
					},
				},
			}
			Expect(testClient.Create(ctx, pod)).To(Succeed())
			cleanupObjects = append(cleanupObjects, pod)

			By("Wait for pod to be created")
			Eventually(func(g Gomega) {
				p := &corev1.Pod{}
				g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: defaultNS, Name: "malformed-pod"}, p)).To(Succeed())
			}).WithTimeout(10 * time.Second).Should(Succeed())

			By("Verify pod annotation remains malformed (reconciler handles error gracefully)")
			Consistently(func(g Gomega) {
				p := &corev1.Pod{}
				g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: defaultNS, Name: "malformed-pod"}, p)).To(Succeed())
				// Annotation should remain unchanged because the reconciler can't parse it
				g.Expect(p.Annotations[multusKey]).To(Equal("this-is-not-valid-json"))
			}).WithTimeout(10 * time.Second).Should(Succeed())

			By("Turning the Pod State to Succeed")
			Eventually(func(g Gomega) {
				p := &corev1.Pod{}
				g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: defaultNS, Name: "malformed-pod"}, p)).To(Succeed())
				p.Status.Phase = corev1.PodSucceeded
				p.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Pod"))
				p.ManagedFields = nil
				g.Expect(testClient.Status().Patch(ctx, p, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())
			}).WithTimeout(10 * time.Second).Should(Succeed())
		})
	})

	Context("When calculating network digests", func() {
		var cleanupObjects []client.Object

		BeforeEach(func() {
			cleanupObjects = []client.Object{}
		})

		AfterEach(func() {
			By("Cleaning up the objects")
			Expect(testutils.CleanupAndWait(ctx, testClient, cleanupObjects...)).To(Succeed())
		})

		It("should calculate digest for networks with IPAM and MTU", func() {
			By("Create ServiceInterface for Service")
			cleanupObjects = append(cleanupObjects, createServiceInterfaceForService(
				ctx, strings.Join([]string{serviceName, ifcName}, "-"), serviceName, ifcName))
			By("Create ServiceChain with IPAM and MTU")
			defaultGateway := true
			ipam := &dpuservicev1.IPAM{
				DefaultGateway: ptr.To(defaultGateway),
				MatchLabels:    ipamLabels,
			}
			cleanupObjects = append(cleanupObjects, createServiceChainWithServiceInterface(ctx, svcName1, ipam, serviceName, ifcName, ptr.To(4000)))
			By("Create IPPool")
			cleanupObjects = append(cleanupObjects, createIPPool(ctx, ipamName, ipamLabels))
			By("Create Pod with Network Annotation")
			pod := createTestPodInMemory(
				map[string]string{multusKey: singleNetAnnotationWithInvalid(ifcName)},
				map[string]string{dpuservicev1.DPFServiceIDLabelKey: serviceName})

			By("Calculate network digest")
			networks, _ := GetPodNetworks(pod)
			digest := calculateDigestEventually(ctx, pod, networks)

			By("Verify digest is consistent for same configuration")
			networks2, err := GetPodNetworks(pod)
			Expect(err).NotTo(HaveOccurred())
			digest2, _ := CalculatePodNetworkDigest(ctx, testClient, pod, networks2)
			Expect(digest2).To(Equal(digest))
		})

		It("should calculate digest for networks without IPAM", func() {
			By("Create ServiceInterface for Service")
			cleanupObjects = append(cleanupObjects, createServiceInterfaceForService(
				ctx, strings.Join([]string{serviceName, ifcName}, "-"), serviceName, ifcName))
			By("Create ServiceChain without IPAM but with MTU")
			cleanupObjects = append(cleanupObjects, createServiceChainWithServiceInterface(ctx, svcName1, nil, serviceName, ifcName, ptr.To(3000)))
			By("Create Pod with Network Annotation")
			pod := createTestPodInMemory(
				map[string]string{multusKey: singleNetAnnotationWithInvalid(ifcName)},
				map[string]string{dpuservicev1.DPFServiceIDLabelKey: serviceName})

			By("Calculate network digest")
			networks, err := GetPodNetworks(pod)
			Expect(err).NotTo(HaveOccurred())
			digest := calculateDigestEventually(ctx, pod, networks)
			Expect(digest).NotTo(BeEmpty())
		})

		It("should return empty digest for pod with no networks", func() {
			By("Create Pod without Network Annotation")
			pod := createTestPodInMemory(nil, nil)

			By("Calculate network digest")
			networks, err := GetPodNetworks(pod)
			Expect(err).NotTo(HaveOccurred())
			digest, _ := CalculatePodNetworkDigest(ctx, testClient, pod, networks)
			Expect(digest).To(BeEmpty())
		})

		It("should filter out invalid networks when calculating digest", func() {
			By("Create ServiceInterface for Service")
			cleanupObjects = append(cleanupObjects, createServiceInterfaceForService(
				ctx, strings.Join([]string{serviceName, ifcName}, "-"), serviceName, ifcName))
			By("Create ServiceChain with IPAM")
			defaultGateway := false
			ipam := &dpuservicev1.IPAM{
				DefaultGateway: ptr.To(defaultGateway),
				MatchLabels:    ipamLabels,
			}
			cleanupObjects = append(cleanupObjects, createServiceChainWithServiceInterface(ctx, svcName1, ipam, serviceName, ifcName, ptr.To(1500)))
			By("Create IPPool")
			cleanupObjects = append(cleanupObjects, createIPPool(ctx, ipamName, ipamLabels))
			By("Create Pod with Network Annotation including invalid network")
			pod := createTestPodInMemory(
				map[string]string{multusKey: singleNetAnnotationWithInvalid(ifcName)},
				map[string]string{dpuservicev1.DPFServiceIDLabelKey: serviceName})

			By("Calculate network digest")
			networks, err := GetPodNetworks(pod)
			Expect(err).NotTo(HaveOccurred())
			digest := calculateDigestEventually(ctx, pod, networks)

			By("Verify digest matches when invalid network is not present")
			podNoInvalid := createTestPodInMemory(
				map[string]string{multusKey: singleNetAnnotation()},
				map[string]string{dpuservicev1.DPFServiceIDLabelKey: serviceName})
			networksNoInvalid, err := GetPodNetworks(podNoInvalid)
			Expect(err).NotTo(HaveOccurred())
			digestNoInvalid, _ := CalculatePodNetworkDigest(ctx, testClient, podNoInvalid, networksNoInvalid)
			Expect(digestNoInvalid).To(Equal(digest))
		})

		It("should calculate different digests for different network configurations", func() {
			By("Create ServiceInterface for Service")
			cleanupObjects = append(cleanupObjects, createServiceInterfaceForService(
				ctx, strings.Join([]string{serviceName, ifcName}, "-"), serviceName, ifcName))
			By("Create ServiceChain with IPAM and MTU 1500")
			defaultGateway := false
			ipam := &dpuservicev1.IPAM{
				DefaultGateway: ptr.To(defaultGateway),
				MatchLabels:    ipamLabels,
			}
			cleanupObjects = append(cleanupObjects, createServiceChainWithServiceInterface(ctx, svcName1, ipam, serviceName, ifcName, ptr.To(1500)))
			By("Create IPPool")
			cleanupObjects = append(cleanupObjects, createIPPool(ctx, ipamName, ipamLabels))

			By("Create Pod with MTU 1500")
			pod1 := createTestPodInMemory(
				map[string]string{multusKey: singleNetAnnotationWithInvalid(ifcName)},
				map[string]string{dpuservicev1.DPFServiceIDLabelKey: serviceName})
			networks1, err := GetPodNetworks(pod1)
			Expect(err).NotTo(HaveOccurred())
			digest1 := calculateDigestEventually(ctx, pod1, networks1)

			By("Update ServiceChain with MTU 3000")
			serviceChain := &dpuservicev1.ServiceChain{}
			Expect(testClient.Get(ctx, client.ObjectKey{Namespace: defaultNS, Name: svcName1}, serviceChain)).To(Succeed())
			serviceChain.Spec.Switches[0].ServiceMTU = ptr.To(3000)
			Expect(testClient.Patch(ctx, serviceChain, client.Merge)).To(Succeed())

			By("Wait for cache to reflect the updated ServiceChain")
			Eventually(func(g Gomega) {
				sc := &dpuservicev1.ServiceChain{}
				g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: defaultNS, Name: svcName1}, sc)).To(Succeed())
				g.Expect(sc.Spec.Switches[0].ServiceMTU).NotTo(BeNil())
				g.Expect(*sc.Spec.Switches[0].ServiceMTU).To(Equal(3000))
			}).WithTimeout(2 * time.Second).Should(Succeed())

			By("Calculate digest with MTU 3000")
			digest2, err := CalculatePodNetworkDigest(ctx, testClient, pod1, networks1)
			Expect(err).NotTo(HaveOccurred())
			Expect(digest2).NotTo(BeEmpty())

			By("Verify digests are different")
			Expect(digest1).NotTo(Equal(digest2))
		})

		It("should handle multiple networks in digest calculation", func() {
			By("Create ServiceInterfaces for Service")
			cleanupObjects = append(cleanupObjects, createServiceInterfaceForService(
				ctx, strings.Join([]string{serviceName, ifcName}, "-"), serviceName, ifcName))
			cleanupObjects = append(cleanupObjects, createServiceInterfaceForService(
				ctx, strings.Join([]string{serviceName, ifcName2}, "-"), serviceName, ifcName2))
			By("Create ServiceChain1 with IPAM")
			defaultGateway := false
			ipam := &dpuservicev1.IPAM{
				DefaultGateway: ptr.To(defaultGateway),
				MatchLabels:    ipamLabels,
			}
			cleanupObjects = append(cleanupObjects, createServiceChainWithServiceInterface(ctx, svcName1, ipam, serviceName, ifcName, ptr.To(3000)))
			By("Create ServiceChain2 with IPAM")
			ipam2 := &dpuservicev1.IPAM{
				DefaultGateway: ptr.To(defaultGateway),
				MatchLabels:    ipamLabels2,
			}
			cleanupObjects = append(cleanupObjects, createServiceChainWithServiceInterface(ctx, svcName2, ipam2, serviceName, ifcName2, ptr.To(1500)))
			By("Create IpPool1")
			cleanupObjects = append(cleanupObjects, createIPPool(ctx, ipamName, ipamLabels))
			By("Create IpPool2")
			cleanupObjects = append(cleanupObjects, createIPPool(ctx, ipamName2, ipamLabels2))

			By("Create Pod with multiple networks")
			pod := createTestPodInMemory(
				map[string]string{multusKey: multipleNetAnnotation()},
				map[string]string{dpuservicev1.DPFServiceIDLabelKey: serviceName})

			By("Calculate network digest")
			networks, err := GetPodNetworks(pod)
			Expect(err).NotTo(HaveOccurred())
			digest := calculateDigestEventually(ctx, pod, networks)
			Expect(digest).NotTo(BeEmpty())
		})

	})

	Context("isPodUsingOnlyVirtualNetworks", func() {
		var testPod *corev1.Pod
		var cleanupObjects []client.Object

		BeforeEach(func() {
			cleanupObjects = []client.Object{}
		})

		AfterEach(func() {
			By("Cleaning up the objects")
			Expect(testutils.CleanupAndWait(ctx, testClient, cleanupObjects...)).To(Succeed())
		})

		It("should return true for pod using virtual network", func() {
			By("Create ServiceInterface with virtual network for service")
			si := &dpuservicev1.ServiceInterface{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "virtual-net-si",
					Namespace: defaultNS,
					Labels: map[string]string{
						dpuservicev1.DPFServiceIDLabelKey: serviceName,
					},
				},
				Spec: dpuservicev1.ServiceInterfaceSpec{
					InterfaceType: dpuservicev1.InterfaceTypeService,
					Node:          ptr.To(nodeName),
					Service: &dpuservicev1.ServiceDef{
						ServiceID:      serviceName,
						Network:        "mybrsfc",
						InterfaceName:  ifcName,
						VirtualNetwork: ptr.To("virtual-network"),
					},
				},
			}
			Expect(testClient.Create(ctx, si)).NotTo(HaveOccurred())
			cleanupObjects = append(cleanupObjects, si)

			By("Create Pod in memory (not in cluster)")
			testPod = &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod-virtual-net",
					Namespace: defaultNS,
					Labels: map[string]string{
						dpuservicev1.DPFServiceIDLabelKey: serviceName,
					},
				},
				Spec: corev1.PodSpec{
					NodeName: nodeName,
					Containers: []corev1.Container{
						{Name: "ctr1", Image: "image"},
					},
				},
			}

			By("Check if pod is using virtual network")
			Eventually(func(g Gomega) {
				isVirtual, err := isPodUsingOnlyVirtualNetworks(ctx, testClient, testPod)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(isVirtual).To(BeTrue())
			}).WithTimeout(2 * time.Second).Should(Succeed())
		})

		It("should return false for pod not using virtual network", func() {
			By("Create ServiceInterface without virtual network for service")
			si := &dpuservicev1.ServiceInterface{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "non-virtual-net-si",
					Namespace: defaultNS,
					Labels: map[string]string{
						dpuservicev1.DPFServiceIDLabelKey: serviceName,
					},
				},
				Spec: dpuservicev1.ServiceInterfaceSpec{
					InterfaceType: dpuservicev1.InterfaceTypeService,
					Node:          ptr.To(nodeName),
					Service: &dpuservicev1.ServiceDef{
						ServiceID:     serviceName,
						Network:       "mybrsfc",
						InterfaceName: ifcName,
					},
				},
			}
			Expect(testClient.Create(ctx, si)).NotTo(HaveOccurred())
			cleanupObjects = append(cleanupObjects, si)

			By("Create Pod in memory (not in cluster)")
			testPod = &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod-no-virtual-net",
					Namespace: defaultNS,
					Labels: map[string]string{
						dpuservicev1.DPFServiceIDLabelKey: serviceName,
					},
				},
				Spec: corev1.PodSpec{
					NodeName: nodeName,
					Containers: []corev1.Container{
						{Name: "ctr1", Image: "image"},
					},
				},
			}

			By("Check if pod is not using virtual network")
			Eventually(func(g Gomega) {
				isVirtual, err := isPodUsingOnlyVirtualNetworks(ctx, testClient, testPod)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(isVirtual).To(BeFalse())
			}).WithTimeout(2 * time.Second).Should(Succeed())
		})

		It("should return true for pod using virtual network via NSI entry", func() {
			By("Create NodeServiceInterfaces with a virtual network entry")
			nsi := createNodeServiceInterfaces(ctx, "nsi-virtual-net", dpuservicev1.NSITypeVPC, dpuservicev1.InterfaceEntry{
				Name:          defaultNS + "_nsi-virtual-net-entry",
				InterfaceType: dpuservicev1.InterfaceTypeService,
				Service: &dpuservicev1.ServiceDef{
					ServiceID:      serviceName,
					Network:        "mybrsfc",
					InterfaceName:  ifcName,
					VirtualNetwork: ptr.To("virtual-network"),
				},
			})
			cleanupObjects = append(cleanupObjects, nsi)

			By("Create Pod in memory (not in cluster)")
			testPod = &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod-virtual-net-nsi",
					Namespace: defaultNS,
					Labels: map[string]string{
						dpuservicev1.DPFServiceIDLabelKey: serviceName,
					},
				},
				Spec: corev1.PodSpec{
					NodeName: nodeName,
					Containers: []corev1.Container{
						{Name: "ctr1", Image: "image"},
					},
				},
			}

			By("Check if pod is using virtual network")
			Eventually(func(g Gomega) {
				isVirtual, err := isPodUsingOnlyVirtualNetworks(ctx, testClient, testPod)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(isVirtual).To(BeTrue())
			}).WithTimeout(2 * time.Second).Should(Succeed())
		})

		It("should return false for pod not using virtual network via NSI entry", func() {
			By("Create NodeServiceInterfaces without a virtual network entry")
			nsi := createNodeServiceInterfaces(ctx, "nsi-non-virtual-net", dpuservicev1.NSITypeSFC, dpuservicev1.InterfaceEntry{
				Name:          defaultNS + "_nsi-non-virtual-net-entry",
				InterfaceType: dpuservicev1.InterfaceTypeService,
				Service: &dpuservicev1.ServiceDef{
					ServiceID:     serviceName,
					Network:       "mybrsfc",
					InterfaceName: ifcName,
				},
			})
			cleanupObjects = append(cleanupObjects, nsi)

			By("Create Pod in memory (not in cluster)")
			testPod = &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod-no-virtual-net-nsi",
					Namespace: defaultNS,
					Labels: map[string]string{
						dpuservicev1.DPFServiceIDLabelKey: serviceName,
					},
				},
				Spec: corev1.PodSpec{
					NodeName: nodeName,
					Containers: []corev1.Container{
						{Name: "ctr1", Image: "image"},
					},
				},
			}

			By("Check if pod is not using virtual network")
			Eventually(func(g Gomega) {
				isVirtual, err := isPodUsingOnlyVirtualNetworks(ctx, testClient, testPod)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(isVirtual).To(BeFalse())
			}).WithTimeout(2 * time.Second).Should(Succeed())
		})

		It("should skip terminating NSI entries when checking virtual networks", func() {
			By("Create NodeServiceInterfaces with a terminating non-virtual entry and a virtual one")
			terminatingNSI := createNodeServiceInterfaces(ctx, "nsi-terminating", dpuservicev1.NSITypeSFC,
				dpuservicev1.InterfaceEntry{
					Name:          defaultNS + "_nsi-terminating-entry",
					Terminating:   true,
					InterfaceType: dpuservicev1.InterfaceTypeService,
					Service: &dpuservicev1.ServiceDef{
						ServiceID:     serviceName,
						Network:       "mybrsfc",
						InterfaceName: ifcName2,
					},
				},
			)
			activeNSI := createNodeServiceInterfaces(ctx, "nsi-active", dpuservicev1.NSITypeVPC,
				dpuservicev1.InterfaceEntry{
					Name:          defaultNS + "_nsi-active-entry",
					InterfaceType: dpuservicev1.InterfaceTypeService,
					Service: &dpuservicev1.ServiceDef{
						ServiceID:      serviceName,
						Network:        "mybrsfc",
						InterfaceName:  ifcName,
						VirtualNetwork: ptr.To("virtual-network"),
					},
				},
			)
			cleanupObjects = append(cleanupObjects, terminatingNSI, activeNSI)

			By("Create Pod in memory (not in cluster)")
			testPod = &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod-terminating-mix",
					Namespace: defaultNS,
					Labels: map[string]string{
						dpuservicev1.DPFServiceIDLabelKey: serviceName,
					},
				},
				Spec: corev1.PodSpec{
					NodeName: nodeName,
					Containers: []corev1.Container{
						{Name: "ctr1", Image: "image"},
					},
				},
			}

			By("Check that only the non-terminating entry is considered")
			Eventually(func(g Gomega) {
				isVirtual, err := isPodUsingOnlyVirtualNetworks(ctx, testClient, testPod)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(isVirtual).To(BeTrue())
			}).WithTimeout(2 * time.Second).Should(Succeed())
		})

		Context("networks without virtual network attached", func() {
			It("should return false when pod has no service ID label", func() {
				By("Create Pod in memory without service ID label")
				testPod = &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod-no-label",
						Namespace: defaultNS,
						Labels:    map[string]string{}, // No service ID label
					},
					Spec: corev1.PodSpec{
						NodeName: nodeName,
						Containers: []corev1.Container{
							{Name: "ctr1", Image: "image"},
						},
					},
				}

				By("Check that pod is not using virtual network")
				isVirtual, err := isPodUsingOnlyVirtualNetworks(ctx, testClient, testPod)
				Expect(err).NotTo(HaveOccurred())
				Expect(isVirtual).To(BeFalse())
			})

			It("should return false when no interface matches the pod's serviceID", func() {
				By("Create a virtual-network ServiceInterface for a different serviceID on the node")
				otherSI := &dpuservicev1.ServiceInterface{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "unrelated-virtual-net-si",
						Namespace: defaultNS,
						Labels:    map[string]string{dpuservicev1.DPFServiceIDLabelKey: "other-service"},
					},
					Spec: dpuservicev1.ServiceInterfaceSpec{
						InterfaceType: dpuservicev1.InterfaceTypeService,
						Node:          ptr.To(nodeName),
						Service: &dpuservicev1.ServiceDef{
							ServiceID:      "other-service",
							Network:        "mybrsfc",
							InterfaceName:  ifcName,
							VirtualNetwork: ptr.To("virtual-network"),
						},
					},
				}
				Expect(testClient.Create(ctx, otherSI)).To(Succeed())
				cleanupObjects = append(cleanupObjects, otherSI)

				testPod = &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod-mismatched-service",
						Namespace: defaultNS,
						Labels:    map[string]string{dpuservicev1.DPFServiceIDLabelKey: serviceName},
					},
					Spec: corev1.PodSpec{
						NodeName:   nodeName,
						Containers: []corev1.Container{{Name: "ctr1", Image: "image"}},
					},
				}

				By("Check that the unrelated virtual-network interface is ignored")
				Eventually(func(g Gomega) {
					isVirtual, err := isPodUsingOnlyVirtualNetworks(ctx, testClient, testPod)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(isVirtual).To(BeFalse())
				}).WithTimeout(2 * time.Second).Should(Succeed())
			})

			It("should return false and no error when no service interface exists", func() {
				By("Create Pod in memory with service ID label but no ServiceInterface")
				testPod = &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod-no-si",
						Namespace: defaultNS,
						Labels: map[string]string{
							dpuservicev1.DPFServiceIDLabelKey: "non-existent-service",
						},
					},
					Spec: corev1.PodSpec{
						NodeName: nodeName,
						Containers: []corev1.Container{
							{Name: "ctr1", Image: "image"},
						},
					},
				}

				By("Check that an error is returned")
				Eventually(func(g Gomega) {
					isVirtual, err := isPodUsingOnlyVirtualNetworks(ctx, testClient, testPod)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(isVirtual).To(BeFalse())
				}).WithTimeout(2 * time.Second).Should(Succeed())
			})
		})
	})

	Context("getServiceInterfaceWithLabels", func() {
		var cleanupObjects []client.Object

		BeforeEach(func() {
			cleanupObjects = []client.Object{}
		})

		AfterEach(func() {
			By("Cleaning up the objects")
			Expect(testutils.CleanupAndWait(ctx, testClient, cleanupObjects...)).To(Succeed())
		})

		Context("getServiceInterfaceWithLabels", func() {
			var cleanupObjects []client.Object

			AfterEach(func() {
				By("Cleaning up the objects")
				Expect(testutils.CleanupAndWait(ctx, testClient, cleanupObjects...)).To(Succeed())
			})

			It("should return error when no matching ServiceInterface is found", func() {
				By("Try to get non-existent ServiceInterface")
				_, err := getServiceInterfaceWithLabels(ctx, testClient, nodeName, defaultNS, map[string]string{
					dpuservicev1.DPFServiceIDLabelKey: "non-existent-service",
				})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("no serviceInterface"))
			})

			It("should return error when multiple matching ServiceInterfaces are found", func() {
				By("Create two ServiceInterfaces with same labels")
				si1 := &dpuservicev1.ServiceInterface{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "duplicate-si-1",
						Namespace: defaultNS,
						Labels: map[string]string{
							dpuservicev1.DPFServiceIDLabelKey: "duplicate-service",
						},
					},
					Spec: dpuservicev1.ServiceInterfaceSpec{
						InterfaceType: dpuservicev1.InterfaceTypeService,
						Node:          ptr.To(nodeName),
						Service: &dpuservicev1.ServiceDef{
							ServiceID:     "duplicate-service",
							InterfaceName: "ifc1",
						},
					},
				}
				Expect(testClient.Create(ctx, si1)).To(Succeed())
				cleanupObjects = append(cleanupObjects, si1)

				si2 := &dpuservicev1.ServiceInterface{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "duplicate-si-2",
						Namespace: defaultNS,
						Labels: map[string]string{
							dpuservicev1.DPFServiceIDLabelKey: "duplicate-service",
						},
					},
					Spec: dpuservicev1.ServiceInterfaceSpec{
						InterfaceType: dpuservicev1.InterfaceTypeService,
						Node:          ptr.To(nodeName),
						Service: &dpuservicev1.ServiceDef{
							ServiceID:     "duplicate-service",
							InterfaceName: "ifc2",
						},
					},
				}
				Expect(testClient.Create(ctx, si2)).To(Succeed())
				cleanupObjects = append(cleanupObjects, si2)

				By("Try to get ServiceInterface - should fail due to multiple matches")
				Eventually(func(g Gomega) {
					_, err := getServiceInterfaceWithLabels(ctx, testClient, nodeName, defaultNS, map[string]string{
						dpuservicev1.DPFServiceIDLabelKey: "duplicate-service",
					})
					g.Expect(err).To(HaveOccurred())
					g.Expect(err.Error()).To(ContainSubstring("expected only one serviceInterface"))
				}).WithTimeout(2 * time.Second).Should(Succeed())
			})

			It("should filter out ServiceInterfaces on different nodes", func() {
				By("Create ServiceInterface on different node")
				siWrongNode := &dpuservicev1.ServiceInterface{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "si-wrong-node",
						Namespace: defaultNS,
						Labels: map[string]string{
							dpuservicev1.DPFServiceIDLabelKey: "test-service",
						},
					},
					Spec: dpuservicev1.ServiceInterfaceSpec{
						InterfaceType: dpuservicev1.InterfaceTypeService,
						Node:          ptr.To("other-node"),
						Service: &dpuservicev1.ServiceDef{
							ServiceID:     "test-service",
							InterfaceName: "ifc1",
						},
					},
				}
				Expect(testClient.Create(ctx, siWrongNode)).To(Succeed())
				cleanupObjects = append(cleanupObjects, siWrongNode)

				By("Try to get ServiceInterface on our node - should fail as it's on wrong node")
				Eventually(func(g Gomega) {
					_, err := getServiceInterfaceWithLabels(ctx, testClient, nodeName, defaultNS, map[string]string{
						dpuservicev1.DPFServiceIDLabelKey: "test-service",
					})
					g.Expect(err).To(HaveOccurred())
					g.Expect(err.Error()).To(ContainSubstring("no serviceInterface"))
				}).WithTimeout(2 * time.Second).Should(Succeed())
			})

			It("should successfully return ServiceInterface when single match on correct node", func() {
				By("Create ServiceInterface on correct node")
				si := &dpuservicev1.ServiceInterface{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "si-correct",
						Namespace: defaultNS,
						Labels: map[string]string{
							dpuservicev1.DPFServiceIDLabelKey: "correct-service",
						},
					},
					Spec: dpuservicev1.ServiceInterfaceSpec{
						InterfaceType: dpuservicev1.InterfaceTypeService,
						Node:          ptr.To(nodeName),
						Service: &dpuservicev1.ServiceDef{
							ServiceID:     "correct-service",
							InterfaceName: "ifc1",
						},
					},
				}
				Expect(testClient.Create(ctx, si)).To(Succeed())
				cleanupObjects = append(cleanupObjects, si)

				By("Get ServiceInterface - should succeed")
				Eventually(func(g Gomega) {
					result, err := getServiceInterfaceWithLabels(ctx, testClient, nodeName, defaultNS, map[string]string{
						dpuservicev1.DPFServiceIDLabelKey: "correct-service",
					})
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(result.GetName()).To(Equal("si-correct"))
				}).WithTimeout(2 * time.Second).Should(Succeed())
			})

			It("should filter correctly when multiple SIs exist but only one on correct node", func() {
				By("Create ServiceInterface on wrong node")
				siWrongNode := &dpuservicev1.ServiceInterface{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "si-wrong-node",
						Namespace: defaultNS,
						Labels: map[string]string{
							dpuservicev1.DPFServiceIDLabelKey: "filter-test-service",
						},
					},
					Spec: dpuservicev1.ServiceInterfaceSpec{
						InterfaceType: dpuservicev1.InterfaceTypeService,
						Node:          ptr.To("other-node"),
						Service: &dpuservicev1.ServiceDef{
							ServiceID:     "filter-test-service",
							InterfaceName: "ifc1",
						},
					},
				}
				Expect(testClient.Create(ctx, siWrongNode)).To(Succeed())
				cleanupObjects = append(cleanupObjects, siWrongNode)

				By("Create ServiceInterface on correct node")
				siCorrectNode := &dpuservicev1.ServiceInterface{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "si-correct-node",
						Namespace: defaultNS,
						Labels: map[string]string{
							dpuservicev1.DPFServiceIDLabelKey: "filter-test-service",
						},
					},
					Spec: dpuservicev1.ServiceInterfaceSpec{
						InterfaceType: dpuservicev1.InterfaceTypeService,
						Node:          ptr.To(nodeName),
						Service: &dpuservicev1.ServiceDef{
							ServiceID:     "filter-test-service",
							InterfaceName: "ifc2",
						},
					},
				}
				Expect(testClient.Create(ctx, siCorrectNode)).To(Succeed())
				cleanupObjects = append(cleanupObjects, siCorrectNode)

				By("Get ServiceInterface - should return only the one on correct node")
				Eventually(func(g Gomega) {
					result, err := getServiceInterfaceWithLabels(ctx, testClient, nodeName, defaultNS, map[string]string{
						dpuservicev1.DPFServiceIDLabelKey: "filter-test-service",
					})
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(result.GetName()).To(Equal("si-correct-node"))
				}).WithTimeout(2 * time.Second).Should(Succeed())
			})

			// NSI-mode coverage for getServiceInterfaceWithLabels lives in podipam_controller_nsi_test.go.
		})
	})

	Context("getNVIPAMPoolsByMatchLabels", func() {
		var cleanupObjects []client.Object

		BeforeEach(func() {
			cleanupObjects = []client.Object{}
		})

		AfterEach(func() {
			By("Cleaning up the objects")
			Expect(testutils.CleanupAndWait(ctx, testClient, cleanupObjects...)).To(Succeed())
		})

		It("selects the first IPPool when requiredIPFamilies is omitted", func() {
			testLabels := map[string]string{
				"test-label": "multiple-ippools",
			}

			By("Create first IPPool")
			pool1 := &nvipamv1.IPPool{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ippool-1",
					Namespace: defaultNS,
					Labels:    testLabels,
				},
				Spec: nvipamv1.IPPoolSpec{
					Subnet:           "192.168.1.0/24",
					Gateway:          "192.168.1.1",
					PerNodeBlockSize: 10,
				},
			}
			Expect(testClient.Create(ctx, pool1)).To(Succeed())
			cleanupObjects = append(cleanupObjects, pool1)

			By("Create second IPPool with same labels")
			pool2 := &nvipamv1.IPPool{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ippool-2",
					Namespace: defaultNS,
					Labels:    testLabels,
				},
				Spec: nvipamv1.IPPoolSpec{
					Subnet:           "192.168.2.0/24",
					Gateway:          "192.168.2.1",
					PerNodeBlockSize: 10,
				},
			}
			Expect(testClient.Create(ctx, pool2)).To(Succeed())
			cleanupObjects = append(cleanupObjects, pool2)

			By("Call getNVIPAMPoolsByMatchLabels")
			ipam := &dpuservicev1.IPAM{
				MatchLabels: testLabels,
			}
			Eventually(func(g Gomega) {
				poolNames, poolType, err := getNVIPAMPoolsByMatchLabels(ctx, testClient, ipam)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(poolNames).To(Or(Equal([]string{"ippool-1"}), Equal([]string{"ippool-2"})))
				g.Expect(poolType).To(Equal("ippool"))
			}).WithTimeout(2 * time.Second).Should(Succeed())

			ipam.RequiredIPFamilies = []corev1.IPFamily{corev1.IPv4Protocol}
			Eventually(func(g Gomega) {
				_, _, err := getNVIPAMPoolsByMatchLabels(ctx, testClient, ipam)
				g.Expect(err).To(MatchError(ContainSubstring("requires exactly 1")))
			}).WithTimeout(2 * time.Second).Should(Succeed())
		})

		It("selects the first CIDRPool when requiredIPFamilies is omitted and no IPPool matches", func() {
			testLabels := map[string]string{
				"test-label": "multiple-cidrpools",
			}

			By("Create first CIDRPool")
			pool1 := &nvipamv1.CIDRPool{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cidrpool-1",
					Namespace: defaultNS,
					Labels:    testLabels,
				},
				Spec: nvipamv1.CIDRPoolSpec{
					CIDR:                 "192.168.10.0/24",
					PerNodeNetworkPrefix: 31,
				},
			}
			Expect(testClient.Create(ctx, pool1)).To(Succeed())
			cleanupObjects = append(cleanupObjects, pool1)

			By("Create second CIDRPool with same labels")
			pool2 := &nvipamv1.CIDRPool{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cidrpool-2",
					Namespace: defaultNS,
					Labels:    testLabels,
				},
				Spec: nvipamv1.CIDRPoolSpec{
					CIDR:                 "192.168.20.0/24",
					PerNodeNetworkPrefix: 31,
				},
			}
			Expect(testClient.Create(ctx, pool2)).To(Succeed())
			cleanupObjects = append(cleanupObjects, pool2)

			By("Call getNVIPAMPoolsByMatchLabels")
			ipam := &dpuservicev1.IPAM{
				MatchLabels: testLabels,
			}
			Eventually(func(g Gomega) {
				poolNames, poolType, err := getNVIPAMPoolsByMatchLabels(ctx, testClient, ipam)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(poolNames).To(Or(Equal([]string{"cidrpool-1"}), Equal([]string{"cidrpool-2"})))
				g.Expect(poolType).To(Equal("cidrpool"))
			}).WithTimeout(2 * time.Second).Should(Succeed())
		})

		It("should return error when no pools match labels", func() {
			testLabels := map[string]string{
				"test-label": "no-match",
			}

			By("Call getNVIPAMPoolsByMatchLabels with non-existent labels")
			ipam := &dpuservicev1.IPAM{
				MatchLabels: testLabels,
			}
			_, _, err := getNVIPAMPoolsByMatchLabels(ctx, testClient, ipam)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no IPPool or CIDRPool found for labels"))
		})

		It("prefers IPPool and does not validate unselected CIDRPools when requiredIPFamilies is omitted", func() {
			testLabels := map[string]string{"test-label": "legacy-kind-preference"}
			ipPool := &nvipamv1.IPPool{
				ObjectMeta: metav1.ObjectMeta{Name: "selected-ippool", Namespace: defaultNS, Labels: testLabels},
				Spec:       nvipamv1.IPPoolSpec{Subnet: "192.0.2.0/24", Gateway: "192.0.2.1", PerNodeBlockSize: 10},
			}
			invalidCIDRPool := &nvipamv1.CIDRPool{
				ObjectMeta: metav1.ObjectMeta{Name: "unselected-invalid-cidrpool", Namespace: defaultNS, Labels: testLabels},
				Spec:       nvipamv1.CIDRPoolSpec{CIDR: "not-a-cidr", PerNodeNetworkPrefix: 28},
			}
			Expect(testClient.Create(ctx, ipPool)).To(Succeed())
			Expect(testClient.Create(ctx, invalidCIDRPool)).To(Succeed())
			cleanupObjects = append(cleanupObjects, ipPool, invalidCIDRPool)

			Eventually(func(g Gomega) {
				ipPools := &nvipamv1.IPPoolList{}
				g.Expect(testClient.List(ctx, ipPools, client.MatchingLabels(testLabels))).To(Succeed())
				g.Expect(ipPools.Items).To(HaveLen(1))
				cidrPools := &nvipamv1.CIDRPoolList{}
				g.Expect(testClient.List(ctx, cidrPools, client.MatchingLabels(testLabels))).To(Succeed())
				g.Expect(cidrPools.Items).To(HaveLen(1))

				poolNames, poolType, err := getNVIPAMPoolsByMatchLabels(ctx, testClient, &dpuservicev1.IPAM{MatchLabels: testLabels})
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(poolNames).To(Equal([]string{"selected-ippool"}))
				g.Expect(poolType).To(Equal("ippool"))
			}).WithTimeout(2 * time.Second).Should(Succeed())
		})

		It("should reject ambiguous mixed pool kinds", func() {
			testLabels := map[string]string{
				"test-label": "both-types",
			}

			By("Create IPPool")
			ipPool := &nvipamv1.IPPool{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ippool-preferred",
					Namespace: defaultNS,
					Labels:    testLabels,
				},
				Spec: nvipamv1.IPPoolSpec{
					Subnet:           "192.168.30.0/24",
					Gateway:          "192.168.30.1",
					PerNodeBlockSize: 10,
				},
			}
			Expect(testClient.Create(ctx, ipPool)).To(Succeed())
			cleanupObjects = append(cleanupObjects, ipPool)

			By("Create CIDRPool with same labels")
			cidrPool := &nvipamv1.CIDRPool{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cidrpool-not-preferred",
					Namespace: defaultNS,
					Labels:    testLabels,
				},
				Spec: nvipamv1.CIDRPoolSpec{
					CIDR:                 "2001:db8:40::/64",
					PerNodeNetworkPrefix: 80,
				},
			}
			Expect(testClient.Create(ctx, cidrPool)).To(Succeed())
			cleanupObjects = append(cleanupObjects, cidrPool)

			By("Call getNVIPAMPoolsByMatchLabels")
			ipam := &dpuservicev1.IPAM{
				MatchLabels:        testLabels,
				RequiredIPFamilies: []corev1.IPFamily{corev1.IPv4Protocol, corev1.IPv6Protocol},
			}
			Eventually(func(g Gomega) {
				_, _, err := getNVIPAMPoolsByMatchLabels(ctx, testClient, ipam)
				g.Expect(err).To(MatchError(ContainSubstring("must have the same poolType")))
			}).WithTimeout(2 * time.Second).Should(Succeed())
		})

		It("returns an IPv4 and IPv6 IPPool pair in canonical order", func() {
			testLabels := map[string]string{"test-label": "dual-stack-ippools"}
			ipv6Pool := &nvipamv1.IPPool{
				ObjectMeta: metav1.ObjectMeta{Name: "pool-v6", Namespace: defaultNS, Labels: testLabels},
				Spec:       nvipamv1.IPPoolSpec{Subnet: "2001:db8::/64", Gateway: "2001:db8::1", PerNodeBlockSize: 10},
			}
			ipv4Pool := &nvipamv1.IPPool{
				ObjectMeta: metav1.ObjectMeta{Name: "pool-v4", Namespace: defaultNS, Labels: testLabels},
				Spec:       nvipamv1.IPPoolSpec{Subnet: "192.0.2.0/24", Gateway: "192.0.2.1", PerNodeBlockSize: 10},
			}
			Expect(testClient.Create(ctx, ipv6Pool)).To(Succeed())
			Expect(testClient.Create(ctx, ipv4Pool)).To(Succeed())
			cleanupObjects = append(cleanupObjects, ipv6Pool, ipv4Pool)

			ipam := &dpuservicev1.IPAM{
				MatchLabels:        testLabels,
				RequiredIPFamilies: []corev1.IPFamily{corev1.IPv6Protocol, corev1.IPv4Protocol},
			}
			Eventually(func(g Gomega) {
				poolNames, poolType, err := getNVIPAMPoolsByMatchLabels(ctx, testClient, ipam)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(poolNames).To(Equal([]string{"pool-v4", "pool-v6"}))
				g.Expect(poolType).To(Equal("ippool"))
			}).WithTimeout(2 * time.Second).Should(Succeed())
		})

		It("returns an IPv4 and IPv6 CIDRPool pair in canonical order", func() {
			testLabels := map[string]string{"test-label": "dual-stack-cidrpools"}
			ipv6Pool := &nvipamv1.CIDRPool{
				ObjectMeta: metav1.ObjectMeta{Name: "cidrpool-v6", Namespace: defaultNS, Labels: testLabels},
				Spec:       nvipamv1.CIDRPoolSpec{CIDR: "2001:db8::/64", PerNodeNetworkPrefix: 80},
			}
			ipv4Pool := &nvipamv1.CIDRPool{
				ObjectMeta: metav1.ObjectMeta{Name: "cidrpool-v4", Namespace: defaultNS, Labels: testLabels},
				Spec:       nvipamv1.CIDRPoolSpec{CIDR: "192.0.2.0/24", PerNodeNetworkPrefix: 28},
			}
			Expect(testClient.Create(ctx, ipv6Pool)).To(Succeed())
			Expect(testClient.Create(ctx, ipv4Pool)).To(Succeed())
			cleanupObjects = append(cleanupObjects, ipv6Pool, ipv4Pool)

			ipam := &dpuservicev1.IPAM{
				MatchLabels:        testLabels,
				RequiredIPFamilies: []corev1.IPFamily{corev1.IPv6Protocol, corev1.IPv4Protocol},
			}
			Eventually(func(g Gomega) {
				poolNames, poolType, err := getNVIPAMPoolsByMatchLabels(ctx, testClient, ipam)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(poolNames).To(Equal([]string{"cidrpool-v4", "cidrpool-v6"}))
				g.Expect(poolType).To(Equal("cidrpool"))
			}).WithTimeout(2 * time.Second).Should(Succeed())
		})
	})

	It("injects both dual-stack pool names into the CNI arguments", func() {
		networks := []*multustypes.NetworkSelectionElement{{Name: "service-network", InterfaceRequest: "sf0"}}
		poolType := "ippool"
		settings := map[string]*networkSettings{
			"sf0": {IPAMPoolNames: []string{"pool-v4", "pool-v6"}, IPAMPoolType: &poolType},
		}
		mutated, changed := mutateNetworksWithSettings(networks, settings)
		Expect(changed).To(BeTrue())
		Expect(*mutated[0].CNIArgs).To(HaveKeyWithValue("poolNames", []string{"pool-v4", "pool-v6"}))
		Expect(*mutated[0].CNIArgs).To(HaveKeyWithValue("poolType", "ippool"))
	})
})

// nolint:unparam
func expectedSingleNetAnnotationWithIPAM(ifcName string, pooltype string, assignGW bool, mtu int) string {
	s := fmt.Sprintf("[{\"name\":\"mybrsfc\",\"namespace\":\"default\",\"interface\":\"%s\",\"cni-args\":{\"allocateDefaultGateway\":%v,\"mtu\":%d,\"poolNames\":[\"pool-1\"],\"poolType\":\"%s\"}}]",
		ifcName, assignGW, mtu, pooltype)
	return s
}

func expectedSingleNetAnnotationWithoutIPAM(mtu int) string {
	s := fmt.Sprintf("[{\"name\":\"mybrsfc\",\"namespace\":\"default\",\"interface\":\"%s\",\"cni-args\":{\"mtu\":%d}}]", ifcName, mtu)
	return s
}

// nolint:unparam
func singleNetAnnotationWithInvalid(ifcName string) string {
	return fmt.Sprintf(`[{"name":"mybrsfc","interface": "%s"},{"name":"invalid-network","namespace":"invalid-namespace","interface":"invalid-interface"}]`, ifcName)
}

func singleNetAnnotation() string {
	return fmt.Sprintf(`[{"name":"mybrsfc","interface": "%s"}]`, ifcName)
}

func multipleNetAnnotation() string {
	return fmt.Sprintf(`[{"name":"mybrsfc","interface":"%s"},{"name":"second-network","interface":"%s"},{"name":"invalid-network","namespace":"invalid-namespace","interface":"invalid-interface"}]`, ifcName, ifcName2)
}

func expectedMultipleNetAnnotation(pooltype string, assignGW bool, mtu1 int, mtu2 int) string {
	s := fmt.Sprintf(
		"[{\"name\":\"mybrsfc\",\"namespace\":\"default\",\"interface\":\"sfceth1\",\"cni-args\":{\"allocateDefaultGateway\":%v,\"mtu\":%d,\"poolNames\":[\"pool-1\"],\"poolType\":\"%s\"}},"+
			"{\"name\":\"second-network\",\"namespace\":\"default\",\"interface\":\"sfceth2\",\"cni-args\":{\"allocateDefaultGateway\":%v,\"mtu\":%d,\"poolNames\":[\"pool-2\"],\"poolType\":\"%s\"}}]",
		assignGW, mtu1, pooltype, assignGW, mtu2, pooltype)
	return s
}

func changePodState(ctx context.Context, phase corev1.PodPhase) {
	Eventually(func(g Gomega) {
		pod := &corev1.Pod{}
		g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: defaultNS, Name: podName}, pod)).To(Succeed())
		pod.Status.Phase = phase
		pod.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Pod"))
		pod.ManagedFields = nil
		g.Expect(testClient.Status().Patch(ctx, pod, client.Apply, client.ForceOwnership, client.FieldOwner("test"))).To(Succeed())
	}).WithTimeout(10 * time.Second).Should(Succeed())
}

// calculateDigestEventually wraps CalculatePodNetworkDigest with Eventually to handle cache delays
func calculateDigestEventually(ctx context.Context, pod *corev1.Pod, networks []*multustypes.NetworkSelectionElement) string {
	var digest string
	Eventually(func(g Gomega) {
		var err error
		digest, err = CalculatePodNetworkDigest(ctx, testClient, pod, networks)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(digest).NotTo(BeEmpty())
	}).WithTimeout(2 * time.Second).Should(Succeed())
	return digest
}

func createPodWithNetworkAnnotation(ctx context.Context, networkAnnot string) *corev1.Pod {
	grace := int64(0)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:                       podName,
			Namespace:                  defaultNS,
			Annotations:                map[string]string{},
			Labels:                     map[string]string{dpuservicev1.DPFServiceIDLabelKey: serviceName},
			DeletionGracePeriodSeconds: &grace,
		},
		Spec: corev1.PodSpec{
			NodeName: nodeName,
			Containers: []corev1.Container{
				{Name: "ctr1", Image: "image"},
			},
		},
	}
	if networkAnnot != "" {
		pod.Annotations[multusKey] = networkAnnot
	}
	Expect(testClient.Create(ctx, pod)).NotTo(HaveOccurred())
	By("Turning the Pod to Pending")
	changePodState(ctx, corev1.PodPending)
	return pod
}

func createIPPool(ctx context.Context, name string, labels map[string]string) *nvipamv1.IPPool {
	pool := &nvipamv1.IPPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: defaultNS,
			Labels:    labels,
		},
		Spec: nvipamv1.IPPoolSpec{
			Subnet:           "192.168.0.0/16",
			Gateway:          "192.168.0.1",
			PerNodeBlockSize: 10,
		},
	}
	Expect(testClient.Create(ctx, pool)).NotTo(HaveOccurred())
	return pool
}

func createCidrPool(ctx context.Context, name string, labels map[string]string) *nvipamv1.CIDRPool {
	pool := &nvipamv1.CIDRPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: defaultNS,
			Labels:    labels,
		},
		Spec: nvipamv1.CIDRPoolSpec{
			CIDR:                 "192.168.100.0/24",
			PerNodeNetworkPrefix: 31,
		},
	}
	Expect(testClient.Create(ctx, pool)).NotTo(HaveOccurred())
	return pool
}

// nolint:unparam
func createServiceChainWithServiceInterface(ctx context.Context, name string, ipam *dpuservicev1.IPAM, svcName string, ifcName string, mtu *int) *dpuservicev1.ServiceChain {
	scs := &dpuservicev1.ServiceChain{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: defaultNS,
		},
		Spec: dpuservicev1.ServiceChainSpec{
			Node: ptr.To(nodeName),
			Switches: []dpuservicev1.Switch{
				{
					ServiceMTU: mtu,
					Ports: []dpuservicev1.Port{
						{
							ServiceInterface: dpuservicev1.ServiceIfc{
								MatchLabels: map[string]string{
									dpuservicev1.DPFServiceIDLabelKey: svcName,
									serviceInterfaceAnnotKey:          ifcName,
								},
								IPAM: ipam,
							},
						},
					},
				},
			},
		},
	}
	Expect(testClient.Create(ctx, scs)).NotTo(HaveOccurred())
	return scs
}

// nolint:unparam
func createServiceInterfaceForService(ctx context.Context, name string, svcName string, ifcName string) *dpuservicev1.ServiceInterface {
	si := &dpuservicev1.ServiceInterface{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: defaultNS,
			Labels: map[string]string{
				dpuservicev1.DPFServiceIDLabelKey: svcName,
				serviceInterfaceAnnotKey:          ifcName,
			},
		},
		Spec: dpuservicev1.ServiceInterfaceSpec{
			InterfaceType: dpuservicev1.InterfaceTypeService,
			Node:          ptr.To(nodeName),
			Service: &dpuservicev1.ServiceDef{
				ServiceID:     serviceName,
				InterfaceName: ifcName,
			},
		},
	}
	Expect(testClient.Create(ctx, si)).NotTo(HaveOccurred())
	return si
}

// nolint:unparam
func createNodeServiceInterfaces(ctx context.Context, name, nsiType string, entries ...dpuservicev1.InterfaceEntry) *dpuservicev1.NodeServiceInterfaces {
	nsi := &dpuservicev1.NodeServiceInterfaces{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: utils.NSIObjectsNamespace,
		},
		Spec: dpuservicev1.NodeServiceInterfacesSpec{
			Node:       nodeName,
			Type:       nsiType,
			Interfaces: entries,
		},
	}
	Expect(testClient.Create(ctx, nsi)).NotTo(HaveOccurred())
	return nsi
}

// Helper functions for creating test objects

// createTestPodInMemory creates a pod object in memory (not in cluster) for digest calculation tests.
// This is used for unit tests that need to test digest calculation without creating actual pods in the cluster.
// The pod is created with the specified name, namespace, nodeName, annotations, and labels.
func createTestPodInMemory(annotations map[string]string, labels map[string]string) *corev1.Pod {
	if annotations == nil {
		annotations = make(map[string]string)
	}
	if labels == nil {
		labels = make(map[string]string)
	}

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test-pod",
			Namespace:   defaultNS,
			Annotations: annotations,
			Labels:      labels,
		},
		Spec: corev1.PodSpec{
			NodeName: nodeName,
		},
	}
}
