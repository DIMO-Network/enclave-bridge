package config

import "time"

// BridgeSettings is the configuration for setting up the bridge.
type BridgeSettings struct {
	AppName string           `json:"appName"`
	Logger  LoggerSettings   `json:"logger"`
	Servers []ServerSettings `json:"servers"`
	Clients []ClientSettings `json:"clients"`
}

// ServerSettings is the configuration for setting up the server.
type ServerSettings struct {
	EnclaveCID        uint32 `json:"enclaveCid"`
	EnclaveListenPort uint32 `json:"enclaveListenPort"`
	BridgeTCPPort     uint32 `json:"bridgeTcpPort"`
}

// ClientSettings is the configuration for setting up the client.
type ClientSettings struct {
	EnclaveDialPort uint32        `json:"enclaveDialPort"`
	RequestTimeout  time.Duration `json:"requestTimeout"`
}

// LoggerSettings is the configuration for setting up the logger.
type LoggerSettings struct {
	Level           string `json:"level"`
	EnclaveDialPort uint32 `json:"enclaveDialPort"`
}
