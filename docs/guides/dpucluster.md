## DPUCluster


The DPUCluster is a Kubernetes CRD which managed the control plane of a DPUCluster in DPF. The DPUCluster can be backed by different implementations.

Two implementations are included in this repo:
- [Kamaji cluster manager](../../cmd/kamaji-cluster-manager) which creates Kamaji TenantControlPlanes to back the DPUCluster.
- [Static cluster manager](../../cmd/static-cluster-manager) which transforms an existing Kubernetes control plane into a DPUCluster control plane.

### DPUCluster usage

A DPUCluster is a user API and the usage will differ depending on the implementation.

When using the static cluster manager the CR will look like:

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
  ## Version is the version of the Kubernetes control plane.
  version: v1.30.2
  ## Kubeconfig is the name of a secret in the same namespace as the DPUCluster object.
  ## Note: This field is supplied by the user in the static cluster manager - but this may not be the case for other implementations.
  kubeconfig: dpu-cluster-1-admin-kubeconfig
```

When using the Kamaji cluster manager the CR will look like:
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
  ## Version is the version of the Kubernetes control plane.
  version: v1.30.2
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
- only operate on a DPUCluster which has a `type` it is responsible for.
- be the only DPUCluster controller implementation in a cluster.
- provide an admin Kubeconfig to a functioning Kubernetes cluster as a Kubernetes Secret.
- ensure the name of that Secret is available in the `.spec.kubeconfig` of the DPUCluster object.

The Kubeconfig provided by the DPUCluster should have the following format:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: dpu-cluster-1
  namespace: dpf-operator-system
type: Opaque
data:
  admin.conf: $KUBECONFIG_DATA
```

This Kubeconfig is deserialized [in this package](https://gitlab-master.nvidia.com/doca-platform-foundation/doca-platform-foundation/blob/8cac7f383dc91f598311ab2e53b208132302b9b7/internal/dpucluster/dpucluster.go).
