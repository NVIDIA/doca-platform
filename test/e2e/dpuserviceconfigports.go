/*
Copyright 2025 NVIDIA

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
	"net/http"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/dummydpuservice"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	machineryruntime "k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func ValidateDPUServiceConfigPorts(ctx context.Context, input *systemTestInput) {
	if !input.hasDpuNodes() {
		Skip("Skip DPUService ConfigPorts test as there are no DPU nodes")
	}

	By("Creating a DPUService with ConfigPorts")
	dpuService := input.dpuService.DeepCopy()
	dpuService.Name = "dummydpuservice"
	dpuService.Namespace = dpfOperatorSystemNamespace
	dpuService.SetLabels(afterEachCleanupLabels)
	dpuService.Spec.HelmChart.Source = dpuservicev1.ApplicationSource{
		Chart:   "dummydpuservice-chart",
		Version: tag,
		RepoURL: helmRegistry,
	}
	dpuService.Spec.HelmChart.Values = &machineryruntime.RawExtension{
		Raw: []byte(`{"imagePullSecrets": [{"name": "dpf-pull-secret"}]}`),
	}
	dpuService.Spec.ConfigPorts = &dpuservicev1.ConfigPorts{
		// TODO: test also ClusterIP. Currently this is not working as k3s doesn't have kube-proxy deployed.
		ServiceType: corev1.ServiceTypeNodePort,
		Ports: []dpuservicev1.ConfigPort{
			{
				Name:     "exampletcp",
				Port:     8080,
				Protocol: corev1.ProtocolTCP,
			},
		},
	}
	Expect(input.client.Create(ctx, dpuService)).To(Succeed())

	By("Waiting for dummydpuservice Pods to be ready")
	VerifyDPUClusterPods(ctx, []string{"dummydpuservice"})

	By("Verifying the ConfigPorts are exposed via the DPUService")
	Eventually(func(g Gomega) {
		g.Expect(input.client.Get(ctx, client.ObjectKeyFromObject(dpuService), dpuService)).To(Succeed())
		g.Expect(dpuService.Status.ConfigPorts).NotTo(BeNil())
	}).WithTimeout(120 * time.Second).Should(Succeed())

	By("Verifying reachability to ConfigPorts from the DPU Cluster")
	// First get the nodePort of the ConfigPort exposed on the host cluster.
	var nodePort *uint16
	for _, port := range dpuService.Status.ConfigPorts {
		if port[0].Name == "exampletcp" {
			nodePort = port[0].NodePort
		}
	}
	// Then get the name and IPs of the host nodes and check reachability.
	nodeIPs := make(map[string]string)
	nodeList := &corev1.NodeList{}
	Expect(input.client.List(ctx, nodeList, client.MatchingLabels{
		"feature.node.kubernetes.io/dpu-enabled": "true",
	})).To(Succeed())
	for _, node := range nodeList.Items {
		for _, addr := range node.Status.Addresses {
			if addr.Type == corev1.NodeInternalIP {
				// assuming only one NodeInternalIP per node
				nodeIPs[node.Name] = addr.Address
				break
			}
		}
	}
	// And finally check reachability by looping over all nodes.
	for nodeName, nodeIP := range nodeIPs {
		dpuNodeIP, err := getDPUIPForHost(ctx, input.client, nodeName)
		Expect(err).NotTo(HaveOccurred())
		Eventually(func(g Gomega) {
			resp, err := http.Get(fmt.Sprintf("http://%s:%d", nodeIP, *nodePort))
			g.Expect(err).NotTo(HaveOccurred())
			defer func() {
				err := resp.Body.Close()
				g.Expect(err).NotTo(HaveOccurred())
			}()

			var podInfo dummydpuservice.PodInfo
			g.Expect(json.NewDecoder(resp.Body).Decode(&podInfo)).To(Succeed())
			g.Expect(podInfo.NodeIP).To(Equal(dpuNodeIP))
			g.Expect(podInfo.PodNamespace).To(Equal(dpfOperatorSystemNamespace))
			// This timeout needs to be big enough because in multi node nic cloud setup the image pulling may take
			// longer than expected
		}).WithTimeout(30 * time.Minute).Should(Succeed())
	}
}

func getDPUIPForHost(ctx context.Context, c client.Client, nodeName string) (string, error) {
	var dpuList provisioningv1.DPUList
	if err := c.List(ctx, &dpuList, client.MatchingLabels{
		cutil.DPUNodeNameLabel: nodeName,
	}); err != nil {
		return "", err
	}
	if len(dpuList.Items) == 0 || len(dpuList.Items) > 1 {
		return "", fmt.Errorf("no DPU found for host %s", nodeName)
	}
	for _, addr := range dpuList.Items[0].Status.Addresses {
		if addr.Type == corev1.NodeInternalIP {
			return addr.Address, nil
		}
	}
	return "", fmt.Errorf("no DPU IP found for host %s", nodeName)
}
