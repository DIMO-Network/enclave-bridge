package watchdog

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/DIMO-Network/enclave-bridge/pkg/config"
	"github.com/DIMO-Network/enclave-bridge/pkg/enclave"
	"github.com/cenkalti/backoff/v5"
	"github.com/gofrs/uuid"
	"github.com/rs/zerolog"
)

// WatchdogError is a typed error for watchdog-related errors.
type WatchdogError string

func (e WatchdogError) Error() string { return string(e) }

const (
	// ErrEnclaveIDRequired is returned when the enclave ID is missing in the settings.
	ErrEnclaveIDRequired = WatchdogError("enclave ID is required")
	// ErrEnclaveHeartbeatTimeout is returned when the enclave doesn't send a heartbeat within the interval.
	ErrEnclaveHeartbeatTimeout = WatchdogError("enclave heartbeat timeout")
	// ErrEnclaveIDMismatch is returned when the enclave ID doesn't match the expected ID.
	ErrEnclaveIDMismatch = WatchdogError("enclave ID mismatch")
)

// Watchdog is a struct that handles the enclave watchdog.
type Watchdog struct {
	enclaveID    uuid.UUID
	interval     time.Duration
	ticker       *time.Ticker
	watchErrChan chan error
}

// New creates a new watchdog.
func New(settings *config.WatchdogSettings) (*Watchdog, error) {
	if settings.EnclaveID == uuid.Nil {
		return nil, ErrEnclaveIDRequired
	}
	return &Watchdog{
		enclaveID:    settings.EnclaveID,
		interval:     settings.Interval,
		ticker:       time.NewTicker(settings.Interval),
		watchErrChan: make(chan error),
	}, nil
}

// StartServerSide starts the watchdog. The Watchdog will return an error if the accepted connection from the listener is not the correct enclave ID.
// Or if no connection sends a heartbeat within the interval.
// If the context is cancelled, the watchdog will stop without error.
func (w *Watchdog) StartServerSide(ctx context.Context, listener net.Listener) error {
	logger := zerolog.Ctx(ctx).With().Str("component", "watchdog").Logger()
	defer listener.Close() //nolint:errcheck
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				logger.Error().Err(err).Msg("failed to accept connection")
				continue
			}
			// asynchronously handle the connection since we are the server.
			go w.HandleConn(ctx, conn)
		}
	}()
	return w.startTicker(ctx)
}

// StartClientSide starts the watchdog. The Watchdog will return an error if the accepted connection from the listener is not the correct enclave ID.
// Or if no connection sends a heartbeat within the interval.
// If the context is cancelled, the watchdog will stop without error.
func (w *Watchdog) StartClientSide(ctx context.Context, dial func() (net.Conn, error)) error {
	logger := zerolog.Ctx(ctx).With().Str("component", "watchdog").Logger()

	retryBackoff := backoff.ExponentialBackOff{
		InitialInterval:     time.Millisecond * 100,
		RandomizationFactor: backoff.DefaultRandomizationFactor,
		Multiplier:          backoff.DefaultMultiplier,
		MaxInterval:         w.interval,
	}

	go func() {
		for {
			watchDogConn, err := dial()
			if err != nil {
				logger.Error().Err(err).Msg("watchdog client dial failed")
				// Use exponential backoff for retry
				time.Sleep(retryBackoff.NextBackOff())
				continue
			}
			// Reset backoff on successful connection
			retryBackoff.Reset()
			// synchronously handle the connection since we are the one that initiated the connection.
			w.HandleConn(ctx, watchDogConn)
			watchDogConn.Close() //nolint:errcheck
		}
	}()
	return w.startTicker(ctx)
}

// Start starts the watchdog.
func (w *Watchdog) startTicker(ctx context.Context) error {
	w.ticker.Reset(w.interval)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-w.ticker.C:
			return fmt.Errorf("%w: no heartbeat within %s", ErrEnclaveHeartbeatTimeout, w.interval)
		case watchErr := <-w.watchErrChan:
			return watchErr
		}
	}
}

// HandleConn handles a connection from the enclave.
func (w *Watchdog) HandleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close() //nolint:errcheck
	go Heartbeat(ctx, append(w.enclaveID.Bytes(), '\n'), conn, w.interval)
	for {
		enclaveID, err := enclave.ReadBytesWithContext(ctx, conn, '\n')
		if err != nil {
			// This will error if something happens to the connection or the context is cancelled
			// In either case, we don't need to do anything.
			return
		}
		// Remove the newline character
		enclaveID = enclaveID[:len(enclaveID)-1]
		if w.enclaveID != uuid.FromBytesOrNil(enclaveID) {
			w.watchErrChan <- fmt.Errorf("%w: got %v, expected %v",
				ErrEnclaveIDMismatch, uuid.FromBytesOrNil(enclaveID), w.enclaveID)
			return
		}
		w.ticker.Reset(w.interval)
	}
}

// NewStandardSettings returns a standard watchdog settings.
func NewStandardSettings() config.WatchdogSettings {
	return config.WatchdogSettings{
		EnclaveID: uuid.Must(uuid.NewV4()),
		Interval:  time.Second * 30,
	}
}

// Heartbeat sends a heartbeat to a watchdog.
func Heartbeat(ctx context.Context, uuidMessage []byte, watchDogConn net.Conn, interval time.Duration) error {
	ticker := time.NewTicker(interval / 2)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if _, err := watchDogConn.Write(uuidMessage); err != nil {
				return fmt.Errorf("failed to write to conn: %w", err)
			}
		}
	}
}
