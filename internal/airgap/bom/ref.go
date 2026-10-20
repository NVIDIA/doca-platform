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
	"fmt"
	"regexp"
	"strings"
)

const (
	// DockerHubHost is the canonical host of Docker Hub image references.
	DockerHubHost = "docker.io"
	// dockerHubLibrary is the implicit namespace of single-component Docker Hub references.
	dockerHubLibrary = "library"
)

var (
	// pathComponentRe is one path component of a repository, as defined by the OCI distribution spec.
	pathComponentRe = regexp.MustCompile(`^[a-z0-9]+(?:(?:[._]|__|-+)[a-z0-9]+)*$`)
	// hostRe is a registry host, optionally with a port.
	hostRe = regexp.MustCompile(`^(?:[a-zA-Z0-9](?:[a-zA-Z0-9-]*[a-zA-Z0-9])?)(?:\.(?:[a-zA-Z0-9](?:[a-zA-Z0-9-]*[a-zA-Z0-9])?))*(?::[0-9]+)?$`)
	tagRe  = regexp.MustCompile(`^[\w][\w.-]{0,127}$`)
	// digestRe is a content digest: algorithm:hex.
	digestRe = regexp.MustCompile(`^[a-z0-9]+(?:[.+_-][a-z0-9]+)*:[a-fA-F0-9]{32,}$`)
)

// ImageRef is a normalised container image reference: Host/Path[:Tag][@Digest].
//
// Normalisation applies the Docker Hub defaults: a reference without a registry host is on
// docker.io, and a single-component Docker Hub path is in the library namespace
// (nginx => docker.io/library/nginx). index.docker.io is folded into docker.io.
type ImageRef struct {
	// Host is the registry host, possibly with a port, e.g. nvcr.io or localhost:5000.
	Host string
	// Path is the repository path within the registry, e.g. nvidia/doca/doca_hbn.
	Path string
	// Tag is the tag, empty if the reference has none.
	Tag string
	// Digest is the manifest digest (e.g. sha256:...), empty if the reference has none.
	Digest string
}

// ParseImageRef parses and normalises an image reference. It fails on anything that is not a
// valid reference, e.g. a value still carrying a $VARIABLE placeholder.
func ParseImageRef(s string) (ImageRef, error) {
	orig := s
	s = strings.TrimSpace(s)
	if s == "" {
		return ImageRef{}, fmt.Errorf("empty image reference")
	}
	var ref ImageRef
	if i := strings.Index(s, "@"); i >= 0 {
		ref.Digest = s[i+1:]
		s = s[:i]
		if !digestRe.MatchString(ref.Digest) {
			return ImageRef{}, fmt.Errorf("invalid image reference %q: invalid digest %q", orig, ref.Digest)
		}
	}
	// The tag follows the last colon after the last slash; an earlier colon is a host port.
	if i := strings.LastIndex(s, ":"); i > strings.LastIndex(s, "/") {
		ref.Tag = s[i+1:]
		s = s[:i]
		if !tagRe.MatchString(ref.Tag) {
			return ImageRef{}, fmt.Errorf("invalid image reference %q: invalid tag %q", orig, ref.Tag)
		}
	}
	host, path := splitHost(s)
	if host == "index.docker.io" {
		host = DockerHubHost
	}
	if host == DockerHubHost && !strings.Contains(path, "/") {
		path = dockerHubLibrary + "/" + path
	}
	if !hostRe.MatchString(host) {
		return ImageRef{}, fmt.Errorf("invalid image reference %q: invalid registry host %q", orig, host)
	}
	if path == "" {
		return ImageRef{}, fmt.Errorf("invalid image reference %q: empty repository", orig)
	}
	for _, c := range strings.Split(path, "/") {
		if !pathComponentRe.MatchString(c) {
			return ImageRef{}, fmt.Errorf("invalid image reference %q: invalid repository path component %q", orig, c)
		}
	}
	ref.Host = host
	ref.Path = path
	return ref, nil
}

// splitHost splits a reference without tag and digest into registry host and path. The first
// component is a host when it contains a dot or a port, or is localhost; otherwise the
// reference is on Docker Hub.
func splitHost(s string) (string, string) {
	i := strings.Index(s, "/")
	if i < 0 {
		return DockerHubHost, s
	}
	first := s[:i]
	if strings.ContainsAny(first, ".:") || first == "localhost" {
		return first, s[i+1:]
	}
	return DockerHubHost, s
}

// MustParseImageRef is ParseImageRef that panics on error. For constants and tests only.
func MustParseImageRef(s string) ImageRef {
	r, err := ParseImageRef(s)
	if err != nil {
		panic(err)
	}
	return r
}

func (r ImageRef) Repository() string {
	return r.Host + "/" + r.Path
}

func (r ImageRef) String() string {
	s := r.Repository()
	if r.Tag != "" {
		s += ":" + r.Tag
	}
	if r.Digest != "" {
		s += "@" + r.Digest
	}
	return s
}

func (r ImageRef) WithoutDigest() ImageRef {
	r.Digest = ""
	return r
}
