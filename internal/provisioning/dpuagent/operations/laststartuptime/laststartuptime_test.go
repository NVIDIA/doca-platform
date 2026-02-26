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

package laststartuptime

import (
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ReportLastStartupTime Operation", func() {
	It("should never be skipped", func() {
		operation := &ReportLastStartupTime{}
		Expect(operation.ShouldSkip(&operations.Context{})).To(BeFalse())
	})

	It("should always update status before continue", func() {
		operation := &ReportLastStartupTime{}
		Expect(operation.ShouldUpdateStatusBeforeContinue(&operations.Context{})).To(BeTrue())
	})

	It("should set LastStartupTime in status", func() {
		operation := &ReportLastStartupTime{}
		optCtx := &operations.Context{
			Status: provisioningv1.AgentStatus{},
		}
		err := operation.Execute(ctx, optCtx)
		Expect(err).NotTo(HaveOccurred())
		Expect(optCtx.Status.LastStartupTime).NotTo(BeNil())
	})
})
