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

package rpcclient

import (
	"fmt"
	"path/filepath"

	snapstoragev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"

	"k8s.io/klog/v2"
)

const basePath = "/var/lib/nvidia/storage/snap/providers"
const timeout = 60

func ExposeDevice(snapProvider, deviceName string, dpuStatus snapstoragev1.VolumeAttachmentStatusDPU, parameters map[string]string) (int, string, string, error) {
	unixSocketPath := filepath.Join(basePath, snapProvider, "snap.sock")
	klog.Infof("Constructed Socket Path: %s", unixSocketPath)

	client, err := NewJSONRPCSnapClient(unixSocketPath, timeout)
	if err != nil {
		return 0, "", "", fmt.Errorf("failed to create JSON-RPC client: %v", err)
	}

	emulationFunctions, err := EmulationFunctionList(client)
	if err != nil {
		klog.Errorf("Failed to retrieve emulation functions: %v", err)
		return 0, "", "", fmt.Errorf("failed to retrieve emulation functions: %v", err)
	}

	subsystems, err := NvmeSubsystemList(client)
	if err != nil {
		return 0, "", "", fmt.Errorf("failed to retrieve NVMe subsystems: %v", err)
	}

	nsid := getNamespaceByDeviceName(deviceName, subsystems)

	var currUUID string
	if nsid == -1 {
		nsid, currUUID, err = NvmeNamespaceCreate(client, deviceName, subsystems, dpuStatus)
		if err != nil {
			return 0, "", "", fmt.Errorf("failed to create namespace: %v", err)
		}
		klog.Infof("Created new namespace: NSID=%d, UUID=%s", nsid, currUUID)
	} else {
		klog.Infof("Namespace already exists: NSID=%d", nsid)
	}

	ctrlID := getCtrlByDeviceName(deviceName, subsystems)
	if err != nil {
		klog.Errorf("Error retrieving NVMe controllers: %v", err)
		return nsid, "", currUUID, err
	}

	var pciBDF string
	if ctrlID != "" {
		klog.Infof("Controller already exists: ID=%s", ctrlID)
		pciBDF, err = getPciAddrByCtrlID(ctrlID, emulationFunctions)
		if err != nil {
			return nsid, "", currUUID, fmt.Errorf("failed to get PCI BDF for controller: %v", err)
		}
	} else {
		ctrlID, pciBDF, err = NvmeControllerCreate(client, subsystems, emulationFunctions, dpuStatus, parameters)
		if err != nil {
			return nsid, "", currUUID, fmt.Errorf("failed to create controller: %v", err)
		}

		isAttached := isControllerAttachedToNamespace(ctrlID, nsid, subsystems)
		if isAttached {
			klog.Infof("Controller %s is already attached to namespace %d, skipping attachment", ctrlID, nsid)
		} else {
			err = NvmeControllerAttachNs(client, ctrlID, nsid)
			if err != nil {
				return nsid, pciBDF, currUUID, fmt.Errorf("failed to attach namespace to controller: %v", err)
			}
		}

		err = NvmeControllerResume(client, ctrlID)
		if err != nil {
			return nsid, pciBDF, currUUID, fmt.Errorf("failed to resume controller: %v", err)
		}

		klog.Infof("Created new controller: ID=%s, PCI BDF=%s", ctrlID, pciBDF)
	}

	klog.Infof("Final Device State -> NSID=%d, PCI BDF=%s", nsid, pciBDF)
	return nsid, pciBDF, currUUID, nil
}

func DestroyDevice(snapProvider string, nsid int, pciAddr string) error {
	unixSocketPath := filepath.Join(basePath, snapProvider, "snap.sock")
	klog.Infof("Constructed Socket Path: %s", unixSocketPath)

	client, err := NewJSONRPCSnapClient(unixSocketPath, timeout)
	if err != nil {
		return fmt.Errorf("failed to create JSON-RPC client: %v", err)
	}

	emulationFunctions, err := EmulationFunctionList(client)
	if err != nil {
		klog.Errorf("Failed to get emulation functions list: %v", err)
		return fmt.Errorf("failed to retrieve emulation functions: %v", err)
	}

	subsystems, err := NvmeSubsystemList(client)
	if err != nil {
		return fmt.Errorf("failed to retrieve NVMe subsystems: %v", err)
	}

	ctrlID, err := getNvmeControllerByPciAddr(pciAddr, emulationFunctions)
	if err != nil {
		klog.Errorf("Error finding NVMe controller for PCI Address %s: %v", pciAddr, err)
		return fmt.Errorf("error finding NVMe controller: %v", err)
	}

	namespaceExists := checkNamespaceAttached(nsid, ctrlID, subsystems)
	if err != nil {
		klog.Errorf("Failed to check namespace existence: %v", err)
		return err
	}

	if ctrlID == "" && !namespaceExists {
		klog.Infof("NVMe Controller and namespace not found for PCI Address %s. Skipping detach and destroy.", pciAddr)
		return nil
	}

	// Detach the namespace only if both exist
	if ctrlID == "" || !namespaceExists {
		klog.Infof("Namespace/Controller does not exist. Skipping detach.")
	} else {
		err = NvmeControllerDetachNs(client, ctrlID, nsid)
		if err != nil {
			klog.Errorf("Failed to detach namespace: %v", err)
			return fmt.Errorf("failed to detach namespace: %v", err)
		}
		klog.Infof("Successfully detached namespace ID %d", nsid)
	}

	if ctrlID == "" {
		klog.Infof("Controller ID does not exist. Skipping destroy.")
	} else {
		err = NvmeControllerDestroy(client, ctrlID)
		if err != nil {
			klog.Errorf("Failed to destroy controller: %v", err)
			return fmt.Errorf("failed to destroy controller: %v", err)
		}
		klog.Infof("Successfully destroyed controller ID %s", ctrlID)
	}

	if !namespaceExists {
		klog.Infof("Namespace ID %d does not exist. Skipping detach.", nsid)
	} else {
		err = NvmeNamespaceDestroy(client, nsid, subsystems)
		if err != nil {
			klog.Errorf("Failed to destroy namespace ID %d: %v", nsid, err)
			return fmt.Errorf("failed to destroy namespace: %v", err)
		}
		klog.Infof("Successfully destroyed namespace ID %d", nsid)
	}

	return nil
}
