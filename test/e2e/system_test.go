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

package e2e

import (
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	"github.com/nvidia/doca-platform/test/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

//nolint:dupl
var _ = Describe("DPF System tests", Ordered, func() {
	// The operatorConfig for the test.
	dpfOperatorConfig := &operatorv1.DPFOperatorConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configName,
			Namespace: dpfOperatorSystemNamespace,
			Labels:    cleanupLabels,
		},
		Spec: operatorv1.DPFOperatorConfigSpec{
			ProvisioningController: operatorv1.ProvisioningControllerConfiguration{
				BFBPersistentVolumeClaimName: "bfb-pvc",
			},
			StaticClusterManager: &operatorv1.StaticClusterManagerConfiguration{
				Disable: ptr.To(false),
			},
			// Disable the Kamaji cluster manager so only one cluster manager is running.
			// TODO: Enable Kamaji by default in the e2e tests.
			KamajiClusterManager: &operatorv1.KamajiClusterManagerConfiguration{
				Disable: ptr.To(true),
			},
			ImagePullSecrets: []string{"dpf-pull-secret", "pull-secret-extra"},
		},
	}

	Context("DPF Operator initialization", func() {
		BeforeAll(func() {
			By("cleaning up objects created during recent tests", func() {
				Expect(utils.CleanupWithLabelAndWait(ctx, testClient, labelSelector, resourcesToDelete...)).To(Succeed())
			})
		})

		AfterAll(func() {
			By("collecting resources and logs for the clusters")
			input := collectResourcesInput{
				collectResources: collectResources,
				testClient:       testClient,
				clientset:        clientset,
			}
			err := collectResourcesAndLogs(ctx, input)
			if err != nil {
				// Don't fail the test if the log collector fails - just print the errors.
				GinkgoLogr.Error(err, "failed to collect resources and logs for the clusters")
			}
			if skipCleanup {
				return
			}
			By("cleaning up objects created during the test", func() {
				Expect(utils.CleanupWithLabelAndWait(ctx, testClient, labelSelector, resourcesToDelete...)).To(Succeed())
			})
		})

		input := systemTestInput{
			namespace:       dpfOperatorSystemNamespace,
			config:          dpfOperatorConfig,
			pullSecretNames: dpfOperatorConfig.Spec.ImagePullSecrets,
			client:          testClient,
			skipCleanup:     skipCleanup,
			bfbImageURL:     bfbImageURL,
		}
		input.applyConfig(*conf)

		tests := []dpfTest{
			ValidateDPFOperatorConfiguration,
			VerifyKSMMetricsCollection,
			ValidateDPUService,
			ValidateDPUDeployment,
			ValidateDPUServiceIPAM,
			ValidateDPUServiceChain,
			ValidateDPUServiceCredentialRequest,
			ValidateDPUServiceTemplate,
			ValidateDPUServiceConfigPorts,
			ValidateGeneralDPFMetrics,
			// Note that this test triggers reprovisioning. If we move to parallel tests this might break other tests
			// since we don't wait for the nodes to be reprovisioned (to not introduce more latency).
			// TODO(tdvorianchenko): Could you please advise? I think we need a phase before doing the actual
			// provisioning to test how things look like when we change various settings in DPFOperatorConfig.
			ValidateDPFOperatorKubernetesAPIServerVIPAndPort,
			ValidateOperatorCleanup,
		}

		// Run the test spec.
		DPFSystemTest(ctx, input, tests)
	})
})
