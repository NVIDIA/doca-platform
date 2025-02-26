## Upgrading DPU Services

Notice:
Service chain modifications are not supported.

> Notice:
> This operation will result in a network
> disruption on each one of the nodes sequentially (a "rolling" update managed by the daemonset object).
> The update order of nodes cannot be controlled.
> There's an option to divide the cluster into "zones",
> each with its own DPU service, and perform the update per "zone" to control the disruption.
> Please refer to section "Dividing the cluster
> into several zones" for more information.

These are the required steps for upgrading DPU services:
1) Update the DPU service YAML in the DPF deployment environment to include the required version (In the case of the OVN DPU Service, you can modify the $DTS_VERSION variable), here's an example (with the DTS DPU Service):

```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUService
metadata:
  name: doca-telemetry-service
  namespace: dpf-operator-system
spec:
  helmChart:
    source:
      repoURL: https://helm.ngc.nvidia.com/nvidia/doca
      version: 0.2.3
      chart: doca-telemetry
```

2) Re-apply the YMAL, this will result in a redeployment of the DPU service:
  ```shell
  kubectl apply -f dts-dpuservice.yaml
  ```
## Dividing the cluster into several zones

For a better control of maintenance and down-time, the cluster can be logically divided into several "zones".
Each zone can have its own set of DPU services, that can be upgraded individually, affecting only the specific zone.
The creation of zones for DPU services is done by adding labels on the nodes in the DPU cluster and then using them with the DPU service YAML:


Create a specific DPU Set for worker nodes labeled as "e2e.servers/dk=true",
by adding the "cluster -> nodeLabels" section, assign their DPUs the label "bfb=dk" (on the DPU cluster).

```yaml
---
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUSet
metadata:
  name: dpuset-dk
  namespace: dpf-operator-system
spec:
  nodeSelector:
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
    automaticNodeReboot: true
    cluster:
    nodeLabels:
      bfb: "dk"
```
Then use the assigned label to create an HBN DPU Service for these specific nodes (under the "nodeSelector" section):

```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUService
metadata:
  name: doca-hbn-dk
  namespace: dpf-operator-system
spec:
  serviceID: doca-hbn
  interfaces:
  - p0-sf-dk
  - p1-sf-dk
  - app-sf-dk
  serviceDaemonSet:
  nodeSelector:
    nodeSelectorTerms:
    - matchExpressions:
    - key: "bfb"
        operator: In
      values: ["dk"]
  annotations:
    k8s.v1.cni.cncf.io/networks: |-
    [
    {"name": "iprequest", "interface": "ip_lo", "cni-args": {"poolNames": ["loopback"], "poolType": "cidrpool"}},
    {"name": "iprequest", "interface": "ip_pf2dpu3", "cni-args": {"poolNames": ["pool1"], "poolType": "cidrpool", "allocateDefaultGateway": true}}
        ]
  helmChart:
  source:
    repoURL: https://helm.ngc.nvidia.com/nvidia/doca
    version: 1.0.1
    chart: doca-hbn
  values:
    imagePullSecrets:
    - name: dpf-pull-secret
    image:
    repository: nvcr.io/nvidia/doca/doca_hbn
    tag: 2.4.1-doca2.9.1
    resources:
    memory: 6Gi
    nvidia.com/bf_sf: 3
    configuration:
    perDPUValuesYAML: |
    - hostnamePattern: "*"
      values:
        bgp_autonomous_system: 65111
        bgp_peer_group: hbn
    startupYAMLJ2: |
      - header:
        model: BLUEFIELD
        nvue-api-version: nvue_v1
        rev-id: 1.0
        version: HBN 2.4.0
      - set:
        interface:
        lo:
          ip:
          address:
            {{ ipaddresses.ip_lo.ip }}/32: {}
          type: loopback
        p0_if,p1_if:
          type: swp
          link:
          mtu: 9000
        pf2dpu3_if:
          ip:
          address:
            {{ ipaddresses.ip_pf2dpu3.cidr }}: {}
          type: swp
          link:
          mtu: 9000
        router:
        bgp:
          autonomous-system: {{ config.bgp_autonomous_system }}
          enable: on
          graceful-restart:
          mode: full
          router-id: {{ ipaddresses.ip_lo.ip }}
        vrf:
        default:
          router:
          bgp:
            address-family:
            ipv4-unicast:
              enable: on
              redistribute:
              connected:
                enable: on
            ipv6-unicast:
              enable: on
              redistribute:
              connected:
                enable: on
            enable: on
            neighbor:
            p0_if:
              peer-group: {{ config.bgp_peer_group }}
              type: unnumbered
            p1_if:
              peer-group: {{ config.bgp_peer_group }}
              type: unnumbered
            path-selection:
            multipath:
              aspath-ignore: on
            peer-group:
            {{ config.bgp_peer_group }}:
              remote-as: external
```

You can do the same for the additional required YAMLs (interfaces and chains):

```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceInterface
metadata:
  name: app-sf-dk
  namespace: dpf-operator-system
spec:
  template:
  spec:
    nodeSelector:
    matchLabels:
      bfb: "dk"
    template:
    metadata:
      labels:
      svc.dpu.nvidia.com/interface: "app_sf"
      svc.dpu.nvidia.com/service: doca-hbn
    spec:
      interfaceType: service
      service:
      serviceID: doca-hbn
      network: mybrhbn
      interfaceName: pf2dpu3_if
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceInterface
metadata:
  name: p0-sf-dk
  namespace: dpf-operator-system
spec:
  template:
  spec:
    nodeSelector:
    matchLabels:
      bfb: "dk"
    template:
    metadata:
      labels:
      svc.dpu.nvidia.com/interface: "p0_sf"
      svc.dpu.nvidia.com/service: doca-hbn
    spec:
      interfaceType: service
      service:
      serviceID: doca-hbn
      network: mybrhbn
      interfaceName: p0_if
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceInterface
metadata:
  name: p1-sf-dk
  namespace: dpf-operator-system
spec:
  template:
  spec:
    nodeSelector:
    matchLabels:
      bfb: "dk"
    template:
    metadata:
      labels:
      svc.dpu.nvidia.com/interface: "p1_sf"
      svc.dpu.nvidia.com/service: doca-hbn
    spec:
      interfaceType: service
      service:
      serviceID: doca-hbn
      network: mybrhbn
      interfaceName: p1_if
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceChain
metadata:
  name: hbn-to-fabric-dk
  namespace: dpf-operator-system
spec:
  template:
  spec:
    nodeSelector:
    matchLabels:
      bfb: "dk"
    template:
    spec:
      switches:
      - ports:
        - serviceInterface:
          matchLabels:
          uplink: p0
        - serviceInterface:
          matchLabels:
          svc.dpu.nvidia.com/service: doca-hbn
          svc.dpu.nvidia.com/interface: "p0_sf"
      - ports:
        - serviceInterface:
          matchLabels:
          uplink: p1
        - serviceInterface:
          matchLabels:
          svc.dpu.nvidia.com/service: doca-hbn
          svc.dpu.nvidia.com/interface: "p1_sf"
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceChain
metadata:
  name: ovn-to-hbn-dk
  namespace: dpf-operator-system
spec:
  template:
  spec:
    nodeSelector:
    matchLabels:
      bfb: "dk"
    template:
    spec:
      switches:
      - ports:
        - serviceInterface:
          matchLabels:
          svc.dpu.nvidia.com/service: doca-hbn
          svc.dpu.nvidia.com/interface: "app_sf"
        - serviceInterface:
          matchLabels:
          port: ovn
```
