/*
COPYRIGHT 2025 NVIDIA

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

package ovnlib_test

import (
	"context"

	"github.com/nvidia/doca-platform/internal/vpc/ovn/nbdb"
	"github.com/nvidia/doca-platform/internal/vpc/ovn/ovnlib"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("logical router nat CRUD test", func() {
	var ctx = context.Background()
	const (
		lrName     = "lr1"
		logicalIP  = "192.168.100.0/24"
		externalIP = "172.18.1.2"
	)

	getAllNATs := func() []*nbdb.NAT {
		natList := []*nbdb.NAT{}
		err := ovnClient.List(ctx, &natList)
		Expect(err).ToNot(HaveOccurred())
		return natList
	}

	It("create logical router nat", func() {
		lr, err := ovnClient.CreateLogicalRouter(ctx, &nbdb.LogicalRouter{Name: lrName})
		Expect(err).ToNot(HaveOccurred())
		lrGetParams := &nbdb.LogicalRouterGetParams{
			UUID: lr.UUID,
		}

		natType := nbdb.NATTypeSNAT
		// invalid arguments, need at least 3 argumetns Priority, Match and Action
		natRes, err := ovnClient.CreateLogicalRouterNat(ctx, lrGetParams, &nbdb.NAT{
			Type: natType,
		})
		Expect(err).To(HaveOccurred())
		Expect(ovnlib.GetOvnErrorCodeFromError(err)).To(Equal(ovnlib.ErrInvalidArgument))
		Expect(natRes).To(BeNil())

		// invalid arguments, need at least 3 argumetns
		natRes, err = ovnClient.CreateLogicalRouterNat(ctx, lrGetParams, &nbdb.NAT{
			Type:       natType,
			ExternalIP: externalIP,
		})
		Expect(err).To(HaveOccurred())
		Expect(ovnlib.GetOvnErrorCodeFromError(err)).To(Equal(ovnlib.ErrInvalidArgument))
		Expect(natRes).To(BeNil())

		// logical router not found
		natRes, err = ovnClient.CreateLogicalRouterNat(ctx, &nbdb.LogicalRouterGetParams{UUID: "not-exist"}, &nbdb.NAT{
			Type:       natType,
			ExternalIP: externalIP,
			LogicalIP:  logicalIP,
		})
		Expect(err).To(HaveOccurred())
		Expect(ovnlib.GetOvnErrorCodeFromError(err)).To(Equal(ovnlib.ErrNotFound))
		Expect(natRes).To(BeNil())

		// created successfully
		natRes, err = ovnClient.CreateLogicalRouterNat(ctx, lrGetParams, &nbdb.NAT{
			Type:       natType,
			ExternalIP: externalIP,
			LogicalIP:  logicalIP,
		})
		Expect(err).ToNot(HaveOccurred())

		// Verify nat was added to router
		lrGet, err := ovnClient.GetLogicalRouter(ctx, lrGetParams)
		Expect(err).ToNot(HaveOccurred())
		Expect(lrGet).NotTo(BeNil())
		Expect(lrGet.Nat).To(HaveLen(1))
		Expect(lrGet.Nat[0]).To(Equal(natRes.UUID))

		// Verify route fields are correct
		natList := getAllNATs()
		Expect(natList).To(HaveLen(1))
		Expect(natList[0].Type).To(Equal(natType))
		Expect(natList[0].ExternalIP).To(Equal(externalIP))
		Expect(natList[0].LogicalIP).To(Equal(logicalIP))
	})

	It("delete logical router nat", func() {
		_, err := ovnClient.CreateLogicalRouter(ctx, &nbdb.LogicalRouter{
			Name: lrName,
		})
		Expect(err).ToNot(HaveOccurred())

		lrGetParams := &nbdb.LogicalRouterGetParams{Name: lrName}
		natType := nbdb.NATTypeSNAT
		_, err = ovnClient.CreateLogicalRouterNat(ctx, lrGetParams, &nbdb.NAT{
			Type:       natType,
			ExternalIP: externalIP,
			LogicalIP:  logicalIP,
		})
		Expect(err).ToNot(HaveOccurred())

		// Router not found
		err = ovnClient.DeleteLogicalRouterNat(ctx, &nbdb.LogicalRouterGetParams{UUID: "not-exist-router"}, &nbdb.NatDeleteParams{
			Type:      natType,
			LogicalIP: logicalIP,
		})
		Expect(err).To(HaveOccurred())
		Expect(ovnlib.GetOvnErrorCodeFromError(err)).To(Equal(ovnlib.ErrNotFound))

		// Nat not found by UUID
		err = ovnClient.DeleteLogicalRouterNat(ctx, lrGetParams, &nbdb.NatDeleteParams{
			UUID: "not-exist",
		})
		Expect(err).To(HaveOccurred())
		Expect(ovnlib.GetOvnErrorCodeFromError(err)).To(Equal(ovnlib.ErrNotFound))

		// Invalid arguments: requires at least Type and LogicalIP
		err = ovnClient.DeleteLogicalRouterNat(ctx, lrGetParams, &nbdb.NatDeleteParams{
			Type: natType,
		})
		Expect(err).To(HaveOccurred())
		Expect(ovnlib.GetOvnErrorCodeFromError(err)).To(Equal(ovnlib.ErrInvalidArgument))

		// Router nat not found
		err = ovnClient.DeleteLogicalRouterNat(ctx, lrGetParams, &nbdb.NatDeleteParams{
			Type:      nbdb.ACLActionAllow,
			LogicalIP: logicalIP,
		})
		Expect(err).To(HaveOccurred())
		Expect(ovnlib.GetOvnErrorCodeFromError(err)).To(Equal(ovnlib.ErrNotFound))

		// Deleted successfully
		err = ovnClient.DeleteLogicalRouterNat(ctx, lrGetParams, &nbdb.NatDeleteParams{
			Type:      natType,
			LogicalIP: logicalIP,
		})
		Expect(err).NotTo(HaveOccurred())

		// Verify nat was removed from router
		lrGet, err := ovnClient.GetLogicalRouter(ctx, lrGetParams)
		Expect(err).ToNot(HaveOccurred())
		Expect(lrGet).NotTo(BeNil())
		Expect(lrGet.Nat).To(BeEmpty())

		natList := getAllNATs()
		Expect(natList).To(BeEmpty())
	})

	It("get logical router nat", func() {
		lrGetParams := &nbdb.LogicalRouterGetParams{Name: lrName}

		_, err := ovnClient.CreateLogicalRouter(ctx, &nbdb.LogicalRouter{Name: lrName})
		Expect(err).ToNot(HaveOccurred())

		natType := nbdb.NATTypeSNAT
		// Not found
		natGet, err := ovnClient.GetLogicalRouterNat(ctx, &nbdb.NatGetParams{
			Type:      natType,
			LogicalIP: logicalIP,
		})
		Expect(err).To(HaveOccurred())
		Expect(ovnlib.GetOvnErrorCodeFromError(err)).To(Equal(ovnlib.ErrNotFound))
		Expect(natGet).To(BeNil())

		nat, err := ovnClient.CreateLogicalRouterNat(ctx, lrGetParams, &nbdb.NAT{
			Type:       natType,
			ExternalIP: externalIP,
			LogicalIP:  logicalIP,
		})
		Expect(err).ToNot(HaveOccurred())

		// Invalid arguments: need both Type and LogicalIP
		natGet, err = ovnClient.GetLogicalRouterNat(ctx, &nbdb.NatGetParams{LogicalIP: logicalIP})
		Expect(err).To(HaveOccurred())
		Expect(ovnlib.GetOvnErrorCodeFromError(err)).To(Equal(ovnlib.ErrInvalidArgument))
		Expect(natGet).To(BeNil())

		// Get by UUID
		natGet, err = ovnClient.GetLogicalRouterNat(ctx, &nbdb.NatGetParams{UUID: nat.UUID})
		Expect(err).ToNot(HaveOccurred())
		Expect(natGet.UUID).To(Equal(nat.UUID))
		Expect(natGet.Type).To(Equal(nat.Type))
		Expect(natGet.ExternalIP).To(Equal(nat.ExternalIP))
		Expect(natGet.LogicalIP).To(Equal(nat.LogicalIP))

		// Get by Type and LogicalIP
		natGet, err = ovnClient.GetLogicalRouterNat(ctx, &nbdb.NatGetParams{
			Type:      natType,
			LogicalIP: logicalIP,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(natGet.UUID).To(Equal(nat.UUID))
		Expect(natGet.Type).To(Equal(nat.Type))
		Expect(natGet.ExternalIP).To(Equal(nat.ExternalIP))
		Expect(natGet.LogicalIP).To(Equal(nat.LogicalIP))
	})

	It("list logical router nat", func() {
		lrGetParams := &nbdb.LogicalRouterGetParams{Name: lrName}

		_, err := ovnClient.CreateLogicalRouter(ctx, &nbdb.LogicalRouter{Name: lrName})
		Expect(err).ToNot(HaveOccurred())

		natType1 := nbdb.NATTypeSNAT
		// Not found, empty list returned
		res, err := ovnClient.ListLogicalRouterNat(ctx, &nbdb.NatListParams{
			Type: natType1, ExternalIP: externalIP, LogicalIP: logicalIP})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(BeEmpty())

		nat1, err := ovnClient.CreateLogicalRouterNat(ctx, lrGetParams, &nbdb.NAT{
			Type:       natType1,
			ExternalIP: externalIP,
			LogicalIP:  logicalIP,
		})
		Expect(err).ToNot(HaveOccurred())

		natType2 := nbdb.NATTypeSNAT
		externalIP2 := "172.18.1.3"
		logicalIP2 := "192.168.200.0/24"
		nat2, err := ovnClient.CreateLogicalRouterNat(ctx, lrGetParams, &nbdb.NAT{
			Type:       natType2,
			ExternalIP: externalIP2,
			LogicalIP:  logicalIP2,
		})
		Expect(err).ToNot(HaveOccurred())

		// List by LogicalIP
		res, err = ovnClient.ListLogicalRouterNat(ctx, &nbdb.NatListParams{
			LogicalIP: logicalIP,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(res).To(HaveLen(1))
		Expect(res[0].UUID).To(Equal(nat1.UUID))
		Expect(res[0].Type).To(Equal(nat1.Type))
		Expect(res[0].ExternalIP).To(Equal(nat1.ExternalIP))
		Expect(res[0].LogicalIP).To(Equal(nat1.LogicalIP))

		// List by NAT type
		res, err = ovnClient.ListLogicalRouterNat(ctx, &nbdb.NatListParams{
			Type: nbdb.NATTypeSNAT,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(res).To(HaveLen(2))

		// List by UUID
		res, err = ovnClient.ListLogicalRouterNat(ctx, &nbdb.NatListParams{
			UUID: nat2.UUID,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(res).To(HaveLen(1))
		Expect(res[0].UUID).To(Equal(nat2.UUID))
		Expect(res[0].Type).To(Equal(nat2.Type))
		Expect(res[0].ExternalIP).To(Equal(nat2.ExternalIP))
		Expect(res[0].LogicalIP).To(Equal(nat2.LogicalIP))
	})
})
