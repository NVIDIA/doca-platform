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

DPF has two sets of certificates, and `certificateAuthority.issuerRef` in the `dpf-operator` chart
anchors both:

* **DPF Operator certificates** - the serving certificates of the validating and mutating webhooks
  that DPF deploys.
* **Provisioning certificates** - the certificates used to provision a DPU, including the mTLS
  identities of the DPU BMC and the DPU operating system.

Each set derives from an intermediate CA of its own, so the two can be rotated and reasoned about
separately while chaining to the same authority. Re-anchoring on a cluster that already has
provisioned DPUs is a CA rotation rather than a configuration change, see [Scope](#scope).

The components DPF depends on, such as Kamaji or the maintenance operator, run their own PKI. They
are not signed by DPF. Each of their charts accepts an issuer of your own, see
[Dependencies](#dependencies).

# The DPF Operator certificate chain

The `dpf-operator` chart creates a root and one intermediate CA per purpose, and the certificates of
the DPF components chain up to the intermediate of their purpose:

```
dpf-global-bootstrapper (selfSigned)
  └── dpf-global-root-ca (isCA)
        └── dpf-global-root-issuer
              ├── dpf-webhook-intermediate-ca (isCA)
              │     └── dpf-webhook-issuer
              │           ├── dpuservice-serving-cert
              │           ├── dpf-nodesriovdeviceplugin-serving-cert
              │           └── dpf-provisioning-serving-cert
              └── dpf-platform-intermediate-ca (isCA)
                    └── dpf-platform-issuer
                          ├── dpf-provisioning-redfish-client-cert
                          ├── dpf-provisioning-redfish-client-cert-bf4
                          ├── bfb-registry-server-cert
                          └── the DPU BMC server certificates
```

Because the components reference the intermediate issuer and never the root, replacing the root does
not change which identity signs them.

## Using your own certificate authority

Set `certificateAuthority.issuerRef` in the values of the `dpf-operator` chart to an existing
cert-manager issuer. The chart then skips `dpf-global-bootstrapper` and `dpf-global-root-ca` and
requests both intermediate CAs from your issuer instead:

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

The anchor itself is reported on the `CertManagementReady` condition of the `DPFOperatorConfig`,
which you can read with [`dpfctl`](../operational-readiness/troubleshooting/dpfctl/README.md) or
`kubectl`:

**Using dpfctl (recommended):**

```bash
dpfctl describe all --show-resources=dpfoperatorconfig --show-conditions=all --show-pending
```

**Using kubectl:**

```bash
kubectl -n dpf-operator-system get dpfoperatorconfig dpfoperatorconfig \
  -o jsonpath='{range .status.conditions[?(@.type=="CertManagementReady")]}{.status}/{.reason}{"\n"}{.message}{"\n"}{end}'
```

`IssuerNotFound` means the issuer named in `certificateAuthority.issuerRef` does not exist, and the
message names the kind and name that were looked for. `Pending` with a message waiting for it to
become ready means the issuer is there but its own readiness is still outstanding, which for a `ca`
issuer is usually the Secret behind it.

# The trust bundle

The components of DPF, the DPU operating systems and the DPU BMC truststores validate the peers they
talk to against one ConfigMap in the namespace of the release, `dpf-ca-trust-bundle`. Its `ca.crt`
holds the authorities to trust, and the `bundle-hash` beside it is what the convergence of the DPUs
and the BMCs on a change of that content is measured against.

Which side writes `ca.crt` follows the choice of authority:

* Anchored to the self-signed root of DPF, the operator writes it. It holds the root, so it can
  derive what the peers have to trust. Certificates of authorities it does not publish are
  preserved, which is how an additional CA merged into the ConfigMap stays there. Another
  certificate of an authority it does publish is taken away, since the one published in its place
  carries the same subject and key and so validates everything it did.
* Anchored to an issuer of yours, the content is yours. DPF cannot derive what the peers have to
  trust from an authority it has no access to, so it does not write `ca.crt` and maintains only
  `bundle-hash`. Populating it, and carrying two authorities in it across a rotation, is part of
  running your own PKI.

The name defaults to `dpf-ca-trust-bundle`, in both modes, and is set on the `DPFOperatorConfig`
rather than in the chart. Name a bundle of your own when you keep the trust material of the cluster
under a name of your choosing:

```yaml
spec:
  security:
    certManagement:
      trustBundleConfigMapName: enterprise-ca-trust-bundle
```

It is fixed once the config exists. The name reaches a DPU in the provisioning configuration it is
installed with, so renaming it afterwards would leave the DPUs already provisioned reading a
ConfigMap DPF no longer maintains.

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

Set `certificateAuthority.issuerRef` before the first DPU is provisioned. On a cluster that has
none, the choice of authority is a configuration change and takes effect as the chain is issued. The
certificates that provision a DPU are signed by `dpf-platform-issuer` over
`dpf-platform-intermediate-ca`, which is requested from the issuer in
`certificateAuthority.issuerRef` like every other intermediate above, so pointing DPF at your
authority is all that is needed to re-root them.

Afterwards it is a CA rotation. The certificates below `dpf-platform-intermediate-ca` are the ones
DPU BMCs and DPU operating systems are provisioned to trust, and their truststores are reached
through `dpf-ca-trust-bundle` rather than rewritten by the chart. DPF reports this on the
`CertManagementReady` condition as `CARotationRequired` and keeps reporting it until the rotation has
been carried out. See
[Switching a provisioned cluster to your own authority](#switching-a-provisioned-cluster-to-your-own-authority).

The rotation is less disruptive than it sounds, because `dpf-platform-intermediate-ca` is declared
with `privateKey.rotationPolicy: Never`: it keeps its key and subject DN when the new authority
re-signs it, so the certificates it has already issued stay valid throughout. What the rotation
distributes is the new authority, not a new signing identity.

On a cluster upgraded from a release that provisioned its DPUs under the self-signed provisioning CA,
`dpf-provisioning-ca-cert` and the chain over it are retained and stay self-signed. DPF collapses
them into the platform intermediate CA only where it owns the authority at the top of the chain;
under an issuer of your own it owns no keys, and the trust material below that issuer is yours to
curate rather than DPF's to publish. That makes the certificate of `dpf-provisioning-ca-cert` part of
what your `ca.crt` has to carry, for as long as a DPU provisioned under it is in the cluster.

The webhook serving certificates are safe to re-root at any time, including on upgrade. Their trust
is distributed inside the cluster through the `cert-manager.io/inject-ca-from` annotation, so
cert-manager reissues the certificate and re-injects the matching `caBundle` into the webhook
configuration with no manual action. The webhooks use `failurePolicy: Fail`, so admission of the
affected custom resources can be rejected for the short period between the new CA bundle reaching the
API server and the reissued certificate reaching the webhook pod.

# Switching a provisioned cluster to your own authority

> [!WARNING]
> This is a manual, one-time and disruptive procedure. A peer that has not been given the new
> authority to trust cannot validate the chain, which means provisioning and BMC mTLS downtime for
> that peer until it has. DPF reports that the authority changed and reissues what it owns, but
> moving the truststores over is yours to drive.

Carry it out in the order below, which is the anchor rotation in
[CA Certificate Rotation](ca-certificate-rotation.md) with its first step replaced: there the new
authority is a re-keyed self-signed root, here it is your issuer. Use that guide for the commands and
the convergence checks. Its `hack/scripts/ca-rotation.sh` covers the two steps that remain DPF's
under an external issuer, re-signing the intermediates and rotating the leaf and DPU BMC
certificates, and refuses the ones that act on a root or on the content of the bundle. Those are the
manual steps here.

1. **Record what is being retired.** The current `bundle-hash` and the authority in `ca.crt` that the
   peers trust today, per step 1 of the rotation guide. Keep the copy of the bundle: once the chart is
   anchored elsewhere DPF stops writing `ca.crt`, and this is the only record of what was in it.

2. **Create your issuer** and confirm it reports `Ready`, as in
   [Using your own certificate authority](#using-your-own-certificate-authority).

3. **Add your authority to the bundle before it signs anything.** Merge the certificate of your root
   into `ca.crt` of `dpf-ca-trust-bundle`, keeping the one already there, and wait for the DPUs and
   the BMCs to converge on the resulting `bundle-hash` before going on. Your authority is not one
   DPF publishes, so it survives the reconciles in between. Doing it first is what makes the rest of
   the procedure a window rather than an outage: every peer then trusts both authorities.

4. **Upgrade the chart with `certificateAuthority.issuerRef` set.** It stops creating
   `dpf-global-bootstrapper`, `dpf-global-root-ca` and `dpf-global-root-issuer`, and requests both
   intermediate CAs from your issuer instead. `ca.crt` becomes yours to maintain at this point, see
   [The trust bundle](#the-trust-bundle). DPF records the authority it has rolled out under
   `status.security.certManagement.anchor` and reports `CertManagementReady` as `CARotationRequired`
   until the authority in the chart is the one it has rolled out.

5. **Reissue the leaves, and re-push the certificates that are installed rather than mounted.** Per
   step 4 of the rotation guide. The DPU BMC server certificate is the one that does not follow from
   a reissue on its own: it is installed over Redfish, so it is re-pushed by annotating the
   `DPUDevice` with `provisioning.dpu.nvidia.com/rotate-bmc-server-certificate`.

6. **Drop the retired authority from `ca.crt`** once every DPU and DPUDevice reports the current
   `bundle-hash`, per step 5 of the rotation guide. Removing it earlier is what turns the switch into
   an outage for whatever has not converged yet.

Moving on to a third authority is the same procedure. Going back to the self-signed root of DPF drops
step 3: that root does not exist until the chart upgrade creates it, and the operator publishes it
into `ca.crt` itself, keeping your authority beside it as one it does not publish. Upgrade the chart
with `certificateAuthority.issuerRef` removed, wait there for every peer to converge on the resulting
`bundle-hash` rather than ahead of it, and then carry out steps 5 and 6.
