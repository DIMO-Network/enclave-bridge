package enclave

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"

	"github.com/mdlayher/vsock"
	"github.com/rs/zerolog/log"
)

// StdoutTunnel is a tunnel that copies data from the vsock connection to stdout.
type StdoutTunnel struct {
	port uint32
}

// Port returns the port of the ClientTunnel.
func (c *StdoutTunnel) Port() uint32 {
	return c.port
}

// NewStdoutTunnel creates a new StdoutTunnel.
func NewStdoutTunnel(port uint32) *StdoutTunnel {
	return &StdoutTunnel{
		port: port,
	}
}

// HandleConn dial a vsock connection and copy data in both directions.
func (c *StdoutTunnel) HandleConn(vsockConn net.Conn) error {
	defer vsockConn.Close()
	_, err := io.Copy(vsockConn, os.Stdout)
	if err != nil {
		return fmt.Errorf("failed to copy data from TCP target to vsock client: %w", err)
	}
	return nil
}

// ListenForTargetRequests listens for target requests on the vsock port.
func (c *StdoutTunnel) ListenForTargetRequests(ctx context.Context) error {
	listener, err := vsock.ListenContextID(DefaultHostCID, c.port, nil)
	if err != nil {
		return fmt.Errorf("failed to listen for target requests: %w", err)
	}
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
			return fmt.Errorf("failed to accept target request: %w", err)
		}
		log.Debug().Msgf("Accepted target request from %s", conn.RemoteAddr())

		go c.HandleConn(conn)
	}
}
