/*
Copyright 2024 NVIDIA

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

package webhooks

import (
	"context"
	"errors"
	"fmt"
	"net"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// DPUServiceIPAMValidator validates DPUServiceIPAM objects
type DPUServiceIPAMValidator struct {
	Client client.Reader
}

const (
	ipv4DefaultRoute = "0.0.0.0/0"
	ipv6DefaultRoute = "::/0"
)

var _ webhook.CustomValidator = &DPUServiceIPAMValidator{}

// +kubebuilder:webhook:path=/validate-svc-dpu-nvidia-com-v1alpha1-dpuserviceipam,mutating=false,failurePolicy=fail,groups=svc.dpu.nvidia.com,resources=dpuserviceipams,verbs=create;update,versions=v1alpha1,name=ipam-validator.svc.dpu.nvidia.com,admissionReviewVersions=v1,sideEffects=None

func (v *DPUServiceIPAMValidator) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&dpuservicev1.DPUServiceIPAM{}).
		WithValidator(v).
		Complete()
}

// ValidateCreate validates the DPUServiceIPAM object on creation.
func (v *DPUServiceIPAMValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (warnings admission.Warnings, err error) {
	log := ctrl.LoggerFrom(ctx)

	ipam, ok := obj.(*dpuservicev1.DPUServiceIPAM)
	if !ok {
		return nil, apierrors.NewBadRequest(fmt.Sprintf("expected a DPUServiceIPAM but got a %T", obj))
	}

	ctrl.LoggerInto(ctx, log.WithValues("DPUServiceIPAM", types.NamespacedName{Namespace: ipam.Namespace, Name: ipam.Name}))

	if err := validateDPUServiceIPAM(ipam, nil); err != nil {
		log.Error(err, "rejected resource creation")
		return nil, apierrors.NewBadRequest(err.Error())
	}

	return nil, nil
}

// ValidateUpdate validates the DPUServiceIPAM object on update.
func (v *DPUServiceIPAMValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (warnings admission.Warnings, err error) {
	log := ctrl.LoggerFrom(ctx)

	oldIpamObj, oldOk := oldObj.(*dpuservicev1.DPUServiceIPAM)
	newIpamObj, newOk := newObj.(*dpuservicev1.DPUServiceIPAM)

	if !newOk || !oldOk {
		return nil, apierrors.NewBadRequest(fmt.Sprintf("expected a DPUServiceIPAM but got a new objecy: %T, and an old object: %T", newObj, oldObj))
	}

	ctrl.LoggerInto(ctx, log.WithValues("DPUServiceIPAM", types.NamespacedName{Namespace: newIpamObj.Namespace, Name: newIpamObj.Name}))

	if err := validateDPUServiceIPAM(newIpamObj, oldIpamObj); err != nil {
		log.Error(err, "rejected resource update")
		return nil, apierrors.NewBadRequest(err.Error())
	}

	return nil, nil
}

// ValidateDelete validates the DPUServiceIPAM object on deletion.
func (v *DPUServiceIPAMValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (warnings admission.Warnings, err error) {
	return nil, nil
}

// validateDPUServiceIPAM validates if a DPUServiceIPAM object is valid
func validateDPUServiceIPAM(newIpamObj, oldIpamObj *dpuservicev1.DPUServiceIPAM) error {
	var errs []error

	// TODO: Drop this once multi namespace NVIPAM is supported
	if newIpamObj.Namespace != "dpf-operator-system" {
		errs = append(errs, errors.New("currently only 'dpf-operator-system' namespace is supported"))
	}

	// TODO: Drop once we fully support transition from IPV4Network to IPV4Subnet and vice versa
	if oldIpamObj != nil && newIpamObj != nil {
		if (oldIpamObj.Spec.IPV4Subnet != nil && newIpamObj.Spec.IPV4Network != nil) || (oldIpamObj.Spec.IPV4Network != nil && newIpamObj.Spec.IPV4Subnet != nil) {
			errs = append(errs, errors.New("transitioning from ipv4subnet to ipv4network and vice versa is currently not supported"))
		}
	}

	if newIpamObj.Spec.IPV4Network == nil && newIpamObj.Spec.IPV4Subnet == nil {
		errs = append(errs, errors.New("either ipv4Subnet or ipv4Network must be specified"))
	}

	if newIpamObj.Spec.IPV4Network != nil && newIpamObj.Spec.IPV4Subnet != nil {
		errs = append(errs, errors.New("either ipv4Subnet or ipv4Network must be specified but not both"))
	}

	if newIpamObj.Spec.IPV4Network != nil {
		errs = append(errs, validateDPUServiceIPAMIPV4Network(newIpamObj.Spec.IPV4Network))
		if oldIpamObj != nil && oldIpamObj.Spec.IPV4Network != nil {
			errs = append(errs, validateIPRangeNotShrinking(newIpamObj.Spec.IPV4Network.Network, oldIpamObj.Spec.IPV4Network.Network))
		}
	}

	if newIpamObj.Spec.IPV4Subnet != nil {
		errs = append(errs, validateDPUServiceIPAMIPV4Subnet(newIpamObj.Spec.IPV4Subnet))
		if oldIpamObj != nil && oldIpamObj.Spec.IPV4Subnet != nil {
			errs = append(errs, validateIPRangeNotShrinking(newIpamObj.Spec.IPV4Subnet.Subnet, oldIpamObj.Spec.IPV4Subnet.Subnet))
		}
	}

	return kerrors.NewAggregate(errs)
}

// validateDPUServiceIPAMIPV4Network validates the .spec.IPV4Network of a DPUServiceIPAM object
func validateDPUServiceIPAMIPV4Network(ipv4Network *dpuservicev1.IPV4Network) error {
	_, network, err := net.ParseCIDR(ipv4Network.Network)
	if err != nil {
		return fmt.Errorf("network %s is not a valid network", ipv4Network.Network)
	}

	networkPrefix, _ := network.Mask.Size()
	if networkPrefix > int(ipv4Network.PrefixSize) {
		return fmt.Errorf("prefixSize %d doesn't fit in network prefix %d", ipv4Network.PrefixSize, networkPrefix)
	}

	var errs []error
	for _, exclusion := range ipv4Network.Exclusions {
		ip := net.ParseIP(exclusion)
		if ip == nil {
			errs = append(errs, fmt.Errorf("exclusion %s is not a valid IP", exclusion))
			continue
		}
		if !network.Contains(ip) {
			errs = append(errs, fmt.Errorf("exclusion %s is not part of network %s", exclusion, ipv4Network.Network))
		}
	}
	for _, allocation := range ipv4Network.Allocations {
		_, allocationNetwork, err := net.ParseCIDR(allocation)
		if err != nil {
			errs = append(errs, fmt.Errorf("allocation %s is not a valid subnet", allocation))
			continue
		}

		allocationNetworkPrefix, _ := allocationNetwork.Mask.Size()
		if !network.Contains(allocationNetwork.IP) || allocationNetworkPrefix != int(ipv4Network.PrefixSize) {
			errs = append(errs, fmt.Errorf("allocation %s is not part of the network %s", allocation, ipv4Network.Network))
		}
	}
	errs = append(errs, validateRoutes(ipv4Network.Routes, network, ipv4Network.DefaultGateway))
	return kerrors.NewAggregate(errs)
}

// validateDPUServiceIPAMIPV4Subnet validates the .spec.IPV4Subnet of a DPUServiceIPAM object
func validateDPUServiceIPAMIPV4Subnet(ipv4Subnet *dpuservicev1.IPV4Subnet) error {
	_, network, err := net.ParseCIDR(ipv4Subnet.Subnet)
	if err != nil {
		return fmt.Errorf("subnet %s is not a valid network", ipv4Subnet.Subnet)
	}

	ip := net.ParseIP(ipv4Subnet.Gateway)
	if ip == nil {
		return fmt.Errorf("gateway %s is not a valid IP", ipv4Subnet.Gateway)
	}

	if !network.Contains(ip) {
		return fmt.Errorf("gateway %s is not part of subnet %s", ipv4Subnet.Gateway, ipv4Subnet.Subnet)
	}

	err = validateRoutes(ipv4Subnet.Routes, network, ipv4Subnet.DefaultGateway)
	if err != nil {
		return err
	}

	return nil
}

// validateRoutes validate routes:
// - dst is a valid CIDR
func validateRoutes(routes []dpuservicev1.Route, network *net.IPNet, defaultGateway bool) error {
	var errs []error
	for _, r := range routes {
		_, routeNet, err := net.ParseCIDR(r.Dst)
		if err != nil {
			errs = append(errs, fmt.Errorf("route %s is not a valid subnet", r.Dst))
		}
		if routeNet != nil && network != nil {
			if (routeNet.IP.To4() != nil) != (network.IP.To4() != nil) {
				errs = append(errs, fmt.Errorf("route %s is not same address family IPv4/IPv6", r.Dst))
			}
		}
		if routeNet != nil && defaultGateway {
			if isDefaultRoute(routeNet) {
				errs = append(errs, fmt.Errorf("default route %s is not allowed if 'defaultGateway' is true", r.Dst))
			}
		}
	}
	return kerrors.NewAggregate(errs)
}

func validateIPRangeNotShrinking(newSubnet, oldSubnet string) error {
	oldIP, oldCIDR, err := net.ParseCIDR(oldSubnet)
	if err != nil {
		return err
	}

	_, newCIDR, err := net.ParseCIDR(newSubnet)
	if err != nil {
		return err
	}

	oldMaskSize, _ := oldCIDR.Mask.Size()
	newMaskSize, _ := newCIDR.Mask.Size()

	if !newCIDR.Contains(oldIP) || oldMaskSize < newMaskSize {
		return errors.New("you cannot shrink the ip subnet")
	}

	return nil
}

func isDefaultRoute(ipNet *net.IPNet) bool {
	// Check if it's IPv4 and matches 0.0.0.0/0
	if ipNet.IP.To4() != nil && ipNet.String() == ipv4DefaultRoute {
		return true
	}

	// Check if it's IPv6 and matches ::/0
	if ipNet.IP.To4() == nil && ipNet.String() == ipv6DefaultRoute {
		return true
	}

	return false
}
