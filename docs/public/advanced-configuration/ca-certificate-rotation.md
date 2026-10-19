---
title: "CA Certificate Rotation"
---

[[_TOC_]]

This guide explains how to rotate the certificate authority that anchors the DPF PKI while keeping
bfb-registry, provisioning-controller, DPU BMC mTLS, hostagent and DPU OS trust healthy during the
transition.

# Overview

DPF certificates chain up to a root through one intermediate CA per purpose:

```
dpf-global-bootstrapper (selfSigned)
  └── dpf-global-root-ca (isCA)
        └── dpf-global-root-issuer
              ├── dpf-webhook-intermediate-ca (isCA)   signs the webhook serving certificates
              └── dpf-platform-intermediate-ca (isCA)
                    └── dpf-platform-issuer            signs the provisioning certificates
```

Rotating the authority means replacing the root while everything below it keeps working.

`dpf-platform-intermediate-ca` is declared with `privateKey.rotationPolicy: Never`, so it keeps its
key and its subject DN when the new root re-signs it. The identity that signs the provisioning
certificates therefore never changes during a rotation; only what signs *it* does. Certificates the
intermediate has already issued stay valid, and certificates it issues during the rotation are
signed by an identity every peer already trusts.

That property is what makes the rotation straightforward:

1. Record the certificates of the authority being retired.
2. Re-key the root, which creates the new authority.
3. Re-sign the intermediates under it, and wait until the resulting trust bundle has reached every
   DPU and DPUDevice. The bundle carries both authorities throughout this window.
4. Renew the leaf certificates, and in Zero Trust rotate the DPU BMC server certificates.
5. Prune the retired authority from the trust bundle.
6. Delete the backup.

DPF propagates `dpf-ca-trust-bundle` to its consumers automatically. Both authorities stay in it
until step 5 removes the retired one. These actions can be performed with the manual runbook or the
rotation script below.

> No issuer is pinned to a backup CA at any point. Earlier releases needed that, because rotating
> the single provisioning CA changed the identity signing the leaves immediately. The platform
> intermediate preserving its key and DN removes the window that pin was covering.

# Prerequisites

Before rotating the CA, ensure that:

* You have `kubectl` access to the DPF management cluster namespace (examples use `dpf-operator-system`).
* `cmctl` is installed and can manage cert-manager resources in that namespace.
* `openssl` is installed.
* You can read and modify `dpfoperatorconfig`, `issuer`, `certificate`, `configmap`, `secret`,
  `dpudevice`, and `dpu` resources.
* `CertManagementReady` on the `DPFOperatorConfig` is `True`, so the authority currently in effect is
  reported and the chain below it is issued.
* You run this during a maintenance window where certificate updates are allowed.

# Certificate authority modes

What you rotate, and how much of it DPF owns, depends on what anchors the PKI. Read it from the
status DPF reports:

```bash
kubectl -n dpf-operator-system get dpfoperatorconfig dpfoperatorconfig \
  -o jsonpath='{.status.security.certManagement.anchor}{"\n"}'
```

* **Self-signed** — the anchor is `dpf-global-root-issuer`. DPF owns the root and writes the trust
  bundle, so the whole of this guide applies.
* **External** — the anchor is an issuer you own, configured through `certificateAuthority.issuerRef`
  in the `dpf-operator` chart, see
  [External Certificate Authority](external-certificate-authority.md). DPF cannot see the authority
  behind it and does not write `dpf-ca-trust-bundle`, so re-keying the root and curating the bundle
  are yours to perform. Only steps 3 and 4 below apply, after your new root certificate is in the
  bundle alongside the old one.

# DPU BMC CA trust rotation status (`DPUDevice`, Zero Trust only)

* `observedBundleHash` — the `bundle-hash` value last successfully applied on the BMC truststore.
* `lastUpdateTime` — time of last successful reconciliation.

`CATrustBundleReady` condition on `DPUDevice` reports health:

| Condition status | Reason                    | Meaning                                      |
|------------------|---------------------------|----------------------------------------------|
| `True`           | `CATrustBundleSynced`     | BMC truststore matches desired bundle.       |
| `False`          | `CATrustBundleSyncing`    | Install/remove operations are in progress.   |
| `False`          | `CATrustBundleSyncFailed` | Last sync attempt failed; retry will happen. |
| `False`          | `CATrustBundleUnavailable`| Bundle missing, unreadable, or invalid.      |

# DPU OS CA trust rotation status (`DPU`)

`DPU.status.agentStatus` exposes DPU OS trust application progress:

* `trustBundleHash` — the `bundle-hash` value last applied by dpu-agent.
* `trustBundleLastUpdateTime` — last successful apply time on DPU OS.

# Manual CA rotation

## 1) Record the certificates being retired

Step 5 prunes the retired authority from the trust bundle, and the root recorded here is what it
prunes by: everything that root signed goes with it. Record it before anything changes, because step
2 replaces the root and the certificate being retired is then no longer on the cluster to read.

The private key of the root is kept alongside, so a rotation abandoned halfway can be undone by
restoring the Secret. Step 6 discards it.

It passes through the filesystem on its way there, so write it to a directory only you can read.
`mktemp -d` creates one with `0700` whatever umask is in effect, which a predictable path under
`/tmp` would not give you on a shared administrative host.

```bash
CA_BACKUP_DIR=$(mktemp -d)

kubectl -n dpf-operator-system get secret dpf-global-root-ca \
  -o jsonpath='{.data.tls\.crt}' | base64 -d > "$CA_BACKUP_DIR/root-tls.crt"
kubectl -n dpf-operator-system get secret dpf-global-root-ca \
  -o jsonpath='{.data.tls\.key}' | base64 -d > "$CA_BACKUP_DIR/root-tls.key"
kubectl -n dpf-operator-system get secret dpf-platform-intermediate-ca \
  -o jsonpath='{.data.tls\.crt}' | base64 -d > "$CA_BACKUP_DIR/intermediate-tls.crt"

kubectl -n dpf-operator-system create secret generic dpf-ca-rotation-backup \
  --from-file=root-tls.crt="$CA_BACKUP_DIR/root-tls.crt" \
  --from-file=root-tls.key="$CA_BACKUP_DIR/root-tls.key" \
  --from-file=intermediate-tls.crt="$CA_BACKUP_DIR/intermediate-tls.crt"
```

Note the fingerprints of what is being retired, then remove the copy on disk. Step 2 checks the
re-key against the root fingerprint, and step 5 prunes by the root certificate itself, which it
reads back out of the Secret. The Secret is what the rest of the procedure reads, including the
rollback in step 2, so nothing below needs the key to stay there:

```bash
export OLD_ROOT_FINGERPRINT=$(openssl x509 -in "$CA_BACKUP_DIR/root-tls.crt" -noout -fingerprint -sha256 | cut -d= -f2)
export OLD_INTERMEDIATE_FINGERPRINT=$(openssl x509 -in "$CA_BACKUP_DIR/intermediate-tls.crt" -noout -fingerprint -sha256 | cut -d= -f2)
printf 'old root:         %s\nold intermediate: %s\n' \
  "$OLD_ROOT_FINGERPRINT" "$OLD_INTERMEDIATE_FINGERPRINT"

rm -rf "$CA_BACKUP_DIR"
```

Proceed only when `dpf-ca-rotation-backup` exists.

## 2) Re-key the self-signed root

> Skip this step in external CA deployments. Rotate your own root instead, and add its new
> certificate to `dpf-ca-trust-bundle` alongside the old one before continuing with step 3.

Delete the root's Secret. That leaves cert-manager no key to reuse, so it generates a new keypair
and self-signs it through `dpf-global-bootstrapper`. Renewing the Certificate instead would re-sign
the existing key and leave the authority unchanged:

```bash
kubectl -n dpf-operator-system delete secret dpf-global-root-ca
```

Nothing below the root moves yet. The intermediates keep the certificates the previous root signed
until step 3 renews them, so the chain in effect is undisturbed for the whole of this step.

Check before moving on:

```bash
kubectl -n dpf-operator-system get certificate dpf-global-root-ca \
  -o jsonpath='{range .status.conditions[?(@.type=="Ready")]}{.status}{"/"}{.reason}{"\n"}{end}'

kubectl -n dpf-operator-system get secret dpf-global-root-ca \
  -o jsonpath='{.data.tls\.crt}' | base64 -d \
  | openssl x509 -noout -fingerprint -sha256 | cut -d= -f2
```

Proceed only when the Certificate is `True/Ready` and the fingerprint differs from
`$OLD_ROOT_FINGERPRINT`.

## 3) Re-sign the intermediates and converge dual trust

This step re-signs the intermediates under the authority now in effect. Each keeps its key and its
subject DN, which is why nothing has to be pinned: the certificates they have already signed are
still validated by the re-signed intermediate.

```bash
cmctl renew dpf-platform-intermediate-ca -n dpf-operator-system
cmctl renew dpf-webhook-intermediate-ca -n dpf-operator-system
```

In external CA deployments renew `dpf-platform-intermediate-ca` only. The webhook intermediate is
re-signed by your issuer on the same schedule, and its trust is injected into the webhook
configurations by cert-manager rather than through the trust bundle.

Verify the renewal and that the trust bundle now carries both authorities:

```bash
kubectl -n dpf-operator-system get certificate dpf-platform-intermediate-ca \
  -o jsonpath='{range .status.conditions[?(@.type=="Ready")]}{.status}{"/"}{.reason}{"\n"}{end}'

# The authority above the intermediate, which DPF publishes into the bundle.
kubectl -n dpf-operator-system get secret dpf-platform-intermediate-ca \
  -o jsonpath='{.data.ca\.crt}' | base64 -d \
  | openssl x509 -noout -fingerprint -sha256 | cut -d= -f2

# Both the retired and the current authority are expected here during the window. Split the bundle
# into a directory of its own, so that what is counted is this bundle and not a leftover of an
# earlier read.
bundle_dir="$(mktemp -d)"
kubectl -n dpf-operator-system get configmap dpf-ca-trust-bundle \
  -o jsonpath='{.data.ca\.crt}' \
  | awk -v d="$bundle_dir" '/BEGIN CERT/{n++} {print > (d "/bundle-" n ".pem")}'
for cert in "$bundle_dir"/bundle-*.pem; do
  openssl x509 -in "$cert" -noout -subject -fingerprint -sha256
done
rm -rf "$bundle_dir"
```

Then wait for propagation before moving on:

```bash
export BUNDLE_HASH=$(kubectl -n dpf-operator-system get configmap dpf-ca-trust-bundle \
  -o jsonpath='{.data.bundle-hash}')
echo "$BUNDLE_HASH"
```

For Zero Trust deployments, verify BMC-side convergence (`DPUDevice`). Skip this check in Host
Trusted deployments:

```bash
kubectl -n dpf-operator-system get dpudevice -o jsonpath='
{range .items[*]}
{.metadata.name}{"\t"}
{.status.caTrustBundle.observedBundleHash}{"\t"}
{range .status.conditions[?(@.type=="CATrustBundleReady")]}{.status}{"/"}{.reason}{end}
{"\n"}
{end}'
```

Verify DPU OS-side convergence (`DPU`):

```bash
kubectl -n dpf-operator-system get dpu -o jsonpath='
{range .items[*]}
{.metadata.name}{"\t"}
{.status.agentStatus.trustBundleHash}{"\t"}
{.status.agentStatus.trustBundleLastUpdateTime}
{"\n"}
{end}'
```

Proceed to the next step only when:

* The trust bundle carries the retired authority and the current one.
* In Zero Trust deployments, `status.caTrustBundle.observedBundleHash == $BUNDLE_HASH` and
  `CATrustBundleReady=True` for all target `DPUDevice`.
* `status.agentStatus.trustBundleHash == $BUNDLE_HASH` for all target `DPU`.

## 4) Renew leaf certificates and rotate certificates

The leaf certificates are valid either way, since the intermediate kept the key that signed them.
This step is about the copy of the chain each leaf Secret carries: renewing them means no peer is
handed a chain ending at an authority step 5 is about to prune.

Renew all certificates issued by `dpf-platform-issuer`:

```bash
for cert in $(kubectl -n dpf-operator-system get certificate -o jsonpath='{range .items[?(@.spec.issuerRef.name=="dpf-platform-issuer")]}{.metadata.name}{"\n"}{end}'); do
  cmctl renew "$cert" -n dpf-operator-system
done
```

Wait for every renewed leaf certificate to carry the authority now in effect. What is asked of each
one is the `ca.crt` beside it rather than the signature over it: the intermediate kept the key that
signed these certificates, so one issued before the rotation still verifies against it afterwards.
The authority above the intermediate is what the rotation replaces, and cert-manager writes it into
`ca.crt` of every certificate below, so a leaf still naming the old one has not been reissued:

```bash
anchor=$(kubectl -n dpf-operator-system get secret dpf-platform-intermediate-ca \
  -o jsonpath='{.data.ca\.crt}' | base64 -d | openssl x509 -noout -fingerprint -sha256)
for cert in $(kubectl -n dpf-operator-system get certificate -o jsonpath='{range .items[?(@.spec.issuerRef.name=="dpf-platform-issuer")]}{.metadata.name}{"\n"}{end}'); do
  secret=$(kubectl -n dpf-operator-system get certificate "$cert" \
    -o jsonpath='{.spec.secretName}')
  leaf=$(kubectl -n dpf-operator-system get secret "$secret" -o jsonpath='{.data.ca\.crt}' \
    | base64 -d | openssl x509 -noout -fingerprint -sha256)
  if [ "$leaf" = "$anchor" ]; then echo "$cert OK"; else echo "$cert PENDING"; fi
done
```

Continue only when every leaf certificate reports `OK`.

The webhook serving certificates need no action. Their trust is distributed through the
`cert-manager.io/inject-ca-from` annotation, so cert-manager refreshes the `caBundle` of the webhook
configurations itself once the intermediate above them has been re-signed.

For Zero Trust deployments only, trigger DPU BMC server certificate rotation. Skip this action in
Host Trusted deployments:

```bash
export ROTATION_TOKEN="$(date +%s)"
kubectl -n dpf-operator-system annotate dpudevice --all \
  provisioning.dpu.nvidia.com/rotate-bmc-server-certificate="$ROTATION_TOKEN" --overwrite
```

For Zero Trust deployments, check BMC server certificate convergence before moving on:

```bash
kubectl -n dpf-operator-system get dpudevice -o jsonpath='
{range .items[*]}
{.metadata.name}{"\t"}
{range .status.conditions[?(@.type=="BMCServerCertificateReady")]}{.status}{"/"}{.reason}{end}{"\t"}
{.status.bmcServerCertificate.observedManualTrigger}
{"\n"}
{end}'
```

Proceed only when all target devices show `BMCServerCertificateReady=True` and
`observedManualTrigger == $ROTATION_TOKEN`.

## 5) Close the dual-trust window by pruning the retired authority

> Skip this step in external CA deployments and prune your own bundle instead.

Remove the retired root, which nothing chains to any more, and everything it signed, which are
paths that now end nowhere. Expect more than the two certificates recorded in step 1: a rotation
leaves the earlier certificates of the intermediate in the bundle as well, and those go with the
root that signed them. Removing them takes no trust away, because the intermediate still in the
bundle carries the same key and subject as the ones being removed.

Perform this only after step 4 convergence is complete, otherwise components still presenting the
old chain may lose trust.

Ask each certificate in the bundle whether the retired root signed it. Asking whose name is on it
instead would not separate the two: step 2 re-keyed the root in place, so the retired root and the
one that replaced it carry the same subject.

The retired root answers yes about itself, so it is marked `PRUNE` along with everything it signed,
which is what this step is for. The root that replaced it is marked `KEEP`: verifying it takes its
own key, and only the retired one is in the file being asked.

```bash
prune_dir="$(mktemp -d)"
kubectl -n dpf-operator-system get secret dpf-ca-rotation-backup \
  -o jsonpath='{.data.root-tls\.crt}' | base64 -d > "$prune_dir/old-root.pem"
kubectl -n dpf-operator-system get configmap dpf-ca-trust-bundle \
  -o jsonpath='{.data.ca\.crt}' \
  | awk -v d="$prune_dir" '/BEGIN CERT/{n++} {print > (d "/bundle-" n ".pem")}'
for cert in "$prune_dir"/bundle-*.pem; do
  if openssl verify -no_check_time -partial_chain \
      -CAfile "$prune_dir/old-root.pem" "$cert" > /dev/null 2>&1; then
    verdict=PRUNE
  else
    verdict=KEEP
  fi
  echo "$verdict $(openssl x509 -in "$cert" -noout -fingerprint -sha256 | cut -d= -f2)"
done
rm -rf "$prune_dir"
```

Edit `dpf-ca-trust-bundle` and remove the PEM blocks the listing marked `PRUNE`:

```bash
kubectl -n dpf-operator-system edit configmap dpf-ca-trust-bundle
```

Leave every certificate marked `KEEP` in place. DPF re-merges the bundle without pruning, so
anything else you keep there is preserved.

Check before moving on:

```bash
# Capture the new desired hash after the prune
export BUNDLE_HASH=$(kubectl -n dpf-operator-system get configmap dpf-ca-trust-bundle \
  -o jsonpath='{.data.bundle-hash}')
echo "$BUNDLE_HASH"
```

Then repeat the convergence checks from step 3 against this new `bundle-hash`.

## 6) Delete the backup

This is a cleanup step. Delete the backup only after step 5 convergence is complete and the retired
authority has been pruned from `dpf-ca-trust-bundle`.

```bash
kubectl -n dpf-operator-system delete secret dpf-ca-rotation-backup
```

# Automated CA rotation

DPF provides `hack/scripts/ca-rotation.sh` to perform the rotation and wait for each change to take
effect. Run the script from the repository root.

The script reads `dpfoperatorconfig.status.security.certManagement.anchor` and
`dpfoperatorconfig.spec.deploymentMode`, and adjusts the workflow automatically:

* With the self-signed root it runs all six steps.
* With an external issuer it runs steps 3 and 4 only, and reports that re-keying the root and
  curating the trust bundle are outside DPF.
* In `zero-trust` mode it verifies DPU BMC and DPU OS trust propagation and rotates DPU BMC server
  certificates.
* In `host-trusted` mode it verifies DPU OS trust propagation and skips DPU BMC operations.

## Run the complete rotation

Use `run-all` to perform the complete rotation:

```bash
./hack/scripts/ca-rotation.sh run-all
```

This command validates the required tools and Kubernetes resources, runs the applicable steps in
order, and waits for each step to complete. It skips completed steps and stops if an action fails or
a check times out.

After resolving a reported problem, rerun the same command. To intentionally skip earlier steps, use
`--from-step <number>` only after confirming that those steps are complete:

```bash
./hack/scripts/ca-rotation.sh run-all --from-step 3
```

## Run the rotation step by step

Before running individual steps, validate the required tools and Kubernetes resources without
changing the cluster:

```bash
./hack/scripts/ca-rotation.sh precheck
```

Run each command only after reviewing the corresponding manual step:

```bash
./hack/scripts/ca-rotation.sh step1-backup
./hack/scripts/ca-rotation.sh step2-rekey-root
./hack/scripts/ca-rotation.sh step3-renew-intermediates
./hack/scripts/ca-rotation.sh step4-rotate-leaves
./hack/scripts/ca-rotation.sh step5-prune-old-ca
./hack/scripts/ca-rotation.sh step6-cleanup
```

Each command performs one action and waits for its completion checks to pass. The steps that act on
the root or the trust bundle refuse to run when an external issuer anchors the PKI.

## Configure timeouts

The default timeout for each completion check is 600 seconds, and the default polling interval is
5 seconds. Increase these values when convergence is expected to take longer:

```bash
./hack/scripts/ca-rotation.sh \
  --timeout-sec 1200 \
  --wait-interval-sec 10 \
  run-all
```

Run `./hack/scripts/ca-rotation.sh --help` for all commands and options.

# Related topics

* [External Certificate Authority](external-certificate-authority.md)
* [DPU BMC mTLS Server Certificate Rotation](zero-trust-advanced-configuration.md#bmc-mtls-server-certificate-rotation)
