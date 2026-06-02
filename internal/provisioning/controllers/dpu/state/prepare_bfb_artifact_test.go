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
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/cloudinit"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
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
})

func extractBootstrapKubeconfig(userData []byte) []byte {
	parsed := &artifactCloudConfig{}
	Expect(yaml.Unmarshal(userData, parsed)).To(Succeed())
	for _, file := range parsed.WriteFiles {
		if file.Path == "/var/lib/dpf/dpuagent/bootstrap-kubeconfig" {
			return []byte(file.Content)
		}
	}
	Fail("bootstrap kubeconfig write_files entry not found")
	return nil
}
