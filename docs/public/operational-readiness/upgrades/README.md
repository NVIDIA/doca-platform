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

* Version Compatibility Validation
    * **DPF Version Check**: Validates that the current DPF version is compatible with the target version
    * **Supported Upgrade Path**: Ensures you are upgrading from a supported version (n-1 policy)
* System Components Readiness
    * The operator validates that all critical system components are ready for upgrade
* DPU State Validation
    * **DPU Readiness**: All DPUs must be in a ready state
    * **DPF Version Compatibility**: Each DPU's DPF version must be compatible with the upgrade
    * **DPU Health**: All DPUs must be healthy and operational
* Object Schema Validation
    * All existing DPF API objects are validated against the OpenAPI schema of the installed CRDs. If a field that was previously optional has become required in the new version, any objects missing that field are reported and the upgrade is blocked until they are corrected.
    * Violations are reported via a `PreUpgradeValidationReady` status condition on `DPFOperatorConfig`. While this condition is `False`, the `Ready` condition also remains `False` and the operator does not proceed to upgrade its managed components.
    * **Example condition when validation fails:**
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
    * **Required action when present:** Fix the schema violations on the listed resources. Once all objects pass validation, `PreUpgradeValidationReady` transitions to `True` and the upgrade proceeds automatically.

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
