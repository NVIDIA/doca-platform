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

package inventory

import (
	"context"
	_ "embed"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"

	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Component describes the responsibilities of an item in the Inventory.
type Component interface {
	// Name returns the name of the component.
	Name() operatorv1.ComponentName

	// Parse parses the component data into the relevant fields of the struct and performs some basic validations.
	// Returns an error if the data is invalid or if the component cannot be parsed.
	Parse() error

	// GenerateManifests generates Kubernetes manifests for the component.
	// The manifests are generated based on the provided variables.
	// Returns a slice of client.Object which can be applied to the cluster.
	// If the component is not ready for upgrade, it returns an error.
	GenerateManifests(ctx context.Context, variables Variables) ([]client.Object, error)

	// IsReadyForUpgrade reports an object and a field in that object which is used to check the ready status of a Component.
	// The version contains the version of the DPF Operator that is being upgraded from.
	// Returns an error if the object is not ready.
	IsReadyForUpgrade(ctx context.Context, c client.Client, config *operatorv1.DPFOperatorConfig) error

	// IsReady checks if the component is ready and its version is updated.
	// It checks the readiness of the component in the given namespace.
	// Returns an error if the component is not ready.
	IsReady(ctx context.Context, c client.Client, namespace string) error
}

var _ Component = removedComponent{}

// removedComponent reconciles legacy ApplySets to empty.
type removedComponent struct {
	name operatorv1.ComponentName
}

func (r removedComponent) Name() operatorv1.ComponentName {
	return r.name
}

func (r removedComponent) Parse() error {
	return nil
}

func (r removedComponent) GenerateManifests(context.Context, Variables) ([]client.Object, error) {
	return nil, nil
}

func (r removedComponent) IsReadyForUpgrade(context.Context, client.Client, *operatorv1.DPFOperatorConfig) error {
	return nil
}

func (r removedComponent) IsReady(context.Context, client.Client, string) error {
	return nil
}

// SystemComponents holds kubernetes object manifests to be deployed by the operator.
type SystemComponents struct {
	DPUService                      Component
	DPFProvisioning                 Component
	ServiceFunctionChainSet         Component
	RemoveOVSCNI                    Component
	Multus                          Component
	SRIOVDevicePlugin               Component
	NVIPAM                          Component
	Flannel                         Component
	SfcController                   Component
	KamajiClusterManager            Component
	StaticClusterManager            Component
	DPUDetector                     Component
	CNIInstaller                    Component
	NodeSRIOVDevicePluginController Component
	KubeStateMetrics                Component
	NodeProblemDetector             Component
	OpenTelemetryCollector          Component
	KataContainers                  Component
}

// Embed manifests for Kubernetes objects created by the controller.
var (
	//go:embed manifests/dpuservice-controller.yaml
	dpuServiceData []byte

	//go:embed manifests/servicefunctionchainset-controller.yaml
	serviceChainSetData []byte

	//go:embed manifests/sriov-device-plugin.yaml
	sriovDevicePluginData []byte

	//go:embed manifests/multus.yaml
	multusData []byte

	//go:embed manifests/flannel.yaml
	flannelData []byte

	//go:embed manifests/nv-k8s-ipam.yaml
	nvK8sIpamData []byte

	//go:embed manifests/provisioning-controller.yaml
	provisioningControllerData []byte

	//go:embed manifests/bfb-registry.yaml
	bfbRegistryData []byte

	//go:embed manifests/sfc-controller.yaml
	sfcControllerData []byte

	//go:embed manifests/kamaji-cluster-manager.yaml
	kamajiCMData []byte

	//go:embed manifests/static-cluster-manager.yaml
	staticCMData []byte

	//go:embed manifests/dpu-detector.yaml
	dpuDetectorData []byte

	//go:embed manifests/cni-installer.yaml
	cniInstallerData []byte

	//go:embed manifests/nodesriovdeviceplugin-controller.yaml
	nodeSRIOVDevicePluginControllerData []byte

	//go:embed manifests/kube-state-metrics.yaml
	kubeStateMetricsData []byte

	//go:embed manifests/node-problem-detector.yaml
	nodeProblemDetectorData []byte

	//go:embed manifests/opentelemetry-collector.yaml
	openTelemetryCollectorData []byte

	//go:embed manifests/kata-containers.yaml
	kataContainersData []byte
)

// New returns a new SystemComponents inventory with data preloaded but parsing not completed.
func New() *SystemComponents {
	return &SystemComponents{
		DPUService: &dpuServiceControllerObjects{
			data: dpuServiceData,
		},
		DPFProvisioning: &provisioningControllerObjects{
			data:            provisioningControllerData,
			bfbRegistryData: bfbRegistryData,
		},
		ServiceFunctionChainSet: newServiceChainSetControllerObjects(serviceChainSetData),
		// TODO: Remove this after 26.7 is released.
		RemoveOVSCNI: removedComponent{name: operatorv1.OVSCNIName},
		Multus: &fromDPUService{
			name: operatorv1.MultusName,
			data: multusData,
		},
		SRIOVDevicePlugin: &fromDPUService{
			name: operatorv1.SRIOVDevicePluginName,
			data: sriovDevicePluginData,
		},
		Flannel: &fromDPUService{
			name: operatorv1.FlannelName,
			data: flannelData,
		},
		NVIPAM:        newNVIPAMObjects(nvK8sIpamData),
		SfcController: newSFCControllerObjects(sfcControllerData),
		DPUDetector: &dpuDetectorObjects{
			data: dpuDetectorData,
		},
		KamajiClusterManager: newKamajiClusterManagerObjects(kamajiCMData),
		StaticClusterManager: newStaticClusterManagerObjects(staticCMData),
		CNIInstaller: &fromDPUService{
			name: operatorv1.CNIInstallerName,
			data: cniInstallerData,
		},
		NodeSRIOVDevicePluginController: newNodeSRIOVDevicePluginControllerObjects(
			nodeSRIOVDevicePluginControllerData),
		KubeStateMetrics: newKubeStateMetricsObjects(kubeStateMetricsData),
		NodeProblemDetector: &fromDPUService{
			name: operatorv1.NodeProblemDetectorName,
			data: nodeProblemDetectorData,
		},
		OpenTelemetryCollector: &fromDPUService{
			name: operatorv1.OpenTelemetryCollectorName,
			data: openTelemetryCollectorData,
		},
		KataContainers: &fromDPUService{
			name: operatorv1.KataContainersName,
			data: kataContainersData,
		},
	}
}

// SystemDPUServices returns DPUService Components deployed by the DPF Operator.
func (s *SystemComponents) SystemDPUServices() []Component {
	return []Component{
		s.ServiceFunctionChainSet,
		s.Multus,
		s.SRIOVDevicePlugin,
		s.Flannel,
		s.NVIPAM,
		s.SfcController,
		s.CNIInstaller,
		s.KubeStateMetrics,
		s.NodeProblemDetector,
		s.OpenTelemetryCollector,
		s.KataContainers,
	}
}

// AllComponents returns all Components deployed by the DPF Operator.
func (s *SystemComponents) AllComponents() []Component {
	return []Component{
		s.KamajiClusterManager,
		s.StaticClusterManager,
		s.DPFProvisioning,
		s.DPUService,
		s.DPUDetector,
		s.ServiceFunctionChainSet,
		s.RemoveOVSCNI,
		s.Multus,
		s.SRIOVDevicePlugin,
		s.Flannel,
		s.NVIPAM,
		s.SfcController,
		s.CNIInstaller,
		s.NodeSRIOVDevicePluginController,
		s.KubeStateMetrics,
		s.NodeProblemDetector,
		s.OpenTelemetryCollector,
		s.KataContainers,
	}
}

// EnabledComponents returns the set of components which is not disabled.
func (s *SystemComponents) EnabledComponents(vars Variables) []Component {
	out := []Component{}
	for _, component := range s.AllComponents() {
		if disabled, found := vars.DisableSystemComponents[component.Name()]; found && !disabled {
			out = append(out, component)
		}
	}
	return out
}

// ParseAll creates Kubernetes objects for all manifests related to the DPFOperator.
func (s *SystemComponents) ParseAll() error {
	for _, component := range s.AllComponents() {
		if err := component.Parse(); err != nil {
			return err
		}
	}
	return nil
}

// generateAllManifests returns all Kubernetes objects.
func (s *SystemComponents) generateAllManifests(ctx context.Context, variables Variables) ([]client.Object, error) {
	out := []client.Object{}
	var errs []error
	for _, component := range s.AllComponents() {
		manifests, err := component.GenerateManifests(ctx, variables)
		if err != nil {
			errs = append(errs, err)
		}
		out = append(out, manifests...)
	}
	if len(errs) != 0 {
		return nil, kerrors.NewAggregate(errs)
	}
	return out, nil
}

func (s *SystemComponents) setDPUService(input dpuServiceControllerObjects) *SystemComponents {
	s.DPUService = &input
	return s
}

func (s *SystemComponents) setMultus(input fromDPUService) *SystemComponents {
	s.Multus = &input
	return s

}

func (s *SystemComponents) setSRIOVDevicePlugin(input fromDPUService) *SystemComponents {
	s.SRIOVDevicePlugin = &input
	return s
}

func (s *SystemComponents) setServiceFunctionChainSet(input dpuServicePerDPUClusterObjects) *SystemComponents {
	s.ServiceFunctionChainSet = &input
	return s
}

func (s *SystemComponents) setFlannel(input fromDPUService) *SystemComponents {
	s.Flannel = &input
	return s
}

func (s *SystemComponents) setNvK8sIpam(input dpuServicePerDPUClusterObjects) *SystemComponents {
	s.NVIPAM = &input
	return s
}

func (s *SystemComponents) setSfcController(input fromDPUService) *SystemComponents {
	s.SfcController = &input
	return s
}

func (s *SystemComponents) setCNIInstaller(input fromDPUService) *SystemComponents {
	s.CNIInstaller = &input
	return s
}

func (s *SystemComponents) setKataContainers(input fromDPUService) *SystemComponents {
	s.KataContainers = &input
	return s
}
