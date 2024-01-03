//go:build linux

/*
Copyright 2024 NVIDIA

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
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"sync"

	dpucniprovisioner "github.com/nvidia/doca-platform/internal/cniprovisioner/dpu"
	"github.com/nvidia/doca-platform/internal/cniprovisioner/utils/networkhelper"
	"github.com/nvidia/doca-platform/internal/cniprovisioner/utils/readyz"
	"github.com/nvidia/doca-platform/internal/ipallocator"
	"github.com/nvidia/doca-platform/internal/utils/ovsclient"

	"github.com/vishvananda/netlink"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"k8s.io/utils/clock"
	kexec "k8s.io/utils/exec"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

const (
	// vtepIPAllocationFilePath is the path to the file that contains the VTEP IP allocation done by the IP Allocator.
	// We should ensure that the IP Allocation request name is vtep to have this file created correctly.
	vtepIPAllocationFilePath = "/tmp/ips/vtep"
	// pfIPAllocationFilePath is the path to the file that contains the PF IP allocation done by the IP Allocator.
	// We should ensure that the IP Allocation request name is pf to have this file created correctly.
	pfIPAllocationFilePath = "/tmp/ips/pf"
)

func main() {
	if len(os.Args) != 2 {
		klog.Fatal("expecting mode to be specified via args")
	}

	modeRaw := os.Args[1]
	mode, err := parseMode(modeRaw)
	if err != nil {
		klog.Fatalf("error while parsing mode: %s", err.Error())
	}

	klog.Info("Starting DPU CNI Provisioner")

	node := os.Getenv("NODE_NAME")
	if node == "" {
		klog.Fatal("NODE_NAME environment variable is not found. This is supposed to be configured via Kubernetes Downward API in production")
	}

	var vtepIPNet *net.IPNet
	var gateway net.IP
	var pfIPNet *net.IPNet
	var vtepCIDR *net.IPNet
	if mode == dpucniprovisioner.InternalIPAM {
		vtepIPNet, gateway, err = getInfoFromVTEPIPAllocation()
		if err != nil {
			klog.Fatalf("error while parsing info from the VTEP IP allocation file: %s", err.Error())
		}

		pfIPNet, err = getPFIP()
		if err != nil {
			klog.Fatalf("error while the PF IP from the allocation file: %s", err.Error())
		}

		vtepCIDR, err = getVTEPCIDR()
		if err != nil {
			klog.Fatalf("error while parsing VTEP CIDR: %s", err.Error())
		}
	}

	hostCIDR, err := getHostCIDR()
	if err != nil {
		klog.Fatalf("error while parsing Host CIDR %s", err.Error())
	}

	exec := kexec.New()

	ovsClient, err := ovsclient.New(exec)
	if err != nil {
		klog.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	c := clock.RealClock{}

	config, err := config.GetConfig()
	if err != nil {
		klog.Fatal(err)
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		klog.Fatal(err)
	}

	provisioner := dpucniprovisioner.New(ctx, mode, c, ovsClient, networkhelper.New(), exec, clientset, vtepIPNet, gateway, vtepCIDR, hostCIDR, pfIPNet, node)

	err = provisioner.RunOnce()
	if err != nil {
		klog.Fatal(err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		provisioner.EnsureConfiguration()
	}()

	err = readyz.ReportReady()
	if err != nil {
		klog.Fatal(err)
	}

	klog.Info("DPU CNI Provisioner is ready")

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt)
	<-ch
	klog.Info("Received termination signal, terminating.")
	cancel()
	provisioner.Stop()
	wg.Wait()
}

// getInfoFromVTEPIPAllocation returns the VTEP IP and gateway from a file that contains the VTEP IP allocation done
// by the IP Allocator component.
func getInfoFromVTEPIPAllocation() (*net.IPNet, net.IP, error) {
	content, err := os.ReadFile(vtepIPAllocationFilePath)
	if err != nil {
		return nil, nil, fmt.Errorf("error while reading file %s: %w", vtepIPAllocationFilePath, err)
	}

	results := []ipallocator.NVIPAMIPAllocatorResult{}
	if err := json.Unmarshal(content, &results); err != nil {
		return nil, nil, fmt.Errorf("error while unmarshalling IP Allocator results: %w", err)
	}

	if len(results) != 1 {
		return nil, nil, fmt.Errorf("expecting exactly 1 IP allocation for VTEP")
	}

	vtepIPRaw := results[0].IP
	vtepIP, err := netlink.ParseIPNet(vtepIPRaw)
	if err != nil {
		return nil, nil, fmt.Errorf("error while parsing VTEP IP to net.IPNet: %w", err)
	}

	gatewayRaw := results[0].Gateway
	gateway := net.ParseIP(gatewayRaw)
	if gateway == nil {
		return nil, nil, errors.New("error while parsing Gateway IP to net.IP: input is not valid")
	}

	return vtepIP, gateway, nil
}

// getPFIP() returns the PF IP from a file that contains the PF IP allocation done by the IP Allocator
// component.
func getPFIP() (*net.IPNet, error) {
	content, err := os.ReadFile(pfIPAllocationFilePath)
	if err != nil {
		return nil, fmt.Errorf("error while reading file %s: %w", vtepIPAllocationFilePath, err)
	}

	results := []ipallocator.NVIPAMIPAllocatorResult{}
	if err := json.Unmarshal(content, &results); err != nil {
		return nil, fmt.Errorf("error while unmarshalling IP Allocator results: %w", err)
	}

	if len(results) != 1 {
		return nil, fmt.Errorf("expecting exactly 1 IP allocation for PF")
	}

	pfIPRaw := results[0].IP
	pfIP, err := netlink.ParseIPNet(pfIPRaw)
	if err != nil {
		return nil, fmt.Errorf("error while parsing PF IP to net.IPNet: %w", err)
	}

	return pfIP, nil
}

// getVTEPCIDR returns the VTEP CIDR to be used by the provisioner
func getVTEPCIDR() (*net.IPNet, error) {
	vtepCIDRRaw := os.Getenv("VTEP_CIDR")
	if vtepCIDRRaw == "" {
		return nil, errors.New("required VTEP_CIDR environment variable is not set")
	}

	_, vtepCIDR, err := net.ParseCIDR(vtepCIDRRaw)
	if err != nil {
		klog.Fatalf("error while parsing VTEP CIDR %s as net.IPNet: %s", vtepCIDRRaw, err.Error())
	}

	return vtepCIDR, nil
}

// getHostCIDR returns the Host CIDR to be used by the provisioner
func getHostCIDR() (*net.IPNet, error) {
	hostCIDRRaw := os.Getenv("HOST_CIDR")
	if hostCIDRRaw == "" {
		return nil, errors.New("required HOST_CIDR environment variable is not set")
	}

	_, hostCIDR, err := net.ParseCIDR(hostCIDRRaw)
	if err != nil {
		klog.Fatalf("error while parsing Host CIDR %s as net.IPNet: %s", hostCIDRRaw, err.Error())
	}

	return hostCIDR, nil
}

// parseMode parses the mode in which the binary should be started
func parseMode(mode string) (dpucniprovisioner.Mode, error) {
	m := map[dpucniprovisioner.Mode]struct{}{
		dpucniprovisioner.InternalIPAM: {},
		dpucniprovisioner.ExternalIPAM: {},
	}
	modeTyped := dpucniprovisioner.Mode(mode)
	if _, ok := m[modeTyped]; !ok {
		return "", errors.New("unknown mode")
	}

	return modeTyped, nil
}
