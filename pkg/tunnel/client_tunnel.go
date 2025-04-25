package tunnel

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/DIMO-Network/enclave-bridge/pkg/enclave"
	"github.com/mdlayher/vsock"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
)

// ClientTunnel is a struct that contains the port, request timeout, logger, and pool for the client tunnel.
type ClientTunnel struct {
	port           uint32
	requestTimeout time.Duration
	logger         *zerolog.Logger
	pool           sync.Pool
}

// Port returns the port of the ClientTunnel.
func (c *ClientTunnel) Port() uint32 {
	return c.port
}

func NewClientTunnel(port uint32, requestTimeout time.Duration, logger zerolog.Logger) *ClientTunnel {
	if requestTimeout == 0 {
		requestTimeout = 5 * time.Minute
	}
	return &ClientTunnel{
		port:           port,
		requestTimeout: requestTimeout,
		logger:         &logger,
		pool:           sync.Pool{New: func() any { b := make([]byte, bufSize); return &b }},
	}
}

// HandleConn dial a vsock connection and copy data in both directions.
func (c *ClientTunnel) HandleConn(ctx context.Context, vsockConn net.Conn) {
	defer vsockConn.Close() //nolint:errcheck
	// Create a context with timeout for the entire operation
	requestCtx, cancel := context.WithTimeout(ctx, c.requestTimeout)
	defer cancel()

	// Create a buffered reader to read the target URL
	reader := bufio.NewReader(vsockConn)

	// Read the first line which should contain the target URL
	targetLine, err := reader.ReadBytes('\n')
	if err != nil {
		c.logger.Error().Err(err).Msg("Failed to read target URL")
		return
	}
	// Remove the newline character
	targetAddress := string(targetLine[:len(targetLine)-1])
	c.logger.Trace().Msgf("Received target request: %s", targetAddress)

	// Use a dialer with context
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
	}

	targetConn, err := dialer.DialContext(requestCtx, "tcp", targetAddress)
	if err != nil {
		c.logger.Error().Err(err).Msg("Failed to dial target service")
		return
	}
	defer targetConn.Close() //nolint:errcheck

	_, err = vsockConn.Write(enclave.ACK)
	if err != nil {
		c.logger.Error().Err(err).Msg("Failed to write ACK to target service")
		return
	}

	// Create error group for goroutine coordination
	group, _ := errgroup.WithContext(requestCtx)

	// From vsock client to TCP target
	group.Go(func() error {
		buf := c.pool.Get().(*[]byte)
		defer c.pool.Put(buf)
		_, err := io.CopyBuffer(targetConn, vsockConn, *buf)
		if err != nil {
			return fmt.Errorf("failed to copy data from vsock client to TCP target: %w", err)
		}
		return nil
	})

	// From TCP target to vsock client
	group.Go(func() error {
		buf := c.pool.Get().(*[]byte)
		defer c.pool.Put(buf)
		_, err := io.CopyBuffer(vsockConn, targetConn, *buf)
		if err != nil {
			return fmt.Errorf("failed to copy data from TCP target to vsock client: %w", err)
		}
		return nil
	})

	// Wait for either an error or context cancellation
	if err := group.Wait(); err != nil {
		c.logger.Error().Err(err).Msg("Connection error occurred")
	}
}

// ListenForTargetRequests listens for target requests on the vsock port.
func (c *ClientTunnel) ListenForTargetRequests(ctx context.Context) error {
	listener, err := vsock.ListenContextID(enclave.DefaultHostCID, c.port, nil)
	if err != nil {
		c.logger.Error().Err(err).Msg("Failed to listen for target requests")
		return fmt.Errorf("failed to listen for target requests: %w", err)
	}
	c.logger.Info().Msgf("Listening for target requests on port %d", c.port)
	go func() {
		<-ctx.Done()
		_ = listener.Close() //nolint:errcheck
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			conn, err := listener.Accept()
			if err != nil {
				if errors.Is(err, net.ErrClosed) {
					return nil
				}
				c.logger.Error().Err(err).Msg("Failed to accept target request")
				continue
			}

			go c.HandleConn(ctx, conn)
		}
	}
}
