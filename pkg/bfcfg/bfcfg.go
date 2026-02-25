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

package bfcfg

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"text/template"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util/reboot"

	"github.com/Masterminds/sprig/v3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// MaxBFSize is the maximum size of the bf.cfg file is expanded to 128k since DOCA 2.8
	MaxBFSize     = 1024 * 128
	MaxTrustedSfs = 10
)

var (
	//go:embed bf.cfg.template
	DefaultBFCFGTemplateData []byte
	// ConfigMapDataKey is the key in the configmap where the bfb.cfg.template is stored if overwritten.
	ConfigMapDataKey = "BF_CFG_TEMPLATE"
)

// getTemplateData returns the bf.cfg template content based on the DPFOperatorConfig settings.
// If spec.provisioningController.bfCFGTemplateConfigMap is set it will try to mount a configMap as a volume in the provisioning controller. This behavior is deprecated.
// If spec.provisioningController.enableDynamicBFCFGTemplates is set it will retrieve a configMap with a custom template.
// By default it will return the default template data which is embedded in the controller.
func getTemplateData(ctx context.Context, controllerCtx *util.ControllerContext, dpfOperatorConfig *operatorv1.DPFOperatorConfig, dpu *provisioningv1.DPU) ([]byte, error) {
	provisioningConfig := dpfOperatorConfig.Spec.ProvisioningController

	// This behavior is deprecated and will be removed in a future release.
	//nolint:staticcheck // Intentionally using deprecated field for backward compatibility
	if provisioningConfig != nil && provisioningConfig.BFCFGTemplateConfigMap != nil {
		data, err := os.ReadFile(controllerCtx.Options.BFCFGTemplateFile)
		if err != nil {
			return nil, fmt.Errorf("reading bf.cfg template file %v: %w", provisioningConfig.BFCFGTemplateConfigMap, err)
		}
		return data, nil
	}

	if provisioningConfig != nil && provisioningConfig.EnableDynamicBFCFGTemplates {
		data, err := getTemplateDataFromConfigMap(ctx, controllerCtx.Client, dpfOperatorConfig.Namespace,
			dpu.Spec.BFB, dpu.Namespace,
			dpu.Spec.Cluster.Name, dpu.Spec.Cluster.Namespace)
		if err != nil {
			return nil, fmt.Errorf("resolving dynamic bf.cfg template: %w", err)
		}
		return data, nil
	}

	// Use the embedded config by default.
	return DefaultBFCFGTemplateData, nil
}

type BFCFGData struct {
	KubeadmJoinCMD             string
	DPUHostName                string
	BFGCFGParams               []string
	UbuntuPassword             string
	NVConfigParams             []NVConfigEntry
	Sysctl                     []string
	ConfigFiles                []BFCFGWriteFile
	OVSRawScript               string
	KernelParameters           string
	ContainerdRegistryEndpoint string
	SFNum                      int
	TrustedSFs                 int
	// AdditionalReboot adds an extra reboot during the DPU provisioning. This is required in some environments.
	AdditionalReboot bool
	// OOBNetwork is a flag to indicate if the DPU accesses DPU cluster via the OOB interface.
	OOBNetwork       bool
	RedfishInterface bool
	ControlPlaneMTU  int
	// The interface name for all the PFs.
	PFs     []string
	DpuMode string
}

type NVConfigEntry struct {
	Device     string // Device identifier: "*" (wildcard for all), "p0"/"P0" (port 0), "p1"/"P1" (port 1)
	Parameters string // Space-joined parameters
}

type BFCFGWriteFile struct {
	Path        string
	IsAppend    bool
	Content     string
	Permissions string
}

func GenerateBFConfig(ctx context.Context, controllerContext *util.ControllerContext, dpu *provisioningv1.DPU, dpuNode *provisioningv1.DPUNode, dpuDevice *provisioningv1.DPUDevice, flavor *provisioningv1.DPUFlavor, joinCommand, installInterface string) ([]byte, error) {
	logger := log.FromContext(ctx)
	additionalReboot, err := shouldTriggerAdditionalReboot(ctx, dpuNode, dpu)
	if err != nil {
		return nil, err
	}

	dpfOperatorConfigList := operatorv1.DPFOperatorConfigList{}
	if err := controllerContext.List(ctx, &dpfOperatorConfigList, &client.ListOptions{}); err != nil {
		return nil, fmt.Errorf("list DPFOperatorConfigs: %w", err)
	}
	if len(dpfOperatorConfigList.Items) == 0 || len(dpfOperatorConfigList.Items) > 1 {
		return nil, fmt.Errorf("exactly one DPFOperatorConfig necessary")
	}
	if dpfOperatorConfigList.Items[0].Spec.Networking == nil {
		return nil, fmt.Errorf("DPFOperatorConfig networking section is missing")
	}
	if dpuDevice.Spec.NumberOfPFs == nil {
		return nil, fmt.Errorf("numberOfPFs is not set")
	}

	dpfOperatorConfig := dpfOperatorConfigList.Items[0]
	controlPlaneMTU := *dpfOperatorConfig.Spec.Networking.ControlPlaneMTU

	templateData, err := getTemplateData(ctx, controllerContext, &dpfOperatorConfig, dpu)
	if err != nil {
		return nil, err
	}

	buf, err := Generate(flavor, cutil.GenerateNodeName(dpu), joinCommand, additionalReboot, templateData, installInterface, controlPlaneMTU, *dpuDevice.Spec.NumberOfPFs)
	if err != nil {
		return nil, err
	}
	if buf == nil {
		return nil, fmt.Errorf("failed bf.cfg creation due to buffer issue")
	}

	// If the size check should not be skipped validate the bf.cfg is under MaxBFSize.
	// TODO: Remove this once the underlying check in the BFB install scripts is removed.
	// ref: https://github.com/Mellanox/bfb-build/blob/a6b6fcc115b0c7a525ba5516292f5ded7c187a16/common/install.env/common#L947
	if _, ok := flavor.Annotations[cutil.SkipBFCFGSizeCheck]; !ok && len(buf) > MaxBFSize {
		return nil, fmt.Errorf("bf.cfg for %s size (%d) exceeds the maximum limit (%d)", dpu.Name, len(buf), MaxBFSize)
	}
	logger.V(3).Info(fmt.Sprintf("bf.cfg for %s has len: %d data: %s", dpu.Name, len(buf), string(buf)))

	return buf, nil
}

// shouldTriggerAdditionalReboot returns whether an additional reboot should be triggered after bfb-install
func shouldTriggerAdditionalReboot(ctx context.Context, dpuNode *provisioningv1.DPUNode, dpu *provisioningv1.DPU) (bool, error) {
	logger := log.FromContext(ctx)

	cmd, _, err := reboot.GenerateCmd(dpuNode.Annotations, dpu.Annotations)
	if err != nil {
		logger.Error(err, "failed to generate ipmitool command")
		return false, err
	}
	dpuNodeLabels := dpuNode.GetLabels()
	if _, ok := dpuNodeLabels[cutil.DPUNodeAdditionalDPURebootLabel]; ok {
		return true, nil
	}
	if cmd == reboot.Skip {
		return true, nil
	}
	return false, nil
}

// Generate creates a bf.cfg file from the given parameters and template data.
// If templateData is nil or empty, the embedded default template is used.
func Generate(flavor *provisioningv1.DPUFlavor, dpuName, joinCmd string, additionalReboot bool, templateData []byte, installInterface string, controlPlaneMTU int, numberOfPFs int) ([]byte, error) {
	config := &BFCFGData{
		KubeadmJoinCMD:   joinCmd,
		DPUHostName:      dpuName,
		AdditionalReboot: additionalReboot,
		KernelParameters: strings.TrimSpace(strings.Join(flavor.Spec.Grub.KernelParameters, " ")),
		ControlPlaneMTU:  controlPlaneMTU,
		RedfishInterface: installInterface == string(provisioningv1.InstallViaRedFish),
		DpuMode:          string(flavor.Spec.DpuMode),
	}

	config.ContainerdRegistryEndpoint = flavor.Spec.ContainerdConfig.RegistryEndpoint

	config.BFGCFGParams, config.UbuntuPassword = bfcfgParams(flavor)

	// Process all nvconfig entries for device-specific configurations
	config.NVConfigParams = make([]NVConfigEntry, 0, len(flavor.Spec.NVConfig))
	for _, nvcfg := range flavor.Spec.NVConfig {
		device := "*"
		if nvcfg.Device != nil {
			device = strings.TrimSpace(*nvcfg.Device)
		}
		config.NVConfigParams = append(config.NVConfigParams, NVConfigEntry{
			Device:     device,
			Parameters: strings.Join(nvcfg.Parameters, " "),
		})
	}

	config.Sysctl = flavor.Spec.Sysctl.Parameters
	for _, f := range flavor.Spec.ConfigFiles {
		config.ConfigFiles = append(config.ConfigFiles, BFCFGWriteFile{
			Path:        f.Path,
			Permissions: f.Permissions,
			IsAppend:    f.Operation == provisioningv1.FileAppend,
			Content:     f.Raw,
		})
	}
	config.OVSRawScript = flavor.Spec.OVS.RawConfigScript
	if installInterface == string(provisioningv1.InstallViaRedFish) {
		config.OOBNetwork = true
	}

	if num, ok := getPFTotalSFFromFlavor(flavor); ok {
		config.SFNum = num
	}

	if num, ok := getTrustedSFFromFlavor(flavor); ok {
		config.TrustedSFs = num
	}

	config.PFs = []string{}
	for i := 0; i < numberOfPFs; i++ {
		config.PFs = append(config.PFs, fmt.Sprintf("p%d", i))
		config.PFs = append(config.PFs, fmt.Sprintf("pf%dhpf", i))
	}

	bfbCFGTemplate, err := template.New("").Funcs(sprig.FuncMap()).Parse(string(templateData))
	if err != nil {
		return nil, fmt.Errorf("parsing bf.cfg template: %w", err)
	}
	buf := bytes.NewBuffer(nil)
	if err := bfbCFGTemplate.Execute(buf, config); err != nil {
		return nil, fmt.Errorf("execute bfbCFGTemplate failed, err: %v", err)
	}
	if buf.Bytes() == nil {
		return nil, fmt.Errorf("bfbCFGTemplate execution failed, err %v", fmt.Errorf("template data byte buffer was nil"))
	}
	return buf.Bytes(), nil
}

func bfcfgParams(flavor *provisioningv1.DPUFlavor) ([]string, string) {
	var ret []string
	var passwd string
	for _, param := range flavor.Spec.BFCfgParameters {
		info := strings.Split(param, "=")
		if len(info) != 2 {
			ret = append(ret, param)
			continue
		}
		key := strings.TrimSpace(info[0])
		if key != "ubuntu_PASSWORD" {
			ret = append(ret, param)
			continue
		}
		passwd = strings.TrimSpace(info[1])
	}
	if passwd == "" {
		return ret, passwd
	}
	if !strings.HasPrefix(passwd, "'") {
		passwd = fmt.Sprintf("'%s'", passwd)
	}
	return ret, passwd
}

func getPFTotalSFFromFlavor(flavor *provisioningv1.DPUFlavor) (int, bool) {
	regex := regexp.MustCompile(`^PF_TOTAL_SF=([0-9]+)`)
	for _, nvconfig := range flavor.Spec.NVConfig {
		for _, parmeter := range nvconfig.Parameters {
			matches := regex.FindStringSubmatch(parmeter)
			if len(matches) == 2 {
				if num, err := strconv.Atoi(matches[1]); err == nil {
					return num, true
				}
			}
		}
	}
	return 0, false
}

func getTrustedSFFromFlavor(flavor *provisioningv1.DPUFlavor) (int, bool) {
	if flavor.Annotations != nil {
		trustedSFCountFromAnnotation, found := flavor.Annotations[cutil.TrustedSFCount]
		if found {
			trustedSFCount, err := strconv.Atoi(trustedSFCountFromAnnotation)
			if err == nil && trustedSFCount > 0 && trustedSFCount <= MaxTrustedSfs {
				return trustedSFCount, true
			}

		}
	}
	return 0, false
}

// getTemplateDataFromConfigMap discovers a bf.cfg template ConfigMap by listing ConfigMaps
// with the well-known label and filtering by annotation values for the given BFB and DPUCluster.
// It returns an error if there is not exactly one matching ConfigMap for the given parameters.
func getTemplateDataFromConfigMap(ctx context.Context, c client.Client, namespace, bfbName, bfbNamespace, clusterName, clusterNamespace string) ([]byte, error) {
	selector := labels.SelectorFromSet(labels.Set{
		cutil.BFCFGTemplateLabel: "true",
	})

	// Use PartialObjectMetadataList to fetch only metadata (efficient label-based discovery).
	metadataList := &metav1.PartialObjectMetadataList{}
	metadataList.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "ConfigMapList",
	})
	if err := c.List(ctx, metadataList, &client.ListOptions{
		Namespace:     namespace,
		LabelSelector: selector,
	}); err != nil {
		return nil, fmt.Errorf("listing bf.cfg template ConfigMaps: %w", err)
	}

	// Filter by annotations.
	// Annotations are used here because label values are limited to 64 characters. BFB and DPUCluster names could exceed this limit.
	var matches []metav1.PartialObjectMetadata
	for i := range metadataList.Items {
		item := &metadataList.Items[i]
		annotations := item.GetAnnotations()
		if annotations[cutil.BFCFGTemplateBFBNameAnnotation] == bfbName &&
			annotations[cutil.BFCFGTemplateBFBNamespaceAnnotation] == bfbNamespace &&
			annotations[cutil.BFCFGTemplateClusterNameAnnotation] == clusterName &&
			annotations[cutil.BFCFGTemplateClusterNamespaceAnnotation] == clusterNamespace {
			matches = append(matches, *item)
		}
	}

	if len(matches) != 1 {
		return nil, fmt.Errorf("found %d bf.cfg template ConfigMaps matching BFB %s/%s and DPUCluster %s/%s; exactly one is required",
			len(matches), bfbNamespace, bfbName, clusterNamespace, clusterName)
	}
	// Fetch the full ConfigMap to get the data.
	cm := &corev1.ConfigMap{}
	if err := c.Get(ctx, types.NamespacedName{
		Namespace: namespace,
		Name:      matches[0].Name,
	}, cm); err != nil {
		return nil, fmt.Errorf("getting bf.cfg template ConfigMap %q: %w", matches[0].Name, err)
	}

	templateData, ok := cm.Data[ConfigMapDataKey]
	if !ok {
		return nil, fmt.Errorf("bf.cfg template ConfigMap %q is missing required key %q", cm.Name, ConfigMapDataKey)
	}

	return []byte(templateData), nil
}
