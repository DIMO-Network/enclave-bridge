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

The Enclave Bridge uses a specific handshake protocol during initialization to establish configuration between the host and enclave:

1. **Bridge Initialization**: The bridge starts and listens on a predefined VSOCK port (default: 5000)
2. **Connection Establishment**: The enclave connects to the bridge via VSOCK
3. **Environment Exchange**: The bridge sends host environment variables to the enclave
4. **Configuration Response**: The enclave processes the environment and responds with bridge configuration:
   - Server tunnels to establish (port mappings)
   - Client tunnels to create
   - Logging settings
5. **Acknowledgment**: The bridge sends an ACK signal once configuration is applied
6. **Service Startup**: After successful handshake, both sides establish the configured tunnels

This handshake ensures secure configuration exchange and proper initialization of communication channels between the enclave and host environment.

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
