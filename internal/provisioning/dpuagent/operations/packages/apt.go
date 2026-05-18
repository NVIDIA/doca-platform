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
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/nvidia/doca-platform/internal/provisioning/utils/bash"
)

type runBashFunc func(cmd string, opts ...bash.CmdOption) (bytes.Buffer, bytes.Buffer, error)

// APTPackageManager provides Debian package operations backed by APT and dpkg.
type APTPackageManager interface {
	SetProxy(proxyURL string)
	InstalledPackageVersion(name string) (string, bool, error)
	Update(repoFileRef string) error
	Install(installTarget string, repoFileRef string) error
	AvailableVersions(name string, repoFileRef string) ([]string, error)
	VersionCompare(version string, op string, requestedVersion string) bool
}

type aptPackageManager struct {
	runBash  runBashFunc
	proxyURL string
}

func newAPTPackageManager() APTPackageManager {
	return &aptPackageManager{
		runBash: bash.RunWithOptions,
	}
}

func (a *aptPackageManager) SetProxy(proxyURL string) {
	a.proxyURL = proxyURL
}

func (a *aptPackageManager) InstalledPackageVersion(name string) (string, bool, error) {
	cmd := fmt.Sprintf("dpkg-query -W -f='${Status} ${Version}' %s 2>/dev/null || true", shellQuote(name))
	stdout, stderr, err := a.runBash(cmd)
	if err != nil {
		return "", false, fmt.Errorf("%w, stdout: %s, stderr: %s", err, stdout.String(), stderr.String())
	}
	status := strings.TrimSpace(stdout.String())
	const installedPrefix = "install ok installed "
	if !strings.HasPrefix(status, installedPrefix) {
		return "", false, nil
	}
	return strings.TrimSpace(strings.TrimPrefix(status, installedPrefix)), true, nil
}

func (a *aptPackageManager) Update(repoFileRef string) error {
	cmd := "apt-get update"
	if opts := aptRepoOptions(repoFileRef); opts != "" {
		cmd += " " + opts
	}
	if opts := a.aptProxyOptions(); opts != "" {
		cmd += " " + opts
	}
	stdout, stderr, err := a.runBash(cmd, a.withAPTEnvironment())
	if err != nil {
		return fmt.Errorf("%w, stdout: %s, stderr: %s", err, stdout.String(), stderr.String())
	}
	return nil
}

func (a *aptPackageManager) Install(installTarget string, repoFileRef string) error {
	cmd := "apt-get install -y " + aptDpkgOptions()
	if opts := aptRepoOptions(repoFileRef); opts != "" {
		cmd += " " + opts
	}
	if opts := a.aptProxyOptions(); opts != "" {
		cmd += " " + opts
	}
	stdout, stderr, err := a.runBash(cmd+" "+shellQuote(installTarget), a.withAPTEnvironment())
	if err != nil {
		return fmt.Errorf("%w, stdout: %s, stderr: %s", err, stdout.String(), stderr.String())
	}
	return nil
}

func (a *aptPackageManager) AvailableVersions(name string, repoFileRef string) ([]string, error) {
	cmd := "apt-cache madison " + shellQuote(name)
	if opts := aptRepoOptions(repoFileRef); opts != "" {
		cmd += " " + opts
	}
	stdout, stderr, err := a.runBash(cmd)
	if err != nil {
		return nil, fmt.Errorf("%w, stdout: %s, stderr: %s", err, stdout.String(), stderr.String())
	}
	versions := []string{}
	for _, line := range strings.Split(stdout.String(), "\n") {
		parts := strings.Split(line, "|")
		if len(parts) < 2 {
			continue
		}
		version := strings.TrimSpace(parts[1])
		if version != "" {
			versions = append(versions, version)
		}
	}
	return versions, nil
}

func (a *aptPackageManager) VersionCompare(version string, op string, requestedVersion string) bool {
	cmd := fmt.Sprintf("dpkg --compare-versions %s %s %s", shellQuote(version), op, shellQuote(requestedVersion))
	_, _, err := a.runBash(cmd)
	return err == nil
}

func aptRepoOptions(repoFileRef string) string {
	if repoFileRef == "" {
		return ""
	}
	return strings.Join([]string{
		"-o", "Dir::Etc::sourcelist=" + shellQuote(repoFileRef),
		"-o", "Dir::Etc::sourceparts='-'",
		"-o", "APT::Get::List-Cleanup=0",
	}, " ")
}

func aptDpkgOptions() string {
	return strings.Join([]string{
		"-o", "Dpkg::Options::=--force-confdef",
		"-o", "Dpkg::Options::=--force-confold",
	}, " ")
}

func (a *aptPackageManager) aptProxyOptions() string {
	if a.proxyURL == "" {
		return ""
	}
	return strings.Join([]string{
		// APT's default AddrConfig filtering can fail to resolve the local
		// 127.0.0.1 relay, so disable it.
		"-o", "Acquire::Connect::AddrConfig=false",
		// Route HTTP package downloads through the local DPU relay.
		"-o", "Acquire::http::Proxy=" + shellQuote(a.proxyURL),
		// Route HTTPS package downloads through the same relay.
		"-o", "Acquire::https::Proxy=" + shellQuote(a.proxyURL),
		// The dpu-agent repo is already served by host-agent on the tmfifo
		// link. Access it directly; sending this link-local URL through the
		// forward proxy can fail on the scoped IPv6 host syntax.
		"-o", "Acquire::http::Proxy::" + hostProxyAddress + "=DIRECT",
	}, " ")
}

func (a *aptPackageManager) withAPTEnvironment() bash.CmdOption {
	return func(cmd *exec.Cmd) {
		// dpu-agent owns this unattended provisioning step. Avoid any apt/debconf
		// prompt that could block the agent forever; dpkg conffile prompts are
		// handled by aptDpkgOptions.
		managedKeys := []string{"DEBIAN_FRONTEND", "APT_LISTCHANGES_FRONTEND"}
		env := map[string]string{
			"DEBIAN_FRONTEND":          "noninteractive",
			"APT_LISTCHANGES_FRONTEND": "none",
		}
		cmd.Env = environmentWithout(managedKeys...)
		for key, value := range env {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
	}
}

func environmentWithout(keys ...string) []string {
	removed := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		removed[key] = struct{}{}
	}
	env := []string{}
	for _, entry := range os.Environ() {
		key, _, ok := strings.Cut(entry, "=")
		if !ok {
			env = append(env, entry)
			continue
		}
		if _, shouldRemove := removed[key]; shouldRemove {
			continue
		}
		env = append(env, entry)
	}
	return env
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
