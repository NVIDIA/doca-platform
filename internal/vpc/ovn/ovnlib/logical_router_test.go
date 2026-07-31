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

var _ = Describe("logical router CRUD test", func() {
	var ctx = context.Background()

	lrName := "lr1"

	getAllLRs := func() []*nbdb.LogicalRouter {
		lrList := []*nbdb.LogicalRouter{}
		err := ovnClient.List(ctx, &lrList)
		Expect(err).ToNot(HaveOccurred())
		return lrList
	}

	It("create logical router", func() {
		res, err := ovnClient.CreateLogicalRouter(ctx, &nbdb.LogicalRouter{
			Name: lrName,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(res).NotTo(BeNil())
		Expect(res.UUID).NotTo(BeEmpty())

		lrList := getAllLRs()
		Expect(lrList).To(HaveLen(1))
		Expect(lrList[0].Name).To(Equal(lrName))
	})

	It("delete logical router by name", func() {
		//not found
		err := ovnClient.DeleteLogicalRouter(ctx, &nbdb.LogicalRouterDeleteParams{
			Name: lrName,
		})
		Expect(err).To(HaveOccurred())
		Expect(ovnlib.GetOvnErrorCodeFromError(err)).To(Equal(ovnlib.ErrNotFound))

		_, err = ovnClient.CreateLogicalRouter(ctx, &nbdb.LogicalRouter{
			Name: lrName,
		})
		Expect(err).ToNot(HaveOccurred())

		err = ovnClient.DeleteLogicalRouter(ctx, &nbdb.LogicalRouterDeleteParams{
			Name: lrName,
		})
		Expect(err).ToNot(HaveOccurred())
		lrList := getAllLRs()
		Expect(lrList).To(BeEmpty())
	})

	It("delete logical router by uuid", func() {
		lr, err := ovnClient.CreateLogicalRouter(ctx, &nbdb.LogicalRouter{
			Name: lrName,
		})
		Expect(err).ToNot(HaveOccurred())

		err = ovnClient.DeleteLogicalRouter(ctx, &nbdb.LogicalRouterDeleteParams{
			UUID: lr.UUID,
		})
		Expect(err).ToNot(HaveOccurred())
		lrList := getAllLRs()
		Expect(lrList).To(BeEmpty())
	})

	It("get logical router by name or id", func() {
		_, err := ovnClient.GetLogicalRouter(ctx, &nbdb.LogicalRouterGetParams{Name: lrName})
		Expect(err).To(HaveOccurred())
		Expect(ovnlib.GetOvnErrorCodeFromError(err)).To(Equal(ovnlib.ErrNotFound))

		lr, err := ovnClient.CreateLogicalRouter(ctx, &nbdb.LogicalRouter{
			Name: lrName,
		})
		Expect(err).ToNot(HaveOccurred())

		res, err := ovnClient.GetLogicalRouter(ctx, &nbdb.LogicalRouterGetParams{Name: lrName})
		Expect(err).ToNot(HaveOccurred())
		Expect(res).NotTo(BeNil())
		Expect(res.Name).To(Equal(lrName))
		Expect(res.UUID).To(Equal(lr.UUID))
	})

	It("list logical router", func() {
		lr1, err := ovnClient.CreateLogicalRouter(ctx, &nbdb.LogicalRouter{
			Name: lrName,
		})
		Expect(err).ToNot(HaveOccurred())

		lr2ExternalIDs := map[string]string{"lr2_key": "lr2_val"}
		lr2, err := ovnClient.CreateLogicalRouter(ctx, &nbdb.LogicalRouter{
			Name:        "lr2",
			ExternalIDs: lr2ExternalIDs,
		})
		Expect(err).ToNot(HaveOccurred())

		//List by Name
		res, err := ovnClient.ListLogicalRouter(ctx, &nbdb.LogicalRouterListParams{Name: lrName})
		Expect(err).ToNot(HaveOccurred())
		Expect(res).To(HaveLen(1))
		Expect(res[0].Name).To(Equal(lrName))
		Expect(res[0].UUID).To(Equal(lr1.UUID))

		//List by ExternalIDs
		res, err = ovnClient.ListLogicalRouter(ctx, &nbdb.LogicalRouterListParams{ExternalIDs: lr2ExternalIDs})
		Expect(err).ToNot(HaveOccurred())
		Expect(res).To(HaveLen(1))
		Expect(res[0].Name).To(Equal(lr2.Name))
		Expect(res[0].UUID).To(Equal(lr2.UUID))

		//Does not exist, should expect empty list
		lrList, err := ovnClient.ListLogicalRouter(ctx, &nbdb.LogicalRouterListParams{Name: "lr-not-exist"})
		Expect(err).ToNot(HaveOccurred())
		Expect(lrList).To(BeEmpty())
	})

	It("update logical router options", func() {
		const nullEntry = "null"

		By("create logical router")
		lr, err := ovnClient.CreateLogicalRouter(ctx, &nbdb.LogicalRouter{
			Name: lrName,
		})
		Expect(err).ToNot(HaveOccurred())

		By("set initial options")
		options := make(map[string]string)
		options["external_ip"] = "192.168.1.1"
		updatedLR, err := ovnClient.UpdateLogicalRouterOptions(ctx, &nbdb.LogicalRouterGetParams{UUID: lr.UUID}, options)
		Expect(err).ToNot(HaveOccurred())
		Expect(updatedLR.Options).To(HaveLen(1))
		Expect(updatedLR.Options["external_ip"]).To(Equal("192.168.1.1"))

		By("update existing option key")
		options = make(map[string]string)
		options["external_ip"] = "192.168.1.2"
		updatedLR, err = ovnClient.UpdateLogicalRouterOptions(ctx, &nbdb.LogicalRouterGetParams{UUID: lr.UUID}, options)
		Expect(err).ToNot(HaveOccurred())
		Expect(updatedLR.Options).To(HaveLen(1))
		Expect(updatedLR.Options["external_ip"]).To(Equal("192.168.1.2"))

		By("update existing option key with same value")
		updatedLR, err = ovnClient.UpdateLogicalRouterOptions(ctx, &nbdb.LogicalRouterGetParams{UUID: lr.UUID}, options)
		Expect(err).ToNot(HaveOccurred())
		Expect(updatedLR.Options).To(HaveLen(1))
		Expect(updatedLR.Options["external_ip"]).To(Equal("192.168.1.2"))

		By("adding new option key")
		options = make(map[string]string)
		options["logical_ip"] = "10.100.10.2/24"
		updatedLR, err = ovnClient.UpdateLogicalRouterOptions(ctx, &nbdb.LogicalRouterGetParams{UUID: lr.UUID}, options)
		Expect(err).ToNot(HaveOccurred())
		Expect(updatedLR.Options).To(HaveLen(2))
		Expect(updatedLR.Options["external_ip"]).To(Equal("192.168.1.2"))
		Expect(updatedLR.Options["logical_ip"]).To(Equal("10.100.10.2/24"))

		By("update options, only one option is changed")
		options = make(map[string]string)
		options["external_ip"] = "192.168.1.3"
		options["logical_ip"] = "10.100.10.2/24"
		updatedLR, err = ovnClient.UpdateLogicalRouterOptions(ctx, &nbdb.LogicalRouterGetParams{UUID: lr.UUID}, options)
		Expect(err).ToNot(HaveOccurred())
		Expect(updatedLR.Options).To(HaveLen(2))
		Expect(updatedLR.Options["external_ip"]).To(Equal("192.168.1.3"))
		Expect(updatedLR.Options["logical_ip"]).To(Equal("10.100.10.2/24"))

		By("deleting non-existing option key")
		options = make(map[string]string)
		options["foo"] = nullEntry
		updatedLR, err = ovnClient.UpdateLogicalRouterOptions(ctx, &nbdb.LogicalRouterGetParams{UUID: lr.UUID}, options)
		Expect(err).ToNot(HaveOccurred())
		Expect(updatedLR.Options).To(HaveLen(2))
		Expect(updatedLR.Options["external_ip"]).To(Equal("192.168.1.3"))
		Expect(updatedLR.Options["logical_ip"]).To(Equal("10.100.10.2/24"))

		By("deleting option key")
		options = make(map[string]string)
		options["logical_ip"] = nullEntry
		updatedLR, err = ovnClient.UpdateLogicalRouterOptions(ctx, &nbdb.LogicalRouterGetParams{UUID: lr.UUID}, options)
		Expect(err).ToNot(HaveOccurred())
		Expect(updatedLR.Options).To(HaveLen(1))
		Expect(updatedLR.Options["external_ip"]).To(Equal("192.168.1.3"))

		By("deleting remaining option keys")
		options = make(map[string]string)
		options["external_ip"] = nullEntry
		updatedLR, err = ovnClient.UpdateLogicalRouterOptions(ctx, &nbdb.LogicalRouterGetParams{UUID: lr.UUID}, options)
		Expect(err).ToNot(HaveOccurred())
		Expect(updatedLR.Options).To(BeEmpty())

		By("deleting non-existing option key, no options")
		options = make(map[string]string)
		options["foo"] = nullEntry
		updatedLR, err = ovnClient.UpdateLogicalRouterOptions(ctx, &nbdb.LogicalRouterGetParams{UUID: lr.UUID}, options)
		Expect(err).ToNot(HaveOccurred())
		Expect(updatedLR.Options).To(BeEmpty())
	})
})
