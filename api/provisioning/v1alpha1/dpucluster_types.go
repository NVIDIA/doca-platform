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
)

const (
	DPUClusterKind = "DPUCluster"
)

var DPUClusterGroupVersionKind = GroupVersion.WithKind(DPUClusterKind)

type ClusterType string

type ConditionType string

const (
	StaticCluster ClusterType = "static"
	KamajiCluster ClusterType = "kamaji"
)

const (
	ConditionCreated ConditionType = "Created"
	ConditionReady   ConditionType = "Ready"
)

// ClusterPhase describes current state of DPUCluster.
// Only one of the following state may be specified.
// Default is Pending.
// +kubebuilder:validation:Enum="Pending";"Creating";"Ready";"NotReady";"Failed"
type ClusterPhase string

const (
	PhasePending  ClusterPhase = "Pending"
	PhaseCreating ClusterPhase = "Creating"
	PhaseReady    ClusterPhase = "Ready"
	PhaseNotReady ClusterPhase = "NotReady"
	PhaseFailed   ClusterPhase = "Failed"
)

const (
	FinalizerCleanUp         = "provisioning.dpu.nvidia.com/cluster-manager-clean-up"
	FinalizerInternalCleanUp = "provisioning.dpu.nvidia.com/cluster-manager-internal-clean-up"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// DPUClusterSpec defines the desired state of DPUCluster
type DPUClusterSpec struct {
	// Type of the cluster with few supported values
	// static - existing cluster that is deployed by user. For DPUCluster of this type, the kubeconfig field must be set.
	// kamaji - DPF managed cluster. The kamaji-cluster-manager will create a DPU cluster on behalf of this CR.
	// $(others) - any string defined by ISVs, such type names must start with a prefix.
	// +kubebuilder:validation:Pattern="kamaji|static|[^/]+/.*"
	// +required
	Type string `json:"type"`

	// MaxNodes is the max amount of node in the cluster
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=1000
	// +kubebuilder:default=1000
	// +optional
	MaxNodes int `json:"maxNodes,omitempty"`

	// Version is the K8s control-plane version of the cluster
	Version string `json:"version"`

	// Kubeconfig is the secret that contains the admin kubeconfig
	// +kubebuilder:validation:XValidation:rule="oldSelf==\"\"||self==oldSelf",message="kubeconfig is immutable"
	// +optional
	Kubeconfig string `json:"kubeconfig,omitempty"`

	// ClusterEndpoint contains configurations of the cluster entry point
	// +optional
	ClusterEndpoint *ClusterEndpointSpec `json:"clusterEndpoint,omitempty"`
}

// DPUClusterStatus defines the observed state of DPUCluster
type DPUClusterStatus struct {
	// +kubebuilder:validation:Enum=Pending;Creating;Ready;NotReady;Failed
	// +kubebuilder:default="Pending"
	Phase ClusterPhase `json:"phase"`

	// +optional
	Conditions []metav1.Condition `json:"conditions"`
}

type ClusterEndpointSpec struct {
	// Keepalived configures the keepalived that will be deployed for the cluster control-plane
	// +optional
	Keepalived *KeepalivedSpec `json:"keepalived,omitempty"`
}

type KeepalivedSpec struct {
	// VIP is the virtual IP owned by the keepalived instances
	VIP string `json:"vip"`

	// VirtualRouterID is the virtual_router_id in keepalived.conf
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=255
	VirtualRouterID int `json:"virtualRouterID"`

	// Interface specifies on which interface the VIP should be assigned
	// +kubebuilder:validation:MinLength=1
	Interface string `json:"interface"`

	// NodeSelector is used to specify a subnet of control plane nodes to deploy keepalived instances.
	// Note: keepalived instances are always deployed on control plane nodes
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:annotations=helm.sh/resource-policy=keep
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=`.status.conditions[?(@.type=='Ready')].status`
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase",description="phase of the cluster"
// +kubebuilder:printcolumn:name="Type",type="string",JSONPath=".spec.type",description="type of the cluster"
// +kubebuilder:printcolumn:name="MaxNodes",type="integer",JSONPath=".spec.maxNodes",description="max amount of nodes"
// +kubebuilder:printcolumn:name="Version",type="string",JSONPath=".spec.version",description="Kubernetes control-plane version"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// DPUCluster is the Schema for the dpuclusters API
type DPUCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +required
	Spec DPUClusterSpec `json:"spec,omitempty"`

	// +kubebuilder:default={phase: Pending}
	// +optional
	Status DPUClusterStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DPUClusterList contains a list of DPUCluster
type DPUClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DPUCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DPUCluster{}, &DPUClusterList{})
}

const (
	// DPUClusterLabelKey is the key of the label linking objects to a specific DPU Cluster.
	DPUClusterLabelKey = "dpu.nvidia.com/cluster"
)
