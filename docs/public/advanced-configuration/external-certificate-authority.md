---
title: "External Certificate Authority"
---

[[_TOC_]]

DPF issues the TLS certificates it needs with cert-manager. By default every certificate is signed by
a certificate authority that DPF generates itself and self-signs. In environments with an enterprise
PKI, certificates have to chain back to a corporate root CA or an authorized intermediate instead.
This guide describes how to point DPF, and the components it depends on, at a certificate authority
you own.

# The certificates in a DPF deployment

DPF has two sets of certificates:

* **DPF Operator certificates** - the serving certificates of the validating and mutating webhooks
  that DPF deploys. These are covered by `certificateAuthority.issuerRef` in the `dpf-operator` chart,
  described below.
* **Provisioning certificates** - the certificates used to provision a DPU, including the mTLS
  identities of the DPU BMC and the DPU operating system. These are signed by the provisioning CA and
  are not covered by this guide, see [Scope](#scope).

The components DPF depends on, such as Kamaji or the maintenance operator, run their own PKI. They
are not signed by DPF. Each of their charts accepts an issuer of your own, see
[Dependencies](#dependencies).

# The DPF Operator certificate chain

The `dpf-operator` chart creates a root and one intermediate CA per purpose, and the webhook
certificates of the DPF components chain up to the intermediate:

```
dpf-global-bootstrapper (selfSigned)
  └── dpf-global-root-ca (isCA)
        └── dpf-global-root-issuer
              └── dpf-webhook-intermediate-ca (isCA)
                    └── dpf-webhook-issuer
                          ├── dpuservice-serving-cert
                          ├── dpf-nodesriovdeviceplugin-serving-cert
                          └── dpf-provisioning-serving-cert
```

Because the components reference the intermediate issuer and never the root, replacing the root does
not change anything below it.

## Using your own certificate authority

Set `certificateAuthority.issuerRef` in the values of the `dpf-operator` chart to an existing
cert-manager issuer. The chart then skips `dpf-global-bootstrapper` and `dpf-global-root-ca` and
requests the intermediate CA from your issuer instead:

```yaml
certificateAuthority:
  issuerRef:
    name: openbao-issuer
    kind: ClusterIssuer
```

`kind` defaults to `Issuer` and `group` to `cert-manager.io`. An `Issuer` must exist in the namespace
of the release, a `ClusterIssuer` is cluster scoped.

The issuer, and everything behind it, is yours. DPF only consumes it by name. Whether it is backed by
an external PKI such as OpenBao or Vault, or by an offline CA whose key you keep outside the cluster,
is not visible to DPF.

### Prerequisites

* cert-manager is installed. It is a DPF prerequisite and is installed with the prerequisite
  helmfile.
* The issuer exists and is `Ready` before the `dpf-operator` chart is installed.
* The PKI backend behind it allows signing CA certificates (`isCA: true`). DPF requests its
  intermediate CA through your issuer, so a backend policy that only issues leaf certificates rejects
  it.

### Using an offline root CA

An offline root is the same case as an external issuer from the point of view of DPF: sign the root
outside the cluster, then expose it to cert-manager as an issuer and reference that.

1. Create a Secret with the certificate and key of your CA. For a `ClusterIssuer` it has to live in
   the cluster resource namespace of cert-manager (`cert-manager` by default, set with the
   `--cluster-resource-namespace` flag), for an `Issuer` in the namespace of the DPF release.

    ```bash
    kubectl -n cert-manager create secret tls dpf-root-ca --cert=ca.crt --key=ca.key
    ```

2. Create the issuer over it:

    ```yaml
    apiVersion: cert-manager.io/v1
    kind: ClusterIssuer
    metadata:
      name: dpf-root-issuer
    spec:
      ca:
        secretName: dpf-root-ca
    ```

3. Reference it as shown above.

## Verification

```bash
# With your own CA the self-signed root is absent and only the intermediate exists.
kubectl -n dpf-operator-system get issuers.cert-manager.io

# Every serving certificate is issued by dpf-webhook-issuer and is ready.
kubectl -n dpf-operator-system get certificates.cert-manager.io \
  -o custom-columns=NAME:.metadata.name,ISSUER:.spec.issuerRef.name,READY:.status.conditions[0].status
```

A certificate that stays not ready is usually a policy problem on the side of the issuer. Check the
`CertificateRequest` for the reason returned by the backend:

```bash
kubectl -n dpf-operator-system describe certificaterequests.cert-manager.io
```

# Dependencies

The components DPF depends on bring their own PKI and are installed from their own charts. DPF does
not sign their certificates and does not create issuers for them. To chain a deployment to a single
certificate authority, point each of them at your issuer as well. All of them are configured through
the values DPF ships in `deploy/helmfiles/values/`, described in the
[helm prerequisites](../getting-started/helm-prerequisites.md) guide.

| Component                        | How to point it at your issuer                                                                                          |
|----------------------------------|-------------------------------------------------------------------------------------------------------------------------|
| Kamaji                           | `certManager.issuerRef`                                                                                                 |
| maintenance-operator             | `operator.admissionController.certificates.certManager.issuerRef`                                                       |
| OpenBao                          | replace the issuer that the shipped values create in `extraObjects`                                                     |
| external-secrets                 | replace the issuer that the shipped values create in `extraObjects`, referenced by `webhook.certManager.cert.issuerRef` |
| ovn-kubernetes-resource-injector | not supported yet, the webhook certificate is always signed by an issuer of its own                                     |

Left alone, each of them creates a self-signed issuer of its own, which is the default.

# Scope

The provisioning CA (`dpf-provisioning-ca-cert` and the certificates it signs) is **not** part of the
chain above and stays self-signed. It signs the certificates that DPU BMCs and DPU operating systems
are provisioned to trust, so re-rooting it is a CA rotation rather than a configuration change. See
[Provisioning CA Certificate Rotation](ca-certificate-rotation.md). Only the provisioning webhook
serving certificate is part of the DPF Operator chain.

The webhook serving certificates are safe to re-root at any time, including on upgrade. Their trust
is distributed inside the cluster through the `cert-manager.io/inject-ca-from` annotation, so
cert-manager reissues the certificate and re-injects the matching `caBundle` into the webhook
configuration with no manual action. The webhooks use `failurePolicy: Fail`, so admission of the
affected custom resources can be rejected for the short period between the new CA bundle reaching the
API server and the reissued certificate reaching the webhook pod.
