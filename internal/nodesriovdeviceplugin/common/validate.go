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

package common

import (
	"fmt"
	"maps"
	"math"
	"regexp"
	"slices"
	"strings"

	noderesourcesv1 "github.com/nvidia/doca-platform/api/noderesources/v1alpha1"

	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// resourceNameRegex is the pattern for valid resource names.
// Allows alphanumeric characters, underscores, and hyphens.
var resourceNameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// ValidateNodeSRIOVDevicePluginConfig validates a NodeSRIOVDevicePluginConfig.
func ValidateNodeSRIOVDevicePluginConfig(defaultResourcePrefix string, cfg *noderesourcesv1.NodeSRIOVDevicePluginConfig) field.ErrorList {
	return ValidateDevicePluginResources(field.NewPath("spec").Child("devicePluginResources"),
		defaultResourcePrefix, cfg.Spec.DevicePluginResources)
}

// ValidateDevicePluginResources validates a list of DevicePluginResource.
// It checks that resource Name + Prefix combinations are unique, resource names match
// the required pattern, range start is less than or equal to end, and there are no
// overlapping function indexes for the same type and PF across all resources.
//
// Validation does not stop at the first error: all detected issues are returned
// as a field.ErrorList suitable for webhook validation responses.
func ValidateDevicePluginResources(basePath *field.Path, defaultResourcePrefix string, resources []noderesourcesv1.DevicePluginResource) field.ErrorList {
	if basePath == nil {
		basePath = field.NewPath("devicePluginResources")
	}
	errs := field.ErrorList{}
	if len(resources) == 0 {
		errs = append(errs, field.Required(basePath, "at least one resource must be provided"))
		return errs
	}
	errs = append(errs, validateResource(basePath, defaultResourcePrefix, resources)...)
	errs = append(errs, validateFunctionRanges(basePath, resources)...)
	return errs
}

// validateResource ensures each resource has a supported type, a valid name and prefix, and that no resource name/prefix combination is duplicated.
func validateResource(basePath *field.Path, defaultResourcePrefix string, resources []noderesourcesv1.DevicePluginResource) field.ErrorList {
	errs := field.ErrorList{}
	if dnsErrs := validation.IsDNS1123Subdomain(defaultResourcePrefix); len(dnsErrs) > 0 {
		errs = append(errs, field.Invalid(
			basePath,
			defaultResourcePrefix,
			fmt.Sprintf("invalid default resource prefix %q: %s", defaultResourcePrefix, strings.Join(dnsErrs, ", ")),
		))
	}

	seen := make(map[string]int)
	for i, res := range resources {
		resPath := basePath.Index(i)
		if !res.Type.IsSupported() {
			errs = append(errs, field.Invalid(
				resPath.Child("type"),
				res.Type,
				fmt.Sprintf("unsupported resource type %s for resource %s", res.Type, res.Name),
			))
		}
		if !resourceNameRegex.MatchString(res.Name) {
			errs = append(errs, field.Invalid(
				resPath.Child("name"),
				res.Name,
				fmt.Sprintf("invalid resource name %q: must match pattern %s", res.Name, resourceNameRegex.String()),
			))
		}

		prefix := defaultResourcePrefix
		if res.ResourcePrefix != nil && *res.ResourcePrefix != "" {
			prefix = *res.ResourcePrefix
			if dnsErrs := validation.IsDNS1123Subdomain(prefix); len(dnsErrs) > 0 {
				errs = append(errs, field.Invalid(
					resPath.Child("resourcePrefix"),
					prefix,
					fmt.Sprintf("invalid resource prefix %q: %s", prefix, strings.Join(dnsErrs, ", ")),
				))
			}
		}

		key := prefix + "/" + res.Name
		if firstIndex, exists := seen[key]; exists {
			errs = append(errs, field.Duplicate(
				resPath.Child("name"),
				fmt.Sprintf("duplicate resource: name %q with prefix %q appears multiple times (also at index %d)",
					res.Name, prefix, firstIndex),
			))
			continue
		}
		seen[key] = i
	}
	return errs
}

// ValidateCrossDPUResourceTypes checks that the same prefix/name is not used as
// both vf and sf across DPUs. The device plugin has one deviceType per extended
// resource, so a type conflict cannot be represented in the generated config.
func ValidateCrossDPUResourceTypes(defaultResourcePrefix string, input NodeInputConfig) field.ErrorList {
	type seenInfo struct {
		typ    noderesourcesv1.DevicePluginResourceType
		serial string
	}
	seen := make(map[string]seenInfo)
	errs := field.ErrorList{}
	for _, serial := range slices.Sorted(maps.Keys(input)) {
		for i, res := range input[serial] {
			prefix := defaultResourcePrefix
			if res.ResourcePrefix != nil && *res.ResourcePrefix != "" {
				prefix = *res.ResourcePrefix
			}
			key := prefix + "/" + res.Name
			prev, exists := seen[key]
			if !exists {
				seen[key] = seenInfo{typ: res.Type, serial: serial}
				continue
			}
			if prev.typ == res.Type {
				continue
			}
			errs = append(errs, field.Invalid(
				field.NewPath(serial).Index(i).Child("type"),
				res.Type,
				fmt.Sprintf("resource %q has type %q on DPU %s, but DPU %s already defined it as %q",
					key, res.Type, serial, prev.serial, prev.typ),
			))
		}
	}
	return errs
}

// validateFunctionRanges checks that for each range start is less than or equal to end,
// and that there are no overlapping indexes for the same resource type and PF.
// VF and SF ranges on the same PF are independent and do not overlap with each other.
func validateFunctionRanges(basePath *field.Path, resources []noderesourcesv1.DevicePluginResource) field.ErrorList {
	errs := field.ErrorList{}
	type rangeInfo struct {
		resourceName string
		start        int32
		end          int32
		path         *field.Path
	}
	type rangeKey struct {
		typ     noderesourcesv1.DevicePluginResourceType
		pfIndex int32
	}
	grouped := make(map[rangeKey][]rangeInfo)

	for i, res := range resources {
		resPath := basePath.Index(i)
		if len(res.Ranges) == 0 {
			errs = append(errs, field.Required(resPath.Child("ranges"),
				fmt.Sprintf("no ranges specified for resource %q", res.Name)))
			continue
		}
		for j, r := range res.Ranges {
			rangePath := resPath.Child("ranges").Index(j)
			if res.Type == noderesourcesv1.DevicePluginResourceTypeSF {
				if r.Start == nil {
					errs = append(errs, field.Required(rangePath.Child("start"),
						fmt.Sprintf("start must be set for sf resource %q", res.Name)))
				}
				if r.End == nil {
					errs = append(errs, field.Required(rangePath.Child("end"),
						fmt.Sprintf("end must be set for sf resource %q", res.Name)))
				}
				if r.Start == nil || r.End == nil {
					continue
				}
			}
			if r.Start != nil && r.End != nil && *r.Start > *r.End {
				errs = append(errs, field.Invalid(
					rangePath,
					fmt.Sprintf("%d-%d", *r.Start, *r.End),
					fmt.Sprintf("invalid range in resource %q: start (%d) is greater than end (%d)",
						res.Name, *r.Start, *r.End),
				))
				continue
			}

			// Use effective start and end values.
			// If start is nil, treat as 0; if end is nil, treat as MaxInt for comparison purposes.
			// SF ranges always have both bounds after the checks above.
			start := int32(0)
			if r.Start != nil {
				start = *r.Start
			}
			end := int32(math.MaxInt32)
			if r.End != nil {
				end = *r.End
			}
			key := rangeKey{typ: res.Type, pfIndex: r.PFIndex}
			grouped[key] = append(grouped[key], rangeInfo{
				resourceName: res.Name,
				start:        start,
				end:          end,
				path:         rangePath,
			})
		}
	}

	// Check for overlaps within each type+PF combination.
	for key, ranges := range grouped {
		if len(ranges) < 2 {
			continue
		}

		slices.SortFunc(ranges, func(a, b rangeInfo) int {
			if a.start != b.start {
				return int(a.start - b.start)
			}
			return int(a.end - b.end)
		})

		for i := range len(ranges) - 1 {
			current := ranges[i]
			next := ranges[i+1]
			// Ranges overlap if current.end >= next.start.
			if current.end >= next.start {
				msg := fmt.Sprintf(
					"overlapping %s ranges for PF %d: resource %q (range %d-%d) overlaps with "+
						"resource %q (range %d-%d)",
					key.typ, key.pfIndex, current.resourceName, current.start, current.end,
					next.resourceName, next.start, next.end)
				errs = append(errs, field.Invalid(current.path, fmt.Sprintf("%d-%d", current.start, current.end), msg))
				errs = append(errs, field.Invalid(next.path, fmt.Sprintf("%d-%d", next.start, next.end), msg))
			}
		}
	}

	return errs
}
