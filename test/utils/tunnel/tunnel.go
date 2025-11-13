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

package tunnel

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/dpucluster"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Tunnel represents a port forwarding tunnel to a Kamaji cluster
type Tunnel struct {
	pf        *portforward.PortForwarder
	stopCh    chan struct{}
	errCh     chan error
	localPort int
}

// Close stops the tunnel
func (t *Tunnel) Close() {
	t.pf.Close()
	close(t.stopCh)
	// Close the error channel to prevent goroutine leaks
	if t.errCh != nil {
		close(t.errCh)
	}
}

// LocalPort returns the local port being used for the tunnel
func (t *Tunnel) LocalPort() int {
	return t.localPort
}

// IsHealthy checks if the tunnel is still healthy and operational.
// Returns true if the tunnel is healthy, false if an error has occurred or a Close() has been issued.
func (t *Tunnel) IsHealthy() bool {
	select {
	case <-t.errCh:
		return false
	case <-t.stopCh:
		return false
	default:
		return true
	}
}

// NewTunneledRestConfig creates a tunneled REST config for accessing the Kamaji cluster.
// Returns the REST config and a health check function that returns true if the tunnel is healthy.
func NewTunneledRestConfig(ctx context.Context, hostClient client.Client, hostRESTConfig *rest.Config, dpuCluster *provisioningv1.DPUCluster) (*rest.Config, func() bool) {
	// Create dpucluster.Config to handle kubeconfig retrieval
	clusterConfig := dpucluster.NewConfig(hostClient, dpuCluster)

	// Get the Kamaji cluster kubeconfig for authentication
	kamajiKubeconfig, err := clusterConfig.Kubeconfig(ctx)
	Expect(err).NotTo(HaveOccurred(), "Should get Kamaji kubeconfig")

	// Create REST config from Kamaji kubeconfig using clientcmd
	kamajiRESTConfig, err := clientcmd.NewDefaultClientConfig(*kamajiKubeconfig, &clientcmd.ConfigOverrides{}).ClientConfig()
	Expect(err).NotTo(HaveOccurred(), "Should create Kamaji REST config")

	// Set up port forwarding to the Kamaji cluster
	tunnel := setupPortForward(ctx, hostClient, hostRESTConfig, dpuCluster)

	// Update the host to use local port
	kamajiRESTConfig.Host = fmt.Sprintf("https://localhost:%d", tunnel.LocalPort())

	// Return REST config and a health check function
	healthCheck := func() bool {
		return tunnel.IsHealthy()
	}

	return kamajiRESTConfig, healthCheck
}

// NewTunneledClient creates a new client that tunnels through the host cluster to access the Kamaji cluster.
// This function works in air-gapped environments where only the Kubernetes API is accessible.
// Returns the client and a health check function that returns true if the tunnel is healthy.
func NewTunneledClient(ctx context.Context, hostClient client.Client, hostRESTConfig *rest.Config, dpuCluster *provisioningv1.DPUCluster) (client.Client, func() bool) {
	kamajiRESTConfig, healthCheck := NewTunneledRestConfig(ctx, hostClient, hostRESTConfig, dpuCluster)

	// Create client for Kamaji cluster
	kamajiClient, err := client.New(kamajiRESTConfig, client.Options{})
	Expect(err).NotTo(HaveOccurred(), "Should create Kamaji client")

	return kamajiClient, healthCheck
}

// NewTunneledClientset creates a new clientset that tunnels through the host cluster to access the Kamaji cluster.
// This function works in air-gapped environments where only the Kubernetes API is accessible.
// Returns the clientset and a health check function that returns true if the tunnel is healthy.
func NewTunneledClientset(ctx context.Context, hostClient client.Client, hostRESTConfig *rest.Config, dpuCluster *provisioningv1.DPUCluster) (*kubernetes.Clientset, func() bool) {
	kamajiRESTConfig, healthCheck := NewTunneledRestConfig(ctx, hostClient, hostRESTConfig, dpuCluster)

	// Create clientset for Kamaji cluster
	kamajiClientset, err := kubernetes.NewForConfig(kamajiRESTConfig)
	Expect(err).NotTo(HaveOccurred(), "Should create Kamaji clientset")

	return kamajiClientset, healthCheck
}

// setupPortForward sets up port forwarding to the Kamaji cluster service
func setupPortForward(ctx context.Context, hostClient client.Client, hostRESTConfig *rest.Config, dpuCluster *provisioningv1.DPUCluster) *Tunnel {

	// Find the Kamaji cluster service and get the target port
	targetPort := getKamajiServicePort(ctx, hostClient, dpuCluster)

	// Find the Kamaji control plane pod.
	// TODO(tgiese): verify why service port-forwarding does not work here.
	pod := getKamajiControlPlanePod(ctx, hostClient, dpuCluster)

	// Create and start the port forwarder
	tunnel := createPortForwarder(hostRESTConfig, pod, targetPort)

	// Wait for the tunnel to be ready and get the local port
	tunnel.waitForTunnelReadyWithContext(ctx)

	ports, err := tunnel.pf.GetPorts()
	Expect(err).NotTo(HaveOccurred(), "Should get forwarded ports")
	Expect(ports).NotTo(BeEmpty(), "Should have at least one forwarded port")
	tunnel.localPort = int(ports[0].Local)

	return tunnel
}

// getKamajiServicePort finds the Kamaji cluster service and returns the kube-apiserver port
func getKamajiServicePort(ctx context.Context, hostClient client.Client, dpuCluster *provisioningv1.DPUCluster) int32 {
	// Find the Kamaji cluster service
	service := &corev1.Service{}
	serviceKey := types.NamespacedName{
		Namespace: dpuCluster.Namespace,
		Name:      dpuCluster.Name,
	}

	err := hostClient.Get(ctx, serviceKey, service)
	Expect(err).NotTo(HaveOccurred(), "Should get Kamaji service")

	// Find the kube-apiserver port
	var targetPort int32
	for _, port := range service.Spec.Ports {
		if port.Name == "kube-apiserver" {
			targetPort = port.Port
			break
		}
	}
	Expect(targetPort).NotTo(BeZero(), "kube-apiserver port should be found in service")

	return targetPort
}

// getKamajiControlPlanePod finds the Kamaji control plane pod
func getKamajiControlPlanePod(ctx context.Context, hostClient client.Client, dpuCluster *provisioningv1.DPUCluster) *corev1.Pod {
	// Find the Kamaji control plane pod
	podList := &corev1.PodList{}
	err := hostClient.List(ctx, podList, client.InNamespace(dpuCluster.Namespace), client.MatchingLabels(map[string]string{
		"kamaji.clastix.io/name": dpuCluster.Name,
	}))
	Expect(err).NotTo(HaveOccurred(), "Should list Kamaji pods")
	Expect(podList.Items).NotTo(BeEmpty(), "Should find at least one Kamaji control plane pod")

	// Use the first pod (there should be only one control plane pod)
	return &podList.Items[0]
}

// createPortForwarder creates and starts the port forwarder
func createPortForwarder(hostRESTConfig *rest.Config, pod *corev1.Pod, targetPort int32) *Tunnel {
	// Create a clientset for REST client operations
	hostClientset, err := kubernetes.NewForConfig(hostRESTConfig)
	Expect(err).NotTo(HaveOccurred(), "Should create host clientset")

	// Create port forward request
	req := hostClientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(pod.Name).
		Namespace(pod.Namespace).
		SubResource("portforward")

	// Create SPDY dialer
	transport, upgrader, err := spdy.RoundTripperFor(hostRESTConfig)
	Expect(err).NotTo(HaveOccurred(), "Should create SPDY transport")

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, "POST", req.URL())

	// Create tunnel channels
	stopCh := make(chan struct{})
	readyCh := make(chan struct{})
	errCh := make(chan error, 1)

	// Start port forwarding with a random local port
	portString := fmt.Sprintf("0:%d", targetPort)
	pf, err := portforward.New(dialer, []string{portString}, stopCh, readyCh, os.Stdout, os.Stderr)
	Expect(err).NotTo(HaveOccurred(), "Should create port forward")

	tunnel := &Tunnel{
		stopCh: stopCh,
		pf:     pf,
		errCh:  errCh,
	}

	// Start port forwarding in a goroutine
	go func() {
		defer tunnel.Close()
		// Forward the error from port forwarding to the error channel
		if err := pf.ForwardPorts(); err != nil {
			select {
			case errCh <- err:
			case <-stopCh:
				errCh <- fmt.Errorf("port forwarding stopped: %w", err)
			}
		}
	}()

	return tunnel
}

// waitForTunnelReadyWithContext waits for the tunnel to be ready with context support.
// Also waits for the port forwarding to be established and the local port to be assigned.
func (t *Tunnel) waitForTunnelReadyWithContext(ctx context.Context) bool {
	Eventually(func(g Gomega) int {
		// Check if the tunnel is stopped, context is done or an error occurred.
		// If any of these conditions are met, close the tunnel and return 0.
		select {
		case <-t.stopCh:
			t.Close()
			StopTrying("Tunnel was stopped before it could be established").Now()
			return 0
		case <-ctx.Done():
			t.Close()
			StopTrying("Context was canceled before tunnel could be established").Now()
			return 0
		case err := <-t.errCh:
			t.Close()
			Expect(err).NotTo(HaveOccurred(), "Port forwarding should not have errors")
			return 0
		default:
		}

		// Get the actual local port that was assigned
		ports, err := t.pf.GetPorts()
		g.Expect(err).NotTo(HaveOccurred(), "Failed to get tunnel ports")
		if len(ports) == 0 {
			return 0
		}
		return int(ports[0].Local)
	}, 30*time.Second, 100*time.Millisecond).Should(Not(BeZero()),
		"Port forwarding should be established and return a valid local port")

	return true
}
