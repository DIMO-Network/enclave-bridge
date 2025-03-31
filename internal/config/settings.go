package config

// Settings contains the application config
type Settings struct {
	Environment string `yaml:"ENVIRONMENT"`
	LogLevel    string `yaml:"LOG_LEVEL"`
	Port        int    `yaml:"PORT"`
	MonPort     int    `yaml:"MON_PORT"`

	EnclaveCID  uint32 `yaml:"ENCLAVE_CID"`
	EnclavePort uint32 `yaml:"ENCLAVE_PORT"`
}
