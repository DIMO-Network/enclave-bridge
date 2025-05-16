package handshake

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/DIMO-Network/enclave-bridge/pkg/config"
	"github.com/DIMO-Network/enclave-bridge/pkg/enclave"
	"github.com/DIMO-Network/enclave-bridge/pkg/watchdog"
	"github.com/caarlos0/env/v11"
	"github.com/mdlayher/vsock"
	"github.com/rs/zerolog"
)

type connectionError string

func (e connectionError) Error() string { return string(e) }

// ErrConnectionNotEstablished is returned when attempting to use a connection that hasn't been established.
const (
	ErrConnectionNotEstablished = connectionError("connection not established")
	ErrMissingAck               = connectionError("missing ack from enclave-bridge")
)

// EnclaveSetup is a struct that contains the enclave-bridge setup process.
type EnclaveSetup struct {
	mutex       sync.Mutex
	conn        *vsock.Conn
	ready       chan struct{}
	err         error
	environment map[string]string
}

// StartHandshake starts the enclave-bridge setup process.
func (e *EnclaveSetup) StartHandshake(ctx context.Context) error {
	return e.StartHandshakeWithPort(ctx, enclave.InitPort)
}

// StartHandshakeWithPort starts the enclave-bridge setup process with a custom init port.
func (e *EnclaveSetup) StartHandshakeWithPort(ctx context.Context, initPort uint32) error {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	logger := zerolog.Ctx(ctx)
	e.ready = make(chan struct{})

	var envSettings []byte
	var err error
	for {
		if envSettings, err = e.setupConnection(ctx, initPort); err == nil {
			break
		}
		logger.Error().Err(err).Msg("connection setup failed")
		time.Sleep(1 * time.Second)
	}

	e.environment = map[string]string{}
	err = json.Unmarshal([]byte(envSettings), &e.environment)
	if err != nil {
		retErr := fmt.Errorf("failed to unmarshal environment variables: %w", err)
		_ = e.conn.Close()
		return retErr
	}
	return nil
}

// setupConnection attempts to establish a connection to the enclave and get environment settings.
func (e *EnclaveSetup) setupConnection(ctx context.Context, initPort uint32) ([]byte, error) {
	var err error
	e.conn, err = vsock.Dial(enclave.DefaultHostCID, initPort, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to dial vsock: %w", err)
	}
	_, err = e.conn.Write(enclave.ACK)
	if err != nil {
		_ = e.conn.Close()
		return nil, fmt.Errorf("failed to write ack: %w", err)
	}
	envSettings, err := enclave.ReadBytesWithContext(ctx, e.conn, '\n')
	if err != nil {
		_ = e.conn.Close()
		return nil, fmt.Errorf("failed to read environment variables: %w", err)
	}
	return envSettings, nil
}

// Environment returns the environment variables from the enclave-bridge.
// This functions should be called after the Start function.
func (e *EnclaveSetup) Environment() map[string]string {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	return e.environment
}

// FinishHandshakeAndWait sends the final config to the enclave-bridge and starts the watchdog. This function runs indefinitely.
// It returns an error if the handshake fails to complete or the watchdog fails for any reason.
func (e *EnclaveSetup) FinishHandshakeAndWait(ctx context.Context, bridgeConfig *config.BridgeSettings) error {
	e.mutex.Lock()
	if e.conn == nil {
		e.mutex.Unlock()
		return ErrConnectionNotEstablished
	}
	marshaledSettings, err := json.Marshal(bridgeConfig)
	if err != nil {
		e.mutex.Unlock()
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	err = enclave.WriteWithContext(ctx, e.conn, append(marshaledSettings, '\n'))
	if err != nil {
		e.mutex.Unlock()
		return fmt.Errorf("failed to write config: %w", err)
	}
	go func() {
		// wait for ack then close connection
		msg, err := enclave.ReadBytesWithContext(ctx, e.conn, '\n')
		if err != nil {
			e.err = fmt.Errorf("failed to wait for enclave-bridge to ack config: %w", err)
		}
		if !bytes.Equal(msg, enclave.ACK) {
			e.err = ErrMissingAck
		}
		_ = e.conn.Close()
		e.markReady()
	}()
	e.mutex.Unlock()
	return e.runWatchdog(ctx, bridgeConfig)
}

// WaitForBridgeSetup waits for the enclave-bridge to be ready.
func (e *EnclaveSetup) WaitForBridgeSetup() error {
	<-e.ready
	return e.err
}

func (e *EnclaveSetup) runWatchdog(ctx context.Context, bridgeConfig *config.BridgeSettings) error {
	// Wait for the enclave-bridge to be ready or the context to be done.
	select {
	case <-e.ready:
	case <-ctx.Done():
		return ctx.Err()
	}

	wd, err := watchdog.New(&bridgeConfig.Watchdog)
	if err != nil {
		e.err = fmt.Errorf("failed to create watchdog: %w", err)
	}
	dialer := func() (net.Conn, error) {
		return vsock.Dial(enclave.DefaultHostCID, enclave.InitPort, nil)
	}
	return wd.StartClientSide(ctx, dialer)
}

// markReady marks the enclave-bridge as ready.
func (e *EnclaveSetup) markReady() {
	select {
	case <-e.ready:
		return
	default:
		close(e.ready)
	}
}

// Close closes the connection to the enclave-bridge.
// If closeErr is not nil, an error message will be sent to the enclave-bridge.
// The provided context is used to send the error message.
// The error returned is the error from the connection close.
func (e *EnclaveSetup) Close() error {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.conn == nil {
		return nil
	}
	err := e.conn.Close()
	return err
}

// ConfigFromEnvMap parses the environment variables from the enclave-bridge and returns a struct.
func ConfigFromEnvMap[T any](envMap map[string]string) (T, error) {
	var zeroValue T
	envOpts := env.Options{
		Environment: envMap,
	}
	enclaveConfig, err := env.ParseAsWithOptions[T](envOpts)
	if err != nil {
		return zeroValue, fmt.Errorf("failed to parse environment variables: %w", err)
	}
	return enclaveConfig, nil
}
