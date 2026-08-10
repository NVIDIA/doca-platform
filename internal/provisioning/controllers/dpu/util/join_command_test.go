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

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/test/utils"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestGenerateJoinCommand(t *testing.T) {
	g := NewWithT(t)

	// Setup test resources
	namespace := "default"
	dpuCluster := utils.GetTestDPUCluster(namespace, "test-cluster")
	secret, err := utils.GetFakeKamajiClusterSecretFromEnvtest(dpuCluster, cfg)
	g.Expect(err).NotTo(HaveOccurred())

	dpu := &provisioningv1.DPU{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-dpu",
			Namespace: "dpf-operator-system",
		},
	}

	// Create the DPUCluster and its secret
	g.Expect(testClient.Create(ctx, &dpuCluster)).To(Succeed())
	g.Expect(testClient.Create(ctx, secret)).To(Succeed())

	// Cleanup after test
	defer func() {
		g.Expect(utils.CleanupAndWait(ctx, testClient, &dpuCluster)).To(Succeed())
		g.Expect(utils.CleanupAndWait(ctx, testClient, secret)).To(Succeed())
	}()

	t.Run("valid join command generation", func(t *testing.T) {
		g := NewWithT(t)
		generator := &KubeadmBootstrapTokenGenerator{testClient}
		cmd, err := generator.GenerateJoinCommand(ctx, &dpuCluster, dpu)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(cmd).NotTo(BeEmpty())

		secretList := &corev1.SecretList{}
		g.Expect(testClient.List(ctx, secretList,
			client.InNamespace("kube-system"),
			client.MatchingLabels{
				cutil.LabelDPUName:      dpu.Name,
				cutil.LabelDPUNamespace: dpu.Namespace,
			},
		)).To(Succeed())
		g.Expect(secretList.Items).To(HaveLen(1))
		g.Expect(secretList.Items[0].Type).To(Equal(corev1.SecretTypeBootstrapToken))

		g.Expect(DeleteNodeJoinBootstrapTokens(ctx, testClient, dpu.Name, dpu.Namespace)).To(Succeed())
		g.Expect(testClient.List(ctx, secretList,
			client.InNamespace("kube-system"),
			client.MatchingLabels{
				cutil.LabelDPUName:      dpu.Name,
				cutil.LabelDPUNamespace: dpu.Namespace,
			},
		)).To(Succeed())
		g.Expect(secretList.Items).To(BeEmpty())
	})

	t.Run("skips non-bootstrap secrets with the same labels", func(t *testing.T) {
		g := NewWithT(t)
		generator := &KubeadmBootstrapTokenGenerator{testClient}
		_, err := generator.GenerateJoinCommand(ctx, &dpuCluster, dpu)
		g.Expect(err).NotTo(HaveOccurred())

		other := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "other-secret-with-dpu-labels",
				Namespace: "kube-system",
				Labels: map[string]string{
					cutil.LabelDPUName:      dpu.Name,
					cutil.LabelDPUNamespace: dpu.Namespace,
				},
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{"key": []byte("value")},
		}
		g.Expect(testClient.Create(ctx, other)).To(Succeed())
		defer func() {
			_ = testClient.Delete(ctx, other)
		}()

		g.Expect(DeleteNodeJoinBootstrapTokens(ctx, testClient, dpu.Name, dpu.Namespace)).To(Succeed())

		secretList := &corev1.SecretList{}
		g.Expect(testClient.List(ctx, secretList,
			client.InNamespace("kube-system"),
			client.MatchingLabels{
				cutil.LabelDPUName:      dpu.Name,
				cutil.LabelDPUNamespace: dpu.Namespace,
			},
		)).To(Succeed())
		g.Expect(secretList.Items).To(HaveLen(1))
		g.Expect(secretList.Items[0].Name).To(Equal(other.Name))
		g.Expect(secretList.Items[0].Type).To(Equal(corev1.SecretTypeOpaque))
	})
}
