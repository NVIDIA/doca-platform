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

package release

const (
	// V2570 is the version of DPF v25.7.0.
	// TODO: delete this version with the next release v25.10 and add a new one for v25.10.
	V2570 = "v25.7.0"
	// DPFVersionLabelKey is the DPF version label key used for label resources.
	DPFVersionLabelKey = "operator.dpu.nvidia.com/dpf-version"
)

// dpfVersion is the version of the DPF Operator that is currently built.
// This variable is set via ldflags during the build process.
// Example: go build -ldflags="-X github.com/nvidia/doca-platform/internal/release.dpfVersion=v25.7.0"
var dpfVersion = "v0.1.0"

// DPFVersion returns the DPF version the binary is built with.
func DPFVersion() string {
	return dpfVersion
}
