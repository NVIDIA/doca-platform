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

package main

import (
	"github.com/nvidia/doca-platform/cmd/dpfctl/cmd"
	"github.com/nvidia/doca-platform/internal/release"
)

// version is set by the Makefile.
var version = "unknown"

func main() {
	// If the version is not set, get it from the release package.
	// During dpfctl release we set the version via go build ldflags.
	// If it's builtin our controllers, we read it from the /etc/dpf-version file.
	if version == "unknown" {
		version = release.DPFVersion()
	}
	cmd.Execute(version)
}
