package acme

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"fmt"
	"math/big"
	"net/http"
	"sync"
	"time"

	"github.com/go-acme/lego/v4/certcrypto"
	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/registration"

	"github.com/rs/zerolog"
)

// tickFrequency how frequently we should check whether our cert needs renewal.
const tickFrequency = 15 * time.Second

// LegoUser implements registration.User, required by lego.
type LegoUser struct {
	key          crypto.PrivateKey
	registration *registration.Resource
	email        string
}

// GetEmail returns the email address of the user.
func (l *LegoUser) GetEmail() string {
	return l.email
}

// GetRegistration returns the registration resource for the user.
func (l *LegoUser) GetRegistration() *registration.Resource {
	return l.registration
}

// GetPrivateKey returns the private key for the user.
func (l *LegoUser) GetPrivateKey() crypto.PrivateKey {
	return l.key
}

// SupportsCurve returns the key type and true if the curve is supported.
func SupportsCurve(curve elliptic.Curve) (certcrypto.KeyType, bool) {
	switch curve {
	case elliptic.P256():
		return certcrypto.EC256, true
	case elliptic.P384():
		return certcrypto.EC384, true
	}
	return "", false
}

// Uses techniques from https://diogomonica.com/2017/01/11/hitless-tls-certificate-rotation-in-go/
// to automatically rotate certificates when they're renewed.

// CertManager manages ACME certificate and renewal.
type CertManager struct {
	acmeClient  *lego.Client
	resource    *certificate.Resource
	certificate *tls.Certificate
	leaf        *x509.Certificate
	domains     []string
	provider    *TLSALPN01Provider
	sync.RWMutex
}

// CertManagerConfig contains configuration options for creating a new ACMECertManager.
type CertManagerConfig struct {
	Key        *ecdsa.PrivateKey
	HTTPClient *http.Client
	Logger     *zerolog.Logger
	Email      string
	CADirURL   string
	Domains    []string
}

// NewCertManager configures an ACME client, creates & registers a new ACME
// user. After creating a client you must call ObtainCertificate and
// RenewCertificate yourself.
func NewCertManager(acmeConfig CertManagerConfig) (*CertManager, error) {
	user := &LegoUser{
		email: acmeConfig.Email,
		key:   acmeConfig.Key,
	}

	keyType, ok := SupportsCurve(acmeConfig.Key.Curve)
	if !ok {
		return nil, fmt.Errorf("unsupported curve: %s", acmeConfig.Key.Curve)
	}

	// Create a configuration using our HTTPS client, ACME server, user details.
	config := &lego.Config{
		CADirURL:   acmeConfig.CADirURL,
		User:       user,
		HTTPClient: acmeConfig.HTTPClient,
		Certificate: lego.CertificateConfig{
			KeyType: keyType,
			Timeout: 30 * time.Second,
		},
	}

	// Create an ACME client and configure use of `tls-alpn-01` challenge
	client, err := lego.NewClient(config)
	if err != nil {
		return nil, err
	}

	provider := NewTLSALPN01Provider(acmeConfig.Logger)
	err = client.Challenge.SetTLSALPN01Provider(provider)
	if err != nil {
		return nil, fmt.Errorf("couldn't set TLS-ALPN-01 provider: %w", err)
	}

	// Register our ACME user
	reg, err := client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
	if err != nil {
		return nil, fmt.Errorf("couldn't register ACME user: %w", err)
	}
	user.registration = reg

	return &CertManager{
		acmeClient: client,
		domains:    acmeConfig.Domains,
		provider:   provider,
	}, nil
}

// Start obtains a certificate and runs a ticker for renewal in a goroutine.
func (c *CertManager) Start(ctx context.Context, logger *zerolog.Logger) error {
	logger.Info().Msg("Obtaining certificate")
	err := c.ObtainCertificate()
	if err != nil {
		return err
	}
	logger.Info().Msg("Certificate obtained")
	go c.runRenewal(ctx, logger)
	return nil
}

// ObtainCertificate gets a new certificate using ACME. Not thread safe.
func (c *CertManager) ObtainCertificate() error {
	request := certificate.ObtainRequest{
		Domains: c.domains,
		Bundle:  true,
	}

	resource, err := c.acmeClient.Certificate.Obtain(request)
	if err != nil {
		return err
	}

	return c.switchCertificate(resource)
}

// RenewCertificate renews an existing certificate using ACME. Not thread safe.
func (c *CertManager) RenewCertificate() error {
	resource, err := c.acmeClient.Certificate.RenewWithOptions(*c.resource, &certificate.RenewOptions{Bundle: true})
	if err != nil {
		return err
	}

	return c.switchCertificate(resource)
}

// GetCertificate locks around returning a tls.Certificate; use as tls.Config.GetCertificate.
func (c *CertManager) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	// Check if this is a TLS-ALPN-01 challenge request
	if hello != nil {
		for _, proto := range hello.SupportedProtos {
			if proto == "acme-tls/1" {
				// This is a TLS-ALPN-01 challenge request
				// Get the challenge from the provider
				cert, ok := c.provider.GetChallenge()
				if !ok {
					return nil, fmt.Errorf("no challenge found for domain: %s", hello.ServerName)
				}

				return cert, nil
			}
		}
	}

	// Normal certificate request
	c.RLock()
	defer c.RUnlock()
	return c.certificate, nil
}

// GetLeaf returns the currently valid leaf x509.Certificate.
func (c *CertManager) GetLeaf() x509.Certificate {
	c.RLock()
	defer c.RUnlock()
	return *c.leaf
}

// NextRenewal returns when the certificate will be 2/3 of the way to expiration.
func (c *CertManager) NextRenewal() time.Time {
	leaf := c.GetLeaf()
	lifetime := leaf.NotAfter.Sub(leaf.NotBefore).Seconds()
	return leaf.NotBefore.Add(time.Duration(lifetime*2/3) * time.Second)
}

// NeedsRenewal returns true if the certificate's age is more than 2/3 it's
// lifetime.
func (c *CertManager) NeedsRenewal() bool {
	return time.Now().After(c.NextRenewal())
}

func (c *CertManager) switchCertificate(newResource *certificate.Resource) error {
	// The certificate.Resource represents our certificate as a PEM-encoded
	// bundle of bytes. Let's process it. First create a tls.Certificate
	// for use with the tls package.
	crt, err := tls.X509KeyPair(newResource.Certificate, newResource.PrivateKey)
	if err != nil {
		return fmt.Errorf("failed to create tls.Certificate: %w", err)
	}

	// Now create a x509.Certificate so we can figure out when the cert
	// expires. Note that the first certificate in the bundle is the leaf.
	// Go ahead and set crt.Leaf as an optimization.
	leaf, err := x509.ParseCertificate(crt.Certificate[0])
	if err != nil {
		return fmt.Errorf("failed to parse certificate: %w", err)
	}
	crt.Leaf = leaf

	c.Lock()
	defer c.Unlock()
	c.resource = newResource
	c.certificate = &crt
	c.leaf = leaf

	return nil
}

// runRenewal schedules periodic certificate renewals
// We tick every timeFrequency but only renew if the certificate
// is approaching expiration. That'll give us some resilience to CA
// downtime.
func (c *CertManager) runRenewal(ctx context.Context, logger *zerolog.Logger) {
	ticker := time.NewTicker(tickFrequency)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if c.NeedsRenewal() {
				logger.Info().Msg("Renewing certificate")
				err := c.RenewCertificate()
				if err != nil {
					logger.Error().Err(err).Msg("Error loading certificate and key")
				} else {
					leaf := c.GetLeaf()
					logger.Info().Msgf("Renewed certificate: %s [%s - %s]", leaf.Subject, leaf.NotBefore, leaf.NotAfter)
					logger.Info().Msgf("Next renewal at %s (%s)", c.NextRenewal(), time.Until(c.NextRenewal()))
				}
			}
		}
	}
}

// ChallengeCert generates a certificate for the TLS-ALPN-01 challenge.
func ChallengeCert(domain, keyAuth string) (*tls.Certificate, error) {
	// Generate a self-signed certificate with the acmeValidation-v1 extension
	// containing the SHA-256 digest of the keyAuth
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate key: %w", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: domain,
		},
		NotBefore: time.Now(),
		NotAfter:  time.Now().Add(24 * time.Hour),
		KeyUsage:  x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
		},
		BasicConstraintsValid: true,
		DNSNames:              []string{domain},
	}

	// Add the acmeValidation-v1 extension
	h := sha256.Sum256([]byte(keyAuth))
	ext := pkix.Extension{
		Id:       asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 1, 31}, // acmeValidation-v1 OID
		Critical: false,
		Value:    h[:],
	}
	template.ExtraExtensions = []pkix.Extension{ext}

	derBytes, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("failed to create certificate: %w", err)
	}

	return &tls.Certificate{
		Certificate: [][]byte{derBytes},
		PrivateKey:  key,
	}, nil
}
