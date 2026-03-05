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
	const (
		lrName  = "lr1"
		lrpName = "lr1p"
		macAddr = "02:AB:CD:EF:12:34"
		network = "100.64.0.1/16"
	)
	getAllLRPorts := func() []*nbdb.LogicalRouterPort {
		lrpList := []*nbdb.LogicalRouterPort{}
		err := ovnClient.List(ctx, &lrpList)
		Expect(err).ToNot(HaveOccurred())
		return lrpList
	}

	It("create logical router port", func() {
		lrGetParams := &nbdb.LogicalRouterGetParams{
			Name: lrName,
		}
		_, err := ovnClient.CreateLogicalRouter(ctx, &nbdb.LogicalRouter{Name: lrName})
		Expect(err).ToNot(HaveOccurred())

		lrpRes, err := ovnClient.CreateLogicalRouterPort(ctx, lrGetParams, &nbdb.LogicalRouterPort{
			Name:     lrpName,
			MAC:      macAddr,
			Networks: []string{network},
		})
		Expect(err).ToNot(HaveOccurred())

		// Verify port was added to router
		lrGet, err := ovnClient.GetLogicalRouter(ctx, lrGetParams)
		Expect(err).ToNot(HaveOccurred())
		Expect(lrGet).NotTo(BeNil())
		Expect(lrGet.Ports).To(HaveLen(1))
		Expect(lrGet.Ports[0]).To(Equal(lrpRes.UUID))

		// Verify port fields are correct
		lrpList := getAllLRPorts()
		Expect(lrpList).To(HaveLen(1))
		Expect(lrpList[0].Name).To(Equal(lrpName))
		Expect(lrpList[0].MAC).To(Equal(macAddr))
		Expect(lrpList[0].Networks).To(HaveLen(1))
		Expect(lrpList[0].Networks[0]).To(Equal(network))
	})

	It("delete logical router port", func() {
		_, err := ovnClient.CreateLogicalRouter(ctx, &nbdb.LogicalRouter{
			Name: lrName,
		})
		Expect(err).ToNot(HaveOccurred())

		lrGetParams := &nbdb.LogicalRouterGetParams{Name: lrName}
		lrp, err := ovnClient.CreateLogicalRouterPort(ctx, lrGetParams, &nbdb.LogicalRouterPort{
			Name:     lrpName,
			MAC:      macAddr,
			Networks: []string{network},
		})
		Expect(err).ToNot(HaveOccurred())

		// Router not found
		err = ovnClient.DeleteLogicalRouterPort(ctx, &nbdb.LogicalRouterGetParams{Name: "not-exist-router"}, &nbdb.LogicalRouterPortDeleteParams{
			Name: lrpName,
		})
		Expect(err).To(HaveOccurred())
		Expect(ovnlib.GetOvnErrorCodeFromError(err)).To(Equal(ovnlib.ErrNotFound))

		// Router port not found
		err = ovnClient.DeleteLogicalRouterPort(ctx, lrGetParams, &nbdb.LogicalRouterPortDeleteParams{
			Name: "not-exist-port",
		})
		Expect(err).To(HaveOccurred())
		Expect(ovnlib.GetOvnErrorCodeFromError(err)).To(Equal(ovnlib.ErrNotFound))

		// Deleted successfully
		err = ovnClient.DeleteLogicalRouterPort(ctx, lrGetParams, &nbdb.LogicalRouterPortDeleteParams{
			UUID: lrp.UUID,
		})
		Expect(err).NotTo(HaveOccurred())

		// Verify port was removed from router
		lrGet, err := ovnClient.GetLogicalRouter(ctx, lrGetParams)
		Expect(err).ToNot(HaveOccurred())
		Expect(lrGet).NotTo(BeNil())
		Expect(lrGet.Ports).To(BeEmpty())

		lrpList := getAllLRPorts()
		Expect(lrpList).To(BeEmpty())
	})

	It("get logical router port", func() {
		lrGetParams := &nbdb.LogicalRouterGetParams{Name: lrName}

		_, err := ovnClient.CreateLogicalRouter(ctx, &nbdb.LogicalRouter{Name: lrName})
		Expect(err).ToNot(HaveOccurred())

		// Not found
		_, err = ovnClient.GetLogicalRouterPort(ctx, &nbdb.LogicalRouterPortGetParams{Name: lrpName})
		Expect(err).To(HaveOccurred())
		Expect(ovnlib.GetOvnErrorCodeFromError(err)).To(Equal(ovnlib.ErrNotFound))

		lrp, err := ovnClient.CreateLogicalRouterPort(ctx, lrGetParams, &nbdb.LogicalRouterPort{
			Name:     lrpName,
			MAC:      macAddr,
			Networks: []string{network},
		})
		Expect(err).ToNot(HaveOccurred())

		// Get by UUID
		res, err := ovnClient.GetLogicalRouterPort(ctx, &nbdb.LogicalRouterPortGetParams{UUID: lrp.UUID})
		Expect(err).ToNot(HaveOccurred())
		Expect(res.UUID).To(Equal(lrp.UUID))
		Expect(res.Name).To(Equal(lrp.Name))
	})

	It("list logical router port", func() {
		lrGetParams := &nbdb.LogicalRouterGetParams{Name: lrName}

		_, err := ovnClient.CreateLogicalRouter(ctx, &nbdb.LogicalRouter{Name: lrName})
		Expect(err).ToNot(HaveOccurred())

		// Not found
		res, err := ovnClient.ListLogicalRouterPort(ctx, &nbdb.LogicalRouterPortListParams{Name: lrpName})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(BeEmpty())

		lrp1, err := ovnClient.CreateLogicalRouterPort(ctx, lrGetParams, &nbdb.LogicalRouterPort{
			Name:     lrpName,
			MAC:      macAddr,
			Networks: []string{network},
		})
		Expect(err).ToNot(HaveOccurred())

		macAddr2 := "02:AB:CD:EF:12:35"
		network2 := "100.65.0.1/16"
		lrp2, err := ovnClient.CreateLogicalRouterPort(ctx, lrGetParams, &nbdb.LogicalRouterPort{
			Name:     "lrp2",
			MAC:      macAddr2,
			Networks: []string{network2},
		})
		Expect(err).ToNot(HaveOccurred())

		// List by MAC
		res, err = ovnClient.ListLogicalRouterPort(ctx, &nbdb.LogicalRouterPortListParams{
			MAC: macAddr,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(res).To(HaveLen(1))
		Expect(res[0].UUID).To(Equal(lrp1.UUID))
		Expect(res[0].Name).To(Equal(lrp1.Name))

		// List by Networks
		res, err = ovnClient.ListLogicalRouterPort(ctx, &nbdb.LogicalRouterPortListParams{
			Networks: []string{"100.65.0.1/16"},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(res).To(HaveLen(1))
		Expect(res[0].UUID).To(Equal(lrp2.UUID))
		Expect(res[0].Name).To(Equal(lrp2.Name))
	})
})
