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

// Package agent impersonates the dpu-agent of one DPU: it takes the identity the install artifact
// carries, exchanges the bootstrap token for a client certificate exactly like cmd/dpuagent does
// and then runs the agent operation pipeline against the DPU object with no-op operations that
// write the same status the real ones would.
package agent

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nvidia/doca-platform/cmd/dpuagent/opts"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	provcertificate "github.com/nvidia/doca-platform/internal/provisioning/utils/certificate"
	"github.com/nvidia/doca-platform/internal/provisioning/utils/certificate/bootstrap"
	providentity "github.com/nvidia/doca-platform/internal/provisioning/utils/certificate/identity"
	"github.com/nvidia/doca-platform/test/mock/dpuagent/pkg/artifact"

	"k8s.io/apimachinery/pkg/util/wait"
	clientset "k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/klog/v2"
)

const (
	// dpuAgentPairName matches cmd/dpuagent so the certificate files have the same names.
	dpuAgentPairName = "dpu-agent-client"

	agentConfFile          = "dpuagent.conf"
	bootstrapKubeconfig    = "bootstrap-kubeconfig"
	kubeconfigFile         = "kubeconfig"
	caTrustBundleFile      = "dpf-ca.crt"
	pkiDir                 = "pki"
	certWaitInterval       = 5 * time.Second
	identityFilePermission = 0o600
)

// Identity is what the mock takes from /opt/dpf/dpuagent.conf. Every other agent flag is ignored:
// the mock does not run the real operations that would consume them.
type Identity struct {
	DPUName                string
	DPUNamespace           string
	DPUUID                 string
	DPUType                string
	AstraEnabled           bool
	KubeadmSecretName      string
	KubeadmSecretNamespace string
}

// ParseAgentConf reads the `--key=value` lines of dpuagent.conf.
func ParseAgentConf(conf string) (*Identity, error) {
	id := &Identity{}
	scanner := bufio.NewScanner(strings.NewReader(conf))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "--") {
			continue
		}
		key, value, _ := strings.Cut(strings.TrimPrefix(line, "--"), "=")
		switch key {
		case "dpu-name":
			id.DPUName = value
		case "dpu-namespace":
			id.DPUNamespace = value
		case "dpu-uid":
			id.DPUUID = value
		case "dpu-type":
			id.DPUType = value
		case "astra-enabled":
			id.AstraEnabled, _ = strconv.ParseBool(value)
		case "kubeadm-secret-name":
			id.KubeadmSecretName = value
		case "kubeadm-secret-namespace":
			id.KubeadmSecretNamespace = value
		}
	}
	if id.DPUName == "" || id.DPUNamespace == "" || id.DPUUID == "" {
		return nil, fmt.Errorf("dpuagent.conf must carry --dpu-name, --dpu-namespace and --dpu-uid")
	}
	if id.KubeadmSecretName == "" {
		id.KubeadmSecretName = cutil.KubeadmJoinSecretName(id.DPUName)
	}
	if id.KubeadmSecretNamespace == "" {
		id.KubeadmSecretNamespace = id.DPUNamespace
	}
	return id, nil
}

// Options builds the subset of dpu-agent options the reused operations and the status patch read.
// Validate() is deliberately not called: the mock never parses real agent flags.
func (id *Identity) Options() opts.Options {
	return opts.Options{
		ZeroTrustMode:          true,
		DPUName:                id.DPUName,
		DPUNamespace:           id.DPUNamespace,
		DPUUID:                 id.DPUUID,
		DPUType:                id.DPUType,
		AstraEnabled:           id.AstraEnabled,
		KubeadmSecretName:      id.KubeadmSecretName,
		KubeadmSecretNamespace: id.KubeadmSecretNamespace,
	}
}

// Materialize writes the agent files of a parsed cloud-init user-data into the per-DPU directory
// dir and returns the identity. dir is "<work dir>/<dpu uid>" so a reprovisioned DPU never reuses
// stale credentials.
func Materialize(files *artifact.AgentFiles, dir string) (*Identity, error) {
	id, err := ParseAgentConf(files.AgentConf)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(dir, pkiDir), 0o700); err != nil {
		return nil, err
	}
	for name, content := range map[string]string{
		agentConfFile:       files.AgentConf,
		bootstrapKubeconfig: files.BootstrapKubeconfig,
		caTrustBundleFile:   files.CATrustBundle,
	} {
		if content == "" {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), identityFilePermission); err != nil {
			return nil, fmt.Errorf("write %s: %w", name, err)
		}
	}
	// A fresh install never reuses the client certificate of a previous run in the same directory.
	_ = os.Remove(filepath.Join(dir, kubeconfigFile))
	return id, nil
}

// Bootstrap replays the bootstrap-kubeconfig branch of cmd/dpuagent buildClientConfig: the
// bootstrap token authenticates a CSR for da-<dpu> in the dpu-agents organization, the csr
// controller approves it and the returned config authenticates with the rotated client
// certificate. Rotation stops when ctx is canceled.
func Bootstrap(ctx context.Context, dir string, id *Identity) (*restclient.Config, error) {
	kubeconfigPath := filepath.Join(dir, kubeconfigFile)
	bootstrapPath := filepath.Join(dir, bootstrapKubeconfig)
	certDir := filepath.Join(dir, pkiDir)

	certConfig, clientConfig, err := bootstrap.LoadClientConfig(kubeconfigPath, bootstrapPath, certDir, dpuAgentPairName)
	if err != nil {
		return nil, fmt.Errorf("load client config: %w", err)
	}
	commonName := providentity.DPUAgentUsername(id.DPUName)
	newClientsetFn := func(current *tls.Certificate) (clientset.Interface, error) {
		cfg := certConfig
		if current != nil {
			cfg = clientConfig
		}
		return clientset.NewForConfig(cfg)
	}
	mgr, err := provcertificate.NewCertificateManager(certDir, "", clientConfig.CertFile, clientConfig.KeyFile,
		newClientsetFn, dpuAgentPairName, commonName, []string{providentity.DPUAgentOrganization})
	if err != nil {
		return nil, fmt.Errorf("create certificate manager: %w", err)
	}
	transportConfig := restclient.AnonymousClientConfig(clientConfig)
	stopCh := make(chan struct{})
	closeConns, err := provcertificate.UpdateTransport(stopCh, transportConfig, mgr, 0)
	if err != nil {
		close(stopCh)
		return nil, fmt.Errorf("update transport: %w", err)
	}
	mgr.Start()
	go func() {
		<-ctx.Done()
		close(stopCh)
		mgr.Stop()
		closeConns()
	}()

	klog.InfoS("TLS bootstrapping", "cn", commonName, "server", transportConfig.Host)
	if err := wait.PollUntilContextCancel(ctx, certWaitInterval, true, func(context.Context) (bool, error) {
		if mgr.Current() != nil {
			return true, nil
		}
		klog.V(2).InfoS("client certificate not available yet", "cn", commonName)
		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("wait for client certificate: %w", err)
	}
	klog.InfoS("TLS bootstrapping completed", "cn", commonName)
	return transportConfig, nil
}
