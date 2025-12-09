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

package utils

import (
	"context"
	"fmt"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/dpucluster"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ResourcesExceedError is an error returned by GetAllocatableResources() that indicates that the reserved resources
// exceed the total resources
type ResourcesExceedError struct {
	// AdditionalResourcesRequired are the extra resources needed so that the they can fit the total resources
	AdditionalResourcesRequired []string
	msg                         string
}

// Error is the error message of that error struct
func (e *ResourcesExceedError) Error() string { return e.msg }

// GetAllocatableResources returns the available resources after subtracting the reserved ones from the total ones
func GetAllocatableResources(total corev1.ResourceList, reserved corev1.ResourceList) (corev1.ResourceList, error) {
	if total == nil {
		return nil, nil
	}
	availableResources := make(corev1.ResourceList)
	for k, v := range total {
		availableResources[k] = v
	}

	for resourceName, quantity := range reserved {
		totalResource := resource.Quantity{}
		if resource, ok := availableResources[resourceName]; ok {
			totalResource = resource
		}

		totalResource.Sub(quantity)
		availableResources[resourceName] = totalResource
	}

	additionalResourcesRequired := []string{}
	for resourceName, quantity := range availableResources {
		if quantity.Sign() < 0 {
			quantity.Neg()
			additionalResourcesRequired = append(additionalResourcesRequired, fmt.Sprintf("%s: %s", resourceName, quantity.String()))
		}
	}

	if len(additionalResourcesRequired) > 0 {
		return nil, &ResourcesExceedError{
			AdditionalResourcesRequired: additionalResourcesRequired,
			msg:                         "error while calculating allocatable resources, reserved resources don't fit in total resources",
		}
	}

	return availableResources, nil
}

// LabelSelectorAsSelector is a wrapper around metav1.LabelSelectorAsSelector()
// to not select labels.Nothing() when the input is nil.
// If the input is nil, it returns labels.Everything().
func LabelSelectorAsSelector(labelSelector *metav1.LabelSelector) (labels.Selector, error) {
	if labelSelector == nil {
		return labels.Everything(), nil
	}

	return metav1.LabelSelectorAsSelector(labelSelector)
}

// GetDPFOperatorConfig returns the DPFOperatorConfig object. It returns an error if there is more
// than one DPFOperatorConfig object or if there is no DPFOperatorConfig object.
func GetDPFOperatorConfig(ctx context.Context, c client.Client) (*operatorv1.DPFOperatorConfig, error) {
	dpfOperatorConfigList := operatorv1.DPFOperatorConfigList{}
	if err := c.List(ctx, &dpfOperatorConfigList); err != nil {
		return nil, fmt.Errorf("listing DPFOperatorConfigs: %w", err)
	}
	if len(dpfOperatorConfigList.Items) == 0 || len(dpfOperatorConfigList.Items) > 1 {
		return nil, fmt.Errorf("exactly one DPFOperatorConfig must exist")
	}
	return &dpfOperatorConfigList.Items[0], nil
}

// GetMatchingDPUClusters returns a list of DPUCluster Configs from the given list that match the given selector.
// In case no selector is provided, it returns the entire list.
func GetMatchingDPUClusters(dpuClusters []*dpucluster.Config, clusterSelector *metav1.LabelSelector) ([]*dpucluster.Config, error) {
	if clusterSelector == nil {
		return dpuClusters, nil
	}

	matchingClusters := make([]*dpucluster.Config, 0)
	selector, err := LabelSelectorAsSelector(clusterSelector)
	if err != nil {
		return matchingClusters, fmt.Errorf("failed to parse label selector: %w", err)
	}
	for i, dpuCluster := range dpuClusters {
		if !selector.Matches(labels.Set(dpuCluster.Cluster.Labels)) {
			continue
		}
		matchingClusters = append(matchingClusters, dpuClusters[i])
	}
	return matchingClusters, nil
}

// EnsureNamespace ensures the namespace exists in the cluster by creating it if it does not exist.
func EnsureNamespace(ctx context.Context, c client.Client, namespace string) error {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}
	if err := c.Create(ctx, ns); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return err
		}
	}
	return nil
}
