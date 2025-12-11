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

package util

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

var _ = Describe("Capacity", func() {
	Context("CanSatisfy", func() {
		It("converts BlueFieldSpecs to ResourceList according to DPUFlavor resource format", func() {
			spec := BlueFieldSpecs{CPU: 16, Mem: "32"}
			fit := []corev1.ResourceList{
				{
					corev1.ResourceCPU:    resource.MustParse("15"),
					corev1.ResourceMemory: resource.MustParse("32Gi"),
				},
				{
					corev1.ResourceCPU:    resource.MustParse("16"),
					corev1.ResourceMemory: resource.MustParse("31Gi"),
				},
				{
					corev1.ResourceCPU:    resource.MustParse("16"),
					corev1.ResourceMemory: resource.MustParse("32G"),
				},
			}
			notFit := []corev1.ResourceList{
				{
					corev1.ResourceCPU:    resource.MustParse("17"),
					corev1.ResourceMemory: resource.MustParse("32Gi"),
				},
				{
					corev1.ResourceCPU:    resource.MustParse("16"),
					corev1.ResourceMemory: resource.MustParse("33Gi"),
				},
				{
					corev1.ResourceCPU:    resource.MustParse("16"),
					corev1.ResourceMemory: resource.MustParse("33G"),
				},
			}
			for _, r := range fit {
				Expect(spec.CanSatisfy(r)).To(Equal(CapacitySatisfied))
			}
			for _, r := range notFit {
				Expect(spec.CanSatisfy(r)).To(Equal(CapacityInsufficient))
			}
		})

		It("should return CapacityUnknown for nil spec", func() {
			var spec *BlueFieldSpecs = nil
			req := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("8"),
				corev1.ResourceMemory: resource.MustParse("16Gi"),
			}
			Expect(spec.CanSatisfy(req)).To(Equal(CapacityUnknown))
		})

		It("should handle exact match", func() {
			spec := BlueFieldSpecs{CPU: 16, Mem: "32"}
			req := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("16"),
				corev1.ResourceMemory: resource.MustParse("32Gi"),
			}
			Expect(spec.CanSatisfy(req)).To(Equal(CapacitySatisfied))
		})

		It("should handle zero requirements", func() {
			spec := BlueFieldSpecs{CPU: 16, Mem: "32"}
			req := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("0"),
				corev1.ResourceMemory: resource.MustParse("0"),
			}
			Expect(spec.CanSatisfy(req)).To(Equal(CapacitySatisfied))
		})

		It("should correctly compare different memory formats", func() {
			spec := BlueFieldSpecs{CPU: 16, Mem: "32"}

			// BinarySI format (Gi)
			reqBinary := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("16"),
				corev1.ResourceMemory: resource.MustParse("32Gi"),
			}
			Expect(spec.CanSatisfy(reqBinary)).To(Equal(CapacitySatisfied))

			// DecimalSI format (G)
			reqDecimal := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("16"),
				corev1.ResourceMemory: resource.MustParse("32G"),
			}
			Expect(spec.CanSatisfy(reqDecimal)).To(Equal(CapacitySatisfied))
		})
	})

	Context("convertToResourceList", func() {
		It("should convert to BinarySI format", func() {
			spec := BlueFieldSpecs{CPU: 16, Mem: "32"}
			req := corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			}
			result, err := spec.convertToResourceList(req.Memory().Format)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Cpu().String()).To(Equal("16"))
			Expect(result.Memory().String()).To(Equal("32Gi"))
		})

		It("should convert to DecimalSI format", func() {
			spec := BlueFieldSpecs{CPU: 8, Mem: "16"}
			req := corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("1G"),
			}
			result, err := spec.convertToResourceList(req.Memory().Format)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Cpu().String()).To(Equal("8"))
			Expect(result.Memory().String()).To(Equal("16G"))
		})
	})

	Context("LookUpPartNumber", func() {
		It("should return BlueFieldSpecs for known part numbers", func() {
			testCases := map[string]BlueFieldSpecs{
				"06CMW1":             {8, "16"},
				"0HFWRM":             {16, "32"},
				"900-9D3B4-00CC-EA0": {8, "16"},
				"900-9D3B6-00CC-EA0": {16, "32"},
				"900-9D3C6-00CV-DA0": {16, "48"},
				"P66102-001":         {16, "32"},
				"SN37B36732":         {16, "32"},
			}

			for partNumber, expectedSpec := range testCases {
				spec := LookUpPartNumber(partNumber)
				Expect(spec).NotTo(BeNil(), "Part number %s should be found", partNumber)
				Expect(spec.CPU).To(Equal(expectedSpec.CPU), "CPU mismatch for %s", partNumber)
				Expect(spec.Mem).To(Equal(expectedSpec.Mem), "Mem mismatch for %s", partNumber)
			}
		})

		It("should return nil for unknown part numbers", func() {
			unknownPartNumbers := []string{
				"UNKNOWN-123",
				"",
				"invalid",
				"900-XXXX-00CC-EA0",
			}

			for _, partNumber := range unknownPartNumbers {
				spec := LookUpPartNumber(partNumber)
				Expect(spec).To(BeNil(), "Part number %s should not be found", partNumber)
			}
		})

		It("should return a copy, not the original map entry", func() {
			spec1 := LookUpPartNumber("06CMW1")
			spec2 := LookUpPartNumber("06CMW1")
			Expect(spec1).NotTo(BeIdenticalTo(spec2), "Should return different pointers")
			Expect(*spec1).To(Equal(*spec2), "Values should be equal")
		})
	})

	Context("LookUpModel", func() {
		It("grep model name from description", func() {
			descriptions := map[string]string{
				"Nvidia BlueField-3 B3140H E-series HHHL SuperNIC, 400GbE(default mode)/NDR IB, Single-port QSFP112, PCIe Gen5.0 x16, 8 Arm cores, 16GB on board DDR, integrated BMC, Crypto Enabled, Tall Bracket": "B3140H",
				"Nvidia Bluefield-3 Dual Port 200 GbE QSFP Crypto DPU": "",
				"NVIDIA BlueField-3 B3240 P-Series FHHL DPU for Cold Aisle, 400GbE/NDR IB (default mode), Dual-port QSFP112, PCIe Gen5.0 x16 with x16 PCIe extension option, 16 Arm cores, 32GB on-board DDR, integrated BMC, Crypto Enable":      "B3240",
				"NVIDIA BlueField-3 B3220 P-Series FHHL DPU, 200GbE (default mode) / NDR200 IB, Dual-port QSFP112, PCIe Gen5.0 x16 with x16 PCIe extension option, 16 Arm cores, 32GB on-board DDR, integrated BMC, Crypto Enabled, Tall Bracket": "B3220",
				"NVIDIA BlueField-3 B3210 P-Series FHHL DPU, 100GbE (default mode) / HDR100 IB, Dual-port QSFP112, PCIe Gen5.0 x16 with x16 PCIe extension option, 16 Arm cores, 32GB on-board DDR, integrated BMC, Crypto Enabled, Tall Bracket": "B3210",
				"NVIDIA BlueField-3 B3210E E-Series FHHL DPU, 100GbE (default mode) / HDR100 IB, Dual-port QSFP112, PCIe Gen5.0 x16 with x16 PCIe extension option, 16 Arm cores, 32GB on-board DDR,integrated BMC, Crypto Enabled, Tall Bracket": "B3210E",
				"Nvidia Bluefield-3 Dual Port 100 GbE QSFP Crypto DPU": "",
				"Oracle B3220 P-Series 200GbE,Dual-port QSFP112,PCIe Gen5.0 x16 ,16 ARM cores,32 GB on-board DDR, integrated BMC,Crypto Enabled,Tall Bracket":                                                                                                      "B3220",
				"NVIDIA BlueField-3 B3210L E-series FHHL SuperNIC, 100GbE (default mode) /HDR100 IB, Dual port QSFP112, PCIe Gen5.0 x16, 8 Arm cores, 16GB on-board DDR, integrated BMC, Crypto Enabled, Tall Bracket":                                             "B3210L",
				"NVIDIA BlueField-3 B3220L E-Series FHHL SuperNIC, 200GbE (default mode) / NDR200 IB, Dual-port QSFP112, PCIe Gen5.0 x16, 8 Arm cores, 16GB on-board DDR, integrated BMC, Crypto Enabled, Tall Bracket":                                            "B3220L",
				"NVIDIA BlueField-3 B3140L E-Series FHHL SuperNIC, 400GbE / NDR IB (default mode), Single-port QSFP112, PCIe Gen5.0 x16, 8 Arm cores, 16GB on-board DDR, integrated BMC, Crypto Enabled, Tall Bracket":                                             "B3140L",
				"NVIDIA BlueField-3 B3140L E-Series FHHL SuperNIC, 400GbE / NDR IB (default mode), Single-port QSFP112, PCIe Gen5.0 x16, 8 Arm cores, 16GB on-board DDR, integrated BMC, Crypto Disabled, Tall Bracket":                                            "B3140L",
				"NVIDIA BlueField-3 B3210L E-series FHHL SuperNIC, 100GbE (default mode) /HDR100 IB, Dual port QSFP112, PCIe Gen5.0 x16, 8 Arm cores, 16GB on-board DDR, integrated BMC, Crypto Disabled, Tall Bracket":                                            "B3210L",
				"NVIDIA BlueField-3 B3220L E-Series FHHL SuperNIC, 200GbE (default mode) / NDR200 IB, Dual-port QSFP112, PCIe Gen5.0 x16, 8 Arm cores, 16GB on-board DDR, integrated BMC, Crypto Disabled, Tall Bracket":                                           "B3220L",
				"NVIDIA BlueField-3 B3240 P-Series Dual-slot FHHL DPU, 400GbE / NDR IB (default mode), Dual-port QSFP112, PCIe Gen5.0 x16 with x16 PCIe extension option, 16 Arm cores, 32GB on-board DDR, integrated BMC, Crypto Enabled, Tall Bracket":           "B3240",
				"NVIDIA BlueField-3 B3240 P-Series FHHL DPU for Cold Aisle, 400GbE / NDR IB (default mode), Dual-port QSFP112, PCIe Gen5.0 x16 with x16 PCIe extension option, 16 Arm cores, 32GB on-board DDR, integrated BMC, Crypto Enabled, Tall Bracket":      "B3240",
				"NVIDIA BlueField-3 B3240 P-Series FHHL DPU for GB200 Cold Aisle, 400GbE / NDR IB (default mode), Dual-port QSFP112, PCIe Gen5 x16 with x16 PCIe extension option, 16 Arm cores, 32GB on-board DDR, integrated BMC, Crypto Enabled, No Bracket":    "B3240",
				"NVIDIA BlueField-3 B3210E E-Series FHHL DPU, 100GbE (default mode) / HDR100 IB, Dual-port QSFP112, PCIe Gen5.0 x16 with x16 PCIe extension option, 16 Arm cores, 32GB on-board DDR,integrated BMC, Crypto Disabled, Tall Bracket":                 "B3210E",
				"NVIDIA BlueField-3 B3240 P-Series Dual-slot FHHL DPU, 400GbE / NDR IB (default mode), Dual-port QSFP112, PCIe Gen5.0 x16 with x16 PCIe extension option, 16 Arm cores, 32GB on-board DDR, integrated BMC, Crypto Disabled, Tall Bracket":          "B3240",
				"NVIDIA BlueField-3 B3220 P-Series FHHL DPU, 200GbE (default mode) / NDR200 IB, Dual-port QSFP112, PCIe Gen5.0 x16 with x16 PCIe extension option, 16 Arm cores, 32GB on-board DDR, integrated BMC, Crypto Disabled, Tall Bracket":                 "B3220",
				"NVIDIA BlueField-3 B3210E E-Series FHHL DPU, 100GbE (default mode) / HDR100 IB, Dual-port QSFP112, PCIe Gen5.0 x16 with x16 PCIe extension option, 16 Arm cores, 32GB on-board DDR,integrated BMC, Crypto Disabled, Tall Bracket - for ByteDance": "B3210E",
				"NVIDIA BlueField-3 B3220 P-Series FHHL DPU, 200GbE (default mode) / NDR200 IB, Dual-port QSFP112, PCIe Gen5.0 x16 with x16 PCIe extension option, 16 Arm cores, 32GB on-board DDR, integrated BMC, Crypto Disabled, Tall Bracket - for ByteDance": "B3220",
				"BlueField-3 B3240 P-Series Single-slot FHHL DPU, 400GbE (default mode) / NDR IB, Dual-port QSFP112, PCIe Gen5.0 x16 with x16 PCIe extension option, 16 Arm cores, 32GB on-board DDR, integrated BMC, Crypto, Tall Bracket, Omniva clean board":    "B3240",
				"NVIDIA BlueField-3 B3220SH E-Series FHHL Storage Controller, 200GbE (default mode) / NDR200 IB, Dual-port QSFP112, PCIe Gen5.0 x16 with x16 PCIe extension option, 16 Arm cores, 48GB on-board DDR, integrated BMC, Crypto Enabled, Secure Boot":  "B3220SH",
				"NVIDIA BlueField-3 B3220SH E-Series FHHL Storage Controller, 200GbE (default mode) / NDR200 IB, Dual-port QSFP112, PCIe Gen5.0 x16 with x16 PCIe extension option, 16 Arm cores, 48GB on-board DDR, integrated BMC, Crypto Disabled, Tall Bracke": "B3220SH",
				"CADENCE BlueField-3 B3220SH E-Series FHHL Storage Controller, 200GbE (default mode) / NDR200 IB, Dual-port QSFP112, PCIe Gen5.0 x16 with x16 PCIe extension option, 16 Arm cores, 48GB on-board DDR, integrated BMC, Crypto Disabled, Tall Brack": "B3220SH",
				"Nvidia BlueField-3 B3140H E-series HHHL SuperNIC, 400GbE (default mode) /NDR IB, Single-port QSFP112, PCIe Gen5.0 x16, 8 Arm cores, 16GB on board DDR, integrated BMC, Crypto Enabled, Tall Bracket":                                              "B3140H",
				"Nvidia BlueField-3 B3140H E-series HHHL SuperNIC, 400GbE (default mode) /NDR IB, Single-port QSFP112, PCIe Gen5.0 x16, 8 Arm cores, 16GB on board DDR, integrated BMC, Crypto Disabled, Tall Bracket":                                             "B3140H",
				"Nvidia BlueField-3 B3140H E-series HHHL SuperNIC for Cold Aisle, 400GbE (default mode)/NDR IB, Single-port QSFP112, PCIe Gen5.0 x16, 8 Arm cores, 16GB on board DDR, integrated BMC, Crypto Disabled, Tall Bracket":                               "B3140H",
				"NVIDIA BlueField-3 B3240 P-Series FHHL DPU Liquid Cooled, 400GbE / NDR IB (default mode), Dual-port QSFP112, PCIe Gen5.0 x16 with x16 PCIe extension option, 16 Arm cores, 32GB on-board DDR, integrated BMC, Crypto Enabled, Tall Bracket":       "B3240",
				"HPE Data Processing Unit InfiniBand NDR200/Ethernet 200Gb 2-port QSFP112 FHHL B3220 Adapter":                                                                                                                                                      "B3220",
				"HPE Data Processing Unit InfiniBand NDR/Ethernet 400Gb 1-port QSFP112 HHHL 	 Adapter":                                                                                                                                                             "",
				"HPE Storage Networking 100GbE 2-port NVMe-oF TCP Accelerated Secure Network Adapter NV62100M":                                                                                                                                                     "",
				"ThinkSystem NVIDIA BlueField-3 VPI QSFP112 2P 200G PCIe Gen5 x16":        "",
				"ThinkSystem NVIDIA BlueField-3 VPI QSFP112 1P 400G PCIe Gen5 x16 B3140H": "B3140H",
			}

			for description, expectedModel := range descriptions {
				_, err := fmt.Fprintf(GinkgoWriter, "description: %s\n", description)
				Expect(err).ToNot(HaveOccurred())
				bfSpec := LookUpModel(description)
				var expectedSpec *BlueFieldSpecs
				if expectedModel != "" {
					s, ok := Models[expectedModel]
					Expect(ok).To(BeTrue())
					expectedSpec = &s
				} else {
					expectedSpec = nil
				}
				if expectedSpec == nil {
					Expect(bfSpec).To(BeNil())
				} else {
					Expect(bfSpec).NotTo(BeNil())
					Expect(bfSpec.CPU).To(Equal(expectedSpec.CPU))
					Expect(bfSpec.Mem).To(Equal(expectedSpec.Mem))
				}
			}
		})

		It("should return nil for descriptions without model", func() {
			noModelDescriptions := []string{
				"Some random description without model",
				"NVIDIA BlueField-3 without specific model",
				"",
			}

			for _, desc := range noModelDescriptions {
				spec := LookUpModel(desc)
				Expect(spec).To(BeNil(), "Description '%s' should not match any model", desc)
			}
		})

		It("should match model as whole word only", func() {
			// B3210 should not match B3210E or B3210L
			spec := LookUpModel("Model B3210 standalone")
			Expect(spec).NotTo(BeNil())
			Expect(spec.CPU).To(Equal(16))
			Expect(spec.Mem).To(Equal("32"))
		})

		It("should return a copy, not the original map entry", func() {
			spec1 := LookUpModel("NVIDIA BlueField-3 B3240")
			spec2 := LookUpModel("NVIDIA BlueField-3 B3240")
			Expect(spec1).NotTo(BeIdenticalTo(spec2), "Should return different pointers")
		})
	})

	Context("LookUpResource", func() {
		It("grep resource from description", func() {
			desc := "NVIDIA BlueField-3 B3220 P-Series FHHL DPU; 200GbE (default mode) / NDR200 IB; Dual-port QSFP112; PCIe Gen5.0 x16 with x16 PCIe extension option; 16 Arm cores; 32GB on-board DDR; integrated BMC; Crypto Enabled"
			bfSpec := LookUpResource(desc)
			Expect(bfSpec).NotTo(BeNil())
			Expect(bfSpec.CPU).To(Equal(16))
			Expect(bfSpec.Mem).To(Equal("32"))
		})

		It("should parse various valid descriptions", func() {
			testCases := []struct {
				description string
				cpu         int
				mem         string
			}{
				{"16 Arm cores, 32GB on-board DDR", 16, "32"},
				{"8 Arm cores, 16GB on-board DDR", 8, "16"},
				{"16 Arm cores, 48GB on-board DDR", 16, "48"},
				{"Description with 8 Arm cores and 16GB on-board DDR info", 8, "16"},
			}

			for _, tc := range testCases {
				spec := LookUpResource(tc.description)
				Expect(spec).NotTo(BeNil(), "Description '%s' should match", tc.description)
				Expect(spec.CPU).To(Equal(tc.cpu), "CPU mismatch for '%s'", tc.description)
				Expect(spec.Mem).To(Equal(tc.mem), "Mem mismatch for '%s'", tc.description)
			}
		})

		It("should return nil for invalid descriptions", func() {
			invalidDescriptions := []string{
				"",
				"No cores or memory info",
				"16 cores, 32GB DDR", // missing "Arm"
				"16 Arm cores only",  // missing memory
				"32GB on-board DDR",  // missing cores
			}

			for _, desc := range invalidDescriptions {
				spec := LookUpResource(desc)
				Expect(spec).To(BeNil(), "Description '%s' should not match", desc)
			}
		})
	})

	Context("ParseDescription", func() {
		It("should try LookUpModel first, then LookUpResource", func() {
			// Description with both model and resource info - should use model
			desc := "NVIDIA BlueField-3 B3220 with 16 Arm cores, 32GB on-board DDR"
			spec := ParseDescription(desc)
			Expect(spec).NotTo(BeNil())
			// B3220 has {16, "32"} which matches the resource info
			Expect(spec.CPU).To(Equal(16))
			Expect(spec.Mem).To(Equal("32"))
		})

		It("should fall back to LookUpResource when model not found", func() {
			desc := "Unknown model with 8 Arm cores, 16GB on-board DDR"
			spec := ParseDescription(desc)
			Expect(spec).NotTo(BeNil())
			Expect(spec.CPU).To(Equal(8))
			Expect(spec.Mem).To(Equal("16"))
		})

		It("should return nil when no method matches", func() {
			desc := "Completely unknown device"
			spec := ParseDescription(desc)
			Expect(spec).To(BeNil())
		})
	})
})
