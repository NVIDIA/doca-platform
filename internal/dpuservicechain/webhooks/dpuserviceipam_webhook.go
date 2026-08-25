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
	"math/big"
	"net"
	"net/netip"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/internal/dpuservicechain/utils/iputils"

	corev1 "k8s.io/api/core/v1"
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

type configurationField uint8

const (
	configurationFieldUnknown configurationField = iota
	configurationFieldIPv4Network
	configurationFieldIPv4Subnet
	configurationFieldNetwork
	configurationFieldSubnet
)

type allocationMode uint8

const (
	allocationModeUnknown allocationMode = iota
	allocationModeNetwork
	allocationModeSubnet
)

// updateValidationInput contains the immutable fields shared by both API representations of an allocation mode.
type updateValidationInput struct {
	cidr                string
	allocationSize      int64
	allocationField     string
	perCluster          *int32
	perClusterFieldName string
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

// validateDPUServiceIPAM validates the selected allocation mode and the fields that are immutable on update.
//
//nolint:staticcheck // SA1019: Deprecated IPv4 fields remain supported for backward compatibility.
func validateDPUServiceIPAM(newIPAM, oldIPAM *dpuservicev1.DPUServiceIPAM) error {
	var errs []error

	// TODO: Drop this once multi namespace NVIPAM is supported
	if newIPAM.Namespace != "dpf-operator-system" {
		errs = append(errs, errors.New("currently only 'dpf-operator-system' namespace is supported"))
	}

	// Preserve the validation errors returned for deprecated-only objects. The CRD CEL rule validates combinations
	// involving the replacement fields before the webhook runs.
	if newIPAM.Spec.Network == nil && newIPAM.Spec.Subnet == nil {
		if newIPAM.Spec.IPV4Network == nil && newIPAM.Spec.IPV4Subnet == nil {
			errs = append(errs, errors.New("either ipv4Subnet or ipv4Network must be specified"))
		}
		if newIPAM.Spec.IPV4Network != nil && newIPAM.Spec.IPV4Subnet != nil {
			errs = append(errs, errors.New("either ipv4Subnet or ipv4Network must be specified but not both"))
		}
	}

	newField := selectConfiguration(&newIPAM.Spec)

	oldField := configurationFieldUnknown
	if oldIPAM != nil {
		if selected := selectConfiguration(&oldIPAM.Spec); selected != configurationFieldUnknown {
			oldField = selected
			oldMode, newMode := configurationMode(oldField), configurationMode(newField)
			oldFamily, newFamily := configurationFamily(oldField, &oldIPAM.Spec), configurationFamily(newField, &newIPAM.Spec)
			if oldMode != newMode {
				errs = append(errs, errors.New("transitioning from ipv4subnet to ipv4network and vice versa is currently not supported"))
			} else if oldFamily != "" && newFamily != "" && oldFamily != newFamily {
				errs = append(errs, errors.New("transitioning between address families is not supported; create a new DPUServiceIPAM instead"))
			}
		}
	}

	switch newField {
	case configurationFieldIPv4Network:
		errs = append(errs, validateNetwork(newIPAM.Spec.IPV4Network))
	case configurationFieldNetwork:
		errs = append(errs, validateNetwork(newIPAM.Spec.Network))
	case configurationFieldIPv4Subnet:
		errs = append(errs, validateSubnet(newIPAM.Spec.IPV4Subnet))
	case configurationFieldSubnet:
		errs = append(errs, validateSubnet(newIPAM.Spec.Subnet))
	}

	if oldIPAM != nil && oldField != configurationFieldUnknown && configurationMode(oldField) == configurationMode(newField) {
		oldFamily := configurationFamily(oldField, &oldIPAM.Spec)
		newFamily := configurationFamily(newField, &newIPAM.Spec)
		// An invalid CIDR has no family. Run the update checks in that case so the existing CIDR error is preserved.
		if oldFamily == "" || newFamily == "" || oldFamily == newFamily {
			errs = append(errs, validateConfigurationUpdate(oldField, &oldIPAM.Spec, newField, &newIPAM.Spec))
		}
	}

	return kerrors.NewAggregate(errs)
}

// selectConfiguration returns the configured allocation field. The CRD CEL rule ensures exactly one field is set.
//
//nolint:staticcheck // SA1019: Deprecated IPv4 fields remain supported for backward compatibility.
func selectConfiguration(spec *dpuservicev1.DPUServiceIPAMSpec) configurationField {
	if spec.IPV4Network != nil {
		return configurationFieldIPv4Network
	}
	if spec.IPV4Subnet != nil {
		return configurationFieldIPv4Subnet
	}
	if spec.Network != nil {
		return configurationFieldNetwork
	}
	if spec.Subnet != nil {
		return configurationFieldSubnet
	}
	return configurationFieldUnknown
}

func configurationMode(field configurationField) allocationMode {
	switch field {
	case configurationFieldIPv4Network, configurationFieldNetwork:
		return allocationModeNetwork
	case configurationFieldIPv4Subnet, configurationFieldSubnet:
		return allocationModeSubnet
	default:
		return allocationModeUnknown
	}
}

// configurationFamily derives the address family from the selected field's CIDR. Deprecated and replacement fields
// have identical address-family behavior.
//
//nolint:staticcheck // SA1019: Deprecated fields remain supported for update compatibility.
func configurationFamily(field configurationField, spec *dpuservicev1.DPUServiceIPAMSpec) corev1.IPFamily {
	switch field {
	case configurationFieldIPv4Network:
		return familyFromCIDR(spec.IPV4Network.Network)
	case configurationFieldIPv4Subnet:
		return familyFromCIDR(spec.IPV4Subnet.Subnet)
	case configurationFieldNetwork:
		return familyFromCIDR(spec.Network.Network)
	case configurationFieldSubnet:
		return familyFromCIDR(spec.Subnet.Subnet)
	default:
		return ""
	}
}

// familyFromCIDR returns the address family of a valid, non-mapped CIDR.
func familyFromCIDR(cidr string) corev1.IPFamily {
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil || prefix.Addr().Is4In6() {
		return ""
	}
	if prefix.Addr().Is4() {
		return corev1.IPv4Protocol
	}
	return corev1.IPv6Protocol
}

// updateValidationInputFor maps fields that have the same update rules in the deprecated and new APIs.
//
//nolint:staticcheck // SA1019: Deprecated IPv4 fields remain supported for update validation.
func updateValidationInputFor(field configurationField, spec *dpuservicev1.DPUServiceIPAMSpec) (updateValidationInput, bool) {
	switch field {
	case configurationFieldIPv4Network:
		return updateValidationInput{
			cidr:                spec.IPV4Network.Network,
			allocationSize:      int64(spec.IPV4Network.PrefixSize),
			allocationField:     "prefixSize",
			perCluster:          spec.IPV4Network.SubnetsPerDPUCluster,
			perClusterFieldName: "subnetsPerDPUCluster",
		}, true
	case configurationFieldNetwork:
		return updateValidationInput{
			cidr:                spec.Network.Network,
			allocationSize:      int64(spec.Network.PrefixSize),
			allocationField:     "prefixSize",
			perCluster:          spec.Network.SubnetsPerDPUCluster,
			perClusterFieldName: "subnetsPerDPUCluster",
		}, true
	case configurationFieldIPv4Subnet:
		return updateValidationInput{
			cidr:                spec.IPV4Subnet.Subnet,
			allocationSize:      int64(spec.IPV4Subnet.PerNodeIPCount),
			allocationField:     "perNodeIPCount",
			perCluster:          spec.IPV4Subnet.BlocksPerDPUCluster,
			perClusterFieldName: "blocksPerDPUCluster",
		}, true
	case configurationFieldSubnet:
		return updateValidationInput{
			cidr:                spec.Subnet.Subnet,
			allocationSize:      int64(spec.Subnet.PerNodeIPCount),
			allocationField:     "perNodeIPCount",
			perCluster:          spec.Subnet.BlocksPerDPUCluster,
			perClusterFieldName: "blocksPerDPUCluster",
		}, true
	default:
		return updateValidationInput{}, false
	}
}

// validateConfigurationUpdate applies the existing update rules across both API representations.
func validateConfigurationUpdate(oldField configurationField, oldSpec *dpuservicev1.DPUServiceIPAMSpec, newField configurationField, newSpec *dpuservicev1.DPUServiceIPAMSpec) error {
	oldInput, oldOK := updateValidationInputFor(oldField, oldSpec)
	newInput, newOK := updateValidationInputFor(newField, newSpec)
	if !oldOK || !newOK {
		return nil
	}

	var errs []error
	errs = append(errs, validateIPRangeNotShrinking(newInput.cidr, oldInput.cidr))
	if newInput.allocationSize != oldInput.allocationSize {
		errs = append(errs, fmt.Errorf("%s is immutable", newInput.allocationField))
	}
	if (oldInput.perCluster == nil) != (newInput.perCluster == nil) {
		errs = append(errs, fmt.Errorf("%s cannot be toggled between set and unset", newInput.perClusterFieldName))
	} else if oldInput.perCluster != nil && *newInput.perCluster < *oldInput.perCluster {
		errs = append(errs, fmt.Errorf("%s cannot be decreased", newInput.perClusterFieldName))
	}
	return kerrors.NewAggregate(errs)
}

// parseCIDR keeps the existing CIDR parsing behavior while rejecting IPv4-mapped IPv6 prefixes, whose address family
// is ambiguous to the family-aware validation and allocation code.
func parseCIDR(value, field string) (*net.IPNet, error) {
	prefix, err := netip.ParsePrefix(value)
	if err != nil {
		return nil, fmt.Errorf("%s %s is not a valid network", field, value)
	}
	if prefix.Addr().Is4In6() {
		return nil, fmt.Errorf("%s %s uses an IPv4-mapped IPv6 address, which is not supported", field, value)
	}
	_, network, err := net.ParseCIDR(value)
	if err != nil {
		return nil, fmt.Errorf("%s %s is not a valid network", field, value)
	}
	return network, nil
}

// validateNetwork applies the existing network-mode validation to either API representation.
func validateNetwork(configuration *dpuservicev1.Network) error {
	network, err := parseCIDR(configuration.Network, "network")
	if err != nil {
		return err
	}

	networkPrefix, addressBits := network.Mask.Size()
	if int(configuration.PrefixSize) < 1 || int(configuration.PrefixSize) > addressBits {
		return fmt.Errorf("prefixSize %d is invalid, must be between 1 and %d", configuration.PrefixSize, addressBits)
	}
	if networkPrefix > int(configuration.PrefixSize) {
		return fmt.Errorf("prefixSize %d doesn't fit in network prefix %d", configuration.PrefixSize, networkPrefix)
	}

	var errs []error

	//nolint:staticcheck // SA1019: Exclusions remains supported for backward compatibility.
	errs = append(errs, validateExclusions(configuration.Exclusions, network)...)
	errs = append(errs, validateExcludeRanges(configuration.ExcludeRanges, network)...)

	for _, allocation := range configuration.Allocations {
		_, allocationNetwork, err := net.ParseCIDR(allocation)
		if err != nil {
			errs = append(errs, fmt.Errorf("allocation %s is not a valid subnet", allocation))
			continue
		}
		if allocationPrefix, err := netip.ParsePrefix(allocation); err == nil && allocationPrefix.Addr().Is4In6() {
			errs = append(errs, fmt.Errorf("allocation %s uses an IPv4-mapped IPv6 address, which is not supported", allocation))
			continue
		}

		allocationNetworkPrefix, allocationAddressBits := allocationNetwork.Mask.Size()
		if allocationAddressBits != addressBits || !network.Contains(allocationNetwork.IP) || allocationNetworkPrefix != int(configuration.PrefixSize) {
			errs = append(errs, fmt.Errorf("allocation %s is not part of the network %s", allocation, configuration.Network))
		}
	}
	if configuration.GatewayIndex != nil {
		if *configuration.GatewayIndex < 0 {
			errs = append(errs, errors.New("gatewayIndex must be at least 0"))
		} else {
			blockSize := iputils.PrefixAddressCount(addressBits, int(configuration.PrefixSize))
			if big.NewInt(int64(*configuration.GatewayIndex)).Cmp(blockSize) >= 0 {
				lastIndex := new(big.Int).Sub(blockSize, big.NewInt(1))
				errs = append(errs, fmt.Errorf("gatewayIndex %d is out of range for /%d prefix (valid range: 0–%s)", *configuration.GatewayIndex, configuration.PrefixSize, lastIndex))
			}
		}
	}
	if configuration.SubnetsPerDPUCluster != nil {
		if *configuration.SubnetsPerDPUCluster < 1 {
			errs = append(errs, errors.New("subnetsPerDPUCluster must be at least 1 when set"))
		} else {
			totalSubnets := iputils.PrefixAddressCount(int(configuration.PrefixSize), networkPrefix)
			if big.NewInt(int64(*configuration.SubnetsPerDPUCluster)).Cmp(totalSubnets) > 0 {
				errs = append(errs, fmt.Errorf("subnetsPerDPUCluster %d exceeds the %s available subnets in network %s", *configuration.SubnetsPerDPUCluster, totalSubnets, configuration.Network))
			}
		}
	}
	errs = append(errs, validateRoutes(configuration.Routes, network, configuration.DefaultGateway))
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
// IPv4-mapped IPv6 addresses are rejected for the same reason as in parseCIDR. net.ParseIP would normalize them,
// but the allocator rejects them, so accepting one here would admit an object that can never be reconciled.
func validateIP(ip string, network *net.IPNet) (net.IP, error) {
	pip := net.ParseIP(ip)
	if pip == nil {
		return nil, fmt.Errorf("ip %s is not a valid IP", ip)
	}
	if addr, err := netip.ParseAddr(ip); err == nil && addr.Is4In6() {
		return nil, fmt.Errorf("ip %s uses an IPv4-mapped IPv6 address, which is not supported", ip)
	}
	if !network.Contains(pip) {
		return nil, fmt.Errorf("ip %s is not part of network %s", ip, network.String())
	}
	return pip, nil
}

// validateSubnet applies the existing subnet-mode validation to either API representation.
func validateSubnet(configuration *dpuservicev1.Subnet) error {
	network, err := parseCIDR(configuration.Subnet, "subnet")
	if err != nil {
		return err
	}

	prefixLen, addressBits := network.Mask.Size()
	// Block IPv4 /31 and /32 and IPv6 /127 and /128. NV-IPAM reserves no addresses in these prefixes, while every
	// wider prefix reserves the subnet address and so starts its per-node blocks one address later. An update may
	// grow the subnet, and growing out of one of these prefixes would shift every block already assigned to a node.
	if prefixLen >= addressBits-1 {
		return fmt.Errorf("subnet %s must be larger than /%d — /%d and /%d are not supported",
			configuration.Subnet, addressBits-2, addressBits-1, addressBits)
	}

	if excludeRangesErrs := validateExcludeRanges(configuration.ExcludeRanges, network); len(excludeRangesErrs) > 0 {
		return kerrors.NewAggregate(excludeRangesErrs)
	}

	gateway := net.ParseIP(configuration.Gateway)
	if gateway == nil {
		return fmt.Errorf("gateway %s is not a valid IP", configuration.Gateway)
	}
	if addr, err := netip.ParseAddr(configuration.Gateway); err == nil && addr.Is4In6() {
		return fmt.Errorf("gateway %s uses an IPv4-mapped IPv6 address, which is not supported", configuration.Gateway)
	}
	if !network.Contains(gateway) {
		return fmt.Errorf("gateway %s is not part of subnet %s", configuration.Gateway, configuration.Subnet)
	}

	if configuration.PerNodeIPCount < 1 {
		return errors.New("perNodeIPCount must be at least 1")
	}

	effectiveSize := allocatableAddressCount(network)
	if big.NewInt(int64(configuration.PerNodeIPCount)).Cmp(effectiveSize) > 0 {
		return fmt.Errorf("perNodeIPCount %d exceeds the %s allocatable IPs in subnet %s", configuration.PerNodeIPCount, effectiveSize, configuration.Subnet)
	}

	if configuration.BlocksPerDPUCluster != nil {
		if *configuration.BlocksPerDPUCluster < 1 {
			return errors.New("blocksPerDPUCluster must be at least 1 when set")
		}
		totalBlocks := new(big.Int).Quo(effectiveSize, big.NewInt(int64(configuration.PerNodeIPCount)))
		if big.NewInt(int64(*configuration.BlocksPerDPUCluster)).Cmp(totalBlocks) > 0 {
			return fmt.Errorf("blocksPerDPUCluster %d exceeds the %s available blocks in subnet %s", *configuration.BlocksPerDPUCluster, totalBlocks, configuration.Subnet)
		}
	}

	err = validateRoutes(configuration.Routes, network, configuration.DefaultGateway)
	if err != nil {
		return err
	}

	return nil
}

// allocatableAddressCount preserves the existing IPv4 calculation and adds the equivalent IPv6 rules. Ordinary IPv6
// prefixes reserve the subnet address but do not have a broadcast address. Point-to-point and single-address prefixes
// keep their full size, which validateSubnet rejects before calling this function.
func allocatableAddressCount(network *net.IPNet) *big.Int {
	prefixLen, addressBits := network.Mask.Size()
	count := iputils.PrefixAddressCount(addressBits, prefixLen)
	if prefixLen < addressBits-1 {
		count.Sub(count, big.NewInt(1))
		if network.IP.To4() != nil {
			count.Sub(count, big.NewInt(1))
		}
	}
	return count
}

// validateRoutes validate routes:
// - dst is a valid CIDR
func validateRoutes(routes []dpuservicev1.Route, network *net.IPNet, defaultGateway bool) error {
	var errs []error
	for _, r := range routes {
		_, routeNet, err := net.ParseCIDR(r.Dst)
		if err != nil {
			errs = append(errs, fmt.Errorf("route %s is not a valid subnet", r.Dst))
			continue
		}
		if routePrefix, err := netip.ParsePrefix(r.Dst); err == nil && routePrefix.Addr().Is4In6() {
			errs = append(errs, fmt.Errorf("route %s uses an IPv4-mapped IPv6 address, which is not supported", r.Dst))
			continue
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
