package watchdog

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/DIMO-Network/enclave-bridge/pkg/config"
	"github.com/DIMO-Network/enclave-bridge/pkg/enclave"
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
	settings     *config.WatchdogSettings
	timer        *time.Ticker
	watchErrChan chan error
}

// New creates a new watchdog.
func New(settings *config.WatchdogSettings) (*Watchdog, error) {
	if settings.EnclaveID == uuid.Nil {
		return nil, ErrEnclaveIDRequired
	}
	return &Watchdog{
		settings:     settings,
		timer:        time.NewTicker(settings.Interval),
		watchErrChan: make(chan error),
	}, nil
}

// Start starts the watchdog. The Watchdog will return an error if the accepted connection from the listener is not the correct enclave ID.
// Or if no connection sends a heartbeat within the interval.
// If the context is cancelled, the watchdog will stop without error.
func (w *Watchdog) Start(ctx context.Context, listener net.Listener) error {
	logger := zerolog.Ctx(ctx).With().Str("component", "watchdog").Logger()
	defer listener.Close() //nolint:errcheck
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				logger.Error().Err(err).Msg("failed to accept connection")
				continue
			}
			go w.handleConn(ctx, conn)
		}
	}()
	return w.startTimer(ctx)
}

// Start starts the watchdog.
func (w *Watchdog) startTimer(ctx context.Context) error {
	w.timer.Reset(w.settings.Interval)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-w.timer.C:
			return fmt.Errorf("%w: no heartbeat within %s", ErrEnclaveHeartbeatTimeout, w.settings.Interval)
		case watchErr := <-w.watchErrChan:
			return watchErr
		}
	}
}

// HandleConn handles a connection from the enclave.
func (w *Watchdog) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close() //nolint:errcheck
	for {
		enclaveID, err := enclave.ReadBytesWithContext(ctx, conn, '\n')
		if err != nil {
			// This will error if something happens to the connection or the context is cancelled
			// In either case, we don't need to do anything.
			return
		}
		// Remove the newline character
		enclaveID = enclaveID[:len(enclaveID)-1]
		if w.settings.EnclaveID != uuid.FromBytesOrNil(enclaveID) {
			w.watchErrChan <- fmt.Errorf("%w: got %v, expected %v",
				ErrEnclaveIDMismatch, uuid.FromBytesOrNil(enclaveID), w.settings.EnclaveID)
			return
		}
		w.timer.Reset(w.settings.Interval)
	}
}

// NewStandardSettings returns a standard watchdog settings.
func NewStandardSettings() config.WatchdogSettings {
	return config.WatchdogSettings{
		EnclaveID: uuid.Must(uuid.NewV4()),
		Interval:  time.Second * 30,
	}
}
