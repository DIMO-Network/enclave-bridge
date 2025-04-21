package config

// TLSConfig contains the settings for the TLS configuration.
type TLSConfig struct {
	// Enabled is whether TLS is enabled.
	Enabled bool `env:"ENABLED" yaml:"enabled"`
	// LocalCerts is the configuration for the local certificates.
	LocalCerts LocalCertConfig `envPrefix:"LOCAL_" yaml:"localCerts"`
	// ACMEConfig is the configuration for the ACME certificates.
	ACMEConfig ACMEConfig `envPrefix:"ACME_" yaml:"acmeConfig"`
}

// LocalCertConfig contains the settings for the local certificates.
type LocalCertConfig struct {
	// CertFile is the path to the certificate file.
	CertFile string `env:"CERT_FILE" yaml:"certFile"`
	// KeyFile is the path to the key file for the certificate.
	KeyFile string `env:"KEY_FILE"  yaml:"keyFile"`
}

// ACMEConfig contains the settings for the ACME certificates.
type ACMEConfig struct {
	// CA directory URL
	CADirURL string `env:"CA_DIR_URL" yaml:"caDirUrl"`
	// Email for the ACME account
	Email string `env:"EMAIL" yaml:"email"`
	// Domains for the ACME account
	Domains []string `env:"DOMAINS" yaml:"domains"`
}
