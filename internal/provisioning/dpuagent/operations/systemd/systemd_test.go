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

package systemd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"
	"github.com/nvidia/doca-platform/internal/provisioning/utils/bash"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const testTimeout = 5 * time.Second

var _ = Describe("ManageServices", func() {
	It("should skip if no systemd services are configured", func() {
		op := &ManageServices{}
		Expect(op.ShouldSkip(&operations.Context{})).To(BeTrue())
	})

	It("should not skip if systemd services are configured", func() {
		op := &ManageServices{}
		Expect(op.ShouldSkip(&operations.Context{
			DPUFlavor: provisioningv1.DPUFlavor{
				Spec: provisioningv1.DPUFlavorSpec{
					SystemdServices: []provisioningv1.SystemdServiceSpec{
						{Name: "foo", Operation: provisioningv1.SystemdServiceStart},
					},
				},
			},
		})).To(BeFalse())
	})

	It("should daemon-reload then enable and start a service", func() {
		var commands []string
		fake := fakeRun(&commands, nil)
		op := &ManageServices{run: fake, startTimeout: testTimeout}

		err := op.Execute(ctx, &operations.Context{
			DPUFlavor: provisioningv1.DPUFlavor{
				Spec: provisioningv1.DPUFlavorSpec{
					SystemdServices: []provisioningv1.SystemdServiceSpec{
						{Name: "xyz", Operation: provisioningv1.SystemdServiceEnableAndStart},
					},
				},
			},
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(commands).To(Equal([]string{
			"systemctl daemon-reload",
			"systemctl enable xyz",
			"systemctl start xyz",
		}))
	})

	It("should only enable a service when operation is Enable", func() {
		var commands []string
		fake := fakeRun(&commands, nil)
		op := &ManageServices{run: fake, startTimeout: testTimeout}

		err := op.Execute(ctx, &operations.Context{
			DPUFlavor: provisioningv1.DPUFlavor{
				Spec: provisioningv1.DPUFlavorSpec{
					SystemdServices: []provisioningv1.SystemdServiceSpec{
						{Name: "xyz", Operation: provisioningv1.SystemdServiceEnable},
					},
				},
			},
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(commands).To(Equal([]string{
			"systemctl daemon-reload",
			"systemctl enable xyz",
		}))
	})

	It("should only start a service when operation is Start", func() {
		var commands []string
		fake := fakeRun(&commands, nil)
		op := &ManageServices{run: fake, startTimeout: testTimeout}

		err := op.Execute(ctx, &operations.Context{
			DPUFlavor: provisioningv1.DPUFlavor{
				Spec: provisioningv1.DPUFlavorSpec{
					SystemdServices: []provisioningv1.SystemdServiceSpec{
						{Name: "xyz", Operation: provisioningv1.SystemdServiceStart},
					},
				},
			},
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(commands).To(Equal([]string{
			"systemctl daemon-reload",
			"systemctl start xyz",
		}))
	})

	It("should manage multiple services in order", func() {
		var commands []string
		fake := fakeRun(&commands, nil)
		op := &ManageServices{run: fake, startTimeout: testTimeout}

		err := op.Execute(ctx, &operations.Context{
			DPUFlavor: provisioningv1.DPUFlavor{
				Spec: provisioningv1.DPUFlavorSpec{
					SystemdServices: []provisioningv1.SystemdServiceSpec{
						{Name: "aaa", Operation: provisioningv1.SystemdServiceEnableAndStart},
						{Name: "bbb", Operation: provisioningv1.SystemdServiceEnable},
					},
				},
			},
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(commands).To(Equal([]string{
			"systemctl daemon-reload",
			"systemctl enable aaa",
			"systemctl start aaa",
			"systemctl enable bbb",
		}))
	})

	It("should fail if daemon-reload fails", func() {
		var commands []string
		fake := fakeRun(&commands, map[string]error{
			"systemctl daemon-reload": errors.New("daemon-reload error"),
		})
		op := &ManageServices{run: fake, startTimeout: testTimeout}

		err := op.Execute(ctx, &operations.Context{
			DPUFlavor: provisioningv1.DPUFlavor{
				Spec: provisioningv1.DPUFlavorSpec{
					SystemdServices: []provisioningv1.SystemdServiceSpec{
						{Name: "xyz", Operation: provisioningv1.SystemdServiceStart},
					},
				},
			},
		})

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("daemon-reload"))
		Expect(commands).To(Equal([]string{"systemctl daemon-reload"}))
	})

	It("should fail on first service error and not continue", func() {
		var commands []string
		fake := fakeRun(&commands, map[string]error{
			"systemctl enable aaa": errors.New("enable failed"),
		})
		op := &ManageServices{run: fake, startTimeout: testTimeout}

		err := op.Execute(ctx, &operations.Context{
			DPUFlavor: provisioningv1.DPUFlavor{
				Spec: provisioningv1.DPUFlavorSpec{
					SystemdServices: []provisioningv1.SystemdServiceSpec{
						{Name: "aaa", Operation: provisioningv1.SystemdServiceEnable},
						{Name: "bbb", Operation: provisioningv1.SystemdServiceStart},
					},
				},
			},
		})

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to enable service aaa"))
		Expect(commands).To(Equal([]string{
			"systemctl daemon-reload",
			"systemctl enable aaa",
		}))
	})

	It("should fail if start fails", func() {
		var commands []string
		fake := fakeRun(&commands, map[string]error{
			"systemctl start xyz": errors.New("start failed"),
		})
		op := &ManageServices{run: fake, startTimeout: testTimeout}

		err := op.Execute(ctx, &operations.Context{
			DPUFlavor: provisioningv1.DPUFlavor{
				Spec: provisioningv1.DPUFlavorSpec{
					SystemdServices: []provisioningv1.SystemdServiceSpec{
						{Name: "xyz", Operation: provisioningv1.SystemdServiceEnableAndStart},
					},
				},
			},
		})

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to start service xyz"))
		Expect(commands).To(Equal([]string{
			"systemctl daemon-reload",
			"systemctl enable xyz",
			"systemctl start xyz",
		}))
	})

	It("should fail if start exceeds the timeout", func() {
		blockingRun := func(cmdCtx context.Context, cmdStr string, _ ...bash.CmdOption) (bytes.Buffer, bytes.Buffer, error) {
			var stdout, stderr bytes.Buffer
			if cmdStr == "systemctl start slow" {
				<-cmdCtx.Done()
				return stdout, stderr, cmdCtx.Err()
			}
			return stdout, stderr, nil
		}
		op := &ManageServices{run: blockingRun, startTimeout: 50 * time.Millisecond}

		err := op.Execute(ctx, &operations.Context{
			DPUFlavor: provisioningv1.DPUFlavor{
				Spec: provisioningv1.DPUFlavorSpec{
					SystemdServices: []provisioningv1.SystemdServiceSpec{
						{Name: "slow", Operation: provisioningv1.SystemdServiceStart},
					},
				},
			},
		})

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to start service slow"))
	})
})

func fakeRun(commands *[]string, failOn map[string]error) bash.RunWithContextFunc {
	return func(_ context.Context, cmdStr string, _ ...bash.CmdOption) (bytes.Buffer, bytes.Buffer, error) {
		*commands = append(*commands, cmdStr)
		var stdout, stderr bytes.Buffer
		if failOn != nil {
			if err, ok := failOn[cmdStr]; ok {
				fmt.Fprint(&stderr, err.Error())
				return stdout, stderr, err
			}
		}
		return stdout, stderr, nil
	}
}
