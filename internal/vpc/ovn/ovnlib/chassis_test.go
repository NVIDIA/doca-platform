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
	"fmt"

	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/ovnlib"
	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/sbdb"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("chassis CRUD test", func() {
	var (
		ctx         context.Context
		chassisName string
		encap       *sbdb.Encap
	)

	BeforeEach(func() {
		ctx = context.Background()
		chassisName = "chassis1"
		encap = &sbdb.Encap{
			ChassisName: chassisName,
			IP:          "20.0.0.2",
			Type:        sbdb.EncapTypeGeneve,
		}
	})

	AfterEach(func() {
		_ = ovnSBClient.ClearAll(ctx)
	})

	Context("when creating a chassis", func() {
		It("should create chassis successfully", func() {
			res, err := ovnSBClient.CreateChassis(ctx, encap, &sbdb.Chassis{
				Name: chassisName,
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(res).NotTo(BeNil())
			Expect(res.UUID).NotTo(BeEmpty())
			fmt.Println("chassis created", res)

			chassisList := getAllChassis(ctx)
			Expect(chassisList).To(HaveLen(1))
			Expect(chassisList[0].Name).To(Equal(chassisName))
		})
	})

	Context("when deleting a chassis", func() {
		It("should return error when deleting non-existent chassis by name", func() {
			err := ovnSBClient.DeleteChassis(ctx, &sbdb.ChassisDeleteParams{
				Name: chassisName,
			})
			Expect(err).To(HaveOccurred())
			Expect(ovnlib.GetOvnErrorCodeFromError(err)).To(Equal(ovnlib.ErrNotFound))
		})

		It("should delete chassis by name successfully", func() {
			_, err := ovnSBClient.CreateChassis(ctx, encap, &sbdb.Chassis{
				Name: chassisName,
			})
			Expect(err).ToNot(HaveOccurred())

			err = ovnSBClient.DeleteChassis(ctx, &sbdb.ChassisDeleteParams{
				Name: chassisName,
			})
			Expect(err).ToNot(HaveOccurred())
			chassisList := getAllChassis(ctx)
			Expect(chassisList).To(BeEmpty())
		})

		It("should delete chassis by uuid successfully", func() {
			chassis, err := ovnSBClient.CreateChassis(ctx, encap, &sbdb.Chassis{
				Name: chassisName,
			})
			Expect(err).ToNot(HaveOccurred())

			err = ovnSBClient.DeleteChassis(ctx, &sbdb.ChassisDeleteParams{
				UUID: chassis.UUID,
			})
			Expect(err).ToNot(HaveOccurred())
			chassisList := getAllChassis(ctx)
			Expect(chassisList).To(BeEmpty())
		})
	})

	Context("when getting a chassis", func() {
		It("should return error when getting non-existent chassis", func() {
			_, err := ovnSBClient.GetChassis(ctx, &sbdb.ChassisGetParams{Name: chassisName})
			Expect(err).To(HaveOccurred())
			Expect(ovnlib.GetOvnErrorCodeFromError(err)).To(Equal(ovnlib.ErrNotFound))
		})

		It("should get chassis by name successfully", func() {
			chassis, err := ovnSBClient.CreateChassis(ctx, encap, &sbdb.Chassis{
				Name: chassisName,
			})
			Expect(err).ToNot(HaveOccurred())

			res, err := ovnSBClient.GetChassis(ctx, &sbdb.ChassisGetParams{Name: chassisName})
			Expect(err).ToNot(HaveOccurred())
			Expect(res).NotTo(BeNil())
			Expect(res.Name).To(Equal(chassisName))
			Expect(res.UUID).To(Equal(chassis.UUID))
		})
	})

	Context("when listing chassis", func() {
		It("should list chassis by name successfully", func() {
			chassis1, err := ovnSBClient.CreateChassis(ctx, encap, &sbdb.Chassis{
				Name: chassisName,
			})
			Expect(err).ToNot(HaveOccurred())

			res, err := ovnSBClient.ListChassis(ctx, &sbdb.ChassisListParams{Name: chassisName})
			Expect(err).ToNot(HaveOccurred())
			Expect(res).To(HaveLen(1))
			Expect(res[0].Name).To(Equal(chassisName))
			Expect(res[0].UUID).To(Equal(chassis1.UUID))
		})

		It("should return empty list for non-existent chassis", func() {
			chassisList, err := ovnSBClient.ListChassis(ctx, &sbdb.ChassisListParams{Name: "chassis-not-exist"})
			Expect(err).ToNot(HaveOccurred())
			Expect(chassisList).To(BeEmpty())
		})

		It("should list all chassis successfully", func() {
			_, err := ovnSBClient.CreateChassis(ctx, encap, &sbdb.Chassis{
				Name: "chassis1",
			})
			Expect(err).ToNot(HaveOccurred())

			encap2 := &sbdb.Encap{
				ChassisName: "chassis2",
				IP:          "20.0.0.3",
				Type:        sbdb.EncapTypeGeneve,
			}
			_, err = ovnSBClient.CreateChassis(ctx, encap2, &sbdb.Chassis{
				Name: "chassis2",
			})
			Expect(err).ToNot(HaveOccurred())

			chassisList, err := ovnSBClient.ListChassis(ctx, &sbdb.ChassisListParams{})
			Expect(err).ToNot(HaveOccurred())
			Expect(chassisList).To(HaveLen(2))
		})
	})
})

// getAllChassis is a helper function to get all chassis from the OVN SB database
func getAllChassis(ctx context.Context) []*sbdb.Chassis {
	chassisList := []*sbdb.Chassis{}
	err := ovnSBClient.List(ctx, &chassisList)
	Expect(err).ToNot(HaveOccurred())
	return chassisList
}
