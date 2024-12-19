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

	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type State interface {
	Handle(ctx context.Context, client client.Client) (provisioningv1.BFBStatus, error)
}

func GetBFBState(bfb *provisioningv1.BFB, recorder record.EventRecorder) State {
	switch bfb.Status.Phase {
	case provisioningv1.BFBInitializing:
		return &bfbInitializingState{
			bfb,
		}
	case provisioningv1.BFBDownloading:
		return &bfbDownloadingState{
			bfb,
			recorder,
		}
	case provisioningv1.BFBReady:
		return &bfbReadyState{
			bfb,
		}
	case provisioningv1.BFBDeleting:
		return &bfbDeletingState{
			bfb,
			recorder,
		}
	case provisioningv1.BFBError:
		return &bfbErrorState{
			bfb,
		}
	}

	return &bfbInitializingState{
		bfb,
	}
}

func isDeleting(bfb *provisioningv1.BFB) bool {
	return !bfb.DeletionTimestamp.IsZero()
}
