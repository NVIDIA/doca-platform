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

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/test/utils/dpuservice"
	"github.com/nvidia/doca-platform/test/utils/netshoot"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	machineryruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func ValidateDPUServiceNADConsumedByPod(ctx context.Context, input *systemTestInput) {
	if !input.hasDpuNodes() {
		Skip("Skip test as there are not multiple nodes")
	}

	const (
		serviceName                      = "dummydpuservice"
		namespace                        = "dpuservicenadconsumedbypodns"
		dpuServiceInterfaceCustomNADName = "dpu-service-interface-with-custom-nad"
		dpuServiceNADName                = "mynad"
		serviceChainName                 = "my-service-chain"
		mtu                              = 1500 // All MTUs must match; settings non-default MTU to check for anomalies
		defaultTimeout                   = 10 * time.Second
	)

	serviceInterfaceLabels := map[string]string{
		"svc-iface":                      "my-svc-iface",
		"dpuservice.dpu.nvidia.com/name": serviceName,
	}

	By("Create test namespace: " + namespace)
	createTestNamespace(ctx, input.client, namespace)

	By("Create DPUServiceNAD")
	dpuServiceNAD := constructDPUServiceNAD(dpuServiceNADName, namespace, mtu)
	Expect(input.client.Create(ctx, dpuServiceNAD)).To(Succeed())

	By("Create DPUServiceInterface")
	dpuServiceInterface := constructDPUServiceInterface(dpuServiceInterfaceCustomNADName, namespace, serviceName, dpuServiceNADName, serviceInterfaceLabels)
	Expect(input.client.Create(ctx, dpuServiceInterface)).To(Succeed())

	// FIXME: There is a bug that incorrectly requires a DPUServiceChain to exist before a DPUService can be deployed successfully; remove the DPUServiceChain part if this is fixed
	By("Create DPUServiceChain")
	dpuServiceChain := constructDPUServiceChain(serviceChainName, namespace, mtu, serviceInterfaceLabels)
	Expect(input.client.Create(ctx, dpuServiceChain)).To(Succeed())

	By("Deploy DummyDPUService")
	dpuServiceDummy := constructDummyDPUServiceObject(serviceName, namespace, dpuServiceInterfaceCustomNADName)
	Expect(input.client.Create(ctx, dpuServiceDummy)).To(Succeed())

	By("Verify DPUServiceNAD is ready")
	EventuallyCheckReadyStatusCondition(ctx, input.client, dpuServiceNAD, defaultTimeout)
	By("Verify DPUServiceInterface is ready")
	EventuallyCheckReadyStatusCondition(ctx, input.client, dpuServiceInterface, 10*time.Minute)
	// Only now verify that the DPUServiceChain and DummyDPUService are ready
	// Reason: They depend on each other and the DPUServiceInterface and only then become ready
	By("Verify DPUServiceChain is ready")
	EventuallyCheckReadyStatusCondition(ctx, input.client, dpuServiceChain, 3*time.Minute)

	By("Verify DummyDPUService is ready")
	EventuallyCheckReadyStatusCondition(ctx, input.client, dpuServiceDummy, 3*time.Minute)

	By("Verify DPUService pods are created in DPU cluster")
	Eventually(func(g Gomega) {
		const podServiceLabel string = "svc.dpu.nvidia.com/service"
		podList := &corev1.PodList{}
		g.Expect(dpuClusterClient.List(ctx, podList,
			client.InNamespace(namespace),
			client.MatchingLabels{podServiceLabel: serviceName},
		)).To(Succeed())
		g.Expect(podList.Items).ToNot(BeEmpty(), "No Pods found in DPU cluster containing label: "+podServiceLabel)
	}).WithTimeout(5 * time.Minute).Should(Succeed())
}

func constructDPUServiceNAD(name, namespace string, mtu int) *dpuservicev1.DPUServiceNAD {
	dpuServiceNAD := &dpuservicev1.DPUServiceNAD{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    afterEachCleanupLabels,
		},
		Spec: dpuservicev1.DPUServiceNADSpec{
			ResourceType: "sf",
			Bridge:       "br-sfc",
			ServiceMTU:   mtu,
			IPAM:         false,
		},
	}
	return dpuServiceNAD
}

func constructDPUServiceInterface(name, namespace, serviceName string, network string, serviceInterfaceLabels map[string]string) *dpuservicev1.DPUServiceInterface {
	dpuServiceInterface := &dpuservicev1.DPUServiceInterface{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    afterEachCleanupLabels,
		},
	}
	dpuServiceInterface.Spec.Template.Spec.Template.ObjectMeta.Labels = serviceInterfaceLabels
	dpuServiceInterface.Spec.Template.Spec.Template.Spec.InterfaceType = dpuservicev1.InterfaceTypeService
	dpuServiceInterface.Spec.Template.Spec.Template.Spec.Service = &dpuservicev1.ServiceDef{
		ServiceID:     serviceName,
		Network:       namespace + "/" + network,
		InterfaceName: serviceName,
	}

	return dpuServiceInterface
}

func constructDPUServiceChain(name, namespace string, mtu int, serviceInterfaceLabels map[string]string) *dpuservicev1.DPUServiceChain {
	dpuServiceChain := &dpuservicev1.DPUServiceChain{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    afterEachCleanupLabels,
		},
	}
	dpuServiceChain.Spec.Template.Spec.Template.Spec.Switches = []dpuservicev1.Switch{
		{
			Ports: []dpuservicev1.Port{
				{
					ServiceInterface: dpuservicev1.ServiceIfc{
						MatchLabels: serviceInterfaceLabels,
					},
				},
			},
			ServiceMTU: ptr.To(mtu),
		},
	}

	return dpuServiceChain
}

func constructDummyDPUServiceObject(serviceName, namespace, interfaceName string) *dpuservicev1.DPUService {
	dpuServiceDummy := &dpuservicev1.DPUService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: namespace,
			Labels:    afterEachCleanupLabels,
		},
	}

	By("Set HelmChart; tag: " + tag + ", repo: " + helmRegistry)
	dpuServiceDummy.Spec.HelmChart.Source = dpuservicev1.ApplicationSource{
		Chart:   serviceName + "-chart",
		Version: tag,
		RepoURL: helmRegistry,
	}

	if ngcAPIKey != "" {
		dpuServiceDummy.Spec.HelmChart.Values = &machineryruntime.RawExtension{
			Raw: []byte(fmt.Sprintf(
				`{"imagePullSecrets": [{"name": "%s"}]}`, ngcPullSecretName,
			)),
		}
	}

	dpuServiceDummy.Spec.ServiceID = ptr.To(serviceName)

	dpuServiceDummy.Spec.Interfaces = []string{interfaceName}

	return dpuServiceDummy
}

// VerifyDPUPodToPodRDMATraffic verifies that 2 Pods in the DPUCluster can run RDMA traffic between each other.
func VerifyDPUPodToPodRDMATraffic(ctx context.Context, input *systemTestInput) {
	if input.numberOfDPUNodes != 2 {
		// Test assumes that there are exactly 2 host nodes to match the DPU cluster
		Skip("Skip test as there are not exactly 2 nodes")
	}

	By("Creating the objects in the host cluster")
	setupDPUPodToPodRDMATrafficTest(ctx, input)

	By("Getting the pods in the dpu cluster")
	pod1, pod2 := get2DPUServicePods(ctx, input.namespace, "dummydpuservice-rdma")
	podIP1 := getPodIPForInterface(pod1, "app_rdma_if")
	podIP2 := getPodIPForInterface(pod2, "app_rdma_if")
	Expect(podIP1).ToNot(BeEmpty())
	Expect(podIP2).ToNot(BeEmpty())

	By("Running RDMA traffic test between the pods in the dpucluster")
	netshoot.RunRDMATrafficTest(dpuClusterRestClient, dpuClusterRestConfig, input.namespace, pod1.Name, pod2.Name, podIP2)
}

func setupDPUPodToPodRDMATrafficTest(ctx context.Context, input *systemTestInput) {
	interfaceConfigs := []dpuservice.TestDPUServiceInterfaceConfig{
		{
			Name:          "p0-rdma",
			Namespace:     input.namespace,
			Type:          "physical",
			InterfaceName: "p0",
			Labels: map[string]string{
				"uplink": "p0",
			},
			Annotations: map[string]string{
				"svc.dpu.nvidia.com/noop-physical-removal": "",
			},
		},
		{
			Name:          "app-sf-rdma",
			Namespace:     input.namespace,
			Type:          "sf",
			InterfaceName: "app_rdma_if",
			ServiceID:     "dummydpuservice-rdma",
			Network:       "mybrsfc-rdma",
			Labels: map[string]string{
				"svc.dpu.nvidia.com/interface": "app-sf-rdma",
			},
		},
	}
	poolLabels := map[string]string{"svc.dpu.nvidia.com/pool": "dummydpuservice-rdma"}

	By("Create and wait for dpu service interfaces")
	createAndWaitForInterfaces(ctx, input.client, input.dpuServiceInterfaceTemplate, interfaceConfigs)

	By("Create the chain between the workload pod and p0")
	fabricChain := generateDPUObj("pod-to-fabric", input.namespace, input.dpuServiceChainTemplate.DeepCopy())
	fabricChain.Spec.Template.Spec.Template.Spec.Switches = []dpuservicev1.Switch{
		{
			Ports: []dpuservicev1.Port{
				{
					ServiceInterface: dpuservicev1.ServiceIfc{
						MatchLabels: interfaceConfigs[1].Labels,
						IPAM: &dpuservicev1.IPAM{
							MatchLabels: poolLabels,
						},
					},
				},
				{
					ServiceInterface: dpuservicev1.ServiceIfc{
						MatchLabels: interfaceConfigs[0].Labels,
					},
				},
			},
		},
	}
	Expect(input.client.Create(ctx, fabricChain)).To(Succeed())

	By("Create DPUServiceIPAM")
	dpuServiceIPAMTemplate := dpuservicev1.DPUServiceIPAM{
		Spec: dpuservicev1.DPUServiceIPAMSpec{
			ObjectMeta: dpuservicev1.ObjectMeta{
				Labels: poolLabels,
			},
			IPV4Subnet: &dpuservicev1.IPV4Subnet{
				Subnet:         "192.168.0.0/24",
				Gateway:        "192.168.0.1",
				PerNodeIPCount: 8,
			},
		},
	}
	dpuServiceIPAM := generateDPUObj("mybrsfc-rdma", input.namespace, &dpuServiceIPAMTemplate)
	Expect(input.client.Create(ctx, dpuServiceIPAM)).To(Succeed())

	By("Create DPUServiceNAD")
	dpuServiceNADTemplate := dpuservicev1.DPUServiceNAD{
		Spec: dpuservicev1.DPUServiceNADSpec{
			ResourceType: "sf",
			Bridge:       "br-sfc",
			// TODO: Remove once the defaulting is fixed in DPUServiceNAD
			ServiceMTU: 1500,
			IPAM:       true,
			Chain:      []dpuservicev1.CNIPlugin{{Type: ptr.To("rdma")}},
		},
	}
	dpuServiceNAD := generateDPUObj("mybrsfc-rdma", input.namespace, &dpuServiceNADTemplate)
	Expect(input.client.Create(ctx, dpuServiceNAD)).To(Succeed())

	By("Create and wait for dummydpuservice DPUService")
	createDummyDPUServiceForRDMA(ctx, input.client, input.namespace, input.dpuService)
	dpuservice.WaitForDPUServices(ctx, input.client, input.namespace, []string{"dummydpuservice-rdma"})
	VerifyDPUClusterPods(ctx, []string{"dummydpuservice-rdma"})

	By("Verify underlying ServiceChain and ServiceInterface objects are ready")
	dpuservice.VerifyUnderlyingDPUObjectsReady(ctx, dpuClusterClient, input.namespace, interfaceConfigs, []string{"pod-to-fabric"})
}

// createDummyDPUServiceForRDMA creates a DPUService using the dummydpuservice and configures it for RDMA testing
func createDummyDPUServiceForRDMA(ctx context.Context, testClient client.Client, namespace string, dpuService *dpuservicev1.DPUService) {
	dummyDPUService := generateDPUObj("dummydpuservice-rdma", namespace, dpuService.DeepCopy())

	dummyDPUService.Spec.HelmChart.Source = dpuservicev1.ApplicationSource{
		Chart:   "dummydpuservice-chart",
		Version: tag,
		RepoURL: helmRegistry,
	}

	values := make(map[string]any)
	values["imagePullSecrets"] = []map[string]string{{"name": "dpf-pull-secret"}}
	values["image"] = map[string]string{"repository": netutilsImage}
	values["securityContext"] = map[string]any{"capabilities": map[string]any{"add": []string{"IPC_LOCK"}}}
	rawValues, err := json.Marshal(values)
	Expect(err).NotTo(HaveOccurred())
	dummyDPUService.Spec.HelmChart.Values.Raw = rawValues

	dummyDPUService.Spec.Interfaces = []string{"app-sf-rdma"}
	dummyDPUService.Spec.ServiceID = ptr.To("dummydpuservice-rdma")

	Expect(testClient.Create(ctx, dummyDPUService)).To(Succeed())
}

// getPodIPForInterface extracts IP from the k8s.v1.cni.cncf.io/networks-status annotation for the given Pod for the
// given interfaceName. It expects that only one IP is assigned to this interface.
func getPodIPForInterface(pod corev1.Pod, interfaceName string) string {
	networksStatusAnnotation, exists := pod.Annotations["k8s.v1.cni.cncf.io/networks-status"]
	Expect(exists).To(BeTrue())

	var networksStatus []map[string]any
	err := json.Unmarshal([]byte(networksStatusAnnotation), &networksStatus)
	Expect(err).NotTo(HaveOccurred())

	var podIPs []string
	for _, network := range networksStatus {
		if iface, ok := network["interface"].(string); ok && iface == interfaceName {
			if ips, ok := network["ips"].([]any); ok {
				Expect(ips).To(HaveLen(1))
				podIPs = append(podIPs, ips[0].(string))
			}
		}
	}

	Expect(podIPs).To(HaveLen(1))

	return podIPs[0]
}

// get2DPUServicePods returns the 2 DPUService Pods associated with a service
func get2DPUServicePods(ctx context.Context, namespace string, serviceID string) (corev1.Pod, corev1.Pod) {
	pods := &corev1.PodList{}
	Expect(dpuClusterClient.List(ctx, pods, client.MatchingLabels{"svc.dpu.nvidia.com/service": serviceID}, client.InNamespace(namespace))).ToNot(HaveOccurred())
	Expect(pods.Items).To(HaveLen(2))
	return pods.Items[0], pods.Items[1]
}
