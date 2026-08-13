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

package vpd

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func createVPDResource(tag byte, fields ...string) []byte {
	resourceData := []byte{}
	for i := 0; i < len(fields); i += 2 {
		resourceData = append(resourceData, fields[i]...)
		resourceData = append(resourceData, byte(len(fields[i+1])))
		resourceData = append(resourceData, fields[i+1]...)
	}
	vpdData := []byte{tag, byte(len(resourceData)), byte(len(resourceData) >> 8)}
	return append(vpdData, resourceData...)
}

var _ = Describe("VPD", func() {
	It("parses and looks up fields", func() {
		data, err := Parse(createVPDResource(readOnlyTag, "PN", "test-part", "VU", "test-function-vuid "))
		Expect(err).NotTo(HaveOccurred())

		value, found := data.Lookup("VU")
		Expect(found).To(BeTrue())
		Expect(value).To(Equal("test-function-vuid"))
	})

	It("trims NUL padding from field values", func() {
		data, err := Parse(createVPDResource(readOnlyTag, "VU", "test-function-vuid \x00"))
		Expect(err).NotTo(HaveOccurred())

		value, found := data.Lookup("VU")
		Expect(found).To(BeTrue())
		Expect(value).To(Equal("test-function-vuid"))
	})

	It("reports a missing field", func() {
		data, err := Parse(createVPDResource(readOnlyTag, "PN", "test-part"))
		Expect(err).NotTo(HaveOccurred())

		_, found := data.Lookup("VU")
		Expect(found).To(BeFalse())
	})

	It("rejects a resource that exceeds the data", func() {
		// Declares a 65535-byte resource payload but provides none.
		_, err := Parse([]byte{readOnlyTag, 0xff, 0xff})
		Expect(err).To(MatchError(ContainSubstring("exceeds VPD data")))
	})

	It("rejects a truncated large resource header", func() {
		_, err := Parse([]byte{readOnlyTag, 0x04})
		Expect(err).To(MatchError(ContainSubstring("truncated large resource header")))
	})

	It("rejects a field that exceeds its resource", func() {
		// Declares a two-byte VU value but provides only one byte.
		_, err := Parse([]byte{readOnlyTag, 0x04, 0x00, 'V', 'U', 0x02, 'A'})
		Expect(err).To(MatchError(ContainSubstring("exceeds VPD resource data")))
	})

	It("returns fields parsed before an error", func() {
		// Contains a valid VU field followed by a PN field whose declared
		// two-byte value contains only one byte.
		resourceData := []byte{'V', 'U', 0x04, 'v', 'u', 'i', 'd', 'P', 'N', 0x02, 'A'}
		raw := append([]byte{readOnlyTag, byte(len(resourceData)), 0x00}, resourceData...)

		data, err := Parse(raw)
		Expect(err).To(MatchError(ContainSubstring("exceeds VPD resource data")))
		value, found := data.Lookup("VU")
		Expect(found).To(BeTrue())
		Expect(value).To(Equal("vuid"))
	})

	It("stops at the end tag and ignores trailing data", func() {
		raw := append(createVPDResource(readOnlyTag, "VU", "test-function-vuid"), byte(endTag<<smallResourceTagShift))
		// Append a malformed resource after the end tag to verify it is ignored.
		raw = append(raw, readOnlyTag, 0xff, 0xff)

		data, err := Parse(raw)
		Expect(err).NotTo(HaveOccurred())
		value, found := data.Lookup("VU")
		Expect(found).To(BeTrue())
		Expect(value).To(Equal("test-function-vuid"))
	})

	It("skips identifier and small resources", func() {
		// Add a four-byte identifier resource followed by an unrelated
		// one-byte small resource; neither should be parsed as VPD fields.
		raw := []byte{0x82, 0x04, 0x00, 't', 'e', 's', 't'}
		raw = append(raw, byte(0x01<<smallResourceTagShift|0x01), 0xff)
		raw = append(raw, createVPDResource(readWriteTag, "VU", "test-function-vuid")...)

		data, err := Parse(raw)
		Expect(err).NotTo(HaveOccurred())
		value, found := data.Lookup("VU")
		Expect(found).To(BeTrue())
		Expect(value).To(Equal("test-function-vuid"))
	})

	It("keeps the first value for duplicate fields", func() {
		raw := createVPDResource(readOnlyTag, "VU", "read-only-vuid")
		raw = append(raw, createVPDResource(readWriteTag, "VU", "read-write-vuid")...)

		data, err := Parse(raw)
		Expect(err).NotTo(HaveOccurred())
		value, found := data.Lookup("VU")
		Expect(found).To(BeTrue())
		Expect(value).To(Equal("read-only-vuid"))
	})
})
