//go:build linux

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

package app

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"
	"time"

	"github.com/nvidia/doca-platform/cmd/hostagent/options"
	provcertificate "github.com/nvidia/doca-platform/internal/provisioning/utils/certificate"
	"github.com/nvidia/doca-platform/internal/provisioning/utils/certificate/bootstrap"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	clientset "k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/util/certificate"
	nodeutil "k8s.io/component-helpers/node/util"
	"k8s.io/klog/v2"
)

const (
	TLSTimeOut = 30 * time.Second
)

// GetConfigOrDie returns a config for client. It will try to use the in-cluster config first,
// and if that fails, it will try to use the kubeconfig and bootstrap files.
func GetConfigOrDie(opt *options.HostAgentFlags) *restclient.Config {
	if opt.KubeconfigPath != "" || opt.BootstrapPath != "" {
		cfg, cm, err := BuildHostAgentClientConfig(context.Background(), opt)
		if err != nil {
			klog.Fatalf("Failed to use kubeconfig or bootstrap, err: %v", err)
		}
		// wait until cert is available
		err = wait.PollUntilContextCancel(context.TODO(), 10*time.Second, true, func(ctx context.Context) (bool, error) {
			if cm.Current() != nil {
				klog.Infof("Using kubeconfig at %s or bootstrap kubeconfig at %s", opt.KubeconfigPath, opt.BootstrapPath)
				return true, nil
			}
			klog.Info("Cert is not available, waiting...")
			return false, nil
		})
		if err != nil {
			klog.Fatalf("Failed to wait for cert: %v", err)
		}
		return cfg
	}
	cfg, err := restclient.InClusterConfig()
	if err != nil {
		klog.Fatalf("Failed to get in-cluster config: %v", err)
	}
	klog.V(2).Infof("Using in-cluster config")
	return cfg
}

func BuildHostAgentClientConfig(ctx context.Context, opt *options.HostAgentFlags) (*restclient.Config, certificate.Manager, error) {
	certConfig, clientConfig, err := bootstrap.LoadClientConfig(opt.KubeconfigPath, opt.BootstrapPath, opt.CertDir, "host-agent-client")
	if err != nil {
		return nil, nil, err
	}

	// use the correct content type for cert rotation, but don't set QPS
	setContentTypeForClient(certConfig, opt.ContentType)

	kubeClientConfigOverrides(opt, clientConfig)

	hostName, err := nodeutil.GetHostname("")
	if err != nil {
		return nil, nil, err
	}
	nodeName := types.NodeName(hostName)
	clientCertificateManager, err := buildClientCertificateManager(certConfig, clientConfig, opt.CertDir, nodeName)
	if err != nil {
		return nil, nil, err
	}
	// the rotating transport will use the cert from the cert manager instead of these files
	transportConfig := restclient.AnonymousClientConfig(clientConfig)
	_, err = provcertificate.UpdateTransport(wait.NeverStop, transportConfig, clientCertificateManager, TLSTimeOut)
	if err != nil {
		return nil, nil, err
	}

	klog.V(2).InfoS("Starting client certificate rotation")
	clientCertificateManager.Start()

	return transportConfig, clientCertificateManager, nil
}

// setContentTypeForClient sets the appropriate content type into the rest config
// and handles defaulting AcceptContentTypes based on that input.
func setContentTypeForClient(cfg *restclient.Config, contentType string) {
	if len(contentType) == 0 {
		return
	}
	cfg.ContentType = contentType
	switch contentType {
	case runtime.ContentTypeProtobuf:
		cfg.AcceptContentTypes = strings.Join([]string{runtime.ContentTypeProtobuf, runtime.ContentTypeJSON}, ",")
	default:
		// otherwise let the rest client perform defaulting
	}
}

func kubeClientConfigOverrides(s *options.HostAgentFlags, clientConfig *restclient.Config) {
	setContentTypeForClient(clientConfig, s.ContentType)
	// Override kubeconfig qps/burst settings from flags
	clientConfig.QPS = float32(s.KubeAPIQPS)
	clientConfig.Burst = s.KubeAPIBurst
}

// buildClientCertificateManager creates a certificate manager that will use certConfig to request a client certificate
// if no certificate is available, or the most recent clientConfig (which is assumed to point to the cert that the manager will
// write out).
func buildClientCertificateManager(certConfig, clientConfig *restclient.Config, certDir string, nodeName types.NodeName) (certificate.Manager, error) {
	newClientsetFn := func(current *tls.Certificate) (clientset.Interface, error) {
		// If we have a valid certificate, use that to fetch CSRs. Otherwise use the bootstrap
		// credentials. In the future it would be desirable to change the behavior of bootstrap
		// to always fall back to the external bootstrap credentials when such credentials are
		// provided by a fundamental trust system like cloud VM identity or an HSM module.
		config := certConfig
		if current != nil {
			config = clientConfig
		}
		return clientset.NewForConfig(config)
	}
	return provcertificate.NewCertificateManager(
		certDir,
		nodeName,
		clientConfig.CertFile,
		clientConfig.KeyFile,
		newClientsetFn,
		"host-agent-client",
		fmt.Sprintf("dpf:host-agent:%s", nodeName),
		[]string{"dpf:host-agent"},
	)
}
