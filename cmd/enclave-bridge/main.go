package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/DIMO-Network/sample-enclave-api/pkg/config"
	"github.com/DIMO-Network/sample-enclave-api/pkg/enclave"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/adaptor"
	"github.com/mdlayher/vsock"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
	"inet.af/tcpproxy"
)

const (
	defaultMonPort = 8888
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	group, groupCtx := errgroup.WithContext(ctx)

	monApp := CreateMonitoringServer(strconv.Itoa(defaultMonPort))
	RunFiber(ctx, monApp, ":"+strconv.Itoa(defaultMonPort), group)

	// Wait for enclave to start and send config
	logger := DefaultLogger("enclave-bridge")
	logger.Info().Msg("Waiting for config...")
	bridgeSettings, err := waitForConfig(ctx, &logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to wait for config.")
	}
	logger = DefaultLogger(bridgeSettings.AppName).With().Str("app", bridgeSettings.AppName).Str("component", "enclave-bridge").Logger()

	// create a flag for the settings file

	SetLevel(&logger, bridgeSettings.LogLevel)
	serverTunnels := make([]*enclave.ServerTunnel, 0, len(bridgeSettings.Servers))
	for _, serversSettings := range bridgeSettings.Servers {
		serverTunnels = append(serverTunnels, enclave.NewServerTunnel(serversSettings.EnclaveCID, serversSettings.EnclaveListenPort, &logger))
	}
	clientTunnels := make([]*enclave.ClientTunnel, 0, len(bridgeSettings.Clients))
	for _, clientSettings := range bridgeSettings.Clients {
		clientTunnels = append(clientTunnels, enclave.NewClientTunnel(clientSettings.EnclaveDialPort, clientSettings.RequestTimeout, &logger))
	}

	for _, serverTunnel := range serverTunnels {
		portStr := strconv.FormatUint(uint64(serverTunnel.Port()), 10)
		logger.Info().Str("port", portStr).Msgf("Starting bridge enclave")
		runProxy(groupCtx, serverTunnel, ":"+portStr, group)
	}
	for _, clientTunnel := range clientTunnels {
		portStr := strconv.FormatUint(uint64(clientTunnel.Port()), 10)
		logger.Info().Str("port", portStr).Msgf("Starting bridge client")
		runProxyClient(groupCtx, clientTunnel, group)
	}

	err = group.Wait()
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to run servers.")
	}
}

func runProxyClient(ctx context.Context, proxy *enclave.ClientTunnel, group *errgroup.Group) {
	group.Go(func() error {
		return proxy.ListenForTargetRequests(ctx)
	})
}

func runProxy(ctx context.Context, target tcpproxy.Target, addr string, group *errgroup.Group) {
	proxy := tcpproxy.Proxy{}
	group.Go(func() error {
		proxy.AddRoute(addr, target)
		return proxy.Run()
	})
	group.Go(func() error {
		<-ctx.Done()
		proxy.Close()
		return nil
	})
}

func CreateMonitoringServer(port string) *fiber.App {
	monApp := fiber.New(fiber.Config{DisableStartupMessage: true})
	monApp.Get("/", func(c *fiber.Ctx) error { return nil })
	monApp.Get("/metrics", adaptor.HTTPHandler(promhttp.Handler()))
	return monApp
}

// waitForConfig starts listening on the default vsock port for a config file and returns the config.
func waitForConfig(ctx context.Context, logger *zerolog.Logger) (*config.BridgeSettings, error) {
	var conn net.Conn
	var listener *vsock.Listener
	var err error
	for {
		listener, err = vsock.ListenContextID(enclave.DefaultHostCID, enclave.InitPort, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to listen for target requests: %w", err)
		}
		conn, err = listen(ctx, listener)
		if err == nil {
			break
		}
		logger.Error().Err(err).Msg("Failed to listen for target requests")
		time.Sleep(1 * time.Second)
	}
	defer listener.Close()
	defer conn.Close()

	configBytes, err := io.ReadAll(conn)
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	var config config.BridgeSettings
	err = json.Unmarshal(configBytes, &config)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}
	logger.Debug().Interface("config", config).Msg("Received config")

	return &config, nil
}

func listen(ctx context.Context, listener *vsock.Listener) (net.Conn, error) {
	defer listener.Close()
	conn, err := listener.Accept()
	if err != nil {
		return nil, fmt.Errorf("failed to accept target request: %w", err)
	}
	return conn, nil

}

// RunFiber runs a fiber server and returns a context that can be used to stop the server.
func RunFiber(ctx context.Context, fiberApp *fiber.App, addr string, group *errgroup.Group) {
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
