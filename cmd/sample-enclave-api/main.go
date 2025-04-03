package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/DIMO-Network/sample-enclave-api/internal/config"
	"github.com/DIMO-Network/sample-enclave-api/pkg/server"
	"github.com/DIMO-Network/shared"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/adaptor"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
	"inet.af/tcpproxy"
)

// @title                       DIMO Fetch API
// @version                     1.0
// @securityDefinitions.apikey  BearerAuth
// @in                          header
// @name                        Authorization
func main() {
	logger := server.DefaultLogger("sample-enclave-api")

	// create a flag for the settings file
	settingsFile := flag.String("settings", "settings.yaml", "settings file")
	flag.Parse()
	settings, err := shared.LoadConfig[config.Settings](*settingsFile)
	if err != nil {
		logger.Fatal().Err(err).Msg("Couldn't load settings.")
	}
	server.SetLevel(logger, settings.LogLevel)

	vsockProxy := NewServerTunnel(settings.EnclaveCID, settings.EnclavePort, logger)
	vsockClientProxy := NewClientTunnel(settings.EnclavePort, 5*time.Minute, logger)

	monApp := CreateMonitoringServer(strconv.Itoa(settings.MonPort), logger)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	group, groupCtx := errgroup.WithContext(ctx)

	logger.Info().Str("port", strconv.Itoa(settings.MonPort)).Msgf("Starting monitoring server")
	server.RunFiber(groupCtx, monApp, ":"+strconv.Itoa(settings.MonPort), group)
	logger.Info().Str("port", strconv.Itoa(settings.Port)).Msgf("Starting proxy server")
	runProxy(groupCtx, vsockProxy, ":"+strconv.Itoa(settings.Port), group)
	logger.Info().Str("port", strconv.Itoa(int(settings.EnclavePort+1))).Msgf("Starting proxy client")
	runProxyClient(groupCtx, vsockClientProxy, group)

	err = group.Wait()
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to run servers.")
	}
}

func runProxyClient(ctx context.Context, proxy *ClientTunnel, group *errgroup.Group) {
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

func CreateMonitoringServer(port string, logger *zerolog.Logger) *fiber.App {
	monApp := fiber.New(fiber.Config{DisableStartupMessage: true})
	monApp.Get("/", func(c *fiber.Ctx) error { return nil })
	monApp.Get("/metrics", adaptor.HTTPHandler(promhttp.Handler()))

	return monApp
}
