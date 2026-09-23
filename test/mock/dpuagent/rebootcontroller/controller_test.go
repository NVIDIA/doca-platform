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

package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/test/mock/dpuagent/pkg/hostctl"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type rebootRecorder struct {
	mu      sync.Mutex
	methods []string
}

func (r *rebootRecorder) HostReboot(_ context.Context, method string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.methods = append(r.methods, method)
}

func TestReconcileRebootsMockDPUsAndClearsAnnotation(t *testing.T) {
	recorder := &rebootRecorder{}
	srv := httptest.NewServer(hostctl.Handler(context.Background(), recorder))
	defer srv.Close()
	host, portStr, err := net.SplitHostPort(srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(portStr)

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(provisioningv1.AddToScheme(scheme))
	device := &provisioningv1.DPUDevice{
		ObjectMeta: metav1.ObjectMeta{Name: "mt2616606m3h", Namespace: "dpf-operator-system"},
		Status:     provisioningv1.DPUDeviceStatus{BMCIP: ptr.To(host)},
	}
	dpuNode := &provisioningv1.DPUNode{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "dpu-node-mt2616606m3h",
			Namespace:   "dpf-operator-system",
			Annotations: map[string]string{provisioningv1.DPUNodeExternalRebootRequiredAnnotation: "true"},
		},
		Spec: provisioningv1.DPUNodeSpec{
			NodeRebootMethod: &provisioningv1.NodeRebootMethod{External: &provisioningv1.External{}},
			DPUs:             []provisioningv1.DPURef{{Name: device.Name}},
		},
		Status: provisioningv1.DPUNodeStatus{RebootMethod: ptr.To(provisioningv1.RebootMethodSystemLevelReset)},
	}
	dpu := &provisioningv1.DPU{
		ObjectMeta: metav1.ObjectMeta{Name: cutil.GenerateDPUName(dpuNode.Name, device.Name), Namespace: dpuNode.Namespace},
		Status: provisioningv1.DPUStatus{
			Phase:        provisioningv1.DPURebooting,
			RebootStatus: &provisioningv1.RebootStatus{Phase: provisioningv1.RebootStatusWaitForShutdown},
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(device, dpuNode, dpu).WithStatusSubresource(dpu).Build()
	r := &DPUNodeRebootReconciler{Client: c, HostPort: port, HTTPClient: srv.Client()}

	// The DPUNode controller has not released the DPU to the external reboot yet: nothing happens.
	key := types.NamespacedName{Namespace: dpuNode.Namespace, Name: dpuNode.Name}
	res, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: key})
	if err != nil {
		t.Fatal(err)
	}
	if res.RequeueAfter == 0 {
		t.Fatal("expected a requeue while the DPU is still waiting for its Arm shutdown")
	}
	recorder.mu.Lock()
	early := len(recorder.methods)
	recorder.mu.Unlock()
	if early != 0 {
		t.Fatalf("rebooted before the DPUNode controller released the DPU: %v", recorder.methods)
	}

	dpu.Status.RebootStatus = &provisioningv1.RebootStatus{Phase: provisioningv1.RebootStatusPending, Reason: externalRebootWaitReason}
	if err := c.Status().Update(context.Background(), dpu); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: key}); err != nil {
		t.Fatal(err)
	}
	recorder.mu.Lock()
	methods := append([]string{}, recorder.methods...)
	recorder.mu.Unlock()
	if len(methods) != 1 || methods[0] != string(provisioningv1.RebootMethodSystemLevelReset) {
		t.Fatalf("unexpected reboot calls %v", methods)
	}
	updated := &provisioningv1.DPUNode{}
	if err := c.Get(context.Background(), key, updated); err != nil {
		t.Fatal(err)
	}
	if _, ok := updated.Annotations[provisioningv1.DPUNodeExternalRebootRequiredAnnotation]; ok {
		t.Fatal("annotation was not removed")
	}
	// Without the annotation nothing happens.
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: key}); err != nil {
		t.Fatal(err)
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if len(recorder.methods) != 1 {
		t.Fatalf("unexpected extra reboot calls %v", recorder.methods)
	}
}

func TestReconcileKeepsAnnotationWhenRebootFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	host, portStr, _ := net.SplitHostPort(srv.Listener.Addr().String())
	port, _ := strconv.Atoi(portStr)

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(provisioningv1.AddToScheme(scheme))
	device := &provisioningv1.DPUDevice{
		ObjectMeta: metav1.ObjectMeta{Name: "dev", Namespace: "ns"},
		Spec:       provisioningv1.DPUDeviceSpec{BMCIP: ptr.To(host)},
	}
	dpuNode := &provisioningv1.DPUNode{
		ObjectMeta: metav1.ObjectMeta{Name: "node", Namespace: "ns",
			Annotations: map[string]string{provisioningv1.DPUNodeExternalRebootRequiredAnnotation: "true"}},
		Spec: provisioningv1.DPUNodeSpec{
			NodeRebootMethod: &provisioningv1.NodeRebootMethod{External: &provisioningv1.External{}},
			DPUs:             []provisioningv1.DPURef{{Name: device.Name}},
		},
	}
	dpu := &provisioningv1.DPU{
		ObjectMeta: metav1.ObjectMeta{Name: cutil.GenerateDPUName(dpuNode.Name, device.Name), Namespace: dpuNode.Namespace},
		Status: provisioningv1.DPUStatus{
			Phase:        provisioningv1.DPURebooting,
			RebootStatus: &provisioningv1.RebootStatus{Phase: provisioningv1.RebootStatusPending, Reason: externalRebootWaitReason},
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(device, dpuNode, dpu).Build()
	r := &DPUNodeRebootReconciler{Client: c, HostPort: port, HTTPClient: srv.Client()}
	key := types.NamespacedName{Namespace: "ns", Name: "node"}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: key}); err == nil {
		t.Fatal("expected error when the mock rejects the reboot")
	}
	updated := &provisioningv1.DPUNode{}
	if err := c.Get(context.Background(), key, updated); err != nil {
		t.Fatal(err)
	}
	if _, ok := updated.Annotations[provisioningv1.DPUNodeExternalRebootRequiredAnnotation]; !ok {
		t.Fatal("annotation must stay until every DPU rebooted")
	}
}

func TestHostctlRequestBody(t *testing.T) {
	body, err := json.Marshal(hostctl.RebootRequest{Method: "PowerCycle"})
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `{"method":"PowerCycle"}` {
		t.Fatalf("unexpected body %s", body)
	}
}
