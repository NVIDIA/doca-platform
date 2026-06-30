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

// Package features defines DPF-wide feature gates. Both the dpf-operator
// binary and the dpuservice controller binary register the same gate names
// so a value set on the operator's --feature-gates flag can be propagated
// down to the dpuservice controller deployment without translation.
package features

import (
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/component-base/featuregate"
)

const (
	// PrivilegedPodEnforcement gates the ValidatingAdmissionPolicy that
	// restricts privileged DPUService pods. When enabled, the DPUService
	// reconciler creates the policy/binding/ConfigMap in each
	// DPUCluster. When disabled, the same resources are removed.
	PrivilegedPodEnforcement featuregate.Feature = "PrivilegedPodEnforcement"

	// NSIPathForSFC gates the NodeServiceInterfaces reconciliation path for
	// plain SFC ServiceInterfaceSets (those without a virtualNetwork).
	// When disabled, sets fall back to the legacy ServiceInterface path.
	NSIPathForSFC featuregate.Feature = "NSIPathForSFC"

	// NSIPathForVPC gates the NodeServiceInterfaces reconciliation path for
	// VPC ServiceInterfaceSets (those with a virtualNetwork set).
	// When disabled, sets fall back to the legacy ServiceInterface path.
	NSIPathForVPC featuregate.Feature = "NSIPathForVPC"
)

var (
	// MutableGates is a mutable version of DefaultFeatureGate.
	// Only top-level commands/options setup and the k8s.io/component-base/featuregate/testing package should make use of this.
	// Tests that need to modify featuregate gates for the duration of their test should use:
	//   defer featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.<FeatureName>, <value>)()
	MutableGates featuregate.MutableFeatureGate = featuregate.NewFeatureGate()

	// Gates is a shared global FeatureGate.
	// Top-level commands/options setup that needs to modify this featuregate gate should use DefaultMutableFeatureGate.
	Gates featuregate.FeatureGate = MutableGates
)

func init() {
	runtime.Must(MutableGates.Add(defaultDPFFeatureGates))
}

// defaultDPFFeatureGates contains all known DPF feature gates.
// To add a new feature, define a key for it above and add it here.
var defaultDPFFeatureGates = map[featuregate.Feature]featuregate.FeatureSpec{
	// Every feature should be registered here:
	PrivilegedPodEnforcement: {Default: true, PreRelease: featuregate.Beta},
	NSIPathForSFC:            {Default: false, PreRelease: featuregate.Alpha},
	NSIPathForVPC:            {Default: false, PreRelease: featuregate.Alpha},
}
