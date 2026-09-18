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

package main

import (
	"github.com/nvidia/doca-platform/internal/sriovdpconfiginit"

	"github.com/spf13/pflag"
	"k8s.io/component-base/logs"
	logsv1 "k8s.io/component-base/logs/api/v1"
	_ "k8s.io/component-base/logs/json/register"
	"k8s.io/klog/v2"
)

var (
	logOptions = logs.NewOptions()
	fs         = pflag.CommandLine
)

// main is the sriovdp-config-init entrypoint. It resolves which SR-IOV device
// plugin config the plugin should consume and writes it to the active path.
func main() {
	var opts sriovdpconfiginit.Options

	fs.StringVar(&opts.GeneratedPath, "generated-path", "/var/lib/dpf/sriovdp/config.json",
		"Path to the config dpu-agent generates for this node")
	fs.StringVar(&opts.DefaultPath, "default-path", "/etc/pcidp-default/config.json",
		"Path to the default config to fall back on when the generated one is absent")
	fs.StringVar(&opts.ActivePath, "active-path", "/etc/pcidp/config.json",
		"Path to write the resolved config to, where the device plugin reads it")

	logsv1.AddFlags(logOptions, fs)

	pflag.Parse()

	if err := logsv1.ValidateAndApply(logOptions, nil); err != nil {
		klog.Fatalf("Failed to validate and apply log options: %v", err)
	}

	if _, err := sriovdpconfiginit.Resolve(opts); err != nil {
		klog.Fatalf("Failed to resolve the SR-IOV device plugin config: %v", err)
	}
}
