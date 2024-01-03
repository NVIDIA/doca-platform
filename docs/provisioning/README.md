# DPF-Provisioning-Controller
The DPF-Provisioning-Controller includes DPUSet controller and DPU controller:
- DPUSet controller manages a group of DPU and is responsible for handling group operations on DPUs such as creation, upgrade and removal of DPU.
- DPU controller manages a single DPU and is responsible for handling DPU configuration and provisioning operations.
- BFB controller manages a BFB files and is responsible for downloading/removing BFB files from the BFB volume.
----

## Quick Start
This guide will cover:
- Prerequisites
- Configure DPF provisioinging controller
- Install DPF provisioning controller
- Example of DPU provsioning
- Build image from source code

### Prerequisites
#### Dependent Software
- [Cert manager](https://github.com/cert-manager/cert-manager) version v1.14.2+
- [Node Feature Discovery](https://kubernetes-sigs.github.io/node-feature-discovery/stable/get-started/index.html) version v0.15.2+
- [Kamaji Tenant Control Plane](https://kamaji.clastix.io/concepts/#tenant-control-plane)

#### DPU Configuration
DPF uses SRIOV to setup communication channel between DPU and host. The port p0/p1 of DPU should be connected to the switch and ensure it is `up`. PF0VF0 is reserved for ovn-kubernetes management and PF0VF1 is reserved for host-dpu communication channel. So, those 2 VFs must be excluded from sriov operator policy for kubernetes resources.

Ditry configruation of DPU may cause dpu provisionig to fail, so DPU should use the the default configuration before provisioning.

#### Rshim Configuration
DPF uses rshim service on x86 host for DPU provisioning. The rshim service should be disabled inside the DPU's BMC to prevent it being reserved by the DPU's BMC.

#### DPU cluster discovery
DPUSet controller joins DPU to the `Kamaji tenant control plane` according to field
`.spec.dpuTemplate.spec.cluster` in `DPUSet`. 

Refer to [kamaji-installation](./kamaji-installation.md) for instructions on manipulating `Kamaji` on OCP.


#### DPU discovery
DPF uses [Node Feature Discovery](https://kubernetes-sigs.github.io/node-feature-discovery/stable/get-started/index.html) to detect when a node contains a DPU and label it with for DPU containing its device ID and PCI address. 

Refer to [node-feature-discovery-installation](./node-feature-discovery.md) for instructions on manipulating `node-feature-discovery` on OCP.

----

### Install DPF provisioning controller

#### Create namespace
Create `dpf-provisioning` namespace.
```bash
oc create ns dpf-provisioning
```
#### Create image pull secret
Currently, we use `gitlab-master.nvidia.com:5005` image registry to store the controller image and DMS image.
Donwloading images requires creating a named "regcred" dockerconfigjson image private secret.

Following [personal_access_tokens](https://gitlab-master.nvidia.com/help/user/profile/personal_access_tokens) steps to create a gitlab personal access token. This will be used to create a pull-image-private-registry secret.

Following this [instruction](https://kubernetes.io/docs/tasks/configure-pod-container/pull-image-private-registry/#create-a-secret-by-providing-credentials-on-the-command-line) to create a pull-image-private-registry secret in the `dpf-provisioning` namespace. The secret name must be `dpf-provisioning-regcred`.

#### Create PV and PVC
DPF provisioning controller requires shared storage with DMS pod to store BFB files. You need create a PV to store the BFB files and a PVC named `bfb-pvc` in `dpf-provisioning` namespace.

If using signle worker node as a test environment, you can use hostpath pv and pvc. If it is a multi-node environment, the cluster needs to provide shared storage that multiple nodes can access.

Following is an example to create a PV with hostpath.
1. Log on the worker node and create `/opt/dpf/bfb` directory, then 

2. Create PV and PVC
```bash
cat << EOF > dpf-provsioning-storage.yaml
apiVersion: v1
kind: PersistentVolume
metadata:
  name: bfb-pv
spec:
  capacity:
    storage: 10Gi
  volumeMode: Filesystem
  accessModes:
    - ReadWriteMany
  hostPath:
    path: "/opt/dpf/bfb"
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: bfb-pvc
  namespace: dpf-provisioning
spec:
  accessModes:
  - ReadWriteMany
  resources:
    requests:
      storage: 10Gi
  volumeMode: Filesystem
EOF

oc create -f dpf-provsioning-storage.yaml
```

### Create Security Context Constraints
Submit [SCC (Security Context Constraints)](https://docs.openshift.com/container-platform/4.14/authentication/managing-security-context-constraints.html)

```bash
cat << EOF > scc-dpf-provisioning.yaml
allowHostDirVolumePlugin: true
allowHostIPC: true
allowHostNetwork: true
allowHostPID: true
allowHostPorts: true
allowPrivilegeEscalation: true
allowPrivilegedContainer: true
allowedCapabilities:
- '*'
allowedUnsafeSysctls:
- '*'
apiVersion: security.openshift.io/v1
defaultAddCapabilities: null
fsGroup:
  type: RunAsAny
groups: []
kind: SecurityContextConstraints
metadata:
  name: dpf-provisioning
priority: 99
readOnlyRootFilesystem: false
requiredDropCapabilities: null
runAsUser:
  type: RunAsAny
seLinuxContext:
  type: RunAsAny
seccompProfiles:
- '*'
supplementalGroups:
  type: RunAsAny
users:
- system:serviceaccount:dpf-provisioning:dpf-provisioning-controller-manager
- system:serviceaccount:dpf-provisioning:default
volumes:
- '*'
EOF

oc create -f scc-dpf-provisioning.yaml
```

#### Deploy controller
1. Generate deployment  yaml files
```bash
make generate-manifests-dpf-provisioning
```
2. Modify the controller images
Edit output/deploy.yaml, update the `dpf-provisioning-controller` tag to `v0.2` (v0.2 is temporary version)

3. Run the following command to deploy the controller:
```bash
oc apply -f provisioning-output/deploy.yaml
```

#### Undeploy controller
1. Remove all the DpuSet CRs
2. Remove all the BFB CRs
3. Remove all the `br-dpu` bridge on host manually
- `oc get nncp` to check the NodeNetworkConfigurationPolicy on each worker node.
- `oc edit nncp NodeNetworkConfigurationPolicyName` change `spec.desiredState.interfaces.bridge.state` from `up` to `absent`. The br-dpu bridge on worker node should be removed.
- `oc delete nncp NodeNetworkConfigurationPolicyName`
4. Uninstall dpf controller
```
make undeploy
```
----

### Example of DPU provsioning
#### BFB yaml file example
```
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: Bfb
metadata:
  name: doca-24.04
  namespace: dpf-provisioning
spec:
  # fileName must use ".bfb" extension
  fileName: "doca-24.04.bfb"
  # the URL to download bfb file 
  url: "https://content.mellanox.com/BlueField/BFBs/Ubuntu22.04/bf-bundle-2.7.0-33_24.04_ubuntu-22.04_prod.bfb"
```

#### DPUFlavor yaml file example
```
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUFlavor
metadata:
  name: dpuflavor-example
  namespace: dpf-provisioning
spec:
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
      - hugepages=3072
  sysctl:
    parameters:
    - net.ipv4.ip_forward=1
    - net.ipv4.ip_forward_update_priority=0
  nvconfig:
    - device: "*"
      parameters:
        - PF_BAR2_ENABLE=0
        - PER_PF_NUM_SF=1
        - PF_TOTAL_SF=40
        - PF_SF_BAR_SIZE=10
        - NUM_PF_MSIX_VALID=0
        - PF_NUM_PF_MSIX_VALID=1
        - PF_NUM_PF_MSIX=228
        - INTERNAL_CPU_MODEL=1
        - SRIOV_EN=1
        - NUM_OF_VFS=30
        - LAG_RESOURCE_ALLOCATION=1
  ovs:
    rawConfigScript: |
      ovs-vsctl set Open_vSwitch . other_config:doca-init=true
      ovs-vsctl set Open_vSwitch . other_config:dpdk-max-memzones="50000"
      ovs-vsctl set Open_vSwitch . other_config:hw-offload="true"
      ovs-vsctl set Open_vSwitch . other_config:pmd-quiet-idle=true
      ovs-vsctl set Open_vSwitch . other_config:max-idle=20000
      ovs-vsctl set Open_vSwitch . other_config:max-revalidator=5000
  bfcfgParameters:
    - ubuntu_PASSWORD=$1$rvRv4qpw$mS6kYODr8oMxORt.TkiTB0
    - WITH_NIC_FW_UPDATE=yes
    - ENABLE_SFC_HBN=no
  configFiles:
  - path: /etc/bla/blabla.cfg
    operation: append
    raw: |
        CREATE_OVS_BRIDGES="no"
        CREATE_OVS_BRIDGES="no"
    permissions: "0755"
```

#### DpuSet yaml file example
```
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DpuSet
metadata:
  name: dpuset-demo
  namespace: dpf-provisioning
spec:
  nodeSelector:
    matchLabels:
      feature.node.kubernetes.io/dpu-enabled: "true"
  strategy:
    rollingUpdate:
      maxUnavailable: 10%
    type: RollingUpdate
  dpuTemplate:
    spec:
      dpuFlavor: "hbn" 
      BFB:
        bfb: "doca-24.04"
      nodeEffect:
        taint:
          key: "dpu"
          value: "provisioning"
          effect: NoSchedule
      automaticNodeReboot: true
      cluster:
        name: "tenant-00"
        namespace: "tenant-00-ns"
        nodeLabels:
          "dpf.node.dpu/role": "worker"
```

### Build image from source code

#### Build docker image
```
make docker-build-dpf-provisioning
```

#### Push docker image

```
docker login gitlab-master.nvidia.com:5005


make docker-push-dpf-provisioning
```

### Setup cluster level pull from local image registry to access NGC from the DPU
Pulling images from a local registry significantly reduces the load on the network, ensures compatibility with air-gapped environments, enhances security, speeds up deployment, and improves reliability and availability.
To configure containerd to pull from a local image registry, User should configure a registry on the k8s cluster. e.g.,
```
apiVersion: v1
kind: Service
metadata:
  name: registry
  namespace: dpf-provisioning
spec:
  selector:
    app: registry
  ports:
  - protocol: TCP
    port: 5000
    targetPort: 5000
  type: ClusterIP
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: registry
  namespace: dpf-provisioning
  labels:
    app: registry
spec:
  replicas: 1
  selector:
    matchLabels:
      app: registry
  template:
    metadata:
      labels:
        app: registry
    spec:
      imagePullSecrets:
      - name: dpf-docker
      containers:
      - name: registry
        image: registry:2
        ports:
        - containerPort: 5000
        volumeMounts:
        - name: config
          readOnly: true
          mountPath: "/etc/docker/registry/config.yml"
          subPath: config
      volumes:
      - name: config
        secret:
          secretName: registry-config
---
apiVersion: v1
kind: Secret
metadata:
  name: registry-config
  namespace: dpf-provisioning
stringData:
  config: |
    # Default config
    version: 0.1
    log:
      fields:
        service: registry
    storage:
      cache:
        blobdescriptor: inmemory
      filesystem:
        rootdirectory: /var/lib/registry
    http:
      addr: :5000
      headers:
        X-Content-Type-Options: [nosniff]
    health:
      storagedriver:
        enabled: true
        interval: 10s
        threshold: 3
    # Custom config
    proxy:
      remoteurl: https://nvcr.io
      username: $oauthtoken
      password: <REPLACE_WITH_TOKEN>
```
Notice that we have provided a token in the Secret which in the above example has the value of <REPLACE_WITH_TOKEN>. To acquire such a token, use the following links:
https://docs.nvidia.com/ngc/gpu-cloud/ngc-user-guide/index.html#generating-api-key
https://ngc.nvidia.com/setup

You need a docker hub account to pull images without having rate limit errors. You can create your personal account [here](https://hub.docker.com/signup). Once you have your account, you can use either your account password or a read-only access token as the value of the environment variable down below.

```bash
export DOCKER_HUB_NAME=YOUR_DOCKER_HUB_ACCOUNT_NAME
export DOCKER_HUB_PASSWORD=YOUR_DOCKER_HUB_ACCOUNT_PASSWORD_OR_TOKEN
oc create secret docker-registry dpf-docker -n dpf-provisioning --docker-server=docker.io --docker-username=${DOCKER_HUB_NAME} --docker-password=${DOCKER_HUB_PASSWORD}
```

#### Specify registry mirror endpoint in DPUFlavor yaml file
Once registry service is up and running, user should pass the service IP as a parameter to the DPUFlavor yaml file, to ensure the containerd configuration is set to pull from the local cache. Edit your DPUFlavor yaml file to have this lines:
```
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUFlavor
metadata:
  name: dpuflavor-example
  namespace: dpf-provisioning
spec:
  containerdConfig:
    registryEndpoint: http://<Registry_Service_ClusterIP>:<Registry_Service_Port>
...

```
