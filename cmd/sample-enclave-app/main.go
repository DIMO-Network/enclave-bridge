package main

import (
	"context"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/DIMO-Network/sample-enclave-api/internal/app"
	"github.com/DIMO-Network/sample-enclave-api/pkg/server"
	"github.com/mdlayher/vsock"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
)

const (
	// backlog is the maximum number of pending connections to queue.
	backlog = 5
	// heartInterval is the interval to check if the enclave is still alive.
	heartInterval = 10 * time.Second
	// bufSize is the size of the buffer to receive messages.
	bufSize = 4096
)

func main() {
	logger := server.DefaultLogger("sample-enclave-app")
	if len(os.Args) < 2 {
		logger.Fatal().Msg("Port argument required.")
	}
	port, err := strconv.Atoi(os.Args[1])
	if err != nil {
		logger.Fatal().Err(err).Msgf("Couldn't parse port %q.", os.Args[1])
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	listener, err := vsock.Listen(uint32(port), nil)
	if err != nil {
		logger.Fatal().Err(err).Msgf("Couldn't listen on port %d.", port)
	}
	logger.Debug().Msgf("Listening on %s", listener.Addr())

	enclaveApp, err := app.CreateEnclaveWebServer(logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("Couldn't create enclave web server.")
	}

	_ = enclave(ctx, port, logger)
	group, gCtx := errgroup.WithContext(ctx)
	server.RunFiberWithListener(gCtx, enclaveApp, listener, group)

	err = group.Wait()
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to run servers.")
	}
}

func enclave(ctx context.Context, port int, logger *zerolog.Logger) error {
	// fd, err := unix.Socket(unix.AF_VSOCK, unix.SOCK_STREAM, 0)
	// if err != nil {
	// 	return err
	// }

	// logger.Debug().Msgf("Created socket %d.", fd)

	// socketAddress := &unix.SockaddrVM{
	// 	CID:  unix.VMADDR_CID_ANY,
	// 	Port: uint32(port),
	// }

	// if err := unix.Bind(fd, socketAddress); err != nil {
	// 	return err
	// }

	// logger.Debug().Msgf("Bound socket with a random address and port %d.", port)

	// if err := unix.Listen(fd, backlog); err != nil {
	// 	return err
	// }

	// logger.Debug().Msgf("Accepting requests with backlog %d.", backlog)

	go func() {
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
	}()

	// for {
	// 	select {
	// 	case <-ctx.Done():
	// 		return nil
	// 	default:
	// 		if err := accept(fd, logger); err != nil {
	// 			logger.Err(err).Msg("Accept failed.")
	// 		}
	// 	}
	// }
	return nil
}

// func accept(fd int, logger *zerolog.Logger) error {
// 	nfd, _, err := unix.Accept(fd)
// 	if err != nil {
// 		return fmt.Errorf("failed to accept connection: %w", err)
// 	}
// 	defer unix.Close(nfd)

// 	buf := make([]byte, bufSize)
// 	readBytes, _, err := unix.Recvfrom(nfd, buf, 0)
// 	if err != nil {
// 		return fmt.Errorf("failed to receive message: %w", err)
// 	}

// 	logger.Debug().Msg("Got message.")

// 	// respond with hey I got: <message>
// 	err = unix.Send(nfd, []byte("hey I got: "+string(buf[:readBytes])), 0)
// 	if err != nil {
// 		return fmt.Errorf("failed to send response: %w", err)
// 	}

// 	return nil
// }
