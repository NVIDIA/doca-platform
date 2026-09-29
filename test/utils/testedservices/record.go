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

// Package testedservices dumps Ready cluster CRs for the tested-services BOM.
package testedservices

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/conditions"

	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

const collectBOMEnabledValue = "true"

const chartDigestAnnotation = "tested-services.dpu.nvidia.com/chart-sha256"

// chartDigestTimeout bounds each registry lookup so a stalled NGC/Harbor
// connection cannot hold ReportAfterEach or AfterSuite until the job timeout.
const chartDigestTimeout = 30 * time.Second

// RecordReadyDPUServices writes Ready external DPUService CRs to
// ARTIFACTS_DIR/dpu-services/<job> when DPF_E2E_COLLECT_BOM=true. It is a no-op otherwise.
// DPF operator system components (flannel, sfc-controller, …) and first-party
// test charts are skipped; those versions are the DPF tag.
func RecordReadyDPUServices(ctx context.Context, testClient client.Client) {
	outDir, collecting := bomDumpDir("dpu-services")
	if !collecting {
		return
	}
	list := &dpuservicev1.DPUServiceList{}
	Expect(testClient.List(ctx, list)).To(Succeed())
	for i := range list.Items {
		svc := &list.Items[i]
		if !shouldRecordExternalDPUService(svc) {
			continue
		}
		addChartDigest(ctx, svc)
		writeCRDump(outDir, svc, dpuservicev1.DPUServiceGroupVersionKind)
	}
}

// shouldRecordExternalDPUService is the tested-services BOM inclusion rule.
// Relaxing it puts extra rows in artifacts/tested-services-bom.yaml (stable promote).
//
// Skip: not Ready (version not actually tested); operator system DPUServices
// (label dpu.nvidia.com/component: flannel, sfc-controller, multus, kata, …)
// whose version is the DPF tag; empty/hello-world/dummydpuservice-chart
// (e2e harness, not a product service).
func shouldRecordExternalDPUService(svc *dpuservicev1.DPUService) bool {
	if !conditions.IsTrue(svc, conditions.TypeReady) {
		return false
	}
	if _, isDPFSystem := svc.GetLabels()[operatorv1.DPFComponentLabelKey]; isDPFSystem {
		return false
	}
	switch svc.Spec.HelmChart.Source.Chart {
	case "", "hello-world", "dummydpuservice-chart":
		return false
	}
	return true
}

// chartDigests holds the first digest seen for a chart identity in this run.
// Callers re-dump the same DPUService as the suite progresses, and a chart
// version is mutable, so the earliest lookup is the closest to what was
// installed and must win over any later one.
var chartDigests sync.Map

// addChartDigest pins the chart on the artifact copy. HTTP: index.yaml package
// digest. OCI: manifest digest, which stays pullable after a retag.
func addChartDigest(ctx context.Context, svc *dpuservicev1.DPUService) {
	source := svc.Spec.HelmChart.Source
	key := source.RepoURL + "|" + source.Chart + "|" + source.Version
	digest, cached := chartDigests.Load(key)
	if !cached {
		resolved, err := chartDigest(ctx, source)
		Expect(err).NotTo(HaveOccurred(), "chart digest for %s %s from %s", source.Chart, source.Version, source.RepoURL)
		Expect(resolved).NotTo(BeEmpty(), "chart digest for %s %s from %s", source.Chart, source.Version, source.RepoURL)
		digest, _ = chartDigests.LoadOrStore(key, resolved)
	}

	if svc.Annotations == nil {
		svc.Annotations = map[string]string{}
	}
	svc.Annotations[chartDigestAnnotation] = digest.(string)
}

// chartDigest asks the registry what this chart version resolves to now.
func chartDigest(ctx context.Context, source dpuservicev1.ApplicationSource) (string, error) {
	lookupCtx, cancel := context.WithTimeout(ctx, chartDigestTimeout)
	defer cancel()

	if strings.HasPrefix(source.RepoURL, "oci://") {
		repo, err := remote.NewRepository(strings.TrimPrefix(strings.TrimRight(source.RepoURL, "/"), "oci://") + "/" + source.Chart)
		if err != nil {
			return "", err
		}
		httpClient := &http.Client{Timeout: chartDigestTimeout}
		if key := os.Getenv("NGC_API_KEY"); key != "" && isNGCRegistry(repo.Reference.Registry) {
			repo.Client = &auth.Client{Client: httpClient, Credential: auth.StaticCredential(repo.Reference.Registry, auth.Credential{Username: "$oauthtoken", Password: key})}
		} else {
			repo.Client = httpClient
		}
		desc, err := repo.Resolve(lookupCtx, source.Version)
		if err != nil {
			return "", err
		}
		return desc.Digest.String(), nil
	}

	req, err := http.NewRequestWithContext(lookupCtx, http.MethodGet, strings.TrimRight(source.RepoURL, "/")+"/index.yaml", nil)
	if err != nil {
		return "", err
	}
	if key := os.Getenv("NGC_API_KEY"); key != "" && isNGCRegistry(req.URL.Hostname()) {
		req.SetBasicAuth("$oauthtoken", key)
	}
	resp, err := (&http.Client{Timeout: chartDigestTimeout}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close() //nolint:errcheck
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	// Parse the index.yaml file into a struct.
	var index struct {
		Entries map[string][]struct {
			Version string `json:"version"`
			Digest  string `json:"digest"`
		} `json:"entries"`
	}
	if err := yaml.Unmarshal(body, &index); err != nil {
		return "", err
	}
	for _, chart := range index.Entries[source.Chart] {
		if chart.Version == source.Version {
			return "sha256:" + strings.TrimPrefix(chart.Digest, "sha256:"), nil
		}
	}
	return "", fmt.Errorf("chart %s version %s not listed by %s", source.Chart, source.Version, source.RepoURL)
}

// isNGCRegistry is true when host is an NGC registry we may send NGC_API_KEY to.
// Compared on the parsed hostname only (nvcr.io, ngc.nvidia.com, *.ngc.nvidia.com).
func isNGCRegistry(host string) bool {
	host = strings.ToLower(host)
	if host == "nvcr.io" || host == "ngc.nvidia.com" {
		return true
	}
	return strings.HasSuffix(host, ".ngc.nvidia.com")
}

// RecordReadyBFBs writes Ready BFB CRs to ARTIFACTS_DIR/bfbs/<job> when
// DPF_E2E_COLLECT_BOM=true. It is a no-op otherwise.
func RecordReadyBFBs(ctx context.Context, testClient client.Client) {
	outDir, collecting := bomDumpDir("bfbs")
	if !collecting {
		return
	}
	list := &provisioningv1.BFBList{}
	Expect(testClient.List(ctx, list)).To(Succeed())
	for i := range list.Items {
		bfb := &list.Items[i]
		if !shouldRecordBFB(bfb) {
			continue
		}
		writeCRDump(outDir, bfb, provisioningv1.BFBGroupVersionKind)
	}
}

// shouldRecordBFB is the BFB tested-services BOM inclusion rule.
// Relaxing it puts extra firmware rows in artifacts/tested-services-bom.yaml.
//
// Skip: not Ready (file not actually used); fake e2e harness BFB (test-bfb.bfb).
func shouldRecordBFB(bfb *provisioningv1.BFB) bool {
	if bfb.Status.Phase != provisioningv1.BFBReady {
		return false
	}
	if strings.Contains(bfb.Spec.URL, "test-bfb.bfb") {
		return false
	}
	return true
}

// RecordReadyBlueFieldSoftware writes Ready BlueFieldSoftware CRs to
// ARTIFACTS_DIR/bluefieldsoftwares/<job> when DPF_E2E_COLLECT_BOM=true.
// It is a no-op otherwise.
func RecordReadyBlueFieldSoftware(ctx context.Context, testClient client.Client) {
	outDir, collecting := bomDumpDir("bluefieldsoftwares")
	if !collecting {
		return
	}
	list := &provisioningv1.BlueFieldSoftwareList{}
	Expect(testClient.List(ctx, list)).To(Succeed())
	for i := range list.Items {
		bfs := &list.Items[i]
		if !shouldRecordBlueFieldSoftware(bfs) {
			continue
		}
		writeCRDump(outDir, bfs, provisioningv1.BlueFieldGroupVersionKind)
	}
}

// shouldRecordBlueFieldSoftware is the BlueFieldSoftware BOM inclusion rule.
// Relaxing it puts extra firmware rows in artifacts/tested-services-bom.yaml.
//
// Skip: not Ready; template placeholder URLs that were never overridden.
func shouldRecordBlueFieldSoftware(bfs *provisioningv1.BlueFieldSoftware) bool {
	if bfs.Status.Phase != provisioningv1.BlueFieldSoftwareReady {
		return false
	}
	if bfs.Spec.OsIso == "" || bfs.Spec.OsIso == "REPLACE_ME" {
		return false
	}
	return true
}

// bomDumpDir returns the directory dumps of kindDir belong in, creating it, and
// false when DPF_E2E_COLLECT_BOM is not set so every Record* call is a no-op.
// Per-job subdirectory so GitLab `needs` does not overwrite dumps from other e2e jobs.
func bomDumpDir(kindDir string) (string, bool) {
	if os.Getenv("DPF_E2E_COLLECT_BOM") != collectBOMEnabledValue {
		return "", false
	}
	dir := os.Getenv("ARTIFACTS_DIR")
	Expect(dir).NotTo(BeEmpty(), "ARTIFACTS_DIR must be set when DPF_E2E_COLLECT_BOM=true")
	job := os.Getenv("CI_JOB_NAME")
	if job == "" {
		job = "local"
	}
	outDir := filepath.Join(dir, kindDir, job)
	Expect(os.MkdirAll(outDir, 0o755)).To(Succeed())
	return outDir, true
}

// writeCRDump writes obj as outDir/<namespace>_<name>.yaml.
func writeCRDump(outDir string, obj client.Object, gvk schema.GroupVersionKind) {
	obj.GetObjectKind().SetGroupVersionKind(gvk)
	raw, err := yaml.Marshal(obj)
	Expect(err).NotTo(HaveOccurred())
	Expect(os.WriteFile(filepath.Join(outDir, fmt.Sprintf("%s_%s.yaml", obj.GetNamespace(), obj.GetName())), raw, 0o644)).To(Succeed())
}
