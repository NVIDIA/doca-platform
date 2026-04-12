# vfmac

A Go package for managing persistent MAC addresses for Mellanox Virtual Functions (VFs).

## Purpose

- Assigns and persists MAC addresses for all VFs on Mellanox SmartNIC ECPFs.
- Stores MAC assignments in a TOML config file for persistence across reboots.
- Ensures each VF gets a unique, stable MAC address.

## Configuration

You can override defaults using environment variables:

| Variable              | Default Value                              | Description                       |
|---------------------- |--------------------------------------------|-----------------------------------|
| VFMAC_CONFIG_DIR      | `/etc/mellanox`                            | Directory for config file          |
| VFMAC_CONFIG_FILE     | `dpf-vf-mac-mapping.toml`                  | Config file name                  |

## Usage Example

```go
import (
  "os"
  "path/to/pkg/vfmac"
)

func main() {
  if err := vfmac.ProcessVFs(); err != nil {
      os.Exit(1)
  }
}

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
[enp1s0np0]
  [enp1s0np0.vf0]
    mac = "ab:22:ea:7f:fe:12" 
  ...
```
