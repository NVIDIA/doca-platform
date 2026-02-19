---
title: "Scale Testing"
---

[TOC]

This document describes a test design for assessing the DPF core components at scale. It mocks a number of parts of the DPF system to enable performance testing of the core DPF components in response to growth in specific dimensions of scale.

### Testing components

The major differences between a full DPF installation and the scale testing infrastructure are:

**1. No DPU hardware**: The scale test does not use Bluefield DPUs. Interactions with the DPU are implemented on the API level.

**2. No DPU Kubernetes nodes**: The scale test does not provision Kubernetes nodes

**3. No DMS**: DPF uses DMS to manage the lifecycle of the DPU. Scale testing relies on a `mock-dms` component which implements the API expected by the DPU controller.

**4. No hostnetwork configuration**: DPF uses a hostnetwork pod to configure networking on the host.

The scale test requires a new component - `mock-dms`. `mock-dms` is a Kubernetes controller that:

* Watches DPU objects
* Creates a mock DMS listener on a new port for each DPU
* Adds an annotation to DPUs overriding the DMS address, DMS pod, and hostnetwork pod
* Answers gRPC calls from the DPU controller
* Creates a Kubernetes node object representing the DPU node

### Testing Dimensions

The initial scale targets for the test are shown in the table below. Testing will be an iterative process and these targets will be updated on in response to test results.

| Object                       | Scale target |
|------------------------------|:-------------|
| DPUs                         | 1000         |
| DPUServices                  | 10           |
| DPUServiceChains             | 30           |
| DPUServiceIPAMs              | 30           |
| DPUServiceInterfaces         | 30           |
| DPUSets                      | 10           |
| DPUDeployments               | 10           |
| BFBs                         | 10           |
| DPUServiceCredentialRequests | 10           |
| DPUClusters                  | 1            |
| DPFOperatorConfigs           | 1            |

### Testing Targets

The scale tests rely on [DPF metrics](../../../advanced-configuration/observability-guide.md) to assess the performance of the components.

The following categories of metrics are of interest. The testing process is iterative and these targets will be further specified and updated in response to test results.

1. time to provision target number of DPU nodes 
2. time to provision target number of DPUServices
3. time to provision target number of DPUServiceInterfaces
4. time to provision target number of DPUServiceChains
5. time to provision target number of DPUServiceIPAMs
6. number of errors in DPF controllers
7. number of errors in DPU cluster control plane
8. number of errors in target cluster control plane
9. reconcile time for DPF controllers
10. CPU / memory usage DPU cluster control plane
11. CPU / memory usage target cluster control plane
12. CPU / memory usage DPF controllers

### Gaps

This scale testing approach does not adequately test the following at scale:

* DPUCluster components and management network - i.e. `sfc-controller`, `ovs-cni` `nvipam` `flannel` etc.
* DPUCluster control plane scale including etcd performance
* DPF controllers at large target cluster scale
* Resources on individual DPUs at scale e.g. DPU file descriptors, memory
* Specific DPUServices - i.e. OVN-Kubernetes, HBN at scale
* DMS operations at scale


### Running the Scale Tests

You can set up a scale testing environment locally with the DPF developer environment. This builds and pushes the required images, spins up a new cluster, deploys dpf and mock-dms.
```yaml
export REGISTRY=$YOUR_REGISTRY
export TAG=$YOUR_TAG
export IMAGE_PULL_KEY=$YOUR_IMAGE_KEY
export NODE_MEMORY=16g #adjust as per your system limits
export E2E_TEST_ARGS="-e2e.skip-cleanup -ginkgo.label-filter=SCALE -e2e.config=config-scale.yaml"


make clean-test-env generate test-release-e2e-quick test-env-e2e test-deploy-operator-helm test-deploy-mock-dms test-e2e
```


### Verify

```
k get pods -n dpf-operator-system | grep mock-dms
mock-dms-controller-manager-9b7db9b4d-rs4rb                   1/1     Running   0          39m

k get nodes -A | grep dpu-worker | wc -l
10
```

### Future Work

#### Improving Test Signal

* choose specific metrics and target values for a given infrastructure
* iterate on scale dimensions

#### Extending Scale Test Coverage

* Adding compute to the DPUCluster to test DPUCluster components and DPUCluster control plane
* Adding compute to the target cluster to test scaling of DPF components in large target clusters
