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

package kernelmodule

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"
	"github.com/nvidia/doca-platform/internal/provisioning/utils/bash"

	"k8s.io/klog/v2"
)

const (
	defaultRootFS = "/"
	confPath      = "/etc/modules-load.d/br_netfilter.conf"
	moduleName    = "br_netfilter"
)

type LoadModule struct {
	rootFS     string
	loadModule func(module string) error
}

func (k *LoadModule) Name() string {
	return "Load kernel modules"
}

func (k *LoadModule) ConditionType() string {
	return "KernelModuleLoaded"
}

func (k *LoadModule) ShouldSkip(ctx *operations.Context) bool {
	return false
}

func (k *LoadModule) ShouldUpdateStatusBeforeContinue(ctx *operations.Context) bool {
	return false
}

func (k *LoadModule) Execute(execCtx context.Context, optCtx *operations.Context) error {
	klog.Infof("Ensuring kernel module %s is loaded", moduleName)
	if k.loadModule == nil {
		k.loadModule = modprobe
	}
	if err := k.loadModule(moduleName); err != nil {
		return fmt.Errorf("failed to load module %s: %w", moduleName, err)
	}

	if k.rootFS == "" {
		k.rootFS = defaultRootFS
	}
	confPath := filepath.Join(k.rootFS, confPath)
	if err := ensureModuleLoadConf(moduleName, confPath); err != nil {
		return fmt.Errorf("failed to ensure module load config %s: %w", confPath, err)
	}

	return nil
}

func ensureModuleLoadConf(module string, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", filepath.Dir(path), err)
	}
	// Check if file exists and has correct content
	existing, err := os.ReadFile(path)
	if err == nil && strings.TrimSpace(string(existing)) == module {
		return nil
	}

	if err := os.WriteFile(path, []byte(module), 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}
	klog.Infof("Created %s to load %s on boot", path, module)
	return nil
}

func modprobe(module string) error {
	stdout, stderr, err := bash.Run(fmt.Sprintf("modprobe %s", module))
	if err != nil {
		return fmt.Errorf("failed to load module %s. stdout: %s, stderr: %s, err: %w", module, stdout.String(), stderr.String(), err)
	}
	return nil
}
