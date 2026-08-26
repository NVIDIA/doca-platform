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
apiVersion: operator.dpu.nvidia.com/v1alpha1
kind: DPFOperatorConfig
metadata:
  name: dpfoperatorconfig
  namespace: dpf-operator-system
spec:
  deploymentMode: host-trusted
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
apiVersion: operator.dpu.nvidia.com/v1alpha1
kind: DPFOperatorConfig
metadata:
  name: dpfoperatorconfig
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

In host-trusted deployments, the out-of-band bridge name can also be configured. The bridge must exist on worker nodes
before DPF components start; DPF does not create it. Changing the bridge name on a running cluster is a destructive
operation: update the bridge on all nodes first, then immediately update `DPFOperatorConfig` with the new name.

```yaml
spec:
  networking:
    controlPlaneMTU: 1500    # Management network MTU (range: 1280-9216, default: 1500)
    highSpeedMTU: 1500       # High-speed interface MTU (range: 1280-9216, default: 1500)
    dpuNodeOOBBridgeName: br-dpu  # Host-trusted OOB bridge name (default: br-dpu, max 15 chars)
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
        endpoint: "http://<host-node-ip>:30050"
        # OTLP transport used to export to the endpoint: "http" (default) or "grpc".
        # transport: grpc
        # Reference to a Secret holding the PEM-encoded CA bundle (key "ca.crt")
        # used to verify the endpoint's TLS certificate. The Secret may live in any
        # namespace; namespace defaults to the DPFOperatorConfig namespace.
        # Required only for certificates issued by a private CA.
        # caSecretRef:
        #   name: otel-ca
      # The metrics configuration takes the same endpoint, transport and
      # caSecretRef fields, configured independently of logging.
      # metrics:
      #   endpoint: "http://<host-node-ip>:30050"
```

Each component supports `disable` and `daemon` (image, resources) overrides. To disable all monitoring at once, set `spec.monitoring.disable: true`.

For TLS endpoints (`https://`), the certificate is verified against the system CA pool by default. To use a private CA, create a Secret with the CA bundle under the `ca.crt` key and reference it via `caSecretRef`. The Secret may live in any namespace, for example alongside the endpoint's cert-manager `Certificate`; the reference's namespace defaults to `dpf-operator-system`. Certificate verification is never skipped. `transport` and `caSecretRef` are set per signal, so `logging` and `metrics` can target different endpoints.

A single node IP as the endpoint is a single point of failure. For production, use a highly available address that fronts the Host Cluster nodes (a failover VIP, a `LoadBalancer` Service address, or a DNS name resolving to one of those). See the [High Availability](../../operational-readiness/observability/deployment/operator-managed-components.md#high-availability) section for details.

For detailed configuration options and architecture, see [DPF-Operator-Managed Components](../../operational-readiness/observability/deployment/operator-managed-components.md).

### Optional Component Configurations

The following components can be configured to enable/disable features or specify a different container image.  
Most components are enabled by default with preconfigured images. Some components, such as `staticClusterManager`,
and `nodeSRIOVDevicePluginController`, are disabled by default and can be enabled when needed.

```yaml
spec:
  cniInstaller: { }
  dpuDetector: { }
  dpuServiceController: { }
  flannel: { }
  kamajiClusterManager: { }
  multus: { }
  nodeSRIOVDevicePluginController: { }
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

To enable a component that is disabled by default, set `disable` to `false`:

```yaml
spec:
  nodeSRIOVDevicePluginController:
    disable: false
  staticClusterManager:
    disable: false
```

> [!WARNING]
> **Deprecated:** Setting the image at component level (e.g., `spec.dpuDetector.image`) is deprecated.
> Use the sub-component specific image field instead (e.g., `spec.dpuDetector.daemon.image`).

For a detailed description of each component and its available configuration options, see  
the [API Reference](./api.md#operatordpunvidiacomv1alpha1).

#### DPU Service Controller Configuration options

* `spec.dpuServiceController.disableDPUReadyTaints`: When set to true, acts as a full taint kill-switch for the DPUReady controller. No taint managed by this controller (`NoSchedule` for critical DPUServices, or `NoExecute` for HostNetworkReady) is added, removed, or otherwise touched on host worker nodes. See [Disabling All DPUReady Taints](./dpuservice.md#disabling-all-dpuready-taints) for details.

```yaml
spec:
  dpuServiceController:
    disableDPUReadyTaints: true
```

* `spec.dpuServiceController.disableHostNetworkReadyNoExecuteTaints`: When set to false, enables adding a `NoExecute` taint to a host worker node when its Ready-phase DPU reports `HostNetworkReady` is not `True`, evicting workloads that depend on host VFs. Has no effect when `disableDPUReadyTaints` is `true`. See [Host Network Readiness NoExecute Taints](./dpuservice.md#host-network-readiness-noexecute-taints) for details.

```yaml
spec:
  dpuServiceController:
    disableHostNetworkReadyNoExecuteTaints: false
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

* `spec.sfcController.secureFlowDeletionTimeout`: Used to control the secure flow deletion feature.

    The default value is 0, which means that the feature is disabled.  
    When set with a valid duration value, indicating the API server unavailability threshold, 
    SFC controller will delete all openflow flows to prevent unintended packet leaks,
    if API server is unavailable for more than the specified duration.  
    Value must be in units accepted by Go time.ParseDuration https://golang.org/pkg/time/#ParseDuration.

```yaml
spec:
  sfcController:
    secureFlowDeletionTimeout: 5m
```

#### Provisioning Controller Configuration Options

* `spec.provisioningController.bfbPVCName`: **(Optional)** Name of the PVC containing the BFB (BF Bundle) for provisioning DPUs. If it is not set, node local storage via a hostPath volume is used by default.

* `spec.provisioningController.maxDPUParallelInstallations`: Controls the maximum number of DPUs that can be provisioned concurrently.
    The default value is 50. The value must be at least 1.

* `spec.provisioningController.maxUnavailableDPUNodes`: Maximum number of DPU nodes that can be unavailable during updates. The provisioning controller interacts with the maintenance-operator to implement the drain node effect. The number of nodes that can be applied node effect simultaneously is determined by MaxUnavailableDPUNodes in dpfoperatorconfig and MaxParallelOperations in the NodeMaintenance-operator configuration. NodeMainteanceOperator has higher priority than what is defined in the DPFOperatorConfig. The default value of DPFOperatorConfig.MaxUnavailableDPUNodes is 50. For the default MaintenanceOperatorConfig values see instructions in [helm prerequisites](https://gitlab-master.nvidia.com/doca-platform-foundation/doca-platform-foundation/-/blob/main/docs/public/getting-started/helm-prerequisites.md?ref_type=heads).

* `spec.provisioningController.osInstallRetries`: Maximum number of retryable OS installation attempts in zero-trust mode before the DPU transitions to Error. When unset, the provisioning controller defaults to 2.

  The maxDPUParallelInstallations and maxUnavailableDPUNodes options can be configured together and can be combined with maxParallelOperations and maxUnavailable in Nvidia NodeMaintenance-operator configuration. Below are some examples to show the expected behaviour.

| maxDPUParallelInstallations in DPFOperatorconfig | maxUnavailableDPUNodes in DPFOperatorconfig | maxParallelOperations in Nvidia NodeMaintenanceConfig | maxUnavailable in Nvidia NodeMaintenanceConfig | max number of DPUs in provisioning | max number of Nodes under node effect in NodeMaintenanceOperator|
|-------------------------------------|-------------------------------|-----------------------------------------------------|-----------------------------------|--------------------------------------------------------|--------------------------------------------------------|
| 5 | 1 | 10 | 5  | up to 5 DPUs provisioning in parallel | up to 1 node under node effect |
| 1 | 5 | 10 | 10 | up to 1 DPU provisioning              | up to 1 node under node effect |
| 5 | 5 | 1  | 5  | up to 5 DPUs provisioning in parallel | up to 1 node under node effect |
| 5 | 5 | 10 | 2  | up to 5 DPUs provisioning in parallel | up to 2 node under node effect |

* `spec.provisioningController.bfCFGTemplateConfigMap`: Name of ConfigMap containing a bf.cfg template for DPU configuration. **Deprecated** and will be removed in a future release; use `enableDynamicBFCFGTemplates` instead. Mutually exclusive with `enableDynamicBFCFGTemplates`.

* `spec.provisioningController.enableDynamicBFCFGTemplates`: Enables runtime discovery of bf.cfg templates via ConfigMaps. When enabled, the provisioning controller discovers ConfigMaps by matching labels for BFB name/namespace and DPUCluster name/namespace. Mutually exclusive with `bfCFGTemplateConfigMap`.

* `spec.provisioningController.customCASecretName`: Name of Secret containing custom CA certificates for secure communication.

* `spec.provisioningController.dmsTimeout`: Timeout in seconds for DMS (DPU Management Service) operations.

* `spec.provisioningController.replicas`: Number of provisioning-controller pods for high availability.

* `spec.provisioningController.multiDPUOperationsSyncWaitTime`: Wait time for synchronizing operations across multiple DPUs.
    Value must be in units accepted by Go time.ParseDuration https://golang.org/pkg/time/#ParseDuration.

* `spec.provisioningController.registry`: Configuration for the container registry used during provisioning.
    * `address`: Registry address. **Deprecated** and will be removed in a future release; use `loadBalancerAddress` instead.
    * `port`: Registry port. **Deprecated** and will be removed in a future release; use `loadBalancerAddress` instead.
    * `loadBalancerAddress`: Load balancer address for the BFB Registry that the host agent/Redfish use to fetch the BFB and generated bf.cfg. This is the current, non-deprecated way to configure registry access. To use it, deploy your own load balancer controller pointed at the `bfb-registry` NodePort service. The value must start with `http://` or `https://`.

* `spec.provisioningController.nodeEffectRemovalTimeout`: Maximum time allowed for the Node Effect Removal phase. If the `DPUNodeMaintenance` CR still has requestors after this timeout, the DPU transitions to Error state, which is terminal and requires reprovisioning (deleting and recreating the DPU). The default is `0s`, which disables the timeout entirely (no time limit is enforced). To enable, set to a non-zero duration (e.g. `30m`). Value must be in units accepted by Go `time.ParseDuration` (e.g. `30m`, `1h`, `45m30s`).

* `spec.provisioningController.bmcServerCertRenewBefore`: How long before expiry the provisioning controller automatically rotates the DPU BMC mTLS server certificate (Zero Trust / Redfish only). The certificate is issued for 365 days, and the default renewal window is `720h` (30 days), so each DPU's certificate is renewed about a month before it expires. Value must be in units accepted by Go `time.ParseDuration` (e.g. `720h`, `30m`). If set to `0s` or to a value greater than or equal to the certificate validity period, the controller falls back to renewing at half the certificate lifetime. See [BMC mTLS Server Certificate Rotation](../../advanced-configuration/zero-trust-advanced-configuration.md#bmc-mtls-server-certificate-rotation) for the full rotation flow and the manual-rotation trigger.

* `spec.provisioningController.installInterface`: Method for installing DPU firmware. Choose one:
    * `installViaHostAgent`: Install via host agent
    * `installViaGNOI`: Install via gNOI protocol (deprecated; use `installViaHostAgent` instead)
    * `installViaRedfish`: Install via Redfish API with additional options:
        * `bfbRegistry.disable`: Disable the BFB registry
        * `bfbRegistry.port`: Port for BFB registry (deprecated; use `spec.provisioningController.registry.loadBalancerAddress` instead)
        * `bfbRegistryAddress`: Address of BFB registry (deprecated; use `spec.provisioningController.registry.loadBalancerAddress` instead)
        * `skipDPUNodeDiscovery`: Skip automatic DPU node discovery (default: `true`)
        * `discoveredDPUDeviceBMCFactoryResetPolicy`: The BMC factory reset policy stamped into the DPUDevices that discovery creates, `OnInitialization` or `Never`. When omitted, the discovery controller uses `OnInitialization`. A factory reset erases the BMC network configuration, so set this to `Never` when the discovered BMCs have static IP addresses. See [BMC Factory Reset](../../advanced-configuration/bmc-factory-reset.md).

```yaml
spec:
  provisioningController:
    maxDPUParallelInstallations: 25  # Limit concurrent provisioning to 25 DPUs
    maxUnavailableDPUNodes: 5
    dmsTimeout: 600
    replicas: 2
    multiDPUOperationsSyncWaitTime: 30s
    nodeEffectRemovalTimeout: 0s  # Disabled by default. Set to e.g. "30m" to enforce a timeout.
    bmcServerCertRenewBefore: 720h  # Rotate the BMC mTLS server cert 30 days before expiry (Zero Trust).
    customCASecretName: my-ca-secret
    installInterface:
      installViaRedfish:
        skipDPUNodeDiscovery: false
        discoveredDPUDeviceBMCFactoryResetPolicy: OnInitialization  # Set to Never for BMCs with a static IP address
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
    dpuOpenvSwitchSystemSharedPath: "/lib"
    dpuOpenvSwitchSystemSharedLib64Path: "/lib64"

    # Optional additional DPU library paths mounted into the SFC Controller
    dpuLinkerCachePath: "/etc/ld.so.cache"
    dpuOptLibraryPath: "/usr/opt"
    
    # Flannel-specific overrides
    flannelSkipCNIConfigInstallation: false
```

#### Override Options

* `paused`: When set to true, pauses reconciliation of the DPFOperatorConfig resource.
* `kubernetesAPIServerVIP`: The Kubernetes API server virtual IP address. **Required in Zero Trust mode** (when `installViaRedfish` is used).
* `kubernetesAPIServerPort`: The Kubernetes API server port (default: 6443). **Required in Zero Trust mode** (when `installViaRedfish` is used).
* `dpuCNIPath`: Path at which the CNI config files are installed on the DPU (default: `/etc/cni/net.d`). This only controls where DPF writes the CNI config; it does not reconfigure kubelet, which continues to read the CNI config from its own configured directory. Only override this if kubelet on the DPU is already configured to read from the same non-default path, otherwise kubelet will not find the CNI config.
* `dpuCNIBinPath`: Path at which the CNI binaries are installed on the DPU (default: `/opt/cni/bin`). This only controls where DPF installs the CNI binaries; it does not reconfigure kubelet, which continues to load CNI binaries from its own configured directory. Only override this if kubelet on the DPU is already configured to load binaries from the same non-default path.
* `dpuOpenvSwitchBinPath`: Path to the OpenvSwitch bin directory on DPU nodes (default: `/usr/bin/`).
* `dpuOpenvSwitchRunPath`: Path to the OpenvSwitch run directory on DPU nodes (default: `/var/run/openvswitch`).
* `dpuOpenvSwitchSystemSharedPath`: Path to the system shared library directory used by OVS components on the DPU (default: `/lib`).
* `dpuOpenvSwitchSystemSharedLib64Path`: Path to the system `lib64` directory used by OVS components on the DPU. If unset, no `lib64` volume mount is configured in the SFC Controller component.
* `dpuLinkerCachePath`: Path on the DPU to a prebuilt dynamic-linker cache file. When set, the file is mounted read-only into the SFC Controller container so host OVS binaries resolve shared libraries using the DPU's linker configuration. If unset, no linker cache mount is added.
* `dpuOptLibraryPath`: Path on the DPU to an additional library directory (for example `/usr/opt` on an RHCOS BFB). When set, the directory is mounted read-only into the SFC Controller container. If unset, no additional library directory is mounted.
* `flannelSkipCNIConfigInstallation`: Whether Flannel skips installing its own CNI configuration (default: `true`). Set to `false` to have Flannel install a CNI configuration.
* `argoCDNamespace`: Namespace where Argo CD is installed. Defaults to the namespace of the `DPFOperatorConfig`. AppProjects and cluster secrets required by DPF are created in this namespace.
