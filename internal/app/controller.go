package app

import (
	"strconv"

	"github.com/DIMO-Network/sample-enclave-api/enclave-bridge/pkg/attest"
	"github.com/DIMO-Network/sample-enclave-api/internal/client/identity"
	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog"
)

type Controller struct {
	identityClient *identity.Service
	logger         *zerolog.Logger
}

func NewController(identityClient *identity.Service, logger *zerolog.Logger) *Controller {
	return &Controller{identityClient: identityClient, logger: logger}
}

func (c *Controller) GetVehicleInfo(ctx *fiber.Ctx) error {
	vehicleTokenID := ctx.Params("tokenId")
	vehicleTokenIDUint, err := strconv.ParseUint(vehicleTokenID, 10, 32)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "Invalid vehicle token Id")
	}

	vehicleInfo, err := c.identityClient.GetVehicleInfo(ctx.Context(), uint32(vehicleTokenIDUint))
	if err != nil {
		c.logger.Error().Err(err).Msg("Failed to get vehicle info")
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to get vehicle info")
	}

	return ctx.JSON(vehicleInfo)
}

func (c *Controller) GetNSMAttestations(ctx *fiber.Ctx) error {
	attestation, err := attest.GetNSMAttestation(c.logger)
	if err != nil {
		c.logger.Error().Err(err).Msg("Failed to get NSM attestations")
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to get NSM attestations")
	}

	return ctx.JSON(attestation)
}
