# Zero Trust State Machine — E2E Test Coverage & Retry Gaps

## Zero Trust (Redfish) Happy-Path State Machine

```
Initializing
    │
    ▼
  Pending  ─────────────────────────────────────────────────┐
    │                                                        │ (BFB/Flavor not ready → wait)
    ▼                                                        │
Node Effect                                                  │
    │                                                        │
    ├── (initial provision) ──────────────▶ Initialize Interface
    │                                            │
    │                                   ┌────────┤
    │                                   │        │
    │                             (NIC mode,     │ (Secure Boot mismatch)
    │                              need DPU      │
    │                              mode switch)  ▼
    │                                   │   Perform ARM Force Restart
    │                                   │        │
    │                                   │        │ (restarts done)
    │                                   ▼        ▼
    │                              Rebooting ──▶ Initialize Interface (verify)
    │                                                │
    │                                                ▼
    │                                       Config FW Parameters
    │                                                │
    │                                                ▼
    │                                           Prepare BFB
    │                                                │
    │                                                ▼
    │                                          OS Installing
    │                                                │
    │                                                ▼
    │                                            DPU Config
    │                                                │
    │                                   ┌────────────┤
    │                                   │            │
    │                            (RebootMethod     (NoAction)
    │                             ≠ NoAction)        │
    │                                   │            │
    │                                   ▼            │
    │                              Rebooting         │
    │                                   │            │
    │                                   ▼            ▼
    │                             DPU Cluster Config
    │                                        │
    ├── (post-provision node effect) ────────┤
    │                                        │
    │                                        ▼
    │                              Node Effect Removal
    │                                        │
    │                                        ▼
    └───────────────────────────────────   Ready
```

## Currently Covered in `system_test.go` (Domain.ZeroTrust)

### Provisioning Flow (BeforeEach / setup)


| What is tested                                        | Where                                                                        |
| ----------------------------------------------------- | ---------------------------------------------------------------------------- |
| DPUCluster creation + Ready phase                     | `VerifyDPUClusterWithNodes` via `system_setup.go`                            |
| BFB download + Ready phase                            | `ProvisionBFBAndDPUFlavor`                                                   |
| DPUSet creation → DPU objects created                 | `ProvisionDPUSet`                                                            |
| DPUNodeMaintenance hold → release                     | `ProcessDPUNodeMaintenanceHold`                                              |
| DPU reaches Rebooting phase                           | `RebootAndVerifyDPU` (waits for `DPURebooting`)                              |
| External host reboot via script                       | `RebootAndVerifyDPU` (calls `RebootHostByScript`)                            |
| DPUNode annotation removal post-reboot                | `RebootAndVerifyDPU`                                                         |
| DPU reaches Ready                                     | `VerifyDPUClusterWithNodes`                                                  |
| DPUServices deployed on DPU cluster                   | `VerifyDPUServicesDeployed` / `VerifyClusterPods`                            |
| DPFOperatorConfig Ready                               | `VerifyDPFOperatorConfigReady`                                               |
| DPU Discovery + DPUDevice creation                    | `CreateDPUDiscovery`                                                         |
| BFB registry service + pods                           | `DeployDPFSystemComponents` (ZT-specific check)                              |
| DPU fails instantly on `DPUError` during provisioning | `RebootAndVerifyDPU` (`Expect(current.Status.Phase).NotTo(Equal(DPUError))`) |

## Flows That Require Retry — Candidates for New Zero Trust Tests

These are retry/error-recovery scenarios implemented in the controller code but **not exercised** by the current E2E tests. All are relevant exclusively or primarily to zero trust mode.

### 1. BFB Download Retry (Error → Downloading)

**Code**: `internal/provisioning/controllers/bfb/state/error.go`

The BFB controller retries transient download failures (HTTP errors, network timeouts) within a 6-hour window, waiting at least 10 minutes between attempts. Only `ReasonError` (transient) triggers retry; `ReasonFailure` (terminal, e.g., corrupt file) does not.

**Suggested test**: Serve a BFB URL that returns HTTP 503 once, then succeeds. Verify the BFB transitions `Downloading → Error → Downloading → Ready`.

### 2. Redfish OS Install Failure → DPUError (no retry from Installing)

**Code**: `internal/provisioning/controllers/dpu/state/redfish/installing.go`

When `InstallBFB` fails (bad registry address, HTTP error, Redfish task `Exception`), the DPU goes directly to `DPUError` with **no automatic retry**. The code has an explicit comment: *"the current Installing phase has no retry mechanism"*.

**Suggested test**: Trigger an install failure (e.g., unreachable BFB registry, malformed BFB URL) and verify the DPU reaches `DPUError` with the correct condition reason. Then verify that manual intervention (e.g., fixing the BFB + resetting the DPU phase) can recover the flow.

### 3. Secure Boot Configuration → ARM Force Restart Cycle

**Code**: `internal/provisioning/controllers/dpu/state/redfish/initialize_interface.go`, `perform_arm_force_restart.go`

When Secure Boot spec doesn't match BMC state, the flow stages ARM force restarts:

- `InitializeInterface` detects mismatch → creates tracker → `PerformArmForceRestart`
- Executes N restarts with 90s minimum interval → returns to `InitializeInterface` for verification
- On success: clears tracker, continues to `ConfigFWParameters`
- On safety limit exceeded (10 restarts): `DPUError`
- On verification timeout (stale tracker): `DPUError`

**Suggested tests**:

- **Happy path**: Set `spec.secureBoot: true` on a DPU with Secure Boot disabled. Verify the DPU cycles through `InitializeInterface → PerformArmForceRestart → InitializeInterface → ConfigFWParameters`.
- **Safety limit**: Verify that exceeding `MaxSafetyLimit` (10) results in `DPUError` with reason `MaxSafetyLimitExceeded`.
- **Stale tracker**: Verify that a corrupted/stale tracker is cleared and the DPU returns to `InitializeInterface` for re-detection.

### 4. NIC Mode → DPU Mode Switch Requiring Host Reboot

**Code**: `internal/provisioning/controllers/dpu/state/redfish/initialize_interface.go`

When the DPU is in NIC mode (not DPU mode), the controller requests a mode switch and transitions to `DPURebooting` (host power cycle required). After reboot, the DPU returns to `InitializeInterface` to verify the mode change succeeded.

**Suggested test**: Start with a DPU in NIC mode. Verify the flow `InitializeInterface → DPURebooting → InitializeInterface → ConfigFWParameters`. Verify the reboot annotation is set and cleared correctly.

### 5. Redfish "Another Update in Progress" (HTTP 400) Wait-and-Retry

**Code**: `internal/provisioning/controllers/dpu/state/redfish/installing.go` (lines ~134–136)

When a Redfish `InstallBFB` call gets HTTP 400 with "Another update is in progress", the controller returns without error, relying on the 5s requeue interval to retry later.

**Suggested test**: Simulate a concurrent update scenario. Verify the DPU stays in `OSInstalling` phase (not `DPUError`) and eventually proceeds once the concurrent update completes.

### 6. BMC Connectivity Transient Failures (Redfish Client Errors)

**Code**: Multiple locations in `redfish/` handlers

All Redfish client errors (GetSecureBoot, GetSystem, ForceRestartDPUArm, CheckTaskProgress) return `err` to trigger controller-runtime rate-limited requeue. These are designed to self-heal when the BMC becomes reachable again.

**Suggested test**: Temporarily make the BMC unreachable during `InitializeInterface` or `OSInstalling`. Verify the DPU stays in the same phase (not `DPUError`) and resumes when connectivity is restored.

### 7. Bootstrap Token Reuse / Expiration

**Code**: `internal/provisioning/controllers/util/zt_bootstrap.go`

Bootstrap tokens are created per DPU with ~4h TTL. If a valid token exists, it is reused. If creation races (`AlreadyExists`), the existing token is fetched.

**Suggested test**: Trigger a re-provisioning of a DPU (e.g., BFB update) and verify that the bootstrap token is reused if still valid, or a new one is created if expired.

## Priority Ranking

| Priority | Test                                          | Reason                                                                     |
|:--------:|-----------------------------------------------|----------------------------------------------------------------------------|
| **P0**   | #2 — OS install failure → Error               | Most common real-world failure; no retry means operator must intervene     |
| **P0**   | #1 — BFB download retry                       | Flaky networks are common; retry logic is complex (6h window, 10m backoff) |
| **P1**   | #3 — Secure Boot ARM restart                  | New feature, multi-step with safety limits; high blast radius if broken   |
| **P1**   | #6 — BMC transient failures                   | Most common infra failure; validates self-healing vs. false-positive errors |
| **P2**   | #4 — NIC→DPU mode switch reboot               | Covers the mode-switch + reboot + re-enter flow                           |
| **P2**   | #5 — Concurrent update wait                   | Edge case but can cause install failures in busy environments             |
| **P3**   | #7 — Bootstrap token reuse                    | Lower risk; token creation is idempotent                                  |
