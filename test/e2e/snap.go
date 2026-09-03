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
	"strings"
	"time"

	"github.com/nvidia/doca-platform/test/e2e/cleanup"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// Each StatefulSet replica gets its own PVC and exercises an independent volume attachment.
	snapWorkloadReplicas = 4

	// Consumer identifiers. StatefulSet pods are named <sts>-<ordinal>.
	snapVirtioFSStatefulSetName = "storage-test-pod-virtiofs-hotplug-pf"
	snapVirtioFSNamespace       = "snap-virtiofs-test"
	snapVirtioFSMountPath       = "/mnt/vol1"
	snapVirtioFSHeartbeatFile   = snapVirtioFSMountPath + "/heartbeat"

	snapNVMeStatefulSetName = "storage-test-pod-nvme-hotplug-pf"
	snapNVMeNamespace       = "snap-nvme-test"
	snapNVMeDevicePath      = "/dev/xvda"

	// Storage control plane objects, as named in the manifests the suite deploys.
	snapVirtioFSStorageVendorName = "nfs-csi"
	snapVirtioFSStoragePolicyName = "policy-fs"
	snapNVMeStorageVendorName     = "local-block"
	snapNVMeStoragePolicyName     = "policy-block"

	// csi-hostpath backend identifiers, as declared in the manifests it is deployed from.
	csiHostpathNamespace      = "default"
	snapAttacherContainerName = "nvidia-external-attacher"

	// Time budgets.
	snapProvisioningTimeout = 20 * time.Minute
)

// Cleanup scopes are split along what a run can reuse:
//
//   - snapDeploymentScope holds the DPUDeployment and the objects it references. Keeping it
//     (-e2e.skip-cleanup.named-scopes=snap-deployment) keeps the DPU provisioned across runs.
//   - snapControlPlaneScope holds the vendors and policies shared by both consumer tests.
//   - each consumer scope owns one namespace, StorageClass, and StatefulSet.
var (
	snapDeploymentScope       *cleanup.Scope
	snapControlPlaneScope     *cleanup.Scope
	snapVirtioFSWorkloadScope *cleanup.Scope
	snapNVMeWorkloadScope     *cleanup.Scope
)

// deploySNAPStorageStack applies the SNAP storage stack: the host-cluster objects and the DPUDeployment
// on the host cluster, and the csi-hostpath backend on the DPU (tenant) cluster. The DPUDeployment
// creates its own DPUSet and thus drives DPU provisioning, so SNAP skips the standalone ProvisionDPUSet
// (see e2e_suite_test.go); the stack is applied here before the provisioning wait in the suite BeforeAll.
func deploySNAPStorageStack(ctx context.Context, input *systemTestInput, conf config) {
	By("Deploying the SNAP host-cluster storage objects")
	applyObjectsFromManifests(ctx, input.client, conf.SNAPHostVerbatimPaths, withCleanupLabels(snapDeploymentScope.CleanupLabels))
	applyObjectsFromManifests(ctx, input.client, conf.SNAPHostTemplatePaths, withCleanupLabels(snapDeploymentScope.CleanupLabels), patchChartSource)
	applyObjectsFromManifests(ctx, input.client, conf.SNAPHostServicePaths, withCleanupLabels(snapDeploymentScope.CleanupLabels), patchChartSource, func(obj *unstructured.Unstructured) {
		updateImagePullSecret(obj, dpfPullSecretName)
	})
	applyObjectsFromManifests(ctx, input.client, conf.SNAPHostConfigPaths, withCleanupLabels(snapDeploymentScope.CleanupLabels), patchSNAPServiceConfiguration)
	// The control plane is re-created for each suite run even when the provisioned deployment is kept.
	applyObjectsFromManifests(ctx, input.client, conf.SNAPStorageControlPlanePaths, withCleanupLabels(snapControlPlaneScope.CleanupLabels))

	// Applied after the templates/configurations it references. Its DPUSet targets only the DPU pinned
	// below, so the SNAP flavor's firmware settings reach that DPU alone.
	By("Deploying the SNAP DPUDeployment")
	pinLabels := pinSNAPDPUDevice(ctx, input)
	applyObjectsFromManifests(ctx, input.client, []string{conf.DPUDeploymentPath}, withCleanupLabels(snapDeploymentScope.CleanupLabels), withDPUSetPinnedToDPU(pinLabels))

	By("Deploying the csi-hostpath backend on the DPU cluster")
	Expect(dpuClusterClient).ToNot(BeEmpty(), "no DPU cluster client available for the csi-hostpath backend")
	Expect(storageSystemImage).ToNot(BeEmpty(), "STORAGE_SYSTEM_IMAGE must be set for SNAP runs")
	// The plugin pod pulls the storage-system image (its nvidia-external-attacher sidecar) from the e2e
	// registry, so it needs the pull secret in its own namespace. The other images are public.
	copyPullSecretToDPUCluster(ctx, input, dpuClusterClient[0], csiHostpathNamespace)
	// No cleanup labels: the named-scope cleanup runs on the host cluster only; the DPU (tenant)
	// cluster is torn down with its DPUCluster.
	applyObjectsFromManifests(ctx, dpuClusterClient[0], conf.SNAPDPUClusterObjectPaths,
		withContainerImage(snapAttacherContainerName, fmt.Sprintf("%s:%s", storageSystemImage, tag)))
}

// copyPullSecretToDPUCluster creates a copy of the host cluster's dpf-pull-secret in the given namespace
// of the DPU cluster, so pods there can pull from the e2e registry.
//
// The copy takes the credentials but not the source's labels. DPF syncs labeled pull secrets into the DPU
// clusters and keeps reconciling them, and this copy is the suite's own to manage.
func copyPullSecretToDPUCluster(ctx context.Context, input *systemTestInput, dpuClient client.Client, namespace string) {
	By(fmt.Sprintf("Copying %s into namespace %s of the DPU cluster", dpfPullSecretName, namespace))
	source := &corev1.Secret{}
	Expect(input.client.Get(ctx, client.ObjectKey{Namespace: dpfOperatorSystemNamespace, Name: dpfPullSecretName}, source)).To(Succeed())

	copied := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: dpfPullSecretName, Namespace: namespace},
		Type:       source.Type,
		Data:       source.Data,
	}
	Expect(client.IgnoreAlreadyExists(dpuClient.Create(ctx, copied))).To(Succeed())
}

// snapDPUHostNode returns the worker node the suite runs SNAP on, the first one by name. Both the DPU pin
// and the workload derive their node from here, so they cannot end up on different hosts.
func snapDPUHostNode(ctx context.Context, input *systemTestInput) string {
	nodes := listWorkerNodes(ctx, input.client)
	Expect(nodes).ToNot(BeEmpty(), "no worker node to provision a DPU on")

	return nodes[0].Name
}

// pinSNAPDPUDevice pins the DPU that the SNAP DPUDeployment will provision: on the suite's DPU host node,
// the DPUDevice with the lexicographically lowest PCI address. It returns the labels that select that
// device in the DPUDeployment's DPUSet.
func pinSNAPDPUDevice(ctx context.Context, input *systemTestInput) map[string]string {
	// One dpuSet pinned to one device provisions exactly one DPU, so the suite must expect one.
	Expect(input.numberOfDPUNodes).To(Equal(1), "the SNAP DPUDeployment provisions a single DPU")
	Expect(input.numberOfDPUsPerNode).To(Equal(1), "the SNAP DPUDeployment provisions a single DPU")

	return pinDPUDeviceOnNode(ctx, input.client, dpfOperatorSystemNamespace, snapDPUHostNode(ctx, input))
}

// applySNAPWorkload creates one consumer namespace, StorageClass, and StatefulSet on the host cluster.
func applySNAPWorkload(ctx context.Context, input *systemTestInput, scope *cleanup.Scope, namespace, storageClassPath, workloadPath string) {
	By(fmt.Sprintf("Creating the SNAP workload namespace %s", namespace))
	// Not createTestNamespace: that tags with the It/Suite scope, whose per-It cleanup would delete the
	// namespace between the ordered checks for this consumer.
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace, Labels: scope.CleanupLabels}}
	Expect(client.IgnoreAlreadyExists(input.client.Create(ctx, ns))).To(Succeed())

	By("Applying the SNAP workload StorageClass and StatefulSet")
	applyObjectsFromManifests(ctx, input.client, []string{storageClassPath}, withCleanupLabels(scope.CleanupLabels))
	// The workload must land on the host whose DPU was provisioned because the CSI node services run there.
	applyObjectsFromManifests(ctx, input.client, []string{workloadPath}, withCleanupLabels(scope.CleanupLabels), withNamespace(namespace),
		withPodNodeSelector(map[string]string{corev1.LabelHostname: snapDPUHostNode(ctx, input)}))
}

func snapWorkloadPodNames(statefulSetName string) []string {
	podNames := make([]string, snapWorkloadReplicas)
	for ordinal := range snapWorkloadReplicas {
		podNames[ordinal] = fmt.Sprintf("%s-%d", statefulSetName, ordinal)
	}
	return podNames
}

// patchChartSource patches a DPUServiceTemplate or DPUService: the empty Helm chart source coordinates
// (repoURL/version) set to the e2e registry.
func patchChartSource(obj *unstructured.Unstructured) {
	setNestedField(obj, helmRegistry, "spec", "helmChart", "source", "repoURL")
	setNestedField(obj, tag, "spec", "helmChart", "source", "version")
}

// patchSNAPServiceConfiguration patches a DPUServiceConfiguration: the main DOCA SNAP image, the fake
// vendor-plugin images, and the DPU-side image-pull secrets.
func patchSNAPServiceConfiguration(obj *unstructured.Unstructured) {
	const (
		docaSnapServiceName              = "doca-snap"
		fsStorageDPUPluginServiceName    = "fs-storage-dpu-plugin"
		blockStorageDPUPluginServiceName = "block-storage-dpu-plugin"
	)
	if obj.GetName() == docaSnapServiceName && snapImageURL != "" {
		tagSeparator := strings.LastIndex(snapImageURL, ":")
		lastPathSeparator := strings.LastIndex(snapImageURL, "/")
		Expect(tagSeparator > lastPathSeparator+1 && tagSeparator < len(snapImageURL)-1).To(BeTrue(), "SNAP_IMAGE_URL must use repository:tag format")
		setNestedField(obj, snapImageURL[:tagSeparator], "spec", "serviceConfiguration", "helmChart", "values", "dpu", "docaSnap", "image", "repository")
		setNestedField(obj, snapImageURL[tagSeparator+1:], "spec", "serviceConfiguration", "helmChart", "values", "dpu", "docaSnap", "image", "tag")
	}
	switch obj.GetName() {
	case fsStorageDPUPluginServiceName:
		Expect(fakeFSStorageVendorImage).ToNot(BeEmpty(), "FAKE_FS_STORAGE_IMAGE must be set for SNAP runs")
		setNestedField(obj, fakeFSStorageVendorImage, "spec", "serviceConfiguration", "helmChart", "values", "dpu", "fsStorageVendorDpuPlugin", "image", "repository")
		setNestedField(obj, tag, "spec", "serviceConfiguration", "helmChart", "values", "dpu", "fsStorageVendorDpuPlugin", "image", "tag")
	case blockStorageDPUPluginServiceName:
		Expect(fakeBlockStorageVendorImage).ToNot(BeEmpty(), "FAKE_BLOCK_STORAGE_IMAGE must be set for SNAP runs")
		setNestedField(obj, fakeBlockStorageVendorImage, "spec", "serviceConfiguration", "helmChart", "values", "dpu", "blockStorageVendorDpuPlugin", "image", "repository")
		setNestedField(obj, tag, "spec", "serviceConfiguration", "helmChart", "values", "dpu", "blockStorageVendorDpuPlugin", "image", "tag")
	}

	appendServiceConfigurationImagePullSecret(obj, dpfPullSecretName)
	// The DOCA SNAP image is pulled from NGC; add its secret only when an NGC key is configured.
	if obj.GetName() == docaSnapServiceName && ngcAPIKey != "" {
		appendServiceConfigurationImagePullSecret(obj, ngcPullSecretName)
	}
}

// appendServiceConfigurationImagePullSecret appends secretName to a DPUServiceConfiguration's
// spec.serviceConfiguration.helmChart.values.imagePullSecrets (mirrors updateImagePullSecret's shape).
func appendServiceConfigurationImagePullSecret(obj *unstructured.Unstructured, secretName string) {
	path := []string{"spec", "serviceConfiguration", "helmChart", "values", "imagePullSecrets"}
	pullSecrets, found, err := unstructured.NestedSlice(obj.Object, path...)
	Expect(err).ToNot(HaveOccurred())
	if !found {
		pullSecrets = make([]interface{}, 0)
	}
	pullSecrets = append(pullSecrets, map[string]interface{}{"name": secretName})
	Expect(unstructured.SetNestedSlice(obj.Object, pullSecrets, path...)).To(Succeed())
}
