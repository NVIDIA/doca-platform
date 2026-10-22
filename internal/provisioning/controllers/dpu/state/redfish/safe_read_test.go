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

package redfish

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	rc "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/client"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"

	"github.com/go-logr/logr"
	"github.com/go-resty/resty/v2"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestTaskReadFailureKeepsIdentity(t *testing.T) {
	for _, status := range []int{200, 404, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			// Hold the handler until the client has observed cancellation. Returning
			// earlier makes net/http write an empty HTTP 200, which the client can
			// accept instead of context.Canceled.
			hold := make(chan struct{})
			releaseHold := sync.OnceFunc(func() { close(hold) })
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if status == 200 {
					cancel()
					<-hold
					return
				}
				w.WriteHeader(status)
				_, _ = io.WriteString(w, "upstream unavailable")
			}))
			defer server.Close()
			defer releaseHold()
			c, err := rc.NewRawClient(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			defer c.CloseIdleConnections()
			taskID := "accepted-task"
			dpu := &provisioningv1.DPU{}
			state := &provisioningv1.DPUStatus{Phase: provisioningv1.DPUOSInstalling, RedfishTaskID: &taskID}
			submissions := 0
			install := func(string) (*resty.Response, *rc.TaskInfo, error) {
				submissions++
				return nil, nil, errors.New("must not submit")
			}
			err = reconcileBf4ArmTransfer(ctx, logr.Discard(), dpu, state, c, provisioningv1.DPUCondIsoTransferred, "image", install, "ISO")
			releaseHold()
			if err == nil || isRestartOSInstallError(err) || state.RedfishTaskID == nil || *state.RedfishTaskID != taskID || submissions != 0 || state.Phase != provisioningv1.DPUOSInstalling {
				t.Fatalf("read failure lost operation identity: state=%+v submissions=%d err=%v", state, submissions, err)
			}
			if status == 200 && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if status != 200 && !rc.HasHTTPStatus(err, status) {
				t.Fatalf("expected HTTP %d, got %v", status, err)
			}
		})
	}
}

func TestProductDescriptionPreservesReadError(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redfish/v1/Systems" {
			_, _ = io.WriteString(w, `{"Members":[{"@odata.id":"/redfish/v1/Systems/Bluefield"}]}`)
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, "sensitive response body")
	}))
	defer server.Close()
	c, err := rc.NewRawClient(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseIdleConnections()
	_, err = getProductDescription(context.Background(), c)
	if !rc.HasHTTPStatus(err, http.StatusServiceUnavailable) || strings.Contains(err.Error(), "sensitive response body") {
		t.Fatalf("expected sanitized, wrapped HTTP failure: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = getProductDescription(ctx, c)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected wrapped cancellation: %v", err)
	}
}

func TestSELDeadlineIncludesDeviceLookup(t *testing.T) {
	var deadline, observedAt time.Time
	k8sClient := fake.NewClientBuilder().WithInterceptorFuncs(interceptor.Funcs{
		Get: func(ctx context.Context, _ client.WithWatch, _ types.NamespacedName, _ client.Object, _ ...client.GetOption) error {
			deadline, _ = ctx.Deadline()
			observedAt = time.Now()
			return context.Canceled
		},
	}).Build()
	started := time.Now()
	hint := bestEffortRailHint(context.Background(), &provisioningv1.DPU{}, &dutil.ControllerContext{Client: k8sClient}, logr.Discard(), nil)
	if hint != "" || deadline.Before(started.Add(railHintProbeTimeout)) || deadline.After(observedAt.Add(railHintProbeTimeout)) {
		t.Fatalf("SEL setup was not bounded: hint=%q deadline=%v", hint, deadline)
	}
}

func TestBootProgressDeadlineIncludesDeviceLookup(t *testing.T) {
	uid := types.UID(t.Name())
	t.Cleanup(func() { installProgressProbedAt.Delete(uid) })
	var deadline, observedAt time.Time
	k8sClient := fake.NewClientBuilder().WithInterceptorFuncs(interceptor.Funcs{
		Get: func(ctx context.Context, _ client.WithWatch, _ types.NamespacedName, _ client.Object, _ ...client.GetOption) error {
			deadline, _ = ctx.Deadline()
			observedAt = time.Now()
			return context.Canceled
		},
	}).Build()
	dpu := &provisioningv1.DPU{}
	dpu.UID = uid
	started := time.Now()
	progress := bootProgressState(context.Background(), dpu, &dutil.ControllerContext{Client: k8sClient}, logr.Discard())
	maxDeadline := observedAt.Add(rc.ReadTimeout)
	if progress != "" || deadline.Before(started.Add(rc.ReadTimeout)) || deadline.After(maxDeadline) {
		t.Fatalf("boot progress setup was not bounded: progress=%q deadline=%v", progress, deadline)
	}
}
