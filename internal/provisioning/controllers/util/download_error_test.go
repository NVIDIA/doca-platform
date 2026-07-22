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
	"errors"
	"fmt"
	"net/url"
	"os"
	"syscall"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("download error classification", func() {
	DescribeTable("classifies recoverable errors",
		func(err error, recoverable bool) {
			Expect(IsRecoverableDownloadError(err)).To(Equal(recoverable))
		},
		Entry("nil", nil, false),
		Entry("HTTP status error", errors.New("failed to get: http://x/y status: 404"), false),
		Entry("unknown error", errors.New("some other error"), false),
		Entry("path error", &os.PathError{Op: "mkdir", Path: "/bfb/components", Err: syscall.ENOENT}, true),
		Entry("link error", &os.LinkError{Op: "rename", Old: "/bfb/x.tmp", New: "/bfb/x", Err: syscall.EACCES}, true),
		Entry("wrapped path error", fmt.Errorf("wrapped: %w", &os.PathError{Op: "open", Path: "/bfb/x", Err: syscall.EACCES}), true),
		Entry("URL error", &url.Error{Op: "Get", URL: "http://x", Err: errors.New("dial tcp: connection refused")}, true),
	)
})
