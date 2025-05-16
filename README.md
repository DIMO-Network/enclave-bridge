# Enclave Bridge 🌉

A secure bridge for communication between enclaves and host environments

## Overview

The Enclave Bridge facilitates secure communication between applications running in confidential computing enclaves and the host environment. It provides bidirectional tunneling for both client and server connections via VSOCK (Virtual Socket) communication.

## Features

- **TCP Server Exposure**: Allows enclaves to create TCP servers that are accessible from the host environment or external networks
- **TCP Client Connectivity**: Enables enclaves to establish outbound TCP connections to external services through the host
- **Host Logging Bridge**: Provides a mechanism for enclaves to send logs to the host's standard output for monitoring and troubleshooting
- **Dynamic Configuration**: Supports runtime configuration exchange between enclaves and the bridge

## Quick Start Example

We provide a simple example that demonstrates how to create an enclave application that communicates with the bridge. The example shows:

- Setting up logging to the bridge
- Establishing communication with the bridge
- Creating a simple HTTP server in the enclave
- Handling graceful shutdown

### Example Overview

The example creates a simple HTTP server that runs inside an enclave. The server is accessible from the host machine through the enclave-bridge. Here's how it works:

1. The enclave starts and establishes a connection with the bridge
2. The bridge is configured to forward TCP connections from port 8080 on the host to port 5001 in the enclave
3. The enclave runs a simple HTTP server that responds with "Hello from the enclave!"
4. When you make a request to `http://localhost:8080/` on the host, it's forwarded to the enclave

### Key Components

#### Bridge Handshake

The handshake process establishes communication between the enclave and bridge:

1. The enclave connects to the bridge via VSOCK
2. Configuration is exchanged
3. The bridge sets up the necessary tunnels
4. The watchdog is started to monitor the enclave's health

#### Server Setup

The example uses Fiber to create a simple HTTP server that:

1. Listens on VSOCK port 5001
2. Responds to HTTP requests
3. Handles graceful shutdown

#### Logging

Logs are sent through VSOCK to the bridge, making them visible on the host machine.

### Port Mapping

- Host port 8080 → Enclave VSOCK port 5001 (server)
- Enclave VSOCK port 5002 (client, for outbound connections)

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
