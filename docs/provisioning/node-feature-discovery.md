# Node DPU discovery

## Install NFD on vanilla kubernetes
Follow [steps](https://kubernetes-sigs.github.io/node-feature-discovery/stable/get-started/index.html#quick-start--the-short-short-version) to install NFD on vanilla kubernetes

## Install NFD on OpenShift Cluster
1. Submit [SCC (Security Context Constraints)](https://docs.openshift.com/container-platform/4.14/authentication/managing-security-context-constraints.html)

```bash
cat << EOF > scc-nfd-worker.yaml
allowHostDirVolumePlugin: true
allowHostIPC: false
allowHostNetwork: true
allowHostPID: false
allowHostPorts: true
allowPrivilegeEscalation: true
allowPrivilegedContainer: true
allowedCapabilities: null
apiVersion: security.openshift.io/v1
defaultAddCapabilities: null
fsGroup:
  type: MustRunAs
groups: []
kind: SecurityContextConstraints
metadata:
  name: nfd-worker
priority: null
readOnlyRootFilesystem: false
requiredDropCapabilities:
- KILL
- MKNOD
- SETUID
- SETGID
runAsUser:
  type: RunAsAny
seLinuxContext:
  type: MustRunAs
supplementalGroups:
  type: MustRunAs
seccompProfiles:
- '*'
users:
- system:serviceaccount:node-feature-discovery:nfd-worker
volumes:
- configMap
- downwardAPI
- emptyDir
- persistentVolumeClaim
- projected
- secret
- hostPath
EOF

oc create -f scc-nfd-worker.yaml
```
2. Update `securityContext` for nfd-worker daemonset. 
```bash
oc patch daemonset nfd-worker -n node-feature-discovery -p \
  '{"spec": {"template": {"spec": {"containers": [{"name": "nfd-worker","securityContext": {"allowPrivilegeEscalation": true}}]}}}}'

oc patch daemonset nfd-worker -n node-feature-discovery -p \
  '{"spec": {"template": {"spec": {"hostNetwork": true}}}}'

oc patch daemonset nfd-worker -n node-feature-discovery -p \
  '{"spec": {"template": {"spec": {"containers": [{"name": "nfd-worker","securityContext": {"privileged": true}}]}}}}'
```

## NFD worker configuration
In order to be able to discovery DPU device, NFD needs to update the `nfd-worker-conf` configmap.

Run `oc edit cm nfd-worker-conf -n node-feature-discovery` command, then update the configuration as follows:
```
sources:
  pci:
    deviceClassWhitelist:
      - "0200"
    deviceLabelFields:
      - "class"
      - "vendor"
      - "device"
  local:
    hooksEnabled: true
```

After enabling the above configuration, you can see that some DPU-related labels are added to the host with the DPU device through NFD. For example:
```
feature.node.kubernetes.io/pci-0200_15b3_a2dc.present=true
feature.node.kubernetes.io/pci-0200_15b3_a2dc.sriov.capable=true
```

## Customizatioin NodeFeatureRule
[`NodeFeatureRule`](https://kubernetes-sigs.github.io/node-feature-discovery/v0.15/usage/customization-guide.html#nodefeaturerule-custom-resource) objects provide an easy way to create customized label to describe DPU information. The label will be used in `DPUSet`. Following is an example to discovery `a2dc`(MT43244 BlueField-3 integrated ConnectX-7 network controller) and `a2d6`(MT42822 BlueField-2 integrated ConnectX-6 Dx network controller) devices, and NFD wil add a `feature.node.kubernetes.io/dpu-enabled=true` label to these nodes.
```
cat << 'EOF' | tee nfr.yaml
apiVersion: nfd.k8s-sigs.io/v1alpha1
kind: NodeFeatureRule
metadata:
  name: dpu-detection-rule
spec:
  rules:
  - name: "DPU-detection-rule"
    labels:
      "dpu-enabled": "true"
    matchFeatures:
    - feature: pci.device
      matchExpressions:
        vendor: {op: In, value: ["15b3"]}
        device: {op: In, value: ["a2dc", "a2d6"]}
EOF

oc apply -f nfr.yaml
```

## Discovery device ID and PCI address
NFD can use a script to discovery the DPU device ID and DPU PCI address and will label to node. The `nfd-worker` should use [full](https://kubernetes-sigs.github.io/node-feature-discovery/v0.15/deployment/image-variants.html#full) image to support run shell-based hooks. 

Following is an example to add device ID and PCI address labels to node. Put this script to /etc/kubernetes/node-feature-discovery/source.d/ directory on all the worker node.
```
#!/usr/bin/bash

sysfs_base_path="/sys/bus/pci/devices"

# 0xa2dc: MT43244 BlueField-3 integrated ConnectX-7 network controller
# 0xa2d6: MT42822 BlueField-2 integrated ConnectX-6 Dx network controller
dpu_device_list=("0xa2dc" "0xa2d6")

device_key="dpu-deviceID"
pci_address_key="dpu-pciAddress"
pf_name_key="dpu-pf-name"

device_list=($(find "${sysfs_base_path}" -mindepth 1 -maxdepth 1))


for sub_dir in "${device_list[@]}"; do
    pci_address=$(basename "${sub_dir}")
    device=`cat ${sub_dir}/device`
    for dpu_device in "${dpu_device_list[@]}"; do
        if [ "${dpu_device}" = "${device}" ]; then
            trimmed_string="${pci_address%.*}"
            p0_pci_address=${trimmed_string}.0
            pf_list=($(find "${sysfs_base_path}/${p0_pci_address}/net" -mindepth 1 -maxdepth 1))
            pf_device=$(basename "${pf_list[0]}")
            format_pci_address="${trimmed_string//:/-}"
            echo "${pci_address_key}=${format_pci_address}"
            echo "${device_key}=${dpu_device}"
            echo "${pf_name_key}=${pf_device}"
        fi
    done
done
```