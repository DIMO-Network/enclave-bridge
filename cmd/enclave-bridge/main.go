package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/DIMO-Network/enclave-bridge/pkg/enclave"
	"github.com/DIMO-Network/enclave-bridge/pkg/tunnel"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/adaptor"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/sync/errgroup"
)

const (
	defaultMonPort = 8888
)

// ReadyFunc is a function that returns an error if the enclave is not ready.
type ReadyFunc func() error

func main() {
	parentCtx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	logger := enclave.GetAndSetDefaultLogger("enclave-bridge", os.Stdout)
	go func() {
		<-parentCtx.Done()
		logger.Info().Msg("Received signal, shutting down...")
	}()
	group, groupCtx := errgroup.WithContext(parentCtx)

	stdoutPort, err := getStdoutPort()
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to get stdout port")
	}
	stdoutTunnel := tunnel.NewStdoutTunnel(stdoutPort, logger.With().Str("component", "stdout-tunnel").Logger())
	runClientTunnel(groupCtx, stdoutTunnel, group)

	// Start monitoring server
	monApp := CreateMonitoringServer()
	runFiber(groupCtx, monApp, ":"+strconv.Itoa(defaultMonPort), group)
	bridge, err := CreateBridge(groupCtx)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to create bridge")
	}
	group.Go(func() error {
		return bridge.Run(groupCtx)
	})
	err = group.Wait()
	if err != nil && !errors.Is(err, context.Canceled) {
		logger.Fatal().Err(err).Msg("Bridge failed")
	}
}

// CreateMonitoringServer creates a fiber server that listens for requests on the given port.
func CreateMonitoringServer() *fiber.App {
	monApp := fiber.New(fiber.Config{DisableStartupMessage: true})
	monApp.Get("/", func(*fiber.Ctx) error { return nil })
	monApp.Get("/metrics", adaptor.HTTPHandler(promhttp.Handler()))
	return monApp
}
