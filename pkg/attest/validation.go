package attest

import (
	"crypto/ecdsa"
	"crypto/sha512"
	"crypto/x509"
	"errors"
	"fmt"
	"math/big"

	"github.com/fxamacker/cbor/v2"
)

// validateAttestation performs syntactic, semantic and cryptographic validation of the attestation document.
func validateAttestation(coseSign1 COSESign1) error {
	// Syntactic validation
	if err := validateSyntactic(coseSign1); err != nil {
		return fmt.Errorf("syntactic validation failed: %w", err)
	}

	// Semantic validation
	if err := validateSemantic(coseSign1.Payload); err != nil {
		return fmt.Errorf("semantic validation failed: %w", err)
	}

	// Cryptographic validation
	if err := validateCryptographic(coseSign1); err != nil {
		return fmt.Errorf("cryptographic validation failed: %w", err)
	}

	return nil
}

// validateSyntactic performs syntactic validation of the attestation document.
func validateSyntactic(coseSign1 COSESign1) error {
	// Validate protected header
	if len(coseSign1.Protected) != 4 {
		return fmt.Errorf("invalid protected header length: expected 4 bytes, got %d", len(coseSign1.Protected))
	}

	// Parse protected header as CBOR map
	var protectedMap map[int]int
	if err := cbor.Unmarshal(coseSign1.Protected, &protectedMap); err != nil {
		return fmt.Errorf("failed to parse protected header: %w", err)
	}

	// Validate algorithm (should be -35 for P-384)
	if alg, ok := protectedMap[1]; !ok {
		return errors.New("missing algorithm in protected header")
	} else if alg != -35 {
		return fmt.Errorf("invalid algorithm: expected -35 (P-384), got %d", alg)
	}

	// Validate unprotected header (should be empty)
	if len(coseSign1.Unprotected) != 0 {
		return fmt.Errorf("unprotected header should be empty, got %d items", len(coseSign1.Unprotected))
	}

	// Validate payload (should be a byte string)
	if len(coseSign1.Payload) == 0 {
		return errors.New("payload is empty")
	}

	return nil
}

// validateSemantic performs semantic validation of the attestation document.
func validateSemantic(payload []byte) error {
	// Parse the payload as an AttestationDocument
	var attestDoc AttestationDocument
	if err := cbor.Unmarshal(payload, &attestDoc); err != nil {
		return fmt.Errorf("failed to parse attestation document: %w", err)
	}

	// Validate mandatory fields
	if attestDoc.ModuleID == "" {
		return errors.New("missing module_id")
	}
	if attestDoc.Digest == "" {
		return errors.New("missing digest")
	}
	if attestDoc.Timestamp == 0 {
		return errors.New("missing timestamp")
	}
	if len(attestDoc.PCRs) == 0 {
		return errors.New("missing PCRs")
	}
	if len(attestDoc.Certificate) == 0 {
		return errors.New("missing certificate")
	}
	if len(attestDoc.CABundle) == 0 {
		return errors.New("missing CA bundle")
	}

	// Validate certificate chain
	if err := validateCertificateChain(attestDoc.Certificate, attestDoc.CABundle); err != nil {
		return fmt.Errorf("certificate chain validation failed: %w", err)
	}

	return nil
}

// validateCertificateChain validates the certificate chain.
func validateCertificateChain(certBytes []byte, caBundle [][]byte) error {
	// Parse the certificate
	cert, err := x509.ParseCertificate(certBytes)
	if err != nil {
		return fmt.Errorf("failed to parse certificate: %w", err)
	}

	// Create a new certificate pool
	roots := x509.NewCertPool()
	for _, caBytes := range caBundle {
		ca, err := x509.ParseCertificate(caBytes)
		if err != nil {
			return fmt.Errorf("failed to parse CA certificate: %w", err)
		}
		roots.AddCert(ca)
	}

	// Verify the certificate chain
	opts := x509.VerifyOptions{
		Roots: roots,
	}
	if _, err := cert.Verify(opts); err != nil {
		return fmt.Errorf("certificate verification failed: %w", err)
	}

	return nil
}

// validateCryptographic performs cryptographic validation of the attestation document.
func validateCryptographic(coseSign1 COSESign1) error {
	// Parse the certificate from the payload
	var attestDoc AttestationDocument
	if err := cbor.Unmarshal(coseSign1.Payload, &attestDoc); err != nil {
		return fmt.Errorf("failed to parse attestation document: %w", err)
	}

	// Parse the certificate
	cert, err := x509.ParseCertificate(attestDoc.Certificate)
	if err != nil {
		return fmt.Errorf("failed to parse certificate: %w", err)
	}

	// Get the public key
	pubKey, ok := cert.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		return errors.New("invalid public key type: not ECDSA")
	}

	// For P-384, the signature is 96 bytes (48 bytes for R, 48 bytes for S)
	if len(coseSign1.Signature) != 96 {
		return fmt.Errorf("invalid signature length: expected 96 bytes for P-384, got %d", len(coseSign1.Signature))
	}

	// Split the signature into R and S components
	r := new(big.Int).SetBytes(coseSign1.Signature[:48])
	s := new(big.Int).SetBytes(coseSign1.Signature[48:])

	// Create the COSE_Sign1 structure to be signed
	// This matches the C code's sig_struct_buffer construction
	sigStructure := []any{
		"Signature1",        // context
		coseSign1.Protected, // protected headers
		[]byte{},            // external aad
		coseSign1.Payload,   // payload
	}

	// Serialize the structure to CBOR
	sigStructureBytes, err := cbor.Marshal(sigStructure)
	if err != nil {
		return fmt.Errorf("failed to serialize signature structure: %w", err)
	}

	// Hash the signature structure with SHA-384
	hash := sha512.Sum384(sigStructureBytes)

	// Verify the signature
	if !ecdsa.Verify(pubKey, hash[:], r, s) {
		return errors.New("signature verification failed")
	}

	return nil
}
