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

package e2e

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/internal/airgap/mirror"
	"github.com/nvidia/doca-platform/test/utils/airgap"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	machineryruntime "k8s.io/apimachinery/pkg/runtime"
)

// An air-gapped e2e run (AIRGAPPED_TEST=true) points every image the DPU pulls at an in-rack mirror
// registry the CI fills (hack/scripts/do_not_publish/ci-airgap.sh), with the DPU's egress to the
// internet blocked. The objects of the run are changed as the air-gap user guide tells a user to.
const (
	// defaultAirgapMirror is the mirror as the DPU reaches it: the registry the CI runs on the rack
	// gateway, below the airgap prefix.
	defaultAirgapMirror = "10.0.110.253:5000/airgap"
	// defaultAirgapAptMirror is the apt repository of the DPUFlavor packages on the rack gateway.
	defaultAirgapAptMirror = "10.0.110.253:8080"
	// defaultAirgapBOM is the air-gap BOM, relative to the e2e package.
	defaultAirgapBOM = "../../deploy/charts/dpf-operator/airgap/airgapped.yaml"
)

var (
	// airgappedTest is AIRGAPPED_TEST.
	airgappedTest bool
	// airgapMirror maps the images of the run to the mirror; set by applyAirgap.
	airgapMirror *airgap.Mirror
)

// getAirgapEnvVariables reads AIRGAPPED_TEST and, when set, the mirror configuration.
func getAirgapEnvVariables() {
	v, found := os.LookupEnv("AIRGAPPED_TEST")
	if !found || v == "" {
		return
	}
	var err error
	if airgappedTest, err = strconv.ParseBool(v); err != nil {
		panic(fmt.Sprintf("AIRGAPPED_TEST=%q is not a boolean", v))
	}
}

// newAirgapMirror returns the mirror of the run from AIRGAP_MIRROR, AIRGAP_LAYOUT (flat or nested,
// default flat), AIRGAP_APT_MIRROR, AIRGAP_BOM, REGISTRY and TAG.
func newAirgapMirror() *airgap.Mirror {
	registry, found := os.LookupEnv("REGISTRY")
	Expect(found && registry != "").To(BeTrue(), "AIRGAPPED_TEST needs REGISTRY, the registry of the DPF images")
	m, err := airgap.New(airgap.Config{
		Mirror:      envOr("AIRGAP_MIRROR", defaultAirgapMirror),
		Layout:      mirror.Layout(os.Getenv("AIRGAP_LAYOUT")),
		AptMirror:   envOr("AIRGAP_APT_MIRROR", defaultAirgapAptMirror),
		BOMPath:     envOr("AIRGAP_BOM", defaultAirgapBOM),
		DPFRegistry: registry,
		DPFVersion:  tag,
	})
	Expect(err).NotTo(HaveOccurred(), "failed to read the air-gap mirror")
	return m
}

// envOr returns the environment variable key, or def when it is unset or empty.
func envOr(key, def string) string {
	if v, found := os.LookupEnv(key); found && v != "" {
		return v
	}
	return def
}

// applyAirgap points the objects of the run at the mirror: the DPU-side images of the
// DPFOperatorConfig and the Kamaji DPU cluster addons, the DPUFlavor (sandbox image, plain-HTTP
// mirror, apt mirror, no DPU OS internet traffic) and the image values of the DPUServices.
func applyAirgap(t *systemTestInput) {
	if !airgappedTest {
		return
	}
	Expect(isGinkgoLabelApplied(Domain.OCP)).To(BeFalse(), "AIRGAPPED_TEST is not supported for the OCP suites")
	Expect(isGinkgoLabelApplied(Domain.ZeroTrust)).To(BeTrue(),
		"AIRGAPPED_TEST needs ZeroTrust in the label filter, because outside Zero Trust the dpu-agent installs packages past the egress block")
	Expect(t.dpuFlavorTemplate == nil && !useFlavorTemplate()).To(BeTrue(),
		"AIRGAPPED_TEST supports no DPUFlavorTemplate, because the e2e can't point a template at the mirror. Set dpuFlavor, not dpuFlavorTemplate, in the e2e config, and unset %s", useFlavorTemplateEnvVar)

	airgapMirror = newAirgapMirror()
	By("Pointing the DPU images at the air-gap mirror " + envOr("AIRGAP_MIRROR", defaultAirgapMirror))

	set, err := airgapMirror.ApplyToDPFOperatorConfig(t.config)
	Expect(err).NotTo(HaveOccurred())
	GinkgoWriter.Printf("air-gap DPFOperatorConfig:\n  %s\n", strings.Join(set, "\n  "))

	if t.dpuFlavor != nil {
		airgapMirror.ApplyToDPUFlavor(t.dpuFlavor)
	}
	for _, tpl := range []*dpuservicev1.DPUServiceTemplate{t.dpuServiceTemplate, t.additionalDPUServiceTemplate} {
		if tpl != nil {
			tpl.Spec.HelmChart.Values = airgapHelmValues(tpl.Spec.HelmChart.Values)
		}
	}
	if t.dpuService != nil {
		t.dpuService.Spec.HelmChart.Values = airgapHelmValues(t.dpuService.Spec.HelmChart.Values)
	}
	if t.dpuServiceHBN != nil {
		t.dpuServiceHBN.Spec.HelmChart.Values = airgapHelmValues(t.dpuServiceHBN.Spec.HelmChart.Values)
	}
	repo, err := airgapMirror.Repository(netutilsImage)
	Expect(err).NotTo(HaveOccurred())
	netutilsImage = repo
}

// airgapHelmValues points the image repositories of DPUService helm values at the mirror in an
// air-gapped run.
func airgapHelmValues(values *machineryruntime.RawExtension) *machineryruntime.RawExtension {
	if airgapMirror == nil {
		return values
	}
	out, err := airgapMirror.RewriteHelmValues(values)
	Expect(err).NotTo(HaveOccurred())
	return out
}

// dpuImage returns the image a DPU pod pulls: the mirror copy in an air-gapped run.
func dpuImage(image string) string {
	if airgapMirror == nil {
		return image
	}
	mirrored, err := airgapMirror.Image(image)
	Expect(err).NotTo(HaveOccurred())
	return mirrored
}

// dpuRepository returns the image repository a DPU pod pulls from: the mirror repository in an
// air-gapped run.
func dpuRepository(repo string) string {
	if airgapMirror == nil {
		return repo
	}
	mirrored, err := airgapMirror.Repository(repo)
	Expect(err).NotTo(HaveOccurred())
	return mirrored
}

// VerifyAirgapDPUImages checks, in an air-gapped run, that every pod of every DPU cluster runs an
// image of the mirror and every DPU node pulled the sandbox image from it.
func VerifyAirgapDPUImages(ctx context.Context) {
	if airgapMirror == nil {
		return
	}
	By("Verifying the DPU clusters pull every image from the air-gap mirror")
	for i, c := range dpuClusterClient {
		images, err := airgap.PodImages(ctx, c)
		Expect(err).NotTo(HaveOccurred())
		Expect(airgapMirror.Unmirrored(images)).To(BeEmpty(), "DPU cluster %d runs images that are not pulled from the mirror", i)
		nodes, err := airgapMirror.NodesWithoutSandbox(ctx, c)
		Expect(err).NotTo(HaveOccurred())
		Expect(nodes).To(BeEmpty(), "DPU nodes of DPU cluster %d did not pull the sandbox image %s", i, airgapMirror.SandboxImage())
	}
}
