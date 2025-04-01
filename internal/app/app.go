package app

import (
	"errors"
	"fmt"
	"strings"

	"github.com/DIMO-Network/sample-enclave-api/internal/config"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/rs/zerolog"
	"golang.org/x/sys/unix"
)

// CreateWebServer creates a new web server with the given logger and settings.
func CreateWebServer(logger *zerolog.Logger, settings *config.Settings) (*fiber.App, error) {
	app := fiber.New(fiber.Config{
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			return ErrorHandler(c, err, logger)
		},
		DisableStartupMessage: true,
	})

	app.Use(recover.New(recover.Config{
		Next:              nil,
		EnableStackTrace:  true,
		StackTraceHandler: nil,
	}))
	app.Use(cors.New())
	app.Get("/", HealthCheck)
	app.Get("/forward", forward(logger, settings.EnclaveCID, settings.EnclavePort))

	return app, nil
}

func forward(logger *zerolog.Logger, cid uint32, port uint32) fiber.Handler {
	return func(ctx *fiber.Ctx) error {
		msg := ctx.Query("msg")
		if msg == "" {
			msg = "Hello, World!"
		}

		fd, err := unix.Socket(unix.AF_VSOCK, unix.SOCK_STREAM, 0)
		if err != nil {
			return fmt.Errorf("failed to create socket: %w", err)
		}
		defer unix.Close(fd) //nolint

		logger.Debug().Msgf("Created socket %d.", fd)

		sa := &unix.SockaddrVM{CID: cid, Port: port}

		if err := unix.Connect(fd, sa); err != nil {
			return fmt.Errorf("failed to connect to socket: %w", err)
		}
		logger.Debug().Msgf("Connected to socket %d.", fd)

		if _, err := unix.Write(fd, []byte(msg)); err != nil {
			return fmt.Errorf("failed to write to socket: %w", err)
		}
		logger.Debug().Msgf("Wrote to '%s' to socket %d.", msg, fd)

		buf := make([]byte, 4096)
		n, err := unix.Read(fd, buf)
		if err != nil {
			return fmt.Errorf("failed to read from socket: %w", err)
		}
		logger.Debug().Msgf("Read from '%s' to socket %d.", string(buf[:n]), fd)

		return ctx.JSON(map[string]string{"data": string(buf[:n])})
	}
}

// HealthCheck godoc
// @Summary Show the status of server.
// @Description get the status of server.
// @Tags root
// @Accept */*
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router / [get]
func HealthCheck(ctx *fiber.Ctx) error {
	res := map[string]any{
		"data": "Server is up and running",
	}

	return ctx.JSON(res)
}

// ErrorHandler custom handler to log recovered errors using our logger and return json instead of string
func ErrorHandler(ctx *fiber.Ctx, err error, logger *zerolog.Logger) error {
	code := fiber.StatusInternalServerError // Default 500 statuscode
	message := "Internal error."

	var e *fiber.Error
	if errors.As(err, &e) {
		code = e.Code
		message = e.Message
	}

	// don't log not found errors
	if code != fiber.StatusNotFound {
		logger.Err(err).Int("httpStatusCode", code).
			Str("httpPath", strings.TrimPrefix(ctx.Path(), "/")).
			Str("httpMethod", ctx.Method()).
			Msg("caught an error from http request")
	}

	return ctx.Status(code).JSON(codeResp{Code: code, Message: message})
}

type codeResp struct {
	Message string `json:"message"`
	Code    int    `json:"code"`
}
