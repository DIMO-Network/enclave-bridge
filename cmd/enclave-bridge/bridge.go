package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/DIMO-Network/enclave-bridge/pkg/config"
	"github.com/DIMO-Network/enclave-bridge/pkg/enclave"
	"github.com/DIMO-Network/enclave-bridge/pkg/tunnel"
	"github.com/DIMO-Network/enclave-bridge/pkg/watchdog"
	"github.com/gofiber/fiber/v2"
	"github.com/mdlayher/vsock"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
	"inet.af/tcpproxy"
)

// InitPortEnvVar is the environment variable used to set the init port.
const (
	InitPortEnvVar   = "ENCLAVE_BRIDGE_VSOCK_INIT_PORT"
	StdoutPortEnvVar = "ENCLAVE_BRIDGE_VSOCK_STDOUT_PORT"
	readTimeout      = time.Second * 10
)

// Bridge is a struct that handles running the enclave-bridge.
type Bridge struct {
	settings  *config.BridgeSettings
	readyFunc func() error
	listener  net.Listener
}

// CreateBridge listens for a new connection and then starts a new bridge instance.
func CreateBridge(parentCtx context.Context) (*Bridge, error) {
	logger := zerolog.Ctx(parentCtx)
	initPort, err := getInitPort()
	if err != nil {
		return nil, err
	}
	// Create new listener that waits for a new enclave to initiate a handshake
	listener, err := vsock.ListenContextID(enclave.DefaultHostCID, initPort, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to listen for target requests: %w", err)
	}

	// Keep waiting until and enclave is up and connected
	logger.Info().Msg("Waiting for new connection...")
	var conn net.Conn
	for {
		conn, err = listener.Accept()
		if err == nil {
			break
		}
		// Check if the context was canceled
		if parentCtx.Err() != nil {
			_ = listener.Close()
			return nil, parentCtx.Err()
		}
		// keep trying until the context is canceled
		logger.Error().Err(err).Msg("Failed to accept target request")
	}

	// Wait for the enclave to send an ACK. If an ack is not sent then this may not be a new enclave and we want to bail.
	readCtx, readCancel := context.WithTimeout(parentCtx, readTimeout)
	defer readCancel()
	ack, err := enclave.ReadBytesWithContext(readCtx, conn, '\n')
	if err != nil {
		_ = listener.Close()
		_ = conn.Close()
		return nil, fmt.Errorf("failed to read ACK: %w", err)
	}
	if !bytes.Equal(ack, enclave.ACK) {
		_ = listener.Close()
		_ = conn.Close()
		return nil, errors.New("first message from enclave was not an ACK")
	}
	logger.Info().Msg("Starting new bridge")

	bridge, err := completeHandshake(parentCtx, logger, conn)
	if err != nil {
		_ = listener.Close()
		_ = conn.Close()
		return nil, fmt.Errorf("failed to complete handshake: %w", err)
	}
	bridge.listener = listener
	return bridge, nil
}

func completeHandshake(ctx context.Context, logger *zerolog.Logger, conn net.Conn) (*Bridge, error) {
	logger.Info().Msg("Sending Environment to enclave")
	environment, err := config.SerializeEnvironment("")
	if err != nil {
		return nil, fmt.Errorf("failed to serialize environment: %w", err)
	}
	err = enclave.WriteWithContext(ctx, conn, append(environment, '\n'))
	if err != nil {
		return nil, fmt.Errorf("failed to write environment: %w", err)
	}

	logger.Info().Msg("Waiting for enclave to send bridge configuration")
	readCtx, readCancel := context.WithTimeout(ctx, readTimeout)
	defer readCancel()
	configBytes, err := enclave.ReadBytesWithContext(readCtx, conn, '\n')
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}
	var settings config.BridgeSettings
	err = json.Unmarshal(configBytes, &settings)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// readyFunc is a function that sends an ACK to the enclave and closes the connection when the bridge is all setup
	readyFunc := func() error {
		logger.Debug().Msg("Sending start ACK to enclave")
		defer func() {
			closeErr := conn.Close()
			if closeErr != nil {
				logger.Warn().Err(closeErr).Msg("Error closing connection after ACK")
			}
		}()

		// Send ACK to enclave
		err = enclave.WriteWithContext(ctx, conn, enclave.ACK)
		if err != nil {
			return fmt.Errorf("failed to send ACK to enclave: %w", err)
		}
		return nil
	}
	return &Bridge{settings: &settings, readyFunc: readyFunc}, nil
}

// Run runs the bridge by starting all client and server tunnels.
// Run blocks until the context is canceled or an error occurs.
func (b *Bridge) Run(ctx context.Context) error {
	group, groupCtx := errgroup.WithContext(ctx)
	logger := zerolog.Ctx(ctx).With().Str("component", "enclave-bridge").Logger()
	groupCtx = logger.WithContext(groupCtx)

	// Set up logger.
	err := enclave.SetLoggerLevel(b.settings.Logger.Level)
	if err != nil {
		return fmt.Errorf("failed to set logger level: %w", err)
	}

	// Set up server tunnels.
	for _, serversSettings := range b.settings.Servers {
		serverTunnel := tunnel.NewServerTunnel(serversSettings.EnclaveCID, serversSettings.EnclaveListenPort, logger.With().Str("component", "server-tunnel").Logger())
		portStr := strconv.FormatUint(uint64(serversSettings.BridgeTCPPort), 10)
		logger.Info().Str("port", portStr).Msgf("Starting Bridge server")
		runServerTunnel(groupCtx, serverTunnel, ":"+portStr, group)
	}

	// Set up client tunnels.
	for _, clientSettings := range b.settings.Clients {
		clientTunnel := tunnel.NewClientTunnel(clientSettings.EnclaveDialPort, clientSettings.RequestTimeout, logger.With().Str("component", "client-tunnel").Logger())
		portStr := strconv.FormatUint(uint64(clientSettings.EnclaveDialPort), 10)
		logger.Info().Str("port", portStr).Msgf("Starting Bridge client")
		runClientTunnel(groupCtx, clientTunnel, group)
	}

	watchDog, err := watchdog.New(&b.settings.Watchdog)
	if err != nil {
		return fmt.Errorf("failed to create watchdog: %w", err)
	}
	group.Go(func() error {
		return watchDog.StartServerSide(groupCtx, b.listener)
	})

	err = b.readyFunc()
	if err != nil {
		return fmt.Errorf("failed to ACK to enclave: %w", err)
	}

	err = group.Wait()
	if err != nil {
		return fmt.Errorf("failed to run servers: %w", err)
	}
	return nil
}

// runFiber runs a fiber server and returns a context that can be used to stop the server.
func runFiber(ctx context.Context, fiberApp *fiber.App, addr string, group *errgroup.Group) {
	group.Go(func() error {
		if err := fiberApp.Listen(addr); err != nil {
			return fmt.Errorf("failed to start server: %w", err)
		}
		return nil
	})
	group.Go(func() error {
		<-ctx.Done()
		if err := fiberApp.Shutdown(); err != nil {
			return fmt.Errorf("failed to shutdown server: %w", err)
		}
		return nil
	})
}

type targetListener interface {
	ListenForTargetRequests(ctx context.Context) error
}

func runClientTunnel(ctx context.Context, proxy targetListener, group *errgroup.Group) {
	// No need for waitGroup since errgroup handles waiting for goroutines
	group.Go(func() error {
		return proxy.ListenForTargetRequests(ctx)
	})
}

func runServerTunnel(ctx context.Context, target tcpproxy.Target, addr string, group *errgroup.Group) {
	proxy := tcpproxy.Proxy{}
	proxy.AddRoute(addr, target)

	// First goroutine to run the proxy
	group.Go(func() error {
		err := proxy.Run()
		if err != nil {
			return fmt.Errorf("proxy run failed: %w", err)
		}
		return nil
	})

	// Second goroutine to handle shutdown
	group.Go(func() error {
		<-ctx.Done()
		err := proxy.Close()
		if err != nil {
			return fmt.Errorf("proxy close failed: %w", err)
		}
		return nil
	})
}

func getInitPort() (uint32, error) {
	initPort := os.Getenv(InitPortEnvVar)
	if initPort == "" {
		return enclave.InitPort, nil
	}
	initPortInt64, err := strconv.ParseUint(initPort, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("failed to convert VSOCK_INIT_PORT to int: %w", err)
	}
	return uint32(initPortInt64), nil
}

func getStdoutPort() (uint32, error) {
	stdoutPort := os.Getenv(StdoutPortEnvVar)
	if stdoutPort == "" {
		return enclave.StdoutPort, nil
	}
	stdoutPortInt64, err := strconv.ParseUint(stdoutPort, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("failed to convert VSOCK_STDOUT_PORT to int: %w", err)
	}
	return uint32(stdoutPortInt64), nil
}
