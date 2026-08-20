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

// Package dpuflavor provides helpers for adapting DPUFlavor fixtures in tests.
package dpuflavor

import (
	"slices"
	"strings"
)

// WithConsoleKernelParameter returns a copy of kernelParameters with all
// existing console parameters replaced by a single console parameter.
func WithConsoleKernelParameter(kernelParameters []string, console string) []string {
	filtered := slices.DeleteFunc(
		slices.Clone(kernelParameters),
		func(parameter string) bool {
			return strings.HasPrefix(strings.TrimSpace(parameter), "console=")
		},
	)
	return append(filtered, "console="+console)
}
