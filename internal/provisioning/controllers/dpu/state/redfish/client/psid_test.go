/*
Copyright 2026 NVIDIA

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

package client

import (
	"context"
	"net/http"
	"net/http/httptest"

	"github.com/go-resty/resty/v2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("GetPSID", func() {
	newBF4Client := func(systemBody, chassisBody string) (*Client, *int, func()) {
		chassisReads := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			switch req.URL.Path {
			case "/" + RedfishAPIs.Path("APIGetSystems"):
				_, _ = w.Write([]byte(`{"Members":[{"@odata.id":"/redfish/v1/Systems/Bluefield"}]}`))
			case "/redfish/v1/Systems/Bluefield":
				_, _ = w.Write([]byte(systemBody))
			case "/redfish/v1/Chassis/BlueField_0":
				chassisReads++
				_, _ = w.Write([]byte(chassisBody))
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		return &Client{
			Client: resty.New().SetBaseURL(server.URL),
			IsBF4:  true,
		}, &chassisReads, server.Close
	}

	It("returns the System AssetTag when it is set", func() {
		client, chassisReads, closeServer := newBF4Client(`{"AssetTag":"MT_0000001234"}`, `{"AssetTag":"MT_9999999999"}`)
		defer closeServer()

		psid, err := client.GetPSID(context.Background())
		Expect(err).NotTo(HaveOccurred())
		Expect(psid).To(Equal("MT_0000001234"))
		Expect(*chassisReads).To(BeZero())
	})

	It("falls back to the Chassis AssetTag when the System AssetTag is empty", func() {
		client, chassisReads, closeServer := newBF4Client(`{"AssetTag":""}`, `{"AssetTag":"MT_0000001234"}`)
		defer closeServer()

		psid, err := client.GetPSID(context.Background())
		Expect(err).NotTo(HaveOccurred())
		Expect(psid).To(Equal("MT_0000001234"))
		Expect(*chassisReads).To(Equal(1))
	})

	It("falls back to the Chassis AssetTag when the System AssetTag is missing", func() {
		client, _, closeServer := newBF4Client(`{}`, `{"AssetTag":"MT_0000001234"}`)
		defer closeServer()

		psid, err := client.GetPSID(context.Background())
		Expect(err).NotTo(HaveOccurred())
		Expect(psid).To(Equal("MT_0000001234"))
	})

	It("falls back to the Chassis AssetTag when the System AssetTag is unavailable", func() {
		client, chassisReads, closeServer := newBF4Client(`{"AssetTag":"N/A"}`, `{"AssetTag":"MT_0000001234"}`)
		defer closeServer()

		psid, err := client.GetPSID(context.Background())
		Expect(err).NotTo(HaveOccurred())
		Expect(psid).To(Equal("MT_0000001234"))
		Expect(*chassisReads).To(Equal(1))
	})

	DescribeTable("returns an error when no set AssetTag is available",
		func(systemBody, chassisBody string) {
			client, _, closeServer := newBF4Client(systemBody, chassisBody)
			defer closeServer()

			psid, err := client.GetPSID(context.Background())
			Expect(err).To(MatchError(ErrAssetTagUnavailable))
			Expect(psid).To(BeEmpty())
		},
		Entry("empty System and empty Chassis AssetTag", `{"AssetTag":""}`, `{"AssetTag":""}`),
		Entry("empty System and missing Chassis AssetTag", `{"AssetTag":""}`, `{}`),
		Entry("empty System and unavailable Chassis AssetTag", `{"AssetTag":""}`, `{"AssetTag":"N/A"}`),
		Entry("missing System and unavailable Chassis AssetTag", `{}`, `{"AssetTag":"N/A"}`),
		Entry("unavailable System and unavailable Chassis AssetTag", `{"AssetTag":"N/A"}`, `{"AssetTag":"N/A"}`),
	)
})
