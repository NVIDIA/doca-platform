---
title: "Per-DPU BMC Credentials"
---

[[_TOC_]]

By default, DPF in Zero Trust mode uses a single shared secret (`bmc-shared-password`) to authenticate with the BMC on every DPU in a
namespace. The per-DPU BMC credentials feature allows each DPUDevice to reference its own secret containing a unique
BMC password via `DPUDevice.spec.bmcCredentialSecretName`. When set, the per-device password takes precedence over the
shared one.

# How DPF selects the BMC password

DPF resolves the BMC password independently for each DPUDevice:

* If `spec.bmcCredentialSecretName` is set, DPF uses the password from that secret for **all** password-based BMC
  authentication on that DPU, and the shared password is not used for it (even if the `bmc-shared-password` secret
  exists).
* If `spec.bmcCredentialSecretName` is not set, DPF falls back to the shared `bmc-shared-password` secret.

The two modes coexist within a namespace: some DPUs can use per-device credentials while others use the shared
password.

## Onboarding modes and the shared password

How a DPU first obtains its credential depends on how its DPUDevice is created.

### Auto-discovery (DPUDiscovery)

When [`DPUDiscovery`](../developer-guides/api/dpudiscovery.md) scans a BMC IP range, it authenticates to
each BMC using the shared `bmc-shared-password` secret and creates a DPUDevice **without**
`spec.bmcCredentialSecretName`. DPUs onboarded this way therefore start on the shared password. To move such a DPU to
a unique password afterwards, set `spec.bmcCredentialSecretName`; DPF treats this as a
[rotation](#password-rotation) from the shared password to the per-device one.

Because the discovery crawler always authenticates with the shared password, the `bmc-shared-password` secret is
**required** whenever `DPUDiscovery` is used.

### Declarative creation

If you create the DPUDevice yourself with both `spec.bmcIp` and `spec.bmcCredentialSecretName` set before discovery
reaches that BMC, DPF authenticates to the BMC directly with the per-device password (changing it from the BMC
default if necessary). In this case the shared password is not used for that DPU. `DPUDiscovery` skips any BMC IP
that already belongs to a DPUDevice, so a pre-declared device is not overridden.

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

> [!IMPORTANT]
> A per-device credential secret must be referenced by only one DPUDevice. Sharing the same
> `bmcCredentialSecretName` across devices is not allowed. If a second DPUDevice references a secret that is already
> in use, DPF sets `BMCCredentialsReady=False` with reason `SharedCredentialSecretNotAllowed` on that device and
> does not use the credential. A device that already adopted the secret in status keeps working. The blocked device
> is requeued periodically and recovers once the conflicting reference is removed. The shared `bmc-shared-password`
> secret is the only credential that may be used by multiple devices.

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

| Condition                   | Reason                              | Meaning                                                              |
|-----------------------------|-------------------------------------|--------------------------------------------------------------------- |
| `BMCCredentialsReady=True`  | `Success`                           | Credential secret is valid and authentication succeeded              |
| `BMCCredentialsReady=False` | `CredentialsSecretNotFound`         | The referenced secret does not exist                                 |
| `BMCCredentialsReady=False` | `CredentialsSecretInvalid`          | The secret exists but is malformed (missing or empty `password` key) |
| `BMCCredentialsReady=False` | `BMCAuthenticationFailed`           | The password was rejected by the BMC                                 |
| `BMCCredentialsReady=False` | `ModeSwitchNotAllowed`              | Attempted to switch from per-device to shared mode                   |
| `BMCCredentialsReady=False` | `SharedCredentialSecretNotAllowed`  | Device references a per-device secret already used by another DPUDevice |

Check the `BMCCredentialsReady` condition on the DPUDevice status to diagnose issues.

# Secret Protection

DPF adds a finalizer (`provisioning.dpu.nvidia.com/bmc-credential`) to per-device credential secrets to prevent
accidental deletion while a DPUDevice depends on them. The finalizer is automatically removed when:

* The DPUDevice is deleted.
* The credential is rotated to a new secret (the finalizer moves from the old secret to the new one).

# Limitations

* Each per-device credential secret (`spec.bmcCredentialSecretName`) can be used by only one DPUDevice. A device
  that shares a secret already adopted by another device is rejected with `SharedCredentialSecretNotAllowed`; the
  established owner keeps working. Only `bmc-shared-password` may be shared.
* The shared `bmc-shared-password` secret cannot currently be globally disabled. It remains the credential used by
  `DPUDiscovery` and the fallback for any DPUDevice without `spec.bmcCredentialSecretName`. Per-device credentials
  are an override applied per DPUDevice, not a cluster-wide replacement of the shared password.
* Auto-discovery (`DPUDiscovery`) cannot onboard a DPU directly onto a unique password; discovered DPUs start on the
  shared password. They can keep using it, or be rotated to a per-device credential afterwards. Onboarding directly
  with a unique password is only possible through [declarative creation](#declarative-creation).
