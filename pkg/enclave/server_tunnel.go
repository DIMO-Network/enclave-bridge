package enclave

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"

	"github.com/mdlayher/vsock"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
)

const bufSize = 1024

// ServerTunnel implements tcpproxy.Target to forward connections to a VSock endpoint.
type ServerTunnel struct {
	cid       uint32
	port      uint32
	logger    *zerolog.Logger
	parentCtx context.Context //nolint:containedctx // This is needed since we can't pass a context into the HandleConn function
	cancel    context.CancelFunc
	pool      sync.Pool
}

// Port returns the port of the ServerTunnel.
func (v *ServerTunnel) Port() uint32 {
	return v.port
}

// CID returns the CID of the ServerTunnel.
func (v *ServerTunnel) CID() uint32 {
	return v.cid
}

// NewServerTunnel creates a new ServerTunnel.
func NewServerTunnel(cid uint32, port uint32, logger zerolog.Logger) *ServerTunnel {
	ctx, cancel := context.WithCancel(context.Background())
	return &ServerTunnel{
		cid:       cid,
		port:      port,
		logger:    &logger,
		parentCtx: ctx,
		cancel:    cancel,
		pool:      sync.Pool{New: func() any { b := make([]byte, bufSize); return &b }},
	}
}

// Stop stops the ServerTunnel.
func (v *ServerTunnel) Stop() {
	v.cancel()
}

// HandleConn dial a vsock connection and copy data in both directions.
func (v *ServerTunnel) HandleConn(conn net.Conn) {
	defer conn.Close() //nolint:errcheck
	// Create a vsock connection to the target
	vsockConn, err := vsock.Dial(v.cid, v.port, nil)
	if err != nil {
		v.logger.Error().Err(err).Msgf("Failed to dial vsock CID %d, Port %d", v.cid, v.port)
		return
	}

	v.logger.Trace().Msgf("Forwarding TCP connection to vsock CID %d, Port %d", v.cid, v.port)

	// Create error group for goroutine coordination
	group, _ := errgroup.WithContext(v.parentCtx)

	// From TCP proxy to vsock server
	group.Go(func() error {
		buf := v.pool.Get().(*[]byte)
		defer v.pool.Put(buf)
		_, err := io.CopyBuffer(vsockConn, conn, *buf)
		if err != nil {
			return fmt.Errorf("failed to copy data from TCP proxy to vsock server: %w", err)
		}
		return nil
	})

	// From vsock server to TCP client
	group.Go(func() error {
		buf := v.pool.Get().(*[]byte)
		defer v.pool.Put(buf)
		_, err := io.CopyBuffer(conn, vsockConn, *buf)
		if err != nil {
			return fmt.Errorf("failed to copy data from vsock server to TCP client: %w", err)
		}
		return nil
	})

	// Wait for either an error or context cancellation
	if err := group.Wait(); err != nil {
		v.logger.Error().Err(err).Msg("Connection error occurred")
	}
}
