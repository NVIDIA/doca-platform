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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"sigs.k8s.io/yaml"
)

// ChartRef locates a helm chart: a local chart directory (Path), or a chart in a repository.
type ChartRef struct {
	// Path is a local chart directory. When set, the other fields only describe the chart.
	Path string
	// RepoURL is the chart repository: an http(s) helm repository, or oci://host/path.
	RepoURL string
	Name    string
	Version string
}

// String describes the chart. A local chart is described by its name, so the description does
// not depend on where the repository is checked out.
func (c ChartRef) String() string {
	if c.Path != "" {
		if c.Name != "" {
			return "local:" + c.Name
		}
		return c.Path
	}
	return strings.TrimSuffix(c.RepoURL, "/") + "/" + c.Name + ":" + c.Version
}

// RenderRequest is a chart to render with values, as helm template does.
type RenderRequest struct {
	Chart ChartRef
	// Values are the user-supplied values, merged over the chart's defaults.
	Values map[string]any
	// ReleaseName defaults to the chart name.
	ReleaseName string
	// Namespace defaults to "default".
	Namespace string
}

// Renderer renders a chart into multi-document YAML of Kubernetes objects, including hooks.
type Renderer interface {
	Render(ctx context.Context, req RenderRequest) ([]byte, error)
}

// HelmRendererOptions configures a HelmRenderer.
type HelmRendererOptions struct {
	// Helm is the helm (v3) binary. Defaults to $HELM, then helm in $PATH.
	Helm string
	// KubeVersion is the Kubernetes version charts are rendered for, e.g. v1.35.6. Empty uses the
	// helm default.
	KubeVersion string
}

// HelmRenderer renders charts with the helm binary (helm template), without a cluster. helm runs
// with private configuration, cache and registry credentials, so the helm and Docker configuration
// of the user is never used and chart downloads are anonymous. A local chart is rendered from a
// copy of its source, whose directory is never written. Staging a local chart has these limits:
//
//   - only the remote dependencies of its Chart.yaml are pulled; those of a subchart are neither
//     pulled nor kept;
//   - Chart.lock is ignored: a dependency is pulled at the version of Chart.yaml;
//   - a symlinked directory in the chart is rejected.
//
// helm template gets no --api-versions, and helm pull no plain-HTTP option.
type HelmRenderer struct {
	opts HelmRendererOptions
	helm string
	// dir holds the helm home directories and the downloaded charts.
	dir string
	env []string

	mu sync.Mutex
	// pulled maps a downloaded chart (repository, name, version) to its archive.
	pulled map[string]string
}

var _ Renderer = &HelmRenderer{}

// errNotHelmV3 is returned for a helm binary of another major version than v3.
var errNotHelmV3 = errors.New("helm v3 is required")

// NewHelmRenderer returns a HelmRenderer. Close it to remove its private directory.
func NewHelmRenderer(opts HelmRendererOptions) (*HelmRenderer, error) {
	helm, err := findHelm(opts.Helm)
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "airgap-bom-helm-")
	if err != nil {
		return nil, err
	}
	h := &HelmRenderer{opts: opts, helm: helm, dir: dir, env: helmEnvironment(dir), pulled: map[string]string{}}
	version, err := h.run(context.Background(), nil, "version", "--template", "{{.Version}}")
	if err != nil {
		_ = h.Close()
		return nil, fmt.Errorf("failed to run %s version: %w", helm, err)
	}
	if v := strings.TrimSpace(string(version)); !strings.HasPrefix(v, "v3.") {
		_ = h.Close()
		return nil, fmt.Errorf("%s is helm %s: %w", helm, v, errNotHelmV3)
	}
	return h, nil
}

func findHelm(path string) (string, error) {
	if path == "" {
		path = os.Getenv("HELM")
	}
	if path == "" {
		path = "helm"
	}
	found, err := exec.LookPath(path)
	if err != nil {
		return "", fmt.Errorf("helm binary %q not found, install helm v3 or point --helm or $HELM at it: %w", path, err)
	}
	return found, nil
}

// helmEnvironment returns the environment of the process with the helm and Docker configuration
// replaced by private ones below dir. The helm registry client falls back to the Docker
// configuration and its credential stores even for an empty registry configuration; a Docker
// configuration directory without a config.json bypasses it.
func helmEnvironment(dir string) []string {
	env := make([]string, 0, len(os.Environ())+8)
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(key, "HELM_") || key == "DOCKER_CONFIG" {
			continue
		}
		env = append(env, kv)
	}
	return append(env,
		"HELM_CACHE_HOME="+filepath.Join(dir, "cache"),
		"HELM_CONFIG_HOME="+filepath.Join(dir, "config"),
		"HELM_DATA_HOME="+filepath.Join(dir, "data"),
		"HELM_PLUGINS="+filepath.Join(dir, "plugins"),
		"HELM_REGISTRY_CONFIG="+filepath.Join(dir, "registry.json"),
		"HELM_REPOSITORY_CONFIG="+filepath.Join(dir, "repositories.yaml"),
		"HELM_REPOSITORY_CACHE="+filepath.Join(dir, "repository"),
		"DOCKER_CONFIG="+filepath.Join(dir, "docker"),
	)
}

// run runs helm with the private environment, stdin as its standard input (optional), and returns
// its standard output.
func (h *HelmRenderer) run(ctx context.Context, stdin []byte, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, h.helm, args...)
	cmd.Env = h.env
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("%w: %s", err, msg)
		}
		return nil, err
	}
	return stdout.Bytes(), nil
}

func (h *HelmRenderer) Close() error {
	return os.RemoveAll(h.dir)
}

func (h *HelmRenderer) Render(ctx context.Context, req RenderRequest) ([]byte, error) {
	work, err := os.MkdirTemp(h.dir, "render-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(work) }()

	chartPath, releaseName, err := h.prepare(ctx, req.Chart, work)
	if err != nil {
		return nil, fmt.Errorf("failed to load chart %s: %w", req.Chart, err)
	}
	if req.ReleaseName != "" {
		releaseName = req.ReleaseName
	}
	namespace := req.Namespace
	if namespace == "" {
		namespace = "default"
	}
	values := req.Values
	if values == nil {
		values = map[string]any{}
	}
	rawValues, err := json.Marshal(values)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal the values of chart %s: %w", req.Chart, err)
	}
	// helm template includes hooks and renders the way an install does; CRDs of the crds/
	// directory are not rendered. The values come from the standard input as JSON, which helm
	// reads as YAML (any JSON document is valid YAML). The flags end with --, so helm takes the
	// release name as an argument even when it starts with --.
	args := []string{"template", "--namespace", namespace, "--values", "-"}
	if h.opts.KubeVersion != "" {
		args = append(args, "--kube-version", h.opts.KubeVersion)
	}
	args = append(args, "--", releaseName, chartPath)
	out, err := h.run(ctx, rawValues, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to render chart %s: %w", req.Chart, err)
	}
	return out, nil
}

// prepare returns what helm template renders (a chart archive, or the staged copy of a local
// chart below work) and the chart name, the default release name.
func (h *HelmRenderer) prepare(ctx context.Context, ref ChartRef, work string) (string, string, error) {
	if ref.Path == "" {
		path, err := h.pull(ctx, ref.RepoURL, ref.Name, ref.Version)
		return path, ref.Name, err
	}
	return h.stageLocalChart(ctx, ref.Path, filepath.Join(work, "chart"))
}

// stageLocalChart copies the chart directory src to dst and returns dst and the chart name. The
// chart archives in charts/ (left by helm dependency update, not part of the repository) are left
// out, so the result does not vary with earlier builds. The remote dependencies Chart.yaml
// declares are downloaded and added to the copy as helm dependency build would.
func (h *HelmRenderer) stageLocalChart(ctx context.Context, src, dst string) (string, string, error) {
	if err := CopyChartSource(src, dst); err != nil {
		return "", "", err
	}
	meta, err := readChartMetadata(dst)
	if err != nil {
		return "", "", err
	}
	for _, dep := range meta.Dependencies {
		if !isRemote(dep.Repository) {
			continue
		}
		archive, err := h.pull(ctx, dep.Repository, dep.Name, dep.Version)
		if err != nil {
			return "", "", fmt.Errorf("failed to download dependency %s %s of chart %s: %w", dep.Name, dep.Version, meta.Name, err)
		}
		if err := copyFile(archive, filepath.Join(dst, "charts", filepath.Base(archive))); err != nil {
			return "", "", err
		}
	}
	return dst, meta.Name, nil
}

func isRemote(repository string) bool {
	return repository != "" && !strings.HasPrefix(repository, "file://")
}

func readChartMetadata(dir string) (*chartMetadata, error) {
	data, err := os.ReadFile(filepath.Join(dir, "Chart.yaml"))
	if err != nil {
		return nil, err
	}
	meta := &chartMetadata{}
	if err := yaml.Unmarshal(data, meta); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", filepath.Join(dir, "Chart.yaml"), err)
	}
	return meta, nil
}

// Pull downloads a chart of a repository into the private directory of the renderer (once) and
// returns its archive, which is removed on Close.
func (h *HelmRenderer) Pull(ctx context.Context, ref ChartRef) (string, error) {
	if ref.Path != "" {
		return "", fmt.Errorf("chart %s is a local chart", ref)
	}
	return h.pull(ctx, ref.RepoURL, ref.Name, ref.Version)
}

// pull downloads a chart into the private directory (once) and returns its archive.
func (h *HelmRenderer) pull(ctx context.Context, repoURL, name, version string) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	key := repoURL + "\x00" + name + "\x00" + version
	if path, ok := h.pulled[key]; ok {
		return path, nil
	}
	dest, err := os.MkdirTemp(h.dir, "pull-")
	if err != nil {
		return "", err
	}
	chart, args := name, []string{"pull"}
	if strings.HasPrefix(repoURL, "oci://") {
		chart = strings.TrimSuffix(repoURL, "/") + "/" + name
	} else {
		args = append(args, "--repo", repoURL)
	}
	if version != "" {
		args = append(args, "--version", version)
	}
	args = append(args, "--destination", dest)
	// Go ignores SSL_CERT_FILE on some platforms (macOS), helm gets it explicitly.
	if caFile := os.Getenv("SSL_CERT_FILE"); caFile != "" {
		args = append(args, "--ca-file", caFile)
	}
	args = append(args, "--", chart)
	if _, err := h.run(ctx, nil, args...); err != nil {
		return "", err
	}
	archives, err := filepath.Glob(filepath.Join(dest, "*.tgz"))
	if err != nil {
		return "", err
	}
	if len(archives) != 1 {
		return "", fmt.Errorf("helm pull of %s/%s:%s wrote %d chart archives, expected 1", strings.TrimSuffix(repoURL, "/"), name, version, len(archives))
	}
	h.pulled[key] = archives[0]
	return archives[0], nil
}

// chartMetadata are the fields of Chart.yaml the renderer reads.
type chartMetadata struct {
	Name         string `json:"name"`
	Dependencies []struct {
		Name       string `json:"name"`
		Version    string `json:"version"`
		Repository string `json:"repository"`
	} `json:"dependencies"`
}

// CopyChartSource copies the chart directory src to dst, without the chart archives in the
// charts/ directories. Symbolic links to files are copied as files; one to a directory is an
// error, as helm's directory loader reports it.
func CopyChartSource(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		if isDependencyArchive(filepath.ToSlash(rel)) {
			return nil
		}
		fi, err := os.Stat(path)
		if err != nil {
			return err
		}
		if fi.IsDir() {
			return fmt.Errorf("cannot load symlinked directory %s of chart %s", filepath.ToSlash(rel), src)
		}
		if !fi.Mode().IsRegular() {
			return fmt.Errorf("cannot load irregular file %s of chart %s", filepath.ToSlash(rel), src)
		}
		return copyFile(path, target)
	})
}

// isDependencyArchive reports whether the slash-separated path, relative to the chart directory, is
// a file directly in the charts/ directory of the chart or of one of its subchart directories
// (charts/<sub>/charts/...) that helm would load as a packaged subchart.
func isDependencyArchive(n string) bool {
	parts := strings.Split(n, "/")
	for i := 0; i+1 < len(parts) && parts[i] == "charts"; i += 2 {
		if i+2 == len(parts) {
			return strings.HasSuffix(parts[i+1], ".tgz") || strings.HasSuffix(parts[i+1], ".tar.gz")
		}
	}
	return false
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o600)
}
