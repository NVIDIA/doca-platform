---
title: "Zero Trust BMC RShim troubleshooting"
---

# Zero Trust BMC RShim troubleshooting (BlueField-3)

In Zero Trust provisioning, DPF disables host RShim and enables BMC RShim through Redfish during **Config FW Parameters**, then waits for `BmcRShimEnabled=true` before preparing the BFB.

## Symptoms

The DPU stays in `Config FW Parameters`. The `FWConfigured` condition shows one of:

| Reason | Meaning |
|---|---|
| `WaitingForBMCRShim` | BMC RShim was requested once; waiting up to 10 minutes. |
| `DPUArmPowerOnTimeout` | The DPU Arm did not power on within 5 minutes of a one-shot Arm ForceRestart. |
| `BMCRShimUnavailableLowPowerStandby` | As above, with `PowerState=Paused`, `Status.State=StandbyOffline`, `BootProgress.OemLastState=LowPowerStandby`. The BMC RShim USB endpoint cannot attach. |
| `BMCRShimWaitTimeout` | The Arm is powered but BMC RShim is still disabled 10 minutes after the request. |

The last three also set the `FWBMCRShimStalled` condition, raise a Warning event and fire the `DPFDPUBMCRShimStalled` alert. The DPU is **not** moved to `Error`: provisioning continues automatically once the DPU is recovered.

## Diagnose

* Collect a BMC dump first (`dpfctl dump`, see [Diagnostic Dumps](dpfctl/dump.md)).
* Redfish ComputerSystem: check `PowerState`, `Status.State`, `BootProgress.OemLastState`.
* Redfish `Managers/Bluefield_BMC/Oem/Nvidia`: check `BmcRShim.BmcRShimEnabled`.
* On the BMC: `systemctl status rshim` and `ls /dev/rshim*`.

`HostRshim=Disabled` and `BmcRShimEnabled=true` do not prove that `/dev/rshim0` is usable; only the USB attachment does.

## Recover

For the LowPowerStandby signature, the validated recovery is a chassis power reset on the DPU BMC:

```
ipmitool chassis power reset
```

This is disruptive. Collect diagnostics first and do not script it without guards.
