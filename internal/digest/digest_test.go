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
	"testing"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	. "github.com/onsi/gomega"
	"github.com/opencontainers/go-digest"
)

func Test_Digest(t *testing.T) {
	cases := []struct {
		name     string
		objects  []any
		expected string
	}{
		{
			name:     "empty objects",
			objects:  []any{},
			expected: "",
		},
		{
			name:     "single object",
			objects:  []any{"foo"},
			expected: "sha256:464f0da35dc95dc2dc0bc4c84904197cb0f035eed8e08839a01515320c76c832",
		},
		{
			name:     "multiple objects",
			objects:  []any{"foo", "bar"},
			expected: "sha256:ae40de46fb59e1cf8dd15ade2693d4dfd7721203a558d276383f3f976ff7a40a",
		},
		{
			name:     "error encoding",
			objects:  []any{make(chan int)},
			expected: "",
		},
		{
			name: "bfb and dpuFlavor",
			objects: []any{
				provisioningv1.BFBSpec{
					FileName: "test",
					URL:      "http://dummy-bfb-url.com",
				},
				provisioningv1.DPUFlavorSpec{
					ConfigFiles: []provisioningv1.ConfigFile{
						{
							Path:        "/var/lib/doca/config",
							Permissions: "0644",
						},
					},
				},
			},
			expected: "sha256:225c560afa47526947016d5e17468ebcb970417ff3195af291fbc131c9681422",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := NewWithT(t)
			d := FromObjects(c.objects...)
			if d.String() != c.expected {
				g.Expect(d.String()).To(Equal(c.expected))
			}
		})
	}
}

func Test_Short(t *testing.T) {
	cases := []struct {
		name     string
		d        digest.Digest
		length   int
		expected string
	}{
		{
			name:     "empty digest",
			d:        "",
			length:   10,
			expected: "",
		},
		{
			name:     "shorter than length",
			d:        "sha256:1234",
			length:   10,
			expected: "1234",
		},
		{
			name:     "equal to length",
			d:        "sha256:c26cf8af13",
			length:   10,
			expected: "c26cf8af13",
		},
		{
			name:     "longer than length",
			d:        "sha256:c26cf8af130955c5c67cfea",
			length:   10,
			expected: "c26cf8af13",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := NewWithT(t)
			s := Short(c.d, c.length)
			if s != c.expected {
				g.Expect(s).To(Equal(c.expected))
			}
		})
	}
}
