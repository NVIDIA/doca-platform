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

package trustedhost

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/hostagent/service/types"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	defaultHostAgentEndpoint = "http://169.254.0.1:11029"
	defaultMaxRetries        = 10
	defaultRetryInterval     = 2 * time.Second
)

type TrustedhostClient struct {
	hostAgentEndpoint string
	dpuName           string
	dpuNamespace      string
	scheme            *runtime.Scheme
}

func NewTrustedhostClient(dpuName string, dpuNamespace string) *TrustedhostClient {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(provisioningv1.AddToScheme(scheme))
	return &TrustedhostClient{
		dpuName:      dpuName,
		dpuNamespace: dpuNamespace,
		scheme:       scheme,
	}
}

func (c *TrustedhostClient) HealthCheck() error {
	if c.hostAgentEndpoint == "" {
		c.hostAgentEndpoint = defaultHostAgentEndpoint
	}
	resp, err := runWithRetry(func() (*http.Response, error) {
		return http.Get(fmt.Sprintf("%s/healthz", c.hostAgentEndpoint))
	}, defaultMaxRetries, defaultRetryInterval)
	if err != nil {
		return err
	}
	defer closeBody(resp)
	if resp.StatusCode != http.StatusOK {
		return returnError(resp)
	}
	return nil
}

func (c *TrustedhostClient) UpdateStatus(ctx context.Context, dpuInfo provisioningv1.DPUInternalStatus) error {
	if c.hostAgentEndpoint == "" {
		c.hostAgentEndpoint = defaultHostAgentEndpoint
	}
	url := fmt.Sprintf("%s/update-status", c.hostAgentEndpoint)
	request := types.UpdateStatusRequest{
		DPUName:      c.dpuName,
		DPUNamespace: c.dpuNamespace,
		DPUInfo:      dpuInfo,
	}
	requestBody, err := json.Marshal(request)
	if err != nil {
		return err
	}
	resp, err := runWithRetry(func() (*http.Response, error) {
		return http.Post(url, "application/json", bytes.NewBuffer(requestBody))
	}, defaultMaxRetries, defaultRetryInterval)
	if err != nil {
		return err
	}
	defer closeBody(resp)
	if resp.StatusCode != http.StatusOK {
		return returnError(resp)
	}
	return nil
}

func (c *TrustedhostClient) GetObject(ctx context.Context, namespace string, name string, obj client.Object) error {
	if c.hostAgentEndpoint == "" {
		c.hostAgentEndpoint = defaultHostAgentEndpoint
	}
	gvks, _, err := c.scheme.ObjectKinds(obj)
	if err != nil {
		return fmt.Errorf("failed to get object GVK: %v", err)
	} else if len(gvks) == 0 {
		return fmt.Errorf("no GVK found for object %T", obj)
	}
	gvk := gvks[0]
	url := fmt.Sprintf("%s/get-object?group=%s&version=%s&kind=%s&namespace=%s&name=%s", c.hostAgentEndpoint, gvk.Group, gvk.Version, gvk.Kind, namespace, name)
	resp, err := runWithRetry(func() (*http.Response, error) {
		return http.Get(url)
	}, defaultMaxRetries, defaultRetryInterval)
	if err != nil {
		return err
	}
	defer closeBody(resp)

	if resp.StatusCode != http.StatusOK {
		return returnError(resp)
	}
	return json.NewDecoder(resp.Body).Decode(obj)
}

type sendRequestFunc func() (*http.Response, error)

// runWithRetry retries a request in case of non-http errors.
// Such a retry mechanism is useful because the rshim channel is not reliable.
// It's quite often that the first request fails while follow-up requests succeed.
func runWithRetry(sendReq sendRequestFunc, maxRetries int, retryInterval time.Duration) (*http.Response, error) {
	var resp *http.Response
	var err error
	for i := 0; i < maxRetries; i++ {
		resp, err = sendReq()
		if err == nil {
			return resp, nil
		}
		klog.Errorf("[TrustedhostClient] Failed to send request (attempt %d/%d): %v", i+1, maxRetries, err)
		time.Sleep(retryInterval)
	}
	return resp, err
}

func closeBody(resp *http.Response) {
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

func returnError(resp *http.Response) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		klog.Warningf("failed to read body: %v", err)
	}
	return fmt.Errorf("status code: %d, body: %s", resp.StatusCode, string(body))
}
