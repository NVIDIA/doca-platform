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

// airgap-bom generates the air-gap bill of materials (BOM) of DPF during a release and checks it
// against the release.
//
// The BOM is committed in the dpf-operator chart, so every build of the chart ships it.
// "make generate-airgapped-yaml" regenerates it and "make verify-airgapped-yaml" (run by
// "make verify" and "make release") fails when the committed file is out of date. Both need the
// network and a helm (v3) binary (--helm, else $HELM, else helm in $PATH). The schema is documented
// in package bom, what is harvested in package release.
//
// "make release-check-airgap-bom" runs check-release after the images are pushed and before any
// chart is published: the first-party images of the BOM must be exactly the release and vpc images
// of the release manifests.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"slices"
	"strings"

	"github.com/nvidia/doca-platform/hack/tools/airgap-bom/internal/bom"
	"github.com/nvidia/doca-platform/hack/tools/airgap-bom/internal/bom/release"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"
)

const defaultBOM = "deploy/charts/dpf-operator/airgap/airgapped.yaml"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := newRootCmd().ExecuteContext(ctx); err != nil {
		stop()
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:          "airgap-bom",
		Short:        "Generate the air-gap bill of materials of DPF and check it against a release",
		SilenceUsage: true,
	}
	root.AddCommand(
		newGenerateCmd(),
		newCheckReleaseCmd(),
	)
	return root
}

func newGenerateCmd() *cobra.Command {
	var f generateFlags
	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Write the air-gap BOM",
		Long: "Write the air-gap BOM (make generate-airgapped-yaml). It needs the network to download remote charts " +
			"and to read the kubeadm constants of the Kamaji Kubernetes version.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return generateBOM(cmd.Context(), f)
		},
	}
	cmd.Flags().StringVar(&f.repoRoot, "repo-root", ".", "root of the repository checkout")
	cmd.Flags().StringVar(&f.output, "output", "", "BOM file to write, relative to the repository root, "+
		"e.g. "+defaultBOM)
	cmd.Flags().StringVar(&f.helm, "helm", "", "helm (v3) binary used to render the charts (default: $HELM, then helm in $PATH)")
	_ = cmd.MarkFlagRequired("output")
	return cmd
}

func generateBOM(ctx context.Context, f generateFlags) error {
	if err := os.Chdir(f.repoRoot); err != nil {
		return err
	}
	// The renderer does not use the helm and Docker configuration of the user: generation is
	// hermetic and anonymous, as a user air-gapping a release is.
	renderer, err := bom.NewHelmRenderer(bom.HelmRendererOptions{Helm: f.helm, KubeVersion: cutil.KubernetesVersion})
	if err != nil {
		return err
	}
	defer func() { _ = renderer.Close() }()
	engine := &bom.Engine{Renderer: renderer, Logf: logf}
	b, err := release.Release(ctx, release.Options{RepoRoot: ".", Engine: engine, Logf: logf})
	if err != nil {
		return err
	}
	if err := b.WriteFile(f.output, bom.Header); err != nil {
		return err
	}
	logf("wrote %s: %d images, %d services, %d charts", f.output, len(b.Images), len(b.Services), len(b.Charts))
	return nil
}

func newCheckReleaseCmd() *cobra.Command {
	var f checkReleaseFlags
	cmd := &cobra.Command{
		Use:   "check-release",
		Short: "Check the BOM against the release manifests",
		Long: "Fail unless the release- and vpc-scope images of the release manifests are exactly the first-party " +
			"images of the BOM; ignored names are dropped from both sides. Charts are not compared: the release " +
			"manifests record them only after the check.",
		Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return checkRelease(f)
		},
	}
	cmd.Flags().StringArrayVar(&f.manifests, "manifest", nil, "release manifest, e.g. artifacts/release-manifest.yaml "+
		"(repeatable: the union of the release images is checked)")
	cmd.Flags().StringVar(&f.bom, "bom", defaultBOM, "BOM file")
	cmd.Flags().StringArrayVar(&f.ignore, "ignore", nil, "image name left out of the check on both sides, the release "+
		"manifests and the BOM, e.g. built by another repository (repeatable)")
	cmd.Flags().BoolVar(&f.skipVPC, "skip-vpc", false, "leave the images of the VPC charts out of the check, for a release "+
		"without the VPC build")
	_ = cmd.MarkFlagRequired("manifest")
	return cmd
}

func checkRelease(f checkReleaseFlags) error {
	b, err := bom.Load(f.bom)
	if err != nil {
		return err
	}
	if f.skipVPC {
		f.ignore = append(f.ignore, release.VPCImageNames()...)
	}
	var released []string
	for _, path := range f.manifests {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		manifest := releaseManifest{}
		if err := yaml.Unmarshal(data, &manifest); err != nil {
			return fmt.Errorf("failed to parse %s: %w", path, err)
		}
		for _, a := range manifest.Artifacts {
			if a.Type == "image" && slices.Contains(release.ReleaseManifestScopes, a.Scope) && !slices.Contains(f.ignore, a.Name) {
				released = append(released, a.Name)
			}
		}
	}
	released = slices.Compact(slices.Sorted(slices.Values(released)))
	bomNames := slices.DeleteFunc(b.Names(), func(n string) bool { return slices.Contains(f.ignore, n) })
	var errs []error
	for _, n := range released {
		if !slices.Contains(bomNames, n) {
			errs = append(errs, fmt.Errorf("release image %q is not in the BOM", n))
		}
	}
	for _, n := range bomNames {
		if !slices.Contains(released, n) {
			errs = append(errs, fmt.Errorf("BOM image %q is not a release image of the manifest", n))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s does not match %s:\n%w", f.bom, strings.Join(f.manifests, ", "), errors.Join(errs...))
	}
	logf("%s matches the %d release images of %s", f.bom, len(released), strings.Join(f.manifests, ", "))
	return nil
}

func logf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[airgap-bom] "+format+"\n", args...)
}

type generateFlags struct {
	repoRoot string
	output   string
	helm     string
}

// releaseManifest is the release manifest the Makefile records (release-manifest-init and
// record_release_artifact).
type releaseManifest struct {
	Registry  string `json:"registry"`
	Tag       string `json:"tag"`
	Artifacts []struct {
		Type  string `json:"type"`
		Name  string `json:"name"`
		Ref   string `json:"ref"`
		Scope string `json:"scope"`
	} `json:"artifacts"`
}

type checkReleaseFlags struct {
	manifests []string
	bom       string
	ignore    []string
	skipVPC   bool
}
