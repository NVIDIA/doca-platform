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

package e2e

import (
	"context"
	"path/filepath"
	"time"

	"github.com/nvidia/doca-platform/internal/dpfctl/sosreport"

	. "github.com/onsi/ginkgo/v2"
)

// collectSOSReports uses sosreport.Collect to gather SOS reports from all host
// and DPU cluster nodes. It is guarded by sync.Once so that it runs at most
// once per test suite, triggered on the first test failure.
func collectSOSReports(ctx context.Context, outputDir string) error {
	By("Collecting SOS reports via dpfctl sosreport")
	return sosreport.Collect(ctx, sosreport.CollectOptions{
		StartOptions: sosreport.StartOptions{
			Namespace:    "default",
			Image:        "ghcr.io/nvidia/sosreport:latest",
			Cluster:      "all",
			NodeSelector: "node-role.kubernetes.io/control-plane!=",
			Timeout:      20 * time.Minute,
			Output:       sosreport.OutputLocal,
		},
		OutputDir: filepath.Join(outputDir, "sosreports"),
	})
}
