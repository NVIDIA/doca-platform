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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
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

func (r *DPUDiscoveryReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconcile DPUDiscovery")

	var dpuDiscovery provisioningv1.DPUDiscovery
	if err := r.Get(ctx, req.NamespacedName, &dpuDiscovery); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Get workers from spec or use default
	var workers int
	if dpuDiscovery.Spec.Workers != nil {
		workers = *dpuDiscovery.Spec.Workers
	} else {
		startIP := net.ParseIP(dpuDiscovery.Spec.IPRangeSpec.IPRange.StartIP)
		endIP := net.ParseIP(dpuDiscovery.Spec.IPRangeSpec.IPRange.EndIP)
		if startIP == nil || endIP == nil || startIP.To4() == nil || endIP.To4() == nil {
			return ctrl.Result{}, fmt.Errorf("only IPv4 addresses are supported")
		}

		start := binary.BigEndian.Uint32(startIP.To4())
		end := binary.BigEndian.Uint32(endIP.To4())

		if start > end {
			return ctrl.Result{}, fmt.Errorf("startIP must not be greater than endIP")
		}

		const ipPerWorker = 255
		workers = int((end-start)/uint32(ipPerWorker)) + 1
		if workers < 1 {
			workers = 1
		}
	}

	dpfOperatorConfig, err := utils.GetDPFOperatorConfig(ctx, r.Client)
	if err != nil {
		logger.Error(err, "Failed to get operator config")
		return ctrl.Result{}, err
	}

	if dpfOperatorConfig.Spec.ProvisioningController.InstallInterface.InstallViaRedfish == nil {
		return ctrl.Result{}, fmt.Errorf("InstallViaRedfish not configured in DPFOperatorConfig")
	}

	skipDpuNodeDiscovery := true
	if dpfOperatorConfig.Spec.ProvisioningController.InstallInterface.InstallViaRedfish.SkipDPUNodeDiscovery != nil {
		skipDpuNodeDiscovery = *dpfOperatorConfig.Spec.ProvisioningController.InstallInterface.InstallViaRedfish.SkipDPUNodeDiscovery
	}
	crawler := NewCrawlerService(r.Client, dpuDiscovery.Namespace, workers, skipDpuNodeDiscovery)

	// Check if it's time to scan
	if dpuDiscovery.Status.LastScanTime != nil {
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
	if err := r.Status().Update(ctx, &dpuDiscovery); err != nil {
		logger.Error(err, "Failed to update crawler status")
		return ctrl.Result{}, err
	}

	// Schedule next scan
	return ctrl.Result{
		RequeueAfter: dpuDiscovery.Spec.ScanInterval.Duration,
	}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DPUDiscoveryReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&provisioningv1.DPUDiscovery{}).
		Complete(r)
}
