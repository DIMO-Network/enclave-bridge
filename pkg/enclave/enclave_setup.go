package enclave

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

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

type connectionError string

func (e connectionError) Error() string { return string(e) }

// Is implements the interface needed for errors.Is to work.
func (e connectionError) Is(target error) bool {
	if valStr, ok := target.(connectionError); ok {
		return e == valStr
	}
	return false
}

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

// Start starts the enclave-bridge setup process.
func (e *EnclaveSetup) Start(ctx context.Context, initPort uint32) error {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	e.ready = make(chan struct{})
	var err error
	e.conn, err = vsock.Dial(DefaultHostCID, initPort, nil)
	if err != nil {
		return fmt.Errorf("failed to dial vsock: %w", err)
	}
	envSettings, err := ReadBytesWithContext(ctx, e.conn, '\n')
	if err != nil {
		// This only fails when something is wrong with the connection so do  not try to send an error.
		_ = e.Close(ctx, nil)
		return fmt.Errorf("failed to read environment variables: %w", err)
	}
	e.environment = map[string]string{}
	err = json.Unmarshal([]byte(envSettings), &e.environment)
	if err != nil {
		retErr := fmt.Errorf("failed to unmarshal environment variables: %w", err)
		_ = e.Close(ctx, retErr)
		return retErr
	}
	return nil
}

// Environment returns the environment variables from the enclave-bridge.
// This functions should be called after the Start function.
func (e *EnclaveSetup) Environment() map[string]string {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	return e.environment
}

// SendError sends an error message to the enclave-bridge instead of the config.
func (e *EnclaveSetup) sendError(ctx context.Context, errorMsg string) error {
	errSettings := config.BridgeSettings{Error: errorMsg}
	marshaledError, err := json.Marshal(errSettings)
	if err != nil {
		return fmt.Errorf("failed to marshal error: %w", err)
	}
	err = WriteWithContext(ctx, e.conn, marshaledError)
	if err != nil {
		return fmt.Errorf("failed to send error: %w", err)
	}
	return nil
}

// SendBridgeConfig sends the config to the enclave-bridge.
func (e *EnclaveSetup) SendBridgeConfig(ctx context.Context, bridgeConfig *config.BridgeSettings) error {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.conn == nil {
		return ErrConnectionNotEstablished
	}
	marshaledSettings, err := json.Marshal(bridgeConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	err = WriteWithContext(ctx, e.conn, append(marshaledSettings, '\n'))
	if err != nil {
		_ = e.Close(ctx, fmt.Errorf("failed to write config: %w", err))
		return fmt.Errorf("failed to write config: %w", err)
	}
	go func() {
		// wait for ack then close connection
		msg, err := ReadByteWithContext(ctx, e.conn)
		if err != nil {
			e.err = fmt.Errorf("failed to wait for enclave-bridge to ack config: %w", err)
		}
		if msg != ACK {
			e.err = ErrMissingAck
		}
		_ = e.Close(ctx, nil)
		e.markReady()
	}()
	return nil
}

// WaitForBridgeSetup waits for the enclave-bridge to be ready.
func (e *EnclaveSetup) WaitForBridgeSetup() error {
	<-e.ready
	return e.err
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
func (e *EnclaveSetup) Close(ctx context.Context, closeErr error) error {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.conn == nil {
		return nil
	}
	var sendErr error
	if closeErr != nil {
		sendErr = e.sendError(ctx, closeErr.Error())
	}
	err := e.conn.Close()
	e.conn = nil
	if sendErr != nil {
		return fmt.Errorf("failed to send error: %w", sendErr)
	}
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
