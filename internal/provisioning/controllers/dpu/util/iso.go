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

package util

import (
	"encoding/binary"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	diskfs "github.com/diskfs/go-diskfs"
	"github.com/diskfs/go-diskfs/disk"
	"github.com/diskfs/go-diskfs/filesystem"
	"github.com/diskfs/go-diskfs/filesystem/iso9660"
)

// IsoRootFile is one root-level file in the image, in the same sense as each positional
// pathname argument to mkisofs (all names are written under the ISO root directory).
type IsoRootFile struct {
	Name string
	Data []byte
}

// MkIso writes an ISO 9660 image equivalent to OpenBSD mkhybrid mkisofs with:
//
//	mkisofs -output <isoname>.iso -volid <volumelabel> -joliet -rock <files[0].Name> <files[1].Name> ...
//
// That is -J (Joliet), -R (Rock Ridge, long option -rock; not rationalized -r), and -V
// (volume ID), per gnu/usr.sbin/mkhybrid/src/mkisofs.c. The caller must pass files in the
// same order as mkisofs source arguments; extent / directory order follows that order.
func MkIso(isoname, volumelabel string, files []IsoRootFile) (string, error) {
	var err error
	var storageSize int64
	var diskSize int64
	var blockSize int64 = 2048
	volLabel := volumelabel

	if len(volLabel) > 32 {
		return "", fmt.Errorf("volume label longer than 32 bytes (ISO 9660 primary volume id field)")
	}
	if utf8.RuneCountInString(volLabel) > 16 {
		return "", fmt.Errorf("volume label longer than 16 Unicode code points (Joliet supplementary volume id field)")
	}
	for _, f := range files {
		if f.Name == "" || strings.ContainsAny(f.Name, `/\`) {
			return "", fmt.Errorf("invalid ISO root file name %q", f.Name)
		}
		storageSize += int64(len(f.Data)) + blockSize // for each file filelength + blocksize
	}
	diskImg := fmt.Sprintf("%v.iso", isoname)
	// Working image must fit the finalized ISO (data + tables + descriptors). Trim to PVD size after finalize.
	const isoMetadataSlack = 512 * 1024
	diskSize = storageSize + isoMetadataSlack
	if diskSize < 384*1024 {
		diskSize = 384 * 1024
	}
	mydisk, err := diskfs.Create(diskImg, diskSize, diskfs.SectorSizeDefault)
	if err != nil {
		return "", err
	}

	// the following line is required for an ISO, which may have logical block sizes
	// only of 2048, 4096, 8192
	mydisk.LogicalBlocksize = blockSize
	fspec := disk.FilesystemSpec{
		Partition:   0,
		FSType:      filesystem.TypeISO9660,
		VolumeLabel: volLabel}

	fs, err := mydisk.CreateFilesystem(fspec)
	if err != nil {
		return "", err
	}
	for _, f := range files {
		dest, err := fs.OpenFile(f.Name, os.O_CREATE|os.O_RDWR)
		defer func() { _ = dest.Close() }()
		if err != nil {
			return "", err
		}
		_, err = dest.Write(f.Data)
		if err != nil {
			return "", err
		}
	}

	iso, ok := fs.(*iso9660.FileSystem)
	if !ok {
		return "", fmt.Errorf("not an iso9660 filesystem")
	}
	// RockRidge: mkisofs -R / --rock (full Rock Ridge, not -r rational-rock).
	// Joliet: mkisofs -J / --joliet. VolumeIdentifier: mkisofs -V / --volid.
	err = iso.Finalize(iso9660.FinalizeOptions{
		RockRidge:        true,
		Joliet:           true,
		VolumeIdentifier: volLabel,
	})
	if err != nil {
		return "", err
	}
	if err := truncateISOToPVDSize(diskImg, int(blockSize)); err != nil {
		return "", err
	}
	// go-diskfs writes the Joliet SVD volume identifier as raw ASCII; the field must be
	// UCS-2BE (ECMA-119), otherwise blkid and other tools decode pairs of ASCII bytes as
	// UTF-16 codepoints and show a garbled LABEL. Also pad the PVD field with spaces like mkisofs.
	if err := fixISO9660AndJolietVolumeLabels(diskImg, int(blockSize), volLabel); err != nil {
		return "", err
	}
	return diskImg, err
}

// truncateISOToPVDSize shrinks the image file to the logical size recorded in the Primary Volume
// Descriptor. go-diskfs leaves the backing file at its preallocated length; mkisofs does not.
func truncateISOToPVDSize(isoPath string, logicalBlockSize int) error {
	const pvdSector = 16
	f, err := os.OpenFile(isoPath, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open ISO for truncate: %w", err)
	}
	defer func() { _ = f.Close() }()

	pvd := make([]byte, logicalBlockSize)
	if _, err := f.ReadAt(pvd, int64(pvdSector*logicalBlockSize)); err != nil {
		return fmt.Errorf("read primary volume descriptor: %w", err)
	}
	volumeSize := binary.LittleEndian.Uint32(pvd[80:84])
	bs := binary.LittleEndian.Uint16(pvd[128:130])
	if bs == 0 {
		bs = uint16(logicalBlockSize)
	}
	isoBytes := int64(volumeSize) * int64(bs)
	if err := f.Truncate(isoBytes); err != nil {
		return fmt.Errorf("truncate ISO to %d bytes: %w", isoBytes, err)
	}
	return nil
}

const (
	isoVolDescTypePrimary       = 0x01
	isoVolDescTypeSupplementary = 0x02
	isoVolDescTypeTerminator    = 0xff
	jolietEscByte0              = 0x25 // '%'
	jolietEscByte1              = 0x2f // '/'
)

// fixISO9660AndJolietVolumeLabels corrects go-diskfs finalize output for volume identifiers:
//   - Primary volume descriptor (sector 16) field is space-padded to 32 bytes.
//   - Joliet supplementary descriptor volume id (offset 40, 32 bytes) is UCS-2BE / "UTF-16BE"
//     padded with NUL code units, matching mkisofs/genisoimage.
func fixISO9660AndJolietVolumeLabels(isoPath string, logicalBlockSize int, label string) error {
	f, err := os.OpenFile(isoPath, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open ISO for volume label patch: %w", err)
	}
	defer func() { _ = f.Close() }()

	pvdOff := int64(16 * logicalBlockSize)
	pvd := make([]byte, logicalBlockSize)
	if _, err := f.ReadAt(pvd, pvdOff); err != nil {
		return fmt.Errorf("read primary volume descriptor: %w", err)
	}
	if pvd[0] != isoVolDescTypePrimary {
		return fmt.Errorf("sector 16: expected primary volume descriptor (type 1), got %d", pvd[0])
	}
	var pvdField [32]byte
	copy(pvdField[:], label)
	for i := len(label); i < 32; i++ {
		pvdField[i] = ' '
	}
	if _, err := f.WriteAt(pvdField[:], pvdOff+40); err != nil {
		return fmt.Errorf("write PVD volume identifier: %w", err)
	}

	const maxVDSector = 48 // volume descriptor set is small; bound scans on corrupt media
	sector := make([]byte, logicalBlockSize)
	for sec := 16; sec < maxVDSector; sec++ {
		off := int64(sec * logicalBlockSize)
		if _, err := f.ReadAt(sector, off); err != nil {
			return fmt.Errorf("read volume descriptor at sector %d: %w", sec, err)
		}
		switch sector[0] {
		case isoVolDescTypeTerminator:
			return fmt.Errorf("no Joliet supplementary volume descriptor found to patch")
		case isoVolDescTypeSupplementary:
			if sector[88] != jolietEscByte0 || sector[89] != jolietEscByte1 {
				continue
			}
			var jField [32]byte
			i := 0
			for _, r := range label {
				if r > 0xffff {
					return fmt.Errorf("volume label contains U+%04X, outside UCS-2 BMP", r)
				}
				if i >= 16 {
					return fmt.Errorf("internal error: joliet volume field overflow")
				}
				binary.BigEndian.PutUint16(jField[2*i:2*i+2], uint16(r))
				i++
			}
			if _, err := f.WriteAt(jField[:], off+40); err != nil {
				return fmt.Errorf("write Joliet volume identifier: %w", err)
			}
			return nil
		default:
			continue
		}
	}
	return fmt.Errorf("no Joliet supplementary volume descriptor found within first %d sectors", maxVDSector)
}
