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
	"testing"

	"github.com/nvidia/doca-platform/internal/airgap/bom"

	. "github.com/onsi/gomega"
)

// writeRepo writes files (path relative to the root: content) into a temporary repository.
func writeRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	g := NewWithT(t)
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		g.Expect(os.MkdirAll(filepath.Dir(path), 0o755)).To(Succeed())
		g.Expect(os.WriteFile(path, []byte(content), 0o600)).To(Succeed())
	}
	return dir
}

// TestSourceInstances keeps the chart repository and version a manifest sets, and maps the empty
// ones of a first-party chart to the placeholders; a chart without them that is not first-party
// fails, as does a chart with only one of them.
func TestSourceInstances(t *testing.T) {
	g := NewWithT(t)
	repo := writeRepo(t, map[string]string{
		"src/a.yaml":    svcTemplate("a", "https://charts.example.org", "a", "1.0.0", "    values:\n      image: nvcr.io/x/y:1\n"),
		"src/c.yaml":    svcTemplate("c", "", "dpf-storage", "", ""),
		"src/vals.yaml": "not: an object\n",
		"bad/d.yaml":    svcTemplate("d", "", "doca-hbn", "", ""),
		"half/e.yaml":   svcTemplate("e", "", "dpf-storage", FirstPartyVersion, ""),
	})
	opts := Options{RepoRoot: repo}
	charts := map[string]string{"dpf-storage": "/charts/dpf-storage"}

	inst, err := sourceInstances(opts, charts, "src")
	g.Expect(err).NotTo(HaveOccurred())
	byChart := map[string]bom.ServiceInstance{}
	for _, i := range inst {
		byChart[i.Chart.Name] = i
	}
	g.Expect(byChart).To(HaveLen(2))
	g.Expect(byChart["a"].Chart).To(Equal(bom.ChartRef{RepoURL: "https://charts.example.org", Name: "a", Version: "1.0.0"}))
	g.Expect(byChart["a"].Values).To(HaveKeyWithValue("image", "nvcr.io/x/y:1"))
	g.Expect(byChart["a"].Origin).To(HavePrefix("src/a.yaml"))
	g.Expect(byChart["dpf-storage"].Chart).To(Equal(bom.ChartRef{RepoURL: "oci://" + FirstPartyHost, Name: "dpf-storage", Version: FirstPartyVersion}))

	_, err = sourceInstances(opts, charts, "bad")
	g.Expect(err).To(MatchError(ContainSubstring(`chart "doca-hbn" has no repository or version`)))
	_, err = sourceInstances(opts, charts, "half")
	g.Expect(err).To(MatchError(ContainSubstring(`chart "dpf-storage": set both repoURL and version`)))
}

func svcTemplate(name, repoURL, chart, version, values string) string {
	return fmt.Sprintf(`apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: %[1]s
  namespace: dpf-operator-system
spec:
  deploymentServiceName: %[1]s
  helmChart:
    source:
      repoURL: %[2]q
      chart: %[3]s
      version: %[4]q
%[5]s`, name, repoURL, chart, version, values)
}
