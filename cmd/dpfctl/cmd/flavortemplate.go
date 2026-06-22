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

package cmd

import (
	"fmt"
	"os"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuflavortemplate"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/yaml"
)

type flavorTemplateRenderOptions struct {
	templateFile  string
	dpuDeviceFile string
}

var ftRenderOpts flavorTemplateRenderOptions

var flavorTemplateCmd = &cobra.Command{
	Use:   "flavor-template",
	Short: "Work with DPUFlavorTemplates",
	Long:  "Utilities for authoring and previewing DPUFlavorTemplates.",
}

var flavorTemplateRenderExample = `# Render a template against a DPUDevice's values
%[1]s flavor-template render -f template.yaml --dpudevice-file device.yaml

# Preview what the controller will generate for a live DPUDevice, then validate it
kubectl get dpudevice <name> -n <namespace> -o yaml > device.yaml
%[1]s flavor-template render -f template.yaml --dpudevice-file device.yaml | kubectl create --dry-run=server -f -
`

var flavorTemplateRenderCmd = &cobra.Command{
	Use:     "render",
	Short:   "Render a DPUFlavorTemplate into a concrete DPUFlavor",
	Long:    "Render a local DPUFlavorTemplate file against a DPUDevice's values and print the resulting DPUFlavor as YAML.",
	Example: fmt.Sprintf(flavorTemplateRenderExample, rootCmd.Root().Name()),
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runFlavorTemplateRender()
	},
}

func init() {
	rootCmd.AddCommand(flavorTemplateCmd)
	flavorTemplateCmd.AddCommand(flavorTemplateRenderCmd)

	flavorTemplateRenderCmd.Flags().StringVarP(&ftRenderOpts.templateFile, "file", "f", "",
		"Path to a DPUFlavorTemplate YAML/JSON file.")
	flavorTemplateRenderCmd.Flags().StringVar(&ftRenderOpts.dpuDeviceFile, "dpudevice-file", "",
		"Path to a DPUDevice YAML/JSON file whose spec.values are used to render the template.")
}

func runFlavorTemplateRender() error {
	spec, err := loadTemplateSpec()
	if err != nil {
		return err
	}
	values, err := loadDeviceValues()
	if err != nil {
		return err
	}

	flavor, err := dpuflavortemplate.Render(*spec, values)
	if err != nil {
		return fmt.Errorf("render failed: %w", err)
	}
	flavor.TypeMeta = metav1.TypeMeta{
		APIVersion: provisioningv1.GroupVersion.String(),
		Kind:       provisioningv1.DPUFlavorKind,
	}

	out, err := yaml.Marshal(flavor)
	if err != nil {
		return fmt.Errorf("failed to marshal rendered DPUFlavor: %w", err)
	}
	fmt.Print(string(out))
	return nil
}

// loadTemplateSpec reads the DPUFlavorTemplate spec from the local file given by -f.
func loadTemplateSpec() (*provisioningv1.DPUFlavorTemplateSpec, error) {
	if ftRenderOpts.templateFile == "" {
		return nil, fmt.Errorf("a DPUFlavorTemplate file is required: pass -f <file>")
	}
	data, err := os.ReadFile(ftRenderOpts.templateFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read template file: %w", err)
	}
	template := &provisioningv1.DPUFlavorTemplate{}
	if err := yaml.Unmarshal(data, template); err != nil {
		return nil, fmt.Errorf("failed to parse DPUFlavorTemplate: %w", err)
	}
	return &template.Spec, nil
}

// loadDeviceValues reads a DPUDevice from the local file given by --dpudevice-file and returns
// its spec.values.
func loadDeviceValues() (*runtime.RawExtension, error) {
	if ftRenderOpts.dpuDeviceFile == "" {
		return nil, fmt.Errorf("a DPUDevice file is required: pass --dpudevice-file <file>")
	}
	data, err := os.ReadFile(ftRenderOpts.dpuDeviceFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read DPUDevice file: %w", err)
	}
	device := &provisioningv1.DPUDevice{}
	if err := yaml.Unmarshal(data, device); err != nil {
		return nil, fmt.Errorf("failed to parse DPUDevice: %w", err)
	}
	return device.Spec.Values, nil
}
