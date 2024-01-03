DPF Operator MVP design
-----------------------

The DPF Operator is responsible for coordinating DPF initialization, deploying a set of system components to the OCP cluster and deploying a set of system components to the DPU cluster. It is a single controller which is responsible for managing the lifecycle of the following objects:

* DPFOperatorConfig
* Manifests for system components on the OCP cluster
* Manifests for system components on the DPU cluster

### Scope

This design covers the Operator and the steps it takes to initialise the DPF system. It is aligned to the April MVP scope and targets OpenShift clusters.

#### Out of scope

* Detailed configuration of components installed and managed by the DPF operator
* Recovery of a DPF system which fails during or after installation
* Extensive validation of user configuration
* Reverting an OCP cluster to a good state after initialising DPF

#### Prerequisites

The following is a set of pre-requisites that are out of scope for this design. Each of these must be satisfied for the DPF Operator to successfully reconcile the system.

* A dedicated clean OCP cluster.
* Network setup on the cluster e.g. ovn-kubernetes installed, correct topology.
* The following deployed on the cluster:
  * ArgoCD installed with a well-known configuration
  * Kamaji installed with a well-known configuration
  * A Kamaji TenantControlPlane with a well-known configuration
  * Connection provider for Kamaji control plane e.g. MetalLB Load balancer or Cluster IP.
  * Volume provider for Kamaji etc e.g. PersistentVolume
  * Volume provider supporting ReadOnlyMany and ReadWriteOnce for DPUProvisioning controller
  * Image pull secrets
  * Node Feature Discovery with configuration for DPU detection (NFD Operator)
  * NM Operator for creation of "host dpu bridge" with VF by DPF provisioning
  * Cert manager (cert-manager Operator for RH-OCP)
  * VF Provisioning tool - possibly SR-IOV Network Operator or other implementation

### API

The DPF Operator reconciles a DPFOperatorConfig object. This object contains only the information required for initialising the system. It must be unique and is confined to a single name and namespace.
```yaml
apiVersion: operator.dpu.nvidia.com/v1alpha1
kind: DPFOperatorConfig
metadata:
  name: dpfoperatorconfig
  namespace: dpf-operator-system
spec:
  imagePullSecrets: # Pull secrets for system components. Also mirrored to the DPUCluster.
  - ngc-secret-name
  - docker-secret-name
  bfbPersistentVolume: volume-one # Name of the persistent volume used by the DPU Provisioning controller.
  imageOverrides: # User provided set of images to be used for specific components.
    dpuservice: localhost:5000/my-image
  hostNetworkConfiguration: # User provided. Must be provided to enable ovn-kubernetes setup. Two IPs required per Kubernetes node.
    hostPF0: ens2f0np0
    hostPF0VF0: enp23s0f0v0
    CIDR: 10.0.96.0/20
    hosts:
    - hostClusterNodeName: ocp-worker-1
      dpuClusterNodeName: dpu-worker-1
      hostIP: 10.0.96.10/24
      dpuIP: 10.0.96.20/24
      gateway: 10.0.96.254
    - hostClusterNodeName: ocp-worker-2
      dpuClusterNodeName: dpu-worker-2
      hostIP: 10.0.97.10/24
      dpuIP: 10.0.97.20/24
      gateway: 10.0.97.254
```

### Controller flow

The DPF Operator is deployed from an operator bundle. The bundle contains CRDs for components the Operator deploys to the OCP cluster including DPFOperatorConfig, DPUService, DPUServiceInterface DPUServiceChain and DPUServiceIPAM.

#### 1. Reconcile deletion if needed

* Remove finalizer. Trigger ownerReference-based cleanup of all manifests on the OCP cluster and DPUServices on the DPU cluster deployed by the operator.
* The OCP cluster is in a broken state after deletion.

#### 2. Deploy DPF system components

##### OCP Cluster

For each of the following manifests - including associated configurations:

* DPUService controller
* DPUServiceFunctionChain controller manager
* DPU provisioning controller manager

The DPFOperator deploys the components and wait for each application Object - e.g. Deployment, Daemonset - to become ready. If not ready it requeues the request.

##### DPU Cluster

Deploy DPUServices containing the following components and their associated configurations:

* DPUServiceFunctionChainSet controller
* DPUServiceFunctionChain controller
* DPUServiceIPAMSet controller
* DPUServiceIPAM controller
* DPUServiceInterfaceSet controller
* DPUServiceInterface controller

Deploy them to the underlying cluster.

#### 3. Configure the network for the OCP cluster

For each node with a DPU - discovered from the NFD provided label. Reconcile the network for the node.

* Stop conflicting OCP components
* Scale down controllers impacting OCP network to 0 replicas
* cluster-version-operator
* Openshift cluster network-operator
* openshift-ovn-kubernetes
* Remove the nodeIdentity validatingWebhookConfiguration
* Edit ovn-kubernetes daemonset nodeselector to ensure it does not deploy on hosts with DPUs
* Update node metadata
* Add a taint to ensure no workloads are scheduled to the OCP node.
* Remove OVN node ID annotation from the node
* Deploy hostnetworkprovisioner as a DaemonSet
* Deploy dpucniprovisioner as a DPUService
* Deploy custom ovn-kubernetes daemonset with the correct node selector. Pods are not be deployed to OCP nodes until the taints are removed.
* Check that both hostnetworkprovisioner and dpucniprovisioner pods are ready for a given Node. If not requeue.
* Once both pods are ready remove the taint / add a label from the node.


### Validation

The DPF Operator validates the fields passed in the DPFOperatorConfig.

### Failures

The DPF Operator is not able to recover from serious failures. Intermittent issues such as network flakes are rereconciled but misconfiguration or other errors result in a broken installation of DPF and the OCP cluster will be left in an unknown state.
