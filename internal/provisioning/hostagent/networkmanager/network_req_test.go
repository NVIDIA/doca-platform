//go:build linux

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

package networkmanager

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	hostutil "github.com/nvidia/doca-platform/internal/provisioning/hostagent/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("NetworkManager", func() {
	Context("NetworkManager.NetworkRequest", Label("NetworkRequest"), func() {
		It("should serialize to JSON correctly", func() {
			nr := NetworkRequest{
				DpuName:         "test-dpu",
				DPUNamespace:    "default",
				UID:             "test-uid-123",
				VFName:          "enp3s0f0v0",
				SerialNumber:    "MT2334XZ0L",
				PCIAddress:      "0000:03:00.0",
				NumOfVFs:        4,
				ControlPlaneMTU: 1500,
				PortConfigs: []hostutil.PortConfig{
					{PortNumber: 0, MTU: int32Ptr(9000)},
				},
			}

			jsonData, err := json.Marshal(nr)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(jsonData)).To(ContainSubstring(`"dpuName":"test-dpu"`))
			Expect(string(jsonData)).To(ContainSubstring(`"serialNumber":"MT2334XZ0L"`))
			Expect(string(jsonData)).To(ContainSubstring(`"numOfVFs":4`))
		})

		It("should deserialize from JSON correctly", func() {
			jsonData := `{
				"dpuName": "test-dpu",
				"dpuNamespace": "default",
				"uid": "test-uid-123",
				"vfName": "enp3s0f0v0",
				"serialNumber": "MT2334XZ0L",
				"pciAddress": "0000:03:00.0",
				"numOfVFs": 4,
				"controlPlaneMTU": 1500
			}`

			var nr NetworkRequest
			err := json.Unmarshal([]byte(jsonData), &nr)
			Expect(err).NotTo(HaveOccurred())
			Expect(nr.DpuName).To(Equal("test-dpu"))
			Expect(nr.DPUNamespace).To(Equal("default"))
			Expect(nr.UID).To(Equal("test-uid-123"))
			Expect(nr.VFName).To(Equal("enp3s0f0v0"))
			Expect(nr.SerialNumber).To(Equal("MT2334XZ0L"))
			Expect(nr.PCIAddress).To(Equal("0000:03:00.0"))
			Expect(nr.NumOfVFs).To(Equal(4))
			Expect(nr.ControlPlaneMTU).To(Equal(1500))
		})

		It("should handle empty PortConfigs", func() {
			nr := NetworkRequest{
				DpuName:      "test-dpu",
				DPUNamespace: "default",
			}

			jsonData, err := json.Marshal(nr)
			Expect(err).NotTo(HaveOccurred())
			// PortConfigs should be omitted when empty (omitempty tag)
			Expect(string(jsonData)).NotTo(ContainSubstring("portConfigs"))
		})
	})

	Context("SetDPUObjectMeta", Label("SetDPUObjectMeta"), func() {
		It("should set DPU metadata on NetworkRequest", func() {
			nr := &NetworkRequest{}
			dpu := provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpu",
					Namespace: "test-namespace",
					UID:       types.UID("test-uid-456"),
				},
			}

			nr.SetDPUObjectMeta(dpu)

			Expect(nr.DpuName).To(Equal("test-dpu"))
			Expect(nr.DPUNamespace).To(Equal("test-namespace"))
			Expect(nr.UID).To(Equal("test-uid-456"))
		})

		It("should overwrite existing metadata", func() {
			nr := &NetworkRequest{
				DpuName:      "old-dpu",
				DPUNamespace: "old-namespace",
				UID:          "old-uid",
			}
			dpu := provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "new-dpu",
					Namespace: "new-namespace",
					UID:       types.UID("new-uid"),
				},
			}

			nr.SetDPUObjectMeta(dpu)

			Expect(nr.DpuName).To(Equal("new-dpu"))
			Expect(nr.DPUNamespace).To(Equal("new-namespace"))
			Expect(nr.UID).To(Equal("new-uid"))
		})
	})

	Context("parseVFConfig", Label("parseVFConfig"), func() {
		It("should parse valid VF config content", func() {
			content := `serial_number=MT2334XZ0L
device_pci_address=0000:03:00.0
num_of_vfs=4
control_plane_mtu=1500`

			nr, err := parseVFConfig(strings.NewReader(content))
			Expect(err).NotTo(HaveOccurred())
			Expect(nr.SerialNumber).To(Equal("MT2334XZ0L"))
			Expect(nr.PCIAddress).To(Equal("0000:03:00.0"))
			Expect(nr.NumOfVFs).To(Equal(4))
			Expect(nr.ControlPlaneMTU).To(Equal(1500))
		})

		It("should parse VF config with extra whitespace in values", func() {
			content := `serial_number=  MT2334XZ0L  
device_pci_address=  0000:03:00.0  
num_of_vfs=  4  
control_plane_mtu=  1500  `

			nr, err := parseVFConfig(strings.NewReader(content))
			Expect(err).NotTo(HaveOccurred())
			Expect(nr.SerialNumber).To(Equal("MT2334XZ0L"))
			Expect(nr.PCIAddress).To(Equal("0000:03:00.0"))
			Expect(nr.NumOfVFs).To(Equal(4))
			Expect(nr.ControlPlaneMTU).To(Equal(1500))
		})

		It("should handle partial VF config", func() {
			content := `serial_number=MT2334XZ0L
device_pci_address=0000:03:00.0`

			nr, err := parseVFConfig(strings.NewReader(content))
			Expect(err).NotTo(HaveOccurred())
			Expect(nr.SerialNumber).To(Equal("MT2334XZ0L"))
			Expect(nr.PCIAddress).To(Equal("0000:03:00.0"))
			Expect(nr.NumOfVFs).To(Equal(0))
			Expect(nr.ControlPlaneMTU).To(Equal(0))
		})

		It("should ignore unknown keys", func() {
			content := `serial_number=MT2334XZ0L
unknown_key=some_value
device_pci_address=0000:03:00.0`

			nr, err := parseVFConfig(strings.NewReader(content))
			Expect(err).NotTo(HaveOccurred())
			Expect(nr.SerialNumber).To(Equal("MT2334XZ0L"))
			Expect(nr.PCIAddress).To(Equal("0000:03:00.0"))
		})

		It("should return error for invalid line format", func() {
			content := `serial_number=MT2334XZ0L
invalid_line_without_equals`

			_, err := parseVFConfig(strings.NewReader(content))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid line"))
		})

		It("should return error for invalid num_of_vfs", func() {
			content := `num_of_vfs=not_a_number`

			_, err := parseVFConfig(strings.NewReader(content))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid num_of_vfs"))
		})

		It("should return error for invalid control_plane_mtu", func() {
			content := `control_plane_mtu=not_a_number`

			_, err := parseVFConfig(strings.NewReader(content))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid control_plane_mtu"))
		})

		It("should handle empty content", func() {
			content := ``

			nr, err := parseVFConfig(strings.NewReader(content))
			Expect(err).NotTo(HaveOccurred())
			Expect(nr.SerialNumber).To(BeEmpty())
			Expect(nr.PCIAddress).To(BeEmpty())
		})

		It("should handle values containing equals sign", func() {
			content := `serial_number=MT2334=XZ0L`

			nr, err := parseVFConfig(strings.NewReader(content))
			Expect(err).NotTo(HaveOccurred())
			Expect(nr.SerialNumber).To(Equal("MT2334=XZ0L"))
		})
	})

	Context("writeNetworkRequestFile", Label("writeNetworkRequestFile"), func() {
		var (
			tempDir               string
			origNetworkRequestDir string
		)

		BeforeEach(func() {
			var err error
			tempDir, err = os.MkdirTemp("", "network-req-test-*")
			Expect(err).NotTo(HaveOccurred())

			origNetworkRequestDir = NetworkRequestDir
			NetworkRequestDir = tempDir
		})

		AfterEach(func() {
			NetworkRequestDir = origNetworkRequestDir
			_ = os.RemoveAll(tempDir)
		})

		It("should write network request file with correct content", func() {
			nr := &NetworkRequest{
				DpuName:         "test-dpu",
				DPUNamespace:    "default",
				UID:             "test-uid-789",
				VFName:          "enp3s0f0v0",
				SerialNumber:    "MT2334XZ0L",
				PCIAddress:      "0000:03:00.0",
				NumOfVFs:        4,
				ControlPlaneMTU: 1500,
			}

			err := writeNetworkRequestFile(nr)
			Expect(err).NotTo(HaveOccurred())

			// Verify file was created
			filePath := filepath.Join(tempDir, nr.UID)
			Expect(filePath).To(BeAnExistingFile())

			// Read and verify content
			readData, err := os.ReadFile(filePath)
			Expect(err).NotTo(HaveOccurred())

			var readNR NetworkRequest
			err = json.Unmarshal(readData, &readNR)
			Expect(err).NotTo(HaveOccurred())
			Expect(readNR.DpuName).To(Equal("test-dpu"))
			Expect(readNR.DPUNamespace).To(Equal("default"))
			Expect(readNR.UID).To(Equal("test-uid-789"))
			Expect(readNR.VFName).To(Equal("enp3s0f0v0"))
			Expect(readNR.SerialNumber).To(Equal("MT2334XZ0L"))
			Expect(readNR.PCIAddress).To(Equal("0000:03:00.0"))
			Expect(readNR.NumOfVFs).To(Equal(4))
			Expect(readNR.ControlPlaneMTU).To(Equal(1500))
		})

		It("should create directory if it doesn't exist", func() {
			// Use a nested directory that doesn't exist
			NetworkRequestDir = filepath.Join(tempDir, "nested", "dir")

			nr := &NetworkRequest{
				UID: "test-uid",
			}

			err := writeNetworkRequestFile(nr)
			Expect(err).NotTo(HaveOccurred())
			Expect(filepath.Join(NetworkRequestDir, nr.UID)).To(BeAnExistingFile())
		})

		It("should write network request with PortConfigs", func() {
			nr := &NetworkRequest{
				DpuName:      "test-dpu",
				DPUNamespace: "default",
				UID:          "test-uid-with-ports",
				PortConfigs: []hostutil.PortConfig{
					{PortNumber: 0, MTU: int32Ptr(9000)},
					{PortNumber: 1, MTU: int32Ptr(1500)},
				},
			}

			err := writeNetworkRequestFile(nr)
			Expect(err).NotTo(HaveOccurred())

			filePath := filepath.Join(tempDir, nr.UID)
			readData, err := os.ReadFile(filePath)
			Expect(err).NotTo(HaveOccurred())

			var readNR NetworkRequest
			err = json.Unmarshal(readData, &readNR)
			Expect(err).NotTo(HaveOccurred())
			Expect(readNR.PortConfigs).To(HaveLen(2))
			Expect(readNR.PortConfigs[0].PortNumber).To(Equal(int32(0)))
			Expect(*readNR.PortConfigs[0].MTU).To(Equal(int32(9000)))
			Expect(readNR.PortConfigs[1].PortNumber).To(Equal(int32(1)))
			Expect(*readNR.PortConfigs[1].MTU).To(Equal(int32(1500)))
		})

		It("should overwrite existing file", func() {
			nr := &NetworkRequest{
				DpuName: "original-dpu",
				UID:     "test-uid-overwrite",
			}

			err := writeNetworkRequestFile(nr)
			Expect(err).NotTo(HaveOccurred())

			// Update and write again
			nr.DpuName = "updated-dpu"
			err = writeNetworkRequestFile(nr)
			Expect(err).NotTo(HaveOccurred())

			// Verify updated content
			filePath := filepath.Join(tempDir, nr.UID)
			readData, err := os.ReadFile(filePath)
			Expect(err).NotTo(HaveOccurred())

			var readNR NetworkRequest
			err = json.Unmarshal(readData, &readNR)
			Expect(err).NotTo(HaveOccurred())
			Expect(readNR.DpuName).To(Equal("updated-dpu"))
		})
	})
})

// Helper function to create int32 pointer
func int32Ptr(i int32) *int32 {
	return &i
}
