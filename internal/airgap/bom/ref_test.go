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

package bom

import (
	"testing"

	. "github.com/onsi/gomega"
)

const testDigest = "sha256:73aaf090f3d85aa34ee199857f03fa3a95c8ede2ffd4cc2cdb5b94e566b11662"

func TestParseImageRef(t *testing.T) {
	tests := []struct {
		in   string
		want ImageRef
	}{
		{in: "nginx", want: ImageRef{Host: "docker.io", Path: "library/nginx"}},
		{in: "nginx:1.27", want: ImageRef{Host: "docker.io", Path: "library/nginx", Tag: "1.27"}},
		{in: "otel/collector:0.1", want: ImageRef{Host: "docker.io", Path: "otel/collector", Tag: "0.1"}},
		{in: "index.docker.io/library/busybox:1.36", want: ImageRef{Host: "docker.io", Path: "library/busybox", Tag: "1.36"}},
		{in: "docker.io/busybox", want: ImageRef{Host: "docker.io", Path: "library/busybox"}},
		{in: "localhost/app:v1", want: ImageRef{Host: "localhost", Path: "app", Tag: "v1"}},
		{in: "registry.example:5000/team/image:v1", want: ImageRef{Host: "registry.example:5000", Path: "team/image", Tag: "v1"}},
		{in: "registry.example:5000/team/image", want: ImageRef{Host: "registry.example:5000", Path: "team/image"}},
		{in: "nvcr.io/nvidia/doca/doca_hbn:3.2.1-doca3.2.1", want: ImageRef{Host: "nvcr.io", Path: "nvidia/doca/doca_hbn", Tag: "3.2.1-doca3.2.1"}},
		{in: "ghcr.io/a/b:v1@" + testDigest, want: ImageRef{Host: "ghcr.io", Path: "a/b", Tag: "v1", Digest: testDigest}},
		{in: "ghcr.io/a/b@" + testDigest, want: ImageRef{Host: "ghcr.io", Path: "a/b", Digest: testDigest}},
		{in: "  quay.io/a/b:v1  ", want: ImageRef{Host: "quay.io", Path: "a/b", Tag: "v1"}},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			g := NewWithT(t)
			got, err := ParseImageRef(tt.in)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(got).To(Equal(tt.want))
			// The normalised string parses back to the same reference.
			again, err := ParseImageRef(got.String())
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(again).To(Equal(got))
		})
	}
}

func TestParseImageRefErrors(t *testing.T) {
	for _, in := range []string{
		"",
		"$REGISTRY/image:v1",
		"example.com/image:$TAG",
		"ghcr.io/UPPER/case:v1",
		"ghcr.io/a/b:v1@sha256:short",
		"ghcr.io/a/b:bad tag",
		":v0.1.0",
		"oci://ghcr.io/a/b:v1",
	} {
		t.Run(in, func(t *testing.T) {
			_, err := ParseImageRef(in)
			NewWithT(t).Expect(err).To(HaveOccurred())
		})
	}
}
