package enclave

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"

	"github.com/DIMO-Network/enclave-bridge/pkg/config"
	"github.com/caarlos0/env/v11"
	"github.com/mdlayher/vsock"
)

const (
	// ACK is the ACK message sent by the enclave-bridge.
	ACK = 0x06
	// InitPort is the port used to initialize the enclave-bridge.
	InitPort = uint32(5000)
)

// EnclaveSetup is a struct that contains the enclave-bridge setup process.
type EnclaveSetup[T any] struct {
	enclaveConfig T
	conn          net.Conn
	ready         chan struct{}
	err           error
}

// Start starts the enclave-bridge setup process.
func (e *EnclaveSetup[T]) Start(initPort uint32) error {
	e.ready = make(chan struct{})
	var err error
	e.conn, err = vsock.Dial(DefaultHostCID, initPort, nil)
	if err != nil {
		return fmt.Errorf("failed to dial vsock: %w", err)
	}
	reader := bufio.NewReader(e.conn)
	envSettings, err := reader.ReadString('\n')
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return fmt.Errorf("failed to wait for other side to close connection: %w", err)
	}
	environment := map[string]string{}
	err = json.Unmarshal([]byte(envSettings), &environment)
	if err != nil {
		return fmt.Errorf("failed to unmarshal environment variables: %w", err)
	}

	envOpts := env.Options{
		Environment: environment,
	}
	e.enclaveConfig, err = env.ParseAsWithOptions[T](envOpts)
	if err != nil {
		return fmt.Errorf("failed to parse environment variables: %w", err)
	}
	return nil
}

// Config returns the enclave-bridge config.
func (e *EnclaveSetup[T]) Config() T {
	return e.enclaveConfig
}

// SendBridgeConfig sends the config to the enclave-bridge.
func (e *EnclaveSetup[T]) SendBridgeConfig(config *config.BridgeSettings) error {
	if e.conn == nil {
		return fmt.Errorf("connection not established")
	}
	marshaledSettings, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	_, err = e.conn.Write(append(marshaledSettings, '\n'))
	if err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}
	go func() {
		// wait for ack then close connection
		reader := bufio.NewReader(e.conn)
		msg, err := reader.ReadByte()
		if err != nil {
			e.err = fmt.Errorf("failed to wait for enclave-bridge to ack config: %w", err)
		}
		if msg != ACK {
			e.err = fmt.Errorf("bridge failed to ack config")
		}
		_ = e.conn.Close()
		e.markReady()
	}()
	return nil
}

// WaitForBridgeSetup waits for the enclave-bridge to be ready.
func (e *EnclaveSetup[T]) WaitForBridgeSetup() error {
	<-e.ready
	return e.err
}

// markReady marks the enclave-bridge as ready.
func (e *EnclaveSetup[T]) markReady() {
	select {
	case <-e.ready:
		return
	default:
		close(e.ready)
	}
}

// Close closes the connection to the enclave-bridge.
func (e *EnclaveSetup[T]) Close() error {
	return e.conn.Close()
}
