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

package main

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/gomega"
)

const testBOM = `schemaVersion: v1alpha1
images:
  - name: dpf-system
    scope: [dpu, host]
services:
  - name: dpf-storage
    images:
      - name: storage-system
        scope: [dpu]
charts:
  - name: dpf-operator
  - name: dpf-storage
  - repoURL: https://helm.ngc.nvidia.com/nvidia/doca
    name: doca-hbn
    version: 3.5.0
`

func manifest(artifacts string) string {
	return "registry: nvcr.io/nvidia/doca\ntag: v26.10.0\ncommit: abc\nbuilt_at: 2026-10-01T00:00:00Z\nartifacts:\n" + artifacts
}

func TestCheckRelease(t *testing.T) {
	bomPath := writeFile(t, "bom.yaml", testBOM)
	artifact := func(typ, name, scope string) string {
		return "  - type: " + typ + "\n    name: " + name + "\n    ref: nvcr.io/nvidia/doca/" + name + ":v26.10.0\n    scope: " + scope + "\n"
	}
	matching := artifact("image", "dpf-system", "release") +
		artifact("image", "storage-system", "release") +
		artifact("image", "dummydpuservice", "test_helper") +
		artifact("chart", "dpf-operator", "release") +
		artifact("chart", "dpf-storage", "release") +
		artifact("chart", "dummydpuservice-chart", "test_helper")

	t.Run("matches", func(t *testing.T) {
		m := writeFile(t, "manifest.yaml", manifest(matching))
		NewWithT(t).Expect(execute("check-release", "--manifest", m, "--bom", bomPath)).To(Succeed())
	})
	t.Run("release image missing from the BOM", func(t *testing.T) {
		g := NewWithT(t)
		m := writeFile(t, "manifest.yaml", manifest(matching+artifact("image", "vpc-controller", "release")))
		err := execute("check-release", "--manifest", m, "--bom", bomPath)
		g.Expect(err).To(MatchError(ContainSubstring(`release image "vpc-controller" is not in the BOM`)))
		g.Expect(execute("check-release", "--manifest", m, "--bom", bomPath, "--ignore", "vpc-controller")).To(Succeed())
	})
	t.Run("BOM image not released", func(t *testing.T) {
		m := writeFile(t, "manifest.yaml", manifest(artifact("image", "dpf-system", "release")))
		err := execute("check-release", "--manifest", m, "--bom", bomPath)
		NewWithT(t).Expect(err).To(MatchError(ContainSubstring(`BOM image "storage-system" is not a release image`)))
	})
	t.Run("several manifests are united", func(t *testing.T) {
		g := NewWithT(t)
		dpf := writeFile(t, "manifest.yaml", manifest(artifact("image", "dpf-system", "release")))
		vpc := writeFile(t, "vpc-manifest.yaml", manifest(artifact("image", "storage-system", "vpc")))
		g.Expect(execute("check-release", "--manifest", dpf, "--manifest", vpc, "--bom", bomPath)).To(Succeed())
		err := execute("check-release", "--manifest", dpf, "--bom", bomPath)
		g.Expect(err).To(MatchError(ContainSubstring(`BOM image "storage-system" is not a release image`)))
		extra := writeFile(t, "extra.yaml", manifest(artifact("image", "ovn", "vpc")))
		err = execute("check-release", "--manifest", dpf, "--manifest", vpc, "--manifest", extra, "--bom", bomPath)
		g.Expect(err).To(MatchError(ContainSubstring(`release image "ovn" is not in the BOM`)))
	})
	t.Run("--skip-vpc leaves the VPC images out", func(t *testing.T) {
		g := NewWithT(t)
		m := writeFile(t, "manifest.yaml", manifest(matching+artifact("image", "ovn", "vpc")))
		err := execute("check-release", "--manifest", m, "--bom", bomPath)
		g.Expect(err).To(MatchError(ContainSubstring(`release image "ovn" is not in the BOM`)))
		g.Expect(execute("check-release", "--manifest", m, "--bom", bomPath, "--skip-vpc")).To(Succeed())
	})
	t.Run("usage", func(t *testing.T) {
		g := NewWithT(t)
		g.Expect(execute("check-release")).To(MatchError(ContainSubstring(`"manifest" not set`)))
		g.Expect(execute("generate")).To(MatchError(ContainSubstring(`"output" not set`)))
	})
}

func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	NewWithT(t).Expect(os.WriteFile(path, []byte(content), 0o600)).To(Succeed())
	return path
}

// execute runs the airgap-bom command line with the given arguments.
func execute(args ...string) error {
	cmd := newRootCmd()
	cmd.SetArgs(args)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	return cmd.Execute()
}
