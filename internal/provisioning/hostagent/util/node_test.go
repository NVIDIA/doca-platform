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

package util

import (
	"os"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Node", func() {
	Context("GetNodeName", Label("GetNodeName"), func() {
		var originalNodeName string
		var originalK8sNodeName string
		var wasNodeNameSet bool
		var wasK8sNodeNameSet bool

		BeforeEach(func() {
			originalNodeName, wasNodeNameSet = os.LookupEnv(NodeNameEnv)
			originalK8sNodeName, wasK8sNodeNameSet = os.LookupEnv(K8sNodeNameEnv)
		})

		AfterEach(func() {
			if wasNodeNameSet {
				Expect(os.Setenv(NodeNameEnv, originalNodeName)).To(Succeed())
			} else {
				Expect(os.Unsetenv(NodeNameEnv)).To(Succeed())
			}
			if wasK8sNodeNameSet {
				Expect(os.Setenv(K8sNodeNameEnv, originalK8sNodeName)).To(Succeed())
			} else {
				Expect(os.Unsetenv(K8sNodeNameEnv)).To(Succeed())
			}
		})

		It("should return node name from NODE_NAME env var when set", func() {
			Expect(os.Setenv(NodeNameEnv, "test-node-name")).To(Succeed())
			name, truncated, err := GetNodeName()
			Expect(err).NotTo(HaveOccurred())
			Expect(name).To(Equal("test-node-name"))
			Expect(truncated).To(BeFalse())
		})

		It("should convert node name to lowercase", func() {
			Expect(os.Setenv(NodeNameEnv, "TEST-NODE-NAME")).To(Succeed())
			name, truncated, err := GetNodeName()
			Expect(err).NotTo(HaveOccurred())
			Expect(name).To(Equal("test-node-name"))
			Expect(truncated).To(BeFalse())
		})

		It("should trim whitespace from node name", func() {
			Expect(os.Setenv(NodeNameEnv, "  test-node-name  ")).To(Succeed())
			name, truncated, err := GetNodeName()
			Expect(err).NotTo(HaveOccurred())
			Expect(name).To(Equal("test-node-name"))
			Expect(truncated).To(BeFalse())
		})

		It("should truncate node name longer than MaximumHostNameLength", func() {
			longName := strings.Repeat("a", MaximumHostNameLength+10)
			Expect(os.Setenv(NodeNameEnv, longName)).To(Succeed())
			name, truncated, err := GetNodeName()
			Expect(err).NotTo(HaveOccurred())
			Expect(name).To(HaveLen(MaximumHostNameLength))
			Expect(truncated).To(BeTrue())
		})

		It("should not truncate node name exactly at MaximumHostNameLength", func() {
			exactName := strings.Repeat("a", MaximumHostNameLength)
			Expect(os.Setenv(NodeNameEnv, exactName)).To(Succeed())
			name, truncated, err := GetNodeName()
			Expect(err).NotTo(HaveOccurred())
			Expect(name).To(HaveLen(MaximumHostNameLength))
			Expect(truncated).To(BeFalse())
		})

		It("should not truncate node name shorter than MaximumHostNameLength", func() {
			shortName := "short-node"
			Expect(os.Setenv(NodeNameEnv, shortName)).To(Succeed())
			name, truncated, err := GetNodeName()
			Expect(err).NotTo(HaveOccurred())
			Expect(name).To(Equal(shortName))
			Expect(truncated).To(BeFalse())
		})

		It("should fall back to hostname when NODE_NAME env var is not set", func() {
			Expect(os.Unsetenv(NodeNameEnv)).To(Succeed())
			name, _, err := GetNodeName()
			Expect(err).NotTo(HaveOccurred())
			Expect(name).NotTo(BeEmpty())
			Expect(name).To(Equal(strings.ToLower(name)))
		})
	})

	Context("Constants", Label("Constants"), func() {
		It("should have correct DPFNamespace value", func() {
			Expect(DPFNamespace).To(Equal("dpf-operator-system"))
		})

		It("should have correct MaximumHostNameLength value", func() {
			Expect(MaximumHostNameLength).To(Equal(48))
		})

		It("should have correct NodeNameEnv value", func() {
			Expect(NodeNameEnv).To(Equal("NODE_NAME"))
		})

		It("should have correct K8sNodeNameEnv value", func() {
			Expect(K8sNodeNameEnv).To(Equal("KUBERNETES_NODE_NAME"))
		})
	})
})
