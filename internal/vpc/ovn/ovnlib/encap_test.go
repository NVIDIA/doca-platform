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

	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/ovnlib"
	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/sbdb"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Encap test", func() {
	var (
		ctx     context.Context
		chassis *sbdb.Chassis
		encap   *sbdb.Encap
	)

	validateEncapFn := func(actual, expected *sbdb.Encap) {
		ExpectWithOffset(1, actual.ChassisName).To(Equal(expected.ChassisName))
		ExpectWithOffset(1, actual.IP).To(Equal(expected.IP))
		ExpectWithOffset(1, actual.Type).To(Equal(expected.Type))
	}

	BeforeEach(func() {
		ctx = context.Background()
		chassis = &sbdb.Chassis{
			Name: "chassis1",
		}

		encap = &sbdb.Encap{
			ChassisName: chassis.Name,
			IP:          "20.0.0.2",
			Type:        sbdb.EncapTypeGeneve,
		}

		var err error
		chassis, err = ovnSBClient.CreateChassis(ctx, encap, chassis)
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		_ = ovnSBClient.ClearAll(ctx)
	})

	Context("GetEncap", func() {
		It("should get encap by uuid successfully", func() {
			encap, err := ovnSBClient.GetEncap(ctx, &sbdb.EncapGetParams{
				UUID: chassis.Encaps[0],
			})
			Expect(err).ToNot(HaveOccurred())
			validateEncapFn(encap, encap)
		})

		It("should return error when getting encap with non-existent uuid", func() {
			_, err := ovnSBClient.GetEncap(ctx, &sbdb.EncapGetParams{
				UUID: "non-existent-uuid",
			})
			Expect(err).To(HaveOccurred())
			Expect(ovnlib.GetOvnErrorCodeFromError(err)).To(Equal(ovnlib.ErrNotFound))
		})

		It("should return error when getting encap with missing uuid", func() {
			_, err := ovnSBClient.GetEncap(ctx, &sbdb.EncapGetParams{})
			Expect(err).To(HaveOccurred())
			Expect(ovnlib.GetOvnErrorCodeFromError(err)).To(Equal(ovnlib.ErrInvalidArgument))
		})
	})
})
