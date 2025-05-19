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

package ovnlib_test

import (
	"context"

	"github.com/nvidia/doca-platform/internal/vpc/ovn/nbdb"
	"github.com/nvidia/doca-platform/internal/vpc/ovn/ovnlib"

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
})
