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
	"context"
	"fmt"
	"net"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/operator/inventory"
	"github.com/nvidia/doca-platform/internal/utils"
	kamajiv1 "github.com/nvidia/doca-platform/third_party/forked/github.com/clastix/kamaji/api/v1alpha1"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/klog/v2"
	netutil "k8s.io/utils/net"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// KubeDNSServiceName is the DNS Service Pods in the DPU cluster resolve against. It is what kubeadm
	// points kubelet at, so keeping it means nothing on a DPU has to be reconfigured when DNS moves
	// to the host cluster.
	KubeDNSServiceName = "kube-dns"

	// dnsUDPPortName and dnsTCPPortName must match the port names on the host cluster CoreDNS
	// Service, because a Service port is mapped to an endpoint port by name.
	dnsUDPPortName = "dns"
	dnsTCPPortName = "dns-tcp"

	// dnsServiceIPOffset is the offset of the DNS Service IP within the service CIDR, matching how
	// kubeadm derives it.
	dnsServiceIPOffset = 10
)

// reconcileDNSService points the DPU cluster DNS Service at the CoreDNS running on the host cluster
// for this DPUCluster.
//
// The Service deliberately has no selector: nothing in the DPU cluster serves DNS any more, so the
// endpoints are supplied explicitly and name the keepalived VIP on the CoreDNS NodePort. kube-proxy
// on each DPU node translates the ClusterIP to it.
//
// Both objects are written straight to the tenant cluster rather than shipped as a DPUService,
// because ArgoCD excludes EndpointSlice from the resources it manages, so an EndpointSlice in a
// Helm chart is silently never applied and the Service is left black holing DNS.
func (cm *clusterHandler) reconcileDNSService(ctx context.Context, dc *provisioningv1.DPUCluster, tcp *kamajiv1.TenantControlPlane) error {
	logger := log.FromContext(ctx)

	// A DPUCluster the host cannot serve keeps its own CoreDNS, so there is nothing to point
	// anywhere. disableCoreDNSAddon leaves the Kamaji addon alone for the same clusters.
	if !inventory.IsDPUClusterServedByHostDNS(dc) {
		return nil
	}
	vip := dc.Spec.ClusterEndpoint.Keepalived.VIP

	udpPort, tcpPort, err := cm.coreDNSNodePorts(ctx, dc)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Writing the endpoints is the cutover: the Service stops selecting whatever serves DNS
			// in the DPU cluster today and points at the host cluster instead. Doing that before the
			// host cluster CoreDNS answers would black hole DNS until it comes up.
			logger.V(1).Info("host cluster CoreDNS is not serving this DPUCluster yet, skipping DNS Service", "dpucluster", dc.Name)
			return nil
		}
		return err
	}

	tenantClient, err := cm.tenantClient.Client(ctx, dc)
	if err != nil {
		return fmt.Errorf("failed to get tenant client for DNS Service, err: %v", err)
	}

	if err := cm.reconcileDNSServiceObject(ctx, tenantClient, tcp); err != nil {
		return err
	}
	if err := cm.reconcileDNSEndpointSlice(ctx, tenantClient, vip, udpPort, tcpPort); err != nil {
		return err
	}

	// The DPU cluster no longer serves its own DNS, so Kamaji must stop running a CoreDNS for it.
	return cm.disableCoreDNSAddon(ctx, dc, tcp)
}

// coreDNSNodePorts returns the NodePorts of the host cluster CoreDNS serving this DPUCluster, once
// that CoreDNS can answer for it. Both protocols are reported because Kubernetes allocates a
// NodePort per Service port. The objects are found by label, because their names are hashed.
//
// A NotFound error means it cannot answer yet, either because the CoreDNS DPUService has not been
// rendered, or because it has not rolled out.
func (cm *clusterHandler) coreDNSNodePorts(ctx context.Context, dc *provisioningv1.DPUCluster) (int32, int32, error) {
	dpfOperatorConfig, err := utils.GetDPFOperatorConfig(ctx, cm.Client)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get DPFOperatorConfig for DNS Service, err: %v", err)
	}
	listOptions := []client.ListOption{
		client.InNamespace(dpfOperatorConfig.Namespace),
		client.MatchingLabels(inventory.GetCoreDNSWorkloadLabels(dc.Name, dc.Namespace)),
	}

	deployments := &appsv1.DeploymentList{}
	if err := cm.Client.List(ctx, deployments, listOptions...); err != nil {
		return 0, 0, fmt.Errorf("failed to list CoreDNS Deployments, err: %v", err)
	}
	deployment, err := onlyItem(deployments.Items, appsv1.Resource("deployments"))
	if err != nil {
		return 0, 0, err
	}
	if deployment.Status.AvailableReplicas == 0 {
		return 0, 0, apierrors.NewNotFound(appsv1.Resource("deployments"), fmt.Sprintf("%s available replicas", deployment.Name))
	}

	services := &corev1.ServiceList{}
	if err := cm.Client.List(ctx, services, listOptions...); err != nil {
		return 0, 0, fmt.Errorf("failed to list CoreDNS Services, err: %v", err)
	}
	svc, err := onlyItem(services.Items, corev1.Resource("services"))
	if err != nil {
		return 0, 0, err
	}

	var udpPort, tcpPort int32
	for _, port := range svc.Spec.Ports {
		switch port.Name {
		case dnsUDPPortName:
			udpPort = port.NodePort
		case dnsTCPPortName:
			tcpPort = port.NodePort
		}
	}
	if udpPort == 0 || tcpPort == 0 {
		return 0, 0, apierrors.NewNotFound(corev1.Resource("services"), fmt.Sprintf("%s NodePorts", svc.Name))
	}
	return udpPort, tcpPort, nil
}

// onlyItem returns the single object the CoreDNS labels are expected to select. A NotFound error
// means it is not there yet, while more than one means the labels no longer identify one CoreDNS.
func onlyItem[T any](items []T, resource schema.GroupResource) (*T, error) {
	switch len(items) {
	case 0:
		return nil, apierrors.NewNotFound(resource, "CoreDNS")
	case 1:
		return &items[0], nil
	default:
		return nil, fmt.Errorf("expected one CoreDNS %s, got %d", resource.Resource, len(items))
	}
}

// serviceCIDRFromNetworkProfile returns the Service range the DPU cluster is served by. Kamaji
// takes ServiceCIDRs over the deprecated ServiceCIDR, which the CRD defaults whether or not it is
// used, so reading the singular field alone would miss the range a dual stack or custom cluster is
// really on. The first entry is the primary range, which is where the DNS Service lives.
func serviceCIDRFromNetworkProfile(networkProfile kamajiv1.NetworkProfileSpec) string {
	if len(networkProfile.ServiceCIDRs) > 0 {
		return networkProfile.ServiceCIDRs[0]
	}
	// TenantControlPlanes created before DPF set the plural field only carry the singular one.
	return networkProfile.ServiceCIDR
}

// dnsIPFromServiceCIDR returns the DNS Service IP for a service CIDR, the same address kubeadm
// derives and hands to kubelet as clusterDNS.
func getDNSIPFromServiceCIDR(cidr string) (string, error) {
	_, subnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", fmt.Errorf("failed to parse service CIDR %q, err: %v", cidr, err)
	}
	ip, err := netutil.GetIndexedIP(subnet, dnsServiceIPOffset)
	if err != nil {
		return "", fmt.Errorf("failed to derive DNS Service IP from service CIDR %q, err: %v", cidr, err)
	}
	return ip.String(), nil
}

// reconcileDNSServiceObject writes the DPU cluster DNS Service. The Service is read first, both
// because an existing ClusterIP is what kubelet was configured with and must be kept, and so an
// already correct Service costs no write at all.
func (cm *clusterHandler) reconcileDNSServiceObject(ctx context.Context, tenantClient client.Client, tcp *kamajiv1.TenantControlPlane) error {
	logger := log.FromContext(ctx)

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: KubeDNSServiceName, Namespace: metav1.NamespaceSystem},
	}
	result, err := controllerutil.CreateOrPatch(ctx, tenantClient, svc, func() error {
		// The ClusterIP has to match the clusterDNS kubelet was configured with, otherwise Pods
		// resolve against an address nothing serves. An existing Service is authoritative, and its
		// ClusterIP is immutable anyway.
		if svc.Spec.ClusterIP == "" || svc.Spec.ClusterIP == corev1.ClusterIPNone {
			// expectedTenantControlPlane writes the service CIDR, so it is always set.
			clusterIP, err := getDNSIPFromServiceCIDR(serviceCIDRFromNetworkProfile(tcp.Spec.NetworkProfile))
			if err != nil {
				return err
			}
			svc.Spec.ClusterIP = clusterIP
		}
		applyDNSService(svc)
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to reconcile DNS Service, err: %v", err)
	}
	if result != controllerutil.OperationResultNone {
		logger.V(1).Info("reconciled DPU cluster DNS Service", "service", klog.KObj(svc),
			"clusterIP", svc.Spec.ClusterIP, "result", result)
	}
	return nil
}

// applyDNSService sets the fields of the DNS Service this cluster manager owns and leaves the rest
// of the object alone.
func applyDNSService(svc *corev1.Service) {
	if svc.Labels == nil {
		svc.Labels = map[string]string{}
	}
	svc.Labels["k8s-app"] = "kube-dns"
	svc.Labels["kubernetes.io/name"] = "CoreDNS"

	svc.Spec.Type = corev1.ServiceTypeClusterIP
	// Nothing in the DPU cluster serves DNS any more, so the endpoints are supplied explicitly by
	// reconcileDNSEndpointSlice instead of being selected.
	svc.Spec.Selector = nil
	// The target ports are the ones the API server defaults to, so a Service that is already correct
	// does not compare as changed.
	svc.Spec.Ports = []corev1.ServicePort{
		{Name: dnsUDPPortName, Port: 53, Protocol: corev1.ProtocolUDP, TargetPort: intstr.FromInt32(53)},
		{Name: dnsTCPPortName, Port: 53, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromInt32(53)},
	}
}

// reconcileDNSEndpointSlice writes the endpoints of the DPU cluster DNS Service. It is read first
// so an unchanged EndpointSlice, which is the case on almost every reconcile, costs no write.
func (cm *clusterHandler) reconcileDNSEndpointSlice(ctx context.Context, tenantClient client.Client, vip string, udpPort, tcpPort int32) error {
	logger := log.FromContext(ctx)

	if net.ParseIP(vip) == nil {
		return fmt.Errorf("keepalived VIP %q is not a valid IP", vip)
	}

	endpointSlice := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{Name: KubeDNSServiceName, Namespace: metav1.NamespaceSystem},
	}
	result, err := controllerutil.CreateOrPatch(ctx, tenantClient, endpointSlice, func() error {
		applyDNSEndpointSlice(endpointSlice, vip, udpPort, tcpPort)
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to reconcile DNS EndpointSlice, err: %v", err)
	}
	if result != controllerutil.OperationResultNone {
		logger.V(1).Info("reconciled DPU cluster DNS EndpointSlice", "endpointslice", klog.KObj(endpointSlice),
			"address", vip, "udpPort", udpPort, "tcpPort", tcpPort, "result", result)
	}
	return nil
}

// applyDNSEndpointSlice sets the fields of the DNS EndpointSlice this cluster manager owns and
// leaves the rest of the object alone.
func applyDNSEndpointSlice(endpointSlice *discoveryv1.EndpointSlice, vip string, udpPort, tcpPort int32) {
	if endpointSlice.Labels == nil {
		endpointSlice.Labels = map[string]string{}
	}
	endpointSlice.Labels[discoveryv1.LabelServiceName] = KubeDNSServiceName
	endpointSlice.Labels[discoveryv1.LabelManagedBy] = kamajiFieldOwner

	endpointSlice.AddressType = discoveryv1.AddressTypeIPv4
	endpointSlice.Ports = []discoveryv1.EndpointPort{
		{Name: ptr.To(dnsUDPPortName), Port: ptr.To(udpPort), Protocol: ptr.To(corev1.ProtocolUDP)},
		{Name: ptr.To(dnsTCPPortName), Port: ptr.To(tcpPort), Protocol: ptr.To(corev1.ProtocolTCP)},
	}
	endpointSlice.Endpoints = []discoveryv1.Endpoint{
		{
			Addresses:  []string{vip},
			Conditions: discoveryv1.EndpointConditions{Ready: ptr.To(true)},
		},
	}
}
