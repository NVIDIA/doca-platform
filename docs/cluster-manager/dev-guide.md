This guide helps the developers to:
* develope the dpf-standalone 
* run tests in a dpf-standalone environment or other test environments

As DPF is being actively developed, this doc might be out of date at any time, let me know if you need my help. 

# Key changes
## A DPUCluster CR is mandatory for DPU provisioning
Before Oct Rel, creating a kamaji `TenantControlPlane` CR is all you need to create a DPU cluster, the `provisioning-controller` recognises the created cluster by reading a Secret managed by kamaji.

In Oct Rel, the `provisioning-controller` finds DPU clusters by watching the `DPUCluster` CR, it no longer interacts with kamaji directly. To provision a DPU, you must create a `DPUCluster` CR and deploy either the [kamaji-cluster-manger](../../cmd/kamaji-cluster-manager) or the [static-cluster-manager](../../cmd/static-cluster-manager). The kamaji-cluster-manager is preferred in an e2e test, because it includes import FRs like [FR #3825824](https://redmine.mellanox.com/issues/3825824) and [FR #3971787](https://redmine.mellanox.com/issues/3971787).

# Create DPUCluster CR
## Option 1: Use the kamaji-cluster-manager to manage the DPU cluster
kamaji-cluster-manager creates a DPU cluster according to the `DPUCluster` CR.

```yaml
apiVersion: provisioning.dpf.nvidia.com/v1alpha1
kind: DPUCluster
metadata:
  name: test
spec:
  type: kamaji 
  maxNodes: 1000
  version: v1.29.0
  clusterEndpoint:
    # deploy keepalived instances on the nodes that match the given nodeSelector. 
    keepalived:
      # for dpf-standalone env, you can set interface to br-dpu 
      # for non-standalone env, set interface to the uplink interface 
      interface: br-dpu
      # for dpf-standalone env, you can set vip to any value
      # for non-standalone env, set vip to a DPU reachable IP. You may need help from the devops team to get a proper IP
      vip: "10.66.66.66"
      # virtualRouterID must be in range [1,255], make sure the given virtualRouterID does not duplicate with any existing keepalived process running on the host
      virtualRouterID: 126
      # nodeSelector specifies where the keepalived daemonset is deployed
      # for dpf-standalone env, the default value for key "node-role.kubernetes.io/master" is "true". For other distributiosn, it might be ""
      nodeSelector:
        node-role.kubernetes.io/control-plane: "true"
```

## Option 2: Use the static-cluster-manager to manage the DPU cluster
static-cluster-manager does not create DPU cluster for you, so you are on your own to create a DPU cluster. After you create a cluster, store the admin kubeconfig in a secret like the following
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: my-admin-kubeconfig
data:
  # the key must be "admin.conf"
  admin.conf: xxxxxx
type: Opaque
```

```yaml
apiVersion: provisioning.dpf.nvidia.com/v1alpha1
kind: DPUCluster
metadata:
  name: test
spec:
  type: static 
  maxNodes: 1000
  version: v1.29.0
  # kubeconfig is the name of the secret that contains a admin kubeconfig to the DPU cluster
  kubeconfig: my-admin-kubeconfig 
```

# Check The Status of a DPUCluster CR
```bash
# kubectl get dpucluster
NAME   TYPE     MAXNODES    VERSION     PHASE
test   kamaji 1000        v1.29.0     Ready
```
Once the `PHASE` is `Ready`, it's good to go.

# References
* [cluster manager LLD](https://docs.google.com/document/d/1Kv4B02Y1NqiJ0OB_Ut8v2doCRhjPcIxx9BeQ7Axe0wg/edit?usp=sharing)