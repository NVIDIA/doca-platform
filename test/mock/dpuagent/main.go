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

// mock-dpuagent is one non-existent BlueField DPU for control plane scale testing: it serves the
// BMC Redfish API the provisioning controllers talk to and, once the controllers have "installed"
// an OS on it, impersonates the dpu-agent that reports DPU.status.agentStatus and joins the DPU
// cluster. One process is one DPU; everything it needs is in the --config file.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/nvidia/doca-platform/test/mock/dpuagent/pkg/agent"
	"github.com/nvidia/doca-platform/test/mock/dpuagent/pkg/config"
	"github.com/nvidia/doca-platform/test/mock/dpuagent/pkg/hostctl"
	"github.com/nvidia/doca-platform/test/mock/dpuagent/pkg/redfish"

	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
)

func main() {
	configPath := flag.String("config", config.DefaultPath, "Path to the mock-dpuagent configuration file")
	klog.InitFlags(nil)
	flag.Parse()
	defer klog.Flush()
	ctrl.SetLogger(klog.Background())

	if err := run(*configPath); err != nil {
		klog.ErrorS(err, "mock-dpuagent failed")
		os.Exit(1)
	}
}

func run(configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	hostname, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("read hostname: %w", err)
	}
	serial, err := cfg.ResolveSerialNumber(hostname, time.Now())
	if err != nil {
		return err
	}
	personality := redfish.ForType(cfg.DPUType)
	state := redfish.NewState(redfish.Options{
		Personality:  personality,
		SerialNumber: serial,
		PSID:         cfg.BMC.PSID,
		PF0MAC:       config.PF0MAC(serial),
		Firmware:     cfg.BMC.Firmware,
	})
	klog.InfoS("mock-dpuagent starting", "dpuType", cfg.DPUType, "serialNumber", serial, "psid", cfg.BMC.PSID,
		"pf0MAC", config.PF0MAC(serial), "bmcPort", cfg.BMC.Port, "rebootMethod", cfg.Agent.RebootMethod, "dpuClusterJoin", cfg.Agent.DPUClusterJoin)

	sup, err := agent.NewSupervisor(cfg, agent.DefaultWorkDir, state)
	if err != nil {
		return err
	}
	var serverOpts []redfish.ServerOption
	if d := cfg.BMC.ResponseDelay; d != nil {
		minDelay, maxDelay := d.Range()
		serverOpts = append(serverOpts, redfish.WithResponseDelay(minDelay, maxDelay))
		klog.InfoS("Redfish responses delayed", "min", minDelay, "max", maxDelay)
	}
	if overrides := cfg.BMC.ResponseDelayOverrides; len(overrides) > 0 {
		serverOpts = append(serverOpts, redfish.WithResponseDelayOverrides(overrides))
		for _, o := range overrides {
			minDelay, maxDelay := o.Range()
			methods := o.Methods
			if o.AllMethods() {
				methods = []string{"*"}
			}
			klog.InfoS("Redfish responses delayed", "operation", o.Name, "methods", methods, "min", minDelay, "max", maxDelay)
		}
	}
	server, err := redfish.NewServer(state, sup, serverOpts...)
	if err != nil {
		return err
	}
	addr, err := server.Listen(fmt.Sprintf("0.0.0.0:%d", cfg.BMC.Port))
	if err != nil {
		return err
	}
	klog.InfoS("Redfish server listening", "addr", addr.String())

	ctx, cancel := context.WithCancel(ctrl.SetupSignalHandler())
	defer cancel()
	errCh := make(chan error, 2)
	go func() { errCh <- server.Serve(ctx) }()
	go func() { errCh <- hostctl.Serve(ctx, hostctl.DefaultAddr, sup) }()
	klog.InfoS("waiting for the controller to install an OS")

	var firstErr error
	select {
	case <-ctx.Done():
	case firstErr = <-errCh:
		cancel()
	}
	sup.Stop()
	// Let the other server finish its shutdown.
	<-errCh
	return firstErr
}
