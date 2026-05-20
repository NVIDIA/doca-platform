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
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ISO", func() {
	Context("MkIso", func() {
		It("creates an ISO file", func() {
			tmp, err := os.MkdirTemp("", "iso-test-*")
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = os.RemoveAll(tmp) }()
			isoBase := filepath.Join(tmp, "seed")
			volumeLabel := "cidata"
			files := []IsoRootFile{
				{Name: "user-data", Data: []byte("#cloud-config\nhostname: test\n")},
				{Name: "meta-data", Data: []byte{}},
				{Name: "network-config", Data: []byte("version: 2\n")},
			}

			isoPath, err := MkIso(isoBase, volumeLabel, files)
			Expect(err).NotTo(HaveOccurred())
			Expect(isoPath).To(Equal(isoBase + ".iso"))
			Expect(isoPath).To(BeAnExistingFile())
			isoData, err := os.ReadFile(isoPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(isoData).To(ContainSubstring("#cloud-config\nhostname: test\n"))
			Expect(isoData).To(ContainSubstring("meta-data"))
			Expect(isoData).To(ContainSubstring("network-config"))
		})
	})
})
