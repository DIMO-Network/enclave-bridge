package watchdog_test

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	"github.com/DIMO-Network/enclave-bridge/pkg/config"
	"github.com/DIMO-Network/enclave-bridge/pkg/watchdog"
	"github.com/gofrs/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func Main(t *testing.M) {
	zerolog.DefaultContextLogger = nil
	os.Exit(t.Run())
}

// setupWatchdogTest creates a watchdog and listener for testing
func setupWatchdogTest(t *testing.T, interval time.Duration) (*watchdog.Watchdog, net.Listener, uuid.UUID) {
	t.Helper()
	enclaveID := uuid.Must(uuid.NewV4())

	settings := &config.WatchdogSettings{
		EnclaveID: enclaveID,
		Interval:  interval,
	}

	dog, err := watchdog.New(settings)
	require.NoError(t, err)

	// Create a listener
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	return dog, listener, enclaveID
}

func TestNewWatchdog(t *testing.T) {
	t.Parallel()

	t.Run("valid settings", func(t *testing.T) {
		t.Parallel()
		enclaveID := uuid.Must(uuid.NewV4())
		settings := &config.WatchdogSettings{
			EnclaveID: enclaveID,
			Interval:  time.Second,
		}

		dog, err := watchdog.New(settings)
		require.NoError(t, err)
		require.NotNil(t, dog)
	})

	t.Run("nil enclave ID", func(t *testing.T) {
		t.Parallel()
		settings := &config.WatchdogSettings{
			EnclaveID: uuid.Nil,
			Interval:  time.Second,
		}

		dog, err := watchdog.New(settings)
		require.Error(t, err)
		require.Nil(t, dog)
		require.ErrorIs(t, err, watchdog.ErrEnclaveIDRequired)
	})
}

func TestWatchdogTimeout(t *testing.T) {
	t.Parallel()
	interval := 100 * time.Millisecond
	dog, listener, _ := setupWatchdogTest(t, interval)
	defer listener.Close() //nolint:errcheck

	// Start the watchdog in a goroutine
	errCh := make(chan error)
	go func() {
		errCh <- dog.StartServerSide(t.Context(), listener)
	}()

	// Wait for timeout to occur
	select {
	case err := <-errCh:
		require.Error(t, err)
		require.ErrorIs(t, err, watchdog.ErrEnclaveHeartbeatTimeout)
	case <-time.After(interval * 2):
		t.Fatal("timeout waiting for watchdog to return error")
	}
}

func TestWatchdogIDMismatch(t *testing.T) {
	t.Parallel()
	interval := 10 * time.Second // Long interval to prevent timeout
	dog, listener, correctID := setupWatchdogTest(t, interval)
	defer listener.Close() //nolint:errcheck

	// Create a wrong ID
	wrongID := uuid.Must(uuid.NewV4())
	for wrongID == correctID {
		wrongID = uuid.Must(uuid.NewV4())
	}

	// Start the watchdog in a goroutine
	errCh := make(chan error)
	go func() {
		errCh <- dog.StartServerSide(t.Context(), listener)
	}()

	// Connect to the watchdog
	conn, err := net.Dial("tcp", listener.Addr().String())
	require.NoError(t, err)
	defer conn.Close() //nolint:errcheck

	// Send wrong enclave ID
	_, err = conn.Write(append(wrongID.Bytes(), '\n'))
	require.NoError(t, err)

	// Wait for error
	select {
	case err := <-errCh:
		require.Error(t, err)
		require.ErrorIs(t, err, watchdog.ErrEnclaveIDMismatch)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for watchdog to return error")
	}
}

func TestWatchdogHeartbeat(t *testing.T) {
	t.Parallel()
	interval := 200 * time.Millisecond
	dog, listener, enclaveID := setupWatchdogTest(t, interval)
	defer listener.Close() //nolint:errcheck

	// Start the watchdog
	ctx, watchCtxCancel := context.WithCancel(t.Context())
	defer watchCtxCancel()

	errCh := make(chan error)
	go func() {
		errCh <- dog.StartServerSide(ctx, listener)
	}()

	// Connect to the watchdog
	conn, err := net.Dial("tcp", listener.Addr().String())
	require.NoError(t, err)
	defer conn.Close() //nolint:errcheck

	// Send heartbeats every half interval
	done := make(chan struct{})
	go func() {
		for i := 0; i < 5; i++ {
			_, err := conn.Write(append(enclaveID.Bytes(), '\n'))
			if err != nil {
				return // Connection closed
			}
			time.Sleep(interval / 2)
		}
		close(done)
	}()

	// Wait for heartbeats to complete
	select {
	case <-done:
		// Success, now cancel context to stop watchdog
		watchCtxCancel()
	case err := <-errCh:
		t.Fatalf("watchdog returned unexpectedly: %v", err)
	case <-time.After(interval * 6):
		t.Fatal("timeout waiting for heartbeats to complete")
	}

	// Verify watchdog exits with nil error when context is canceled
	select {
	case err := <-errCh:
		require.NoError(t, err, "watchdog should return nil error when context is canceled")
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for watchdog to exit after cancellation")
	}
}

func TestWatchdogContextCancellation(t *testing.T) {
	t.Parallel()
	interval := 10 * time.Second // Long interval to prevent timeout
	dog, listener, _ := setupWatchdogTest(t, interval)
	defer listener.Close() //nolint:errcheck

	// Create a specific cancellable context for this test
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	errCh := make(chan error)

	go func() {
		errCh <- dog.StartServerSide(ctx, listener)
	}()

	// Wait a bit then cancel the context
	time.Sleep(100 * time.Millisecond)
	cancel()

	// Verify watchdog exits without error
	select {
	case err := <-errCh:
		require.NoError(t, err, "watchdog should return nil error when context is canceled")
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for watchdog to exit after cancellation")
	}
}
