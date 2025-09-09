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

package dpuselector

import (
	"context"
	"fmt"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// Options holds configuration for DPU selection.
type Options struct {
	// labelSelector filters DPUs by labels
	labelSelector labels.Selector
	// indexerField specifies indexed field for efficient DPU lookups
	indexerField *string
	// namespace restricts search to specific namespace
	namespace *string
}

// MarshalLog emits a struct containing key/value pairs for logging.
func (o Options) MarshalLog() any {
	var labelSelector, indexerField, namespace string
	if o.labelSelector != nil {
		labelSelector = o.labelSelector.String()
	}
	if o.indexerField != nil {
		indexerField = *o.indexerField
	}
	if o.namespace != nil {
		namespace = *o.namespace
	}
	return struct {
		LabelSelector string `json:"labelSelector,omitempty"`
		IndexerField  string `json:"indexerField,omitempty"`
		Namespace     string `json:"namespace,omitempty"`
	}{
		LabelSelector: labelSelector,
		IndexerField:  indexerField,
		Namespace:     namespace,
	}
}

// Option configures DPU selection behavior.
type Option interface {
	// Apply modifies the Options struct.
	Apply(options *Options)
}

// WithLabelSelector filters DPUs by label selector.
type WithLabelSelector struct {
	Selector labels.Selector
}

// Apply sets the label selector.
func (o WithLabelSelector) Apply(options *Options) {
	options.labelSelector = o.Selector
}

// WithIndexerField uses indexed field for efficient DPU lookups.
type WithIndexerField struct {
	FieldName string
}

// Apply sets the indexer field name.
func (o WithIndexerField) Apply(options *Options) {
	options.indexerField = &o.FieldName
}

// WithInNamespace restricts DPU selection to specific namespace.
type WithInNamespace struct {
	Namespace string
}

// Apply sets the namespace restriction.
func (o WithInNamespace) Apply(options *Options) {
	options.namespace = &o.Namespace
}

// DPUSelector selects DPUs for nodes.
type DPUSelector interface {
	// GetDPUForNode finds single DPU for a DPUNode. If multiple DPUs are found, an error is returned.
	GetDPUForNode(ctx context.Context, c client.Client, dpuNode *provisioningv1.DPUNode) (*provisioningv1.DPU, error)
}

// New creates a DPUSelector with the given options.
func New(opts ...Option) DPUSelector {
	return &dpuSelector{
		options: makeOptions(opts...),
	}
}

// makeOptions creates Options from provided options.
func makeOptions(opts ...Option) *Options {
	options := &Options{}
	for _, opt := range opts {
		opt.Apply(options)
	}
	return options
}

// dpuSelector implements DPUSelector interface.
type dpuSelector struct {
	options *Options
}

// GetDPUForNode finds single DPU for a DPUNode. If multiple DPUs are found, an error is returned.
func (s *dpuSelector) GetDPUForNode(ctx context.Context, c client.Client, dpuNode *provisioningv1.DPUNode) (*provisioningv1.DPU, error) {
	reqLog := ctrllog.FromContext(ctx).WithValues("dpuNode", dpuNode.Name)
	reqLog.Info("Finding DPU for DPUNode", "options", s.options)

	// Get list of DPUs for the node
	dpus, err := s.listDPUsForNode(ctx, c, dpuNode)
	if err != nil {
		return nil, err
	}
	if len(dpus) == 0 {
		err := fmt.Errorf("no DPU found for DPUNode %s", dpuNode.Name)
		reqLog.Error(err, "No DPU found for DPUNode")
		return nil, err
	}
	if len(dpus) > 1 {
		err := fmt.Errorf("%d DPUs found for DPUNode %s", len(dpus), dpuNode.Name)
		reqLog.Error(err, "Multiple DPUs found for DPUNode")
		return nil, err
	}
	reqLog.Info("Found DPU for DPUNode", "dpu", dpus[0].Name)
	return dpus[0], nil
}

// listDPUsForNode retrieves DPUs using configured options.
func (s *dpuSelector) listDPUsForNode(ctx context.Context, c client.Client, dpuNode *provisioningv1.DPUNode) ([]*provisioningv1.DPU, error) {
	listOpts := []client.ListOption{}
	if s.options.namespace != nil {
		listOpts = append(listOpts, client.InNamespace(*s.options.namespace))
	}
	if s.options.indexerField != nil {
		listOpts = append(listOpts, client.MatchingFields{*s.options.indexerField: dpuNode.Name})
	}
	if s.options.labelSelector != nil {
		listOpts = append(listOpts, client.MatchingLabelsSelector{Selector: s.options.labelSelector})
	}

	dpuList := &provisioningv1.DPUList{}
	if err := c.List(ctx, dpuList, listOpts...); err != nil {
		return nil, fmt.Errorf("failed to list DPUs: %w", err)
	}
	if s.options.indexerField != nil {
		dpus := make([]*provisioningv1.DPU, len(dpuList.Items))
		for i := range dpuList.Items {
			dpus[i] = &dpuList.Items[i]
		}
		return dpus, nil
	}
	var dpus []*provisioningv1.DPU
	for i := range dpuList.Items {
		dpu := &dpuList.Items[i]
		if dpu.Spec.DPUNodeName == dpuNode.Name {
			dpus = append(dpus, dpu)
		}
	}
	return dpus, nil
}
