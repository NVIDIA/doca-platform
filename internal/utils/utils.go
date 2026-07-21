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

package utils

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/dpucluster"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ResourcesExceedError is an error returned by GetAllocatableResources() that indicates that the reserved resources
// exceed the total resources
type ResourcesExceedError struct {
	// AdditionalResourcesRequired are the extra resources needed so that the they can fit the total resources
	AdditionalResourcesRequired []string
	msg                         string
}

// Error is the error message of that error struct
func (e *ResourcesExceedError) Error() string { return e.msg }

// GetAllocatableResources returns the available resources after subtracting the reserved ones from the total ones
func GetAllocatableResources(total corev1.ResourceList, reserved corev1.ResourceList) (corev1.ResourceList, error) {
	if total == nil {
		return nil, nil
	}
	availableResources := make(corev1.ResourceList)
	for k, v := range total {
		availableResources[k] = v
	}

	for resourceName, quantity := range reserved {
		totalResource := resource.Quantity{}
		if resource, ok := availableResources[resourceName]; ok {
			totalResource = resource
		}

		totalResource.Sub(quantity)
		availableResources[resourceName] = totalResource
	}

	additionalResourcesRequired := []string{}
	for resourceName, quantity := range availableResources {
		if quantity.Sign() < 0 {
			quantity.Neg()
			additionalResourcesRequired = append(additionalResourcesRequired, fmt.Sprintf("%s: %s", resourceName, quantity.String()))
		}
	}

	if len(additionalResourcesRequired) > 0 {
		return nil, &ResourcesExceedError{
			AdditionalResourcesRequired: additionalResourcesRequired,
			msg:                         "error while calculating allocatable resources, reserved resources don't fit in total resources",
		}
	}

	return availableResources, nil
}

// LabelSelectorAsSelector is a wrapper around metav1.LabelSelectorAsSelector()
// to not select labels.Nothing() when the input is nil.
// If the input is nil, it returns labels.Everything().
func LabelSelectorAsSelector(labelSelector *metav1.LabelSelector) (labels.Selector, error) {
	if labelSelector == nil {
		return labels.Everything(), nil
	}

	return metav1.LabelSelectorAsSelector(labelSelector)
}

// GetDPFOperatorConfig returns the DPFOperatorConfig object. It returns an error if there is more
// than one DPFOperatorConfig object or if there is no DPFOperatorConfig object.
func GetDPFOperatorConfig(ctx context.Context, c client.Client) (*operatorv1.DPFOperatorConfig, error) {
	dpfOperatorConfigList := operatorv1.DPFOperatorConfigList{}
	if err := c.List(ctx, &dpfOperatorConfigList); err != nil {
		return nil, fmt.Errorf("listing DPFOperatorConfigs: %w", err)
	}
	if len(dpfOperatorConfigList.Items) == 0 || len(dpfOperatorConfigList.Items) > 1 {
		return nil, fmt.Errorf("exactly one DPFOperatorConfig must exist")
	}
	return &dpfOperatorConfigList.Items[0], nil
}

// GetOOBBridgeName returns the out-of-band bridge name from the cluster DPFOperatorConfig.
func GetOOBBridgeName(ctx context.Context, c client.Client) (string, error) {
	config, err := GetDPFOperatorConfig(ctx, c)
	if err != nil {
		return "", err
	}
	return config.Spec.Networking.GetDPUNodeOOBBridgeName(), nil
}

// GetMatchingDPUClusters returns a list of DPUCluster Configs from the given list that match the given selector.
// In case no selector is provided, it returns the entire list.
func GetMatchingDPUClusters(dpuClusters []*dpucluster.Config, clusterSelector *metav1.LabelSelector) ([]*dpucluster.Config, error) {
	if clusterSelector == nil {
		return dpuClusters, nil
	}

	matchingClusters := make([]*dpucluster.Config, 0)
	selector, err := LabelSelectorAsSelector(clusterSelector)
	if err != nil {
		return matchingClusters, fmt.Errorf("failed to parse label selector: %w", err)
	}
	for i, dpuCluster := range dpuClusters {
		if !selector.Matches(labels.Set(dpuCluster.Cluster.Labels)) {
			continue
		}
		matchingClusters = append(matchingClusters, dpuClusters[i])
	}
	return matchingClusters, nil
}

// EnsureNamespace ensures the namespace exists in the cluster by creating it if it does not exist.
func EnsureNamespace(ctx context.Context, c client.Client, namespace string) error {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}
	if err := c.Create(ctx, ns); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return err
		}
	}
	return nil
}

// DownloadFile downloads a file from a URL to a destination file.
func DownloadFile(ctx context.Context, url string, dst string, fileMode os.FileMode) error {
	return DownloadFileWithClient(ctx, http.DefaultClient, url, dst, fileMode)
}

// DownloadFileWithClient behaves like DownloadFile but performs the request with the provided
// HTTP client. This lets callers supply a custom TLS configuration (e.g. RootCAs built from the
// DPF CA trust bundle) when downloading from an HTTPS endpoint such as the bfb-registry.
func DownloadFileWithClient(ctx context.Context, httpClient *http.Client, url string, dst string, fileMode os.FileMode) error {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	if _, err := os.Stat(dst); err == nil {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	tempFile, err := os.CreateTemp(filepath.Dir(dst), filepath.Base(dst)+"-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tempFile.Name()) //nolint: errcheck

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to get: %s status: %d", url, resp.StatusCode)
	}
	defer resp.Body.Close() //nolint: errcheck

	expectedSize := resp.ContentLength
	totalWritten, err := copyBodyToFile(ctx, tempFile, resp.Body)
	if err != nil {
		return err
	}

	// Validate downloaded file size against Content-Length header from this GET response.
	// This detects truncated downloads caused by network failures or connection drops.
	// Only validate when Content-Length is positive (known size).
	// Skip validation when Content-Length is -1 (not set) or 0 (empty) since we can't reliably verify.
	// If validation fails, the function returns an error and the deferred os.Remove() cleans up
	// the temporary file, ensuring no partial/corrupted file is left at the destination.
	if expectedSize > 0 && totalWritten != expectedSize {
		return fmt.Errorf("downloaded file size mismatch: expected %d bytes, got %d bytes", expectedSize, totalWritten)
	}

	// Close the temp file before renaming
	if err := tempFile.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempFile.Name(), dst); err != nil {
		return err
	}
	if err := os.Chmod(dst, fileMode); err != nil {
		return err
	}
	return nil
}

// copyBodyToFile streams src into dst, honoring ctx cancellation between reads, and returns the
// total number of bytes written.
func copyBodyToFile(ctx context.Context, dst io.Writer, src io.Reader) (int64, error) {
	var totalWritten int64
	buf := make([]byte, 128*1024*1024)
	for {
		if ctx.Err() != nil {
			return totalWritten, fmt.Errorf("download canceled")
		}
		n, err := src.Read(buf)
		if err != nil && err != io.EOF {
			if errors.Is(err, context.Canceled) {
				return totalWritten, ctx.Err()
			}
			return totalWritten, fmt.Errorf("failed to read from source file: %w", err)
		}
		if n == 0 {
			return totalWritten, nil
		}
		if _, writeErr := dst.Write(buf[:n]); writeErr != nil {
			return totalWritten, writeErr
		}
		totalWritten += int64(n)
	}
}

func GetRandomKVPair[T any](m map[string]T) (string, T) {
	var result T
	for k, v := range m {
		return k, v
	}
	return "", result
}

// HTTPClientWithCABundle returns an HTTP client whose TLS config trusts the host's system
// roots plus any PEM certificates found in caBundlePath. Callers that need CA rotation to take
// effect without a restart should call this before each request so the bundle is re-read from
// disk (e.g. a non-subPath ConfigMap volume that kubelet keeps in sync).
//
// When caBundlePath is empty or the file does not exist, the returned client falls back to the
// system trust store only. A present-but-unparseable bundle is treated as a configuration error.
func HTTPClientWithCABundle(caBundlePath string) (*http.Client, error) {
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}

	if caBundlePath != "" {
		pemBytes, readErr := os.ReadFile(caBundlePath)
		switch {
		case readErr == nil:
			if len(pemBytes) > 0 && !pool.AppendCertsFromPEM(pemBytes) {
				return nil, fmt.Errorf("no valid certificates found in CA bundle %q", caBundlePath)
			}
		case errors.Is(readErr, os.ErrNotExist):
			// The bundle is not mounted yet; fall back to system roots. The request will fail
			// TLS verification if it targets the DPF CA, and the caller is expected to retry.
		default:
			return nil, fmt.Errorf("read CA bundle %q: %w", caBundlePath, readErr)
		}
	}

	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
				RootCAs:    pool,
			},
		},
	}, nil
}
