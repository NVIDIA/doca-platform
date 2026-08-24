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
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-resty/resty/v2"
	. "github.com/onsi/gomega"
)

const osImageVersion = `Ubuntu-Server 24.04.4 LTS "Noble Numbat" - Release arm64 (20260210)`

// missingMemberBody mirrors what a BlueField BMC answers for a firmware inventory member it has not
// published. The body is valid JSON, so it decodes into VersionInfo without error and only the
// status code distinguishes it from a member that is present.
const missingMemberBody = `{
  "error": {
    "code": "Base.1.18.1.ResourceMissingAtURI",
    "message": "The resource at the URI was not found."
  }
}`

func firmwareInventoryServer(t *testing.T, status int, body string) (*httptest.Server, *Client) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	return server, &Client{Client: resty.New().SetBaseURL(server.URL)}
}

func TestCheckOSImage(t *testing.T) {
	g := NewWithT(t)
	server, client := firmwareInventoryServer(t, http.StatusOK, fmt.Sprintf(`{"Version":%q}`, osImageVersion))
	defer server.Close()

	info, err := client.CheckOSImage()
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(info.Version).To(Equal(osImageVersion))
}

func TestCheckOSImageReturnsErrorOnMissingMember(t *testing.T) {
	g := NewWithT(t)
	server, client := firmwareInventoryServer(t, http.StatusNotFound, missingMemberBody)
	defer server.Close()

	info, err := client.CheckOSImage()
	g.Expect(err).To(HaveOccurred())
	g.Expect(info).To(BeNil())
	g.Expect(err.Error()).To(ContainSubstring(APICheckOSImage))
	g.Expect(err.Error()).To(ContainSubstring("status=404"))
	g.Expect(err.Error()).To(ContainSubstring("ResourceMissingAtURI"))
}

func TestCheckOSImageReturnsErrorOnUndecodableBody(t *testing.T) {
	g := NewWithT(t)
	server, client := firmwareInventoryServer(t, http.StatusOK, "<html>gateway error</html>")
	defer server.Close()

	info, err := client.CheckOSImage()
	g.Expect(err).To(HaveOccurred())
	g.Expect(info).To(BeNil())
	g.Expect(err.Error()).To(ContainSubstring(APICheckOSImage))
}

func TestCheckConfigImage(t *testing.T) {
	g := NewWithT(t)
	server, client := firmwareInventoryServer(t, http.StatusOK, `{"Version":"seed.iso"}`)
	defer server.Close()

	info, err := client.CheckConfigImage()
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(info.Version).To(Equal("seed.iso"))
}

func TestCheckConfigImageReturnsErrorOnMissingMember(t *testing.T) {
	g := NewWithT(t)
	server, client := firmwareInventoryServer(t, http.StatusNotFound, missingMemberBody)
	defer server.Close()

	info, err := client.CheckConfigImage()
	g.Expect(err).To(HaveOccurred())
	g.Expect(info).To(BeNil())
	g.Expect(err.Error()).To(ContainSubstring(APICheckConfigImage))
	g.Expect(err.Error()).To(ContainSubstring("status=404"))
}
