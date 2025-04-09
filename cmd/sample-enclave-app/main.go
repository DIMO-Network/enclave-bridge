package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/DIMO-Network/sample-enclave-api/internal/app"
	"github.com/DIMO-Network/sample-enclave-api/internal/config"
	bridgecfg "github.com/DIMO-Network/sample-enclave-api/pkg/config"
	"github.com/DIMO-Network/sample-enclave-api/pkg/enclave"
	"github.com/DIMO-Network/shared"
	"github.com/gofiber/fiber/v2"
	"github.com/mdlayher/vsock"
	"golang.org/x/sync/errgroup"
)

const (
	// heartInterval is the interval to check if the enclave is still alive.
	heartInterval    = 10 * time.Second
	appName          = "sample-enclave"
	serverTunnelPort = uint32(5001)
	clientTunnelPort = uint32(5001)
	loggerPort       = uint32(5002)
)

func main() {
	tmpLogger := enclave.DefaultLogger(appName, os.Stdout)
	tmpLogger.Debug().Msg("Starting enclave app")
	cid, err := vsock.ContextID()
	if err != nil {
		tmpLogger.Fatal().Err(err).Msg("Failed to get context ID.")
	}
	settingsFile := flag.String("settings", "settings.yaml", "settings file")
	flag.Parse()
	settings, err := shared.LoadConfig[config.Settings](*settingsFile)
	if err != nil {
		tmpLogger.Fatal().Err(err).Msg("Couldn't load settings.")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	bridgeSettings := bridgecfg.BridgeSettings{
		AppName: appName,
		Logger: bridgecfg.LoggerSettings{
			Level:           settings.LogLevel,
			EnclaveDialPort: loggerPort,
		},
		Servers: []bridgecfg.ServerSettings{
			{
				EnclaveCID:        cid,
				EnclaveListenPort: serverTunnelPort,
				BridgeTCPPort:     uint32(settings.Port),
			},
		},
		Clients: []bridgecfg.ClientSettings{
			{
				EnclaveDialPort: clientTunnelPort,
				RequestTimeout:  time.Minute * 5,
			},
		},
	}
	err = enclave.SendConfig(&bridgeSettings)
	if err != nil {
		tmpLogger.Fatal().Err(err).Msg("Failed to setup bridge.")
	}
	logger, cleanup, err := enclave.DefaultWithSocket(appName, loggerPort)
	if err != nil {
		tmpLogger.Fatal().Err(err).Msg("Failed to create logger socket.")
	}
	defer cleanup()

	listener, err := vsock.Listen(serverTunnelPort, nil)
	if err != nil {
		logger.Fatal().Err(err).Msgf("Couldn't listen on port %d.", serverTunnelPort)
	}
	logger.Info().Msgf("Listening on %s", listener.Addr())

	enclaveApp, err := app.CreateEnclaveWebServer(&logger, clientTunnelPort)
	if err != nil {
		logger.Fatal().Err(err).Msg("Couldn't create enclave web server.")
	}

	group, gCtx := errgroup.WithContext(ctx)
	RunFiberWithListener(gCtx, enclaveApp, listener, group)

	err = group.Wait()
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to run servers.")
	}
}

// RunFiberWithListener runs a fiber server with a listener and returns a context that can be used to stop the server.
func RunFiberWithListener(ctx context.Context, fiberApp *fiber.App, listener net.Listener, group *errgroup.Group) {
	group.Go(func() error {
		if err := fiberApp.Listener(listener); err != nil {
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
