---
title: "Provisioning CA Certificate Rotation"
---

[[_TOC_]]

This guide explains how to rotate the current self-signed provisioning CA used by DPF while keeping
bfb-registry, provisioning-controller, DPU BMC mTLS, hostagent and DPU OS trust healthy during the
transition.

# Overview

The provisioning CA establishes trust between DPF components. Replacing it in a single operation could break mTLS while some components still use certificates signed by the old CA. DPF therefore uses a dual-trust rotation:

1. Add the new CA alongside the old CA in `dpf-ca-trust-bundle`.
2. Wait until the combined trust bundle has reached every consumer.
3. Switch the provisioning issuer to the new CA, renew provisioning leaf certificates, and rotate
   client and server certificates.
4. Verify that all clients and servers use certificates signed by the new CA.
5. Remove the old CA only after the new certificate chain and trust bundle have converged.

DPF automatically propagates changes from `dpf-ca-trust-bundle`. The operator initiates CA renewal,
switches the signer, rotates certificates, verifies convergence, and closes the dual-trust window.
These actions can be performed with the manual runbook or the rotation script in this guide.

# Prerequisites

Before rotating the CA, ensure that:

* You have `kubectl` access to the DPF management cluster namespace (examples use `dpf-operator-system`).
* `cmctl` is installed and can manage cert-manager resources in that namespace.
* `openssl` is installed.
* You can read and modify `dpfoperatorconfig`, `issuer`, `certificate`, `configmap`, `secret`,
  `dpudevice`, and `dpu` resources.
* You run this during a maintenance window where certificate updates are allowed.

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

# Manual self-signed CA rotation

## 1) Backup current provisioning CA Secret and pin issuer to old CA Secret

This step creates a backup copy of the current CA Secret and pins the provisioning issuer to the
backup Secret. This keeps new certificate issuance on the old CA while dual trust is being
prepared, so renewal of the new CA does not immediately switch the signing chain.

Run backup command:
```bash
kubectl -n dpf-operator-system get secret dpf-provisioning-ca-secret -o yaml \
  | sed 's/name: dpf-provisioning-ca-secret/name: dpf-provisioning-ca-secret-old/' \
  | kubectl -n dpf-operator-system apply -f -
```

Run command to pin the issuer to the old CA Secret:
```bash
kubectl -n dpf-operator-system patch dpfoperatorconfig dpfoperatorconfig --type=merge -p '{
  "spec": {
    "overrides": {
      "provisioningIssuerCASecretName": "dpf-provisioning-ca-secret-old"
    }
  }
}'
```

Check before moving on:

```bash
# Backup secret exists
kubectl -n dpf-operator-system get secret dpf-provisioning-ca-secret-old

# Provisioning issuer has converged to old CA secret
kubectl -n dpf-operator-system get issuer dpf-provisioning-issuer \
  -o jsonpath='{.spec.ca.secretName}{"\n"}'
```

Proceed only when:

* `dpf-provisioning-ca-secret-old` exists.
* issuer secretName output equals `dpf-provisioning-ca-secret-old`.

## 2) Renew provisioning CA certificate and start dual-trust propagation

This step requests cert-manager to issue a new provisioning CA certificate while the issuer
is still pinned to `dpf-provisioning-ca-secret-old`. After renewal completes, DPF reconciles the
updated CA trust bundle and starts propagating dual trust to BMC and DPU-side consumers.

Run CA renewal:

```bash
cmctl renew dpf-provisioning-ca-cert -n dpf-operator-system
```

Verify CA certificate renewal:

```bash
kubectl -n dpf-operator-system get certificate dpf-provisioning-ca-cert \
  -o jsonpath='{range .status.conditions[?(@.type=="Ready")]}{.status}{"/"}{.reason}{"\n"}{end}'
```

Confirm that renewal produced a different CA:

```bash
export OLD_CA_FINGERPRINT=$(kubectl -n dpf-operator-system get secret \
  dpf-provisioning-ca-secret-old -o jsonpath='{.data.tls\.crt}' \
  | base64 -d | openssl x509 -noout -fingerprint -sha256 | cut -d= -f2)
export NEW_CA_FINGERPRINT=$(kubectl -n dpf-operator-system get secret \
  dpf-provisioning-ca-secret -o jsonpath='{.data.tls\.crt}' \
  | base64 -d | openssl x509 -noout -fingerprint -sha256 | cut -d= -f2)
printf 'old: %s\nnew: %s\n' "$OLD_CA_FINGERPRINT" "$NEW_CA_FINGERPRINT"
```


Then wait for dual-trust propagation before moving on:

Get current desired CA trust bundle hash:

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

* In Zero Trust deployments, `status.caTrustBundle.observedBundleHash == $BUNDLE_HASH` and
  `CATrustBundleReady=True` for all target `DPUDevice`.
* `status.agentStatus.trustBundleHash == $BUNDLE_HASH` for all target `DPU`

## 3) Switch issuer back to default CA Secret (new CA)

This step removes the temporary issuer override so DPF reconciles the provisioning issuer back to
the default CA Secret (`dpf-provisioning-ca-secret`). After this converges, newly issued
certificates are signed by the new CA.

```bash
kubectl -n dpf-operator-system patch dpfoperatorconfig dpfoperatorconfig --type=merge -p '{
  "spec": {
    "overrides": {
      "provisioningIssuerCASecretName": null
    }
  }
}'
```

Check before moving on:

```bash
# Override should be cleared
kubectl -n dpf-operator-system get dpfoperatorconfig dpfoperatorconfig \
  -o jsonpath='{.spec.overrides.provisioningIssuerCASecretName}{"\n"}'

# Issuer should converge to default CA secret
kubectl -n dpf-operator-system get issuer dpf-provisioning-issuer \
  -o jsonpath='{.spec.ca.secretName}{"\n"}'
```

Proceed only when override output is empty and issuer output equals `dpf-provisioning-ca-secret`.

## 4) Renew provisioning leaf certificates and rotate certificates

This step moves runtime certificates to the new CA chain. Renew provisioning leaf certificates in
both deployment modes. In Zero Trust deployments, also rotate the DPU BMC server certificates so
BMC endpoints present certificates signed by the new CA.

Renew all certificates issued by `dpf-provisioning-issuer`:

```bash
for cert in $(kubectl -n dpf-operator-system get certificate -o jsonpath='{range .items[?(@.spec.issuerRef.name=="dpf-provisioning-issuer")]}{.metadata.name}{"\n"}{end}'); do
  cmctl renew "$cert" -n dpf-operator-system
done
```

Wait for every renewed leaf certificate to verify against the new CA:

```bash
for cert in $(kubectl -n dpf-operator-system get certificate -o jsonpath='{range .items[?(@.spec.issuerRef.name=="dpf-provisioning-issuer")]}{.metadata.name}{"\n"}{end}'); do
  secret=$(kubectl -n dpf-operator-system get certificate "$cert" \
    -o jsonpath='{.spec.secretName}')
  kubectl -n dpf-operator-system get secret "$secret" -o jsonpath='{.data.tls\.crt}' \
    | base64 -d \
    | openssl verify -CAfile <(kubectl -n dpf-operator-system get secret \
        dpf-provisioning-ca-secret -o jsonpath='{.data.tls\.crt}' | base64 -d) /dev/stdin
done
```

Continue only when every leaf certificate reports `/dev/stdin: OK`.

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

## 5) Close dual-trust window by pruning old CA from trust bundle

This step closes the dual-trust window by removing the old CA from the trust bundle after all
clients/servers have switched successfully. Perform this only after Step 4 convergence is complete,
otherwise components still using the old chain may lose trust.

Edit `dpf-ca-trust-bundle` and remove the old CA PEM block from `data.ca.crt`:

```bash
kubectl -n dpf-operator-system edit configmap dpf-ca-trust-bundle
```

Check before moving on:

```bash
# Capture the new desired hash after prune
export BUNDLE_HASH=$(kubectl -n dpf-operator-system get configmap dpf-ca-trust-bundle \
  -o jsonpath='{.data.bundle-hash}')
echo "$BUNDLE_HASH"
```

Then repeat the dual-trust convergence checks from Step 2 against this new `bundle-hash`.

## 6) Cleanup old CA secret

This is a cleanup step. Delete the backup old-CA secret only after Step 5 convergence is complete
and the old CA has been pruned from `dpf-ca-trust-bundle`.

```bash
kubectl -n dpf-operator-system delete secret dpf-provisioning-ca-secret-old
```

# Automated self-signed CA rotation

DPF provides `hack/scripts/self-signed-ca-rotation.sh` to perform self-signed CA rotation and wait
for each change to take effect. Run the script from the repository root.

The script reads `dpfoperatorconfig.spec.deploymentMode` and adjusts the workflow automatically:

* In `zero-trust` mode, it verifies DPU BMC and DPU OS trust propagation and rotates DPU BMC server
  certificates.
* In `host-trusted` mode, it verifies DPU OS trust propagation and skips DPU BMC operations.

## Run the complete rotation

Use `run-all` to perform the complete rotation:

```bash
./hack/scripts/self-signed-ca-rotation.sh run-all
```

This command validates the required tools and Kubernetes resources, runs Steps 1 through 6 in
order, and waits for each step to complete. It skips completed steps and stops if an action fails
or a check times out.

After resolving a reported problem, rerun the same command. To intentionally skip earlier steps,
use `--from-step <number>` only after confirming that those steps are complete:

```bash
./hack/scripts/self-signed-ca-rotation.sh run-all --from-step 3
```

## Run the rotation step by step

Before running individual steps, validate the required tools and Kubernetes resources without
changing the cluster:

```bash
./hack/scripts/self-signed-ca-rotation.sh precheck
```

Run each command only after reviewing the corresponding manual step:

```bash
./hack/scripts/self-signed-ca-rotation.sh step1-backup
./hack/scripts/self-signed-ca-rotation.sh step2-renew-ca
./hack/scripts/self-signed-ca-rotation.sh step3-switch-ca
./hack/scripts/self-signed-ca-rotation.sh step4-rotate-cert
./hack/scripts/self-signed-ca-rotation.sh step5-prune-old-ca
./hack/scripts/self-signed-ca-rotation.sh step6-cleanup
```

Each command performs one action and waits for its completion checks to pass.

## Configure timeouts

The default timeout for each completion check is 600 seconds, and the default polling interval is
5 seconds. Increase these values when convergence is expected to take longer:

```bash
./hack/scripts/self-signed-ca-rotation.sh \
  --timeout-sec 1200 \
  --wait-interval-sec 10 \
  run-all
```

Run `./hack/scripts/self-signed-ca-rotation.sh --help` for all commands and options.

# Related topics

* [DPU BMC mTLS Server Certificate Rotation](zero-trust-advanced-configuration.md#bmc-mtls-server-certificate-rotation)
