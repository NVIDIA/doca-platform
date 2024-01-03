/*
COPYRIGHT 2024 NVIDIA

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
	nvipamv1 "github.com/nvidia/doca-platform/internal/nvipam/api/v1alpha1"
	testutils "github.com/nvidia/doca-platform/test/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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

		It("should successfully update Network annotation on Pod - IPAM match labels IpPool", func() {
			By("Create ServiceInterface for Service")
			cleanupObjects = append(cleanupObjects, createServiceInterfaceForService(
				ctx, strings.Join([]string{serviceName, ifcName}, "-"), serviceName, ifcName))
			By("Create ServiceChain with IPAM MatchLabels")
			defaultGateway := false
			ipam := &dpuservicev1.IPAM{
				DefaultGateway: ptr.To(defaultGateway),
				MatchLabels:    ipamLabels,
			}
			cleanupObjects = append(cleanupObjects, createServiceChainWithServiceInterface(ctx, svcName1, ipam, serviceName, ifcName))
			By("Create IpPool")
			cleanupObjects = append(cleanupObjects, createIPPool(ctx, ipamName, ipamLabels))
			By("Create Pod with Network Annotation")
			cleanupObjects = append(cleanupObjects, createPodWithNetworkAnnotation(ctx, singleNetAnnotation(ifcName)))
			By("Check that Pod annotation has been updated")
			Eventually(func(g Gomega) {
				pod := &corev1.Pod{}
				g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: defaultNS, Name: podName}, pod)).To(Succeed())
				g.Expect(pod.Annotations[multusKey]).To(Equal(expectedSingleNetAnnotation(ifcName, "ippool", defaultGateway)))
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
			cleanupObjects = append(cleanupObjects, createServiceChainWithServiceInterface(ctx, svcName1, ipam, serviceName, ifcName))
			By("Create CIDRPool")
			cleanupObjects = append(cleanupObjects, createCidrPool(ctx, ipamName, ipamLabels))
			By("Create Pod with Network Annotation")
			cleanupObjects = append(cleanupObjects, createPodWithNetworkAnnotation(ctx, singleNetAnnotation(ifcName)))
			By("Check that Pod annotation has been updated")
			Eventually(func(g Gomega) {
				pod := &corev1.Pod{}
				g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: defaultNS, Name: podName}, pod)).To(Succeed())
				g.Expect(pod.Annotations[multusKey]).To(Equal(expectedSingleNetAnnotation(ifcName, "cidrpool", defaultGateway)))
			}).WithTimeout(2 * time.Second).Should(BeNil())
			By("Turning the Pod State to Succeed")
			changePodState(ctx, corev1.PodSucceeded)
		})

		It("should successfully update Network annotation on Pod - Multiple networks, Multiple IPAM", func() {
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
			cleanupObjects = append(cleanupObjects, createServiceChainWithServiceInterface(ctx, svcName1, ipam, serviceName, ifcName))
			By("Create ServiceChain2 with IPAM ref")
			ipam2 := &dpuservicev1.IPAM{
				DefaultGateway: ptr.To(defaultGateway),
				MatchLabels:    ipamLabels2,
			}
			cleanupObjects = append(cleanupObjects, createServiceChainWithServiceInterface(ctx, svcName2, ipam2, serviceName, ifcName2))
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
				g.Expect(pod.Annotations[multusKey]).To(Equal(expectedMultipleNetAnnotation("ippool", defaultGateway)))
			}).WithTimeout(2 * time.Second).Should(BeNil())
			By("Turning the Pod State to Succeed")
			changePodState(ctx, corev1.PodSucceeded)
		})

		It("should successfully update Network annotation on Pod - Pod Created before Chain", func() {
			defaultGateway := false
			By("Create Pod with Network Annotation")
			cleanupObjects = append(cleanupObjects, createPodWithNetworkAnnotation(ctx, singleNetAnnotation(ifcName)))
			By("Verify Pod annotations is not updated")
			Consistently(func(g Gomega) {
				pod := &corev1.Pod{}
				g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: defaultNS, Name: podName}, pod)).To(Succeed())
				g.Expect(pod.Annotations[multusKey]).To(Equal(expectedSingleNetAnnotation(ifcName, "ippool", defaultGateway)))
			}).WithTimeout(2 * time.Second).Should(Not(BeNil()))
			By("Create ServiceChain with IPAM MatchLabels")
			ipam := &dpuservicev1.IPAM{
				DefaultGateway: ptr.To(defaultGateway),
				MatchLabels:    ipamLabels,
			}
			cleanupObjects = append(cleanupObjects, createServiceChainWithServiceInterface(ctx, svcName1, ipam, serviceName, ifcName))
			By("Verify Pod annotations is not updated")
			Consistently(func(g Gomega) {
				pod := &corev1.Pod{}
				g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: defaultNS, Name: podName}, pod)).To(Succeed())
				g.Expect(pod.Annotations[multusKey]).To(Equal(expectedSingleNetAnnotation(ifcName, "ippool", defaultGateway)))
			}).WithTimeout(2 * time.Second).Should(Not(BeNil()))
			By("Create ServiceInterface for Service")
			cleanupObjects = append(cleanupObjects, createServiceInterfaceForService(
				ctx, strings.Join([]string{serviceName, ifcName}, "-"), serviceName, ifcName))
			By("Verify Pod annotations is not updated")
			Consistently(func(g Gomega) {
				pod := &corev1.Pod{}
				g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: defaultNS, Name: podName}, pod)).To(Succeed())
				g.Expect(pod.Annotations[multusKey]).To(Equal(expectedSingleNetAnnotation(ifcName, "ippool", defaultGateway)))
			}).WithTimeout(2 * time.Second).Should(Not(BeNil()))
			By("Create IpPool")
			cleanupObjects = append(cleanupObjects, createIPPool(ctx, ipamName, ipamLabels))
			By("Check that Pod annotation has been updated")
			Eventually(func(g Gomega) {
				pod := &corev1.Pod{}
				g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: defaultNS, Name: podName}, pod)).To(Succeed())
				g.Expect(pod.Annotations[multusKey]).To(Equal(expectedSingleNetAnnotation(ifcName, "ippool", defaultGateway)))
			}).WithTimeout(2 * time.Second).Should(BeNil())
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
				g.Expect(pod.Annotations[multusKey]).To(Equal(annotation))
			}).WithTimeout(2 * time.Second).Should(BeNil())
			By("Turning the Pod State to Succeed")
			changePodState(ctx, corev1.PodSucceeded)
		})

		It("should not update Network annotation on Pod - no IPAM requested", func() {
			By("Create ServiceInterface for Service")
			cleanupObjects = append(cleanupObjects, createServiceInterfaceForService(
				ctx, strings.Join([]string{serviceName, ifcName}, "-"), serviceName, ifcName))
			By("Create ServiceChain without IPAM")
			cleanupObjects = append(cleanupObjects, createServiceChainWithServiceInterface(ctx, svcName1, nil, serviceName, ifcName))
			By("Create Pod with Network Annotation")
			cleanupObjects = append(cleanupObjects, createPodWithNetworkAnnotation(ctx, singleNetAnnotation(ifcName)))
			By("Check that Pod annotation has been updated")
			Consistently(func(g Gomega) {
				pod := &corev1.Pod{}
				g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: defaultNS, Name: podName}, pod)).To(Succeed())
				g.Expect(pod.Annotations[multusKey]).To(Equal(singleNetAnnotation(ifcName)))
			}).WithTimeout(2 * time.Second).Should(BeNil())
			By("Turning the Pod State to Succeed")
			changePodState(ctx, corev1.PodSucceeded)
		})
	})
})

//nolint:unparam
func expectedSingleNetAnnotation(ifcName string, pooltype string, assignGW bool) string {
	s := fmt.Sprintf("[{\"name\":\"mybrsfc\",\"namespace\":\"default\",\"interface\":\"%s\",\"cni-args\":{\"allocateDefaultGateway\":%v,\"poolNames\":[\"pool-1\"],\"poolType\":\"%s\"}}]",
		ifcName, assignGW, pooltype)
	return s
}

//nolint:unparam
func singleNetAnnotation(ifcName string) string {
	return fmt.Sprintf(`[{"name":"mybrsfc","interface": "%s"}]`, ifcName)
}

func multipleNetAnnotation() string {
	return fmt.Sprintf(`[{"name":"mybrsfc","interface": "%s"}, {"name":"othernet","interface": "dummy"},
	{"name":"second-network","interface": "%s"}]`, ifcName, ifcName2)
}

func expectedMultipleNetAnnotation(pooltype string, assignGW bool) string {
	s := fmt.Sprintf(
		"[{\"name\":\"mybrsfc\",\"namespace\":\"default\",\"interface\":\"sfceth1\",\"cni-args\":{\"allocateDefaultGateway\":%v,\"poolNames\":[\"pool-1\"],\"poolType\":\"%s\"}},"+
			"{\"name\":\"othernet\",\"namespace\":\"default\",\"interface\":\"dummy\",\"cni-args\":null},"+
			"{\"name\":\"second-network\",\"namespace\":\"default\",\"interface\":\"sfceth2\",\"cni-args\":{\"allocateDefaultGateway\":%v,\"poolNames\":[\"pool-2\"],\"poolType\":\"%s\"}}]",
		assignGW, pooltype, assignGW, pooltype)
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
func createServiceChainWithServiceInterface(ctx context.Context, name string, ipam *dpuservicev1.IPAM, svcName string, ifcName string) *dpuservicev1.ServiceChain {
	scs := &dpuservicev1.ServiceChain{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: defaultNS,
		},
		Spec: dpuservicev1.ServiceChainSpec{
			Node: ptr.To(nodeName),
			Switches: []dpuservicev1.Switch{
				{
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

//nolint:unparam
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
