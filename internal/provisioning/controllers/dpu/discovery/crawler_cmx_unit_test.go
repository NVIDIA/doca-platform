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

package discovery

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	rfclient "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/client"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestCreateDPUDeviceAndNodeCMX checks that a discovered CMX BMC yields a
// hostless DPUDevice with no spec.opn and no External DPUNode, while a
// BlueField BMC still gets its DPUNode, its OPN, and no hostless label.
func TestCreateDPUDeviceAndNodeCMX(t *testing.T) {
	for name, tc := range map[string]struct {
		cmx          bool
		wantHostless bool
		wantDPUNode  bool
		wantOPN      bool
	}{
		"cmx": {
			cmx:          true,
			wantHostless: true,
			wantDPUNode:  false,
			wantOPN:      false,
		},
		"bluefield": {
			cmx:          false,
			wantHostless: false,
			wantDPUNode:  true,
			wantOPN:      true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			g := NewWithT(t)
			scheme := runtime.NewScheme()
			g.Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())
			c := fake.NewClientBuilder().WithScheme(scheme).Build()
			crawler := NewCrawlerService(c, "ns", 1, false, provisioningv1.BMCFactoryResetPolicyNever)

			g.Expect(crawler.createDPUDeviceAndNode(context.Background(), CrawlResult{
				IPAddress:    "10.0.0.9",
				Port:         443,
				SerialNumber: "MT5678",
				OPN:          "900-9D3B6-00CV-AA0",
				CMX:          tc.cmx,
			})).To(Succeed())

			device := &provisioningv1.DPUDevice{}
			g.Expect(c.Get(context.Background(), types.NamespacedName{Namespace: "ns", Name: "mt5678"}, device)).To(Succeed())
			if tc.wantOPN {
				g.Expect(device.Spec.OPN).To(HaveValue(Equal("900-9D3B6-00CV-AA0")))
			} else {
				g.Expect(device.Spec.OPN).To(BeNil())
			}
			if tc.wantHostless {
				g.Expect(device.Labels).To(HaveKeyWithValue(util.DPUDeviceHostlessLabel, "true"))
			} else {
				g.Expect(device.Labels).NotTo(HaveKey(util.DPUDeviceHostlessLabel))
			}

			node := &provisioningv1.DPUNode{}
			err := c.Get(context.Background(), types.NamespacedName{Namespace: "ns", Name: "dpu-node-mt5678"}, node)
			if tc.wantDPUNode {
				g.Expect(err).NotTo(HaveOccurred())
			} else {
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}
		})
	}
}

// TestWorkerReadsCMXRootService runs the crawler worker against a fake BMC
// and checks that the product in the root service decides the device shape:
// a CMX root gives a hostless DPUDevice with no DPUNode, and a BlueField root
// keeps its OPN and its DPUNode.
func TestWorkerReadsCMXRootService(t *testing.T) {
	for name, tc := range map[string]struct {
		product string
		wantCMX bool
	}{
		"cmx": {
			product: "CMX",
			wantCMX: true,
		},
		"bluefield": {
			product: "BlueField-3",
			wantCMX: false,
		},
	} {
		t.Run(name, func(t *testing.T) {
			g := NewWithT(t)
			server := httptest.NewTLSServer(fakeBMC{product: tc.product})
			t.Cleanup(server.Close)
			serverURL, err := url.Parse(server.URL)
			g.Expect(err).NotTo(HaveOccurred())
			port, err := strconv.ParseUint(serverURL.Port(), 10, 32)
			g.Expect(err).NotTo(HaveOccurred())

			scheme := runtime.NewScheme()
			g.Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())
			g.Expect(corev1.AddToScheme(scheme)).To(Succeed())
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: rfclient.BMCPasswordSecret, Namespace: "ns"},
				Data:       map[string][]byte{rfclient.BMCSharedPasswordKey: []byte("secret")},
			}).Build()
			crawler := NewCrawlerService(c, "ns", 1, false, provisioningv1.BMCFactoryResetPolicyNever)

			jobs := make(chan uint32, 1)
			results := make(chan CrawlResult, 1)
			jobs <- ipToUint32(net.ParseIP("127.0.0.1"))
			close(jobs)
			var wg sync.WaitGroup
			wg.Add(1)
			crawler.worker(context.Background(), &wg, jobs, results, map[string]bool{}, map[string]bool{}, uint32(port))
			result := <-results

			g.Expect(result.Error).NotTo(HaveOccurred())
			g.Expect(result.Found).To(BeTrue())
			g.Expect(result.CMX).To(Equal(tc.wantCMX))
			g.Expect(crawler.createDPUDeviceAndNode(context.Background(), result)).To(Succeed())

			device := &provisioningv1.DPUDevice{}
			g.Expect(c.Get(context.Background(), types.NamespacedName{Namespace: "ns", Name: "mt5678"}, device)).To(Succeed())
			node := &provisioningv1.DPUNode{}
			nodeErr := c.Get(context.Background(), types.NamespacedName{Namespace: "ns", Name: "dpu-node-mt5678"}, node)
			if tc.wantCMX {
				g.Expect(device.Labels).To(HaveKeyWithValue(util.DPUDeviceHostlessLabel, "true"))
				g.Expect(device.Spec.OPN).To(BeNil())
				g.Expect(apierrors.IsNotFound(nodeErr)).To(BeTrue())
			} else {
				g.Expect(device.Labels).NotTo(HaveKey(util.DPUDeviceHostlessLabel))
				g.Expect(device.Spec.OPN).To(HaveValue(Equal("900-9D3B6-00CV-AA0")))
				g.Expect(nodeErr).NotTo(HaveOccurred())
			}
		})
	}
}

// fakeBMC serves the Redfish reads one discovery worker makes against a BMC.
// Its root service names product, and every account accepts the shared
// password "secret".
type fakeBMC struct {
	product string
}

// ServeHTTP answers the unauthenticated root service, then the authenticated
// Managers probe and the chassis read that the worker and InitPassword make.
func (b fakeBMC) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path == "/redfish/v1" || r.URL.Path == "/redfish/v1/" {
		_, _ = fmt.Fprintf(w, `{"Product":%q}`, b.product)
		return
	}
	if _, pass, ok := r.BasicAuth(); !ok || pass != "secret" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	switch {
	case r.URL.Path == "/redfish/v1/Managers":
		_, _ = w.Write([]byte(`{}`))
	case strings.HasPrefix(r.URL.Path, "/redfish/v1/Chassis/"):
		_, _ = w.Write([]byte(`{"SerialNumber":"MT5678","PartNumber":"900-9D3B6-00CV-AA0"}`))
	default:
		http.NotFound(w, r)
	}
}
