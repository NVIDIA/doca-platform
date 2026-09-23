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

// Package artifact downloads and parses the artifacts the provisioning controller sends to a BMC:
// the BFB + bf.cfg concat stream (BF3), the OS ISO and cidata seed.iso (BF4) and the PLDM
// firmware bundle (BF4), plus the cloud-init user-data both DPU types carry.
package artifact

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ProgressFunc receives the number of bytes downloaded so far and the Content-Length (-1 when
// unknown). It is called at most once per read chunk.
type ProgressFunc func(done, total int64)

// Download streams the content behind imageURI into sink, exactly like a real BMC pulls a
// SimpleUpdate ImageURI: HTTPS, self-signed bfb-registry certificate accepted, full body read.
// imageURI may omit the scheme (the controller passes "host:port/path"). Returns the byte count.
func Download(ctx context.Context, imageURI string, sink io.Writer, progress ProgressFunc) (int64, error) {
	if !strings.Contains(imageURI, "://") {
		imageURI = "https://" + imageURI
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURI, nil)
	if err != nil {
		return 0, fmt.Errorf("build request for %s: %w", imageURI, err)
	}
	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12}, //nolint:gosec // bfb-registry is self-signed, like a real BMC we do not verify it.
		ResponseHeaderTimeout: 2 * time.Minute,
		Proxy:                 http.ProxyFromEnvironment,
	}}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("download %s: %w", imageURI, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("download %s: unexpected status %s", imageURI, resp.Status)
	}
	total := resp.ContentLength
	var done int64
	buf := make([]byte, 1<<20)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := sink.Write(buf[:n]); err != nil {
				return done, fmt.Errorf("consume %s: %w", imageURI, err)
			}
			done += int64(n)
			if progress != nil {
				progress(done, total)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return done, fmt.Errorf("download %s: %w", imageURI, readErr)
		}
	}
	if total >= 0 && done != total {
		return done, fmt.Errorf("download %s: got %d bytes, Content-Length %d", imageURI, done, total)
	}
	return done, nil
}

// LimitedBuffer collects up to max bytes in memory and fails on overflow. It is the sink for
// small artifacts (bf.cfg trailer, seed.iso) whose size a real BMC would also bound.
type LimitedBuffer struct {
	max  int
	data []byte
}

// NewLimitedBuffer returns a buffer that accepts at most max bytes.
func NewLimitedBuffer(max int) *LimitedBuffer {
	return &LimitedBuffer{max: max}
}

func (b *LimitedBuffer) Write(p []byte) (int, error) {
	if len(b.data)+len(p) > b.max {
		return 0, fmt.Errorf("artifact exceeds %d bytes", b.max)
	}
	b.data = append(b.data, p...)
	return len(p), nil
}

// Bytes returns the collected content.
func (b *LimitedBuffer) Bytes() []byte {
	return b.data
}
