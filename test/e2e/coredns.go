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
	"maps"
	"time"

	kamaji "github.com/nvidia/doca-platform/internal/clustermanager/kamaji"
	"github.com/nvidia/doca-platform/internal/operator/inventory"
	"github.com/nvidia/doca-platform/test/utils/netshoot"
	kamajiv1 "github.com/nvidia/doca-platform/third_party/forked/github.com/clastix/kamaji/api/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// externalDNSName is resolved to cover the forward path out of the cluster domain. It has to be a
// name the resolver of the host cluster nodes can answer.
const externalDNSName = "google.com"

var tenantControlPlaneGVK = schema.GroupVersionKind{
	Group:   "kamaji.clastix.io",
	Version: "v1alpha1",
	Kind:    "TenantControlPlane",
}

// hasHostClusterDNS reports whether host cluster DNS applies to this deployment. It is only wired
// up for DPUClusters exposing a keepalived VIP, because the VIP is how a DPU node reaches the host
// cluster CoreDNS NodePort. Static DPUClusters have no VIP and keep serving DNS from inside the DPU
// cluster, so these specs do not apply to them.
func hasHostClusterDNS(input *systemTestInput) bool {
	for _, dpuCluster := range input.dpuClusters {
		if !inventory.IsDPUClusterServedByHostDNS(dpuCluster) {
			return false
		}
	}
	return len(input.dpuClusters) > 0
}

// getCoreDNSListOptions selects the host cluster CoreDNS objects serving the DPUCluster with the given
// name and namespace. They are found by label because their names carry a hash.
func getCoreDNSListOptions(clusterName, clusterNamespace string) []client.ListOption {
	return []client.ListOption{
		client.InNamespace(dpfOperatorSystemNamespace),
		client.MatchingLabels(inventory.GetCoreDNSWorkloadLabels(clusterName, clusterNamespace)),
	}
}

// VerifyKamajiCoreDNSAddonDisabled verifies the Kamaji CoreDNS addon is off for every DPUCluster.
// If it were on, a second CoreDNS would run on the DPUs answering for the same zone, which is
// exactly what moving DNS to the host cluster is meant to avoid.
func VerifyKamajiCoreDNSAddonDisabled(ctx context.Context, input *systemTestInput) {
	for _, dpuCluster := range input.dpuClusters {
		By(fmt.Sprintf("Checking the CoreDNS addon is disabled for DPUCluster %s", dpuCluster.Name))

		Eventually(func(g Gomega) {
			tcp := &unstructured.Unstructured{}
			tcp.SetGroupVersionKind(tenantControlPlaneGVK)
			g.Expect(input.client.Get(ctx, client.ObjectKey{
				Name:      dpuCluster.Name,
				Namespace: dpuCluster.Namespace,
			}, tcp)).To(Succeed())

			_, found, err := unstructured.NestedMap(tcp.Object, "spec", "addons", "coreDNS")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(found).To(BeFalse(), "the Kamaji CoreDNS addon should not be set")

			// kube-proxy must stay on: the DPU cluster still needs Service routing.
			_, found, err = unstructured.NestedMap(tcp.Object, "spec", "addons", "kubeProxy")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(found).To(BeTrue(), "the Kamaji kube-proxy addon should stay enabled")
		}).WithTimeout(2 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())
	}
}

// VerifyHostClusterCoreDNS verifies that every DPUCluster gets its own CoreDNS running on the host
// cluster, exposed by a Service the DPU nodes reach through the keepalived VIP.
func VerifyHostClusterCoreDNS(ctx context.Context, input *systemTestInput) {
	for _, dpuCluster := range input.dpuClusters {
		listOptions := getCoreDNSListOptions(dpuCluster.Name, dpuCluster.Namespace)
		By(fmt.Sprintf("Checking host cluster CoreDNS for DPUCluster %s", dpuCluster.Name))

		Eventually(func(g Gomega) {
			deployments := &appsv1.DeploymentList{}
			g.Expect(input.client.List(ctx, deployments, listOptions...)).To(Succeed())
			g.Expect(deployments.Items).To(HaveLen(1), "exactly one CoreDNS should serve a DPUCluster")
			g.Expect(deployments.Items[0].Status.ReadyReplicas).To(BeNumerically(">", 0),
				"CoreDNS should have at least one ready replica")

			services := &corev1.ServiceList{}
			g.Expect(input.client.List(ctx, services, listOptions...)).To(Succeed())
			g.Expect(services.Items).To(HaveLen(1), "exactly one CoreDNS Service should serve a DPUCluster")
		}).WithTimeout(10 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())
	}
}

// VerifyDPUClusterDNSEndpoint verifies the DNS Service in each DPU cluster is backed by the host
// cluster CoreDNS serving that same DPUCluster. The EndpointSlice matters as much as the Service:
// ArgoCD excludes EndpointSlice from the resources it manages, which is why the cluster manager
// writes both objects directly rather than shipping them in a chart.
func VerifyDPUClusterDNSEndpoint(ctx context.Context, input *systemTestInput) {
	for i, dpuClient := range dpuClusterClient {
		dpuCluster := input.dpuClusters[i]
		By(fmt.Sprintf("Checking the DNS Service in DPU cluster %s", dpuCluster.Name))

		Expect(dpuCluster.Spec.ClusterEndpoint).ToNot(BeNil())
		Expect(dpuCluster.Spec.ClusterEndpoint.Keepalived).ToNot(BeNil())
		wantAddress := dpuCluster.Spec.ClusterEndpoint.Keepalived.VIP

		Eventually(func(g Gomega) {
			dnsService := &corev1.Service{}
			g.Expect(dpuClient.Get(ctx, client.ObjectKey{
				Name:      kamaji.KubeDNSServiceName,
				Namespace: metav1.NamespaceSystem,
			}, dnsService)).To(Succeed())

			// No selector: nothing in the DPU cluster serves DNS, the endpoints are supplied
			// explicitly and point at the host cluster.
			g.Expect(dnsService.Spec.Selector).To(BeEmpty())
			g.Expect(dnsService.Spec.ClusterIP).ToNot(BeEmpty())
			g.Expect(dnsService.Spec.ClusterIP).ToNot(Equal(corev1.ClusterIPNone))

			endpointSlices := &discoveryv1.EndpointSliceList{}
			g.Expect(dpuClient.List(ctx, endpointSlices, client.InNamespace(metav1.NamespaceSystem),
				client.MatchingLabels{discoveryv1.LabelServiceName: kamaji.KubeDNSServiceName})).To(Succeed())
			g.Expect(endpointSlices.Items).ToNot(BeEmpty(), "the DNS Service should have an EndpointSlice")

			addresses := []string{}
			for _, endpointSlice := range endpointSlices.Items {
				for _, endpoint := range endpointSlice.Endpoints {
					addresses = append(addresses, endpoint.Addresses...)
				}
			}

			// The VIP is what distinguishes the CoreDNS serving this DPUCluster from the one
			// serving any other, so this is what proves the clusters are not crossed.
			g.Expect(addresses).To(ConsistOf(wantAddress),
				"DNS should resolve against the CoreDNS serving this DPUCluster")
		}).WithTimeout(10 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())
	}
}

// VerifyDPUClusterServesOwnDNS verifies that a DPUCluster the host cluster does not serve keeps the
// CoreDNS Kamaji deployed for it. Without a keepalived VIP there is no NodePort path back to the
// host cluster, so taking that CoreDNS away would leave the DPU cluster with no DNS at all.
func VerifyDPUClusterServesOwnDNS(ctx context.Context, input *systemTestInput) {
	for i, dpuClient := range dpuClusterClient {
		dpuCluster := input.dpuClusters[i]
		if inventory.IsDPUClusterServedByHostDNS(dpuCluster) {
			continue
		}
		By(fmt.Sprintf("Checking DPUCluster %s still serves its own DNS", dpuCluster.Name))

		By("Verifying no host cluster CoreDNS was created for it")
		listOptions := getCoreDNSListOptions(dpuCluster.Name, dpuCluster.Namespace)
		deployments := &appsv1.DeploymentList{}
		Expect(input.client.List(ctx, deployments, listOptions...)).To(Succeed())
		Expect(deployments.Items).To(BeEmpty(),
			"no host cluster CoreDNS should exist for DPUCluster %s", dpuCluster.Name)
		services := &corev1.ServiceList{}
		Expect(input.client.List(ctx, services, listOptions...)).To(Succeed())
		Expect(services.Items).To(BeEmpty(),
			"no host cluster CoreDNS Service should exist for DPUCluster %s", dpuCluster.Name)

		// A TenantControlPlane only exists when the DPU cluster is Kamaji backed. Where it does, the
		// CoreDNS addon must be left alone, which is what keeps the DNS Pods running on the DPUs.
		tcp := &kamajiv1.TenantControlPlane{}
		err := input.client.Get(ctx, client.ObjectKey{Name: dpuCluster.Name, Namespace: dpuCluster.Namespace}, tcp)
		if err == nil {
			By("Verifying the Kamaji CoreDNS addon is still enabled")
			Expect(tcp.Spec.Addons.CoreDNS).NotTo(BeNil())
		} else {
			Expect(apierrors.IsNotFound(err)).To(BeTrue(),
				"unexpected error getting the TenantControlPlane for DPUCluster %s: %v", dpuCluster.Name, err)
		}

		By("Verifying the DNS Service is backed by Pods inside the DPU cluster")
		Eventually(func(g Gomega) {
			dnsService := &corev1.Service{}
			g.Expect(dpuClient.Get(ctx, client.ObjectKey{
				Name:      kamaji.KubeDNSServiceName,
				Namespace: metav1.NamespaceSystem,
			}, dnsService)).To(Succeed())

			// A DNS Service that still has a selector is the untouched Kamaji one, so its endpoints
			// are the CoreDNS Pods running in this DPU cluster. The host cluster case is the
			// opposite: no selector, and an EndpointSlice this operator writes.
			g.Expect(dnsService.Spec.Selector).ToNot(BeEmpty(),
				"an in cluster DNS Service should select its CoreDNS Pods")

			endpointSlices := &discoveryv1.EndpointSliceList{}
			g.Expect(dpuClient.List(ctx, endpointSlices, client.InNamespace(metav1.NamespaceSystem),
				client.MatchingLabels{discoveryv1.LabelServiceName: kamaji.KubeDNSServiceName})).To(Succeed())

			ready := 0
			for _, endpointSlice := range endpointSlices.Items {
				for _, endpoint := range endpointSlice.Endpoints {
					if endpoint.Conditions.Ready != nil && *endpoint.Conditions.Ready {
						ready += len(endpoint.Addresses)
					}
				}
			}
			g.Expect(ready).To(BeNumerically(">", 0), "the DNS Service should have a ready endpoint")
		}).WithTimeout(10 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())
	}
}

// ValidateDPUClusterDNSResolution resolves a Service name from inside a DPU cluster Pod. This is the
// end to end check, and it applies whichever CoreDNS answers: it only passes if kubelet hands Pods
// the DNS Service ClusterIP and something is actually serving that address.
func ValidateDPUClusterDNSResolution(ctx context.Context, input *systemTestInput) {
	for i, dpuClient := range dpuClusterClient {
		dpuCluster := input.dpuClusters[i]
		By(fmt.Sprintf("Resolving a Service name from a Pod in DPU cluster %s", dpuCluster.Name))

		// Moving DNS to the host cluster deliberately keeps the ClusterIP kubeadm configured
		// kubelet with, so nothing on an already provisioned DPU has to be reconfigured.
		dnsService := &corev1.Service{}
		Expect(dpuClient.Get(ctx, client.ObjectKey{
			Name:      kamaji.KubeDNSServiceName,
			Namespace: metav1.NamespaceSystem,
		}, dnsService)).To(Succeed())

		podName := "coredns-resolution-check"
		pod := generateDNSTestPod(podName)
		Expect(client.IgnoreAlreadyExists(dpuClient.Create(ctx, pod))).To(Succeed())
		DeferCleanup(func() {
			// Best effort: the DPU cluster is reached through a port forward that may already be
			// closing by the time cleanup runs, and a torn down tunnel must not fail a spec whose
			// assertions have all passed. A Pod left behind is idle and is reused by name on the
			// next run.
			if err := client.IgnoreNotFound(dpuClient.Delete(ctx, pod)); err != nil {
				GinkgoWriter.Printf("failed to delete DNS test Pod %s: %v\n", podName, err)
			}
		})
		waitForDNSTestPodReady(ctx, dpuClient, podName)

		// kubernetes.default always exists, so this needs no fixture in the DPU cluster. Resolving
		// it proves the host cluster CoreDNS is watching this DPU cluster and not another one.
		// nslookup reports the server it used, which is the resolver kubelet handed the Pod.
		Eventually(func(g Gomega) {
			stdout, err := netshoot.ExecInPodOnce(dpuClusterRestClient[i], dpuClusterRestConfig[i],
				dpfOperatorSystemNamespace, podName,
				[]string{"nslookup", "kubernetes.default.svc.cluster.local"})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(stdout).To(ContainSubstring(dnsService.Spec.ClusterIP),
				"Pods should resolve against the DNS Service ClusterIP")
			g.Expect(stdout).To(ContainSubstring("kubernetes.default.svc.cluster.local"))
		}).WithTimeout(1 * time.Minute).WithPolling(1 * time.Second).Should(Succeed())

		// A name outside the cluster domain takes the other path through CoreDNS, the forward
		// plugin handing it to the resolver of the host cluster node CoreDNS runs on.
		By("Resolving a name outside the cluster domain")
		Eventually(func(g Gomega) {
			stdout, err := netshoot.ExecInPodOnce(dpuClusterRestClient[i], dpuClusterRestConfig[i],
				dpfOperatorSystemNamespace, podName,
				[]string{"nslookup", externalDNSName})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(stdout).To(ContainSubstring(externalDNSName))
		}).WithTimeout(1 * time.Minute).WithPolling(1 * time.Second).Should(Succeed())
	}
}

// generateDNSTestPod returns a Pod used to inspect DNS from inside a DPU cluster.
func generateDNSTestPod(name string) *corev1.Pod {
	// The cleanup tracker only sweeps the host cluster, so these labels do not delete the Pod. They
	// mark it as e2e owned in the DPU cluster, matching how the other suites label the DPU cluster
	// objects they create, so a leaked Pod is easy to find and remove.
	cleanupLabels := make(map[string]string)
	maps.Copy(cleanupLabels, CleanupScope.It)
	maps.Copy(cleanupLabels, CleanupScope.Suite)

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: dpfOperatorSystemNamespace,
			Labels:    cleanupLabels,
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Tolerations: []corev1.Toleration{
				{Operator: corev1.TolerationOpExists},
			},
			Containers: []corev1.Container{
				{
					Name:    "netshoot",
					Image:   netshoot.Image,
					Command: []string{"/bin/sh", "-c"},
					Args:    []string{"tail -F /dev/null"},
				},
			},
		},
	}
}

func waitForDNSTestPodReady(ctx context.Context, c client.Client, name string) {
	Eventually(func(g Gomega) {
		pod := &corev1.Pod{}
		g.Expect(c.Get(ctx, client.ObjectKey{Name: name, Namespace: dpfOperatorSystemNamespace}, pod)).To(Succeed())
		g.Expect(pod.Status.Phase).To(Equal(corev1.PodRunning))
	}).WithTimeout(5 * time.Minute).WithPolling(1 * time.Second).Should(Succeed())
}
