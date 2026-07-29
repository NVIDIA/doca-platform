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

package utils

import (
	"os"
	"testing"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	testutils "github.com/nvidia/doca-platform/test/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

var _ = Describe("chartutils", Ordered, func() {
	Context("testing the pull secret parsing", func() {
		var testNS *corev1.Namespace
		var config *operatorv1.DPFOperatorConfig
		BeforeAll(func() {
			By("Creating the namespace")
			testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "somens"}}
			Expect(testClient.Create(ctx, testNS)).To(Succeed())
			DeferCleanup(testClient.Delete, ctx, testNS)
		})
		BeforeEach(func() {
			config = &operatorv1.DPFOperatorConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "config",
					Namespace: testNS.Name,
				},
				Spec: operatorv1.DPFOperatorConfigSpec{
					DeploymentMode: operatorv1.DeploymentModeHostTrusted,
					ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{
						BFBPersistentVolumeClaimName: ptr.To("some"),
					},
				},
			}
			// Ensure we try to delete it.
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, config)
		})
		It("parses a valid secret with helm registry correctly", func() {
			Expect(testClient.Create(ctx, config)).To(Succeed())

			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dpu1",
					Namespace: testNS.Name,
					Labels: map[string]string{
						"argocd.argoproj.io/secret-type": "repository",
					},
				},
				StringData: map[string]string{
					"type":     "helm",
					"url":      "https://example.com",
					"username": "myuser",
					"password": "mypassword",
				},
			}
			Expect(testClient.Create(ctx, secret)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, secret)

			username, password, err := getChartPullCredentials(
				ctx,
				testClient,
				dpuservicev1.ApplicationSource{
					RepoURL: "https://example.com",
				})
			Expect(err).ToNot(HaveOccurred())
			Expect(username).To(Equal("myuser"))
			Expect(password).To(Equal("mypassword"))
		})
		It("parses a valid secret with oci registry correctly", func() {
			Expect(testClient.Create(ctx, config)).To(Succeed())

			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dpu1",
					Namespace: testNS.Name,
					Labels: map[string]string{
						"argocd.argoproj.io/secret-type": "repository",
					},
				},
				StringData: map[string]string{
					"type":     "helm",
					"url":      "example.com",
					"username": "myuser",
					"password": "mypassword",
				},
			}
			Expect(testClient.Create(ctx, secret)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, secret)

			username, password, err := getChartPullCredentials(
				ctx,
				testClient,
				dpuservicev1.ApplicationSource{
					RepoURL: "oci://example.com",
				})
			Expect(err).ToNot(HaveOccurred())
			Expect(username).To(Equal("myuser"))
			Expect(password).To(Equal("mypassword"))
		})
		It("matches a secret whose url differs from the source only by a trailing slash", func() {
			Expect(testClient.Create(ctx, config)).To(Succeed())

			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "trailing-slash-in-secret",
					Namespace: testNS.Name,
					Labels: map[string]string{
						"argocd.argoproj.io/secret-type": "repository",
					},
				},
				StringData: map[string]string{
					"type":     "helm",
					"url":      "https://example.com/org/repo/",
					"username": "myuser",
					"password": "mypassword",
				},
			}
			Expect(testClient.Create(ctx, secret)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, secret)

			username, password, err := getChartPullCredentials(
				ctx,
				testClient,
				dpuservicev1.ApplicationSource{
					RepoURL: "https://example.com/org/repo",
				})
			Expect(err).ToNot(HaveOccurred())
			Expect(username).To(Equal("myuser"))
			Expect(password).To(Equal("mypassword"))
		})
		It("matches a secret when the source has the trailing slash and the secret does not", func() {
			Expect(testClient.Create(ctx, config)).To(Succeed())

			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "trailing-slash-in-source",
					Namespace: testNS.Name,
					Labels: map[string]string{
						"argocd.argoproj.io/secret-type": "repository",
					},
				},
				StringData: map[string]string{
					"type":     "helm",
					"url":      "example.com/org/repo",
					"username": "myuser",
					"password": "mypassword",
				},
			}
			Expect(testClient.Create(ctx, secret)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, secret)

			username, password, err := getChartPullCredentials(
				ctx,
				testClient,
				dpuservicev1.ApplicationSource{
					RepoURL: "oci://example.com/org/repo/",
				})
			Expect(err).ToNot(HaveOccurred())
			Expect(username).To(Equal("myuser"))
			Expect(password).To(Equal("mypassword"))
		})
		It("does not match a secret for a different repository", func() {
			Expect(testClient.Create(ctx, config)).To(Succeed())

			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "other-repo",
					Namespace: testNS.Name,
					Labels: map[string]string{
						"argocd.argoproj.io/secret-type": "repository",
					},
				},
				StringData: map[string]string{
					"type":     "helm",
					"url":      "https://example.com/org/other",
					"username": "myuser",
					"password": "mypassword",
				},
			}
			Expect(testClient.Create(ctx, secret)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, secret)

			username, password, err := getChartPullCredentials(
				ctx,
				testClient,
				dpuservicev1.ApplicationSource{
					RepoURL: "https://example.com/org/repo",
				})
			Expect(err).ToNot(HaveOccurred())
			Expect(username).To(BeEmpty())
			Expect(password).To(BeEmpty())
		})
		It("reads secrets from the ArgoCD namespace override rather than the DPFOperatorConfig namespace", func() {
			By("Creating a separate namespace for ArgoCD")
			argoCDNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "argocd"}}
			Expect(testClient.Create(ctx, argoCDNS)).To(Succeed())
			DeferCleanup(testClient.Delete, ctx, argoCDNS)

			config.Spec.Overrides = &operatorv1.Overrides{
				ArgoCDNamespace: ptr.To(argoCDNS.Name),
			}

			Expect(testClient.Create(ctx, config)).To(Succeed())

			By("Creating a matching secret in the DPFOperatorConfig namespace that must be ignored")
			wrongSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "wrong-ns",
					Namespace: testNS.Name,
					Labels: map[string]string{
						"argocd.argoproj.io/secret-type": "repository",
					},
				},
				StringData: map[string]string{
					"type":     "helm",
					"url":      "https://example.com",
					"username": "wronguser",
					"password": "wrongpassword",
				},
			}
			Expect(testClient.Create(ctx, wrongSecret)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, wrongSecret)

			By("Creating the expected secret in the ArgoCD namespace")
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "argocd-secret",
					Namespace: argoCDNS.Name,
					Labels: map[string]string{
						"argocd.argoproj.io/secret-type": "repository",
					},
				},
				StringData: map[string]string{
					"type":     "helm",
					"url":      "https://example.com",
					"username": "argouser",
					"password": "argopassword",
				},
			}
			Expect(testClient.Create(ctx, secret)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, secret)

			username, password, err := getChartPullCredentials(
				ctx,
				testClient,
				dpuservicev1.ApplicationSource{
					RepoURL: "https://example.com",
				})
			Expect(err).ToNot(HaveOccurred())
			Expect(username).To(Equal("argouser"))
			Expect(password).To(Equal("argopassword"))
		})
	})
})

// TODO: these tests are best effort and not really designed to run in each test run.
// They should be replaced in future with e2e tests once we have charts with the annotation in the
// desired registries.
func Test_chartHelper_GetAnnotationsFromChart(t *testing.T) {
	g := NewWithT(t)
	// TODO: create secrets and call the full method which also reads from secrets.
	NGCAPIKey := os.Getenv("NGC_API_KEY_FOR_HELM_CHART_TEST")
	if NGCAPIKey == "" {
		t.Skip("Skipping helm registry retrieval tests." +
			"These tests will run if the environment variable NGC_API_KEY_FOR_HELM_CHART_TEST is set")
	}
	tests := []struct {
		name     string
		source   dpuservicev1.ApplicationSource
		user     string
		password string
		wantErr  bool
	}{
		{
			name:    "get from private Helm registry with username and password set.",
			wantErr: false,
			source: dpuservicev1.ApplicationSource{
				RepoURL: "https://helm.ngc.nvidia.com/nvstaging/doca/",
				Chart:   "doca-telemetry",
				Version: "0.2.8",
			},
			user:     "$oauthtoken",
			password: NGCAPIKey,
		},
		{
			name: "fail to get from private helm registry with username and password set",
			// Expect an error as we can't access the repository without credentials.
			wantErr: true,
			source: dpuservicev1.ApplicationSource{
				RepoURL: "https://helm.ngc.nvidia.com/nvstaging/doca/",
				Chart:   "doca-telemetry",
				Version: "0.2.8",
			},
			user:     "",
			password: "",
		},
		{
			name:    "get from public helm registry with username and password set",
			wantErr: false,
			source: dpuservicev1.ApplicationSource{
				RepoURL: "https://helm.ngc.nvidia.com/nvidia/doca/",
				Chart:   "doca-telemetry",
				Version: "0.2.8",
			},
			user:     "$oauthtoken",
			password: NGCAPIKey,
		},
		{
			name:    "get from public helm registry with username but no password set",
			wantErr: false,
			source: dpuservicev1.ApplicationSource{
				RepoURL: "https://helm.ngc.nvidia.com/nvidia/doca/",
				Chart:   "doca-telemetry",
				Version: "0.2.8",
			},
			user:     "$oauthtoken",
			password: "",
		},
		{
			name:    "get from public helm registry with no password set",
			wantErr: false,
			source: dpuservicev1.ApplicationSource{
				RepoURL: "https://helm.ngc.nvidia.com/nvidia/doca/",
				Chart:   "doca-telemetry",
				Version: "0.2.8",
			},
			user:     "",
			password: "",
		},
		{
			name:    "get from private OCI registry with username and password set.",
			wantErr: false,
			source: dpuservicev1.ApplicationSource{
				RepoURL: "oci://nvcr.io/nvstaging/doca",
				Chart:   "dpf-operator",
				Version: "v25.4.0-latest",
			},
			user:     "$oauthtoken",
			password: NGCAPIKey,
		},
		{
			name:    "fail to get from private OCI registry with authentication and no password set.",
			wantErr: true,
			source: dpuservicev1.ApplicationSource{
				RepoURL: "oci://nvcr.io/nvstaging/doca",
				Chart:   "dpf-operator",
				Version: "v25.4.0-latest",
			},
			user:     "",
			password: "",
		},
		{
			name:    "get from public OCI registry with username and password set.",
			wantErr: false,
			source: dpuservicev1.ApplicationSource{
				RepoURL: "oci://nvcr.io/nvidia/cloud-native",
				Chart:   "network-operator",
				Version: "v25.1.0",
			},
			user:     "$oauthtoken",
			password: NGCAPIKey,
		},
		{
			name:    "get from public OCI registry with no or username password set",
			wantErr: false,
			source: dpuservicev1.ApplicationSource{
				RepoURL: "oci://nvcr.io/nvidia/cloud-native",
				Chart:   "network-operator",
				Version: "v25.1.0",
			},
			user:     "",
			password: "",
		},
		{
			name:    "get from public OCI registry with username but no password set",
			wantErr: false,
			source: dpuservicev1.ApplicationSource{
				RepoURL: "oci://nvcr.io/nvidia/cloud-native",
				Chart:   "network-operator",
				Version: "v25.1.0",
			},
			user:     "$oauthtoken",
			password: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := getAnnotationsForChart(ctx, tt.source, tt.user, tt.password)
			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
				return
			}
			g.Expect(err).NotTo(HaveOccurred())
		})
	}
}
