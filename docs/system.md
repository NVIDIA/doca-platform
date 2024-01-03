# DOCA Platform Framework

DOCA Platform Framework (DPF) manages NVIDIA DPUs and services running on them in a Kubernetes cluster. A second Kubernetes cluster - called a DPUCluster - manages the lifecycle of services running on the DPUs. This is a high-level overview of the components and flows of the DPF system. For more detailed information on setup and usage - including dependencies and prerequisites - see the installation guide.

<!-- toc -->
- [DPF system components](#dpf-system-components)
- [Provisioning components](#provisioning-components)
    - [In the host cluster control plane](#in-the-host-cluster-control-plane)
    - [On each node in the host cluster](#on-each-node-in-the-host-cluster)
- [DPUService components](#dpuservice-components)
    - [In the host cluster control plane](#in-the-host-cluster-control-plane-1)
- [DPUServiceChain components](#dpuservicechain-components)
    - [In the host cluster control plane](#in-the-host-cluster-control-plane-2)
    - [In the DPU cluster control plane](#in-the-dpu-cluster-control-plane)
    - [On each node in the DPU cluster](#on-each-node-in-the-dpu-cluster)
- [Provisioning user flows](#provisioning-user-flows)
  - [Create a DPU Cluster](#create-a-dpu-cluster)
  - [Provision a DPU](#provision-a-dpu)
  - [Update a DPU](#update-a-dpu)
  - [Delete a DPU](#delete-a-dpu)
- [DPUService flow](#dpuservice-flow)
  - [Create a DPUServiceCredentialRequest](#create-a-dpuservicecredentialrequest)
  - [Create DPUService](#create-dpuservice)
  - [Update DPUService](#update-dpuservice)
  - [Delete DPUServices](#delete-dpuservices)
<!-- /toc -->

## DPF system components
DPF is made up of the following sets of components:
- DPF Operator to install and configure the DPF system.
- Provisioning components to manage the lifecycle of the DPUs including OS installation, configuration and Kubernetes Node creation.
- DPUService components to manage the full lifecycle of services running on DPUs.
- DPUServiceChain components to manage the lifecycle of ServiceFunctionChains on DPUs.

```mermaid
%%{init: {'theme':'dark'}}%%
graph TD
subgraph Host Cluster
DPF_Operator[DPF Operator]

        subgraph Provisioning
            DPU_Controller[DPU controller]
            BFB_Controller[BFB controller]
            DPUSet_Controller[DPUSet controller]
            DPUCluster_Controller[DPUCluster controller]
            Kamaji[Kamaji]
            DOCA_Management[DMS]
            Hostnetwork_Config[HostnetworkConfig]
            DPU_Feature_Discovery[DPU feature discovery]
        end

        subgraph DPUService
            DPUService_Controller[DPUService controller]
            DPUDeployment_Controller[DPUDeployment controller]
            DPUServiceCredential_Controller[DPUServiceCredential controller]
            ArgoCD[ArgoCD]
        end

        subgraph DPUServiceChain
            DPUServiceInterface_Controller[DPUServiceInterface controller]
            DPUServiceIPAM_Controller[DPUServiceIPAM controller]
            DPUServiceChain_Controller[DPUServiceChain controller]
        end
    end

    subgraph DPUCluster
        NVIPAM[NVIPAM]
        Multus[Multus]
        SR-IOV_Device_Plugin[SR-IOV Device Plugin]
        ServiceChainSet_Controller[ServiceChainSet controller]
        ServiceInterfaceSet_Controller[ServiceInterfaceSet controller]
        ServiceInterface_Controller[ServiceInterface controller]
        ServiceFunctionChain_CNI[ServiceFunctionChain CNI]
    end

    DPF_Operator ---> Provisioning
    DPF_Operator --> DPUService
    DPF_Operator ----> DPUServiceChain
    DPUServiceChain --> DPUCluster
```

## Provisioning components
This component set uses the Kamaji Cluster Manager - but other Cluster managers may be used.
#### In the host cluster control plane
* Kamaji system components - including the DPF Kamaji Cluster manager
  * Manage the lifecycle of a Kamaji pod-based control plane
  * Manage the Kamaji cluster load balancer
  * Communicate with the host control plane and DPU control plane
* BFB controller
  * Download the Bluefield Bitstream (BFB) from a remote server
  * Communicate with the host control plane and remote BFB server
* DPUSet controller
  * Create DPU objects and manage their lifecycle
  * Select a Kubernetes control plane for DPU Cluster nodes to join
  * Communicate with the host control plane
* DPU controller
  * Flash the Bluefield Bitstream to the DPU
  * Communicate with the DOCA Management Service

#### On each node in the host cluster
* Node feature discovery - including DPU Detector
    * Add information about DPUs to the Kubernetes Node representing the host node
    * Communicate with the host control plane and host node filesystem
* DOCA management service
  * Flash a BFB to DPU hardware
  * Communicate with the BlueField DPU and DPU Controller
* Hostnetwork configuration
  * Configure up Virtual Functionss, bridges and routes for Host to DPU communication
  * Communicate with the host node through CLI calls

## DPUService components
#### In the host cluster control plane
* DPUService controller
  * Manage the lifecycle of DPUServices created by users
  * Manage the lifecycle of ArgoCD Applications linked to DPUServices
  * Communicate with the host control plane
* DPUDeployment controller
  * Manage the lifecycle of a group of DPUServices and DPUSets
  * Communicate with the host control plane
* DPUServiceCredential controller
  * Manage authorization and authentication for communication between the host control plane and DPU control plane
  * Communicate with the host control plane and DPU control plane
* ArgoCD system components
  * Manage lifecycle of helm charts on the DPU Cluster
  * Communicate with the host control plane and DPU control plane

## DPUServiceChain components
#### In the host cluster control plane
* DPUServiceInterface controller
  * Manage the lifecycle of DPUServiceInterfaces created by users
  * Communicate with the host control plane and DPU control plane
* DPUServiceIPAM controller
  * Manage the lifecycle of the DPUServiceIPAM created by users
  * Communicate with the host control plane and DPU control plane
* DPUServiceChain controller
  * Manage the lifecycle of the DPUServiceChain created by users
  * Communicate with the host control plane and DPU control plane
#### In the DPU cluster control plane
* NVIPAM
  * Manage allocation of IPs in the DPU cluster
  * Communicate with the DPU control plane
* ServiceChainSet controller
  * Manage the lifecycle of ServiceChainSets on the DPUCluster
  * Create ServiceChain objects for relevant DPU nodes
  * Communicate with the DPU control plane
* ServiceInterfaceSet controller
  * Manage the lifecycle of ServiceInterfaceSets on the DPUCluster
  * Create ServiceInterface objects for relevant DPU nodes
  * Communicate with the DPU control plane
#### On each node in the DPU cluster
* ServiceInterface controller
    * Creates ovs ports on DPU based on ServiceInterface objects
    * Communicate withe the DPU control plane and host OVS
* ServiceChain controller
  * Create ovs flows on DPU based on ServiceChain objects
  * Communicate with the DPU control plane and DPU host OVS
* ServiceFunctionChain CNI
  * Adds ovs network interfaces to pods
  * Communicate with Container Runtime Interface and host OVS
* NVIPAM
  * Allocate IPs for pods on the DPU node
  * Communicate with the DPU control plane and host OS
* Multus
  * Allocate network devices for pods on DPU nodes
  * Communicate with the DPU control plane, Container Runtime Ibnterface and host OS
* SR-IOV Device Plugin
  * Manage the lifecycle of Virtual Functions on the DPU node
  * Communicate with the host OS and Kubelet

## Provisioning user flows

DPF provisioning has four principle user flows.

### Create a DPU Cluster

```mermaid
sequenceDiagram
    participant User
    participant DPUCluster Manager
    participant Kamaji Controllers
    participant Load Balancer

    User->>DPUCluster Manager: Create DPUCluster object
    DPUCluster Manager->>Kamaji Controllers: Create TenantControlPlane
    Kamaji Controllers->>Kamaji Controllers: Create control plane pods
    DPUCluster Manager->>Load Balancer: Create load balancer for Kamaji control plane
    DPUCluster Manager->>User: Update DPUCluster with kubeconfig
```

This is a prerequisite to provision a DPU with DPF. This flow is based on the Kamaji Cluster Manager - but other Cluster managers may be used.
1) The user creates a DPUCluster object.
2) The DPUCluster manager creates an underlying Kamaji TenantControlPlane.
3) The Kamaji controllers create the cluster control plane pods.
4) The DPUCluster manager creates a load balancer for the Kamaji control plane.
5) The DPUCluster manager updates the DPUCluster with a kubeconfig for the Kamaji control plane. 

### Provision a DPU

```mermaid
sequenceDiagram
    participant User
    participant Node Feature Discovery
    participant BFB Controller
    participant DPUSet Controller
    participant DPU Controller
    participant DMS
    participant Host Network Configuration Daemon
    participant Kubeadm

    Node Feature Discovery->>Kubernetes Nodes: Label nodes with DPU information
    User->>BFB Controller: Create BFB object
    BFB Controller->>BFB Controller: Download BFB from URL
    User->>DPUSet Controller: Create DPUFlavor object (if not default)
    User->>DPUSet Controller: Create DPUSet object (references BFB and DPUFlavor)
    DPUSet Controller->>DPUSet Controller: Create DPU object based on host node labels
    DPU Controller->>Target Node: Deploy DMS pod
    DPU Controller->>DMS: Instruct to install and configure BFB on target DPU
    DPU Controller->>DMS: Instruct to reboot DPU node
    DPU Controller->>DMS: Instruct to reboot Host node (based on policy)
    DPU Controller->>Target Node: Deploy Host Network Configuration  pod
    Host Network Configuration Daemon->>Host: Ensure VFs are created for BlueField
    Host Network Configuration Daemon->>Host: Create bridge for network communication to target DPU
    Kubeadm->>DPU: Initialize Kubernetes Node and join DPUCluster
```

1) Node Feature Discovery labels Kubernetes nodes with DPU information.
2) The user creates a BFB object.
3) The BFB controller downloads the BFB from a URL.
4) The user creates a DPUFlavor object if not using a default.
5) The user creates a DPUSet object which references both the BFB and the DPUFlavor.
6) The DPUSet controller creates a DPU object based on host node labels.
7) The DPU controller deploys DMS pod to a target node.
8) The DPU controller instructs DMS to install and configure the BFB on a target DPU.
9) The DPU controller instructs DMS to reboot the DPU node.
10) The DPU controller instructs DMS to reboot the Host node based on reboot policy.
11) The DPU controller deploys the Host Network Configuration pod on a target node.
12) The Host Network Configuration daemon ensures VFs are created on the host for the BlueField.
13) The Host Network Configuration daemon creates a bridge for network communication to the target DPU..
14) Kubeadm initializes a Kubernetes Node on the DPU and joins the DPUCluster.

### Update a DPU

```mermaid
sequenceDiagram
    participant User
    participant DPUSet Controller
    participant DPU Controller

    User->>DPUSet Controller: Update DPUSet (alter BFB or DPUFlavor)
    DPUSet Controller->>DPU Controller: Delete target DPU object (based on update policy)
    DPU Controller->>DPU Kubernetes Node: Delete DPU Kubernetes node (following "Delete a DPU")
    DPUSet Controller->>DPUSet Controller: Create new DPU object for target DPU
    DPUSet Controller->>DPU Controller: Follow "Provision a DPU" process
```

1) User updates the DPUSet altering its the BFB or DPUFlavor.
2) DPUSet controller deletes the target DPU object based on update policy rules.
3) DPU controller deletes the DPU Kubernetes node following `Delete a DPU`.
4) DPUSet controller creates a new DPU object for the target DPU.
5) The process in `Provision a DPU` is followed to create a new DPU node with the updated DPU system.

### Delete a DPU

```mermaid
sequenceDiagram
    participant User
    participant DPUSet Controller
    participant DPU Controller

    User->>DPUSet Controller: Delete DPUSet or alter selector
    DPUSet Controller->>DPU Controller: Delete target DPU
    DPU Controller->>Target Node: Delete DMS and Host Network Configuration static pods
    DPU Controller->>DPU Kubernetes Node: Delete DPU Kubernetes node
```

1) User deletes the DPUSet or alters the selector to reduce the number of DPU objects.
2) DPUSet controller deletes target DPU.
3) DPU controller deletes the DMS and Host Network Configuration static pods.
4) DPU controller deletes the DPU Kubernetes node.

## DPUService flow
DPUService orchestration has four principle user flows.

### Create a DPUServiceCredentialRequest

```mermaid
sequenceDiagram
    participant User
    participant DPUServiceCredentialRequest Controller
    participant Target Cluster

    User->>DPUServiceCredentialRequest Controller: Create DPUServiceCredentialRequest
    DPUServiceCredentialRequest Controller->>Target Cluster: Create Service Account
    DPUServiceCredentialRequest Controller->>Target Cluster: Create Secret (kubeconfig/token)
```

This is a prerequisite for DPUServices that are deployed in the host control plane but communicate with the DPU control plane and vice versa. Users must modify their DPUService to use the secret created by the DPUServiceCredentialRequest.
1) User creates a DPUServiceCredentialRequest
2) DPUServiceCredentialRequest controller creates a service account in the target cluster.
3) DPUServiceCredentialRequest controller creates a secret with either a kubeconfig or token for the service account.

### Create DPUService

```mermaid
sequenceDiagram
    participant User
    participant DPUService Controller
    participant ArgoCD System

    User->>DPUService Controller: Create DPUService object (references helm chart)
    DPUService Controller->>DPUCluster: Create imagePullSecrets and namespaces
    DPUService Controller->>ArgoCD System: Create ArgoCD application (with helm chart and values)
    ArgoCD System->>DPUCluster: Reconcile ArgoCD application and install helm chart
```

1) User creates a DPUService object referencing a helm chart in a repository.
2) DPUService controller creates associated imagePullSecrets and namespaces in the DPUCluster.
3) DPUService controller creates an ArgoCD application with the DPUService helm chart and values.
4) ArgoCD system reconciles the ArgoCD application and installs the helm chart to the DPU cluster.

### Update DPUService

```mermaid
sequenceDiagram
    participant User
    participant DPUService Controller
    participant ArgoCD System

    User->>DPUService Controller: Update DPUService object
    DPUService Controller->>ArgoCD System: Update ArgoCD application
    ArgoCD System->>DPUCluster: Sync updated application to DPU cluster
```

1) User updates the DPUService object.
2) DPUService controller updates the ArgoCD application.
3) ArgoCD system components sync the updated application to the DPU cluster.

### Delete DPUServices

```mermaid
sequenceDiagram
    participant User
    participant DPUService Controller
    participant ArgoCD Controller

    User->>DPUService Controller: Delete DPUService object
    DPUService Controller->>ArgoCD Controller: Delete ArgoCD application
    ArgoCD Controller->>DPUCluster: Delete DPUService helm chart
```

1) User deletes the DPUService object.
2) DPUService controller deletes the ArgoCD application.
3) ArgoCD controller deletes the DPUService helm chart in the DPU cluster.


TODO: DPUDeployment flow

TODO: DPUServiceInterface flow