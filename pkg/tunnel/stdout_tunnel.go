package tunnel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"

	"github.com/DIMO-Network/enclave-bridge/pkg/enclave"
	"github.com/mdlayher/vsock"
	"github.com/rs/zerolog"
)

// StdoutTunnel is a tunnel that copies data from the vsock connection to stdout.
type StdoutTunnel struct {
	port   uint32
	logger *zerolog.Logger
	pool   sync.Pool
}

// Port returns the port of the ClientTunnel.
func (c *StdoutTunnel) Port() uint32 {
	return c.port
}

// NewStdoutTunnel creates a new StdoutTunnel.
func NewStdoutTunnel(port uint32, logger zerolog.Logger) *StdoutTunnel {
	return &StdoutTunnel{
		port:   port,
		logger: &logger,
		pool:   sync.Pool{New: func() any { b := make([]byte, bufSize); return &b }},
	}
}

// HandleConn dial a vsock connection and copy data in both directions.
func (c *StdoutTunnel) HandleConn(vsockConn net.Conn) {
	defer vsockConn.Close() //nolint:errcheck
	buf := c.pool.Get().(*[]byte)
	defer c.pool.Put(buf)
	_, err := io.CopyBuffer(os.Stdout, vsockConn, *buf)
	if err != nil {
		c.logger.Error().Err(err).Msg("Failed to copy data from vsock to stdout")
		return
	}
}

// ListenForTargetRequests listens for target requests on the vsock port.
func (c *StdoutTunnel) ListenForTargetRequests(ctx context.Context) error {
	listener, err := vsock.ListenContextID(enclave.DefaultHostCID, c.port, nil)
	if err != nil {
		return fmt.Errorf("failed to listen for target requests: %w", err)
	}
	go func() {
		<-ctx.Done()
		_ = listener.Close() //nolint:errcheck
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("failed to accept target request: %w", err)
		}

		go c.HandleConn(conn)
	}
}
