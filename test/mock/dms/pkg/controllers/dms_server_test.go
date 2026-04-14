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

package controllers

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	operatorcontroller "github.com/nvidia/doca-platform/internal/operator/controllers"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/pkg/dpucluster"
	"github.com/nvidia/doca-platform/test/mock/dms/pkg/certs"
	"github.com/nvidia/doca-platform/test/utils"

	. "github.com/onsi/gomega"
	"google.golang.org/grpc"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	bfbName        string = "bfb-1"
	dpuClusterName string = "dpu-cluster-1"
	dpuFlavorName  string = "dpu-flavor-1"
)

func TestDMSServerReconciler(t *testing.T) {
	g := NewWithT(t)
	g.Expect(testClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace}})).To(Succeed())

	// 1) Create a DPUSet to create DPUs. DPUs will be created for matching DPUNodes.
	// DPUNodes are created in the mock-dms controller based on corev1.Node objects
	// with the correct label. This mocks the behavior of mock-dms.
	dpuSet := &provisioningv1.DPUSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dpuset-one",
			Namespace: dpfOperatorSystemNamespace,
		},
		Spec: provisioningv1.DPUSetSpec{
			Strategy: provisioningv1.DPUSetStrategy{
				// TODO: Update to OnDelete when this is implemented
				Type: provisioningv1.RollingUpdateStrategyType,
				RollingUpdate: &provisioningv1.RollingUpdateDPU{
					MaxUnavailable: &intstr.IntOrString{Type: intstr.Int, IntVal: 1},
				},
			},
			DPUTemplate: provisioningv1.DPUTemplate{
				Spec: provisioningv1.DPUTemplateSpec{
					BFB: provisioningv1.BFBReference{
						Name: bfbName,
					},
					DPUFlavor: dpuFlavorName,
					NodeEffect: provisioningv1.NodeEffect{
						Action: provisioningv1.Action{
							NoEffect: ptr.To(true),
						},
					},
				},
			},
		},
	}
	g.Expect(testClient.Create(ctx, dpuSet)).To(Succeed())

	// 2) Initializing phase requires a node with the DPUOOBBridgeConfiguredLabel exists.
	config := &operatorv1.DPFOperatorConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "config",
			Namespace: dpfOperatorSystemNamespace,
		},
		Spec: operatorv1.DPFOperatorConfigSpec{
			ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{
				BFBPersistentVolumeClaimName: ptr.To("name"),
				InstallInterface: &operatorv1.ProvisioningInstallInterface{
					InstallViaGNOI: &operatorv1.InstallViaGNOI{},
				},
			},
		},
	}
	g.Expect(testClient.Create(ctx, config)).To(Succeed())

	// 3) Pending phase requires that the BFB exist and is in Phase "BFBReady".
	// The BFB file path must exist
	_, err := os.Create(filepath.Join(cutil.BFBBaseDir, bfbName))
	g.Expect(err).NotTo(HaveOccurred())
	bfb := &provisioningv1.BFB{
		ObjectMeta: metav1.ObjectMeta{
			Name:      bfbName,
			Namespace: dpfOperatorSystemNamespace,
		},
		Spec: provisioningv1.BFBSpec{
			URL: "http://BlueField/BFBs/bf-bundle-dummy-8KB.bfb",
		},
	}
	g.Expect(testClient.Create(ctx, bfb)).To(Succeed())
	bfb.Status.Phase = provisioningv1.BFBReady
	bfb.Status.FileName = bfbName
	g.Expect(testClient.Status().Update(ctx, bfb)).To(Succeed())

	// 4) DPUInitializeInterface phase DMS Deploy checks that the DMS pod is "Running" and has a PodIP.
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dmsServerPodName,
			Namespace: operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "bfb",
					Image: "nvidia/cuda/bundle:1.0",
				},
			},
		},
	}
	g.Expect(testClient.Create(ctx, pod)).To(Succeed())
	pod.Status.Phase = corev1.PodRunning
	// This IP will is used by the DPU controller to talk to DMS.
	pod.Status.PodIP = "127.0.0.1"

	// 4) DPUInitializeInterface OS Installing Phase requires the provisioning client Secret.
	// This allows communication with the DPF server.
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			// This name is hard-coded in the provisioning controller
			Name:      "dpf-provisioning-client-secret",
			Namespace: dpfOperatorSystemNamespace,
		},
		Data: map[string][]byte{
			"tls.crt": certs.EncodeCertPEM(cert),
			"tls.key": certs.EncodePrivateKeyPEM(key),
			"ca.crt":  certs.EncodeCertPEM(cert),
		},
	}
	g.Expect(testClient.Create(ctx, secret)).To(Succeed())

	// The OS Installing phase requires the dpuFlavor.
	dpuFlavor := &provisioningv1.DPUFlavor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dpuFlavorName,
			Namespace: dpfOperatorSystemNamespace,
		},
	}
	g.Expect(testClient.Create(ctx, dpuFlavor)).To(Succeed())

	// The OS Installing phase requires the dpuCluster.
	dpuCluster := &provisioningv1.DPUCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dpuClusterName,
			Namespace: dpfOperatorSystemNamespace,
		},
		Spec: provisioningv1.DPUClusterSpec{
			Type:       "static",
			Kubeconfig: fmt.Sprintf("%v-admin-kubeconfig", dpuClusterName),
		},
	}
	g.Expect(testClient.Create(ctx, dpuCluster)).To(Succeed())
	dpuCluster.Status.Phase = provisioningv1.PhaseReady
	g.Expect(testClient.Status().Update(ctx, dpuCluster)).To(Succeed())

	// For this test the DPUCluster points to the envtest kubeconfig.
	kubeconfigSecret, err := utils.GetFakeKamajiClusterSecretFromEnvtest(*dpuCluster, cfg)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(testClient.Create(ctx, kubeconfigSecret)).To(Succeed())

	// CLOUD_INIT_TIMEOUT is used in the dmsHandler in installing.go. It's set here to reduce waiting time during the
	// test.
	g.Expect(os.Setenv("CLOUD_INIT_TIMEOUT", "1")).To(Succeed())

	// 5) Rebooting phase is handled by using a stub HostUptimeChecker which simulates a reboot.

	// 6) The DPUInitializeInterface HostNetworkingConfig phase checks that the first container in the pod has a Ready ContainerStatus.
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "Ready", Ready: true}}
	g.Expect(testClient.Status().Update(ctx, pod)).To(Succeed())

	// 7) The DPUClusterConfig phase requires the DPU node object be created and ready.
	// This is handled in the controller code.
	tests := []struct {
		name          string
		numberOfNodes int
	}{
		{
			name:          "Provision many Nodes until the DPUs become ready",
			numberOfNodes: 30,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create the DPU and check its provisioning process.
			for _, node := range createNodes(tt.numberOfNodes) {
				g.Expect(testClient.Create(ctx, node)).To(Succeed())
			}
			// Construct the client for the DPU cluster.
			cluster, err := dpucluster.GetConfigs(ctx, testClient)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(cluster).To(HaveLen(1))
			dpuClient, err := cluster[0].Client(ctx)
			g.Expect(err).NotTo(HaveOccurred())

			g.Eventually(func(g Gomega) {
				// Check a node has been created for each DPU.
				got := &provisioningv1.DPUList{}
				g.Expect(testClient.List(ctx, got)).To(Succeed())
				g.Expect(got.Items).To(HaveLen(tt.numberOfNodes))
				for _, dpu := range got.Items {
					n := &corev1.Node{}
					g.Expect(testClient.Get(ctx, client.ObjectKey{Name: dpu.Spec.DPUNodeName}, n)).To(Succeed())
					// mock-dms relies on an external component to put the Node in a Ready state.
					g.Expect(dpu.Status.Phase).To(Equal(provisioningv1.DPUClusterConfig))
				}

				// Ensure Nodes have been created for each DPU.
				nodeList := &corev1.NodeList{}
				g.Expect(dpuClient.List(ctx, nodeList, client.HasLabels{fakeNodeLabel})).To(Succeed())
				g.Expect(nodeList.Items).To(HaveLen(tt.numberOfNodes))
			}).WithTimeout(100 * time.Second).Should(Succeed())

			g.Eventually(func(g Gomega) {
				nodeList := &corev1.NodeList{}
				// Delete the DPUs and check the nodes have been cleaned up.
				g.Expect(testClient.DeleteAllOf(ctx, &provisioningv1.DPU{}, client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())
				g.Expect(dpuClient.List(ctx, nodeList, client.HasLabels{fakeNodeLabel})).To(Succeed())
				g.Expect(nodeList.Items).To(BeEmpty())
			}).WithTimeout(100 * time.Second).Should(Succeed())
		})
	}
}

func createNodes(n int) []*corev1.Node {
	nodes := []*corev1.Node{}
	for i := range n {
		name := fmt.Sprintf("target-cluster-%d", i)
		nodes = append(nodes, &corev1.Node{
			TypeMeta: metav1.TypeMeta{},
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
				Labels: map[string]string{
					// These labels are added by node feature discovery in a full setup.
					"feature.node.kubernetes.io/dpu-deviceID":              "0xa2d6",
					"feature.node.kubernetes.io/dpu-enabled":               "true",
					"feature.node.kubernetes.io/dpu-oob-bridge-configured": "true",
					cutil.DPUDevicePCIAddressLabel:                         "0000-08-00",
				},
				Annotations: map[string]string{
					cutil.OverrideDMSPodNameAnnotationKey: dmsServerPodName,
				},
			},
			Status: corev1.NodeStatus{
				Addresses: []corev1.NodeAddress{
					{
						Type:    corev1.NodeInternalIP,
						Address: "127.0.0.1",
					},
				},
			},
		})
	}
	return nodes
}

// newCert creates a CA certificate.
func newCert(key *rsa.PrivateKey) (*x509.Certificate, error) {
	now := time.Now().UTC()

	tmpl := x509.Certificate{
		SerialNumber: new(big.Int).SetInt64(0),
		Subject: pkix.Name{
			CommonName:   "dms-server",
			Organization: []string{"doca-platform"},
		},
		IPAddresses:           []net.IP{{127, 0, 0, 1}},
		NotBefore:             now.Add(time.Minute * -5),
		NotAfter:              now.Add(time.Hour * 24 * 365 * 10), // 10 years
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		MaxPathLenZero:        true,
		BasicConstraintsValid: true,
		MaxPathLen:            0,
		IsCA:                  true,
	}

	b, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, key.Public(), key)
	if err != nil {
		return nil, fmt.Errorf("failed to create self signed CA certificate")
	}

	c, err := x509.ParseCertificate(b)
	return c, err
}

// mockKubeadmJoinCommandGenerator implements the interface for generating a node join command for the Node.
// The implementation is purely a stub as this command it outputs is never run.
type mockKubeadmJoinCommandGenerator struct{}

func (m *mockKubeadmJoinCommandGenerator) GenerateJoinCommand(context.Context, *provisioningv1.DPUCluster) (string, error) {
	return "soup", nil
}

// mockHostUptimeReporter implements the interface for checking if a node reboot has occurred..
// The implementation returns 0 to speed up testing.
type mockHostUptimeReporter struct{}

func (m mockHostUptimeReporter) HostUptime(ctx context.Context, conn *grpc.ClientConn) (int, error) {
	return 0, nil
}
