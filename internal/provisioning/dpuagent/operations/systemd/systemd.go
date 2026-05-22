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
	"context"
	"fmt"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"
	"github.com/nvidia/doca-platform/internal/provisioning/utils/bash"

	"k8s.io/klog/v2"
)

const (
	conditionType       = "SystemdServicesManaged"
	defaultStartTimeout = 120 * time.Second
)

// ManageServices reconciles DPUFlavor systemd service specs on the DPU.
type ManageServices struct {
	run          bash.RunWithContextFunc
	startTimeout time.Duration
}

func (m *ManageServices) Name() string {
	return "Manage Systemd Services"
}

func (m *ManageServices) ConditionType() string {
	return conditionType
}

func (m *ManageServices) ShouldSkip(ctx *operations.Context) bool {
	return len(ctx.DPUFlavor.Spec.SystemdServices) == 0
}

func (m *ManageServices) ShouldUpdateStatusBeforeContinue(_ *operations.Context) bool {
	return false
}

func (m *ManageServices) Execute(execCtx context.Context, optCtx *operations.Context) error {
	run := m.run
	if run == nil {
		run = bash.RunWithContext
	}

	if _, stderr, err := run(execCtx, "systemctl daemon-reload"); err != nil {
		return fmt.Errorf("systemctl daemon-reload failed, stderr: %s: %w", stderr.String(), err)
	}

	for _, svc := range optCtx.DPUFlavor.Spec.SystemdServices {
		if err := m.manageService(execCtx, run, svc); err != nil {
			return err
		}
	}
	return nil
}

func (m *ManageServices) manageService(execCtx context.Context, run bash.RunWithContextFunc, svc provisioningv1.SystemdServiceSpec) error {
	switch svc.Operation {
	case provisioningv1.SystemdServiceEnable:
		klog.Infof("Enabling systemd service %s", svc.Name)
		if _, stderr, err := run(execCtx, fmt.Sprintf("systemctl enable %s", svc.Name)); err != nil {
			return fmt.Errorf("failed to enable service %s, stderr: %s: %w", svc.Name, stderr.String(), err)
		}
	case provisioningv1.SystemdServiceStart:
		if err := m.startService(execCtx, run, svc.Name); err != nil {
			return err
		}
	case provisioningv1.SystemdServiceEnableAndStart:
		klog.Infof("Enabling and starting systemd service %s", svc.Name)
		if _, stderr, err := run(execCtx, fmt.Sprintf("systemctl enable %s", svc.Name)); err != nil {
			return fmt.Errorf("failed to enable service %s, stderr: %s: %w", svc.Name, stderr.String(), err)
		}
		if err := m.startService(execCtx, run, svc.Name); err != nil {
			return err
		}
	}
	return nil
}

func (m *ManageServices) startService(execCtx context.Context, run bash.RunWithContextFunc, name string) error {
	timeout := m.startTimeout
	if timeout == 0 {
		timeout = defaultStartTimeout
	}
	startCtx, cancel := context.WithTimeout(execCtx, timeout)
	defer cancel()
	klog.Infof("Starting systemd service %s (timeout %s)", name, timeout)
	if _, stderr, err := run(startCtx, fmt.Sprintf("systemctl start %s", name)); err != nil {
		return fmt.Errorf("failed to start service %s, stderr: %s: %w", name, stderr.String(), err)
	}
	return nil
}
