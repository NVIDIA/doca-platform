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

package bfcfg

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"os"
	"strings"
	"text/template"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	"github.com/Masterminds/sprig/v3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"
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
			dpu.Spec.Cluster.Name, dpu.Spec.Cluster.Namespace,
			dpu.Spec.DPUFlavor, dpu.Namespace)
		if err != nil {
			return nil, fmt.Errorf("resolving dynamic bf.cfg template: %w", err)
		}
		return data, nil
	}

	// Use the embedded config by default.
	return DefaultBFCFGTemplateData, nil
}

type BFCFGData struct {
	KubeadmSecretName      string
	KubeadmSecretNamespace string
	Kubeconfig             string
	DPUFlavorYAML          string
	DPUHostName            string
	BFGCFGParams           []string
	UbuntuPassword         string
	ConfigFiles            []BFCFGWriteFile
	OVSRawScript           string
	// OOBNetwork is a flag to indicate if the DPU accesses DPU cluster via the OOB interface.
	OOBNetwork       bool
	RedfishInterface bool
	ControlPlaneMTU  int
	DPUName          string
	DPUNamespace     string
	DPUUID           string
	DPUAgentRepoURL  string
}

type BFCFGWriteFile struct {
	Path        string
	IsAppend    bool
	Content     string
	Permissions string
}

func GenerateBFConfig(ctx context.Context, controllerContext *util.ControllerContext, dpu *provisioningv1.DPU, dpuNode *provisioningv1.DPUNode, dpuDevice *provisioningv1.DPUDevice, flavor *provisioningv1.DPUFlavor, kubeadmSecret *corev1.Secret) ([]byte, error) {
	logger := log.FromContext(ctx)
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
	dpfOperatorConfig := dpfOperatorConfigList.Items[0]
	controlPlaneMTU := *dpfOperatorConfig.Spec.Networking.ControlPlaneMTU

	isRedfish := controllerContext.Options.DPUInstallInterface == string(provisioningv1.InstallViaRedFish)

	var dpuAgentRepoURL string
	var kubeconfig string
	if isRedfish {
		if dpfOperatorConfig.Spec.Overrides == nil || dpfOperatorConfig.Spec.Overrides.KubernetesAPIServerVIP == nil || dpfOperatorConfig.Spec.Overrides.KubernetesAPIServerPort == nil {
			return nil, fmt.Errorf("KubernetesAPIServerVIP and KubernetesAPIServerPort must be set in DPFOperatorConfig for zero-trust mode")
		}
		apiServerAddress := fmt.Sprintf("https://%s:%d", *dpfOperatorConfig.Spec.Overrides.KubernetesAPIServerVIP, *dpfOperatorConfig.Spec.Overrides.KubernetesAPIServerPort)
		// TODO: each DPU agent should use its own kubeconfig instead of sharing one.
		kubeconfigData, err := cutil.GenerateKubeconfig(ctx, controllerContext.Client, apiServerAddress, dpu.Namespace)
		if err != nil {
			return nil, fmt.Errorf("generating dpu-agent kubeconfig: %w", err)
		}
		kubeconfig = string(kubeconfigData)
		bfbRegistryAddr, err := cutil.GetBFBRegistryAddressWithPort(ctx, controllerContext.Client, os.Getenv("POD_NAMESPACE"), controllerContext.Options.BFBRegistry)
		if err != nil {
			return nil, fmt.Errorf("bfb-registry address with port: %w", err)
		}
		dpuAgentRepoURL = strings.TrimRight(bfbRegistryAddr, "/") + "/deb"
	} else {
		dpuAgentRepoURL = "http://[fe80::1%25tmfifo_net0]:11029/deb"
	}

	templateData, err := getTemplateData(ctx, controllerContext, &dpfOperatorConfig, dpu)
	if err != nil {
		return nil, err
	}

	opts := GenerateOptions{
		DPUHostName:            cutil.GenerateNodeName(dpu),
		KubeadmSecretName:      kubeadmSecret.Name,
		KubeadmSecretNamespace: kubeadmSecret.Namespace,
		Kubeconfig:             kubeconfig,
		IsRedfish:              isRedfish,
		ControlPlaneMTU:        controlPlaneMTU,
		DPUName:                dpu.Name,
		DPUNamespace:           dpu.Namespace,
		DPUUID:                 string(dpu.UID),
		DPUAgentRepoURL:        dpuAgentRepoURL,
	}
	buf, err := Generate(flavor, opts, templateData)
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

// GenerateOptions holds the deployment-specific parameters for Generate.
type GenerateOptions struct {
	DPUHostName            string
	KubeadmSecretName      string
	KubeadmSecretNamespace string
	Kubeconfig             string
	IsRedfish              bool
	ControlPlaneMTU        int
	DPUName                string
	DPUNamespace           string
	DPUUID                 string
	DPUAgentRepoURL        string
}

// Generate creates a bf.cfg file from a DPUFlavor, deployment options, and template data.
func Generate(flavor *provisioningv1.DPUFlavor, opts GenerateOptions, templateData []byte) ([]byte, error) {
	flavorBytes, err := yaml.Marshal(flavor)
	if err != nil {
		return nil, fmt.Errorf("marshaling DPUFlavor: %w", err)
	}

	config := &BFCFGData{
		KubeadmSecretName:      opts.KubeadmSecretName,
		KubeadmSecretNamespace: opts.KubeadmSecretNamespace,
		Kubeconfig:             opts.Kubeconfig,
		DPUFlavorYAML:          string(flavorBytes),
		DPUHostName:            opts.DPUHostName,
		ControlPlaneMTU:        opts.ControlPlaneMTU,
		RedfishInterface:       opts.IsRedfish,
		OOBNetwork:             opts.IsRedfish,
		DPUName:                opts.DPUName,
		DPUNamespace:           opts.DPUNamespace,
		DPUUID:                 opts.DPUUID,
		DPUAgentRepoURL:        opts.DPUAgentRepoURL,
	}

	config.BFGCFGParams, config.UbuntuPassword = bfcfgParams(flavor)
	for _, f := range flavor.Spec.ConfigFiles {
		config.ConfigFiles = append(config.ConfigFiles, BFCFGWriteFile{
			Path:        f.Path,
			Permissions: f.Permissions,
			IsAppend:    f.Operation == provisioningv1.FileAppend,
			Content:     f.Raw,
		})
	}
	config.OVSRawScript = flavor.Spec.OVS.RawConfigScript

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

// getTemplateDataFromConfigMap discovers a bf.cfg template ConfigMap by listing ConfigMaps
// with the well-known label and filtering by annotation values for the given BFB, DPUCluster, and DPUFlavor.
// It returns an error if there is not exactly one matching ConfigMap for the given parameters.
func getTemplateDataFromConfigMap(ctx context.Context, c client.Client, namespace, bfbName, bfbNamespace, clusterName, clusterNamespace, dpuFlavorName, dpuFlavorNamespace string) ([]byte, error) {
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
			annotations[cutil.BFCFGTemplateClusterNamespaceAnnotation] == clusterNamespace &&
			annotations[cutil.BFCFGTemplateDPUFlavorNameAnnotation] == dpuFlavorName &&
			annotations[cutil.BFCFGTemplateDPUFlavorNamespaceAnnotation] == dpuFlavorNamespace {
			matches = append(matches, *item)
		}
	}

	if len(matches) != 1 {
		return nil, fmt.Errorf("found %d bf.cfg template ConfigMaps matching BFB %s/%s, DPUCluster %s/%s, and DPUFlavor %s/%s; exactly one is required",
			len(matches), bfbNamespace, bfbName, clusterNamespace, clusterName, dpuFlavorNamespace, dpuFlavorName)
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
