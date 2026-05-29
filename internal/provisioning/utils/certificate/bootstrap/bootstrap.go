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

package bootstrap

import (
	"fmt"
	"os"
	"time"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/client-go/transport"
	certutil "k8s.io/client-go/util/cert"
	"k8s.io/client-go/util/certificate"
	"k8s.io/klog/v2"
)

// LoadClientConfig tries to load the appropriate client config for retrieving certs and for use by users.
// If bootstrapPath is empty, only kubeconfigPath is checked. If bootstrap path is set and the contents
// of kubeconfigPath are valid, both certConfig and userConfig will point to that file. Otherwise the
// kubeconfigPath on disk is populated based on bootstrapPath but pointing to the location of the client cert
// in certDir. This preserves the historical behavior of bootstrapping where on subsequent restarts the
// most recent client cert is used to request new client certs instead of the initial token.
func LoadClientConfig(kubeconfigPath, bootstrapPath, certDir, pairNamePrefix string) (certConfig, userConfig *restclient.Config, err error) {
	if len(bootstrapPath) == 0 {
		clientConfig, err := loadRESTClientConfig(kubeconfigPath)
		if err != nil {
			return nil, nil, fmt.Errorf("unable to load kubeconfig: %v", err)
		}
		klog.V(2).InfoS("No bootstrapping requested, will use kubeconfig")
		return clientConfig, restclient.CopyConfig(clientConfig), nil
	}

	store, err := certificate.NewFileStore(pairNamePrefix, certDir, certDir, "", "")
	if err != nil {
		return nil, nil, fmt.Errorf("unable to build bootstrap cert store: %v", err)
	}

	ok, err := isClientConfigStillValid(kubeconfigPath)
	if err != nil {
		return nil, nil, err
	}

	// use the current client config
	if ok {
		clientConfig, err := loadRESTClientConfig(kubeconfigPath)
		if err != nil {
			return nil, nil, fmt.Errorf("unable to load kubeconfig: %v", err)
		}
		klog.V(2).InfoS("Current kubeconfig file contents are still valid, no bootstrap necessary")
		return clientConfig, restclient.CopyConfig(clientConfig), nil
	}

	bootstrapClientConfig, err := loadRESTClientConfig(bootstrapPath)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to load bootstrap kubeconfig: %v", err)
	}

	clientConfig := restclient.AnonymousClientConfig(bootstrapClientConfig)
	pemPath := store.CurrentPath()
	clientConfig.KeyFile = pemPath
	clientConfig.CertFile = pemPath
	if err := writeKubeconfigFromBootstrapping(clientConfig, kubeconfigPath, pemPath); err != nil {
		return nil, nil, err
	}
	klog.V(2).InfoS("Use the bootstrap credentials to request a cert, and set kubeconfig to point to the certificate dir")
	return bootstrapClientConfig, clientConfig, nil
}

func writeKubeconfigFromBootstrapping(bootstrapClientConfig *restclient.Config, kubeconfigPath, pemPath string) error {
	// Get the CA data from the bootstrap client config.
	caFile, caData := bootstrapClientConfig.CAFile, []byte{}
	if len(caFile) == 0 {
		caData = bootstrapClientConfig.CAData
	}

	cluster := &clientcmdapi.Cluster{
		Server:                   bootstrapClientConfig.Host,
		InsecureSkipTLSVerify:    bootstrapClientConfig.Insecure,
		CertificateAuthority:     caFile,
		CertificateAuthorityData: caData,
	}

	// Preserve proxy-url from the bootstrap kubeconfig so the cert kubeconfig
	// continues to route through the same proxy (e.g. hostagent forward proxy).
	if bootstrapClientConfig.Proxy != nil {
		proxyReq, err := bootstrapClientConfig.Proxy(nil)
		if err != nil {
			return fmt.Errorf("resolving proxy URL from bootstrap kubeconfig: %w", err)
		}
		if proxyReq != nil {
			cluster.ProxyURL = proxyReq.String()
		}
	}

	// Build resulting kubeconfig.
	kubeconfigData := clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{"default-cluster": cluster},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{"default-auth": {
			ClientCertificate: pemPath,
			ClientKey:         pemPath,
		}},
		Contexts: map[string]*clientcmdapi.Context{"default-context": {
			Cluster:   "default-cluster",
			AuthInfo:  "default-auth",
			Namespace: "default",
		}},
		CurrentContext: "default-context",
	}

	// Marshal to disk
	return clientcmd.WriteToFile(kubeconfigData, kubeconfigPath)
}

func loadRESTClientConfig(kubeconfig string) (*restclient.Config, error) {
	// Load structured kubeconfig data from the given path.
	loader := &clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfig}
	loadedConfig, err := loader.Load()
	if err != nil {
		return nil, err
	}
	// Flatten the loaded data to a particular restclient.Config based on the current context.
	return clientcmd.NewNonInteractiveClientConfig(
		*loadedConfig,
		loadedConfig.CurrentContext,
		&clientcmd.ConfigOverrides{},
		loader,
	).ClientConfig()
}

// isClientConfigStillValid checks the provided kubeconfig to see if it has a valid
// client certificate. It returns true if the kubeconfig is valid, or an error if bootstrapping
// should stop immediately.
func isClientConfigStillValid(kubeconfigPath string) (bool, error) {
	_, err := os.Stat(kubeconfigPath)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("error reading existing bootstrap kubeconfig %s: %v", kubeconfigPath, err)
	}
	bootstrapClientConfig, err := loadRESTClientConfig(kubeconfigPath)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("unable to read existing bootstrap client config from %s: %v", kubeconfigPath, err))
		return false, nil
	}
	transportConfig, err := bootstrapClientConfig.TransportConfig()
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("unable to load transport configuration from existing bootstrap client config read from %s: %v", kubeconfigPath, err))
		return false, nil
	}
	// has side effect of populating transport config data fields
	if _, err := transport.TLSConfigFor(transportConfig); err != nil {
		utilruntime.HandleError(fmt.Errorf("unable to load TLS configuration from existing bootstrap client config read from %s: %v", kubeconfigPath, err))
		return false, nil
	}
	certs, err := certutil.ParseCertsPEM(transportConfig.TLS.CertData)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("unable to load TLS certificates from existing bootstrap client config read from %s: %v", kubeconfigPath, err))
		return false, nil
	}
	if len(certs) == 0 {
		utilruntime.HandleError(fmt.Errorf("unable to read TLS certificates from existing bootstrap client config read from %s: %v", kubeconfigPath, err))
		return false, nil
	}
	now := time.Now()
	for _, cert := range certs {
		if now.After(cert.NotAfter) {
			utilruntime.HandleError(fmt.Errorf("part of the existing bootstrap client certificate in %s is expired: %v", kubeconfigPath, cert.NotAfter))
			return false, nil
		}
	}
	return true, nil
}
