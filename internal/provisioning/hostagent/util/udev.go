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
	"os/exec"
	"path/filepath"

	"k8s.io/klog/v2"
)

const (
	nmUnmanagedRulesContent = `ACTION=="add|change|move", ATTRS{device}=="0x101e", ENV{NM_UNMANAGED}="1"
`
)

// nmUnmanagedRulesPath is the file path for the udev rule. Variable for testability.
var nmUnmanagedRulesPath = "/etc/udev/rules.d/10-nm-unmanaged.rules"

// udevRunner abstracts command execution for testability.
var udevRunner = func(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}

// EnsureNMUnmanagedUdevRule writes a udev rule that prevents NetworkManager
// from managing VF interfaces (PCI device ID 0x101e) and reloads/triggers
// udev to apply the rule to both new and already-existing devices.
func EnsureNMUnmanagedUdevRule() error {
	if err := writeUdevRuleFile(); err != nil {
		return fmt.Errorf("failed to write udev rule file: %w", err)
	}

	if err := reloadAndTriggerUdev(); err != nil {
		return fmt.Errorf("failed to reload/trigger udev rules: %w", err)
	}

	return nil
}

func writeUdevRuleFile() error {
	dir := filepath.Dir(nmUnmanagedRulesPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	existing, err := os.ReadFile(nmUnmanagedRulesPath)
	if err == nil && string(existing) == nmUnmanagedRulesContent {
		klog.V(3).Infof("Udev rule %s already up-to-date", nmUnmanagedRulesPath)
		return nil
	}

	if err := os.WriteFile(nmUnmanagedRulesPath, []byte(nmUnmanagedRulesContent), 0644); err != nil {
		return fmt.Errorf("failed to write file %s: %w", nmUnmanagedRulesPath, err)
	}
	klog.Infof("Wrote udev rule to disable NM management of VFs: %s", nmUnmanagedRulesPath)
	return nil
}

func reloadAndTriggerUdev() error {
	output, err := udevRunner("udevadm", "control", "--reload-rules")
	if err != nil {
		return fmt.Errorf("udevadm control --reload-rules failed: %w, output: %s", err, string(output))
	}

	output, err = udevRunner("udevadm", "trigger", "--subsystem-match=net")
	if err != nil {
		return fmt.Errorf("udevadm trigger --subsystem-match=net failed: %w, output: %s", err, string(output))
	}

	klog.V(3).Infof("Reloaded udev rules and triggered net subsystem")
	return nil
}
