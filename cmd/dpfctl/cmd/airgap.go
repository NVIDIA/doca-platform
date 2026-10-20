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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"

	"github.com/nvidia/doca-platform/internal/airgap/bom"
	"github.com/nvidia/doca-platform/internal/airgap/mirror"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	"github.com/spf13/cobra"
)

const (
	airgapOutputPairs = "pairs"
	airgapOutputJSON  = "json"
)

// airgapOptions are the flags shared by the airgap subcommands.
type airgapOptions struct {
	chart        string
	chartVersion string
	bomFile      string
	dpfRegistry  string
	dpfVersion   string
	dpfChartRepo string
	mirror       string
	layout       string
	scopes       []string
	services     []string
	allServices  bool
	files        []string
	helm         string
	output       string
}

var airgapCmd = &cobra.Command{
	Use:   "airgap",
	Short: "Prepare an air-gapped DPF installation",
	Long: "List what to copy into a mirror registry for an air-gapped DPF installation: the images and helm charts " +
		"of the air-gap bill of materials (BOM) a DPF release ships in its dpf-operator chart, and of the DPUServices " +
		"you deploy. dpfctl only lists the copies; copy them with a tool like skopeo, crane or helm.",
}

func init() {
	rootCmd.AddCommand(airgapCmd)
}

// addAirgapFlags adds the flags shared by the airgap subcommands to cmd.
func addAirgapFlags(cmd *cobra.Command, o *airgapOptions) {
	f := cmd.Flags()
	f.StringVar(&o.chart, "chart", "", "dpf-operator chart of the release: <repository URL>/dpf-operator "+
		"(e.g. https://helm.ngc.nvidia.com/nvidia/doca/dpf-operator or oci://registry/path/dpf-operator), "+
		"or a local chart archive or directory")
	f.StringVar(&o.chartVersion, "version", "", "version of the dpf-operator chart in a repository, e.g. v26.10.0")
	f.StringVar(&o.bomFile, "bom", "", "air-gap BOM file (airgap/airgapped.yaml of the dpf-operator chart), "+
		"instead of --chart; needs --dpf-registry and --dpf-version")
	f.StringVar(&o.dpfRegistry, "dpf-registry", "", "registry of the DPF images, e.g. nvcr.io/nvidia/doca "+
		"(default: the registry of the operator image of the chart)")
	f.StringVar(&o.dpfVersion, "dpf-version", "", "DPF release version, the tag of the DPF images and the "+
		"version of the DPF charts (default: the version of the operator image of the chart)")
	f.StringVar(&o.dpfChartRepo, "dpf-chart-repo", "", "helm repository of the DPF charts (default: the "+
		"repository of --chart)")
	f.StringVar(&o.mirror, "mirror", "", "mirror registry to copy to, host[:port][/path], e.g. mirror.example.com:5000/dpf")
	f.StringVar(&o.layout, "layout", string(mirror.LayoutFlat), "layout of the mirror: flat copies every image to "+
		"<mirror>/<name>:<tag> and every chart to oci://<mirror>; nested keeps the upstream registry and path below "+
		"the mirror, e.g. <mirror>/nvcr.io/nvidia/doca/<name>:<tag>")
	f.StringArrayVar(&o.scopes, "scope", nil, "cluster whose images to list: dpu or host (repeatable, default: both)")
	f.StringArrayVar(&o.services, "service", nil, "optional service of the BOM you deploy, e.g. dpf-storage (repeatable)")
	f.BoolVar(&o.allServices, "all-services", false, "include all the optional services of the BOM")
	f.StringArrayVarP(&o.files, "filename", "f", nil, "file or directory of DPUService, DPUServiceTemplate and "+
		"DPUServiceConfiguration manifests you deploy, or - for standard input; their charts are rendered with "+
		"their values (repeatable)")
	f.StringVar(&o.helm, "helm", "", "helm (v3) binary used to download and render charts (default: $HELM, then helm in $PATH)")
	f.StringVarP(&o.output, "output", "o", airgapOutputPairs, "output format: pairs or json")
	must(cmd.MarkFlagRequired("mirror"))
}

// airgapPlan resolves the release and the mirror options of the flags. It downloads the chart and
// renders the charts of the manifests of -f with helm, reading the -f path "-" from stdin and
// writing progress to logOut.
func airgapPlan(ctx context.Context, o *airgapOptions, stdin io.Reader, logOut io.Writer) (mirror.Release, mirror.Options, error) {
	logf := func(format string, args ...any) { _, _ = fmt.Fprintf(logOut, "[airgap] "+format+"\n", args...) }
	if err := o.validate(); err != nil {
		return mirror.Release{}, mirror.Options{}, err
	}

	var renderer *bom.HelmRenderer
	defer func() {
		if renderer != nil {
			_ = renderer.Close()
		}
	}()
	getRenderer := func() (*bom.HelmRenderer, error) {
		if renderer != nil {
			return renderer, nil
		}
		r, err := bom.NewHelmRenderer(bom.HelmRendererOptions{Helm: o.helm, KubeVersion: cutil.KubernetesVersion})
		if err != nil {
			return nil, err
		}
		renderer = r
		return r, nil
	}

	rel, err := o.release(ctx, getRenderer, logf)
	if err != nil {
		return mirror.Release{}, mirror.Options{}, err
	}
	opts := mirror.Options{
		Mirror:      o.mirror,
		Layout:      mirror.Layout(o.layout),
		Services:    o.services,
		AllServices: o.allServices,
		Logf:        logf,
	}
	for _, s := range o.scopes {
		opts.Scopes = append(opts.Scopes, bom.Scope(s))
	}
	if len(o.files) > 0 {
		r, err := getRenderer()
		if err != nil {
			return mirror.Release{}, mirror.Options{}, err
		}
		if opts.Extra, err = renderServices(ctx, r, stdin, o.files, logf); err != nil {
			return mirror.Release{}, mirror.Options{}, err
		}
	}
	return rel, opts, nil
}

func (o *airgapOptions) validate() error {
	var errs []error
	switch {
	case o.chart == "" && o.bomFile == "":
		errs = append(errs, errors.New("pass the dpf-operator chart of the release with --chart, or its BOM with --bom"))
	case o.chart != "" && o.bomFile != "":
		errs = append(errs, errors.New("--chart and --bom are mutually exclusive"))
	case o.bomFile != "" && (o.dpfRegistry == "" || o.dpfVersion == ""):
		errs = append(errs, errors.New("--bom needs --dpf-registry and --dpf-version: the BOM lists the DPF images by name only"))
	}
	for _, s := range o.scopes {
		if s != string(bom.ScopeDPU) && s != string(bom.ScopeHost) {
			errs = append(errs, fmt.Errorf("invalid --scope %q: expected %s or %s", s, bom.ScopeDPU, bom.ScopeHost))
		}
	}
	if o.layout != string(mirror.LayoutFlat) && o.layout != string(mirror.LayoutNested) {
		errs = append(errs, fmt.Errorf("invalid --layout %q: expected %s or %s", o.layout, mirror.LayoutFlat, mirror.LayoutNested))
	}
	if o.allServices && len(o.services) > 0 {
		errs = append(errs, errors.New("--service and --all-services are mutually exclusive"))
	}
	if i := slices.Index(o.files, "-"); i >= 0 && slices.Contains(o.files[i+1:], "-") {
		errs = append(errs, errors.New("-f - is given twice: standard input can be read once"))
	}
	if o.output != airgapOutputPairs && o.output != airgapOutputJSON {
		errs = append(errs, fmt.Errorf("invalid --output %q: expected %s or %s", o.output, airgapOutputPairs, airgapOutputJSON))
	}
	return errors.Join(errs...)
}

// release reads the release of --bom or --chart, downloading a chart of a repository, and applies
// the --dpf-* overrides.
func (o *airgapOptions) release(ctx context.Context, getRenderer func() (*bom.HelmRenderer, error), logf func(string, ...any)) (mirror.Release, error) {
	var rel mirror.Release
	switch {
	case o.bomFile != "":
		b, err := bom.Load(o.bomFile)
		if err != nil {
			return mirror.Release{}, err
		}
		rel.BOM = b
	case isLocalPath(o.chart):
		var err error
		if rel, err = mirror.ReleaseFromChart(o.chart); err != nil {
			return mirror.Release{}, err
		}
	default:
		repoURL, name, err := mirror.ParseChartURL(o.chart)
		if err != nil {
			return mirror.Release{}, err
		}
		if o.chartVersion == "" {
			return mirror.Release{}, fmt.Errorf("--version is required for chart %s of a repository", o.chart)
		}
		r, err := getRenderer()
		if err != nil {
			return mirror.Release{}, err
		}
		ref := bom.ChartRef{RepoURL: repoURL, Name: name, Version: o.chartVersion}
		logf("downloading chart %s", ref)
		archive, err := r.Pull(ctx, ref)
		if err != nil {
			return mirror.Release{}, fmt.Errorf("failed to download chart %s: %w", ref, err)
		}
		if rel, err = mirror.ReleaseFromChart(archive); err != nil {
			return mirror.Release{}, err
		}
		rel.ChartRepo = repoURL
	}
	if o.dpfRegistry != "" {
		rel.Registry = o.dpfRegistry
	}
	if o.dpfVersion != "" {
		rel.Version = o.dpfVersion
	}
	if o.dpfChartRepo != "" {
		rel.ChartRepo = o.dpfChartRepo
	}
	if rel.Registry == "" || rel.Version == "" {
		return mirror.Release{}, fmt.Errorf("chart %s does not pin the DPF operator image: pass --dpf-registry and --dpf-version", o.chart)
	}
	return rel, nil
}

// renderServices renders the charts of the DPUServices of the manifests of paths, files or
// directories, or "-" for stdin, and returns their images. The images are listed as the charts
// reference them, also DPF images: their tags need not be the release version.
func renderServices(ctx context.Context, r bom.Renderer, stdin io.Reader, paths []string, logf func(string, ...any)) ([]bom.Service, error) {
	var objs []bom.SourceObject
	for _, p := range paths {
		var o []bom.SourceObject
		var err error
		if p == "-" {
			o, err = stdinObjects(stdin)
		} else {
			o, err = bom.LoadObjects(p)
		}
		if err != nil {
			return nil, err
		}
		objs = append(objs, o...)
	}
	instances, err := bom.ServiceInstancesFromObjects(objs, logf)
	if err != nil {
		return nil, err
	}
	if len(instances) == 0 {
		return nil, fmt.Errorf("no DPUService or DPUServiceTemplate found in %v", paths)
	}
	engine := &bom.Engine{Renderer: r, Logf: logf}
	return engine.Services(ctx, instances, bom.ServicesOptions{})
}

// stdinObjects reads the Kubernetes objects from stdin, the -f path "-".
func stdinObjects(stdin io.Reader) ([]bom.SourceObject, error) {
	data, err := io.ReadAll(stdin)
	if err != nil {
		return nil, fmt.Errorf("failed to read standard input: %w", err)
	}
	objs, err := bom.DecodeObjects(data)
	if err != nil {
		return nil, fmt.Errorf("standard input: %w", err)
	}
	out := make([]bom.SourceObject, 0, len(objs))
	for _, o := range objs {
		out = append(out, bom.SourceObject{File: "-", Object: o})
	}
	return out, nil
}

func isLocalPath(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// printAirgapJSON writes v as indented JSON.
func printAirgapJSON(out io.Writer, v any) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
