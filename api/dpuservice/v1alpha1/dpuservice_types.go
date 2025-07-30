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
	"strings"

	"github.com/nvidia/doca-platform/pkg/conditions"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// DPUServiceKind is the kind of the DPUService.
	DPUServiceKind = "DPUService"
	// DPUServiceListKind is the kind of the DPUServiceList.
	DPUServiceListKind = "DPUServiceList"
	// DPUServiceFinalizer is the finalizer that will be added to the DPUService.
	DPUServiceFinalizer = "dpu.nvidia.com/dpuservice"
	// DPUServiceNameLabelKey is the label key that is used to store the name of the DPUService.
	DPUServiceNameLabelKey = "dpu.nvidia.com/dpuservice-name"
	// DPUServiceNamespaceLabelKey is the label key that is used to store the namespace of the DPUService.
	DPUServiceNamespaceLabelKey = "dpu.nvidia.com/dpuservice-namespace"
	// DPFServiceIDLabelKey is a label of DPU Service pods with a value of service identifier.
	DPFServiceIDLabelKey = "svc.dpu.nvidia.com/service"
	// DPUServiceExposedPortForDPUClusterLabelKey is the label key that is used to store the exposed port of the DPUService.
	DPUServiceExposedPortForDPUClusterLabelKey = "dpu.nvidia.com/exposed-port-for-dpucluster"

	// DPFImagePullSecretLabelKey marks a secret as being an ImagePullSecret used by DPF which should be mirrored to DPUClusters.
	DPFImagePullSecretLabelKey = "dpu.nvidia.com/image-pull-secret"

	// DPUServiceInterfaceAnnotationKey is the key used to add an annotation to a
	// the DPUServiceInterface to indicate that it is consumed by a DPUService.
	DPUServiceInterfaceAnnotationKey = "dpu.nvidia.com/consumed-by"

	// InterfaceIndexKey is the key used to index the DPUService by the interfaces
	// it consumes.
	InterfaceIndexKey = ".metadata.interfaces"
)

var DPUServiceGroupVersionKind = GroupVersion.WithKind(DPUServiceKind)

// Condition types
const (
	// ConditionDPUServiceInterfaceReconciled is the condition type that indicates that the
	// DPUServiceInterface is reconciled.
	ConditionDPUServiceInterfaceReconciled conditions.ConditionType = "DPUServiceInterfaceReconciled"
	// ConditionApplicationPrereqsReconciled is the condition type that indicates that the
	// application prerequisites are reconciled.
	ConditionApplicationPrereqsReconciled conditions.ConditionType = "ApplicationPrereqsReconciled"
	// ConditionApplicationsReconciled is the condition type that indicates that the
	// applications are reconciled.
	ConditionApplicationsReconciled conditions.ConditionType = "ApplicationsReconciled"
	// ConditionConfigPortsReconciled is the condition type that indicates that the
	// applications config ports are reconciled.
	ConditionConfigPortsReconciled conditions.ConditionType = "ConfigPortsReconciled"
	// ConditionApplicationsReady is the condition type that indicates that the
	// applications are ready.
	ConditionApplicationsReady conditions.ConditionType = "ApplicationsReady"
)

var (
	// Conditions is the list of conditions that the DPUService can have.
	Conditions = []conditions.ConditionType{
		conditions.TypeReady,
		ConditionApplicationPrereqsReconciled,
		ConditionApplicationsReconciled,
		ConditionApplicationsReady,
		ConditionDPUServiceInterfaceReconciled,
		ConditionConfigPortsReconciled,
	}
)

var _ conditions.GetSet = &DPUService{}

// GetConditions returns the conditions of the DPUService.
func (s *DPUService) GetConditions() []metav1.Condition {
	return s.Status.Conditions
}

// SetConditions sets the conditions of the DPUService.
func (s *DPUService) SetConditions(conditions []metav1.Condition) {
	s.Status.Conditions = conditions
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:annotations=helm.sh/resource-policy=keep
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=`.status.conditions[?(@.type=='Ready')].status`
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=`.status.conditions[?(@.type=='Ready')].reason`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:validation:XValidation:rule="self.metadata.name.size() <= 63", message="name length can't be bigger than 63 chars"

// DPUService is the Schema for the dpuservices API
type DPUService struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DPUServiceSpec   `json:"spec,omitempty"`
	Status DPUServiceStatus `json:"status,omitempty"`
}

// DPUServiceSpec defines the desired state of DPUService
// +kubebuilder:validation:XValidation:rule="(has(self.interfaces) && has(self.serviceID)) || (!has(self.interfaces) && !has(self.serviceID)) || has(self.serviceID)", message="serviceID must be provided when interfaces are provided"
// +kubebuilder:validation:XValidation:rule="!(has(self.deployInCluster) && self.deployInCluster && has(self.configPorts))", message="configPorts cannot be set when deployInCluster is true"
type DPUServiceSpec struct {
	// HelmChart reflects the Helm related configuration
	// +required
	HelmChart HelmChart `json:"helmChart"`

	// ServiceID is the ID of the service that the DPUService is associated with.
	// +optional
	ServiceID *string `json:"serviceID,omitempty"`

	// ServiceDaemonSet specifies the configuration for the ServiceDaemonSet.
	// +optional
	ServiceDaemonSet *ServiceDaemonSetValues `json:"serviceDaemonSet,omitempty"`

	// DeployInCluster indicates if the DPUService Helm Chart will be deployed on
	// the Host cluster. Default to false.
	// +optional
	DeployInCluster *bool `json:"deployInCluster,omitempty"`

	// Interfaces specifies the DPUServiceInterface names that the DPUService
	// uses in the same namespace.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=50
	// +optional
	Interfaces []string `json:"interfaces,omitempty"`

	// Paused indicates that the DPUService is paused.
	// Underlying resources are also paused when this is set to true.
	// No deletion of resources will occur when this is set to true.
	// +optional
	Paused *bool `json:"paused,omitempty"`

	// ConfigPorts defines the desired state of port configurations for a DPUService.
	// This struct determines how ports are exposed from the DPU to the host cluster.
	// A DPUService can only have a single ServiceType across all ports.
	// +optional
	ConfigPorts *ConfigPorts `json:"configPorts,omitempty"`
}

// ConfigPorts defines the desired state of port configurations for a DPUService.
// This struct determines how ports are exposed from the DPU to the host cluster.
// A DPUService can only have a single ServiceType across all ports.
//
// Validation:
// - If any port has a NodePort assigned, ServiceType **must** be "NodePort".
//
// +kubebuilder:validation:XValidation:rule="!(self.serviceType != 'NodePort' && self.ports.exists(p, has(p.nodePort)))",message="nodePort can only be set when serviceType is NodePort"
type ConfigPorts struct {
	// ServiceType specifies the type of Kubernetes Service to create.
	// All ports within this ConfigPorts will have the same ServiceType.
	// The value is immutable and cannot be changed after creation.
	// Supported values:
	// - "NodePort": Exposes ports externally on a node.
	// - "ClusterIP": Exposes ports internally within the cluster.
	// - "None": Internal-only service with no cluster IP.
	// Default: "NodePort"
	//
	// +required
	// +kubebuilder:validation:Enum=NodePort;ClusterIP;None
	// +kubebuilder:default:=NodePort
	// +kubebuilder:validation:XValidation:rule="self == oldSelf", message="Value is immutable"
	ServiceType corev1.ServiceType `json:"serviceType"`

	// Ports defines the list of port configurations that will be exposed by the DPUService.
	// Each port must specify a name, port number, and protocol.
	//
	// Constraints:
	// - If ServiceType is "NodePort", ports may optionally specify a NodePort.
	// - If ServiceType is "None" or "ClusterIP", ports **cannot** specify a NodePort.
	//
	// +required
	Ports []ConfigPort `json:"ports"`
}

// ConfigPort defines the configuration of a single port within a DPUService.
// Each port must have a unique name within the service.
type ConfigPort struct {
	// Name is a unique identifier for the port within the DPUService.
	// This name is used for reference inside the service.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern=`^[a-z0-9-]+$`
	Name string `json:"name"`

	// Port is the port number that will be exposed by the service.
	// Must be within the valid range of TCP/UDP ports (1-65535).
	//
	// +required
	Port uint16 `json:"port"`

	// Protocol specifies the transport protocol used by the port.
	// Supported values: TCP, UDP
	//
	// +required
	// +kubebuilder:validation:Enum=TCP;UDP
	Protocol corev1.Protocol `json:"protocol"`

	// NodePort is the external port assigned on each node in the cluster.
	// If not set, Kubernetes will automatically allocate a NodePort.
	//
	// Constraints:
	// - Can only be set when ServiceType is "NodePort".
	// - Must be within the clusters valid NodePort range (Kubernetes default is 30000-32767).
	//
	// +optional
	NodePort *uint16 `json:"nodePort,omitempty"`
}

// SetServiceDeamonSetNodeSelector sets the nodeSelector for the ServiceDaemonSet.
func (s *DPUService) SetServiceDeamonSetNodeSelector(nodeSelector *corev1.NodeSelector) {
	if s.Spec.ServiceDaemonSet == nil {
		s.Spec.ServiceDaemonSet = &ServiceDaemonSetValues{}
	}

	s.Spec.ServiceDaemonSet.NodeSelector = nodeSelector
}

// IsPaused returns true if the DPUService is paused.
func (s *DPUService) IsPaused() bool {
	return s.Spec.Paused != nil && *s.Spec.Paused
}

// ShouldDeployInCluster returns true if the DPUService should be deployed on the host cluster.
func (s *DPUService) ShouldDeployInCluster() bool {
	if s.Spec.DeployInCluster == nil {
		return false
	}
	return *s.Spec.DeployInCluster
}

// HelmChart reflects the helm related configuration
type HelmChart struct {
	// Source specifies information about the Helm chart
	// +required
	Source ApplicationSource `json:"source"`

	// Values specifies Helm values to be passed to Helm template, defined as a map. This takes precedence over Values.
	// +kubebuilder:pruning:PreserveUnknownFields
	// +optional
	Values *runtime.RawExtension `json:"values,omitempty"`
}

// ApplicationSource specifies the source of the Helm chart.
type ApplicationSource struct {
	// RepoURL specifies the URL to the repository that contains the application Helm chart.
	// The URL must begin with either 'oci://' or 'https://', ensuring it points to a valid
	// OCI registry or a web-based repository.
	// +kubebuilder:validation:Pattern=`^(oci://|https://).+$`
	// +required
	RepoURL string `json:"repoURL"`

	// Path is the location of the chart inside the repo.
	// +optional
	Path string `json:"path"`

	// Version is a semver tag for the Chart's version.
	// +kubebuilder:validation:MinLength=1
	// +required
	Version string `json:"version"`

	// Chart is the name of the helm chart.
	// +optional
	Chart string `json:"chart,omitempty"`

	// ReleaseName is the name to give to the release generate from the DPUService.
	// +optional
	ReleaseName string `json:"releaseName,omitempty"`
}

// ServiceDaemonSetValues specifies the configuration for the ServiceDaemonSet.
type ServiceDaemonSetValues struct {
	// NodeSelector specifies which Nodes to deploy the ServiceDaemonSet to.
	// +optional
	NodeSelector *corev1.NodeSelector `json:"nodeSelector,omitempty"`

	// UpdateStrategy specifies the DeaemonSet update strategy for the ServiceDaemonset.
	// +optional
	UpdateStrategy *appsv1.DaemonSetUpdateStrategy `json:"updateStrategy,omitempty"`

	// Labels specifies labels which are added to the ServiceDaemonSet.
	// +optional
	Labels map[string]string `json:"labels,omitempty"`

	// Annotations specifies annotations which are added to the ServiceDaemonSet.
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`

	// Resources specifies resources which are added to the ServiceDaemonSet.
	// +optional
	Resources corev1.ResourceList `json:"resources,omitempty"`
}

// DPUServiceStatus defines the observed state of DPUService
type DPUServiceStatus struct {
	// Conditions defines current service state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ObservedGeneration records the Generation observed on the object the last time it was patched.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// ConfigPorts defines the observed state of the config ports.
	// It contains the actual port numbers that are exposed on the DPUService per cluster.
	// +optional
	ConfigPorts map[string][]ConfigPort `json:"configPorts,omitempty"`
}

// +kubebuilder:object:root=true

// DPUServiceList contains a list of DPUService
type DPUServiceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DPUService `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DPUService{}, &DPUServiceList{})
}

func (a *ApplicationSource) GetArgoRepoURL() string {
	return strings.TrimPrefix(a.RepoURL, "oci://")
}

// MatchLabels returns the labels that match the DPUService.
func (s *DPUService) MatchLabels() client.MatchingLabels {
	return map[string]string{
		DPUServiceNameLabelKey:      s.Name,
		DPUServiceNamespaceLabelKey: s.Namespace,
	}
}
