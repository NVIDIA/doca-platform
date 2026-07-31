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
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nvidia/doca-platform/test/utils/netshoot"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// openBaoNamespace is the namespace containing the OpenBao deployment.
	openBaoNamespace = "openbao"
	// openBaoPodName is the OpenBao pod used for bao commands.
	openBaoPodName = "openbao-0"
	// openBaoRootTokenSecretName is the bootstrap root token Secret.
	openBaoRootTokenSecretName = "openbao-root-token"

	// openBaoCAConfigMapName is the OpenBao CA ConfigMap copied by Helmfile.
	openBaoCAConfigMapName = "openbao-ca"
	// openBaoCAConfigMapKey is the CA certificate key in the ConfigMap.
	openBaoCAConfigMapKey = "ca.crt"

	// openBaoOperationTimeout bounds retries of OpenBao operations.
	openBaoOperationTimeout = time.Minute
	// openBaoOperationPollingInterval controls OpenBao retry frequency.
	openBaoOperationPollingInterval = 2 * time.Second
)

// openBaoClient runs authenticated bao commands through Kubernetes pod exec.
type openBaoClient struct {
	restClient *rest.RESTClient
	restConfig *rest.Config
	rootToken  string
}

// openBaoSecretMount contains the fields needed to identify an OpenBao mount.
type openBaoSecretMount struct {
	Type    string            `json:"type"`
	Options map[string]string `json:"options"`
}

// newOpenBaoClient reads the root token and returns a pod-exec-backed client.
func newOpenBaoClient(ctx context.Context, c client.Client, restClient *rest.RESTClient, restConfig *rest.Config) (*openBaoClient, error) {
	rootTokenSecret := &corev1.Secret{}
	if err := c.Get(ctx, client.ObjectKey{
		Namespace: openBaoNamespace,
		Name:      openBaoRootTokenSecretName,
	}, rootTokenSecret); err != nil {
		return nil, fmt.Errorf("get OpenBao root token Secret: %w", err)
	}

	rootToken, ok := rootTokenSecret.Data["token"]
	if !ok || len(rootToken) == 0 {
		return nil, fmt.Errorf("OpenBao root token Secret %s/%s has no token", openBaoNamespace, openBaoRootTokenSecretName)
	}

	return &openBaoClient{
		restClient: restClient,
		restConfig: restConfig,
		rootToken:  string(rootToken),
	}, nil
}

// execute runs one authenticated bao command in the OpenBao pod.
func (c *openBaoClient) execute(args ...string) (string, error) {
	command := append([]string{
		"sh",
		"-c",
		`BAO_TOKEN="$(cat)" "$@"`,
		"_",
		"bao",
	}, args...)

	return netshoot.ExecInPodOnceWithStdin(
		c.restClient,
		c.restConfig,
		openBaoNamespace,
		openBaoPodName,
		command,
		strings.NewReader(c.rootToken),
	)
}

// enableKVv2 ensures the mount is configured as a KV v2 secrets engine.
func (c *openBaoClient) enableKVv2(mount string) error {
	mounts, err := c.listSecretMounts()
	if err != nil {
		return err
	}

	existingMount, exists := mounts[mount+"/"]
	if exists {
		if existingMount.Type != "kv" || existingMount.Options["version"] != "2" {
			return fmt.Errorf("mount %q already exists but is not KV v2", mount)
		}
		return nil
	}

	output, err := c.execute("secrets", "enable", "-path="+mount, "kv-v2")
	return openBaoCommandError("enable KV v2 mount", output, err)
}

// enableTransit ensures the mount is configured as a Transit secrets engine.
func (c *openBaoClient) enableTransit(mount string) error {
	mounts, err := c.listSecretMounts()
	if err != nil {
		return err
	}

	existingMount, exists := mounts[mount+"/"]
	if exists {
		if existingMount.Type != "transit" {
			return fmt.Errorf("mount %q already exists but is not Transit", mount)
		}
		return nil
	}

	output, err := c.execute("secrets", "enable", "-path="+mount, "transit")
	return openBaoCommandError("enable Transit mount", output, err)
}

// createTransitKey ensures the Transit key exists.
func (c *openBaoClient) createTransitKey(mount, keyName string) error {
	output, err := c.execute("write", "-f", mount+"/keys/"+keyName)
	return openBaoCommandError("create Transit key", output, err)
}

// writeReaderPolicy grants read access to one KV v2 key.
func (c *openBaoClient) writeReaderPolicy(policyName, mount, remoteKey string) error {
	policy := fmt.Sprintf(`path "%s/data/%s" {
  capabilities = ["read"]
}`, mount, remoteKey)

	output, err := c.execute(
		"write",
		"sys/policies/acl/"+policyName,
		"policy="+policy,
	)
	return openBaoCommandError("write reader policy", output, err)
}

// writeVaultKMSPolicy grants encrypt and decrypt access to one Transit key.
func (c *openBaoClient) writeVaultKMSPolicy(policyName, mount, keyName string) error {
	policy := fmt.Sprintf(`path "%s/encrypt/%s" {
  capabilities = ["update"]
}

path "%s/decrypt/%s" {
  capabilities = ["update"]
}`, mount, keyName, mount, keyName)

	output, err := c.execute(
		"write",
		"sys/policies/acl/"+policyName,
		"policy="+policy,
	)
	return openBaoCommandError("write Vault KMS policy", output, err)
}

// writeKubernetesRole binds a service account to a policy.
func (c *openBaoClient) writeKubernetesRole(roleName, serviceAccountName, serviceAccountNamespace, policyName string) error {
	output, err := c.execute(
		"write",
		"auth/kubernetes/role/"+roleName,
		"bound_service_account_names="+serviceAccountName,
		"bound_service_account_namespaces="+serviceAccountNamespace,
		"policies="+policyName,
		"ttl=1h",
	)
	return openBaoCommandError("write Kubernetes auth role", output, err)
}

// putKV writes credentials to a KV v2 key.
func (c *openBaoClient) putKV(mount, remoteKey, username, password string) error {
	output, err := c.execute(
		"kv",
		"put",
		mount+"/"+remoteKey,
		"username="+username,
		"password="+password,
	)
	return openBaoCommandError("write KV data", output, err)
}

// deleteKubernetesRole removes a Kubernetes auth role.
func (c *openBaoClient) deleteKubernetesRole(roleName string) error {
	output, err := c.execute("delete", "auth/kubernetes/role/"+roleName)
	return openBaoCommandError("delete Kubernetes auth role", output, err)
}

// deletePolicy removes a policy.
func (c *openBaoClient) deletePolicy(policyName string) error {
	output, err := c.execute("delete", "sys/policies/acl/"+policyName)
	return openBaoCommandError("delete policy", output, err)
}

// listSecretMounts returns the configured OpenBao secrets mounts.
func (c *openBaoClient) listSecretMounts() (map[string]openBaoSecretMount, error) {
	output, err := c.execute("secrets", "list", "-format=json")
	if err != nil {
		return nil, openBaoCommandError("list secret mounts", output, err)
	}

	mounts := map[string]openBaoSecretMount{}
	if err := json.Unmarshal([]byte(output), &mounts); err != nil {
		return nil, fmt.Errorf("parse OpenBao secret mounts: %w", err)
	}
	return mounts, nil
}

// disableMountIfExists removes a secrets mount when present.
func (c *openBaoClient) disableMountIfExists(mount string) error {
	mounts, err := c.listSecretMounts()
	if err != nil {
		return err
	}
	if _, exists := mounts[mount+"/"]; !exists {
		return nil
	}

	output, err := c.execute("secrets", "disable", mount)
	return openBaoCommandError("disable secret mount", output, err)
}

// openBaoCommandError adds command output to an execution error.
func openBaoCommandError(operation, output string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w: %s", operation, err, strings.TrimSpace(output))
}

// expectOpenBaoOperation retries an OpenBao operation until it succeeds.
func expectOpenBaoOperation(operation func() error) {
	Eventually(func(g Gomega) {
		g.Expect(operation()).To(Succeed())
	}).WithTimeout(openBaoOperationTimeout).
		WithPolling(openBaoOperationPollingInterval).
		Should(Succeed())
}
