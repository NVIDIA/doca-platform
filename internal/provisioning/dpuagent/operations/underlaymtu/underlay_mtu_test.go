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

package underlaymtu

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nvidia/doca-platform/cmd/dpuagent/opts"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"
	pciutil "github.com/nvidia/doca-platform/internal/provisioning/utils/pci"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("SetNetplanUnderlayMTU", func() {
	It("should skip if SkipNetworkConfig is true", func() {
		operation := &SetNetplanUnderlayMTU{}
		Expect(operation.ShouldSkip(&operations.Context{
			Options: opts.Options{SkipNetworkConfig: true},
		})).To(BeTrue())
	})

	It("should write netplan MTU only when OVS mtu_request is unset or the Interface is missing", func() {
		tempDir := GinkgoT().TempDir()
		applied := false
		operation := &SetNetplanUnderlayMTU{
			netplanRoot: tempDir,
			runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
				var stdout, stderr bytes.Buffer
				switch {
				case strings.Contains(cmd, "get Interface p0 mtu_request"):
					stdout.WriteString("9216\n")
					return stdout, stderr, nil
				case strings.Contains(cmd, "get Interface pf0hpf mtu_request"):
					stdout.WriteString("9216\n")
					return stdout, stderr, nil
				case strings.Contains(cmd, "get Interface p1 mtu_request"):
					stdout.WriteString("[]\n")
					return stdout, stderr, nil
				case strings.Contains(cmd, "get Interface pf1hpf mtu_request"):
					stderr.WriteString(`ovs-vsctl: no row "pf1hpf" in table Interface`)
					return stdout, stderr, fmt.Errorf(`no row "pf1hpf" in table Interface`)
				default:
					return stdout, stderr, fmt.Errorf("unexpected command: %s", cmd)
				}
			},
			listPFRepsFunc: func() ([]string, error) {
				return []string{"pf0hpf", "pf1hpf"}, nil
			},
			applyNetplanFunc: func() error {
				applied = true
				return nil
			},
		}
		Expect(operation.Execute(context.Background(), &operations.Context{
			DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
				return []pciutil.NICPort{
					{Netdev: "p0"},
					{Netdev: "p1"},
				}, nil
			},
		})).To(Succeed())

		content, err := os.ReadFile(filepath.Join(tempDir, "97-pf-mtu.yaml"))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(content)).To(ContainSubstring("p1:"))
		Expect(string(content)).To(ContainSubstring("pf1hpf:"))
		Expect(string(content)).NotTo(ContainSubstring("p0:"))
		Expect(string(content)).NotTo(ContainSubstring("pf0hpf:"))
		Expect(applied).To(BeTrue())
	})

	It("should not write netplan when all underlay ports have OVS mtu_request", func() {
		tempDir := GinkgoT().TempDir()
		applied := false
		operation := &SetNetplanUnderlayMTU{
			netplanRoot: tempDir,
			runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
				var stdout bytes.Buffer
				stdout.WriteString("9216\n")
				return stdout, bytes.Buffer{}, nil
			},
			listPFRepsFunc: func() ([]string, error) {
				return []string{"pf0hpf"}, nil
			},
			applyNetplanFunc: func() error {
				applied = true
				return nil
			},
		}
		Expect(operation.Execute(context.Background(), &operations.Context{
			DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
				return []pciutil.NICPort{{Netdev: "p0"}}, nil
			},
		})).To(Succeed())
		_, err := os.Stat(filepath.Join(tempDir, "97-pf-mtu.yaml"))
		Expect(os.IsNotExist(err)).To(BeTrue())
		Expect(applied).To(BeFalse())
	})

	It("should fail when ovs-vsctl errors for a reason other than a missing Interface", func() {
		tempDir := GinkgoT().TempDir()
		applied := false
		operation := &SetNetplanUnderlayMTU{
			netplanRoot: tempDir,
			runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
				var stderr bytes.Buffer
				stderr.WriteString("database connection failed")
				return bytes.Buffer{}, stderr, fmt.Errorf("timed out")
			},
			listPFRepsFunc: func() ([]string, error) {
				return []string{"pf0hpf"}, nil
			},
			applyNetplanFunc: func() error {
				applied = true
				return nil
			},
		}
		err := operation.Execute(context.Background(), &operations.Context{
			DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
				return []pciutil.NICPort{{Netdev: "p0"}}, nil
			},
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to get OVS mtu_request for p0"))
		_, statErr := os.Stat(filepath.Join(tempDir, "97-pf-mtu.yaml"))
		Expect(os.IsNotExist(statErr)).To(BeTrue())
		Expect(applied).To(BeFalse())
	})
})
