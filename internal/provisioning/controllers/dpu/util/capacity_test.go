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

var _ = Describe("Compare DPUFlavor and BlueFieldSpecs", func() {
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
})

var _ = Describe("Parse BlueFieldSpecs from Description", func() {
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
			_, _ = GinkgoWriter.Write([]byte(fmt.Sprintf("description: %s\n", description)))
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

	It("grep resource from description", func() {
		desc := "NVIDIA BlueField-3 B3220 P-Series FHHL DPU; 200GbE (default mode) / NDR200 IB; Dual-port QSFP112; PCIe Gen5.0 x16 with x16 PCIe extension option; 16 Arm cores; 32GB on-board DDR; integrated BMC; Crypto Enabled"
		bfSpec := LookUpResource(desc)
		Expect(bfSpec).NotTo(BeNil())
		Expect(bfSpec.CPU).To(Equal(16))
		Expect(bfSpec.Mem).To(Equal("32"))
	})
})
