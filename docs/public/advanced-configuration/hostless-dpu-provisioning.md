---
title: "Hostless DPU Provisioning"
---

[[_TOC_]]

Hostless DPU provisioning is used when a DPU is managed without a host-side Kubernetes node or host agent. In this mode, DPF manages the DPU through Redfish and creates a synthetic `DPUNode` for the hostless `DPUDevice`.

This mode is intended for environments where the DPU is reachable through its BMC/Redfish endpoint, but there is no host node that DPF can discover, drain, reboot, or run host-side actions on.

# Requirements

Hostless DPU provisioning has the following requirements:

* DPF must be deployed in `zero-trust` mode.
* The provisioning controller must use `installViaRedfish`.
* `skipDPUNodeDiscovery` must be set to `true`.
* Do not use `DPUDiscovery` for hostless DPUs. Create `DPUDevice` objects manually instead.
* Each hostless `DPUDevice` must have the label `provisioning.dpu.nvidia.com/hostless: "true"`.
* The DPU BMC/Redfish endpoint must be reachable from the provisioning controller.

Example `DPFOperatorConfig` settings:

```yaml
apiVersion: operator.dpu.nvidia.com/v1alpha1
kind: DPFOperatorConfig
metadata:
  name: dpfoperatorconfig
  namespace: dpf-operator-system
spec:
  deploymentMode: zero-trust
  provisioningController:
    installInterface:
      installViaRedfish:
        skipDPUNodeDiscovery: true
```

# Create A Hostless DPUDevice

Create one `DPUDevice` for each hostless DPU. Replace the values in angle brackets with values from your environment.

```yaml
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUDevice
metadata:
  name: <dpu-device-name>
  namespace: dpf-operator-system
  labels:
    provisioning.dpu.nvidia.com/hostless: "true"
spec:
  bmcIp: <bmc-ip-address>
  bmcPort: <bmc-redfish-port>
  numberOfPFs: <number-of-pfs>
  opn: <dpu-opn>
  serialNumber: <dpu-serial-number>
```

For example, if the BMC Redfish endpoint is reachable at `https://<bmc-ip-address>:<bmc-redfish-port>`, set `bmcIp` to the BMC IP address and `bmcPort` to the Redfish HTTPS port.

Apply the manifest:

```bash
kubectl apply -f hostless-dpudevice.yaml
```

After the `DPUDevice` is created, DPF creates a synthetic `DPUNode` for the hostless DPU. You can verify the resources with:

```bash
kubectl get dpudevices -n dpf-operator-system
kubectl get dpunodes -n dpf-operator-system
```

# DPUDiscovery Is Not Used

Do not create a `DPUDiscovery` resource for hostless DPU provisioning. Hostless DPUs must be represented by manually created `DPUDevice` objects because there is no host node for DPF to discover and associate with the DPU.

If `DPUDiscovery` is used, it may create resources that follow the normal Redfish discovery flow rather than the hostless flow. Keep `skipDPUNodeDiscovery: true` and manage hostless `DPUDevice` objects explicitly.

# Node Effect Recommendation

For hostless DPUs, configure node effect as `noEffect`.

```yaml
spec:
  dpus:
    nodeEffect:
      noEffect: true
```

Hostless DPUs do not have a host node for DPF to drain, taint, or run host-side actions on. For this reason, `customAction`, custom scripts, and external node-effect workflows are not recommended for hostless provisioning.

# Reboot Behavior

Hostless reboot handling is different from the usual zero-trust flow.

In the normal zero-trust flow, the environment may need to coordinate host-side actions or external reboot handling. In hostless DPU provisioning, DPF owns the DPU reboot workflow:

* The DPU agent reports `HostlessDPUReboot`.
* The DPU controller moves the DPU to the rebooting phase.
* The Redfish reboot handler triggers a DPU ARM restart through Redfish.
* The controller waits for the DPU OS to report running and for the DPU agent to report a fresh startup before continuing provisioning.

Users do not need to integrate a custom reboot script, external reboot controller, or host-side reboot mechanism for this flow.

# Summary

Use hostless DPU provisioning when a DPU is managed directly through Redfish without a host-side node or host agent. Deploy DPF in zero-trust mode, keep `skipDPUNodeDiscovery: true`, avoid `DPUDiscovery`, create labeled `DPUDevice` objects manually, and use `nodeEffect.noEffect`.
