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

package nvidia

import (
	"fmt"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/operator/inventory"
	testutils "github.com/nvidia/doca-platform/test/utils"
	kamajiv1 "github.com/nvidia/doca-platform/third_party/forked/github.com/clastix/kamaji/api/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("Kamaji Handler - DPU cluster DNS Service", func() {
	var (
		testNS       *corev1.Namespace
		handler      *clusterHandler
		tenantClient client.Client
		dpuCluster   *provisioningv1.DPUCluster
		tcp          *kamajiv1.TenantControlPlane
	)

	// createDPFOperatorConfig creates the singleton config, whose namespace is where the host
	// cluster CoreDNS Service lives.
	createDPFOperatorConfig := func(namespace string) *operatorv1.DPFOperatorConfig {
		config := &operatorv1.DPFOperatorConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "config", Namespace: namespace},
			Spec: operatorv1.DPFOperatorConfigSpec{
				DeploymentMode: operatorv1.DeploymentModeHostTrusted,
				ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{
					BFBPersistentVolumeClaimName: ptr.To("pvc"),
				},
			},
		}
		Expect(k8sClient.Create(ctx, config)).To(Succeed())
		DeferCleanup(testutils.CleanupAndWait, ctx, k8sClient, config)
		return config
	}

	// createHostCoreDNS creates the host cluster CoreDNS the operator renders for a DPUCluster: the
	// Deployment answering queries and the Service exposing it on a NodePort.
	createHostCoreDNS := func(dc *provisioningv1.DPUCluster, namespace string, udpPort, tcpPort, availableReplicas int32) *corev1.Service {
		// The name is arbitrary, the cluster manager finds these by label.
		name := fmt.Sprintf("coredns-%s", dc.Name)
		labels := inventory.GetCoreDNSWorkloadLabels(dc.Name, dc.Namespace)
		deployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels},
			Spec: appsv1.DeploymentSpec{
				Selector: &metav1.LabelSelector{MatchLabels: labels},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: labels},
					Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "coredns", Image: "coredns"}}},
				},
			},
		}
		Expect(k8sClient.Create(ctx, deployment)).To(Succeed())
		DeferCleanup(k8sClient.Delete, ctx, deployment)
		deployment.Status.Replicas = availableReplicas
		deployment.Status.ReadyReplicas = availableReplicas
		deployment.Status.AvailableReplicas = availableReplicas
		Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())

		serviceType := corev1.ServiceTypeNodePort
		if udpPort == 0 && tcpPort == 0 {
			// A ClusterIP Service models the window before the NodePorts are allocated. A NodePort
			// Service cannot, because the API server allocates ports for it immediately.
			serviceType = corev1.ServiceTypeClusterIP
		}
		svc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels},
			Spec: corev1.ServiceSpec{
				Type: serviceType,
				Ports: []corev1.ServicePort{
					{Name: dnsUDPPortName, Port: 53, Protocol: corev1.ProtocolUDP, NodePort: udpPort},
					{Name: dnsTCPPortName, Port: 53, Protocol: corev1.ProtocolTCP, NodePort: tcpPort},
				},
			},
		}
		Expect(k8sClient.Create(ctx, svc)).To(Succeed())
		DeferCleanup(k8sClient.Delete, ctx, svc)
		return svc
	}

	BeforeEach(func() {
		testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "testns-"}}
		Expect(k8sClient.Create(ctx, testNS)).To(Succeed())
		DeferCleanup(k8sClient.Delete, ctx, testNS)

		tenantScheme := runtime.NewScheme()
		Expect(scheme.AddToScheme(tenantScheme)).To(Succeed())
		tenantClient = fake.NewClientBuilder().WithScheme(tenantScheme).Build()

		handler = &clusterHandler{
			Client:       k8sClient,
			Scheme:       scheme.Scheme,
			tenantClient: &staticTenantClientProvider{client: tenantClient},
		}

		dpuCluster = &provisioningv1.DPUCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "dns-cluster", Namespace: testNS.Name},
			Spec: provisioningv1.DPUClusterSpec{
				Type:     string(provisioningv1.KamajiCluster),
				MaxNodes: 100,
				ClusterEndpoint: &provisioningv1.ClusterEndpointSpec{
					Keepalived: &provisioningv1.KeepalivedSpec{VIP: "10.0.110.200"},
				},
			},
		}
		// Built the way the cluster manager builds it, so the DNS Service IP is derived from the
		// service CIDR the DPU cluster is really created with.
		var err error
		tcp, err = expectedTenantControlPlane(dpuCluster, scheme.Scheme, int32(30443), inventory.DefaultFlannelPodCIDR)
		Expect(err).NotTo(HaveOccurred())
	})

	// createLegacyTCP creates the DPUCluster and a TenantControlPlane that still has the Kamaji
	// CoreDNS addon, which is how a cluster created before DNS moved to the host cluster looks.
	createLegacyTCP := func() *kamajiv1.TenantControlPlane {
		// Interface and VirtualRouterID are required by the CRD.
		dpuCluster.Spec.ClusterEndpoint.Keepalived.Interface = "br-dpu"
		dpuCluster.Spec.ClusterEndpoint.Keepalived.VirtualRouterID = 126
		Expect(k8sClient.Create(ctx, dpuCluster)).To(Succeed())
		DeferCleanup(testutils.CleanupAndWait, ctx, k8sClient, dpuCluster)

		var err error
		tcp, err = expectedTenantControlPlane(dpuCluster, scheme.Scheme, int32(30443), inventory.DefaultFlannelPodCIDR)
		Expect(err).NotTo(HaveOccurred())
		tcp.Spec.Addons.CoreDNS = &kamajiv1.AddonSpec{}
		Expect(k8sClient.Create(ctx, tcp)).To(Succeed())
		DeferCleanup(k8sClient.Delete, ctx, tcp)
		return tcp
	}

	reconcileDNS := func() {
		Expect(handler.reconcileDNSService(ctx, dpuCluster, tcp)).To(Succeed())
	}

	// dnsObjects reads back what was written into the tenant cluster.
	dnsObjects := func() (*corev1.Service, *discoveryv1.EndpointSlice) {
		svc := &corev1.Service{}
		Expect(tenantClient.Get(ctx, types.NamespacedName{Name: KubeDNSServiceName, Namespace: metav1.NamespaceSystem}, svc)).To(Succeed())
		endpointSlice := &discoveryv1.EndpointSlice{}
		Expect(tenantClient.Get(ctx, types.NamespacedName{Name: KubeDNSServiceName, Namespace: metav1.NamespaceSystem}, endpointSlice)).To(Succeed())
		return svc, endpointSlice
	}

	It("should point the DNS Service at the host cluster CoreDNS", func() {
		dpfOperatorConfig := createDPFOperatorConfig(testNS.Name)
		createHostCoreDNS(dpuCluster, dpfOperatorConfig.Namespace, 32166, 32167, 1)

		reconcileDNS()

		svc, endpointSlice := dnsObjects()

		By("Verifying the Service has no selector, so only the explicit endpoints back it")
		Expect(svc.Spec.Selector).To(BeEmpty())
		Expect(svc.Spec.ClusterIP).To(Equal("10.96.0.10"))
		Expect(svc.Spec.Ports).To(HaveLen(2))

		By("Verifying the endpoints name the keepalived VIP on the CoreDNS NodePorts")
		Expect(endpointSlice.Endpoints).To(HaveLen(1))
		Expect(endpointSlice.Endpoints[0].Addresses).To(ConsistOf("10.0.110.200"))
		Expect(endpointSlice.Labels).To(HaveKeyWithValue(discoveryv1.LabelServiceName, KubeDNSServiceName))

		ports := map[string]int32{}
		for _, port := range endpointSlice.Ports {
			ports[*port.Name] = *port.Port
		}
		Expect(ports).To(HaveKeyWithValue(dnsUDPPortName, int32(32166)))
		Expect(ports).To(HaveKeyWithValue(dnsTCPPortName, int32(32167)))
	})

	It("should not write again once the DNS Service and EndpointSlice are correct", func() {
		// The DPU cluster API server is reconciled against every minute, so an unchanged DNS Service
		// must not cost a write.
		dpfOperatorConfig := createDPFOperatorConfig(testNS.Name)
		createHostCoreDNS(dpuCluster, dpfOperatorConfig.Namespace, 32178, 32179, 1)

		reconcileDNS()
		svc, endpointSlice := dnsObjects()

		reconcileDNS()

		svcAgain, endpointSliceAgain := dnsObjects()
		Expect(svcAgain.ResourceVersion).To(Equal(svc.ResourceVersion))
		Expect(endpointSliceAgain.ResourceVersion).To(Equal(endpointSlice.ResourceVersion))
	})

	It("should update the endpoints when the CoreDNS NodePorts change", func() {
		dpfOperatorConfig := createDPFOperatorConfig(testNS.Name)
		coreDNS := createHostCoreDNS(dpuCluster, dpfOperatorConfig.Namespace, 32180, 32181, 1)

		reconcileDNS()

		coreDNS.Spec.Ports[0].NodePort = 32182
		coreDNS.Spec.Ports[1].NodePort = 32183
		Expect(k8sClient.Update(ctx, coreDNS)).To(Succeed())

		reconcileDNS()

		_, endpointSlice := dnsObjects()
		ports := map[string]int32{}
		for _, port := range endpointSlice.Ports {
			ports[*port.Name] = *port.Port
		}
		Expect(ports).To(HaveKeyWithValue(dnsUDPPortName, int32(32182)))
		Expect(ports).To(HaveKeyWithValue(dnsTCPPortName, int32(32183)))
	})

	It("should keep the ClusterIP of an already existing DNS Service", func() {
		dpfOperatorConfig := createDPFOperatorConfig(testNS.Name)
		createHostCoreDNS(dpuCluster, dpfOperatorConfig.Namespace, 32168, 32169, 1)

		// A cluster whose service CIDR is not the default still has kubelet pointed at its own DNS
		// Service IP, so an existing ClusterIP must never be replaced.
		Expect(tenantClient.Create(ctx, &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: KubeDNSServiceName, Namespace: metav1.NamespaceSystem},
			Spec:       corev1.ServiceSpec{ClusterIP: "172.16.0.10"},
		})).To(Succeed())

		reconcileDNS()

		svc, _ := dnsObjects()
		Expect(svc.Spec.ClusterIP).To(Equal("172.16.0.10"))
	})

	It("should derive the DNS Service IP from the service CIDR", func() {
		dpfOperatorConfig := createDPFOperatorConfig(testNS.Name)
		createHostCoreDNS(dpuCluster, dpfOperatorConfig.Namespace, 32172, 32173, 1)
		tcp.Spec.NetworkProfile.ServiceCIDRs = []string{"172.16.0.0/16"}

		reconcileDNS()

		svc, _ := dnsObjects()
		Expect(svc.Spec.ClusterIP).To(Equal("172.16.0.10"))
	})

	It("should derive the DNS Service IP from a TenantControlPlane on the deprecated field", func() {
		// Kamaji defaults the deprecated field whether or not it is used, so a cluster created
		// before DPF set the plural one still has to resolve against its own range.
		dpfOperatorConfig := createDPFOperatorConfig(testNS.Name)
		createHostCoreDNS(dpuCluster, dpfOperatorConfig.Namespace, 32190, 32191, 1)
		tcp.Spec.NetworkProfile.ServiceCIDRs = nil
		tcp.Spec.NetworkProfile.ServiceCIDR = "172.16.0.0/16"

		reconcileDNS()

		svc, _ := dnsObjects()
		Expect(svc.Spec.ClusterIP).To(Equal("172.16.0.10"))
	})

	It("should do nothing for a DPUCluster without a keepalived VIP", func() {
		// Static DPUClusters keep serving DNS from inside the DPU cluster.
		dpuCluster.Spec.ClusterEndpoint = nil

		reconcileDNS()

		svc := &corev1.Service{}
		err := tenantClient.Get(ctx, types.NamespacedName{Name: KubeDNSServiceName, Namespace: metav1.NamespaceSystem}, svc)
		Expect(err).To(HaveOccurred())
	})

	It("should do nothing while the CoreDNS Service does not exist yet", func() {
		createDPFOperatorConfig(testNS.Name)

		reconcileDNS()

		svc := &corev1.Service{}
		err := tenantClient.Get(ctx, types.NamespacedName{Name: KubeDNSServiceName, Namespace: metav1.NamespaceSystem}, svc)
		Expect(err).To(HaveOccurred())
	})

	It("should do nothing while the CoreDNS NodePorts are not allocated yet", func() {
		dpfOperatorConfig := createDPFOperatorConfig(testNS.Name)
		createHostCoreDNS(dpuCluster, dpfOperatorConfig.Namespace, 0, 0, 1)

		reconcileDNS()

		svc := &corev1.Service{}
		err := tenantClient.Get(ctx, types.NamespacedName{Name: KubeDNSServiceName, Namespace: metav1.NamespaceSystem}, svc)
		Expect(err).To(HaveOccurred())
	})

	It("should do nothing while the host cluster CoreDNS has not rolled out", func() {
		// Pointing the DNS Service at a CoreDNS that cannot answer yet would black hole DNS for
		// the DPU cluster, which still serves it itself at this point.
		dpfOperatorConfig := createDPFOperatorConfig(testNS.Name)
		createHostCoreDNS(dpuCluster, dpfOperatorConfig.Namespace, 32184, 32185, 0)

		reconcileDNS()

		svc := &corev1.Service{}
		err := tenantClient.Get(ctx, types.NamespacedName{Name: KubeDNSServiceName, Namespace: metav1.NamespaceSystem}, svc)
		Expect(err).To(HaveOccurred())
	})

	It("should stop Kamaji serving DNS once the DNS Service points at the host cluster", func() {
		tcp := createLegacyTCP()
		dpfOperatorConfig := createDPFOperatorConfig(testNS.Name)
		createHostCoreDNS(dpuCluster, dpfOperatorConfig.Namespace, 32186, 32187, 1)

		reconcileDNS()

		got := &kamajiv1.TenantControlPlane{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: tcp.Name, Namespace: tcp.Namespace}, got)).To(Succeed())
		Expect(got.Spec.Addons.CoreDNS).To(BeNil())
	})

	It("should keep Kamaji serving DNS while the host cluster CoreDNS cannot answer", func() {
		tcp := createLegacyTCP()
		dpfOperatorConfig := createDPFOperatorConfig(testNS.Name)
		createHostCoreDNS(dpuCluster, dpfOperatorConfig.Namespace, 32188, 32189, 0)

		reconcileDNS()

		By("Verifying the DPU cluster keeps the only DNS it has")
		got := &kamajiv1.TenantControlPlane{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: tcp.Name, Namespace: tcp.Namespace}, got)).To(Succeed())
		Expect(got.Spec.Addons.CoreDNS).NotTo(BeNil())

		svc := &corev1.Service{}
		err := tenantClient.Get(ctx, types.NamespacedName{Name: KubeDNSServiceName, Namespace: metav1.NamespaceSystem}, svc)
		Expect(err).To(HaveOccurred())
	})
})

var _ = DescribeTable("getDNSIPFromServiceCIDR",
	func(serviceCIDR string, want string, wantErr bool) {
		got, err := getDNSIPFromServiceCIDR(serviceCIDR)
		if wantErr {
			Expect(err).To(HaveOccurred())
			return
		}
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal(want))
	},
	Entry("kubeadm default", "10.96.0.0/16", "10.96.0.10", false),
	Entry("non default CIDR", "172.16.0.0/16", "172.16.0.10", false),
	Entry("unaligned CIDR is masked first", "10.96.0.5/16", "10.96.0.10", false),
	Entry("CIDR too small for a DNS IP", "10.96.0.0/29", "", true),
	Entry("not a CIDR", "not-a-cidr", "", true),
)
