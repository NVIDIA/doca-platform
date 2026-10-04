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

package dpudevice

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	rfclient "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/client"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestMTLSAcquisitionFailureDoesNotBootstrap(t *testing.T) {
	for _, status := range []int{403, 429, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var writes atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					writes.Add(1)
				}
				w.WriteHeader(status)
				_, _ = io.WriteString(w, "unavailable")
			}))
			defer server.Close()
			basic, err := rfclient.NewRawClient(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			defer basic.CloseIdleConnections()
			device := &provisioningv1.DPUDevice{ObjectMeta: metav1.ObjectMeta{Namespace: "test"}}
			r := &DPUDeviceReconciler{Client: fake.NewClientBuilder().Build()}
			err = r.ensureRedfishMTLS(context.Background(), device, server.URL, basic)
			if !rfclient.HasHTTPStatus(err, status) || writes.Load() != 0 || len(device.Status.Conditions) != 0 {
				t.Fatalf("read failure entered bootstrap: err=%v writes=%d conditions=%v", err, writes.Load(), device.Status.Conditions)
			}
		})
	}
}

func TestMTLSAcquisitionCancellationDoesNotBootstrap(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var reads, writes atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writes.Add(1)
			return
		}
		reads.Add(1)
		cancel()
		<-r.Context().Done()
	}))
	defer server.Close()
	basic, err := rfclient.NewRawClient(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer basic.CloseIdleConnections()
	device := &provisioningv1.DPUDevice{}
	r := &DPUDeviceReconciler{Client: fake.NewClientBuilder().Build()}
	err = r.ensureRedfishMTLS(ctx, device, server.URL, basic)
	if !errors.Is(err, context.Canceled) || reads.Load() == 0 || writes.Load() != 0 || len(device.Status.Conditions) != 0 {
		t.Fatalf("cancellation entered bootstrap: err=%v reads=%d writes=%d conditions=%v", err, reads.Load(), writes.Load(), device.Status.Conditions)
	}
}

func TestCertificateBackfillReadFailureDoesNotRotate(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	c, err := rfclient.NewRawClient(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseIdleConnections()
	r := &DPUDeviceReconciler{}
	handled, result := r.backfillServerCertExpiry(context.Background(), &provisioningv1.DPUDevice{}, c, defaultBMCServerCertRenewBefore)
	if !handled || result.RequeueAfter <= 0 {
		t.Fatalf("failed observation authorized rotation: handled=%v result=%v", handled, result)
	}
}

func TestFactoryResetDefaultCredentialUnavailableIsNotRejection(t *testing.T) {
	var writes atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writes.Add(1)
		}
		if strings.HasSuffix(r.URL.Path, "/Managers") {
			_, password, _ := r.BasicAuth()
			if password == rfclient.BMCDefaultPassword {
				w.WriteHeader(http.StatusServiceUnavailable)
			} else {
				w.WriteHeader(http.StatusUnauthorized)
			}
		}
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()
	address := server.Listener.Addr().(*net.TCPAddr)
	ip, port := address.IP.String(), uint32(address.Port)
	device := &provisioningv1.DPUDevice{
		ObjectMeta: metav1.ObjectMeta{Namespace: "test"},
		Status:     provisioningv1.DPUDeviceStatus{BMCIP: &ip, BMCPort: &port},
	}
	r := &DPUDeviceReconciler{Client: fake.NewClientBuilder().WithObjects(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: rfclient.BMCPasswordSecret, Namespace: "test"},
		Data:       map[string][]byte{rfclient.BMCSharedPasswordKey: []byte("rejected-secret-password")},
	}).Build()}
	c, err := r.privilegedClientForFactoryReset(context.Background(), device)
	if c != nil {
		defer c.CloseIdleConnections()
	}
	if c != nil || !rfclient.HasHTTPStatus(err, http.StatusServiceUnavailable) ||
		strings.Contains(err.Error(), "accepts neither") || writes.Load() != 0 {
		t.Fatalf("transient default-password failure misclassified: client=%v err=%v writes=%d", c, err, writes.Load())
	}
}
