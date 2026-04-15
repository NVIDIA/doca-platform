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
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/dpucluster"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Tunnel represents a port forwarding tunnel to a Kamaji cluster.
type Tunnel struct {
	pf        *portforward.PortForwarder
	stopCh    chan struct{}
	errCh     chan error
	localPort int
	closeOnce sync.Once
}

// Close stops the tunnel. Safe to call multiple times.
// Only closes stopCh and the port forwarder. The errCh is left open
// to avoid racing with the goroutine that may still send on it.
func (t *Tunnel) Close() {
	t.closeOnce.Do(func() {
		if t.pf != nil {
			t.pf.Close()
		}
		close(t.stopCh)
	})
}

// LocalPort returns the local port being used for the tunnel.
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

// NewTunneledRestConfig creates a tunneled REST config for accessing a DPU cluster.
// Returns the REST config, the tunnel (caller must close when done), and any error.
func NewTunneledRestConfig(ctx context.Context, hostClient client.Client, hostRESTConfig *rest.Config, dpuCluster *provisioningv1.DPUCluster) (*rest.Config, *Tunnel, error) {
	// Create dpucluster.Config to handle kubeconfig retrieval
	clusterConfig := dpucluster.NewConfig(hostClient, dpuCluster)

	// Get the Kamaji cluster kubeconfig for authentication
	kamajiKubeconfig, err := clusterConfig.Kubeconfig(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("get Kamaji kubeconfig: %w", err)
	}

	// Create REST config from Kamaji kubeconfig
	kamajiRESTConfig, err := clientcmd.NewDefaultClientConfig(*kamajiKubeconfig, &clientcmd.ConfigOverrides{}).ClientConfig()
	if err != nil {
		return nil, nil, fmt.Errorf("create Kamaji REST config: %w", err)
	}

	// Set up port forwarding to the Kamaji cluster
	tun, err := setupPortForward(ctx, hostClient, hostRESTConfig, dpuCluster)
	if err != nil {
		return nil, nil, fmt.Errorf("set up port forward: %w", err)
	}

	// Update the host to use local port
	kamajiRESTConfig.Host = fmt.Sprintf("https://localhost:%d", tun.LocalPort())

	return kamajiRESTConfig, tun, nil
}

// setupPortForward sets up port forwarding to the Kamaji cluster service
func setupPortForward(ctx context.Context, hostClient client.Client, hostRESTConfig *rest.Config, dpuCluster *provisioningv1.DPUCluster) (*Tunnel, error) {
	// Find the Kamaji cluster service and get the target port
	targetPort, err := getKamajiServicePort(ctx, hostClient, dpuCluster)
	if err != nil {
		return nil, err
	}

	// Find the Kamaji control plane pod.
	// TODO(tgiese): verify why service port-forwarding does not work here.
	pod, err := getKamajiControlPlanePod(ctx, hostClient, dpuCluster)
	if err != nil {
		return nil, err
	}

	// Create and start the port forwarder
	tun, err := createPortForwarder(hostRESTConfig, pod, targetPort)
	if err != nil {
		return nil, err
	}

	// Wait for the tunnel to be ready and get the local port
	if err := tun.waitForReady(ctx); err != nil {
		tun.Close()
		return nil, err
	}

	return tun, nil
}

// getKamajiServicePort finds the Kamaji cluster service and returns the kube-apiserver port
func getKamajiServicePort(ctx context.Context, hostClient client.Client, dpuCluster *provisioningv1.DPUCluster) (int32, error) {
	service := &corev1.Service{}
	serviceKey := client.ObjectKey{
		Namespace: dpuCluster.Namespace,
		Name:      dpuCluster.Name,
	}

	if err := hostClient.Get(ctx, serviceKey, service); err != nil {
		return 0, fmt.Errorf("get Kamaji service: %w", err)
	}

	for _, port := range service.Spec.Ports {
		if port.Name == "kube-apiserver" {
			return port.Port, nil
		}
	}
	return 0, fmt.Errorf("kube-apiserver port not found in service %s/%s", dpuCluster.Namespace, dpuCluster.Name)
}

// getKamajiControlPlanePod finds the Kamaji control plane pod
func getKamajiControlPlanePod(ctx context.Context, hostClient client.Client, dpuCluster *provisioningv1.DPUCluster) (*corev1.Pod, error) {
	podList := &corev1.PodList{}
	err := hostClient.List(ctx, podList,
		client.InNamespace(dpuCluster.Namespace),
		client.MatchingLabels{"kamaji.clastix.io/name": dpuCluster.Name},
	)
	if err != nil {
		return nil, fmt.Errorf("list Kamaji pods: %w", err)
	}
	if len(podList.Items) == 0 {
		return nil, fmt.Errorf("no Kamaji control plane pods found for DPUCluster %s/%s", dpuCluster.Namespace, dpuCluster.Name)
	}

	return &podList.Items[0], nil
}

// createPortForwarder creates and starts the port forwarder
func createPortForwarder(hostRESTConfig *rest.Config, pod *corev1.Pod, targetPort int32) (*Tunnel, error) {
	// Create a clientset for REST client operations
	hostClientset, err := kubernetes.NewForConfig(hostRESTConfig)
	if err != nil {
		return nil, fmt.Errorf("create host clientset: %w", err)
	}

	// Create port forward request
	req := hostClientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(pod.Name).
		Namespace(pod.Namespace).
		SubResource("portforward")

	// Create SPDY dialer
	transport, upgrader, err := spdy.RoundTripperFor(hostRESTConfig)
	if err != nil {
		return nil, fmt.Errorf("create SPDY transport: %w", err)
	}

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, "POST", req.URL())

	// Create tunnel channels
	stopCh := make(chan struct{})
	readyCh := make(chan struct{})
	errCh := make(chan error, 1)

	// Start port forwarding with a random local port
	portString := fmt.Sprintf("0:%d", targetPort)
	pf, err := portforward.New(dialer, []string{portString}, stopCh, readyCh, io.Discard, os.Stderr)
	if err != nil {
		close(stopCh)
		return nil, fmt.Errorf("create port forward: %w", err)
	}

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
			default:
			}
		}
	}()

	return tunnel, nil
}

// waitForReady waits for the tunnel to be ready.
// Also waits for the port forwarding to be established and the local port to be assigned.
func (t *Tunnel) waitForReady(ctx context.Context) error {
	return wait.PollUntilContextTimeout(ctx, 100*time.Millisecond, 30*time.Second, true, func(ctx context.Context) (bool, error) {
		select {
		case <-t.stopCh:
			return false, fmt.Errorf("tunnel stopped before it could be established")
		case err := <-t.errCh:
			return false, fmt.Errorf("port forwarding error: %w", err)
		default:
		}

		// Get the actual local port that was assigned
		ports, err := t.pf.GetPorts()
		if err != nil {
			return false, nil
		}
		if len(ports) == 0 || ports[0].Local == 0 {
			return false, nil
		}

		t.localPort = int(ports[0].Local)
		return true, nil
	})
}
