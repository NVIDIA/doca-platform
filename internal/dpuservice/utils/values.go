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

package utils

import (
	"encoding/json"
	"fmt"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"

	"k8s.io/apimachinery/pkg/runtime"
)

// MergeServiceTemplateAndConfigurationValues returns the helm values of the DPUService a
// DPUDeployment generates from a DPUServiceTemplate and a DPUServiceConfiguration: the
// configuration values merged over the template values. It returns nil when neither sets values.
func MergeServiceTemplateAndConfigurationValues(serviceTemplate *dpuservicev1.DPUServiceTemplate, serviceConfig *dpuservicev1.DPUServiceConfiguration) (*runtime.RawExtension, error) {
	var serviceConfigValues, serviceTemplateValues map[string]interface{}
	if serviceConfig != nil && serviceConfig.Spec.ServiceConfiguration.HelmChart.Values != nil {
		if err := json.Unmarshal(serviceConfig.Spec.ServiceConfiguration.HelmChart.Values.Raw, &serviceConfigValues); err != nil {
			return nil, fmt.Errorf("error while unmarshaling serviceConfig values: %w", err)
		}
	}
	if serviceTemplate.Spec.HelmChart.Values != nil {
		if err := json.Unmarshal(serviceTemplate.Spec.HelmChart.Values.Raw, &serviceTemplateValues); err != nil {
			return nil, fmt.Errorf("error while unmarshaling serviceTemplate values: %w", err)
		}
	}

	mergedValues := MergeMaps(serviceConfigValues, serviceTemplateValues)
	if mergedValues == nil {
		return nil, nil
	}
	mergedValuesRaw, err := json.Marshal(mergedValues)
	if err != nil {
		return nil, fmt.Errorf("error while marshaling merged ServiceTemplate and ServiceConfig values: %w", err)
	}
	return &runtime.RawExtension{Raw: mergedValuesRaw}, nil
}
