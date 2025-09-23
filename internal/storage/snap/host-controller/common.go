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

package hostcontroller

import (
	"errors"
	"time"

	"github.com/nvidia/doca-platform/internal/storage/snap/host-controller/dpuclusterhelper"
	"github.com/nvidia/doca-platform/internal/storage/snap/host-controller/utils"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	// requeueIntervalOnDpuClusterNotConnected is the interval for requeuing when DPU cluster is not connected
	requeueIntervalOnDpuClusterNotConnected time.Duration = time.Second * 5
	// cacheUpdateTimeout is the timeout for waiting for the cache to be updated with the expected state
	cacheUpdateTimeout = 15 * time.Second
	// cacheUpdateCheckInterval is the interval for checking if the cache is updated with the expected state
	cacheUpdateCheckInterval = 250 * time.Millisecond
)

// Options holds common options for controllers
type Options struct {
	// Namespace in the host cluster where the controller runs
	Namespace string
	// Namespace in the DPU cluster to create Volume and VolumeAttachment objects
	TargetNamespace string
}

// DPUClusterResourcesCleanupResult represents the result of the DPU cluster resources cleanup
type DPUClusterResourcesCleanupResult struct {
	// Completed indicates if the cleanup is completed
	Completed bool
	// Reason contains the reason why the cleanup is not completed
	Reason string
}

// FinalizeReconcileResult handles DPU cluster unavailability by setting requeue intervals.
func FinalizeReconcileResult(result ctrl.Result, err error) (ctrl.Result, error) {
	if errors.Is(err, dpuclusterhelper.ErrDPUClusterClientNotAvailable) {
		result.RequeueAfter = requeueIntervalOnDpuClusterNotConnected
		err = nil
	}
	return result, err
}

// ReconcileRequestByOwnedBy returns reconcile requests for objects referenced by owner annotations.
func ReconcileRequestByOwnedBy(ownedByHelper utils.OwnedByHelper, o client.Object, namespace string) []reconcile.Request {
	dpuVolumeKey, err := ownedByHelper.GetOwnedBy(o)
	if err != nil || dpuVolumeKey.Name == "" {
		return nil
	}
	if dpuVolumeKey.Namespace != namespace {
		return nil
	}
	return []reconcile.Request{{NamespacedName: dpuVolumeKey}}
}
