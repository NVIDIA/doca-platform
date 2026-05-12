---
title: "DPUService Development Guide"
---

[TOC]

The DOCA Platform Framework (DPF) manages application lifecycles through DPUService resources. A DPUService typically
represents an application that runs on NVIDIA BlueField DPUs and utilizes the DOCA SDK.

DOCA provides libraries for networking and data processing programmability that leverage NVIDIA BlueField DPU hardware
accelerators. For more information about developing DOCA applications see the [DOCA SDK programming guide](https://docs.nvidia.com/doca/sdk/doca+programming+guide/index.html).

A DPUService consists of the following components:

* One or more applications designed to run on the DPU
* One or more container images that include the application and its runtime environment
* A Helm chart that defines the Kubernetes manifests required to run the application

Note: An example DPUService is available at github.com/nvidia/doca-platform under `dpuservices/dummydpuservice`. This
example is a good base to start from when developing your own DPUServices.

## Application

A DPUService application:

* can be written in any language and serve any function.
* must run in a containerized environment.
* must run on `arm64`.

The DPUService API is flexible, and some implementations may not fully conform to the constraints described in this
document. For example, some DPUServices are deployed on clusters other than the DPUCluster using the `.spec.deployInCluster`
parameter. Services deployed in this manner must match the architecture of their target cluster.

## Container Image

A DPUService container image must:

* be available on a container registry which is accessible from the DPU.
* run on `arm64` architecture.

## DPUService Helm Chart

A DPUService Helm chart must:

* be available on a Helm registry which is accessible from the DPF control plane.
* contain exactly one DaemonSet - the `serviceDaemonSet` - that is part of a service function chain.
* Each Kubernetes object must have a `{{ .Release.Name }}` in their `.metadata.name`. This enables deployment of multiple
    instances of the service.
* Include [version constraints](#version-constraints) in the Helm chart annotations  - e.g. `dpu.nvidia.com/doca-version`
    for DOCA version.
* [Implement certain parameters](#Helm-chart-parameters)

DPUService developers may implement additional parameters in the Helm chart to meet their needs. 

### Helm chart parameters

A DPUService Helm chart must expose the following parameters which can be set using Helm values:

| Parameter                          | Type                                                                                                                            | Optional | Description                                                                                                                                                                                      |
|------------------------------------|---------------------------------------------------------------------------------------------------------------------------------|----------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `serviceDaemonSet.nodeSelector`    | [nodeSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.32/#nodeselector-v1-core)                       | No       | Node selector for the DaemonSet using [`nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution`](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.32/#nodeaffinity-v1-core) |
| `serviceDaemonSet.labels`          | map[string]string                                                                                                               | No       | serviceDaemonSet pod labels                                                                                                                                                                      |
| `serviceDaemonSet.annotations`     | map[string]string                                                                                                               | No       | serviceDaemonSet pod annotations                                                                                                                                                                 |
| `serviceDaemonSet.upgradeStrategy` | [daemonSetUpdateStrategy](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.32/#daemonsetupdatestrategy-v1-apps) | No       | serviceDaemonSet upgrade strategy configuration                                                                                                                                                  |
| `serviceDaemonSet.resources`       | map[string]string                                                                                                               | No       | serviceDaemonSet pod Resource requests/limits  (e.g. CPU, memory)                                                                                                                                |
| `exposedPorts.ports.<portName>`    | boolean                                                                                                                         | Yes      | Enable/disable exposure of specific ports (default: false)                                                                                                                                       |
| `exposedPorts.labels`              | map[string]string                                                                                                               | Yes*     | Custom labels for the Service resources to be discovered by the DPUService controller (*Required if exposing ports)                                                                              |

### Exposing ports

DPUServices can optionally expose ports to the outside world. Exposing these ports is enabled by setting the `exposedPorts`
parameter in the Helm chart. This parameter is optional for chart implementers.

* `exposedPorts.ports` parameter is a map of port names to boolean values.
* By default, all ports should be disabled (`false`)
* Each port is configurable individually. Users can enable exposure by setting `exposedPorts.ports.<portName>=true`.
* Custom labels must be accepted under `exposedPorts.labels` and will be applied to the relevant resources. This is necessary
    for the `Service` to be discovered by the DPUService controller.
* The Helm chart contains a `Service` of type `NodePort`.
* Both `internalTrafficPolicy` and `externalTrafficPolicy` must be set to `Local` to ensure correct routing behavior.

### Recommendations

The following list contains some recommended features for the DPUService Helm chart. If the chart developer does not
implement these, users may face issues deploying the chart in e.g. air-gapped or large scale environments.

* ImagePullSecrets for pod controllers - e.g. DaemonSets, Deployments should be configurable.
* CRDs should be placed in the crd folder of the Helm chart. 
* If the application needs to access resources in a different cluster than the one the application is deployed in, the
  chart should allow for configurable `volumes`, `volumeMounts` and `env` so that a [DPUServiceCredentialRequest](../api/dpuservice-credential-request.md)
  can be used.

## Developing a DPUService

This guide requires working knowledge of Docker, Kubernetes and Helm.

#### Prerequisites

* a docker build environment
* `docker` CLI installed - see the [guide](https://docs.docker.com/get-docker/)
* `helm` CLI installed - see [the guide](https://helm.sh/docs/intro/quickstart/) 
* `dpfctl` CLI installed - see [the guide](../../operational-readiness/troubleshooting/dpfctl/README.md)
* `kubectl` CLI installed - see [the guide](https://kubernetes.io/docs/tasks/tools/)
* The repo `github.com/nvidia/doca-platform` cloned to `$PATH_TO_ROOT_OF_DPF_REPO`
* A docker registry - `$DPUSERVICE_DOCKER_REGISTRY` you can push images to.
* A Helm registry  - `$DPUSERVICE_HELM_REGISTRY` you can push Helm charts to.

### 1. Choose an application to run as a DPUService

In this example we will use the `dummydpuservice` which is implemented as part of this repository. The `dummydpuservice`
is an application - written in `Go` - which exposes an http server that returns information about its runtime environment.
The implementation for this application is at `$PATH_TO_ROOT_OF_DPF_REPO/cmd/dummydpuservice/main.go`.

### 2. Containerize the application so it can be deployed and run on a BlueField DPU

Every DPUService has a container which runs the application on the DPU. Developers can define a container build process
that meets the requirements of their DPUService as long as [the basic requirements](#container-image).

#### Writing the Dockerfile

For the `dummydpuservice` we need to use go to build a binary. This application has no runtime dependencies so we can
use a minimal `distroless` base image.

This results in the following `Dockerfile`.

```Dockerfile
# Golang base image required to build the dummydpuservice.
FROM docker.io/library/golang:1.25 AS builder

WORKDIR /workspace

# Copy the go source from the DPF repo
COPY ./ ./

# Build the dummydpuservice binary.
RUN CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -o dummydpuservice cmd/dummydpuservice/main.go

# Copy the binary to a clean base image.
FROM nvcr.io/nvidia/doca/dpf_containers:1.0.2-ubuntu22.04-distroless
WORKDIR /
COPY --from=builder /workspace/manager .
USER 65532:65532

ENTRYPOINT ["/dummydpuservice"]
```

#### Building the image

Save this file to `Dockerfile.dummydpuservice` and run:

```bash 
# Build the container image
docker build --platform linux/arm64 \
  -f Dockerfile.dummydpuservice \
  -t $DPUSERVICE_DOCKER_REGISTRY/dummydpuservice:v0.1.0 \
  $PATH_TO_ROOT_OF_DPF_REPO
```

Note: in order to build this image you need to have an environment capable of building `arm64` container images. For
more information [see the Docker guide](https://docs.docker.com/build/building/multi-platform/).

#### Publishing the image

Publish the image to your docker registry:

```bash
docker tag dummydpuservice:latest $DPUSERVICE_DOCKER_REGISTRY/dummydpuservice:v0.1.0
docker push $DPUSERVICE_DOCKER_REGISTRY/dummydpuservice:v0.1.0
```

### 3. Create a Helm chart for the application

A helm chart is a set of templates which are used to create Kubernetes objects. For information about building helm
charts see [the official documentation](https://helm.sh/docs/chart_template_guide/getting_started/).

Each DPUService has its own Kubernetes objects which will differ from application to application.

The `dummydpuservice` chart requires:

* A DaemonSet which will run the application on each DPU
* A Service which will serve requests to the application

#### Writing the chart

The `dummydpuservice` chart is already developed and located at `$PATH_TO_ROOT_OF_DPF_REPO/dpuservices/dummydpuservice/chart`.
This helm chart is an excellent base to start with when developing a new DPUService.

#### Validating the chart

The dpuservice values can be validated with:

```bash
helm lint $PATH_TO_ROOT_OF_DPF_REPO/dpuservices/dummydpuservice/chart
```

This will validate the `dummydpuservice` chart against the `$PATH_TO_ROOT_OF_DPF_REPO/dpuservices/dummydpuservice/chart/values.schema.json`
schema. This schema can be copied to any DPUService to ensure that the values are valid.


#### Publishing the chart

A DPUService Helm chart must be available on a Helm registry which is accessible from the DPF control plane.

To package and publish the chart:

```bash
helm package $PATH_TO_ROOT_OF_DPF_REPO/dpuservices/dummydpuservice/chart --version v0.1.0
helm push dummydpuservice-chart-0.1.0.tgz $DPUSERVICE_HELM_REGISTRY
```

### 4. Deploy your DPUService

This step requires a working DPF installation. For information on how to set up DPF see the [user guides](../../user-guides/README.md).

A DPUService is a Kubernetes object which has a helm chart reference and a set of configuration values.

#### Writing the DPUService

For the `dummydpuservice` the DPUService will have the form:
[embedmd]:#(../../../../dpuservices/dummydpuservice/DPUService.yaml)
```yaml
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUService
metadata:
  name: dummydpuservice
  namespace: dummydpuservice
spec:
  helmChart:
    source:
      repoURL: https://$DPUSERVICE_HELM_REGISTRY/dpf
      version: v0.1.0
      chart: dummydpuservice-chart
    values:
      image:
        tag: v0.1.0
        repository: $DPUSERVICE_DOCKER_REGISTRY/dummydpuservice
  serviceID: dummydpuservice
  serviceDaemonSet:
    nodeSelector:
      nodeSelectorTerms:
      - matchExpressions:
          - key: dpu
            operator: In
            values:
              - dpu
    updateStrategy:
      type: RollingUpdate
      rollingUpdate:
        maxUnavailable: 2
    labels:
      dpuservice.dpu.nvidia.com/name: dummydpuservice
    annotations:
      dpuservice.dpu.nvidia.com/name: dummydpuservice
    resources:
      nvidia.com/bf_sf: 1
  configPorts:
    serviceType: NodePort
    ports:
      - name: exampletcp
        port: 8080
        protocol: TCP
      - name: exampleudp
        port: 8081
        protocol: UDP
```

You can deploy your DPUService by running the following command:
```bash
kubectl apply -f $PATH_TO_DPU_SERVICE_YAML
```

#### Checking DPUService health

You can monitor the health of your DPUService with:

```bash
dpfctl describe dpuservices --show-conditions=dpuservice/dummydpuservice
```

For advanced troubleshooting that requires inspecting workloads directly on the DPU cluster, see
[Accessing the Kamaji DPU Cluster](../../operational-readiness/troubleshooting/kamaji-cluster-access.md).

## Additional Information About Supported Features

### Version Constraints

The version constraints can be defined by setting specific annotations as part of the `Chart.yaml` file of the chart.

[embedmd]:#(../../../../dpuservices/dummydpuservice/chart/Chart.yaml)
```yaml
apiVersion: v2
name: dummydpuservice-chart
description: Dummydpuservice chart for Kubernetes
type: application
version: 0.1.0
appVersion: "0.1.0"
annotations:
  # Annotation to track the DOCA version compatibility
  # of a DPUService. This is a requirement for services
  # which implement synchronized upgrades using DPUDeployments.
  dpu.nvidia.com/doca-version: ">= 2.9"
```

The constraints must comply with https://github.com/Masterminds/semver/blob/v3.3.1/README.md?plain=1#L117-L212

There is a specific set of annotations that are handled today, the rest are stripped:

* `dpu.nvidia.com/doca-version`
