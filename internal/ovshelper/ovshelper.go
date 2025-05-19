/*
Copyright 2024 NVIDIA.

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

package ovshelper

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nvidia/doca-platform/internal/utils/ovsclient"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	"k8s.io/utils/clock"
)

// OVSHelper is responsible for finding and resolving known OVS bugs that the system hits in runtime
type OVSHelper struct {
	clock clock.WithTicker
	// ticketInterval is the interval in which the discovery and fix job will be executed
	tickerInterval time.Duration
	ovsClient      ovsclient.OVSClient
}

// New creates a OVSHelper
func New(clock clock.WithTicker, ovsClient ovsclient.OVSClient) *OVSHelper {
	return &OVSHelper{
		clock:          clock,
		tickerInterval: 30 * time.Second,
		ovsClient:      ovsClient,
	}
}

// RunOnce runs the find and fix job once
func (o *OVSHelper) RunOnce() error {
	klog.Info("Checking for the blackhole issue")

	if err := o.handleBlackholeIssue(); err != nil {
		return fmt.Errorf("handling the blackhole issue: %w", err)
	}
	return nil
}

// Start starts the OVSHelper find and fix loop. This is a blocking function.
func (o *OVSHelper) Start(ctx context.Context) {
	runTicker := o.clock.NewTicker(o.tickerInterval)
	for {
		select {
		case <-ctx.Done():
			return
		case <-runTicker.C():
			if err := o.RunOnce(); err != nil {
				klog.Errorf("Error while running the job: %s", err.Error())
			}
		}
	}
}

// handleBlackholeIssue handles an issue ports can't send or receive traffic while they look to be setup correctly in OVS.
// To workaround it, this function unplugs and replugs such ports based on the existence of Poll Mode Driver (PMD)
// Receive (Rx) queues for those interfaces, by maintaining the relevant metadata.
func (o *OVSHelper) handleBlackholeIssue() error {
	allInterfaces, err := o.ovsClient.ListInterfaces(ovsclient.DPDK)
	if err != nil {
		return fmt.Errorf("listing all OVS interfaces: %w", err)
	}

	interfacesWithPMDRXQueue, err := o.ovsClient.GetInterfacesWithPMDRXQueue()
	if err != nil {
		return fmt.Errorf("getting all OVS interfaces with a PMD RX Queue: %w", err)
	}

	badInterfaces := []string{}
	for intf := range allInterfaces {
		if _, ok := interfacesWithPMDRXQueue[intf]; ok {
			continue
		}

		badInterfaces = append(badInterfaces, intf)
	}

	if len(badInterfaces) == 0 {
		return nil
	}

	klog.Infof("Detected bad interfaces: %v", badInterfaces)

	for _, intf := range badInterfaces {
		klog.Infof("Fixing %s", intf)
		portExternalIDs, err := o.ovsClient.GetPortExternalIDs(intf)
		if err != nil {
			return fmt.Errorf("getting the external ids for port %s: %w", intf, err)
		}
		interfaceExternalIDs, err := o.ovsClient.GetInterfaceExternalIDs(intf)
		if err != nil {
			return fmt.Errorf("getting the external ids for interface %s: %w", intf, err)
		}
		ofPort, err := o.ovsClient.GetInterfaceOfPort(intf)
		if err != nil {
			return fmt.Errorf("getting the ofport number for interface %s: %w", intf, err)
		}

		if ofPort < 0 {
			return errors.New("invalid openflow port, skipping the loop and retrying again later")
		}

		bridge, err := o.ovsClient.InterfaceToBridge(intf)
		if err != nil {
			return fmt.Errorf("getting the bridge for interface %s: %w", intf, err)
		}

		if err := o.ovsClient.DeletePort(intf); err != nil {
			return fmt.Errorf("deleting port %s: %w", intf, err)
		}

		if err := wait.ExponentialBackoff(
			wait.Backoff{
				Duration: 100 * time.Millisecond,
				Factor:   1.5,
				Steps:    15,
				Jitter:   0.4,
				Cap:      15 * time.Second,
			}, func() (done bool, err error) {
				if err := o.ovsClient.AddPortWithMetadata(bridge, intf, ovsclient.DPDK, portExternalIDs, interfaceExternalIDs, ofPort); err != nil {
					klog.Errorf("Error while adding port %s with metadata: %s", intf, err.Error())
					return false, nil
				}
				return true, nil
			}); err != nil {
			return fmt.Errorf("repeatedly trying to add port %s with metadata: %w", intf, err)
		}
	}

	return nil
}
