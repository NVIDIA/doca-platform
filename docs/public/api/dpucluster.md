---
title: "DPUCluster"
---

[TOC]

The DPUCluster is a Kubernetes CRD which managed the control plane of a DPUCluster in DPF. The DPUCluster can be backed by different implementations.

Two implementations are included in this repo:

* Kamaji cluster manager which creates Kamaji TenantControlPlanes to back the DPUCluster.
* Static cluster manager which transforms an existing Kubernetes control plane into a DPUCluster control plane.

## DPUCluster usage

A DPUCluster is a user API and the usage will differ depending on the implementation.

#### Using the static cluster manager
The static cluster manager controller should be enabled first. It is enabled by adding staticClusterManager field in the DPUOperatorConfig CR:

```yaml
apiVersion: operator.dpu.nvidia.com/v1alpha1
kind: DPFOperatorConfig
metadata:
  name: dpfoperatorconfig
  namespace: dpf-operator-system
spec:
  provisioningController:
    bfbPVCName: "bfb-pvc"
  staticClusterManager: {}
```

Then create a secret for storing the kubeconfig of the existing Kubernetes control plane. For example, the kubeconfig is under the home directory:

```shell
TENANT_KUBE_CONFIG=`cat ~/.kube/config | base64 -w 0`

cat <<EOF | kubectl apply -f -
apiVersion: v1
data:
  super-admin.conf: ${TENANT_KUBE_CONFIG}
kind: Secret
metadata:
  name: dpu-cluster-1-admin-kubeconfig
  namespace: dpf-operator-system
type: Opaque
EOF
```

the DPUCluster will look like:

```yaml
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUCluster
metadata:
  name: dpu-cluster-1
  namespace: dpf-operator-system
spec:
  ## type signals which controller implementation should take responsibility for the DPUCluster.
  type: static
  ## Max nodes is the maximum number of nodes supported by the DPUCluster implementation.
  maxNodes: 10
  ## Kubeconfig is the name of a secret in the same namespace as the DPUCluster object.
  ## Note: This field is supplied by the user in the static cluster manager - but this may not be the case for other implementations.
  kubeconfig: dpu-cluster-1-admin-kubeconfig
```

#### Using the Kamaji cluster manager
The DPUCluster will look like:
```yaml
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUCluster
metadata:
  name: dpu-cluster-1
  namespace: dpf-operator-system
spec:
  ## type signals which controller implementation should take responsibility for the DPUCluster.
  type: kamaji
  ## Max nodes is the maximum number of nodes supported by the DPUCluster implementation.
  maxNodes: 10
  ## Cluster endpoint is supplied by the user and provides and IP and other details to make the APIServer available. 
  clusterEndpoint:
    # deploy keepalived instances on the nodes that match the given nodeSelector.
    keepalived:
      # interface on which keepalived will listen. Should be the oob interface of the control plane node.
      interface: interface_one
      # vip is the Virtual IP reserved for the DPU Cluster load balancer. Must not be allocatable by DHCP.
      vip: dpucluster_vip
      # virtualRouterID must be in range [1,255], make sure the given virtualRouterID does not duplicate with any existing keepalived process running on the host
      virtualRouterID: 126
      # nodeSelector selects which nodes the keepalived pods will be scheduled to.
      nodeSelector:
        node-role.kubernetes.io/control-plane: ""
```

### DPUCluster implementation


A DPUCluster implementation is a Kubernetes controller which operates on the DPF DPUCluster object. It should:

* only operate on a DPUCluster which has a `type` it is responsible for.
* be the only DPUCluster controller implementation in a cluster.
* provide an admin Kubeconfig to a functioning Kubernetes cluster as a Kubernetes Secret.
* ensure the name of that Secret is available in the `.spec.kubeconfig` of the DPUCluster object.

The Kubeconfig provided by the DPUCluster should have the following format:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: dpu-cluster-1
  namespace: dpf-operator-system
type: Opaque
data:
  super-admin.conf: $KUBECONFIG_DATA
```
