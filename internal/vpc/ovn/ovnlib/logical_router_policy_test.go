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

var _ = Describe("logical router policy CRUD test", func() {
	var ctx = context.Background()
	const (
		lrName = "lr1"
		match  = "ip4.src == 192.168.100.0/24"
	)

	getAllLRPolicies := func() []*nbdb.LogicalRouterPolicy {
		lrpList := []*nbdb.LogicalRouterPolicy{}
		err := ovnClient.List(ctx, &lrpList)
		Expect(err).ToNot(HaveOccurred())
		return lrpList
	}

	It("create logical router policy", func() {
		lr, err := ovnClient.CreateLogicalRouter(ctx, &nbdb.LogicalRouter{Name: lrName})
		Expect(err).ToNot(HaveOccurred())
		lrGetParams := &nbdb.LogicalRouterGetParams{
			UUID: lr.UUID,
		}

		priority := 100
		action := nbdb.ACLActionDrop
		// invalid arguments, need at least 3 argumetns Priority, Match and Action
		lrpRes, err := ovnClient.CreateLogicalRouterPolicy(ctx, lrGetParams, &nbdb.LogicalRouterPolicy{
			Priority: priority,
		})
		Expect(err).To(HaveOccurred())
		Expect(ovnlib.GetOvnErrorCodeFromError(err)).To(Equal(ovnlib.ErrInvalidArgument))
		Expect(lrpRes).To(BeNil())

		// invalid arguments, need at least 3 argumetns Priority, Match and Action
		lrpRes, err = ovnClient.CreateLogicalRouterPolicy(ctx, lrGetParams, &nbdb.LogicalRouterPolicy{
			Priority: priority,
			Match:    match,
		})
		Expect(err).To(HaveOccurred())
		Expect(ovnlib.GetOvnErrorCodeFromError(err)).To(Equal(ovnlib.ErrInvalidArgument))
		Expect(lrpRes).To(BeNil())

		// logical router not found
		lrpRes, err = ovnClient.CreateLogicalRouterPolicy(ctx, &nbdb.LogicalRouterGetParams{UUID: "not-exist"}, &nbdb.LogicalRouterPolicy{
			Priority: priority,
			Match:    match,
			Action:   action,
		})
		Expect(err).To(HaveOccurred())
		Expect(ovnlib.GetOvnErrorCodeFromError(err)).To(Equal(ovnlib.ErrNotFound))
		Expect(lrpRes).To(BeNil())

		// created successfully
		lrpRes, err = ovnClient.CreateLogicalRouterPolicy(ctx, lrGetParams, &nbdb.LogicalRouterPolicy{
			Priority: priority,
			Match:    match,
			Action:   action,
		})
		Expect(err).ToNot(HaveOccurred())

		// Verify policy was added to router
		lrGet, err := ovnClient.GetLogicalRouter(ctx, lrGetParams)
		Expect(err).ToNot(HaveOccurred())
		Expect(lrGet).NotTo(BeNil())
		Expect(lrGet.Policies).To(HaveLen(1))
		Expect(lrGet.Policies[0]).To(Equal(lrpRes.UUID))

		// Verify route fields are correct
		lrpList := getAllLRPolicies()
		Expect(lrpList).To(HaveLen(1))
		Expect(lrpList[0].Priority).To(Equal(priority))
		Expect(lrpList[0].Match).To(Equal(match))
		Expect(lrpList[0].Action).To(Equal(action))
	})

	It("delete logical router policy", func() {
		_, err := ovnClient.CreateLogicalRouter(ctx, &nbdb.LogicalRouter{
			Name: lrName,
		})
		Expect(err).ToNot(HaveOccurred())

		lrGetParams := &nbdb.LogicalRouterGetParams{Name: lrName}
		_, err = ovnClient.CreateLogicalRouterPolicy(ctx, lrGetParams, &nbdb.LogicalRouterPolicy{
			Priority: 100,
			Match:    match,
			Action:   nbdb.ACLActionDrop,
		})
		Expect(err).ToNot(HaveOccurred())

		// Router not found
		err = ovnClient.DeleteLogicalRouterPolicy(ctx, &nbdb.LogicalRouterGetParams{UUID: "not-exist-router"}, &nbdb.LogicalRouterPolicyDeleteParams{
			Priority: 100,
			Match:    match,
		})
		Expect(err).To(HaveOccurred())
		Expect(ovnlib.GetOvnErrorCodeFromError(err)).To(Equal(ovnlib.ErrNotFound))

		// Router policy not found by UUID
		err = ovnClient.DeleteLogicalRouterPolicy(ctx, lrGetParams, &nbdb.LogicalRouterPolicyDeleteParams{
			UUID: "not-exist",
		})
		Expect(err).To(HaveOccurred())
		Expect(ovnlib.GetOvnErrorCodeFromError(err)).To(Equal(ovnlib.ErrNotFound))

		// Invalid arguments: requires Priority and Match
		err = ovnClient.DeleteLogicalRouterPolicy(ctx, lrGetParams, &nbdb.LogicalRouterPolicyDeleteParams{
			Priority: 100,
		})
		Expect(err).To(HaveOccurred())
		Expect(ovnlib.GetOvnErrorCodeFromError(err)).To(Equal(ovnlib.ErrInvalidArgument))

		// Router policy not found by Priority and Match
		err = ovnClient.DeleteLogicalRouterPolicy(ctx, lrGetParams, &nbdb.LogicalRouterPolicyDeleteParams{
			Priority: 100,
			Match:    "not-exist",
		})
		Expect(err).To(HaveOccurred())
		Expect(ovnlib.GetOvnErrorCodeFromError(err)).To(Equal(ovnlib.ErrNotFound))

		// Deleted successfully
		err = ovnClient.DeleteLogicalRouterPolicy(ctx, lrGetParams, &nbdb.LogicalRouterPolicyDeleteParams{
			Priority: 100,
			Match:    match,
		})
		Expect(err).NotTo(HaveOccurred())

		// Verify policy was removed from router
		lrGet, err := ovnClient.GetLogicalRouter(ctx, lrGetParams)
		Expect(err).ToNot(HaveOccurred())
		Expect(lrGet).NotTo(BeNil())
		Expect(lrGet.Policies).To(BeEmpty())

		lrpList := getAllLRPolicies()
		Expect(lrpList).To(BeEmpty())
	})

	It("get logical router policy", func() {
		lrGetParams := &nbdb.LogicalRouterGetParams{Name: lrName}

		_, err := ovnClient.CreateLogicalRouter(ctx, &nbdb.LogicalRouter{Name: lrName})
		Expect(err).ToNot(HaveOccurred())

		priority := 100
		action := nbdb.ACLActionDrop
		// Not found
		lrpGet, err := ovnClient.GetLogicalRouterPolicy(ctx, &nbdb.LogicalRouterPolicyGetParams{
			Priority: priority,
			Match:    match,
		})
		Expect(err).To(HaveOccurred())
		Expect(ovnlib.GetOvnErrorCodeFromError(err)).To(Equal(ovnlib.ErrNotFound))
		Expect(lrpGet).To(BeNil())

		lrp, err := ovnClient.CreateLogicalRouterPolicy(ctx, lrGetParams, &nbdb.LogicalRouterPolicy{
			Priority: priority,
			Match:    match,
			Action:   action,
		})
		Expect(err).ToNot(HaveOccurred())

		// Invalid arguments: need both Priority and Match
		lrpGet, err = ovnClient.GetLogicalRouterPolicy(ctx, &nbdb.LogicalRouterPolicyGetParams{Priority: 100})
		Expect(err).To(HaveOccurred())
		Expect(ovnlib.GetOvnErrorCodeFromError(err)).To(Equal(ovnlib.ErrInvalidArgument))
		Expect(lrpGet).To(BeNil())

		// Get by UUID
		lrpGet, err = ovnClient.GetLogicalRouterPolicy(ctx, &nbdb.LogicalRouterPolicyGetParams{UUID: lrp.UUID})
		Expect(err).ToNot(HaveOccurred())
		Expect(lrpGet.UUID).To(Equal(lrp.UUID))
		Expect(lrpGet.Priority).To(Equal(lrp.Priority))
		Expect(lrpGet.Match).To(Equal(lrp.Match))
		Expect(lrpGet.Action).To(Equal(lrp.Action))

		// Get by Priority, Match
		lrpGet, err = ovnClient.GetLogicalRouterPolicy(ctx, &nbdb.LogicalRouterPolicyGetParams{
			Priority: priority,
			Match:    match,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(lrpGet.UUID).To(Equal(lrp.UUID))
		Expect(lrpGet.Priority).To(Equal(lrp.Priority))
		Expect(lrpGet.Match).To(Equal(lrp.Match))
		Expect(lrpGet.Action).To(Equal(lrp.Action))
	})

	It("list logical router policy", func() {
		lrGetParams := &nbdb.LogicalRouterGetParams{Name: lrName}

		_, err := ovnClient.CreateLogicalRouter(ctx, &nbdb.LogicalRouter{Name: lrName})
		Expect(err).ToNot(HaveOccurred())

		priority1 := 100
		action1 := nbdb.ACLActionDrop
		// Not found, empty list returned
		res, err := ovnClient.ListLogicalRouterPolicy(ctx, &nbdb.LogicalRouterPolicyListParams{
			Priority: priority1, Match: match, Action: action1})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(BeEmpty())

		lrp1, err := ovnClient.CreateLogicalRouterPolicy(ctx, lrGetParams, &nbdb.LogicalRouterPolicy{
			Priority: priority1,
			Match:    match,
			Action:   action1,
		})
		Expect(err).ToNot(HaveOccurred())

		priority2 := 100
		match2 := "ip4.src == 192.168.200.0/24"
		action2 := nbdb.ACLActionDrop
		lrp2, err := ovnClient.CreateLogicalRouterPolicy(ctx, lrGetParams, &nbdb.LogicalRouterPolicy{
			Priority: priority2,
			Match:    match2,
			Action:   action2,
		})
		Expect(err).ToNot(HaveOccurred())

		// List by Match
		res, err = ovnClient.ListLogicalRouterPolicy(ctx, &nbdb.LogicalRouterPolicyListParams{
			Match: match,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(res).To(HaveLen(1))
		Expect(res[0].UUID).To(Equal(lrp1.UUID))
		Expect(res[0].Priority).To(Equal(lrp1.Priority))
		Expect(res[0].Match).To(Equal(lrp1.Match))
		Expect(res[0].Action).To(Equal(lrp1.Action))

		// List by Priority and Action
		res, err = ovnClient.ListLogicalRouterPolicy(ctx, &nbdb.LogicalRouterPolicyListParams{
			Priority: 100,
			Action:   nbdb.ACLActionDrop,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(res).To(HaveLen(2))

		// List by UUID
		res, err = ovnClient.ListLogicalRouterPolicy(ctx, &nbdb.LogicalRouterPolicyListParams{
			UUID: lrp2.UUID,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(res).To(HaveLen(1))
		Expect(res[0].UUID).To(Equal(lrp2.UUID))
		Expect(res[0].Priority).To(Equal(lrp2.Priority))
		Expect(res[0].Match).To(Equal(lrp2.Match))
		Expect(res[0].Action).To(Equal(lrp2.Action))
	})
})
