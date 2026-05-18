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

package packages

import (
	"bytes"
	"errors"
	"os/exec"
	"strings"

	"github.com/nvidia/doca-platform/internal/provisioning/utils/bash"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type aptCommand struct {
	cmd string
	env map[string]string
}

type fakeAptRunner struct {
	commands []aptCommand
	output   string
	err      error
}

func (f *fakeAptRunner) run(cmd string, opts ...bash.CmdOption) (bytes.Buffer, bytes.Buffer, error) {
	f.commands = append(f.commands, aptCommand{cmd: cmd, env: decoratedAPTEnvironment(cmd, opts...)})
	if f.err != nil {
		return bytes.Buffer{}, bytes.Buffer{}, f.err
	}
	return *bytes.NewBufferString(f.output), bytes.Buffer{}, nil
}

func decoratedAPTEnvironment(cmd string, opts ...bash.CmdOption) map[string]string {
	execCmd := exec.Command("bash", "-c", cmd)
	for _, opt := range opts {
		if opt != nil {
			opt(execCmd)
		}
	}
	if execCmd.Env == nil {
		return nil
	}

	aptKeys := map[string]struct{}{
		"DEBIAN_FRONTEND":          {},
		"APT_LISTCHANGES_FRONTEND": {},
	}
	env := map[string]string{}
	for _, entry := range execCmd.Env {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if _, keep := aptKeys[key]; keep {
			env[key] = value
		}
	}
	return env
}

var _ = Describe("APT package manager", func() {
	It("should return installed package version", func() {
		runner := &fakeAptRunner{output: "install ok installed 1.2.3\n"}
		manager := &aptPackageManager{runBash: runner.run}

		version, installed, err := manager.InstalledPackageVersion("doca-extra")

		Expect(err).NotTo(HaveOccurred())
		Expect(installed).To(BeTrue())
		Expect(version).To(Equal("1.2.3"))
		Expect(runner.commands).To(Equal([]aptCommand{
			{cmd: "dpkg-query -W -f='${Status} ${Version}' 'doca-extra' 2>/dev/null || true"},
		}))
	})

	It("should report not installed when dpkg-query has no installed status", func() {
		runner := &fakeAptRunner{}
		manager := &aptPackageManager{runBash: runner.run}

		version, installed, err := manager.InstalledPackageVersion("doca-extra")

		Expect(err).NotTo(HaveOccurred())
		Expect(installed).To(BeFalse())
		Expect(version).To(BeEmpty())
	})

	It("should update apt metadata with proxy and repo file constraints", func() {
		runner := &fakeAptRunner{}
		manager := &aptPackageManager{runBash: runner.run, proxyURL: "http://127.0.0.1:11030/"}

		err := manager.Update("/etc/apt/sources.list.d/doca.list")

		Expect(err).NotTo(HaveOccurred())
		Expect(runner.commands).To(Equal([]aptCommand{
			{
				cmd: "apt-get update -o Dir::Etc::sourcelist='/etc/apt/sources.list.d/doca.list' -o Dir::Etc::sourceparts='-' -o APT::Get::List-Cleanup=0 -o Acquire::Connect::AddrConfig=false -o Acquire::http::Proxy='http://127.0.0.1:11030/' -o Acquire::https::Proxy='http://127.0.0.1:11030/' -o Acquire::http::Proxy::fe80::1%tmfifo_net0=DIRECT",
				env: map[string]string{
					"DEBIAN_FRONTEND":          "noninteractive",
					"APT_LISTCHANGES_FRONTEND": "none",
				},
			},
		}))
	})

	It("should install a package target", func() {
		runner := &fakeAptRunner{}
		manager := &aptPackageManager{runBash: runner.run, proxyURL: "http://127.0.0.1:11030/"}

		err := manager.Install("doca-extra=1.2.3", "")

		Expect(err).NotTo(HaveOccurred())
		Expect(runner.commands).To(Equal([]aptCommand{
			{
				cmd: "apt-get install -y -o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold -o Acquire::Connect::AddrConfig=false -o Acquire::http::Proxy='http://127.0.0.1:11030/' -o Acquire::https::Proxy='http://127.0.0.1:11030/' -o Acquire::http::Proxy::fe80::1%tmfifo_net0=DIRECT 'doca-extra=1.2.3'",
				env: map[string]string{
					"DEBIAN_FRONTEND":          "noninteractive",
					"APT_LISTCHANGES_FRONTEND": "none",
				},
			},
		}))
	})

	It("should list available package versions", func() {
		runner := &fakeAptRunner{
			output: "doca-extra | 1.9.0 | repo\n" +
				"doca-extra | 2.0.0 | repo\n",
		}
		manager := &aptPackageManager{runBash: runner.run}

		versions, err := manager.AvailableVersions("doca-extra", "/etc/apt/sources.list.d/doca.list")

		Expect(err).NotTo(HaveOccurred())
		Expect(versions).To(Equal([]string{"1.9.0", "2.0.0"}))
		Expect(runner.commands).To(Equal([]aptCommand{
			{cmd: "apt-cache madison 'doca-extra' -o Dir::Etc::sourcelist='/etc/apt/sources.list.d/doca.list' -o Dir::Etc::sourceparts='-' -o APT::Get::List-Cleanup=0"},
		}))
	})

	It("should compare package versions through dpkg", func() {
		successRunner := &fakeAptRunner{}
		successManager := &aptPackageManager{runBash: successRunner.run}
		Expect(successManager.VersionCompare("2.0.0", "ge", "1.9.0")).To(BeTrue())
		Expect(successRunner.commands).To(Equal([]aptCommand{
			{cmd: "dpkg --compare-versions '2.0.0' ge '1.9.0'"},
		}))

		failureRunner := &fakeAptRunner{err: errors.New("comparison failed")}
		failureManager := &aptPackageManager{runBash: failureRunner.run}
		Expect(failureManager.VersionCompare("1.9.0", "ge", "2.0.0")).To(BeFalse())
	})
})
