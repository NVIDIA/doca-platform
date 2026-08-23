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
| `BMCFactoryResetReady=True` | `FactoryResetSkipped` | No reset was performed, because the policy is `Never` or because DPF already managed the device before this feature existed |
| `BMCFactoryResetReady=False` | `FactoryResetInProgress` | The BMC accepted the reset and DPF is waiting for it to come back |
| `BMCFactoryResetReady=False` | `FactoryResetFailed` | DPF could not submit the reset. This is retried, not terminal |

`status.bmcFactoryResetRequestTime` records when the reset was submitted. It is never cleared, and
it is what guarantees a BMC is not reset twice for the same DPUDevice.

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
* If the message says the BMC is reachable but still rejects the factory default password, the BMC
  answered and the reset has not taken effect (for example it never rebooted and still holds the
  Secret password). DPF does not resubmit the reset. Reset the BMC out of band, or delete and
  recreate the DPUDevice if a fresh onboarding is acceptable.

DPF waits indefinitely rather than failing on either path, and never resubmits the reset while
`status.bmcFactoryResetRequestTime` is set.

A post-reset BlueField BMC that answers `/redfish/v1` with the factory default password but returns
`403 PasswordChangeRequired` on Managers is **not** stuck: that is how DPF detects the reset took
effect, and the next initialization step changes the password to clear the requirement.

**The device is stuck at `FactoryResetFailed` with "the BMC accepts neither the password in the
credential Secret nor the factory default".** The BMC holds a password DPF does not know, so it
cannot be reset over Redfish. Put the correct password in the credential secret, or reset the BMC
out of band, and DPF proceeds on the next reconcile.

**A reset that must not happen already did.** There is no undo. Restore the BMC configuration
manually, and set `spec.bmcFactoryResetPolicy: Never` on the remaining devices, or
`discoveredDPUDeviceBMCFactoryResetPolicy: Never` in the DPFOperatorConfig, before onboarding more
DPUs.

# Notes

* Deleting and recreating a DPUDevice re-onboards the DPU from scratch, and resets its BMC again
  unless the recreated device opts out.
* The `ResettingBMC` condition, which older DPF versions set when the mTLS setup path reset a BMC,
  no longer exists. `BMCFactoryResetReady` replaces it, with the opposite polarity.
