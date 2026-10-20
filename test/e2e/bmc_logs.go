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
	"errors"
	"path/filepath"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/dpfctl/bmcdump"
	redfishclient "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/client"

	. "github.com/onsi/ginkgo/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func collectBMCLogsZT(ctx context.Context, specName, artifactsDir string, testClient client.Client, _ *systemTestInput) error {
	devices := &provisioningv1.DPUDeviceList{}
	if err := testClient.List(ctx, devices, client.InNamespace(dpfOperatorSystemNamespace)); err != nil {
		return err
	}
	// One device at a time, so that a fallback below is scoped to the device that needs it.
	var errs []error
	for _, device := range devices.Items {
		if err := collectDeviceBMCLogs(ctx, filepath.Join(artifactsDir, "bmc", specName), testClient, device.Name); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// collectDeviceBMCLogs verifies first, so CI keeps exercising the path users run. A BMC that has not
// been issued its DPF certificate yet (factory reset, early provisioning) cannot be verified, and
// its logs are the ones needed then, so that device alone is retried unverified, and only for that
// failure. The verified attempt failed at the first request, before it created any dump, so the
// retry has nothing to preserve and clears existing entries like any other collection.
func collectDeviceBMCLogs(ctx context.Context, outputDir string, testClient client.Client, device string) error {
	opts := bmcdump.CollectOptions{
		Namespace:     dpfOperatorSystemNamespace,
		OutputDir:     outputDir,
		Devices:       []string{device},
		ClearExisting: true,
		Quiet:         true,
	}
	err := bmcdump.Collect(ctx, testClient, opts)
	if !redfishclient.IsBMCServerCertUntrusted(err) {
		return err
	}
	GinkgoLogr.Error(err, "verified BMC log collection failed, retrying without TLS verification", "device", device)
	opts.InsecureSkipTLSVerify = true
	return bmcdump.Collect(ctx, testClient, opts)
}
