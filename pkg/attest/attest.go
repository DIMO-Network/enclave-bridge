package attest

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"fmt"
	"time"

	"github.com/hf/nitrite"
	"github.com/hf/nsm"
	"github.com/hf/nsm/request"
	"github.com/rs/zerolog"
)

type NSMResponse struct {
	RawAttestation []byte            `json:"rawAttestation"`
	COSESign1      []byte            `json:"coseSign1"`
	Document       *nitrite.Document `json:"document"`
	Certificate    *x509.Certificate `json:"certificate"`
	IsValid        bool              `json:"isValid"`
}

// GetNSMAttesation gets the NSM attestation.
func GetNSMAttestation(logger *zerolog.Logger) (*NSMResponse, error) {
	// create private key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("failed to generate private key: %w", err)
	}
	// call nsm with private key
	attesationDocument, err := getNSMDocument(privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get NSM document: %w", err)
	}

	res, err := nitrite.Verify(attesationDocument, nitrite.VerifyOptions{CurrentTime: time.Now()})
	if err != nil {
		return nil, fmt.Errorf("failed to verify nsm attestation document: %w", err)
	}

	// return the document
	return &NSMResponse{
		RawAttestation: attesationDocument,
		COSESign1:      res.COSESign1,
		Document:       res.Document,
		Certificate:    res.Certificates[0],
		IsValid:        err == nil,
	}, nil

}

func getNSMDocument(privateKey *rsa.PrivateKey) ([]byte, error) {
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
