---
title: "Encrypted Secrets"
---

[[_TOC_]]

This guide demonstrates how to replicate secret data from an external secret store into the management cluster as a
Kubernetes Secret with External Secrets Operator. The example uses OpenBao as the external secret store.

The example uses placeholder names and values only. Do not reuse the example credentials in production.

## Prerequisites

The example is aligned with the OpenBao and External Secrets configuration deployed by the DPF Helmfile. It assumes the
optional `openbao` and `external-secrets` releases from the
[Helm Prerequisites](../getting-started/helm-prerequisites.md) guide are enabled.

The Helmfile configuration deploys OpenBao with TLS enabled, initializes it, configures the OpenBao Kubernetes auth
mount, and copies the OpenBao CA certificate to the `external-secrets` namespace as the `openbao-ca` ConfigMap. The
OpenBao chart configuration also enables the auth delegator RBAC needed for OpenBao to validate Kubernetes service
account tokens through the TokenReview API.

The remaining steps configure access to a specific OpenBao path and tell External Secrets Operator how to replicate it.

The Helmfile initialization hook stores the OpenBao root token in the `openbao-root-token` Secret. For this example, read
it from the Secret and export it as `OPENBAO_ROOT_TOKEN`:

> [!WARNING]
> The OpenBao root token is for bootstrap purposes only and should be stored offline securely. This example assumes the
> token is available in a Kubernetes Secret, which is not recommended for production deployments.


```bash
export OPENBAO_ROOT_TOKEN="$(kubectl -n openbao get secret openbao-root-token -o jsonpath='{.data.token}' | base64 -d)"
```

## Bootstrap OpenBao Access

Use the root token only for OpenBao bootstrap tasks such as enabling mounts, creating reader and writer policies, and
binding Kubernetes service accounts to OpenBao roles.

Create Kubernetes service accounts to bind to the OpenBao roles:

```bash
kubectl create namespace encrypted-secrets-example --dry-run=client -o yaml | kubectl apply -f -

kubectl -n encrypted-secrets-example create serviceaccount sample-application-secret-writer --dry-run=client -o yaml | kubectl apply -f -

kubectl -n external-secrets create serviceaccount sample-application-secret-reader --dry-run=client -o yaml | kubectl apply -f -
```

Create a KV v2 mount for application secrets:

```bash
kubectl -n openbao exec openbao-0 -- env BAO_TOKEN="${OPENBAO_ROOT_TOKEN}" \
  bao secrets enable -path=application-secrets kv-v2
```

Create reader and writer OpenBao policies for the sample application path:

```bash
kubectl -n openbao exec -i openbao-0 -- env BAO_TOKEN="${OPENBAO_ROOT_TOKEN}" sh <<'EOF'
cat > /tmp/sample-application-secret-reader-policy.hcl <<'POLICY'
path "application-secrets/data/sample-application/*" {
  capabilities = ["read"]
}
POLICY

bao policy write sample-application-secret-reader /tmp/sample-application-secret-reader-policy.hcl

cat > /tmp/sample-application-secret-writer-policy.hcl <<'POLICY'
path "application-secrets/data/sample-application/*" {
  capabilities = ["create", "update"]
}
POLICY

bao policy write sample-application-secret-writer /tmp/sample-application-secret-writer-policy.hcl
EOF
```

Create OpenBao Kubernetes auth roles that bind the Kubernetes service accounts to the policies:

```bash
kubectl -n openbao exec -i openbao-0 -- env BAO_TOKEN="${OPENBAO_ROOT_TOKEN}" sh <<'EOF'
bao write auth/kubernetes/role/sample-application-secret-reader \
  bound_service_account_names=sample-application-secret-reader \
  bound_service_account_namespaces=external-secrets \
  policies=sample-application-secret-reader \
  ttl=1h

bao write auth/kubernetes/role/sample-application-secret-writer \
  bound_service_account_names=sample-application-secret-writer \
  bound_service_account_namespaces=encrypted-secrets-example \
  policies=sample-application-secret-writer \
  ttl=1h
EOF
```

The `sample-application-secret-reader` role is bound to the service account used by the `ClusterSecretStore` below. The
`sample-application-secret-writer` role is bound to a separate service account used only to write the sample data. If
External Secrets Operator is installed in a different namespace, update the reader service account, OpenBao role binding,
and `ClusterSecretStore` service account reference.

## Store Secret Data in OpenBao

Authenticate to OpenBao with the sample application writer service account:

```bash
WRITER_JWT="$(kubectl -n encrypted-secrets-example create token sample-application-secret-writer)"

OPENBAO_WRITER_TOKEN="$(kubectl -n openbao exec openbao-0 -- \
  bao write -field=token auth/kubernetes/login \
  role=sample-application-secret-writer \
  jwt="${WRITER_JWT}")"
```

Store explicit sample application credentials under the KV v2 mount with the writer token:

```bash
kubectl -n openbao exec openbao-0 -- env BAO_TOKEN="${OPENBAO_WRITER_TOKEN}" \
  bao kv put application-secrets/sample-application/credentials \
  username='sample-user' \
  password='sample-password'
```

## Configure External Secrets Operator

Create a `ClusterSecretStore` that connects External Secrets Operator to OpenBao:

```bash
kubectl apply -f - <<'EOF'
apiVersion: external-secrets.io/v1
kind: ClusterSecretStore
metadata:
  name: management-cluster-secret-store
spec:
  provider:
    vault:
      server: "https://openbao.openbao.svc.cluster.local:8200"
      path: "application-secrets"
      version: "v2"
      caProvider:
        type: ConfigMap
        namespace: external-secrets
        name: openbao-ca
        key: ca.crt
      auth:
        kubernetes:
          mountPath: kubernetes
          role: sample-application-secret-reader
          serviceAccountRef:
            name: sample-application-secret-reader
            namespace: external-secrets
EOF
```

Create an `ExternalSecret` that maps the OpenBao data to a Kubernetes Secret in the management cluster:

```bash
kubectl apply -f - <<'EOF'
apiVersion: external-secrets.io/v1
kind: ExternalSecret
metadata:
  name: sample-application-credentials
  namespace: encrypted-secrets-example
spec:
  refreshInterval: 10m
  secretStoreRef:
    name: management-cluster-secret-store
    kind: ClusterSecretStore
  target:
    name: sample-application-credentials
    creationPolicy: Owner
  data:
    - secretKey: username
      remoteRef:
        key: sample-application/credentials
        property: username
    - secretKey: password
      remoteRef:
        key: sample-application/credentials
        property: password
EOF
```

External Secrets Operator can also render a Kubernetes Secret value from the same source data. The following example uses
the `sample-application/credentials` OpenBao data and renders it into a JSON document in the replicated Secret:

```bash
kubectl apply -f - <<'EOF'
apiVersion: external-secrets.io/v1
kind: ExternalSecret
metadata:
  name: sample-rendered-json-secret
  namespace: encrypted-secrets-example
spec:
  refreshInterval: 10m
  secretStoreRef:
    name: management-cluster-secret-store
    kind: ClusterSecretStore
  target:
    name: sample-rendered-json-secret
    creationPolicy: Owner
    template:
      engineVersion: v2
      type: Opaque
      metadata:
        labels:
          dpu.nvidia.com/image-pull-secret: ""
      data:
        secret.json: |-
          {
            "rpcTokens": [
              {
                "name": "sample-target",
                "username": "{{ .username }}",
                "password": "{{ .password }}"
              }
            ]
          }
  data:
    - secretKey: username
      remoteRef:
        key: sample-application/credentials
        property: username
    - secretKey: password
      remoteRef:
        key: sample-application/credentials
        property: password
EOF
```

## Verify the Replicated Secret

Check the `ExternalSecret` status:

```bash
kubectl -n encrypted-secrets-example get externalsecret sample-application-credentials
```

Check that the Kubernetes Secret was created:

```bash
kubectl -n encrypted-secrets-example get secret sample-application-credentials
```

Check the rendered JSON Secret:

```bash
kubectl -n encrypted-secrets-example get externalsecret sample-rendered-json-secret
kubectl -n encrypted-secrets-example get secret sample-rendered-json-secret -o jsonpath='{.data.secret\.json}' | base64 -d
```

The replicated Secret is managed by External Secrets Operator. Update the data in OpenBao when the source value needs to
change, and External Secrets Operator will refresh the Kubernetes Secret according to `spec.refreshInterval`.
