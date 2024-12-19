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

package state

import (
	"context"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/allocator"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

type State interface {
	Handle(ctx context.Context, client client.Client, option util.DPUOptions) (provisioningv1.DPUStatus, error)
}

func GetDPUState(dpu *provisioningv1.DPU, alloc allocator.Allocator) State {
	switch dpu.Status.Phase {
	case provisioningv1.DPUInitializing:
		return &dpuInitializingState{
			dpu,
			alloc,
		}
	case provisioningv1.DPUNodeEffect:
		return &dpuNodeEffectState{
			dpu,
		}
	case provisioningv1.DPUPending:
		return &dpuPendingState{
			dpu,
		}
	case provisioningv1.DPUDMSDeployment:
		return &dmsDeploymentState{
			dpu,
		}
	case provisioningv1.DPUHostNetworkConfiguration:
		return &dpuHostNetworkConfigState{
			dpu,
		}
	case provisioningv1.DPUOSInstalling:
		return &dpuOSInstallingState{
			dpu,
		}
	case provisioningv1.DPUReady:
		return &dpuReadyState{
			dpu,
		}
	case provisioningv1.DPUError:
		return &dpuErrorState{
			dpu,
		}
	case provisioningv1.DPUDeleting:
		return &dpuDeletingState{
			dpu,
			alloc,
		}
	case provisioningv1.DPURebooting:
		return &dpuRebootingState{
			dpu,
		}
	case provisioningv1.DPUClusterConfig:
		return &dpuClusterConfig{
			dpu,
		}
	}

	return &dpuInitializingState{
		dpu,
		alloc,
	}
}

func isDeleting(dpu *provisioningv1.DPU) bool {
	return !dpu.DeletionTimestamp.IsZero()
}
