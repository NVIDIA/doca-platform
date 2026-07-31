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

package e2e

import (
	"context"
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// externalSecretsNamespace is the namespace containing External Secrets Operator.
	externalSecretsNamespace = "external-secrets"

	// encryptedSecretsTestName identifies deterministic test-owned resources.
	encryptedSecretsTestName = "dpf-e2e-encrypted-secrets"
	// encryptedSecretsReaderName identifies the test reader policy, role, and service account.
	encryptedSecretsReaderName = encryptedSecretsTestName + "-reader"
	// encryptedSecretsStoreName identifies the test ClusterSecretStore.
	encryptedSecretsStoreName = encryptedSecretsTestName + "-store"
	// encryptedSecretsRemoteKey is the KV v2 key replicated by the test.
	encryptedSecretsRemoteKey = "sample-application/credentials"
	// encryptedSecretsRefreshInterval keeps refresh validation fast.
	encryptedSecretsRefreshInterval = "5s"

	// initialUsername is the initially replicated username.
	initialUsername = "sample-user"
	// initialPassword is the initially replicated password.
	initialPassword = "sample-password"
	// updatedUsername is the username expected after refresh.
	updatedUsername = "updated-sample-user"
	// updatedPassword is the password expected after refresh.
	updatedPassword = "updated-sample-password"

	// encryptedSecretsSyncTimeout bounds Secret replication waits.
	encryptedSecretsSyncTimeout = 2 * time.Minute
	// encryptedSecretsPollingInterval controls Secret replication polling.
	encryptedSecretsPollingInterval = time.Second
)

var (
	// clusterSecretStoreGVK identifies the unstructured ClusterSecretStore.
	clusterSecretStoreGVK = schema.GroupVersionKind{
		Group:   "external-secrets.io",
		Version: "v1",
		Kind:    "ClusterSecretStore",
	}
	// externalSecretGVK identifies the unstructured ExternalSecret.
	externalSecretGVK = schema.GroupVersionKind{
		Group:   "external-secrets.io",
		Version: "v1",
		Kind:    "ExternalSecret",
	}
)

// ValidateExternalSecretsOpenBaoIntegration verifies initial replication and refresh from OpenBao.
func ValidateExternalSecretsOpenBaoIntegration(ctx context.Context, input *systemTestInput) {
	By("Creating the OpenBao test client")
	var openBao *openBaoClient
	Eventually(func(g Gomega) {
		var err error
		openBao, err = newOpenBaoClient(ctx, input.client, hostClusterRESTClient, input.restConfig)
		g.Expect(err).NotTo(HaveOccurred())
	}).WithTimeout(openBaoOperationTimeout).
		WithPolling(openBaoOperationPollingInterval).
		Should(Succeed())

	DeferCleanup(func() {
		expectExternalSecretsOpenBaoStateReset(openBao)
	})

	By("Removing OpenBao state left by previous test runs")
	expectExternalSecretsOpenBaoStateReset(openBao)

	By("Configuring the OpenBao KV mount and reader access")
	expectOpenBaoOperation(func() error {
		return openBao.enableKVv2(encryptedSecretsTestName)
	})
	expectOpenBaoOperation(func() error {
		return openBao.writeReaderPolicy(
			encryptedSecretsReaderName,
			encryptedSecretsTestName,
			encryptedSecretsRemoteKey,
		)
	})
	expectOpenBaoOperation(func() error {
		return openBao.writeKubernetesRole(
			encryptedSecretsReaderName,
			encryptedSecretsReaderName,
			externalSecretsNamespace,
			encryptedSecretsReaderName,
		)
	})

	By("Writing initial secret data to OpenBao")
	expectOpenBaoOperation(func() error {
		return openBao.putKV(
			encryptedSecretsTestName,
			encryptedSecretsRemoteKey,
			initialUsername,
			initialPassword,
		)
	})

	By("Creating the External Secrets resources")
	createEncryptedSecretsTestResources(ctx, input.client)

	By("Waiting for the initial OpenBao data to be replicated")
	expectReplicatedSecretData(ctx, input.client, initialUsername, initialPassword)

	By("Updating the secret data in OpenBao")
	expectOpenBaoOperation(func() error {
		return openBao.putKV(
			encryptedSecretsTestName,
			encryptedSecretsRemoteKey,
			updatedUsername,
			updatedPassword,
		)
	})

	By("Waiting for the updated OpenBao data to be replicated")
	expectReplicatedSecretData(ctx, input.client, updatedUsername, updatedPassword)
}

// expectExternalSecretsOpenBaoStateReset retries removal of test-owned OpenBao state.
func expectExternalSecretsOpenBaoStateReset(openBao *openBaoClient) {
	Eventually(func(g Gomega) {
		g.Expect(resetExternalSecretsOpenBaoTestState(openBao)).To(Succeed())
	}).WithTimeout(openBaoOperationTimeout).
		WithPolling(openBaoOperationPollingInterval).
		Should(Succeed())
}

// createEncryptedSecretsTestResources creates the Kubernetes resources for the workflow.
func createEncryptedSecretsTestResources(ctx context.Context, c client.Client) {
	namespace := encryptedSecretsTestNamespace()
	Expect(c.Create(ctx, namespace)).To(Succeed())

	readerServiceAccount := encryptedSecretsReaderServiceAccount()
	Expect(c.Create(ctx, readerServiceAccount)).To(Succeed())

	store := encryptedSecretsClusterSecretStore()
	Expect(c.Create(ctx, store)).To(Succeed())

	externalSecret := encryptedSecretsExternalSecret()
	Expect(c.Create(ctx, externalSecret)).To(Succeed())
}

// expectReplicatedSecretData waits for the expected values in the generated Secret.
func expectReplicatedSecretData(ctx context.Context, c client.Client, username, password string) {
	Eventually(func(g Gomega) {
		secret := &corev1.Secret{}
		g.Expect(c.Get(ctx, client.ObjectKey{
			Namespace: encryptedSecretsTestName,
			Name:      encryptedSecretsTestName,
		}, secret)).To(Succeed())
		g.Expect(string(secret.Data["username"])).To(Equal(username))
		g.Expect(string(secret.Data["password"])).To(Equal(password))
	}).WithTimeout(encryptedSecretsSyncTimeout).
		WithPolling(encryptedSecretsPollingInterval).
		Should(Succeed())
}

// resetExternalSecretsOpenBaoTestState removes all test-owned OpenBao configuration and data.
func resetExternalSecretsOpenBaoTestState(openBao *openBaoClient) error {
	var errs []error

	if err := openBao.deleteKubernetesRole(encryptedSecretsReaderName); err != nil {
		errs = append(errs, err)
	}
	if err := openBao.deletePolicy(encryptedSecretsReaderName); err != nil {
		errs = append(errs, err)
	}
	if err := openBao.disableMountIfExists(encryptedSecretsTestName); err != nil {
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}

// encryptedSecretsTestNamespace returns the test namespace.
func encryptedSecretsTestNamespace() *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   encryptedSecretsTestName,
			Labels: CleanupScope.It,
		},
	}
}

// encryptedSecretsReaderServiceAccount returns the OpenBao reader service account.
func encryptedSecretsReaderServiceAccount() *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      encryptedSecretsReaderName,
			Namespace: externalSecretsNamespace,
			Labels:    CleanupScope.It,
		},
	}
}

// encryptedSecretsClusterSecretStore returns the OpenBao-backed ClusterSecretStore.
func encryptedSecretsClusterSecretStore() *unstructured.Unstructured {
	store := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"spec": map[string]interface{}{
				"provider": map[string]interface{}{
					"vault": map[string]interface{}{
						"server":  "https://openbao.openbao.svc.cluster.local:8200",
						"path":    encryptedSecretsTestName,
						"version": "v2",
						"caProvider": map[string]interface{}{
							"type":      "ConfigMap",
							"namespace": externalSecretsNamespace,
							"name":      openBaoCAConfigMapName,
							"key":       openBaoCAConfigMapKey,
						},
						"auth": map[string]interface{}{
							"kubernetes": map[string]interface{}{
								"mountPath": "kubernetes",
								"role":      encryptedSecretsReaderName,
								"serviceAccountRef": map[string]interface{}{
									"name":      encryptedSecretsReaderName,
									"namespace": externalSecretsNamespace,
								},
							},
						},
					},
				},
			},
		},
	}
	store.SetGroupVersionKind(clusterSecretStoreGVK)
	store.SetName(encryptedSecretsStoreName)
	store.SetLabels(CleanupScope.It)
	return store
}

// encryptedSecretsClusterSecretStoreList returns a list type for e2e cleanup.
func encryptedSecretsClusterSecretStoreList() *unstructured.UnstructuredList {
	storeList := &unstructured.UnstructuredList{}
	storeList.SetGroupVersionKind(clusterSecretStoreGVK.GroupVersion().WithKind(clusterSecretStoreGVK.Kind + "List"))
	return storeList
}

// encryptedSecretsExternalSecret returns the ExternalSecret under test.
func encryptedSecretsExternalSecret() *unstructured.Unstructured {
	externalSecret := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"spec": map[string]interface{}{
				"refreshInterval": encryptedSecretsRefreshInterval,
				"secretStoreRef": map[string]interface{}{
					"name": encryptedSecretsStoreName,
					"kind": "ClusterSecretStore",
				},
				"target": map[string]interface{}{
					"name":           encryptedSecretsTestName,
					"creationPolicy": "Owner",
				},
				"data": []interface{}{
					map[string]interface{}{
						"secretKey": "username",
						"remoteRef": map[string]interface{}{
							"key":      encryptedSecretsRemoteKey,
							"property": "username",
						},
					},
					map[string]interface{}{
						"secretKey": "password",
						"remoteRef": map[string]interface{}{
							"key":      encryptedSecretsRemoteKey,
							"property": "password",
						},
					},
				},
			},
		},
	}
	externalSecret.SetGroupVersionKind(externalSecretGVK)
	externalSecret.SetName(encryptedSecretsTestName)
	externalSecret.SetNamespace(encryptedSecretsTestName)
	externalSecret.SetLabels(CleanupScope.It)
	return externalSecret
}

// encryptedSecretsExternalSecretList returns a list type for e2e cleanup.
func encryptedSecretsExternalSecretList() *unstructured.UnstructuredList {
	externalSecretList := &unstructured.UnstructuredList{}
	externalSecretList.SetGroupVersionKind(externalSecretGVK.GroupVersion().WithKind(externalSecretGVK.Kind + "List"))
	return externalSecretList
}
