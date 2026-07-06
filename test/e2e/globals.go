/*
Copyright 2024 NVIDIA

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
	"github.com/nvidia/doca-platform/test/e2e/cleanup"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ArtifactsDir is the path where test artifacts will be stored.
var ArtifactsDir string

// externalTest is the path to the external test script, set via the
// -e2e.externalTestScript flag in e2e_suite_test.go's init().
var externalTest string

// CollectResources indicates whether to collect logs an objects after an e2e test run.
var CollectResources = true

var (
	// CleanupFlags holds all flags to control skip cleanup behavior
	CleanupFlags   *cleanup.CleanupFlags
	CleanupTracker *cleanup.Tracker
	TestClient     client.Client
	RestConfig     *rest.Config
	Clientset      *kubernetes.Clientset
	Ctx            = ctrl.SetupSignalHandler()
	Conf           *Config
)

// input is the singleton populated by SetInput() and consumed by every DPF
// System - Core test function. External callers should use the value
// returned by SetInput() rather than this package-private variable.
var input *SystemTestInput

// VPCOVNInput is the singleton populated by VPCOVNBeforeSuite (via
// VPCOVNTestInput.ApplyVPCOVNConfig) and consumed by the VPCOVN test funcs.
var VPCOVNInput = &VPCOVNTestInput{}
