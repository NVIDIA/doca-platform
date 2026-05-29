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

package kubelet

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"
	"github.com/nvidia/doca-platform/internal/provisioning/utils/bash"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	kubeletconfigv1beta1 "k8s.io/kubelet/config/v1beta1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

const (
	defaultRootFS        = "/"
	builtinKubeletConfig = "/usr/lib/systemd/system/kubelet.service.d/90-kubelet-bluefield.conf"
	conditionType        = "KubeletConfigured"
	kubeadminSecretKey   = "join"

	defaultKubleteCA           = "/etc/kubernetes/pki/ca.crt"
	defaultKubeletBootstrap    = "/etc/kubernetes/bootstrap-kubelet.conf"
	defaultKubeletConf         = "/etc/kubernetes/kubelet.conf"
	defaultKubeletDataConfig   = "/var/lib/kubelet/config.yaml"
	kubeletSystemdDropInDir    = "/etc/systemd/system/kubelet.service.d"
	kubeletSystemdDropInFile   = "10-bf.conf"
	kubeletSystemdDropInConfig = `[Service]
Environment="KUBELET_KUBECONFIG_ARGS=--bootstrap-kubeconfig=/etc/kubernetes/bootstrap-kubelet.conf --kubeconfig=/etc/kubernetes/kubelet.conf"
Environment="KUBELET_CONFIG_ARGS=--config=/var/lib/kubelet/config.yaml"
EnvironmentFile=-/var/lib/kubelet/kubeadm-flags.env
EnvironmentFile=-/etc/default/kubelet
ExecStart=
ExecStart=/usr/bin/kubelet $KUBELET_KUBECONFIG_ARGS $KUBELET_CONFIG_ARGS $KUBELET_KUBEADM_ARGS $KUBELET_EXTRA_ARGS
`
)

type RemoveBuiltinKubelet struct {
	rootFS      string
	stopKubelet func() error
}

func (k *RemoveBuiltinKubelet) Name() string {
	return "Remove Built-in Kubelet"
}

func (k *RemoveBuiltinKubelet) ConditionType() string {
	return "BuiltinKubeletRemoved"
}

func (k *RemoveBuiltinKubelet) ShouldSkip(ctx *operations.Context) bool {
	return ctx.Options.SkipRemoveBuiltinKubelet
}

func (k *RemoveBuiltinKubelet) ShouldUpdateStatusBeforeContinue(ctx *operations.Context) bool {
	return false
}

func (k *RemoveBuiltinKubelet) Execute(execCtx context.Context, optCtx *operations.Context) error {
	if k.rootFS == "" {
		k.rootFS = defaultRootFS
	}
	builtinFile := filepath.Join(k.rootFS, builtinKubeletConfig)
	_, err := os.Stat(builtinFile)
	if err == nil {
		if err := k.removeBuiltinKubeletConfig(builtinFile); err != nil {
			return fmt.Errorf("failed to remove built in kubelet config: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to stat built-in kubelet config file %s: %w", builtinFile, err)
	}
	return nil
}

func (k *RemoveBuiltinKubelet) removeBuiltinKubeletConfig(path string) error {
	if k.stopKubelet == nil {
		k.stopKubelet = stopKubelet
	}
	if err := k.stopKubelet(); err != nil {
		return fmt.Errorf("failed to stop kubelet: %w", err)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("failed to remove file %s: %w", path, err)
	}
	klog.Infof("Removed file %s", path)
	return nil
}

func stopKubelet() error {
	_, _, err := bash.Run("systemctl stop kubelet")
	if err != nil {
		return fmt.Errorf("failed to stop kubelet: %w", err)
	}
	klog.Infof("Stopped kubelet")
	return nil
}

type StartKubelet struct {
	runBash func(cmd string) (bytes.Buffer, bytes.Buffer, error)
}

func (s *StartKubelet) Name() string {
	return "Start Kubelet"
}

func (s *StartKubelet) ConditionType() string {
	return "KubeletStarted"
}

func (s *StartKubelet) ShouldSkip(ctx *operations.Context) bool {
	return ctx.Options.SkipStartKubelet
}

func (s *StartKubelet) ShouldUpdateStatusBeforeContinue(ctx *operations.Context) bool {
	return false
}

func (s *StartKubelet) Execute(_ context.Context, _ *operations.Context) error {
	if s.runBash == nil {
		s.runBash = bash.Run
	}
	// Kubelet must not start before SFs are created. In fact, kubelet is not enabled after kubeadm join.
	// We disable it explicitly to avoid it being enabled by users.
	if _, stderr, err := s.runBash("systemctl disable kubelet"); err != nil {
		return fmt.Errorf("failed to disable kubelet: %w, stderr: %s", err, stderr.String())
	}
	// Since kubelet is disabled, we must start it after every reboot.
	if _, stderr, err := s.runBash("systemctl start kubelet"); err != nil {
		return fmt.Errorf("failed to start kubelet: %w, stderr: %s", err, stderr.String())
	}
	klog.Infof("Started kubelet")
	return nil
}

type ConfigureKubelet struct {
	caPath            string
	bootstrapPath     string
	kubeletConfPath   string
	kubeletDataConfig string
	systemdDropInDir  string
	stopKubelet       func() error
	runBash           func(cmd string) (bytes.Buffer, bytes.Buffer, error)
}

func (c *ConfigureKubelet) Name() string {
	return "Configure Kubelet"
}

func (c *ConfigureKubelet) ConditionType() string {
	return conditionType
}

func (c *ConfigureKubelet) ShouldSkip(ctx *operations.Context) bool {
	return ctx.Options.SkipConfigureKubelet
}

func (c *ConfigureKubelet) ShouldUpdateStatusBeforeContinue(ctx *operations.Context) bool {
	return true
}

func (c *ConfigureKubelet) Execute(execCtx context.Context, optCtx *operations.Context) error {
	if optCtx.LatestDPU == nil {
		return fmt.Errorf("latest DPU not retrieved (this should never happen)")
	}
	if optCtx.LatestDPU.Status.AgentStatus != nil {
		cond := meta.FindStatusCondition(optCtx.LatestDPU.Status.AgentStatus.Conditions, conditionType)
		if cond != nil && cond.Status == metav1.ConditionTrue {
			klog.Infof("Kubelet already configured, skip")
			return nil
		}
	}
	if c.caPath == "" {
		c.caPath = defaultKubleteCA
	}
	if c.bootstrapPath == "" {
		c.bootstrapPath = defaultKubeletBootstrap
	}
	if c.kubeletConfPath == "" {
		c.kubeletConfPath = defaultKubeletConf
	}
	_ = os.Remove(c.caPath)
	_ = os.Remove(c.bootstrapPath)
	_ = os.Remove(c.kubeletConfPath)
	if c.stopKubelet == nil {
		c.stopKubelet = stopKubelet
	}
	if err := c.stopKubelet(); err != nil {
		return fmt.Errorf("failed to stop kubelet: %w", err)
	}

	// Create kubelet systemd drop-in file
	if err := c.createKubeletSystemdDropIn(); err != nil {
		return fmt.Errorf("failed to create kubelet systemd drop-in: %w", err)
	}

	timeCtx, cancel := context.WithTimeout(execCtx, time.Minute)
	defer cancel()
	secret := &corev1.Secret{}
	if err := optCtx.Client.Get(timeCtx, client.ObjectKey{Namespace: optCtx.Options.KubeadmSecretNamespace, Name: optCtx.Options.KubeadmSecretName}, secret); err != nil {
		return fmt.Errorf("failed to get kubeadm secret: %w", err)
	}
	joinCmd, ok := secret.Data[kubeadminSecretKey]
	if !ok {
		return fmt.Errorf("kubeadm secret does not contain key %q", kubeadminSecretKey)
	}
	if c.runBash == nil {
		c.runBash = bash.Run
	}
	stdout, stderr, err := c.runBash(string(joinCmd))
	if err != nil {
		return fmt.Errorf("failed to run join command: %w, stdout: %s, stderr: %s", err, stdout.String(), stderr.String())
	}
	if err := c.addKubeletCustomizedConfig(); err != nil {
		return fmt.Errorf("failed to add kubelet customized config: %w", err)
	}
	kubeletVersion, err := c.KubeletVersion()
	if err != nil {
		return fmt.Errorf("failed to get kubelet version: %w", err)
	}
	optCtx.Status.KubeletVersion = kubeletVersion
	return nil
}

func (c *ConfigureKubelet) KubeletVersion() (*string, error) {
	if c.runBash == nil {
		c.runBash = bash.Run
	}
	stdout, stderr, err := c.runBash("kubelet --version")
	if err != nil {
		return nil, fmt.Errorf("failed to run kubelet version: %w, stdout: %s, stderr: %s", err, stdout.String(), stderr.String())
	}
	if stdout.Len() == 0 {
		return nil, fmt.Errorf("kubelet version output is empty, stderr: %s", stderr.String())
	}
	output := strings.TrimSpace(stdout.String())
	if !strings.HasPrefix(output, "Kubernetes ") {
		return nil, fmt.Errorf("unexpected kubelet version output: %s", output)
	}
	kubeletVersion := strings.TrimPrefix(output, "Kubernetes ")
	return &kubeletVersion, nil
}

func (c *ConfigureKubelet) createKubeletSystemdDropIn() error {
	if c.systemdDropInDir == "" {
		c.systemdDropInDir = kubeletSystemdDropInDir
	}

	if err := os.MkdirAll(c.systemdDropInDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", c.systemdDropInDir, err)
	}

	dropInPath := filepath.Join(c.systemdDropInDir, kubeletSystemdDropInFile)
	if err := os.WriteFile(dropInPath, []byte(kubeletSystemdDropInConfig), 0644); err != nil {
		return fmt.Errorf("failed to write file %s: %w", dropInPath, err)
	}
	klog.Infof("Created kubelet systemd drop-in file: %s", dropInPath)
	return nil
}

func (c *ConfigureKubelet) addKubeletCustomizedConfig() error {
	path := c.kubeletDataConfig
	if path == "" {
		path = defaultKubeletDataConfig
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("kubelet config file %s not found: %w", path, err)
		}
		return fmt.Errorf("read kubelet config %s: %w", path, err)
	}

	cfg := &kubeletconfigv1beta1.KubeletConfiguration{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("parse kubelet config %s: %w", path, err)
	}

	cfg.ProtectKernelDefaults = true
	cfg.SeccompDefault = ptr.To(true)
	cfg.StreamingConnectionIdleTimeout = metav1.Duration{Duration: 5 * time.Minute}
	cfg.EventRecordQPS = ptr.To(int32(50))

	out, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal kubelet config: %w", err)
	}
	if err := os.WriteFile(path, out, 0644); err != nil {
		return fmt.Errorf("write kubelet config %s: %w", path, err)
	}
	klog.Infof("Added kubelet customized config to %s", path)
	return nil
}
