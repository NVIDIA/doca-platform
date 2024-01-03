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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	// DPUServiceConfigurationKind is the kind of the DPUServiceConfiguration object
	DPUServiceConfigurationKind = "DPUServiceConfiguration"
)

// DPUServiceConfigurationGroupVersionKind is the GroupVersionKind of the DPUServiceConfiguration object
var DPUServiceConfigurationGroupVersionKind = GroupVersion.WithKind(DPUServiceConfigurationKind)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:annotations=helm.sh/resource-policy=keep

// DPUServiceConfiguration is the Schema for the dpuserviceconfigurations API. This object is intended to be used in
// conjunction with a DPUDeployment object. This object is the template from which the DPUService will be created. It
// contains all configuration options from the user to be provided to the service itself via the helm chart values.
// This object doesn't allow configuration of nodeSelector and resources in purpose as these are delegated to the
// DPUDeployment and DPUServiceTemplate accordingly.
type DPUServiceConfiguration struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DPUServiceConfigurationSpec   `json:"spec,omitempty"`
	Status DPUServiceConfigurationStatus `json:"status,omitempty"`
}

// DPUServiceConfigurationSpec defines the desired state of DPUServiceConfiguration
type DPUServiceConfigurationSpec struct {
	// DeploymentServiceName is the name of the DPU service this configuration refers to. It must match
	// .spec.deploymentServiceName of a DPUServiceTemplate object and one of the keys in .spec.services of a
	// DPUDeployment object.
	// +required
	DeploymentServiceName string `json:"deploymentServiceName"`
	// ServiceConfiguration contains fields that are configured on the generated DPUService.
	// +optional
	ServiceConfiguration ServiceConfiguration `json:"serviceConfiguration,omitempty"`
	// Interfaces specifies the DPUServiceInterface to be generated for the generated DPUService.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=50
	// +optional
	Interfaces []ServiceInterfaceTemplate `json:"interfaces,omitempty"`
}

// ServiceInterfaceTemplate contains the information related to an interface of the DPUService
type ServiceInterfaceTemplate struct {
	// Name is the name of the interface
	// +required
	Name string `json:"name"`
	// Network is the Network Attachment Definition in the form of "namespace/name"
	// or just "name" if the namespace is the same as the namespace the pod is running.
	// +required
	Network string `json:"network"`
}

// ServiceConfiguration contains fields that are configured on the generated DPUService.
type ServiceConfiguration struct {
	// HelmChart reflects the Helm related configuration. The user is supposed to configure values specific to that
	// DPUServiceConfiguration used in a DPUDeployment and should not specify values that could be shared across multiple
	// DPUDeployments using different DPUServiceConfigurations. These values are merged with values specified in the
	// DPUServiceTemplate. In case of conflict, the DPUServiceConfiguration values take precedence.
	// +optional
	HelmChart ServiceConfigurationHelmChart `json:"helmChart,omitempty"`
	// ServiceDaemonSet contains settings related to the underlying DaemonSet that is part of the Helm chart
	// +optional
	ServiceDaemonSet DPUServiceConfigurationServiceDaemonSetValues `json:"serviceDaemonSet,omitempty"`
	// TODO: Add nodeEffect
	// DeployInCluster indicates if the DPUService Helm Chart will be deployed on the Host cluster. Default to false.
	// +optional
	DeployInCluster *bool `json:"deployInCluster,omitempty"`
}

// ServiceConfigurationHelmChart reflects the helm related configuration
type ServiceConfigurationHelmChart struct {
	// Values specifies Helm values to be passed to Helm template, defined as a map. This takes precedence over Values.
	// +kubebuilder:pruning:PreserveUnknownFields
	// +optional
	Values *runtime.RawExtension `json:"values,omitempty"`
}

// DPUServiceConfigurationServiceDaemonSetValues reflects the Helm related configuration
type DPUServiceConfigurationServiceDaemonSetValues struct {
	// Labels specifies labels which are added to the ServiceDaemonSet.
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
	// Annotations specifies annotations which are added to the ServiceDaemonSet.
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
}

// DPUServiceConfigurationStatus defines the observed state of DPUServiceConfiguration
type DPUServiceConfigurationStatus struct{}

// +kubebuilder:object:root=true

// DPUServiceConfigurationList contains a list of DPUServiceConfiguration
type DPUServiceConfigurationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DPUServiceConfiguration `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DPUServiceConfiguration{}, &DPUServiceConfigurationList{})
}
