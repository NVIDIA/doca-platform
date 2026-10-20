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
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/nvidia/doca-platform/internal/airgap/bom"
)

// serviceSources are the directories the services of the BOM are read from: the DPUServices,
// DPUServiceTemplates and DPUServiceConfigurations below them, rendered with the values they set.
// A service is listed by its chart: first-party charts (a local chart, or a VPC chart) by name,
// others with their repository and version.
var serviceSources = []string{
	// The DPUServices of the DPF storage e2e tests, each enabling one component of the dpf-storage
	// chart. Their chart repository and version are set when the tests run: the DPF build.
	"test/objects/storage",
}

// harvestServices returns the services of serviceSources and the VPC charts.
func harvestServices(ctx context.Context, opts Options, charts map[string]string) ([]bom.Service, error) {
	var (
		instances []bom.ServiceInstance
		errs      []error
	)
	for _, dir := range serviceSources {
		inst, err := sourceInstances(opts, charts, dir)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", dir, err))
			continue
		}
		instances = append(instances, inst...)
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	services, err := opts.Engine.Services(ctx, instances, bom.ServicesOptions{ResolveChart: resolveChart(charts)})
	if err != nil {
		return nil, err
	}
	// VPC charts are never rendered: their images are listed as those of the release.
	return append(services, vpcServices()...), nil
}

// sourceInstances returns the service instances of the manifests below dir, relative to the
// repository root. A chart without repository and version is one of the DPF build and must be a
// first-party chart; a chart with only one of them is an error.
func sourceInstances(opts Options, charts map[string]string, dir string) ([]bom.ServiceInstance, error) {
	objs, err := bom.LoadObjects(filepath.Join(opts.RepoRoot, dir))
	if err != nil {
		return nil, err
	}
	relativeFiles(opts.RepoRoot, objs)
	instances, err := bom.ServiceInstancesFromObjects(objs, opts.Logf)
	if err != nil {
		return nil, err
	}
	for i := range instances {
		c := &instances[i].Chart
		switch {
		case c.RepoURL != "" && c.Version != "":
			continue
		case c.RepoURL != "" || c.Version != "":
			return nil, fmt.Errorf("%s: chart %q: set both repoURL and version, or neither for a first-party chart", instances[i].Origin, c.Name)
		}
		if _, local := charts[c.Name]; !local {
			return nil, fmt.Errorf("%s: chart %q has no repository or version and is not a first-party chart", instances[i].Origin, c.Name)
		}
		c.RepoURL, c.Version = "oci://"+FirstPartyHost, FirstPartyVersion
	}
	return instances, nil
}

func relativeFiles(repoRoot string, objs []bom.SourceObject) {
	for i := range objs {
		if rel, err := filepath.Rel(repoRoot, objs[i].File); err == nil {
			objs[i].File = rel
		}
	}
}
