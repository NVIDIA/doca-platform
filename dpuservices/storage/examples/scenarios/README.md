# DPF Storage Subsystem scenarios

This directory contains examples for various storage scenarios.

> Notes:
> * These scenarios are provided as starting points and should be customized for specific environments. They are not intended to be used as-is.
> * The example manifests include a basic network configuration for demonstration purposes only. The storage service is configured to bridge directly to PF1 on the DPU.

## Required Environment Variables

Before deployment, export these variables in your shell:

```shell
# Provisioning environment variables

# set the download URL for the BFB bundle
export BLUEFIELD_BITSTREAM="https://example.com/bfb/example.bfb"

# DPF environment variables

# set the DPF registry for helm chart
export DPF_CHART_REPO="example.com/dpf"
# set the DPF chart version
export DPF_CHART_VERSION="0.0.0"
# set the DPF image registry address
export DPF_IMAGE_REGISTRY="example.com/dpf"
# set the DPF image tag
export DPF_IMAGE_TAG="0.0.0"
# use '[]' to pull images from the registry that doesn't require auth, or '[{"name": "secret-name"}]' to specify the secret name
export DPF_IMAGE_PULL_SECRET='[]'

# SNAP environment variables

# set the SNAP image registry address
export DPF_SNAP_IMAGE_REGISTRY="example.com/snap"
# set the SNAP image tag
export DPF_SNAP_IMAGE_TAG="0.0.0"
# use '[]' to pull images from the registry that doesn't require auth, or '[{"name": "secret-name"}]' to specify the secret name
export DPF_SNAP_IMAGE_PULL_SECRET='[]'

# NFS CSI environment variables
# Note: NFS CSI is not included in the DPF release.
# For instructions on building a DPF-specific NFS CSI Helm chart,
# refer to dpuservices/storage/examples/nfs-csi/README.md.

# set the NFS CSI chart repository
export NFS_CSI_CHART_REPO="https://example.com/nfs-csi-chart"
# set the NFS CSI chart version
export NFS_CSI_CHART_VERSION="0.0.0"

# SPDK CSI environment variables
# Note: SPDK CSI is not included in the DPF release.
# For instructions on building a DPF-specific SPDK CSI Helm chart and image,
# refer to dpuservices/storage/examples/spdk-csi/README.md.

# set the SPDK CSI chart repository
export SPDK_CSI_CHART_REPO="https://example.com/spdk-csi-chart"
# set the SPDK CSI chart version
export SPDK_CSI_CHART_VERSION="0.0.0"
# set the SPDK CSI image registry address
export SPDK_CSI_IMAGE_REGISTRY="example.com/spdk-csi"
# set the SPDK CSI image tag
export SPDK_CSI_IMAGE_TAG="0.0.0"
# use '[]' to pull images from the registry that doesn't require auth, or '[{"name": "secret-name"}]' to specify the secret name
export SPDK_CSI_IMAGE_PULL_SECRET='[]'

# Workload environment variables

# For zero trust workloads only, set to the name of the node to which to attach the DPU volume
export KUBE_NODE_NAME="<kubernetes-node-name>"
```

Save the above variables to a file, for example `envvars.env`, and source it in your shell:
```shell
source envvars.env
```

## Available Scenarios

### Trusted Kubernetes Cluster Scenarios

#### NVMe Virtual Functions on static Physical Functions

```shell
cat trusted-k8s-cluster/nvme-vf-on-static-pf/*.yaml | envsubst | kubectl apply -f -
```

This will deploy the following objects:

<details markdown="1"><summary>bf-bundle BFB</summary>

[embedmd]:#(trusted-k8s-cluster/nvme-vf-on-static-pf/bf-bundle-bfb.yaml)
```yaml
---
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: BFB
metadata:
  name: bf-bundle
  namespace: dpf-operator-system
spec:
  url: $BLUEFIELD_BITSTREAM
```
</details>

<details markdown="1"><summary>dpf-provisioning-storage DPUFlavor</summary>

[embedmd]:#(trusted-k8s-cluster/nvme-vf-on-static-pf/dpf-provisioning-storage-dpuflavor.yaml)
```yaml
---
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUFlavor
metadata:
  name: dpf-provisioning-storage
  namespace: dpf-operator-system
spec:
  bfcfgParameters:
    - UPDATE_ATF_UEFI=yes
    - UPDATE_DPU_OS=yes
    - WITH_NIC_FW_UPDATE=yes
  configFiles:
    - operation: override
      path: /etc/mellanox/mlnx-bf.conf
      permissions: "0644"
      raw: |
        ALLOW_SHARED_RQ="no"
        IPSEC_FULL_OFFLOAD="no"
        ENABLE_ESWITCH_MULTIPORT="yes"
        RDMA_SET_NETNS_EXCLUSIVE="no"
    - operation: override
      path: /etc/mellanox/mlnx-ovs.conf
      permissions: "0644"
      raw: |
        CREATE_OVS_BRIDGES="no"
        OVS_DOCA="yes"
    - operation: override
      path: /etc/mellanox/mlnx-sf.conf
      permissions: "0644"
      raw: ""
  grub:
    kernelParameters:
      - console=hvc0
      - console=ttyAMA0
      - earlycon=pl011,0x13010000
      - fixrttc
      - net.ifnames=0
      - biosdevname=0
      - iommu.passthrough=1
      - cgroup_no_v1=net_prio,net_cls
      - hugepagesz=2048kB
      - hugepages=5120
  nvconfig:
    - device: "*"
      parameters:
        - PF_BAR2_ENABLE=0
        - PER_PF_NUM_SF=1
        - PF_TOTAL_SF=20
        - PF_SF_BAR_SIZE=10
        - NUM_PF_MSIX_VALID=0
        - PF_NUM_PF_MSIX_VALID=1
        - PF_NUM_PF_MSIX=228
        - INTERNAL_CPU_MODEL=1
        - INTERNAL_CPU_OFFLOAD_ENGINE=0
        - SRIOV_EN=1
        - NUM_OF_VFS=46
        - LAG_RESOURCE_ALLOCATION=1
        - NVME_EMULATION_ENABLE=1
        - NVME_EMULATION_NUM_PF=1
        - NVME_EMULATION_NUM_VF=125
        - NVME_EMULATION_NUM_MSIX=2
  ovs:
    rawConfigScript: |
      _ovs-vsctl() {
        ovs-vsctl --no-wait --timeout 15 "$@"
      }

      _ovs-vsctl set Open_vSwitch . other_config:doca-init=true
      _ovs-vsctl set Open_vSwitch . other_config:dpdk-max-memzones=50000
      _ovs-vsctl set Open_vSwitch . other_config:hw-offload=true
      _ovs-vsctl set Open_vSwitch . other_config:pmd-quiet-idle=true
      _ovs-vsctl set Open_vSwitch . other_config:max-idle=20000
      _ovs-vsctl set Open_vSwitch . other_config:max-revalidator=5000
      _ovs-vsctl --if-exists del-br ovsbr1
      _ovs-vsctl --if-exists del-br ovsbr2
      _ovs-vsctl --may-exist add-br br-sfc
      _ovs-vsctl set bridge br-sfc datapath_type=netdev
      _ovs-vsctl set bridge br-sfc fail_mode=secure
      _ovs-vsctl --may-exist add-port br-sfc p0
      _ovs-vsctl set Interface p0 type=dpdk
      _ovs-vsctl set Port p0 external_ids:dpf-type=physical
```
</details>

<details markdown="1"><summary>storage DPUDeployment</summary>

[embedmd]:#(trusted-k8s-cluster/nvme-vf-on-static-pf/dpudeployment.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUDeployment
metadata:
  name: storage
  namespace: dpf-operator-system
spec:
  dpus:
    bfb: bf-bundle
    flavor: dpf-provisioning-storage
    dpuSets:
      - nameSuffix: "dpuset1"
        nodeSelector:
          matchLabels:
            feature.node.kubernetes.io/dpu-enabled: "true"
  services:
    snap-csi-plugin:
      serviceTemplate: snap-csi-plugin
      serviceConfiguration: snap-csi-plugin
    snap-host-controller:
      serviceTemplate: snap-host-controller
      serviceConfiguration: snap-host-controller
    snap-controller:
      serviceTemplate: snap-controller
      serviceConfiguration: snap-controller
    snap-node-driver:
      serviceTemplate: snap-node-driver
      serviceConfiguration: snap-node-driver
    snap-configuration:
      serviceTemplate: snap-configuration
      serviceConfiguration: snap-configuration
    doca-snap:
      serviceTemplate: doca-snap
      serviceConfiguration: doca-snap
    block-storage-dpu-plugin:
      serviceTemplate: block-storage-dpu-plugin
      serviceConfiguration: block-storage-dpu-plugin
    spdk-csi-controller:
      serviceTemplate: spdk-csi-controller
      serviceConfiguration: spdk-csi-controller
    spdk-csi-controller-dpu:
      serviceTemplate: spdk-csi-controller-dpu
      serviceConfiguration: spdk-csi-controller-dpu
  serviceChains:
    switches:
      - ports:
          - serviceInterface:
              matchLabels:
                uplink: p1
          - serviceInterface:
              matchLabels:
                uplink: pf1hpf
          - service:
              name: doca-snap
              interface: app_sf
              ipam:
                matchLabels:
                  svc.dpu.nvidia.com/pool: storage-pool
```
</details>

<details markdown="1"><summary>doca-snap DPUServiceConfiguration</summary>

[embedmd]:#(trusted-k8s-cluster/nvme-vf-on-static-pf/doca-snap-dpuserviceconfiguration.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: doca-snap
  namespace: dpf-operator-system
spec:
  deploymentServiceName: doca-snap
  serviceConfiguration:
    helmChart:
      values:
        dpu:
          docaSnap:
            enabled: true
            image:
              repository: $DPF_SNAP_IMAGE_REGISTRY/doca_vfs
              tag: $DPF_SNAP_IMAGE_TAG
            imagePullSecrets: $DPF_SNAP_IMAGE_PULL_SECRET
            snapRpcInitConf: |
              nvme_subsystem_create --nqn nqn.2022-10.io.nvda.nvme:0
              nvme_controller_create --nqn nqn.2022-10.io.nvda.nvme:0 --ctrl NVMeCtrl1 --pf_id 0 --admin_only
  interfaces:
    - name: app_sf
      network: mybrsfc
```
</details>

<details markdown="1"><summary>doca-snap DPUServiceTemplate</summary>

[embedmd]:#(trusted-k8s-cluster/nvme-vf-on-static-pf/doca-snap-dpuservicetemplate.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: doca-snap
  namespace: dpf-operator-system
spec:
  deploymentServiceName: doca-snap
  helmChart:
    source:
      repoURL: $DPF_CHART_REPO
      version: $DPF_CHART_VERSION
      chart: dpf-storage
    values:
      serviceDaemonSet:
        resources:
          memory: "2Gi"
          hugepages-2Mi: "4Gi"
          cpu: "8"
          nvidia.com/bf_sf: 1
  resourceRequirements:
    memory: "2Gi"
    hugepages-2Mi: "4Gi"
    cpu: "8"
    nvidia.com/bf_sf: 1
```
</details>

<details markdown="1"><summary>snap-csi-plugin DPUServiceConfiguration</summary>

[embedmd]:#(trusted-k8s-cluster/nvme-vf-on-static-pf/snap-csi-plugin-dpuserviceconfiguration.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: snap-csi-plugin
  namespace: dpf-operator-system
spec:
  deploymentServiceName: snap-csi-plugin
  upgradePolicy:
    applyNodeEffect: false
  serviceConfiguration:
    deployInCluster: true
    helmChart:
      values:
        imagePullSecrets: $DPF_IMAGE_PULL_SECRET
        host:
          snapCsiPlugin:
            enabled: true
            controller:
              plugin:
                image:
                  repository: $DPF_IMAGE_REGISTRY/storage-system
                  tag: $DPF_IMAGE_TAG
              affinity:
                nodeAffinity:
                  requiredDuringSchedulingIgnoredDuringExecution:
                    nodeSelectorTerms:
                      - matchExpressions:
                          - key: "node-role.kubernetes.io/master"
                            operator: Exists
                      - matchExpressions:
                          - key: "node-role.kubernetes.io/control-plane"
                            operator: Exists
            node:
              plugin:
                image:
                  repository: $DPF_IMAGE_REGISTRY/storage-host
                  tag: $DPF_IMAGE_TAG
```
</details>

<details markdown="1"><summary>snap-csi-plugin DPUServiceTemplate</summary>

[embedmd]:#(trusted-k8s-cluster/nvme-vf-on-static-pf/snap-csi-plugin-dpuservicetemplate.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: snap-csi-plugin
  namespace: dpf-operator-system
spec:
  deploymentServiceName: snap-csi-plugin
  helmChart:
    source:
      repoURL: $DPF_CHART_REPO
      version: $DPF_CHART_VERSION
      chart: dpf-storage
```
</details>

<details markdown="1"><summary>block-storage-dpu-plugin DPUServiceConfiguration</summary>

[embedmd]:#(trusted-k8s-cluster/nvme-vf-on-static-pf/block-storage-dpu-plugin-dpuserviceconfiguration.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: block-storage-dpu-plugin
  namespace: dpf-operator-system
spec:
  deploymentServiceName: block-storage-dpu-plugin
  serviceConfiguration:
    helmChart:
      values:
        imagePullSecrets: $DPF_IMAGE_PULL_SECRET
        dpu:
          blockStorageVendorDpuPlugin:
            enabled: true
            image:
              repository: $DPF_IMAGE_REGISTRY/storage-system
              tag: $DPF_IMAGE_TAG
```
</details>

<details markdown="1"><summary>block-storage-dpu-plugin DPUServiceTemplate</summary>

[embedmd]:#(trusted-k8s-cluster/nvme-vf-on-static-pf/block-storage-dpu-plugin-dpuservicetemplate.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: block-storage-dpu-plugin
  namespace: dpf-operator-system
spec:
  deploymentServiceName: block-storage-dpu-plugin
  helmChart:
    source:
      repoURL: $DPF_CHART_REPO
      version: $DPF_CHART_VERSION
      chart: dpf-storage
```
</details>

<details markdown="1"><summary>policy-block DPUStoragePolicy</summary>

[embedmd]:#(trusted-k8s-cluster/nvme-vf-on-static-pf/policy-block-dpustoragepolicy.yaml)
```yaml
---
apiVersion: storage.dpu.nvidia.com/v1alpha1
kind: DPUStoragePolicy
metadata:
  name: policy-block
  namespace: dpf-operator-system
spec:
  dpuStorageVendors:
    - spdk-csi
  selectionAlgorithm: "NumberVolumes"
  parameters: {}
```
</details>

<details markdown="1"><summary>spdk-csi-controller-dpu DPUServiceConfiguration</summary>

[embedmd]:#(trusted-k8s-cluster/nvme-vf-on-static-pf/spdk-csi-controller-dpu-dpuserviceconfiguration.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: spdk-csi-controller-dpu
  namespace: dpf-operator-system
spec:
  deploymentServiceName: spdk-csi-controller-dpu
  serviceConfiguration:
    helmChart:
      values:
        dpu:
          enabled: true
          storageClass:
            # the name of the storage class that will be created for spdk-csi,
            # this StorageClass name should be used in the StorageVendor settings
            name: spdkcsi-sc
            # name of the secret that contains credentials for the remote SPDK target,
            # content of the secret is injected during CreateVolume request
            secretName: spdkcsi-secret
            # namespace of the secret with credentials for the remote SPDK target
            secretNamespace: dpf-operator-system
          rbacRoles:
            spdkCsiController:
              # the name of the service account for spdk-csi-controller
              # this value must be aligned with the value from the DPUServiceCredentialRequest
              serviceAccount: spdk-csi-controller-sa
```
</details>

<details markdown="1"><summary>spdk-csi-controller-dpu DPUServiceTemplate</summary>

[embedmd]:#(trusted-k8s-cluster/nvme-vf-on-static-pf/spdk-csi-controller-dpu-dpuservicetemplate.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: spdk-csi-controller-dpu
  namespace: dpf-operator-system
spec:
  deploymentServiceName: spdk-csi-controller-dpu
  helmChart:
    source:
      repoURL: $SPDK_CSI_CHART_REPO
      version: $SPDK_CSI_CHART_VERSION
      chart: spdk-csi-controller-chart
```
</details>

<details markdown="1"><summary>spdk-csi-controller DPUServiceConfiguration</summary>

[embedmd]:#(trusted-k8s-cluster/nvme-vf-on-static-pf/spdk-csi-controller-dpuserviceconfiguration.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: spdk-csi-controller
  namespace: dpf-operator-system
spec:
  deploymentServiceName: spdk-csi-controller
  upgradePolicy:
    applyNodeEffect: false
  serviceConfiguration:
    deployInCluster: true
    helmChart:
      values:
        host:
          enabled: true
          imagePullSecrets: $SPDK_CSI_IMAGE_PULL_SECRET
          plugin:
            image:
              repository: $SPDK_CSI_IMAGE_REGISTRY/spdk-csi
              tag: $SPDK_CSI_IMAGE_TAG
          config:
            targets:
              nodes:
                # name of the target
                - name: spdk-target
                  # management address
                  rpcURL: http://10.33.33.33:8000
                  # type of the target, e.g. nvme-tcp, nvme-rdma
                  targetType: nvme-tcp
                  # target service IP
                  targetAddr: 10.44.44.100
            # required parameter, name of the secret that contains connection
            # details to access the DPU cluster.
            # this secret should be created by the DPUServiceCredentialRequest API.
            dpuClusterSecret: spdk-csi-controller-dpu-cluster-credentials
```
</details>

<details markdown="1"><summary>spdk-csi-controller-credentials DPUServiceCredentialRequest</summary>

[embedmd]:#(trusted-k8s-cluster/nvme-vf-on-static-pf/spdk-csi-controller-dpuservicecredentialrequest.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceCredentialRequest
metadata:
  name: spdk-csi-controller-credentials
  namespace: dpf-operator-system
spec:
  duration: 10m
  serviceAccount:
    name: spdk-csi-controller-sa
    namespace: dpf-operator-system
  targetCluster:
    name: dpu-cplane-tenant1
    namespace: dpu-cplane-tenant1
  type: tokenFile
  secret:
    name: spdk-csi-controller-dpu-cluster-credentials
    namespace: dpf-operator-system
```
</details>

<details markdown="1"><summary>spdk-csi-controller DPUServiceTemplate</summary>

[embedmd]:#(trusted-k8s-cluster/nvme-vf-on-static-pf/spdk-csi-controller-dpuservicetemplate.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: spdk-csi-controller
  namespace: dpf-operator-system
spec:
  deploymentServiceName: spdk-csi-controller
  helmChart:
    source:
      repoURL: $SPDK_CSI_CHART_REPO
      version: $SPDK_CSI_CHART_VERSION
      chart: spdk-csi-controller-chart
```
</details>

<details markdown="1"><summary>spdk-csi DPUStorageVendor</summary>

[embedmd]:#(trusted-k8s-cluster/nvme-vf-on-static-pf/spdk-csi-dpustoragevendor.yaml)
```yaml
---
apiVersion: storage.dpu.nvidia.com/v1alpha1
kind: DPUStorageVendor
metadata:
  name: spdk-csi
  namespace: dpf-operator-system
spec:
  storageClassName: spdkcsi-sc
  pluginName: nvidia-block
```
</details>

<details markdown="1"><summary>spdkcsi-secret Secret</summary>

[embedmd]:#(trusted-k8s-cluster/nvme-vf-on-static-pf/spdk-csi-secret.yaml)
```yaml
---
apiVersion: v1
kind: Secret
metadata:
  name: spdkcsi-secret
  namespace: dpf-operator-system
  labels:
    # this label enables replication of the secret from the host to the dpu cluster
    dpu.nvidia.com/image-pull-secret: ""
stringData:
  # name field in the "rpcTokens" list should match name of the
  # spdk target from DPUService.helmChart.values.host.config.targets.nodes
  secret.json: |-
    {
      "rpcTokens": [
        {
          "name": "spdk-target",
          "username": "exampleuser",
          "password": "examplepassword"
        }
      ]
    }
```
</details>

<details markdown="1"><summary>snap-node-driver DPUServiceConfiguration</summary>

[embedmd]:#(trusted-k8s-cluster/nvme-vf-on-static-pf/snap-node-driver-dpuserviceconfiguration.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: snap-node-driver
  namespace: dpf-operator-system
spec:
  deploymentServiceName: snap-node-driver
  serviceConfiguration:
    helmChart:
      values:
        imagePullSecrets: $DPF_IMAGE_PULL_SECRET
        dpu:
          snapNodeDriver:
            enabled: true
            image:
              repository: $DPF_IMAGE_REGISTRY/storage-system
              tag: $DPF_IMAGE_TAG
```
</details>

<details markdown="1"><summary>snap-node-driver DPUServiceTemplate</summary>

[embedmd]:#(trusted-k8s-cluster/nvme-vf-on-static-pf/snap-node-driver-dpuservicetemplate.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: snap-node-driver
  namespace: dpf-operator-system
spec:
  deploymentServiceName: snap-node-driver
  helmChart:
    source:
      repoURL: $DPF_CHART_REPO
      version: $DPF_CHART_VERSION
      chart: dpf-storage
```
</details>

<details markdown="1"><summary>snap-controller DPUServiceConfiguration</summary>

[embedmd]:#(trusted-k8s-cluster/nvme-vf-on-static-pf/snap-controller-dpuserviceconfiguration.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: snap-controller
  namespace: dpf-operator-system
spec:
  deploymentServiceName: snap-controller
  upgradePolicy:
    applyNodeEffect: false
  serviceConfiguration:
    deployInCluster: true
    helmChart:
      values:
        imagePullSecrets: $DPF_IMAGE_PULL_SECRET
        host:
          snapController:
            enabled: true
            image:
              repository: $DPF_IMAGE_REGISTRY/storage-system
              tag: $DPF_IMAGE_TAG
            config:
              # required parameter, name of the secret that contains connection
              # details to access the DPU cluster.
              # this secret should be created by the DPUServiceCredentialRequest API.
              dpuClusterSecret: snap-controller-dpu-cluster-credentials
              # controls where to search for StorageVendor and StoragePolicy CRs
              configNamespace: dpf-operator-system
            affinity:
              nodeAffinity:
                requiredDuringSchedulingIgnoredDuringExecution:
                  nodeSelectorTerms:
                    - matchExpressions:
                        - key: "node-role.kubernetes.io/master"
                          operator: Exists
                    - matchExpressions:
                        - key: "node-role.kubernetes.io/control-plane"
                          operator: Exists
```
</details>

<details markdown="1"><summary>snap-controller-credentials DPUServiceCredentialRequest</summary>

[embedmd]:#(trusted-k8s-cluster/nvme-vf-on-static-pf/snap-controller-dpuservicecredentialrequest.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceCredentialRequest
metadata:
  name: snap-controller-credentials
  namespace: dpf-operator-system
spec:
  duration: 10m
  serviceAccount:
    name: snap-controller-sa
    namespace: dpf-operator-system
  targetCluster:
    name: dpu-cplane-tenant1
    namespace: dpu-cplane-tenant1
  type: tokenFile
  secret:
    name: snap-controller-dpu-cluster-credentials
    namespace: dpf-operator-system
```
</details>

<details markdown="1"><summary>snap-controller DPUServiceTemplate</summary>

[embedmd]:#(trusted-k8s-cluster/nvme-vf-on-static-pf/snap-controller-dpuservicetemplate.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: snap-controller
  namespace: dpf-operator-system
spec:
  deploymentServiceName: snap-controller
  helmChart:
    source:
      repoURL: $DPF_CHART_REPO
      version: $DPF_CHART_VERSION
      chart: dpf-storage
```
</details>

<details markdown="1"><summary>snap-host-controller DPUServiceConfiguration</summary>

[embedmd]:#(trusted-k8s-cluster/nvme-vf-on-static-pf/snap-host-controller-dpuserviceconfiguration.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: snap-host-controller
  namespace: dpf-operator-system
spec:
  deploymentServiceName: snap-host-controller
  upgradePolicy:
    applyNodeEffect: false
  serviceConfiguration:
    deployInCluster: true
    helmChart:
      values:
        imagePullSecrets: $DPF_IMAGE_PULL_SECRET
        host:
          snapHostController:
            image:
              repository: $DPF_IMAGE_REGISTRY/storage-system
              tag: $DPF_IMAGE_TAG
            enabled: true
            config:
              dpuClusterName: dpu-cplane-tenant1
              dpuClusterNamespace: dpu-cplane-tenant1
              targetNamespace: dpf-operator-system
            affinity:
              nodeAffinity:
                requiredDuringSchedulingIgnoredDuringExecution:
                  nodeSelectorTerms:
                  - matchExpressions:
                      - key: "node-role.kubernetes.io/master"
                        operator: Exists
                  - matchExpressions:
                      - key: "node-role.kubernetes.io/control-plane"
                        operator: Exists
```
</details>

<details markdown="1"><summary>snap-host-controller DPUServiceTemplate</summary>

[embedmd]:#(trusted-k8s-cluster/nvme-vf-on-static-pf/snap-host-controller-dpuservicetemplate.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: snap-host-controller
  namespace: dpf-operator-system
spec:
  deploymentServiceName: snap-host-controller
  helmChart:
    source:
      repoURL: $DPF_CHART_REPO
      version: $DPF_CHART_VERSION
      chart: dpf-storage
```
</details>

<details markdown="1"><summary>p1 DPUServiceInterface</summary>

[embedmd]:#(trusted-k8s-cluster/nvme-vf-on-static-pf/p1-dpuserviceinterface.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceInterface
metadata:
  name: p1
  namespace: dpf-operator-system
spec:
  template:
    spec:
      nodeSelector:
        matchExpressions:
          - key: kubernetes.io/os
            operator: In
            values:
              - "linux"
      template:
        metadata:
          labels:
            uplink: "p1"
        spec:
          interfaceType: physical
          physical:
            interfaceName: p1
```
</details>

<details markdown="1"><summary>pf1hpf DPUServiceInterface</summary>

[embedmd]:#(trusted-k8s-cluster/nvme-vf-on-static-pf/pf1hpf-dpuserviceinterface.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceInterface
metadata:
  name: pf1hpf
  namespace: dpf-operator-system
spec:
  template:
    spec:
      nodeSelector:
        matchExpressions:
          - key: kubernetes.io/os
            operator: In
            values:
              - "linux"
      template:
        metadata:
          labels:
            uplink: "pf1hpf"
        spec:
          interfaceType: pf
          pf:
            pfID: 1
```
</details>

<details markdown="1"><summary>snap-configuration DPUServiceConfiguration</summary>

[embedmd]:#(trusted-k8s-cluster/nvme-vf-on-static-pf/snap-configuration-dpuserviceconfiguration.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: snap-configuration
  namespace: dpf-operator-system
spec:
  deploymentServiceName: snap-configuration
  serviceConfiguration:
    helmChart:
      values:
        dpu:
          deployCrds: true
          configuration:
            enabled: true
            # RBAC roles for clients from host cluster
            # serviceAccount names must be aligned with the serviceAccount names from DPUServiceCredentialRequest
            rbacRoles:
              snapController:
                serviceAccount: snap-controller-sa
```
</details>

<details markdown="1"><summary>snap-configuration DPUServiceTemplate</summary>

[embedmd]:#(trusted-k8s-cluster/nvme-vf-on-static-pf/snap-configuration-dpuservicetemplate.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: snap-configuration
  namespace: dpf-operator-system
spec:
  deploymentServiceName: snap-configuration
  helmChart:
    source:
      repoURL: $DPF_CHART_REPO
      version: $DPF_CHART_VERSION
      chart: dpf-storage
```
</details>

<details markdown="1"><summary>storage-pool DPUServiceIPAM</summary>

[embedmd]:#(trusted-k8s-cluster/nvme-vf-on-static-pf/storage-pool-dpuserviceipam.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceIPAM
metadata:
  name: storage-pool
  namespace: dpf-operator-system
spec:
  metadata:
    labels:
      svc.dpu.nvidia.com/pool: storage-pool
  ipv4Subnet:
    subnet: "10.44.44.0/24"
    gateway: "10.44.44.1"
    perNodeIPCount: 20
```
</details>

```shell
cat trusted-k8s-cluster/nvme-vf-on-static-pf/workload/*.yaml | envsubst | kubectl apply -f -
```

This will deploy the following objects:

<details markdown="1"><summary>snap-nvme-vf StorageClass</summary>

[embedmd]:#(trusted-k8s-cluster/nvme-vf-on-static-pf/workload/storageclass.yaml)
```yaml
---
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: snap-nvme-vf
provisioner: csi.snap.nvidia.com
parameters:
  policy: "policy-block"
  functionType: "vf"
  hotplugFunction: "false"
```
</details>

<details markdown="1"><summary>storage-test-pod-nvme-vf StatefulSet</summary>

[embedmd]:#(trusted-k8s-cluster/nvme-vf-on-static-pf/workload/sts-block.yaml)
```yaml
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: storage-test-pod-nvme-vf
spec:
  serviceName: "storage-test-pod-nvme-vf"
  podManagementPolicy: "Parallel"
  replicas: 1
  selector:
    matchLabels:
      app: storage-test-pod-nvme-vf
  template:
    metadata:
      labels:
        app: storage-test-pod-nvme-vf
    spec:
      containers:
      - name: test
        image: registry.k8s.io/nginx-slim:0.21
        volumeDevices:
          - name: vol1
            devicePath: /dev/xvda
  volumeClaimTemplates:
  - metadata:
      name: vol1
    spec:
      accessModes: [ "ReadWriteOnce" ]
      volumeMode: Block
      storageClassName: snap-nvme-vf
      resources:
        requests:
          storage: 1Gi
```
</details>

### Non-Trusted Host Scenarios

#### NVMe Virtual Functions on static Physical Functions

```shell
cat non-trusted-host/nvme-vf-on-static-pf/*.yaml | envsubst | kubectl apply -f -
```

This will deploy the following objects:

<details markdown="1"><summary>bf-bundle BFB</summary>

[embedmd]:#(non-trusted-host/nvme-vf-on-static-pf/bf-bundle-bfb.yaml)
```yaml
---
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: BFB
metadata:
  name: bf-bundle
  namespace: dpf-operator-system
spec:
  url: $BLUEFIELD_BITSTREAM
```
</details>

<details markdown="1"><summary>block-storage-dpu-plugin DPUServiceConfiguration</summary>

[embedmd]:#(non-trusted-host/nvme-vf-on-static-pf/block-storage-dpu-plugin-dpuserviceconfiguration.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: block-storage-dpu-plugin
  namespace: dpf-operator-system
spec:
  deploymentServiceName: block-storage-dpu-plugin
  serviceConfiguration:
    helmChart:
      values:
        imagePullSecrets: $DPF_IMAGE_PULL_SECRET
        dpu:
          blockStorageVendorDpuPlugin:
            enabled: true
            image:
              repository: $DPF_IMAGE_REGISTRY/storage-system
              tag: $DPF_IMAGE_TAG
```
</details>

<details markdown="1"><summary>block-storage-dpu-plugin DPUServiceTemplate</summary>

[embedmd]:#(non-trusted-host/nvme-vf-on-static-pf/block-storage-dpu-plugin-dpuservicetemplate.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: block-storage-dpu-plugin
  namespace: dpf-operator-system
spec:
  deploymentServiceName: block-storage-dpu-plugin
  helmChart:
    source:
      repoURL: $DPF_CHART_REPO
      version: $DPF_CHART_VERSION
      chart: dpf-storage
```
</details>

<details markdown="1"><summary>doca-snap DPUServiceConfiguration</summary>

[embedmd]:#(non-trusted-host/nvme-vf-on-static-pf/doca-snap-dpuserviceconfiguration.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: doca-snap
  namespace: dpf-operator-system
spec:
  deploymentServiceName: doca-snap
  serviceConfiguration:
    helmChart:
      values:
        dpu:
          docaSnap:
            enabled: true
            image:
              repository: $DPF_SNAP_IMAGE_REGISTRY/doca_vfs
              tag: $DPF_SNAP_IMAGE_TAG
            imagePullSecrets: $DPF_SNAP_IMAGE_PULL_SECRET
            snapRpcInitConf: |
              nvme_subsystem_create --nqn nqn.2022-10.io.nvda.nvme:0
              nvme_controller_create --nqn nqn.2022-10.io.nvda.nvme:0 --ctrl NVMeCtrl1 --pf_id 0 --admin_only
  interfaces:
    - name: app_sf
      network: mybrsfc
```
</details>

<details markdown="1"><summary>doca-snap DPUServiceTemplate</summary>

[embedmd]:#(non-trusted-host/nvme-vf-on-static-pf/doca-snap-dpuservicetemplate.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: doca-snap
  namespace: dpf-operator-system
spec:
  deploymentServiceName: doca-snap
  helmChart:
    source:
      repoURL: $DPF_CHART_REPO
      version: $DPF_CHART_VERSION
      chart: dpf-storage
    values:
      serviceDaemonSet:
        resources:
          memory: "2Gi"
          hugepages-2Mi: "4Gi"
          cpu: "8"
          nvidia.com/bf_sf: 1
  resourceRequirements:
    memory: "2Gi"
    hugepages-2Mi: "4Gi"
    cpu: "8"
    nvidia.com/bf_sf: 1
```
</details>

<details markdown="1"><summary>dpf-provisioning-storage DPUFlavor</summary>

[embedmd]:#(non-trusted-host/nvme-vf-on-static-pf/dpf-provisioning-storage-dpuflavor.yaml)
```yaml
---
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUFlavor
metadata:
  name: dpf-provisioning-storage
  namespace: dpf-operator-system
spec:
  bfcfgParameters:
    - UPDATE_ATF_UEFI=yes
    - UPDATE_DPU_OS=yes
    - WITH_NIC_FW_UPDATE=yes
  configFiles:
    - operation: override
      path: /etc/mellanox/mlnx-bf.conf
      permissions: "0644"
      raw: |
        ALLOW_SHARED_RQ="no"
        IPSEC_FULL_OFFLOAD="no"
        ENABLE_ESWITCH_MULTIPORT="yes"
        RDMA_SET_NETNS_EXCLUSIVE="no"
    - operation: override
      path: /etc/mellanox/mlnx-ovs.conf
      permissions: "0644"
      raw: |
        CREATE_OVS_BRIDGES="no"
        OVS_DOCA="yes"
    - operation: override
      path: /etc/mellanox/mlnx-sf.conf
      permissions: "0644"
      raw: ""
  grub:
    kernelParameters:
      - console=hvc0
      - console=ttyAMA0
      - earlycon=pl011,0x13010000
      - fixrttc
      - net.ifnames=0
      - biosdevname=0
      - iommu.passthrough=1
      - cgroup_no_v1=net_prio,net_cls
      - hugepagesz=2048kB
      - hugepages=5120
  nvconfig:
    - device: "*"
      parameters:
        - PF_BAR2_ENABLE=0
        - PER_PF_NUM_SF=1
        - PF_TOTAL_SF=20
        - PF_SF_BAR_SIZE=10
        - NUM_PF_MSIX_VALID=0
        - PF_NUM_PF_MSIX_VALID=1
        - PF_NUM_PF_MSIX=228
        - INTERNAL_CPU_MODEL=1
        - INTERNAL_CPU_OFFLOAD_ENGINE=0
        - SRIOV_EN=1
        - NUM_OF_VFS=46
        - LAG_RESOURCE_ALLOCATION=1
        - NVME_EMULATION_ENABLE=1
        - NVME_EMULATION_NUM_PF=1
        - NVME_EMULATION_NUM_VF=125
        - NVME_EMULATION_NUM_MSIX=2
  ovs:
    rawConfigScript: |
      _ovs-vsctl() {
        ovs-vsctl --no-wait --timeout 15 "$@"
      }

      _ovs-vsctl set Open_vSwitch . other_config:doca-init=true
      _ovs-vsctl set Open_vSwitch . other_config:dpdk-max-memzones=50000
      _ovs-vsctl set Open_vSwitch . other_config:hw-offload=true
      _ovs-vsctl set Open_vSwitch . other_config:pmd-quiet-idle=true
      _ovs-vsctl set Open_vSwitch . other_config:max-idle=20000
      _ovs-vsctl set Open_vSwitch . other_config:max-revalidator=5000
      _ovs-vsctl --if-exists del-br ovsbr1
      _ovs-vsctl --if-exists del-br ovsbr2
      _ovs-vsctl --may-exist add-br br-sfc
      _ovs-vsctl set bridge br-sfc datapath_type=netdev
      _ovs-vsctl set bridge br-sfc fail_mode=secure
      _ovs-vsctl --may-exist add-port br-sfc p0
      _ovs-vsctl set Interface p0 type=dpdk
      _ovs-vsctl set Port p0 external_ids:dpf-type=physical
```
</details>

<details markdown="1"><summary>storage DPUDeployment</summary>

[embedmd]:#(non-trusted-host/nvme-vf-on-static-pf/dpudeployment.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUDeployment
metadata:
  name: storage
  namespace: dpf-operator-system
spec:
  dpus:
    bfb: bf-bundle
    flavor: dpf-provisioning-storage
    dpuSets:
      - nameSuffix: "dpuset1"
        nodeSelector:
          matchLabels:
            feature.node.kubernetes.io/dpu-enabled: "true"
  services:
    snap-host-controller:
      serviceTemplate: snap-host-controller
      serviceConfiguration: snap-host-controller
    snap-controller:
      serviceTemplate: snap-controller
      serviceConfiguration: snap-controller
    snap-node-driver:
      serviceTemplate: snap-node-driver
      serviceConfiguration: snap-node-driver
    snap-configuration:
      serviceTemplate: snap-configuration
      serviceConfiguration: snap-configuration
    doca-snap:
      serviceTemplate: doca-snap
      serviceConfiguration: doca-snap
    block-storage-dpu-plugin:
      serviceTemplate: block-storage-dpu-plugin
      serviceConfiguration: block-storage-dpu-plugin
    spdk-csi-controller:
      serviceTemplate: spdk-csi-controller
      serviceConfiguration: spdk-csi-controller
    spdk-csi-controller-dpu:
      serviceTemplate: spdk-csi-controller-dpu
      serviceConfiguration: spdk-csi-controller-dpu
  serviceChains:
    switches:
      - ports:
          - serviceInterface:
              matchLabels:
                uplink: p1
          - serviceInterface:
              matchLabels:
                uplink: pf1hpf
          - service:
              name: doca-snap
              interface: app_sf
              ipam:
                matchLabels:
                  svc.dpu.nvidia.com/pool: storage-pool
```
</details>

<details markdown="1"><summary>p1 DPUServiceInterface</summary>

[embedmd]:#(non-trusted-host/nvme-vf-on-static-pf/p1-dpuserviceinterface.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceInterface
metadata:
  name: p1
  namespace: dpf-operator-system
spec:
  template:
    spec:
      nodeSelector:
        matchExpressions:
          - key: kubernetes.io/os
            operator: In
            values:
              - "linux"
      template:
        metadata:
          labels:
            uplink: "p1"
        spec:
          interfaceType: physical
          physical:
            interfaceName: p1
```
</details>

<details markdown="1"><summary>pf1hpf DPUServiceInterface</summary>

[embedmd]:#(non-trusted-host/nvme-vf-on-static-pf/pf1hpf-dpuserviceinterface.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceInterface
metadata:
  name: pf1hpf
  namespace: dpf-operator-system
spec:
  template:
    spec:
      nodeSelector:
        matchExpressions:
          - key: kubernetes.io/os
            operator: In
            values:
              - "linux"
      template:
        metadata:
          labels:
            uplink: "pf1hpf"
        spec:
          interfaceType: pf
          pf:
            pfID: 1
```
</details>

<details markdown="1"><summary>policy-block DPUStoragePolicy</summary>

[embedmd]:#(non-trusted-host/nvme-vf-on-static-pf/policy-block-dpustoragepolicy.yaml)
```yaml
---
apiVersion: storage.dpu.nvidia.com/v1alpha1
kind: DPUStoragePolicy
metadata:
  name: policy-block
  namespace: dpf-operator-system
spec:
  dpuStorageVendors:
    - spdk-csi
  selectionAlgorithm: "NumberVolumes"
  parameters: {}
```
</details>

<details markdown="1"><summary>snap-configuration DPUServiceConfiguration</summary>

[embedmd]:#(non-trusted-host/nvme-vf-on-static-pf/snap-configuration-dpuserviceconfiguration.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: snap-configuration
  namespace: dpf-operator-system
spec:
  deploymentServiceName: snap-configuration
  serviceConfiguration:
    helmChart:
      values:
        dpu:
          deployCrds: true
          configuration:
            enabled: true
            # RBAC roles for clients from host cluster
            # serviceAccount names must be aligned with the serviceAccount names from DPUServiceCredentialRequest
            rbacRoles:
              snapController:
                serviceAccount: snap-controller-sa
```
</details>

<details markdown="1"><summary>snap-configuration DPUServiceTemplate</summary>

[embedmd]:#(non-trusted-host/nvme-vf-on-static-pf/snap-configuration-dpuservicetemplate.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: snap-configuration
  namespace: dpf-operator-system
spec:
  deploymentServiceName: snap-configuration
  helmChart:
    source:
      repoURL: $DPF_CHART_REPO
      version: $DPF_CHART_VERSION
      chart: dpf-storage
```
</details>

<details markdown="1"><summary>snap-controller DPUServiceConfiguration</summary>

[embedmd]:#(non-trusted-host/nvme-vf-on-static-pf/snap-controller-dpuserviceconfiguration.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: snap-controller
  namespace: dpf-operator-system
spec:
  deploymentServiceName: snap-controller
  upgradePolicy:
    applyNodeEffect: false
  serviceConfiguration:
    deployInCluster: true
    helmChart:
      values:
        imagePullSecrets: $DPF_IMAGE_PULL_SECRET
        host:
          snapController:
            enabled: true
            image:
              repository: $DPF_IMAGE_REGISTRY/storage-system
              tag: $DPF_IMAGE_TAG
            config:
              # required parameter, name of the secret that contains connection
              # details to access the DPU cluster.
              # this secret should be created by the DPUServiceCredentialRequest API.
              dpuClusterSecret: snap-controller-dpu-cluster-credentials
              # controls where to search for StorageVendor and StoragePolicy CRs
              configNamespace: dpf-operator-system
            affinity:
              nodeAffinity:
                requiredDuringSchedulingIgnoredDuringExecution:
                  nodeSelectorTerms:
                    - matchExpressions:
                        - key: "node-role.kubernetes.io/master"
                          operator: Exists
                    - matchExpressions:
                        - key: "node-role.kubernetes.io/control-plane"
                          operator: Exists
```
</details>

<details markdown="1"><summary>snap-controller DPUServiceCredentialRequest</summary>

[embedmd]:#(non-trusted-host/nvme-vf-on-static-pf/snap-controller-dpuservicecredentialrequest.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceCredentialRequest
metadata:
  name: snap-controller-credentials
  namespace: dpf-operator-system
spec:
  duration: 10m
  serviceAccount:
    name: snap-controller-sa
    namespace: dpf-operator-system
  targetCluster:
    name: dpu-cplane-tenant1
    namespace: dpu-cplane-tenant1
  type: tokenFile
  secret:
    name: snap-controller-dpu-cluster-credentials
    namespace: dpf-operator-system
```
</details>

<details markdown="1"><summary>snap-controller DPUServiceTemplate</summary>

[embedmd]:#(non-trusted-host/nvme-vf-on-static-pf/snap-controller-dpuservicetemplate.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: snap-controller
  namespace: dpf-operator-system
spec:
  deploymentServiceName: snap-controller
  helmChart:
    source:
      repoURL: $DPF_CHART_REPO
      version: $DPF_CHART_VERSION
      chart: dpf-storage
```
</details>

<details markdown="1"><summary>snap-host-controller DPUServiceConfiguration</summary>

[embedmd]:#(non-trusted-host/nvme-vf-on-static-pf/snap-host-controller-dpuserviceconfiguration.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: snap-host-controller
  namespace: dpf-operator-system
spec:
  deploymentServiceName: snap-host-controller
  upgradePolicy:
    applyNodeEffect: false
  serviceConfiguration:
    deployInCluster: true
    helmChart:
      values:
        imagePullSecrets: $DPF_IMAGE_PULL_SECRET
        host:
          snapHostController:
            image:
              repository: $DPF_IMAGE_REGISTRY/storage-system
              tag: $DPF_IMAGE_TAG
            enabled: true
            config:
              dpuClusterName: dpu-cplane-tenant1
              dpuClusterNamespace: dpu-cplane-tenant1
              targetNamespace: dpf-operator-system
            affinity:
              nodeAffinity:
                requiredDuringSchedulingIgnoredDuringExecution:
                  nodeSelectorTerms:
                  - matchExpressions:
                      - key: "node-role.kubernetes.io/master"
                        operator: Exists
                  - matchExpressions:
                      - key: "node-role.kubernetes.io/control-plane"
                        operator: Exists
```
</details>

<details markdown="1"><summary>snap-host-controller DPUServiceTemplate</summary>

[embedmd]:#(non-trusted-host/nvme-vf-on-static-pf/snap-host-controller-dpuservicetemplate.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: snap-host-controller
  namespace: dpf-operator-system
spec:
  deploymentServiceName: snap-host-controller
  helmChart:
    source:
      repoURL: $DPF_CHART_REPO
      version: $DPF_CHART_VERSION
      chart: dpf-storage
```
</details>

<details markdown="1"><summary>snap-node-driver DPUServiceConfiguration</summary>

[embedmd]:#(non-trusted-host/nvme-vf-on-static-pf/snap-node-driver-dpuserviceconfiguration.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: snap-node-driver
  namespace: dpf-operator-system
spec:
  deploymentServiceName: snap-node-driver
  serviceConfiguration:
    helmChart:
      values:
        imagePullSecrets: $DPF_IMAGE_PULL_SECRET
        dpu:
          snapNodeDriver:
            enabled: true
            image:
              repository: $DPF_IMAGE_REGISTRY/storage-system
              tag: $DPF_IMAGE_TAG
```
</details>

<details markdown="1"><summary>snap-node-driver DPUServiceTemplate</summary>

[embedmd]:#(non-trusted-host/nvme-vf-on-static-pf/snap-node-driver-dpuservicetemplate.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: snap-node-driver
  namespace: dpf-operator-system
spec:
  deploymentServiceName: snap-node-driver
  helmChart:
    source:
      repoURL: $DPF_CHART_REPO
      version: $DPF_CHART_VERSION
      chart: dpf-storage
```
</details>

<details markdown="1"><summary>spdk-csi-controller DPUServiceConfiguration</summary>

[embedmd]:#(non-trusted-host/nvme-vf-on-static-pf/spdk-csi-controller-dpuserviceconfiguration.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: spdk-csi-controller
  namespace: dpf-operator-system
spec:
  deploymentServiceName: spdk-csi-controller
  upgradePolicy:
    applyNodeEffect: false
  serviceConfiguration:
    deployInCluster: true
    helmChart:
      values:
        host:
          enabled: true
          imagePullSecrets: $SPDK_CSI_IMAGE_PULL_SECRET
          plugin:
            image:
              repository: $SPDK_CSI_IMAGE_REGISTRY/spdk-csi
              tag: $SPDK_CSI_IMAGE_TAG
          config:
            targets:
              nodes:
                # name of the target
                - name: spdk-target
                  # management address
                  rpcURL: http://10.33.33.33:8000
                  # type of the target, e.g. nvme-tcp, nvme-rdma
                  targetType: nvme-tcp
                  # target service IP
                  targetAddr: 10.44.44.100
            # required parameter, name of the secret that contains connection
            # details to access the DPU cluster.
            # this secret should be created by the DPUServiceCredentialRequest API.
            dpuClusterSecret: spdk-csi-controller-dpu-cluster-credentials
```
</details>

<details markdown="1"><summary>spdk-csi-controller DPUServiceCredentialRequest</summary>

[embedmd]:#(non-trusted-host/nvme-vf-on-static-pf/spdk-csi-controller-dpuservicecredentialrequest.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceCredentialRequest
metadata:
  name: spdk-csi-controller-credentials
  namespace: dpf-operator-system
spec:
  duration: 10m
  serviceAccount:
    name: spdk-csi-controller-sa
    namespace: dpf-operator-system
  targetCluster:
    name: dpu-cplane-tenant1
    namespace: dpu-cplane-tenant1
  type: tokenFile
  secret:
    name: spdk-csi-controller-dpu-cluster-credentials
    namespace: dpf-operator-system
```
</details>

<details markdown="1"><summary>spdk-csi-controller DPUServiceTemplate</summary>

[embedmd]:#(non-trusted-host/nvme-vf-on-static-pf/spdk-csi-controller-dpuservicetemplate.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: spdk-csi-controller
  namespace: dpf-operator-system
spec:
  deploymentServiceName: spdk-csi-controller
  helmChart:
    source:
      repoURL: $SPDK_CSI_CHART_REPO
      version: $SPDK_CSI_CHART_VERSION
      chart: spdk-csi-controller-chart
```
</details>

<details markdown="1"><summary>spdk-csi-controller-dpu DPUServiceConfiguration</summary>

[embedmd]:#(non-trusted-host/nvme-vf-on-static-pf/spdk-csi-controller-dpu-dpuserviceconfiguration.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: spdk-csi-controller-dpu
  namespace: dpf-operator-system
spec:
  deploymentServiceName: spdk-csi-controller-dpu
  serviceConfiguration:
    helmChart:
      values:
        dpu:
          enabled: true
          storageClass:
            # the name of the storage class that will be created for spdk-csi,
            # this StorageClass name should be used in the StorageVendor settings
            name: spdkcsi-sc
            # name of the secret that contains credentials for the remote SPDK target,
            # content of the secret is injected during CreateVolume request
            secretName: spdkcsi-secret
            # namespace of the secret with credentials for the remote SPDK target
            secretNamespace: dpf-operator-system
          rbacRoles:
            spdkCsiController:
              # the name of the service account for spdk-csi-controller
              # this value must be aligned with the value from the DPUServiceCredentialRequest
              serviceAccount: spdk-csi-controller-sa
```
</details>

<details markdown="1"><summary>spdk-csi-controller-dpu DPUServiceTemplate</summary>

[embedmd]:#(non-trusted-host/nvme-vf-on-static-pf/spdk-csi-controller-dpu-dpuservicetemplate.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: spdk-csi-controller-dpu
  namespace: dpf-operator-system
spec:
  deploymentServiceName: spdk-csi-controller-dpu
  helmChart:
    source:
      repoURL: $SPDK_CSI_CHART_REPO
      version: $SPDK_CSI_CHART_VERSION
      chart: spdk-csi-controller-chart
```
</details>

<details markdown="1"><summary>spdk-csi DPUStorageVendor</summary>

[embedmd]:#(non-trusted-host/nvme-vf-on-static-pf/spdk-csi-dpustoragevendor.yaml)
```yaml
---
apiVersion: storage.dpu.nvidia.com/v1alpha1
kind: DPUStorageVendor
metadata:
  name: spdk-csi
  namespace: dpf-operator-system
spec:
  storageClassName: spdkcsi-sc
  pluginName: nvidia-block
```
</details>

<details markdown="1"><summary>spdk-csi Secret</summary>

[embedmd]:#(non-trusted-host/nvme-vf-on-static-pf/spdk-csi-secret.yaml)
```yaml
---
apiVersion: v1
kind: Secret
metadata:
  name: spdkcsi-secret
  namespace: dpf-operator-system
  labels:
    # this label enables replication of the secret from the host to the dpu cluster
    dpu.nvidia.com/image-pull-secret: ""
stringData:
  # name field in the "rpcTokens" list should match name of the
  # spdk target from DPUService.helmChart.values.host.config.targets.nodes
  secret.json: |-
    {
      "rpcTokens": [
        {
          "name": "spdk-target",
          "username": "exampleuser",
          "password": "examplepassword"
        }
      ]
    }
```
</details>

<details markdown="1"><summary>storage-pool DPUServiceIPAM</summary>

[embedmd]:#(non-trusted-host/nvme-vf-on-static-pf/storage-pool-dpuserviceipam.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceIPAM
metadata:
  name: storage-pool
  namespace: dpf-operator-system
spec:
  metadata:
    labels:
      svc.dpu.nvidia.com/pool: storage-pool
  ipv4Subnet:
    subnet: "10.44.44.0/24"
    gateway: "10.44.44.1"
    perNodeIPCount: 20
```
</details>

```shell
cat non-trusted-host/nvme-vf-on-static-pf/workload/*.yaml | envsubst | kubectl apply -f -
```

This will deploy the following objects:

<details markdown="1"><summary>test-volume-nvme-vf DPUVolume</summary>

[embedmd]:#(non-trusted-host/nvme-vf-on-static-pf/workload/dpuvolume.yaml)
```yaml
---
apiVersion: storage.dpu.nvidia.com/v1alpha1
kind: DPUVolume
metadata:
  name: test-volume-nvme-vf
  namespace: dpf-operator-system
spec:
  dpuStoragePolicyName: policy-block
  resources:
    requests:
      storage: 1Gi
  accessModes:
  - ReadWriteOnce
  volumeMode: Block
```
</details>

<details markdown="1"><summary>test-volume-attachment-nvme-vf DPUVolumeAttachment</summary>

[embedmd]:#(non-trusted-host/nvme-vf-on-static-pf/workload/dpuvolumeattachment.yaml)
```yaml
---
apiVersion: storage.dpu.nvidia.com/v1alpha1
kind: DPUVolumeAttachment
metadata:
  name: test-volume-attachment-nvme-vf
  namespace: dpf-operator-system
spec:
  dpuNodeName: $KUBE_NODE_NAME
  dpuVolumeName: test-volume-nvme-vf
  functionType: vf
  hotplugFunction: false
```
</details>

#### NVMe with hot-plug Physical Functions

```shell
cat non-trusted-host/nvme-hotplug-pf/*.yaml | envsubst | kubectl apply -f -
```

This will deploy the following objects:

<details markdown="1"><summary>bf-bundle BFB</summary>

[embedmd]:#(non-trusted-host/nvme-hotplug-pf/bf-bundle-bfb.yaml)
```yaml
---
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: BFB
metadata:
  name: bf-bundle
  namespace: dpf-operator-system
spec:
  url: $BLUEFIELD_BITSTREAM
```
</details>

<details markdown="1"><summary>block-storage-dpu-plugin DPUServiceConfiguration</summary>

[embedmd]:#(non-trusted-host/nvme-hotplug-pf/block-storage-dpu-plugin-dpuserviceconfiguration.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: block-storage-dpu-plugin
  namespace: dpf-operator-system
spec:
  deploymentServiceName: block-storage-dpu-plugin
  serviceConfiguration:
    helmChart:
      values:
        imagePullSecrets: $DPF_IMAGE_PULL_SECRET
        dpu:
          blockStorageVendorDpuPlugin:
            enabled: true
            image:
              repository: $DPF_IMAGE_REGISTRY/storage-system
              tag: $DPF_IMAGE_TAG
```
</details>

<details markdown="1"><summary>block-storage-dpu-plugin DPUServiceTemplate</summary>

[embedmd]:#(non-trusted-host/nvme-hotplug-pf/block-storage-dpu-plugin-dpuservicetemplate.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: block-storage-dpu-plugin
  namespace: dpf-operator-system
spec:
  deploymentServiceName: block-storage-dpu-plugin
  helmChart:
    source:
      repoURL: $DPF_CHART_REPO
      version: $DPF_CHART_VERSION
      chart: dpf-storage
```
</details>

<details markdown="1"><summary>doca-snap DPUServiceConfiguration</summary>

[embedmd]:#(non-trusted-host/nvme-hotplug-pf/doca-snap-dpuserviceconfiguration.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: doca-snap
  namespace: dpf-operator-system
spec:
  deploymentServiceName: doca-snap
  serviceConfiguration:
    helmChart:
      values:
        dpu:
          docaSnap:
            enabled: true
            image:
              repository: $DPF_SNAP_IMAGE_REGISTRY/doca_vfs
              tag: $DPF_SNAP_IMAGE_TAG
            imagePullSecrets: $DPF_SNAP_IMAGE_PULL_SECRET
            snapRpcInitConf: |
              nvme_subsystem_create --nqn nqn.2022-10.io.nvda.nvme:0
  interfaces:
    - name: app_sf
      network: mybrsfc
```
</details>

<details markdown="1"><summary>doca-snap DPUServiceTemplate</summary>

[embedmd]:#(non-trusted-host/nvme-hotplug-pf/doca-snap-dpuservicetemplate.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: doca-snap
  namespace: dpf-operator-system
spec:
  deploymentServiceName: doca-snap
  helmChart:
    source:
      repoURL: $DPF_CHART_REPO
      version: $DPF_CHART_VERSION
      chart: dpf-storage
    values:
      serviceDaemonSet:
        resources:
          memory: "2Gi"
          hugepages-2Mi: "4Gi"
          cpu: "8"
          nvidia.com/bf_sf: 1
  resourceRequirements:
    memory: "2Gi"
    hugepages-2Mi: "4Gi"
    cpu: "8"
    nvidia.com/bf_sf: 1
```
</details>

<details markdown="1"><summary>dpf-provisioning-storage DPUFlavor</summary>

[embedmd]:#(non-trusted-host/nvme-hotplug-pf/dpf-provisioning-storage-dpuflavor.yaml)
```yaml
---
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUFlavor
metadata:
  name: dpf-provisioning-storage
  namespace: dpf-operator-system
spec:
  bfcfgParameters:
    - UPDATE_ATF_UEFI=yes
    - UPDATE_DPU_OS=yes
    - WITH_NIC_FW_UPDATE=yes
  configFiles:
    - operation: override
      path: /etc/mellanox/mlnx-bf.conf
      permissions: "0644"
      raw: |
        ALLOW_SHARED_RQ="no"
        IPSEC_FULL_OFFLOAD="no"
        ENABLE_ESWITCH_MULTIPORT="yes"
        RDMA_SET_NETNS_EXCLUSIVE="no"
    - operation: override
      path: /etc/mellanox/mlnx-ovs.conf
      permissions: "0644"
      raw: |
        CREATE_OVS_BRIDGES="no"
        OVS_DOCA="yes"
    - operation: override
      path: /etc/mellanox/mlnx-sf.conf
      permissions: "0644"
      raw: ""
  grub:
    kernelParameters:
      - console=hvc0
      - console=ttyAMA0
      - earlycon=pl011,0x13010000
      - fixrttc
      - net.ifnames=0
      - biosdevname=0
      - iommu.passthrough=1
      - cgroup_no_v1=net_prio,net_cls
      - hugepagesz=2048kB
      - hugepages=5120
  nvconfig:
    - device: "*"
      parameters:
        - PF_BAR2_ENABLE=0
        - PER_PF_NUM_SF=1
        - PF_TOTAL_SF=20
        - PF_SF_BAR_SIZE=10
        - NUM_PF_MSIX_VALID=0
        - PF_NUM_PF_MSIX_VALID=1
        - PF_NUM_PF_MSIX=228
        - INTERNAL_CPU_MODEL=1
        - INTERNAL_CPU_OFFLOAD_ENGINE=0
        - SRIOV_EN=1
        - NUM_OF_VFS=46
        - LAG_RESOURCE_ALLOCATION=1
        - PCI_SWITCH_EMULATION_ENABLE=1
        - PCI_SWITCH_EMULATION_NUM_PORT=32
        - NVME_EMULATION_ENABLE=1
        - NVME_EMULATION_NUM_PF=0
  ovs:
    rawConfigScript: |
      _ovs-vsctl() {
        ovs-vsctl --no-wait --timeout 15 "$@"
      }

      _ovs-vsctl set Open_vSwitch . other_config:doca-init=true
      _ovs-vsctl set Open_vSwitch . other_config:dpdk-max-memzones=50000
      _ovs-vsctl set Open_vSwitch . other_config:hw-offload=true
      _ovs-vsctl set Open_vSwitch . other_config:pmd-quiet-idle=true
      _ovs-vsctl set Open_vSwitch . other_config:max-idle=20000
      _ovs-vsctl set Open_vSwitch . other_config:max-revalidator=5000
      _ovs-vsctl --if-exists del-br ovsbr1
      _ovs-vsctl --if-exists del-br ovsbr2
      _ovs-vsctl --may-exist add-br br-sfc
      _ovs-vsctl set bridge br-sfc datapath_type=netdev
      _ovs-vsctl set bridge br-sfc fail_mode=secure
      _ovs-vsctl --may-exist add-port br-sfc p0
      _ovs-vsctl set Interface p0 type=dpdk
      _ovs-vsctl set Port p0 external_ids:dpf-type=physical
```
</details>

<details markdown="1"><summary>storage DPUDeployment</summary>

[embedmd]:#(non-trusted-host/nvme-hotplug-pf/dpudeployment.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUDeployment
metadata:
  name: storage
  namespace: dpf-operator-system
spec:
  dpus:
    bfb: bf-bundle
    flavor: dpf-provisioning-storage
    dpuSets:
      - nameSuffix: "dpuset1"
        nodeSelector:
          matchLabels:
            feature.node.kubernetes.io/dpu-enabled: "true"
  services:
    snap-host-controller:
      serviceTemplate: snap-host-controller
      serviceConfiguration: snap-host-controller
    snap-controller:
      serviceTemplate: snap-controller
      serviceConfiguration: snap-controller
    snap-node-driver:
      serviceTemplate: snap-node-driver
      serviceConfiguration: snap-node-driver
    snap-configuration:
      serviceTemplate: snap-configuration
      serviceConfiguration: snap-configuration
    doca-snap:
      serviceTemplate: doca-snap
      serviceConfiguration: doca-snap
    block-storage-dpu-plugin:
      serviceTemplate: block-storage-dpu-plugin
      serviceConfiguration: block-storage-dpu-plugin
    spdk-csi-controller:
      serviceTemplate: spdk-csi-controller
      serviceConfiguration: spdk-csi-controller
    spdk-csi-controller-dpu:
      serviceTemplate: spdk-csi-controller-dpu
      serviceConfiguration: spdk-csi-controller-dpu
  serviceChains:
    switches:
      - ports:
          - serviceInterface:
              matchLabels:
                uplink: p1
          - serviceInterface:
              matchLabels:
                uplink: pf1hpf
          - service:
              name: doca-snap
              interface: app_sf
              ipam:
                matchLabels:
                  svc.dpu.nvidia.com/pool: storage-pool
```
</details>

<details markdown="1"><summary>p1 DPUServiceInterface</summary>

[embedmd]:#(non-trusted-host/nvme-hotplug-pf/p1-dpuserviceinterface.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceInterface
metadata:
  name: p1
  namespace: dpf-operator-system
spec:
  template:
    spec:
      nodeSelector:
        matchExpressions:
          - key: kubernetes.io/os
            operator: In
            values:
              - "linux"
      template:
        metadata:
          labels:
            uplink: "p1"
        spec:
          interfaceType: physical
          physical:
            interfaceName: p1
```
</details>

<details markdown="1"><summary>pf1hpf DPUServiceInterface</summary>

[embedmd]:#(non-trusted-host/nvme-hotplug-pf/pf1hpf-dpuserviceinterface.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceInterface
metadata:
  name: pf1hpf
  namespace: dpf-operator-system
spec:
  template:
    spec:
      nodeSelector:
        matchExpressions:
          - key: kubernetes.io/os
            operator: In
            values:
              - "linux"
      template:
        metadata:
          labels:
            uplink: "pf1hpf"
        spec:
          interfaceType: pf
          pf:
            pfID: 1
```
</details>

<details markdown="1"><summary>policy-block DPUStoragePolicy</summary>

[embedmd]:#(non-trusted-host/nvme-hotplug-pf/policy-block-dpustoragepolicy.yaml)
```yaml
---
apiVersion: storage.dpu.nvidia.com/v1alpha1
kind: DPUStoragePolicy
metadata:
  name: policy-block
  namespace: dpf-operator-system
spec:
  dpuStorageVendors:
    - spdk-csi
  selectionAlgorithm: "NumberVolumes"
  parameters: {}
```
</details>

<details markdown="1"><summary>snap-configuration DPUServiceConfiguration</summary>

[embedmd]:#(non-trusted-host/nvme-hotplug-pf/snap-configuration-dpuserviceconfiguration.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: snap-configuration
  namespace: dpf-operator-system
spec:
  deploymentServiceName: snap-configuration
  serviceConfiguration:
    helmChart:
      values:
        dpu:
          deployCrds: true
          configuration:
            enabled: true
            # RBAC roles for clients from host cluster
            # serviceAccount names must be aligned with the serviceAccount names from DPUServiceCredentialRequest
            rbacRoles:
              snapController:
                serviceAccount: snap-controller-sa
```
</details>

<details markdown="1"><summary>snap-configuration DPUServiceTemplate</summary>

[embedmd]:#(non-trusted-host/nvme-hotplug-pf/snap-configuration-dpuservicetemplate.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: snap-configuration
  namespace: dpf-operator-system
spec:
  deploymentServiceName: snap-configuration
  helmChart:
    source:
      repoURL: $DPF_CHART_REPO
      version: $DPF_CHART_VERSION
      chart: dpf-storage
```
</details>

<details markdown="1"><summary>snap-controller DPUServiceConfiguration</summary>

[embedmd]:#(non-trusted-host/nvme-hotplug-pf/snap-controller-dpuserviceconfiguration.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: snap-controller
  namespace: dpf-operator-system
spec:
  deploymentServiceName: snap-controller
  upgradePolicy:
    applyNodeEffect: false
  serviceConfiguration:
    deployInCluster: true
    helmChart:
      values:
        imagePullSecrets: $DPF_IMAGE_PULL_SECRET
        host:
          snapController:
            enabled: true
            image:
              repository: $DPF_IMAGE_REGISTRY/storage-system
              tag: $DPF_IMAGE_TAG
            config:
              # required parameter, name of the secret that contains connection
              # details to access the DPU cluster.
              # this secret should be created by the DPUServiceCredentialRequest API.
              dpuClusterSecret: snap-controller-dpu-cluster-credentials
              # controls where to search for StorageVendor and StoragePolicy CRs
              configNamespace: dpf-operator-system
            affinity:
              nodeAffinity:
                requiredDuringSchedulingIgnoredDuringExecution:
                  nodeSelectorTerms:
                    - matchExpressions:
                        - key: "node-role.kubernetes.io/master"
                          operator: Exists
                    - matchExpressions:
                        - key: "node-role.kubernetes.io/control-plane"
                          operator: Exists
```
</details>

<details markdown="1"><summary>snap-controller DPUServiceCredentialRequest</summary>

[embedmd]:#(non-trusted-host/nvme-hotplug-pf/snap-controller-dpuservicecredentialrequest.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceCredentialRequest
metadata:
  name: snap-controller-credentials
  namespace: dpf-operator-system
spec:
  duration: 10m
  serviceAccount:
    name: snap-controller-sa
    namespace: dpf-operator-system
  targetCluster:
    name: dpu-cplane-tenant1
    namespace: dpu-cplane-tenant1
  type: tokenFile
  secret:
    name: snap-controller-dpu-cluster-credentials
    namespace: dpf-operator-system
```
</details>

<details markdown="1"><summary>snap-controller DPUServiceTemplate</summary>

[embedmd]:#(non-trusted-host/nvme-hotplug-pf/snap-controller-dpuservicetemplate.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: snap-controller
  namespace: dpf-operator-system
spec:
  deploymentServiceName: snap-controller
  helmChart:
    source:
      repoURL: $DPF_CHART_REPO
      version: $DPF_CHART_VERSION
      chart: dpf-storage
```
</details>

<details markdown="1"><summary>snap-host-controller DPUServiceConfiguration</summary>

[embedmd]:#(non-trusted-host/nvme-hotplug-pf/snap-host-controller-dpuserviceconfiguration.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: snap-host-controller
  namespace: dpf-operator-system
spec:
  deploymentServiceName: snap-host-controller
  upgradePolicy:
    applyNodeEffect: false
  serviceConfiguration:
    deployInCluster: true
    helmChart:
      values:
        imagePullSecrets: $DPF_IMAGE_PULL_SECRET
        host:
          snapHostController:
            image:
              repository: $DPF_IMAGE_REGISTRY/storage-system
              tag: $DPF_IMAGE_TAG
            enabled: true
            config:
              dpuClusterName: dpu-cplane-tenant1
              dpuClusterNamespace: dpu-cplane-tenant1
              targetNamespace: dpf-operator-system
            affinity:
              nodeAffinity:
                requiredDuringSchedulingIgnoredDuringExecution:
                  nodeSelectorTerms:
                  - matchExpressions:
                      - key: "node-role.kubernetes.io/master"
                        operator: Exists
                  - matchExpressions:
                      - key: "node-role.kubernetes.io/control-plane"
                        operator: Exists
```
</details>

<details markdown="1"><summary>snap-host-controller DPUServiceTemplate</summary>

[embedmd]:#(non-trusted-host/nvme-hotplug-pf/snap-host-controller-dpuservicetemplate.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: snap-host-controller
  namespace: dpf-operator-system
spec:
  deploymentServiceName: snap-host-controller
  helmChart:
    source:
      repoURL: $DPF_CHART_REPO
      version: $DPF_CHART_VERSION
      chart: dpf-storage
```
</details>

<details markdown="1"><summary>snap-node-driver DPUServiceConfiguration</summary>

[embedmd]:#(non-trusted-host/nvme-hotplug-pf/snap-node-driver-dpuserviceconfiguration.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: snap-node-driver
  namespace: dpf-operator-system
spec:
  deploymentServiceName: snap-node-driver
  serviceConfiguration:
    helmChart:
      values:
        imagePullSecrets: $DPF_IMAGE_PULL_SECRET
        dpu:
          snapNodeDriver:
            enabled: true
            image:
              repository: $DPF_IMAGE_REGISTRY/storage-system
              tag: $DPF_IMAGE_TAG
```
</details>

<details markdown="1"><summary>snap-node-driver DPUServiceTemplate</summary>

[embedmd]:#(non-trusted-host/nvme-hotplug-pf/snap-node-driver-dpuservicetemplate.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: snap-node-driver
  namespace: dpf-operator-system
spec:
  deploymentServiceName: snap-node-driver
  helmChart:
    source:
      repoURL: $DPF_CHART_REPO
      version: $DPF_CHART_VERSION
      chart: dpf-storage
```
</details>

<details markdown="1"><summary>spdk-csi-controller DPUServiceConfiguration</summary>

[embedmd]:#(non-trusted-host/nvme-hotplug-pf/spdk-csi-controller-dpuserviceconfiguration.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: spdk-csi-controller
  namespace: dpf-operator-system
spec:
  deploymentServiceName: spdk-csi-controller
  upgradePolicy:
    applyNodeEffect: false
  serviceConfiguration:
    deployInCluster: true
    helmChart:
      values:
        host:
          enabled: true
          imagePullSecrets: $SPDK_CSI_IMAGE_PULL_SECRET
          plugin:
            image:
              repository: $SPDK_CSI_IMAGE_REGISTRY/spdk-csi
              tag: $SPDK_CSI_IMAGE_TAG
          config:
            targets:
              nodes:
                # name of the target
                - name: spdk-target
                  # management address
                  rpcURL: http://10.33.33.33:8000
                  # type of the target, e.g. nvme-tcp, nvme-rdma
                  targetType: nvme-tcp
                  # target service IP
                  targetAddr: 10.44.44.100
            # required parameter, name of the secret that contains connection
            # details to access the DPU cluster.
            # this secret should be created by the DPUServiceCredentialRequest API.
            dpuClusterSecret: spdk-csi-controller-dpu-cluster-credentials
```
</details>

<details markdown="1"><summary>spdk-csi-controller DPUServiceCredentialRequest</summary>

[embedmd]:#(non-trusted-host/nvme-hotplug-pf/spdk-csi-controller-dpuservicecredentialrequest.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceCredentialRequest
metadata:
  name: spdk-csi-controller-credentials
  namespace: dpf-operator-system
spec:
  duration: 10m
  serviceAccount:
    name: spdk-csi-controller-sa
    namespace: dpf-operator-system
  targetCluster:
    name: dpu-cplane-tenant1
    namespace: dpu-cplane-tenant1
  type: tokenFile
  secret:
    name: spdk-csi-controller-dpu-cluster-credentials
    namespace: dpf-operator-system
```
</details>

<details markdown="1"><summary>spdk-csi-controller DPUServiceTemplate</summary>

[embedmd]:#(non-trusted-host/nvme-hotplug-pf/spdk-csi-controller-dpuservicetemplate.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: spdk-csi-controller
  namespace: dpf-operator-system
spec:
  deploymentServiceName: spdk-csi-controller
  helmChart:
    source:
      repoURL: $SPDK_CSI_CHART_REPO
      version: $SPDK_CSI_CHART_VERSION
      chart: spdk-csi-controller-chart
```
</details>

<details markdown="1"><summary>spdk-csi-controller-dpu DPUServiceConfiguration</summary>

[embedmd]:#(non-trusted-host/nvme-hotplug-pf/spdk-csi-controller-dpu-dpuserviceconfiguration.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: spdk-csi-controller-dpu
  namespace: dpf-operator-system
spec:
  deploymentServiceName: spdk-csi-controller-dpu
  serviceConfiguration:
    helmChart:
      values:
        dpu:
          enabled: true
          storageClass:
            # the name of the storage class that will be created for spdk-csi,
            # this StorageClass name should be used in the StorageVendor settings
            name: spdkcsi-sc
            # name of the secret that contains credentials for the remote SPDK target,
            # content of the secret is injected during CreateVolume request
            secretName: spdkcsi-secret
            # namespace of the secret with credentials for the remote SPDK target
            secretNamespace: dpf-operator-system
          rbacRoles:
            spdkCsiController:
              # the name of the service account for spdk-csi-controller
              # this value must be aligned with the value from the DPUServiceCredentialRequest
              serviceAccount: spdk-csi-controller-sa
```
</details>

<details markdown="1"><summary>spdk-csi-controller-dpu DPUServiceTemplate</summary>

[embedmd]:#(non-trusted-host/nvme-hotplug-pf/spdk-csi-controller-dpu-dpuservicetemplate.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: spdk-csi-controller-dpu
  namespace: dpf-operator-system
spec:
  deploymentServiceName: spdk-csi-controller-dpu
  helmChart:
    source:
      repoURL: $SPDK_CSI_CHART_REPO
      version: $SPDK_CSI_CHART_VERSION
      chart: spdk-csi-controller-chart
```
</details>

<details markdown="1"><summary>spdk-csi DPUStorageVendor</summary>

[embedmd]:#(non-trusted-host/nvme-hotplug-pf/spdk-csi-dpustoragevendor.yaml)
```yaml
---
apiVersion: storage.dpu.nvidia.com/v1alpha1
kind: DPUStorageVendor
metadata:
  name: spdk-csi
  namespace: dpf-operator-system
spec:
  storageClassName: spdkcsi-sc
  pluginName: nvidia-block
```
</details>

<details markdown="1"><summary>spdk-csi Secret</summary>

[embedmd]:#(non-trusted-host/nvme-hotplug-pf/spdk-csi-secret.yaml)
```yaml
---
apiVersion: v1
kind: Secret
metadata:
  name: spdkcsi-secret
  namespace: dpf-operator-system
  labels:
    # this label enables replication of the secret from the host to the dpu cluster
    dpu.nvidia.com/image-pull-secret: ""
stringData:
  # name field in the "rpcTokens" list should match name of the
  # spdk target from DPUService.helmChart.values.host.config.targets.nodes
  secret.json: |-
    {
      "rpcTokens": [
        {
          "name": "spdk-target",
          "username": "exampleuser",
          "password": "examplepassword"
        }
      ]
    }
```
</details>

<details markdown="1"><summary>storage-pool DPUServiceIPAM</summary>

[embedmd]:#(non-trusted-host/nvme-hotplug-pf/storage-pool-dpuserviceipam.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceIPAM
metadata:
  name: storage-pool
  namespace: dpf-operator-system
spec:
  metadata:
    labels:
      svc.dpu.nvidia.com/pool: storage-pool
  ipv4Subnet:
    subnet: "10.44.44.0/24"
    gateway: "10.44.44.1"
    perNodeIPCount: 20
```
</details>

```shell
cat non-trusted-host/nvme-hotplug-pf/workload/*.yaml | envsubst | kubectl apply -f -
```

This will deploy the following objects:

<details markdown="1"><summary>test-volume-nvme-hotplug-pf DPUVolume</summary>

[embedmd]:#(non-trusted-host/nvme-hotplug-pf/workload/dpuvolume.yaml)
```yaml
---
apiVersion: storage.dpu.nvidia.com/v1alpha1
kind: DPUVolume
metadata:
  name: test-volume-nvme-hotplug-pf
  namespace: dpf-operator-system
spec:
  dpuStoragePolicyName: policy-block
  resources:
    requests:
      storage: 1Gi
  accessModes:
  - ReadWriteOnce
  volumeMode: Block
```
</details>

<details markdown="1"><summary>test-volume-attachment-nvme-hotplug-pf DPUVolumeAttachment</summary>

[embedmd]:#(non-trusted-host/nvme-hotplug-pf/workload/dpuvolumeattachment.yaml)
```yaml
---
apiVersion: storage.dpu.nvidia.com/v1alpha1
kind: DPUVolumeAttachment
metadata:
  name: test-volume-attachment-nvme-hotplug-pf
  namespace: dpf-operator-system
spec:
  dpuNodeName: $KUBE_NODE_NAME
  dpuVolumeName: test-volume-nvme-hotplug-pf
  functionType: pf
  hotplugFunction: true
```
</details>

#### VirtioFS with hot-plug Physical Functions

```shell
cat non-trusted-host/virtiofs-hotplug-pf/*.yaml | envsubst | kubectl apply -f -
```

This will deploy the following objects:

<details markdown="1"><summary>bf-bundle BFB</summary>

[embedmd]:#(non-trusted-host/virtiofs-hotplug-pf/bf-bundle-bfb.yaml)
```yaml
---
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: BFB
metadata:
  name: bf-bundle
  namespace: dpf-operator-system
spec:
  url: $BLUEFIELD_BITSTREAM
```
</details>

<details markdown="1"><summary>doca-snap DPUServiceConfiguration</summary>

[embedmd]:#(non-trusted-host/virtiofs-hotplug-pf/doca-snap-dpuserviceconfiguration.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: doca-snap
  namespace: dpf-operator-system
spec:
  deploymentServiceName: doca-snap
  serviceConfiguration:
    helmChart:
      values:
        dpu:
          docaSnap:
            enabled: true
            image:
              repository: $DPF_SNAP_IMAGE_REGISTRY/doca_vfs
              tag: $DPF_SNAP_IMAGE_TAG
            imagePullSecrets: $DPF_SNAP_IMAGE_PULL_SECRET
  interfaces:
    - name: app_sf
      network: mybrsfc
```
</details>

<details markdown="1"><summary>doca-snap DPUServiceTemplate</summary>

[embedmd]:#(non-trusted-host/virtiofs-hotplug-pf/doca-snap-dpuservicetemplate.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: doca-snap
  namespace: dpf-operator-system
spec:
  deploymentServiceName: doca-snap
  helmChart:
    source:
      repoURL: $DPF_CHART_REPO
      version: $DPF_CHART_VERSION
      chart: dpf-storage
    values:
      serviceDaemonSet:
        resources:
          memory: "2Gi"
          hugepages-2Mi: "4Gi"
          cpu: "8"
          nvidia.com/bf_sf: 1
  resourceRequirements:
    memory: "2Gi"
    hugepages-2Mi: "4Gi"
    cpu: "8"
    nvidia.com/bf_sf: 1
```
</details>

<details markdown="1"><summary>dpf-provisioning-storage DPUFlavor</summary>

[embedmd]:#(non-trusted-host/virtiofs-hotplug-pf/dpf-provisioning-storage-dpuflavor.yaml)
```yaml
---
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUFlavor
metadata:
  name: dpf-provisioning-storage
  namespace: dpf-operator-system
spec:
  bfcfgParameters:
    - UPDATE_ATF_UEFI=yes
    - UPDATE_DPU_OS=yes
    - WITH_NIC_FW_UPDATE=yes
  configFiles:
    - operation: override
      path: /etc/mellanox/mlnx-bf.conf
      permissions: "0644"
      raw: |
        ALLOW_SHARED_RQ="no"
        IPSEC_FULL_OFFLOAD="no"
        ENABLE_ESWITCH_MULTIPORT="yes"
        RDMA_SET_NETNS_EXCLUSIVE="no"
    - operation: override
      path: /etc/mellanox/mlnx-ovs.conf
      permissions: "0644"
      raw: |
        CREATE_OVS_BRIDGES="no"
        OVS_DOCA="yes"
    - operation: override
      path: /etc/mellanox/mlnx-sf.conf
      permissions: "0644"
      raw: ""
  grub:
    kernelParameters:
      - console=hvc0
      - console=ttyAMA0
      - earlycon=pl011,0x13010000
      - fixrttc
      - net.ifnames=0
      - biosdevname=0
      - iommu.passthrough=1
      - cgroup_no_v1=net_prio,net_cls
      - hugepagesz=2048kB
      - hugepages=5120
  nvconfig:
    - device: "*"
      parameters:
        - PF_BAR2_ENABLE=0
        - PER_PF_NUM_SF=1
        - PF_TOTAL_SF=20
        - PF_SF_BAR_SIZE=10
        - NUM_PF_MSIX_VALID=0
        - PF_NUM_PF_MSIX_VALID=1
        - PF_NUM_PF_MSIX=228
        - INTERNAL_CPU_MODEL=1
        - INTERNAL_CPU_OFFLOAD_ENGINE=0
        - SRIOV_EN=1
        - NUM_OF_VFS=46
        - LAG_RESOURCE_ALLOCATION=1
        - PCI_SWITCH_EMULATION_ENABLE=1
        - PCI_SWITCH_EMULATION_NUM_PORT=32
        - VIRTIO_FS_EMULATION_ENABLE=1
        - VIRTIO_FS_EMULATION_NUM_PF=0
  ovs:
    rawConfigScript: |
      _ovs-vsctl() {
        ovs-vsctl --no-wait --timeout 15 "$@"
      }

      _ovs-vsctl set Open_vSwitch . other_config:doca-init=true
      _ovs-vsctl set Open_vSwitch . other_config:dpdk-max-memzones=50000
      _ovs-vsctl set Open_vSwitch . other_config:hw-offload=true
      _ovs-vsctl set Open_vSwitch . other_config:pmd-quiet-idle=true
      _ovs-vsctl set Open_vSwitch . other_config:max-idle=20000
      _ovs-vsctl set Open_vSwitch . other_config:max-revalidator=5000
      _ovs-vsctl --if-exists del-br ovsbr1
      _ovs-vsctl --if-exists del-br ovsbr2
      _ovs-vsctl --may-exist add-br br-sfc
      _ovs-vsctl set bridge br-sfc datapath_type=netdev
      _ovs-vsctl set bridge br-sfc fail_mode=secure
      _ovs-vsctl --may-exist add-port br-sfc p0
      _ovs-vsctl set Interface p0 type=dpdk
      _ovs-vsctl set Port p0 external_ids:dpf-type=physical
```
</details>

<details markdown="1"><summary>storage DPUDeployment</summary>

[embedmd]:#(non-trusted-host/virtiofs-hotplug-pf/dpudeployment.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUDeployment
metadata:
  name: storage
  namespace: dpf-operator-system
spec:
  dpus:
    bfb: bf-bundle
    flavor: dpf-provisioning-storage
    dpuSets:
      - nameSuffix: "dpuset1"
        nodeSelector:
          matchLabels:
            feature.node.kubernetes.io/dpu-enabled: "true"
  services:
    snap-host-controller:
      serviceTemplate: snap-host-controller
      serviceConfiguration: snap-host-controller
    snap-controller:
      serviceTemplate: snap-controller
      serviceConfiguration: snap-controller
    snap-node-driver:
      serviceTemplate: snap-node-driver
      serviceConfiguration: snap-node-driver
    snap-configuration:
      serviceTemplate: snap-configuration
      serviceConfiguration: snap-configuration
    doca-snap:
      serviceTemplate: doca-snap
      serviceConfiguration: doca-snap
    fs-storage-dpu-plugin:
      serviceTemplate: fs-storage-dpu-plugin
      serviceConfiguration: fs-storage-dpu-plugin
    nfs-csi-controller:
      serviceTemplate: nfs-csi-controller
      serviceConfiguration: nfs-csi-controller
    nfs-csi-controller-dpu:
      serviceTemplate: nfs-csi-controller-dpu
      serviceConfiguration: nfs-csi-controller-dpu
  serviceChains:
    switches:
      - ports:
          - serviceInterface:
              matchLabels:
                uplink: p1
          - serviceInterface:
              matchLabels:
                uplink: pf1hpf
          - service:
              name: doca-snap
              interface: app_sf
              ipam:
                matchLabels:
                  svc.dpu.nvidia.com/pool: storage-pool
          - service:
              name: fs-storage-dpu-plugin
              interface: app_sf
              ipam:
                matchLabels:
                  svc.dpu.nvidia.com/pool: storage-pool
```
</details>

<details markdown="1"><summary>fs-storage-dpu-plugin DPUServiceConfiguration</summary>

[embedmd]:#(non-trusted-host/virtiofs-hotplug-pf/fs-storage-dpu-plugin-dpuserviceconfiguration.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: fs-storage-dpu-plugin
  namespace: dpf-operator-system
spec:
  deploymentServiceName: fs-storage-dpu-plugin
  serviceConfiguration:
    helmChart:
      values:
        imagePullSecrets: $DPF_IMAGE_PULL_SECRET
        dpu:
          fsStorageVendorDpuPlugin:
            enabled: true
            image:
              repository: $DPF_IMAGE_REGISTRY/storage-host
              tag: $DPF_IMAGE_TAG
  interfaces:
    - name: app_sf
      network: mybrsfc
```
</details>

<details markdown="1"><summary>fs-storage-dpu-plugin DPUServiceTemplate</summary>

[embedmd]:#(non-trusted-host/virtiofs-hotplug-pf/fs-storage-dpu-plugin-dpuservicetemplate.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: fs-storage-dpu-plugin
  namespace: dpf-operator-system
spec:
  deploymentServiceName: fs-storage-dpu-plugin
  helmChart:
    source:
      repoURL: $DPF_CHART_REPO
      version: $DPF_CHART_VERSION
      chart: dpf-storage
    values:
      serviceDaemonSet:
        resources:
          nvidia.com/bf_sf: 1
  resourceRequirements:
    nvidia.com/bf_sf: 1
```
</details>

<details markdown="1"><summary>nfs-csi-controller DPUServiceConfiguration</summary>

[embedmd]:#(non-trusted-host/virtiofs-hotplug-pf/nfs-csi-controller-dpuserviceconfiguration.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: nfs-csi-controller
  namespace: dpf-operator-system
spec:
  deploymentServiceName: nfs-csi-controller
  upgradePolicy:
    applyNodeEffect: false
  serviceConfiguration:
    deployInCluster: true
    helmChart:
      values:
        host:
          enabled: true
          plugin:
            image:
              repository: registry.k8s.io/sig-storage/nfsplugin
              tag: v4.11.0
          config:
            # required parameter, name of the secret that contains connection
            # details to access the DPU cluster.
            # this secret should be created by the DPUServiceCredentialRequest API.
            dpuClusterSecret: nfs-csi-controller-dpu-cluster-credentials
```
</details>

<details markdown="1"><summary>nfs-csi-controller DPUServiceCredentialRequest</summary>

[embedmd]:#(non-trusted-host/virtiofs-hotplug-pf/nfs-csi-controller-dpuservicecredentialrequest.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceCredentialRequest
metadata:
  name: nfs-csi-controller-credentials
  namespace: dpf-operator-system
spec:
  duration: 10m
  serviceAccount:
    name: nfs-csi-controller-sa
    namespace: dpf-operator-system
  targetCluster:
    name: dpu-cplane-tenant1
    namespace: dpu-cplane-tenant1
  type: tokenFile
  secret:
    name: nfs-csi-controller-dpu-cluster-credentials
    namespace: dpf-operator-system
```
</details>

<details markdown="1"><summary>nfs-csi-controller DPUServiceTemplate</summary>

[embedmd]:#(non-trusted-host/virtiofs-hotplug-pf/nfs-csi-controller-dpuservicetemplate.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: nfs-csi-controller
  namespace: dpf-operator-system
spec:
  deploymentServiceName: nfs-csi-controller
  helmChart:
    source:
      repoURL: $NFS_CSI_CHART_REPO
      version: $NFS_CSI_CHART_VERSION
      chart: nfs-csi-controller-chart
```
</details>

<details markdown="1"><summary>nfs-csi-controller-dpu DPUServiceConfiguration</summary>

[embedmd]:#(non-trusted-host/virtiofs-hotplug-pf/nfs-csi-controller-dpu-dpuserviceconfiguration.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: nfs-csi-controller-dpu
  namespace: dpf-operator-system
spec:
  deploymentServiceName: nfs-csi-controller-dpu
  serviceConfiguration:
    helmChart:
      values:
        dpu:
          enabled: true
          storageClasses:
            # List of storage classes to be created for nfs-csi
            # These StorageClass names should be used in the StorageVendor settings
            - name: nfs-csi
              parameters:
                server: 10.44.44.100
                share: /srv/nfs/share
          rbacRoles:
            nfsCsiController:
              # the name of the service account for nfs-csi-controller
              # this value must be aligned with the value from the DPUServiceCredentialRequest
              serviceAccount: nfs-csi-controller-sa
```
</details>

<details markdown="1"><summary>nfs-csi-controller-dpu DPUServiceTemplate</summary>

[embedmd]:#(non-trusted-host/virtiofs-hotplug-pf/nfs-csi-controller-dpu-dpuservicetemplate.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: nfs-csi-controller-dpu
  namespace: dpf-operator-system
spec:
  deploymentServiceName: nfs-csi-controller-dpu
  helmChart:
    source:
      repoURL: $NFS_CSI_CHART_REPO
      version: $NFS_CSI_CHART_VERSION
      chart: nfs-csi-controller-chart
```
</details>

<details markdown="1"><summary>nfs-csi DPUStorageVendor</summary>

[embedmd]:#(non-trusted-host/virtiofs-hotplug-pf/nfs-csi-dpustoragevendor.yaml)
```yaml
---
apiVersion: storage.dpu.nvidia.com/v1alpha1
kind: DPUStorageVendor
metadata:
  name: nfs-csi
  namespace: dpf-operator-system
spec:
  storageClassName: nfs-csi
  pluginName: nvidia-fs
```
</details>

<details markdown="1"><summary>p1 DPUServiceInterface</summary>

[embedmd]:#(non-trusted-host/virtiofs-hotplug-pf/p1-dpuserviceinterface.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceInterface
metadata:
  name: p1
  namespace: dpf-operator-system
spec:
  template:
    spec:
      nodeSelector:
        matchExpressions:
          - key: kubernetes.io/os
            operator: In
            values:
              - "linux"
      template:
        metadata:
          labels:
            uplink: "p1"
        spec:
          interfaceType: physical
          physical:
            interfaceName: p1
```
</details>

<details markdown="1"><summary>pf1hpf DPUServiceInterface</summary>

[embedmd]:#(non-trusted-host/virtiofs-hotplug-pf/pf1hpf-dpuserviceinterface.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceInterface
metadata:
  name: pf1hpf
  namespace: dpf-operator-system
spec:
  template:
    spec:
      nodeSelector:
        matchExpressions:
          - key: kubernetes.io/os
            operator: In
            values:
              - "linux"
      template:
        metadata:
          labels:
            uplink: "pf1hpf"
        spec:
          interfaceType: pf
          pf:
            pfID: 1
```
</details>

<details markdown="1"><summary>policy-fs DPUStoragePolicy</summary>

[embedmd]:#(non-trusted-host/virtiofs-hotplug-pf/policy-fs-dpustoragepolicy.yaml)
```yaml
---
apiVersion: storage.dpu.nvidia.com/v1alpha1
kind: DPUStoragePolicy
metadata:
  name: policy-fs
  namespace: dpf-operator-system
spec:
  dpuStorageVendors:
    - nfs-csi
  selectionAlgorithm: "NumberVolumes"
  parameters: {}
```
</details>

<details markdown="1"><summary>snap-configuration DPUServiceConfiguration</summary>

[embedmd]:#(non-trusted-host/virtiofs-hotplug-pf/snap-configuration-dpuserviceconfiguration.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: snap-configuration
  namespace: dpf-operator-system
spec:
  deploymentServiceName: snap-configuration
  serviceConfiguration:
    helmChart:
      values:
        dpu:
          deployCrds: true
          configuration:
            enabled: true
            # RBAC roles for clients from host cluster
            # serviceAccount names must be aligned with the serviceAccount names from DPUServiceCredentialRequest
            rbacRoles:
              snapController:
                serviceAccount: snap-controller-sa
```
</details>

<details markdown="1"><summary>snap-configuration DPUServiceTemplate</summary>

[embedmd]:#(non-trusted-host/virtiofs-hotplug-pf/snap-configuration-dpuservicetemplate.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: snap-configuration
  namespace: dpf-operator-system
spec:
  deploymentServiceName: snap-configuration
  helmChart:
    source:
      repoURL: $DPF_CHART_REPO
      version: $DPF_CHART_VERSION
      chart: dpf-storage
```
</details>

<details markdown="1"><summary>snap-controller DPUServiceConfiguration</summary>

[embedmd]:#(non-trusted-host/virtiofs-hotplug-pf/snap-controller-dpuserviceconfiguration.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: snap-controller
  namespace: dpf-operator-system
spec:
  deploymentServiceName: snap-controller
  upgradePolicy:
    applyNodeEffect: false
  serviceConfiguration:
    deployInCluster: true
    helmChart:
      values:
        imagePullSecrets: $DPF_IMAGE_PULL_SECRET
        host:
          snapController:
            enabled: true
            image:
              repository: $DPF_IMAGE_REGISTRY/storage-system
              tag: $DPF_IMAGE_TAG
            config:
              # required parameter, name of the secret that contains connection
              # details to access the DPU cluster.
              # this secret should be created by the DPUServiceCredentialRequest API.
              dpuClusterSecret: snap-controller-dpu-cluster-credentials
              # controls where to search for StorageVendor and StoragePolicy CRs
              configNamespace: dpf-operator-system
            affinity:
              nodeAffinity:
                requiredDuringSchedulingIgnoredDuringExecution:
                  nodeSelectorTerms:
                    - matchExpressions:
                        - key: "node-role.kubernetes.io/master"
                          operator: Exists
                    - matchExpressions:
                        - key: "node-role.kubernetes.io/control-plane"
                          operator: Exists
```
</details>

<details markdown="1"><summary>snap-controller DPUServiceCredentialRequest</summary>

[embedmd]:#(non-trusted-host/virtiofs-hotplug-pf/snap-controller-dpuservicecredentialrequest.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceCredentialRequest
metadata:
  name: snap-controller-credentials
  namespace: dpf-operator-system
spec:
  duration: 10m
  serviceAccount:
    name: snap-controller-sa
    namespace: dpf-operator-system
  targetCluster:
    name: dpu-cplane-tenant1
    namespace: dpu-cplane-tenant1
  type: tokenFile
  secret:
    name: snap-controller-dpu-cluster-credentials
    namespace: dpf-operator-system
```
</details>

<details markdown="1"><summary>snap-controller DPUServiceTemplate</summary>

[embedmd]:#(non-trusted-host/virtiofs-hotplug-pf/snap-controller-dpuservicetemplate.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: snap-controller
  namespace: dpf-operator-system
spec:
  deploymentServiceName: snap-controller
  helmChart:
    source:
      repoURL: $DPF_CHART_REPO
      version: $DPF_CHART_VERSION
      chart: dpf-storage
```
</details>

<details markdown="1"><summary>snap-host-controller DPUServiceConfiguration</summary>

[embedmd]:#(non-trusted-host/virtiofs-hotplug-pf/snap-host-controller-dpuserviceconfiguration.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: snap-host-controller
  namespace: dpf-operator-system
spec:
  deploymentServiceName: snap-host-controller
  upgradePolicy:
    applyNodeEffect: false
  serviceConfiguration:
    deployInCluster: true
    helmChart:
      values:
        imagePullSecrets: $DPF_IMAGE_PULL_SECRET
        host:
          snapHostController:
            image:
              repository: $DPF_IMAGE_REGISTRY/storage-system
              tag: $DPF_IMAGE_TAG
            enabled: true
            config:
              dpuClusterName: dpu-cplane-tenant1
              dpuClusterNamespace: dpu-cplane-tenant1
              targetNamespace: dpf-operator-system
            affinity:
              nodeAffinity:
                requiredDuringSchedulingIgnoredDuringExecution:
                  nodeSelectorTerms:
                  - matchExpressions:
                      - key: "node-role.kubernetes.io/master"
                        operator: Exists
                  - matchExpressions:
                      - key: "node-role.kubernetes.io/control-plane"
                        operator: Exists
```
</details>

<details markdown="1"><summary>snap-host-controller DPUServiceTemplate</summary>

[embedmd]:#(non-trusted-host/virtiofs-hotplug-pf/snap-host-controller-dpuservicetemplate.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: snap-host-controller
  namespace: dpf-operator-system
spec:
  deploymentServiceName: snap-host-controller
  helmChart:
    source:
      repoURL: $DPF_CHART_REPO
      version: $DPF_CHART_VERSION
      chart: dpf-storage
```
</details>

<details markdown="1"><summary>snap-node-driver DPUServiceConfiguration</summary>

[embedmd]:#(non-trusted-host/virtiofs-hotplug-pf/snap-node-driver-dpuserviceconfiguration.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: snap-node-driver
  namespace: dpf-operator-system
spec:
  deploymentServiceName: snap-node-driver
  serviceConfiguration:
    helmChart:
      values:
        imagePullSecrets: $DPF_IMAGE_PULL_SECRET
        dpu:
          snapNodeDriver:
            enabled: true
            image:
              repository: $DPF_IMAGE_REGISTRY/storage-system
              tag: $DPF_IMAGE_TAG
```
</details>

<details markdown="1"><summary>snap-node-driver DPUServiceTemplate</summary>

[embedmd]:#(non-trusted-host/virtiofs-hotplug-pf/snap-node-driver-dpuservicetemplate.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: snap-node-driver
  namespace: dpf-operator-system
spec:
  deploymentServiceName: snap-node-driver
  helmChart:
    source:
      repoURL: $DPF_CHART_REPO
      version: $DPF_CHART_VERSION
      chart: dpf-storage
```
</details>

<details markdown="1"><summary>storage-pool DPUServiceIPAM</summary>

[embedmd]:#(non-trusted-host/virtiofs-hotplug-pf/storage-pool-dpuserviceipam.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceIPAM
metadata:
  name: storage-pool
  namespace: dpf-operator-system
spec:
  metadata:
    labels:
      svc.dpu.nvidia.com/pool: storage-pool
  ipv4Subnet:
    subnet: "10.44.44.0/24"
    gateway: "10.44.44.1"
    perNodeIPCount: 20
```
</details>

```shell
cat non-trusted-host/virtiofs-hotplug-pf/workload/*.yaml | envsubst | kubectl apply -f -
```

This will deploy the following objects:

<details markdown="1"><summary>test-volume-virtiofs-hotplug-pf DPUVolume</summary>

[embedmd]:#(non-trusted-host/virtiofs-hotplug-pf/workload/dpuvolume.yaml)
```yaml
---
apiVersion: storage.dpu.nvidia.com/v1alpha1
kind: DPUVolume
metadata:
  name: test-volume-virtiofs-hotplug-pf
  namespace: dpf-operator-system
spec:
  dpuStoragePolicyName: policy-fs
  resources:
    requests:
      storage: 1Gi
  accessModes:
  - ReadWriteOnce
  volumeMode: Filesystem
```
</details>

<details markdown="1"><summary>test-volume-attachment-virtiofs-hotplug-pf DPUVolumeAttachment</summary>

[embedmd]:#(non-trusted-host/virtiofs-hotplug-pf/workload/dpuvolumeattachment.yaml)
```yaml
---
apiVersion: storage.dpu.nvidia.com/v1alpha1
kind: DPUVolumeAttachment
metadata:
  name: test-volume-attachment-virtiofs-hotplug-pf
  namespace: dpf-operator-system
spec:
  dpuNodeName: $KUBE_NODE_NAME
  dpuVolumeName: test-volume-virtiofs-hotplug-pf
  functionType: pf
  hotplugFunction: true
```
</details>

### Workload Examples
Each scenario includes a `workload/` directory with:
* `StorageClass` and `StatefulSet` examples for trusted k8s cluster scenarios
* `DPUVolume` and `DPUVolumeAttachment` examples for Zero Trust scenarios

## Links

* [DPF Documentation](../../../../docs)
* [DPF Storage Subsystem Documentation](../../../../docs/public/developer-guides/services/storage.md)
