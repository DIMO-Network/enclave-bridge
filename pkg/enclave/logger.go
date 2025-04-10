package enclave

import (
	"fmt"
	"io"
	"runtime/debug"

	"github.com/mdlayher/vsock"
	"github.com/rs/zerolog"
)

// DefaultLogger creates a new logger with the given app name.
func DefaultLogger(appName string, writer io.Writer) zerolog.Logger {
	logger := zerolog.New(writer).With().Timestamp().Str("app", appName).Logger()
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" && len(s.Value) == 40 {
				logger = logger.With().Str("commit", s.Value[:7]).Logger()
				break
			}
		}
	}
	return logger
}

// SetLoggerLevel sets the log level for the logger if the level is not empty.
func SetLoggerLevel(level string) error {
	if level != "" {
		lvl, err := zerolog.ParseLevel(level)
		if err != nil {
			return fmt.Errorf("failed to parse log level: %w", err)
		}
		zerolog.SetGlobalLevel(lvl)
	}
	return nil
}

// DefaultLoggerWithSocket creates a new logger that logs to a vsock socket.
func DefaultLoggerWithSocket(appName string, port uint32) (zerolog.Logger, func(), error) {
	conn, err := vsock.Dial(DefaultHostCID, port, nil)
	if err != nil {
		return zerolog.Logger{}, nil, fmt.Errorf("failed to dial socket: %w", err)
	}
	close := func() {
		_ = conn.Close() //nolint:errcheck
	}
	logger := DefaultLogger(appName, conn)
	return logger, close, nil
}
