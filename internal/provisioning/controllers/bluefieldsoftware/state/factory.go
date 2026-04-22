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

package state

import (
	"context"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type State interface {
	Handle(ctx context.Context, client client.Client) error
}

func GetBlueFieldSoftwareState(bfs *provisioningv1.BlueFieldSoftware, recorder record.EventRecorder) State {
	switch bfs.Status.Phase {
	case provisioningv1.BlueFieldSoftwareInitializing:
		return &blueFieldSoftwareInitializingState{
			bfs,
		}
	case provisioningv1.BlueFieldSoftwareDownloading:
		return &blueFieldSoftwareDownloadingState{
			bfs,
			recorder,
		}
	case provisioningv1.BlueFieldSoftwareExtracting:
		return &blueFieldSoftwareExtractingState{
			bfs:      bfs,
			recorder: recorder,
		}
	case provisioningv1.BlueFieldSoftwareReady:
		return &blueFieldSoftwareReadyState{
			bfs,
			recorder,
		}
	case provisioningv1.BlueFieldSoftwareDeleting:
		return &blueFieldSoftwareDeletingState{
			bfs,
			recorder,
		}
	case provisioningv1.BlueFieldSoftwareError:
		return &blueFieldSoftwareErrorState{
			bfs,
		}
	}

	return &blueFieldSoftwareInitializingState{
		bfs,
	}
}

func isDeleting(bfs *provisioningv1.BlueFieldSoftware) bool {
	return bfs != nil && !bfs.DeletionTimestamp.IsZero()
}
