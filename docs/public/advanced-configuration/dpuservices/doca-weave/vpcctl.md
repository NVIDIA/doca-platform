---
title: vpcctl CLI Tool
---

# vpcctl

A command-line interface tool for interacting locally with the Weave NetworkIsolation gRPC service. it is packaged as part of the DOCA weave image and can be executed from each DPU by exec from the corresponding `weave-flow-controller` pod.

## Overview

`vpcctl` provides a simple CLI interface to manage virtual networks and attachments through the NetworkIsolation gRPC service. It allows you to create, update, delete, get, and list virtual networks and their attachments.

**gRPC API shape:** Virtual network and attachment resources use a **spec/status** structure. The `spec` holds the desired configuration. The `status` holds server-managed state (phase, reason, etc.). Create RPCs take a **spec** in the request; Get/List responses return the full resource with both `spec` and `status`.

## Configuration

### gRPC Target

The gRPC service is addressed via a `--target` endpoint in one of the following forms:

- `unix:///absolute/path/to.sock`
- `tcp://host:port`

The target can be configured in the following ways (in order of precedence):

1. **Command-line flag**: `--target=unix:///path/to/socket` or `--target=tcp://host:port`
2. **Environment variable**: `VPCCTL_TARGET=unix:///path/to/socket`
3. **Default**:
   - NetworkIsolation commands: `unix:///var/run/dpf/weave/grpc/flow-controller.sock`
   - DHCP commands: `unix:///var/run/dpf/weave/grpc/dhcp.sock`, falling back to `tcp://127.0.0.1:50222` when that socket does not exist on disk

### DHCP Auth Token File

DHCP commands require an auth token that is read from a file on disk. The token
file path can be configured in the following ways (in order of precedence):

1. **Command-line flag**: `--dhcp-token-file=/path/to/token`
2. **Environment variable**: `DHCP_AUTH_TOKEN_PATH=/path/to/token`
3. **Default**: `/var/run/dpf/weave/grpc/weave-dhcp-agent.token`

The token is transparently attached to every DHCP gRPC call as a bearer token.

## Commands

### Virtual Network Commands

#### create-vnet

Create a new virtual network.

```bash
vpcctl create-vnet [--id <id>] --vni <number> --subnet-v4 <cidr> [--user-data <key=value>...]
```

| Flag | Required | Description |
|------|----------|-------------|
| `--id` | No | Unique identifier for the virtual network (server generates if not set) |
| `--vni` | Yes | VXLAN Network Identifier (1-1048575) |
| `--subnet-v4` | Yes | IPv4 subnet in CIDR notation |
| `--user-data` | No | Arbitrary `key=value` pairs stored in `metadata.user_data`. Repeatable, or comma-separated (e.g. `--user-data k1=v1,k2=v2`) |

#### update-vnet

Update the user data of an existing virtual network.

```bash
vpcctl update-vnet --id <id> (--user-data <key=value>... | --clear-user-data) [--resource-version <version>]
```

| Flag | Required | Description |
|------|----------|-------------|
| `--id` | Yes | Virtual network identifier to update |
| `--user-data` | One of | **Replaces** `metadata.user_data` with these `key=value` pairs. Repeatable, or comma-separated (e.g. `--user-data k1=v1,k2=v2`) |
| `--clear-user-data` | One of | Removes all `metadata.user_data` entries |
| `--resource-version` | No | Sets `metadata.resource_version` as an optimistic concurrency precondition. If unset, the server uses the version it reads while serving the request |

> **Note:** `--user-data` replaces the stored map in its entirety, it does not merge into it. Keys that are not passed are removed. Exactly one of `--user-data` or `--clear-user-data` must be provided, so that clearing user data is always explicit.

#### delete-vnet

Delete an existing virtual network.

```bash
vpcctl delete-vnet --id <id>
```

| Flag | Required | Description |
|------|----------|-------------|
| `--id` | Yes | Virtual network identifier to delete |

#### get-vnet

Retrieve details of a virtual network.

```bash
vpcctl get-vnet --id <id>
```

| Flag | Required | Description |
|------|----------|-------------|
| `--id` | Yes | Virtual network identifier to retrieve |

#### list-vnet

List all virtual networks.

```bash
vpcctl list-vnet [--vni <number>]
```

| Flag | Required | Description |
|------|----------|-------------|
| `--vni` | No | Filter by VXLAN Network Identifier |

### Attachment Commands

#### create-attachment

Create a new virtual network attachment.

```bash
vpcctl create-attachment [--id <id>] --vnet-id <id> --type <pf|vf|ovs> (--nic-id <id> --pf <mac> [--vf <index>] | --rep <rep-netdev>) [--user-data <key=value>...] [--ovs-bridge-name <name>] [--ovn-network-name <name>]
```

| Flag | Required | Description |
|------|----------|-------------|
| `--id` | No | Unique identifier for the attachment (server generates if not set) |
| `--vnet-id` | Yes | Virtual network identifier |
| `--nic-id` | Yes (unless `--rep` provided for type=pf) | NIC identifier |
| `--type` | Yes | Attachment type: `pf` (Physical Function), `vf` (Virtual Function), or `ovs` (OVS bridge) |
| `--pf` | If type=pf or vf (unless `--rep` provided) | PF identifier (MAC address), required for `pf` and `vf` |
| `--vf` | If type=vf | VF index (required when type is `vf`) |
| `--rep` | No (only for type=pf) | Representor netdev of the host attachment. When set, `--nic-id`, and `--pf` are derived from the matching devlink port and must not be provided. |
| `--user-data` | No | Arbitrary `key=value` pairs stored in `metadata.user_data`. Repeatable, or comma-separated (e.g. `--user-data k1=v1,k2=v2`) |
| `--ovs-bridge-name` | If type=ovs | OVS bridge name to connect to via a patch port (max 32 chars); the bridge is created if it does not exist; required when type is `ovs` |
| `--ovn-network-name` | No (only for type=ovs) | OVN network name (letters, digits, `-`, `_` only). When set, an `ovn-bridge-mappings` entry mapping this network name to `--ovs-bridge-name` is programmed so ovn-controller can attach to the bridge |

#### update-attachment

Update the user data of an existing attachment.

```bash
vpcctl update-attachment --id <id> (--user-data <key=value>... | --clear-user-data) [--resource-version <version>]
```

| Flag | Required | Description |
|------|----------|-------------|
| `--id` | Yes | Attachment identifier to update |
| `--user-data` | One of | **Replaces** `metadata.user_data` with these `key=value` pairs. Repeatable, or comma-separated (e.g. `--user-data k1=v1,k2=v2`) |
| `--clear-user-data` | One of | Removes all `metadata.user_data` entries |
| `--resource-version` | No | Sets `metadata.resource_version` as an optimistic concurrency precondition. If unset, the server uses the version it reads while serving the request |

> **Note:** `--user-data` replaces the stored map in its entirety, it does not merge into it. Keys that are not passed are removed. Exactly one of `--user-data` or `--clear-user-data` must be provided, so that clearing user data is always explicit.

#### delete-attachment

Delete an existing attachment.

```bash
vpcctl delete-attachment --id <id>
```

| Flag | Required | Description |
|------|----------|-------------|
| `--id` | Yes | Attachment identifier to delete |

#### get-attachment

Retrieve details of an attachment.

```bash
vpcctl get-attachment --id <id>
```

| Flag | Required | Description |
|------|----------|-------------|
| `--id` | Yes | Attachment identifier to retrieve |

#### list-attachment

List all attachments.

```bash
vpcctl list-attachment [--vnet-id <id>] [--nic-id <id>]
```

| Flag | Required | Description |
|------|----------|-------------|
| `--vnet-id` | No | Filter by virtual network identifier |
| `--nic-id` | No | Filter by NIC identifier |

### DHCP Commands

#### get-dhcp-config

Retrieve the current DHCP agent configuration.

```bash
vpcctl get-dhcp-config
```

#### set-dhcp-config

Apply a DHCP agent configuration from a JSON file or from stdin. The input must
be the protobuf JSON representation of `DHCPAgentConfig`.

```bash
# From file
vpcctl set-dhcp-config --config <path>

# From stdin (JSON config piped or redirected)
cat config.json | vpcctl set-dhcp-config --config -
vpcctl set-dhcp-config --config - < config.json
```

| Flag | Required | Description |
|------|----------|-------------|
| `--config` | Yes | Path to the DHCP config JSON file, or `-` to read JSON from stdin |

## Examples

### Using with a custom target

```bash
# Via flag (unix socket)
vpcctl --target=unix:///tmp/weave/flow-controller.sock list-vnet

# Via environment variable
export VPCCTL_TARGET=unix:///tmp/weave/flow-controller.sock
vpcctl list-vnet
```

### Using with a custom DHCP token file

```bash
# Via flag
vpcctl --dhcp-token-file=/tmp/weave-dhcp-agent.token get-dhcp-config

# Via environment variable
export DHCP_AUTH_TOKEN_PATH=/tmp/weave-dhcp-agent.token
vpcctl get-dhcp-config
```

### Creating a virtual network

```bash
# With explicit ID
vpcctl \
    create-vnet \
    --id "my-vnet-001" \
    --vni 100 \
    --subnet-v4 "10.0.0.0/24"

# Without ID (server will generate one)
vpcctl \
    create-vnet \
    --vni 100 \
    --subnet-v4 "10.0.0.0/24"

# With user-data (repeatable form)
vpcctl \
    create-vnet \
    --vni 100 \
    --subnet-v4 "10.0.0.0/24" \
    --user-data owner=alice \
    --user-data env=dev

# With user-data (comma-separated form)
vpcctl \
    create-vnet \
    --vni 100 \
    --subnet-v4 "10.0.0.0/24" \
    --user-data owner=alice,env=dev
```

Output:
```json
{
  "virtualNetwork": {
    "spec": {
      "id": "my-vnet-001",
      "vni": 100,
      "subnetIpv4": "10.0.0.0/24"
    },
    "status": {
      "state": {
        "phase": "PHASE_READY"
      }
    }
  }
}
```

### Creating a PF attachment

```bash
vpcctl \
    create-attachment \
    --id "att-pf-001" \
    --vnet-id "my-vnet-001" \
    --nic-id "00:11:22:33:44:55" \
    --type pf \
    --pf "00:11:22:33:44:55" \
    --user-data owner=alice,team=net
```

Output:
```json
{
  "virtualNetworkAttachment": {
    "spec": {
      "id": "att-pf-001",
      "vnetId": "my-vnet-001",
      "nicId": "00:11:22:33:44:55",
      "attachmentType": "ATTACHMENT_TYPE_PF",
      "attachmentPf": {
        "pfId": "00:11:22:33:44:55"
      }
    },
    "status": {
      "state": {
        "phase": "PHASE_READY"
      },
      "hostIpv4": "10.0.0.1"
    }
  }
}
```

### Creating a VF attachment

```bash
vpcctl \
    create-attachment \
    --vnet-id "my-vnet-001" \
    --nic-id "00:11:22:33:44:55" \
    --type vf \
    --pf "00:11:22:33:44:55" \
    --vf 0
```

### Creating a PF attachment from a representor netdev

Instead of passing `--nic-id` and `--pf` explicitly, a representor netdev can
be provided via `--rep`. The NIC id and PF id are derived from it:

```bash
# PF attachment derived from a PF representor
vpcctl \
    create-attachment \
    --vnet-id "my-vnet-001" \
    --type pf \
    --rep "A1c1pf0"
```

### Creating an OVS (`ovs`) attachment

```bash
vpcctl \
    create-attachment \
    --vnet-id "my-vnet-001" \
    --nic-id "00:11:22:33:44:55" \
    --type ovs \
    --ovs-bridge-name "my-ovs-br" \
    --ovn-network-name "my-ovn-net"
```

`--ovn-network-name` is optional; when omitted the attachment is a pure patch-port connection with no ovn-bridge-mappings entry.

### Listing and filtering

```bash
# List all virtual networks
vpcctl list-vnet

# List virtual networks with specific VNI
vpcctl list-vnet --vni 100

# List all attachments
vpcctl list-attachment

# List attachments for a specific virtual network
vpcctl list-attachment --vnet-id "my-vnet-001"

# List attachments for a specific NIC
vpcctl list-attachment --nic-id "00:11:22:33:44:55"

# List attachments with multiple filters
vpcctl list-attachment --vnet-id "my-vnet-001" --nic-id "00:11:22:33:44:55"
```

### Updating user data

```bash
# Replace the user-data of a virtual network. Keys that are not passed are removed
vpcctl update-vnet --id "my-vnet-001" --user-data owner=bob,env=prod

# Remove all user-data from a virtual network
vpcctl update-vnet --id "my-vnet-001" --clear-user-data

# Update with an optimistic concurrency precondition
vpcctl update-vnet --id "my-vnet-001" --user-data owner=bob --resource-version 3

# Replace the user-data of an attachment
vpcctl update-attachment --id "att-pf-001" --user-data owner=bob

# Remove all user-data from an attachment
vpcctl update-attachment --id "att-pf-001" --clear-user-data

# Update an attachment with an optimistic concurrency precondition
vpcctl update-attachment --id "att-pf-001" --user-data owner=bob --resource-version 2
```

To add a key while preserving the existing ones, read the current user-data first and pass the merged result:

```bash
vpcctl get-vnet --id "my-vnet-001" | jq '.virtualNetwork.metadata.userData'
vpcctl update-vnet --id "my-vnet-001" --user-data owner=alice,env=dev,team=net
```

### Getting and deleting resources

```bash
# Get a virtual network
vpcctl get-vnet --id "my-vnet-001"

# Get an attachment
vpcctl get-attachment --id "att-pf-001"

# Delete an attachment
vpcctl delete-attachment --id "att-pf-001"

# Delete a virtual network
vpcctl delete-vnet --id "my-vnet-001"
```

### Getting and setting DHCP agent configuration

```bash
# Get dhcp configuration (token read from default path)
vpcctl get-dhcp-config

# Get dhcp configuration with an explicit token file
vpcctl --dhcp-token-file=/path/to/weave-dhcp-agent.token get-dhcp-config

# Get dhcp configuration via environment variable
export DHCP_AUTH_TOKEN_PATH=/path/to/weave-dhcp-agent.token
vpcctl get-dhcp-config

# Get dhcp configuration with custom target
vpcctl --target tcp://127.0.0.1:50222 get-dhcp-config

# Set DHCP configuration
vpcctl set-dhcp-config --config /tmp/my-config.json
```

Sample dhcp configuration:
```json
{
    "interfaceConfigs": [
        {
            "name": "nic1",
            "mac": "fe:00:10:00:00:00",
            "ipv4Subnet": "10.0.0.0/31",
            "ipsToServe": [
                {
                    "mac": "02:ab:cc:11:22:33",
                    "ipv4": "10.0.0.1"
                }
            ]
        },
        {
            "name": "nic2",
            "mac": "ff:00:10:01:00:00",
            "ipv4Subnet": "10.1.0.0/31",
            "ipsToServe": [
                {
                    "mac": "02:ab:cc:11:22:55",
                    "ipv4": "10.1.0.1"
                }
            ]
        }
    ]
}
```

The full dhcp config is defined in the DHCP agent config gRPC Service (internal only).

## Output Format

All command responses are returned as pretty-printed JSON, making it easy to parse with tools like `jq`:

```bash
# List virtual network IDs
vpcctl list-vnet | jq '.virtualNetworks[].spec.id'

# List attachment IDs
vpcctl list-attachment | jq '.virtualNetworkAttachments[].spec.id'
```

## Shell Completion

`vpcctl` supports shell completion for bash, zsh, fish, and PowerShell:

```bash
# Bash
vpcctl completion bash > /etc/bash_completion.d/vpcctl

# Zsh
vpcctl completion zsh > "${fpath[1]}/_vpcctl"

# Fish
vpcctl completion fish > ~/.config/fish/completions/vpcctl.fish
```
