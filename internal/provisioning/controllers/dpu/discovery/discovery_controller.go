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

package discovery

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/utils"
	"github.com/nvidia/doca-platform/internal/utils/tracing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const (
	// DPUDiscoveryControllerName is the name recorded on DPUDiscovery reconcile spans.
	DPUDiscoveryControllerName = "dpudiscovery"
)

// DPUDiscoveryReconciler reconciles a DPUDiscovery object
type DPUDiscoveryReconciler struct {
	client.Client
}

// Reconcile handles the reconciliation loop for DPUDiscovery

// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpudiscoveries,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpudiscoveries/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpudiscoveries/finalizers,verbs=update
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpudevices,verbs=create;get;list;watch
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpunodes,verbs=create;get;list;watch
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;create;delete

func (r *DPUDiscoveryReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconcile DPUDiscovery")

	var dpuDiscovery provisioningv1.DPUDiscovery
	if err := r.Get(ctx, req.NamespacedName, &dpuDiscovery); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	ctx, endSpan := discoverySpan(ctx, &dpuDiscovery)
	defer endSpan(&reterr)

	workers, err := scanWorkers(dpuDiscovery.Spec)
	if err != nil {
		return ctrl.Result{}, err
	}

	dpfOperatorConfig, err := utils.GetDPFOperatorConfig(ctx, r.Client)
	if err != nil {
		logger.Error(err, "Failed to get operator config")
		return ctrl.Result{}, err
	}

	if dpfOperatorConfig.Spec.ProvisioningController.InstallInterface.InstallViaRedfish == nil {
		return ctrl.Result{}, fmt.Errorf("InstallViaRedfish not configured in DPFOperatorConfig")
	}

	installViaRedfish := dpfOperatorConfig.Spec.ProvisioningController.InstallInterface.InstallViaRedfish
	skipDpuNodeDiscovery := true
	if installViaRedfish.SkipDPUNodeDiscovery != nil {
		skipDpuNodeDiscovery = *installViaRedfish.SkipDPUNodeDiscovery
	}
	bmcFactoryResetPolicy := provisioningv1.GetBMCFactoryResetPolicy(installViaRedfish.DiscoveredDPUDeviceBMCFactoryResetPolicy)
	crawler := NewCrawlerService(r.Client, dpuDiscovery.Namespace, workers, skipDpuNodeDiscovery, bmcFactoryResetPolicy)

	// Run scan immediately when spec changed (e.g. IP range), otherwise respect scan interval
	specChanged := dpuDiscovery.Generation != dpuDiscovery.Status.ObservedGeneration
	// The scan for this generation already completed, which ends the trace of its change.
	if !specChanged {
		if err := tracing.EndObjectTrace(ctx, r.Client, &dpuDiscovery); err != nil {
			return ctrl.Result{}, err
		}
	}
	if !specChanged && dpuDiscovery.Status.LastScanTime != nil {
		nextScan := dpuDiscovery.Status.LastScanTime.Add(dpuDiscovery.Spec.ScanInterval.Duration)
		if time.Now().Before(nextScan) {
			return ctrl.Result{
				RequeueAfter: time.Until(nextScan),
			}, nil
		}
	}

	// Perform the scan
	if numFound, err := crawler.Crawl(ctx, dpuDiscovery.Spec.IPRangeSpec.IPRange); err != nil {
		logger.Error(err, "Failed to crawl IP range")
		return ctrl.Result{}, err
	} else {
		dpuDiscovery.Status.FoundDPUs += numFound
	}

	// Update status
	now := metav1.Now()
	dpuDiscovery.Status.LastScanTime = &now
	dpuDiscovery.Status.ObservedGeneration = dpuDiscovery.Generation
	if err := r.Status().Update(ctx, &dpuDiscovery); err != nil {
		logger.Error(err, "Failed to update crawler status")
		return ctrl.Result{}, err
	}
	if err := tracing.EndObjectTrace(ctx, r.Client, &dpuDiscovery); err != nil {
		return ctrl.Result{}, err
	}

	// Schedule next scan
	return ctrl.Result{
		RequeueAfter: dpuDiscovery.Spec.ScanInterval.Duration,
	}, nil
}

// discoverySpan starts the span for one DPUDiscovery reconcile. Until the scan for the current
// generation completes, the reconcile continues the trace of that create or spec change: the
// trace annotation when a client set one, otherwise the trace derived from the UID and generation.
// A periodic rescan is its own operation, so it starts a new root trace and the DPUDevices it
// finds start their own traces instead of joining an earlier change's trace.
func discoverySpan(ctx context.Context, dpuDiscovery *provisioningv1.DPUDiscovery) (context.Context, func(*error)) {
	if dpuDiscovery.Generation != dpuDiscovery.Status.ObservedGeneration {
		return tracing.ReconcileSpan(ctx, DPUDiscoveryControllerName, dpuDiscovery)
	}
	ctx, span := tracing.StartSpan(ctx, DPUDiscoveryControllerName+".scan",
		tracing.ObjectAttributes(DPUDiscoveryControllerName, dpuDiscovery)...)
	return ctx, func(errp *error) {
		var err error
		if errp != nil {
			err = *errp
		}
		tracing.EndSpan(span, err)
	}
}

// scanWorkers resolves how many workers a scan runs with: the value from the spec when set, and
// otherwise one worker per 255 addresses of the range.
func scanWorkers(spec provisioningv1.DPUDiscoverySpec) (int, error) {
	if spec.Workers != nil {
		return *spec.Workers, nil
	}

	startIP := net.ParseIP(spec.IPRangeSpec.IPRange.StartIP)
	endIP := net.ParseIP(spec.IPRangeSpec.IPRange.EndIP)
	if startIP == nil || endIP == nil || startIP.To4() == nil || endIP.To4() == nil {
		return 0, fmt.Errorf("only IPv4 addresses are supported")
	}

	start := binary.BigEndian.Uint32(startIP.To4())
	end := binary.BigEndian.Uint32(endIP.To4())
	if start > end {
		return 0, fmt.Errorf("startIP must not be greater than endIP")
	}

	const ipPerWorker = 255
	return int((end-start)/uint32(ipPerWorker)) + 1, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DPUDiscoveryReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&provisioningv1.DPUDiscovery{}).
		// Avoid reconciling on status-only updates; only reconcile when spec changes (generation bump).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Complete(r)
}
