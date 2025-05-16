// Package main demonstrates a simple enclave application that communicates with the enclave-bridge.
// This example shows how to:
// 1. Set up logging to the bridge
// 2. Establish communication with the bridge using the handshake protocol
// 3. Create a simple HTTP server in the enclave that's accessible from the host
// 4. Handle graceful shutdown
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	bridgecfg "github.com/DIMO-Network/enclave-bridge/pkg/config"
	"github.com/DIMO-Network/enclave-bridge/pkg/enclave"
	"github.com/DIMO-Network/enclave-bridge/pkg/enclave/handshake"
	"github.com/DIMO-Network/enclave-bridge/pkg/watchdog"
	"github.com/gofiber/fiber/v2"
	"github.com/mdlayher/vsock"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
)

const (
	// appName is used for logging and identification
	appName = "simple-enclave"
	// serverTunnelPort is the VSOCK port the enclave server listens on
	// This port is mapped to BridgeTCPPort (8080) on the host
	serverTunnelPort uint32 = 5001
	// clientTunnelPort is the VSOCK port used for outbound connections from the enclave
	// This allows the enclave to make HTTP requests to external services
	clientTunnelPort uint32 = 5002
)

func main() {
	// Setup context with signal handling for graceful shutdown
	// This allows the application to clean up resources when receiving SIGTERM or SIGINT
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	group, gCtx := errgroup.WithContext(ctx)

	// Setup logging to bridge
	// This creates a logger that sends logs through VSOCK to the bridge
	// If the VSOCK connection fails, it falls back to stdout
	logger, cleanup, err := enclave.GetAndSetDefaultLoggerWithSocket(appName, enclave.StdoutPort)
	if err != nil {
		logger = zerolog.New(os.Stdout).With().Timestamp().Logger()
		logger.Fatal().Err(err).Msg("Failed to create logger socket.")
	}
	defer cleanup()

	// Log shutdown signal
	// This goroutine logs when the application is shutting down
	go func() {
		<-ctx.Done()
		logger.Info().Msg("Received signal in enclave, shutting down...")
	}()

	// Get enclave's context ID
	// The context ID is used to identify this enclave to the bridge
	// It's required for the VSOCK communication
	cid, err := vsock.ContextID()
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to get context ID.")
	}

	// Start bridge handshake
	// This initiates the handshake protocol with the bridge
	// The handshake establishes the initial connection and exchanges configuration
	var bridgeSetup handshake.BridgeHandshake
	err = bridgeSetup.StartHandshake(ctx)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to setup bridge.")
	}

	// Configure bridge settings
	// This defines how the bridge should handle communication with this enclave
	bridgeSettings := bridgecfg.BridgeSettings{
		AppName: appName,
		Logger: bridgecfg.LoggerSettings{
			Level: "debug",
		},
		// Server settings define how the bridge should expose the enclave's server
		// In this case, port 8080 on the host will be mapped to port 5001 in the enclave
		Servers: []bridgecfg.ServerSettings{
			{
				EnclaveCID:        cid,
				EnclaveListenPort: serverTunnelPort,
				BridgeTCPPort:     8080, // This port will be exposed on the host
			},
		},
		// Client settings define how the enclave can make outbound connections
		// This allows the enclave to make HTTP requests to external services
		Clients: []bridgecfg.ClientSettings{
			{
				EnclaveDialPort: clientTunnelPort,
				RequestTimeout:  time.Minute * 5,
			},
		},
		// Watchdog settings ensure the bridge can monitor the enclave's health
		Watchdog: watchdog.NewStandardSettings(),
	}

	// Run handshake in background
	// This completes the handshake process and starts the watchdog
	group.Go(func() error {
		err := bridgeSetup.FinishHandshakeAndWait(ctx, &bridgeSettings)
		if err != nil {
			return fmt.Errorf("failed to run handshake: %w", err)
		}
		return nil
	})

	// Wait for bridge setup
	// This ensures the bridge is fully configured before starting the server
	logger.Debug().Msg("Waiting for bridge setup")
	err = bridgeSetup.WaitForBridgeSetup()
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to setup bridge.")
	}

	// Create VSOCK listener for the enclave server
	// This creates a listener on the specified VSOCK port
	// The bridge will forward TCP connections from the host to this port
	listener, err := vsock.Listen(serverTunnelPort, nil)
	if err != nil {
		logger.Fatal().Err(err).Msgf("Couldn't listen on port %d.", serverTunnelPort)
	}
	logger.Info().Msgf("Listening on %s", listener.Addr())

	// Create simple Fiber app
	// This is a basic HTTP server that will run inside the enclave
	app := fiber.New()
	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendString("Hello from the enclave!")
	})

	// Run the server
	// This starts the HTTP server using the VSOCK listener
	group.Go(func() error {
		if err := app.Listener(listener); err != nil {
			return fmt.Errorf("failed to start server: %w", err)
		}
		return nil
	})

	// Handle shutdown
	// This ensures the server shuts down gracefully when the context is cancelled
	group.Go(func() error {
		<-gCtx.Done()
		if err := app.Shutdown(); err != nil {
			return fmt.Errorf("failed to shutdown server: %w", err)
		}
		return nil
	})

	// Wait for all goroutines
	// This blocks until all goroutines complete or an error occurs
	err = group.Wait()
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to run servers.")
	}
}
