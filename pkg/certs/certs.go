package certs

import (
	"crypto/tls"

	"github.com/DIMO-Network/enclave-bridge/pkg/config"
)

// GetCertificateFunc is a function that returns a certificate for the given settings.
type GetCertificateFunc func(*tls.ClientHelloInfo) (*tls.Certificate, error)

// GetCertificatesFromSettings returns a function that returns a certificate for the given settings.
func GetCertificatesFromSettings(settings *config.LocalCertConfig) (GetCertificateFunc, error) {
	cert, err := tls.LoadX509KeyPair(settings.CertFile, settings.KeyFile)
	if err != nil {
		return nil, err
	}

	return func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
		return &cert, nil
	}, nil
}
