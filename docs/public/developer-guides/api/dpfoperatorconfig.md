---
title: "DPFOperatorConfig"
---

[TOC]

## Overview

The `DPFOperatorConfig` controls how DPF operates in your Kubernetes cluster. This guide explains the major
configuration options. When the config is applied, the DPF Operator will deploy all necessary components and configure
them according to the configuration.


## Basic Configuration Example

This basic config example enables the Kamaji cluster manager and sets the PVC name of the BFB PVC which is necessary for
the provisioning controller to download the bf-bundle.

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
spec:
  staticClusterManager:
    disable: true
  kamajiClusterManager:
    disable: false
  provisioningController:
    bfbPVCName: bfb-pvc
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

## Configuration Options

### Networking

There are networking options that can be configured. The MTU for the control plane and high-speed interfaces can be
configured. The default value is set to 1500, however it can be adjusted if required.

```yaml
spec:
  networking:
    controlPlaneMTU: 1500    # Management network MTU (range: 1000-9216, default: 1500)
    highSpeedMTU: 1500       # High-speed interface MTU (range: 1000-9216, default: 1500)
```

### Image Pull Secrets

Specify secrets for pulling container images. This is only necessary if your container registry requires authentication.
If you are using the public GHCR registry, which is the default, you don't need to configure this.

```yaml
spec:
  imagePullSecrets:
    - "my-registry-secret"
    - "another-secret"
```

### Optional Component Configurations

The following components can be configured to enable/disable features or specify a different container image.  
By default, all components are enabled with preconfigured images, and changes are usually only needed for development,
testing, or specific deployments.

```yaml
spec:
  dpuDetector: { }
  dpuServiceController: { }
  flannel: { }
  multus: { }
  nvipam: { }
  ovsCNI: { }
  provisioningController: { }
  serviceSetController: { }
  sfcController: { }
  sriovDevicePlugin: { }
```

To disable a component or override its container image, use the following configuration:

```yaml
spec:
  sriovDevicePlugin:
    enabled: false
  dpuDetector:
    image: "my-registry/my-dpu-detector:latest"
```

For a detailed description of each component and its available configuration options, see  
the [API Reference](./api.md#operatordpunvidiacomv1alpha1).

#### SFC Controller Configuration options

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

#### Provisioning Controller Configuration options

* `spec.provisioningController.maxDPUParallelInstallations`: Controls the maximum number of DPUs that can be provisioned concurrently.

    The default value is 50.  
    The value must be at least 1.

```yaml
spec:
  provisioningController:
    maxDPUParallelInstallations: 25  # Limit concurrent provisioning to 25 DPUs
```
