---
title: "DPFOperatorConfig"
---

[[_TOC_]]

## Overview

The `DPFOperatorConfig` controls how DPF operates in your Kubernetes cluster. This guide explains the major
configuration options. When the config is applied, the DPF Operator will deploy all necessary components and configure
them according to the configuration.


## Basic Configuration Example

This basic config example enables the Kamaji cluster manager.

> [!NOTE]
> In the current implementation the `DPFOperatorConfig` resource is a singleton. This means that only one instance of
> this resource can exist in the cluster. If you try to create a second instance, the controllers will not work as
> expected.

You can find the full API documentation in the [API Reference](./api.md#operatordpunvidiacomv1alpha1).

```yaml
apiVersion: operator.doca-platform.nvidia.com/v1alpha1
kind: DPFOperatorConfig
metadata:
  name: dpf-operator-config
  namespace: dpf-operator-system
spec:
  staticClusterManager:
    disable: true
  kamajiClusterManager:
    disable: false
```

We can verify if the configuration is applied correctly by checking the status of the `DPFOperatorConfig` resource.

```shell
$ kubectl -n dpf-operator-system get dpfoperatorconfig
NAME                READY   PHASE     AGE
dpfoperatorconfig   True    Success   1h
```

or via `dpfctl`

```shell
$ kubectl -n dpf-operator-system exec deployment/dpf-operator-controller-manager -- /dpfctl describe all
NAME                                      NAMESPACE            STATUS  REASON   SINCE  MESSAGE
DPFOperatorConfig/dpfoperatorconfig       dpf-operator-system
            ├─Ready                                            True    Success  1h
            ├─ImagePullSecretsReconciled                       True    Success  1h
            ├─SystemComponentsReady                            True    Success  1h
            └─SystemComponentsReconciled                       True    Success  1h
```

## Argo CD Namespace

The DPF system namespace is `dpf-operator-system`. The `DPFOperatorConfig` must be created in this namespace, and DPF
Applications are reconciled from this namespace.

If Argo CD is installed in a different namespace, set `spec.overrides.argoCDNamespace` to the Argo CD namespace.
Ensure that `dpf-operator-system` is included in the Argo CD Helm value `configs.params.application.namespaces` (or an
equivalent configuration) so Argo CD reconciles Applications in `dpf-operator-system`. See
[Helm Prerequisites](../../getting-started/helm-prerequisites.md#running-argo-cd-in-a-separate-namespace) for the
matching install-time guidance.

```yaml
apiVersion: operator.doca-platform.nvidia.com/v1alpha1
kind: DPFOperatorConfig
metadata:
  name: dpf-operator-config
  namespace: dpf-operator-system
spec:
  overrides:
    argoCDNamespace: argo-cd
  staticClusterManager:
    disable: true
  kamajiClusterManager:
    disable: false
```

## Configuration Options

### Networking

There are networking options that can be configured. The MTU for the control plane and high-speed interfaces can be
configured. The default value is set to 1500, however it can be adjusted if required.

```yaml
spec:
  networking:
    controlPlaneMTU: 1500    # Management network MTU (range: 1280-9216, default: 1500)
    highSpeedMTU: 1500       # High-speed interface MTU (range: 1280-9216, default: 1500)
```

### Image Pull Secrets

Specify secrets for pulling container images. This is only necessary if your container registry requires authentication.
If you are using the public GHCR registry, which is the default, you do not need to configure this.

```yaml
spec:
  imagePullSecrets:
    - "my-registry-secret"
    - "another-secret"
```

### Resources

All system components deployed by the DPF Operator support standard Kubernetes resource requests and limits.
Resources can be configured per component at the container level. Components may have multiple containers with
different resource requirements that can be configured independently.

Below is an example of configuring resources for the SFC Controller component:

```yaml
spec:
  sfcController:
    controller:
      resources:
        requests:
          cpu: 6
          memory: 2Gi
        limits:
          cpu: 8
          memory: 4Gi
```

This pattern applies to all components listed in
the [Optional Component Configurations](#optional-component-configurations) section below.  
For production deployments, it is recommended to set appropriate resource limits based on your cluster's workload.

### Monitoring

The `spec.monitoring` field configures DPF-operator-managed observability components deployed on each DPU cluster. By default, Kube-State-Metrics and Node-Problem-Detector are enabled. OpenTelemetry Collector requires an explicit logging endpoint.

```yaml
spec:
  monitoring:
    # Disable all monitoring components (default: false)
    # disable: true
    kubeStateMetrics:
      disable: false
    nodeProblemDetector:
      disable: false
    openTelemetryCollector:
      disable: false
      logging:
        endpoint: "http://<host-node-ip>:30318"
```

Each component supports `disable` and `daemon` (image, resources) overrides. To disable all monitoring at once, set `spec.monitoring.disable: true`.

For detailed configuration options and architecture, see [DPF-Operator-Managed Components](../../operational-readiness/observability/deployment/operator-managed-components.md).

### Optional Component Configurations

The following components can be configured to enable/disable features or specify a different container image.  
By default, all components are enabled with preconfigured images, and changes are usually only needed for development,
testing, or specific deployments.

```yaml
spec:
  cniInstaller: { }
  dpuDetector: { }
  dpuServiceController: { }
  flannel: { }
  kamajiClusterManager: { }
  multus: { }
  nvipam: { }
  ovsCNI: { }
  provisioningController: { }
  serviceSetController: { }
  sfcController: { }
  sriovDevicePlugin: { }
  staticClusterManager: { }
```

To disable a component or override its container image, use the following configuration:

```yaml
spec:
  sriovDevicePlugin:
    disable: true
  dpuDetector:
    daemon:
      image: "my-registry/my-dpu-detector:latest"
```

> [!WARNING]
> **Deprecated:** Setting the image at component level (e.g., `spec.dpuDetector.image`) is deprecated.
> Use the sub-component specific image field instead (e.g., `spec.dpuDetector.daemon.image`).

For a detailed description of each component and its available configuration options, see  
the [API Reference](./api.md#operatordpunvidiacomv1alpha1).

#### DPU Service Controller Configuration options

* `spec.dpuServiceController.disableDPUReadyTaints`: When set to true, disables the automatic tainting of DPU nodes when they're not ready.

```yaml
spec:
  dpuServiceController:
    disableDPUReadyTaints: true
```

#### Flannel Configuration Options

* `spec.flannel.podCIDR`: CIDR range for pod networking when using Flannel CNI.

```yaml
spec:
  flannel:
    podCIDR: "10.244.0.0/16"
```

#### Component Deployment Configuration

Several components support additional deployment configuration options:

* `helmChart`: Override the Helm chart repository/version for the component

```yaml
spec:
  multus:
    helmChart: "custom-repo/multus:v1.0.0"
```

#### SFC Controller Configuration Options

* `spec.sfcController.SecureFlowDeletionTimeout`: Used to control the secure flow deletion feature.

    The default value is 0, which means that the feature is disabled.  
    When set with a valid duration value, indicating the API server unavailability threshold, 
    SFC controller will delete all openflow flows to prevent unintended packet leaks,
    if API server is unavailable for more than the specified duration.  
    Value must be in units accepted by Go time.ParseDuration https://golang.org/pkg/time/#ParseDuration.

```yaml
spec:
  sfcController:
    SecureFlowDeletionTimeout: 5m
```

#### Provisioning Controller Configuration Options

* `spec.provisioningController.bfbPVCName`: **(Optional)** Name of the PVC containing the BFB (BF Bundle) for provisioning DPUs. If it is not set, node local storage via a hostPath volume is used by default.

* `spec.provisioningController.maxDPUParallelInstallations`: Controls the maximum number of DPUs that can be provisioned concurrently.
    The default value is 50. The value must be at least 1.

* `spec.provisioningController.maxUnavailableDPUNodes`: Maximum number of DPU nodes that can be unavailable during updates. The provisioning controller interacts with the maintenance-operator to implement the drain node effect. The number of nodes that can be applied node effect simultaneously is determined by MaxUnavailableDPUNodes in dpfoperatorconfig and MaxParallelOperations in the NodeMaintenance-operator configuration. NodeMainteanceOperator has higher priority than what is defined in the DPFOperatorConfig. The default value of DPFOperatorConfig.MaxUnavailableDPUNodes is 50. For the default MaintenanceOperatorConfig values see instructions in [helm prerequisites](https://gitlab-master.nvidia.com/doca-platform-foundation/doca-platform-foundation/-/blob/main/docs/public/getting-started/helm-prerequisites.md?ref_type=heads).

  The maxDPUParallelInstallations and maxUnavailableDPUNodes options can be configured together and can be combined with maxParallelOperations and maxUnavailable in Nvidia NodeMaintenance-operator configuration. Below are some examples to show the expected behaviour.

| maxDPUParallelInstallations in DPFOperatorconfig | maxUnavailableDPUNodes in DPFOperatorconfig | maxParallelOperations in Nvidia NodeMaintenanceConfig | maxUnavailable in Nvidia NodeMaintenanceConfig | max number of DPUs in provisioning | max number of Nodes under node effect in NodeMaintenanceOperator|
|-------------------------------------|-------------------------------|-----------------------------------------------------|-----------------------------------|--------------------------------------------------------|--------------------------------------------------------|
| 5 | 1 | 10 | 5  | up to 5 DPUs provisioning in parallel | up to 1 node under node effect |
| 1 | 5 | 10 | 10 | up to 1 DPU provisioning              | up to 1 node under node effect |
| 5 | 5 | 1  | 5  | up to 5 DPUs provisioning in parallel | up to 1 node under node effect |
| 5 | 5 | 10 | 2  | up to 5 DPUs provisioning in parallel | up to 2 node under node effect |

* `spec.provisioningController.bfCFGTemplateConfigMap`: Name of ConfigMap containing bf-cfg template for DPU configuration.

* `spec.provisioningController.customCASecretName`: Name of Secret containing custom CA certificates for secure communication.

* `spec.provisioningController.dmsTimeout`: Timeout in seconds for DMS (DPU Management Service) operations.

* `spec.provisioningController.replicas`: Number of provisioning-controller pods for high availability.

* `spec.provisioningController.multiDPUOperationsSyncWaitTime`: Wait time for synchronizing operations across multiple DPUs.
    Value must be in units accepted by Go time.ParseDuration https://golang.org/pkg/time/#ParseDuration.

* `spec.provisioningController.registry`: Configuration for the container registry used during provisioning.
    * `address`: Registry address (deprecated)
    * `port`: Registry port (deprecated)
    * `loadBalancerAddress`: Load balancer address for registry

* `spec.provisioningController.nodeEffectRemovalTimeout`: Maximum time allowed for the Node Effect Removal phase. If the `DPUNodeMaintenance` CR still has requestors after this timeout, the DPU transitions to Error state, which is terminal and requires reprovisioning (deleting and recreating the DPU). The default is `0s`, which disables the timeout entirely (no time limit is enforced). To enable, set to a non-zero duration (e.g. `30m`). Value must be in units accepted by Go `time.ParseDuration` (e.g. `30m`, `1h`, `45m30s`).

* `spec.provisioningController.installInterface`: Method for installing DPU firmware. Choose one:
    * `installViaHostAgent`: Install via host agent
    * `installViaGNOI`: Install via gNOI protocol
    * `installViaRedfish`: Install via Redfish API with additional options:
        * `bfbRegistry.disable`: Disable the BFB registry
        * `bfbRegistry.port`: Port for BFB registry (deprecated)
        * `bfbRegistryAddress`: Address of BFB registry (deprecated)
        * `skipDpuNodeDiscovery`: Skip automatic DPU node discovery

```yaml
spec:
  provisioningController:
    maxDPUParallelInstallations: 25  # Limit concurrent provisioning to 25 DPUs
    maxUnavailableDPUNodes: 5
    dmsTimeout: 600
    replicas: 2
    multiDPUOperationsSyncWaitTime: 30s
    nodeEffectRemovalTimeout: 0s  # Disabled by default. Set to e.g. "30m" to enforce a timeout.
    customCASecretName: my-ca-secret
    installInterface:
      installViaRedfish:
        skipDpuNodeDiscovery: false
```

### Advanced Overrides

The `overrides` section allows customization of system-level paths and settings. These are typically only needed for
non-standard deployments or testing scenarios.

```yaml
spec:
  overrides:
    # Pause reconciliation of the DPFOperatorConfig
    paused: false
    
    # Kubernetes API server configuration
    kubernetesAPIServerVIP: "192.168.1.100"
    kubernetesAPIServerPort: 6443
    
    # DPU filesystem paths for CNI
    dpuCNIPath: "/etc/cni/net.d"
    dpuCNIBinPath: "/opt/cni/bin"
    
    # DPU OpenVSwitch paths
    dpuOpenvSwitchBinPath: "/usr/bin"
    dpuOpenvSwitchRunPath: "/var/run/openvswitch"
    dpuOpenvSwitchSystemSharedPath: "/usr/share/openvswitch"
    dpuOpenvSwitchSystemSharedLib64Path: "/usr/lib64"
    
    # Flannel-specific overrides
    flannelSkipCNIConfigInstallation: false
```

#### Override Options

* `paused`: When set to true, pauses reconciliation of the DPFOperatorConfig resource.
* `kubernetesAPIServerVIP`: The Kubernetes API server virtual IP address. **Required in Zero Trust mode** (when `installViaRedfish` is used).
* `kubernetesAPIServerPort`: The Kubernetes API server port (default: 6443). **Required in Zero Trust mode** (when `installViaRedfish` is used).
* `dpuCNIPath`: Path to CNI configuration directory on DPU nodes.
* `dpuCNIBinPath`: Path to CNI binaries on DPU nodes.
* `dpuOpenvSwitchBinPath`: Path to OpenvSwitch binaries on DPU nodes.
* `dpuOpenvSwitchRunPath`: Path to OpenvSwitch runtime directory on DPU nodes.
* `dpuOpenvSwitchSystemSharedPath`: Path to OpenvSwitch shared directory on DPU nodes.
* `dpuOpenvSwitchSystemSharedLib64Path`: Path to OpenvSwitch 64-bit libraries on DPU nodes.
* `flannelSkipCNIConfigInstallation`: Skip automatic CNI configuration installation for Flannel.
* `argoCDNamespace`: Namespace where Argo CD is installed. Defaults to the namespace of the `DPFOperatorConfig`. AppProjects and cluster secrets required by DPF are created in this namespace.
