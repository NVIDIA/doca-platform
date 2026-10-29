/*
Copyright 2024 NVIDIA

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

package digest

import (
	"encoding/json"
	"strings"

	"github.com/opencontainers/go-digest"
)

// FromObjects returns a digest for the specified objects.
// SHA-256 is used as the algorithm.
// The objects are serialized to JSON before the digest is calculated.
// If an error occurs during serialization, an empty digest is returned.
func FromObjects(obj ...any) digest.Digest {
	if len(obj) == 0 {
		return ""
	}
	digester := digest.Canonical.Digester()
	enc := json.NewEncoder(digester.Hash())
	for _, spec := range obj {
		if err := enc.Encode(spec); err != nil {
			return ""
		}
	}
	return digester.Digest()
}

// Short returns a short string representation of the digest.
// If the full digest is shorter than the specified length, the full digest is returned.
// This is useful for displaying digests in user facing outputs.
// As example, for a sha:256 digest, the full digest is 64 characters long, but a short digest can be 10 characters long.
// This will have a total of 16^10 or 2^40 possible values, which is still a large enough space to avoid collisions.
func Short(d digest.Digest, length int) string {
	repr := d.String()
	// trim the encoding prefix `sha256:` or similar
	full := repr[strings.Index(repr, ":")+1:]
	if len(full) <= length {
		return full
	}
	return full[:length]
}
