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

var _ = Describe("logical router static route CRUD test", func() {
	var ctx = context.Background()
	lrName := "lr1"

	getAllLRStaticRoutes := func() []*nbdb.LogicalRouterStaticRoute {
		lrsrList := []*nbdb.LogicalRouterStaticRoute{}
		err := ovnClient.List(ctx, &lrsrList)
		Expect(err).ToNot(HaveOccurred())
		return lrsrList
	}

	It("create logical router static route", func() {
		lr, err := ovnClient.CreateLogicalRouter(ctx, &nbdb.LogicalRouter{Name: lrName})
		Expect(err).ToNot(HaveOccurred())
		lrGetParams := &nbdb.LogicalRouterGetParams{
			UUID: lr.UUID,
		}

		// invalid arguments
		ipPrefix := "100.64.0.0/16"
		nextHopIPAddr := "100.64.0.2"
		lrsrRes, err := ovnClient.CreateLogicalRouterStaticRoute(ctx, lrGetParams, &nbdb.LogicalRouterStaticRoute{
			IPPrefix: ipPrefix,
		})
		Expect(err).To(HaveOccurred())
		Expect(ovnlib.GetOvnErrorCodeFromError(err)).To(Equal(ovnlib.ErrInvalidArgument))
		Expect(lrsrRes).To(BeNil())

		// logical router not found
		lrsrRes, err = ovnClient.CreateLogicalRouterStaticRoute(ctx, &nbdb.LogicalRouterGetParams{UUID: "not-exist"}, &nbdb.LogicalRouterStaticRoute{
			IPPrefix: ipPrefix,
			Nexthop:  nextHopIPAddr,
		})
		Expect(err).To(HaveOccurred())
		Expect(ovnlib.GetOvnErrorCodeFromError(err)).To(Equal(ovnlib.ErrNotFound))
		Expect(lrsrRes).To(BeNil())

		// created successfully
		lrsrRes, err = ovnClient.CreateLogicalRouterStaticRoute(ctx, lrGetParams, &nbdb.LogicalRouterStaticRoute{
			IPPrefix: ipPrefix,
			Nexthop:  nextHopIPAddr,
		})
		Expect(err).ToNot(HaveOccurred())

		// Verify route was added to router
		lrGet, err := ovnClient.GetLogicalRouter(ctx, lrGetParams)
		Expect(err).ToNot(HaveOccurred())
		Expect(lrGet).NotTo(BeNil())
		Expect(lrGet.StaticRoutes).To(HaveLen(1))
		Expect(lrGet.StaticRoutes[0]).To(Equal(lrsrRes.UUID))

		// Verify route fields are correct
		lrsrList := getAllLRStaticRoutes()
		Expect(lrsrList).To(HaveLen(1))
		Expect(lrsrList[0].IPPrefix).To(Equal(ipPrefix))
		Expect(lrsrList[0].Nexthop).To(Equal(nextHopIPAddr))
	})

	It("delete logical router static route", func() {
		_, err := ovnClient.CreateLogicalRouter(ctx, &nbdb.LogicalRouter{
			Name: lrName,
		})
		Expect(err).ToNot(HaveOccurred())

		lrGetParams := &nbdb.LogicalRouterGetParams{Name: lrName}
		lrsr, err := ovnClient.CreateLogicalRouterStaticRoute(ctx, lrGetParams, &nbdb.LogicalRouterStaticRoute{
			IPPrefix: "10.52.0.0/16",
			Nexthop:  "10.52.0.2",
		})
		Expect(err).ToNot(HaveOccurred())

		// Router not found
		err = ovnClient.DeleteLogicalRouterStaticRoute(ctx, &nbdb.LogicalRouterGetParams{UUID: "not-exist-router"}, &nbdb.LogicalRouterStaticRouteDeleteParams{
			UUID: lrsr.UUID,
		})
		Expect(err).To(HaveOccurred())
		Expect(ovnlib.GetOvnErrorCodeFromError(err)).To(Equal(ovnlib.ErrNotFound))

		// Router static route not found by UUID
		err = ovnClient.DeleteLogicalRouterStaticRoute(ctx, lrGetParams, &nbdb.LogicalRouterStaticRouteDeleteParams{
			UUID: "not-exist-port",
		})
		Expect(err).To(HaveOccurred())
		Expect(ovnlib.GetOvnErrorCodeFromError(err)).To(Equal(ovnlib.ErrNotFound))

		// Router static route not found by IPPrefix and Nexthop
		err = ovnClient.DeleteLogicalRouterStaticRoute(ctx, lrGetParams, &nbdb.LogicalRouterStaticRouteDeleteParams{
			IPPrefix: "10.52.0.0/16",
			Nexthop:  "10.52.0.3",
		})
		Expect(err).To(HaveOccurred())
		Expect(ovnlib.GetOvnErrorCodeFromError(err)).To(Equal(ovnlib.ErrNotFound))

		// Router static route invalid arguments, need both IPPrefix and Nexthop to find a route
		err = ovnClient.DeleteLogicalRouterStaticRoute(ctx, lrGetParams, &nbdb.LogicalRouterStaticRouteDeleteParams{
			IPPrefix: "10.52.0.0/16",
		})
		Expect(err).To(HaveOccurred())
		Expect(ovnlib.GetOvnErrorCodeFromError(err)).To(Equal(ovnlib.ErrInvalidArgument))

		// Deleted successfully
		err = ovnClient.DeleteLogicalRouterStaticRoute(ctx, lrGetParams, &nbdb.LogicalRouterStaticRouteDeleteParams{
			IPPrefix: "10.52.0.0/16",
			Nexthop:  "10.52.0.2",
		})
		Expect(err).ToNot(HaveOccurred())

		// Verify static route was removed from router
		lrGet, err := ovnClient.GetLogicalRouter(ctx, lrGetParams)
		Expect(err).ToNot(HaveOccurred())
		Expect(lrGet).NotTo(BeNil())
		Expect(lrGet.StaticRoutes).To(BeEmpty())

		lrpList := getAllLRStaticRoutes()
		Expect(lrpList).To(BeEmpty())
	})

	It("get logical router static route", func() {
		lrGetParams := &nbdb.LogicalRouterGetParams{Name: lrName}

		_, err := ovnClient.CreateLogicalRouter(ctx, &nbdb.LogicalRouter{Name: lrName})
		Expect(err).ToNot(HaveOccurred())

		ipPrefix := "10.52.0.0/15"
		nextHop := "10.52.0.2"
		// Not found
		lrsrGet, err := ovnClient.GetLogicalRouterStaticRoute(ctx, &nbdb.LogicalRouterStaticRouteGetParams{
			IPPrefix: ipPrefix,
			Nexthop:  nextHop,
		})
		Expect(err).To(HaveOccurred())
		Expect(ovnlib.GetOvnErrorCodeFromError(err)).To(Equal(ovnlib.ErrNotFound))
		Expect(lrsrGet).To(BeNil())

		lrsr, err := ovnClient.CreateLogicalRouterStaticRoute(ctx, lrGetParams, &nbdb.LogicalRouterStaticRoute{
			IPPrefix: ipPrefix,
			Nexthop:  nextHop,
		})
		Expect(err).ToNot(HaveOccurred())

		// Get by UUID
		lrsrGet, err = ovnClient.GetLogicalRouterStaticRoute(ctx, &nbdb.LogicalRouterStaticRouteGetParams{UUID: lrsr.UUID})
		Expect(err).ToNot(HaveOccurred())
		Expect(lrsrGet.UUID).To(Equal(lrsr.UUID))
		Expect(lrsrGet.IPPrefix).To(Equal(lrsr.IPPrefix))
		Expect(lrsrGet.Nexthop).To(Equal(lrsr.Nexthop))
	})

	It("list logical router static routes", func() {
		lrGetParams := &nbdb.LogicalRouterGetParams{Name: lrName}

		_, err := ovnClient.CreateLogicalRouter(ctx, &nbdb.LogicalRouter{Name: lrName})
		Expect(err).ToNot(HaveOccurred())

		ipPrefix1 := "10.52.0.0/16"
		nextHop1 := "10.52.0.2"
		// Not found
		res, err := ovnClient.ListLogicalRouterStaticRoute(ctx, &nbdb.LogicalRouterStaticRouteListParams{
			IPPrefix: ipPrefix1, Nexthop: nextHop1})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(BeEmpty())

		lrsr1, err := ovnClient.CreateLogicalRouterStaticRoute(ctx, lrGetParams, &nbdb.LogicalRouterStaticRoute{
			IPPrefix: ipPrefix1,
			Nexthop:  nextHop1,
		})
		Expect(err).ToNot(HaveOccurred())

		ipPrefix2 := "10.53.0.0/16"
		nextHop2 := "10.53.0.2"
		lrsr2, err := ovnClient.CreateLogicalRouterStaticRoute(ctx, lrGetParams, &nbdb.LogicalRouterStaticRoute{
			IPPrefix: ipPrefix2,
			Nexthop:  nextHop2,
		})
		Expect(err).ToNot(HaveOccurred())

		// List by IPPrefix
		res, err = ovnClient.ListLogicalRouterStaticRoute(ctx, &nbdb.LogicalRouterStaticRouteListParams{
			IPPrefix: ipPrefix1,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(res).To(HaveLen(1))
		Expect(res[0].UUID).To(Equal(lrsr1.UUID))
		Expect(res[0].IPPrefix).To(Equal(lrsr1.IPPrefix))
		Expect(res[0].Nexthop).To(Equal(lrsr1.Nexthop))

		// List by Nexthop
		res, err = ovnClient.ListLogicalRouterStaticRoute(ctx, &nbdb.LogicalRouterStaticRouteListParams{
			Nexthop: nextHop2,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(res).To(HaveLen(1))
		Expect(res[0].UUID).To(Equal(lrsr2.UUID))
		Expect(res[0].IPPrefix).To(Equal(lrsr2.IPPrefix))
		Expect(res[0].Nexthop).To(Equal(lrsr2.Nexthop))

		// List by UUID
		res, err = ovnClient.ListLogicalRouterStaticRoute(ctx, &nbdb.LogicalRouterStaticRouteListParams{
			UUID: lrsr2.UUID,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(res).To(HaveLen(1))
		Expect(res[0].UUID).To(Equal(lrsr2.UUID))
		Expect(res[0].IPPrefix).To(Equal(lrsr2.IPPrefix))
		Expect(res[0].Nexthop).To(Equal(lrsr2.Nexthop))
	})
})
