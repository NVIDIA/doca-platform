---
title: "DPF Operator Upgrade Guide"
---

# DPF Operator Version Migration Guides

This document describes the process of upgrading between different versions of the DPF Operator.

[[_TOC_]]

## Prerequisites

Before starting the upgrade process, ensure you have the
following [Helm Prerequisites](../../getting-started/helm-prerequisites.md) installed and configured.

Further prerequisites include:

* All DPF Operator components MUST be in a **ready state**
* All DPUs MUST be in a **ready and healthy state**
* Always **review the release notes** for the version you are upgrading to
  * Release notes contain critical information about:
    * New features and capabilities
    * Bug fixes and improvements
    * Breaking changes and migration requirements
    * Known issues and workarounds
    * Configuration changes that may be required

### Verifying Readiness

You can verify the readiness of your DPF Operator and DPUs using [`dpfctl`](../troubleshooting/dpfctl/README.md) or `kubectl`:

**Using dpfctl (recommended):**

```bash
# Check overall DPF Operator status
dpfctl describe all --show-resources=dpfoperatorconfig

# Check all conditions for troubleshooting
dpfctl describe all --show-resources=dpfoperatorconfig --show-conditions=all
```

The `dpfctl` tool provides a more user-friendly view of the DPFOperatorConfig status and all related conditions, making
it easier to identify validation issues.

**Using kubectl:**

```bash
kubectl get dpfoperatorconfig -o yaml
```

All status conditions in the status section should be `True`, and the `version` field should match the current DPF
Operator version.

## DPF Operator Lifecycle

When a new major version is released:

* New major version is supported
* Previous major version enters EOL (end-of-life)

## Supported Upgrade Path (n-1 Policy)

**Important**: DPF Operator follows an **n-1 upgrade policy** with the version pattern `year.month.patchVersion`.
You can upgrade from the previous release version to the current release version. Patch-level upgrades within the same
minor version are always allowed.

✅ **Supported**:

* v25.7.0 → v25.10.0
* v25.7.0 → v25.7.1

❌ **Not Supported**:

* v25.4 → v25.7 (unsupported version upgrade)
* v25.7 → next GA release (skipping version v25.10)

**Why this matters**: Skipping versions can lead to:

* Failed upgrades due to missing intermediate API changes
* Data corruption or loss
* Incompatible resource states
* Unsupported configuration combinations

> [!IMPORTANT]
> The n-1 upgrade policy applies to the **DPF Operator** itself, regardless of the BFB version running on DPUs.
> Even when operating DPUs with an older BFB LTS version (e.g., BFB 3.2), you must still upgrade DPF through
> each sequential release (e.g., v25.7 → v25.10 → v26.4). Skipping DPF releases is not supported.

## BlueField BFB Support Matrix

For the full BFB version compatibility matrix, see the [BlueField BFB Support Matrix](../../platform-support.md#bluefield-bfb-support-matrix) in the Platform Support documentation.

## Prevalidation

Before the DPF Operator upgrades any components, the system performs prevalidation checks to ensure the cluster is
in a safe state for the upgrade. These validations are **automatically executed** and **must pass** before the upgrade
can continue.

The operator performs the following prevalidations, any of which can block the upgrade:

* [DPU State Validation](#dpu-state-validation)
* [Kubernetes Version Skew](#kubernetes-version-skew)
* [Object Schema Validation](#object-schema-validation)
* [System Components Readiness](#system-components-readiness)
* [Version Compatibility Validation](#version-compatibility-validation)

### Validation Failures

All failures are reported through a single `PreUpgradeValidationReady` status condition on `DPFOperatorConfig`. While
this condition is `False`, the `Ready` condition also remains `False` and the operator does not proceed to upgrade its
managed components. Each example below shows the condition for a single failing check; when several checks fail, they are
aggregated under the same `Validation must pass for DPF upgrade to continue:` message.

#### DPU State Validation

* **DPU Readiness**: All DPUs must have reached a terminal state. Any DPU still progressing toward readiness blocks the upgrade.

<details markdown="1"><summary>Example failure condition</summary>

```yaml
status:
  conditions:
  - type: PreUpgradeValidationReady
    status: "False"
    reason: Error
    message: |-
      Validation must pass for DPF upgrade to continue:
        * DPU State:
          * DPU worker-1-0000-08-00 is not ready
          * DPU worker-2-0000-08-00 is not ready
```

</details>

#### Kubernetes Version Skew

* Each ready DPU's kubelet version must satisfy the [Kubernetes version skew policy](https://kubernetes.io/releases/version-skew-policy/) relative to its DPU cluster's kube-apiserver: the same major version, never newer than the API server, and at most three minor versions behind. DPUs provisioned by older operator versions that do not report a kubelet version are skipped.

<details markdown="1"><summary>Example failure condition</summary>

```yaml
status:
  conditions:
  - type: PreUpgradeValidationReady
    status: "False"
    reason: Error
    message: |-
      Validation must pass for DPF upgrade to continue:
        * Kubernetes Version Skew:
          * kubernetes version skew violated: cluster dpf-operator-system/dpu-cplane-tenant1: DPU dpf-operator-system/worker-1-0000-08-00: kubelet version (v1.31.0) is more than 3 minor versions behind kube-apiserver version (v1.35.4)
```

</details>

#### Object Schema Validation

* All existing DPF API objects are validated against the OpenAPI schema of the installed CRDs. If a field that was previously optional has become required in the new version, any objects missing that field are reported and the upgrade is blocked until they are corrected.
* **Required action when present:** Fix the schema violations on the listed resources. Once all objects pass validation, the condition transitions to `True` and the upgrade proceeds automatically.

<details markdown="1"><summary>Example failure condition</summary>
```yaml
status:
  conditions:
  - type: PreUpgradeValidationReady
    status: "False"
    reason: Error
    message: |-
      Validation must pass for DPF upgrade to continue:
        * Object Schema Validation:
          * storage.dpu.nvidia.com/v1alpha1, Kind=DPUStorageVendor:
            * dpf-operator-system/example has schema validation errors: [spec.pluginName: Required value]
          * svc.dpu.nvidia.com/v1alpha1, Kind=DPUDeployment:
            * dpf-operator-system/example has schema validation errors: [spec.dpus.dpuSetStrategy: Required value]
```

</details>

#### System Components Readiness

* The operator validates that all enabled DPF system components are ready for upgrade.

<details markdown="1"><summary>Example failure condition</summary>

```yaml
status:
  conditions:
  - type: PreUpgradeValidationReady
    status: "False"
    reason: Error
    message: |-
      Validation must pass for DPF upgrade to continue:
        * System Components:
          * dpuservice-controller: Deployment dpf-operator-system/dpuservice-controller-manager has 0 readyReplicas, want 1
```

</details>

#### Version Compatibility Validation

* **DPF Version Check**: Validates that the current DPF version is compatible with the target version
* **Supported Upgrade Path**: Ensures you are upgrading from a supported version (n-1 policy)

<details markdown="1"><summary>Example failure condition</summary>

```yaml
status:
  conditions:
  - type: PreUpgradeValidationReady
    status: "False"
    reason: Error
    message: |-
      Validation must pass for DPF upgrade to continue:
        * DPF version validation: DPF version v25.7.0 is not compatible with current DPF version v26.4.0, only upgrades from v25.10.0 are supported
```

</details>

### Validation Failure Handling

If any validation fails:

* **Upgrade Blocked**: The upgrade process is automatically halted
* **Status Updates**: The `DPFOperatorConfig` status reflects the validation failure

## Performing the Upgrade

Upgrade the DPF Operator using Helm. This assumes the DPF Helm repository is already configured as described in the
[Getting Started Guide](../../getting-started/dpf-host-trusted.md#deploy-the-dpf-operator) (repo setup, OCI registry
variants, and Helm prerequisites are covered there).

> [!IMPORTANT]
> Any custom values applied to the existing release must be preserved during the upgrade, or they will be reverted to
> the chart defaults. The recommended command below uses `--reset-then-reuse-values` (Helm 3.14 or later), which
> applies the new chart's defaults and then layers the existing release values on top.

```bash
helm repo update
helm upgrade --install -n dpf-operator-system dpf-operator \
  dpf-repository/dpf-operator --version=$TAG --reset-then-reuse-values
```

Replace `$TAG` with the target DPF Operator version (e.g. `v26.4.0`).

Alternatively, re-apply the original values file explicitly with `-f values.yaml`. If the file is no longer
available, the currently deployed values can be retrieved from the release:

```bash
helm get values dpf-operator -n dpf-operator-system > values.yaml
```

## Validate the Upgrade

Verify the controller has rolled out:

```bash
kubectl -n dpf-operator-system rollout status deployment dpf-operator-controller-manager
```

Confirm that `DPFOperatorConfig` reports the new version in its status (`status.version` should match `$TAG`):

```bash
kubectl -n dpf-operator-system get dpfoperatorconfig dpfoperatorconfig -o jsonpath='{.status.version}'
```

Or, using [`dpfctl`](../troubleshooting/dpfctl/README.md):

```bash
dpfctl describe all --show-resources=dpfoperatorconfig
```

## What to Do Next

After completing your DPF Operator upgrade, follow these steps to ensure everything is working correctly:

1. **Update to Supported BFB and DPUFlavor**: Create the new BFB object and DPUFlavor
   object [according to the new guide](../../user-guides/README.md). Review and update your DPUDeployment/DPUSet resources as needed
   to reference those objects to make the system compatible with the new operator version.

2. **Update DPUServices**: Review and update your DPUDeployment/DPUService resources to use services that are compatible with
   BFB version and operator version.

3. **Monitor the Upgrade**: Use `dpfctl describe all` to monitor the reconciliation process and ensure all resources
   reach ready state.
