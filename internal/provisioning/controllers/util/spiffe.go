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

package util

import (
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
)

// SpiffeEnabled reports whether the cluster DPFOperatorConfig opts into the SPIFFE
// DPU Agent identity flow. It is the single gate the provisioning controllers use to
// decide which identity mode to stamp onto a freshly initialized DPU.
func SpiffeEnabled(cfg *operatorv1.DPFOperatorConfig) bool {
	return cfg != nil && cfg.Spec.Security != nil && cfg.Spec.Security.SPIFFE != nil
}

// IsSpiffeDPU reports whether the DPU has been stamped with the SPIFFE identity mode.
// A DPU with an unset IdentityMode is treated as bootstrap-token (legacy) and is not a
// SPIFFE DPU.
func IsSpiffeDPU(dpu *provisioningv1.DPU) bool {
	return dpu != nil && dpu.Status.IdentityMode != nil &&
		*dpu.Status.IdentityMode == provisioningv1.IdentityModeSpiffe
}

// IsBootstrapTokenDPU reports whether the DPU uses the bootstrap-token identity mode.
// An unset (nil) IdentityMode is treated semantically as bootstrap-token per the API
// contract, so legacy DPUs provisioned before SPIFFE return true.
func IsBootstrapTokenDPU(dpu *provisioningv1.DPU) bool {
	if dpu == nil {
		return false
	}
	return dpu.Status.IdentityMode == nil ||
		*dpu.Status.IdentityMode == provisioningv1.IdentityModeBootstrapToken
}
