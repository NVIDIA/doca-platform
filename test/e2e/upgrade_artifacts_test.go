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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// upgradeArtifactsFile returns the on-disk path for the snapshot identified
// by key. Files live one level above artifactsDir so all phases in a run
// share the same parent and later phases can read earlier ones.
func upgradeArtifactsFile(key string) string {
	return filepath.Join(artifactsDir, "..", "upgrade-artifacts-"+key+".json")
}

// upgradeExpectedChange describes a known spec change introduced by an upgrade.
// Objects that are recreated can't be handled by this struct and need to be
// handled in a different way. transform is applied only to the after artifact
// of the matching GVK before comparison, resetting the changed field(s) back
// to their pre-upgrade value so the assertion does not fail on expected
// changes. The matching before artifact's generation is bumped by one to
// account for the single spec change the upgrade introduced.
//
// Example — a field that flips from false to true after upgrade:
//
//	{gvk: dpuservicev1.GroupVersion.WithKind("DPUService"), transform: func(a map[string]interface{}) {
//	    spec, _ := a["spec"].(map[string]interface{})
//	    spec["something"] = false
//	}}
type upgradeExpectedChange struct {
	gvk       schema.GroupVersionKind
	transform func(artifact map[string]interface{})
}

// applyUpgradeExpectedChanges mutates `after` to reset the fields touched by
// each registered transform, and bumps the matching `before` artifact's
// generation by one (since the upgrade necessarily bumped it once).
func applyUpgradeExpectedChanges(before, after []map[string]interface{}, expectedChanges []upgradeExpectedChange) {
	type artifactKey struct{ apiVersion, kind, name, namespace string }
	beforeIdx := make(map[artifactKey]int, len(before))
	for i, b := range before {
		k := artifactKey{
			apiVersion: fmt.Sprintf("%v", b["apiVersion"]),
			kind:       fmt.Sprintf("%v", b["kind"]),
			name:       fmt.Sprintf("%v", b["name"]),
			namespace:  fmt.Sprintf("%v", b["namespace"]),
		}
		beforeIdx[k] = i
	}

	for i, artifact := range after {
		apiVersion, _ := artifact["apiVersion"].(string)
		kind, _ := artifact["kind"].(string)
		gv, err := schema.ParseGroupVersion(apiVersion)
		Expect(err).ToNot(HaveOccurred())
		artifactGVK := gv.WithKind(kind)
		for _, change := range expectedChanges {
			if change.gvk != artifactGVK {
				continue
			}
			change.transform(after[i])
			// The spec change introduced by the upgrade bumped the generation once.
			// Increment the matching before artifact's generation so the comparison holds.
			k := artifactKey{
				apiVersion: apiVersion,
				kind:       kind,
				name:       fmt.Sprintf("%v", artifact["name"]),
				namespace:  fmt.Sprintf("%v", artifact["namespace"]),
			}
			if idx, ok := beforeIdx[k]; ok {
				if gen, ok := before[idx]["generation"].(float64); ok {
					before[idx]["generation"] = gen + 1
				}
			}
		}
	}
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

// compareArtifactSnapshots loads the two named snapshots, applies the given
// expected-change transforms, and asserts they match (modulo sorting). The
// phaseDescription is used in assertion messages.
func compareArtifactSnapshots(prevKey, currKey, phaseDescription string, expectedChanges []upgradeExpectedChange) {
	prev := getArtifacts(upgradeArtifactsFile(prevKey))
	curr := getArtifacts(upgradeArtifactsFile(currKey))
	applyUpgradeExpectedChanges(prev, curr, expectedChanges)
	By(fmt.Sprintf("Comparing artifacts: %s vs %s", prevKey, currKey))
	Expect(curr).To(HaveLen(len(prev)),
		"Number of tracked objects should be unchanged after %s upgrade", phaseDescription)
	sort.Slice(prev, func(i, j int) bool { return fmt.Sprintf("%v", prev[i]) < fmt.Sprintf("%v", prev[j]) })
	sort.Slice(curr, func(i, j int) bool { return fmt.Sprintf("%v", curr[i]) < fmt.Sprintf("%v", curr[j]) })
	Expect(curr).To(BeComparableTo(prev),
		"Object artifacts should be identical — no reprovisioning expected during %s upgrade", phaseDescription)
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
		artifacts = append(artifacts, artifact)
	}
	return artifacts
}
