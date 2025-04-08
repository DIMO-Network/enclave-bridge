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
	"github.com/DIMO-Network/sample-enclave-api/pkg/server"
	"github.com/DIMO-Network/shared"
	"github.com/gofiber/fiber/v2"
	"github.com/mdlayher/vsock"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
)

const (
	// heartInterval is the interval to check if the enclave is still alive.
	heartInterval    = 10 * time.Second
	appName          = "sample-enclave"
	serverTunnelPort = 5001
	clientTunnelPort = 5001
)

func main() {
	logger := server.DefaultLogger(appName)
	logger.Info().Msg("Starting enclave app")
	cid, err := vsock.ContextID()
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to get context ID.")
	}
	settingsFile := flag.String("settings", "settings.yaml", "settings file")
	flag.Parse()
	settings, err := shared.LoadConfig[config.Settings](*settingsFile)
	if err != nil {
		logger.Fatal().Err(err).Msg("Couldn't load settings.")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	bridgeSettings := bridgecfg.BridgeSettings{
		AppName:  appName,
		LogLevel: settings.LogLevel,
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
	logger.Info().Msgf("Sending config to bridge")
	err = enclave.SendConfig(&bridgeSettings)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to setup bridge.")
	}

	listener, err := vsock.Listen(uint32(serverTunnelPort), nil)
	if err != nil {
		logger.Fatal().Err(err).Msgf("Couldn't listen on port %d.", serverTunnelPort)
	}
	logger.Debug().Msgf("Listening on %s", listener.Addr())

	enclaveApp, err := app.CreateEnclaveWebServer(logger, uint32(clientTunnelPort))
	if err != nil {
		logger.Fatal().Err(err).Msg("Couldn't create enclave web server.")
	}

	group, gCtx := errgroup.WithContext(ctx)
	RunFiberWithListener(gCtx, enclaveApp, listener, group)

	go heartbeatLog(ctx, logger)
	err = group.Wait()
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to run servers.")
	}
}

func heartbeatLog(ctx context.Context, logger *zerolog.Logger) {
	t := time.NewTicker(heartInterval)
	for {
		select {
		case <-t.C:
			logger.Debug().Msg("Enclave still alive.")
		case <-ctx.Done():
			t.Stop()
			return
		}
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
