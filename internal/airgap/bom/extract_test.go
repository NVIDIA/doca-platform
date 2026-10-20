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
	"os"
	"testing"

	. "github.com/onsi/gomega"
)

func TestImagesFromManifests(t *testing.T) {
	g := NewWithT(t)
	data, err := os.ReadFile("testdata/manifests/workloads.yaml")
	g.Expect(err).NotTo(HaveOccurred())

	found, err := ImagesFromManifests(data)
	g.Expect(err).NotTo(HaveOccurred())

	type result struct {
		Raw    string
		Strict bool
	}
	got := make([]result, 0, len(found))
	for _, f := range found {
		got = append(got, result{f.Raw, f.Strict})
	}
	g.Expect(got).To(ConsistOf(
		result{"example.com/dpf-system:v0.1.0", true},
		result{"busybox:1.36", true},
		result{"example.com/hostdriver:v0.1.0", false},
		result{"example.com/bfb-registry:v0.1.0", false},
		result{"ghcr.io/ahrtr/etcd-defrag:v0.22.0@sha256:a7424de0a437f54d7565e96f8a50913e0ace05398bb5c10a77d5af5fc9bf9301", true},
		result{"quay.io/example/widget:2.0", false},
		result{"not a reference", false},
		// Any --*image= flag is a candidate; Engine.Images drops bare words and unparsable values.
		result{"foo", false},
		result{"nginx", true},
		result{"registry.k8s.io/pause:3.10", true},
	))

	for _, f := range found {
		if f.Raw == "busybox:1.36" {
			g.Expect(f.Object).To(Equal("Deployment/system/controller"))
			g.Expect(f.Field).To(Equal(".spec.template.spec.initContainers[0].image"))
			g.Expect(f.String()).To(ContainSubstring("Deployment/system/controller"))
		}
	}
}
