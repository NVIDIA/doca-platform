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
		applied := 0
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
				applied++
				return nil
			},
			linkMTUFunc: func(string) (int, error) {
				return underlayMTU, nil
			},
		}
		optCtx := &operations.Context{
			DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
				return []pciutil.NICPort{
					{Netdev: "p0"},
					{Netdev: "p1"},
				}, nil
			},
		}
		Expect(operation.Execute(context.Background(), optCtx)).To(MatchError(errNetplanApplied))

		content, err := os.ReadFile(filepath.Join(tempDir, "97-pf-mtu.yaml"))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(content)).To(ContainSubstring("p1:"))
		Expect(string(content)).To(ContainSubstring("pf1hpf:"))
		Expect(string(content)).NotTo(ContainSubstring("p0:"))
		Expect(string(content)).NotTo(ContainSubstring("pf0hpf:"))
		Expect(applied).To(Equal(1))

		By("succeeding on the retry without applying again")
		Expect(operation.Execute(context.Background(), optCtx)).To(Succeed())
		Expect(applied).To(Equal(1))
	})

	Context("when 97-pf-mtu.yaml already has the desired content", func() {
		var (
			tempDir string
			applied int
			mtus    map[string]int
			optCtx  *operations.Context
		)
		newOperation := func() *SetNetplanUnderlayMTU {
			return &SetNetplanUnderlayMTU{
				netplanRoot: tempDir,
				runBash: func(string) (bytes.Buffer, bytes.Buffer, error) {
					var stdout bytes.Buffer
					stdout.WriteString("[]\n")
					return stdout, bytes.Buffer{}, nil
				},
				listPFRepsFunc: func() ([]string, error) {
					return []string{"pf0hpf"}, nil
				},
				applyNetplanFunc: func() error {
					applied++
					return nil
				},
				linkMTUFunc: func(dev string) (int, error) {
					return mtus[dev], nil
				},
			}
		}
		BeforeEach(func() {
			tempDir = GinkgoT().TempDir()
			applied = 0
			mtus = map[string]int{"p0": underlayMTU, "pf0hpf": underlayMTU}
			optCtx = &operations.Context{
				DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
					return []pciutil.NICPort{{Netdev: "p0"}}, nil
				},
			}
			Expect(newOperation().Execute(context.Background(), optCtx)).To(MatchError(errNetplanApplied))
			applied = 0
		})

		It("should not apply netplan when the links already have the MTU", func() {
			Expect(newOperation().Execute(context.Background(), optCtx)).To(Succeed())
			Expect(applied).To(BeZero())
		})

		It("should apply netplan once and fail until a link has the MTU", func() {
			mtus["pf0hpf"] = 1500
			operation := newOperation()
			Expect(operation.Execute(context.Background(), optCtx)).To(MatchError(errNetplanApplied))
			Expect(applied).To(Equal(1))

			By("failing without re-applying while the link MTU stays wrong")
			err := operation.Execute(context.Background(), optCtx)
			Expect(err).To(MatchError(errLinkMTU))
			Expect(err.Error()).To(ContainSubstring("pf0hpf has MTU 1500"))
			Expect(operation.Execute(context.Background(), optCtx)).To(MatchError(errLinkMTU))
			Expect(applied).To(Equal(1))

			By("succeeding once the link has the MTU")
			mtus["pf0hpf"] = underlayMTU
			Expect(operation.Execute(context.Background(), optCtx)).To(Succeed())
			Expect(applied).To(Equal(1))
		})

		It("should apply netplan again after a failed apply", func() {
			mtus["pf0hpf"] = 1500
			operation := newOperation()
			operation.applyNetplanFunc = func() error {
				applied++
				return fmt.Errorf("netplan failed")
			}
			Expect(operation.Execute(context.Background(), optCtx)).To(MatchError(ContainSubstring("netplan failed")))
			operation.applyNetplanFunc = func() error {
				applied++
				return nil
			}
			Expect(operation.Execute(context.Background(), optCtx)).To(MatchError(errNetplanApplied))
			Expect(applied).To(Equal(2))
		})
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

	It("should not hold after netplan apply in ZeroTrustMode", func() {
		applied := false
		operation := &SetNetplanUnderlayMTU{
			netplanRoot: GinkgoT().TempDir(),
			runBash: func(string) (bytes.Buffer, bytes.Buffer, error) {
				var stderr bytes.Buffer
				stderr.WriteString(`ovs-vsctl: no row "p0" in table Interface`)
				return bytes.Buffer{}, stderr, fmt.Errorf(`no row "p0" in table Interface`)
			},
			listPFRepsFunc: func() ([]string, error) {
				return nil, nil
			},
			applyNetplanFunc: func() error {
				applied = true
				return nil
			},
		}
		Expect(operation.Execute(context.Background(), &operations.Context{
			Options: opts.Options{ZeroTrustMode: true},
			DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
				return []pciutil.NICPort{{Netdev: "p0"}}, nil
			},
		})).To(Succeed())
		Expect(applied).To(BeTrue())
	})
})
