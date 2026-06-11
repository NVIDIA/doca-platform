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

//go:generate mockgen -copyright_file ../../hack/boilerplate.go.txt -package mock -destination mock/manager.go . ECPFManager

package ecpf

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/utils/networkhelper"

	"github.com/k8snetworkplumbingwg/sriovnet"
	"github.com/vishvananda/netlink/nl"
	"k8s.io/apimachinery/pkg/util/sets"
)

const (
	// Supported bluefield PCI device IDs as advertised in https://admin.pci-ids.ucw.cz/read/PC/15b3
	bluefield2DeviceID = "0xa2d6"
	bluefield3DeviceID = "0xa2dc"
	bluefield4DeviceID = "0xa2df"

	devlinkPCIBusName = "pci"

	sysBusPciDevicesDir = "/sys/bus/pci/devices"
)

var (
	dpuDeviceIDs = sets.New(bluefield2DeviceID, bluefield3DeviceID, bluefield4DeviceID)
)

// ECPFManager is an interface for managing ECPF representors.
type ECPFManager interface {
	// GetRepresentorForPFServiceInterface returns the representor for the given PF ServiceInterface.
	GetRepresentorForPFServiceInterface(pfsi *dpuservicev1.PF) (string, error)
	// GetRepresentorForVFServiceInterface returns the representor for the given VF ServiceInterface.
	GetRepresentorForVFServiceInterface(vfsi *dpuservicev1.VF) (string, error)
}

// ecpfEntry represents an embedded CPU PF (ECPF) device.
type ecpfEntry struct {
	// address is the PCI address of the ECPF device.
	address string
	// isDPU is true if the ECPF device belongs to the DPU ASIC.
	isDPU bool
}

type ecpfEntries []ecpfEntry

func (e ecpfEntries) String() string {
	addresses := make([]string, len(e))
	for i, entry := range e {
		addresses[i] = entry.address
	}
	return strings.Join(addresses, ",")
}

var _ ECPFManager = &ecpfManager{}

// ecpfManager is the implementation of the ECPFManager interface
type ecpfManager struct {
	ecpfs         []ecpfEntry
	fs            filesystem
	networkhelper networkhelper.NetworkHelper
}

// NewECPFManager creates a new ECPFManager instance.
func NewECPFManager() (ECPFManager, error) {
	return newECPFManager(networkhelper.New(), newFileSystem())
}

// newECPFManager creates a new ECPFManager instance with dependencies provided via args for testability.
func newECPFManager(networkhelper networkhelper.NetworkHelper, fs filesystem) (*ecpfManager, error) {
	em := &ecpfManager{
		networkhelper: networkhelper,
		fs:            fs,
	}

	ecpfs, err := em.discoverECPFs()
	if err != nil {
		return nil, fmt.Errorf("failed to discover ECPFs: %w", err)
	}
	em.ecpfs = ecpfs
	return em, nil
}

func (em *ecpfManager) discoverECPFs() ([]ecpfEntry, error) {
	ports, err := em.networkhelper.DevlinkPortList()
	if err != nil {
		return nil, fmt.Errorf("failed to list devlink ports: %w", err)
	}

	ecpfsAddresses := sets.New[string]()
	for _, port := range ports {
		if port.BusName != devlinkPCIBusName {
			continue
		}
		if port.PortFlavour != nl.DEVLINK_PORT_FLAVOUR_PCI_PF {
			continue
		}
		ecpfsAddresses.Insert(port.DeviceName)
	}
	// sort ecpfs addresses for consistency
	ecpfsAddressesSorted := ecpfsAddresses.UnsortedList()
	slices.Sort(ecpfsAddressesSorted)

	ecpfs := make([]ecpfEntry, 0, len(ecpfsAddressesSorted))
	for _, address := range ecpfsAddressesSorted {
		deviceIDBytes, err := em.fs.ReadFile(filepath.Join(sysBusPciDevicesDir, address, "device"))
		if err != nil {
			return nil, fmt.Errorf("failed to read device ID of ECPF %s: %w", address, err)
		}
		isDPU := dpuDeviceIDs.Has(strings.TrimSpace(string(deviceIDBytes)))

		ecpfs = append(ecpfs, ecpfEntry{
			address: address,
			isDPU:   isDPU,
		})
	}

	return ecpfs, nil
}

// GetRepresentorForPFServiceInterface returns the representor for the given PF
func (em *ecpfManager) GetRepresentorForPFServiceInterface(pfsi *dpuservicev1.PF) (string, error) {
	return em.getRepresentorForEachECPF(pfsi.NICSelector, func(ecpf string) (string, error) {
		pp := &sriovnet.RepresentorPortParams{
			ECPF:             ecpf,
			ControllerNumber: uint32(pfsi.NICSelector.GetControllerNumber()),
			PFNumber:         uint16(pfsi.ID),
		}
		return em.networkhelper.GetPfRepresentorFromPortParams(pp)
	})
}

// GetRepresentorForVFServiceInterface returns the representor for the given VF
func (em *ecpfManager) GetRepresentorForVFServiceInterface(vfsi *dpuservicev1.VF) (string, error) {
	return em.getRepresentorForEachECPF(vfsi.NICSelector, func(ecpf string) (string, error) {
		pp := &sriovnet.RepresentorPortParams{
			ECPF:             ecpf,
			ControllerNumber: uint32(vfsi.NICSelector.GetControllerNumber()),
			PFNumber:         uint16(vfsi.PFID),
		}
		return em.networkhelper.GetVfRepresentorFromPortParams(pp, uint32(vfsi.VFID))
	})
}

// getRepresentorForEachECPF returns the representor for the given NIC selector by trying to get the representor for each ECPF candidate
// getRepForECPF is a function that takes an ECPF address and returns the representor for that ECPF or error.
// It is expected to find exactly one representor across the ECPF candidates that match the NIC selector.
func (em *ecpfManager) getRepresentorForEachECPF(nicSelector *dpuservicev1.NICSelectorSpec, getRepForECPF func(ecpf string) (string, error)) (string, error) {
	// get candidate ecpfs entries
	candidateECPFs, err := em.getECPFCandidates(nicSelector)
	if err != nil {
		return "", fmt.Errorf("failed to get ECPF candidates: %w", err)
	}

	if len(candidateECPFs) == 0 {
		return "", fmt.Errorf("no ECPF candidates found")
	}

	// for each ecpf, try to retrieve the representor
	var errs []error
	//nolint:prealloc
	var representors []string
	for _, ecpf := range candidateECPFs {
		representor, err := getRepForECPF(ecpf.address)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to get representor for ECPF %s: %w", ecpf.address, err))
			continue
		}
		representors = append(representors, representor)
	}

	// return representor only if one found
	if len(representors) == 0 {
		return "", fmt.Errorf("no representor found on ECPF candidates: %s errors: %w", ecpfEntries(candidateECPFs).String(), errors.Join(errs...))
	}
	if len(representors) > 1 {
		return "", fmt.Errorf("multiple representors found for ECPF candidates. ECPFs: %s representors: %s errors: %w",
			ecpfEntries(candidateECPFs).String(), strings.Join(representors, ","), errors.Join(errs...))
	}

	return representors[0], nil
}

// getECPFCandidates returns the ECPF candidates that match the given NIC selector
func (em *ecpfManager) getECPFCandidates(nicSelector *dpuservicev1.NICSelectorSpec) ([]ecpfEntry, error) {
	if nicSelector == nil {
		return filterSlice(em.ecpfs, filterIsDPU), nil
	}

	// note: we only filter based on the selector type. the controller number is taken into account
	// when resolving the representor.

	switch nicSelector.Type {
	case dpuservicev1.NICSelectorTypeDPU:
		return filterSlice(em.ecpfs, filterIsDPU), nil
	case dpuservicev1.NICSelectorTypePCI:
		if nicSelector.PCI == nil {
			return nil, fmt.Errorf("PCI selector is nil")
		}
		return filterSlice(em.ecpfs, filterDBDMatch(nicSelector.PCI.Address)), nil
	default:
		return nil, fmt.Errorf("invalid NIC selector type: %s", nicSelector.Type)
	}
}

// FileSystem abstracts file and OS operations for testability.
type filesystem interface {
	// ReadFile reads the named file and returns the contents.
	ReadFile(name string) ([]byte, error)
}

// newFileSystem creates a new filesystem instance
func newFileSystem() filesystem {
	return &filesystemImpl{}
}

type filesystemImpl struct {
}

// ReadFile reads the named file and returns the contents.
func (o *filesystemImpl) ReadFile(name string) ([]byte, error) {
	return os.ReadFile(name)
}

// filterSlice filters the given slice using the given filter function
func filterSlice[T any](slice []T, filter func(T) bool) []T {
	var result []T
	for _, item := range slice {
		if filter(item) {
			result = append(result, item)
		}
	}
	return result
}

// filterIsDPU filters the given ECPF entry if it is a DPU
func filterIsDPU(e ecpfEntry) bool {
	return e.isDPU
}

// filterDBDMatch returns a filter function that filters on the given address D:B:D (Domain:Bus:Device prefix of an ecpfEntry.address)
func filterDBDMatch(address string) func(ecpfEntry) bool {
	return func(e ecpfEntry) bool {
		return e.address[:len(e.address)-2] == address[:len(address)-2]
	}
}
