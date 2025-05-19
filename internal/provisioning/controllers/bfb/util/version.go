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
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	"github.com/Masterminds/semver/v3"
)

const (
	magicNumber     = 0x13026642
	headerLength    = 24
	majorVer        = 1
	minorVer        = 2
	bluefieldString = "BlueField"
	releaseString   = "release"
	buildIDString   = "BId"
)

// VersionFromBFBFile reads a BlueField BFB file and returns the version information.
// The source of truth for this function is this script https://github.com/Mellanox/bfscripts/blob/c471e1c3229e7fb45e7e70dcb64caa43a459c303/bfver#L91.
// This function is a go implementation of that script.
func VersionFromBFBFile(filename string) (*provisioningv1.BFBVersions, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = file.Close()
	}()

	v, err := findUEFIVersions(file)
	if err != nil {
		return nil, err
	}
	atfVersion, err := findATFVersionInBFB(file)
	if err != nil {
		return nil, err
	}
	v.atf = atfVersion

	versions, err := v.format()
	if err != nil {
		return nil, err
	}
	return versions, nil
}

// bfbInfo is the raw data used to build the versions.
type bfbInfo struct {
	buildID string
	atf     string
	uefi    string
}

var (
	// imageTable is the specific image in the set of images contained in the BFB that contains the UEFI version and buildID.
	imageTable = map[int]string{
		5: "bl33",
	}
)

// imageHdr is the structure of the header for the image.
type imageHdr struct {
	magic           uint32
	major           uint8
	minor           uint8
	length          uint8
	imageID         uint8
	imageLen        uint32
	imageCRC        uint32
	followingImages uint64
	nextImgVer      uint8
	curImgVer       uint8
}

func numPaddingBytes(length int) int {
	return ((length + 7) & ^7) - length
}

func parseHeader(r io.Reader) (*imageHdr, error) {
	var hdr imageHdr
	data := make([]byte, headerLength)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, err
	}

	w0 := binary.LittleEndian.Uint64(data[0:8])
	w1 := binary.LittleEndian.Uint64(data[8:16])
	w2 := binary.LittleEndian.Uint64(data[16:24])

	hdr.magic = uint32(w0 & 0xFFFFFFFF)
	hdr.major = uint8((w0 >> 32) & 0xF)
	hdr.minor = uint8((w0 >> 36) & 0xF)
	hdr.length = uint8((w0 >> 52) & 0xF)
	hdr.imageID = uint8((w0 >> 56) & 0xFF)
	hdr.imageLen = uint32(w1 & 0xFFFFFFFF)
	hdr.imageCRC = uint32((w1 >> 32) & 0xFFFFFFFF)
	hdr.followingImages = w2

	if hdr.minor <= 2 {
		hdr.nextImgVer = uint8((w0 >> 44) & 0xFFFFFFFF)
		hdr.nextImgVer = uint8((w0 >> 48) & 0xFFFFFFFF)
	} else {
		hdr.nextImgVer = 0
		hdr.curImgVer = 0
	}
	if hdr.major == 0 || hdr.minor == 0 || hdr.length == 0 {
		return nil, fmt.Errorf("failed to parse image header for BFB")
	}
	if hdr.magic != magicNumber {
		return nil, errors.New("bad magic number")
	}

	if hdr.major != majorVer {
		return nil, errors.New("bad major bfbVersion")
	}

	if hdr.minor > minorVer {
		return nil, errors.New("bad minor bfbVersion")
	}

	return &hdr, nil
}

type image struct {
	header *imageHdr
	bits   []byte
}

func parseImage(r io.Reader) (*image, error) {
	hdr, err := parseHeader(r)
	if err != nil {
		return nil, err
	}

	bits := make([]byte, hdr.imageLen)
	if _, err := io.ReadFull(r, bits); err != nil {
		return nil, err
	}

	padding := make([]byte, numPaddingBytes(int(hdr.imageLen)))
	if _, err := io.ReadFull(r, padding); err != nil {
		return nil, err
	}

	crc := crc32.ChecksumIEEE(bits)
	crc = crc32.Update(crc, crc32.IEEETable, padding)
	if crc != hdr.imageCRC {
		return nil, errors.New("CRC mismatch")
	}

	return &image{header: hdr, bits: bits}, nil
}

func findVersionFileFromBFB(reader io.Reader) ([]byte, error) {
	for {
		img, err := parseImage(reader)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		id := img.header.imageID
		_, ok := imageTable[int(id)]
		if !ok {
			continue
		}
		return img.bits, nil
	}
	return nil, nil
}

func findUEFIVersions(file io.Reader) (*bfbInfo, error) {
	out := &bfbInfo{}
	versionFileData, err := findVersionFileFromBFB(file)
	if err != nil {
		return nil, err
	}

	reader, err := gzip.NewReader(bytes.NewBuffer(versionFileData))
	if err != nil {
		return nil, err
	}
	var currentString []uint16
	for {
		var r uint16
		err := binary.Read(reader, binary.LittleEndian, &r)
		if err != nil {
			break
		}

		if utf8.ValidRune(rune(r)) && unicode.IsPrint(rune(r)) && !unicode.IsSpace(rune(r)) {
			currentString = append(currentString, r)
		} else {
			if len(currentString) >= 4 {
				runes := utf16.Decode(currentString)
				if strings.Contains(string(runes), bluefieldString) {
					out.uefi = string(runes)
				}
				if strings.Contains(string(runes), buildIDString) {
					out.buildID = string(runes)
				}
			}
			if out.uefi != "" && out.buildID != "" {
				return out, nil
			}
			currentString = nil
		}
	}
	return nil, fmt.Errorf("string not found: %s", bluefieldString)
}

func findATFVersionInBFB(reader io.Reader) (string, error) {
	var currentString []rune
	r := bufio.NewReader(reader)
	for {
		r, _, err := r.ReadRune()
		if err != nil {
			break
		}
		// This file is encoded as
		if r <= 0x7F && unicode.IsPrint(r) {
			currentString = append(currentString, r)
		} else {
			if len(currentString) >= 4 {
				if strings.Contains(string(currentString), releaseString) {
					return string(currentString), nil
				}
			}
			currentString = nil
		}
	}
	return "", fmt.Errorf("string not found: %s", releaseString)
}

func formatBSP(uefi, bid string) (string, error) {
	buildIDRE := regexp.MustCompile(`.*BId([0-9]*).*`)
	buildIDMatch := buildIDRE.FindStringSubmatch(bid)
	if len(buildIDMatch) != 2 {
		return "", fmt.Errorf("invalid buildID: %s", bid)
	}

	bspVersionRE := regexp.MustCompile(`(.+\..+\..+).*-[0-9]+-g[0-9a-fA-F]+`)
	bspVersionMatch := bspVersionRE.FindStringSubmatch(uefi)
	if len(bspVersionMatch) != 2 {
		return "", fmt.Errorf("invalid uefi bfbVersion: %s", uefi)
	}
	bspVersion, err := semver.NewVersion(bspVersionMatch[1])
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s.%s", bspVersion.String(), buildIDMatch[1]), nil
}

func formatDOCA(bspVersion string) (string, error) {
	parts := strings.Split(bspVersion, ".")
	v, err := semver.NewVersion(strings.Join(parts[0:3], "."))
	if err != nil {
		return "", err
	}
	return semver.New(
		v.Major()-2,
		v.Minor(),
		v.Patch(),
		v.Prerelease(),
		v.Metadata()).String(), nil
}

func (v bfbInfo) format() (*provisioningv1.BFBVersions, error) {
	uefiParts := strings.Split(v.uefi, ":")
	if len(uefiParts) != 2 {
		return nil, fmt.Errorf("invalid uefi bfbVersion: %s", v.uefi)
	}

	bspVersion, err := formatBSP(uefiParts[1], v.buildID)
	if err != nil {
		return nil, err
	}

	docaVersion, err := formatDOCA(bspVersion)
	if err != nil {
		return nil, err
	}
	return &provisioningv1.BFBVersions{
		UEFI: uefiParts[1],
		ATF:  v.atf,
		DOCA: docaVersion,
		BSP:  bspVersion,
	}, nil
}
