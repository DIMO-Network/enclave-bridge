package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/DIMO-Network/sample-enclave-api/internal/config"
	"github.com/DIMO-Network/sample-enclave-api/pkg/server"
	"github.com/DIMO-Network/shared"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/adaptor"
	"github.com/mdlayher/vsock"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sys/unix"
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

	vsockProxy := &VSockProxy{
		CID:    settings.EnclaveCID,
		Port:   settings.EnclavePort,
		logger: logger,
	}
	vsockClientProxy := &VSockClientProxy{
		Port:   settings.EnclavePort,
		logger: logger,
	}

	monApp := CreateMonitoringServer(strconv.Itoa(settings.MonPort), logger)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	ctxId, err := vsock.ContextID()
	if err != nil {
		logger.Warn().Err(err).Msg("Failed to get context ID")
	}
	logger.Info().Msgf("Context ID: %d", ctxId)

	group, groupCtx := errgroup.WithContext(ctx)

	logger.Info().Str("port", strconv.Itoa(settings.MonPort)).Msgf("Starting monitoring server")
	server.RunFiber(groupCtx, monApp, ":"+strconv.Itoa(settings.MonPort), group)
	logger.Info().Str("port", strconv.Itoa(settings.Port)).Msgf("Starting proxy server")
	runProxy(groupCtx, vsockProxy, ":"+strconv.Itoa(settings.Port), group)
	logger.Info().Str("port", strconv.Itoa(int(settings.EnclavePort+1))).Msgf("Starting proxy client")
	runProxyClient(groupCtx, vsockClientProxy, group)

	ctxId, err = vsock.ContextID()
	if err != nil {
		logger.Warn().Err(err).Msg("Failed again to get context ID")
	}
	logger.Info().Msgf("Context ID2: %d", ctxId)

	err = group.Wait()
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to run servers.")
	}
}

func runProxyClient(ctx context.Context, proxy *VSockClientProxy, group *errgroup.Group) {
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

// VSockTarget implements tcpproxy.Target to forward connections to a VSock endpoint.
type VSockProxy struct {
	CID    uint32
	Port   uint32
	logger *zerolog.Logger
}

// HandleConn dial a vsock connection and copy data in both directions.
func (v *VSockProxy) HandleConn(conn net.Conn) {
	// Create a vsock connection to the target
	vsockConn, err := vsock.Dial(v.CID, v.Port, nil)
	if err != nil {
		v.logger.Error().Err(err).Msgf("Failed to dial vsock CID %d, Port %d", v.CID, v.Port)
		conn.Close()
		return
	}

	v.logger.Info().Msgf("Forwarding TCP connection to vsock CID %d, Port %d", v.CID, v.Port)

	// Create error group for goroutine coordination
	group, _ := errgroup.WithContext(context.Background())

	// From TCP proxy to vsock server
	group.Go(func() error {
		defer conn.Close()
		_, err := io.Copy(vsockConn, conn)
		if err != nil {
			return fmt.Errorf("failed to copy data from TCP proxy to vsock server: %w", err)
		}
		return nil
	})

	// From vsock server to TCP client
	group.Go(func() error {
		defer vsockConn.Close()
		_, err := io.Copy(conn, vsockConn)
		if err != nil {
			return fmt.Errorf("failed to copy data from vsock server to TCP client: %w", err)
		}
		return nil
	})

	// Wait for either an error or context cancellation
	if err := group.Wait(); err != nil {
		v.logger.Error().Err(err).Msg("Connection error occurred")
	}
}

type VSockClientProxy struct {
	Port   uint32
	logger *zerolog.Logger
}

// HandleConn dial a vsock connection and copy data in both directions.
func (v *VSockClientProxy) HandleConn(ctx context.Context, vsockConn net.Conn) {
	// Create a context with timeout for the entire operation
	proxyCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Create a buffered reader to read the target URL
	reader := bufio.NewReader(vsockConn)

	// Read the first line which should contain the target URL
	targetLine, err := reader.ReadString('\n')
	if err != nil {
		v.logger.Error().Err(err).Msg("Failed to read target URL")
		_ = vsockConn.Close()
		return
	}
	// Trim whitespace and extract the target URL
	targetAddress := strings.TrimSpace(targetLine)
	v.logger.Info().Msgf("Received target request: %s", targetAddress)

	// Use a dialer with context
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
	}

	targetConn, err := dialer.DialContext(proxyCtx, "tcp", targetAddress)
	if err != nil {
		v.logger.Error().Err(err).Msg("Failed to dial target service")
		_ = vsockConn.Close()
		return
	}

	// Create error group for goroutine coordination
	group, _ := errgroup.WithContext(proxyCtx)

	// From TCP target to vsock client
	group.Go(func() error {
		defer targetConn.Close()
		_, err := io.Copy(vsockConn, targetConn)
		if err != nil {
			return fmt.Errorf("failed to copy data from TCP target to vsock client: %w", err)
		}
		return nil
	})

	// From vsock client to TCP target
	group.Go(func() error {
		defer vsockConn.Close()
		_, err := io.Copy(targetConn, vsockConn)
		if err != nil {
			return fmt.Errorf("failed to copy data from vsock client to TCP target: %w", err)
		}
		return nil
	})

	// Wait for either an error or context cancellation
	if err := group.Wait(); err != nil {
		v.logger.Error().Err(err).Msg("Connection error occurred")
	}
}

func (v *VSockClientProxy) ListenForTargetRequests(ctx context.Context) error {
	listener, err := vsock.ListenContextID(unix.VMADDR_CID_ANY, v.Port, nil)
	if err != nil {
		v.logger.Error().Err(err).Msg("Failed to listen for target requests")
		return fmt.Errorf("failed to listen for target requests: %w", err)
	}
	v.logger.Info().Msgf("Listening for target requests on port %d", v.Port)
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
			v.logger.Error().Err(err).Msg("Failed to accept target request")
			continue
		}

		go v.HandleConn(ctx, conn)
	}
}
