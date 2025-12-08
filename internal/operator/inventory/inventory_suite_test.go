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

package inventory

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nvidia/doca-platform/internal/release"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestMain(m *testing.M) {
	// Load defaults from the generated build file for testing
	// This runs once before all tests in this package
	defaultsFile := filepath.Join("..", "..", "release", "manifests", "defaults.yaml")
	defaultsContent, err := os.ReadFile(defaultsFile)
	if err != nil {
		panic("Failed to read defaults file (run 'make generate-manifests-release-defaults'): " + err.Error())
	}
	release.SetDefaultsContentForTesting(defaultsContent)

	// Run all tests
	os.Exit(m.Run())
}

func TestInventory(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Inventory Suite")
}
