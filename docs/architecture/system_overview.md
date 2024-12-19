## DOCA Platform Framework


A system for provisioning and managing DPUs and the DOCA services that run on them. Enable DPUs and DOCA Services deployment and management at scale.

```mermaid
%%{
  init: {
      'theme': 'base',
    'themeVariables': {
      'primaryColor': 'lightgray',
      'primaryTextColor': 'black',
      'primaryBorderColor': '#7C0000',
      'lineColor': 'black',
      'background':	'#76B900',
      'tertiaryColor': '#FFFFED'
    },
    "flowchart": {"wrap": true,"nodePadding": 1}
  }
}%%




graph TD
    style B fill:#76B900,stroke:#FFF,stroke-width:2px
    style C fill:#76B900,stroke:#FFF,stroke-width:2px
    style E fill:#76B900,stroke:#FFF,stroke-width:2px
    style F fill:#76B900,stroke:#FFF,stroke-width:2px
    style G fill:#76B900,stroke:#FFF,stroke-width:2px
    subgraph Host Cluster
        B[DPF Operator]
    end

    subgraph DPUCluster
        C[DPU<br>control plane]
    end

    subgraph DPUNode
        H[DPUService]
    end

    B -->E[Provisioning system]
    B --> F[DPUService system]
    B --> G[DPUServiceChain system]

    E -->| Node lifecycle |DPUNode
    E -->|Control plane lifecycle|C
    F -->|Service lifecycle|H
    G -->|ServiceChain lifecycle|H
```

---

## Provisioning system
Manage the lifecycle of the DPU hardware and its Kubernetes orchestration system.
```mermaid

%%{
  init: {
      'theme': 'base',
    'themeVariables': {
      'primaryColor': 'lightgray',
      'primaryTextColor': 'black',
      'primaryBorderColor': '#7C0000',
      'lineColor': 'black',
      'background':	'#76B900',
      'tertiaryColor': '#FFFFED'
    },
    "flowchart": {"wrap": true,"nodePadding": 1}
  }
}%%

graph LR
style D fill:#76B900,stroke:#FFF,stroke-width:2px


    subgraph 1[Host control plane]
        A[DPU<br>control plane]
        D[DPU<br>controller]
    end

    subgraph 2[Host]
        3-.-4[rshim]
    end
    
    subgraph 3[Host Node]
        Kubelet
        F[DOCA<br>Management<br>Service]
    end

    subgraph 4[DPU]
        d[Kubelet]
    end

    F -->|Flash BFB| 4
    D -->|Deploy| F
    D -->|Send provisioning commands| F
    d -->|Join|A
```

1. User creates a `DPUSet` and `BFB` object representing a set of DPUs they want to run services on.
2. DPUSet controller creates a `DPU` object based on the DPUSet NodeSelector.
3. DPU controller deploys DMS and communicates the BFB and other configuration to it.
4. DMS installs BFB to the DPU over rshim.
5. The DPU node joins the DPU Control plane .

---



## DPUService system
Orchestrate DOCA Services to run on DPUs.
```mermaid
%%{
  init: {
    'theme': 'base',
    'themeVariables': {
      'primaryColor': 'lightgray',
      'primaryTextColor': 'black',
      'primaryBorderColor': '#7C0000',
      'lineColor': 'black',
      'background':	'#76B900',
      'tertiaryColor': '#FFFFED'
    }
  }
}%%

graph LR
style D fill:#76B900,stroke:#FFF,stroke-width:2px

    subgraph 1[Host control plane]
        A[DPU control plane]
        D[DPUServiceController]
        B[ArgoCD system]
    end

    subgraph 2[Host]
        3-.-4[rshim]
    end
    
    subgraph 3[Host Node]
        Kubelet
    end

    subgraph 4[DPU]
        d[Kubelet]
    end
    D -->|Create Application|B
    B -->|Apply Kubernetes objects|A
    A-->|Schedule Pod| d
```
1. User creates a `DPUService` referencing a helm chart.
2. DPUService controller creates an ArgoCD Application representing the DPUService.
3. ArgoCD system syncs the Application to the DPU Cluster.
4. A Pod for the `DPUService` runs on the DPU Node.

---


## DPUServiceChain system
Orchestrate Service Chains for advanced network flows through DPUs.
```mermaid
%%{
  init: {
    'theme': 'base',
    'themeVariables': {
      'primaryColor': 'lightgray',
      'primaryTextColor': 'black',
      'primaryBorderColor': '#7C0000',
      'lineColor': 'black',
      'background':	'#76B900',
      'tertiaryColor': '#FFFFED'
    }
  }
}%%

graph LR
style D fill:#76B900,stroke:#FFF,stroke-width:2px
style B fill:#76B900,stroke:#FFF,stroke-width:2px
style e fill:#76B900,stroke:#FFF,stroke-width:2px

    subgraph 1[Host control plane]
        A[DPU control plane]
        D[DPUServiceController]
        B[DPUServiceChain controllers]
    end

    subgraph 2[Host]
        3~~~4
    end
    
    subgraph 3[Host Node]
        Kubelet
    end

    subgraph 4[DPU]
        d[Kubelet]
        e[SFC CNI]
    end
    D -->|Create DPUServiceChain objects|B
    B -->|Create Kubernetes objects|A
    A-->|Create Node ServiceChain objects| d
    e-->|Update OVS ports and flows|e
```
1. User creates `DPUServiceInterface`, `DPUServiceChain`, `DPUServiceIPAM` representing ovs ports, ovs flows and CNI IPAM.
2. Service Function Chain controllers sync these objects to the DPUCluster.
3. ServiceInterface controller creates OVS ports for the Service Function Chain
4. ServiceChain controller creates OVS flows for the Service Function Chain
5. NVIPAM allocates an IP Pool for the Service Function Chain
6. User creates a DPUService for the ServiceFunctionChain

---


## OVN Kubernetes DPUService
Offload primary and secondary network to the DPU

```mermaid
%%{
  init: {
    'theme': 'base',
    'themeVariables': {
      'primaryColor': 'lightgray',
      'primaryTextColor': 'black',
      'primaryBorderColor': '#7C0000',
      'lineColor': 'black',
      'background':	'#76B900',
      'tertiaryColor': '#FFFFED'
    }
  }
}%%

graph LR
style D fill:#76B900,stroke:#FFF,stroke-width:2px
style l fill:#76B900,stroke:#FFF,stroke-width:2px
style z fill:#76B900,stroke:#FFF,stroke-width:2px

    subgraph 1[Host control plane]
        A[DPU control plane]
        D[DPUServiceController]
        B[ArgoCD system]
    end

    subgraph 2[Host]
        3~~~4
    end
    
    subgraph 3[Host Node]
        Kubelet
        p[Workload Pod]
        l[OVN Kubernetes]
        
    end

    subgraph 4[DPU]
        d[Kubelet]
        z[OVN Kubernetes]
        l-->|Networking offload|z
        p---|Virtual Function|4
    end

    D -->|Application|B
    B -->|Objects|A
    A-->|OVN Kubernetes| 4
```
1. User deploys OVN Kubernetes as the primary CNI
2. User creates OVN Kubernetes `DPUService`
3. DPUService controller and ArgoCD sync the OVNKubernetes DPUService to the DPUNode
4. OVN is the primary network which links workload pods with the ServiceFunctionChains operating on the DPU node
5. All pods get a VF from the Bluefield which is used for the primary network
6. OVN processing offloaded to DPU
7. Other network functions - like an L3 firewall using HBN - can be added to the service chain for a pod.