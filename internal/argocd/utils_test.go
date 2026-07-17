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

package argocd

import (
	"strings"
	"testing"

	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/util/validation"
)

func Test_GetApplicationName(t *testing.T) {
	t.Run("returns the plain concatenation when within the DNS-1035 label limit", func(t *testing.T) {
		g := NewWithT(t)
		g.Expect(GetApplicationName("my-cluster", "hbn-7ncp8")).To(Equal("my-cluster-hbn-7ncp8"))
	})

	t.Run("bounds the name to the DNS-1035 label limit when it would overflow", func(t *testing.T) {
		g := NewWithT(t)
		clusterName := strings.Repeat("a", 60)
		dpuServiceName := strings.Repeat("b", 30)

		name := GetApplicationName(clusterName, dpuServiceName)
		g.Expect(len(name)).To(BeNumerically("<=", validation.DNS1035LabelMaxLength))
		// The result must remain a valid DNS-1035 label so it can be used as a Service name.
		g.Expect(validation.IsDNS1035Label(name)).To(BeEmpty())
	})

	t.Run("is deterministic for the same inputs", func(t *testing.T) {
		g := NewWithT(t)
		clusterName := strings.Repeat("a", 60)
		dpuServiceName := strings.Repeat("b", 30)

		g.Expect(GetApplicationName(clusterName, dpuServiceName)).
			To(Equal(GetApplicationName(clusterName, dpuServiceName)))
	})

	t.Run("produces different names for different inputs that share a truncated prefix", func(t *testing.T) {
		g := NewWithT(t)
		clusterName := strings.Repeat("a", 60)

		g.Expect(GetApplicationName(clusterName, "service-one")).
			NotTo(Equal(GetApplicationName(clusterName, "service-two")))
	})
}
