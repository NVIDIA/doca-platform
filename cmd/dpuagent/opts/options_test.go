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

package opts

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestOpts(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "dpuagent opts")
}

var _ = Describe("Options.Validate SPIFFE mode", func() {
	// validSpiffe returns Options that pass Validate, so each case can mutate a
	// single field to drive exactly one failure branch.
	validSpiffe := func() Options {
		return Options{
			SpiffeMode:        true,
			Kubeconfig:        DefaultKubeconfig,
			TokenFilePath:     "/var/lib/dpf/dpuagent/spiffe/token",
			DPUName:           "dpu",
			DPUUID:            "uid",
			DPUFlavor:         "/tmp/flavor.yaml",
			KubeadmSecretName: "secret",
			NICDeviceCount:    DefaultNICDeviceCount,
		}
	}

	DescribeTable("rejects invalid SPIFFE option combinations",
		func(mutate func(*Options), wantErr string) {
			o := validSpiffe()
			mutate(&o)
			Expect(o.Validate()).To(MatchError(ContainSubstring(wantErr)))
		},
		Entry("bootstrap-kubeconfig set with SPIFFE", func(o *Options) {
			o.BootstrapKubeconfig = "/tmp/bootstrap"
		}, "bootstrap-kubeconfig cannot be used with SPIFFE mode"),
		Entry("kubeconfig empty in SPIFFE mode", func(o *Options) {
			o.Kubeconfig = ""
		}, "kubeconfig is required in SPIFFE mode"),
		Entry("token-file-path empty in SPIFFE mode", func(o *Options) {
			o.TokenFilePath = ""
		}, "token-file-path is required in SPIFFE mode"),
	)

	It("accepts a valid SPIFFE configuration", func() {
		Expect(validSpiffe().Validate()).To(Succeed())
	})

	It("accepts SpiffeMode combined with ZeroTrustMode", func() {
		o := validSpiffe()
		o.ZeroTrustMode = true
		Expect(o.Validate()).To(Succeed())
		Expect(o.SpiffeMode).To(BeTrue())
		Expect(o.ZeroTrustMode).To(BeTrue())
	})
})
