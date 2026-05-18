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

package sosreport

import (
	"fmt"
	"strings"
	"testing"

	. "github.com/onsi/gomega"
)

func TestDownloadFilename(t *testing.T) {
	longClusterName := "dpu-" + strings.Repeat("cluster-", 10)
	longNodeName := strings.Repeat("node-", 45) + "example.com"

	tests := []struct {
		name     string
		cluster  string
		node     string
		want     string
		wantHash bool
	}{
		{
			name:    "uses full cluster and node for short filename",
			cluster: "host",
			node:    "worker-1.example.com",
			want:    "sosreport-host-worker-1.example.com.tar.gz",
		},
		{
			name:     "truncates and hashes long filename",
			cluster:  longClusterName,
			node:     longNodeName,
			wantHash: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			got := downloadFilename(tt.cluster, tt.node)

			g.Expect(len(got)).To(BeNumerically("<=", maxDownloadFilenameLength))
			g.Expect(got).To(HaveSuffix(downloadFileSuffix))
			if tt.want != "" {
				g.Expect(got).To(Equal(tt.want))
			}
			if tt.wantHash {
				rawName := fmt.Sprintf("sosreport-%s-%s", tt.cluster, tt.node)
				g.Expect(got).To(ContainSubstring(shortHash(rawName)))
			}
		})
	}
}
