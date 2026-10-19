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

package bom

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	dpuserviceutils "github.com/nvidia/doca-platform/internal/dpuservice/utils"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
)

// defaultNamespace is the namespace of a service instance whose object has none.
const defaultNamespace = "dpf-operator-system"

// SourceObject is a Kubernetes object read from a file.
type SourceObject struct {
	File   string
	Object *unstructured.Unstructured
}

func (s SourceObject) String() string {
	return fmt.Sprintf("%s %s/%s", s.File, s.Object.GetKind(), s.Object.GetName())
}

// LoadObjects reads the Kubernetes objects of the *.yaml, *.yml and *.json files below dir, in
// lexical order.
func LoadObjects(dir string) ([]SourceObject, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".yaml", ".yml", ".json":
			if !d.IsDir() {
				files = append(files, path)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	var out []SourceObject
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		objs, err := DecodeObjects(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		for _, o := range objs {
			out = append(out, SourceObject{File: f, Object: o})
		}
	}
	return out, nil
}

// ServiceInstance is one deployment of a DPUService chart: the chart, the helm values the manifests
// set, and the cluster it is deployed to. The values the DPUService controller adds
// (serviceDaemonSet, exposed ports) never set an image, so they are not computed.
type ServiceInstance struct {
	// Chart is the chart as named by the object.
	Chart ChartRef
	// ReleaseName is the helm release name, empty for the chart name.
	ReleaseName string
	// Namespace is the namespace the chart is deployed to.
	Namespace string
	Values    map[string]any
	// Scope is the cluster the chart is deployed to: host for deployInCluster, dpu otherwise.
	Scope Scope
	// Origin identifies the object(s) the instance was computed from.
	Origin string
}

// ServiceInstancesFromObjects computes the service instances of DPUServices, and of
// DPUServiceTemplates combined with the DPUServiceConfigurations of the same deploymentServiceName
// in any namespace (one instance per configuration, or one of its own without). These are all the
// pairs the DPUDeployment controller makes and possibly more: the controller pairs only the objects
// a DPUDeployment names, in its own namespace. A DPUServiceConfiguration without a template is
// dropped and logged to logf (optional). Other kinds are ignored.
//
// For a service with dependsOn, the DPUDeployment controller fills Go templates in the
// configuration values, e.g. {{ .Services.<name>.Name }}, with the name and namespace of the
// DPUServices it depends on. These values are kept as written: they name services, not images.
func ServiceInstancesFromObjects(objs []SourceObject, logf func(format string, args ...any)) ([]ServiceInstance, error) {
	var (
		out       []ServiceInstance
		errs      []error
		configs   = map[string][]configWithSource{}
		templates = map[string]bool{}
	)
	for _, o := range objs {
		if o.Object.GetKind() != dpuservicev1.DPUServiceConfigurationKind {
			continue
		}
		cfg := &dpuservicev1.DPUServiceConfiguration{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(o.Object.Object, cfg); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", o, err))
			continue
		}
		configs[cfg.Spec.DeploymentServiceName] = append(configs[cfg.Spec.DeploymentServiceName], configWithSource{cfg, o})
	}
	for _, o := range objs {
		var (
			inst ServiceInstance
			err  error
		)
		switch o.Object.GetKind() {
		case dpuservicev1.DPUServiceKind:
			svc := &dpuservicev1.DPUService{}
			if err = runtime.DefaultUnstructuredConverter.FromUnstructured(o.Object.Object, svc); err == nil {
				inst, err = ServiceInstanceFromDPUService(svc)
			}
			out, errs = appendInstance(out, errs, inst, err, o.String())
		case dpuservicev1.DPUServiceTemplateKind:
			tpl := &dpuservicev1.DPUServiceTemplate{}
			if err = runtime.DefaultUnstructuredConverter.FromUnstructured(o.Object.Object, tpl); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", o, err))
				continue
			}
			templates[tpl.Spec.DeploymentServiceName] = true
			cfgs := configs[tpl.Spec.DeploymentServiceName]
			if len(cfgs) == 0 {
				cfgs = []configWithSource{{}}
			}
			for _, c := range cfgs {
				origin := o.String()
				if c.config != nil {
					origin += " + " + c.source.String()
				}
				inst, err = ServiceInstanceFromTemplate(tpl, c.config)
				out, errs = appendInstance(out, errs, inst, err, origin)
			}
		}
	}
	for _, name := range slices.Sorted(maps.Keys(configs)) {
		if templates[name] || logf == nil {
			continue
		}
		for _, c := range configs[name] {
			logf("ignoring %s: no DPUServiceTemplate has deploymentServiceName %q", c.source, name)
		}
	}
	return out, errors.Join(errs...)
}

// appendInstance appends inst, computed from the objects described by origin, to the instances, or
// err to the errors.
func appendInstance(out []ServiceInstance, errs []error, inst ServiceInstance, err error, origin string) ([]ServiceInstance, []error) {
	if err != nil {
		return out, append(errs, fmt.Errorf("%s: %w", origin, err))
	}
	inst.Origin = origin
	return append(out, inst), errs
}

type configWithSource struct {
	config *dpuservicev1.DPUServiceConfiguration
	source SourceObject
}

// ServiceInstanceFromDPUService returns the service instance of a DPUService.
func ServiceInstanceFromDPUService(svc *dpuservicev1.DPUService) (ServiceInstance, error) {
	return newServiceInstance(svc.Name, svc.Namespace, svc.Spec.HelmChart.Source, svc.Spec.HelmChart.Values, svc.Spec.DeployInCluster)
}

// ServiceInstanceFromTemplate returns the service instance of the DPUService a DPUDeployment
// generates from a DPUServiceTemplate and an optional DPUServiceConfiguration: the configuration
// values merged over the template values, deployed in the cluster the configuration says.
func ServiceInstanceFromTemplate(tpl *dpuservicev1.DPUServiceTemplate, cfg *dpuservicev1.DPUServiceConfiguration) (ServiceInstance, error) {
	values, err := dpuserviceutils.MergeServiceTemplateAndConfigurationValues(tpl, cfg)
	if err != nil {
		return ServiceInstance{}, err
	}
	var deployInCluster *bool
	if cfg != nil {
		deployInCluster = cfg.Spec.ServiceConfiguration.DeployInCluster
	}
	return newServiceInstance(tpl.Spec.DeploymentServiceName, tpl.Namespace, tpl.Spec.HelmChart.Source, values, deployInCluster)
}

func newServiceInstance(name, namespace string, src dpuservicev1.ApplicationSource, values *runtime.RawExtension, deployInCluster *bool) (ServiceInstance, error) {
	if src.Chart == "" {
		return ServiceInstance{}, fmt.Errorf("DPUService %s: only helm repository charts are supported, chart is empty", name)
	}
	inst := ServiceInstance{
		Chart:       ChartRef{RepoURL: src.RepoURL, Name: src.Chart, Version: src.Version},
		ReleaseName: src.ReleaseName,
		Namespace:   cmp.Or(namespace, defaultNamespace),
		Values:      map[string]any{},
		Scope:       ScopeDPU,
	}
	if values != nil {
		if err := json.Unmarshal(values.Raw, &inst.Values); err != nil {
			return ServiceInstance{}, fmt.Errorf("DPUService %s: failed to read the helm values: %w", name, err)
		}
	}
	if ptr.Deref(deployInCluster, false) {
		inst.Scope = ScopeHost
	}
	return inst, nil
}

// ChartResolveFunc maps the chart of a service instance to the chart to render and the chart
// identity listed in the BOM.
type ChartResolveFunc func(ChartRef) (render ChartRef, listed Chart, err error)

// unresolvedVarRe matches a $NAME or ${NAME} variable left in a value.
var unresolvedVarRe = regexp.MustCompile(`\$\{?[A-Za-z_][A-Za-z0-9_]*\}?`)

// DefaultChartResolve renders the chart as named and lists it with its repository and version. A
// chart with a variable left ($NAME or ${NAME}), e.g. of a manifest whose environment variables
// were not substituted, is an error naming the variables.
func DefaultChartResolve(c ChartRef) (ChartRef, Chart, error) {
	if s := c.RepoURL + " " + c.Name + " " + c.Version; strings.Contains(s, "$") {
		vars := unresolvedVarRe.FindAllString(s, -1)
		slices.Sort(vars)
		vars = slices.Compact(vars)
		noun := "an unresolved variable"
		if len(vars) > 1 {
			noun = "unresolved variables"
		}
		return ChartRef{}, Chart{}, fmt.Errorf("chart %s has %s %s", c, noun, strings.Join(vars, ", "))
	}
	return c, Chart{RepoURL: c.RepoURL, Name: c.Name, Version: c.Version}, nil
}

// ServicesOptions configures Engine.Services.
type ServicesOptions struct {
	// ResolveChart defaults to DefaultChartResolve.
	ResolveChart ChartResolveFunc
}

// Services renders every instance and returns one BOM service per listed chart, with the union of
// the images (and their scopes) of its instances.
func (e *Engine) Services(ctx context.Context, instances []ServiceInstance, opts ServicesOptions) ([]Service, error) {
	resolve := opts.ResolveChart
	if resolve == nil {
		resolve = DefaultChartResolve
	}
	var out []Service
	var errs []error
	for _, inst := range instances {
		renderRef, listed, err := resolve(inst.Chart)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", inst.Origin, err))
			continue
		}
		found, err := e.RenderImages(ctx, RenderRequest{Chart: renderRef, Values: inst.Values, ReleaseName: inst.ReleaseName, Namespace: inst.Namespace})
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", inst.Origin, err))
			continue
		}
		images, err := e.Images(found, inst.Scope)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", inst.Origin, err))
			continue
		}
		out = append(out, Service{Chart: listed, Images: images})
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	// Normalize merges the instances of one chart.
	b := &BOM{Services: out}
	b.Normalize()
	return b.Services, nil
}
