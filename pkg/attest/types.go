package attest

// COSESign1 represents a COSE_Sign1 message structure.
type COSESign1 struct {
	_           struct{} `cbor:",toarray"`
	Protected   []byte
	Unprotected map[string]interface{}
	Payload     []byte
	Signature   []byte
}

// AttestationDocument represents the attestation document structure.
type AttestationDocument struct {
	// ModuleID is the issuing NSM ID
	ModuleID string `cbor:"module_id"`

	// Digest is the digest function used for calculating the register values
	// Can be: "SHA256" | "SHA512"
	Digest string `cbor:"digest"`

	// Timestamp is the UTC time when document was created expressed as milliseconds since Unix Epoch
	Timestamp uint64 `cbor:"timestamp"`

	// PCRs is the map of all locked PCRs at the moment the attestation document was generated
	PCRs map[int][]byte `cbor:"pcrs"`

	// Certificate is the infrastructure certificate used to sign the document, DER encoded
	Certificate []byte `cbor:"certificate"`

	// CABundle is the issuing CA bundle for infrastructure certificate
	CABundle [][]byte `cbor:"cabundle"`

	// PublicKey is an optional DER-encoded key the attestation consumer can use to encrypt data with
	PublicKey []byte `cbor:"public_key,omitempty"`

	// UserData is additional signed user data, as defined by protocol
	UserData []byte `cbor:"user_data,omitempty"`

	// Nonce is an optional cryptographic nonce provided by the attestation consumer as a proof of authenticity
	Nonce []byte `cbor:"nonce,omitempty"`
}
