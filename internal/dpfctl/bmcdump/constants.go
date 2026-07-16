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

	defaultRequestTimeout        = 30 * time.Second
	defaultTaskTimeout           = 30 * time.Minute
	defaultTaskPollInterval      = 5 * time.Second
	defaultEntryRetryCount       = 15
	defaultEntryRetryInterval    = 2 * time.Second
	defaultPort                  = 443
	defaultUser                  = "admin"
	dumpPath                     = "/redfish/v1/Managers/BlueField_BMC_0/LogServices/Dump"
	dumpClearPath                = dumpPath + "/Actions/LogService.ClearLog"
	dumpCollectDiagnosticPath    = dumpPath + "/Actions/LogService.CollectDiagnosticData"
	dumpEntriesPath              = dumpPath + "/Entries"
	urlScheme                    = "https://"
	sharedPasswordSecretName     = "bmc-shared-password"
	passwordSecretDataKey        = "password"
	collectDiagnosticRequestBody = `{"DiagnosticDataType":"Manager"}`
)
