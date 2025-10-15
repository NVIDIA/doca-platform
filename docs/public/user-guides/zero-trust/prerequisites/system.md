---
title: "DPF System Prerequisites for Zero Trust"
---

[TOC]

DPF makes a number of assumptions about the hardware, software and networking of the machines it runs on. Some of the
specific [user guides](../README.md) add their own requirements.

## Hardware setup

There are high availability control plane machines running DPF and workload machines (TODO make better).

### Control plane machines

Each control plane machine:

* May be virtualized
* x86_64 architecture
* 16 GB RAM
* 8 CPUs
* DPUs are not installed

### Workload machines

Each workload machine has the following characteristics:

* Bare metal - no virtualization
* Any number of DPUs **but only one can be provisioned**

#### DPUs

* Bluefield 3
* 32 GB memory
* Flashed with NVIDIA BFB with DOCA version 2.5 or higher
* out-of-band management port must be connected to the management network

## System software setup

### Control plane machines

* NFS client packages - i.e. ` nfs-common`
* NFS server available with `/mnt/dpf_share` readable and writable by any user

### Kubernetes

* Kubernetes 1.31
* Control plane nodes have the labels `"node-role.kubernetes.io/control-plane" : ""`

## Network setup

* All nodes must have internet access to be able to pull images - included the DPUs.
* Virtual IP from the management subnet reserved for internal DPF usage.
* The DPU out-of-band physical interface must be connected with the DPF control planes.
* The control plane nodes hosting the DPU control plane pods must be located on the same L2 broadcast domain.
* The out-of-band management fabric on which control plane nodes are connected should allow MultiCast traffic (used for
  VRRP).
