---
title: "DPF System Prerequisites for Host Trusted"
---

[TOC]

DPF makes a number of assumptions about the hardware, software and networking of the machines it runs on. Some of the
specific [user guides](../README.md) add their own requirements.

## Hardware Setup

There is a high availability control plane machines serving many worker nodes in a cluster running DPF.

### Control Plane Machines

Each control plane machine:

* May be virtualized
* x86_64 architecture
* 16 GB RAM
* 8 CPUs
* DPUs are not installed

### Worker Machines

Each worker machine:

* Bare metal - no virtualization
* x86_64 architecture
* 16 GB RAM
* 8 CPUs
* Any number of DPUs

#### DPUs

* Bluefield 3
* 32 GB memory
* Flashed with NVIDIA BFB with DOCA version 2.5 or higher
* Out-of-band management port is not used

## System Software Setup

### Control Plane Machines

* NFS client packages - i.e. ` nfs-common`
* NFS server available with `/mnt/dpf_share` readable and writable by any user

### Worker Machines

* `In-Band Manageability Interface` enabled in BIOS
* NFS client packages - i.e. ` nfs-common`
* NFS server available with `/mnt/dpf_share` readable and writable by any user
* rshim package is not installed

### Kubernetes

* Kubernetes 1.32 - 1.34
* Control plane nodes have the labels `"node-role.kubernetes.io/control-plane" : ""`

## Network Setup

* All nodes have full internet access - both from the host out-of-band and DPU high speed interfaces
* Virtual IP from the management subnet reserved for internal DPF usage
* The out-of-band management and high-speed networks are routable to each other
* The control plane nodes hosting the DPU control plane pods must be located on the same L2 broadcast domain
* The out-of-band management fabric on which control plane nodes are connected should allow MultiCast traffic (used for
  VRRP)
