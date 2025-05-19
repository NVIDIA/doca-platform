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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("dhcp options CRUD test", func() {
	var ctx = context.Background()

	getAllDhcpOptions := func() []*nbdb.DHCPOptions {
		dhcpOptionsList := []*nbdb.DHCPOptions{}
		err := ovnClient.List(ctx, &dhcpOptionsList)
		Expect(err).ToNot(HaveOccurred())
		return dhcpOptionsList
	}
	cidr := "10.152.0.0/16"

	It("create dhcp options", func() {
		dhcpOptions := map[string]string{
			"mtu":        "9000",
			"lease_time": "3600",
			"router":     "10.152.0.1",
			"server_id":  "10.152.0.1",
			"server_mac": "02:AB:CD:EF:12:34",
		}
		res, err := ovnClient.CreateDhcpOptions(ctx, &nbdb.DHCPOptions{
			Cidr:    cidr,
			Options: dhcpOptions,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(res).ToNot(BeNil())
		Expect(res.UUID).NotTo(BeEmpty())

		dhcpOptionsList := getAllDhcpOptions()
		Expect(dhcpOptionsList).To(HaveLen(1))
		Expect(dhcpOptionsList[0].Cidr).To(Equal(cidr))
		Expect(dhcpOptionsList[0].Options).To(Equal(dhcpOptions))
	})

	It("delete dhcp options", func() {
		_, err := ovnClient.CreateDhcpOptions(ctx, &nbdb.DHCPOptions{
			Cidr: cidr,
		})
		Expect(err).ToNot(HaveOccurred())

		err = ovnClient.DeleteDhcpOptions(ctx, &nbdb.DHCPOptionsDeleteParams{
			Cidr: cidr,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(getAllDhcpOptions()).To(BeEmpty())
	})

	It("get dhcp options", func() {
		dhcpOptions, err := ovnClient.CreateDhcpOptions(ctx, &nbdb.DHCPOptions{
			Cidr: cidr,
		})
		Expect(err).ToNot(HaveOccurred())

		// Does not exist
		res, err := ovnClient.GetDhcpOptions(ctx, &nbdb.DHCPOptionsGetParams{
			Cidr: "not exist",
		})
		Expect(err).To(HaveOccurred())
		Expect(res).To(BeNil())

		// Get by Cidr
		res, err = ovnClient.GetDhcpOptions(ctx, &nbdb.DHCPOptionsGetParams{
			Cidr: cidr,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(res).ToNot(BeNil())
		Expect(res.Cidr).To(Equal(cidr))
		Expect(res.UUID).To(Equal(dhcpOptions.UUID))

		// Get by UUID
		res, err = ovnClient.GetDhcpOptions(ctx, &nbdb.DHCPOptionsGetParams{
			UUID: dhcpOptions.UUID,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(res).ToNot(BeNil())
		Expect(res.Cidr).To(Equal(cidr))
		Expect(res.UUID).To(Equal(dhcpOptions.UUID))
	})

	It("list dhcp options", func() {
		options1 := map[string]string{
			"mtu":        "9000",
			"lease_time": "3600",
			"router":     "10.152.0.1",
			"server_id":  "10.152.0.1",
			"server_mac": "02:AB:CD:EF:12:34",
		}
		dhcpOptions1, err := ovnClient.CreateDhcpOptions(ctx, &nbdb.DHCPOptions{
			Cidr:    cidr,
			Options: options1,
		})
		Expect(err).ToNot(HaveOccurred())

		options2 := map[string]string{
			"mtu":        "9000",
			"lease_time": "3600",
			"router":     "10.153.0.1",
			"server_id":  "10.153.0.1",
			"server_mac": "02:AB:CD:EF:12:35",
		}
		dhcpOptions2, err := ovnClient.CreateDhcpOptions(ctx, &nbdb.DHCPOptions{
			Cidr:    "10.153.0.0/16",
			Options: options2,
		})
		Expect(err).ToNot(HaveOccurred())

		// Empty list
		res, err := ovnClient.ListDhcpOptions(ctx, &nbdb.DHCPOptionsListParams{
			Options: map[string]string{"not_exist_key": "not_exist_value"},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(res).To(BeEmpty())

		// List by Cidr, expect first dhcp
		res, err = ovnClient.ListDhcpOptions(ctx, &nbdb.DHCPOptionsListParams{
			Cidr: cidr,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(res).To(HaveLen(1))
		Expect(res[0].UUID).To(Equal(dhcpOptions1.UUID))

		// List by partial options, expects both dhcp options with the same mtu value
		res, err = ovnClient.ListDhcpOptions(ctx, &nbdb.DHCPOptionsListParams{
			Options: map[string]string{"mtu": "9000"},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(res).To(HaveLen(2))

		// List by an option that is only exists in the second dhcp option object
		res, err = ovnClient.ListDhcpOptions(ctx, &nbdb.DHCPOptionsListParams{
			Options: map[string]string{"router": "10.153.0.1"},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(res).To(HaveLen(1))
		Expect(res[0].UUID).To(Equal(dhcpOptions2.UUID))
	})
})
