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

// gen trims a real BFB and a real PLDM firmware package into the small, structurally identical
// fixtures under test/mock/dpuagent/testdata. It is run by hand, never in CI:
//
//	go run ./test/mock/dpuagent/testdata/gen --bfb <real.bfb> --pldm <real.fwpkg> --out test/mock/dpuagent/testdata
//
// BFB: every mlx-mkbfb image header is kept (id, version bits, following_images bitmap); only the
// image carrying the Redfish software inventory JSON keeps its data, every other image shrinks to
// eight zero bytes. image_len and the CRC32 over data+padding are rewritten.
//
// PLDM: the DSP0267 header is kept byte for byte except that each component's
// ComponentLocationOffset/ComponentSize is rewritten for a 16 byte payload and the header CRC32 is
// recomputed.
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
)

const (
	bfbMagic       = 0x13026642
	bfbHeaderLen   = 24
	inventoryMark  = "This JSON represents"
	maxInlineImage = 1 << 20
	trimmedPayload = 16
)

func main() {
	bfb := flag.String("bfb", "", "path to a real BFB file")
	pldm := flag.String("pldm", "", "path to a real PLDM firmware package")
	out := flag.String("out", "test/mock/dpuagent/testdata", "output directory")
	flag.Parse()
	if *bfb == "" && *pldm == "" {
		fmt.Fprintln(os.Stderr, "at least one of --bfb or --pldm is required")
		os.Exit(2)
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		fail(err)
	}
	if *bfb != "" {
		if err := trimBFB(*bfb, filepath.Join(*out, "bf3-trimmed.bfb")); err != nil {
			fail(fmt.Errorf("trim BFB: %w", err))
		}
	}
	if *pldm != "" {
		if err := trimPLDM(*pldm, filepath.Join(*out, "bf4-trimmed.fwpkg")); err != nil {
			fail(fmt.Errorf("trim PLDM: %w", err))
		}
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func pad8(n int64) int64 {
	if rem := n % 8; rem != 0 {
		return 8 - rem
	}
	return 0
}

func trimBFB(in, out string) error {
	f, err := os.Open(in)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	var result bytes.Buffer
	header := make([]byte, bfbHeaderLen)
	images, kept := 0, 0
	for {
		if _, err := io.ReadFull(f, header); err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("read header %d: %w", images, err)
		}
		w0 := binary.LittleEndian.Uint64(header[0:8])
		if uint32(w0) != bfbMagic {
			return fmt.Errorf("image %d: bad magic %#x", images, uint32(w0))
		}
		imageLen := int64(binary.LittleEndian.Uint32(header[8:12]))
		var data []byte
		if imageLen <= maxInlineImage {
			data = make([]byte, imageLen)
			if _, err := io.ReadFull(f, data); err != nil {
				return fmt.Errorf("read image %d: %w", images, err)
			}
			if _, err := f.Seek(pad8(imageLen), io.SeekCurrent); err != nil {
				return err
			}
			if !bytes.Contains(data, []byte(inventoryMark)) {
				data = nil
			}
		} else if _, err := f.Seek(imageLen+pad8(imageLen), io.SeekCurrent); err != nil {
			return err
		}
		if data == nil {
			data = make([]byte, 8)
		} else {
			kept++
		}
		padding := make([]byte, pad8(int64(len(data))))
		crc := crc32.Update(crc32.ChecksumIEEE(data), crc32.IEEETable, padding)
		newHeader := make([]byte, bfbHeaderLen)
		copy(newHeader, header)
		binary.LittleEndian.PutUint64(newHeader[8:16], uint64(len(data))|uint64(crc)<<32)
		result.Write(newHeader)
		result.Write(data)
		result.Write(padding)
		images++
	}
	if kept != 1 {
		return fmt.Errorf("expected exactly one software inventory image, found %d in %d images", kept, images)
	}
	fmt.Printf("BFB: %d images, %d bytes -> %s\n", images, result.Len(), out)
	return os.WriteFile(out, result.Bytes(), 0o644)
}

type walker struct {
	b   []byte
	pos int
}

func (w *walker) u8() uint8   { v := w.b[w.pos]; w.pos++; return v }
func (w *walker) u16() uint16 { v := binary.LittleEndian.Uint16(w.b[w.pos:]); w.pos += 2; return v }
func (w *walker) u32() uint32 { v := binary.LittleEndian.Uint32(w.b[w.pos:]); w.pos += 4; return v }
func (w *walker) skip(n int)  { w.pos += n }

func trimPLDM(in, out string) error {
	f, err := os.Open(in)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	prefix := make([]byte, 19)
	if _, err := io.ReadFull(f, prefix); err != nil {
		return err
	}
	headerSize := int(binary.LittleEndian.Uint16(prefix[17:19]))
	header := make([]byte, headerSize)
	copy(header, prefix)
	if _, err := io.ReadFull(f, header[19:]); err != nil {
		return fmt.Errorf("read %d byte header: %w", headerSize, err)
	}

	w := &walker{b: header, pos: 16}
	revision := w.u8()
	w.skip(2 + 13) // HeaderSize, ReleaseDateTime
	bitmapBytes := (int(w.u16()) + 7) / 8
	w.skip(1) // PackageVersionStringType
	w.skip(int(w.u8()))
	skipRecords := func() {
		count := int(w.u8())
		for i := 0; i < count; i++ {
			w.skip(2)             // RecordLength
			descriptors := w.u8() // DescriptorCount
			w.skip(4 + 1)         // flags, set version type
			setLen := int(w.u8())
			pkgDataLen := int(w.u16())
			if revision >= 4 {
				w.skip(4)
			}
			w.skip(bitmapBytes + setLen)
			for j := 0; j < int(descriptors); j++ {
				w.skip(2)
				w.skip(int(w.u16()))
			}
			w.skip(pkgDataLen)
		}
	}
	skipRecords()
	if revision >= 2 {
		skipRecords()
	}
	count := int(w.u16())
	var payloads bytes.Buffer
	nextOffset := uint32(headerSize)
	for i := 0; i < count; i++ {
		w.skip(2 + 2 + 4 + 2 + 2)
		offsetPos := w.pos
		offset := w.u32()
		size := w.u32()
		w.skip(1)
		w.skip(int(w.u8()))
		if revision >= 3 {
			w.skip(int(w.u32()))
		}
		sample := make([]byte, trimmedPayload)
		if size < trimmedPayload {
			return fmt.Errorf("component %d payload is only %d bytes", i, size)
		}
		if _, err := f.ReadAt(sample, int64(offset)); err != nil {
			return fmt.Errorf("read component %d payload: %w", i, err)
		}
		binary.LittleEndian.PutUint32(header[offsetPos:], nextOffset)
		binary.LittleEndian.PutUint32(header[offsetPos+4:], trimmedPayload)
		payloads.Write(sample)
		nextOffset += trimmedPayload
	}
	binary.LittleEndian.PutUint32(header[headerSize-4:], crc32.ChecksumIEEE(header[:headerSize-4]))
	result := append(header, payloads.Bytes()...)
	fmt.Printf("PLDM: %d components, %d bytes -> %s\n", count, len(result), out)
	return os.WriteFile(out, result, 0o644)
}
