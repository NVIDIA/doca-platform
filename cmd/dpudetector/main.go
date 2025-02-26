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
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nvidia/doca-platform/cmd/dpudetector/bash"
	"github.com/nvidia/doca-platform/cmd/dpudetector/dpu"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	"github.com/golang/glog"
)

const (
	Interval        = 60
	PCISysDir       = "/host-sys/bus/pci/devices"
	NFDFeaturesFile = "/etc/kubernetes/node-feature-discovery/features.d/dpu"
)

func main() {
	var psIDCollector bool
	flag.BoolVar(&psIDCollector, "collector.PSID", false, "Enable collecting the DPU PSID")

	err := flag.Set("logtostderr", "true")
	if err != nil {
		fmt.Println("flag set failed.")
		panic(err)
	}
	flag.Parse()
	defer glog.Flush()
	// 0xa2dc: MT43244 BlueField-3 integrated ConnectX-7 network controller
	// 0xa2d6: MT42822 BlueField-2 integrated ConnectX-6 Dx network controller
	deviceList := []string{"0xa2dc", "0xa2d6"}
	settings := discoverySettings{
		enablePSIDCollector: psIDCollector,
	}

	ticker := time.NewTicker(Interval * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		if err := discoveryDPU(deviceList, settings); err != nil {
			glog.Errorf("DPU discovery failed, error: %v", err)
		}
	}
}

type discoverySettings struct {
	enablePSIDCollector bool
}

func discoveryDPU(deviceList []string, settings discoverySettings) error {
	discoveryStart := time.Now()
	dpuMap := make(map[string]dpu.DPU)
	dpuIndex := 0

	files, err := os.ReadDir(PCISysDir)
	if err != nil {
		return err
	}
	for _, file := range files {
		filePath := filepath.Join(PCISysDir, file.Name(), "device")
		deviceid, err := os.ReadFile(filePath)
		if err != nil {
			return err
		}
		for _, device := range deviceList {
			// Skip if the device is not in the list of known devices.
			if device != strings.TrimSpace(string(deviceid)) {
				continue
			}

			fileName := file.Name()
			labelPCIAddr, cmdPCIAddr, err := formatPCIAddress(fileName)
			if err != nil {
				return err
			}

			// Skip if the DPU has been discovered already.
			if _, ok := dpuMap[labelPCIAddr]; ok {
				continue
			}

			var psid string
			if settings.enablePSIDCollector {
				psid, err = bash.GetPSID(cmdPCIAddr)
				if err != nil {
					return err
				}
			}
			pf0Name, err := getPF0Name(labelPCIAddr)
			if err != nil {
				return err
			}

			dpu := dpu.DPU{
				PCIAddress: labelPCIAddr, // 0000-04-00
				DeviceID:   device,       // 0xa2dc
				Index:      dpuIndex,     // 0
				PSID:       psid,         // MT_0000000375
				PF0Name:    pf0Name,      // ens1f0np0
			}
			dpuMap[labelPCIAddr] = dpu
			glog.Infof("discovery DPU: %v", dpu)
			dpuIndex++
		}
	}

	glog.Infof("discovery completed, duration: %v", time.Since(discoveryStart))
	return writeNFDFeatureFile(dpuMap)
}

/* The pciAddress parameter likes "0000:04:00.x"
 * The labelPCIAddr which wil be used in node label, likes 0000-04-00
 * The cmdPCIAddr which will be used in mlx tool command, likes 04:00.0
 */
func formatPCIAddress(pciAddress string) (labelPCIAddr string, cmdPCIAddr string, err error) {
	dotIndex := strings.LastIndex(pciAddress, ".")
	if dotIndex != -1 {
		labelPCIAddr = pciAddress[:dotIndex]
	} else {
		return "", "", fmt.Errorf("get label_pci_addr failed from %s", pciAddress)
	}

	colonIndex := strings.Index(labelPCIAddr, ":")
	if colonIndex != -1 {
		cmdPCIAddr = labelPCIAddr[colonIndex+1:] + ".0"
	} else {
		return "", "", fmt.Errorf("get format PCI address failed from %s", pciAddress)
	}

	return strings.ReplaceAll(labelPCIAddr, ":", "-"), cmdPCIAddr, nil
}

// pciAddress: 0000-04-00
func getPF0Name(pciAddress string) (string, error) {
	pf0 := strings.ReplaceAll(pciAddress, "-", ":") + ".0"
	filePath := filepath.Join(PCISysDir, pf0, "net")
	glog.Infof("get PF0 from %s", filePath)
	files, err := os.ReadDir(filePath)
	if err != nil {
		return "", err
	}
	if len(files) != 0 {
		return files[0].Name(), nil
	}
	return "", fmt.Errorf("not found PF0 name")
}

func writeNFDFeatureFile(dpuMap map[string]dpu.DPU) error {
	discoveryFile, err := os.OpenFile(NFDFeaturesFile, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer func() {
		if err := discoveryFile.Close(); err != nil {
			glog.Errorf("close discovery file failed, err: %v", err)
		}
	}()

	write := bufio.NewWriter(discoveryFile)
	for _, dpu := range dpuMap {
		var pciLabel, psidLabel, pf0Label string
		if dpu.PCIAddress != "" {
			pciLabel = fmt.Sprintf("dpu-%d-pci-address=%s\r\n", dpu.Index, dpu.PCIAddress)
		}
		if dpu.PSID != "" {
			psidLabel = fmt.Sprintf("dpu-%d-ps-id=%s\r\n", dpu.Index, dpu.PSID)
		}
		if dpu.PF0Name != "" {
			pf0Label = fmt.Sprintf("dpu-%d-pf0-name=%s\r\n", dpu.Index, dpu.PF0Name)
		}
		if _, err := write.WriteString(pciLabel); err != nil {
			return err
		}
		if _, err := write.WriteString(psidLabel); err != nil {
			return err
		}
		if _, err := write.WriteString(pf0Label); err != nil {
			return err
		}
	}

	// Add label if the DPU OOB bridge is configured properly.
	if isOOBBridgeConfigured() {
		nfdString := fmt.Sprintf("%s=\r\n", cutil.DPUOOBBridgeConfiguredLabel)
		if _, err := write.WriteString(nfdString); err != nil {
			return err
		}
	}

	return write.Flush()
}
