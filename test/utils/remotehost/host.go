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

// Package remotehost runs SSH and docker/podman commands on remote test machines.
package remotehost

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/gomega"
)

const (
	// DefaultTimeout is the default Eventually timeout.
	DefaultTimeout = 1 * time.Minute
	// LongTimeout is the Eventually timeout for slow host operations.
	LongTimeout    = 6 * time.Minute
	defaultSSHUser = "root"
	// hostSudoPreamble sets $SUDO to "sudo -n" when not root (NOPASSWD required).
	hostSudoPreamble = `SUDO=''; if [ "$(id -u)" -ne 0 ]; then SUDO='sudo -n'; fi`
)

// Host is a remote SSH machine with an optional container runtime.
type Host struct {
	// Addr is the SSH address.
	Addr string
	// User is the SSH user, empty defaults to root.
	User string
	// Password is the SSH password for sshpass.
	Password string
	// ContainerName is the docker/podman container name.
	ContainerName string
	// Runtime is docker or podman.
	Runtime string
}

// SSH runs cmd on the host, not in the container.
func (h Host) SSH(cmd string) (string, error) {
	return ssh(h.Addr, h.sshUser(), h.Password, cmd)
}

// SpinUp starts a detached privileged host-network container.
func (h Host) SpinUp(image string) {
	rmCmd := h.runtimeCmd(fmt.Sprintf("rm -f %s >/dev/null 2>&1 || true", shellQuote(h.ContainerName)))
	runCmd := h.runtimeCmd(fmt.Sprintf(
		"run -d --name %s --network host --privileged --ulimit memlock=-1:-1 %s tail -F /dev/null",
		shellQuote(h.ContainerName), shellQuote(image)))

	Eventually(func(g Gomega) {
		output, err := h.SSH(rmCmd + "; " + runCmd)
		g.Expect(err).ToNot(HaveOccurred(), "failed to start %s container on %s: %s", h.runtime(), h.Addr, output)
	}, LongTimeout).Should(Succeed())

	Eventually(func(g Gomega) {
		output, err := h.SSH(h.runtimeCmd(fmt.Sprintf("inspect -f '{{.State.Running}}' %s", shellQuote(h.ContainerName))))
		g.Expect(err).ToNot(HaveOccurred(), "failed to inspect %s container on %s: %s", h.runtime(), h.Addr, output)
		g.Expect(lastNonEmptyLine(output)).To(Equal("true"),
			"container %s is not running on %s: %s", h.ContainerName, h.Addr, output)
	}, DefaultTimeout).Should(Succeed())
}

// Remove force-removes the container if it exists.
func (h Host) Remove() {
	rmCmd := h.runtimeCmd(fmt.Sprintf("rm -f %s >/dev/null 2>&1 || true", shellQuote(h.ContainerName)))
	output, err := h.SSH(rmCmd)
	Expect(err).ToNot(HaveOccurred(), "failed to remove %s container %s on %s: %s",
		h.runtime(), h.ContainerName, h.Addr, output)
}

// SpinUpAll starts privileged host-network containers on the given hosts.
func SpinUpAll(hosts []Host, image string) {
	for _, h := range hosts {
		h.SpinUp(image)
	}
}

// sshUser returns User, or root if empty.
func (h Host) sshUser() string {
	if u := strings.TrimSpace(h.User); u != "" {
		return u
	}
	return defaultSSHUser
}

// runtime returns docker or podman, and fails if unset.
func (h Host) runtime() string {
	rt := strings.TrimSpace(h.Runtime)
	Expect(rt).ToNot(BeEmpty(), "Host.Runtime must be set (docker or podman)")
	return rt
}

// runtimeCmd is docker/podman on the host, sudo -n for non-root podman.
func (h Host) runtimeCmd(args string) string {
	rt := h.runtime()
	cmd := rt + " " + args
	if rt == "podman" && h.sshUser() != defaultSSHUser {
		return "sudo -n " + cmd
	}
	return cmd
}

// execShell runs a shell script in the container.
func (h Host) execShell(script string) (string, error) {
	return h.SSH(h.runtimeCmd(fmt.Sprintf("exec --privileged %s sh -c %s", shellQuote(h.ContainerName), shellQuote(script))))
}

// exec runs a command in the container.
func (h Host) exec(command ...string) (string, error) {
	return h.SSH(h.runtimeCmd(fmt.Sprintf("exec --privileged %s %s", shellQuote(h.ContainerName), shellJoin(command...))))
}

// execEventually retries exec until timeout.
func (h Host) execEventually(timeout time.Duration, command ...string) string {
	var output string
	Eventually(func(g Gomega) {
		var err error
		output, err = h.exec(command...)
		g.Expect(err).ToNot(HaveOccurred(), "remote %s exec failed on %s container %s: %s",
			h.runtime(), h.Addr, h.ContainerName, output)
	}, timeout).Should(Succeed())
	return output
}

// ssh runs remoteCmd over SSH with sshpass.
func ssh(host, user, password, remoteCmd string) (string, error) {
	if strings.TrimSpace(user) == "" {
		user = defaultSSHUser
	}

	cmd := exec.Command("sshpass", "-e", "ssh",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "GlobalKnownHostsFile=/dev/null",
		"-o", "PubkeyAuthentication=no",
		"-o", "ConnectTimeout=30",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=3",
		user+"@"+host, remoteCmd,
	)
	cmd.Env = append(os.Environ(), "SSHPASS="+password)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// shellQuote wraps s in single quotes for sh -c.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

// shellJoin quotes each arg for sh.
func shellJoin(args ...string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = shellQuote(a)
	}
	return strings.Join(quoted, " ")
}

// lastNonEmptyLine returns the last non-empty line of s.
func lastNonEmptyLine(s string) string {
	lines := strings.Split(s, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line != "" {
			return line
		}
	}
	return ""
}
