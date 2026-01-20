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

import (
	"os"
	"strings"
)

const (
	// LastReleasedDPFGAVersion is the latest release version of DPF.
	LastReleasedDPFGAVersion = "v25.10.1"

	// DPFVersionLabelKey is the DPF version label key used for label resources.
	DPFVersionLabelKey = "operator.dpu.nvidia.com/dpf-version"

	// dpfVersionFile is the file path where the DPF version is stored at runtime.
	// Loading version from a file instead of build-time ldflags improves Docker
	// build cache efficiency by preventing version changes from invalidating layers.
	dpfVersionFile = "/etc/dpf-version"
)

// dpfVersion is the version of the DPF Operator that is currently built.
// We read the version from the dpfVersionFile during initialization.
var dpfVersion = "v0.1.0"

func init() {
	data, err := os.ReadFile(dpfVersionFile)
	if err != nil {
		return
	}
	dpfVersion = strings.TrimSpace(string(data))
}

// DPFVersion returns the DPF version the binary is built with.
func DPFVersion() string {
	return dpfVersion
}
