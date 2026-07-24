---
title: "Kamaji Encryption at Rest"
---

[[_TOC_]]

## Overview

DPF can encrypt Kubernetes `Secrets` and `ConfigMaps` stored in the etcd datastore of Kamaji-managed DPU clusters.
Encryption is configured globally under `spec.kamajiClusterManager.etcdEncryptionAtRest` in the `DPFOperatorConfig` and
is applied when each DPU cluster is created.

DPF supports two encryption providers:

* `staticKey` uses a user-provided AES-GCM key.
* `vaultKMS` uses the Kubernetes KMS v2 API and a DPF KMS plugin backed by the OpenBao or HashiCorp Vault Transit
  secrets engine.

This guide provides examples for configuring encryption at rest with either provider for Kamaji-managed DPU clusters.

Each encrypted DPU cluster records its committed provider in `status.etcdEncryptionAtRest.provider`. List the active
provider for all DPU clusters:

```bash
kubectl get dpuclusters -A \
  -o custom-columns='NAMESPACE:.metadata.namespace,NAME:.metadata.name,PROVIDER:.status.etcdEncryptionAtRest.provider'
```

An empty `PROVIDER` means that encryption at rest was not enabled when the DPU cluster was created.

For background information about Kubernetes encryption at rest, see:

* [Encrypting Confidential Data at Rest](https://kubernetes.io/docs/tasks/administer-cluster/encrypt-data/)
* [KMS provider](https://kubernetes.io/docs/tasks/administer-cluster/kms-provider/)
* [OpenBao Transit secrets engine](https://openbao.org/docs/secrets/transit/)
* [OpenBao Kubernetes auth method](https://openbao.org/docs/auth/kubernetes/)

## Important Considerations

* Encryption configuration applies only to Kamaji-managed DPU clusters.
* The provider is selected when a DPU cluster is created. Changing `DPFOperatorConfig` does not enable, disable, or
  change the provider for an existing cluster.
* DPF encrypts Kubernetes `Secrets` and `ConfigMaps`. Other etcd resources are not encrypted by this configuration.
* Back up each per-cluster encryption configuration Secret together with the matching Kamaji etcd backup. DPF stores the
  committed provider configuration in this Secret. For `staticKey`, the Secret also contains encryption key material, so
  store the backup in encrypted durable storage and restrict access to DPU cluster administrators. For more information,
  see the
  [Kamaji etcd backup strategy](../operational-readiness/disaster-recovery/kamaji-backup-restore.md#backup-strategy).
* DPU clusters that use `vaultKMS` depend on the DPF KMS plugin and its configured OpenBao or Vault backend. A DPU
  cluster becomes unavailable if either component is unavailable or unresponsive.

## Prerequisites

The examples are aligned with the OpenBao and External Secrets configuration deployed by the DPF Helmfile. The
`staticKey` example assumes the optional `openbao` and `external-secrets` releases from the
[Helm Prerequisites](../getting-started/helm-prerequisites.md) guide are enabled. The `vaultKMS` example assumes the
optional `openbao` release is enabled.

The Helmfile configuration deploys OpenBao with TLS enabled, initializes it, configures the OpenBao Kubernetes auth
mount, and copies the OpenBao CA certificate to the `external-secrets` and `dpf-operator-system` namespaces as the
`openbao-ca` ConfigMap. The OpenBao chart configuration also enables the auth delegator RBAC needed for OpenBao to
validate Kubernetes service account tokens through the TokenReview API.

The Helmfile initialization hook stores the OpenBao root token in the `openbao-root-token` Secret. For this example,
read it from the Secret and export it as `OPENBAO_ROOT_TOKEN`:

> [!WARNING]
> The OpenBao root token is for bootstrap purposes only and should be stored offline securely. This example assumes the
> token is available in a Kubernetes Secret, which is not recommended for production deployments.
> Treat any shell variable or environment variable that contains tokens, keys, or other secret material as sensitive.
> Unset these variables after completing the configuration, and avoid pasting raw secret values into commands so they
> are not written to shell history.

```bash
export OPENBAO_ROOT_TOKEN="$(kubectl -n openbao get secret openbao-root-token -o jsonpath='{.data.token}' | base64 -d)"
```

## Static Key Provider

The `staticKey` provider reads a base64-encoded AES key from a Kubernetes Secret in the `DPFOperatorConfig` namespace.
The following example stores the source value in OpenBao and uses External Secrets Operator to create the Kubernetes
Secret. For more information about this workflow, see [Encrypted Secrets](encrypted-secrets.md).

To use the `staticKey` provider:

1. [Prepare the static key](#prepare-the-static-key):
   * [Configure OpenBao access](#configure-openbao-access) by creating the service accounts, policies, and Kubernetes
     auth roles needed to write and read the key.
   * [Generate and store the static key](#generate-and-store-the-static-key) in OpenBao.
   * [Replicate the static key](#replicate-the-static-key) as a Secret in the `DPFOperatorConfig` namespace with
     External Secrets Operator.
2. [Enable static key encryption for new DPU clusters](#enable-static-key-encryption-for-new-dpu-clusters). Configure
   the `DPFOperatorConfig` to select the `staticKey` provider and reference the replicated Secret.

### Prepare the Static Key

#### Configure OpenBao Access

Create service accounts for writing the sample key and reading it with External Secrets Operator:

```bash
kubectl -n dpf-operator-system create serviceaccount dpf-etcd-key-writer \
  --dry-run=client -o yaml | kubectl apply -f -

kubectl -n external-secrets create serviceaccount dpf-etcd-key-reader \
  --dry-run=client -o yaml | kubectl apply -f -
```

Create a KV v2 secrets engine for the key:

```bash
kubectl -n openbao exec openbao-0 -- env BAO_TOKEN="${OPENBAO_ROOT_TOKEN}" \
  bao secrets enable -path=dpf-secrets kv-v2
```

Create policies that allow access only to the encryption key:

```bash
kubectl -n openbao exec -i openbao-0 -- env BAO_TOKEN="${OPENBAO_ROOT_TOKEN}" sh <<'EOF'
cat > /tmp/dpf-etcd-key-reader.hcl <<'POLICY'
path "dpf-secrets/data/etcd-encryption/static-key" {
  capabilities = ["read"]
}
POLICY

bao policy write dpf-etcd-key-reader /tmp/dpf-etcd-key-reader.hcl

cat > /tmp/dpf-etcd-key-writer.hcl <<'POLICY'
path "dpf-secrets/data/etcd-encryption/static-key" {
  capabilities = ["create", "update"]
}
POLICY

bao policy write dpf-etcd-key-writer /tmp/dpf-etcd-key-writer.hcl
EOF
```

Create OpenBao Kubernetes auth roles for the service accounts:

```bash
kubectl -n openbao exec -i openbao-0 -- env BAO_TOKEN="${OPENBAO_ROOT_TOKEN}" sh <<'EOF'
bao write auth/kubernetes/role/dpf-etcd-key-reader \
  bound_service_account_names=dpf-etcd-key-reader \
  bound_service_account_namespaces=external-secrets \
  policies=dpf-etcd-key-reader \
  ttl=1h

bao write auth/kubernetes/role/dpf-etcd-key-writer \
  bound_service_account_names=dpf-etcd-key-writer \
  bound_service_account_namespaces=dpf-operator-system \
  policies=dpf-etcd-key-writer \
  ttl=1h
EOF
```

After completing this initial OpenBao configuration, unset the root token environment variable:

```bash
unset OPENBAO_ROOT_TOKEN
```

#### Generate and Store the Static Key

Generate a 32-byte AES key and authenticate to OpenBao with the writer service account:

```bash
STATIC_KEY="$(openssl rand -base64 32)"
WRITER_JWT="$(kubectl -n dpf-operator-system create token dpf-etcd-key-writer)"

OPENBAO_WRITER_TOKEN="$(kubectl -n openbao exec openbao-0 -- \
  bao write -field=token auth/kubernetes/login \
  role=dpf-etcd-key-writer \
  jwt="${WRITER_JWT}")"
```

Store the base64-encoded key text in OpenBao:

```bash
kubectl -n openbao exec openbao-0 -- env BAO_TOKEN="${OPENBAO_WRITER_TOKEN}" \
  bao kv put dpf-secrets/etcd-encryption/static-key key="${STATIC_KEY}"

unset STATIC_KEY WRITER_JWT OPENBAO_WRITER_TOKEN
```

The value stored in the resulting Kubernetes Secret must be the base64-encoded AES key text. It must not be the raw key
bytes. The decoded AES key must be 16, 24, or 32 bytes.

#### Replicate the Static Key

Create a `ClusterSecretStore` that authenticates to OpenBao with the reader service account:

```bash
kubectl apply -f - <<'EOF'
apiVersion: external-secrets.io/v1
kind: ClusterSecretStore
metadata:
  name: dpf-etcd-key-store
spec:
  provider:
    vault:
      server: "https://openbao.openbao.svc.cluster.local:8200"
      path: "dpf-secrets"
      version: "v2"
      caProvider:
        type: ConfigMap
        namespace: external-secrets
        name: openbao-ca
        key: ca.crt
      auth:
        kubernetes:
          mountPath: kubernetes
          role: dpf-etcd-key-reader
          serviceAccountRef:
            name: dpf-etcd-key-reader
            namespace: external-secrets
EOF
```

Create an `ExternalSecret` that replicates the key into the `DPFOperatorConfig` namespace:

```bash
kubectl apply -f - <<'EOF'
apiVersion: external-secrets.io/v1
kind: ExternalSecret
metadata:
  name: dpf-etcd-encryption-key
  namespace: dpf-operator-system
spec:
  refreshInterval: 10m
  secretStoreRef:
    name: dpf-etcd-key-store
    kind: ClusterSecretStore
  target:
    name: dpf-etcd-encryption-key
    creationPolicy: Owner
  data:
    - secretKey: key
      remoteRef:
        key: etcd-encryption/static-key
        property: key
EOF
```

Wait for the Secret to be ready before creating encrypted DPU clusters:

```bash
kubectl -n dpf-operator-system wait \
  --for=condition=Ready externalsecret/dpf-etcd-encryption-key --timeout=60s
```

### Enable Static Key Encryption for New DPU Clusters

Add the following settings to the `DPFOperatorConfig`:

```yaml
apiVersion: operator.dpu.nvidia.com/v1alpha1
kind: DPFOperatorConfig
metadata:
  name: dpfoperatorconfig
  namespace: dpf-operator-system
spec:
  kamajiClusterManager:
    disable: false
    etcdEncryptionAtRest:
      provider: staticKey
      staticKey:
        keySecretRef:
          name: dpf-etcd-encryption-key
          key: key
```

The key is read and copied into a per-cluster encryption configuration when a DPU cluster is created.

### Rotate the Static Key and Re-encrypt Stored Resources

When automatic static key rotation is enabled, DPF treats the static key referenced by
`spec.kamajiClusterManager.etcdEncryptionAtRest.staticKey.keySecretRef` in the current `DPFOperatorConfig` as the
desired key for every DPU cluster that uses the `staticKey` provider. If the reference or the value in the referenced
Secret changes, DPF attempts to rotate all Ready clusters that use `staticKey` and re-encrypt their stored `Secrets` and
`ConfigMaps` with the new key.

> [!NOTE]
> Automatic rotation requires the current `DPFOperatorConfig` to continue selecting the `staticKey` provider and
> referencing a valid key. If the encryption settings are removed or changed to another provider, automatic rotation is
> disabled for existing `staticKey` clusters. Each cluster continues using the active key stored in its per-cluster
> encryption configuration. The source Secret version for that key remains recorded in
> `status.etcdEncryptionAtRest.staticKey.activeKeyRef`.

The following example replaces the value at the OpenBao path used by the `ExternalSecret`:

```bash
NEW_STATIC_KEY="$(openssl rand -base64 32)"
WRITER_JWT="$(kubectl -n dpf-operator-system create token dpf-etcd-key-writer)"

OPENBAO_WRITER_TOKEN="$(kubectl -n openbao exec openbao-0 -- \
  bao write -field=token auth/kubernetes/login \
  role=dpf-etcd-key-writer \
  jwt="${WRITER_JWT}")"

kubectl -n openbao exec openbao-0 -- env BAO_TOKEN="${OPENBAO_WRITER_TOKEN}" \
  bao kv put dpf-secrets/etcd-encryption/static-key key="${NEW_STATIC_KEY}"

unset NEW_STATIC_KEY WRITER_JWT OPENBAO_WRITER_TOKEN
```

On the next refresh, External Secrets Operator updates the referenced Kubernetes Secret and DPF starts the rotation. For
each cluster, DPF:

1. Adds the new key to the encryption configuration as a decryption key.
2. Verifies that every Kubernetes API server instance has loaded the updated configuration.
3. Promotes the new key so that new writes use it.
4. Rewrites all Kubernetes `Secrets` and `ConfigMaps` by using `StorageVersionMigration` resources.
5. Removes the old key and verifies that every API server instance has loaded the final configuration.

The DPU cluster status records the source Secret version for the active key in
`status.etcdEncryptionAtRest.staticKey.activeKeyRef`. To verify that the cluster is using the expected key, compare this
reference with `keySecretRef` in the current `DPFOperatorConfig` and the metadata of the referenced Secret. Use
`.metadata.resourceVersion` from that Secret as the expected `resourceVersion`. When automatic rotation is enabled,
rotation is fully complete when all reference fields match and both
`EtcdEncryptionRotationInProgress` and `EtcdEncryptionRotationBlocked` are `False`.

`EtcdEncryptionRotationInProgress=True` means that DPF is reloading the encryption configuration or migrating encrypted
resources. `EtcdEncryptionRotationBlocked=True` reports a state that prevents rotation from progressing, including an
unavailable or invalid source key, an invalid encryption configuration, or a failed storage migration. These conditions
do not gate DPU cluster readiness.

Set `spec.kamajiClusterManager.etcdEncryptionAtRest.staticKey.automaticRotationDisabled` to `true` to pause automatic
rotation without disabling encryption at rest. An in-progress rotation stops at the next stable checkpoint. Remove the
field or set it to `false` to resume automatic rotation. While automatic rotation is paused,
`EtcdEncryptionRotationBlocked=True` with reason `Disabled` is expected.

## Vault KMS Provider

The `vaultKMS` provider deploys a DPF KMS plugin DaemonSet on management cluster control-plane nodes. The plugin uses
the OpenBao or HashiCorp Vault Transit secrets engine to encrypt and decrypt data for Kamaji DPU cluster API servers.

The KMS plugin exposes the Kubernetes KMS v2 gRPC API through a Unix domain socket. The socket is available on every
control-plane node of the management cluster. It is shared by the Kamaji API server instances running on that node for
DPU clusters configured with `vaultKMS` as their encryption provider.

The example below uses OpenBao with Kubernetes auth. DPF also supports other auth methods for Vault and OpenBao through
`spec.security.vaultKMS.auth`.

To use the `vaultKMS` provider:

1. [Configure OpenBao Transit](#configure-openbao-transit). Enable the Transit secrets engine, create the encryption key
   and policy, and create a Kubernetes auth role for the DPF KMS plugin.
2. [Enable Vault KMS encryption for new DPU clusters](#enable-vault-kms-encryption-for-new-dpu-clusters). Configure the
   `DPFOperatorConfig` to deploy the KMS plugin and select the `vaultKMS` provider.

### Configure OpenBao Transit

> [!WARNING]
> Back up the OpenBao persisted data that contains the Transit key and test the restore procedure before using
> `vaultKMS`. Losing the Transit key permanently prevents existing DPU clusters from decrypting their data. See
> [Backing up OpenBao's persisted data](https://openbao.org/docs/concepts/storage/#backing-up-openbaos-persisted-data).
> OpenBao also supports [Transit key backup](https://openbao.org/api-docs/secret/transit/#backup-key) and
> [restore](https://openbao.org/api-docs/secret/transit/#restore-key), but key-level backup requires
> `allow_plaintext_backup` to be enabled for the key. This setting cannot be disabled after it is enabled, and the
> resulting backup must be protected. The examples in this guide do not enable `allow_plaintext_backup`.

Enable the Transit secrets engine and create a key:

```bash
kubectl -n openbao exec openbao-0 -- env BAO_TOKEN="${OPENBAO_ROOT_TOKEN}" \
  bao secrets enable -path=transit transit

kubectl -n openbao exec openbao-0 -- env BAO_TOKEN="${OPENBAO_ROOT_TOKEN}" \
  bao write -f transit/keys/dpf-etcd
```

Create a policy that allows the KMS plugin to encrypt and decrypt with this key and to inspect and renew its OpenBao
token:

```bash
kubectl -n openbao exec -i openbao-0 -- env BAO_TOKEN="${OPENBAO_ROOT_TOKEN}" sh <<'EOF'
cat > /tmp/dpf-etcd-kms.hcl <<'POLICY'
path "transit/encrypt/dpf-etcd" {
  capabilities = ["update"]
}

path "transit/decrypt/dpf-etcd" {
  capabilities = ["update"]
}

path "auth/token/lookup-self" {
  capabilities = ["read"]
}

path "auth/token/renew-self" {
  capabilities = ["update"]
}
POLICY

bao policy write dpf-etcd-kms /tmp/dpf-etcd-kms.hcl
EOF
```

Create a Kubernetes auth role for the service account used by the DPF KMS plugin. The `dpf-kms` audience restricts the
projected service account token to this authentication flow:

```bash
kubectl -n openbao exec openbao-0 -- env BAO_TOKEN="${OPENBAO_ROOT_TOKEN}" \
  bao write auth/kubernetes/role/dpf-etcd-kms \
  bound_service_account_names=dpf-kms-plugin \
  bound_service_account_namespaces=dpf-operator-system \
  audience=dpf-kms \
  policies=dpf-etcd-kms \
  ttl=1h
```

After completing this initial OpenBao configuration, unset the root token environment variable:

```bash
unset OPENBAO_ROOT_TOKEN
```

### Enable Vault KMS Encryption for New DPU Clusters

Add the following settings to the `DPFOperatorConfig`:

```yaml
apiVersion: operator.dpu.nvidia.com/v1alpha1
kind: DPFOperatorConfig
metadata:
  name: dpfoperatorconfig
  namespace: dpf-operator-system
spec:
  security:
    vaultKMS:
      disable: false
      address: "https://openbao.openbao.svc.cluster.local:8200"
      tls:
        caConfigMapRef:
          name: openbao-ca
          key: ca.crt
      auth:
        method: kubernetes
        kubernetes:
          role: dpf-etcd-kms
          audience: dpf-kms
          authEngineMountPath: kubernetes
      transit:
        mount: transit
        keyName: dpf-etcd
  kamajiClusterManager:
    disable: false
    etcdEncryptionAtRest:
      provider: vaultKMS
```

> [!WARNING]
> Never remove or disable `spec.security.vaultKMS` while at least one DPU cluster uses the `vaultKMS` encryption
> provider.
> Those clusters depend on the KMS plugin and its OpenBao or Vault key material for every encryption and decryption
> operation.
> Changing `vaultKMS` settings is supported to accommodate operational changes, such as moving the backend endpoint or
> updating authentication and TLS settings. However, such changes are dangerous and should be limited to advanced use
> cases. Before changing these settings, verify that the new address, namespace, Transit mount, Transit key,
> authentication settings, and TLS settings provide access to the same secret material as the previous configuration. A
> misconfiguration can prevent all existing DPU clusters that use `vaultKMS` from encrypting or decrypting their data.
