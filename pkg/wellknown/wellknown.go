// Package wellknown provides fiber controllers for well-known endpoints.
package wellknown

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/DIMO-Network/enclave-bridge/pkg/attest"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/gofiber/fiber/v2"
	"github.com/hf/nitrite"
	"github.com/hf/nsm/request"
	"github.com/rs/zerolog"
)

const (
	maxNonceLength = 64 // Maximum length for nonce parameter
)

// NsmAttestationResponse is the response from the NSM attestation.
type NsmAttestationResponse struct {
	Attestation *nitrite.Result `json:"attestation"`
	Document    []byte          `json:"document"`
}

// KeysResponse is the response for the keys endpoint.
type KeysResponse struct {
	PublicKey       string `json:"publicKey"`
	EthereumAddress string `json:"ethereumAddress"`
}

// RegisterRoutes adds the well-known routes for an enclave to a fiber app.
func RegisterRoutes(app *fiber.App, controller *Controller) {
	wellKnown := app.Group("/.well-known")
	wellKnown.Get("nsm-attestation", controller.GetNSMAttestations)
	if controller.publicKey != nil {
		wellKnown.Get("keys", controller.GetKeys)
	}
}

// Controller is a controller for well-known endpoints including NSM attestation.
type Controller struct {
	publicKey   *ecdsa.PublicKey
	getCertFunc func(*tls.ClientHelloInfo) (*tls.Certificate, error)
	cachedResp  atomic.Pointer[NsmAttestationResponse]
}

// NewController creates a new Controller.
func NewController(
	publicKey *ecdsa.PublicKey,
	getCertFunc func(*tls.ClientHelloInfo) (*tls.Certificate, error),
) (*Controller, error) {
	return &Controller{
		publicKey:   publicKey,
		getCertFunc: getCertFunc,
	}, nil
}

// GetKeys godoc
// @Summary Get public keys
// @Description Get the public key and Ethereum address of the controller
// @Tags keys
// @Accept json
// @Produce json
// @Success 200 {object} KeysResponse
// @Router /keys [get]
func (c *Controller) GetKeys(ctx *fiber.Ctx) error {
	keyResponse := KeysResponse{
		PublicKey:       "0x" + hex.EncodeToString(crypto.FromECDSAPub(c.publicKey)),
		EthereumAddress: crypto.PubkeyToAddress(*c.publicKey).Hex(),
	}
	return ctx.JSON(keyResponse)
}

// GetNSMAttestations godoc
// @Summary Get NSM attestation
// @Description Get the Nitro Security Module attestation
// @Tags attestation
// @Accept json
// @Produce json
// @Param nonce query string false "Nonce"
// @Success 200 {object} NsmAttestationResponse
// @Failure 400 {object} codeResp
// @Failure 500 {object} codeResp
// @Router /.well-known/nsm-attestation [get]
func (c *Controller) GetNSMAttestations(ctx *fiber.Ctx) error {
	logger := zerolog.Ctx(ctx.UserContext())
	nonceStr := ctx.Query("nonce")
	var nonce []byte
	if len(nonceStr) > maxNonceLength {
		return fiber.NewError(fiber.StatusBadRequest, "nonce too long")
	}
	if len(nonceStr) > 0 {
		nonce = []byte(nonceStr)
	}

	certBytes, err := c.getCert()
	if err != nil {
		logger.Error().Err(err).Msg("Failed to get certificate")
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to get certificate")
	}

	// Check cache
	if nonce == nil {
		if cached := c.cachedResp.Load(); cached != nil && c.isValidCache(cached, certBytes) {
			return ctx.JSON(*cached)
		}
	}

	// Clear cache if certificate is expired
	if cached := c.cachedResp.Load(); cached != nil && !c.isValidCache(cached, certBytes) {
		c.cachedResp.Store(nil)
	}

	req := &request.Attestation{
		PublicKey: crypto.FromECDSAPub(c.publicKey),
		UserData:  certBytes,
		Nonce:     nonce,
	}

	document, nsmResult, err := attest.GetNSMAttestation(req)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to get NSM attestation")
		return fiber.NewError(fiber.StatusInternalServerError, "Failed to get NSM attestation")
	}

	resp := NsmAttestationResponse{
		Attestation: nsmResult,
		Document:    document,
	}

	if nonce == nil {
		// Update cache
		c.cachedResp.Store(&resp)
	}

	return ctx.JSON(resp)
}

// isValidCache checks if the cached result is valid
func (c *Controller) isValidCache(cached *NsmAttestationResponse, certBytes []byte) bool {
	return len(cached.Attestation.Certificates) > 0 &&
		cached.Attestation.Certificates[0] != nil &&
		cached.Attestation.Certificates[0].NotBefore.Before(time.Now()) &&
		cached.Attestation.Certificates[0].NotAfter.After(time.Now()) &&
		bytes.Equal(cached.Attestation.Document.UserData, certBytes)
}

func (c *Controller) getCert() ([]byte, error) {
	if c.getCertFunc == nil {
		return nil, nil
	}
	cert, err := c.getCertFunc(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get certificate: %w", err)
	}
	if cert == nil {
		return nil, nil
	}

	certBytes, err := x509.MarshalPKIXPublicKey(cert.Certificate[0])
	if err != nil {
		return nil, fmt.Errorf("failed to marshal certificate: %w", err)
	}
	return certBytes, nil
}
