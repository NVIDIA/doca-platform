---
title: "Host Installation for non-Kubernetes Environments"
---

[[_TOC_]]

## Installation Steps

### 1. Install DPF

Select a use case from the [host trusted use cases](../../user-guides/host-trusted/README.md#use-cases) and complete all prerequisite steps before provisioning.

> [!NOTE]
> The DPU Detector is not used in non-Kubernetes environments. After creating `DPFOperatorConfig`, you can ignore any related errors, or [disable the DPU Detector](../../developer-guides/api/dpfoperatorconfig.md#optional-component-configurations) by setting `dpuDetector.disable=true`.

### 2. Create Bootstrap Token 
For security reasons, it’s recommended that the host agent join the cluster using a bootstrap token. The following commands create a bootstrap token that expires in 24 hours and use that token to generate a kubeconfig. 

Run the following commands on any of the **control plane nodes**
```bash
export TOKEN_SUFFIX=$(cat /dev/urandom | tr -dc 'a-z0-9' | fold -w 6 | head -n 1)
export TOKEN_SECRET=$(cat /dev/urandom | tr -dc 'a-z0-9' | fold -w 16 | head -n 1)
export CLUSTER_NAME="cluster.local"
export BOOTSTRAP_TOKEN="$TOKEN_SUFFIX.$TOKEN_SECRET"
export ADMIN_KUBECONFIG_FILE=${HOME}/.kube/config
export BOOTSTRAP_KUBECONFIG_FILE="bootstrap.kubeconfig"

# create a token that expires in 24 hours
kubectl create secret generic bootstrap-token-$TOKEN_SUFFIX \
  --type=bootstrap.kubernetes.io/token \
  --namespace=kube-system \
  --from-literal=description="Bootstrap token for host-agent" \
  --from-literal=token-id=$TOKEN_SUFFIX \
  --from-literal=token-secret=$TOKEN_SECRET \
  --from-literal=expiration=$(date -u -d '+24 hours' +%Y-%m-%dT%H:%M:%SZ) \
  --from-literal=usage-bootstrap-authentication=true \
  --from-literal=usage-bootstrap-signing=true \
  --from-literal=auth-extra-groups=system:bootstrappers:dpf:host-agent

# create a kubeconfig file out of the token and the admin kubeconfig
cp ${ADMIN_KUBECONFIG_FILE} $BOOTSTRAP_KUBECONFIG_FILE
kubectl config unset --kubeconfig=$BOOTSTRAP_KUBECONFIG_FILE current-context
kubectl config unset --kubeconfig=$BOOTSTRAP_KUBECONFIG_FILE users
kubectl config unset --kubeconfig=$BOOTSTRAP_KUBECONFIG_FILE contexts
kubectl config set-credentials bootstrap-user \
  --token=$BOOTSTRAP_TOKEN \
  --kubeconfig=$BOOTSTRAP_KUBECONFIG_FILE
kubectl config set-context bootstrap-context \
  --cluster=$CLUSTER_NAME \
  --user=bootstrap-user \
  --kubeconfig=$BOOTSTRAP_KUBECONFIG_FILE
kubectl config use-context bootstrap-context \
  --kubeconfig=$BOOTSTRAP_KUBECONFIG_FILE
```

### 3. Distribute The Kubeconfig File
Distribute the generated kubeconfig file under /var/lib/dpf/hostagent/ of each worker node.

### 4. Install DOCA and containerd
Run the following commands on worker host.

```bash
# Set DOCA repository URL
export DOCA_URL="https://linux.mellanox.com/public/repo/doca/3.2.1/ubuntu24.04/x86_64/"

# Add NVIDIA Mellanox GPG key
curl https://linux.mellanox.com/public/repo/doca/GPG-KEY-Mellanox.pub | sudo gpg --dearmor > /etc/apt/trusted.gpg.d/GPG-KEY-Mellanox.pub

# Add DOCA repository
echo "deb [signed-by=/etc/apt/trusted.gpg.d/GPG-KEY-Mellanox.pub] $DOCA_URL ./" | sudo tee /etc/apt/sources.list.d/doca.list

# Update and install required packages
sudo apt-get update
sudo apt-get -y install doca-all doca-networking containerd
```

### 5. Pull The HostDriver Image From NGC
Run the following commands on worker host.

```bash
export IMAGE=<the hostdriver image>
export IMAGE_PULL_KEY=<your NGC key granted permission for pulling image>
sudo ctr image pull --user \$oauthtoken:$IMAGE_PULL_KEY  $IMAGE
```

### 6. Initialize Host Agent Flags
Before you run the following commands, you need to set the BFB_REGISTRY_ADDRESS with the same value as you set in [DPFOperatorConfig.spec.provisioningController.registry](../../developer-guides/api/api.md#registryconfiguration)

Run the following commands on worker host.
```bash
export BFB_REGISTRY_ADDRESS=<address of the bfb-registry>
export HOST_AGENT_DIR=/var/lib/dpf/hostagent
export HOST_AGENT_ENV_FILE=$HOST_AGENT_DIR/dpf-host-agent.env

sudo mkdir -p $HOST_AGENT_DIR
sudo tee $HOST_AGENT_ENV_FILE >/dev/null <<EOF
BFB_REGISTRY_ADDRESS="$BFB_REGISTRY_ADDRESS"
HOST_AGENT_CONTAINER_NAME="dpf-host-agent"
DMSD_CONTAINER_NAME="dpf-dmsd"
RUN_CONTAINER_ARGS="--rm --net-host --privileged --mount type=bind,src=/var/run/dbus/system_bus_socket,dst=/var/run/dbus/system_bus_socket,options=rbind:rw --mount type=bind,src=/tmp,dst=/tmp,options=rbind:rw --mount type=bind,src=/etc/netplan,dst=/etc/netplan,options=rbind:rw --mount type=bind,src=/run/systemd,dst=/run/systemd,options=rbind:rw --mount type=bind,src=/run/udev,dst=/run/udev,options=rbind:rw  --mount type=bind,src=/usr/lib/systemd/network,dst=/usr/lib/systemd/network,options=rbind:rw --mount type=bind,src=/proc,dst=/proc,options=rbind:rw --mount type=bind,src=/dev,dst=/dev,options=rbind:rw --mount type=bind,src=/sys,dst=/sys,options=rbind:rw --mount type=bind,src=/lib/modules,dst=/lib/modules,options=rbind:ro --mount type=bind,src=/var/lib/dpf,dst=/var/lib/dpf,options=rbind:rw $IMAGE"
EOF
```

### 7. Start rshim and DMS

Run the following commands on worker host.
```bash
export DMSD_SERVICE_FILE=/etc/systemd/system/dpf-dmsd.service
sudo tee $DMSD_SERVICE_FILE >/dev/null <<EOF
[Unit]
Description=DMS server
Wants=network.target
After=network-pre.target

[Service]
Type=exec
EnvironmentFile=$HOST_AGENT_ENV_FILE
ExecStartPre=-/usr/bin/ctr snapshot delete \$DMSD_CONTAINER_NAME 
ExecStartPre=-/usr/bin/ctr container delete \$DMSD_CONTAINER_NAME 
ExecStart=/usr/bin/ctr run \$RUN_CONTAINER_ARGS \$DMSD_CONTAINER_NAME /hostagent rundms

TimeoutStopSec=30

Restart=on-failure
RestartSec=5

StartLimitIntervalSec=120
StartLimitBurst=5

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl enable rshim
sudo systemctl start rshim
sudo systemctl enable dpf-dmsd
sudo systemctl start dpf-dmsd
```


### 8. Start Host Agent With Systemd and containerd
Run the following commands on worker host.

```bash
export HOST_AGENT_SERVICE_FILE=/etc/systemd/system/dpf-host-agent.service
sudo tee $HOST_AGENT_SERVICE_FILE >/dev/null <<EOF
[Unit]
Description=DPF Host Agent
Wants=network.target
After=network-pre.target

[Service]
Type=exec
EnvironmentFile=$HOST_AGENT_ENV_FILE
ExecStartPre=-/usr/bin/ctr snapshot delete \$HOST_AGENT_CONTAINER_NAME 
ExecStartPre=-/usr/bin/ctr container delete \$HOST_AGENT_CONTAINER_NAME 
ExecStart=/usr/bin/ctr run \$RUN_CONTAINER_ARGS \$HOST_AGENT_CONTAINER_NAME /hostagent serve --bootstrap-kubeconfig=/var/lib/dpf/hostagent/bootstrap.kubeconfig --kubeconfig=/var/lib/dpf/hostagent/kubeconfig --bfb-registry-address=\${BFB_REGISTRY_ADDRESS} -v 3

TimeoutStopSec=30

Restart=on-failure
RestartSec=5

StartLimitIntervalSec=120
StartLimitBurst=5

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl enable dpf-host-agent
sudo systemctl start dpf-host-agent
```

## Verification

To verify the installation is working correctly, run the following commands on worker host.

```bash
# Check services status
sudo systemctl status rshim
sudo systemctl status dpf-dmsd 
sudo systemctl status dpf-host-agent
```

## External Host Reboot

After the worker node has rebooted, remove the `provisioning.dpu.nvidia.com/dpunode-external-reboot-required` annotation
from the DPUNode using kubectl:

```bash
kubectl annotate dpunode <node-name> provisioning.dpu.nvidia.com/dpunode-external-reboot-required-
```

This step is only required for hosts that are not managed by Kubernetes.
