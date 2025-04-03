package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/mdlayher/vsock"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
)

const defaultHostCID = 3

type ClientTunnel struct {
	Port           uint32
	RequestTimeout time.Duration
	Logger         *zerolog.Logger
}

// HandleConn dial a vsock connection and copy data in both directions.
func (c *ClientTunnel) HandleConn(ctx context.Context, vsockConn net.Conn) {
	defer vsockConn.Close()
	// Create a context with timeout for the entire operation
	requestCtx, cancel := context.WithTimeout(ctx, c.RequestTimeout)
	defer cancel()

	// Create a buffered reader to read the target URL
	reader := bufio.NewReader(vsockConn)

	// Read the first line which should contain the target URL
	targetLine, err := reader.ReadString('\n')
	if err != nil {
		c.Logger.Error().Err(err).Msg("Failed to read target URL")
		_ = vsockConn.Close()
		return
	}
	// Remove the newline character
	targetAddress := strings.TrimSpace(targetLine)
	c.Logger.Info().Msgf("Received target request: %s", targetAddress)

	// Use a dialer with context
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
	}

	targetConn, err := dialer.DialContext(requestCtx, "tcp", targetAddress)
	if err != nil {
		c.Logger.Error().Err(err).Msg("Failed to dial target service")
		return
	}
	defer targetConn.Close()

	// Create error group for goroutine coordination
	group, _ := errgroup.WithContext(requestCtx)

	// From TCP target to vsock client
	group.Go(func() error {
		_, err := io.Copy(vsockConn, targetConn)
		if err != nil {
			return fmt.Errorf("failed to copy data from TCP target to vsock client: %w", err)
		}
		return nil
	})

	// From vsock client to TCP target
	group.Go(func() error {
		_, err := io.Copy(targetConn, vsockConn)
		if err != nil {
			return fmt.Errorf("failed to copy data from vsock client to TCP target: %w", err)
		}
		return nil
	})

	// Wait for either an error or context cancellation
	if err := group.Wait(); err != nil {
		c.Logger.Error().Err(err).Msg("Connection error occurred")
	}
}

// ListenForTargetRequests listens for target requests on the vsock port.
func (c *ClientTunnel) ListenForTargetRequests(ctx context.Context) error {
	listener, err := vsock.ListenContextID(defaultHostCID, c.Port, nil)
	if err != nil {
		c.Logger.Error().Err(err).Msg("Failed to listen for target requests")
		return fmt.Errorf("failed to listen for target requests: %w", err)
	}
	c.Logger.Info().Msgf("Listening for target requests on port %d", c.Port)
	go func() {
		<-ctx.Done()
		listener.Close()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			c.Logger.Error().Err(err).Msg("Failed to accept target request")
			continue
		}

		go c.HandleConn(ctx, conn)
	}
}
