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

// Test scope for leader-election failover.
//
// This test exercises leader-election failover only for controllers that satisfy
// all of:
//   * managed as a Deployment (e.g. not a DaemonSet)
//   * deployed on the host cluster (e.g. not a DPU cluster)
//   * enabled by default in the standard e2e config
//   * default replicas >= 2 (HA is meaningful)
//
// Excluded controllers, with reason:
//   * cmd/operator (dpf-operator):            replicas=1 by default
//   * cmd/servicechainset:                    runs on DPU cluster
//   * cmd/storage/snap-host-controller:       storage DPUService is not enabled in default e2e config
//   * cmd/nodesriovdeviceplugin/controller:   disabled by default in DPFOperatorConfig
//   * cmd/sfc-controller:                     DaemonSet with per-node sharding -- no leader election
//   * cmd/storage/snap-node-driver:           DaemonSet (see above)

import (
	"context"
	"fmt"
	"strings"
	"time"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// defaultLeaseDuration matches controller-runtime's default LeaderElection.LeaseDuration.
	defaultLeaseDuration = 15 * time.Second

	leaseReadTimeout = 30 * time.Second
	// 4 * defaultLeaseDuration: covers both fast handover (via
	// LeaderElectionReleaseOnCancel, seconds) and slow handover
	// (natural lease expiry).
	leaseHandoverTimeout       = 4 * defaultLeaseDuration
	leaseRenewTimeout          = 30 * time.Second
	deploymentReadyTimeout     = 3 * time.Minute
	leaderElectionPollInterval = 1 * time.Second
)

// leaderElectionTarget names one controller's Deployment and its Lease.
type leaderElectionTarget struct {
	// component is the controller's ComponentName (e.g. "provisioning-controller"). It
	// labels the spec and is matched against DPFOperatorConfig.ComponentConfigs() Name()
	// to locate the controller config when scaling replicas.
	component string
	// deploymentName is the Deployment's metadata.name in dpfOperatorSystemNamespace.
	deploymentName string
	// leaseName is the coordination.k8s.io/Lease metadata.name (matches LeaderElectionID
	// passed to ctrl.NewManager in each controller's cmd/*/main.go).
	leaseName string
}

// replicasSetter is implemented by controller component configs (those embedding
// BaseControllerConfig). It lets scaleControllerReplicas set the replica count via
// DPFOperatorConfig.ComponentConfigs() without switching on the concrete controller type.
type replicasSetter interface {
	SetReplicas(*int32)
}

// ValidateLeaderElectionFailover runs the full failover scenario for one
// controller: capture the current leader -> delete that pod (simulates leader
// failure) -> assert a different pod takes over the Lease and renews it at
// least once -> wait for the Deployment to recover.
func ValidateLeaderElectionFailover(ctx context.Context, c client.Client, target leaderElectionTarget) {
	// CI deploys controllers at 1 replica; scale this target to 2 for the failover
	// scenario and revert to 1 afterwards.
	scaleControllerReplicas(ctx, c, target, 2)
	// Ginkgo runs this after the spec (including on failure) and supplies a fresh,
	// managed context, so the revert does not depend on the spec's ctx still being live.
	DeferCleanup(func(ctx context.Context) { scaleControllerReplicas(ctx, c, target, 1) })

	originalLeader := captureCurrentLeader(ctx, c, target)

	// The lease holder identity is "<podName>_<uuid>" (controller-runtime uses the
	// pod hostname). Match it to a live pod.
	pods := &corev1.PodList{}
	Expect(c.List(ctx, pods, client.InNamespace(DPFOperatorSystemNamespace))).To(Succeed())
	var leaderPod *corev1.Pod
	for i := range pods.Items {
		if strings.HasPrefix(originalLeader, pods.Items[i].Name+"_") {
			leaderPod = &pods.Items[i]
			break
		}
	}
	Expect(leaderPod).ToNot(BeNil(), "no pod found for lease holder %q in %s", originalLeader, DPFOperatorSystemNamespace)

	By(fmt.Sprintf("Deleting leader pod %q (lease holder %q) to simulate leader failure", leaderPod.Name, originalLeader))
	Expect(c.Delete(ctx, leaderPod)).To(Succeed())

	verifyLeaseHandover(ctx, c, target, originalLeader)
	verifyDeploymentReady(ctx, c, target)
}

// scaleControllerReplicas sets the target controller's replica count via the
// DPFOperatorConfig and waits for the Deployment to report that many ready replicas.
func scaleControllerReplicas(ctx context.Context, c client.Client, target leaderElectionTarget, replicas int32) {
	By(fmt.Sprintf("Scaling %s to %d replica(s) via DPFOperatorConfig", target.component, replicas))
	Eventually(func(g Gomega) {
		operatorConfig := &operatorv1.DPFOperatorConfig{}
		g.Expect(c.Get(ctx, client.ObjectKey{
			Namespace: DPFOperatorSystemNamespace,
			Name:      ConfigName,
		}, operatorConfig)).To(Succeed())
		configPatch := client.MergeFrom(operatorConfig.DeepCopy())
		applied := false
		for _, componentConfig := range operatorConfig.ComponentConfigs() {
			if componentConfig.Name() != target.component {
				continue
			}
			setter, ok := componentConfig.(replicasSetter)
			g.Expect(ok).To(BeTrue(), "component %q does not expose replicas", target.component)
			setter.SetReplicas(ptr.To(replicas))
			applied = true
			break
		}
		g.Expect(applied).To(BeTrue(), "component %q not found in DPFOperatorConfig", target.component)
		g.Expect(c.Patch(ctx, operatorConfig, configPatch)).To(Succeed())
	}).WithTimeout(leaseReadTimeout).WithPolling(leaderElectionPollInterval).Should(Succeed())

	By(fmt.Sprintf("Waiting for the %s Deployment to report %d ready replicas", target.component, replicas))
	Eventually(func(g Gomega) {
		leaderDeployment := &appsv1.Deployment{}
		g.Expect(c.Get(ctx, client.ObjectKey{
			Namespace: DPFOperatorSystemNamespace,
			Name:      target.deploymentName,
		}, leaderDeployment)).To(Succeed())
		g.Expect(ptr.Deref(leaderDeployment.Spec.Replicas, 0)).To(Equal(replicas))
		g.Expect(leaderDeployment.Status.ReadyReplicas).To(Equal(replicas))
	}).WithTimeout(deploymentReadyTimeout).WithPolling(leaderElectionPollInterval).Should(Succeed())
}

// captureCurrentLeader reads the controller's Lease and returns the current
// holder identity (= the active leader pod's name).
func captureCurrentLeader(ctx context.Context, c client.Client, target leaderElectionTarget) string {
	By("Reading the current Lease and identifying the active leader pod")
	lease := &coordinationv1.Lease{}
	Eventually(func(g Gomega) {
		g.Expect(c.Get(ctx, client.ObjectKey{
			Namespace: DPFOperatorSystemNamespace,
			Name:      target.leaseName,
		}, lease)).To(Succeed())

		g.Expect(lease.Spec.HolderIdentity).ToNot(BeNil())
		g.Expect(*lease.Spec.HolderIdentity).ToNot(BeEmpty())
	}).WithTimeout(leaseReadTimeout).WithPolling(leaderElectionPollInterval).Should(Succeed(),
		"expected a Lease %s/%s with a non-empty holderIdentity",
		DPFOperatorSystemNamespace, target.leaseName)
	return *lease.Spec.HolderIdentity
}

// verifyLeaseHandover waits for a pod other than originalHolder to acquire the
// Lease, then verifies the new leader renews the Lease at least once (proving
// it is alive and healthy, not just holding a stale lease).
func verifyLeaseHandover(ctx context.Context, c client.Client, target leaderElectionTarget, originalHolder string) {
	By("Waiting for a different pod to acquire the Lease")
	lease := &coordinationv1.Lease{}
	Eventually(func(g Gomega) {
		g.Expect(c.Get(ctx, client.ObjectKey{
			Namespace: DPFOperatorSystemNamespace,
			Name:      target.leaseName,
		}, lease)).To(Succeed())

		g.Expect(lease.Spec.HolderIdentity).ToNot(BeNil())
		g.Expect(*lease.Spec.HolderIdentity).ToNot(BeEmpty())
		g.Expect(*lease.Spec.HolderIdentity).ToNot(Equal(originalHolder),
			"lease is still held by the deleted pod")
	}).WithTimeout(leaseHandoverTimeout).WithPolling(leaderElectionPollInterval).Should(Succeed(),
		"expected a new pod to acquire the Lease %s/%s after the original leader was deleted",
		DPFOperatorSystemNamespace, target.leaseName)

	By("Verifying the new leader has renewed the Lease at least once")
	Expect(lease.Spec.RenewTime).ToNot(BeNil())
	baselineRenewTime := lease.Spec.RenewTime.Time
	Eventually(func(g Gomega) {
		g.Expect(c.Get(ctx, client.ObjectKey{
			Namespace: DPFOperatorSystemNamespace,
			Name:      target.leaseName,
		}, lease)).To(Succeed())

		g.Expect(lease.Spec.RenewTime).ToNot(BeNil())
		g.Expect(lease.Spec.RenewTime.Time.After(baselineRenewTime)).To(BeTrue(),
			"renewTime did not advance: %s (latest) vs %s (baseline)",
			lease.Spec.RenewTime.Time, baselineRenewTime)
	}).WithTimeout(leaseRenewTimeout).WithPolling(leaderElectionPollInterval).Should(Succeed())
}

// verifyDeploymentReady waits for the Deployment to become fully ready again
// (Status.ReadyReplicas == Spec.Replicas) so the cluster is left in a healthy
// state for downstream tests.
func verifyDeploymentReady(ctx context.Context, c client.Client, target leaderElectionTarget) {
	By(fmt.Sprintf("Verifying the %s Deployment is fully ready again (all replicas ready)", target.component))
	leaderDeployment := &appsv1.Deployment{}
	Eventually(func(g Gomega) {
		g.Expect(c.Get(ctx, client.ObjectKey{
			Namespace: DPFOperatorSystemNamespace,
			Name:      target.deploymentName,
		}, leaderDeployment)).To(Succeed())

		g.Expect(leaderDeployment.Status.ReadyReplicas).To(
			Equal(ptr.Deref(leaderDeployment.Spec.Replicas, 0)),
			"Deployment %s/%s did not return to fully ready (got %d/%d ready)",
			DPFOperatorSystemNamespace,
			target.deploymentName,
			leaderDeployment.Status.ReadyReplicas,
			ptr.Deref(leaderDeployment.Spec.Replicas, 0),
		)
	}).WithTimeout(deploymentReadyTimeout).WithPolling(leaderElectionPollInterval).Should(Succeed())
}
