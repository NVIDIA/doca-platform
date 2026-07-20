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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ErrorMessages", func() {
	It("returns an @Message.ExtendedInfo message with MessageId and Resolution (rshim not owned by BMC)", func() {
		body := `{
  "error": {
    "@Message.ExtendedInfo": [
      {
        "Message": "The requested resource of type Targets named '/dev/rshim0/boot' was not found.",
        "MessageId": "Base.1.18.1.ResourceNotFound",
        "Resolution": "Provide a valid resource identifier and resubmit the request."
      }
    ],
    "code": "Base.1.18.1.ResourceNotFound",
    "message": "The requested resource of type Targets named '/dev/rshim0/boot' was not found."
  }
}`
		msgs := ErrorMessages(body)
		Expect(msgs).To(HaveLen(1))
		Expect(msgs[0]).To(ContainSubstring("The requested resource of type Targets named '/dev/rshim0/boot' was not found."))
		Expect(msgs[0]).To(ContainSubstring("(Base.1.18.1.ResourceNotFound)"))
		Expect(msgs[0]).To(ContainSubstring("BMC Resolution: Provide a valid resource identifier and resubmit the request."))
	})

	It("returns every @Message.ExtendedInfo entry in order", func() {
		body := `{
  "error": {
    "@Message.ExtendedInfo": [
      {"Message": "first problem", "MessageId": "Base.1.0.First"},
      {"Message": "second problem", "MessageId": "Base.1.0.Second"}
    ],
    "code": "Base.1.0.GeneralError",
    "message": "A general error occurred."
  }
}`
		msgs := ErrorMessages(body)
		Expect(msgs).To(Equal([]string{
			"first problem (Base.1.0.First)",
			"second problem (Base.1.0.Second)",
		}))
	})

	It("falls back to the top-level error.message when there is no ExtendedInfo", func() {
		msgs := ErrorMessages(`{"error":{"code":"Base.1.0.GeneralError","message":"boom"}}`)
		Expect(msgs).To(Equal([]string{"boom"}))
	})

	It("returns nil for an empty body", func() {
		Expect(ErrorMessages("")).To(BeNil())
	})

	It("returns nil for non-JSON bodies", func() {
		Expect(ErrorMessages("502 Bad Gateway")).To(BeNil())
	})

	It("returns nil for valid JSON without an error payload", func() {
		Expect(ErrorMessages(`{"unexpected":"payload"}`)).To(BeNil())
	})

	It("returns nil for an empty error object", func() {
		Expect(ErrorMessages(`{"error":{}}`)).To(BeNil())
	})
})

var _ = Describe("RespBody", func() {
	It("returns an empty string for a nil response without panicking", func() {
		Expect(RespBody(nil)).To(Equal(""))
	})
})
