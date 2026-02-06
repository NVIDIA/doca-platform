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
	"math"
	"regexp"
	"sort"
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
// the required pattern, VF range start is less than or equal to end, and there are no
// overlapping VF indexes for the same PF across all resources.
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
	errs = append(errs, validateVFRanges(basePath, resources)...)
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
		if res.Type != noderesourcesv1.DevicePluginResourceTypeVF {
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

// validateVFRanges checks that for each VFRange start is less than or equal to end, and
// that there are no overlapping VF indexes for the same PF across all resources.
func validateVFRanges(basePath *field.Path, resources []noderesourcesv1.DevicePluginResource) field.ErrorList {
	errs := field.ErrorList{}
	type rangeInfo struct {
		resourceName string
		start        int32
		end          int32
		path         *field.Path
	}
	pfRanges := make(map[int32][]rangeInfo)

	for i, res := range resources {
		resPath := basePath.Index(i)
		if len(res.Ranges) == 0 {
			errs = append(errs, field.Required(resPath.Child("ranges"),
				fmt.Sprintf("no VF ranges specified for resource %q", res.Name)))
			continue
		}
		for j, r := range res.Ranges {
			rangePath := resPath.Child("ranges").Index(j)
			if r.Start != nil && r.End != nil && *r.Start > *r.End {
				errs = append(errs, field.Invalid(
					rangePath,
					fmt.Sprintf("%d-%d", *r.Start, *r.End),
					fmt.Sprintf("invalid VF range in resource %q: start (%d) is greater than end (%d)",
						res.Name, *r.Start, *r.End),
				))
				continue
			}

			// Use effective start and end values.
			// If start is nil, treat as 0; if end is nil, treat as MaxInt for comparison purposes.
			start := int32(0)
			if r.Start != nil {
				start = *r.Start
			}
			end := int32(math.MaxInt32)
			if r.End != nil {
				end = *r.End
			}
			pfRanges[r.PFIndex] = append(pfRanges[r.PFIndex], rangeInfo{
				resourceName: res.Name,
				start:        start,
				end:          end,
				path:         rangePath,
			})
		}
	}

	// Check for overlaps within each PF.
	for pfIndex, ranges := range pfRanges {
		if len(ranges) < 2 {
			continue
		}

		// Sort ranges by start index.
		sort.Slice(ranges, func(i, j int) bool {
			if ranges[i].start == ranges[j].start {
				return ranges[i].end < ranges[j].end
			}
			return ranges[i].start < ranges[j].start
		})

		// Check for overlaps between consecutive ranges.
		for i := 0; i < len(ranges)-1; i++ {
			current := ranges[i]
			next := ranges[i+1]
			// Ranges overlap if current.end >= next.start.
			if current.end >= next.start {
				msg := fmt.Sprintf(
					"overlapping VF ranges for PF %d: resource %q (range %d-%d) overlaps with "+
						"resource %q (range %d-%d)",
					pfIndex, current.resourceName, current.start, current.end,
					next.resourceName, next.start, next.end)
				errs = append(errs, field.Invalid(current.path, fmt.Sprintf("%d-%d", current.start, current.end), msg))
				errs = append(errs, field.Invalid(next.path, fmt.Sprintf("%d-%d", next.start, next.end), msg))
			}
		}
	}

	return errs
}
