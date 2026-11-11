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

package airgap

import (
	"errors"
	"fmt"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"

	"k8s.io/utils/ptr"
)

// Upstream repositories of the DPU images a DPFOperatorConfig field points at the mirror. DPF
// images are named by their image name.
const (
	flannelRepository             = "ghcr.io/flannel-io/flannel"
	flannelCNIRepository          = "ghcr.io/flannel-io/flannel-cni-plugin"
	multusRepository              = "ghcr.io/k8snetworkplumbingwg/multus-cni"
	sriovDevicePluginRepository   = "ghcr.io/k8snetworkplumbingwg/sriov-network-device-plugin"
	nvIPAMRepository              = "ghcr.io/mellanox/nvidia-k8s-ipam"
	nodeProblemDetectorRepository = "registry.k8s.io/node-problem-detector/node-problem-detector"
	otelCollectorRepository       = "docker.io/otel/opentelemetry-collector-contrib"
	kataDeployRepository          = "quay.io/kata-containers/kata-deploy"
	spiffeCSIDriverRepository     = "ghcr.io/spiffe/spiffe-csi-driver"
	csiNodeDriverRegistrarRepo    = "registry.k8s.io/sig-storage/csi-node-driver-registrar"
	kubeProxyRepository           = "registry.k8s.io/kube-proxy"
	dpfSystemImageName            = "dpf-system"
	cniInstallerImageName         = "dpf-cni-installer"
)

// imageOverride is a DPFOperatorConfig field that sets the image of a DPU container, as the
// "Where each DPU image is set" table of the air-gap user guide lists them.
type imageOverride struct {
	// field is the YAML path of the field, for messages.
	field string
	// repository is the upstream repository of the image, see Mirror.bomImage.
	repository string
	// applies reports whether the component is deployed; nil is always.
	applies func(*operatorv1.DPFOperatorConfig) bool
	// set sets the field.
	set func(*operatorv1.DPFOperatorConfig, string)
}

// imageConfig returns a component config that sets only the image.
func imageConfig(image string) operatorv1.ImageComponentConfig {
	return operatorv1.ImageComponentConfig{Image: ptr.To(image)}
}

// overrides returns default overrides that set only the image.
func overrides(img string) *operatorv1.DefaultOverridesConfiguration {
	return &operatorv1.DefaultOverridesConfiguration{ImageComponentConfig: imageConfig(img)}
}

// setOverrides sets the image of *dst, creating it when nil.
func setOverrides(dst **operatorv1.DefaultOverridesConfiguration, img string) {
	if *dst == nil {
		*dst = overrides(img)
		return
	}
	(*dst).ImageComponentConfig = imageConfig(img)
}

// spiffeEnabled reports whether c configures SPIFFE, which deploys the SPIFFE CSI driver.
func spiffeEnabled(c *operatorv1.DPFOperatorConfig) bool {
	return c.Spec.Security != nil && c.Spec.Security.SPIFFE != nil
}

// kataEnabled reports whether c configures Kata Containers, which deploys kata-deploy.
func kataEnabled(c *operatorv1.DPFOperatorConfig) bool {
	return c.Spec.Security != nil && c.Spec.Security.Kata != nil
}

// ensureMonitoring returns c.Spec.Monitoring, creating it when nil.
func ensureMonitoring(c *operatorv1.DPFOperatorConfig) *operatorv1.MonitoringConfiguration {
	if c.Spec.Monitoring == nil {
		c.Spec.Monitoring = &operatorv1.MonitoringConfiguration{}
	}
	return c.Spec.Monitoring
}

// ensureNVIPAM returns c.Spec.NVIPAM, creating it when nil.
func ensureNVIPAM(c *operatorv1.DPFOperatorConfig) *operatorv1.NVIPAMConfiguration {
	if c.Spec.NVIPAM == nil {
		c.Spec.NVIPAM = &operatorv1.NVIPAMConfiguration{}
	}
	return c.Spec.NVIPAM
}

// ensureSRIOVDevicePlugin returns c.Spec.SRIOVDevicePlugin, creating it when nil.
func ensureSRIOVDevicePlugin(c *operatorv1.DPFOperatorConfig) *operatorv1.SRIOVDevicePluginConfiguration {
	if c.Spec.SRIOVDevicePlugin == nil {
		c.Spec.SRIOVDevicePlugin = &operatorv1.SRIOVDevicePluginConfiguration{}
	}
	return c.Spec.SRIOVDevicePlugin
}

// ensureFlannel returns c.Spec.Flannel, creating it when nil.
func ensureFlannel(c *operatorv1.DPFOperatorConfig) *operatorv1.FlannelConfiguration {
	if c.Spec.Flannel == nil {
		c.Spec.Flannel = &operatorv1.FlannelConfiguration{}
	}
	return c.Spec.Flannel
}

// ensureSPIFFECSIDriver returns c.Spec.Security.SPIFFE.CSIDriver, creating it when nil. It panics
// when c.Spec.Security.SPIFFE is nil; check spiffeEnabled first.
func ensureSPIFFECSIDriver(c *operatorv1.DPFOperatorConfig) *operatorv1.SPIFFECSIDriverConfiguration {
	if c.Spec.Security.SPIFFE.CSIDriver == nil {
		c.Spec.Security.SPIFFE.CSIDriver = &operatorv1.SPIFFECSIDriverConfiguration{}
	}
	return c.Spec.Security.SPIFFE.CSIDriver
}

// imageOverrides are the DPFOperatorConfig fields of the DPU-side components. Components the
// host cluster runs, e.g. the service chain set controller and the nvipam controller, per DPU
// cluster DPUServices deployed in the host cluster, keep their upstream images: the host cluster
// is not air-gapped.
var imageOverrides = []imageOverride{
	{field: "spec.flannel.daemon.image", repository: flannelRepository, set: func(c *operatorv1.DPFOperatorConfig, img string) {
		f := ensureFlannel(c)
		if f.Daemon == nil {
			f.Daemon = &operatorv1.FlannelDaemon{}
		}
		f.Daemon.ImageComponentConfig = imageConfig(img)
	}},
	{field: "spec.flannel.cni.image", repository: flannelCNIRepository, set: func(c *operatorv1.DPFOperatorConfig, img string) {
		ensureFlannel(c).CNI = &operatorv1.FlannelCNI{ImageComponentConfig: imageConfig(img)}
	}},
	{field: "spec.multus.cni.image", repository: multusRepository, set: func(c *operatorv1.DPFOperatorConfig, img string) {
		if c.Spec.Multus == nil {
			c.Spec.Multus = &operatorv1.MultusConfiguration{}
		}
		setOverrides(&c.Spec.Multus.CNI, img)
	}},
	{field: "spec.sriovDevicePlugin.deviceplugin.image", repository: sriovDevicePluginRepository, set: func(c *operatorv1.DPFOperatorConfig, img string) {
		setOverrides(&ensureSRIOVDevicePlugin(c).DevicePlugin, img)
	}},
	{field: "spec.sriovDevicePlugin.configInit.image", repository: dpfSystemImageName, set: func(c *operatorv1.DPFOperatorConfig, img string) {
		setOverrides(&ensureSRIOVDevicePlugin(c).ConfigInit, img)
	}},
	{field: "spec.nvipam.node.image", repository: nvIPAMRepository, set: func(c *operatorv1.DPFOperatorConfig, img string) {
		n := ensureNVIPAM(c)
		if n.Node == nil {
			n.Node = &operatorv1.NVIPAMNode{}
		}
		n.Node.ImageComponentConfig = imageConfig(img)
	}},
	{field: "spec.sfcController.controller.image", repository: dpfSystemImageName, set: func(c *operatorv1.DPFOperatorConfig, img string) {
		if c.Spec.SFCController == nil {
			c.Spec.SFCController = &operatorv1.SFCControllerConfiguration{}
		}
		setOverrides(&c.Spec.SFCController.Controller, img)
	}},
	{field: "spec.cniInstaller.installer.image", repository: cniInstallerImageName, set: func(c *operatorv1.DPFOperatorConfig, img string) {
		if c.Spec.CNIInstaller == nil {
			c.Spec.CNIInstaller = &operatorv1.CNIInstallerConfiguration{}
		}
		setOverrides(&c.Spec.CNIInstaller.Installer, img)
	}},
	{field: "spec.monitoring.nodeProblemDetector.daemon.image", repository: nodeProblemDetectorRepository, set: func(c *operatorv1.DPFOperatorConfig, img string) {
		m := ensureMonitoring(c)
		if m.NodeProblemDetector == nil {
			m.NodeProblemDetector = &operatorv1.NodeProblemDetectorConfiguration{}
		}
		setOverrides(&m.NodeProblemDetector.Daemon, img)
	}},
	{field: "spec.monitoring.openTelemetryCollector.daemon.image", repository: otelCollectorRepository,
		// The collector is only deployed with its configuration; an empty one does not validate.
		applies: func(c *operatorv1.DPFOperatorConfig) bool {
			return c.Spec.Monitoring != nil && c.Spec.Monitoring.OpenTelemetryCollector != nil
		},
		set: func(c *operatorv1.DPFOperatorConfig, img string) {
			setOverrides(&c.Spec.Monitoring.OpenTelemetryCollector.Daemon, img)
		}},
	{field: "spec.security.kata.daemon.image", repository: kataDeployRepository, applies: kataEnabled,
		set: func(c *operatorv1.DPFOperatorConfig, img string) {
			c.Spec.Security.Kata.Daemon = &operatorv1.ImageComponentConfig{Image: ptr.To(img)}
		}},
	{field: "spec.security.spiffe.csiDriver.driver.image", repository: spiffeCSIDriverRepository, applies: spiffeEnabled,
		set: func(c *operatorv1.DPFOperatorConfig, img string) {
			ensureSPIFFECSIDriver(c).Driver = &operatorv1.ImageComponentConfig{Image: ptr.To(img)}
		}},
	{field: "spec.security.spiffe.csiDriver.nodeDriverRegistrar.image", repository: csiNodeDriverRegistrarRepo, applies: spiffeEnabled,
		set: func(c *operatorv1.DPFOperatorConfig, img string) {
			ensureSPIFFECSIDriver(c).NodeDriverRegistrar = &operatorv1.ImageComponentConfig{Image: ptr.To(img)}
		}},
}

// ApplyToDPFOperatorConfig points the images of the DPU-side components of cfg at their mirror
// copies, and the Kamaji DPU cluster addons at the mirror. It returns the fields it set, as
// field=value. An image the BOM does not list is an error: the mirror would not serve it.
func (m *Mirror) ApplyToDPFOperatorConfig(cfg *operatorv1.DPFOperatorConfig) ([]string, error) {
	var set []string
	var errs []error
	for _, o := range imageOverrides {
		if o.applies != nil && !o.applies(cfg) {
			continue
		}
		img, ok := m.bomImage(o.repository)
		if !ok {
			errs = append(errs, fmt.Errorf("%s: the BOM lists no DPU image of %s", o.field, o.repository))
			continue
		}
		o.set(cfg, img)
		set = append(set, o.field+"="+img)
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	addons, err := m.AddonsRepository()
	if err != nil {
		return nil, err
	}
	if cfg.Spec.KamajiClusterManager == nil {
		cfg.Spec.KamajiClusterManager = &operatorv1.KamajiClusterManagerConfiguration{}
	}
	if cfg.Spec.KamajiClusterManager.DPUClusterImages == nil {
		cfg.Spec.KamajiClusterManager.DPUClusterImages = &operatorv1.DPUClusterImagesConfiguration{}
	}
	cfg.Spec.KamajiClusterManager.DPUClusterImages.AddonsRepository = ptr.To(addons)
	set = append(set, "spec.kamajiClusterManager.dpuClusterImages.addonsRepository="+addons)
	clearDeprecatedImages(cfg)
	return set, nil
}

// clearDeprecatedImages clears the deprecated image fields the overrides replace: they cannot be set
// together with the new ones.
//
//nolint:staticcheck // it clears the deprecated fields
func clearDeprecatedImages(cfg *operatorv1.DPFOperatorConfig) {
	if cfg.Spec.Flannel != nil {
		cfg.Spec.Flannel.Images = nil
	}
	if cfg.Spec.Multus != nil {
		cfg.Spec.Multus.Image = nil
	}
	if cfg.Spec.SRIOVDevicePlugin != nil {
		cfg.Spec.SRIOVDevicePlugin.Image = nil
	}
	if cfg.Spec.SFCController != nil {
		cfg.Spec.SFCController.Image = nil
	}
	if cfg.Spec.NVIPAM != nil {
		cfg.Spec.NVIPAM.Image = nil
	}
}
