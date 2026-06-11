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

package ecpf

import (
	"errors"
	"fmt"
	"path/filepath"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	networkhelper_mock "github.com/nvidia/doca-platform/pkg/utils/networkhelper/mock"

	"github.com/k8snetworkplumbingwg/sriovnet"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netlink/nl"
	"go.uber.org/mock/gomock"
	"k8s.io/utils/ptr"
)

type mockFilesystem struct {
	files map[string][]byte
}

func (m *mockFilesystem) ReadFile(name string) ([]byte, error) {
	data, ok := m.files[name]
	if !ok {
		return nil, fmt.Errorf("file not found: %s", name)
	}
	return data, nil
}

func pciDeviceFile(address string) string {
	return filepath.Join(sysBusPciDevicesDir, address, "device")
}

func pciPFDevlinkPort(address string) *netlink.DevlinkPort {
	return &netlink.DevlinkPort{
		BusName:     devlinkPCIBusName,
		DeviceName:  address,
		PortFlavour: nl.DEVLINK_PORT_FLAVOUR_PCI_PF,
	}
}

var _ = Describe("ECPF Manager tests", func() {
	var (
		ctrl   *gomock.Controller
		mockNH *networkhelper_mock.MockNetworkHelper
		fs     *mockFilesystem

		errTest = errors.New("test error")
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockNH = networkhelper_mock.NewMockNetworkHelper(ctrl)
		fs = &mockFilesystem{files: map[string][]byte{}}
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Describe("discoverECPFs", func() {
		It("discovers PCI PF ports, classifies DPU devices, and sorts by address", func() {
			mockNH.EXPECT().DevlinkPortList().Return([]*netlink.DevlinkPort{
				pciPFDevlinkPort("0000:03:00.1"),
				{
					BusName:     "auxiliary",
					DeviceName:  "mlx5_core.eth.0",
					PortFlavour: nl.DEVLINK_PORT_FLAVOUR_PHYSICAL,
				},
				{
					BusName:     devlinkPCIBusName,
					DeviceName:  "0000:03:00.0",
					PortFlavour: nl.DEVLINK_PORT_FLAVOUR_PCI_VF,
				},
				pciPFDevlinkPort("0000:03:00.0"),
			}, nil)

			fs.files[pciDeviceFile("0000:03:00.0")] = []byte(bluefield3DeviceID + "\n")
			fs.files[pciDeviceFile("0000:03:00.1")] = []byte("0x1234\n")

			mgr, err := newECPFManager(mockNH, fs)
			Expect(err).NotTo(HaveOccurred())

			Expect(mgr.ecpfs).To(Equal([]ecpfEntry{
				{address: "0000:03:00.0", isDPU: true},
				{address: "0000:03:00.1", isDPU: false},
			}))
		})

		It("returns an error when DevlinkPortList fails", func() {
			mockNH.EXPECT().DevlinkPortList().Return(nil, errTest)

			_, err := newECPFManager(mockNH, fs)
			Expect(err).To(MatchError(ContainSubstring("failed to discover ECPFs")))
			Expect(err).To(MatchError(ContainSubstring("failed to list devlink ports")))
		})

		It("returns an error when reading the PCI device ID fails", func() {
			mockNH.EXPECT().DevlinkPortList().Return([]*netlink.DevlinkPort{
				pciPFDevlinkPort("0000:03:00.0"),
			}, nil)

			_, err := newECPFManager(mockNH, fs)
			Expect(err).To(MatchError(ContainSubstring("failed to discover ECPFs")))
			Expect(err).To(MatchError(ContainSubstring("failed to read device ID of ECPF 0000:03:00.0")))
		})
	})

	Describe("ecpfEntries.String", func() {
		It("joins ECPF addresses", func() {
			Expect(ecpfEntries{
				{address: "0000:03:00.0"},
				{address: "0000:03:00.1"},
			}.String()).To(Equal("0000:03:00.0,0000:03:00.1"))
		})
	})

	Describe("getECPFCandidates", func() {
		var em *ecpfManager

		BeforeEach(func() {
			em = &ecpfManager{
				ecpfs: []ecpfEntry{
					{address: "0000:03:00.0", isDPU: true},
					{address: "0000:03:00.1", isDPU: false},
					{address: "0000:04:00.0", isDPU: true},
				},
			}
		})

		It("returns DPU ECPFs when the NIC selector is nil", func() {
			candidates, err := em.getECPFCandidates(nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(candidates).To(Equal([]ecpfEntry{
				{address: "0000:03:00.0", isDPU: true},
				{address: "0000:04:00.0", isDPU: true},
			}))
		})

		It("returns DPU ECPFs for a DPU NIC selector", func() {
			candidates, err := em.getECPFCandidates(&dpuservicev1.NICSelectorSpec{
				Type: dpuservicev1.NICSelectorTypeDPU,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(candidates).To(HaveLen(2))
		})

		It("returns ECPFs matching the PCI domain:bus:device prefix", func() {
			candidates, err := em.getECPFCandidates(&dpuservicev1.NICSelectorSpec{
				Type: dpuservicev1.NICSelectorTypePCI,
				PCI: &dpuservicev1.PCISelector{
					Address: "0000:03:00.2",
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(candidates).To(Equal([]ecpfEntry{
				{address: "0000:03:00.0", isDPU: true},
				{address: "0000:03:00.1", isDPU: false},
			}))
		})

		It("returns an error when the PCI selector is nil", func() {
			_, err := em.getECPFCandidates(&dpuservicev1.NICSelectorSpec{
				Type: dpuservicev1.NICSelectorTypePCI,
			})
			Expect(err).To(MatchError("PCI selector is nil"))
		})

		It("returns an error for an invalid NIC selector type", func() {
			_, err := em.getECPFCandidates(&dpuservicev1.NICSelectorSpec{
				Type: "invalid",
			})
			Expect(err).To(MatchError(ContainSubstring("invalid NIC selector type")))
		})
	})

	Describe("GetRepresentorForPF", func() {
		var em *ecpfManager

		BeforeEach(func() {
			em = &ecpfManager{
				networkhelper: mockNH,
				ecpfs: []ecpfEntry{
					{address: "0000:03:00.0", isDPU: true},
					{address: "0000:04:00.0", isDPU: true},
				},
			}
		})

		It("returns the representor from the matching DPU ECPF", func() {
			mockNH.EXPECT().
				GetPfRepresentorFromPortParams(gomock.Any()).
				DoAndReturn(func(pp *sriovnet.RepresentorPortParams) (string, error) {
					Expect(pp).To(Equal(&sriovnet.RepresentorPortParams{
						ECPF:             "0000:03:00.0",
						ControllerNumber: 2,
						PFNumber:         0,
					}))
					return "pf-rep0", nil
				})
			mockNH.EXPECT().
				GetPfRepresentorFromPortParams(gomock.Any()).
				Return("", errTest)

			rep, err := em.GetRepresentorForPFServiceInterface(&dpuservicev1.PF{
				ID: 0,
				NICSelector: &dpuservicev1.NICSelectorSpec{
					Type:             dpuservicev1.NICSelectorTypeDPU,
					ControllerNumber: ptr.To(int32(2)),
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(rep).To(Equal("pf-rep0"))
		})

		It("returns an error when no ECPF candidates match", func() {
			em.ecpfs = []ecpfEntry{{address: "0000:03:00.1", isDPU: false}}

			_, err := em.GetRepresentorForPFServiceInterface(&dpuservicev1.PF{
				ID: 1,
				NICSelector: &dpuservicev1.NICSelectorSpec{
					Type: dpuservicev1.NICSelectorTypeDPU,
				},
			})
			Expect(err).To(MatchError(ContainSubstring("no ECPF candidates found")))
		})

		It("returns an error when no representor is found on any candidate", func() {
			mockNH.EXPECT().GetPfRepresentorFromPortParams(gomock.Any()).Return("", errTest).Times(2)

			_, err := em.GetRepresentorForPFServiceInterface(&dpuservicev1.PF{
				ID: 1,
				NICSelector: &dpuservicev1.NICSelectorSpec{
					Type: dpuservicev1.NICSelectorTypeDPU,
				},
			})
			Expect(err).To(MatchError(ContainSubstring("no representor found on ECPF candidates")))
			Expect(err).To(MatchError(ContainSubstring("0000:03:00.0,0000:04:00.0")))
		})

		It("returns an error when multiple representors are found", func() {
			mockNH.EXPECT().GetPfRepresentorFromPortParams(gomock.Any()).Return("pf-rep0", nil)
			mockNH.EXPECT().GetPfRepresentorFromPortParams(gomock.Any()).Return("pf-rep1", nil)

			_, err := em.GetRepresentorForPFServiceInterface(&dpuservicev1.PF{
				ID: 1,
				NICSelector: &dpuservicev1.NICSelectorSpec{
					Type: dpuservicev1.NICSelectorTypeDPU,
				},
			})
			Expect(err).To(MatchError(ContainSubstring("multiple representors found")))
			Expect(err).To(MatchError(ContainSubstring("pf-rep0,pf-rep1")))
		})

		It("selects ECPFs matching the PCI domain:bus:device prefix", func() {
			em.ecpfs = []ecpfEntry{
				{address: "0000:03:00.0", isDPU: true},
				{address: "0000:04:00.0", isDPU: true},
			}
			mockNH.EXPECT().
				GetPfRepresentorFromPortParams(gomock.Any()).
				DoAndReturn(func(pp *sriovnet.RepresentorPortParams) (string, error) {
					Expect(pp.ECPF).To(Equal("0000:03:00.0"))
					return "pf-rep-pci", nil
				})

			rep, err := em.GetRepresentorForPFServiceInterface(&dpuservicev1.PF{
				ID: 5,
				NICSelector: &dpuservicev1.NICSelectorSpec{
					Type: dpuservicev1.NICSelectorTypePCI,
					PCI: &dpuservicev1.PCISelector{
						Address: "0000:03:00.2",
					},
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(rep).To(Equal("pf-rep-pci"))
		})
	})

	Describe("GetRepresentorForVF", func() {
		var em *ecpfManager

		BeforeEach(func() {
			em = &ecpfManager{
				networkhelper: mockNH,
				ecpfs: []ecpfEntry{
					{address: "0000:03:00.0", isDPU: true},
				},
			}
		})

		It("returns the VF representor from the matching ECPF", func() {
			mockNH.EXPECT().
				GetVfRepresentorFromPortParams(gomock.Any(), uint32(7)).
				DoAndReturn(func(pp *sriovnet.RepresentorPortParams, vfIndex uint32) (string, error) {
					Expect(pp).To(Equal(&sriovnet.RepresentorPortParams{
						ECPF:             "0000:03:00.0",
						ControllerNumber: 1,
						PFNumber:         2,
					}))
					Expect(vfIndex).To(Equal(uint32(7)))
					return "vf-rep0", nil
				})

			rep, err := em.GetRepresentorForVFServiceInterface(&dpuservicev1.VF{
				VFID: 7,
				PFID: 2,
				NICSelector: &dpuservicev1.NICSelectorSpec{
					Type: dpuservicev1.NICSelectorTypeDPU,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(rep).To(Equal("vf-rep0"))
		})

		It("returns an error when no representor is found", func() {
			mockNH.EXPECT().
				GetVfRepresentorFromPortParams(gomock.Any(), gomock.Any()).
				Return("", errTest)

			_, err := em.GetRepresentorForVFServiceInterface(&dpuservicev1.VF{
				VFID: 1,
				PFID: 1,
				NICSelector: &dpuservicev1.NICSelectorSpec{
					Type: dpuservicev1.NICSelectorTypeDPU,
				},
			})
			Expect(err).To(MatchError(ContainSubstring("no representor found on ECPF candidates")))
		})
	})
})
