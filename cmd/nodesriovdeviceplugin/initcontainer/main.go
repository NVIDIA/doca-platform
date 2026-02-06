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
	"context"
	"os/signal"
	"syscall"
	"time"

	"github.com/nvidia/doca-platform/internal/nodesriovdeviceplugin/initcontainer"

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

func main() {
	var inputPath string
	var outputPath string
	var defaultResourcePrefix string
	var devicesReadinessTimeout time.Duration
	var devicesReadinessPollInterval time.Duration

	fs.StringVar(&inputPath, "input-path", "", "Path to the input config file")
	fs.StringVar(&outputPath, "output-path", "", "Path to the output config directory")
	fs.StringVar(&defaultResourcePrefix, "default-resource-prefix", "",
		"Default resource prefix for resources that don't specify one")
	fs.DurationVar(&devicesReadinessTimeout, "devices-readiness-timeout", time.Hour,
		"Timeout for discovering DPUs and waiting for VFs to be ready")
	fs.DurationVar(&devicesReadinessPollInterval, "devices-readiness-poll-interval", 30*time.Second,
		"Interval for polling for DPUs and VFs to be ready")

	logsv1.AddFlags(logOptions, fs)

	pflag.Parse()

	if err := logsv1.ValidateAndApply(logOptions, nil); err != nil {
		klog.Fatalf("Failed to validate and apply log options: %v", err)
	}

	if inputPath == "" {
		klog.Fatalf("input-path is required")
	}
	if outputPath == "" {
		klog.Fatalf("output-path is required")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	defer cancel()

	opts := initcontainer.Options{
		InputPath:                    inputPath,
		OutputPath:                   outputPath,
		DefaultResourcePrefix:        defaultResourcePrefix,
		DevicesReadinessTimeout:      devicesReadinessTimeout,
		DevicesReadinessPollInterval: devicesReadinessPollInterval,
	}

	if err := initcontainer.Run(ctx, opts); err != nil {
		klog.Fatalf("Init container failed: %v", err)
	}
}
