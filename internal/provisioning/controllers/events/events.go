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

package events

// Define events for DPU provisioning resources.
const (
	// EventFailedCreateDPUReason indicates that DPU create execution failed.
	EventFailedCreateDPUReason = "FailedCreate"
	// EventSuccessfulCreateDPUReason indicates that DPU is successfully created.
	EventSuccessfulCreateDPUReason = "SuccessfulCreate"
	// EventFailedDeleteDPUReason indicates that DPU delete execution failed.
	EventFailedDeleteDPUReason = "FailedDelete"
	// EventSuccessfulDeleteDPUReason indicates that DPU is successfully deleted.
	EventSuccessfulDeleteDPUReason = "SuccessfulDelete"

	// EventFailedDeleteBFBReason indicates that BFB delete execution failed.
	EventFailedDeleteBFBReason = "FailedDelete"
	// EventFailedDownloadBFBReason indicates that BFB download execution failed.
	EventFailedDownloadBFBReason = "FailedDownload"
	// EventSuccessfulDownloadBFBReason indicates that BFB is successfully downloaded.
	EventSuccessfulDownloadBFBReason = "SuccessfulDownload"
)
