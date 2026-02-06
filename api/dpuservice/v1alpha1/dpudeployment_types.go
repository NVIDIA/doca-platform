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
	"fmt"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/digest"
	"github.com/nvidia/doca-platform/pkg/conditions"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	DPUDeploymentFinalizer = "dpu.nvidia.com/dpudeployment"
	DPUDeploymentKind      = "DPUDeployment"
	// ParentDPUDeploymentNameLabel contains the name of the DPUDeployment object that owns the resource
	ParentDPUDeploymentNameLabel = "svc.dpu.nvidia.com/owned-by-dpudeployment"

	// DependentDPUDeploymentLabelKeyPrefix is the prefix of the label key that is applied to dependent objects of
	// a DPUDeployment
	DependentDPUDeploymentLabelKeyPrefix = "svc.dpu.nvidia.com/consumed-by-dpudeployment"
	// DependentDPUDeploymentLabelValue is the label value that is applied to dependent objects of a DPUDeployment
	DependentDPUDeploymentLabelValue = ""
)

var DPUDeploymentGroupVersionKind = GroupVersion.WithKind(DPUDeploymentKind)

// Status related variables
const (
	ConditionPreReqsReady                   conditions.ConditionType = "PrerequisitesReady"
	ConditionResourceFittingReady           conditions.ConditionType = "ResourceFittingReady"
	ConditionVersionMatchingReady           conditions.ConditionType = "VersionMatchingReady"
	ConditionDPUSetsReconciled              conditions.ConditionType = "DPUSetsReconciled"
	ConditionDPUSetsReady                   conditions.ConditionType = "DPUSetsReady"
	ConditionDPUServicesReconciled          conditions.ConditionType = "DPUServicesReconciled"
	ConditionDPUServicesReady               conditions.ConditionType = "DPUServicesReady"
	ConditionDPUServiceChainsReconciled     conditions.ConditionType = "DPUServiceChainsReconciled"
	ConditionDPUServiceChainsReady          conditions.ConditionType = "DPUServiceChainsReady"
	ConditionDPUServiceInterfacesReconciled conditions.ConditionType = "DPUServiceInterfacesReconciled"
	ConditionDPUServiceInterfacesReady      conditions.ConditionType = "DPUServiceInterfacesReady"
)

var (
	DPUDeploymentConditions = []conditions.ConditionType{
		conditions.TypeReady,
		ConditionPreReqsReady,
		ConditionResourceFittingReady,
		ConditionVersionMatchingReady,
		ConditionDPUSetsReconciled,
		ConditionDPUSetsReady,
		ConditionDPUServicesReconciled,
		ConditionDPUServicesReady,
		ConditionDPUServiceChainsReconciled,
		ConditionDPUServiceChainsReady,
		ConditionDPUServiceInterfacesReconciled,
		ConditionDPUServiceInterfacesReady,
	}
)

var _ conditions.GetSet = &DPUDeployment{}

func (c *DPUDeployment) GetConditions() []metav1.Condition {
	return c.Status.Conditions
}

func (c *DPUDeployment) SetConditions(conditions []metav1.Condition) {
	c.Status.Conditions = conditions
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:metadata:annotations=helm.sh/resource-policy=keep
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=`.status.conditions[?(@.type=='Ready')].status`
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=`.status.conditions[?(@.type=='Ready')].reason`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:validation:XValidation:rule="self.metadata.name.size() <= 20", message="name length can't be bigger than 20 chars"

// DPUDeployment is the Schema for the dpudeployments API. This object connects DPUServices with specific BFBs and
// DPUServiceChains.
type DPUDeployment struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DPUDeploymentSpec   `json:"spec,omitempty"`
	Status DPUDeploymentStatus `json:"status,omitempty"`
}

// DPUDeploymentSpec defines the desired state of DPUDeployment
type DPUDeploymentSpec struct {
	// DPUs contains the DPU related configuration
	// +required
	DPUs DPUs `json:"dpus"`

	// Services contains the DPUDeploymentService related configuration. The key is the deploymentServiceName and the value is its
	// configuration. All underlying objects must specify the same deploymentServiceName in order to be able to be consumed by the
	// DPUDeployment.
	// +kubebuilder:validation:XValidation:rule="self.all(key, key.size()<=28)", message="service names can't be bigger than 28 chars"
	// +kubebuilder:validation:MinProperties=1
	// +kubebuilder:validation:MaxProperties=50
	// +required
	Services map[string]DPUDeploymentServiceConfiguration `json:"services"`

	// ServiceChains contains the configuration related to the DPUServiceChains that the DPUDeployment creates.
	// +optional
	ServiceChains *ServiceChains `json:"serviceChains,omitempty"`

	// The maximum number of revisions that can be retained during upgrades.
	// Defaults to 10.
	// +optional
	// +kubebuilder:default=10
	RevisionHistoryLimit *int32 `json:"revisionHistoryLimit,omitempty"`
}

type ServiceChains struct {
	// UpgradePolicy contains the configuration for the upgrade process
	// +kubebuilder:default={}
	// +required
	UpgradePolicy UpgradePolicy `json:"upgradePolicy"`

	// Switches is the list of switches that form the service chain
	// +required
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=50
	Switches []DPUDeploymentSwitch `json:"switches"`
}

type UpgradePolicy struct {
	// ApplyNodeEffect specifies if the node effect should be applied during the
	// upgrade. It signals the reconciler that this object upgrade is disruptive.
	// Hence a new revision of the object should be created and node effect should
	// be applied.
	// +kubebuilder:default=true
	// +optional
	ApplyNodeEffect *bool `json:"applyNodeEffect,omitempty"`
}

// ShouldApplyNodeEffect returns the value of ApplyNodeEffect, if it is nil it returns true
func (u *UpgradePolicy) ShouldApplyNodeEffect() bool {
	if u.ApplyNodeEffect == nil {
		return true
	}
	return *u.ApplyNodeEffect
}

// DPUs contains the DPU related configuration
type DPUs struct {
	// BFB is the name of the BFB object to be used in this DPUDeployment. It must be in the same namespace as the
	// DPUDeployment.
	// +required
	BFB string `json:"bfb"`

	// Flavor is the name of the DPUFlavor object to be used in this DPUDeployment. It must be in the same namespace as
	// the DPUDeployment.
	// +required
	Flavor string `json:"flavor"`

	// DPUSets contains configuration for each DPUSet that is going to be created by the DPUDeployment
	// +optional
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=50
	// +kubebuilder:validation:XValidation:rule="self.all(x, self.exists_one(y, y.nameSuffix == x.nameSuffix))", message="DPUSet.NameSuffix values must be unique"
	DPUSets []DPUSet `json:"dpuSets,omitempty"`

	// NodeEffect is the effect the DPU has on Nodes during provisioning.
	// +optional
	NodeEffect *provisioningv1.Action `json:"nodeEffect,omitempty"`

	// DPUSetStrategy is the strategy to use for the DPUSets created by the DPUDeployment.
	// +optional
	DPUSetStrategy *provisioningv1.DPUSetStrategy `json:"dpuSetStrategy,omitempty"`

	// SecureBoot specifies whether UEFI Secure Boot should be enabled.
	// +optional
	SecureBoot *bool `json:"secureBoot,omitempty"`
}

// +kubebuilder:validation:XValidation:rule="!(has(self.dpuAnnotations) && (self.dpuAnnotations.exists(key, (key.contains('dpu.nvidia.com/') || key.endsWith('dpu.nvidia.com')) && !key.startsWith('noderesources.dpu.nvidia.com'))))", message="should not contain dpu.nvidia.com/ and should not end with dpu.nvidia.com"
// +kubebuilder:validation:XValidation:rule="!(has(self.nodeSelector) && has(self.dpuNodeSelector))", message="only one of nodeSelector or dpuNodeSelector can be specified"
// +kubebuilder:validation:XValidation:rule="!(has(self.dpuSelector) && has(self.dpuDeviceSelector))", message="only one of dpuSelector or dpuDeviceSelector can be specified"

// DPUSet contains configuration for the DPUSet to be created by the DPUDeployment
type DPUSet struct {
	// NameSuffix is the suffix to be added to the name of the DPUSet object created by the DPUDeployment.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=24
	// +required
	NameSuffix string `json:"nameSuffix,omitempty"`

	// NodeSelector defines the nodes that the DPUSet should target
	//
	// Deprecated: This field is deprecated and will be removed with v26.7.0. Use DPUNodeSelector instead.
	// +optional
	NodeSelector *metav1.LabelSelector `json:"nodeSelector,omitempty"`

	// DPUSelector defines the DPUs that the DPUSet should target
	//
	// Deprecated: This field is deprecated and will be removed with v26.7.0. Use DPUDeviceSelector instead.
	// +optional
	DPUSelector map[string]string `json:"dpuSelector,omitempty"`

	// DPUNodeSelector defines the selector for DPUNodes that the DPUSet should target and should create a DPU for.
	// +optional
	DPUNodeSelector *metav1.LabelSelector `json:"dpuNodeSelector,omitempty"`

	// DPUDeviceSelector defines the selector for DPUDevices that the DPUSet should target and should create a DPU for.
	// +optional
	DPUDeviceSelector *metav1.LabelSelector `json:"dpuDeviceSelector,omitempty"`

	// DPUClusterSelector defines the selector for DPUClusters that the DPUs created by the DPUSets created by the
	// DPUDeployment should join
	// TODO(4797319): The current implementation has some flaws. Consider using a metav1.LabelSelector instead, this will
	// require multiple DPUServices, DPUServiceInterfaces, and DPUServiceChains to be created so that we can mathematically
	// cover the union of all the selectors across all the DPUSets.
	// +optional
	DPUClusterSelector map[string]string `json:"dpuClusterSelector,omitempty"`

	// DPUAnnotations is the annotations to be added to the DPU object created by the DPUSet.
	// +optional
	// +kubebuilder:validation:MaxProperties=50
	DPUAnnotations map[string]string `json:"dpuAnnotations,omitempty"`
}

// DPUDeploymentServiceConfiguration describes the configuration of a particular Service
type DPUDeploymentServiceConfiguration struct {
	// ServiceTemplate is the name of the DPUServiceTemplate object to be used for this Service. It must be in the same
	// namespace as the DPUDeployment.
	ServiceTemplate string `json:"serviceTemplate"`

	// ServiceConfiguration is the name of the DPUServiceConfiguration object to be used for this Service. It must be
	// in the same namespace as the DPUDeployment.
	ServiceConfiguration string `json:"serviceConfiguration"`

	// DependsOn is a list of local object dependencies that are required for this Service.
	// +optional
	// +kubebuilder:validation:MinItems=1
	DependsOn []LocalObjectDependency `json:"dependsOn,omitempty"`
}

// LocalObjectDependency is a list of local object dependencies that are required for this Service.
// The object must be part of the dpuDeployment `spec.services` list.
type LocalObjectDependency struct {
	// Name is the name of the object
	// +required
	Name string `json:"name"`
}

// DPUDeploymentSwitch holds the ports that are connected in switch topology
type DPUDeploymentSwitch struct {
	// Ports contains the ports of the switch
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=50
	// +required
	Ports []DPUDeploymentPort `json:"ports"`

	// ServiceMTU of the switch
	// The default is 1500.
	// +kubebuilder:validation:Minimum=1280
	// +kubebuilder:validation:Maximum=9216
	// +kubebuilder:default=1500
	// +optional
	ServiceMTU *int `json:"serviceMTU,omitempty"`
}

// DPUDeploymentPort defines how a port can be configured
// +kubebuilder:validation:XValidation:rule="(has(self.service) && !has(self.serviceInterface)) || (!has(self.service) && has(self.serviceInterface))", message="either service or serviceInterface must be specified"
type DPUDeploymentPort struct {
	// Service holds configuration that helps configure the Service Function Chain and identify a port associated with
	// a DPUService
	// +optional
	Service *DPUDeploymentService `json:"service,omitempty"`
	// ServiceInterface holds configuration that helps configure the Service Function Chain and identify a user defined
	// port
	// +optional
	ServiceInterface *ServiceIfc `json:"serviceInterface,omitempty"`
}

// DPUDeploymentService is the struct used for referencing an interface.
type DPUDeploymentService struct {
	// Name is the name of the service as defined in the DPUDeployment Spec
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=28
	// +required
	Name string `json:"name"`
	// Interface name is the name of the interface as defined in the DPUServiceConfiguration
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=15
	// +required
	InterfaceName string `json:"interface"`
	// IPAM defines the IPAM configuration that is configured in the Service Function Chain
	// +optional
	IPAM *IPAM `json:"ipam,omitempty"`
}

// DPUDeploymentStatus defines the observed state of DPUDeployment
type DPUDeploymentStatus struct {
	// Conditions reflect the status of the object
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// ObservedGeneration records the Generation observed on the object the last time it was patched.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true

// DPUDeploymentList contains a list of DPUDeployment
type DPUDeploymentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DPUDeployment `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DPUDeployment{}, &DPUDeploymentList{})
}

func (c *DPUDeployment) GetDependentLabelKey() string {
	nn := types.NamespacedName{Name: c.Name, Namespace: c.Namespace}
	return fmt.Sprintf("%s-%s", DependentDPUDeploymentLabelKeyPrefix, digest.Short(digest.FromObjects(nn), 10))
}
