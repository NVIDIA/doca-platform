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
	"strconv"
	"strings"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	"github.com/nvidia/doca-platform/internal/conditions"
	"github.com/nvidia/doca-platform/internal/operator/utils"

	"k8s.io/apimachinery/pkg/api/meta"
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
	operatorv1.OVSHelperName:            true,
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

	// copy object
	dpuServiceCopy := f.dpuService.DeepCopy()

	dpuServiceCopy.SetName(f.Name())

	labelsToAdd := map[string]string{operatorv1.DPFComponentLabelKey: f.Name()}
	applySetID := ApplySetID(vars.Namespace, f)
	// Add the ApplySet labels to the manifests unless disabled.
	if !opts.skipApplySet {
		labelsToAdd[applysetPartOfLabel] = applySetID
	}

	// apply edits
	edits := NewEdits().AddForAll(
		NamespaceEdit(vars.Namespace),
		LabelsEdit(labelsToAdd))

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
	imageString, ok := vars.Images[f.Name()]
	if ok {
		imageEdits, err := imageEditsForComponent(f.Name(), imageString)
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

	// Add the ApplySet to the manifests if this hasn't been disabled.
	if !opts.skipApplySet {
		ret = append(ret, applySetParentForComponent(f, applySetID, vars, applySetInventoryString(dpuServiceCopy)))
	}
	return append(ret, dpuServiceCopy), nil
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
	case operatorv1.OVSHelperName:
		return ovsHelperEdits(vars)
	case operatorv1.NVIPAMName:
		return nvipamEdits(vars)
	case operatorv1.FlannelName:
		return flannelEdits(vars)
	// Other DPUServices do not need additional values.
	default:
		return nil, nil
	}
}

var (
	cniBinDirPathKey                   = "cniBinDir"
	cniConfDirPathKey                  = "cniConfDir"
	openvSwitchRunDirPathKey           = "openvSwitchRunDir"
	openvSwitchBinDirPathKey           = "openvSwitchBinDir"
	openvSwitchSharedLibraryDirPathKey = "openvSwitchSharedLibraryDir"
)

func ovsHelperEdits(vars Variables) ([]StructuredEdit, error) {
	return []StructuredEdit{
		dpuServiceAddValueEdit(vars.DPUOpenvSwitchRunPath, operatorv1.OVSHelperName, openvSwitchRunDirPathKey),
		dpuServiceAddValueEdit(vars.DPUOpenvSwitchBinPath, operatorv1.OVSHelperName, openvSwitchBinDirPathKey),
		dpuServiceAddValueEdit(vars.DPUOpenvSwitchSharedLibPath, operatorv1.OVSHelperName, openvSwitchSharedLibraryDirPathKey),
	}, nil
}

func sfcControllerEdits(vars Variables) ([]StructuredEdit, error) {
	return []StructuredEdit{
		dpuServiceAddValueEdit(vars.DPUOpenvSwitchRunPath, operatorv1.SFCControllerName, openvSwitchRunDirPathKey),
		dpuServiceAddValueEdit(vars.DPUOpenvSwitchBinPath, operatorv1.SFCControllerName, openvSwitchBinDirPathKey),
		dpuServiceAddValueEdit(vars.DPUOpenvSwitchSharedLibPath, operatorv1.SFCControllerName, openvSwitchSharedLibraryDirPathKey),
		dpuServiceAddValueEdit(vars.SFCController.SecureFlowDeletionTimeout.String(), operatorv1.SFCControllerName, "controllerManager", "manager", "secureFlowDeletionTimeout"),
	}, nil
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
	return []StructuredEdit{
		// flannel has an additional "flannel" structure inside its helm chart values.
		dpuServiceAddValueEdit(vars.DPUCNIBinPath, operatorv1.FlannelName, "flannel", cniBinDirPathKey),
		dpuServiceAddValueEdit(vars.DPUCNIConfPath, operatorv1.FlannelName, "flannel", cniConfDirPathKey),
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
		currentValues := map[string]interface{}{}
		if dpuService.Spec.HelmChart.Values.Raw != nil {
			err := json.Unmarshal(dpuService.Spec.HelmChart.Values.Raw, &currentValues)
			if err != nil {
				return fmt.Errorf("error merging values in DPUService manifests: %w", err)
			}
			// This function needs to handle the case where dpuService.Spec.HelmChart.Values.Object holds the values.
			// This is because the function can be called multiple times and stores updated values there.
		} else if dpuService.Spec.HelmChart.Values.Object != nil {
			uns, ok := dpuService.Spec.HelmChart.Values.Object.(*unstructured.Unstructured)
			if !ok {
				return fmt.Errorf("could not treat values object field as unstructured")
			}
			currentValues = uns.UnstructuredContent()
		}

		valuesObject := &unstructured.Unstructured{Object: currentValues}
		if err := unstructured.SetNestedField(valuesObject.UnstructuredContent(), value, key...); err != nil {
			return err
		}

		dpuService.Spec.HelmChart.Values.Object = valuesObject
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

// IsReady returns an error if the DPUService does not have a Ready status condition.
func (f *fromDPUService) IsReady(ctx context.Context, c client.Client, namespace string) error {
	obj := &dpuservicev1.DPUService{}
	err := c.Get(ctx, client.ObjectKey{Name: f.Name(), Namespace: namespace}, obj)
	if err != nil {
		return err
	}
	if !meta.IsStatusConditionTrue(obj.GetConditions(), string(conditions.TypeReady)) {
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
// imageOverride can be a comma-delimited string and each part of the string will be parsed as an image.
func imageEditsForComponent(name string, imageOverride string) ([]StructuredEdit, error) {
	images := []*image{}
	for _, override := range strings.Split(imageOverride, ",") {
		i, err := parseImageString(override)
		if err != nil {
			return nil, err
		}
		images = append(images, i)
	}
	edits := map[string]func(...*image) []StructuredEdit{
		operatorv1.FlannelName:              setFlannelImage,
		operatorv1.ServiceSetControllerName: setServiceSetControllerImage,
		operatorv1.MultusName:               setMultusImage,
		operatorv1.SRIOVDevicePluginName:    setSRIOVDevicePluginImage,
		operatorv1.OVSCNIName:               setOVSCNIImage,
		operatorv1.NVIPAMName:               setNVIPAMImage,
		operatorv1.SFCControllerName:        setSFCControllerImage,
		operatorv1.OVSHelperName:            setOVSHelperImage,
	}
	editForComponent, ok := edits[name]
	if !ok {
		return nil, fmt.Errorf("failed to find image edit for component %q", name)
	}
	return editForComponent(images...), nil
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

func setServiceSetControllerImage(imageOverrides ...*image) []StructuredEdit {
	imageOverride := imageOverrides[0]
	repoPath := []string{operatorv1.ServiceSetControllerName, "controllerManager", "manager", "image", "repository"}
	tagPath := []string{operatorv1.ServiceSetControllerName, "controllerManager", "manager", "image", "tag"}

	return []StructuredEdit{
		dpuServiceAddValueEdit(imageOverride.repoImage, repoPath...),
		dpuServiceAddValueEdit(imageOverride.tag, tagPath...),
	}
}

func setSFCControllerImage(imageOverrides ...*image) []StructuredEdit {
	imageOverride := imageOverrides[0]

	repoPath := []string{operatorv1.SFCControllerName, "controllerManager", "manager", "image", "repository"}
	tagPath := []string{operatorv1.SFCControllerName, "controllerManager", "manager", "image", "tag"}

	return []StructuredEdit{
		dpuServiceAddValueEdit(imageOverride.repoImage, repoPath...),
		dpuServiceAddValueEdit(imageOverride.tag, tagPath...),
	}
}

func setOVSHelperImage(imageOverrides ...*image) []StructuredEdit {
	imageOverride := imageOverrides[0]

	repoPath := []string{operatorv1.OVSHelperName, "ovsHelper", "image", "repository"}
	tagPath := []string{operatorv1.OVSHelperName, "ovsHelper", "image", "tag"}

	return []StructuredEdit{
		dpuServiceAddValueEdit(imageOverride.repoImage, repoPath...),
		dpuServiceAddValueEdit(imageOverride.tag, tagPath...),
	}
}

// ovsCNI has two containers which both use the same image.
func setOVSCNIImage(imageOverrides ...*image) []StructuredEdit {
	imageOverride := imageOverrides[0]

	markerRepoPath := []string{operatorv1.OVSCNIName, "arm64", "ovsCniMarker", "image", "repository"}
	markerTagPath := []string{operatorv1.OVSCNIName, "arm64", "ovsCniMarker", "image", "tag"}
	pluginRepoPath := []string{operatorv1.OVSCNIName, "arm64", "ovsCniPlugin", "image", "repository"}
	pluginTagPath := []string{operatorv1.OVSCNIName, "arm64", "ovsCniPlugin", "image", "tag"}

	return []StructuredEdit{
		dpuServiceAddValueEdit(imageOverride.repoImage, markerRepoPath...),
		dpuServiceAddValueEdit(imageOverride.tag, markerTagPath...),
		dpuServiceAddValueEdit(imageOverride.repoImage, pluginRepoPath...),
		dpuServiceAddValueEdit(imageOverride.tag, pluginTagPath...),
	}
}

// multus has two containers which both use the same image.
func setMultusImage(imageOverrides ...*image) []StructuredEdit {
	imageOverride := imageOverrides[0]

	installerRepoPath := []string{operatorv1.MultusName, "kubeMultusDs", "installMultusBinary", "image", "repository"}
	installerTagPath := []string{operatorv1.MultusName, "kubeMultusDs", "installMultusBinary", "image", "tag"}
	daemonSetRepoPath := []string{operatorv1.MultusName, "kubeMultusDs", "kubeMultus", "image", "repository"}
	daemonSetTagPath := []string{operatorv1.MultusName, "kubeMultusDs", "kubeMultus", "image", "tag"}

	return []StructuredEdit{
		dpuServiceAddValueEdit(imageOverride.repoImage, installerRepoPath...),
		dpuServiceAddValueEdit(imageOverride.tag, installerTagPath...),
		dpuServiceAddValueEdit(imageOverride.repoImage, daemonSetRepoPath...),
		dpuServiceAddValueEdit(imageOverride.tag, daemonSetTagPath...),
	}
}

func setSRIOVDevicePluginImage(imageOverrides ...*image) []StructuredEdit {
	imageOverride := imageOverrides[0]

	repoPath := []string{operatorv1.SRIOVDevicePluginName, "kubeSriovDevicePlugin", "kubeSriovdp", "image", "repository"}
	tagPath := []string{operatorv1.SRIOVDevicePluginName, "kubeSriovDevicePlugin", "kubeSriovdp", "image", "tag"}
	return []StructuredEdit{
		dpuServiceAddValueEdit(imageOverride.repoImage, repoPath...),
		dpuServiceAddValueEdit(imageOverride.tag, tagPath...),
	}
}

func setNVIPAMImage(imageOverrides ...*image) []StructuredEdit {
	imageOverride := imageOverrides[0]
	tagPath := []string{operatorv1.NVIPAMName, "nvIpam", "image", "tag"}
	repoPath := []string{operatorv1.NVIPAMName, "nvIpam", "image", "repository"}
	return []StructuredEdit{
		dpuServiceAddValueEdit(imageOverride.repoImage, repoPath...),
		dpuServiceAddValueEdit(imageOverride.tag, tagPath...),
	}
}
