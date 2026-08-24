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
	"encoding/json"
	"net/http"
	"net/http/httptest"

	"github.com/go-resty/resty/v2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("DPU ARM reset", func() {
	It("uses the discovered system ID and accepts HTTP 202 Accepted for reset requests", func() {
		resetTypes := []string{}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			switch req.URL.Path {
			case "/redfish/v1/Systems":
				Expect(req.Method).To(Equal(http.MethodGet))
				_, _ = w.Write([]byte(`{"Members":[{"@odata.id":"/redfish/v1/Systems/Bluefield"}]}`))
			case "/redfish/v1/Systems/Bluefield/Actions/ComputerSystem.Reset":
				Expect(req.Method).To(Equal(http.MethodPost))
				var body ResetRequest
				Expect(json.NewDecoder(req.Body).Decode(&body)).To(Succeed())
				resetTypes = append(resetTypes, body.ResetType)
				w.WriteHeader(http.StatusAccepted)
			default:
				http.NotFound(w, req)
			}
		}))
		defer server.Close()

		client := &Client{Client: resty.New().SetBaseURL(server.URL)}

		_, err := client.ForceRestartDPUArm()
		Expect(err).NotTo(HaveOccurred())

		_, err = client.GracefulRestartDPUArm()
		Expect(err).NotTo(HaveOccurred())

		Expect(resetTypes).To(Equal([]string{"ForceRestart", "GracefulRestart"}))
	})

	It("posts Oem Nvidia SOC.ForceReset using the discovered system ID", func() {
		posted := false
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			switch req.URL.Path {
			case "/redfish/v1/Systems":
				Expect(req.Method).To(Equal(http.MethodGet))
				_, _ = w.Write([]byte(`{"Members":[{"@odata.id":"/redfish/v1/Systems/Bluefield"}]}`))
			case "/redfish/v1/Systems/Bluefield/Oem/Nvidia/SOC.ForceReset":
				Expect(req.Method).To(Equal(http.MethodPost))
				posted = true
				w.WriteHeader(http.StatusAccepted)
			default:
				http.NotFound(w, req)
			}
		}))
		defer server.Close()

		client := &Client{Client: resty.New().SetBaseURL(server.URL)}
		_, err := client.ForceResetSOC()
		Expect(err).NotTo(HaveOccurred())
		Expect(posted).To(BeTrue())
	})
})
