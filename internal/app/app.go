package app

import (
	"errors"
	"strings"

	"github.com/DIMO-Network/sample-enclave-api/internal/client/identity"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/rs/zerolog"
)

// CreateEnclaveWebServer creates a new web server with the given logger and settings.
func CreateEnclaveWebServer(logger *zerolog.Logger, port uint32) (*fiber.App, error) {
	app := fiber.New(fiber.Config{
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			return ErrorHandler(c, err, logger)
		},
		DisableStartupMessage: true,
	})
	identClient, err := identity.NewService("https://identity-api.dimo.zone", port)
	if err != nil {
		return nil, err
	}
	ctrl := NewController(
		identClient,
		logger,
	)
	app.Use(recover.New(recover.Config{
		Next:              nil,
		EnableStackTrace:  true,
		StackTraceHandler: nil,
	}))
	app.Use(cors.New())
	app.Get("/", HealthCheck)
	app.Get("/forward", func(ctx *fiber.Ctx) error {
		logger.Debug().Msg("Forward request received")
		msg := ctx.Query("msg")
		if msg == "" {
			msg = "Hello, World!"
		}
		return ctx.JSON(map[string]string{"data": "Hello From The Enclave! Did you say: " + msg})
	})
	app.Get("/vehicle/:tokenId", ctrl.GetVehicleInfo)
	return app, nil
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

// ErrorHandler custom handler to log recovered errors using our logger and return json instead of string.
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
