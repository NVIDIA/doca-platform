//go:build linux

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

// Command rshim-console-collector is an E2E-only helper that discovers host
// rshim console devices, streams DPU console output to stdout, and reconnects
// after DPU resets so the existing pod log pipeline can archive the output.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	rshimcollector "github.com/nvidia/doca-platform/test/rshim-console-collector/pkg"

	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
)

func main() {
	if err := run(); err != nil {
		klog.ErrorS(err, "Rshim console collector failed")
		klog.Flush()
		os.Exit(1)
	}
	klog.Flush()
}

func run() error {
	devRoot := flag.String("dev-root", "/dev", "Root directory containing rshim device directories")
	scanInterval := flag.Duration("scan-interval", 10*time.Second, "Interval between rshim device discovery scans")
	retryMin := flag.Duration("retry-min", time.Second, "Initial delay before retrying a console")
	retryMax := flag.Duration("retry-max", 30*time.Second, "Maximum delay between console retries")
	maxLineBytes := flag.Int("max-line-bytes", 1024*1024, "Maximum bytes accepted in one console line")
	klog.InitFlags(nil)
	flag.Parse()

	ctrl.SetLogger(klog.Background())
	nodeName := strings.TrimSpace(os.Getenv("NODE_NAME"))
	if nodeName == "" {
		return fmt.Errorf("NODE_NAME environment variable is required")
	}

	consoleCollector, err := rshimcollector.New(rshimcollector.Config{
		NodeName:     nodeName,
		DevRoot:      *devRoot,
		ScanInterval: *scanInterval,
		RetryMin:     *retryMin,
		RetryMax:     *retryMax,
		MaxLineBytes: *maxLineBytes,
		Output:       os.Stdout,
	})
	if err != nil {
		return fmt.Errorf("invalid collector configuration: %w", err)
	}

	ctx := klog.NewContext(ctrl.SetupSignalHandler(), klog.Background())
	if err := consoleCollector.Run(ctx); err != nil {
		return fmt.Errorf("run collector: %w", err)
	}
	return nil
}
