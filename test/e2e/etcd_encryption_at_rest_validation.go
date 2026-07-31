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
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/utils/tunnel"
	testutils "github.com/nvidia/doca-platform/test/utils"
	"github.com/nvidia/doca-platform/test/utils/netshoot"
	kamajiv1 "github.com/nvidia/doca-platform/third_party/forked/github.com/clastix/kamaji/api/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	etcdEncryptionValidationPodName      = "etcd-encryption-validation-client"
	etcdEncryptionValidationImage        = "quay.io/coreos/etcd:v3.5.17"
	etcdEncryptionValidationContainer    = "etcd-client"
	etcdEncryptionEndpoint               = "https://kamaji-etcd-client.dpf-operator-system.svc.cluster.local:2379"
	staticKeyEncryptionEnvelopePrefix    = "k8s:enc:aesgcm:v1:"
	vaultKMSEncryptionEnvelopePrefix     = "k8s:enc:kms:v2:vault-kms-plugin:"
	etcdEncryptionConfigurationSecret    = "kamaji-etcd-certs"
	etcdEncryptionRootClientSecret       = "kamaji-etcd-root-client-certs"
	etcdEncryptionProbeNamespace         = "default"
	etcdEncryptionProbeGenerateName      = "dpf-e2e-etcd-encryption-"
	etcdEncryptionValidationPollInterval = time.Second
	etcdEncryptionValidationTimeout      = 3 * time.Minute

	// staticKeyRotationValidationTimeout allows a full static key rotation,
	// which can take up to 10 minutes. Rotation starts near the beginning of
	// suite setup, while this validation normally runs at the end of the suite
	// and should find it completed. The longer timeout supports running this
	// test in isolation, where it must wait for the full rotation.
	staticKeyRotationValidationTimeout = 15 * time.Minute
)

type etcdRangeResponse struct {
	KVs []etcdRangeKeyValue `json:"kvs"`
}

type etcdRangeKeyValue struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// ValidateDPUClusterEtcdEncryptionAtRest validates every managed Kamaji
// DPUCluster configured with encryption at rest. For static keys, it waits for
// rotation to finish and verifies that status references the latest source
// Secret revision. It then verifies that all Secrets and ConfigMaps remain
// readable through the tenant API and use the configured encryption provider
// in the shared Kamaji etcd. Static-key clusters must use one active key for
// every stored value.
func ValidateDPUClusterEtcdEncryptionAtRest(ctx context.Context, input *systemTestInput) {
	if etcdEncryptionAtRestConfiguration(input.config) == nil {
		Skip("Skipping etcd encryption-at-rest validation because it is not configured")
	}

	clusters := &provisioningv1.DPUClusterList{}
	Expect(input.client.List(ctx, clusters)).To(Succeed())

	kamajiClusters := make([]*provisioningv1.DPUCluster, 0, len(clusters.Items))
	for i := range clusters.Items {
		cluster := &clusters.Items[i]
		if cluster.Spec.Type == string(provisioningv1.KamajiCluster) {
			kamajiClusters = append(kamajiClusters, cluster)
		}
	}
	if len(kamajiClusters) == 0 {
		Skip("Skipping etcd encryption-at-rest validation because no Kamaji DPUClusters exist")
	}

	createEtcdEncryptionValidationPod(ctx, input.client)
	for _, cluster := range kamajiClusters {
		expectDPUClusterEncryptionAtRest(
			ctx,
			input.client,
			cluster,
			input.config,
			staticKeyRotationValidationTimeout,
		)
		validateDPUClusterRawEtcdEncryption(ctx, input, cluster)
	}
}

// createEtcdEncryptionValidationPod creates a long-running etcdctl client with
// the certificates required to read the shared Kamaji datastore.
func createEtcdEncryptionValidationPod(ctx context.Context, c client.Client) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      etcdEncryptionValidationPodName,
			Namespace: dpfOperatorSystemNamespace,
			Labels:    CleanupScope.It,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:            etcdEncryptionValidationContainer,
					Image:           etcdEncryptionValidationImage,
					ImagePullPolicy: corev1.PullIfNotPresent,
					Command:         etcdctlCommand("watch", "/dpf-e2e-etcd-encryption-watch"),
					VolumeMounts: []corev1.VolumeMount{
						{Name: "etcd-certs", MountPath: "/etc/etcd/pki", ReadOnly: true},
						{Name: "client-certs", MountPath: "/etc/etcd/client", ReadOnly: true},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "etcd-certs",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: etcdEncryptionConfigurationSecret,
						},
					},
				},
				{
					Name: "client-certs",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: etcdEncryptionRootClientSecret,
						},
					},
				},
			},
			Tolerations: []corev1.Toleration{
				{Key: "node-role.kubernetes.io/master", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoSchedule},
				{Key: "node-role.kubernetes.io/control-plane", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoSchedule},
			},
			RestartPolicy: corev1.RestartPolicyAlways,
		},
	}
	Expect(client.IgnoreAlreadyExists(c.Create(ctx, pod))).To(Succeed())

	Eventually(func(g Gomega) {
		current := &corev1.Pod{}
		g.Expect(c.Get(ctx, client.ObjectKeyFromObject(pod), current)).To(Succeed())
		g.Expect(current.Status.Phase).To(Equal(corev1.PodRunning))
		g.Expect(current.Status.Conditions).To(ContainElement(And(
			HaveField("Type", Equal(corev1.PodReady)),
			HaveField("Status", Equal(corev1.ConditionTrue)),
		)))
	}).WithTimeout(etcdEncryptionValidationTimeout).
		WithPolling(etcdEncryptionValidationPollInterval).
		Should(Succeed())
}

// validateDPUClusterRawEtcdEncryption creates tenant resources, verifies that
// every Secret and ConfigMap remains readable through the tenant API, and
// validates their raw encryption envelopes in etcd.
func validateDPUClusterRawEtcdEncryption(ctx context.Context, input *systemTestInput, cluster *provisioningv1.DPUCluster) {
	tenantClient, clusterTunnel := newEtcdEncryptionTenantClient(ctx, input, cluster)
	cleanupObjects := []client.Object{}
	DeferCleanup(func(ctx context.Context) {
		defer clusterTunnel.Close()
		Expect(testutils.CleanupAndWait(ctx, tenantClient, cleanupObjects...)).To(Succeed())
	})

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: etcdEncryptionProbeGenerateName,
			Namespace:    etcdEncryptionProbeNamespace,
		},
		Data: map[string][]byte{"payload": []byte("encrypted-secret-probe")},
	}
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: etcdEncryptionProbeGenerateName,
			Namespace:    etcdEncryptionProbeNamespace,
		},
		Data: map[string]string{"payload": "encrypted-configmap-probe"},
	}
	Expect(tenantClient.Create(ctx, secret)).To(Succeed())
	cleanupObjects = append(cleanupObjects, secret)
	Expect(tenantClient.Create(ctx, configMap)).To(Succeed())
	cleanupObjects = append(cleanupObjects, configMap)

	schema := dpuClusterDatastoreSchema(ctx, input.client, cluster)
	Eventually(func(g Gomega) {
		secretValues := readEtcdResourcePrefix(g, input, schema, "secrets")
		configMapValues := readEtcdResourcePrefix(g, input, schema, "configmaps")

		secretProbeValue := expectEtcdProbeValue(g, secretValues, schema, "secrets", secret)
		expectEtcdProbeValue(g, configMapValues, schema, "configmaps", configMap)

		expectedPrefix := expectedEtcdEncryptionEnvelopePrefix(g, input, secretProbeValue)
		expectEtcdValuesUseEnvelopePrefix(g, secretValues, expectedPrefix)
		expectEtcdValuesUseEnvelopePrefix(g, configMapValues, expectedPrefix)
	}).WithTimeout(etcdEncryptionValidationTimeout).
		WithPolling(etcdEncryptionValidationPollInterval).
		Should(Succeed())

	expectTenantEncryptedResourcesAccessible(ctx, tenantClient, secret, configMap)
}

// newEtcdEncryptionTenantClient creates a scoped tunneled client for a DPUCluster.
func newEtcdEncryptionTenantClient(
	ctx context.Context,
	input *systemTestInput,
	cluster *provisioningv1.DPUCluster,
) (client.Client, *tunnel.Tunnel) {
	var tenantClient client.Client
	var clusterTunnel *tunnel.Tunnel
	Eventually(func(g Gomega) {
		current := cluster.DeepCopy()
		g.Expect(input.client.Get(ctx, client.ObjectKeyFromObject(cluster), current)).To(Succeed())

		restConfig, newTunnel, err := tunnel.NewTunneledRestConfig(ctx, input.client, input.restConfig, current)
		g.Expect(err).NotTo(HaveOccurred())
		newClient, err := client.New(restConfig, client.Options{})
		if err != nil && newTunnel != nil {
			newTunnel.Close()
		}
		g.Expect(err).NotTo(HaveOccurred())
		tenantClient = newClient
		clusterTunnel = newTunnel
	}).WithTimeout(etcdEncryptionValidationTimeout).
		WithPolling(etcdEncryptionValidationPollInterval).
		Should(Succeed())
	return tenantClient, clusterTunnel
}

// dpuClusterDatastoreSchema returns the resolved etcd prefix from the Kamaji API.
func dpuClusterDatastoreSchema(ctx context.Context, c client.Client, cluster *provisioningv1.DPUCluster) string {
	var schema string
	Eventually(func(g Gomega) {
		tcp := &kamajiv1.TenantControlPlane{}
		g.Expect(c.Get(ctx, client.ObjectKeyFromObject(cluster), tcp)).To(Succeed())
		schema = tcp.Status.Storage.Setup.Schema
		g.Expect(schema).NotTo(BeEmpty())
	}).WithTimeout(time.Minute).WithPolling(time.Second).Should(Succeed())
	return strings.Trim(schema, "/")
}

// readEtcdResourcePrefix returns one consistent snapshot of all values under a
// tenant resource prefix.
func readEtcdResourcePrefix(g Gomega, input *systemTestInput, schema, resource string) map[string][]byte {
	prefix := fmt.Sprintf("/%s/%s/", schema, resource)
	output, err := netshoot.ExecInPodOnceWithErrorParser(
		hostClusterRESTClient,
		input.restConfig,
		dpfOperatorSystemNamespace,
		etcdEncryptionValidationPodName,
		etcdctlCommand("--write-out=json", "get", prefix, "--prefix"),
		netshoot.StderrOnlyErrorParser,
	)
	g.Expect(err).NotTo(HaveOccurred(), "read raw etcd values under %s", prefix)

	response := &etcdRangeResponse{}
	g.Expect(json.Unmarshal([]byte(output), response)).To(Succeed(), "parse etcd range response for %s", prefix)
	g.Expect(response.KVs).NotTo(BeEmpty(), "expected raw etcd values under %s", prefix)

	values := make(map[string][]byte, len(response.KVs))
	for _, kv := range response.KVs {
		key, err := base64.StdEncoding.DecodeString(kv.Key)
		g.Expect(err).NotTo(HaveOccurred(), "decode etcd key under %s", prefix)
		value, err := base64.StdEncoding.DecodeString(kv.Value)
		g.Expect(err).NotTo(HaveOccurred(), "decode etcd value for key %s", key)
		values[string(key)] = value
	}
	return values
}

// expectEtcdProbeValue returns the raw value for a generated probe.
func expectEtcdProbeValue(g Gomega, values map[string][]byte, schema, resource string, obj client.Object) []byte {
	key := fmt.Sprintf("/%s/%s/%s/%s", schema, resource, obj.GetNamespace(), obj.GetName())
	value, ok := values[key]
	g.Expect(ok).To(BeTrue(), "expected probe at raw etcd key %s", key)
	return value
}

// expectedEtcdEncryptionEnvelopePrefix returns the provider-specific envelope
// prefix expected for every stored value. For static keys, the post-rotation
// Secret probe identifies the active key name used for new writes.
func expectedEtcdEncryptionEnvelopePrefix(g Gomega, input *systemTestInput, staticKeyProbeValue []byte) string {
	encryptionAtRest := etcdEncryptionAtRestConfiguration(input.config)
	g.Expect(encryptionAtRest).NotTo(BeNil())

	switch encryptionAtRest.Provider {
	case operatorv1.EtcdEncryptionProviderStaticKey:
		g.Expect(bytes.HasPrefix(staticKeyProbeValue, []byte(staticKeyEncryptionEnvelopePrefix))).To(
			BeTrue(),
			"raw Secret probe does not use static-key encryption",
		)
		remainder := staticKeyProbeValue[len(staticKeyEncryptionEnvelopePrefix):]
		separator := bytes.IndexByte(remainder, ':')
		g.Expect(separator).To(BeNumerically(">", 0), "raw Secret probe has no static key name")
		return staticKeyEncryptionEnvelopePrefix + string(remainder[:separator]) + ":"
	case operatorv1.EtcdEncryptionProviderVaultKMS:
		return vaultKMSEncryptionEnvelopePrefix
	default:
		g.Expect(encryptionAtRest.Provider).To(
			Or(Equal(operatorv1.EtcdEncryptionProviderStaticKey), Equal(operatorv1.EtcdEncryptionProviderVaultKMS)),
			"unsupported etcd encryption-at-rest provider %q",
			encryptionAtRest.Provider,
		)
		return ""
	}
}

// expectEtcdValuesUseEnvelopePrefix verifies all values in a resource snapshot
// use the configured provider and, for static keys, the same active key.
func expectEtcdValuesUseEnvelopePrefix(g Gomega, values map[string][]byte, expectedPrefix string) {
	g.Expect(values).NotTo(BeEmpty())
	for key, value := range values {
		g.Expect(bytes.HasPrefix(value, []byte(expectedPrefix))).To(
			BeTrue(),
			"raw etcd key %s does not use expected encryption prefix %q",
			key,
			expectedPrefix,
		)
	}
}

// expectTenantEncryptedResourcesAccessible verifies that every Secret and
// ConfigMap can be listed through the tenant API and that the generated probes
// retain their expected data.
func expectTenantEncryptedResourcesAccessible(
	ctx context.Context,
	c client.Client,
	secretProbe *corev1.Secret,
	configMapProbe *corev1.ConfigMap,
) {
	Eventually(func(g Gomega) {
		secrets := &corev1.SecretList{}
		g.Expect(c.List(ctx, secrets)).To(Succeed(), "list all tenant Secrets")
		secretProbeFound := false
		for i := range secrets.Items {
			secret := &secrets.Items[i]
			if secret.Namespace == secretProbe.Namespace && secret.Name == secretProbe.Name {
				secretProbeFound = true
				g.Expect(secret.Data["payload"]).To(Equal(secretProbe.Data["payload"]))
				break
			}
		}
		g.Expect(secretProbeFound).To(BeTrue(), "Secret probe was not returned by the tenant API")

		configMaps := &corev1.ConfigMapList{}
		g.Expect(c.List(ctx, configMaps)).To(Succeed(), "list all tenant ConfigMaps")
		configMapProbeFound := false
		for i := range configMaps.Items {
			configMap := &configMaps.Items[i]
			if configMap.Namespace == configMapProbe.Namespace && configMap.Name == configMapProbe.Name {
				configMapProbeFound = true
				g.Expect(configMap.Data["payload"]).To(Equal(configMapProbe.Data["payload"]))
				break
			}
		}
		g.Expect(configMapProbeFound).To(BeTrue(), "ConfigMap probe was not returned by the tenant API")
	}).WithTimeout(etcdEncryptionValidationTimeout).
		WithPolling(etcdEncryptionValidationPollInterval).
		Should(Succeed())
}

// etcdctlCommand builds an etcdctl command using the mounted Kamaji credentials.
func etcdctlCommand(args ...string) []string {
	command := []string{
		"/usr/local/bin/etcdctl",
		"--endpoints=" + etcdEncryptionEndpoint,
		"--cacert=/etc/etcd/pki/ca.crt",
		"--cert=/etc/etcd/client/tls.crt",
		"--key=/etc/etcd/client/tls.key",
	}
	return append(command, args...)
}
