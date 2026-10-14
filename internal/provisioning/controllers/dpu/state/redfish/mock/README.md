# Redfish Mock Server

This package provides a minimal mock Redfish server for testing DPU discovery functionality. The mock server implements the essential Redfish API endpoints used by the DPU discovery crawler.

## Features

- **Core Discovery Support**: Implements endpoints needed for DPU discovery
- **Realistic Responses**: Returns proper Redfish JSON responses with correct OData annotations
- **TLS Support**: Runs with TLS for realistic testing scenarios
- **Configurable**: Supports custom passwords

## Supported Endpoints

### Core Redfish Services
- `/redfish/v1/` - Root service (used for BMC identification)
- `/redfish/v1/Chassis/Card1` - Chassis information (used for serial number and part number)

## Usage

### Basic Usage

```go
package main

import (
    "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/mock"
    "k8s.io/klog/v2"
)

func main() {
    // Create and start the mock server
    server, err := mock.CreateMockRedfishServer("testpassword")
    if err != nil {
        klog.Fatalf("Failed to create mock server: %v", err)
    }
    defer server.Stop()

    klog.Infof("Mock server started at: %s", server.URL())

    // Get a client connected to the mock server
    client, err := server.GetClient()
    if err != nil {
        klog.Fatalf("Failed to create client: %v", err)
    }

    // Use the client for testing
    resp, err := client.GetRootService()
    if err != nil {
        klog.Fatalf("Failed to get root service: %v", err)
    }
    klog.Infof("Root service response: %d", resp.StatusCode())
}
```

### Integration Testing

```go
func TestDPUDiscovery(t *testing.T) {
    // Start mock server
    server, err := mock.CreateMockRedfishServer("testpass")
    if err != nil {
        t.Fatalf("Failed to create mock server: %v", err)
    }
    defer server.Stop()

    // Configure your test to use the mock server URL
    mockServerURL := server.URL()
    
    // Your test logic here...
}
```

### Custom Configuration

```go
// Custom password
server, err := mock.CreateMockRedfishServer("custompassword")
if err != nil {
    log.Fatalf("Failed to create mock server: %v", err)
}
defer server.Stop()

// Default configuration (0penBmc)
server, err := mock.CreateMockRedfishServer("")
if err != nil {
    log.Fatalf("Failed to create mock server: %v", err)
}
defer server.Stop()
```

## Mock Server Behavior

### Discovery Process

The mock server supports the DPU discovery process by:

1. **Root Service**: Returns Redfish root service information to identify BMCs
2. **Chassis Information**: Provides serial number and part number for DPU identification

### Response Format

All responses follow the Redfish specification with:

- Proper OData annotations (`@odata.context`, `@odata.id`, `@odata.type`)
- Correct HTTP status codes
- JSON content type headers
- Realistic data structures

## Testing

Run the tests to verify the mock server functionality:

```bash
go test ./internal/provisioning/controllers/dpu/state/redfish/mock/ -v
```

## Integration with DPU Discovery

The mock server can be integrated with the DPU discovery crawler by:

1. **Replacing BMC IP**: Use the mock server URL instead of real BMC IP
2. **Authentication**: Use the configured password for authentication
3. **TLS Configuration**: The mock server uses TLS with self-signed certificates

## Example Test Scenarios

### Basic DPU Discovery
```go
// Test DPU discovery with mock server
func TestDPUDiscovery(t *testing.T) {
    server, _ := mock.CreateMockRedfishServer("testpass")
    defer server.Stop()
    
    client, _ := server.GetClient()
    
    // Test root service
    resp, err := client.GetRootService()
    if err != nil {
        t.Fatalf("Root service failed: %v", err)
    }
    
    if resp.StatusCode() != 200 {
        t.Errorf("Expected 200, got %d", resp.StatusCode())
    }
    
    // Test chassis information
    resp, chassisInfo, err := client.GetChassis()
    if err != nil {
        t.Fatalf("Chassis info failed: %v", err)
    }
    
    if chassisInfo.SerialNumber == "" {
        t.Error("Expected serial number")
    }
    
    if chassisInfo.PartNumber == "" {
        t.Error("Expected part number")
    }
}
```

## Server Information

### Network Details

The mock server provides methods to get network information:

```go
server, _ := mock.CreateMockRedfishServer("testpass")
defer server.Stop()

// Get server details
url := server.URL()
ip := server.GetIPAddress()
port := server.GetPort()
address := server.GetAddress()

fmt.Printf("Server URL: %s\n", url)
fmt.Printf("Server IP: %s\n", ip)
fmt.Printf("Server Port: %d\n", port)
fmt.Printf("Server Address: %s\n", address)
```

## Troubleshooting

### Common Issues

1. **TLS Certificate Errors**: The mock server uses self-signed certificates. Ensure your client is configured to skip verification for testing.

2. **Connection Refused**: Make sure the mock server is started before attempting to connect.

3. **Authentication Failures**: Verify the password matches what was configured when creating the server.

### Debugging

Enable debug logging to see server requests:

```go
import "k8s.io/klog/v2"

// Set log level
klog.InitFlags(nil)
flag.Set("v", "4") // Enable debug logging
```

## API Reference

### CreateMockRedfishServer

Creates and starts a mock Redfish server.

```go
func CreateMockRedfishServer(password string) (*RedfishMockServer, error)
```

**Parameters:**
- `password`: BMC password (defaults to "0penBmc" if empty)

**Returns:**
- `*RedfishMockServer`: The mock server instance
- `error`: Any error that occurred during creation

### RedfishMockServer Methods

#### Start()
Starts the mock server with TLS.

#### Stop()
Stops the mock server and cleans up resources.

#### URL() string
Returns the server URL.

#### GetClient() (*client.Client, error)
Returns a configured Redfish client connected to the mock server.

#### GetIPAddress() string
Returns the IP address of the mock server.

#### GetPort() int
Returns the port number of the mock server.

#### GetAddress() string
Returns the full address (IP:port) of the mock server.

## Contributing

When adding new endpoints or modifying existing ones:

1. Follow the Redfish specification for response format
2. Include proper OData annotations
3. Add corresponding tests
4. Update this documentation

## License

This code is licensed under the Apache License, Version 2.0. 