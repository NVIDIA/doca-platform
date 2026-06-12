---
title: "Per-DPU BMC Credentials"
---

[[_TOC_]]

By default, DPF in Zero Trust mode uses a single shared secret (`bmc-shared-password`) to authenticate with the BMC on every DPU in a
namespace. The per-DPU BMC credentials feature allows each DPUDevice to reference its own secret containing a unique
BMC password via `DPUDevice.spec.bmcCredentialSecretName`. When set, the per-device password takes precedence over the
shared one.

# Creating a Per-DPU Credential secret

Create a Kubernetes secret in the same namespace as the DPUDevice. The secret must contain a `password` key with the
BMC credential value and should be created with `immutable: true`.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: my-dpu-bmc-v1
  namespace: dpf-operator-system
type: Opaque
immutable: true
stringData:
  password: <bmc-password>
```

> [!IMPORTANT]
> If the secret is not created with `immutable: true`, DPF sets it to immutable after reading the password. This
> enforces the rotation model where a new secret with a different name is required for password changes.

# Assigning a Credential to a DPUDevice

Set `spec.bmcCredentialSecretName` on the DPUDevice to the name of the secret:

```yaml
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUDevice
metadata:
  name: my-dpu
  namespace: dpf-operator-system
spec:
  bmcCredentialSecretName: my-dpu-bmc-v1
  bmcIp: 10.0.110.122
```

When this field is set, DPF uses the password from the referenced secret for all BMC authentication operations on that
DPU. If the field is not set, DPF falls back to the shared `bmc-shared-password` secret.

> [!NOTE]
> When `bmcCredentialSecretName` is set but the secret is missing or invalid, DPF reports an error rather than falling
> back to the shared password.

# Password Rotation

To rotate a DPU's BMC password:

1. Create a new immutable secret with a different name containing the new password (see
   [Creating a Per-DPU Credential secret](#creating-a-per-dpu-credential-secret)).
2. Update `spec.bmcCredentialSecretName` on the DPUDevice to reference the new secret.
3. DPF detects that `spec.bmcCredentialSecretName` differs from `status.bmcCredentialSecretName`, authenticates to
   the BMC and updates the password to the new one.
4. Wait for `status.bmcCredentialSecretName` to match the spec value, then delete the old secret.

> [!NOTE]
> DPF places a finalizer on the active credential secret, so deletion is blocked until the controller has finished
> the rotation and moved to the new secret. See [Secret Protection](#secret-protection) for details.

# Mode Switching

## Shared to Per-Device (Allowed)

Setting `spec.bmcCredentialSecretName` on a DPUDevice that was previously using the shared password is treated as a
password rotation from the shared password to the per-device one.

## Per-Device to Shared (Not Allowed)

Switching from a per-device credential back to shared mode is not allowed. If the DPUDevice status shows a per-device
credential and the spec is changed to remove `bmcCredentialSecretName` (or set it to `bmc-shared-password`), DPF sets
`BMCCredentialsReady=False` with reason `ModeSwitchNotAllowed` and does not perform any BMC operations.

To return to shared mode, delete the DPUDevice and recreate it without `bmcCredentialSecretName`.

# Monitoring Credential Health

The DPUDevice reports credential status through the `BMCCredentialsReady` condition:

| Condition                   | Reason                      | Meaning                                                              |
|-----------------------------|-----------------------------|--------------------------------------------------------------------- |
| `BMCCredentialsReady=True`  | `CredentialsValid`          | Credential secret is valid and authentication succeeded              |
| `BMCCredentialsReady=False` | `CredentialsSecretNotFound` | The referenced secret does not exist                                 |
| `BMCCredentialsReady=False` | `CredentialsSecretInvalid`  | The secret exists but is malformed (missing or empty `password` key) |
| `BMCCredentialsReady=False` | `BMCAuthenticationFailed`   | The password was rejected by the BMC                                 |
| `BMCCredentialsReady=False` | `ModeSwitchNotAllowed`      | Attempted to switch from per-device to shared mode                   |

Check the `BMCCredentialsReady` condition on the DPUDevice status to diagnose issues.

# Secret Protection

DPF adds a finalizer (`provisioning.dpu.nvidia.com/bmc-credential`) to per-device credential secrets to prevent
accidental deletion while a DPUDevice depends on them. The finalizer is automatically removed when:

* The DPUDevice is deleted.
* The credential is rotated to a new secret (the finalizer moves from the old secret to the new one).
