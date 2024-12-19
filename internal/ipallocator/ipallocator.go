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

package ipallocator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/containernetworking/cni/libcni"
	"github.com/containernetworking/cni/pkg/types"
	types040 "github.com/containernetworking/cni/pkg/types/040"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
)

// sharedResultDirectory is the directory where the files with the IP allocations will be written to
const sharedResultDirectory = "/tmp/ips"

// CNIBinDir is the directory which contains the CNI binaries
const CNIBinDir = "/opt/cni/bin"

// NVIPAMIPAllocator uses the CNI spec to allocate an IP from NVIPAM
type NVIPAMIPAllocator struct {
	cninet       *libcni.CNIConfig
	podName      string
	podNamespace string
	podUID       string
	// FileSystemRoot controls the file system root. It's used for enabling easier testing of the package. Defaults to
	// empty.
	FileSystemRoot string
}

// NVIPAMIPAllocatorRequest is a struct that contains the fields a request to the IP Allocator can have
type NVIPAMIPAllocatorRequest struct {
	// Name is the name of the request. Determines the file the result will be written to.
	Name string `json:"name"`
	// PoolName is the name of the NVIPAM pool we should request an IP from
	PoolName string `json:"poolName"`
	// PoolType is the type of the NVIPAM pool we should request an IP from. If empty, we defer to the default defined
	// by NVIPAM.
	PoolType NVIPAMPoolType `json:"poolType,omitempty"`
	// AllocateIPWIthIndex instructs NVIPAM to allocate the IP with the given index from the chunk of the pool that is
	// allocated for the node
	AllocateIPWIthIndex *int32 `json:"allocateIPWithIndex,omitempty"`
}

// NVIPAMPoolType are the supported NVIPAM pools types
type NVIPAMPoolType string

const (
	// PoolTypeIPPool contains string representation for pool type of IPPool
	PoolTypeIPPool NVIPAMPoolType = "ippool"
	// PoolTypeCIDRPool contains string representation for pool type of CIDRPool
	PoolTypeCIDRPool NVIPAMPoolType = "cidrpool"
)

// NVIPAMIPAllocatorResult is a struct that contains the fields the result of the IP Allocator will write to a file per request
// for each IP allocated.
type NVIPAMIPAllocatorResult struct {
	// IP is the allocated IP in CIDR format
	IP string `json:"ip"`
	// Gateway is the gateway of the subnet the IP belongs to
	Gateway string `json:"gateway"`
}

// New creates a new IPAllocator that can handle allocation of IPs using the NVIPAM.
func New(cninet *libcni.CNIConfig, podName string, podNamespace string, podUID string) *NVIPAMIPAllocator {
	return &NVIPAMIPAllocator{
		cninet:       cninet,
		podName:      podName,
		podNamespace: podNamespace,
		podUID:       podUID,
	}
}

// ParseRequests parses requests from the given input. This input is supposed to be a list of json objects.
func (a *NVIPAMIPAllocator) ParseRequests(requests string) ([]NVIPAMIPAllocatorRequest, error) {
	reqs := []NVIPAMIPAllocatorRequest{}

	dec := json.NewDecoder(bytes.NewReader([]byte(requests)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&reqs); err != nil {
		return nil, fmt.Errorf("error while parsing requests: %w", err)
	}

	if len(reqs) == 0 {
		return nil, fmt.Errorf("no IP requests specified")
	}

	var errs []error
	names := make(map[string]interface{})
	for _, req := range reqs {
		if len(req.Name) == 0 {
			errs = append(errs, fmt.Errorf("name must be specified in %#v", req))
			continue
		}

		if _, ok := names[req.Name]; ok {
			errs = append(errs, fmt.Errorf("name must be unique, but found duplicate %s", req.Name))
			continue
		}
		names[req.Name] = struct{}{}

		if req.PoolType != "" && req.PoolType != PoolTypeCIDRPool && req.PoolType != PoolTypeIPPool {
			errs = append(errs, fmt.Errorf("poolType is specified to a non supported type %s", req.PoolType))
		}

		if req.AllocateIPWIthIndex != nil && *req.AllocateIPWIthIndex < 0 {
			errs = append(errs, fmt.Errorf("allocateIPWithIndex must not be negative, but it is %d", *req.AllocateIPWIthIndex))
		}
	}

	return reqs, kerrors.NewAggregate(errs)
}

// generateNetConfBytes generates a network configuration for the given input
// TODO: Use 1.0.0 after a release is cut that will include https://github.com/containernetworking/cni/pull/1052 so that
// we can parse 1.0.0 cni netConf from bytes.
func generateNetConfBytes(netName string, poolName string, poolType NVIPAMPoolType, allocateIPWithIndex *int32) ([]byte, error) {
	m := make(map[string]interface{})
	m["cniVersion"] = "0.4.0"
	m["name"] = netName
	m["type"] = "nv-ipam"
	m["ipam"] = map[string]interface{}{
		"type":     "nv-ipam",
		"poolName": poolName,
		"poolType": poolType,
	}

	if allocateIPWithIndex != nil {
		m["args"] = map[string]interface{}{
			"cni": map[string]interface{}{
				"allocateIPWithIndex": *allocateIPWithIndex,
			},
		}
	}

	bytes, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("error while marshaling netConf: %w", err)
	}
	return bytes, nil
}

// Allocate allocates an IP from the NVIPAM given the input request
func (a *NVIPAMIPAllocator) Allocate(ctx context.Context, req NVIPAMIPAllocatorRequest) error {
	rt := a.constructRuntimeConf(req.Name)

	netConfBytes, err := generateNetConfBytes(req.Name, req.PoolName, req.PoolType, req.AllocateIPWIthIndex)
	if err != nil {
		return fmt.Errorf("error while generating netconfig bytes: %w", err)
	}

	netconf, err := libcni.ConfFromBytes(netConfBytes)
	if err != nil {
		return fmt.Errorf("error while unmarshaling netconf: %w", err)
	}

	res, err := a.cninet.GetNetworkCachedResult(netconf, rt)
	if err != nil {
		return fmt.Errorf("error while getting result from cache: %w", err)
	}

	// CNI already called for this pod
	if res != nil {
		if err := a.populateSharedResultFile(req.Name, res); err != nil {
			return fmt.Errorf("error while populating file with cache result: %w", err)
		}
		return nil
	}

	res, err = a.cninet.AddNetwork(ctx, netconf, rt)
	if err != nil {
		return fmt.Errorf("error while calling CNI ADD: %w", err)
	}

	if res == nil {
		return errors.New("no result, something went wrong")
	}

	if err := a.populateSharedResultFile(req.Name, res); err != nil {
		return fmt.Errorf("error while populating file with CNI ADD result: %w", err)
	}
	return nil
}

// Deallocate deallocates the allocated IP from the NVIPAM
func (a *NVIPAMIPAllocator) Deallocate(ctx context.Context, req NVIPAMIPAllocatorRequest) error {
	rt := a.constructRuntimeConf(req.Name)

	netConfBytes, err := generateNetConfBytes(req.Name, req.PoolName, req.PoolType, req.AllocateIPWIthIndex)
	if err != nil {
		return fmt.Errorf("error while generating netconfig bytes: %w", err)
	}

	netconf, err := libcni.ConfFromBytes(netConfBytes)
	if err != nil {
		return fmt.Errorf("error while unmarshaling netconf: %w", err)
	}

	res, err := a.cninet.GetNetworkCachedResult(netconf, rt)
	if err != nil {
		return fmt.Errorf("error while getting result from cache: %w", err)
	}

	if res == nil {
		return errors.New("allocation can't be found in cache, we may leak some IP")
	}

	err = a.cninet.DelNetwork(ctx, netconf, rt)
	if err != nil {
		return fmt.Errorf("error while calling CNI DEL: %w", err)
	}

	return nil
}

func (a *NVIPAMIPAllocator) constructRuntimeConf(reqName string) *libcni.RuntimeConf {
	return &libcni.RuntimeConf{
		// ContainerID is used by NVIPAM to construct the key for the allocation entry in the local store. We don't need
		// a real container id. However it should be different per allocation.
		ContainerID: fmt.Sprintf("%s-%s", a.podUID, reqName),
		// NetNS is used by NVIPAM to construct the key for the allocation entry in the local store. We don't need a
		// a real network namespace. However it should be different per allocation.
		NetNS:  fmt.Sprintf("%s-%s", a.podUID, reqName),
		IfName: "allocator",
		Args: [][2]string{
			{"K8S_POD_NAME", a.podName},
			{"K8S_POD_NAMESPACE", a.podNamespace},
			{"K8S_POD_UID", a.podUID},
		},
	}
}

// populateSharedResultFile writes the IPs to the path which the consumer will read
func (a *NVIPAMIPAllocator) populateSharedResultFile(fileName string, res types.Result) error {
	res040, ok := res.(*types040.Result)
	if !ok {
		return errors.New("error converting result to 0.4.0 result")
	}

	results := make([]NVIPAMIPAllocatorResult, 0, len(res040.IPs))
	for _, ip := range res040.IPs {
		result := NVIPAMIPAllocatorResult{
			IP:      ip.Address.String(),
			Gateway: ip.Gateway.String(),
		}
		results = append(results, result)
	}
	bytes, err := json.Marshal(results)
	if err != nil {
		return err
	}

	resultDirPath := filepath.Join(a.FileSystemRoot, sharedResultDirectory)
	err = os.MkdirAll(resultDirPath, 0755)
	if err != nil {
		return fmt.Errorf("error while creating dir %s: %w", resultDirPath, err)
	}

	resultFilePath := filepath.Join(resultDirPath, fileName)
	err = os.WriteFile(resultFilePath, bytes, 0644)
	if err != nil {
		return fmt.Errorf("error while writing file %s: %w", resultFilePath, err)
	}

	return nil
}
