package config

// Settings contains the application config.
type Settings struct {
	Environment string `env:"ENVIRONMENT" yaml:"environment"`
	LogLevel    string `env:"LOG_LEVEL"   yaml:"logLevel"`
	Port        int    `env:"PORT"        yaml:"port"`
	MonPort     int    `env:"MON_PORT"    yaml:"monPort"`
	EnclaveCID  uint32 `env:"ENCLAVE_CID" yaml:"enclaveCid"`
}
