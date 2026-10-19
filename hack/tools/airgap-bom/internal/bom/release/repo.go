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
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"

	"sigs.k8s.io/yaml"
)

var (
	// releaseChartRe matches a Makefile line recording a pushed release chart, e.g.
	// $(call record_release_artifact,chart,$(STORAGE_CHART_NAME),$(HELM_REGISTRY)/$(STORAGE_CHART_NAME):$(TAG),release)
	releaseChartRe = regexp.MustCompile(`record_release_artifact,chart,\$\(([A-Z0-9_]+)\),[^,]*,release\)`)
	makeVarRe      = regexp.MustCompile(`(?m)^(?:export\s+)?([A-Z0-9_]+)\s*[?:]?=\s*(\S+)\s*$`)
)

// ReleaseChartNames returns the names of the charts the Makefile pushes with the release scope
// (helm-push-all over HELM_TARGETS), sorted. Charts of other scopes, such as the test_helper
// dummydpuservice chart, are not release charts.
func ReleaseChartNames(repoRoot string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(repoRoot, "Makefile"))
	if err != nil {
		return nil, err
	}
	return recordedNames(data, releaseChartRe, "chart")
}

// recordedNames returns the values of the Makefile variables naming the release artifacts re
// matches, sorted and unique.
func recordedNames(makefile []byte, re *regexp.Regexp, kind string) ([]string, error) {
	vars := makeVars(makefile)
	var names []string
	for _, m := range re.FindAllSubmatch(makefile, -1) {
		v, ok := vars[string(m[1])]
		if !ok {
			return nil, fmt.Errorf("release %s variable %s is not assigned in the Makefile", kind, m[1])
		}
		names = append(names, v)
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("no release %ss found in the Makefile", kind)
	}
	slices.Sort(names)
	return slices.Compact(names), nil
}

// makeVars returns the simple variable assignments of a Makefile (the first assignment of a
// variable wins, as with ?=).
func makeVars(makefile []byte) map[string]string {
	vars := map[string]string{}
	for _, m := range makeVarRe.FindAllSubmatch(makefile, -1) {
		if _, ok := vars[string(m[1])]; !ok {
			vars[string(m[1])] = string(m[2])
		}
	}
	return vars
}

// localCharts returns the directories of the first-party charts of the repository by chart name:
// the deployment charts and the DPUService charts.
func localCharts(repoRoot string) (map[string]string, error) {
	var files []string
	for _, pattern := range []string{"deploy/charts/*/Chart.yaml", "dpuservices/*/chart/Chart.yaml"} {
		m, err := filepath.Glob(filepath.Join(repoRoot, pattern))
		if err != nil {
			return nil, err
		}
		files = append(files, m...)
	}
	charts := map[string]string{}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		meta := struct {
			Name string `json:"name"`
		}{}
		if err := yaml.Unmarshal(data, &meta); err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		if meta.Name == "" {
			return nil, fmt.Errorf("%s: chart has no name", f)
		}
		if other, ok := charts[meta.Name]; ok {
			return nil, fmt.Errorf("chart %q is defined twice: %s and %s", meta.Name, other, filepath.Dir(f))
		}
		charts[meta.Name] = filepath.Dir(f)
	}
	return charts, nil
}
