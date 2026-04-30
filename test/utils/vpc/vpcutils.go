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

package vpc

import (
	"context"
	"fmt"
	"maps"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	nadutils "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/utils"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	DefaultTimeout = 1 * time.Minute
	LongTimeout    = 6 * time.Minute
)

func CreateTestNamespace(ctx context.Context, testClient client.Client, namespace string, labels map[string]string) {
	testNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
	testNS.SetLabels(labels)
	Expect(client.IgnoreAlreadyExists(testClient.Create(ctx, testNS))).To(Succeed())
}

// GetPodIPAddressFromNetworkStatus returns the IP address of a pod from the network status with the given interface name.
func GetPodIPAddressFromNetworkStatus(ctx context.Context, testClient client.Client, namespace, podName string, interfaceName string) string {
	pod := &corev1.Pod{}
	Expect(testClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: podName}, pod)).To(Succeed())
	networkStatusList, err := nadutils.GetNetworkStatus(pod)
	Expect(err).NotTo(HaveOccurred())
	for _, network := range networkStatusList {
		if network.Interface == interfaceName {
			return network.IPs[0]
		}
	}
	return ""
}

// GetPodsMatchingLabels gets the pods matching the given labels.
func GetPodsMatchingLabels(ctx context.Context, testclient client.Client, namespace string, matchingLabels map[string]string) []corev1.Pod {
	pods := &corev1.PodList{}
	err := testclient.List(
		ctx,
		pods,
		client.InNamespace(namespace),
		client.MatchingLabels(matchingLabels),
	)
	Expect(err).NotTo(HaveOccurred())
	return pods.Items
}

// UpdateDPUNodeLabelsMerge adds/updates labels from toSet and deletes labels in toDelete.
func UpdateDPUNodeLabelsMerge(ctx context.Context, c client.Client, nodeName string, toSet map[string]string, toDelete []string) {
	node := &corev1.Node{}
	Expect(c.Get(ctx, client.ObjectKey{Name: nodeName}, node)).To(Succeed())
	original := node.DeepCopy()

	if node.Labels == nil {
		node.Labels = make(map[string]string)
	}

	maps.Copy(node.Labels, toSet)
	for _, k := range toDelete {
		delete(node.Labels, k)
	}

	Expect(c.Patch(ctx, node, client.MergeFrom(original))).To(Succeed())
}

// SetLinkMacAddress sets the MAC address on a link via SSH to the host (idempotent).
func SetLinkMacAddress(hostIP net.IP, interfaceName, macAddress string) {
	password := os.Getenv("VM_PASSWORD")
	Expect(password).ToNot(BeEmpty(), "VM_PASSWORD environment variable is not set")

	Expect(strings.ContainsAny(interfaceName, ";|&$`")).To(BeFalse(), "invalid characters in interface name")
	Expect(strings.ContainsAny(macAddress, ";|&$`")).To(BeFalse(), "invalid characters in MAC address")

	sshKeygenCmd := fmt.Sprintf("ssh-keygen -R %s 2>/dev/null || true", hostIP.String())
	Expect(exec.Command("bash", "-c", sshKeygenCmd).Run()).To(Succeed(), "failed to remove host from known_hosts")

	sshOpts := "-o StrictHostKeyChecking=no -o PubkeyAuthentication=no -o ConnectTimeout=30 -o ServerAliveInterval=15 -o ServerAliveCountMax=3"
	sshCommand := fmt.Sprintf("sshpass -p '%s' ssh %s root@%s 'ip link set dev %s down && ip link set dev %s address %s && ip link set dev %s up'",
		password, sshOpts, hostIP.String(), interfaceName, interfaceName, macAddress, interfaceName)

	Eventually(func(g Gomega) {
		cmd := exec.Command("bash", "-c", sshCommand)
		output, runErr := cmd.CombinedOutput()
		g.Expect(runErr).ToNot(HaveOccurred(), "failed to set MAC address on %s, output: %s", hostIP.String(), string(output))
	}, DefaultTimeout).Should(Succeed())
}

// CreateDHCPNetworkAttachmentDefinition creates a NAD using the host-device CNI plugin
// with DHCP IPAM and a tuning plugin for MTU configuration. Works for both VFs and PFs.
func CreateDHCPNetworkAttachmentDefinition(ctx context.Context, testClient client.Client, namespace, nadName, hostDevice string, mtu int, labels map[string]string) {
	nad := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "k8s.cni.cncf.io/v1",
			"kind":       "NetworkAttachmentDefinition",
			"metadata": map[string]interface{}{
				"name":      nadName,
				"namespace": namespace,
				"labels":    labels,
			},
			"spec": map[string]interface{}{
				"config": fmt.Sprintf(`{
					"cniVersion": "0.4.0",
					"name": "%s",
					"plugins": [
						{
							"type": "host-device",
							"device": "%s",
							"ipam": {
								"type": "dhcp"
							}
						},
						{
							"type": "tuning",
							"mtu": %d
						}
					]
				}`, nadName, hostDevice, mtu),
			},
		},
	}
	Expect(client.IgnoreAlreadyExists(testClient.Create(ctx, nad))).To(Succeed())
}
