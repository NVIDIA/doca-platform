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

package util

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"hash/crc32"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	. "github.com/onsi/gomega"
)

func TestParseBFBVersions(t *testing.T) {
	g := NewGomegaWithT(t)
	expected := &provisioningv1.BFBVersions{
		ATF:  "v2.2(release):4.9.1-21-gfc25b08d9",
		BSP:  "4.9.1.12345",
		DOCA: "2.9.1",
		UEFI: "4.9.1-36-g0c3239837a",
	}

	mockFile, err := os.CreateTemp("", "test-bfb-*.bfb")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = mockFile.Close() }()
	defer func() { _ = os.Remove(mockFile.Name()) }()
	var buf bytes.Buffer
	uefiImage := createTestImage(5, createUEFIContent(g, expected.UEFI, "BId12345"))
	if err := writeImage(&buf, uefiImage); err != nil {
		t.Fatal(err)
	}

	atfImage := createTestImage(2, []byte(expected.ATF))
	if err := writeImage(&buf, atfImage); err != nil {
		t.Fatal(err)
	}

	if _, err := mockFile.Write(buf.Bytes()); err != nil {
		t.Fatal(err)
	}

	versions, err := VersionFromBFBFile(mockFile.Name())
	if err != nil {
		t.Fatalf("parseBFBVersions failed on mock file: %v", err)
	}

	if !reflect.DeepEqual(versions, expected) {
		t.Errorf("Mock file versions don't match expected")
	}
}

// Helper function to create a test image with the correct header format
func createTestImage(imageID uint8, content []byte) *image {
	hdr := &imageHdr{
		magic:           0x13026642,
		major:           1,
		minor:           2,
		length:          8, // typical value
		imageID:         imageID,
		imageLen:        uint32(len(content)),
		imageCRC:        0, // Will be calculated later
		followingImages: 0, // Changed from 1 to 0 - this might be the issue
		nextImgVer:      1,
		curImgVer:       1,
	}

	// Calculate CRC including padding
	padding := make([]byte, numPaddingBytes(len(content)))
	crc := crc32.ChecksumIEEE(content)
	crc = crc32.Update(crc, crc32.IEEETable, padding)
	hdr.imageCRC = crc

	return &image{
		header: hdr,
		bits:   content,
	}
}

// Helper function to write an image to a buffer
func writeImage(w io.Writer, img *image) error {
	// Create the 24-byte header as three uint64 words
	var headerBuf [24]byte

	// First word (bytes 0-7)
	// The correct bit positions based on the parseHeader function in version.go:
	// Magic: bits 0-31, Major: bits 32-35, Minor: bits 36-39, Length: bits 52-55, ImageID: bits 56-63
	w0 := uint64(img.header.magic) |
		(uint64(img.header.major&0xF) << 32) |
		(uint64(img.header.minor&0xF) << 36) |
		(uint64(img.header.length&0xF) << 52) |
		(uint64(img.header.imageID&0xFF) << 56)

	// Second word (bytes 8-15)
	w1 := uint64(img.header.imageLen) |
		(uint64(img.header.imageCRC) << 32)

	// Third word (bytes 16-23)
	w2 := img.header.followingImages

	// Add NextImgVer and CurImgVer for minor version <= 2
	if img.header.minor <= 2 {
		w0 |= (uint64(img.header.nextImgVer) << 44)
		w0 |= (uint64(img.header.curImgVer) << 48)
	}

	// Write the words in little-endian format
	binary.LittleEndian.PutUint64(headerBuf[0:], w0)
	binary.LittleEndian.PutUint64(headerBuf[8:], w1)
	binary.LittleEndian.PutUint64(headerBuf[16:], w2)

	if _, err := w.Write(headerBuf[:]); err != nil {
		return err
	}

	// Write content
	if _, err := w.Write(img.bits); err != nil {
		return err
	}

	// Write padding to align to 8-byte boundary
	padding := make([]byte, numPaddingBytes(len(img.bits)))
	if _, err := w.Write(padding); err != nil {
		return err
	}

	return nil
}

// Helper function to create UEFI content in the correct format
func createUEFIContent(g Gomega, uefiVersion, buildID string) []byte {
	// Create a buffer for the raw UTF-16LE data
	var rawBuf bytes.Buffer

	// Write the UEFI version string as UTF-16LE
	// Make sure it contains "BlueField"
	if !strings.Contains(uefiVersion, "BlueField") {
		uefiVersion = "BlueField:" + uefiVersion
	}

	// Convert to UTF-16LE and write each character
	for _, r := range uefiVersion {
		g.Expect(binary.Write(&rawBuf, binary.LittleEndian, uint16(r))).To(Succeed())
	}

	// Add a null terminator
	g.Expect(binary.Write(&rawBuf, binary.LittleEndian, uint16(0))).Should(Succeed())

	// Write the build ID string as UTF-16LE
	// Make sure it contains "BId"
	if !strings.Contains(buildID, "BId") {
		buildID = "BId" + buildID
	}

	// Convert to UTF-16LE and write each character
	for _, r := range buildID {
		g.Expect(binary.Write(&rawBuf, binary.LittleEndian, uint16(r))).To(Succeed())
	}

	// Add a null terminator
	g.Expect(binary.Write(&rawBuf, binary.LittleEndian, uint16(0))).Should(Succeed())

	// Compress the raw data with gzip
	var gzipBuf bytes.Buffer
	gzipWriter := gzip.NewWriter(&gzipBuf)
	_, err := gzipWriter.Write(rawBuf.Bytes())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(gzipWriter.Close()).To(Succeed())

	return gzipBuf.Bytes()
}
