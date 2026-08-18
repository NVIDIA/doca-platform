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
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	dpuserviceutils "github.com/nvidia/doca-platform/internal/dpuservice/utils"
	"github.com/nvidia/doca-platform/internal/operator/utils"
	"github.com/nvidia/doca-platform/internal/release"
	"github.com/nvidia/doca-platform/pkg/conditions"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ Component = &fromDPUService{}

type fromDPUService struct {
	data       []byte
	name       operatorv1.ComponentName
	dpuService *unstructured.Unstructured
}

// dpuNetworkingSubCharts are the DPUServices that use the dpu-networking helm chart by default.
var dpuNetworkingSubCharts = map[operatorv1.ComponentName]bool{
	operatorv1.FlannelName:                true,
	operatorv1.ServiceSetControllerName:   true,
	operatorv1.MultusName:                 true,
	operatorv1.SRIOVDevicePluginName:      true,
	operatorv1.NVIPAMControllerName:       true,
	operatorv1.SFCControllerName:          true,
	operatorv1.CNIInstallerName:           true,
	operatorv1.KubeStateMetricsName:       true,
	operatorv1.DPUMonitoringName:          true,
	operatorv1.NodeProblemDetectorName:    true,
	operatorv1.OpenTelemetryCollectorName: true,
	operatorv1.KataContainersName:         true,
	operatorv1.SpireAgentRBACName:         true,
}

func (f *fromDPUService) Name() operatorv1.ComponentName {
	return f.name
}

func (f *fromDPUService) Parse() error {
	if f.data == nil {
		return fmt.Errorf("data for DPUService %s can not be empty", f.name)
	}

	objects, err := utils.BytesToUnstructured(f.data)
	if err != nil {
		return fmt.Errorf("error while converting DPUService %v manifest to object: %w", f.name, err)
	}

	for _, obj := range objects {
		if ObjectKind(obj.GetKind()) != DPUServiceKind {
			return fmt.Errorf("manifests for %s should only contain a DPUService object: found %v", f.name, obj.GetObjectKind().GroupVersionKind().Kind)
		}
	}

	if len(objects) != 1 {
		return fmt.Errorf("manifests for %s should contain exactly one DPUService. found %v", f.name, len(objects))
	}

	f.dpuService = objects[0]

	return nil
}

func (f *fromDPUService) GenerateManifests(_ context.Context, vars Variables) ([]client.Object, error) {
	ret := []client.Object{}
	if ok := vars.DisableSystemComponents[f.Name()]; ok {
		return nil, nil
	}

	labelsToAdd := map[string]string{
		operatorv1.DPFComponentLabelKey: f.Name().String(),
		release.DPFVersionLabelKey:      release.DPFVersion(),
		applysetPartOfLabel:             ApplySetID(vars.Namespace, f),
	}

	dpuServiceCopy, err := f.applyDPUServiceEdits(vars, labelsToAdd)
	if err != nil {
		return nil, fmt.Errorf("failed to apply DPUService edits: %w", err)
	}

	return append(ret, dpuServiceCopy), nil
}

// applyDPUServiceEdits creates a copy of the DPUService that is part of the object and prepares it for apply. It doesn't
// handle the ApplySet mechanism and expects the caller to handle that.
func (f *fromDPUService) applyDPUServiceEdits(vars Variables, labelsToAdd map[string]string) (*unstructured.Unstructured, error) {
	// copy object
	dpuServiceCopy := f.dpuService.DeepCopy()

	dpuServiceCopy.SetName(f.Name().String())

	// apply edits
	edits := NewEdits().AddForAll(
		NamespaceEdit(vars.Namespace),
		LabelsEdit(labelsToAdd))

	// Add the component label to ServiceDaemonSet.Labels for system components, so that the pods
	// running on DPU clusters from system DPUServices can be identified
	if componentName, ok := labelsToAdd[operatorv1.DPFComponentLabelKey]; ok {
		edits.AddForKindS(DPUServiceKind, dpuServiceSetServiceDaemonSetLabelEdit(operatorv1.DPFComponentLabelKey, componentName))
	}

	// Update resources from variables if possible.
	// Handle all resources for this component (both single and multi-container)
	for resourceKey, resourceReqs := range vars.Resources {
		// Check if this resource belongs to this component
		if resourceKey != f.Name().String() && !strings.HasPrefix(resourceKey, f.Name().String()+multiSplitChar) {
			continue
		}
		resourceEdits, err := resourceEditsForComponent(resourceKey, resourceReqs)
		if err != nil {
			return nil, err
		}
		for _, edit := range resourceEdits {
			edits.AddForKindS(DPUServiceKind, edit)
		}
	}

	// The DPUNetworking helm chart has all components disabled by default. Enable this DPUService in the helm chart values.
	if _, ok := dpuNetworkingSubCharts[f.Name()]; ok {
		edits.AddForKindS(DPUServiceKind, dpuServiceAddValueEdit(true, f.Name().String(), "enabled"))
	}

	// Add the helm chart.
	helmChartString, ok := vars.HelmCharts[f.Name()]
	if !ok {
		return nil, fmt.Errorf("could not find helm chart source for DPUService %s", f.Name())
	}
	edits.AddForKindS(DPUServiceKind, dpuServiceSetHelmChartEdit(helmChartString))

	// Update the image from variables if possible.
	// Handle all images for this component (both single and multi-container)
	for imageKey, imageString := range vars.Images {
		if imageString == "" {
			continue
		}
		// Check if this image belongs to this component
		if imageKey != f.Name().String() && !strings.HasPrefix(imageKey, f.Name().String()+multiSplitChar) {
			continue
		}
		imageEdits, err := imageEditsForComponent(imageKey, imageString)
		if err != nil {
			return nil, err
		}
		for _, edit := range imageEdits {
			edits.AddForKindS(DPUServiceKind, edit)
		}
	}

	if vars.ImagePullSecrets != nil {
		secrets := pullSecretValueFromStrings(vars.ImagePullSecrets...)
		edits.AddForKindS(DPUServiceKind, dpuServiceAddValueEdit(secrets, f.Name().String(), "imagePullSecrets"))
	}

	// Propagate replicas from DPFOperatorConfig.Spec.<Component>.Replicas into the helm
	// chart values at controllerManager.replicas.
	if replicas, exists := vars.Replicas[f.Name()]; exists && replicas != nil {
		edits.AddForKindS(DPUServiceKind, dpuServiceAddValueEdit(*replicas, f.Name().String(), "controllerManager", "replicas"))
	}

	// Update the networking values from variables if possible.
	networkingEdits := networkEditsForComponent(f.Name(), vars.Networking)
	for _, edit := range networkingEdits {
		edits.AddForKindS(DPUServiceKind, edit)
	}

	// Add any additional values that might be required by the DPUService
	additionalValuesEdits, err := additionalValuesForComponent(f.Name(), vars)
	if err != nil {
		return nil, err
	}

	for _, edit := range additionalValuesEdits {
		edits.AddForKindS(DPUServiceKind, edit)
	}

	// Apply the edits.
	if err := edits.Apply([]*unstructured.Unstructured{dpuServiceCopy}); err != nil {
		return nil, err
	}

	return dpuServiceCopy, nil
}

func pullSecretValueFromStrings(names ...string) []interface{} {
	pullSecrets := make([]interface{}, 0, len(names))
	for _, name := range names {
		pullSecrets = append(pullSecrets, map[string]interface{}{"name": name})
	}
	return pullSecrets
}

func additionalValuesForComponent(name operatorv1.ComponentName, vars Variables) ([]StructuredEdit, error) {
	switch name {
	case operatorv1.MultusName:
		return multusEdits(vars)
	case operatorv1.SFCControllerName:
		return sfcControllerEdits(vars)
	case operatorv1.NVIPAMControllerName:
		return nvipamEdits(vars)
	case operatorv1.FlannelName:
		return flannelEdits(vars)
	case operatorv1.CNIInstallerName:
		return cniInstallerEdits(vars)
	case operatorv1.OpenTelemetryCollectorName:
		return openTelemetryCollectorEdits(vars)
	case operatorv1.KataContainersName:
		return kataContainersEdits(vars)
	// Other DPUServices do not need additional values.
	default:
		return nil, nil
	}
}

func sfcControllerEdits(vars Variables) ([]StructuredEdit, error) {
	edits := []StructuredEdit{
		dpuServiceAddValueEdit(vars.DPUOpenvSwitchRunPath, operatorv1.SFCControllerName.String(), openvSwitchRunDirPathKey),
		dpuServiceAddValueEdit(vars.DPUOpenvSwitchBinPath, operatorv1.SFCControllerName.String(), openvSwitchBinDirPathKey),
		dpuServiceAddValueEdit(vars.DPUOpenvSwitchSharedLibPath, operatorv1.SFCControllerName.String(), openvSwitchSharedLibraryDirPathKey),
		dpuServiceAddValueEdit(vars.SFCController.SecureFlowDeletionTimeout.String(), operatorv1.SFCControllerName.String(), "controllerManager", "manager", "secureFlowDeletionTimeout"),
	}
	if vars.DPUOpenvSwitchSharedLib64Path != nil {
		edits = append(edits, dpuServiceAddValueEdit(*vars.DPUOpenvSwitchSharedLib64Path, operatorv1.SFCControllerName.String(), openvSwitchSharedLibrary64DirPathKey))
	}
	if vars.DPULinkerCachePath != nil {
		edits = append(edits, dpuServiceAddValueEdit(*vars.DPULinkerCachePath, operatorv1.SFCControllerName.String(), dpuLinkerCachePathKey))
	}
	if vars.DPUOptLibraryPath != nil {
		edits = append(edits, dpuServiceAddValueEdit(*vars.DPUOptLibraryPath, operatorv1.SFCControllerName.String(), dpuOptLibraryPathKey))
	}
	return edits, nil
}

func cniInstallerEdits(vars Variables) ([]StructuredEdit, error) {
	edits := []StructuredEdit{
		dpuServiceAddValueEdit(vars.DPUCNIBinPath, operatorv1.CNIInstallerName.String(), cniBinDirPathKey),
	}
	return edits, nil
}

func kataContainersEdits(vars Variables) ([]StructuredEdit, error) {
	edits := []StructuredEdit{}

	shims := vars.KataContainers.Shims
	for _, shim := range shims {
		edits = append(edits, dpuServiceAddValueEdit(true, operatorv1.KataContainersName.String(), "shims", shim, "enabled"))
	}
	if len(shims) == 0 {
		edits = append(edits, dpuServiceAddValueEdit(true, operatorv1.KataContainersName.String(), "shims", string(operatorv1.KataShimQEMU), "enabled"))
	}

	configFileName := vars.KataContainers.ContainerdConfigFileName
	if configFileName != "" {
		edits = append(edits, dpuServiceAddValueEdit(configFileName, operatorv1.KataContainersName.String(), "containerd", "configFileName"))
	}

	for k, v := range vars.KataContainers.NodeSelector {
		edits = append(edits, dpuServiceAddValueEdit(v, operatorv1.KataContainersName.String(), "nodeSelector", k))
	}

	return edits, nil
}

func nvipamEdits(vars Variables) ([]StructuredEdit, error) {
	return []StructuredEdit{
		dpuServiceAddValueEdit(vars.DPUCNIBinPath, operatorv1.NVIPAMControllerName.String(), cniBinDirPathKey),
		dpuServiceAddValueEdit(vars.DPUCNIConfPath, operatorv1.NVIPAMControllerName.String(), cniConfDirPathKey),
	}, nil
}

func openTelemetryCollectorEdits(vars Variables) ([]StructuredEdit, error) {
	edits := []StructuredEdit{}

	if vars.OpenTelemetryCollector.Logging.Endpoint != "" {
		edits = append(edits, dpuServiceAddValueEdit(vars.OpenTelemetryCollector.Logging.Endpoint,
			operatorv1.OpenTelemetryCollectorName.String(), "logging", "endpoint"))
		if vars.OpenTelemetryCollector.Logging.Transport != "" {
			edits = append(edits, dpuServiceAddValueEdit(vars.OpenTelemetryCollector.Logging.Transport,
				operatorv1.OpenTelemetryCollectorName.String(), "logging", "transport"))
		}
		if vars.OpenTelemetryCollector.Logging.CACert != "" {
			edits = append(edits, dpuServiceAddValueEdit(vars.OpenTelemetryCollector.Logging.CACert,
				operatorv1.OpenTelemetryCollectorName.String(), "logging", "caCert"))
		}
	}

	if vars.OpenTelemetryCollector.Metrics.Endpoint != "" {
		edits = append(edits, dpuServiceAddValueEdit(vars.OpenTelemetryCollector.Metrics.Endpoint,
			operatorv1.OpenTelemetryCollectorName.String(), "metrics", "endpoint"))
		if vars.OpenTelemetryCollector.Metrics.Transport != "" {
			edits = append(edits, dpuServiceAddValueEdit(vars.OpenTelemetryCollector.Metrics.Transport,
				operatorv1.OpenTelemetryCollectorName.String(), "metrics", "transport"))
		}
		if vars.OpenTelemetryCollector.Metrics.CACert != "" {
			edits = append(edits, dpuServiceAddValueEdit(vars.OpenTelemetryCollector.Metrics.CACert,
				operatorv1.OpenTelemetryCollectorName.String(), "metrics", "caCert"))
		}
	}

	return edits, nil
}

func multusEdits(vars Variables) ([]StructuredEdit, error) {
	return []StructuredEdit{
		dpuServiceAddValueEdit(vars.DPUCNIBinPath, operatorv1.MultusName.String(), cniBinDirPathKey),
		dpuServiceAddValueEdit(vars.DPUCNIConfPath, operatorv1.MultusName.String(), cniConfDirPathKey),
	}, nil
}

func perClusterEdits(chartName string, secretName string, serviceAccountName string, labels map[string]string) []StructuredEdit {
	edits := []StructuredEdit{}

	// Create the projected volume configuration for the tokenfile
	tokenfileVolume := []corev1.Volume{
		{
			Name: "tokenfile",
			VolumeSource: corev1.VolumeSource{
				Projected: &corev1.ProjectedVolumeSource{
					Sources: []corev1.VolumeProjection{
						{
							Secret: &corev1.SecretProjection{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: secretName,
								},
								Items: []corev1.KeyToPath{
									{
										Key:  "TOKEN_FILE",
										Path: "token",
									},
								},
							},
						},
						{
							Secret: &corev1.SecretProjection{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: secretName,
								},
								Items: []corev1.KeyToPath{
									{
										Key:  "KUBERNETES_CA_DATA",
										Path: "ca.crt",
									},
								},
							},
						},
					},
				},
			},
		},
	}

	// Create the environment variable configuration for Kubernetes API access
	envVars := []corev1.EnvVar{
		{
			Name: "KUBERNETES_SERVICE_HOST",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: secretName,
					},
					Key: "KUBERNETES_SERVICE_HOST",
				},
			},
		},
		{
			Name: "KUBERNETES_SERVICE_PORT",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: secretName,
					},
					Key: "KUBERNETES_SERVICE_PORT",
				},
			},
		},
	}

	// Create the volume mounts configuration for the tokenfile
	volumeMounts := []corev1.VolumeMount{
		{
			Name:      "tokenfile",
			MountPath: "/var/run/secrets/kubernetes.io/serviceaccount",
			ReadOnly:  true,
		},
	}

	edits = append(edits,
		dpuServiceInClusterEdit(true),
		dpuServiceAddValueEdit(serviceAccountName, chartName, "serviceAccount", "name"),
		dpuServiceAddValueEdit(true, chartName, "deployHostManifests"),
		dpuServiceAddValueEdit(tokenfileVolume, chartName, "volumes"),
		dpuServiceAddValueEdit(envVars, chartName, "env"),
		dpuServiceAddValueEdit(volumeMounts, chartName, "volumeMounts"),
	)

	if len(labels) > 0 {
		edits = append(edits, dpuServiceAddValueEdit(labels, chartName, "serviceAccount", "labels"))
	}
	return edits
}

func rbacAndCRDEdits(chartName string, serviceAccounts []types.NamespacedName) []StructuredEdit {
	// Convert []types.NamespacedName to []interface{} with lowercase keys for Helm chart compatibility
	serviceAccountList := make([]interface{}, 0, len(serviceAccounts))
	for _, sa := range serviceAccounts {
		serviceAccountList = append(serviceAccountList, map[string]interface{}{
			"name":      sa.Name,
			"namespace": sa.Namespace,
		})
	}

	edits := []StructuredEdit{}
	edits = append(edits,
		dpuServiceAddValueEdit(true, chartName, "deployDPUManifests"),
		dpuServiceAddValueEdit(serviceAccountList, chartName, "rbac", "serviceAccounts"),
		dpuServiceInClusterEdit(false),
	)
	return edits
}

func flannelEdits(vars Variables) ([]StructuredEdit, error) {
	if _, _, err := net.ParseCIDR(vars.FlannelPodCIDR); err != nil {
		return nil, fmt.Errorf("invalid flannel pod CIDR: %s %w", vars.FlannelPodCIDR, err)
	}
	return []StructuredEdit{
		// flannel has an additional "flannel" structure inside its helm chart values.
		dpuServiceAddValueEdit(vars.DPUCNIBinPath, operatorv1.FlannelName.String(), "flannel", cniBinDirPathKey),
		dpuServiceAddValueEdit(vars.DPUCNIConfPath, operatorv1.FlannelName.String(), "flannel", cniConfDirPathKey),
		dpuServiceAddValueEdit(vars.FlannelSkipCNIConfigInstallation, operatorv1.FlannelName.String(), "flannel", skipCNIConfigInstallationKey),
		dpuServiceAddValueEdit(vars.FlannelPodCIDR, operatorv1.FlannelName.String(), "podCidr"),
	}, nil
}

func dpuServiceSetHelmChartEdit(helmChart string) StructuredEdit {
	return func(obj client.Object) error {
		dpuService, ok := obj.(*dpuservicev1.DPUService)
		if !ok {
			return fmt.Errorf("unexpected object kind %s. expected DPUService", obj.GetObjectKind().GroupVersionKind())
		}

		chart, err := ParseHelmChartString(helmChart)
		if err != nil {
			return fmt.Errorf("failed parsing %s: %w", dpuService.Name, err)
		}

		dpuService.Spec.HelmChart.Source.Chart = chart.Chart
		dpuService.Spec.HelmChart.Source.RepoURL = chart.Repo
		dpuService.Spec.HelmChart.Source.Version = chart.Version
		return nil
	}
}

func dpuServiceAddValueEdit(value interface{}, key ...string) StructuredEdit {
	return func(obj client.Object) error {
		dpuService, ok := obj.(*dpuservicev1.DPUService)
		if !ok {
			return fmt.Errorf("unexpected object kind %s. expected DPUService", obj.GetObjectKind().GroupVersionKind())
		}

		if dpuService.Spec.HelmChart.Values == nil {
			dpuService.Spec.HelmChart.Values = &runtime.RawExtension{}
		}

		currentValues := make(map[string]interface{})
		if dpuService.Spec.HelmChart.Values.Raw != nil {
			if err := json.Unmarshal(dpuService.Spec.HelmChart.Values.Raw, &currentValues); err != nil {
				return fmt.Errorf("error unmarshaling current values: %w", err)
			}
		} else if dpuService.Spec.HelmChart.Values.Object != nil {
			uns, ok := dpuService.Spec.HelmChart.Values.Object.(*unstructured.Unstructured)
			if !ok {
				return fmt.Errorf("could not treat values object field as unstructured")
			}
			currentValues = uns.UnstructuredContent()
		}

		// Create the new value to merge
		newValue := make(map[string]interface{})
		current := newValue
		for _, k := range key[:len(key)-1] {
			current[k] = make(map[string]interface{})
			current = current[k].(map[string]interface{})
		}
		current[key[len(key)-1]] = value

		// Use the existing MergeMaps utility to safely merge the values
		mergedValues := dpuserviceutils.MergeMaps(newValue, currentValues)

		// Update the DPUService
		dpuService.Spec.HelmChart.Values.Object = &unstructured.Unstructured{Object: mergedValues}
		dpuService.Spec.HelmChart.Values.Raw = nil

		return nil
	}
}

func dpuServiceInClusterEdit(deployInCluster bool) StructuredEdit {
	return func(obj client.Object) error {
		dpuService, ok := obj.(*dpuservicev1.DPUService)
		if !ok {
			return fmt.Errorf("unexpected object kind %s. expected DPUService", obj.GetObjectKind().GroupVersionKind())
		}
		dpuService.Spec.DeployInCluster = ptr.To(deployInCluster)
		if deployInCluster {
			// Unset security.privileged when deployInCluster is true.
			// Otherwise the DPUService will not pass validation.
			dpuService.Spec.Security = nil
		}
		return nil
	}
}

// IsReadyForUpgrade returns an error if the DPUService does not have a Ready status condition and the version is not updated.
func (f *fromDPUService) IsReadyForUpgrade(ctx context.Context, c client.Client, config *operatorv1.DPFOperatorConfig) error {
	shouldSkip, err := ShouldSkipUpgradeCheck(f.Name(), *config.Status.Version)
	if err != nil {
		return fmt.Errorf("determine if component %s should skip upgrade check: %w", f.Name(), err)
	}
	if shouldSkip {
		return nil
	}
	return f.isReady(ctx, c, config.GetNamespace(), false)
}

// IsReady returns an error if the DPUService does not have a Ready status condition.
func (f *fromDPUService) IsReady(ctx context.Context, c client.Client, namespace string) error {
	return f.isReady(ctx, c, namespace, true)
}

func (f *fromDPUService) isReady(ctx context.Context, c client.Client, namespace string, versionValidation bool) error {
	obj := &dpuservicev1.DPUService{}
	err := c.Get(ctx, client.ObjectKey{Name: f.Name().String(), Namespace: namespace}, obj)
	if err != nil {
		return err
	}

	if versionValidation {
		if obj.GetLabels()[release.DPFVersionLabelKey] != "" && obj.GetLabels()[release.DPFVersionLabelKey] != release.DPFVersion() {
			return fmt.Errorf("DPUService %s/%s has version %s, want %s",
				obj.GetNamespace(), obj.GetName(), obj.GetLabels()[release.DPFVersionLabelKey], release.DPFVersion())
		}
	}

	if !conditions.IsTrue(obj, conditions.TypeReady) {
		return fmt.Errorf("DPUService %s/%s is not ready", obj.Namespace, obj.Name)
	}
	return nil
}

type HelmChartSource struct {
	Repo    string
	Chart   string
	Version string
}

func ParseHelmChartString(repoChartVersion string) (*HelmChartSource, error) {
	versionStart := strings.LastIndex(repoChartVersion, ":")

	if versionStart == -1 {
		return nil, fmt.Errorf("failed to parse helm chart source: invalid format %s", repoChartVersion)
	}
	version := repoChartVersion[versionStart+1:]

	repoChart := repoChartVersion[:versionStart]
	imageStart := strings.LastIndex(repoChart, "/")
	if imageStart == -1 {
		return nil, fmt.Errorf("failed to parse helm chart source: invalid format %s", repoChartVersion)
	}

	image := repoChart[imageStart+1:]
	repo := repoChart[:imageStart]

	return &HelmChartSource{
		Version: version,
		Chart:   image,
		Repo:    repo,
	}, nil
}

func networkEditsForComponent(name operatorv1.ComponentName, networking Networking) []StructuredEdit {
	edits := map[operatorv1.ComponentName][]StructuredEdit{
		operatorv1.FlannelName: setFlannelMTUEdit(networking),
		operatorv1.MultusName:  setMultusMTUEdit(networking),
	}
	return edits[name]
}

func setFlannelMTUEdit(networking Networking) []StructuredEdit {
	mtu := strconv.Itoa(networking.ControlPlaneMTU)
	return []StructuredEdit{
		dpuServiceAddValueEdit(mtu, operatorv1.FlannelName.String(), "flannel", "mtu"),
	}
}

func setMultusMTUEdit(networking Networking) []StructuredEdit {
	mtu := strconv.Itoa(networking.HighSpeedMTU)
	return []StructuredEdit{
		dpuServiceAddValueEdit(mtu, operatorv1.MultusName.String(), "mtu"),
	}
}

type image struct {
	repoImage string
	tag       string
}

// Image will be in the form: repoName/imageName:version.
func parseImageString(repoImageVersion string) (*image, error) {
	repoImage, tag, found := strings.Cut(repoImageVersion, ":")
	if !found {
		return nil, fmt.Errorf("image must be in the format 'image:tag' and must always contain a colon. input: %v", repoImageVersion)
	}
	return &image{
		repoImage: repoImage,
		tag:       tag,
	}, nil
}

// imageEditsForComponent contains the correct functions to set images in each of the components deployed by covered by the DPUNetworking helm chart.
// imageEditsForComponent generates image edits using the configurable path system
func imageEditsForComponent(name string, imageOverride string) ([]StructuredEdit, error) {
	// Handle legacy comma-delimited format for multi-container components
	// This handles cases like "flannel" with "image1,image2" where the first image is for daemon, second for cni
	// TODO: remove this special case when we remove the legacy format support.
	if name == operatorv1.FlannelName.String() {
		images := []*image{}
		for _, override := range strings.Split(imageOverride, ",") {
			i, err := parseImageString(override)
			if err != nil {
				return nil, err
			}
			images = append(images, i)
		}
		edits := map[string]func(...*image) []StructuredEdit{
			operatorv1.FlannelName.String(): setFlannelImage,
		}
		editForComponent, ok := edits[name]
		if !ok {
			return nil, fmt.Errorf("failed to find image edit for component %q", name)
		}
		return editForComponent(images...), nil
	}

	imageName, err := parseImageString(imageOverride)
	if err != nil {
		return nil, fmt.Errorf("failed to parse image string %q: %v", imageOverride, err)
	}

	// Handle multi-container component images
	if strings.Contains(name, multiSplitChar) {
		// Extract base component name and container name
		parts := strings.SplitN(name, multiSplitChar, 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid multi-container image name format: %q", name)
		}
		componmentName := operatorv1.ComponentName(parts[0])
		containerName := operatorv1.ContainerName(parts[1])

		// Use the configurable path system for multi-container components
		return generateImageEditsFromPaths(componmentName, containerName, imageName)
	}

	// Handle single container components
	// TODO: delete with v26.1.0 after the deprecation period.
	containerName := getContainerNameFromComponent(operatorv1.ComponentName(name))
	return generateImageEditsFromPaths(operatorv1.ComponentName(name), containerName, imageName)
}

func setFlannelImage(imageOverride ...*image) []StructuredEdit {
	kubeFlannelImage := imageOverride[0]
	cniImage := imageOverride[1]
	repoPath := []string{operatorv1.FlannelName.String(), "flannel", "image", "repository"}
	tagPath := []string{operatorv1.FlannelName.String(), "flannel", "image", "tag"}
	repoPathCNI := []string{operatorv1.FlannelName.String(), "flannel", "image_cni", "repository"}
	tagPathCNI := []string{operatorv1.FlannelName.String(), "flannel", "image_cni", "tag"}

	return []StructuredEdit{
		dpuServiceAddValueEdit(kubeFlannelImage.repoImage, repoPath...),
		dpuServiceAddValueEdit(kubeFlannelImage.tag, tagPath...),
		dpuServiceAddValueEdit(cniImage.repoImage, repoPathCNI...),
		dpuServiceAddValueEdit(cniImage.tag, tagPathCNI...),
	}
}

// generateImageEditsFromPaths generates image edits using the configurable path system
func generateImageEditsFromPaths(componentName operatorv1.ComponentName, containerName operatorv1.ContainerName, image *image) ([]StructuredEdit, error) {
	if image == nil {
		return nil, fmt.Errorf("no images provided for component %q", componentName)
	}

	// Get the component configuration
	componentConfig, exists := helmPaths().getPath(componentName)
	if !exists {
		// If no component paths are configured, return empty edits (component doesn't support image overrides)
		return []StructuredEdit{}, nil
	}

	// Get the specific container configuration
	containerConfig, exists := componentConfig[containerName]
	if !exists {
		// If no container paths are configured, return empty edits (container doesn't support image overrides)
		return []StructuredEdit{}, nil
	}

	// Use the first image (for single container) or specific image for multi-container

	// Build the full paths by prepending the component name
	repoPath := append([]string{componentName.String()}, containerConfig.Repository...)
	tagPath := append([]string{componentName.String()}, containerConfig.Tag...)

	return []StructuredEdit{
		dpuServiceAddValueEdit(image.repoImage, repoPath...),
		dpuServiceAddValueEdit(image.tag, tagPath...),
	}, nil
}

// resourceEditsForComponent generates resource edits using the configurable path system
func resourceEditsForComponent(name string, resourceReqs corev1.ResourceRequirements) ([]StructuredEdit, error) {
	// Check if resources are set (either requests or limits)
	if len(resourceReqs.Requests) == 0 && len(resourceReqs.Limits) == 0 {
		return nil, nil
	}

	// Handle multi-container component resources
	parts := strings.SplitN(name, multiSplitChar, 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid multi-container resource name format: %q", name)
	}
	componentName := operatorv1.ComponentName(parts[0])
	containerName := operatorv1.ContainerName(parts[1])

	// Use the configurable path system for multi-container components
	return generateResourceEditsFromPaths(componentName, containerName, resourceReqs)
}

// generateResourceEditsFromPaths generates resource edits using the configurable path system
func generateResourceEditsFromPaths(
	componentName operatorv1.ComponentName,
	containerName operatorv1.ContainerName,
	resourceReqs corev1.ResourceRequirements,
) ([]StructuredEdit, error) {
	// Get the component configuration
	componentConfig, exists := helmPaths().getPath(componentName)
	if !exists {
		// If no component paths are configured, return empty edits (component doesn't support resource overrides)
		return []StructuredEdit{}, nil
	}

	helmPath, exists := componentConfig[containerName]
	if !exists {
		// If no container paths are configured, return empty edits.
		return []StructuredEdit{}, nil
	}

	resourcePath := append([]string{componentName.String()}, helmPath.Resources...)
	return []StructuredEdit{
		dpuServiceAddValueEdit(resourceReqs, resourcePath...),
	}, nil
}

// dpuServiceSetServiceDaemonSetLabelEdit adds a label to the ServiceDaemonSet labels of a DPUService.
func dpuServiceSetServiceDaemonSetLabelEdit(key, value string) StructuredEdit {
	return func(obj client.Object) error {
		dpuService, ok := obj.(*dpuservicev1.DPUService)
		if !ok {
			return fmt.Errorf("unexpected object kind %s. expected DPUService", obj.GetObjectKind().GroupVersionKind())
		}
		if dpuService.Spec.ServiceDaemonSet == nil {
			dpuService.Spec.ServiceDaemonSet = &dpuservicev1.ServiceDaemonSetValues{}
		}
		if dpuService.Spec.ServiceDaemonSet.Labels == nil {
			dpuService.Spec.ServiceDaemonSet.Labels = map[string]string{}
		}
		dpuService.Spec.ServiceDaemonSet.Labels[key] = value
		return nil
	}
}
