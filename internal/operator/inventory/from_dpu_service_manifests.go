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
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ Component = &fromDPUService{}

type fromDPUService struct {
	data       []byte
	name       string
	dpuService *unstructured.Unstructured
}

// dpuNetworkingSubCharts are the DPUServices that use the dpu-networking helm chart by default.
var dpuNetworkingSubCharts = map[string]bool{
	operatorv1.FlannelName:              true,
	operatorv1.ServiceSetControllerName: true,
	operatorv1.MultusName:               true,
	operatorv1.SRIOVDevicePluginName:    true,
	operatorv1.OVSCNIName:               true,
	operatorv1.NVIPAMName:               true,
	operatorv1.SFCControllerName:        true,
	operatorv1.CNIInstallerName:         true,
}

func (f *fromDPUService) Name() string {
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

func (f *fromDPUService) GenerateManifests(vars Variables, options ...GenerateManifestOption) ([]client.Object, error) {
	ret := []client.Object{}
	opts := &GenerateManifestOptions{}
	for _, option := range options {
		option.Apply(opts)
	}
	if ok := vars.DisableSystemComponents[f.Name()]; ok {
		return nil, nil
	}

	labelsToAdd := map[string]string{
		operatorv1.DPFComponentLabelKey: f.Name(),
		release.DPFVersionLabelKey:      release.DPFVersion(),
	}
	applySetID := ApplySetID(vars.Namespace, f)
	// Add the ApplySet labels to the manifests unless disabled.
	if !opts.skipApplySet {
		labelsToAdd[applysetPartOfLabel] = applySetID
	}

	dpuServiceCopy, err := f.applyDPUServiceEdits(vars, labelsToAdd)
	if err != nil {
		return nil, fmt.Errorf("failed to apply DPUService edits: %w", err)
	}

	// Add the ApplySet to the manifests if this hasn't been disabled.
	if !opts.skipApplySet {
		ret = append(ret, applySetParentForComponent(f, applySetID, vars, applySetInventoryString(dpuServiceCopy)))
	}
	return append(ret, dpuServiceCopy), nil
}

// applyDPUServiceEdits creates a copy of the DPUService that is part of the object and prepares it for apply. It doesn't
// handle the ApplySet mechanism and expects the caller to handle that.
func (f *fromDPUService) applyDPUServiceEdits(vars Variables, labelsToAdd map[string]string) (*unstructured.Unstructured, error) {
	// copy object
	dpuServiceCopy := f.dpuService.DeepCopy()

	dpuServiceCopy.SetName(f.Name())

	// apply edits
	edits := NewEdits().AddForAll(
		NamespaceEdit(vars.Namespace),
		LabelsEdit(labelsToAdd))

	// Update resources from variables if possible.
	// Handle all resources for this component (both single and multi-container)
	for resourceKey, resourceReqs := range vars.Resources {
		// Check if this resource belongs to this component
		if resourceKey != f.Name() && !strings.HasPrefix(resourceKey, f.Name()+multiSplitChar) {
			continue
		}
		resourceEdits, err := resourceEditsForComponent(resourceKey, f.Name(), resourceReqs)
		if err != nil {
			return nil, err
		}
		for _, edit := range resourceEdits {
			edits.AddForKindS(DPUServiceKind, edit)
		}
	}

	// Add deployInCluster if the value is set to true in the DPFOperatorConfig.
	edits.AddForKindS(DPUServiceKind, dpuServiceInClusterEdit(vars.DeployInCluster[f.Name()]))
	// The DPUNetworking helm chart has all components disabled by default. Enable this DPUService in the helm chart values.
	if _, ok := dpuNetworkingSubCharts[f.Name()]; ok {
		edits.AddForKindS(DPUServiceKind, dpuServiceAddValueEdit(true, f.Name(), "enabled"))
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
		if imageKey != f.Name() && !strings.HasPrefix(imageKey, f.Name()+multiSplitChar) {
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
		edits.AddForKindS(DPUServiceKind, dpuServiceAddValueEdit(secrets, f.Name(), "imagePullSecrets"))
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

func additionalValuesForComponent(name string, vars Variables) ([]StructuredEdit, error) {
	switch name {
	// The ServiceSet controller is deployed in the target cluster but operates against the DPUCluster.
	// It deploys an additional DPUService which requires the helm chart details to be set.
	case operatorv1.ServiceSetControllerName:
		return serviceSetControllerEdits(vars)
	case operatorv1.MultusName:
		return multusEdits(vars)
	case operatorv1.OVSCNIName:
		return ovsCNIEdits(vars)
	case operatorv1.SFCControllerName:
		return sfcControllerEdits(vars)
	case operatorv1.NVIPAMName:
		return nvipamEdits(vars)
	case operatorv1.FlannelName:
		return flannelEdits(vars)
	// Other DPUServices do not need additional values.
	default:
		return nil, nil
	}
}

func sfcControllerEdits(vars Variables) ([]StructuredEdit, error) {
	edits := []StructuredEdit{
		dpuServiceAddValueEdit(vars.DPUOpenvSwitchRunPath, operatorv1.SFCControllerName, openvSwitchRunDirPathKey),
		dpuServiceAddValueEdit(vars.DPUOpenvSwitchBinPath, operatorv1.SFCControllerName, openvSwitchBinDirPathKey),
		dpuServiceAddValueEdit(vars.DPUOpenvSwitchSharedLibPath, operatorv1.SFCControllerName, openvSwitchSharedLibraryDirPathKey),
		dpuServiceAddValueEdit(vars.SFCController.SecureFlowDeletionTimeout.String(), operatorv1.SFCControllerName, "controllerManager", "manager", "secureFlowDeletionTimeout"),
	}
	// Only add lib64 path if it's configured
	if vars.DPUOpenvSwitchSharedLib64Path != nil {
		edits = append(edits, dpuServiceAddValueEdit(*vars.DPUOpenvSwitchSharedLib64Path, operatorv1.SFCControllerName, openvSwitchSharedLibrary64DirPathKey))
	}
	return edits, nil
}

func ovsCNIEdits(vars Variables) ([]StructuredEdit, error) {
	return []StructuredEdit{
		dpuServiceAddValueEdit(vars.DPUCNIBinPath, operatorv1.OVSCNIName, cniBinDirPathKey),
		dpuServiceAddValueEdit(vars.DPUOpenvSwitchRunPath, operatorv1.OVSCNIName, openvSwitchRunDirPathKey),
	}, nil
}

func nvipamEdits(vars Variables) ([]StructuredEdit, error) {
	return []StructuredEdit{
		dpuServiceAddValueEdit(vars.DPUCNIBinPath, operatorv1.NVIPAMName, cniBinDirPathKey),
		dpuServiceAddValueEdit(vars.DPUCNIConfPath, operatorv1.NVIPAMName, cniConfDirPathKey),
	}, nil
}

func multusEdits(vars Variables) ([]StructuredEdit, error) {
	return []StructuredEdit{
		dpuServiceAddValueEdit(vars.DPUCNIBinPath, operatorv1.MultusName, cniBinDirPathKey),
		dpuServiceAddValueEdit(vars.DPUCNIConfPath, operatorv1.MultusName, cniConfDirPathKey),
	}, nil
}

func serviceSetControllerEdits(vars Variables) ([]StructuredEdit, error) {
	edits := []StructuredEdit{}
	if len(vars.DPUClusters) > 1 {
		return nil, fmt.Errorf("servicechainset controller does not handle multiple DPUClusters")
	}
	// Set the Name and Namespace of the DPUCluster for the DPUServiceCredentialRequest.
	for _, cluster := range vars.DPUClusters {
		edits = append(edits,
			dpuServiceAddValueEdit(cluster.Cluster.Name, operatorv1.ServiceSetControllerName, "dpucluster", "name"),
			dpuServiceAddValueEdit(cluster.Cluster.Namespace, operatorv1.ServiceSetControllerName, "dpucluster", "namespace"),
		)
	}
	chart, err := ParseHelmChartString(vars.HelmCharts[operatorv1.ServiceSetControllerName])
	if err != nil {
		return nil, err
	}
	edits = append(edits,
		dpuServiceAddValueEdit(chart.Repo, operatorv1.ServiceSetControllerName, "chart", "repoURL"),
		dpuServiceAddValueEdit(chart.Chart, operatorv1.ServiceSetControllerName, "chart", "chart"),
		dpuServiceAddValueEdit(chart.Version, operatorv1.ServiceSetControllerName, "chart", "version"),
	)
	return edits, nil
}

func flannelEdits(vars Variables) ([]StructuredEdit, error) {
	if _, _, err := net.ParseCIDR(vars.FlannelPodCIDR); err != nil {
		return nil, fmt.Errorf("invalid flannel pod CIDR: %s %w", vars.FlannelPodCIDR, err)
	}
	return []StructuredEdit{
		// flannel has an additional "flannel" structure inside its helm chart values.
		dpuServiceAddValueEdit(vars.DPUCNIBinPath, operatorv1.FlannelName, "flannel", cniBinDirPathKey),
		dpuServiceAddValueEdit(vars.DPUCNIConfPath, operatorv1.FlannelName, "flannel", cniConfDirPathKey),
		dpuServiceAddValueEdit(vars.FlannelSkipCNIConfigInstallation, operatorv1.FlannelName, "flannel", skipCNIConfigInstallationKey),
		dpuServiceAddValueEdit(vars.FlannelPodCIDR, operatorv1.FlannelName, "podCidr"),
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
		return nil
	}
}

// IsReadyForUpgrade returns an error if the DPUService does not have a Ready status condition and the version is not updated.
func (f *fromDPUService) IsReadyForUpgrade(ctx context.Context, c client.Client, config *operatorv1.DPFOperatorConfig) error {
	return f.isReady(ctx, c, config.GetNamespace(), false)
}

// IsReady returns an error if the DPUService does not have a Ready status condition.
func (f *fromDPUService) IsReady(ctx context.Context, c client.Client, namespace string) error {
	return f.isReady(ctx, c, namespace, true)
}

func (f *fromDPUService) isReady(ctx context.Context, c client.Client, namespace string, versionValidation bool) error {
	obj := &dpuservicev1.DPUService{}
	err := c.Get(ctx, client.ObjectKey{Name: f.Name(), Namespace: namespace}, obj)
	if err != nil {
		return err
	}

	if versionValidation {
		if obj.GetLabels()[release.DPFVersionLabelKey] != "" && obj.GetLabels()[release.DPFVersionLabelKey] != release.DPFVersion() {
			return fmt.Errorf("DPUService %s/%s has version %s, want %s",
				obj.GetNamespace(), obj.GetName(), obj.GetLabels()[release.DPFVersionLabelKey], release.DPFVersion())
		}
	}
	if obj.GetGeneration() != obj.Status.ObservedGeneration {
		return fmt.Errorf("DPUService %s/%s is not ready: generation is not equal to observed generation", obj.Namespace, obj.Name)
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

func networkEditsForComponent(name string, networking Networking) []StructuredEdit {
	edits := map[string][]StructuredEdit{
		operatorv1.FlannelName: setFlannelMTUEdit(networking),
		operatorv1.MultusName:  setMultusMTUEdit(networking),
	}
	return edits[name]
}

func setFlannelMTUEdit(networking Networking) []StructuredEdit {
	mtu := strconv.Itoa(networking.ControlPlaneMTU)
	return []StructuredEdit{
		dpuServiceAddValueEdit(mtu, operatorv1.FlannelName, "flannel", "mtu"),
	}
}

func setMultusMTUEdit(networking Networking) []StructuredEdit {
	mtu := strconv.Itoa(networking.HighSpeedMTU)
	return []StructuredEdit{
		dpuServiceAddValueEdit(mtu, operatorv1.MultusName, "mtu"),
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
	if name == operatorv1.FlannelName {
		images := []*image{}
		for _, override := range strings.Split(imageOverride, ",") {
			i, err := parseImageString(override)
			if err != nil {
				return nil, err
			}
			images = append(images, i)
		}
		edits := map[string]func(...*image) []StructuredEdit{
			operatorv1.FlannelName: setFlannelImage,
		}
		editForComponent, ok := edits[name]
		if !ok {
			return nil, fmt.Errorf("failed to find image edit for component %q", name)
		}
		return editForComponent(images...), nil
	}

	image, err := parseImageString(imageOverride)
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
		baseComponent := parts[0]
		containerName := parts[1]

		// Use the configurable path system for multi-container components
		return generateImageEditsFromPaths(baseComponent, containerName, image)
	}

	// Handle single container components
	return generateImageEditsFromPaths(name, "", image)
}

func setFlannelImage(imageOverride ...*image) []StructuredEdit {
	kubeFlannelImage := imageOverride[0]
	cniImage := imageOverride[1]
	repoPath := []string{operatorv1.FlannelName, "flannel", "image", "repository"}
	tagPath := []string{operatorv1.FlannelName, "flannel", "image", "tag"}
	repoPathCNI := []string{operatorv1.FlannelName, "flannel", "image_cni", "repository"}
	tagPathCNI := []string{operatorv1.FlannelName, "flannel", "image_cni", "tag"}

	return []StructuredEdit{
		dpuServiceAddValueEdit(kubeFlannelImage.repoImage, repoPath...),
		dpuServiceAddValueEdit(kubeFlannelImage.tag, tagPath...),
		dpuServiceAddValueEdit(cniImage.repoImage, repoPathCNI...),
		dpuServiceAddValueEdit(cniImage.tag, tagPathCNI...),
	}
}

// generateImageEditsFromPaths generates image edits using the configurable path system
func generateImageEditsFromPaths(componentName, containerName string, image *image) ([]StructuredEdit, error) {
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
	containerPaths, exists := componentConfig[containerName]
	if !exists {
		// If no container paths are configured, return empty edits (container doesn't support image overrides)
		return []StructuredEdit{}, nil
	}

	// Use the first image (for single container) or specific image for multi-container

	// Build the full paths by prepending the component name
	repoPath := append([]string{componentName}, containerPaths.Repository...)
	tagPath := append([]string{componentName}, containerPaths.Tag...)

	return []StructuredEdit{
		dpuServiceAddValueEdit(image.repoImage, repoPath...),
		dpuServiceAddValueEdit(image.tag, tagPath...),
	}, nil
}

// resourceEditsForComponent generates resource edits using the configurable path system
func resourceEditsForComponent(name, resourceName string, resourceReqs corev1.ResourceRequirements) ([]StructuredEdit, error) {
	// Check if resources are set (either requests or limits)
	if len(resourceReqs.Requests) == 0 && len(resourceReqs.Limits) == 0 {
		return nil, nil
	}

	// Handle single container components
	if name == resourceName {
		return generateResourceEditsFromPaths(name, "", resourceReqs)
	}

	// Handle multi-container component resources
	parts := strings.SplitN(name, multiSplitChar, 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid multi-container resource name format: %q", name)
	}
	baseComponent := parts[0]
	containerName := parts[1]

	// Use the configurable path system for multi-container components
	return generateResourceEditsFromPaths(baseComponent, containerName, resourceReqs)
}

// generateResourceEditsFromPaths generates resource edits using the configurable path system
func generateResourceEditsFromPaths(componentName, containerName string, resourceReqs corev1.ResourceRequirements) ([]StructuredEdit, error) {
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

	resourcePath := append([]string{componentName}, helmPath.Resources...)
	return []StructuredEdit{
		dpuServiceAddValueEdit(resourceReqs, resourcePath...),
	}, nil
}
