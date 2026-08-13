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
	"fmt"
	"strings"
)

const (
	largeResourceFlag         = 0x80
	smallResourceLengthMask   = 0x07
	smallResourceTagShift     = 3
	largeResourceHeaderLength = 3
	fieldHeaderLength         = 3

	// Large resource tags retain the large-resource flag, so VPD-R and VPD-W
	// are represented as 0x90 and 0x91. Small resource tags are shifted right
	// before comparison, so the end tag is represented as 0x0f.
	endTag       = 0x0f
	readOnlyTag  = 0x90
	readWriteTag = 0x91
)

// Data contains fields parsed from PCI Vital Product Data. If a field occurs
// more than once, the first value is retained.
type Data struct {
	fields map[string]string
}

// Parse parses PCI Vital Product Data.
// This implementation is based on cap_vpd() from pciutils.
// Bytes after the end tag are ignored, and a missing end tag is not an error.
// If malformed data is encountered, fields parsed before the error are returned
// together with the error.
func Parse(raw []byte) (Data, error) {
	data := Data{fields: map[string]string{}}
	for offset := 0; offset < len(raw); {
		resource, nextOffset, err := parseResource(raw, offset)
		if err != nil {
			return data, err
		}
		if resource.tag == endTag {
			return data, nil
		}
		if resource.tag == readOnlyTag || resource.tag == readWriteTag {
			if err := data.addFields(resource.data); err != nil {
				return data, err
			}
		}
		offset = nextOffset
	}
	return data, nil
}

// Lookup returns the value of a VPD field.
func (d Data) Lookup(fieldID string) (string, bool) {
	value, ok := d.fields[fieldID]
	return value, ok
}

// resource contains a decoded VPD resource tag and payload.
type resource struct {
	tag  byte
	data []byte
}

// parseResource decodes one VPD resource and returns the next byte offset.
func parseResource(raw []byte, offset int) (resource, int, error) {
	tag := raw[offset]
	headerLength := 1
	resourceLength := int(tag & smallResourceLengthMask)

	if tag&largeResourceFlag > 0 {
		headerLength = largeResourceHeaderLength
		if len(raw)-offset < headerLength {
			return resource{}, 0, fmt.Errorf("truncated large resource header at offset %d", offset)
		}
		resourceLength = int(raw[offset+1]) | (int(raw[offset+2]) << 8)
	} else {
		tag >>= smallResourceTagShift
	}

	resourceStart := offset + headerLength
	resourceEnd := resourceStart + resourceLength
	if resourceEnd > len(raw) {
		return resource{}, 0, fmt.Errorf("resource at offset %d exceeds VPD data", offset)
	}
	return resource{tag: tag, data: raw[resourceStart:resourceEnd]}, resourceEnd, nil
}

// addFields parses keyword fields from a VPD-R or VPD-W resource.
func (d *Data) addFields(raw []byte) error {
	for offset := 0; offset+fieldHeaderLength <= len(raw); {
		fieldID := string(raw[offset : offset+2])
		fieldLength := int(raw[offset+2])
		fieldStart := offset + fieldHeaderLength
		fieldEnd := fieldStart + fieldLength
		if fieldEnd > len(raw) {
			return fmt.Errorf("field %q exceeds VPD resource data", fieldID)
		}
		if _, exists := d.fields[fieldID]; !exists {
			// Some cards NUL terminate or NUL pad VPD strings, see print_vpd_string() in pciutils.
			d.fields[fieldID] = strings.Trim(string(raw[fieldStart:fieldEnd]), "\x00 \t\n\v\f\r")
		}
		offset = fieldEnd
	}
	return nil
}
