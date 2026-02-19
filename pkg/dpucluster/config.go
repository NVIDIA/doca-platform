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

package dpucluster

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdv1 "k8s.io/client-go/tools/clientcmd/api"
	clientcertutil "k8s.io/client-go/util/cert"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ClientOptions holds the options for the client.
type ClientOptions struct {
	// rest config options
	timeout       time.Duration
	userAgent     string
	updateHost    string
	skipVerifyTLS bool

	// client options
	// scheme is the scheme used for the client.
	scheme *runtime.Scheme
}

func (co *ClientOptions) apply(opts []ClientOption) *ClientOptions {
	for _, o := range opts {
		o.Apply(co)
	}
	return co
}

type ClientOption interface {
	Apply(options *ClientOptions)
}

// ClientOptionConfigHost overrides the host used for the REST config.
type ClientOptionConfigHost struct {
	Host string
}

func (o ClientOptionConfigHost) Apply(options *ClientOptions) {
	options.updateHost = o.Host
}

// ClientOptionSkipVerifyTLS is the option to skip TLS verification for the REST config.
type ClientOptionSkipVerifyTLS struct {
	SkipVerifyTLS bool
}

func (o ClientOptionSkipVerifyTLS) Apply(options *ClientOptions) {
	options.skipVerifyTLS = o.SkipVerifyTLS
}

// ClientOptionTimeout is the timeout used for the REST config, client and cache.
type ClientOptionTimeout struct {
	Timeout time.Duration
}

func (o ClientOptionTimeout) Apply(options *ClientOptions) {
	options.timeout = o.Timeout
}

// ClientOptionUserAgent is the user agent used for the REST config, client and cache.
type ClientOptionUserAgent struct {
	UserAgent string
}

func (o ClientOptionUserAgent) Apply(options *ClientOptions) {
	options.userAgent = o.UserAgent
}

// ClientOptionScheme is the scheme used for the client.
type ClientOptionScheme struct {
	Scheme *runtime.Scheme
}

func (o ClientOptionScheme) Apply(options *ClientOptions) {
	options.scheme = o.Scheme
}

// Config hold the cluster configuration.
// It provides methods to get the client, clientset, and rest config for the cluster.
type Config struct {
	// Cluster is the DPU cluster for which the config is being fetched.
	Cluster *provisioningv1.DPUCluster
	// hostClient is the client for the host cluster. It is used to fetch the kubeconfig secret.
	hostClient      client.Reader
	kubeconfig      *clientcmdv1.Config
	kubeconfigBytes []byte
	clientConfig    clientcmd.OverridingClientConfig
}

// NewConfig returns a new Config.
func NewConfig(c client.Reader, cluster *provisioningv1.DPUCluster) *Config {
	return &Config{
		Cluster:    cluster,
		hostClient: c,
	}
}

func (cc *Config) ClusterNamespaceName() string {
	return fmt.Sprintf("%s-%s", cc.Cluster.Namespace, cc.Cluster.Name)
}

// restConfig returns the rest config for the cluster.
func (cc *Config) restConfig(ctx context.Context, opts ...ClientOption) (*rest.Config, error) {
	options := (&ClientOptions{}).apply(opts)

	_, err := cc.Kubeconfig(ctx)
	if err != nil {
		return nil, err
	}

	restConfig, err := cc.clientConfig.ClientConfig()
	if err != nil {
		return nil, err
	}

	makeOptions(restConfig, options)

	return restConfig, nil
}

func makeOptions(config *rest.Config, opts *ClientOptions) {
	config.UserAgent = opts.userAgent
	config.Timeout = opts.timeout
	config.TLSClientConfig.Insecure = opts.skipVerifyTLS

	// If we're skipping TLS verification, clear any CA certificate data
	if opts.skipVerifyTLS {
		config.TLSClientConfig.CAData = nil
		config.TLSClientConfig.CAFile = ""
	}

	if opts.updateHost != "" {
		config.Host = opts.updateHost
	}
}

// Kubeconfig returns the kubeconfig for the cluster.
func (cc *Config) Kubeconfig(ctx context.Context) (*clientcmdv1.Config, error) {
	if cc.kubeconfig != nil {
		return cc.kubeconfig, nil
	}
	if cc.clientConfig == nil {
		if err := cc.getClientConfig(ctx); err != nil {
			return nil, err
		}
	}

	adminConfig, err := cc.clientConfig.RawConfig()
	if err != nil {
		return nil, fmt.Errorf("error while getting raw config: %w", err)
	}

	if len(adminConfig.AuthInfos) != 1 {
		return nil, fmt.Errorf("secret not in the expected format: auth info list should have one member")
	}
	if len(adminConfig.Clusters) != 1 {
		return nil, fmt.Errorf("secret not in the expected format: cluster list should have one member")
	}

	cc.kubeconfig = &adminConfig
	return cc.kubeconfig, nil
}

// CACertHashes returns the hashes of the CAs contained in the cluster config.
// These hashes are used to authenticate with kubelet when joining nodes to the cluster.
// The reference implementation for this function is from kubeadm:
// https://github.com/kubernetes/kubernetes/blob/78cb3862f11225135afdf76f3424e2d7b33104c7/cmd/kubeadm/app/cmd/util/join.go#L52
func (cc *Config) CACertHashes(ctx context.Context) ([]string, error) {
	if cc.clientConfig == nil {
		if err := cc.getClientConfig(ctx); err != nil {
			return nil, err
		}
	}
	if len(cc.kubeconfig.Clusters) != 1 {
		return nil, fmt.Errorf("cluster list should have exactly one member")
	}
	var cluster *clientcmdv1.Cluster
	for _, c := range cc.kubeconfig.Clusters {
		cluster = c
		break
	}

	var caCerts []*x509.Certificate
	var err error
	if cluster.CertificateAuthorityData == nil {
		return nil, fmt.Errorf("no CA certificate found in kubeconfig")
	}
	caCerts, err = clientcertutil.ParseCertsPEM(cluster.CertificateAuthorityData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CA certificate from kubeconfig %w", err)
	}

	// hash the CA certs and include their public key pins as trusted values
	caHashes := make([]string, 0, len(caCerts))
	for _, caCert := range caCerts {
		caHashes = append(caHashes, hashCert(caCert))
	}

	return caHashes, nil
}

func hashCert(cert *x509.Certificate) string {
	spkiHash := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	return "sha256:" + strings.ToLower(hex.EncodeToString(spkiHash[:]))
}

// Server returns the server cluster.
func (cc *Config) Server(ctx context.Context) (string, error) {
	if cc.clientConfig == nil {
		if err := cc.getClientConfig(ctx); err != nil {
			return "", err
		}
	}
	restconfig, err := cc.clientConfig.ClientConfig()
	if err != nil {
		return "", fmt.Errorf("error while getting client config: %w", err)
	}
	if restconfig.Host == "" {
		return "", fmt.Errorf("server name is empty for DPUCluster %s", cc.Cluster.Name)
	}
	return restconfig.Host, nil
}

func (cc *Config) getClientConfig(ctx context.Context) error {
	// Get the control plane secret using the cluster's `spec.kubeconfig` field.
	secret := &corev1.Secret{}
	key := client.ObjectKey{Namespace: cc.Cluster.Namespace, Name: cc.Cluster.Spec.Kubeconfig}
	if err := cc.hostClient.Get(ctx, key, secret); err != nil {
		return err
	}
	kubeconfigBytes, ok := secret.Data["super-admin.conf"]
	if !ok {
		return fmt.Errorf("secret %v/%v not in the expected format: data.super-admin.conf not found", secret.Namespace, secret.Name)
	}
	clientConfig, err := clientcmd.NewClientConfigFromBytes(kubeconfigBytes)
	if err != nil {
		return fmt.Errorf("error while getting client config: %w", err)
	}
	cc.clientConfig = clientConfig
	cc.kubeconfigBytes = kubeconfigBytes
	return nil
}

// Client returns a new client for the cluster.
func (cc *Config) Client(ctx context.Context, opts ...ClientOption) (client.Client, error) {
	restConfig, err := cc.restConfig(ctx, opts...)
	if err != nil {
		return nil, err
	}

	options := (&ClientOptions{}).apply(opts)

	newClient, err := client.New(restConfig, client.Options{Scheme: options.scheme})
	if err != nil {
		return nil, err
	}
	return newClient, nil
}

// Clientset returns a new clientset for the cluster.
func (cc *Config) Clientset(ctx context.Context, opts ...ClientOption) (*kubernetes.Clientset, []byte, error) {
	restConfig, err := cc.restConfig(ctx, opts...)
	if err != nil {
		return nil, nil, err
	}

	newClient, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, nil, err
	}
	return newClient, cc.kubeconfigBytes, nil
}

// GetConfigs returns a list of Configs for all DPU clusters.
func GetConfigs(ctx context.Context, c client.Client) ([]*Config, error) {
	dpuClusters := &provisioningv1.DPUClusterList{}
	err := c.List(ctx, dpuClusters)
	if err != nil {
		return nil, err
	}

	configs := make([]*Config, 0, len(dpuClusters.Items))
	for _, cluster := range dpuClusters.Items {
		config := NewConfig(c, &cluster)
		configs = append(configs, config)
	}
	return configs, nil
}
