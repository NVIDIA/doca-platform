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

package mirror

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/nvidia/doca-platform/internal/airgap/bom"

	"sigs.k8s.io/yaml"
)

// BOMPath is the path of the air-gap BOM in the dpf-operator chart.
const BOMPath = "airgap/airgapped.yaml"

// ReleaseFromChart reads the release of a dpf-operator chart, a chart archive or directory: the BOM
// it ships, and the registry and version of the first-party images from the image of the operator
// (values controllerManager.image). A chart packaged by the DPF release pins the image of the
// operator; a chart of the repository does not, so its registry and version stay empty.
func ReleaseFromChart(chartPath string) (Release, error) {
	files, err := readChartFiles(chartPath, "Chart.yaml", "values.yaml", BOMPath)
	if err != nil {
		return Release{}, err
	}
	if files[BOMPath] == nil {
		return Release{}, fmt.Errorf("chart %s has no air-gap BOM %s: is it a dpf-operator chart of a release that ships one?", chartPath, BOMPath)
	}
	b, err := bom.Parse(files[BOMPath])
	if err != nil {
		return Release{}, fmt.Errorf("chart %s: %s: %w", chartPath, BOMPath, err)
	}
	var meta struct {
		Name       string `json:"name"`
		Version    string `json:"version"`
		AppVersion string `json:"appVersion"`
	}
	if err := yaml.Unmarshal(files["Chart.yaml"], &meta); err != nil {
		return Release{}, fmt.Errorf("chart %s: Chart.yaml: %w", chartPath, err)
	}
	var values struct {
		ControllerManager struct {
			Image struct {
				Repository string `json:"repository"`
				Tag        string `json:"tag"`
			} `json:"image"`
		} `json:"controllerManager"`
	}
	if err := yaml.Unmarshal(files["values.yaml"], &values); err != nil {
		return Release{}, fmt.Errorf("chart %s: values.yaml: %w", chartPath, err)
	}
	rel := Release{BOM: b}
	if image := values.ControllerManager.Image; image.Repository != "" {
		ref, err := bom.ParseImageRef(image.Repository)
		if err != nil {
			return Release{}, fmt.Errorf("chart %s: values controllerManager.image.repository: %w", chartPath, err)
		}
		rel.Registry = path.Dir(ref.Repository())
		// The operator image is tagged with the release version, the chart's appVersion unless
		// the values pin another tag.
		rel.Version = firstNonEmpty(image.Tag, meta.AppVersion, meta.Version)
	}
	return rel, nil
}

// readChartFiles returns the files, by their path relative to the chart, of a chart directory or
// archive. A missing file is nil.
func readChartFiles(chartPath string, names ...string) (map[string][]byte, error) {
	fi, err := os.Stat(chartPath)
	if err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	if fi.IsDir() {
		for _, name := range names {
			data, err := os.ReadFile(filepath.Join(chartPath, filepath.FromSlash(name)))
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
			out[name] = data
		}
		return out, nil
	}
	f, err := os.Open(chartPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("chart %s is neither a directory nor a chart archive: %w", chartPath, err)
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read chart archive %s: %w", chartPath, err)
		}
		// The files of a chart archive are below a directory named after the chart; those of
		// its subcharts below <chart>/charts/, which are not read.
		_, rel, ok := strings.Cut(path.Clean(hdr.Name), "/")
		if !ok || hdr.Typeflag != tar.TypeReg {
			continue
		}
		for _, name := range names {
			if rel == name && out[name] == nil {
				if out[name], err = io.ReadAll(tr); err != nil {
					return nil, fmt.Errorf("failed to read %s of chart archive %s: %w", name, chartPath, err)
				}
			}
		}
	}
	return out, nil
}

// ParseChartURL splits the URL of a chart in a repository, <repository URL>/<chart name>, e.g.
// https://helm.ngc.nvidia.com/nvidia/doca/dpf-operator or oci://ghcr.io/org/charts/dpf-operator.
func ParseChartURL(s string) (repoURL, name string, err error) {
	s = strings.TrimSuffix(s, "/")
	i := strings.LastIndex(s, "/")
	scheme, _, _ := strings.Cut(s, "://")
	if !strings.Contains(s, "://") || i <= len(scheme)+len("://") {
		return "", "", fmt.Errorf("invalid chart %q: expected a local chart, or <repository URL>/<chart name> "+
			"with an http(s) or oci:// repository URL", s)
	}
	switch scheme {
	case "http", "https", "oci":
	default:
		return "", "", fmt.Errorf("invalid chart %q: expected an http(s) or oci:// repository URL", s)
	}
	return s[:i], s[i+1:], nil
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}
