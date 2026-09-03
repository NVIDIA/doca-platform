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

package initcontainer

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	noderesourcesv1 "github.com/nvidia/doca-platform/api/noderesources/v1alpha1"
	"github.com/nvidia/doca-platform/internal/nodesriovdeviceplugin/common"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	testclock "k8s.io/utils/clock/testing"
	"k8s.io/utils/ptr"
)

const (
	testTimeout           = time.Second * 30
	testReadinessTimeout  = time.Second * 10
	testReadinessInterval = time.Millisecond * 100
)

// mockSysfs holds a mock sysfs directory structure for testing.
type mockSysfs struct {
	root string
}

func writeInputConfigFile(dir, filename string, inputConfig common.NodeInputConfig) string {
	data, err := json.Marshal(inputConfig)
	Expect(err).NotTo(HaveOccurred())

	inputPath := filepath.Join(dir, filename)
	err = os.WriteFile(inputPath, data, 0644)
	Expect(err).NotTo(HaveOccurred())

	return inputPath
}

// newMockSysfs creates a new mock sysfs structure.
func newMockSysfs() *mockSysfs {
	root, err := os.MkdirTemp("", "mock-sysfs-*")
	Expect(err).NotTo(HaveOccurred())

	m := &mockSysfs{root: root}

	err = os.MkdirAll(filepath.Join(root, "bus/pci/devices"), 0755)
	Expect(err).NotTo(HaveOccurred())

	return m
}

func (m *mockSysfs) cleanup() {
	_ = os.RemoveAll(m.root)
}

// addPF adds a mock PF device with totalVFs.
//
//nolint:unparam
func (m *mockSysfs) addPF(pciAddr, serialNumber string, totalVFs int) {
	pfPath := filepath.Join(m.root, "bus/pci/devices", pciAddr)
	err := os.MkdirAll(pfPath, 0755)
	Expect(err).NotTo(HaveOccurred())

	// Write device ID (BlueField-3)
	err = os.WriteFile(filepath.Join(pfPath, "device"), []byte("0xa2dc\n"), 0644)
	Expect(err).NotTo(HaveOccurred())

	// Write VPD with serial number
	vpdData := createVPDWithSerial(serialNumber)
	err = os.WriteFile(filepath.Join(pfPath, "vpd"), vpdData, 0644)
	Expect(err).NotTo(HaveOccurred())

	// Write SR-IOV VF counts.
	if totalVFs > 0 {
		err = os.WriteFile(filepath.Join(pfPath, "sriov_totalvfs"),
			[]byte(fmt.Sprintf("%d", totalVFs)), 0644)
		Expect(err).NotTo(HaveOccurred())
		err = os.WriteFile(filepath.Join(pfPath, "sriov_numvfs"), []byte("0"), 0644)
		Expect(err).NotTo(HaveOccurred())
	}
}

// setNumVFs sets the number of enabled VFs for a mock PF.
//
//nolint:unparam
func (m *mockSysfs) setNumVFs(pfAddr string, numVFs int) {
	err := os.WriteFile(filepath.Join(m.root, "bus/pci/devices", pfAddr, "sriov_numvfs"),
		[]byte(fmt.Sprintf("%d", numVFs)), 0644)
	Expect(err).NotTo(HaveOccurred())
}

// addVF adds a mock VF by creating virtfn symlink from PF.
//
//nolint:unparam
func (m *mockSysfs) addVF(pfAddr string, vfIndex int, vfAddr string) {
	pfPath := filepath.Join(m.root, "bus/pci/devices", pfAddr)
	vfPath := filepath.Join(m.root, "bus/pci/devices", vfAddr)

	err := os.MkdirAll(vfPath, 0755)
	Expect(err).NotTo(HaveOccurred())

	virtfnPath := filepath.Join(pfPath, fmt.Sprintf("virtfn%d", vfIndex))
	err = os.Symlink("../"+vfAddr, virtfnPath)
	Expect(err).NotTo(HaveOccurred())
}

// createVPDWithSerial creates VPD data containing a serial number.
func createVPDWithSerial(serialNumber string) []byte {
	snBytes := []byte(serialNumber)
	fieldLen := len(snBytes)
	totalLen := 2 + 1 + fieldLen

	vpdData := []byte{0x90, byte(totalLen), 0x00, 'S', 'N', byte(fieldLen)}
	vpdData = append(vpdData, snBytes...)
	return vpdData
}

var _ = Describe("Init Container", func() {
	Context("readInputConfig", func() {
		var tempDir string
		BeforeEach(func() {
			var err error
			tempDir, err = os.MkdirTemp("", "inputconfig-test-*")
			Expect(err).NotTo(HaveOccurred())
		})
		AfterEach(func() {
			_ = os.RemoveAll(tempDir)
		})
		It("should read and parse valid input config", func() {
			inputConfig := common.NodeInputConfig{
				"SN1234": {
					{
						Name: "pods_vf",
						Type: noderesourcesv1.DevicePluginResourceTypeVF,
						Ranges: []noderesourcesv1.VFRange{
							{PFIndex: 0, Start: ptr.To(int32(2)), End: ptr.To(int32(10))},
						},
					},
				},
			}
			inputPath := writeInputConfigFile(tempDir, "input.json", inputConfig)

			result, err := readInputConfig("nvidia.com", inputPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(HaveLen(1))
			Expect(result).To(HaveKey("SN1234"))
		})
		It("should return error for non-existent file", func() {
			_, err := readInputConfig("nvidia.com", "/non/existent/path")
			Expect(err).To(HaveOccurred())
		})
		It("should return error for invalid JSON", func() {
			inputPath := filepath.Join(tempDir, "invalid.json")
			err := os.WriteFile(inputPath, []byte("not valid json"), 0644)
			Expect(err).NotTo(HaveOccurred())

			_, err = readInputConfig("nvidia.com", inputPath)
			Expect(err).To(HaveOccurred())
		})
		It("should return error for invalid resources", func() {
			inputConfig := common.NodeInputConfig{
				"SN1234": []noderesourcesv1.DevicePluginResource{},
			}
			inputPath := writeInputConfigFile(tempDir, "invalid-resources.json", inputConfig)

			_, err := readInputConfig("nvidia.com", inputPath)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("validation of input config for DPU SN1234 failed"))
			Expect(err.Error()).To(ContainSubstring("at least one resource must be provided"))
		})
	})
	Context("writeConfig", func() {
		var tempDir string
		BeforeEach(func() {
			var err error
			tempDir, err = os.MkdirTemp("", "config-test-*")
			Expect(err).NotTo(HaveOccurred())
		})
		AfterEach(func() {
			_ = os.RemoveAll(tempDir)
		})
		It("should write config", func() {
			config := &DevicePluginConfig{
				ResourceList: []ResourceConfig{
					{
						ResourceName: "pods_vf",
						Selectors: []Selector{
							{
								RootDevices: []string{"0000:b1:00.0#2-10"},
								IsRdma:      true,
							},
						},
					},
				},
			}

			err := writeConfig(tempDir, config)
			Expect(err).NotTo(HaveOccurred())

			data, err := os.ReadFile(filepath.Join(tempDir, "config.json"))
			Expect(err).NotTo(HaveOccurred())

			var readConfig DevicePluginConfig
			err = json.Unmarshal(data, &readConfig)
			Expect(err).NotTo(HaveOccurred())
			Expect(readConfig).To(BeComparableTo(DevicePluginConfig{
				ResourceList: []ResourceConfig{
					{
						ResourceName: "pods_vf",
						Selectors: []Selector{
							{
								RootDevices: []string{"0000:b1:00.0#2-10"},
								IsRdma:      true,
							},
						},
					},
				},
			}))
		})
	})
	Context("getExpectedDPUsFromInputConfig", func() {
		It("should group PFs by DPU serial", func() {
			inputConfig := common.NodeInputConfig{
				"SN1234": {{
					Name:   "res1",
					Type:   noderesourcesv1.DevicePluginResourceTypeVF,
					Ranges: []noderesourcesv1.VFRange{{PFIndex: 0}, {PFIndex: 1}},
				}},
				"SN5678": {{
					Name:   "res2",
					Type:   noderesourcesv1.DevicePluginResourceTypeVF,
					Ranges: []noderesourcesv1.VFRange{{PFIndex: 0}},
				}},
			}

			expectedDPUs := getExpectedDPUsFromInputConfig(inputConfig)
			Expect(expectedDPUs).To(Equal([]expectedDPU{
				{
					serial:                      "SN1234",
					pfIndexes:                   []int32{0, 1},
					devicePluginResourcesConfig: inputConfig["SN1234"],
				},
				{
					serial:                      "SN5678",
					pfIndexes:                   []int32{0},
					devicePluginResourcesConfig: inputConfig["SN5678"],
				},
			}))
		})
		It("should deduplicate same PF referenced multiple times", func() {
			inputConfig := common.NodeInputConfig{
				"SN1234": {
					{
						Name:   "res1",
						Type:   noderesourcesv1.DevicePluginResourceTypeVF,
						Ranges: []noderesourcesv1.VFRange{{PFIndex: 0}},
					},
					{
						Name:   "res2",
						Type:   noderesourcesv1.DevicePluginResourceTypeVF,
						Ranges: []noderesourcesv1.VFRange{{PFIndex: 0}},
					},
				},
			}

			expectedDPUs := getExpectedDPUsFromInputConfig(inputConfig)
			Expect(expectedDPUs).To(Equal([]expectedDPU{
				{
					serial:                      "SN1234",
					pfIndexes:                   []int32{0},
					devicePluginResourcesConfig: inputConfig["SN1234"],
				},
			}))
		})
	})
	Context("pfHasVFs", func() {
		var mock *mockSysfs
		BeforeEach(func() {
			mock = newMockSysfs()
		})
		AfterEach(func() {
			mock.cleanup()
		})
		It("should return true when sriov_numvfs is non-zero", func() {
			mock.addPF("0000:b1:00.0", "SN1234", 64)
			mock.setNumVFs("0000:b1:00.0", 1)

			Expect(pfHasVFs(mock.root, "0000:b1:00.0")).To(BeTrue())
		})
		It("should return false when sriov_numvfs is zero", func() {
			mock.addPF("0000:b1:00.0", "SN1234", 64)

			Expect(pfHasVFs(mock.root, "0000:b1:00.0")).To(BeFalse())
		})
	})
	Context("resolveVFRange", func() {
		DescribeTable("should resolve VF ranges",
			func(in noderesourcesv1.VFRange, want vfRangeInfo) {
				pf := &PFInfo{Address: "0000:b1:00.0", TotalVFs: 64}
				Expect(resolveVFRange(pf, in)).To(Equal(want))
			},
			Entry("explicit start and end",
				noderesourcesv1.VFRange{PFIndex: 0, Start: ptr.To(int32(2)), End: ptr.To(int32(10))},
				vfRangeInfo{start: 2, end: 10},
			),
			Entry("start defaults to 0",
				noderesourcesv1.VFRange{PFIndex: 0, End: ptr.To(int32(10))},
				vfRangeInfo{start: 0, end: 10},
			),
			Entry("end defaults to totalVFs-1",
				noderesourcesv1.VFRange{PFIndex: 0, Start: ptr.To(int32(2))},
				vfRangeInfo{start: 2, end: 63},
			),
			Entry("start/end defaults when missing",
				noderesourcesv1.VFRange{PFIndex: 0},
				vfRangeInfo{start: 0, end: 63},
			),
		)
	})
	Context("formatRootDevice", func() {
		DescribeTable("should format root device with VF range syntax",
			func(in []vfRangeInfo, want string) {
				Expect(formatRootDevice("0000:b1:00.0", in)).To(Equal(want))
			},
			Entry("single range",
				[]vfRangeInfo{{start: 2, end: 10}},
				"0000:b1:00.0#2-10",
			),
			Entry("multiple ranges combined",
				[]vfRangeInfo{{start: 2, end: 10}, {start: 15, end: 20}},
				"0000:b1:00.0#2-10,15-20",
			),
			Entry("ranges sorted by start index",
				[]vfRangeInfo{{start: 15, end: 20}, {start: 2, end: 10}},
				"0000:b1:00.0#2-10,15-20",
			),
			Entry("three or more ranges",
				[]vfRangeInfo{{start: 30, end: 35}, {start: 2, end: 10}, {start: 15, end: 20}},
				"0000:b1:00.0#2-10,15-20,30-35",
			),
		)
	})
	Context("buildDevicePluginConfig", func() {
		It("should build device plugin config with root devices", func() {
			inputConfig := common.NodeInputConfig{
				"SN1234": {
					{
						Name: "pods_vf",
						Type: noderesourcesv1.DevicePluginResourceTypeVF,
						Options: &noderesourcesv1.DevicePluginResourceOptions{
							IsRdma: ptr.To(true),
						},
						Ranges: []noderesourcesv1.VFRange{
							{PFIndex: 0, Start: ptr.To(int32(2)), End: ptr.To(int32(10))},
						},
					},
				},
			}

			dpuInfoList := []DPUInfo{{
				SerialNumber:                "SN1234",
				BaseAddress:                 "0000:b1:00",
				PFs:                         map[int32]*PFInfo{0: {Address: "0000:b1:00.0", TotalVFs: 64}},
				DevicePluginResourcesConfig: inputConfig["SN1234"],
			}}

			config, err := buildDevicePluginConfig("nvidia.com", dpuInfoList)
			Expect(err).NotTo(HaveOccurred())
			Expect(config).To(BeComparableTo(&DevicePluginConfig{
				ResourceList: []ResourceConfig{{
					ResourceName:   "pods_vf",
					ResourcePrefix: "nvidia.com",
					Selectors: []Selector{{
						RootDevices: []string{"0000:b1:00.0#2-10"},
						IsRdma:      true,
					}},
				}},
			}))
		})
		It("should use totalVFs when end is not specified", func() {
			inputConfig := common.NodeInputConfig{
				"SN1234": {{
					Name:   "pods_vf",
					Type:   noderesourcesv1.DevicePluginResourceTypeVF,
					Ranges: []noderesourcesv1.VFRange{{PFIndex: 0, Start: ptr.To(int32(2))}},
				}},
			}

			dpuInfoList := []DPUInfo{{
				SerialNumber:                "SN1234",
				BaseAddress:                 "0000:b1:00",
				PFs:                         map[int32]*PFInfo{0: {Address: "0000:b1:00.0", TotalVFs: 48}},
				DevicePluginResourcesConfig: inputConfig["SN1234"],
			}}

			config, err := buildDevicePluginConfig("", dpuInfoList)
			Expect(err).NotTo(HaveOccurred())
			Expect(config).To(BeComparableTo(&DevicePluginConfig{
				ResourceList: []ResourceConfig{{
					ResourceName: "pods_vf",
					Selectors:    []Selector{{RootDevices: []string{"0000:b1:00.0#2-47"}}},
				}},
			}))
		})
		It("should create separate selectors for different DPUs", func() {
			inputConfig := common.NodeInputConfig{
				"SN1234": {{
					Name:   "pods_vf",
					Type:   noderesourcesv1.DevicePluginResourceTypeVF,
					Ranges: []noderesourcesv1.VFRange{{PFIndex: 0, Start: ptr.To(int32(2)), End: ptr.To(int32(5))}},
				}},
				"SN5678": {{
					Name:   "pods_vf",
					Type:   noderesourcesv1.DevicePluginResourceTypeVF,
					Ranges: []noderesourcesv1.VFRange{{PFIndex: 0, Start: ptr.To(int32(2)), End: ptr.To(int32(5))}},
				}},
			}

			dpuInfoList := []DPUInfo{
				{
					SerialNumber:                "SN1234",
					BaseAddress:                 "0000:b1:00",
					PFs:                         map[int32]*PFInfo{0: {Address: "0000:b1:00.0", TotalVFs: 64}},
					DevicePluginResourcesConfig: inputConfig["SN1234"],
				},
				{
					SerialNumber:                "SN5678",
					BaseAddress:                 "0000:3c:00",
					PFs:                         map[int32]*PFInfo{0: {Address: "0000:3c:00.0", TotalVFs: 64}},
					DevicePluginResourcesConfig: inputConfig["SN5678"],
				},
			}

			config, err := buildDevicePluginConfig("nvidia.com", dpuInfoList)
			Expect(err).NotTo(HaveOccurred())
			Expect(config).To(BeComparableTo(&DevicePluginConfig{
				ResourceList: []ResourceConfig{{
					ResourceName:   "pods_vf",
					ResourcePrefix: "nvidia.com",
					Selectors: []Selector{
						{RootDevices: []string{"0000:b1:00.0#2-5"}},
						{RootDevices: []string{"0000:3c:00.0#2-5"}},
					},
				}},
			}))
		})
		It("should use explicit resource prefix over default", func() {
			inputConfig := common.NodeInputConfig{
				"SN1234": {{
					Name:           "pods_vf",
					Type:           noderesourcesv1.DevicePluginResourceTypeVF,
					ResourcePrefix: ptr.To("custom.io"),
					Ranges:         []noderesourcesv1.VFRange{{PFIndex: 0}},
				}},
			}

			dpuInfoList := []DPUInfo{{
				SerialNumber:                "SN1234",
				BaseAddress:                 "0000:b1:00",
				PFs:                         map[int32]*PFInfo{0: {Address: "0000:b1:00.0", TotalVFs: 64}},
				DevicePluginResourcesConfig: inputConfig["SN1234"],
			}}

			config, err := buildDevicePluginConfig("nvidia.com", dpuInfoList)
			Expect(err).NotTo(HaveOccurred())
			Expect(config).To(BeComparableTo(&DevicePluginConfig{
				ResourceList: []ResourceConfig{{
					ResourceName:   "pods_vf",
					ResourcePrefix: "custom.io",
					Selectors:      []Selector{{RootDevices: []string{"0000:b1:00.0#0-63"}}},
				}},
			}))
		})
		It("should set isRdma per selector based on DPU config", func() {
			inputConfig := common.NodeInputConfig{
				"SN1234": {{
					Name:    "mgmt_vf",
					Type:    noderesourcesv1.DevicePluginResourceTypeVF,
					Options: &noderesourcesv1.DevicePluginResourceOptions{IsRdma: ptr.To(true)},
					Ranges:  []noderesourcesv1.VFRange{{PFIndex: 0, Start: ptr.To(int32(1)), End: ptr.To(int32(1))}},
				}},
				"SN5678": {{
					Name:   "mgmt_vf",
					Type:   noderesourcesv1.DevicePluginResourceTypeVF,
					Ranges: []noderesourcesv1.VFRange{{PFIndex: 0, Start: ptr.To(int32(1)), End: ptr.To(int32(1))}},
				}},
			}

			dpuInfoList := []DPUInfo{
				{
					SerialNumber:                "SN1234",
					BaseAddress:                 "0000:b1:00",
					PFs:                         map[int32]*PFInfo{0: {Address: "0000:b1:00.0", TotalVFs: 64}},
					DevicePluginResourcesConfig: inputConfig["SN1234"],
				},
				{
					SerialNumber:                "SN5678",
					BaseAddress:                 "0000:3c:00",
					PFs:                         map[int32]*PFInfo{0: {Address: "0000:3c:00.0", TotalVFs: 64}},
					DevicePluginResourcesConfig: inputConfig["SN5678"],
				},
			}

			config, err := buildDevicePluginConfig("nvidia.com", dpuInfoList)
			Expect(err).NotTo(HaveOccurred())
			Expect(config).To(BeComparableTo(&DevicePluginConfig{
				ResourceList: []ResourceConfig{{
					ResourceName:   "mgmt_vf",
					ResourcePrefix: "nvidia.com",
					Selectors: []Selector{
						{RootDevices: []string{"0000:b1:00.0#1-1"}, IsRdma: true},
						{RootDevices: []string{"0000:3c:00.0#1-1"}},
					},
				}},
			}))
		})
		It("should combine multiple ranges for the same PF using multi-range syntax", func() {
			inputConfig := common.NodeInputConfig{
				"SN1234": {{
					Name: "pods_vf",
					Type: noderesourcesv1.DevicePluginResourceTypeVF,
					Ranges: []noderesourcesv1.VFRange{
						{PFIndex: 0, Start: ptr.To(int32(11)), End: ptr.To(int32(20))},
						{PFIndex: 0, Start: ptr.To(int32(2)), End: ptr.To(int32(10))},
					},
				}},
			}

			dpuInfoList := []DPUInfo{{
				SerialNumber:                "SN1234",
				BaseAddress:                 "0000:b1:00",
				PFs:                         map[int32]*PFInfo{0: {Address: "0000:06:00.0", TotalVFs: 64}},
				DevicePluginResourcesConfig: inputConfig["SN1234"],
			}}

			config, err := buildDevicePluginConfig("nvidia.com", dpuInfoList)
			Expect(err).NotTo(HaveOccurred())
			// Ranges should be combined and sorted by start index
			Expect(config).To(BeComparableTo(&DevicePluginConfig{
				ResourceList: []ResourceConfig{{
					ResourceName:   "pods_vf",
					ResourcePrefix: "nvidia.com",
					Selectors:      []Selector{{RootDevices: []string{"0000:06:00.0#2-10,11-20"}}},
				}},
			}))
		})
		It("should combine ranges from multiple PFs correctly", func() {
			inputConfig := common.NodeInputConfig{
				"SN1234": {{
					Name: "pods_vf",
					Type: noderesourcesv1.DevicePluginResourceTypeVF,
					Ranges: []noderesourcesv1.VFRange{
						{PFIndex: 0, Start: ptr.To(int32(2)), End: ptr.To(int32(10))},
						{PFIndex: 1, Start: ptr.To(int32(5)), End: ptr.To(int32(15))},
						{PFIndex: 0, Start: ptr.To(int32(15)), End: ptr.To(int32(20))},
					},
				}},
			}

			dpuInfoList := []DPUInfo{{
				SerialNumber: "SN1234",
				BaseAddress:  "0000:b1:00",
				PFs: map[int32]*PFInfo{
					0: {Address: "0000:06:00.0", TotalVFs: 64},
					1: {Address: "0000:06:00.1", TotalVFs: 64},
				},
				DevicePluginResourcesConfig: inputConfig["SN1234"],
			}}

			config, err := buildDevicePluginConfig("nvidia.com", dpuInfoList)
			Expect(err).NotTo(HaveOccurred())
			Expect(config).To(BeComparableTo(&DevicePluginConfig{
				ResourceList: []ResourceConfig{{
					ResourceName:   "pods_vf",
					ResourcePrefix: "nvidia.com",
					Selectors: []Selector{{
						RootDevices: []string{
							"0000:06:00.0#2-10,15-20",
							"0000:06:00.1#5-15",
						},
					}},
				}},
			}))
		})
		It("should handle multiple resources from the same DPU", func() {
			inputConfig := common.NodeInputConfig{
				"SN1234": {
					{
						Name:   "pods_vf",
						Type:   noderesourcesv1.DevicePluginResourceTypeVF,
						Ranges: []noderesourcesv1.VFRange{{PFIndex: 0, Start: ptr.To(int32(2)), End: ptr.To(int32(10))}},
					},
					{
						Name:   "mgmt_vf",
						Type:   noderesourcesv1.DevicePluginResourceTypeVF,
						Ranges: []noderesourcesv1.VFRange{{PFIndex: 1, Start: ptr.To(int32(0)), End: ptr.To(int32(1))}},
					},
				},
			}

			dpuInfoList := []DPUInfo{{
				SerialNumber: "SN1234",
				BaseAddress:  "0000:b1:00",
				PFs: map[int32]*PFInfo{
					0: {Address: "0000:b1:00.0", TotalVFs: 64},
					1: {Address: "0000:b1:00.1", TotalVFs: 64},
				},
				DevicePluginResourcesConfig: inputConfig["SN1234"],
			}}

			config, err := buildDevicePluginConfig("nvidia.com", dpuInfoList)
			Expect(err).NotTo(HaveOccurred())
			// Resources sorted by key (prefix/name): mgmt_vf < pods_vf
			Expect(config).To(BeComparableTo(&DevicePluginConfig{
				ResourceList: []ResourceConfig{
					{
						ResourceName:   "mgmt_vf",
						ResourcePrefix: "nvidia.com",
						Selectors:      []Selector{{RootDevices: []string{"0000:b1:00.1#0-1"}}},
					},
					{
						ResourceName:   "pods_vf",
						ResourcePrefix: "nvidia.com",
						Selectors:      []Selector{{RootDevices: []string{"0000:b1:00.0#2-10"}}},
					},
				},
			}))
		})
		It("should handle multiple resources from multiple DPUs", func() {
			inputConfig := common.NodeInputConfig{
				"SN1234": {
					{
						Name:   "pods_vf",
						Type:   noderesourcesv1.DevicePluginResourceTypeVF,
						Ranges: []noderesourcesv1.VFRange{{PFIndex: 0, Start: ptr.To(int32(2)), End: ptr.To(int32(10))}},
					},
					{
						Name:   "mgmt_vf",
						Type:   noderesourcesv1.DevicePluginResourceTypeVF,
						Ranges: []noderesourcesv1.VFRange{{PFIndex: 1, Start: ptr.To(int32(0)), End: ptr.To(int32(1))}},
					},
				},
				"SN5678": {
					{
						Name:   "pods_vf",
						Type:   noderesourcesv1.DevicePluginResourceTypeVF,
						Ranges: []noderesourcesv1.VFRange{{PFIndex: 0, Start: ptr.To(int32(2)), End: ptr.To(int32(10))}},
					},
					{
						Name:   "storage_vf",
						Type:   noderesourcesv1.DevicePluginResourceTypeVF,
						Ranges: []noderesourcesv1.VFRange{{PFIndex: 1, Start: ptr.To(int32(5)), End: ptr.To(int32(15))}},
					},
				},
			}

			dpuInfoList := []DPUInfo{
				{
					SerialNumber: "SN1234",
					BaseAddress:  "0000:b1:00",
					PFs: map[int32]*PFInfo{
						0: {Address: "0000:b1:00.0", TotalVFs: 64},
						1: {Address: "0000:b1:00.1", TotalVFs: 64},
					},
					DevicePluginResourcesConfig: inputConfig["SN1234"],
				},
				{
					SerialNumber: "SN5678",
					BaseAddress:  "0000:3c:00",
					PFs: map[int32]*PFInfo{
						0: {Address: "0000:3c:00.0", TotalVFs: 64},
						1: {Address: "0000:3c:00.1", TotalVFs: 64},
					},
					DevicePluginResourcesConfig: inputConfig["SN5678"],
				},
			}

			config, err := buildDevicePluginConfig("nvidia.com", dpuInfoList)
			Expect(err).NotTo(HaveOccurred())
			// Resources sorted by key (prefix/name): mgmt_vf < pods_vf < storage_vf
			// pods_vf has 2 selectors (sorted by DPU serial), mgmt_vf and storage_vf have 1 each
			Expect(config).To(BeComparableTo(&DevicePluginConfig{
				ResourceList: []ResourceConfig{
					{
						ResourceName:   "mgmt_vf",
						ResourcePrefix: "nvidia.com",
						Selectors:      []Selector{{RootDevices: []string{"0000:b1:00.1#0-1"}}},
					},
					{
						ResourceName:   "pods_vf",
						ResourcePrefix: "nvidia.com",
						Selectors: []Selector{
							{RootDevices: []string{"0000:b1:00.0#2-10"}},
							{RootDevices: []string{"0000:3c:00.0#2-10"}},
						},
					},
					{
						ResourceName:   "storage_vf",
						ResourcePrefix: "nvidia.com",
						Selectors:      []Selector{{RootDevices: []string{"0000:3c:00.1#5-15"}}},
					},
				},
			}))
		})
		It("should treat same resource name with different prefix as separate resources", func() {
			inputConfig := common.NodeInputConfig{
				"SN1234": {
					{
						Name:           "pods_vf",
						Type:           noderesourcesv1.DevicePluginResourceTypeVF,
						ResourcePrefix: ptr.To("nvidia.com"),
						Ranges:         []noderesourcesv1.VFRange{{PFIndex: 0, Start: ptr.To(int32(2)), End: ptr.To(int32(10))}},
					},
					{
						Name:           "pods_vf",
						Type:           noderesourcesv1.DevicePluginResourceTypeVF,
						ResourcePrefix: ptr.To("custom.io"),
						Ranges:         []noderesourcesv1.VFRange{{PFIndex: 1, Start: ptr.To(int32(0)), End: ptr.To(int32(5))}},
					},
				},
			}

			dpuInfoList := []DPUInfo{{
				SerialNumber: "SN1234",
				BaseAddress:  "0000:b1:00",
				PFs: map[int32]*PFInfo{
					0: {Address: "0000:b1:00.0", TotalVFs: 64},
					1: {Address: "0000:b1:00.1", TotalVFs: 64},
				},
				DevicePluginResourcesConfig: inputConfig["SN1234"],
			}}

			config, err := buildDevicePluginConfig("default.io", dpuInfoList)
			Expect(err).NotTo(HaveOccurred())
			// Same name but different prefix creates separate resources, sorted by key (prefix/name)
			// custom.io/pods_vf < nvidia.com/pods_vf
			Expect(config).To(BeComparableTo(&DevicePluginConfig{
				ResourceList: []ResourceConfig{
					{
						ResourceName:   "pods_vf",
						ResourcePrefix: "custom.io",
						Selectors:      []Selector{{RootDevices: []string{"0000:b1:00.1#0-5"}}},
					},
					{
						ResourceName:   "pods_vf",
						ResourcePrefix: "nvidia.com",
						Selectors:      []Selector{{RootDevices: []string{"0000:b1:00.0#2-10"}}},
					},
				},
			}))
		})
	})
	Context("Run (integration)", func() {
		var mock *mockSysfs
		var tempDir string
		BeforeEach(func() {
			mock = newMockSysfs()
			var err error
			tempDir, err = os.MkdirTemp("", "run-test-*")
			Expect(err).NotTo(HaveOccurred())
		})
		AfterEach(func() {
			mock.cleanup()
			_ = os.RemoveAll(tempDir)
		})
		It("should generate config for valid input", func() {
			mock.addPF("0000:b1:00.0", "SN1234", 64)
			mock.setNumVFs("0000:b1:00.0", 5)

			inputConfig := common.NodeInputConfig{
				"SN1234": {{
					Name:   "pods_vf",
					Type:   noderesourcesv1.DevicePluginResourceTypeVF,
					Ranges: []noderesourcesv1.VFRange{{PFIndex: 0, Start: ptr.To(int32(2)), End: ptr.To(int32(10))}},
				}},
			}
			inputPath := writeInputConfigFile(tempDir, "input.json", inputConfig)

			outputDir := filepath.Join(tempDir, "output")

			opts := Options{
				InputPath:                    inputPath,
				OutputPath:                   outputDir,
				SysFSRoot:                    mock.root,
				DefaultResourcePrefix:        "nvidia.com",
				DevicesReadinessTimeout:      testReadinessTimeout,
				DevicesReadinessPollInterval: testReadinessInterval,
			}

			err := Run(context.Background(), opts)
			Expect(err).NotTo(HaveOccurred())

			data, err := os.ReadFile(filepath.Join(outputDir, "config.json"))
			Expect(err).NotTo(HaveOccurred())

			var config DevicePluginConfig
			err = json.Unmarshal(data, &config)
			Expect(err).NotTo(HaveOccurred())
			Expect(config).To(BeComparableTo(DevicePluginConfig{
				ResourceList: []ResourceConfig{{
					ResourceName:   "pods_vf",
					ResourcePrefix: "nvidia.com",
					Selectors:      []Selector{{RootDevices: []string{"0000:b1:00.0#2-10"}}},
				}},
			}))
		})
		It("should return error for empty input config", func() {
			inputConfig := common.NodeInputConfig{}
			inputPath := writeInputConfigFile(tempDir, "input.json", inputConfig)

			outputDir := filepath.Join(tempDir, "output")

			opts := Options{
				InputPath:                    inputPath,
				OutputPath:                   outputDir,
				SysFSRoot:                    mock.root,
				DevicesReadinessTimeout:      testReadinessTimeout,
				DevicesReadinessPollInterval: testReadinessInterval,
			}

			err := Run(context.Background(), opts)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("empty"))
		})
	})
	Context("discoverDPUsAndWaitForReadiness (retry polling)", func() {
		var mock *mockSysfs
		var fakeClock *testclock.FakeClock
		type discoverResult struct {
			dpuInfoList []DPUInfo
			err         error
		}
		BeforeEach(func() {
			mock = newMockSysfs()
			fakeClock = testclock.NewFakeClock(time.Now())
		})
		AfterEach(func() {
			mock.cleanup()
		})
		It("should succeed on first attempt when sriov_numvfs is non-zero", func() {
			mock.addPF("0000:b1:00.0", "SN1234", 64)
			mock.setNumVFs("0000:b1:00.0", 16)

			inputConfig := common.NodeInputConfig{
				"SN1234": {{
					Name:   "pods_vf",
					Type:   noderesourcesv1.DevicePluginResourceTypeVF,
					Ranges: []noderesourcesv1.VFRange{{PFIndex: 0}},
				}},
			}

			dpuInfoList, err := discoverDPUsAndWaitForReadiness(context.Background(), fakeClock, mock.root,
				inputConfig, testReadinessTimeout, testReadinessInterval)
			Expect(err).NotTo(HaveOccurred())
			Expect(dpuInfoList).To(BeComparableTo([]DPUInfo{{
				SerialNumber:                "SN1234",
				BaseAddress:                 "0000:b1:00",
				PFs:                         map[int32]*PFInfo{0: {Address: "0000:b1:00.0", TotalVFs: 64}},
				DevicePluginResourcesConfig: inputConfig["SN1234"],
			}}))
		})
		It("should wait for sriov_numvfs to become non-zero", func() {
			mock.addPF("0000:b1:00.0", "SN1234", 64)
			mock.addVF("0000:b1:00.0", 0, "0000:b2:00.0")

			inputConfig := common.NodeInputConfig{
				"SN1234": {{
					Name:   "pods_vf",
					Type:   noderesourcesv1.DevicePluginResourceTypeVF,
					Ranges: []noderesourcesv1.VFRange{{PFIndex: 0}},
				}},
			}
			_, notReadyReasons, err := tryDiscoverDPUsAndCheckReadiness(mock.root,
				getExpectedDPUsFromInputConfig(inputConfig))
			Expect(err).NotTo(HaveOccurred())
			Expect(notReadyReasons).To(ConsistOf(
				"DPU SN1234 PF0 (0000:b1:00.0): no VFs created yet"))

			resCh := make(chan discoverResult, 1)
			go func() {
				dpuInfoList, err := discoverDPUsAndWaitForReadiness(context.Background(), fakeClock, mock.root,
					inputConfig, testReadinessTimeout, testReadinessInterval)
				resCh <- discoverResult{dpuInfoList: dpuInfoList, err: err}
			}()

			// A VF link alone must not unblock discovery while sriov_numvfs is zero.
			Eventually(fakeClock.HasWaiters).WithTimeout(testTimeout).Should(BeTrue())
			Consistently(resCh).WithTimeout(testReadinessInterval).ShouldNot(Receive())

			mock.setNumVFs("0000:b1:00.0", 1)
			fakeClock.Step(testReadinessInterval)

			var res discoverResult
			Eventually(resCh).WithTimeout(testTimeout).Should(Receive(&res))
			Expect(res.err).NotTo(HaveOccurred())
			Expect(res.dpuInfoList).To(BeComparableTo([]DPUInfo{{
				SerialNumber:                "SN1234",
				BaseAddress:                 "0000:b1:00",
				PFs:                         map[int32]*PFInfo{0: {Address: "0000:b1:00.0", TotalVFs: 64}},
				DevicePluginResourcesConfig: inputConfig["SN1234"],
			}}))
		})
		It("should timeout when VFs never become ready", func() {
			// Add PF but no VFs
			mock.addPF("0000:b1:00.0", "SN1234", 64)

			inputConfig := common.NodeInputConfig{
				"SN1234": {{
					Name:   "pods_vf",
					Type:   noderesourcesv1.DevicePluginResourceTypeVF,
					Ranges: []noderesourcesv1.VFRange{{PFIndex: 0}},
				}},
			}

			resCh := make(chan discoverResult, 1)
			go func() {
				dpuInfoList, err := discoverDPUsAndWaitForReadiness(context.Background(), fakeClock, mock.root,
					inputConfig, testReadinessTimeout, testReadinessInterval)
				resCh <- discoverResult{dpuInfoList: dpuInfoList, err: err}
			}()

			// Wait for waiters to be registered, then advance time past timeout
			Eventually(fakeClock.HasWaiters).WithTimeout(testTimeout).Should(BeTrue())
			fakeClock.Step(testReadinessTimeout)

			var res discoverResult
			Eventually(resCh).WithTimeout(testTimeout).Should(Receive(&res))
			Expect(res.err).To(HaveOccurred())
			Expect(res.dpuInfoList).To(BeNil())
			Expect(res.err.Error()).To(ContainSubstring("timeout"))
			Expect(res.err.Error()).To(ContainSubstring("no VFs created"))
		})
		It("should stop polling when context is canceled", func() {
			// Add PF but no VFs so polling continues
			mock.addPF("0000:b1:00.0", "SN1234", 64)

			inputConfig := common.NodeInputConfig{
				"SN1234": {{
					Name:   "pods_vf",
					Type:   noderesourcesv1.DevicePluginResourceTypeVF,
					Ranges: []noderesourcesv1.VFRange{{PFIndex: 0}},
				}},
			}

			ctx, cancel := context.WithCancel(context.Background())

			resCh := make(chan discoverResult, 1)
			go func() {
				dpuInfoList, err := discoverDPUsAndWaitForReadiness(ctx, fakeClock, mock.root, inputConfig,
					testReadinessTimeout, testReadinessInterval)
				resCh <- discoverResult{dpuInfoList: dpuInfoList, err: err}
			}()

			// Wait for waiters to be registered, then cancel context
			Eventually(fakeClock.HasWaiters).WithTimeout(testTimeout).Should(BeTrue())
			cancel()

			var res discoverResult
			Eventually(resCh).WithTimeout(testTimeout).Should(Receive(&res))
			Expect(res.err).To(HaveOccurred())
			Expect(res.dpuInfoList).To(BeNil())
			Expect(res.err.Error()).To(ContainSubstring("context canceled"))
		})
		It("should return error when any DPU is not found", func() {
			inputConfig := common.NodeInputConfig{
				"SN1234": {{
					Name:   "pods_vf",
					Type:   noderesourcesv1.DevicePluginResourceTypeVF,
					Ranges: []noderesourcesv1.VFRange{{PFIndex: 0}, {PFIndex: 1}},
				}},
				"SN5678": {{
					Name:   "pods_vf",
					Type:   noderesourcesv1.DevicePluginResourceTypeVF,
					Ranges: []noderesourcesv1.VFRange{{PFIndex: 0}},
				}},
			}

			// Add first DPU immediately
			mock.addPF("0000:b1:00.0", "SN1234", 64)
			mock.addPF("0000:b1:00.1", "SN1234", 64)
			mock.addVF("0000:b1:00.0", 0, "0000:b1:02.0")
			mock.addVF("0000:b1:00.1", 0, "0000:b1:02.1")

			dpuInfoList, err := discoverDPUsAndWaitForReadiness(context.Background(), fakeClock, mock.root,
				inputConfig, testReadinessTimeout, testReadinessInterval)
			Expect(err).To(HaveOccurred())
			Expect(dpuInfoList).To(BeNil())
			Expect(err.Error()).To(ContainSubstring("SN5678"))
			Expect(err.Error()).To(ContainSubstring("not found on node"))
		})
	})
})
