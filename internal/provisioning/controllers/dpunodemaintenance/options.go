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

package dpunodemaintenance

import "time"

type DPUNodeMaintenanceOptions struct {
	// MultiDPUOperationsSyncWaitTime is the wait time between DPUs sync operations on the same node.
	MultiDPUOperationsSyncWaitTime time.Duration
	// MaxUnavailableDPUNodes is the maximum number of DPUNodes that are unavailable during the node effect period.
	MaxUnavailableDPUNodes int32
}
