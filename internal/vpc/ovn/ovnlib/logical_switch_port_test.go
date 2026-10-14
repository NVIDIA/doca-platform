/*
SPDX-FileCopyrightText: Copyright (c) 2025 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
SPDX-License-Identifier: LicenseRef-NvidiaProprietary

NVIDIA CORPORATION, its affiliates and licensors retain all intellectual
property and proprietary rights in and to this material, related
documentation and any modifications thereto. Any use, reproduction,
disclosure or distribution of this material and related documentation
 without an express license agreement from NVIDIA CORPORATION or
its affiliates is strictly prohibited.
*/

package ovnlib_test

import (
	"context"

	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/nbdb"
	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/ovnlib"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("logical router port CRUD test", func() {
	var ctx = context.Background()
	lsName := "ls1"
	lspName := "ls1p"

	getAllLSPorts := func() []*nbdb.LogicalSwitchPort {
		lspList := []*nbdb.LogicalSwitchPort{}
		err := ovnClient.List(ctx, &lspList)
		Expect(err).ToNot(HaveOccurred())
		return lspList
	}

	It("create logical switch port", func() {
		_, err := ovnClient.CreateLogicalSwitch(ctx, &nbdb.LogicalSwitch{Name: lsName})
		Expect(err).ToNot(HaveOccurred())

		lsGetParams := &nbdb.LogicalSwitchGetParams{
			Name: lsName,
		}
		lsp, err := ovnClient.CreateLogicalSwitchPort(ctx, lsGetParams, &nbdb.LogicalSwitchPort{
			Name: lspName,
		})
		Expect(err).ToNot(HaveOccurred())

		// Verify port was created
		lspList := getAllLSPorts()
		Expect(lspList).To(HaveLen(1))
		Expect(lspList[0].Name).To(Equal(lsp.Name))
		Expect(lspList[0].UUID).To(Equal(lsp.UUID))

		// Verify port was attached to the logical switch

		lsList, err := ovnClient.ListLogicalSwitch(ctx, &nbdb.LogicalSwitchListParams{Name: lsName})
		Expect(err).ToNot(HaveOccurred())
		Expect(lsList).To(HaveLen(1))
		Expect(lsList[0].Ports).To(HaveLen(1))
		Expect(lsList[0].Ports[0]).To(Equal(lsp.UUID))
	})

	It("delete logical switch port", func() {
		_, err := ovnClient.CreateLogicalSwitch(ctx, &nbdb.LogicalSwitch{Name: lsName})
		Expect(err).ToNot(HaveOccurred())

		lsGetParams := &nbdb.LogicalSwitchGetParams{
			Name: lsName,
		}
		lsp, err := ovnClient.CreateLogicalSwitchPort(ctx, lsGetParams, &nbdb.LogicalSwitchPort{
			Name: lspName,
		})
		Expect(err).ToNot(HaveOccurred())

		// Switch does not exist
		err = ovnClient.DeleteLogicalSwitchPort(ctx, &nbdb.LogicalSwitchGetParams{Name: "not-exist"}, &nbdb.LogicalSwitchPortDeleteParams{
			UUID: lsp.UUID,
		})
		Expect(err).To(HaveOccurred())
		Expect(ovnlib.GetOvnErrorCodeFromError(err)).To(Equal(ovnlib.ErrNotFound))

		// Port does not exist
		err = ovnClient.DeleteLogicalSwitchPort(ctx, lsGetParams, &nbdb.LogicalSwitchPortDeleteParams{
			UUID: "not-exist",
		})
		Expect(err).To(HaveOccurred())
		Expect(ovnlib.GetOvnErrorCodeFromError(err)).To(Equal(ovnlib.ErrNotFound))

		// Verify port still exists
		lspList := getAllLSPorts()
		Expect(lspList).NotTo(BeEmpty())

		// Deleted succefully
		err = ovnClient.DeleteLogicalSwitchPort(ctx, lsGetParams, &nbdb.LogicalSwitchPortDeleteParams{
			UUID: lsp.UUID,
		})
		Expect(err).ToNot(HaveOccurred())
		lspList = getAllLSPorts()
		Expect(lspList).To(BeEmpty())
	})

	It("get logical switch port", func() {
		lsGetParams := &nbdb.LogicalSwitchGetParams{Name: lsName}

		_, err := ovnClient.CreateLogicalSwitch(ctx, &nbdb.LogicalSwitch{Name: lsName})
		Expect(err).ToNot(HaveOccurred())

		// Not found
		_, err = ovnClient.GetLogicalSwitchPort(ctx, &nbdb.LogicalSwitchPortGetParams{Name: lspName})
		Expect(err).To(HaveOccurred())
		Expect(ovnlib.GetOvnErrorCodeFromError(err)).To(Equal(ovnlib.ErrNotFound))

		lsp, err := ovnClient.CreateLogicalSwitchPort(ctx, lsGetParams, &nbdb.LogicalSwitchPort{
			Name: lspName,
		})
		Expect(err).ToNot(HaveOccurred())

		// Get by UUID
		res, err := ovnClient.GetLogicalSwitchPort(ctx, &nbdb.LogicalSwitchPortGetParams{UUID: lsp.UUID})
		Expect(err).ToNot(HaveOccurred())
		Expect(res.UUID).To(Equal(lsp.UUID))
		Expect(res.Name).To(Equal(lsp.Name))

		// Get by Name
		res, err = ovnClient.GetLogicalSwitchPort(ctx, &nbdb.LogicalSwitchPortGetParams{Name: lspName})
		Expect(err).ToNot(HaveOccurred())
		Expect(res.UUID).To(Equal(lsp.UUID))
		Expect(res.Name).To(Equal(lsp.Name))
	})

	It("list logical router port", func() {
		lsGetParams := &nbdb.LogicalSwitchGetParams{Name: lsName}

		_, err := ovnClient.CreateLogicalSwitch(ctx, &nbdb.LogicalSwitch{Name: lsName})
		Expect(err).ToNot(HaveOccurred())

		// Not found
		res, err := ovnClient.ListLogicalSwitchPort(ctx, &nbdb.LogicalSwitchPortListParams{Name: lspName})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(BeEmpty())

		lsp1, err := ovnClient.CreateLogicalSwitchPort(ctx, lsGetParams, &nbdb.LogicalSwitchPort{
			Name:      lspName,
			Addresses: []string{"192.168.1.10", "2001:db8::10"},
		})
		Expect(err).ToNot(HaveOccurred())

		lsp2, err := ovnClient.CreateLogicalSwitchPort(ctx, lsGetParams, &nbdb.LogicalSwitchPort{
			Name:      "lsp2",
			Addresses: []string{"192.168.1.11", "2001:db8::11"},
		})
		Expect(err).ToNot(HaveOccurred())

		// List by one of Addresses
		res, err = ovnClient.ListLogicalSwitchPort(ctx, &nbdb.LogicalSwitchPortListParams{
			Addresses: []string{"192.168.1.10"},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(res).To(HaveLen(1))
		Expect(res[0].UUID).To(Equal(lsp1.UUID))
		Expect(res[0].Name).To(Equal(lsp1.Name))

		// List by Name
		res, err = ovnClient.ListLogicalSwitchPort(ctx, &nbdb.LogicalSwitchPortListParams{
			Name: lsp2.Name,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(res).To(HaveLen(1))
		Expect(res[0].UUID).To(Equal(lsp2.UUID))
		Expect(res[0].Name).To(Equal(lsp2.Name))
	})
})
