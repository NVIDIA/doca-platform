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

package state_test

import (
	"context"
	"os"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/constants"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/cloudinit"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/yaml"
)

type artifactCloudConfig struct {
	WriteFiles []artifactWriteFile `json:"write_files"`
}

type artifactWriteFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

var _ = Describe("DefaultDPUArtifactGenerator", func() {
	const bootstrapToken = "abc123.token"

	var (
		ctx       context.Context
		caPath    string
		generator *state.DefaultDPUArtifactGenerator
		req       dutil.DPUArtifactRequest
	)

	BeforeEach(func() {
		ctx = context.Background()
		caFile, err := os.CreateTemp("", "dpu-artifact-ca-*.crt")
		Expect(err).NotTo(HaveOccurred())
		caPath = caFile.Name()
		Expect(os.WriteFile(caPath, []byte("fake-ca-data"), 0600)).To(Succeed())
		generator = &state.DefaultDPUArtifactGenerator{ServiceAccountCAPath: caPath}

		scheme := runtime.NewScheme()
		utilruntime.Must(clientgoscheme.AddToScheme(scheme))
		utilruntime.Must(operatorv1.AddToScheme(scheme))
		utilruntime.Must(provisioningv1.AddToScheme(scheme))

		mtu := 1500
		dpfConfig := &operatorv1.DPFOperatorConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-config",
				Namespace: "test-namespace",
			},
			Spec: operatorv1.DPFOperatorConfigSpec{
				DeploymentMode: operatorv1.DeploymentModeHostTrusted,
				Networking: &operatorv1.Networking{
					ControlPlaneMTU: &mtu,
				},
			},
		}
		dpu := &provisioningv1.DPU{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-dpu",
				Namespace: "test-namespace",
				UID:       "test-uid",
			},
		}
		flavor := &provisioningv1.DPUFlavor{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-flavor",
				Namespace: "test-namespace",
			},
		}
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(dpfConfig).Build()
		req = dutil.DPUArtifactRequest{
			ControllerContext: &dutil.ControllerContext{Client: fakeClient},
			DPU:               dpu,
			Flavor:            flavor,
			BootstrapToken:    bootstrapToken,
		}
	})

	AfterEach(func() {
		if caPath != "" {
			_ = os.Remove(caPath)
		}
	})

	It("should inject bootstrap kubeconfig with configured CA data", func() {
		artifact, err := generator.GenerateBF4(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		Expect(artifact.UserData).NotTo(BeEmpty())

		tmpKubeconfig, err := os.CreateTemp("", "dpu-artifact-kubeconfig-*.yaml")
		Expect(err).NotTo(HaveOccurred())
		defer func() { _ = os.Remove(tmpKubeconfig.Name()) }()
		Expect(os.WriteFile(tmpKubeconfig.Name(), extractBootstrapKubeconfig(artifact.UserData), 0600)).To(Succeed())

		cfg, err := clientcmd.BuildConfigFromFlags("", tmpKubeconfig.Name())
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.CAData).To(Equal([]byte("fake-ca-data")))
	})

	It("should return BF3 bf.cfg bytes", func() {
		data, err := generator.GenerateBF3(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		Expect(data).NotTo(BeEmpty())
	})

	It("should return BF4 user-data and network-config bytes", func() {
		artifact, err := generator.GenerateBF4(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		Expect(artifact.UserData).NotTo(BeEmpty())
		Expect(artifact.NetworkConfig).To(Equal([]byte(cloudinit.GenerateNetworkCfg().Content)))
	})

	It("should return error when CA file is missing", func() {
		generator.ServiceAccountCAPath = "/does/not/exist"
		_, err := generator.GenerateBF3(ctx, req)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("reading CA certificate"))
	})

	Context("when the DPU is in SPIFFE identity mode", func() {
		const (
			trustBundlePEM = "-----BEGIN CERTIFICATE-----\nFAKEBUNDLE\n-----END CERTIFICATE-----"
			testSerial     = "MT2440600YYW"
		)

		var fakeClient client.Client

		defaultDPFConfig := func() *operatorv1.DPFOperatorConfig {
			mtu := 1500
			vip := "10.0.110.1"
			port := 6443
			return &operatorv1.DPFOperatorConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "test-config", Namespace: "test-namespace"},
				Spec: operatorv1.DPFOperatorConfigSpec{
					DeploymentMode: operatorv1.DeploymentModeZeroTrust,
					Networking:     &operatorv1.Networking{ControlPlaneMTU: &mtu},
					Overrides: &operatorv1.Overrides{
						KubernetesAPIServerVIP:  &vip,
						KubernetesAPIServerPort: &port,
					},
					Security: &operatorv1.SecurityConfiguration{
						SPIFFE: &operatorv1.SPIFFEConfiguration{
							SPIREServerAddress:                "spire-server.spire.svc:8081",
							SPIRETrustDomain:                  "cs.internal",
							DPUAgentSPIFFEIDTemplate:          "spiffe://{{ .TrustDomain }}/tenant/operator/service/dsx/dpu/{{ .SerialNumber }}/process/dpu-agent",
							DPUAgentExchangedSPIFFEIDTemplate: "spiffe://operator.example.test/dpu/{{ .SerialNumber }}/process/dpu-agent",
							KubeAPIAudience:                   "dpf",
							SPIREOIDCURL:                      "https://spire-oidc.example.com",
							SPIREControllerManagerClassName:   "spire-mgmt-spire",
							TrustBundle: operatorv1.SPIFFETrustBundleConfigMapReference{
								Name:      "spire-bundle",
								Namespace: "spire",
							},
						},
					},
				},
			}
		}

		buildReqWith := func(dpfConfig *operatorv1.DPFOperatorConfig, objs ...client.Object) {
			scheme := runtime.NewScheme()
			utilruntime.Must(clientgoscheme.AddToScheme(scheme))
			utilruntime.Must(operatorv1.AddToScheme(scheme))
			utilruntime.Must(provisioningv1.AddToScheme(scheme))

			dpu := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{Name: "test-dpu", Namespace: "test-namespace", UID: "test-uid"},
				Spec: provisioningv1.DPUSpec{
					DPUDeviceName: "test-device",
				},
				Status: provisioningv1.DPUStatus{IdentityMode: ptr.To(provisioningv1.IdentityModeSpiffe)},
			}
			dpuDevice := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{Name: "test-device", Namespace: "test-namespace"},
				Spec:       provisioningv1.DPUDeviceSpec{SerialNumber: testSerial},
			}
			flavor := &provisioningv1.DPUFlavor{
				ObjectMeta: metav1.ObjectMeta{Name: "test-flavor", Namespace: "test-namespace"},
			}
			all := append([]client.Object{dpfConfig, dpu, dpuDevice}, objs...)
			fakeClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(all...).Build()
			req = dutil.DPUArtifactRequest{
				ControllerContext: &dutil.ControllerContext{Client: fakeClient},
				DPU:               dpu,
				Flavor:            flavor,
			}
		}

		buildReq := func(objs ...client.Object) {
			buildReqWith(defaultDPFConfig(), objs...)
		}

		trustBundleCM := func() *corev1.ConfigMap {
			return &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "spire-bundle", Namespace: "spire"},
				Data:       map[string]string{"bundle.pem": trustBundlePEM},
			}
		}
		spiffeTrustBundleCM := func() *corev1.ConfigMap {
			return &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "spire-bundle", Namespace: "spire"},
				Data:       map[string]string{"bundle.spiffe": `{"spiffe_sequence":1,"keys":[]}`},
			}
		}

		// This is the pipeline (GenerateBF4) test: it asserts only what the resolve step
		// contributes on top of template rendering -- trust-bundle ConfigMap resolution,
		// the spireServerAddress host/port split, and bootstrap omission. The rendered
		// SPIRE/spiffe-helper config substrings are covered by the cloudinit render unit test.
		It("resolves the trust bundle and SPIRE server address, and omits bootstrap kubeconfig", func() {
			buildReq(trustBundleCM())

			artifact, err := generator.GenerateBF4(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			content, found := extractWriteFile(artifact.UserData, "/etc/spire/agent/trust-bundle.pem")
			Expect(found).To(BeTrue(), "trust-bundle.pem write_files entry should be present")
			Expect(content).To(ContainSubstring("FAKEBUNDLE"))

			agentConf, agentFound := extractWriteFile(artifact.UserData, constants.SPIREAgentConfigPath)
			Expect(agentFound).To(BeTrue())
			Expect(agentConf).To(ContainSubstring(`server_address = "spire-server.spire.svc"`))
			Expect(agentConf).To(ContainSubstring("server_port = 8081"))
			Expect(agentConf).To(ContainSubstring(`trust_bundle_format = "pem"`))

			_, bootstrapFound := extractWriteFile(artifact.UserData, "/var/lib/dpf/dpuagent/bootstrap-kubeconfig")
			Expect(bootstrapFound).To(BeFalse(), "bootstrap kubeconfig must not be rendered for SPIFFE DPUs")
		})

		It("configures SPIRE Agent to read a standard SPIFFE bundle", func() {
			cfg := defaultDPFConfig()
			cfg.Spec.Security.SPIFFE.TrustBundle.Format = operatorv1.SPIFFETrustBundleFormatSPIFFE
			buildReqWith(cfg, spiffeTrustBundleCM())

			artifact, err := generator.GenerateBF4(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			content, found := extractWriteFile(artifact.UserData, constants.SPIRETrustBundleSPIFFEPath)
			Expect(found).To(BeTrue())
			Expect(content).To(ContainSubstring(`"spiffe_sequence":1`))

			agentConf, found := extractWriteFile(artifact.UserData, constants.SPIREAgentConfigPath)
			Expect(found).To(BeTrue())
			Expect(agentConf).To(ContainSubstring(`trust_bundle_format = "spiffe"`))
		})

		It("writes a SPIFFE kubeconfig with CA data and token file authentication", func() {
			buildReq(trustBundleCM())

			artifact, err := generator.GenerateBF4(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			content, found := extractWriteFile(artifact.UserData, constants.SpiffeKubeconfigPath)
			Expect(found).To(BeTrue())

			kubeconfig, err := clientcmd.Load([]byte(content))
			Expect(err).NotTo(HaveOccurred())
			Expect(kubeconfig.Clusters["default"].Server).To(Equal("https://10.0.110.1:6443"))
			Expect(kubeconfig.Clusters["default"].CertificateAuthorityData).To(Equal([]byte("fake-ca-data")))
			Expect(kubeconfig.AuthInfos["default"].TokenFile).To(Equal(constants.SpiffeTokenPath))
		})

		It("configures token exchange", func() {
			config := defaultDPFConfig()
			config.Spec.Security.SPIFFE.KubeAPIAudience = "az51-dev2-dh2"
			config.Spec.Security.SPIFFE.TokenExchangeEndpoint = ptr.To("https://identity-keys.example/v1/exchange")
			buildReqWith(config, trustBundleCM())

			artifact, err := generator.GenerateBF4(ctx, req)
			Expect(err).NotTo(HaveOccurred())

			helperConfig, found := extractWriteFile(artifact.UserData, constants.SpiffeHelperConfigPath)
			Expect(found).To(BeTrue())
			Expect(helperConfig).To(ContainSubstring(`token_exchange_endpoint = "https://identity-keys.example/v1/exchange"`))
		})

		It("embeds SPIFFE cloud-init in BF3 output", func() {
			buildReq(trustBundleCM())

			data, err := generator.GenerateBF3(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(data)).To(ContainSubstring("--spiffe-mode=true"))
			Expect(string(data)).To(ContainSubstring(constants.SpiffeKubeconfigPath))
			Expect(string(data)).To(ContainSubstring(constants.SpiffeTokenPath))
			Expect(string(data)).NotTo(ContainSubstring("/var/lib/dpf/dpuagent/bootstrap-kubeconfig"))
		})

		It("errors when the trust bundle ConfigMap is absent", func() {
			buildReq()

			_, err := generator.GenerateBF4(ctx, req)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("getting SPIRE trust bundle ConfigMap"))
		})

		It("errors when the trust bundle ConfigMap lacks the bundle.pem key", func() {
			cm := trustBundleCM()
			cm.Data = map[string]string{"other": "x"}
			buildReq(cm)

			_, err := generator.GenerateBF4(ctx, req)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("missing non-empty"))
		})

		It("errors when the DPU is SPIFFE-mode but spec.security.spiffe is unset", func() {
			// The SpiffeEnabled guard runs before readTrustBundle, so no ConfigMap is needed.
			cfg := defaultDPFConfig()
			cfg.Spec.Security = nil
			buildReqWith(cfg)

			_, err := generator.GenerateBF4(ctx, req)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("spec.security.spiffe is unset"))
		})

		It("errors when the SPIRE server address is malformed", func() {
			// SplitHostPort runs after readTrustBundle, so the bundle ConfigMap must be present.
			cfg := defaultDPFConfig()
			cfg.Spec.Security.SPIFFE.SPIREServerAddress = "no-colon"
			buildReqWith(cfg, trustBundleCM())

			_, err := generator.GenerateBF4(ctx, req)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("parsing SPIRE server address"))
		})
	})
})

func extractBootstrapKubeconfig(userData []byte) []byte {
	content, found := extractWriteFile(userData, "/var/lib/dpf/dpuagent/bootstrap-kubeconfig")
	if !found {
		Fail("bootstrap kubeconfig write_files entry not found")
	}
	return []byte(content)
}

// extractWriteFile returns the content of the write_files entry at path, and whether it exists.
func extractWriteFile(userData []byte, path string) (string, bool) {
	parsed := &artifactCloudConfig{}
	Expect(yaml.Unmarshal(userData, parsed)).To(Succeed())
	for _, file := range parsed.WriteFiles {
		if file.Path == path {
			return file.Content, true
		}
	}
	return "", false
}
