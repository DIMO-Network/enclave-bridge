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
	"github.com/cenkalti/backoff/v5"
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

// BridgeHandshake is a struct that contains the enclave-bridge handshake process.
type BridgeHandshake struct {
	mutex       sync.Mutex
	conn        *vsock.Conn
	ready       chan struct{}
	err         error
	environment map[string]string
}

// StartHandshake starts the enclave-bridge handshake process.
func (b *BridgeHandshake) StartHandshake(ctx context.Context) error {
	return b.StartHandshakeWithPort(ctx, enclave.InitPort)
}

// StartHandshakeWithPort starts the enclave-bridge setup process with a custom init port.
func (b *BridgeHandshake) StartHandshakeWithPort(ctx context.Context, initPort uint32) error {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	logger := zerolog.Ctx(ctx)
	b.ready = make(chan struct{})
	retryBackoff := backoff.ExponentialBackOff{
		InitialInterval:     time.Millisecond * 10,
		RandomizationFactor: backoff.DefaultRandomizationFactor,
		Multiplier:          backoff.DefaultMultiplier,
		MaxInterval:         time.Second * 5,
	}
	var envSettings []byte
	var err error
	for {
		if envSettings, err = b.setupConnection(ctx, initPort); err == nil {
			break
		}
		logger.Error().Err(err).Msg("connection setup failed")
		time.Sleep(retryBackoff.NextBackOff())
	}

	b.environment = map[string]string{}
	err = json.Unmarshal([]byte(envSettings), &b.environment)
	if err != nil {
		retErr := fmt.Errorf("failed to unmarshal environment variables: %w", err)
		_ = b.conn.Close()
		return retErr
	}
	return nil
}

// setupConnection attempts to establish a connection to the enclave and get environment settings.
func (b *BridgeHandshake) setupConnection(ctx context.Context, initPort uint32) ([]byte, error) {
	var err error
	b.conn, err = vsock.Dial(enclave.DefaultHostCID, initPort, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to dial vsock: %w", err)
	}
	_, err = b.conn.Write(enclave.ACK)
	if err != nil {
		_ = b.conn.Close()
		return nil, fmt.Errorf("failed to write ack: %w", err)
	}
	envSettings, err := enclave.ReadBytesWithContext(ctx, b.conn, '\n')
	if err != nil {
		_ = b.conn.Close()
		return nil, fmt.Errorf("failed to read environment variables: %w", err)
	}
	return envSettings, nil
}

// Environment returns the environment variables from the enclave-bridge.
// This functions should be called after the Start function.
func (b *BridgeHandshake) Environment() map[string]string {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return b.environment
}

// FinishHandshakeAndWait sends the final config to the enclave-bridge and starts the watchdog. This function runs indefinitely.
// It returns an error if the handshake fails to complete or the watchdog fails for any reason.
func (b *BridgeHandshake) FinishHandshakeAndWait(ctx context.Context, bridgeConfig *config.BridgeSettings) error {
	b.mutex.Lock()
	if b.conn == nil {
		b.mutex.Unlock()
		return ErrConnectionNotEstablished
	}
	marshaledSettings, err := json.Marshal(bridgeConfig)
	if err != nil {
		b.mutex.Unlock()
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	err = enclave.WriteWithContext(ctx, b.conn, append(marshaledSettings, '\n'))
	if err != nil {
		b.mutex.Unlock()
		return fmt.Errorf("failed to write config: %w", err)
	}
	go func() {
		// wait for ack then close connection
		msg, err := enclave.ReadBytesWithContext(ctx, b.conn, '\n')
		if err != nil {
			b.err = fmt.Errorf("failed to wait for enclave-bridge to ack config: %w", err)
		}
		if !bytes.Equal(msg, enclave.ACK) {
			b.err = ErrMissingAck
		}
		_ = b.conn.Close()
		b.markReady()
	}()
	b.mutex.Unlock()
	return b.runWatchdog(ctx, bridgeConfig)
}

// WaitForBridgeSetup waits for the enclave-bridge to be ready.
func (b *BridgeHandshake) WaitForBridgeSetup() error {
	<-b.ready
	return b.err
}

func (b *BridgeHandshake) runWatchdog(ctx context.Context, bridgeConfig *config.BridgeSettings) error {
	wd, err := watchdog.New(&bridgeConfig.Watchdog)
	if err != nil {
		return fmt.Errorf("failed to create watchdog: %w", err)
	}
	dialer := func() (net.Conn, error) {
		return vsock.Dial(enclave.DefaultHostCID, enclave.InitPort, nil)
	}

	// Wait for the enclave-bridge to be ready or the context to be done.
	select {
	case <-b.ready:
	case <-ctx.Done():
		return ctx.Err()
	}

	return wd.StartClientSide(ctx, dialer)
}

// markReady marks the enclave-bridge as ready.
func (b *BridgeHandshake) markReady() {
	select {
	case <-b.ready:
		return
	default:
		close(b.ready)
	}
}

// Close closes the connection to the enclave-bridge.
// If closeErr is not nil, an error message will be sent to the enclave-bridge.
// The provided context is used to send the error message.
// The error returned is the error from the connection close.
func (b *BridgeHandshake) Close() error {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	if b.conn == nil {
		return nil
	}
	err := b.conn.Close()
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
