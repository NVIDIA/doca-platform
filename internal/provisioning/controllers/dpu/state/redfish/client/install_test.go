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

var _ = Describe("BFB install over HTTPS", func() {
	var (
		server   *httptest.Server
		lastBody map[string]interface{}
		client   *Client
	)

	BeforeEach(func() {
		lastBody = nil
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			Expect(req.Method).To(Equal(http.MethodPost))
			Expect(req.URL.Path).To(Equal("/" + APIInstallBFB))
			Expect(json.NewDecoder(req.Body).Decode(&lastBody)).To(Succeed())
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"Id":"task-1"}`))
		}))
		client = &Client{Client: resty.New().SetBaseURL(server.URL)}
	})

	AfterEach(func() {
		server.Close()
	})

	const imageURI = "10.0.110.1:30443/bfb/??x.bfb,bfcfg/y.cfg?/bfb-to-install"

	It("submits the BFB install with TransferProtocol HTTPS", func() {
		resp, task, err := client.InstallBFB(imageURI)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode()).To(Equal(http.StatusAccepted))
		Expect(task.ID).To(Equal("task-1"))

		Expect(lastBody).To(HaveKeyWithValue("TransferProtocol", "HTTPS"))
		Expect(lastBody).To(HaveKeyWithValue("ImageURI", imageURI))
		Expect(lastBody["Targets"]).To(ConsistOf("redfish/v1/UpdateService/FirmwareInventory/DPU_OS"))
	})

	It("submits the BlueField ARM image with TransferProtocol HTTPS", func() {
		_, _, err := client.InstallBluefieldArmImage(imageURI)
		Expect(err).NotTo(HaveOccurred())
		Expect(lastBody).To(HaveKeyWithValue("TransferProtocol", "HTTPS"))
		Expect(lastBody).To(HaveKeyWithValue("ImageURI", imageURI))
		Expect(lastBody["Targets"]).To(ConsistOf("redfish/v1/UpdateService/FirmwareInventory/BlueField_OS_Image_CPU_0"))
	})

	It("submits the BlueField ARM config with TransferProtocol HTTPS", func() {
		_, _, err := client.InstallBluefieldArmConfig(imageURI)
		Expect(err).NotTo(HaveOccurred())
		Expect(lastBody).To(HaveKeyWithValue("TransferProtocol", "HTTPS"))
		Expect(lastBody).To(HaveKeyWithValue("ImageURI", imageURI))
		Expect(lastBody["Targets"]).To(ConsistOf("redfish/v1/UpdateService/FirmwareInventory/BlueField_OS_Config_CPU_0"))
	})
})
