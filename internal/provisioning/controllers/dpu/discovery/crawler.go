/*
Copyright 2025 NVIDIA

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

package discovery

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"strings"
	"sync"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	rfclient "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/client"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// CrawlerService handles the discovery of DPU BMCs in a given IP range
type CrawlerService struct {
	client               client.Client
	scheme               *runtime.Scheme
	namespace            string
	workers              int
	skipDpuNodeDiscovery bool
}

type CrawlResult struct {
	IPAddress    string
	Port         uint32
	Found        bool
	Error        error
	SerialNumber string
	OPN          string
}

// NewCrawlerService creates a new instance of CrawlerService
func NewCrawlerService(client client.Client, namespace string, workers int, skipDpuNodeDiscovery bool) *CrawlerService {
	return &CrawlerService{
		client:               client,
		scheme:               scheme.Scheme,
		namespace:            namespace,
		workers:              workers,
		skipDpuNodeDiscovery: skipDpuNodeDiscovery,
	}
}

// Crawl scans the given IP range for DPU BMCs
func (c *CrawlerService) Crawl(ctx context.Context, ipRange provisioningv1.IPRange) (int, error) {
	logger := log.FromContext(ctx)

	// Preload existing DPU devices to avoid re-crawling known IPs
	existingIPs, existingSerialNumbers, err := c.preloadExistingDPUDevices(ctx)
	if err != nil {
		logger.Error(err, "Failed to preload existing DPU devices")
		return 0, err
	}
	logger.Info("Preloaded existing DPU devices", "count", len(existingIPs))

	// Convert IP strings to net.IP
	startIP := net.ParseIP(ipRange.StartIP)
	endIP := net.ParseIP(ipRange.EndIP)

	// Get port from IPRange spec, default to 443 if not specified
	port := uint32(443) // Default port as defined in the CRD
	if ipRange.Port != nil {
		port = *ipRange.Port
	}

	logger.Info("Crawling IP range", "start", ipRange.StartIP, "end", ipRange.EndIP, "port", port)

	// Convert to uint32 for easier comparison
	start := ipToUint32(startIP.To4())
	end := ipToUint32(endIP.To4())

	// Create channels for work distribution and results
	jobs := make(chan uint32, end-start+1)
	results := make(chan CrawlResult, end-start+1)

	// Start worker pool
	var wg sync.WaitGroup

	for i := 0; i < c.workers; i++ {
		wg.Add(1)
		go c.worker(ctx, &wg, jobs, results, existingIPs, existingSerialNumbers, port)
	}

	// Send jobs
	go func() {
		for ip := start; ip <= end; ip++ {
			jobs <- ip
		}
		close(jobs)
	}()

	// Close results channel when all workers are done
	go func() {
		wg.Wait()
		close(results)
	}()

	// Process results
	foundDpus := 0
	for result := range results {
		if result.Found {
			logger.Info("Found DPU BMC", "address", result.IPAddress, "port", result.Port)
			if err := c.createDPUDeviceAndNode(ctx, result); err != nil {
				return 0, fmt.Errorf("failed to create DPU device %s: %w", result.IPAddress, err)
			}
			foundDpus++
		}
	}

	return foundDpus, nil
}

// preloadExistingDPUDevices retrieves all existing DPU devices from the cluster
// and returns a map of their BMC IP addresses for quick lookup
func (c *CrawlerService) preloadExistingDPUDevices(ctx context.Context) (map[string]bool, map[string]bool, error) {
	logger := log.FromContext(ctx)

	// List all DPU devices in the namespace
	dpuDeviceList := &provisioningv1.DPUDeviceList{}
	listOpts := []client.ListOption{
		client.InNamespace(c.namespace),
	}

	if err := c.client.List(ctx, dpuDeviceList, listOpts...); err != nil {
		return nil, nil, fmt.Errorf("failed to list existing DPU devices: %w", err)
	}

	existingIPs := make(map[string]bool, len(dpuDeviceList.Items))
	existingSerialNumbers := make(map[string]bool, len(dpuDeviceList.Items))
	for _, dpuDevice := range dpuDeviceList.Items {
		if dpuDevice.Spec.BMCIP != nil && *dpuDevice.Spec.BMCIP != "" {
			existingIPs[*dpuDevice.Spec.BMCIP] = true
			logger.V(1).Info("Found existing DPU device", "name", dpuDevice.Name, "bmcIP", *dpuDevice.Spec.BMCIP)
		}
		if dpuDevice.Spec.SerialNumber != "" {
			existingSerialNumbers[dpuDevice.Spec.SerialNumber] = true
		}
	}

	return existingIPs, existingSerialNumbers, nil
}

func (c *CrawlerService) worker(ctx context.Context, wg *sync.WaitGroup, jobs <-chan uint32, results chan<- CrawlResult, existingIPs map[string]bool, existingSerialNumbers map[string]bool, port uint32) {
	defer wg.Done()
	logger := log.FromContext(ctx)

	for ip := range jobs {
		address := fmt.Sprintf("%s:%d", uint32ToIP(ip).String(), port)
		ipOnly := uint32ToIP(ip).String()
		logger.Info("Processing IP", "ip", address, "port", port)
		result := CrawlResult{IPAddress: ipOnly, Port: port}

		// Skip if IP already has an associated DPU device
		if existingIPs[ipOnly] {
			logger.V(1).Info("Skipping IP - already has existing DPU device", "ip", ipOnly)
			result.Error = fmt.Errorf("IP %s already has an associated DPU device", ipOnly)
			results <- result
			continue
		}

		// Try to connect to the BMC
		client, err := rfclient.NewRawClient(address)
		if err != nil {
			logger.Error(err, "Failed to create Redfish client", "ip", address)
			result.Error = err
			results <- result
			continue
		}

		// Check if it's a DPU BMC by making a Redfish request
		resp, err := client.GetRootService()
		if err != nil {
			logger.Error(err, "Failed to get root service", "address", address, "response", resp)
			result.Error = err
			results <- result
			continue
		}
		logger.Info("Found response from the Redfish BMC at address", "address", address)

		// Now we we know that it's Redfish BMC on this address.
		// Let's check that this is DPU BMC (could change password if it's default one)
		client, err = rfclient.InitPassword(ctx, address, c.namespace, c.client)
		if err != nil {
			logger.Error(err, "Failed to create authenticated Redfish client", "address", address)
			result.Error = err
			results <- result
			continue
		}

		resp, chassisInfo, err := client.GetChassis()
		if err != nil {
			logger.Error(err, "Failed to get chassis info", "address", address, "response", resp)
			result.Error = err
			results <- result
			continue
		}

		if chassisInfo.SerialNumber == "" {
			err := fmt.Errorf("failed to get serial number")
			logger.Error(err, "address", address, "response", resp)
			result.Error = err
			results <- result
			continue
		}

		result.SerialNumber = strings.Trim(chassisInfo.SerialNumber, " ")
		if existingSerialNumbers[result.SerialNumber] {
			logger.V(1).Info("Skipping – serial already discovered",
				"address", address, "serial", result.SerialNumber)
			result.Error = fmt.Errorf("serial %s already has an associated DPU device", result.SerialNumber)

			results <- result
			continue
		}

		result.OPN = strings.Trim(chassisInfo.PartNumber, " ")
		result.Found = true
		logger.Info("Found DPU BMC", "address", address)

		results <- result
	}
}

func (c *CrawlerService) createDPUDeviceAndNode(ctx context.Context, result CrawlResult) error {
	logger := log.FromContext(ctx)

	dpu := &provisioningv1.DPUDevice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      strings.ToLower(result.SerialNumber),
			Namespace: c.namespace,
		},
		Spec: provisioningv1.DPUDeviceSpec{
			BMCIP:        &result.IPAddress,
			BMCPort:      &result.Port,
			SerialNumber: result.SerialNumber,
			OPN:          &result.OPN,
		},
	}

	err := c.client.Create(ctx, dpu)
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			logger.Info("DPU device already exists", "name", dpu.Name)
			return nil
		}
		return err
	}

	if !c.skipDpuNodeDiscovery {
		return c.createDPUNode(ctx, dpu, result)
	}

	return nil
}

func (c *CrawlerService) createDPUNode(ctx context.Context, dpu *provisioningv1.DPUDevice, result CrawlResult) error {
	logger := log.FromContext(ctx)

	dpuNodeList := &provisioningv1.DPUNodeList{}
	if err := c.client.List(ctx, dpuNodeList, client.InNamespace(c.namespace)); err != nil {
		logger.Error(err, "Failed to list DPU nodes", "namespace", c.namespace)
		return err
	}

	for _, dpuNode := range dpuNodeList.Items {
		if dpuNode.Spec.DPUs != nil {
			for _, dpuRef := range dpuNode.Spec.DPUs {
				if dpuRef.Name == dpu.Name {
					logger.Info("DPU node with specified DPUDevice already exists", "name", dpuNode.Name)
					return nil
				}
			}
		}
	}

	dpuNode := &provisioningv1.DPUNode{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("dpu-node-%s", strings.ToLower(result.SerialNumber)),
			Namespace: c.namespace,
		},
		Spec: provisioningv1.DPUNodeSpec{
			NodeRebootMethod: &provisioningv1.NodeRebootMethod{
				External: &provisioningv1.External{},
			},
			DPUs: []provisioningv1.DPURef{
				{
					Name: dpu.Name,
				},
			},
		},
	}

	dpuNode.Labels = map[string]string{
		util.NodeSelectorLabel: "true",
	}

	err := c.client.Create(ctx, dpuNode)
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			logger.Info("DPU node already exists", "name", dpuNode.Name)
			return nil
		}
		logger.Error(err, "Failed to create DPU node", "name", dpuNode.Name)
		return err
	}
	return nil
}

// Helper functions for IP address manipulation
// IntToIPv4 converts IP address of version 4 from integer to net.IP
// representation.
func ipToUint32(ipaddr net.IP) uint32 {
	return binary.BigEndian.Uint32(ipaddr.To4())
}

func uint32ToIP(n uint32) net.IP {
	ip := make(net.IP, 4)
	binary.BigEndian.PutUint32(ip, n)
	return ip
}
