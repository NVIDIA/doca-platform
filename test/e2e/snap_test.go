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

package e2e

import (
	. "github.com/onsi/ginkgo/v2"
)

func SNAPBeforeSuite() {
	By("Setting SNAP configs for the test")
}

//nolint:dupl
var _ = Describe("DPF SNAP tests", Labels{dpfSystemLabel, snapLabel}, func() {

	BeforeEach(func() {
		By("Placeholder for the SNAP BeforeEach test condition check/action")
	})
	AfterEach(func() {
		By("Placeholder for the SNAP AfterEach test condition check/action/cleanup")
	})

	Context("Placeholder for the SNAP tests", Labels{requiresNodesLabel}, Serial, func() {
	})
})
