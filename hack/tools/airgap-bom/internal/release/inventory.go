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

package release

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"text/template"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/airgap/bom"
	"github.com/nvidia/doca-platform/internal/operator/inventory"
	"github.com/nvidia/doca-platform/internal/release"
	"github.com/nvidia/doca-platform/pkg/dpucluster"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	operatorNamespace = "dpf-operator-system"
	// releaseDefaultsFile is the release defaults the Makefile generates from the release defaults
	// template, committed with the placeholder registry and tag, relative to the repository root.
	releaseDefaultsFile = "internal/release/manifests/defaults.yaml"
)

// generatedInputs are the files the Makefile generates with the registry and tag of a build, and
// which are committed with the placeholder registry and tag, relative to the repository root.
var generatedInputs = []string{releaseDefaultsFile, "dpuservices/storage/chart/values.yaml"}

// checkPlaceholders fails when a generated input was generated for another registry than the
// placeholder FirstPartyHost, as first-party images could not be told from third-party ones. The
// BOM is generated from the files as committed, make verify-generate keeps them up to date.
func checkPlaceholders(repoRoot string) error {
	for _, file := range generatedInputs {
		data, err := os.ReadFile(filepath.Join(repoRoot, file))
		if err != nil {
			return err
		}
		if !bytes.Contains(data, []byte(": "+FirstPartyHost+"/")) {
			return fmt.Errorf("%s was generated for a registry other than %s: the BOM is generated from the generated files as "+
				"committed, with the default REGISTRY and TAG of the Makefile", file, FirstPartyHost)
		}
	}
	return nil
}

// deploymentModes are the deployment modes the inventory is generated for; the BOM holds the union.
var deploymentModes = []operatorv1.DeploymentMode{operatorv1.DeploymentModeHostTrusted, operatorv1.DeploymentModeZeroTrust}

// inventoryImages returns the images of every component of the operator inventory: objects
// applied to the host cluster are host scoped; DPUServices are rendered from the local chart they
// name and scoped by deployInCluster.
func inventoryImages(ctx context.Context, opts Options, charts map[string]string) ([]bom.Image, error) {
	// The inventory components parse the operator defaults themselves (release.NewDefaults().Parse())
	// from package state, so the committed defaults file is injected there.
	data, err := os.ReadFile(filepath.Join(opts.RepoRoot, releaseDefaultsFile))
	if err != nil {
		return nil, err
	}
	release.SetDefaultsContentForTesting(data)
	defaults := release.NewDefaults()
	if err := defaults.Parse(); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", releaseDefaultsFile, err)
	}
	inv := inventory.New()
	if err := inv.ParseAll(); err != nil {
		return nil, err
	}
	var (
		images []bom.Image
		errs   []error
	)
	for _, mode := range deploymentModes {
		vars := inventoryVariables(defaults, mode)
		for _, component := range inv.AllComponents() {
			objs, err := component.GenerateManifests(ctx, vars)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: component %s: %w", mode, component.Name(), err))
				continue
			}
			if len(objs) == 0 {
				opts.logf(fmt.Sprintf("%s: component %s renders no objects", mode, component.Name()))
			}
			for _, obj := range objs {
				imgs, err := objectImages(ctx, opts.Engine, obj, charts)
				if err != nil {
					errs = append(errs, fmt.Errorf("%s: component %s: %w", mode, component.Name(), err))
					continue
				}
				images = append(images, imgs...)
			}
		}
	}
	return images, errors.Join(errs...)
}

// inventoryVariables returns the inventory variables of a DPFOperatorConfig in a deployment mode
// with every system component enabled, for one Kamaji DPUCluster served by host DNS, so every
// component the operator can deploy is generated.
func inventoryVariables(defaults *release.Defaults, mode operatorv1.DeploymentMode) inventory.Variables {
	config := &operatorv1.DPFOperatorConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "dpfoperatorconfig", Namespace: operatorNamespace},
		Spec: operatorv1.DPFOperatorConfigSpec{
			DeploymentMode: mode,
			// Without a BFB PVC the provisioning controller stores BFBs on a hostPath, which adds an
			// init container; that variant is the superset.
			ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{},
		},
	}
	cluster := &provisioningv1.DPUCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "dpu-cluster", Namespace: "dpu-cluster"},
		Spec: provisioningv1.DPUClusterSpec{
			Type: string(provisioningv1.KamajiCluster),
			ClusterEndpoint: &provisioningv1.ClusterEndpointSpec{
				Keepalived: &provisioningv1.KeepalivedSpec{VIP: "10.0.0.1", Interface: "br-dpu", VirtualRouterID: 1},
			},
		},
	}
	vars := inventory.VariablesFromDPFOperatorConfig(defaults, config, []*dpucluster.Config{dpucluster.NewConfig(fake.NewClientBuilder().Build(), cluster)})
	for name := range vars.DisableSystemComponents {
		vars.DisableSystemComponents[name] = false
	}
	vars.SpiffeEnabled = true
	// A minimal valid configuration: without one, the Vault KMS component renders no objects.
	vars.VaultKMS = &operatorv1.VaultKMSConfiguration{
		Address: "https://vault.example.com:8200",
		Transit: operatorv1.VaultKMSTransit{KeyName: "dpf", Mount: ptr.To("transit")},
		Auth: operatorv1.VaultKMSAuth{
			Method: operatorv1.VaultKMSAuthMethodToken,
			Token:  &operatorv1.VaultKMSTokenAuth{TokenSecretRef: operatorv1.SecretKeyRef{Name: "vault-token", Key: "token"}},
		},
	}
	vars.OpenTelemetryCollector.Logging.Endpoint = "otel.example.com:4317"
	vars.OpenTelemetryCollector.Metrics.Endpoint = "otel.example.com:4317"
	return vars
}

func objectImages(ctx context.Context, e *bom.Engine, obj client.Object, charts map[string]string) ([]bom.Image, error) {
	content, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}
	u := &unstructured.Unstructured{Object: content}
	if _, ok := obj.(*dpuservicev1.DPUService); ok || u.GetKind() == dpuservicev1.DPUServiceKind {
		svc := &dpuservicev1.DPUService{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(content, svc); err != nil {
			return nil, err
		}
		inst, err := bom.ServiceInstanceFromDPUService(svc)
		if err != nil {
			return nil, err
		}
		inst.Origin = "DPUService " + svc.Name
		services, err := e.Services(ctx, []bom.ServiceInstance{inst}, bom.ServicesOptions{ResolveChart: resolveChart(charts)})
		if err != nil {
			return nil, err
		}
		return services[0].Images, nil
	}
	return e.Images(bom.ImagesFromObject(content), bom.ScopeHost)
}

// operatorChartImages renders the dpf-operator chart with its default values and the
// first-party operator image the release sets.
func operatorChartImages(ctx context.Context, opts Options, charts map[string]string) ([]bom.Image, error) {
	dir, ok := charts["dpf-operator"]
	if !ok {
		return nil, fmt.Errorf("chart dpf-operator not found")
	}
	values := map[string]any{
		"controllerManager": map[string]any{
			"image": map[string]any{"repository": FirstPartyHost + "/dpf-system", "tag": FirstPartyVersion},
		},
	}
	found, err := opts.Engine.RenderImages(ctx, bom.RenderRequest{
		Chart:       bom.ChartRef{Path: dir, Name: "dpf-operator"},
		Values:      values,
		ReleaseName: "dpf-operator",
		Namespace:   operatorNamespace,
	})
	if err != nil {
		return nil, err
	}
	return opts.Engine.Images(found, bom.ScopeHost)
}

// keepalivedManifest is the template of the keepalived DaemonSet the Kamaji cluster manager
// deploys for a DPUCluster with a keepalived VIP, relative to the repository root.
const keepalivedManifest = "internal/clustermanager/kamaji/manifests/keepalived.yaml"

// keepalivedImages renders the keepalived manifest template with placeholder values.
func keepalivedImages(opts Options) ([]bom.Image, error) {
	data, err := os.ReadFile(filepath.Join(opts.RepoRoot, keepalivedManifest))
	if err != nil {
		return nil, err
	}
	tmpl, err := template.New("keepalived").Option("missingkey=error").Parse(string(data))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", keepalivedManifest, err)
	}
	buf := &bytes.Buffer{}
	err = tmpl.Execute(buf, map[string]any{
		"Name":             "dpu-cluster-keepalived",
		"Interface":        "br-dpu",
		"VirtualRouterID":  1,
		"VirtualIP":        "10.0.0.1",
		"NodePort":         30443,
		"NodeSelector":     map[string]string{},
		"ImagePullSecrets": []string{},
		"KeepalivedImage":  FirstPartyHost + "/dpf-keepalived:" + FirstPartyVersion,
	})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", keepalivedManifest, err)
	}
	found, err := bom.ImagesFromManifests(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("%s: %w", keepalivedManifest, err)
	}
	return opts.Engine.Images(found, bom.ScopeHost)
}

// resolveChart maps first-party charts (the example.com placeholder repository) to their local
// directory, listed by name only; other charts are rendered and listed as named.
func resolveChart(charts map[string]string) bom.ChartResolveFunc {
	return func(c bom.ChartRef) (bom.ChartRef, bom.Chart, error) {
		if !isFirstPartyChart(c.RepoURL) {
			return bom.DefaultChartResolve(c)
		}
		dir, ok := charts[c.Name]
		if !ok {
			return bom.ChartRef{}, bom.Chart{}, fmt.Errorf("first-party chart %q not found in the repository", c.Name)
		}
		return bom.ChartRef{Path: dir, Name: c.Name}, bom.Chart{Name: c.Name}, nil
	}
}
