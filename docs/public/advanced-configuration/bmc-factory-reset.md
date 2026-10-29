---
title: "BMC Factory Reset"
---

[[_TOC_]]

While it initializes a DPUDevice, DPF resets that DPU's BMC to factory defaults once, before it
configures anything on the BMC. This puts every DPU into a known state regardless of what was
configured on it before it reached DPF, and it lets DPF harden every BMC account it manages off the
factory default password from a single known starting point.

> [!WARNING]
> A factory reset erases **all** BMC configuration, including the BMC network settings. A BMC with a
> statically configured IP address loses that address and becomes unreachable at the address in
> `DPUDevice.status.bmcIp`. Only BMCs that get their address over DHCP, from a DHCP server that
> hands back the same address after the reset, are safe to reset. Everything else must opt out with
> [`spec.bmcFactoryResetPolicy: Never`](#opting-out).

The reset happens at most once per DPUDevice, and only while the device is being initialized —
before DPF has provisioned anything on the DPU. Upgrading DPF never resets a BMC that DPF already
manages.

> [!NOTE]
> The reset is a Redfish operation, so it only applies when the DPFOperatorConfig install interface
> is `installViaRedfish`. With any other install interface, such as `installViaHostAgent`, DPF never
> resets the BMC, whatever `spec.bmcFactoryResetPolicy` says, and reports
> `BMCFactoryResetReady=True` with reason `FactoryResetSkipped`. A reset that was already submitted
> or completed under `installViaRedfish` keeps the condition it recorded.
>
> Switching an existing DPUDevice to `installViaRedfish` does not reset its BMC either: DPF already
> manages that device. To reset it, delete and recreate the DPUDevice.

# Opting out

`DPUDevice.spec.bmcFactoryResetPolicy` controls the reset for a single device:

| Value | Meaning |
|-------|---------|
| `OnInitialization` (default) | Reset the BMC once, while the DPUDevice is initialized |
| `Never` | Never reset the BMC. Use this for a BMC with a statically configured IP address |

```yaml
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUDevice
metadata:
  name: my-dpu
  namespace: dpf-operator-system
spec:
  serialNumber: "MT25066004C7"
  bmcIp: 10.0.110.122
  bmcFactoryResetPolicy: Never
```

The field is only read until the reset step finishes. Once `BMCFactoryResetReady` is `True`,
changing the policy has no effect — in particular, flipping it back to `OnInitialization` does not
reset a device a second time.

## Opting out for discovered devices

[`DPUDiscovery`](../developer-guides/api/dpudiscovery.md) creates DPUDevices itself, so the policy
for those devices comes from the DPFOperatorConfig:

```yaml
apiVersion: operator.dpu.nvidia.com/v1alpha1
kind: DPFOperatorConfig
metadata:
  name: dpfoperatorconfig
  namespace: dpf-operator-system
spec:
  provisioningController:
    installInterface:
      installViaRedfish:
        enabled: true
        discoveredDPUDeviceBMCFactoryResetPolicy: Never
```

When this field is omitted, the discovery controller uses `OnInitialization`. The resolved value is
stamped into `spec.bmcFactoryResetPolicy` when a DPUDevice is created. Changing it later does not
affect DPUDevices that already exist; edit those devices individually instead.

# What the reset does to BMC passwords

A factory reset returns every BMC account to the factory default password, so DPF sets the password
from the [BMC credential secret](per-dpu-bmc-credentials.md) on every account it manages right
after the reset completes:

* The Redfish user (`root` on BlueField-3, `admin` on BlueField-4).
* On BlueField-4, the ssh-only `service` account. Firmware that does not expose this account is
  tolerated: the failure is logged and initialization continues.

Because the password is applied to more than one account on BlueField-4, it must satisfy the
account policy of both. A password the BMC rejects — most commonly for being shorter than the
minimum length of 13 characters — fails with an error naming the account and quoting the BMC's own
reason.

# Monitoring the reset

The DPUDevice reports the reset through the `BMCFactoryResetReady` condition. `True` means the
step is finished, not that a reset is running:

| Condition | Reason | Meaning |
|-----------|--------|---------|
| `BMCFactoryResetReady=True` | `FactoryResetCompleted` | The BMC was reset and is reachable again |
| `BMCFactoryResetReady=True` | `FactoryResetSkipped` | No reset was performed, because the policy is `Never`, because DPF already managed the device before this feature existed, or because the install interface is not Redfish |
| `BMCFactoryResetReady=False` | `FactoryResetInProgress` | The BMC accepted the reset and DPF is waiting for it to come back |
| `BMCFactoryResetReady=False` | `FactoryResetAwaitingCompletion` | The BMC accepted the reset but still answers with its pre-reset password. DPF keeps checking that password until the reset starts |
| `BMCFactoryResetReady=False` | `FactoryResetCredentialRejected` | The pre-reset Secret password was rejected. DPF retries only the factory default unless that Secret password changes |
| `BMCFactoryResetReady=False` | `FactoryResetCredentialRecordFailed` | DPF could not save the rejected-password marker. Completion probes pause until an explicit retry |
| `BMCFactoryResetReady=False` | `FactoryResetFailed` | DPF could not submit the reset, or the BMC rejected both known passwords after accepting one. A submission failure is retried. After the reset was accepted, the rejected Secret password is not sent again until that password changes. A retry after the Secret changes sends the factory default again |

`status.bmcFactoryResetRequestTime` records when the reset was submitted. Once persisted, this
timestamp prevents another reset submission for the same DPUDevice and is never cleared. The credential Secret
carries `provisioning.dpu.nvidia.com/fr-rejected-<dpudevice-uid>`, the SHA-256 of the Secret
password the BMC rejected while confirming that reset. A later Secret edit can be distinguished
from the password the BMC already refused. The marker is on the Secret, not the DPUDevice.
DPF removes the device's marker from the currently referenced Secret when the DPUDevice is deleted.

Rejection conditions are saved through the normal deferred status patch. If saving the Secret
marker fails, `FactoryResetCredentialRecordFailed` stops completion probes once status is persisted.
A crash or status-write failure can still repeat a login; rejection tracking is best effort across
persistence failures.

```bash
kubectl get dpudevice MT25066004C7 -n dpf-operator-system \
  -o jsonpath='{.status.conditions[?(@.type=="BMCFactoryResetReady")]}'
```

While the reset is in flight, `Initialized` stays `False` with reason `Pending`.

# Troubleshooting

**The device is stuck at `FactoryResetInProgress`.** DPF submitted the reset but the completion
probe has not succeeded yet. The condition message carries the time elapsed since the request.

* If the message says the BMC is still coming back, the usual cause is that the BMC came back at a
  different address — statically configured, or DHCP handed it a new lease. Correct
  `status.bmcIp` — by letting discovery find the BMC at its new address, or by patching it — and
  the device continues on the next reconcile.

**The condition reason is `FactoryResetAwaitingCompletion`.** The BMC answered and the reset has
not taken effect yet (for example it has not rebooted and still holds the Secret password). DPF
keeps checking that password and does not resubmit the reset. Once the password stops working, DPF
checks the factory default once and continues initialization, including password hardening. If that
check times out, the reason becomes `FactoryResetCredentialRejected`: DPF retries only the factory
default and does not send the rejected Secret password again, because another rejection can lock
the BMC account. Updating the Secret password is treated as a new credential and is probed.

A post-reset BlueField BMC that answers `/redfish/v1` with the factory default password but returns
`403 PasswordChangeRequired` on Managers is **not** stuck: that is how DPF detects the reset took
effect, and the next initialization step changes the password to clear the requirement.

**The condition reason is `FactoryResetCredentialRecordFailed`.** Fix the Secret write error
reported in the condition message, then set the rejection annotation to `retry` using the command
below. A password change alone cannot resume probes because the stored marker may be missing or stale.

**The device is stuck at `FactoryResetFailed` with "the BMC accepts neither the password in the
credential Secret nor the factory default".** This is a *submission* failure:
`status.bmcFactoryResetRequestTime` is still unset, so DPF has not yet asked the BMC to reset.
The BMC holds a password DPF does not know. Put the correct password in the credential secret, or
reset the BMC out of band, and DPF submits on the next reconcile.

**The device is stuck at `FactoryResetFailed` after the reset was accepted.**
`status.bmcFactoryResetRequestTime` is set, so DPF will not submit `ResetToDefaults` again. Both
the factory default password and the Secret password were rejected while confirming the reset;
repeating either can lock the BMC account.

Recovery, without clearing the request time:

* **Update the credential Secret** with the password the BMC now holds, or with the password you
  want DPF to set after an out-of-band factory reset. DPF records the rejected password on the
  Secret and probes again only when that password changes. That retry sends the factory default
  again, so a BMC that is back on the factory default completes without the new Secret password
  needing to work yet. A metadata-only Secret edit (labels, other annotations) does not count as
  a change. Switching `spec.bmcCredentialSecretName` to a new Secret is also not treated as a
  password change. Set the rejection annotation to `retry` on the new Secret using the command
  in the next item. After switching, manually remove this device's marker from the old Secret;
  DPF no longer tracks it for marker cleanup:

  ```bash
  uid=$(kubectl get dpudevice <name> -n dpf-operator-system -o jsonpath='{.metadata.uid}')
  kubectl annotate secret <old-bmc-credential-secret> -n dpf-operator-system \
    "provisioning.dpu.nvidia.com/fr-rejected-${uid}-"
  ```

* **Set the rejection annotation to `retry`** if the Secret password did not change — for example
  you reset the BMC out of band and it is back on the factory default. Use this also after fixing
  a failure to save the rejection marker: without a reliable marker, DPF cannot identify the rejected
  password. Deleting the annotation alone does not authorize another login.

  ```bash
  uid=$(kubectl get dpudevice <name> -n dpf-operator-system -o jsonpath='{.metadata.uid}')
  kubectl annotate secret <bmc-credential-secret> -n dpf-operator-system \
    "provisioning.dpu.nvidia.com/fr-rejected-${uid}=retry" --overwrite
  ```

  Do **not** clear `status.bmcFactoryResetRequestTime`. That would submit a second factory reset.
* Recreating the DPUDevice also recovers, and resets the BMC again unless the recreated device
  opts out.

**A reset that must not happen already did.** There is no undo. Restore the BMC configuration
manually, and set `spec.bmcFactoryResetPolicy: Never` on the remaining devices, or
`discoveredDPUDeviceBMCFactoryResetPolicy: Never` in the DPFOperatorConfig, before onboarding more
DPUs.

# Notes

* Deleting and recreating a DPUDevice re-onboards the DPU from scratch, and resets its BMC again
  unless the recreated device opts out.
* The `ResettingBMC` condition, which older DPF versions set when the mTLS setup path reset a BMC,
  no longer exists. `BMCFactoryResetReady` replaces it, with the opposite polarity.
