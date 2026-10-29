/*
Copyright 2025 NVIDIA

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
	"testing"

	"github.com/nvidia/doca-platform/test/utils"

	. "github.com/onsi/gomega"
)

func TestGenerateJoinCommand(t *testing.T) {
	g := NewWithT(t)

	// Setup test resources
	namespace := "default"
	dpuCluster := utils.GetTestDPUCluster(namespace, "test-cluster")
	secret, err := utils.GetFakeKamajiClusterSecretFromEnvtest(dpuCluster, cfg)
	g.Expect(err).NotTo(HaveOccurred())

	// Create the DPUCluster and its secret
	g.Expect(testClient.Create(ctx, &dpuCluster)).To(Succeed())
	g.Expect(testClient.Create(ctx, secret)).To(Succeed())

	// Cleanup after test
	defer func() {
		g.Expect(utils.CleanupAndWait(ctx, testClient, &dpuCluster)).To(Succeed())
		g.Expect(utils.CleanupAndWait(ctx, testClient, secret)).To(Succeed())
	}()

	t.Run("valid join command generation", func(t *testing.T) {
		generator := &KubeadmBootstrapTokenGenerator{testClient}
		cmd, err := generator.GenerateJoinCommand(ctx, &dpuCluster)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(cmd).NotTo(BeEmpty())
	})
}
