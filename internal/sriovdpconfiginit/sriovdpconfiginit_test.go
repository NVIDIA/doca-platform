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

package sriovdpconfiginit

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	generatedConfig = `{"resourceList":[{"resourceName":"my_sf","resourcePrefix":"nvidia.com","deviceType":"auxNetDevice"}]}`
	defaultConfig   = `{"resourceList":[{"resourceName":"bf_sf","resourcePrefix":"nvidia.com","deviceType":"auxNetDevice"}]}`
)

var _ = Describe("Resolve", func() {
	var (
		dir  string
		opts Options
	)

	BeforeEach(func() {
		dir = GinkgoT().TempDir()
		opts = Options{
			GeneratedPath: filepath.Join(dir, "generated", "config.json"),
			DefaultPath:   filepath.Join(dir, "default", "config.json"),
			ActivePath:    filepath.Join(dir, "active", "config.json"),
		}
		writeFile(opts.DefaultPath, defaultConfig)
	})

	active := func() string {
		data, err := os.ReadFile(opts.ActivePath)
		Expect(err).NotTo(HaveOccurred())
		return string(data)
	}

	It("uses the generated config when dpu-agent wrote one", func() {
		writeFile(opts.GeneratedPath, generatedConfig)

		source, err := Resolve(opts)
		Expect(err).NotTo(HaveOccurred())
		Expect(source).To(Equal(SourceGenerated))
		Expect(active()).To(Equal(generatedConfig))
	})

	It("replaces an empty resourceList with a placeholder the plugin can start with", func() {
		writeFile(opts.GeneratedPath, `{"resourceList":[]}`)

		source, err := Resolve(opts)
		Expect(err).NotTo(HaveOccurred())
		Expect(source).To(Equal(SourceGenerated))
		Expect(active()).To(Equal(dummyResourceConfig))
	})

	It("falls back to the default when dpu-agent wrote nothing", func() {
		source, err := Resolve(opts)
		Expect(err).NotTo(HaveOccurred())
		Expect(source).To(Equal(SourceDefault))
		Expect(active()).To(Equal(defaultConfig))
	})

	It("fails rather than falling back when the generated config does not parse", func() {
		writeFile(opts.GeneratedPath, `{"resourceList":[`)

		_, err := Resolve(opts)
		Expect(err).To(MatchError(ContainSubstring("not usable")))
		Expect(opts.ActivePath).NotTo(BeAnExistingFile())
	})

	It("fails rather than falling back when the generated config is empty", func() {
		writeFile(opts.GeneratedPath, "")

		_, err := Resolve(opts)
		Expect(err).To(HaveOccurred())
		Expect(opts.ActivePath).NotTo(BeAnExistingFile())
	})

	It("fails rather than falling back when the generated config is JSON but not a config", func() {
		writeFile(opts.GeneratedPath, `{"pools":[]}`)

		_, err := Resolve(opts)
		Expect(err).To(MatchError(ContainSubstring("no resourceList key")))
		Expect(opts.ActivePath).NotTo(BeAnExistingFile())
	})

	It("fails when neither a generated nor a default config is available", func() {
		Expect(os.Remove(opts.DefaultPath)).To(Succeed())

		_, err := Resolve(opts)
		Expect(err).To(MatchError(ContainSubstring("failed to read default config")))
	})

	It("fails when the default config it falls back to does not parse", func() {
		writeFile(opts.DefaultPath, "not json")

		_, err := Resolve(opts)
		Expect(err).To(MatchError(ContainSubstring("not usable")))
	})

	It("overwrites a config left behind by an earlier run", func() {
		writeFile(opts.ActivePath, "stale")
		writeFile(opts.GeneratedPath, generatedConfig)

		_, err := Resolve(opts)
		Expect(err).NotTo(HaveOccurred())
		Expect(active()).To(Equal(generatedConfig))
	})

	It("requires all paths to be set", func() {
		_, err := Resolve(Options{GeneratedPath: opts.GeneratedPath})
		Expect(err).To(MatchError(ContainSubstring("must all be set")))
	})
})

var _ = Describe("resourceNames", func() {
	It("qualifies each resource with its prefix", func() {
		Expect(resourceNames([]byte(generatedConfig))).To(Equal([]string{"nvidia.com/my_sf"}))
	})

	It("returns an empty list when no resources are configured", func() {
		Expect(resourceNames([]byte(`{"resourceList":[]}`))).To(BeEmpty())
	})

	It("omits the separator when a resource has no prefix", func() {
		Expect(resourceNames([]byte(`{"resourceList":[{"resourceName":"bf_sf"}]}`))).To(Equal([]string{"bf_sf"}))
	})
})

// writeFile creates path's parent directories if needed and writes content.
func writeFile(path, content string) {
	GinkgoHelper()
	Expect(os.MkdirAll(filepath.Dir(path), 0755)).To(Succeed())
	Expect(os.WriteFile(path, []byte(content), 0644)).To(Succeed())
}
