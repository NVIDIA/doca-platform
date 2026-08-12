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

package bmcdump

import "time"

const (
	// DefaultNamespace is the default namespace for DPUDevices and BMC credential Secrets.
	DefaultNamespace = "dpf-operator-system"

	defaultRequestTimeout     = 30 * time.Second
	defaultTaskTimeout        = 30 * time.Minute
	defaultTaskPollInterval   = 5 * time.Second
	defaultEntryRetryCount    = 15
	defaultEntryRetryInterval = 2 * time.Second
	defaultPort               = 443
	urlScheme                 = "https://"
	sharedPasswordSecretName  = "bmc-shared-password"
	passwordSecretDataKey     = "password"

	rootServicePath           = "/redfish/v1"
	dumpLogServicePath        = "/LogServices/Dump"
	collectDiagnosticDataPath = "/Actions/LogService.CollectDiagnosticData"
	clearLogPath              = "/Actions/LogService.ClearLog"

	// cpuDiagnosticsType is the only OEM dump collected. BF4 offers it; BF3 offers
	// unrelated root-of-trust types instead, so its System dump is skipped rather
	// than being sent a type nobody asked for.
	cpuDiagnosticsType = "DiagnosticType=CPUDiagnosticsData"

	// CollectDiagnosticData bodies. Both are fixed, and the tests assert the exact
	// bytes posted to each dump service.
	managerDumpRequestBody = `{"DiagnosticDataType":"Manager"}`
	systemDumpRequestBody  = `{"DiagnosticDataType":"OEM","OEMDiagnosticDataType":"` + cpuDiagnosticsType + `"}`

	// dumpArchiveName assumes zstd, which is what both dump types compress to on
	// current BF3 and BF4 firmware.
	dumpArchiveName = "log_dump.tar.zst"

	managerUnitName = "manager"
	systemUnitName  = "system"

	// maxDumpUnits bounds the per-target context: a BMC yields at most a Manager
	// and a System dump, each running as its own sequential Redfish task.
	maxDumpUnits = 2
)
