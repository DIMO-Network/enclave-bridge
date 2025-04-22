package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/DIMO-Network/enclave-bridge/pkg/config"
	"github.com/DIMO-Network/enclave-bridge/pkg/enclave"
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

// ReadyFunc is a function that returns an error if the enclave is not ready.
type ReadyFunc func() error

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	group, groupCtx := errgroup.WithContext(ctx)

	// Start monitoring server
	monApp := CreateMonitoringServer(strconv.Itoa(defaultMonPort))
	RunFiber(ctx, monApp, ":"+strconv.Itoa(defaultMonPort), group)

	logger := enclave.DefaultLogger("enclave-bridge", os.Stdout)
	group.Go(func() error {
		return StartBridge(groupCtx, &logger)
	})
	err := group.Wait()
	if err != nil {
		logger.Fatal().Err(err).Msg("Bridge failed")
	}
}

func runBridge(ctx context.Context, bridgeSettings *config.BridgeSettings, readyFunc func() error) error {
	logger := enclave.DefaultLogger(bridgeSettings.AppName, os.Stdout).With().Str("component", "enclave-bridge").Logger()

	group, groupCtx := errgroup.WithContext(ctx)

	// Set up logger.
	err := enclave.SetLoggerLevel(bridgeSettings.Logger.Level)
	if err != nil {
		return fmt.Errorf("failed to set logger level: %w", err)
	}
	stdoutTunnel := enclave.NewStdoutTunnel(bridgeSettings.Logger.EnclaveDialPort, logger.With().Str("component", "stdout-tunnel").Logger())
	runClientTunnel(groupCtx, stdoutTunnel, group)

	// Set up server tunnels.
	for _, serversSettings := range bridgeSettings.Servers {
		serverTunnel := enclave.NewServerTunnel(serversSettings.EnclaveCID, serversSettings.EnclaveListenPort, logger.With().Str("component", "server-tunnel").Logger())
		portStr := strconv.FormatUint(uint64(serversSettings.BridgeTCPPort), 10)
		logger.Info().Str("port", portStr).Msgf("Starting Bridge server")
		runServerTunnel(groupCtx, serverTunnel, ":"+portStr, group)
	}

	// Set up client tunnels.
	for _, clientSettings := range bridgeSettings.Clients {
		clientTunnel := enclave.NewClientTunnel(clientSettings.EnclaveDialPort, clientSettings.RequestTimeout, logger.With().Str("component", "client-tunnel").Logger())
		portStr := strconv.FormatUint(uint64(clientSettings.EnclaveDialPort), 10)
		logger.Info().Str("port", portStr).Msgf("Starting Bridge client")
		runClientTunnel(groupCtx, clientTunnel, group)
	}

	err = readyFunc()
	if err != nil {
		return fmt.Errorf("failed to ACK to enclave: %w", err)
	}

	err = group.Wait()
	if err != nil {
		return fmt.Errorf("failed to run servers: %w", err)
	}
	return nil
}

type tunnel interface {
	ListenForTargetRequests(ctx context.Context) error
}

func runClientTunnel(ctx context.Context, proxy tunnel, group *errgroup.Group) {
	var waitGroup sync.WaitGroup
	waitGroup.Add(1)
	group.Go(func() error {
		waitGroup.Done()
		return proxy.ListenForTargetRequests(ctx)
	})
	waitGroup.Wait()
}

func runServerTunnel(ctx context.Context, target tcpproxy.Target, addr string, group *errgroup.Group) {
	proxy := tcpproxy.Proxy{}
	proxy.AddRoute(addr, target)
	var waitGroup sync.WaitGroup
	waitGroup.Add(2)
	group.Go(func() error {
		waitGroup.Done()
		return proxy.Run()
	})
	group.Go(func() error {
		waitGroup.Done()
		<-ctx.Done()
		_ = proxy.Close()
		return nil
	})
	waitGroup.Wait()
}

// CreateMonitoringServer creates a fiber server that listens for requests on the given port.
func CreateMonitoringServer(port string) *fiber.App {
	monApp := fiber.New(fiber.Config{DisableStartupMessage: true})
	monApp.Get("/", func(c *fiber.Ctx) error { return nil })
	monApp.Get("/metrics", adaptor.HTTPHandler(promhttp.Handler()))
	return monApp
}

// StartBridge listens for a new connection and then starts a new bridge instance.
func StartBridge(parentCtx context.Context, logger *zerolog.Logger) error {
	var listener *vsock.Listener
	var err error
	initPort := os.Getenv("VSOCK_INIT_PORT")
	initPortInt := enclave.InitPort
	if initPort != "" {
		initPortInt64, err := strconv.ParseUint(initPort, 10, 32)
		if err != nil {
			return fmt.Errorf("failed to convert VSOCK_INIT_PORT to int: %w", err)
		}
		initPortInt = uint32(initPortInt64)
	}
	var cancelFunc context.CancelFunc
	var wg sync.WaitGroup

	listener, err = vsock.ListenContextID(enclave.DefaultHostCID, initPortInt, nil)
	if err != nil {
		return fmt.Errorf("failed to listen for target requests: %w", err)
	}
	logger.Info().Msg("Waiting for new connection...")
	// accept connections until the context is canceled
	for {
		conn, err := listener.Accept()
		if err != nil {
			logger.Error().Err(err).Msg("Failed to accept target request")
			time.Sleep(1 * time.Second)
			continue
		}
		logger.Info().Msg("Starting new bridge")
		// on accept stop the currently running bridge
		cancelFunc()
		wg.Wait()
		var bridgeCtx context.Context
		bridgeCtx, cancelFunc = context.WithCancel(parentCtx)
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := handleInit(bridgeCtx, logger, conn)
			if err != nil {
				// if we are restarting do to a new connection just log the error and continue
				// else this should be a fatal error
				if errors.Is(err, context.Canceled) {
					logger.Error().Err(err).Msg("Bridge context canceled")
				} else {
					logger.Fatal().Err(err).Msg("Failed to handle init")
				}
			}
		}()
	}
}

func handleInit(ctx context.Context, logger *zerolog.Logger, conn net.Conn) error {
	go func() {
		<-ctx.Done()
		logger.Info().Msg("Context canceled, shutting down bridge instance")
	}()
	settings, readyFunc, err := completeHandshake(ctx, logger, conn)
	if err != nil {
		return fmt.Errorf("failed to complete handshake: %w", err)
	}
	return runBridge(ctx, settings, readyFunc)
}

func completeHandshake(ctx context.Context, logger *zerolog.Logger, conn net.Conn) (*config.BridgeSettings, func() error, error) {
	logger.Info().Msg("Sending Environment to enclave")
	environment, err := config.SerializeEnvironment("")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to serialize environment: %w", err)
	}
	err = CtxWrite(ctx, conn, append(environment, '\n'))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to write environment: %w", err)
	}
	logger.Info().Msg("Waiting for enclave to send bridge configuration")
	// read until a new line
	configBytes, err := CtxReadBytes(ctx, conn, '\n')
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read config: %w", err)
	}

	var settings config.BridgeSettings
	err = json.Unmarshal(configBytes, &settings)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}
	if settings.Error != "" {
		return nil, nil, fmt.Errorf("enclave failed to configure: %s", settings.Error)
	}
	readyFunc := func() error {
		logger.Debug().Msg("Sending start ACK to enclave")
		defer conn.Close() //nolint:errcheck
		// Send ACK to enclave
		err = CtxWrite(ctx, conn, []byte{enclave.ACK})
		if err != nil {
			return fmt.Errorf("failed to send ACK to enclave: %w", err)
		}
		return nil
	}
	return &settings, readyFunc, nil
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

// CtxWrite writes to a connection and returns after the write has completed or the context is canceled.
func CtxWrite(ctx context.Context, conn net.Conn, data []byte) error {
	writeChan := make(chan error)
	go func() {
		_, err := conn.Write(data)
		writeChan <- err
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-writeChan:
		return err
	}
}

// CtxReadUntil reads from a connection until the context is canceled.
func CtxReadBytes(ctx context.Context, conn net.Conn, delim byte) ([]byte, error) {
	reader := bufio.NewReader(conn)
	readChan := make(chan error)
	var bytes []byte
	go func() {
		var err error
		bytes, err = reader.ReadBytes(delim)
		readChan <- err
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case err := <-readChan:
		return bytes, err
	}
}
