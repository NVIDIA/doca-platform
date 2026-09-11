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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// Matches internal/servicechainset/controllers ownership labels on ServiceInterface children.
	serviceInterfaceSetNameLabel      = dpuservicev1.SvcDpuGroupName + "/serviceinterfaceset-name"
	serviceInterfaceSetNamespaceLabel = dpuservicev1.SvcDpuGroupName + "/serviceinterfaceset-namespace"
)

// upgradeArtifactsFile returns the on-disk path for the snapshot identified
// by key. Files live one level above artifactsDir so all phases in a run
// share the same parent and later phases can read earlier ones.
func upgradeArtifactsFile(key string) string {
	return filepath.Join(artifactsDir, "..", "upgrade-artifacts-"+key+".json")
}

// artifactWait settles the live cluster before a snapshot is captured —
// typically blocking until the hop has converged, so the file records its end
// state rather than a moment mid-flight.
type artifactWait func(ctx context.Context)

// artifactCheck checks a property of an upgrade hop that identity comparison
// cannot express, for example inventory continuity across a kind the hop
// migrates. It sees the raw snapshots, before any normalize rewrites them.
type artifactCheck func(before, after []map[string]interface{})

// artifactNormalize rewrites both snapshots so identity comparison can run
type artifactNormalize func(before, after *[]map[string]interface{})

// artifactCapture is how a phase takes a snapshot. Waits run even when this
// capture has no comparison: the file on disk still has to be the end state.
type artifactCapture struct {
	waits []artifactWait
}

// artifactCompare is how a phase compares two snapshots. Empty checks and
// normalizes means field-for-field identity on every captured kind.
type artifactCompare struct {
	checks     []artifactCheck
	normalizes []artifactNormalize
}

// bumpMatchingBeforeGeneration increments generation on the before artifact
// with the same apiVersion/kind/name/namespace as after. Call it from a
// normalize after rewinding a spec field the hop wrote once.
func bumpMatchingBeforeGeneration(before []map[string]interface{}, after map[string]interface{}) {
	apiVersion := fmt.Sprintf("%v", after["apiVersion"])
	kind := fmt.Sprintf("%v", after["kind"])
	name := fmt.Sprintf("%v", after["name"])
	namespace := fmt.Sprintf("%v", after["namespace"])
	for i, b := range before {
		if fmt.Sprintf("%v", b["apiVersion"]) != apiVersion ||
			fmt.Sprintf("%v", b["kind"]) != kind ||
			fmt.Sprintf("%v", b["name"]) != name ||
			fmt.Sprintf("%v", b["namespace"]) != namespace {
			continue
		}
		gen, ok := before[i]["generation"].(float64)
		Expect(ok).To(BeTrue(),
			"before artifact %s %s/%s generation must be a JSON number", kind, namespace, name)
		before[i]["generation"] = gen + 1
		return
	}
	Fail(fmt.Sprintf("no before artifact matching %s %s %s/%s", apiVersion, kind, namespace, name))
}

// collectArtifacts writes a snapshot of all tracked objects (DPUs,
// DPUDeployment-owned DPUServices, DPUServiceChains, DPUSets,
// DPUServiceInterfaces, plus DPU-cluster-side ServiceChains, ServiceInterfaces,
// and service Pods) to filePath as JSON.
func collectArtifacts(filePath string) {
	By("Collecting artifacts to: " + filePath)
	Expect(os.MkdirAll(filepath.Dir(filePath), 0755)).To(Succeed())

	allArtifacts := make([]map[string]interface{}, 0)

	By("Capturing DPU artifacts")
	dpuList := &provisioningv1.DPUList{}
	Expect(input.client.List(ctx, dpuList, client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())
	allArtifacts = append(allArtifacts, extractArtifacts(ToClientObjectSlice(dpuList.Items))...)

	By("Capturing DPUService artifacts with owned-by-dpudeployment label")
	dpuServiceList := &dpuservicev1.DPUServiceList{}
	Expect(input.client.List(ctx, dpuServiceList,
		client.InNamespace(dpfOperatorSystemNamespace),
		client.HasLabels{dpuservicev1.ParentDPUDeploymentNameLabel})).To(Succeed())
	allArtifacts = append(allArtifacts, extractArtifacts(ToClientObjectSlice(dpuServiceList.Items))...)

	By("Capturing DPUServiceChain artifacts")
	dpuServiceChainList := &dpuservicev1.DPUServiceChainList{}
	Expect(input.client.List(ctx, dpuServiceChainList, client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())
	allArtifacts = append(allArtifacts, extractArtifacts(ToClientObjectSlice(dpuServiceChainList.Items))...)

	By("Capturing DPUSet artifacts")
	dpuSetList := &provisioningv1.DPUSetList{}
	Expect(input.client.List(ctx, dpuSetList, client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())
	allArtifacts = append(allArtifacts, extractArtifacts(ToClientObjectSlice(dpuSetList.Items))...)

	By("Capturing DPUServiceInterface artifacts")
	dpuServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
	Expect(input.client.List(ctx, dpuServiceInterfaceList, client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())
	allArtifacts = append(allArtifacts, extractArtifacts(ToClientObjectSlice(dpuServiceInterfaceList.Items))...)

	By("Capturing ServiceChain artifacts from DPU cluster")
	serviceChainList := &dpuservicev1.ServiceChainList{}
	Expect(dpuClusterClient[0].List(ctx, serviceChainList)).To(Succeed())
	allArtifacts = append(allArtifacts, extractArtifacts(ToClientObjectSlice(serviceChainList.Items))...)

	By("Capturing ServiceInterface artifacts from DPU cluster")
	serviceInterfaceList := &dpuservicev1.ServiceInterfaceList{}
	Expect(dpuClusterClient[0].List(ctx, serviceInterfaceList)).To(Succeed())
	allArtifacts = append(allArtifacts, extractArtifacts(ToClientObjectSlice(serviceInterfaceList.Items))...)

	By("Capturing NodeServiceInterfaces artifacts from DPU cluster")
	// The v25.10 and v26.4 upgrade hops predate this CRD.
	nodeServiceInterfaceList := &dpuservicev1.NodeServiceInterfacesList{}
	switch err := dpuClusterClient[0].List(ctx, nodeServiceInterfaceList); {
	case err == nil:
		allArtifacts = append(allArtifacts, extractArtifacts(ToClientObjectSlice(nodeServiceInterfaceList.Items))...)
	case meta.IsNoMatchError(err) || apierrors.IsNotFound(err):
		By("Skipping NodeServiceInterfaces: kind not served by this release")
	default:
		Expect(err).ToNot(HaveOccurred())
	}

	By("Capturing Pod artifacts from DPU cluster with service label but not system component label")
	podList := &corev1.PodList{}
	hasServiceLabelReq, reqErr := labels.NewRequirement(dpuservicev1.DPFServiceIDLabelKey, selection.Exists, nil)
	Expect(reqErr).ToNot(HaveOccurred())
	notSystemComponentReq, reqErr := labels.NewRequirement(operatorv1.DPFComponentLabelKey, selection.DoesNotExist, nil)
	Expect(reqErr).ToNot(HaveOccurred())
	podSelector := labels.NewSelector().Add(*hasServiceLabelReq, *notSystemComponentReq)
	Expect(dpuClusterClient[0].List(ctx, podList, &client.MatchingLabelsSelector{Selector: podSelector})).To(Succeed())
	allArtifacts = append(allArtifacts, extractArtifacts(ToClientObjectSlice(podList.Items))...)

	artifactData, err := json.MarshalIndent(allArtifacts, "", "  ")
	Expect(err).ToNot(HaveOccurred())

	By("Writing artifacts to: " + filePath)
	Expect(os.WriteFile(filePath, artifactData, 0644)).To(Succeed())
}

// getArtifacts reads a snapshot previously written by collectArtifacts.
func getArtifacts(filePath string) []map[string]interface{} {
	By("Reading artifacts from: " + filePath)
	data, err := os.ReadFile(filePath)
	Expect(err).ToNot(HaveOccurred())

	var artifacts []map[string]interface{}
	Expect(json.Unmarshal(data, &artifacts)).To(Succeed())
	return artifacts
}

// compareArtifactSnapshots asserts the two prepared snapshots match (modulo
// sorting). The phaseDescription is used in assertion messages.
func compareArtifactSnapshots(prev, curr []map[string]interface{}, phaseDescription string) {
	Expect(curr).To(HaveLen(len(prev)),
		"Number of tracked objects should be unchanged after %s upgrade", phaseDescription)
	sort.Slice(prev, func(i, j int) bool { return fmt.Sprintf("%v", prev[i]) < fmt.Sprintf("%v", prev[j]) })
	sort.Slice(curr, func(i, j int) bool { return fmt.Sprintf("%v", curr[i]) < fmt.Sprintf("%v", curr[j]) })
	Expect(curr).To(BeComparableTo(prev),
		"Object artifacts should be identical — no reprovisioning expected during %s upgrade", phaseDescription)
}

// filterUpgradeInterfaceArtifacts drops ServiceInterface and NodeServiceInterfaces
// from both snapshots. SFC SI→NSI migration recreates interface inventory under
// a different GVK during upgrade, so those kinds are not stable across the cutover.
// Register it as a normalize on the cutover hop only, paired with
// assertSFCInterfaceMigration so the inventory it hides stays covered.
//
// TODO(v26.8+): delete this filter (and assertSFCInterfaceMigration / SI labels in
// extractArtifacts) once the previous-GA upgrade hop is NSI-native — NSI objects can
// return to the identity comparison and the SI→NSI cutover assert is obsolete.
func filterUpgradeInterfaceArtifacts(before, after *[]map[string]interface{}) {
	drop := func(artifacts *[]map[string]interface{}) {
		*artifacts = slices.DeleteFunc(*artifacts, func(a map[string]interface{}) bool {
			kind, _ := a["kind"].(string)
			return kind == dpuservicev1.ServiceInterfaceKind || kind == dpuservicev1.NodeServiceInterfacesKind
		})
	}
	drop(before)
	drop(after)
}

// assertSFCInterfaceMigration verifies SFC SI→NSI cutover across upgrade:
// VPC ServiceInterfaces (virtualNetwork set) are ignored — they stay sticky-legacy.
func assertSFCInterfaceMigration(before, after []map[string]interface{}) {
	By("Asserting SFC ServiceInterface → NodeServiceInterfaces entry migration")
	beforeSI := sfcServiceInterfaceMigrationKeys(before)
	afterNSI := sfcNSIEntryMigrationKeys(after)
	afterSI := sfcServiceInterfaceMigrationKeys(after)

	Expect(afterSI).To(BeEmpty(),
		"no SFC ServiceInterface should remain after upgrade (VPC sticky-legacy excluded)")
	if len(beforeSI) == 0 {
		return
	}
	Expect(afterNSI).To(ContainElements(beforeSI),
		"every pre-upgrade SFC ServiceInterface must become an NSI entry (set/ns/node)")
}

func waitForSFCInterfaceMigration(ctx context.Context) {
	By("Waiting for the SFC ServiceInterface → NodeServiceInterfaces cutover to converge")
	Eventually(func(g Gomega) {
		serviceInterfaceList := &dpuservicev1.ServiceInterfaceList{}
		g.Expect(dpuClusterClient[0].List(ctx, serviceInterfaceList)).To(Succeed())
		g.Expect(sfcServiceInterfaceMigrationKeys(extractArtifacts(ToClientObjectSlice(serviceInterfaceList.Items)))).
			To(BeEmpty(), "SFC ServiceInterfaces still awaiting NSI cutover")
	}).WithTimeout(10 * time.Minute).WithPolling(time.Second).Should(Succeed())
}

// sfcServiceInterfaceMigrationKeys returns "setNS/setName/node" keys for SFC
// ServiceInterfaces (no virtualNetwork) owned by a ServiceInterfaceSet.
func sfcServiceInterfaceMigrationKeys(artifacts []map[string]interface{}) []string {
	keys := make([]string, 0)
	for _, a := range artifacts {
		kind, _ := a["kind"].(string)
		if kind != dpuservicev1.ServiceInterfaceKind {
			continue
		}
		spec, _ := a["spec"].(map[string]interface{})
		if artifactHasVirtualNetwork(spec) {
			continue
		}
		setNS := artifactLabel(a, serviceInterfaceSetNamespaceLabel)
		setName := artifactLabel(a, serviceInterfaceSetNameLabel)
		node, _ := spec["node"].(string)
		if setNS == "" || setName == "" || node == "" {
			continue
		}
		keys = append(keys, setNS+"/"+setName+"/"+node)
	}
	return keys
}

// sfcNSIEntryMigrationKeys returns "setNS/setName/node" keys for non-terminating
// entries on SFC NodeServiceInterfaces objects.
func sfcNSIEntryMigrationKeys(artifacts []map[string]interface{}) []string {
	keys := make([]string, 0)
	for _, a := range artifacts {
		kind, _ := a["kind"].(string)
		if kind != dpuservicev1.NodeServiceInterfacesKind {
			continue
		}
		spec, _ := a["spec"].(map[string]interface{})
		if t, _ := spec["type"].(string); t != dpuservicev1.NSITypeSFC {
			continue
		}
		node, _ := spec["node"].(string)
		if node == "" {
			continue
		}
		entries, _ := spec["interfaces"].([]interface{})
		for _, raw := range entries {
			entry, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			if term, _ := entry["terminating"].(bool); term {
				continue
			}
			name, _ := entry["name"].(string)
			setNS, setName := splitInterfaceEntryName(name)
			if setNS == "" || setName == "" {
				continue
			}
			keys = append(keys, setNS+"/"+setName+"/"+node)
		}
	}
	return keys
}

func splitInterfaceEntryName(name string) (namespace, setName string) {
	parts := strings.SplitN(name, "_", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", ""
	}
	return parts[0], parts[1]
}

func artifactHasVirtualNetwork(spec map[string]interface{}) bool {
	if spec == nil {
		return false
	}
	for _, key := range []string{"pf", "vf", "service"} {
		nested, _ := spec[key].(map[string]interface{})
		if vn, _ := nested["virtualNetwork"].(string); vn != "" {
			return true
		}
	}
	return false
}

func artifactLabel(a map[string]interface{}, key string) string {
	labels, _ := a["labels"].(map[string]interface{})
	if labels == nil {
		return ""
	}
	v, _ := labels[key].(string)
	return v
}

// ToClientObjectSlice converts a slice of concrete Kubernetes objects to []client.Object.
// T is the value type (e.g., DPU), but *T must implement client.Object.
func ToClientObjectSlice[T any](in []T) []client.Object {
	out := make([]client.Object, len(in))
	for i := range in {
		out[i] = any(&in[i]).(client.Object)
	}
	return out
}

// extractArtifacts extracts the GVK, name, namespace, UID, generation, and
// spec of each object — the stable subset we care about for upgrade
// comparison. All other fields (status, volatile metadata) are excluded.
// ServiceInterface artifacts also include labels so SI→NSI migration can match
// ownership (set name/namespace); those kinds are filtered from identity compare.
// GVK is resolved via the scheme because List calls do not populate TypeMeta
// on individual items.
func extractArtifacts(objects []client.Object) []map[string]interface{} {
	artifacts := make([]map[string]interface{}, 0, len(objects))
	for _, obj := range objects {
		data, err := json.Marshal(obj)
		Expect(err).ToNot(HaveOccurred())
		var m map[string]interface{}
		Expect(json.Unmarshal(data, &m)).To(Succeed())
		// Resolve GVK from the scheme — TypeMeta is not set on items from List calls.
		gvks, _, err := scheme.Scheme.ObjectKinds(obj)
		Expect(err).ToNot(HaveOccurred())
		Expect(gvks).ToNot(BeEmpty())
		artifact := map[string]interface{}{
			"apiVersion": gvks[0].GroupVersion().String(),
			"kind":       gvks[0].Kind,
			"name":       obj.GetName(),
			"namespace":  obj.GetNamespace(),
			"uid":        string(obj.GetUID()),
			"generation": obj.GetGeneration(),
			"spec":       m["spec"],
		}
		if gvks[0].Kind == dpuservicev1.ServiceInterfaceKind {
			labels := map[string]interface{}{}
			for k, v := range obj.GetLabels() {
				labels[k] = v
			}
			artifact["labels"] = labels
		}
		artifacts = append(artifacts, artifact)
	}
	return artifacts
}
