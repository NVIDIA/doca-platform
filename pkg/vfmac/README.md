# vfmac

A Go package and tool for managing persistent MAC addresses for Mellanox Virtual Functions (VFs).

## Purpose

- Assigns and persists MAC addresses for all VFs on Mellanox SmartNICs (p0, p1).
- Stores MAC assignments in a TOML config file for persistence across reboots.
- Ensures each VF gets a unique, stable MAC address.

## Configuration

You can override defaults using environment variables:

| Variable              | Default Value                              | Description                       |
|---------------------- |--------------------------------------------|-----------------------------------|
| VFMAC_CONFIG_DIR      | `/etc/mellanox`                            | Directory for config file          |
| VFMAC_CONFIG_FILE     | `dpf-vf-mac-mapping.toml`                  | Config file name                  |
| VFMAC_DEVICE_PATH     | `/dev/mst/mt41692_pciconf0`                | Device path for mlxconfig         |
| VFMAC_MLXCONFIG       | `/usr/bin/mlxconfig`                       | Path to mlxconfig binary          |

## Usage

As a library:

```go
import "path/to/pkg/vfmac"
err := vfmac.ProcessVFs()
if err != nil {
    // handle error
}
```

As a CLI (see `cmd/dpuagent/main.go`):

```sh
./dpuagent
```

## Testing

- The package abstracts file/OS operations for testability.
- You can inject a mock FileSystem for unit tests.

## Example Config File

```toml
[p0]
  [p0.vf0]
    mac = "fa:b0:b2:04:9f:b1"
  ...
[p1]
  [p1.vf0]
    mac = "da:f2:ea:53:cf:40"
  ...
```

## Building vfmac

To build the vfmac binary, run the following command:

```bash
make binary-dpuagent
```
