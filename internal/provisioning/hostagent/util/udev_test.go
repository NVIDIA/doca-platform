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
	"fmt"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("EnsureNMUnmanagedUdevRule", func() {
	var (
		origPath   string
		origRunner func(string, ...string) ([]byte, error)
		tempDir    string
		commands   [][]string
	)

	BeforeEach(func() {
		var err error
		tempDir, err = os.MkdirTemp("", "udev-test-*")
		Expect(err).NotTo(HaveOccurred())

		origPath = nmUnmanagedRulesPath
		origRunner = udevRunner

		commands = nil
		udevRunner = func(name string, args ...string) ([]byte, error) {
			commands = append(commands, append([]string{name}, args...))
			return nil, nil
		}
	})

	AfterEach(func() {
		nmUnmanagedRulesPath = origPath
		udevRunner = origRunner
		os.RemoveAll(tempDir)
	})

	setRulesPath := func() string {
		p := filepath.Join(tempDir, "10-nm-unmanaged.rules")
		nmUnmanagedRulesPath = p
		return p
	}

	It("should write the udev rule file and reload rules", func() {
		rulesFile := setRulesPath()

		err := EnsureNMUnmanagedUdevRule()
		Expect(err).NotTo(HaveOccurred())

		content, err := os.ReadFile(rulesFile)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(content)).To(Equal(nmUnmanagedRulesContent))

		Expect(commands).To(HaveLen(2))
		Expect(commands[0]).To(Equal([]string{"udevadm", "control", "--reload-rules"}))
		Expect(commands[1]).To(Equal([]string{"udevadm", "trigger", "--subsystem-match=net"}))
	})

	It("should be idempotent - not rewrite if content matches", func() {
		rulesFile := setRulesPath()

		err := os.MkdirAll(filepath.Dir(rulesFile), 0755)
		Expect(err).NotTo(HaveOccurred())
		err = os.WriteFile(rulesFile, []byte(nmUnmanagedRulesContent), 0644)
		Expect(err).NotTo(HaveOccurred())

		err = EnsureNMUnmanagedUdevRule()
		Expect(err).NotTo(HaveOccurred())

		Expect(commands).To(HaveLen(2))
	})

	It("should overwrite if content differs", func() {
		rulesFile := setRulesPath()

		err := os.MkdirAll(filepath.Dir(rulesFile), 0755)
		Expect(err).NotTo(HaveOccurred())
		err = os.WriteFile(rulesFile, []byte("old content"), 0644)
		Expect(err).NotTo(HaveOccurred())

		err = EnsureNMUnmanagedUdevRule()
		Expect(err).NotTo(HaveOccurred())

		content, err := os.ReadFile(rulesFile)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(content)).To(Equal(nmUnmanagedRulesContent))
	})

	It("should create parent directories if they don't exist", func() {
		nmUnmanagedRulesPath = filepath.Join(tempDir, "subdir", "rules.d", "10-nm-unmanaged.rules")

		err := EnsureNMUnmanagedUdevRule()
		Expect(err).NotTo(HaveOccurred())

		content, err := os.ReadFile(nmUnmanagedRulesPath)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(content)).To(Equal(nmUnmanagedRulesContent))
	})

	It("should return error if udevadm reload fails", func() {
		setRulesPath()

		udevRunner = func(name string, args ...string) ([]byte, error) {
			if len(args) > 0 && args[0] == "control" {
				return []byte("reload failed"), fmt.Errorf("exit status 1")
			}
			return nil, nil
		}

		err := EnsureNMUnmanagedUdevRule()
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("udevadm control --reload-rules failed"))
	})

	It("should return error if udevadm trigger fails", func() {
		setRulesPath()

		udevRunner = func(name string, args ...string) ([]byte, error) {
			if len(args) > 0 && args[0] == "trigger" {
				return []byte("trigger failed"), fmt.Errorf("exit status 1")
			}
			return nil, nil
		}

		err := EnsureNMUnmanagedUdevRule()
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("udevadm trigger --subsystem-match=net failed"))
	})
})
