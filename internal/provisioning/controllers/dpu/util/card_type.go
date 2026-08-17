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

import "strings"

// CardType is the signing key a BlueField card is fused for. Secure boot verifies a
// BFB against that key, so a card only boots firmware of its own type: a PK card
// takes production signed BFBs, a DK card takes development signed BFBs, and a QP
// card is not fused for a production key at all. Flashing the wrong type fails
// signature verification and the DPU never comes up.
type CardType string

const (
	// CardTypeUnknown means the card type could not be determined.
	CardTypeUnknown CardType = ""
	// CardTypePK is a production key card.
	CardTypePK CardType = "PK"
	// CardTypeDK is a development key card.
	CardTypeDK CardType = "DK"
	// CardTypeQP is an unsigned qualification card.
	CardTypeQP CardType = "QP"
)

// CardTypeFromOPN derives the card type from the last character of the OPN's final
// segment: "900-9D3B4-00SC-EA0" is PK, "900-9D3B4-00SC-EAA" is DK and
// "900-9D3B4-00SC-EAB" is QP. These are the documented "AA0", "AAA" and "AAB" part
// number endings.
//
// This assumes two properties of the NVIDIA BlueField-3 part number scheme: that
// every NVIDIA branded SKU is a four segment OPN whose final segment is three
// characters, and that only the last of those three encodes the security type. The
// two before it vary by board family and carry no type information, so they are not
// read.
//
// Partner SKUs (Lenovo, HPE, Oracle) are outside that scheme and carry their own part
// numbers, so they return CardTypeUnknown rather than being guessed at.
func CardTypeFromOPN(opn string) CardType {
	segments := strings.Split(opn, "-")
	if len(segments) != 4 || len(segments[3]) != 3 {
		return CardTypeUnknown
	}
	switch segments[3][2] {
	case '0':
		return CardTypePK
	case 'A':
		return CardTypeDK
	case 'B':
		return CardTypeQP
	default:
		return CardTypeUnknown
	}
}

// CardTypeFromBFBFileName derives the card type a BFB is signed for from the suffix of
// its file name. The documented suffixes are ".prod.bfb" for a PK signed image,
// ".dev.bfb" for a DK signed one and a plain ".bfb" for an unsigned one. Releases
// spell the first two with an underscore, as in
// "bf-bundle-3.4.0-92_26.04_ubuntu-24.04_64k_prod.bfb", so both separators are
// accepted.
//
// Anything that is not a ".bfb" file name yields CardTypeUnknown, including the
// release tree aliases such as "last_stable_ubuntu_24.04_64k_pk", which serve the
// bootstream directly without ever exposing the file name it was built under.
func CardTypeFromBFBFileName(fileName string) CardType {
	if !strings.HasSuffix(fileName, ".bfb") {
		return CardTypeUnknown
	}
	name := strings.TrimSuffix(fileName, ".bfb")

	switch {
	case hasCardTypeSuffix(name, "prod"):
		return CardTypePK
	case hasCardTypeSuffix(name, "dev"):
		return CardTypeDK
	default:
		// An unsigned BFB carries no suffix beyond ".bfb".
		return CardTypeQP
	}
}

// BFBFileNameFromURL returns the file name a BFB download URL ends in, which is the
// last path segment with any query string or fragment removed.
func BFBFileNameFromURL(url string) string {
	name := url
	if i := strings.IndexAny(name, "?#"); i >= 0 {
		name = name[:i]
	}
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	return name
}

// hasCardTypeSuffix reports whether name ends with the suffix, preceded by either
// separator that BFB release names use.
func hasCardTypeSuffix(name, suffix string) bool {
	return strings.HasSuffix(name, "_"+suffix) || strings.HasSuffix(name, "."+suffix)
}
