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

package bash

import (
	"os"
	"os/exec"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Cmd", func() {
	Context("RunBash", Label("RunBash"), func() {
		It("should execute command and return stdout", func() {
			stdout, stderr, err := Run("echo hello")
			Expect(err).NotTo(HaveOccurred())
			Expect(stdout.String()).To(Equal("hello\n"))
			Expect(stderr.String()).To(BeEmpty())
		})

		It("should capture stderr output", func() {
			stdout, stderr, err := Run("echo error >&2")
			Expect(err).NotTo(HaveOccurred())
			Expect(stdout.String()).To(BeEmpty())
			Expect(stderr.String()).To(Equal("error\n"))
		})

		It("should capture both stdout and stderr", func() {
			stdout, stderr, err := Run("echo out && echo err >&2")
			Expect(err).NotTo(HaveOccurred())
			Expect(stdout.String()).To(Equal("out\n"))
			Expect(stderr.String()).To(Equal("err\n"))
		})

		It("should return error for failed command", func() {
			stdout, stderr, err := Run("exit 1")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to run command"))
			Expect(stdout.String()).To(BeEmpty())
			Expect(stderr.String()).To(BeEmpty())
		})

		It("should return error for non-existent command", func() {
			_, _, err := Run("nonexistent_command_12345")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to run command"))
		})

		It("should handle command with arguments", func() {
			stdout, stderr, err := Run("echo -n test")
			Expect(err).NotTo(HaveOccurred())
			Expect(stdout.String()).To(Equal("test"))
			Expect(stderr.String()).To(BeEmpty())
		})

		It("should handle command with pipes", func() {
			stdout, stderr, err := Run("echo 'hello world' | grep hello")
			Expect(err).NotTo(HaveOccurred())
			Expect(stdout.String()).To(Equal("hello world\n"))
			Expect(stderr.String()).To(BeEmpty())
		})

		It("should return error and capture stderr for failing command with output", func() {
			stdout, stderr, err := Run("echo fail_output >&2; exit 2")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to run command"))
			Expect(stdout.String()).To(BeEmpty())
			Expect(stderr.String()).To(Equal("fail_output\n"))
		})

		It("should handle empty command string", func() {
			stdout, stderr, err := Run("")
			Expect(err).NotTo(HaveOccurred())
			Expect(stdout.String()).To(BeEmpty())
			Expect(stderr.String()).To(BeEmpty())
		})

		It("should handle multiline output", func() {
			stdout, stderr, err := Run("echo 'line1'; echo 'line2'; echo 'line3'")
			Expect(err).NotTo(HaveOccurred())
			Expect(stdout.String()).To(Equal("line1\nline2\nline3\n"))
			Expect(stderr.String()).To(BeEmpty())
		})

		It("should handle command with environment variable expansion", func() {
			stdout, stderr, err := Run("export TEST_VAR=testvalue && echo $TEST_VAR")
			Expect(err).NotTo(HaveOccurred())
			Expect(stdout.String()).To(Equal("testvalue\n"))
			Expect(stderr.String()).To(BeEmpty())
		})

		It("should apply command options before executing", func() {
			stdout, stderr, err := RunWithOptions("echo $DPF_TEST_VAR", func(cmd *exec.Cmd) {
				cmd.Env = append(os.Environ(), "DPF_TEST_VAR=from-option")
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(stdout.String()).To(Equal("from-option\n"))
			Expect(stderr.String()).To(BeEmpty())
		})
	})
})
