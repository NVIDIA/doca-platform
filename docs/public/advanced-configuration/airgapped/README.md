---
title: "Air-Gapped"
---

[[_TOC_]]

This section covers running DPUs without direct internet access. Start with the deployment guide. Add the SOCKS proxy guide if the DPUs can reach a proxy but not the public registries.

## Documentation Sections

* [Air-Gapped Deployment Guide](deployment.md) - Run the DPUs of either deployment scenario without internet access: copy the images of a DPF release into a mirror registry inside your network and configure DPF to pull them from there.
* [Air-Gapped DPUs with a SOCKS Proxy](socks-proxy.md) - Letting DPUs without internet access pull images through a SOCKS5 proxy by configuring containerd with a DPUFlavor
