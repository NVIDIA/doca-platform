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

package nvidia

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/clustermanager/kamaji/encryptionconfig"
	kamajiv1 "github.com/nvidia/doca-platform/third_party/forked/github.com/clastix/kamaji/api/v1alpha1"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// encryptionConfigLastConfigInfoMetric reports the hash of the loaded encryption configuration.
	encryptionConfigLastConfigInfoMetric = "apiserver_encryption_config_controller_last_config_info"
	// apiserverDefaultSecurePort is the default kube-apiserver HTTPS port.
	apiserverDefaultSecurePort = int32(6443)
	// apiserverMetricsPath is the kube-apiserver metrics endpoint path.
	apiserverMetricsPath = "/metrics"
	// apiserverMetricsAdminKubeconfigKey is the in-cluster admin kubeconfig key in the Kamaji Secret.
	apiserverMetricsAdminKubeconfigKey = "admin.svc"
	// apiserverMetricsClientTimeout bounds each metrics request.
	apiserverMetricsClientTimeout = 10 * time.Second
	// apiserverMetricsMaxResponseBytes bounds each metrics response to 10 MiB.
	apiserverMetricsMaxResponseBytes = int64(10 * 1024 * 1024)
)

type metricsReloadVerifier struct {
	client client.Client
}

type podMetricsEndpoint struct {
	key     string
	address string
	port    int32
}

// VerifyReload checks that every ready kube-apiserver pod has loaded the expected encryption config.
func (v *metricsReloadVerifier) VerifyReload(ctx context.Context, dc *provisioningv1.DPUCluster, config encryptionconfig.StaticKey) (bool, error) {
	logger := log.FromContext(ctx).WithValues(
		"dpuCluster", klog.KObj(dc),
		"rotationState", config.Phase())
	logger.V(2).Info("checking kube-apiserver encryption config reload")

	expectedHash := config.ConfigHash()
	endpoints, err := v.apiserverPodMetricsEndpoints(ctx, dc)
	if err != nil {
		return false, err
	}
	if len(endpoints) == 0 {
		logger.V(2).Info("waiting to verify kube-apiserver encryption config reload", "reason", "NoStableReadyEndpoints")
		return false, nil
	}
	tlsConfig, err := v.metricsTLSConfig(ctx, dc)
	if err != nil {
		return false, err
	}
	httpClient := &http.Client{
		Timeout: apiserverMetricsClientTimeout,
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return fmt.Errorf("redirects are not allowed for kube-apiserver metrics")
		},
	}
	verified, err := scrapeReloadStateForAllEndpoints(ctx, httpClient, endpoints, expectedHash)
	if err != nil {
		return false, err
	}
	if !verified {
		logger.V(2).Info("waiting for kube-apiserver instances to reload encryption config")
		return false, nil
	}
	// Re-read the pod set after scraping to close the race where a rollout replaces an
	// API server just after we verified the previous pod. Advancing is safe only if the
	// same ready pod identities were present for the whole verification window.
	latestEndpoints, err := v.apiserverPodMetricsEndpoints(ctx, dc)
	if err != nil {
		return false, err
	}
	if !sameMetricsEndpoints(endpoints, latestEndpoints) {
		logger.V(2).Info("waiting to verify kube-apiserver encryption config reload", "reason", "APIServerPodsChanged")
		return false, nil
	}
	logger.V(2).Info("verified kube-apiserver encryption config reload", "replicas", len(endpoints))
	return true, nil
}

// apiserverPodMetricsEndpoints returns direct pod metrics endpoints after proving the Deployment is stable.
func (v *metricsReloadVerifier) apiserverPodMetricsEndpoints(ctx context.Context, dc *provisioningv1.DPUCluster) ([]podMetricsEndpoint, error) {
	tcp := &kamajiv1.TenantControlPlane{}
	if err := v.client.Get(ctx, kamajiTCPName(dc), tcp); err != nil {
		return nil, fmt.Errorf("get TenantControlPlane for kube-apiserver deployment status: %w", err)
	}
	port := apiserverMetricsPort(tcp)
	deploymentStatus := tcp.Status.Kubernetes.Deployment
	if deploymentStatus.Name == "" || deploymentStatus.Namespace == "" {
		return nil, nil
	}
	deployment := &appsv1.Deployment{}
	if err := v.client.Get(ctx, types.NamespacedName{Name: deploymentStatus.Name, Namespace: deploymentStatus.Namespace}, deployment); err != nil {
		return nil, fmt.Errorf("get kube-apiserver Deployment: %w", err)
	}
	if !deploymentStable(deployment) {
		return nil, nil
	}
	selector, err := metav1.LabelSelectorAsSelector(deployment.Spec.Selector)
	if err != nil {
		return nil, fmt.Errorf("parse kube-apiserver Deployment selector: %w", err)
	}
	podList := &corev1.PodList{}
	if err := v.client.List(ctx, podList, client.InNamespace(deployment.Namespace), client.MatchingLabelsSelector{Selector: selector}); err != nil {
		return nil, fmt.Errorf("list kube-apiserver pods: %w", err)
	}
	expectedReplicas := int32(1)
	if deployment.Spec.Replicas != nil {
		expectedReplicas = *deployment.Spec.Replicas
	}
	if int32(len(podList.Items)) != expectedReplicas {
		return nil, nil
	}
	endpoints := make([]podMetricsEndpoint, 0, len(podList.Items))
	for _, pod := range podList.Items {
		if !podReady(pod) || pod.Status.PodIP == "" {
			return nil, nil
		}
		endpoints = append(endpoints, podMetricsEndpoint{
			key:     string(pod.UID),
			address: pod.Status.PodIP,
			port:    port,
		})
	}
	return endpoints, nil
}

// apiserverMetricsPort returns the configured API server port or the Kamaji default when it is not set.
func apiserverMetricsPort(tcp *kamajiv1.TenantControlPlane) int32 {
	if tcp.Spec.NetworkProfile.Port == 0 {
		return apiserverDefaultSecurePort
	}
	return tcp.Spec.NetworkProfile.Port
}

// metricsTLSConfig returns TLS client settings from the Kamaji admin kubeconfig.
func (v *metricsReloadVerifier) metricsTLSConfig(ctx context.Context, dc *provisioningv1.DPUCluster) (*tls.Config, error) {
	tcp := &kamajiv1.TenantControlPlane{}
	if err := v.client.Get(ctx, kamajiTCPName(dc), tcp); err != nil {
		return nil, fmt.Errorf("get TenantControlPlane for kube-apiserver admin kubeconfig: %w", err)
	}
	adminSecretName := tcp.Status.KubeConfig.Admin.SecretName
	if adminSecretName == "" {
		return nil, fmt.Errorf("TenantControlPlane %s/%s does not report admin kubeconfig Secret", tcp.Namespace, tcp.Name)
	}
	adminSecret := &corev1.Secret{}
	if err := v.client.Get(ctx, types.NamespacedName{
		Name:      adminSecretName,
		Namespace: dc.Namespace,
	}, adminSecret); err != nil {
		return nil, fmt.Errorf("get kube-apiserver admin kubeconfig Secret: %w", err)
	}
	kubeconfigData, ok := adminSecret.Data[apiserverMetricsAdminKubeconfigKey]
	if !ok {
		return nil, fmt.Errorf("kube-apiserver admin kubeconfig Secret %s/%s does not contain key %q",
			adminSecret.Namespace, adminSecret.Name, apiserverMetricsAdminKubeconfigKey)
	}
	restConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigData)
	if err != nil {
		return nil, fmt.Errorf("parse kube-apiserver admin kubeconfig Secret %s/%s key %q: %w",
			adminSecret.Namespace, adminSecret.Name, apiserverMetricsAdminKubeconfigKey, err)
	}
	cert, err := tls.X509KeyPair(restConfig.CertData, restConfig.KeyData)
	if err != nil {
		return nil, fmt.Errorf("parse kube-apiserver admin client certificate Secret %s/%s key %q: %w",
			adminSecret.Namespace, adminSecret.Name, apiserverMetricsAdminKubeconfigKey, err)
	}
	roots := x509.NewCertPool()
	if len(restConfig.CAData) == 0 || !roots.AppendCertsFromPEM(restConfig.CAData) {
		return nil, fmt.Errorf("parse kube-apiserver CA from admin kubeconfig Secret %s/%s key %q",
			adminSecret.Namespace, adminSecret.Name, apiserverMetricsAdminKubeconfigKey)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      roots,
		ServerName:   apiserverMetricsServerName(dc),
	}, nil
}

func apiserverMetricsServerName(dc *provisioningv1.DPUCluster) string {
	nn := kamajiTCPName(dc)
	return fmt.Sprintf("%s.%s.svc", nn.Name, nn.Namespace)
}

type scrapeReloadStateResult struct {
	endpoint podMetricsEndpoint
	verified bool
	err      error
}

func scrapeReloadStateForAllEndpoints(ctx context.Context, httpClient *http.Client, endpoints []podMetricsEndpoint, expectedHash string) (bool, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	results := make(chan scrapeReloadStateResult, len(endpoints))
	var wg sync.WaitGroup
	for _, endpoint := range endpoints {
		wg.Add(1)
		go func(endpoint podMetricsEndpoint) {
			defer wg.Done()
			verified, err := scrapeReloadState(ctx, httpClient, endpoint, expectedHash)
			results <- scrapeReloadStateResult{
				endpoint: endpoint,
				verified: verified,
				err:      err,
			}
		}(endpoint)
	}
	go func() {
		wg.Wait()
		close(results)
	}()

	allVerified := true
	var scrapeErr error
	for result := range results {
		if result.err != nil {
			cancel()
			if scrapeErr == nil {
				scrapeErr = fmt.Errorf("scrape kube-apiserver metrics from pod %s: %w", result.endpoint.key, result.err)
			}
			continue
		}
		if !result.verified {
			allVerified = false
		}
	}
	if scrapeErr != nil {
		return false, scrapeErr
	}
	return allVerified, nil
}

// scrapeReloadState scrapes one endpoint and checks the encryption config hash.
func scrapeReloadState(ctx context.Context, httpClient *http.Client, endpoint podMetricsEndpoint, expectedHash string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+net.JoinHostPort(endpoint.address, strconv.Itoa(int(endpoint.port)))+apiserverMetricsPath, nil)
	if err != nil {
		return false, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return false, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("unexpected kube-apiserver metrics status %s", resp.Status)
	}
	return configHashFromMetrics(resp.Body, expectedHash)
}

// configHashFromMetrics reports whether the metrics contain the expected loaded encryption config hash.
func configHashFromMetrics(metrics io.Reader, expectedHash string) (bool, error) {
	limited := &io.LimitedReader{
		R: metrics,
		N: apiserverMetricsMaxResponseBytes + 1,
	}
	scanner := bufio.NewScanner(limited)
	scanner.Buffer(make([]byte, 64*1024), int(apiserverMetricsMaxResponseBytes))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || !isMetricLine(line, encryptionConfigLastConfigInfoMetric) {
			continue
		}
		if strings.Contains(line, `hash="`+expectedHash+`"`) {
			return true, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return false, fmt.Errorf("scan kube-apiserver metrics: %w", err)
	}
	if limited.N == 0 {
		return false, fmt.Errorf("kube-apiserver metrics response exceeds %d bytes", apiserverMetricsMaxResponseBytes)
	}
	return false, nil
}

// isMetricLine reports whether the text line contains a sample for metricName.
func isMetricLine(line, metricName string) bool {
	if !strings.HasPrefix(line, metricName) {
		return false
	}
	if len(line) == len(metricName) {
		return true
	}
	next := line[len(metricName)]
	return next == '{' || next == ' ' || next == '\t'
}

// deploymentStable reports whether the kube-apiserver Deployment has no rollout or unavailable pods.
func deploymentStable(deployment *appsv1.Deployment) bool {
	replicas := int32(1)
	if deployment.Spec.Replicas != nil {
		replicas = *deployment.Spec.Replicas
	}
	return deployment.Status.ObservedGeneration >= deployment.Generation &&
		deployment.Status.UpdatedReplicas == replicas &&
		deployment.Status.ReadyReplicas == replicas &&
		deployment.Status.AvailableReplicas == replicas &&
		deployment.Status.UnavailableReplicas == 0
}

// podReady reports whether a pod is running and ready.
func podReady(pod corev1.Pod) bool {
	if pod.Status.Phase != corev1.PodRunning {
		return false
	}
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

// sameMetricsEndpoints reports whether two endpoint snapshots refer to the same ready API server pods.
func sameMetricsEndpoints(a, b []podMetricsEndpoint) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]podMetricsEndpoint{}
	for _, endpoint := range a {
		seen[endpoint.key] = endpoint
	}
	for _, endpoint := range b {
		got, ok := seen[endpoint.key]
		if !ok || got.address != endpoint.address || got.port != endpoint.port {
			return false
		}
	}
	return true
}
