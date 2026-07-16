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

	"github.com/nvidia/doca-platform/internal/dpfctl/bmcdump"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

func collectBMCLogsZT(ctx context.Context, specName, artifactsDir string, testClient client.Client, _ *systemTestInput) error {
	return bmcdump.Collect(ctx, testClient, bmcdump.CollectOptions{
		Namespace:             dpfOperatorSystemNamespace,
		OutputDir:             filepath.Join(artifactsDir, "bmc", specName),
		ClearExisting:         true,
		InsecureSkipTLSVerify: true,
		Quiet:                 true,
	})
}
