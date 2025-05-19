/*
Copyright 2024 NVIDIA

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

package clusterhelper

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var (
	cfg        *rest.Config
	testClient client.Client
	testEnv    *envtest.Environment
	testToken  = "test-secret-token"
	testUser   = "test-user"
	tmpDir     string
)

func TestSource(t *testing.T) {
	RegisterFailHandler(Fail)
	suiteName := "Cluster helper suite"
	RunSpecs(t, suiteName)
}

// Note: clusterhelper interacts with two different clusters, but in tests single cluster is used
var _ = BeforeSuite(func() {
	logf.SetLogger(klog.Background())

	var err error
	tmpDir, err = os.MkdirTemp("", "clusterhelper*")
	Expect(err).NotTo(HaveOccurred())
	Expect(os.WriteFile(filepath.Join(tmpDir, "auth_file"), []byte(strings.Join([]string{testToken, testUser, "uid1"}, ",")), 0644)).NotTo(HaveOccurred())
	Expect(os.WriteFile(filepath.Join(tmpDir, "token"), []byte(testToken), 0644)).NotTo(HaveOccurred())

	apiServerConf := &envtest.APIServer{}
	apiServerConf.Configure().Append("token-auth-file", filepath.Join(tmpDir, "auth_file"))
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "..", "..", "..", "..", "config", "snap", "crd"),
			filepath.Join("..", "..", "..", "..", "..", "..", "config", "provisioning", "crd", "bases"),
		},
		ControlPlane:          envtest.ControlPlane{APIServer: apiServerConf},
		ErrorIfCRDPathMissing: true,
		// The BinaryAssetsDirectory is only required if you want to run the tests directly
		// without call the makefile target test. If not informed it will look for the
		// default path defined in controller-runtime which is /usr/local/kubebuilder/.
		// Note that you must have the required binaries setup under the bin directory to perform
		// the tests directly. When we run make test it will be setup and used automatically.
		BinaryAssetsDirectory: filepath.Join("..", "..", "..", "..", "..", "..", "hack", "tools", "bin", "k8s",
			fmt.Sprintf("1.29.0-%s-%s", runtime.GOOS, runtime.GOARCH)),
	}
	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())
	Expect(os.WriteFile(filepath.Join(tmpDir, "ca"), cfg.CAData, 0644)).NotTo(HaveOccurred())
	Expect(writeKubeconfig(cfg, filepath.Join(tmpDir, "kubeconfig"))).NotTo(HaveOccurred())

	testClient, err = client.New(cfg, client.Options{Scheme: scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(testClient).NotTo(BeNil())
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
	Expect(os.RemoveAll(tmpDir)).NotTo(HaveOccurred())
})

func writeKubeconfig(cfg *rest.Config, dst string) error {
	return clientcmd.WriteToFile(clientcmdapi.Config{
		Kind:       "Config",
		APIVersion: "v1",
		Clusters: map[string]*clientcmdapi.Cluster{
			"default-cluster": {
				Server:                   cfg.Host,
				CertificateAuthorityData: cfg.CAData,
			},
		},
		Contexts: map[string]*clientcmdapi.Context{
			"default-context": {
				Cluster:  "default-cluster",
				AuthInfo: "default-user",
			},
		},
		CurrentContext: "default-context",
		AuthInfos: map[string]*clientcmdapi.AuthInfo{
			"default-user": {
				ClientCertificateData: cfg.CertData,
				ClientKeyData:         cfg.KeyData,
			},
		},
	}, dst)
}
