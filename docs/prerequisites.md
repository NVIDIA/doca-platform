# DPF System setup

DPF makes a number of assumptions about the hardware, software and networking of the machines it runs on. Some of the specific [use cases](./usecases/) add their own requirements.

## Hardware setup
There are 3 control plane machines serving many worker nodes in a cluster running DPF.

### Control plane machines
Each control plane machine:
- May be virtualized
- x86_64 architecture
- 16 GB RAM
- 8 CPUs
- DPUs are not installed

### Worker machines
Each worker machine:
- Bare metal - no virtualization
- x86_64 architecture
- 16 GB RAM
- 8 CPUs
- Exactly one DPU

#### DPUs
- Bluefield 3
- 32 GB memory
- out-of-band management port is not used

## System software setup

### Control plane machines
- OVS packages - i.e. `ovs-common`, `openvswitch-switch` for Ubuntu 24.04
- NFS packages - i.e. ` nfs-kernel-server`
- NFS server available with `/mnt/dpf_share` readable and writable by any user

### Worker machines
- OVS not installed
- `ipmitool` installed
- DPU P0 must have DHCP enabled
- rshim is not installed

### Kubernetes
- Kubernetes 1.31

## Network setup
- All nodes have full internet access - both from the host out-of-band and DPU high speed interfaces. 

### Control plane machines
- Virtual IP from the management subnet reserved for internal DPF usage.
### Worker machines
- Both the oob and high-speed fabric on each node is routable
- Provisioned with `KUBELET_ADDRESS="--node-ip=0.0.0.0"`
