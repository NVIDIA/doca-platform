---
title: "Zero Trust Advanced Configuration"
---

[[_TOC_]]

This section includes advanced configuration and additional information for the Zero Trust use case.

# Kubernetes API Server Access

In Zero Trust mode, `DPFOperatorConfig.spec.overrides.kubernetesAPIServerVIP` and
`DPFOperatorConfig.spec.overrides.kubernetesAPIServerPort` are required. Set them to the Kubernetes API server endpoint
of the DPF cluster.

# DPU Discovery and DPUNode and DPUDevice Object Creation

DPF provides two approaches for discovering and creating DPU resources:

1. **Automated Discovery**: Using `DPUDiscovery` to automatically scan for DPUs and create `DPUDevice` and `DPUNode` resources.
2. **Manual Creation**: Manually creating `DPUDevice` and `DPUNode` resources for each DPU.

You can choose either approach based on your deployment requirements. Automated discovery is recommended for larger
deployments, while manual creation provides more control for smaller or specific configurations.

## Automated DPU Discovery

DPUDiscovery enables automatic discovery of DPU devices and nodes by scanning specified IP ranges. This approach automatically
creates `DPUDevice` and `DPUNode` resources for any discovered DPUs.

**1.** First, create a YAML file for the DPUDiscovery resource. Let's call it `dpudiscovery.yaml`:

```yaml
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUDiscovery
metadata:
  name: dpu-discovery-192.168.1-10
  namespace: dpf-operator-system
spec:
  # Define the IP range to scan
  ipRangeSpec:
    ipRange:
      startIP: "10.0.110.120"    # Replace with your start IP
      endIP: "10.0.110.125"     # Replace with your end IP
  
  # Optional: Set scan interval
  scanInterval: "3m"
  # Optional: Set number of workers (default is 1 per 255 IPs)
  workers: 1
```

**2.** Apply the resource using kubectl:

```bash
kubectl apply -f dpudiscovery.yaml
```

**3.** Check the status of the crawler:

```bash
kubectl get dpudiscovery dpu-discovery-192.168.1-10 -o yaml
```

The DPU discovery will:

1. Start scanning the specified IP range
2. Create DPUDevice and DPUNode\* resources for any discovered DPUs
3. Continue scanning at the specified interval
4. Update its status with the last scan time and found DPUs

You can monitor the discovered DPUs with:

```bash
# List discovered DPU devices
kubectl get dpudevices

# List discovered DPU nodes
kubectl get dpunodes
```

\* DPUDiscovery will skip the creation of a DPUNode if there is an existing one with the spec.dpus field containing the
DPUDevices serial number.

### Limitations

* When using autodiscovery for DPUNodes, the created DPUNodes will be named after `dpunode-<DPU_SERIAL_NUMBER>`. In case
    the HBN DPUService is used in conjuction with this DPU provisioning mode, the HBN configuration needs to be adjusted
    to match the discovered nodes accordingly.

## Manual DPU Resource Creation

If you prefer to manually create DPU resources or need more control over the creation process, you can create `DPUDevice`
and `DPUNode` resources manually.

### Creating DPUDevice manually

Create a `DPUDevice` resource for each DPU:

> [!NOTE]
> The `DPUDevice` is immutable, and creating a DPUDevice will not trigger DPU provisioning.

```yaml
---
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUDevice 
metadata:
  name: dpu-device-1
  namespace: dpf-operator-system
spec:
    bmcIp: 10.0.110.122
```

### Creating a DPUNode manually

Create a `DPUNode` resource for each host that has a DPU:

> [!NOTE]
> The `.spec.dpus` field contains the names of each DPUDevice attached to the node.

```yaml
---
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUNode
metadata:
  labels:
    feature.node.kubernetes.io/dpu-enabled: "true"
  name: worker1
  namespace: dpf-operator-system
spec:
  dpus:
  - name: dpu-device-1
  nodeRebootMethod:
    external: {}
```

# Secure Boot

DPF supports configuring UEFI Secure Boot on DPUs during Zero Trust provisioning. When `secureBoot` is set in the
DPUDeployment (or DPUSet), the controller detects the current hardware state via the BMC and configures it
automatically, performing the required ARM force restarts.

For configuration details, mode-specific behavior, and the impact of changing this setting on existing DPUs, see
[Secure Boot](../developer-guides/api/dpuset.md#secure-boot). 

For more information on BlueField Secure Boot, see
[Secure Boot](https://networking-docs.nvidia.com/bsp/4.15.0/secure-boot) in the NVIDIA documentation.

# BMC mTLS Server Certificate Rotation

In Zero Trust mode the provisioning controller talks to each DPU BMC over the Redfish API using
mTLS. During provisioning the controller bootstraps mTLS on the BMC: it installs the DPF CA,
requests a BMC **server** certificate through cert-manager, replaces the BMC's server
certificate, and enables mTLS. From that point the controller connects to the BMC with full
certificate verification (it validates the BMC server certificate against the DPF CA and pins the
BMC IP), so the served server certificate must stay valid for the lifetime of the DPU.

The BMC server certificate is issued for **365 days**. To avoid expiry the provisioning controller
automatically rotates it before it expires.

## Automatic rotation

* Each `DPUDevice` tracks its BMC server certificate under `status.bmcServerCertificate`:
    * `notAfter` — the expiry of the currently installed BMC server certificate.
    * `lastRotationTime` — when the controller last rotated the certificate.
    * `observedManualTrigger` — the last manual-rotation token the controller has already processed
        (see [Manual rotation](#manual-rotation)).
* The controller renews the certificate once it enters the renewal window, which defaults to
    **30 days** before expiry. This window is configurable cluster-wide via
    `DPFOperatorConfig.spec.provisioningController.bmcServerCertRenewBefore`
    (see [Provisioning Controller Configuration Options](../developer-guides/api/dpfoperatorconfig.md#provisioning-controller-configuration-options)).
* For DPUs that were provisioned before this feature existed, the controller backfills `notAfter`
    by reading the certificate the BMC is currently serving, rather than forcing an immediate
    fleet-wide rotation.
* Rotation progress is reported through the `BMCServerCertificateReady` condition on the
    `DPUDevice`:

| Condition status | Reason                               | Meaning                                                                                                        |
|------------------|--------------------------------------|----------------------------------------------------------------------------------------------------------------|
| `True`           | `Success`                            | The certificate is installed and outside the renewal window.                                                   |
| `False`          | `BMCServerCertificateRotating`       | A rotation is in progress (a new CSR was generated and the cert-manager `CertificateRequest` is being issued). |
| `False`          | `BMCServerCertificateRotationFailed` | The last rotation attempt failed; the controller retries automatically.                                        |

No user action is required for automatic rotation.

## Manual rotation

You can force an immediate rotation of a single DPU's BMC server certificate by annotating the
`DPUDevice` with `provisioning.dpu.nvidia.com/rotate-bmc-server-certificate`. The annotation value
is an opaque token — any non-empty string (a timestamp or UUID is recommended). Rotation runs
whenever the annotation value differs from `status.bmcServerCertificate.observedManualTrigger`;
after a successful rotation the controller copies the value into `observedManualTrigger`, so the
same token is never processed twice.

```bash
kubectl -n dpf-operator-system annotate dpudevice $DPUDEVICE_NAME \
  provisioning.dpu.nvidia.com/rotate-bmc-server-certificate="$(date +%s)" --overwrite
```

Watch the rotation complete:

```bash
kubectl -n dpf-operator-system get dpudevice $DPUDEVICE_NAME \
  -o jsonpath='{range .status.conditions[?(@.type=="BMCServerCertificateReady")]}{.status}{"\t"}{.reason}{"\n"}{end}'
```

## Expired certificate recovery

Manual rotation requires the provisioning controller to connect to the BMC with mTLS. If the BMC
server certificate has already expired, or otherwise no longer verifies against the trusted CA, the
controller cannot open the mTLS connection needed to request and install a new certificate.
In this case the controller reports `BMCServerCertificateReady=False` with reason
`BMCServerCertificateRotationFailed`.

DPF does not automatically re-run bootstrap when this happens because bootstrap uses basic-auth
access to the BMC. To recover, delete and recreate the affected `DPUDevice` so DPF performs the
mTLS bootstrap flow again and installs a fresh BMC server certificate.

```bash
kubectl -n dpf-operator-system delete dpudevice $DPUDEVICE_NAME
```

# External Host Reboot

In the Zero Trust scenario, DPF cannot manage the DPU's host machine. During the DPU provisioning process, when the DPU
CR reaches the `rebooting` phase, manual power-cycling is required by the user. The power-cycle operation must be
completed within two hours; otherwise, the DPU join cluster's secret will expire, causing DPU CR pending in `DPU Cluster
Config` phase. After the worker node boots up, the `provisioning.dpu.nvidia.com/dpunode-external-reboot-required`
annotation on the DPUNode must be manually removed.

If you use **script-based** host reboot (`nodeRebootMethod.script` on the DPUNode) instead of external power cycle, see
[DPUNode: Script reboot job failures and recovery](../developer-guides/api/dpunode.md#script-reboot-job-failures-and-recovery)
for how Jobs, DPU phase `DPURebooting`, and recovery interact.
