---
title: "DPUSet"
---

[TOC]

The DPUSet is a Kubernetes CRD which managed the DPU CRs in DPF.

## Updating the DPUSet

An update to the DPUSet can be done for upgrading the BFB or modifying provisioning parameters.

> [!NOTE]
> This operation will result in a network
> disruption and also a host reboot.
> A rolling update can be configured to control the number of nodes that will be out-of-service in parallel (Please see
> the DPUSet YAML example below).
> The cluster can also be divided into several DPU-Sets, please refer to the section
> "Using several DPU Sets"

These are the required steps for upgrading the BFB on a set of DPUs
(The BFB is specified as part of the DPU Set CRD):

**1.** Create a BFB YAML that includes the required BFB file and also assigns a distinct name for the object (Different
from the currently used BFB objects). After applying the YAML, the BFB will be pulled from the specified URL to the
shared storage:

```yaml
---
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: BFB
metadata:
  name: bf-bundle-new
  namespace: dpf-operator-system
spec:
  url: https://content.mellanox.com/BlueField/BFBs/Ubuntu22.04/bf-bundle-3.1.0-76_25.07_ubuntu-22.04_prod.bfb
```

**2.** Update the DPUSet YAML to point to the new BFB object:

```yaml
---
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUSet
metadata:
  name: dpuset
  namespace: dpf-operator-system
spec:
  dpuNodeSelector:
    matchLabels:
      feature.node.kubernetes.io/dpu-enabled: "true"
  strategy:
    rollingUpdate:
      maxUnavailable: "10%"
    type: RollingUpdate
  dpuTemplate:
    spec:
      dpuFlavor: dpf-provisioning-hbn-ovn
      bfb:
        name: bf-bundle-new
      nodeEffect:
        taint:
          key: "dpu"
          value: "provisioning"
          effect: NoSchedule
```

**3.** Then delete the DPU objects of the relevant DPUs.

This will initiate a provisioning cycle for the DPUs using the new BFB image:

```shell
kubectl delete dpu -n dpf-operator-system worker1-0000-2b-00 worker2-0000-2b-00
```
**4.** You can later delete the previous BFB object:

```shell
kubectl delete bfb -n dpf-operator-system bf-bundle
```

### Using several DPU Sets

There's an option to create several DPU-Set objects, and assign them to different groups of worker nodes.
This is done by adding relevant labels to the node selector in the DPUSet object YAML.
Each DPU Set can use a different BFB object, can have a different DPU flavor, a different rolling update strategy, etc.

For example:

```yaml
---
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUSet
metadata:
  name: dpuset-dk
  namespace: dpf-operator-system
spec:
  dpuNodeSelector:
    matchLabels:
      e2e.servers/dk: "true"
  strategy:
    rollingUpdate:
      maxUnavailable: "10%"
    type: RollingUpdate
  dpuTemplate:
    spec:
      dpuFlavor: dpf-provisioning-hbn-ovn
      bfb:
      name: bf-bundle-dk-ga
      nodeEffect:
      taint:
        key: "dpu"
        value: "provisioning"
        effect: NoSchedule
```

## DPU Selection

The DPUSet provides two complementary mechanisms for selecting which DPUs should be managed: `dpuNodeSelector` and
`dpuSelector`.

### dpuNodeSelector

The `dpuNodeSelector` field is used to select DPUNodes based on their labels. It uses the 
standard Kubernetes LabelSelector format, which supports both `matchLabels` and `matchExpressions`.

For example, to select nodes with a specific label:

```yaml
spec:
  dpuNodeSelector:
    matchLabels:
      feature.node.kubernetes.io/dpu-enabled: "true"
```

Or using match expressions for more complex selection:

```yaml
spec:
  dpuNodeSelector:
    matchExpressions:
      - key: environment
        operator: In
        values:
          - production
          - staging
```

### dpuSelector

The `dpuSelector` field is used to further filter DPUDevices based on their labels. This is applied after the
`dpuNodeSelector` and allows you to select specific DPUDevices on the selected DPUNodes.

The `dpuSelector` uses a simple map of label key-value pairs, where all specified labels must match (AND logic).

For example, to select only specific DPU models or configurations:

```yaml
spec:
  dpuNodeSelector:
    matchLabels:
      feature.node.kubernetes.io/dpu-enabled: "true"
  dpuSelector:
    provisioning.dpu.nvidia.com/dpudevice-pciAddress: "0000:1a:00.0"
```

In Host Trusted model, the automatically created DPUDevice objects have the following labels:

- `provisioning.dpu.nvidia.com/dpudevice-num-of-pfs`: The number of PFs on the DPU device
- `provisioning.dpu.nvidia.com/dpudevice-pciAddress`: The PCI address of the DPU device
- `provisioning.dpu.nvidia.com/dpudevice-pf0-name`: The name of PF0 on the DPU device
- `provisioning.dpu.nvidia.com/dpunode-name`: The name of the DPUNode the DPU is part of

### Selection Flow


## Host Power-cycle in DPU provisioning

If the version of running BFB is lower than 2.7 before DPU provisioning, the BlueField firmware upgrades and mlxconfig
parameter changes require a host power-cycle. Once the version of BFB is updated to be greater than or equal to 2.7 a
regular reboot would be enough.

For enabling this, the DPUSet provides one annotations in dpuTemplate:
`provisioning.dpu.nvidia.com/host-power-cycle-required` - trigger the host power-cycle (cold boot) instead of warm reboot
after DPU provisioning, notice that after the power cycle command is done the annotation would be removed from the DPU
and DPUSet objects.

Following is an example to enable host power-cycle:

```yaml
---
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUSet
metadata:
  name: dpuset
  namespace: dpf-operator-system
spec:
  dpuNodeSelector:
    matchLabels:
      feature.node.kubernetes.io/dpu-enabled: "true"
  strategy:
    rollingUpdate:
      maxUnavailable: "10%"
    type: RollingUpdate
  dpuTemplate:
    annotations:
      provisioning.dpu.nvidia.com/host-power-cycle-required: "true"
    spec:
      dpuFlavor: dpf-provisioning-hbn-ovn
      bfb:
        name: bf-bundle-new
      nodeEffect:
        taint:
          key: "dpu"
          value: "provisioning"
          effect: NoSchedule
```

## IPMI Command Annotation for Kubernetes Worker Node

The provisioning controller will issue a `ipmi` command to the host to do host power-cycle(cold boot) or warm reboot
after DPU provisioning. The default host power-cycle command is `ipmitool chassis power cycle` and warm reboot command
is `ipmitool chassis power reset`

For some kinds of servers that uses `ipmitool chassis power reset` command for host cold power-cycle instead of
`ipmitool chassis power cycle`. DPF supports changing the host power-cycle/warm reboot command by setting the following
annotation on such kind of worker nodes:

```yaml
provisioning.dpu.nvidia.com/powercycle-command: reset
provisioning.dpu.nvidia.com/reboot-command: cycle
```

## Node effect

Node effect specifies how changes to the DPU should affect the Kubernetes Node the DPU belongs to. Only the following
options can be specified:

* noEffect (bool) - no effect on the node at all
* customLabel (object) - adds the label provided to the Kubernetes Node and DPUNode, ONLY relevant to Kubernetes environment,
    when there is a match of DPUNode and Kubernetes Node
* taint (object) - marks the node as tainted, ONLY relevant to Kubernetes environment, when there is a match of DPUNode
    and Kubernetes Node
* drain (bool)(default) - drains the node and waits till the draining is finished, ONLY relevant to Kubernetes environment,
    when there is a match of DPUNode and Kubernetes Node - this is the default behaviour in a Kubernetes environment
* customAction (string) - name of a ConfigMap which contains a Pod definition - in YAML - to run which will apply the node
    effect. The Pod is expected to exit when node effect is done, if the Pod terminates with error then DPU would move
    to Error phase. First, create a ConfigMap with the pod definition:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: custom-node-effect
  namespace: dpf-operator-system
data:
  pod.yaml: |
    apiVersion: v1
    kind: Pod
    metadata:
      name: custom-node-effect-pod
      namespace: dpf-operator-system
    spec:
      containers:
      - name: node-effect
        image: ubuntu:20.04
        command: ["/bin/bash"]
        args:
        - -c
        - |
          # Example custom node effect script
          echo "Applying custom node effect..."
          # Add your custom logic here
          # For example: network configuration, system checks, reboot.
          sleep 10  # Simulating some work
          exit 0    # Exit successfully when done
      restartPolicy: Never
```

Then, create the DPUSet that uses this custom action

```yaml
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUSet
metadata:
  name: dpuset-custom-effect
  namespace: dpf-operator-system
spec:
  dpuNodeSelector:
    matchLabels:
      feature.node.kubernetes.io/dpu-enabled: "true"
  strategy:
    rollingUpdate:
      maxUnavailable: "10%"
    type: RollingUpdate
  dpuTemplate:
    spec:
      dpuFlavor: dpf-provisioning-hbn-ovn
      bfb:
        name: bf-bundle-new
      nodeEffect:
        customAction: custom-node-effect
```

* hold (bool) - places an annotation with key `wait-for-external-nodeeffect` on the DPU object and waits for it to be
    removed - this is the default behaviour in a non Kubernetes environment
