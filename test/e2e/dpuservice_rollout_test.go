// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/test/utils/netshoot"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const rolloutTestLabel = "e2e.dpu.nvidia.com/rollout"

var _ = Describe("DPUService rollout strategies", Serial, Labels{Domain.DPFSystem}, func() {
	// Before adding the OCP label, skip the managed Multus check: OpenShift provides its own Multus.
	It("propagates the default and configured rollout limit to Multus and SFC", func() {
		config := &operatorv1.DPFOperatorConfig{}
		key := client.ObjectKey{
			Namespace: dpfOperatorSystemNamespace,
			Name:      configName,
		}
		Expect(input.client.Get(ctx, key, config)).To(Succeed())
		var originalLimit int32
		if config.Spec.DPUServiceController != nil {
			originalLimit = config.Spec.DPUServiceController.DefaultServiceDaemonSetMaxUnavailable
		}
		originalEffectiveLimit := config.Spec.DPUServiceController.ServiceDaemonSetMaxUnavailableValue()
		DeferCleanup(func(cleanupCtx context.Context) {
			Eventually(func(g Gomega) {
				g.Expect(input.client.Get(cleanupCtx, key, config)).To(Succeed())
				before := config.DeepCopy()
				if config.Spec.DPUServiceController != nil {
					config.Spec.DPUServiceController.DefaultServiceDaemonSetMaxUnavailable = originalLimit
				} else if originalLimit != 0 {
					config.Spec.DPUServiceController = &operatorv1.DPUServiceControllerConfiguration{
						DefaultServiceDaemonSetMaxUnavailable: originalLimit,
					}
				}
				g.Expect(input.client.Patch(cleanupCtx, config, client.MergeFrom(before))).To(Succeed())
			}).WithTimeout(time.Minute).Should(Succeed())
			waitForManagedRolloutLimit(cleanupCtx, originalEffectiveLimit)
		})

		By("Checking the default rollout limit when the configured value is zero")
		Eventually(func(g Gomega) {
			g.Expect(input.client.Get(ctx, key, config)).To(Succeed())
			before := config.DeepCopy()
			if config.Spec.DPUServiceController != nil {
				config.Spec.DPUServiceController.DefaultServiceDaemonSetMaxUnavailable = 0
			}
			g.Expect(input.client.Patch(ctx, config, client.MergeFrom(before))).To(Succeed())
		}).WithTimeout(time.Minute).Should(Succeed())
		waitForManagedRolloutLimit(ctx, operatorv1.DefaultServiceDaemonSetMaxUnavailableFallback)

		By("Checking that a configured rollout limit of five is propagated")
		Eventually(func(g Gomega) {
			g.Expect(input.client.Get(ctx, key, config)).To(Succeed())
			before := config.DeepCopy()
			if config.Spec.DPUServiceController == nil {
				config.Spec.DPUServiceController = &operatorv1.DPUServiceControllerConfiguration{}
			}
			config.Spec.DPUServiceController.DefaultServiceDaemonSetMaxUnavailable = 5
			g.Expect(input.client.Patch(ctx, config, client.MergeFrom(before))).To(Succeed())
		}).WithTimeout(time.Minute).Should(Succeed())
		waitForManagedRolloutLimit(ctx, 5)
	})

	It("updates the healthy worker while the other DPU worker is unavailable", Labels{Domain.RequiresNodes}, func() {
		if !input.hasDpuNodes() {
			Skip("Requires actual DPU workers")
		}
		VerifyDPUClusterWithNodes(ctx, getProvisionDPUClustersInput())
		VerifyClusterPods(ctx, dpuClusterClient[0], dpuClusterPodsToVerify())
		var workerHostnames []string
		for _, node := range getDPUClusterNodes(ctx, dpuClusterClient[0]) {
			if node.Labels[provisioningv1.DPUNodeNameLabel] != "" && node.Labels[corev1.LabelHostname] != "" && node.Labels["e2e.test.io/fake-node"] != "true" && rolloutNodeReady(&node) {
				workerHostnames = append(workerHostnames, node.Labels[corev1.LabelHostname])
			}
		}
		if len(workerHostnames) < 2 {
			Skip("Requires at least two Ready, non-mock DPU workers")
		}
		service := createRolloutDPUService(ctx, "rollout-unavailable-worker", nil, workerHostnames[:2]...)
		var ds *appsv1.DaemonSet
		Eventually(func(g Gomega) {
			ds = rolloutDaemonSet(ctx, g, service.Name)
			g.Expect(ds.Status.ObservedGeneration).To(Equal(ds.Generation))
			g.Expect(ds.Status.DesiredNumberScheduled).To(Equal(int32(2)))
			g.Expect(ds.Status.NumberReady).To(Equal(int32(2)))
		}).WithTimeout(5 * time.Minute).Should(Succeed())
		// Check rollout progress on both old and fixed builds without requiring the new default here.
		strategyJSON, err := json.Marshal(ds.Spec.UpdateStrategy)
		Expect(err).NotTo(HaveOccurred())
		GinkgoWriter.Printf("Rendered rollout strategy: %s\n", strategyJSON)
		pods := rolloutPods(ctx, Default, ds)
		Expect(pods).To(HaveLen(2))
		victim, healthy := pods[0], pods[1]

		pauseRolloutWorker(ctx, victim.Spec.NodeName, service.Name)
		By("Checking that an unavailable daemon pod remains on the failed worker")
		Eventually(func(g Gomega) {
			node := &corev1.Node{}
			g.Expect(dpuClusterClient[0].Get(ctx, client.ObjectKey{
				Name: victim.Spec.NodeName,
			}, node)).To(Succeed())
			g.Expect(rolloutNodeReady(node)).To(BeFalse())
			unavailablePod := &corev1.Pod{}
			g.Expect(dpuClusterClient[0].Get(ctx, client.ObjectKeyFromObject(&victim), unavailablePod)).To(Succeed())
			g.Expect(unavailablePod.UID).To(Equal(victim.UID))
			g.Expect(rolloutPodReady(unavailablePod)).To(BeFalse())
			ds = rolloutDaemonSet(ctx, g, service.Name)
			// The failed worker may stop being eligible, leaving its old pod misscheduled.
			g.Expect(ds.Status.DesiredNumberScheduled).To(BeNumerically(">=", 1))
			g.Expect(ds.Status.DesiredNumberScheduled).To(BeNumerically("<=", 2))
			g.Expect(ds.Status.NumberUnavailable + ds.Status.NumberMisscheduled).To(BeNumerically(">=", 1))
			g.Expect(ds.Status.NumberReady).To(Equal(int32(1)))
		}).WithTimeout(2 * time.Minute).Should(Succeed())

		By("Triggering the rollout through the DPUService")
		before := service.DeepCopy()
		service.Spec.ServiceDaemonSet.Annotations = map[string]string{rolloutTestLabel: "updated"}
		Expect(input.client.Patch(ctx, service, client.MergeFrom(before))).To(Succeed())
		Eventually(func(g Gomega) {
			ds := rolloutDaemonSet(ctx, g, service.Name)
			g.Expect(ds.Spec.Template.Annotations).To(HaveKeyWithValue(rolloutTestLabel, "updated"))
			g.Expect(ds.Status.ObservedGeneration).To(Equal(ds.Generation))
			node := &corev1.Node{}
			g.Expect(dpuClusterClient[0].Get(ctx, client.ObjectKey{
				Name: victim.Spec.NodeName,
			}, node)).To(Succeed())
			g.Expect(rolloutNodeReady(node)).To(BeFalse(), "the worker must remain unavailable during rollout")
			var replacement *corev1.Pod
			for _, pod := range rolloutPods(ctx, g, ds) {
				if pod.Spec.NodeName == healthy.Spec.NodeName && pod.DeletionTimestamp == nil {
					p := pod
					replacement = &p
				}
			}
			g.Expect(replacement).NotTo(BeNil())
			g.Expect(replacement.UID).NotTo(Equal(healthy.UID))
			g.Expect(replacement.Annotations).To(HaveKeyWithValue(rolloutTestLabel, "updated"))
			g.Expect(rolloutPodReady(replacement)).To(BeTrue())
		}).WithTimeout(2 * time.Minute).Should(Succeed())
	})
})

// waitForManagedRolloutLimit checks rendered resources rather than Helm input values.
func waitForManagedRolloutLimit(ctx context.Context, want int32) {
	Eventually(func(g Gomega) {
		for _, component := range []operatorv1.ComponentName{operatorv1.MultusName, operatorv1.SFCControllerName} {
			sets := &appsv1.DaemonSetList{}
			g.Expect(dpuClusterClient[0].List(ctx, sets, client.MatchingLabels{operatorv1.DPFComponentLabelKey: component.String()})).To(Succeed())
			g.Expect(sets.Items).To(HaveLen(1), "component %s", component)
			strategy := sets.Items[0].Spec.UpdateStrategy
			g.Expect(strategy.Type).To(Equal(appsv1.RollingUpdateDaemonSetStrategyType))
			g.Expect(strategy.RollingUpdate).NotTo(BeNil())
			g.Expect(strategy.RollingUpdate.MaxUnavailable).To(Equal(ptr.To(intstr.FromInt32(want))), "component %s", component)
		}
	}).WithTimeout(5 * time.Minute).Should(Succeed())
}

// createRolloutDPUService uses the published dummy chart and registers unconditional cleanup.
func createRolloutDPUService(ctx context.Context, name string, strategy *appsv1.DaemonSetUpdateStrategy, workerHostnames ...string) *dpuservicev1.DPUService {
	service := &dpuservicev1.DPUService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: dpfOperatorSystemNamespace,
			Labels:    CleanupScope.It,
		},
		Spec: dpuservicev1.DPUServiceSpec{
			ServiceID: ptr.To(name),
			Security: &dpuservicev1.DPUServiceSecurity{
				Privileged: ptr.To(false),
			},
			HelmChart: dpuservicev1.HelmChart{
				Source: dpuservicev1.ApplicationSource{
					Chart:   "dummydpuservice-chart",
					Version: tag,
					RepoURL: helmRegistry,
				},
				Values: &runtime.RawExtension{
					Raw: []byte(fmt.Sprintf(`{"imagePullSecrets":[{"name":%q}]}`, dpfPullSecretName)),
				},
			},
			ServiceDaemonSet: &dpuservicev1.ServiceDaemonSetValues{
				Labels:         map[string]string{rolloutTestLabel: name},
				UpdateStrategy: strategy,
			},
		},
	}
	if len(workerHostnames) > 0 {
		service.Spec.ServiceDaemonSet.NodeSelector = &corev1.NodeSelector{
			NodeSelectorTerms: []corev1.NodeSelectorTerm{{
				MatchExpressions: []corev1.NodeSelectorRequirement{{
					Key:      corev1.LabelHostname,
					Operator: corev1.NodeSelectorOpIn,
					Values:   workerHostnames,
				}},
			}},
		}
	}
	DeferCleanup(func(cleanupCtx context.Context) {
		Expect(client.IgnoreNotFound(input.client.Delete(cleanupCtx, service))).To(Succeed())
		Eventually(func(g Gomega) {
			sets := &appsv1.DaemonSetList{}
			g.Expect(dpuClusterClient[0].List(cleanupCtx, sets, client.MatchingLabels{rolloutTestLabel: name})).To(Succeed())
			g.Expect(sets.Items).To(BeEmpty())
		}).WithTimeout(3 * time.Minute).Should(Succeed())
	})
	Expect(input.client.Create(ctx, service)).To(Succeed())
	return service
}

// rolloutDaemonSet locates the chart resource through a test-specific contract label.
func rolloutDaemonSet(ctx context.Context, g Gomega, name string) *appsv1.DaemonSet {
	sets := &appsv1.DaemonSetList{}
	g.Expect(dpuClusterClient[0].List(ctx, sets, client.MatchingLabels{rolloutTestLabel: name})).To(Succeed())
	g.Expect(sets.Items).To(HaveLen(1))
	return &sets.Items[0]
}

// rolloutPods returns pods selected by the rendered DaemonSet.
func rolloutPods(ctx context.Context, g Gomega, ds *appsv1.DaemonSet) []corev1.Pod {
	pods := &corev1.PodList{}
	g.Expect(dpuClusterClient[0].List(ctx, pods, client.InNamespace(ds.Namespace), client.MatchingLabels(ds.Spec.Selector.MatchLabels))).To(Succeed())
	return pods.Items
}

// rolloutNodeReady checks the kubelet's reported readiness.
func rolloutNodeReady(node *corev1.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

// rolloutPodReady checks readiness without treating a terminating pod as available.
func rolloutPodReady(pod *corev1.Pod) bool {
	if pod.DeletionTimestamp != nil {
		return false
	}
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

// pauseRolloutWorker arms host-side recovery before suspending the kubelet.
// Recovery does not depend on the paused kubelet or the test runner staying alive.
// Shell commands run on the worker to manage systemd timers, so recovery survives a test-runner failure.
func pauseRolloutWorker(ctx context.Context, nodeName, serviceName string) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "rollout-kubelet-pause-",
			Namespace:    dpfOperatorSystemNamespace,
		},
		Spec: corev1.PodSpec{
			NodeName:      nodeName,
			HostPID:       true,
			RestartPolicy: corev1.RestartPolicyNever,
			Tolerations: []corev1.Toleration{
				{
					Key:      corev1.TaintNodeNotReady,
					Operator: corev1.TolerationOpExists,
					Effect:   corev1.TaintEffectNoExecute,
				},
				{
					Key:      corev1.TaintNodeUnreachable,
					Operator: corev1.TolerationOpExists,
					Effect:   corev1.TaintEffectNoExecute,
				},
			},
			ImagePullSecrets: []corev1.LocalObjectReference{{
				Name: dpfPullSecretName,
			}},
			Containers: []corev1.Container{{
				Name:  "pause",
				Image: netutilsImage + ":" + tag,
				// Keep the container running for worker recovery commands.
				Command: []string{"tail", "-F", "/dev/null"},
				SecurityContext: &corev1.SecurityContext{
					Privileged: ptr.To(true),
				},
				VolumeMounts: []corev1.VolumeMount{{
					Name:      "host",
					MountPath: "/host",
				}},
			}},
			Volumes: []corev1.Volume{{
				Name: "host",
				VolumeSource: corev1.VolumeSource{
					HostPath: &corev1.HostPathVolumeSource{
						Path: "/",
						Type: ptr.To(corev1.HostPathDirectory),
					},
				},
			}},
		},
	}
	Expect(dpuClusterClient[0].Create(ctx, pod)).To(Succeed())
	pauseUnit := pod.Name + "-pause"
	resumeUnit := pod.Name + "-resume"
	recoveryArmed := false
	DeferCleanup(func(cleanupCtx context.Context) {
		if !recoveryArmed {
			Expect(client.IgnoreNotFound(dpuClusterClient[0].Delete(cleanupCtx, pod))).To(Succeed())
			return
		}
		By("Waiting for the independent watchdog to restore the worker")
		Eventually(func(g Gomega) {
			node := &corev1.Node{}
			g.Expect(dpuClusterClient[0].Get(cleanupCtx, client.ObjectKey{
				Name: nodeName,
			}, node)).To(Succeed())
			g.Expect(rolloutNodeReady(node)).To(BeTrue())
			// Cancel any pending pause, including when the initial exec failed before the pause.
			// Also remove the timers after watchdog recovery so repeated runs are independent.
			cleanupScript := fmt.Sprintf(`for unit in %s.timer %s.timer; do
if systemctl cat "$unit" >/dev/null 2>&1; then systemctl stop "$unit"; fi
done
systemctl kill --kill-whom=main --signal=CONT kubelet.service`, pauseUnit, resumeUnit)
			_, err := netshoot.ExecInPodOnce(dpuClusterRestClient[0], dpuClusterRestConfig[0], pod.Namespace, pod.Name, []string{"chroot", "/host", "/bin/sh", "-ec", cleanupScript})
			g.Expect(err).NotTo(HaveOccurred())
		}).WithTimeout(8 * time.Minute).WithPolling(time.Second).Should(Succeed())
		Expect(client.IgnoreNotFound(dpuClusterClient[0].Delete(cleanupCtx, pod))).To(Succeed())
		VerifyClusterPods(cleanupCtx, dpuClusterClient[0], dpuClusterPodsToVerify())
		Eventually(func(g Gomega) {
			ds := rolloutDaemonSet(cleanupCtx, g, serviceName)
			g.Expect(ds.Status.ObservedGeneration).To(Equal(ds.Generation))
			readyWorkers := map[string]struct{}{}
			for _, pod := range rolloutPods(cleanupCtx, g, ds) {
				if rolloutPodReady(&pod) && pod.Annotations[rolloutTestLabel] == ds.Spec.Template.Annotations[rolloutTestLabel] {
					readyWorkers[pod.Spec.NodeName] = struct{}{}
				}
			}
			g.Expect(readyWorkers).To(HaveLen(2), "both workers must have Ready pods on the current template")
		}).WithTimeout(3 * time.Minute).Should(Succeed())
	})
	Eventually(func(g Gomega) {
		g.Expect(dpuClusterClient[0].Get(ctx, client.ObjectKeyFromObject(pod), pod)).To(Succeed())
		g.Expect(rolloutPodReady(pod)).To(BeTrue())
	}).WithTimeout(3 * time.Minute).Should(Succeed())
	By("Checking the worker supports kubelet suspension and host-side recovery")
	preflight := `if command -v systemd-run >/dev/null && command -v systemctl >/dev/null && test -x /bin/kill && systemctl is-active --quiet kubelet.service && test "$(systemctl show kubelet.service --property MainPID --value)" -gt 1; then printf supported; else printf unsupported; fi`
	output, err := netshoot.ExecInPodOnce(dpuClusterRestClient[0], dpuClusterRestConfig[0], pod.Namespace, pod.Name, []string{"chroot", "/host", "/bin/sh", "-c", preflight})
	Expect(err).NotTo(HaveOccurred(), "failed to inspect worker recovery capabilities")
	if strings.TrimSpace(output) != "supported" {
		Skip("Worker requires an active kubelet.service, systemd-run, and /bin/kill for watchdog recovery")
	}
	By("Arming a six-minute watchdog, then pausing kubelet after the exec request returns")
	script := fmt.Sprintf(`set -eu
pid=$(systemctl show kubelet.service --property MainPID --value)
test "$pid" -gt 1
systemctl is-active --quiet kubelet.service
systemd-run --collect --unit=%s --on-active=6m --timer-property=AccuracySec=1s /bin/kill -CONT "$pid"
systemd-run --collect --unit=%s --on-active=5s --timer-property=AccuracySec=1s /bin/kill -STOP "$pid"`, resumeUnit, pauseUnit)
	// Register recovery handling before the exec, which may fail after partially arming the timers.
	recoveryArmed = true
	_, err = netshoot.ExecInPodOnce(dpuClusterRestClient[0], dpuClusterRestConfig[0], pod.Namespace, pod.Name, []string{"chroot", "/host", "/bin/sh", "-c", script})
	Expect(err).NotTo(HaveOccurred())
}
