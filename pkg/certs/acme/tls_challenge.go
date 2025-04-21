package acme

import (
	"crypto/tls"
	"sync"

	"github.com/go-acme/lego/challenge/tlsalpn01"
	"github.com/rs/zerolog"
)

// TLSALPN01Provider is a simple store for TLS-ALPN-01 challenges.
type TLSALPN01Provider struct {
	challenge *tls.Certificate
	mu        sync.RWMutex
	logger    *zerolog.Logger
}

// NewTLSALPN01Provider creates a new TLSALPN01 provider.
func NewTLSALPN01Provider(logger *zerolog.Logger) *TLSALPN01Provider {
	if logger == nil {
		l := zerolog.Nop()
		logger = &l
	}
	return &TLSALPN01Provider{
		logger: logger,
	}
}

// Present implements challenge.Provider interface.
// Adds challenge data to be validated by the CA.
func (p *TLSALPN01Provider) Present(domain, token, keyAuth string) error {
	// Generate the challenge certificate using the provided keyAuth and domain.
	cert, err := tlsalpn01.ChallengeCert(domain, keyAuth)
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.challenge = cert
	p.mu.Unlock()

	p.logger.Debug().
		Str("domain", domain).
		Str("token", token).
		Msg("Stored new TLS-ALPN-01 challenge")

	return nil
}

// CleanUp implements challenge.Provider interface
// Removes the challenge once it's no longer needed.
func (p *TLSALPN01Provider) CleanUp(domain, token, keyAuth string) error {
	p.mu.Lock()
	p.challenge = nil
	p.mu.Unlock()
	p.logger.Debug().
		Str("domain", domain).
		Str("token", token).
		Msg("Cleaned up TLS-ALPN-01 challenge")

	return nil
}

// GetChallenge retrieves the current challenge.
func (p *TLSALPN01Provider) GetChallenge() (*tls.Certificate, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.challenge == nil {
		p.logger.Debug().
			Msg("TLS-ALPN-01 challenge not found")
		return nil, false
	}

	p.logger.Debug().
		Msg("Retrieved TLS-ALPN-01 challenge")
	return p.challenge, true
}
