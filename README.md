# Enclave Bridge 🌉

A secure bridge for communication between enclaves and host environments

## Overview

The Enclave Bridge facilitates secure communication between applications running in confidential computing enclaves and the host environment. It provides bidirectional tunneling for both client and server connections via VSOCK (Virtual Socket) communication.

## Features

- **TCP Server Exposure**: Allows enclaves to create TCP servers that are accessible from the host environment or external networks
- **TCP Client Connectivity**: Enables enclaves to establish outbound TCP connections to external services through the host
- **Host Logging Bridge**: Provides a mechanism for enclaves to send logs to the host's standard output for monitoring and troubleshooting
- **Dynamic Configuration**: Supports runtime configuration exchange between enclaves and the bridge

## Startup Configuration Handshake

The Enclave Bridge uses a precise handshake protocol during initialization to establish configuration between the host and enclave:

1. **Bridge Setup**: The bridge starts and listens on a predefined VSOCK port (default: 5000)
2. **Connection Initiation**: The enclave connects to the bridge via VSOCK
3. **Initial ACK**: The enclave immediately sends an ACK message (`0x06, '\n'`) to verify the connection
4. **Environment Exchange**: After receiving the ACK, the bridge serializes and sends host environment variables as a JSON string followed by a newline character
5. **Configuration Response**: The enclave:
   - Receives and parses the environment variables
   - Creates a bridge configuration with settings for:
     - Server tunnels (port mappings)
     - Client tunnels
     - Logging settings
     - Watchdog configuration
   - Sends this configuration as a JSON string followed by a newline character
6. **Service Configuration**: The bridge:
   - Unmarshals the configuration
   - Sets up all requested tunnels and services
7. **Final ACK**: The bridge sends an ACK message (`0x06, '\n'`) to the enclave to signal successful setup completion
8. **Watchdog Activation**: After receiving the final ACK, the enclave:
   - Closes the initial handshake connection
   - Starts a watchdog process to maintain heartbeat communications with the bridge
9. **Service Operation**: Both sides begin normal operation with the established tunnels

This detailed handshake ensures secure configuration exchange and proper initialization of communication channels between the enclave and host environment.

## Getting Started

### Prerequisites

- Go 1.24 or higher
- Docker (for containerized deployment)
- Access to a system supporting VSOCK communication

### Building the Project

```bash
# Build the binary
make build

# Run tests
make test

# Run linter
make lint
```

Key configuration options:

- **Servers**: Configure endpoints that proxy external TCP connections to the enclave
- **Clients**: Configure connections from the enclave to external services
- **Logging**: Configure logging levels and output
