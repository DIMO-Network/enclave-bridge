package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
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
	logger := enclave.DefaultLogger("enclave-bridge", os.Stdout)
	logger.Info().Msg("Waiting for config...")
	bridgeSettings, err := waitForConfig(ctx, &logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to wait for config.")
	}
	logger = enclave.DefaultLogger(bridgeSettings.AppName, os.Stdout).With().Str("app", bridgeSettings.AppName).Str("component", "enclave-bridge").Logger()

	// Set up logger.
	enclave.SetLevel(&logger, bridgeSettings.Logger.Level)
	stdoutTunnel := enclave.NewStdoutTunnel(bridgeSettings.Logger.EnclaveDialPort)
	runClientTunnel(groupCtx, stdoutTunnel, group)

	// Set up server tunnels.
	for _, serversSettings := range bridgeSettings.Servers {
		serverTunnel := enclave.NewServerTunnel(serversSettings.EnclaveCID, serversSettings.EnclaveListenPort, &logger)
		portStr := strconv.FormatUint(uint64(serversSettings.BridgeTCPPort), 10)
		logger.Info().Str("port", portStr).Msgf("Starting Bridge server")
		runServerTunnel(groupCtx, serverTunnel, ":"+portStr, group)
	}

	// Set up client tunnels.
	for _, clientSettings := range bridgeSettings.Clients {
		clientTunnel := enclave.NewClientTunnel(clientSettings.EnclaveDialPort, clientSettings.RequestTimeout, &logger)
		portStr := strconv.FormatUint(uint64(clientSettings.EnclaveDialPort), 10)
		logger.Info().Str("port", portStr).Msgf("Starting Bridge client")
		runClientTunnel(groupCtx, clientTunnel, group)
	}

	err = group.Wait()
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to run servers.")
	}
}

type tunnel interface {
	ListenForTargetRequests(ctx context.Context) error
}

func runClientTunnel(ctx context.Context, proxy tunnel, group *errgroup.Group) {
	group.Go(func() error {
		return proxy.ListenForTargetRequests(ctx)
	})
}

func runServerTunnel(ctx context.Context, target tcpproxy.Target, addr string, group *errgroup.Group) {
	proxy := tcpproxy.Proxy{}
	proxy.AddRoute(addr, target)
	group.Go(func() error {
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
	logger.Info().Msg("Accepted connection")
	// read until a new line
	reader := bufio.NewReader(conn)
	configBytes, err := reader.ReadBytes('\n')
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
