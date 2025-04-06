package attest

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"fmt"

	"github.com/fxamacker/cbor/v2"
	"github.com/hf/nsm"
	"github.com/hf/nsm/request"
	"github.com/rs/zerolog"
)

type NSMResponse struct {
	RawAttestation []byte              `json:"rawAttestation"`
	Attestation    COSESign1           `json:"attestation"`
	Document       AttestationDocument `json:"document"`
	Certificate    *x509.Certificate   `json:"certificate"`
	IsValid        bool                `json:"isValid"`
}

func GetNSMAttesation(logger *zerolog.Logger) (*NSMResponse, error) {
	// create private key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("failed to generate private key: %w", err)
	}
	// call nsm with private key
	rawNsmDocument0, err := getNSMDocument0(privateKey)
	if err != nil {
		logger.Error().Msgf("failed to get NSM document: %w", err)
	} else {
		logger.Info().Str("rawNsmDocument0", fmt.Sprintf("%v", rawNsmDocument0)).Str("rawNsmDocument0_base64", base64.StdEncoding.EncodeToString(rawNsmDocument0)).Msg("NSM document0")
	}

	coseSign1, attestDoc, err := getSignatureAndDocument(rawNsmDocument0)
	if err != nil {
		logger.Error().Msgf("failed to get signature and document: %w", err)
	}
	cert, err := x509.ParseCertificate(attestDoc.Certificate)
	if err != nil {
		logger.Error().Msgf("failed to parse certificate: %w", err)
	}

	err = validateAttestation(coseSign1)
	if err != nil {
		logger.Error().Msgf("failed to validate attestation: %w", err)
	}
	// return the document
	return &NSMResponse{
		RawAttestation: rawNsmDocument0,
		Attestation:    coseSign1,
		Document:       attestDoc,
		Certificate:    cert,
		IsValid:        err == nil,
	}, nil

}

func getNSMDocument0(privateKey *rsa.PrivateKey) ([]byte, error) {
	// create a new session
	session, err := nsm.OpenDefaultSession()
	if err != nil {
		return nil, fmt.Errorf("failed to open NSM session: %w", err)
	}
	defer session.Close()

	// create a new attestation request
	req := &request.Attestation{
		PublicKey: x509.MarshalPKCS1PublicKey(&privateKey.PublicKey),
	}

	// send the request
	res, err := session.Send(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send attestation request: %w", err)
	}

	// check for errors
	if res.Error != "" {
		return nil, fmt.Errorf("NSM returned error: %s", res.Error)
	}

	// return the document
	return res.Attestation.Document, nil
}

// decode NSM Attest rersponse
func getSignatureAndDocument(rawDoc []byte) (COSESign1, AttestationDocument, error) {
	// First try to unmarshal as COSESign1
	var coseSign1 COSESign1
	if err := cbor.Unmarshal(rawDoc, &coseSign1); err != nil {
		fmt.Printf("Error unmarshaling document 0 as COSESign1: %v\n", err)
		return COSESign1{}, AttestationDocument{}, fmt.Errorf("error unmarshaling document 0 as COSESign1: %v", err)
	}

	// Then try to unmarshal the payload as AttestationDocument
	var attestDoc AttestationDocument
	if err := cbor.Unmarshal(coseSign1.Payload, &attestDoc); err != nil {
		fmt.Printf("Error unmarshaling document 0 payload: %v\n", err)
		return COSESign1{}, AttestationDocument{}, fmt.Errorf("error unmarshaling document 0 payload: %v", err)
	}

	return coseSign1, attestDoc, nil
}
