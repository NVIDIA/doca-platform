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

package artifact

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math/bits"
	"regexp"
	"strings"
)

// pldmHeaderIdentifierV10 is the DSP0267 v1.0 PackageHeaderIdentifier. NVIDIA's fwpkg_unpack.py,
// which the BlueFieldSoftware controller runs, rejects every other identifier, so the mock does too.
var pldmHeaderIdentifierV10 = []byte{0xf0, 0x18, 0x87, 0x8c, 0xcb, 0x7d, 0x49, 0x43, 0x98, 0x00, 0xa0, 0x2f, 0x05, 0x9a, 0xca, 0x02}

// PLDMComponent is one entry of the component image information area, named the way fwpkg_unpack
// names the file it would extract.
type PLDMComponent struct {
	Classification uint16
	Identifier     uint16
	Offset         uint32
	Size           uint32
	Version        string
	ImageName      string
}

// PLDMPackage is the parsed header of a DSP0267 firmware update package.
type PLDMPackage struct {
	FormatRevision uint8
	HeaderSize     uint16
	PackageVersion string
	Components     []PLDMComponent
	// Versions are the component versions classified the way the BlueFieldSoftware controller
	// records BlueFieldSoftware.status.versions[psid].
	Versions PLDMVersions
}

// PLDMVersions are the four component versions the DPU firmware update phase compares, plus the
// PSID carried in the CX9 image name.
type PLDMVersions struct {
	BMC     string
	ERoT    string
	SBIOS   string
	NIC     string
	NICPSID string
}

// Complete reports whether every version the controller requires is present.
func (v PLDMVersions) Complete() bool {
	return v.BMC != "" && v.ERoT != "" && v.SBIOS != "" && v.NIC != ""
}

type deviceRecord struct {
	applicable uint64
	setVersion string
}

type pldmReader struct {
	r   *bufio.Reader
	pos int
}

func (p *pldmReader) bytes(n int) ([]byte, error) {
	buf := make([]byte, n)
	if _, err := io.ReadFull(p.r, buf); err != nil {
		return nil, fmt.Errorf("read %d bytes at offset %d: %w", n, p.pos, err)
	}
	p.pos += n
	return buf, nil
}

// skip advances past n bytes without buffering them; it is for fields whose content is not used
// and whose length is package controlled.
func (p *pldmReader) skip(n int64) error {
	if _, err := io.CopyN(io.Discard, p.r, n); err != nil {
		return fmt.Errorf("skip %d bytes at offset %d: %w", n, p.pos, err)
	}
	p.pos += int(n)
	return nil
}

func (p *pldmReader) u8() (uint8, error) {
	b, err := p.bytes(1)
	if err != nil {
		return 0, err
	}
	return b[0], nil
}

func (p *pldmReader) u16() (uint16, error) {
	b, err := p.bytes(2)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint16(b), nil
}

func (p *pldmReader) u32() (uint32, error) {
	b, err := p.bytes(4)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(b), nil
}

func (p *pldmReader) str(n int) (string, error) {
	b, err := p.bytes(n)
	if err != nil {
		return "", err
	}
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return string(b), nil
}

// ParsePLDM reads the package header from r (the payload is not consumed) and classifies the
// component images. It mirrors fwpkg_unpack.py's parsing and image naming so the versions match
// what the BlueFieldSoftware controller extracts from the same bundle.
func ParsePLDM(r io.Reader) (*PLDMPackage, error) {
	p := &pldmReader{r: bufio.NewReader(r)}
	id, err := p.bytes(16)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(id, pldmHeaderIdentifierV10) {
		return nil, fmt.Errorf("unsupported PLDM package header identifier %x (want DSP0267 v1.0)", id)
	}
	pkg := &PLDMPackage{}
	if pkg.FormatRevision, err = p.u8(); err != nil {
		return nil, err
	}
	if pkg.HeaderSize, err = p.u16(); err != nil {
		return nil, err
	}
	if _, err = p.bytes(13); err != nil { // ReleaseDateTime
		return nil, err
	}
	bitmapBits, err := p.u16()
	if err != nil {
		return nil, err
	}
	if _, err = p.u8(); err != nil { // PackageVersionStringType
		return nil, err
	}
	verLen, err := p.u8()
	if err != nil {
		return nil, err
	}
	if pkg.PackageVersion, err = p.str(int(verLen)); err != nil {
		return nil, err
	}
	bitmapBytes := (int(bitmapBits) + 7) / 8
	if bitmapBytes > 8 {
		return nil, fmt.Errorf("ComponentBitmapBitLength %d exceeds 64 components", bitmapBits)
	}

	records, err := p.deviceRecords(pkg.FormatRevision, bitmapBytes)
	if err != nil {
		return nil, fmt.Errorf("firmware device identification area: %w", err)
	}
	if pkg.FormatRevision >= 2 {
		if _, err := p.deviceRecords(pkg.FormatRevision, bitmapBytes); err != nil {
			return nil, fmt.Errorf("downstream device identification area: %w", err)
		}
	}

	count, err := p.u16()
	if err != nil {
		return nil, err
	}
	for i := 0; i < int(count); i++ {
		c := PLDMComponent{}
		if c.Classification, err = p.u16(); err != nil {
			return nil, err
		}
		if c.Identifier, err = p.u16(); err != nil {
			return nil, err
		}
		if _, err = p.u32(); err != nil { // ComparisonStamp
			return nil, err
		}
		if _, err = p.u16(); err != nil { // Options
			return nil, err
		}
		if _, err = p.u16(); err != nil { // RequestedActivationMethod
			return nil, err
		}
		if c.Offset, err = p.u32(); err != nil {
			return nil, err
		}
		if c.Size, err = p.u32(); err != nil {
			return nil, err
		}
		if _, err = p.u8(); err != nil { // VersionStringType
			return nil, err
		}
		vl, err := p.u8()
		if err != nil {
			return nil, err
		}
		if c.Version, err = p.str(int(vl)); err != nil {
			return nil, err
		}
		if pkg.FormatRevision >= 3 {
			opaqueLen, err := p.u32()
			if err != nil {
				return nil, err
			}
			if err := p.skip(int64(opaqueLen)); err != nil {
				return nil, err
			}
		}
		c.ImageName = imageName(records, i, c.Version)
		pkg.Components = append(pkg.Components, c)
	}
	pkg.Versions, err = classify(pkg.Components)
	if err != nil {
		return nil, err
	}
	return pkg, nil
}

func (p *pldmReader) deviceRecords(revision uint8, bitmapBytes int) ([]deviceRecord, error) {
	count, err := p.u8()
	if err != nil {
		return nil, err
	}
	records := make([]deviceRecord, 0, count)
	for i := 0; i < int(count); i++ {
		if _, err := p.u16(); err != nil { // RecordLength
			return nil, err
		}
		descriptorCount, err := p.u8()
		if err != nil {
			return nil, err
		}
		if _, err := p.u32(); err != nil { // DeviceUpdateOptionFlags
			return nil, err
		}
		if _, err := p.u8(); err != nil { // ComponentImageSetVersionStringType
			return nil, err
		}
		setLen, err := p.u8()
		if err != nil {
			return nil, err
		}
		pkgDataLen, err := p.u16()
		if err != nil {
			return nil, err
		}
		if revision >= 4 {
			if _, err := p.u32(); err != nil { // ReferenceManifestLength
				return nil, err
			}
		}
		bitmap, err := p.bytes(bitmapBytes)
		if err != nil {
			return nil, err
		}
		var applicable uint64
		for j, b := range bitmap {
			applicable |= uint64(b) << (8 * j)
		}
		setVersion, err := p.str(int(setLen))
		if err != nil {
			return nil, err
		}
		for j := 0; j < int(descriptorCount); j++ {
			if _, err := p.u16(); err != nil { // DescriptorType
				return nil, err
			}
			length, err := p.u16()
			if err != nil {
				return nil, err
			}
			if _, err := p.bytes(int(length)); err != nil { // DescriptorData (vendor defined included)
				return nil, err
			}
		}
		if _, err := p.bytes(int(pkgDataLen)); err != nil { // FirmwareDevicePackageData
			return nil, err
		}
		records = append(records, deviceRecord{applicable: applicable, setVersion: setVersion})
	}
	return records, nil
}

var multiUnderscore = regexp.MustCompile("_+")

// imageName replays fwpkg_unpack.py get_image_name for component index idx: the first device record
// whose bitmap covers the component contributes its ComponentImageSetVersionString (or, when that
// string is comma separated, the segment for this component's ordinal within the record), then
// ":" becomes "_", "_N/A" is dropped, the component version is appended and the suffix added.
func imageName(records []deviceRecord, idx int, version string) string {
	mask := uint64(1) << idx
	name := ""
	for _, rec := range records {
		if rec.applicable&mask != mask {
			continue
		}
		name = rec.setVersion
		if strings.Contains(name, ",") {
			parts := strings.Split(name, ",")
			ordinal := bits.OnesCount64(rec.applicable & (mask | (mask - 1)))
			if ordinal >= 1 && ordinal <= len(parts) {
				name = parts[ordinal-1]
			}
		}
		break
	}
	if name == "" {
		return ""
	}
	name = strings.ReplaceAll(name, ":", "_")
	name = strings.ReplaceAll(name, "_N/A", "")
	name += "_" + version
	if strings.HasPrefix(name, "FW-Package") {
		name += ".fwpkg"
	} else {
		name += "_image.bin"
	}
	return multiUnderscore.ReplaceAllString(name, "_")
}

// classify mirrors applyDeviceVersions in the BlueFieldSoftware extracting state.
func classify(components []PLDMComponent) (PLDMVersions, error) {
	v := PLDMVersions{}
	for _, c := range components {
		upper := strings.ToUpper(c.ImageName)
		switch {
		case strings.Contains(upper, "CX9"):
			psid, err := psidFromCX9ImageName(upper)
			if err != nil {
				return v, err
			}
			v.NIC = c.Version
			v.NICPSID = psid
		case strings.Contains(upper, "BMC_BF4"):
			v.BMC = c.Version
		case strings.Contains(upper, "EROT"):
			v.ERoT = c.Version
		case strings.Contains(upper, "SBIOS"):
			v.SBIOS = c.Version
		}
	}
	if !v.Complete() {
		return v, fmt.Errorf("PLDM bundle is missing required component versions (need BMC, ERoT, SBIOS and CX9): %+v", v)
	}
	return v, nil
}

// psidFromCX9ImageName extracts "MT_<digits>" from an upper-cased CX9 image name such as
// CX9_MT_0000001775_82.48.4004_IMAGE.BIN.
func psidFromCX9ImageName(imageName string) (string, error) {
	const marker = "CX9_"
	idx := strings.Index(imageName, marker)
	if idx < 0 {
		return "", fmt.Errorf("CX9 image name %q does not contain %q", imageName, marker)
	}
	parts := strings.Split(imageName[idx+len(marker):], "_")
	if len(parts) < 2 || parts[0] != "MT" || parts[1] == "" {
		return "", fmt.Errorf("CX9 image name %q does not contain a PSID after %q", imageName, marker)
	}
	return parts[0] + "_" + parts[1], nil
}
