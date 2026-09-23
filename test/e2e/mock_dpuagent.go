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
	"crypto/rand"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	rfclient "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/client"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	providentity "github.com/nvidia/doca-platform/internal/provisioning/utils/certificate/identity"
	mockconfig "github.com/nvidia/doca-platform/test/mock/dpuagent/pkg/config"
	mocknode "github.com/nvidia/doca-platform/test/mock/dpuagent/pkg/node"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	certificatesv1 "k8s.io/api/certificates/v1"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

// The mock-dpuagent suite provisions simulated BlueField DPUs (test/mock/dpuagent) through the
// real zero-trust control plane inside a kind cluster. Every mock pod is one DPU: its pod IP is
// the BMC address DPUDiscovery finds. The DPU cluster is a second kind cluster on the same docker
// network, registered as a static DPUCluster with a kubeconfig that uses its node IP.
//
// The mock Deployment and its ConfigMap are installed by `make test-e2e-mock-dpuagent` before
// the suite runs; the suite rewrites the ConfigMap and restarts the pods to switch between the
// BF3 / BF4 personalities and the reboot scenarios.
const (
	mockDPUAgentDeployment = "mock-dpuagent"
	mockDPUAgentConfigMap  = "mock-dpuagent-config"
	mockDPUAgentConfigKey  = "config.yaml"
	mockDPUAgentPodLabel   = "app.kubernetes.io/component"
	mockDPUAgentPodValue   = "dpu"

	// mockBFBServer serves the trimmed BFB / PLDM / ISO fixtures inside the cluster so the BFB and
	// BlueFieldSoftware controllers and the mock BMCs download real bytes over HTTP.
	mockBFBServer          = "mock-bfb-server"
	mockBFBFixtureBFB      = "bf3-trimmed.bfb"
	mockBFBFixturePLDM     = "bf4-trimmed.fwpkg"
	mockBFBFixtureISO      = "bf4-os-doca-bundle-3.3.0-420_mock.iso"
	mockBFBFixtureISOBytes = 64 << 10
	mockFixtureDir         = "../mock/dpuagent/testdata"

	// mockDPUClusterKubeconfigEnv names the kubeconfig of the kind cluster that plays the DPU
	// cluster (created by make test-e2e-mock-dpuagent). It must be a separate cluster: with a shared
	// kube-system the node-join and dpu-agent bootstrap tokens of a DPU collide.
	mockDPUClusterKubeconfigEnv = "MOCK_DPU_CLUSTER_KUBECONFIG"

	// mockSkipSecureBootEnv set to "true" leaves spec.secureBoot unset on the DPUSet, skipping the
	// Perform ARM Force Restart phase (two Arm restarts at least 90 s apart per DPU). By default
	// the suite enables Secure Boot so that phase is covered (make test-e2e-mock-dpuagent
	// MOCK_DPUAGENT_SKIP_SECURE_BOOT=true to skip).
	mockSkipSecureBootEnv = "MOCK_DPU_SKIP_SECURE_BOOT"

	mockDPUClusterName       = "dpu-cluster-1"
	mockDPUClusterKubeconfig = "dpu-cluster-1-admin-kubeconfig"
	mockDPUFlavorName        = "mock-dpuagent"
	mockBFBName              = "bf-bundle"
	mockBlueFieldSoftware    = "bf4"
	mockDPUSetName           = "dpuset-1"
	mockDPUDiscoveryName     = "dpu-discovery"
	mockBMCPassword          = "mock-dpuagent-bmc-password"

	mockProvisioningTimeout = 30 * time.Minute
	mockDeprovisionTimeout  = 15 * time.Minute
	mockPollInterval        = time.Second
)

// mockDPUConfig is the part of the mock-dpuagent configuration file the suite varies per case.
type mockDPUConfig struct {
	dpuType      mockconfig.DPUType
	bmcVersion   string
	rebootMethod string
}

func (c mockDPUConfig) yaml() string {
	bmcVersion := c.bmcVersion
	if bmcVersion == "" {
		bmcVersion = mockconfig.BMCMinSupportedVersion
	}
	rebootMethod := c.rebootMethod
	if rebootMethod == "" {
		rebootMethod = mockconfig.DefaultRebootMethod
	}
	cfg := map[string]interface{}{
		"dpuType": string(c.dpuType),
		"bmc": map[string]interface{}{
			"port": mockconfig.DefaultBMCPort,
			"psid": mockconfig.DefaultPSID,
			"firmware": map[string]interface{}{
				"bmc":  bmcVersion,
				"erot": mockconfig.PlaceholderVersion,
				"uefi": mockconfig.PlaceholderVersion,
				"nic":  mockconfig.PlaceholderVersion,
			},
		},
		"agent": map[string]interface{}{
			"rebootMethod":   rebootMethod,
			"dpuClusterJoin": string(mockconfig.JoinSimulated),
		},
	}
	out, err := yaml.Marshal(cfg)
	Expect(err).NotTo(HaveOccurred())
	return string(out)
}

// mockSecureBootEnabled reports whether the DPUSet asks for UEFI Secure Boot, which routes every DPU
// through the Perform ARM Force Restart phase.
func mockSecureBootEnabled() bool {
	return os.Getenv(mockSkipSecureBootEnv) != "true"
}

// mockDPUEnv is the state the suite shares between cases.
type mockDPUEnv struct {
	client        client.Client
	namespace     string
	replicas      int
	bfbServerURL  string
	apiServerVIP  string
	apiServerPort int

	// dpuClusterClient talks to the separate kind cluster the mock DPUs join (see
	// MOCK_DPU_CLUSTER_KUBECONFIG); dpuClusterRESTConfig is what its kubeconfig resolved to.
	dpuClusterClient     client.Client
	dpuClusterRESTConfig *rest.Config
}

// expectedMockAgentConditions are the agent conditions a mock DPU reports after a full run: the 28
// dpu-agent operations minus the two that are skipped in zero-trust mode without Astra (BridgeChecked,
// NICProvisioning), plus the device-query reboot discovery condition.
var expectedMockAgentConditions = []string{
	"KernelModuleLoaded", "NetworkConfigured", "NetworkChecked", "LastStartupTimeReported", "DPURetrieved",
	"DNSConfigured", "StaticFilesVerified", "PackagesInstalled", "SystemdServicesManaged", "BuiltinKubeletRemoved",
	"SysctlParametersSet", "SysctlParametersChecked", "KernelCmdLineConfigured", "ContainerdConfigured",
	"DpuModeEnsured", "NVConfigApplied", "RebootHandled", "KernelCmdLineChecked", "SFCreated", "VFMacSet",
	"OVSScriptRun", "UnderlayNetplanMTUConfigured", "KubeletConfigured", "KubeletStarted", "NodeLabelsReported",
	"ReleaseHostOSInit", cutil.AgentCondRebootMethodDiscovery,
}

// setupMockDPUEnvironment prepares everything that does not change between cases: the operator
// config in zero-trust mode, the BMC password Secret, the fixture file server, the static
// DPUCluster pointing at the kind cluster and the DPUFlavor.
func setupMockDPUEnvironment(ctx context.Context, c client.Client) *mockDPUEnv {
	env := &mockDPUEnv{client: c, namespace: dpfOperatorSystemNamespace}

	By("Resolving the kind API server address the mock DPUs must use")
	env.apiServerVIP, env.apiServerPort = kindAPIServerEndpoint(ctx, c)

	By("Reading the mock-dpuagent Deployment installed by make test-e2e-mock-dpuagent")
	deployment := &appsv1.Deployment{}
	Expect(c.Get(ctx, types.NamespacedName{Namespace: env.namespace, Name: mockDPUAgentDeployment}, deployment)).To(Succeed(),
		"mock-dpuagent must be deployed before the suite runs (make test-e2e-mock-dpuagent does it)")
	env.replicas = int(ptr.Deref(deployment.Spec.Replicas, 1))
	Expect(env.replicas).To(BeNumerically(">", 0))

	By("Creating the shared BMC password Secret")
	bmcSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: rfclient.BMCPasswordSecret, Namespace: env.namespace, Labels: CleanupScope.Suite},
		StringData: map[string]string{rfclient.BMCSharedPasswordKey: mockBMCPassword},
	}
	Expect(client.IgnoreAlreadyExists(c.Create(ctx, bmcSecret))).To(Succeed())

	By("Deploying the DPF operator config in zero-trust mode with only provisioning enabled")
	if input.config.Spec.Overrides == nil {
		input.config.Spec.Overrides = &operatorv1.Overrides{}
	}
	input.config.Spec.Overrides.KubernetesAPIServerVIP = ptr.To(env.apiServerVIP)
	input.config.Spec.Overrides.KubernetesAPIServerPort = ptr.To(env.apiServerPort)
	input.config.SetLabels(CleanupScope.Suite)
	DeployDPFSystemComponents(ctx, DeployDPFSystemComponentsInput{
		systemNamespace:               env.namespace,
		operatorConfig:                input.config,
		ImagePullSecrets:              input.pullSecretNames,
		ProvisioningControllerPVC:     input.pvc,
		client:                        c,
		numberOfDPUNodes:              env.replicas,
		skipSystemComponentValidation: true,
	})
	waitForBFBRegistry(ctx, c, env.namespace)
	VerifyDPFOperatorConfigReady(ctx, c, 15*time.Minute)

	By("Deploying the fixture file server")
	env.bfbServerURL = deployMockBFBServer(ctx, c, env.namespace)

	By("Connecting to the DPU kind cluster")
	connectMockDPUCluster(ctx, env)

	By("Creating the static DPUCluster for the DPU kind cluster")
	createMockDPUCluster(ctx, c, env)

	By("Creating the DPUFlavor")
	// The provisioning webhook Service may not accept connections yet right after the operator
	// config was deployed; retry like the system suite does.
	Eventually(func(g Gomega) {
		g.Expect(client.IgnoreAlreadyExists(c.Create(ctx, mockDPUFlavor(env.namespace)))).To(Succeed())
	}).WithTimeout(60 * time.Second).WithPolling(mockPollInterval).Should(Succeed())
	return env
}

// kindAPIServerEndpoint returns the address other containers use to reach a kind cluster's API
// server: the endpoint of its default/kubernetes Service, which is the control-plane node IP and
// port 6443. The kubeconfig's 127.0.0.1:<host port> is only reachable from the test host.
func kindAPIServerEndpoint(ctx context.Context, c client.Client) (string, int) {
	endpoints := &corev1.Endpoints{}
	Expect(c.Get(ctx, types.NamespacedName{Namespace: "default", Name: "kubernetes"}, endpoints)).To(Succeed())
	for _, subset := range endpoints.Subsets {
		for _, addr := range subset.Addresses {
			for _, port := range subset.Ports {
				if port.Name == "https" || len(subset.Ports) == 1 {
					return addr.IP, int(port.Port)
				}
			}
		}
	}
	Fail("default/kubernetes Endpoints has no https address")
	return "", 0
}

func waitForBFBRegistry(ctx context.Context, c client.Client, namespace string) {
	By("Waiting for the bfb-registry Service and a ready pod")
	Eventually(func(g Gomega) {
		svc := &corev1.Service{}
		g.Expect(c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: "bfb-registry"}, svc)).To(Succeed())
		g.Expect(svc.Spec.Ports).NotTo(BeEmpty())
		g.Expect(svc.Spec.Ports[0].NodePort).NotTo(BeZero())
		pods := &corev1.PodList{}
		g.Expect(c.List(ctx, pods, client.InNamespace(namespace), client.MatchingLabels{"dpu.nvidia.com/component": "bfb-registry"})).To(Succeed())
		ready := false
		for _, pod := range pods.Items {
			for _, cond := range pod.Status.Conditions {
				if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
					ready = true
				}
			}
		}
		g.Expect(ready).To(BeTrue(), "no ready bfb-registry pod")
	}).WithTimeout(5 * time.Minute).WithPolling(mockPollInterval).Should(Succeed())
}

// deployMockBFBServer serves the fixtures from a ConfigMap through busybox httpd and returns the
// in-cluster base URL.
func deployMockBFBServer(ctx context.Context, c client.Client, namespace string) string {
	bfb, err := os.ReadFile(filepath.Join(mockFixtureDir, mockBFBFixtureBFB))
	Expect(err).NotTo(HaveOccurred())
	pldm, err := os.ReadFile(filepath.Join(mockFixtureDir, mockBFBFixturePLDM))
	Expect(err).NotTo(HaveOccurred())
	iso := make([]byte, mockBFBFixtureISOBytes)
	_, err = rand.Read(iso)
	Expect(err).NotTo(HaveOccurred())

	labels := map[string]string{"app.kubernetes.io/name": mockBFBServer}
	for k, v := range CleanupScope.Suite {
		labels[k] = v
	}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: mockBFBServer, Namespace: namespace, Labels: labels},
		BinaryData: map[string][]byte{mockBFBFixtureBFB: bfb, mockBFBFixturePLDM: pldm, mockBFBFixtureISO: iso},
	}
	Expect(client.IgnoreAlreadyExists(c.Create(ctx, cm))).To(Succeed())

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: mockBFBServer, Namespace: namespace, Labels: labels},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To[int32](1),
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app.kubernetes.io/name": mockBFBServer}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					Tolerations: []corev1.Toleration{
						{Key: "node-role.kubernetes.io/control-plane", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoSchedule},
						{Key: "node-role.kubernetes.io/master", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoSchedule},
					},
					Containers: []corev1.Container{{
						Name:    "httpd",
						Image:   fmt.Sprintf("%s/busybox:latest", dockerIORegistry),
						Command: []string{"httpd", "-f", "-v", "-p", "8080", "-h", "/data"},
						Ports:   []corev1.ContainerPort{{Name: "http", ContainerPort: 8080}},
						VolumeMounts: []corev1.VolumeMount{{
							Name: "fixtures", MountPath: "/data", ReadOnly: true,
						}},
					}},
					Volumes: []corev1.Volume{{
						Name:         "fixtures",
						VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: mockBFBServer}}},
					}},
				},
			},
		},
	}
	Expect(client.IgnoreAlreadyExists(c.Create(ctx, deployment))).To(Succeed())
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: mockBFBServer, Namespace: namespace, Labels: labels},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app.kubernetes.io/name": mockBFBServer},
			Ports:    []corev1.ServicePort{{Name: "http", Port: 80, TargetPort: intstr.FromInt32(8080)}},
		},
	}
	Expect(client.IgnoreAlreadyExists(c.Create(ctx, svc))).To(Succeed())
	Eventually(func(g Gomega) {
		d := &appsv1.Deployment{}
		g.Expect(c.Get(ctx, client.ObjectKeyFromObject(deployment), d)).To(Succeed())
		g.Expect(d.Status.ReadyReplicas).To(Equal(int32(1)))
	}).WithTimeout(5 * time.Minute).WithPolling(mockPollInterval).Should(Succeed())
	return fmt.Sprintf("http://%s.%s.svc", mockBFBServer, namespace)
}

// connectMockDPUCluster loads the DPU kind cluster kubeconfig named by MOCK_DPU_CLUSTER_KUBECONFIG.
func connectMockDPUCluster(ctx context.Context, env *mockDPUEnv) {
	path := os.Getenv(mockDPUClusterKubeconfigEnv)
	Expect(path).NotTo(BeEmpty(), "%s must point at the kubeconfig of the DPU kind cluster (make test-e2e-mock-dpuagent sets it)", mockDPUClusterKubeconfigEnv)
	cfg, err := clientcmd.BuildConfigFromFlags("", path)
	Expect(err).NotTo(HaveOccurred())
	cl, err := client.New(cfg, client.Options{Scheme: testClient.Scheme()})
	Expect(err).NotTo(HaveOccurred())
	nodes := &corev1.NodeList{}
	Expect(cl.List(ctx, nodes)).To(Succeed(), "DPU kind cluster %s is not reachable", cfg.Host)
	env.dpuClusterClient = cl
	env.dpuClusterRESTConfig = cfg
}

// createMockDPUCluster registers the DPU kind cluster as a static DPUCluster. The kubeconfig in the
// Secret must be usable from inside the management cluster, so its server is the DPU cluster's
// node address (default/kubernetes Endpoints), not the 127.0.0.1 port mapping of the test host.
func createMockDPUCluster(ctx context.Context, c client.Client, env *mockDPUEnv) {
	cfg := env.dpuClusterRESTConfig
	caData := cfg.CAData
	if len(caData) == 0 && cfg.CAFile != "" {
		data, err := os.ReadFile(cfg.CAFile)
		Expect(err).NotTo(HaveOccurred())
		caData = data
	}
	Expect(caData).NotTo(BeEmpty(), "DPU cluster kubeconfig must carry the cluster CA")
	authInfo := &clientcmdapi.AuthInfo{
		ClientCertificateData: cfg.CertData,
		ClientKeyData:         cfg.KeyData,
		Token:                 cfg.BearerToken,
	}
	if len(authInfo.ClientCertificateData) == 0 && cfg.CertFile != "" {
		cert, err := os.ReadFile(cfg.CertFile)
		Expect(err).NotTo(HaveOccurred())
		key, err := os.ReadFile(cfg.KeyFile)
		Expect(err).NotTo(HaveOccurred())
		authInfo.ClientCertificateData, authInfo.ClientKeyData = cert, key
	}
	ip, port := kindAPIServerEndpoint(ctx, env.dpuClusterClient)
	kubeconfig := clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{mockDPUClusterName: {
			Server:                   fmt.Sprintf("https://%s:%d", ip, port),
			CertificateAuthorityData: caData,
		}},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{"admin": authInfo},
		Contexts: map[string]*clientcmdapi.Context{"default": {
			Cluster: mockDPUClusterName, AuthInfo: "admin",
		}},
		CurrentContext: "default",
	}
	data, err := clientcmd.Write(kubeconfig)
	Expect(err).NotTo(HaveOccurred())
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: mockDPUClusterKubeconfig, Namespace: env.namespace, Labels: CleanupScope.Suite},
		Data:       map[string][]byte{"super-admin.conf": data},
	}
	Expect(client.IgnoreAlreadyExists(c.Create(ctx, secret))).To(Succeed())

	dpuCluster := &provisioningv1.DPUCluster{
		ObjectMeta: metav1.ObjectMeta{Name: mockDPUClusterName, Namespace: env.namespace, Labels: CleanupScope.Suite},
		Spec: provisioningv1.DPUClusterSpec{
			Type:       string(provisioningv1.StaticCluster),
			MaxNodes:   1000,
			Kubeconfig: mockDPUClusterKubeconfig,
		},
	}
	Expect(client.IgnoreAlreadyExists(c.Create(ctx, dpuCluster))).To(Succeed())
	Eventually(func(g Gomega) {
		g.Expect(c.Get(ctx, client.ObjectKeyFromObject(dpuCluster), dpuCluster)).To(Succeed())
		g.Expect(dpuCluster.Status.Phase).To(Equal(provisioningv1.PhaseReady))
	}).WithTimeout(5 * time.Minute).WithPolling(mockPollInterval).Should(Succeed())
}

// mockDPUFlavor is a flavor that makes every agent operation run: kernel parameters, a package, a
// systemd service and an OVS script are what the real operations gate on.
func mockDPUFlavor(namespace string) *provisioningv1.DPUFlavor {
	return &provisioningv1.DPUFlavor{
		ObjectMeta: metav1.ObjectMeta{Name: mockDPUFlavorName, Namespace: namespace, Labels: CleanupScope.Suite},
		Spec: provisioningv1.DPUFlavorSpec{
			Grub:            provisioningv1.DPUFlavorGrub{KernelParameters: []string{"console=ttyAMA0", "hugepages=1024"}},
			BFCfgParameters: []string{"UPDATE_ATF_UEFI=yes", "UPDATE_DPU_OS=yes"},
			Packages:        []provisioningv1.PackageSpec{{Name: "tree"}},
			SystemdServices: []provisioningv1.SystemdServiceSpec{{Name: "chrony", Operation: provisioningv1.SystemdServiceEnableAndStart}},
			OVS:             provisioningv1.DPUFlavorOVS{RawConfigScript: "ovs-vsctl set Open_vSwitch . other_config:hw-offload=true\n"},
		},
	}
}

// reconfigureMockDPUs rewrites the mock ConfigMap, restarts the mock pods and returns their IPs.
func reconfigureMockDPUs(ctx context.Context, env *mockDPUEnv, cfg mockDPUConfig) []net.IP {
	By(fmt.Sprintf("Reconfiguring %d mock DPUs: dpuType=%s bmcVersion=%q rebootMethod=%q", env.replicas, cfg.dpuType, cfg.bmcVersion, cfg.rebootMethod))
	cm := &corev1.ConfigMap{}
	Expect(env.client.Get(ctx, types.NamespacedName{Namespace: env.namespace, Name: mockDPUAgentConfigMap}, cm)).To(Succeed())
	patch := client.MergeFrom(cm.DeepCopy())
	if cm.Data == nil {
		cm.Data = map[string]string{}
	}
	cm.Data[mockDPUAgentConfigKey] = cfg.yaml()
	Expect(env.client.Patch(ctx, cm, patch)).To(Succeed())

	deployment := &appsv1.Deployment{}
	Expect(env.client.Get(ctx, types.NamespacedName{Namespace: env.namespace, Name: mockDPUAgentDeployment}, deployment)).To(Succeed())
	restartedAt := time.Now().UTC().Format(time.RFC3339Nano)
	dPatch := client.MergeFrom(deployment.DeepCopy())
	if deployment.Spec.Template.Annotations == nil {
		deployment.Spec.Template.Annotations = map[string]string{}
	}
	deployment.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = restartedAt
	Expect(env.client.Patch(ctx, deployment, dPatch)).To(Succeed())

	var ips []net.IP
	Eventually(func(g Gomega) {
		d := &appsv1.Deployment{}
		g.Expect(env.client.Get(ctx, client.ObjectKeyFromObject(deployment), d)).To(Succeed())
		g.Expect(d.Status.ObservedGeneration).To(BeNumerically(">=", d.Generation))
		g.Expect(d.Status.UpdatedReplicas).To(Equal(int32(env.replicas)))
		g.Expect(d.Status.ReadyReplicas).To(Equal(int32(env.replicas)))
		g.Expect(d.Status.Replicas).To(Equal(int32(env.replicas)), "old mock pods are still terminating")
		pods := &corev1.PodList{}
		g.Expect(env.client.List(ctx, pods, client.InNamespace(env.namespace), client.MatchingLabels{mockDPUAgentPodLabel: mockDPUAgentPodValue})).To(Succeed())
		ips = ips[:0]
		for _, pod := range pods.Items {
			if pod.DeletionTimestamp != nil || pod.Annotations["kubectl.kubernetes.io/restartedAt"] != restartedAt {
				continue
			}
			g.Expect(pod.Status.PodIP).NotTo(BeEmpty())
			ips = append(ips, net.ParseIP(pod.Status.PodIP))
		}
		g.Expect(ips).To(HaveLen(env.replicas))
	}).WithTimeout(5 * time.Minute).WithPolling(mockPollInterval).Should(Succeed())
	sort.Slice(ips, func(i, j int) bool { return ipToUint32(ips[i]) < ipToUint32(ips[j]) })
	return ips
}

func ipToUint32(ip net.IP) uint32 {
	return binary.BigEndian.Uint32(ip.To4())
}

// discoverMockDPUs creates a DPUDiscovery covering exactly the mock pod IPs and waits until every
// pod is a DPUDevice with its DPUNode (nodeRebootMethod external).
func discoverMockDPUs(ctx context.Context, env *mockDPUEnv, ips []net.IP) []provisioningv1.DPUDevice {
	Expect(ips).NotTo(BeEmpty())
	discovery := &provisioningv1.DPUDiscovery{
		ObjectMeta: metav1.ObjectMeta{Name: mockDPUDiscoveryName, Namespace: env.namespace, Labels: CleanupScope.Suite},
		Spec: provisioningv1.DPUDiscoverySpec{
			IPRangeSpec: provisioningv1.IPRangeValidationSpec{IPRange: provisioningv1.IPRange{
				StartIP: ips[0].String(),
				EndIP:   ips[len(ips)-1].String(),
			}},
			ScanInterval: metav1.Duration{Duration: 30 * time.Second},
		},
	}
	By(fmt.Sprintf("Creating DPUDiscovery for %s-%s", discovery.Spec.IPRangeSpec.IPRange.StartIP, discovery.Spec.IPRangeSpec.IPRange.EndIP))
	Expect(client.IgnoreAlreadyExists(env.client.Create(ctx, discovery))).To(Succeed())

	var devices []provisioningv1.DPUDevice
	Eventually(func(g Gomega) {
		list := &provisioningv1.DPUDeviceList{}
		g.Expect(env.client.List(ctx, list, client.InNamespace(env.namespace))).To(Succeed())
		g.Expect(list.Items).To(HaveLen(env.replicas))
		nodes := &provisioningv1.DPUNodeList{}
		g.Expect(env.client.List(ctx, nodes, client.InNamespace(env.namespace))).To(Succeed())
		g.Expect(nodes.Items).To(HaveLen(env.replicas))
		for _, node := range nodes.Items {
			g.Expect(node.Spec.NodeRebootMethod).NotTo(BeNil())
			g.Expect(node.Spec.NodeRebootMethod.External).NotTo(BeNil(), "DPUNode %s must use the external reboot method", node.Name)
		}
		devices = list.Items
	}).WithTimeout(10 * time.Minute).WithPolling(mockPollInterval).Should(Succeed())
	for _, d := range devices {
		Expect(mockconfig.ValidSerialNumber(d.Spec.SerialNumber)).To(BeTrue(), "DPUDevice %s has an unexpected serial %q", d.Name, d.Spec.SerialNumber)
	}
	return devices
}

// waitForDPUDevicesReady waits for the discovery-side DPUDevice initialization (factory reset,
// password init, mTLS setup, firmware check) and the type detection.
func waitForDPUDevicesReady(ctx context.Context, env *mockDPUEnv, dpuType provisioningv1.DPUType) {
	tracker := NewByTracker()
	Eventually(func(g Gomega) {
		list := &provisioningv1.DPUDeviceList{}
		g.Expect(env.client.List(ctx, list, client.InNamespace(env.namespace))).To(Succeed())
		g.Expect(list.Items).To(HaveLen(env.replicas))
		for _, d := range list.Items {
			for _, condType := range []string{
				string(provisioningv1.ConditionDpuDeviceBMCFactoryResetReady),
				string(provisioningv1.ConditionDpuDeviceInitialized),
				string(provisioningv1.ConditionDpuDeviceDiscovered),
				string(provisioningv1.ConditionDpuDeviceReady),
			} {
				cond := findCondition(d.Status.Conditions, condType)
				status := "<missing>"
				if cond != nil {
					status = string(cond.Status) + "/" + cond.Reason
				}
				tracker.By(d.Name+condType+status, "DPUDevice %s %s: %s", d.Name, condType, status)
				g.Expect(cond).NotTo(BeNil(), "DPUDevice %s lacks condition %s", d.Name, condType)
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue), "DPUDevice %s condition %s: %s %s", d.Name, condType, cond.Reason, cond.Message)
			}
			g.Expect(d.Status.DPUType).To(Equal(dpuType), "DPUDevice %s type", d.Name)
			g.Expect(ptr.Deref(d.Status.PSID, "")).To(Equal(mockconfig.DefaultPSID))
		}
	}).WithTimeout(15 * time.Minute).WithPolling(mockPollInterval).Should(Succeed())
}

func findCondition(conds []metav1.Condition, condType string) *metav1.Condition {
	for i := range conds {
		if conds[i].Type == condType {
			return &conds[i]
		}
	}
	return nil
}

// createMockBFB creates the BFB pointing at the trimmed fixture and waits for Ready; the
// controller parses the same software inventory the mock reports after installation.
func createMockBFB(ctx context.Context, env *mockDPUEnv) *provisioningv1.BFB {
	bfb := &provisioningv1.BFB{
		ObjectMeta: metav1.ObjectMeta{Name: mockBFBName, Namespace: env.namespace, Labels: CleanupScope.Suite},
		Spec:       provisioningv1.BFBSpec{URL: env.bfbServerURL + "/" + mockBFBFixtureBFB},
	}
	Expect(client.IgnoreAlreadyExists(env.client.Create(ctx, bfb))).To(Succeed())
	tracker := NewByTracker()
	Eventually(func(g Gomega) {
		g.Expect(env.client.Get(ctx, client.ObjectKeyFromObject(bfb), bfb)).To(Succeed())
		tracker.By(string(bfb.Status.Phase), "BFB %s phase %s", bfb.Name, bfb.Status.Phase)
		g.Expect(bfb.Status.Phase).To(Equal(provisioningv1.BFBReady))
	}).WithTimeout(10 * time.Minute).WithPolling(mockPollInterval).Should(Succeed())
	Expect(bfb.Status.Versions.UEFI).To(Equal("4.15.0-19-g37c6f5adb2"))
	Expect(bfb.Status.Versions.BSP).To(Equal("4.15.0.13977"))
	return bfb
}

// createMockBlueFieldSoftware creates the BF4 software object with the tiny OS ISO and the trimmed
// PLDM bundle for the mock PSID, so provisioning goes through the Update Firmware phase.
func createMockBlueFieldSoftware(ctx context.Context, env *mockDPUEnv) *provisioningv1.BlueFieldSoftware {
	bfs := &provisioningv1.BlueFieldSoftware{
		ObjectMeta: metav1.ObjectMeta{Name: mockBlueFieldSoftware, Namespace: env.namespace, Labels: CleanupScope.Suite},
		Spec: provisioningv1.BlueFieldSpec{
			OsIso:        env.bfbServerURL + "/" + mockBFBFixtureISO,
			PldmFwBundle: map[string]string{mockconfig.DefaultPSID: env.bfbServerURL + "/" + mockBFBFixturePLDM},
		},
	}
	Expect(client.IgnoreAlreadyExists(env.client.Create(ctx, bfs))).To(Succeed())
	tracker := NewByTracker()
	Eventually(func(g Gomega) {
		g.Expect(env.client.Get(ctx, client.ObjectKeyFromObject(bfs), bfs)).To(Succeed())
		tracker.By(string(bfs.Status.Phase), "BlueFieldSoftware %s phase %s", bfs.Name, bfs.Status.Phase)
		g.Expect(bfs.Status.Phase).To(Equal(provisioningv1.BlueFieldSoftwareReady))
	}).WithTimeout(10 * time.Minute).WithPolling(mockPollInterval).Should(Succeed())
	Expect(bfs.Status.Versions).NotTo(BeNil())
	versions := bfs.Status.Versions.BluefieldSoftwareVersions[mockconfig.DefaultPSID]
	Expect(versions.BMCVersion).To(Equal("BF4-26.07-0005"))
	Expect(versions.BMCErotVersion).To(Equal("02.00.0044.0000_n05"))
	Expect(versions.SBIOSVersion).To(Equal("26.08-0007"))
	Expect(versions.BFNicFwVersion).To(Equal("82.48.4004"))
	return bfs
}

// createMockDPUSet creates the DPUSet selecting every discovered DPUNode.
func createMockDPUSet(ctx context.Context, env *mockDPUEnv, dpuType provisioningv1.DPUType) *provisioningv1.DPUSet {
	dpuSet := &provisioningv1.DPUSet{
		ObjectMeta: metav1.ObjectMeta{Name: mockDPUSetName, Namespace: env.namespace, Labels: CleanupScope.Suite},
		Spec: provisioningv1.DPUSetSpec{
			DPUNodeSelector: &metav1.LabelSelector{MatchLabels: map[string]string{cutil.NodeSelectorLabel: "true"}},
			Strategy: provisioningv1.DPUSetStrategy{
				Type:          provisioningv1.RollingUpdateStrategyType,
				RollingUpdate: &provisioningv1.RollingUpdateDPU{MaxUnavailable: ptr.To(intstr.FromString("100%"))},
			},
			DPUTemplate: provisioningv1.DPUTemplate{
				Spec: provisioningv1.DPUTemplateSpec{
					DPUFlavor:  ptr.To(mockDPUFlavorName),
					NodeEffect: provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}},
					Cluster:    &provisioningv1.ClusterSpec{NodeLabels: map[string]string{"e2e.test.io/mock-dpu": "true"}},
				},
			},
		},
	}
	if dpuType == provisioningv1.DPUTypeBlueField4 {
		dpuSet.Spec.DPUTemplate.Spec.BlueFieldSoftware = &provisioningv1.BlueFieldSoftwareReference{Name: mockBlueFieldSoftware}
	} else {
		dpuSet.Spec.DPUTemplate.Spec.BFB = &provisioningv1.BFBReference{Name: mockBFBName}
	}
	if mockSecureBootEnabled() {
		By("Enabling UEFI Secure Boot on the DPUSet so every DPU goes through Perform ARM Force Restart")
		dpuSet.Spec.DPUTemplate.Spec.SecureBoot = ptr.To(true)
	}
	Expect(client.IgnoreAlreadyExists(env.client.Create(ctx, dpuSet))).To(Succeed())
	return dpuSet
}

// waitForMockDPUsReady waits for every DPU to reach Ready, logging the phases it passes through.
func waitForMockDPUsReady(ctx context.Context, env *mockDPUEnv) {
	tracker := NewByTracker()
	Eventually(func(g Gomega) {
		dpus := &provisioningv1.DPUList{}
		g.Expect(env.client.List(ctx, dpus, client.InNamespace(env.namespace))).To(Succeed())
		g.Expect(dpus.Items).To(HaveLen(env.replicas), "expected one DPU per mock pod")
		for _, dpu := range dpus.Items {
			phase := string(dpu.Status.Phase)
			if rs := dpu.Status.RebootStatus; rs != nil {
				phase += fmt.Sprintf(" (reboot %s/%s %s)", ptr.Deref(rs.Method, ""), rs.Phase, rs.Reason)
			}
			tracker.By(dpu.Name+phase, "DPU %s phase %s", dpu.Name, phase)
			g.Expect(dpu.Status.Phase).NotTo(Equal(provisioningv1.DPUError), "DPU %s entered Error: %s", dpu.Name, dpuConditionsSummary(&dpu))
			g.Expect(dpu.Status.Phase).To(Equal(provisioningv1.DPUReady), "DPU %s is %s", dpu.Name, dpu.Status.Phase)
		}
	}).WithTimeout(mockProvisioningTimeout).WithPolling(mockPollInterval).Should(Succeed())
}

func dpuConditionsSummary(dpu *provisioningv1.DPU) string {
	parts := make([]string, 0, len(dpu.Status.Conditions))
	for _, c := range dpu.Status.Conditions {
		if c.Status != metav1.ConditionTrue {
			parts = append(parts, fmt.Sprintf("%s=%s(%s: %s)", c.Type, c.Status, c.Reason, c.Message))
		}
	}
	return strings.Join(parts, "; ")
}

// verifyMockDPUsProvisioned checks everything the mock agent and the mock kubelet should have
// left behind once every DPU is Ready. armRestarted says whether this round had to go through
// Perform ARM Force Restart to enable Secure Boot: true for freshly discovered devices, false for a
// reprovisioned DPU, whose mock BMC kept Secure Boot enabled so the controller skipped the phase.
func verifyMockDPUsProvisioned(ctx context.Context, env *mockDPUEnv, dpuType provisioningv1.DPUType, armRestarted bool) {
	dpus := &provisioningv1.DPUList{}
	Expect(env.client.List(ctx, dpus, client.InNamespace(env.namespace))).To(Succeed())
	Expect(dpus.Items).To(HaveLen(env.replicas))

	approvedCNs := sets.New[string]()
	for _, cl := range []client.Client{env.client, env.dpuClusterClient} {
		approvedCNs = approvedCNs.Union(approvedCSRCommonNames(ctx, cl))
	}

	for i := range dpus.Items {
		dpu := &dpus.Items[i]
		By(fmt.Sprintf("Verifying DPU %s", dpu.Name))
		Expect(dpu.Status.DPUType).To(Equal(dpuType))
		if mockSecureBootEnabled() {
			Expect(ptr.Deref(dpu.Spec.SecureBoot, false)).To(BeTrue())
			Expect(dpu.Status.SecureBoot).NotTo(BeNil(), "DPU %s has no secureBoot status", dpu.Name)
			Expect(ptr.Deref(dpu.Status.SecureBoot.Enabled, false)).To(BeTrue(), "DPU %s Secure Boot is not reported enabled", dpu.Name)
			if armRestarted {
				// Perform ARM Force Restart: two Arm restarts through the mock BMC, then the
				// controller read Secure Boot back as active.
				_, armCond := cutil.GetDPUCondition(&dpu.Status, provisioningv1.DPUCondArmForceRestarted.String())
				Expect(armCond).NotTo(BeNil(), "DPU %s has no %s condition", dpu.Name, provisioningv1.DPUCondArmForceRestarted)
				Expect(armCond.Status).To(Equal(metav1.ConditionTrue), "DPU %s %s: %s %s", dpu.Name, armCond.Type, armCond.Reason, armCond.Message)
			}
		}
		Expect(dpu.Status.AgentStatus).NotTo(BeNil())
		agent := dpu.Status.AgentStatus
		Expect(agent.LastStartupTime).NotTo(BeNil())
		Expect(ptr.Deref(agent.RebootMethod, "")).To(Equal(provisioningv1.RebootMethodNoAction))
		Expect(ptr.Deref(agent.KubeletVersion, "")).NotTo(BeEmpty())
		Expect(agent.HostOSInit).NotTo(BeNil())
		Expect(agent.HostOSInit.Skipped).NotTo(BeNil(), "flavor has no DELAY_HOST_OS_INIT hold, release must be reported as skipped")

		reported := sets.New[string]()
		for _, cond := range agent.Conditions {
			reported.Insert(cond.Type)
			Expect(cond.Status).To(Equal(metav1.ConditionTrue), "DPU %s agent condition %s: %s %s", dpu.Name, cond.Type, cond.Reason, cond.Message)
		}
		Expect(reported.UnsortedList()).To(ConsistOf(expectedMockAgentConditions), "DPU %s agent conditions", dpu.Name)

		// Identities: the agent certificate in the management cluster (approved by the DPF csr
		// controller) and the kubelet certificate in the DPU cluster (approved by kubeadm's RBAC).
		Expect(approvedCNs.Has(providentity.DPUAgentUsername(dpu.Name))).To(BeTrue(), "no approved CSR for %s", providentity.DPUAgentUsername(dpu.Name))
		Expect(approvedCNs.Has("system:node:"+dpu.Name)).To(BeTrue(), "no approved kubelet CSR for %s", dpu.Name)

		// The DPU cluster Node, its Lease and the labels Cluster Config applied.
		node := &corev1.Node{}
		Expect(env.dpuClusterClient.Get(ctx, types.NamespacedName{Name: dpu.Name}, node)).To(Succeed())
		Expect(cutil.IsNodeReady(node)).To(BeTrue(), "Node %s is not Ready", node.Name)
		Expect(node.Labels).To(HaveKeyWithValue(mocknode.FakeNodeLabel, "true"))
		Expect(node.Labels).To(HaveKeyWithValue("e2e.test.io/mock-dpu", "true"))
		lease := &coordinationv1.Lease{}
		Expect(env.dpuClusterClient.Get(ctx, types.NamespacedName{Namespace: "kube-node-lease", Name: dpu.Name}, lease)).To(Succeed())
		Expect(lease.Spec.RenewTime).NotTo(BeNil())
		Expect(time.Since(lease.Spec.RenewTime.Time)).To(BeNumerically("<", time.Minute), "Lease %s is stale", dpu.Name)

		// Cluster Config revoked the join material.
		joinSecret := &corev1.Secret{}
		err := env.client.Get(ctx, types.NamespacedName{Namespace: env.namespace, Name: cutil.KubeadmJoinSecretName(dpu.Name)}, joinSecret)
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), "join secret of %s should be deleted", dpu.Name)
	}

	// Cluster Config revoked both bootstrap tokens: the node-join token in the DPU cluster and the
	// dpu-agent token in the management cluster.
	for name, cl := range map[string]client.Client{"DPU cluster": env.dpuClusterClient, "management cluster": env.client} {
		tokens := &corev1.SecretList{}
		Expect(cl.List(ctx, tokens, client.InNamespace("kube-system"), client.MatchingLabels{cutil.LabelDPUNamespace: env.namespace})).To(Succeed())
		for _, s := range tokens.Items {
			Expect(s.Type).NotTo(Equal(corev1.SecretTypeBootstrapToken), "bootstrap token %s in the %s was not revoked", s.Name, name)
		}
	}
}

// approvedCSRCommonNames returns the subject common names of every approved and issued CSR.
func approvedCSRCommonNames(ctx context.Context, cl client.Client) sets.Set[string] {
	csrs := &certificatesv1.CertificateSigningRequestList{}
	Expect(cl.List(ctx, csrs)).To(Succeed())
	out := sets.New[string]()
	for _, csr := range csrs.Items {
		approved := false
		for _, cond := range csr.Status.Conditions {
			if cond.Type == certificatesv1.CertificateApproved && cond.Status == corev1.ConditionTrue {
				approved = true
			}
		}
		if !approved || len(csr.Status.Certificate) == 0 {
			continue
		}
		if block, _ := pem.Decode(csr.Spec.Request); block != nil {
			if req, err := x509.ParseCertificateRequest(block.Bytes); err == nil {
				out.Insert(req.Subject.CommonName)
			}
		}
	}
	return out
}

// deprovisionMockDPUs removes the DPUSet, DPUs, software objects, discovery objects and any leftover
// fake Node so the next case starts from an empty control plane.
func deprovisionMockDPUs(ctx context.Context, env *mockDPUEnv) {
	By("Deprovisioning mock DPUs")
	// deleteIfExists deletes obj through cl: the provisioning objects live in the management
	// cluster, the fake Nodes in the DPU cluster.
	deleteIfExists := func(cl client.Client, obj client.Object) {
		err := cl.Delete(ctx, obj)
		if !apierrors.IsNotFound(err) {
			Expect(err).NotTo(HaveOccurred())
		}
	}
	deleteIfExists(env.client, &provisioningv1.DPUSet{ObjectMeta: metav1.ObjectMeta{Name: mockDPUSetName, Namespace: env.namespace}})
	deleteIfExists(env.client, &provisioningv1.DPUDiscovery{ObjectMeta: metav1.ObjectMeta{Name: mockDPUDiscoveryName, Namespace: env.namespace}})
	Eventually(func(g Gomega) {
		dpus := &provisioningv1.DPUList{}
		g.Expect(env.client.List(ctx, dpus, client.InNamespace(env.namespace))).To(Succeed())
		g.Expect(dpus.Items).To(BeEmpty(), "%d DPUs still deleting", len(dpus.Items))
	}).WithTimeout(mockDeprovisionTimeout).WithPolling(mockPollInterval).Should(Succeed())

	deleteIfExists(env.client, &provisioningv1.BFB{ObjectMeta: metav1.ObjectMeta{Name: mockBFBName, Namespace: env.namespace}})
	deleteIfExists(env.client, &provisioningv1.BlueFieldSoftware{ObjectMeta: metav1.ObjectMeta{Name: mockBlueFieldSoftware, Namespace: env.namespace}})
	nodes := &provisioningv1.DPUNodeList{}
	Expect(env.client.List(ctx, nodes, client.InNamespace(env.namespace))).To(Succeed())
	for i := range nodes.Items {
		deleteIfExists(env.client, &nodes.Items[i])
	}
	devices := &provisioningv1.DPUDeviceList{}
	Expect(env.client.List(ctx, devices, client.InNamespace(env.namespace))).To(Succeed())
	for i := range devices.Items {
		deleteIfExists(env.client, &devices.Items[i])
	}
	fakeNodes := &corev1.NodeList{}
	Expect(env.dpuClusterClient.List(ctx, fakeNodes, client.MatchingLabels{mocknode.FakeNodeLabel: "true"})).To(Succeed())
	for i := range fakeNodes.Items {
		deleteIfExists(env.dpuClusterClient, &fakeNodes.Items[i])
	}
	Eventually(func(g Gomega) {
		for _, list := range []client.ObjectList{&provisioningv1.DPUNodeList{}, &provisioningv1.DPUDeviceList{}, &provisioningv1.BFBList{}, &provisioningv1.BlueFieldSoftwareList{}} {
			g.Expect(env.client.List(ctx, list, client.InNamespace(env.namespace))).To(Succeed())
			g.Expect(metaLen(list)).To(BeZero(), "%T not empty yet", list)
		}
		leftovers := &corev1.NodeList{}
		g.Expect(env.dpuClusterClient.List(ctx, leftovers, client.MatchingLabels{mocknode.FakeNodeLabel: "true"})).To(Succeed())
		g.Expect(leftovers.Items).To(BeEmpty())
	}).WithTimeout(mockDeprovisionTimeout).WithPolling(mockPollInterval).Should(Succeed())
}

func metaLen(list client.ObjectList) int {
	switch l := list.(type) {
	case *provisioningv1.DPUNodeList:
		return len(l.Items)
	case *provisioningv1.DPUDeviceList:
		return len(l.Items)
	case *provisioningv1.BFBList:
		return len(l.Items)
	case *provisioningv1.BlueFieldSoftwareList:
		return len(l.Items)
	}
	return 0
}

// provisionMockDPUs runs one full cycle with the given mock configuration: reconfigure the mock
// pods, discover them, publish the software for the DPU type, create the DPUSet and wait for Ready.
func provisionMockDPUs(ctx context.Context, env *mockDPUEnv, cfg mockDPUConfig) {
	dpuType := provisioningv1.DPUTypeBlueField3
	if cfg.dpuType == mockconfig.DPUTypeBF4 {
		dpuType = provisioningv1.DPUTypeBlueField4
	}
	ips := reconfigureMockDPUs(ctx, env, cfg)
	discoverMockDPUs(ctx, env, ips)
	if dpuType == provisioningv1.DPUTypeBlueField4 {
		createMockBlueFieldSoftware(ctx, env)
	} else {
		createMockBFB(ctx, env)
	}
	waitForDPUDevicesReady(ctx, env, dpuType)
	createMockDPUSet(ctx, env, dpuType)
	waitForMockDPUsReady(ctx, env)
	verifyMockDPUsProvisioned(ctx, env, dpuType, true)
}
