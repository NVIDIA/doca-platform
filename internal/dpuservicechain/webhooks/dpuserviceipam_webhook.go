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
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/internal/dpuservicechain/utils/iputils"

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

	if newIpamObj.Spec.IPV4Network != nil { //nolint:dupl
		errs = append(errs, validateDPUServiceIPAMIPV4Network(newIpamObj.Spec.IPV4Network))
		if oldIpamObj != nil && oldIpamObj.Spec.IPV4Network != nil {
			errs = append(errs, validateIPRangeNotShrinking(newIpamObj.Spec.IPV4Network.Network, oldIpamObj.Spec.IPV4Network.Network))
			if newIpamObj.Spec.IPV4Network.PrefixSize != oldIpamObj.Spec.IPV4Network.PrefixSize {
				errs = append(errs, errors.New("prefixSize is immutable"))
			}
			if (oldIpamObj.Spec.IPV4Network.SubnetsPerDPUCluster == nil) != (newIpamObj.Spec.IPV4Network.SubnetsPerDPUCluster == nil) {
				errs = append(errs, errors.New("subnetsPerDPUCluster cannot be toggled between set and unset"))
			}
			if oldIpamObj.Spec.IPV4Network.SubnetsPerDPUCluster != nil && newIpamObj.Spec.IPV4Network.SubnetsPerDPUCluster != nil &&
				*newIpamObj.Spec.IPV4Network.SubnetsPerDPUCluster < *oldIpamObj.Spec.IPV4Network.SubnetsPerDPUCluster {
				errs = append(errs, errors.New("subnetsPerDPUCluster cannot be decreased"))
			}
		}
	}

	if newIpamObj.Spec.IPV4Subnet != nil { //nolint:dupl
		errs = append(errs, validateDPUServiceIPAMIPV4Subnet(newIpamObj.Spec.IPV4Subnet))
		if oldIpamObj != nil && oldIpamObj.Spec.IPV4Subnet != nil {
			errs = append(errs, validateIPRangeNotShrinking(newIpamObj.Spec.IPV4Subnet.Subnet, oldIpamObj.Spec.IPV4Subnet.Subnet))
			if newIpamObj.Spec.IPV4Subnet.PerNodeIPCount != oldIpamObj.Spec.IPV4Subnet.PerNodeIPCount {
				errs = append(errs, errors.New("perNodeIPCount is immutable"))
			}
			if (oldIpamObj.Spec.IPV4Subnet.BlocksPerDPUCluster == nil) != (newIpamObj.Spec.IPV4Subnet.BlocksPerDPUCluster == nil) {
				errs = append(errs, errors.New("blocksPerDPUCluster cannot be toggled between set and unset"))
			}
			if oldIpamObj.Spec.IPV4Subnet.BlocksPerDPUCluster != nil && newIpamObj.Spec.IPV4Subnet.BlocksPerDPUCluster != nil &&
				*newIpamObj.Spec.IPV4Subnet.BlocksPerDPUCluster < *oldIpamObj.Spec.IPV4Subnet.BlocksPerDPUCluster {
				errs = append(errs, errors.New("blocksPerDPUCluster cannot be decreased"))
			}
		}
	}

	return kerrors.NewAggregate(errs)
}

// validateDPUServiceIPAMIPV4Network validates the .spec.IPV4Network of a DPUServiceIPAM object
func validateDPUServiceIPAMIPV4Network(ipv4Network *dpuservicev1.Network) error {
	_, network, err := net.ParseCIDR(ipv4Network.Network)
	if err != nil {
		return fmt.Errorf("network %s is not a valid network", ipv4Network.Network)
	}

	networkPrefix, _ := network.Mask.Size()
	if int(ipv4Network.PrefixSize) < 1 || int(ipv4Network.PrefixSize) > 32 {
		return fmt.Errorf("prefixSize %d is invalid, must be between 1 and 32", ipv4Network.PrefixSize)
	}
	if networkPrefix > int(ipv4Network.PrefixSize) {
		return fmt.Errorf("prefixSize %d doesn't fit in network prefix %d", ipv4Network.PrefixSize, networkPrefix)
	}

	var errs []error

	//nolint:staticcheck // SA1019: Exclusions is deprecated but still supported
	errs = append(errs, validateExclusions(ipv4Network.Exclusions, network)...)
	errs = append(errs, validateExcludeRanges(ipv4Network.ExcludeRanges, network)...)

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
	if ipv4Network.GatewayIndex != nil {
		blockSize := int(iputils.PrefixSize(int(ipv4Network.PrefixSize)))
		if int(*ipv4Network.GatewayIndex) >= blockSize {
			errs = append(errs, fmt.Errorf("gatewayIndex %d is out of range for /%d prefix (valid range: 0–%d)", *ipv4Network.GatewayIndex, ipv4Network.PrefixSize, blockSize-1))
		}
	}
	if ipv4Network.SubnetsPerDPUCluster != nil {
		if *ipv4Network.SubnetsPerDPUCluster < 1 {
			errs = append(errs, errors.New("subnetsPerDPUCluster must be at least 1 when set"))
		} else {
			totalSubnets := 1 << uint(int(ipv4Network.PrefixSize)-networkPrefix)
			if int(*ipv4Network.SubnetsPerDPUCluster) > totalSubnets {
				errs = append(errs, fmt.Errorf("subnetsPerDPUCluster %d exceeds the %d available subnets in network %s", *ipv4Network.SubnetsPerDPUCluster, totalSubnets, ipv4Network.Network))
			}
		}
	}
	errs = append(errs, validateRoutes(ipv4Network.Routes, network, ipv4Network.DefaultGateway))
	return kerrors.NewAggregate(errs)
}

// validateExclusions validates if the exclusions are valid and part of network. returns error for each invalid exclusion..
func validateExclusions(exclusions []string, network *net.IPNet) []error {
	var errs []error
	for _, exclusion := range exclusions {
		_, err := validateIP(exclusion, network)
		if err != nil {
			errs = append(errs, err)
		}
	}

	return errs
}

// validateExcludeRanges validates if the exclude ranges are valid and part of network. returns error for each invalid exclude range.
func validateExcludeRanges(excludeRanges []dpuservicev1.IPRange, network *net.IPNet) []error {
	var errs []error
	for _, excludeRange := range excludeRanges {
		startIP, err := validateIP(excludeRange.StartIP, network)
		if err != nil {
			errs = append(errs, fmt.Errorf("exclude range startIP invalid. %w", err))
		}
		endIP, err := validateIP(excludeRange.EndIP, network)
		if err != nil {
			errs = append(errs, fmt.Errorf("exclude range endIP invalid. %w", err))
		}

		if startIP != nil && endIP != nil {
			// ips are represented as 16 bytes slices we can compare them to see if startIP is greater than endIP.
			if bytes.Compare(startIP, endIP) > 0 {
				errs = append(errs, fmt.Errorf("exclude range startIP %s is greater than endIP %s", excludeRange.StartIP, excludeRange.EndIP))
			}
		}
	}

	return errs
}

// validateIP validates if an IP is valid and part of network. returns the ip or error if occurred.
func validateIP(ip string, network *net.IPNet) (net.IP, error) {
	pip := net.ParseIP(ip)
	if pip == nil {
		return nil, fmt.Errorf("ip %s is not a valid IP", ip)
	}
	if !network.Contains(pip) {
		return nil, fmt.Errorf("ip %s is not part of network %s", ip, network.String())
	}
	return pip, nil
}

// validateDPUServiceIPAMIPV4Subnet validates the .spec.IPV4Subnet of a DPUServiceIPAM object
func validateDPUServiceIPAMIPV4Subnet(ipv4Subnet *dpuservicev1.Subnet) error {
	_, network, err := net.ParseCIDR(ipv4Subnet.Subnet)
	if err != nil {
		return fmt.Errorf("subnet %s is not a valid network", ipv4Subnet.Subnet)
	}

	prefixLen, _ := network.Mask.Size()
	if prefixLen >= 31 {
		return fmt.Errorf("subnet %s must be larger than /30 — /31 and /32 are not supported", ipv4Subnet.Subnet)
	}

	if excludeRangesErrs := validateExcludeRanges(ipv4Subnet.ExcludeRanges, network); len(excludeRangesErrs) > 0 {
		return kerrors.NewAggregate(excludeRangesErrs)
	}

	ip := net.ParseIP(ipv4Subnet.Gateway)
	if ip == nil {
		return fmt.Errorf("gateway %s is not a valid IP", ipv4Subnet.Gateway)
	}

	if !network.Contains(ip) {
		return fmt.Errorf("gateway %s is not part of subnet %s", ipv4Subnet.Gateway, ipv4Subnet.Subnet)
	}

	if ipv4Subnet.PerNodeIPCount < 1 {
		return errors.New("perNodeIPCount must be at least 1")
	}

	// -2 because network and broadcast addresses are not allocatable (see /31 and /32 rejection above)
	effectiveSize := int(iputils.PrefixSize(prefixLen)) - 2
	if ipv4Subnet.PerNodeIPCount > effectiveSize {
		return fmt.Errorf("perNodeIPCount %d exceeds the %d allocatable IPs in subnet %s", ipv4Subnet.PerNodeIPCount, effectiveSize, ipv4Subnet.Subnet)
	}

	if ipv4Subnet.BlocksPerDPUCluster != nil {
		if *ipv4Subnet.BlocksPerDPUCluster < 1 {
			return errors.New("blocksPerDPUCluster must be at least 1 when set")
		}
		totalBlocks := effectiveSize / ipv4Subnet.PerNodeIPCount
		if int(*ipv4Subnet.BlocksPerDPUCluster) > totalBlocks {
			return fmt.Errorf("blocksPerDPUCluster %d exceeds the %d available blocks in subnet %s", *ipv4Subnet.BlocksPerDPUCluster, totalBlocks, ipv4Subnet.Subnet)
		}
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
