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

var _ = Describe("logical switch CRUD test", func() {
	var ctx = context.Background()

	lsName := "ls1"

	getAllLSs := func() []*nbdb.LogicalSwitch {
		lsList := []*nbdb.LogicalSwitch{}
		err := ovnClient.List(ctx, &lsList)
		Expect(err).ToNot(HaveOccurred())
		return lsList
	}

	It("create logical switch", func() {
		res, err := ovnClient.CreateLogicalSwitch(ctx, &nbdb.LogicalSwitch{
			Name: lsName,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(res).NotTo(BeNil())
		Expect(res.UUID).NotTo(BeEmpty())

		lsList := getAllLSs()
		Expect(lsList).To(HaveLen(1))
		Expect(lsList[0].Name).To(Equal(lsName))
	})

	It("delete logical switch by name", func() {
		//not found
		err := ovnClient.DeleteLogicalSwitch(ctx, &nbdb.LogicalSwitchDeleteParams{
			Name: lsName,
		})
		Expect(err).To(HaveOccurred())
		Expect(ovnlib.GetOvnErrorCodeFromError(err)).To(Equal(ovnlib.ErrNotFound))

		_, err = ovnClient.CreateLogicalSwitch(ctx, &nbdb.LogicalSwitch{
			Name: lsName,
		})
		Expect(err).ToNot(HaveOccurred())

		err = ovnClient.DeleteLogicalSwitch(ctx, &nbdb.LogicalSwitchDeleteParams{
			Name: lsName,
		})
		Expect(err).ToNot(HaveOccurred())
		lsList := getAllLSs()
		Expect(lsList).To(BeEmpty())
	})

	It("delete logical switch by uuid", func() {
		ls, err := ovnClient.CreateLogicalSwitch(ctx, &nbdb.LogicalSwitch{
			Name: lsName,
		})
		Expect(err).ToNot(HaveOccurred())

		err = ovnClient.DeleteLogicalSwitch(ctx, &nbdb.LogicalSwitchDeleteParams{
			UUID: ls.UUID,
		})
		Expect(err).ToNot(HaveOccurred())
		lsList := getAllLSs()
		Expect(lsList).To(BeEmpty())
	})

	It("get logical switch by name or id", func() {
		_, err := ovnClient.GetLogicalSwitch(ctx, &nbdb.LogicalSwitchGetParams{Name: lsName})
		Expect(err).To(HaveOccurred())
		Expect(ovnlib.GetOvnErrorCodeFromError(err)).To(Equal(ovnlib.ErrNotFound))

		ls, err := ovnClient.CreateLogicalSwitch(ctx, &nbdb.LogicalSwitch{
			Name: lsName,
		})
		Expect(err).ToNot(HaveOccurred())

		res, err := ovnClient.GetLogicalSwitch(ctx, &nbdb.LogicalSwitchGetParams{Name: lsName})
		Expect(err).ToNot(HaveOccurred())
		Expect(res).NotTo(BeNil())
		Expect(res.Name).To(Equal(lsName))
		Expect(res.UUID).To(Equal(ls.UUID))
	})

	It("list logical switch", func() {
		ls1, err := ovnClient.CreateLogicalSwitch(ctx, &nbdb.LogicalSwitch{
			Name: lsName,
		})
		Expect(err).ToNot(HaveOccurred())

		ls2OtherConfig := map[string]string{"ls2_key": "ls2_val"}
		ls2ExternalIDs := map[string]string{"id2_key": "id2_val"}
		ls2, err := ovnClient.CreateLogicalSwitch(ctx, &nbdb.LogicalSwitch{
			Name:        "ls2",
			OtherConfig: ls2OtherConfig,
			ExternalIDs: ls2ExternalIDs,
		})
		Expect(err).ToNot(HaveOccurred())

		//List by Name
		res, err := ovnClient.ListLogicalSwitch(ctx, &nbdb.LogicalSwitchListParams{Name: lsName})
		Expect(err).ToNot(HaveOccurred())
		Expect(res).To(HaveLen(1))
		Expect(res[0].Name).To(Equal(lsName))
		Expect(res[0].UUID).To(Equal(ls1.UUID))

		//List by OtherConfig
		res, err = ovnClient.ListLogicalSwitch(ctx, &nbdb.LogicalSwitchListParams{OtherConfig: ls2OtherConfig})
		Expect(err).ToNot(HaveOccurred())
		Expect(res).To(HaveLen(1))
		Expect(res[0].Name).To(Equal(ls2.Name))
		Expect(res[0].UUID).To(Equal(ls2.UUID))

		//List by ExternalIDs
		res, err = ovnClient.ListLogicalSwitch(ctx, &nbdb.LogicalSwitchListParams{ExternalIDs: ls2ExternalIDs})
		Expect(err).ToNot(HaveOccurred())
		Expect(res).To(HaveLen(1))
		Expect(res[0].Name).To(Equal(ls2.Name))
		Expect(res[0].UUID).To(Equal(ls2.UUID))

		//Does not exist, should expect empty list
		lsList, err := ovnClient.ListLogicalSwitch(ctx, &nbdb.LogicalSwitchListParams{Name: "ls-not-exist"})
		Expect(err).ToNot(HaveOccurred())
		Expect(lsList).To(BeEmpty())
	})
})
