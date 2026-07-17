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
	"strings"
	"testing"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	. "github.com/onsi/gomega"
	"github.com/opencontainers/go-digest"
	"k8s.io/utils/ptr"
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
					FileName: ptr.To("test"),
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

func Test_GenerateName(t *testing.T) {
	// maxGeneratedNameLength = 63, digestLength = 5, plus 1 for the hyphen, so the base is truncated to 57 chars.
	const maxBaseLen = 57
	longBase := strings.Repeat("a", 100)

	cases := []struct {
		name     string
		base     string
		objects  []any
		expected string
	}{
		{
			name:     "no objects yields no digest suffix",
			base:     "test",
			objects:  nil,
			expected: "test",
		},
		{
			name:     "empty base with single object",
			base:     "",
			objects:  []any{"foo"},
			expected: "464f0",
		},
		{
			name:     "base with single object",
			base:     "test",
			objects:  []any{"foo"},
			expected: "test-464f0",
		},
		{
			name:     "base with multiple objects",
			base:     "test",
			objects:  []any{"foo", "bar"},
			expected: "test-ae40d",
		},
		{
			name:     "encoding error yields empty digest suffix",
			base:     "test",
			objects:  []any{make(chan int)},
			expected: "test",
		},
		{
			name:     "base exactly at max length is not truncated",
			base:     strings.Repeat("a", maxBaseLen),
			objects:  []any{"foo"},
			expected: strings.Repeat("a", maxBaseLen) + "-464f0",
		},
		{
			name:     "base one over max length is truncated",
			base:     strings.Repeat("a", maxBaseLen+1),
			objects:  []any{"foo"},
			expected: strings.Repeat("a", maxBaseLen) + "-464f0",
		},
		{
			name:     "long base is truncated to max length",
			base:     longBase,
			objects:  []any{"foo"},
			expected: strings.Repeat("a", maxBaseLen) + "-464f0",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := NewWithT(t)
			g.Expect(GenerateName(c.base, c.objects...)).To(Equal(c.expected))
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
