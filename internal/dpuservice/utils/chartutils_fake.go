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

package utils

import (
	"context"
	"fmt"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/internal/digest"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

type FakeChartHelper struct {
	annotations map[string]map[string]string
	shouldError map[string]interface{}
}

var _ ChartHelper = &FakeChartHelper{}

// NewFakeChartHelper returns a fake chart helper used in tests
func NewFakeChartHelper() *FakeChartHelper {
	return &FakeChartHelper{
		annotations: make(map[string]map[string]string),
		shouldError: make(map[string]interface{}),
	}
}

// GetAnnotationsFromChart returns the annotations found in the specified chart by pulling the chart locally
func (f *FakeChartHelper) GetAnnotationsFromChart(ctx context.Context, c client.Client, source dpuservicev1.ApplicationSource) (map[string]string, error) {
	hash := digest.FromObjects(source)
	if _, ok := f.shouldError[hash.String()]; ok {
		return nil, fmt.Errorf("error for %v", source)
	}
	if annotations, ok := f.annotations[hash.String()]; ok {
		return annotations, nil
	}
	return nil, nil
}

// SetAnnotationsForChart sets annotations for a chart
func (f *FakeChartHelper) SetAnnotationsForChart(source dpuservicev1.ApplicationSource, annotations map[string]string) {
	hash := digest.FromObjects(source)
	f.annotations[hash.String()] = annotations
}

// ReturnErrorForChart returns error when GetAnnotationsFromChart is called for a given chart
func (f *FakeChartHelper) ReturnErrorForChart(source dpuservicev1.ApplicationSource) {
	hash := digest.FromObjects(source)
	f.shouldError[hash.String()] = struct{}{}
}

// Reset resets the FakeChartHelper
func (f *FakeChartHelper) Reset() {
	f.annotations = make(map[string]map[string]string)
	f.shouldError = make(map[string]interface{})
}
