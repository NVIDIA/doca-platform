# DPF System setup

DPF makes a number of assumptions about the hardware, software and networking of the machines it runs on. Some of the specific [use cases](guides/usecases/) add their own requirements.

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
- Open vSwitch (OVS) packages - i.e. `openvswitch-switch` for Ubuntu 24.04
- NFS client packages - i.e. ` nfs-common`
- NFS server available with `/mnt/dpf_share` readable and writable by any user

### Worker machines
- Open vSwitch (OVS) not installed
- NFS client packages - i.e. ` nfs-common`
- NFS server available with `/mnt/dpf_share` readable and writable by any user
- rshim package is not installed

### Kubernetes
- Kubernetes 1.30
- Control plane nodes have the labels `"node-role.kubernetes.io/control-plane" : ""`

## Network setup
- All nodes have full internet access - both from the host out-of-band and DPU high speed interfaces. 
- Virtual IP from the management subnet reserved for internal DPF usage.
- The out-of-band management and high-speed networks are routable to each other.
- The control plane nodes hosting the DPU control plane pods must be located on the same L2 broadcast domain.
- The out-of-band management fabric on which control plane nodes are connected should allow MultiCast traffic (used for VRRP).
