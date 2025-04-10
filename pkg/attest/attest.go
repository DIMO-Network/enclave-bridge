package attest

import (
	"crypto/ecdsa"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/hf/nitrite"
	"github.com/hf/nsm"
	"github.com/hf/nsm/request"
)

// GetNSMAttestationAndKey gets the NSM attestation and the private key that was included in the attestation.
func GetNSMAttestationAndKey() (*ecdsa.PrivateKey, *nitrite.Result, error) {
	// create private key
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		return nil, nil, err
	}

	req := &request.Attestation{
		PublicKey: crypto.FromECDSAPub(&privateKey.PublicKey),
	}

	attResult, err := GetNSMAttestation(req)
	if err != nil {
		return nil, nil, err
	}

	return privateKey, attResult, nil
}

// GetNSMAttestation gets the NSM attestation that includes the provided private key.
func GetNSMAttestation(attestationRequest *request.Attestation) (*nitrite.Result, error) {
	// call nsm with private key
	attesationDocument, err := getNSMDocument(attestationRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to get NSM document: %w", err)
	}

	res, err := nitrite.Verify(attesationDocument, nitrite.VerifyOptions{CurrentTime: time.Now()})
	if err != nil {
		return nil, fmt.Errorf("failed to verify nsm attestation document: %w", err)
	}

	// return the document
	return res, nil
}

func getNSMDocument(attestationRequest *request.Attestation) ([]byte, error) {
	// create a new session
	session, err := nsm.OpenDefaultSession()
	if err != nil {
		return nil, fmt.Errorf("failed to open NSM session: %w", err)
	}
	defer session.Close() //nolint:errcheck

	// send the request
	res, err := session.Send(attestationRequest)
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
