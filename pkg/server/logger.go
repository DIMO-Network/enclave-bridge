package server

import (
	"os"
	"runtime/debug"

	"github.com/rs/zerolog"
)

// DefaultLogger creates a new logger with the given app name.
func DefaultLogger(appName string) *zerolog.Logger {
	logger := zerolog.New(os.Stdout).With().Timestamp().Str("app", appName).Logger()
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" && len(s.Value) == 40 {
				logger = logger.With().Str("commit", s.Value[:7]).Logger()
				break
			}
		}
	}
	return &logger
}

// SetLevel sets the log level for the logger if the level is not empty.
func SetLevel(logger *zerolog.Logger, level string) {
	if level != "" {
		lvl, err := zerolog.ParseLevel(level)
		if err != nil {
			logger.Fatal().Err(err).Msg("Failed to parse log level.")
		}
		zerolog.SetGlobalLevel(lvl)
	}
}
