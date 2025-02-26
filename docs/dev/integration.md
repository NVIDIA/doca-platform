## DOCA Platform Foundation

Independent software vendors (ISVs) can integrate DOCA Platform Foundation (DPF). Integration may involve modifying or replacing dependencies and components used in the upstream version of DPF.

The below details the system highlighting which components can be changed in integration.

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

## DPF operator

The DPF Operator has a number of dependencies which can be swapped out during integration in favor of solutions managed by the integrator. Replacing some of these components may require additional modification - i.e. certificate management with a tool other than cert manager may require changes to the deployments.

Today the following are either mentioned in the installation guide or installed as dependencies from the DPF Operator helm chart.

- certificate management using `cert-manager`
- storage provisioning using `NFS` and `local-path` provisioner
- NVIDIA Network Operator
- SR-IOV Network Operator
- NVIDIA Node maintenance operator.

## Host networking configuration

DPF relies on each worker node having a specific networking configuration. In this configuration the worker node management interface is plugged into a bridge called `br-dpu`. Virtual functions from the BlueField DPU are added to the same bridge.


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
style E fill:#ADD8E6,stroke:#FFF,stroke-width:2px

    A[Management Network]
    subgraph Host
        B[OOB uplink]
        E[br-dpu]
        C[DPU VF0]
    end
    
    subgraph DPU
        X[DPU VF0 representor]
    end

    A-->B
    B-->E
    E-->C
    C-->X
```

This networking pattern must be implemented when integrating DPF. There is no component in DPF which configures this. For more information on the implementation details see the [host networking prerequisites](../guides/usecases/host-network-configuration-prerequisite.md).

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
style F fill:#76B900,stroke:#FFF,stroke-width:2px
style C fill:#FFA500,stroke:#FFF,stroke-width:2px
style E fill:#ADD8E6,stroke:#FFF,stroke-width:2px


    subgraph 1[Host control plane]
        A[DPU<br>control plane]
        D[DPU<br>controller]
        C[DPUCluster<br>controller]
        E[Kamaji]

    end

    subgraph 2[Host]
        3-.-|br-dpu|4[rshim]
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
    C -->|Create|A
```

The following components are used in upstream DPF and can be modified or replaced during integration:
- DPU Cluster manager- highlighted in orange - can be replaced in full in integration.
- Kamaji controller - a dependency of the DPUCluster manager - can be replaced.

For more information on implementing a DPUCluster manager see [the DPUCluster documentation](../guides/dpucluster.md).

## DPUService system

Orchestrate DOCA Services to run on DPUs.

The following components are used in upstream DPF and can be modified or replaced during integration:
- ArgoCD - a dependency of the DPUService controller manager can be replaced with a managed version of ArgoCD

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
style B fill:#ADD8E6,stroke:#FFF,stroke-width:2px

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

None of the components used in Service chain orchestration should be replaced during integration.
