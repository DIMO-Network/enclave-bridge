package client

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/DIMO-Network/enclave-bridge/pkg/enclave"
	"github.com/mdlayher/vsock"
)

const defaultHostCID = 3

var emptyConfig tls.Config

func defaultConfig() *tls.Config {
	return &emptyConfig
}

// NewHTTPClient creates a new HTTP client that tunnels connections to the enclave Host on the given port.
func NewHTTPClient(port uint32, tlsConfig *tls.Config) *http.Client {
	if tlsConfig == nil {
		tlsConfig = defaultConfig()
	}
	client := &http.Client{}
	client.Transport = &http.Transport{
		DialContext: func(_ context.Context, network, addr string) (net.Conn, error) {
			return dialVsock(port, network, addr)
		},
		DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			vsockConn, err := dialVsock(port, network, addr)
			if err != nil {
				return nil, fmt.Errorf("failed to dial vsock: %w", err)
			}
			config := modifiedConfig(addr, tlsConfig)
			tlsConn := tls.Client(vsockConn, config)
			err = tlsConn.HandshakeContext(ctx)
			if err != nil {
				_ = vsockConn.Close()
				return nil, fmt.Errorf("failed TLS handshake: %w", err)
			}
			return tlsConn, nil
		},
	}
	return client
}

// modifiedConfig modifies the TLS config to use the correct server name.
// copied from https://cs.opensource.google/go/go/+/refs/tags/go1.24.2:src/crypto/tls/tls.go;l=140-156
func modifiedConfig(addr string, config *tls.Config) *tls.Config {
	colonPos := strings.LastIndex(addr, ":")
	if colonPos == -1 {
		colonPos = len(addr)
	}
	hostname := addr[:colonPos]

	if config == nil {
		config = defaultConfig()
	}
	// If no ServerName is set, infer the ServerName
	// from the hostname we're connecting to.
	if config.ServerName == "" {
		// Make a copy to avoid polluting argument or default.
		c := config.Clone()
		c.ServerName = hostname
		config = c
	}
	return config
}

func dialVsock(port uint32, network, addr string) (net.Conn, error) {
	if network != "tcp" {
		return nil, fmt.Errorf("unsupported network: %s", network)
	}
	vsockConn, err := vsock.Dial(defaultHostCID, port, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to dial vsock: %w", err)
	}
	_, err = vsockConn.Write([]byte(addr + "\n"))
	if err != nil {
		return nil, fmt.Errorf("failed to write to vsock: %w", err)
	}
	resp, err := bufio.NewReader(vsockConn).ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("failed to read from vsock: %w", err)
	}
	if bytes.Equal(resp, enclave.ACK) {
		return vsockConn, nil
	}
	return nil, fmt.Errorf("invalid response from vsock: %d", resp)
}
