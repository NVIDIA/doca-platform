---
title: "SNAP DMA SF configuration (BlueField-4)"
---

# BF4 SNAP DMA SF configuration

On BlueField-4 socket-direct systems the SNAP DOCA service needs a **DMA SF** —
a dedicated SF with the well-known `sfnum 8000`, created on the ibdev-less ECPF
of the second Grace PCI link — so it can DMA host memory over both PCI links.

DPF creates this SF from the **dpu-agent** instead of the vendor
`mlnx_bf_configure` script, so creation is opt-in, deterministic, and reported
per-DPU as a status condition. Two knobs configure it, split by ownership:

| Setting | Where | Owner | Meaning |
|---|---|---|---|
| `spec.dma.enabled: true` | DPUFlavor field | DPF | Opts the **dpu-agent** into creating the DMA SF. Ignored on any non-BlueField-4 DPU. |
| `SNAP_DMA_SF="no"` | `mlnx-bf.conf` configFile | vendor (`mlnx_bf_configure`) | Pins the **vendor** DMA SF creation off (its default is `auto`), so the vendor and the agent do not both create it. |

## Example

```yaml
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUFlavor
spec:
  # Create the SNAP DMA SF from the dpu-agent. The agent owns the sfnum (8000).
  dma:
    enabled: true
  configFiles:
    - path: /etc/mellanox/mlnx-bf.conf
      operation: override
      permissions: "0644"
      raw: |
        ENABLE_ESWITCH_MULTIPORT="yes"

        # Pin the vendor DMA SF creation off; the agent may create it instead.
        SNAP_DMA_SF="no"
```

## Notes

- Requires `ENABLE_ESWITCH_MULTIPORT="yes"` (multi-port eswitch) and a
  socket-direct BF4 topology (ECPFs spread across two Grace PCI links). The
  agent auto-selects the target ECPF (the ibdev-less one on the second PCI
  link) and creates exactly one DMA SF. If `spec.dma.enabled` is set but no
  eligible target exists, the agent fails visibly rather than degrading
  silently.
- `sfnum 8000` is a SNAP discovery ABI, so the agent owns it: `spec.dma.enabled` is
  the only knob, and the DMA SF is not expressed as a `spec.scalableFunctions`
  group. Its MAC is a deterministic derivation over the ECPF address and sfnum.
  Workload SFs (pools, `count`, `device`) are declared separately; see
  [ScalableFunction](../../../../developer-guides/api/dpuflavor.md#scalablefunction).
  (on BlueField-4 with DMA, size workload `count` as `PF_TOTAL_SF - 1`) to accomodate this SF.
- Secondary-ECPF silencing (`ENABLE_SD_MERGED_ESWITCH`) and the
  `/run/mellanox/snap_emulation_managers` export remain the vendor script's
  responsibility; DPF only owns DMA SF creation.
