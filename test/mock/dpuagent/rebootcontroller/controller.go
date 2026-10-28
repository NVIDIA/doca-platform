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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/test/mock/dpuagent/pkg/hostctl"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// DPUNodeRebootReconciler plays the "external system" of the external node reboot method: when
// the DPUNode controller asks for a host reboot by annotating the DPUNode, it reboots every mock
// DPU of that node through its host control endpoint, waits until every Arm has booted again and
// then removes the annotation, which the DPUNode controller takes as "the host came back".
type DPUNodeRebootReconciler struct {
	client.Client
	// HostPort is the mock-dpuagent host control port (hostctl.DefaultAddr).
	HostPort int
	// HTTPClient posts the reboot requests and polls the boot status.
	HTTPClient *http.Client

	mu sync.Mutex
	// handled remembers, per DPUNode, the resourceVersion of our own annotation removal. A
	// reconcile that still observes the annotation on an object no newer than that is the cache
	// lagging behind that removal, not a new reboot request.
	handled map[types.UID]uint64
	// rebooting holds the DPUNodes whose reboot was posted to every DPU and whose Arms are still
	// booting: a requeue or watch event for them polls instead of posting again. It is cleared
	// when the annotation goes away, whoever removes it.
	rebooting map[types.UID]struct{}
}

// armBootPollInterval paces the boot status polls of a rebooted node.
const armBootPollInterval = 2 * time.Second

// externalRebootWaitReason is the DPU RebootStatus reason the DPUNode controller stamps once it has
// recorded the reboot request on every rebooting DPU (reasonExternalRebootWaitManual in
// internal/provisioning/controllers/dpunode). Rebooting before that point races the DPUNode
// controller: it would find the annotation gone before its own bookkeeping is done and ask for a
// second reboot. A real external system never wins that race because a host takes minutes to
// reboot; the mock has to wait for the signal explicitly.
const externalRebootWaitReason = "WaitingForManualPowerCycleOrReboot"

// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpunodes,verbs=get;list;watch;patch
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpudevices;dpus,verbs=get;list;watch

// Reconcile handles one DPUNode.
func (r *DPUNodeRebootReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	dpuNode := &provisioningv1.DPUNode{}
	if err := r.Get(ctx, req.NamespacedName, dpuNode); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !rebootRequested(dpuNode) {
		r.setRebooting(dpuNode.UID, false)
		return ctrl.Result{}, nil
	}
	if r.alreadyHandled(dpuNode) {
		logger.V(1).Info("stale DPUNode still carries the reboot annotation we removed; waiting for a fresher object", "dpuNode", dpuNode.Name)
		return ctrl.Result{}, nil
	}
	devices, err := r.deviceBMCs(ctx, dpuNode)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !r.isRebooting(dpuNode.UID) {
		ready, err := rebootingDPUsWaitForExternalReboot(ctx, r.Client, dpuNode)
		if err != nil {
			return ctrl.Result{}, err
		}
		if !ready {
			logger.V(1).Info("DPUNode controller has not finished recording the reboot on its DPUs yet", "dpuNode", dpuNode.Name)
			return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
		}
		method := string(ptr.Deref(dpuNode.Status.RebootMethod, provisioningv1.RebootMethodUnknown))
		logger.Info("external host reboot requested", "dpuNode", dpuNode.Name, "method", method, "dpus", len(dpuNode.Spec.DPUs))
		for _, d := range devices {
			if err := r.rebootHost(ctx, d.bmcIP, method); err != nil {
				return ctrl.Result{}, fmt.Errorf("reboot mock DPU %s at %s: %w", d.device, d.bmcIP, err)
			}
			logger.Info("mock DPU host rebooted", "dpuDevice", d.device, "bmcIP", d.bmcIP)
		}
		r.setRebooting(dpuNode.UID, true)
	}

	for _, d := range devices {
		booted, err := r.armBooted(ctx, d.bmcIP)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("read boot status of mock DPU %s at %s: %w", d.device, d.bmcIP, err)
		}
		if !booted {
			logger.V(1).Info("waiting for the Arm to boot", "dpuNode", dpuNode.Name, "dpuDevice", d.device)
			return ctrl.Result{RequeueAfter: armBootPollInterval}, nil
		}
	}

	patch := client.MergeFrom(dpuNode.DeepCopy())
	delete(dpuNode.Annotations, provisioningv1.DPUNodeExternalRebootRequiredAnnotation)
	if err := r.Patch(ctx, dpuNode, patch); err != nil {
		return ctrl.Result{}, fmt.Errorf("remove external reboot annotation from DPUNode %s: %w", dpuNode.Name, err)
	}
	r.markHandled(dpuNode)
	logger.Info("external host reboot completed", "dpuNode", dpuNode.Name)
	return ctrl.Result{}, nil
}

// deviceBMC is a DPUDevice of a node and the BMC IP its mock serves the host control API on.
type deviceBMC struct {
	device string
	bmcIP  string
}

// deviceBMCs resolves the BMC IP of every DPUDevice of the node; missing DPUDevices are skipped.
func (r *DPUNodeRebootReconciler) deviceBMCs(ctx context.Context, dpuNode *provisioningv1.DPUNode) ([]deviceBMC, error) {
	var bmcs []deviceBMC
	for _, ref := range dpuNode.Spec.DPUs {
		device := &provisioningv1.DPUDevice{}
		if err := r.Get(ctx, types.NamespacedName{Namespace: dpuNode.Namespace, Name: ref.Name}, device); err != nil {
			if apierrors.IsNotFound(err) {
				log.FromContext(ctx).Info("DPUDevice of DPUNode not found, skipping", "dpuDevice", ref.Name)
				continue
			}
			return nil, err
		}
		bmcIP := ptr.Deref(device.Status.BMCIP, ptr.Deref(device.Spec.BMCIP, ""))
		if bmcIP == "" {
			return nil, fmt.Errorf("DPUDevice %s has no BMC IP", device.Name)
		}
		bmcs = append(bmcs, deviceBMC{device: device.Name, bmcIP: bmcIP})
	}
	return bmcs, nil
}

// markHandled records the resourceVersion of the DPUNode whose annotation was just removed and
// ends its reboot.
func (r *DPUNodeRebootReconciler) markHandled(dpuNode *provisioningv1.DPUNode) {
	r.setRebooting(dpuNode.UID, false)
	rv, err := strconv.ParseUint(dpuNode.ResourceVersion, 10, 64)
	if err != nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.handled == nil {
		r.handled = map[types.UID]uint64{}
	}
	r.handled[dpuNode.UID] = rv
}

// setRebooting records whether the reboot of the DPUNode with uid was sent and its Arms are still
// booting.
func (r *DPUNodeRebootReconciler) setRebooting(uid types.UID, rebooting bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !rebooting {
		delete(r.rebooting, uid)
		return
	}
	if r.rebooting == nil {
		r.rebooting = map[types.UID]struct{}{}
	}
	r.rebooting[uid] = struct{}{}
}

// isRebooting reports whether the reboot of the DPUNode with uid was sent and not yet completed.
func (r *DPUNodeRebootReconciler) isRebooting(uid types.UID) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.rebooting[uid]
	return ok
}

// alreadyHandled reports whether dpuNode is a stale view of an object whose reboot request was
// already served. resourceVersion is opaque in general, but on a single etcd-backed API server it
// is monotonic, which is all a test-only controller needs.
func (r *DPUNodeRebootReconciler) alreadyHandled(dpuNode *provisioningv1.DPUNode) bool {
	rv, err := strconv.ParseUint(dpuNode.ResourceVersion, 10, 64)
	if err != nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	handledRV, ok := r.handled[dpuNode.UID]
	return ok && rv <= handledRV
}

// rebootingDPUsWaitForExternalReboot reports whether every DPU of the node that is in the Rebooting
// phase has been marked as waiting for the external reboot. It is false while no DPU is rebooting
// yet or while the DPUNode controller is still between recording the annotation and stamping the
// DPUs.
func rebootingDPUsWaitForExternalReboot(ctx context.Context, c client.Client, dpuNode *provisioningv1.DPUNode) (bool, error) {
	dpus, err := cutil.GetDPUsWithPhase(ctx, c, dpuNode, provisioningv1.DPURebooting)
	if err != nil {
		return false, fmt.Errorf("list rebooting DPUs of DPUNode %s: %w", dpuNode.Name, err)
	}
	if len(dpus) == 0 {
		return false, nil
	}
	for _, dpu := range dpus {
		rs := dpu.Status.RebootStatus
		if rs == nil || rs.Phase != provisioningv1.RebootStatusPending || rs.Reason != externalRebootWaitReason {
			return false, nil
		}
	}
	return true, nil
}

func rebootRequested(dpuNode *provisioningv1.DPUNode) bool {
	if dpuNode.Spec.NodeRebootMethod == nil || dpuNode.Spec.NodeRebootMethod.External == nil {
		return false
	}
	_, ok := dpuNode.Annotations[provisioningv1.DPUNodeExternalRebootRequiredAnnotation]
	return ok
}

func (r *DPUNodeRebootReconciler) rebootHost(ctx context.Context, bmcIP, method string) error {
	body, err := json.Marshal(hostctl.RebootRequest{Method: method})
	if err != nil {
		return err
	}
	url := fmt.Sprintf("http://%s/reboot", joinHostPort(bmcIP, r.HostPort))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("unexpected status %s: %s", resp.Status, string(msg))
	}
	return nil
}

func (r *DPUNodeRebootReconciler) armBooted(ctx context.Context, bmcIP string) (bool, error) {
	url := fmt.Sprintf("http://%s/status", joinHostPort(bmcIP, r.HostPort))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, err
	}
	resp, err := r.httpClient().Do(req)
	if err != nil {
		return false, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return false, fmt.Errorf("unexpected status %s: %s", resp.Status, string(msg))
	}
	var status hostctl.StatusResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&status); err != nil {
		return false, fmt.Errorf("decode status: %w", err)
	}
	return status.ArmBooted, nil
}

func (r *DPUNodeRebootReconciler) httpClient() *http.Client {
	if r.HTTPClient != nil {
		return r.HTTPClient
	}
	return &http.Client{Timeout: 15 * time.Second}
}

func joinHostPort(host string, port int) string {
	if port == 80 {
		return host
	}
	return fmt.Sprintf("%s:%d", host, port)
}

// SetupWithManager registers the reconciler. Only DPUNodes that carry the annotation, or just
// lost it, are queued, so the controller is idle between reboots.
func (r *DPUNodeRebootReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("mock-dpunode-reboot").
		For(&provisioningv1.DPUNode{}, builder.WithPredicates(rebootRequestPredicate())).
		Complete(r)
}

func rebootRequestPredicate() predicate.Predicate {
	requested := func(obj client.Object) bool {
		node, ok := obj.(*provisioningv1.DPUNode)
		return ok && rebootRequested(node)
	}
	return predicate.Funcs{
		CreateFunc:  func(e event.CreateEvent) bool { return requested(e.Object) },
		UpdateFunc:  func(e event.UpdateEvent) bool { return requested(e.ObjectOld) || requested(e.ObjectNew) },
		DeleteFunc:  func(e event.DeleteEvent) bool { return requested(e.Object) },
		GenericFunc: func(e event.GenericEvent) bool { return requested(e.Object) },
	}
}
