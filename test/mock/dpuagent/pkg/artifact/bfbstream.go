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
	"encoding/binary"
	"fmt"
)

const (
	// BFBMagic is the mlx-mkbfb image header magic ("Bf\x02\x13" little endian).
	BFBMagic = 0x13026642
	// BFBHeaderLen is the fixed mlx-mkbfb image header length.
	BFBHeaderLen = 24
	// MaxBFCFGSize bounds the bf.cfg trailer kept in memory. Production bf.cfg is limited to
	// 128 KiB by the BFB install scripts; 1 MiB leaves room for custom templates.
	MaxBFCFGSize = 1 << 20
)

// BFBSplitter consumes the nginx concat stream "<bfb><bf.cfg>" the controller hands to a BF3 BMC
// as a single ImageURI. It walks the mlx-mkbfb image header chain, feeds every BFB byte to the
// InventoryScanner (data is otherwise discarded) and collects whatever follows the last image as
// the bf.cfg. Only one HTTP request is made, so the registry sees the same traffic as with a real
// BMC.
type BFBSplitter struct {
	inventory *InventoryScanner
	header    []byte // partial header while waiting for 24 bytes
	remaining int64  // image bytes (incl. padding) still to skip
	images    int
	bfbBytes  int64
	inTrailer bool
	trailer   []byte
}

// NewBFBSplitter returns a splitter with a fresh inventory scanner.
func NewBFBSplitter() *BFBSplitter {
	return &BFBSplitter{inventory: NewInventoryScanner(), header: make([]byte, 0, BFBHeaderLen)}
}

// Write consumes the next chunk of the concat stream.
func (s *BFBSplitter) Write(p []byte) (int, error) {
	n := len(p)
	for len(p) > 0 {
		if s.inTrailer {
			if len(s.trailer)+len(p) > MaxBFCFGSize {
				return 0, fmt.Errorf("bf.cfg trailer exceeds %d bytes", MaxBFCFGSize)
			}
			s.trailer = append(s.trailer, p...)
			return n, nil
		}
		if s.remaining > 0 {
			take := int64(len(p))
			if take > s.remaining {
				take = s.remaining
			}
			_, _ = s.inventory.Write(p[:take])
			s.bfbBytes += take
			s.remaining -= take
			p = p[take:]
			continue
		}
		// Accumulate a header.
		need := BFBHeaderLen - len(s.header)
		if need > len(p) {
			need = len(p)
		}
		s.header = append(s.header, p[:need]...)
		p = p[need:]
		if len(s.header) < BFBHeaderLen {
			continue
		}
		if binary.LittleEndian.Uint32(s.header[0:4]) != BFBMagic {
			// Not an image header: the BFB ended and this is the start of bf.cfg.
			s.inTrailer = true
			s.trailer = append(s.trailer, s.header...)
			s.header = s.header[:0]
			continue
		}
		_, _ = s.inventory.Write(s.header)
		imageLen := int64(binary.LittleEndian.Uint32(s.header[8:12]))
		s.remaining = imageLen + padTo8(imageLen)
		s.bfbBytes += BFBHeaderLen
		s.images++
		s.header = s.header[:0]
	}
	return n, nil
}

func padTo8(n int64) int64 {
	if rem := n % 8; rem != 0 {
		return 8 - rem
	}
	return 0
}

// Finish must be called after the last Write. Fewer than 24 bytes left over after the last image
// cannot be a header and are treated as trailer. It fails when the stream ended inside an image.
func (s *BFBSplitter) Finish() error {
	if s.remaining > 0 {
		return fmt.Errorf("BFB stream truncated: %d bytes of image %d missing", s.remaining, s.images)
	}
	if !s.inTrailer && len(s.header) > 0 {
		s.inTrailer = true
		s.trailer = append(s.trailer, s.header...)
		s.header = s.header[:0]
	}
	if s.images == 0 {
		return fmt.Errorf("no BFB image header found in stream")
	}
	return nil
}

// BFCFG returns the bytes that followed the last BFB image.
func (s *BFBSplitter) BFCFG() []byte {
	return s.trailer
}

// Images returns the number of BFB images seen.
func (s *BFBSplitter) Images() int {
	return s.images
}

// BFBSize returns the number of BFB bytes (headers, data and padding) seen.
func (s *BFBSplitter) BFBSize() int64 {
	return s.bfbBytes
}

// Inventory returns the software inventory scanner fed with the BFB bytes.
func (s *BFBSplitter) Inventory() *InventoryScanner {
	return s.inventory
}
