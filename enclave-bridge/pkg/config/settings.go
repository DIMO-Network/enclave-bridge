package config

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

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

// SerializeEnvironment creates a key value JSON representation of environment variables
// if excludePattern is provided, it will exclude any environment variables that match the pattern.
func SerializeEnvironment(excludePattern string) ([]byte, error) {
	envMap := make(map[string]string)

	// Compile exclude regex if provided
	var excludeRegex *regexp.Regexp
	var err error
	if excludePattern != "" {
		excludeRegex, err = regexp.Compile(excludePattern)
		if err != nil {
			return nil, fmt.Errorf("invalid exclude pattern: %w", err)
		}
	}

	// Get all environment variables
	for _, envEntry := range os.Environ() {
		parts := strings.SplitN(envEntry, "=", 2)
		if len(parts) != 2 {
			continue // Skip invalid entries
		}

		key, value := parts[0], parts[1]

		// Apply exclude pattern if configured
		if excludeRegex != nil && excludeRegex.MatchString(key) {
			continue // Skip this variable
		}

		envMap[key] = value
	}
	mapBytes, err := json.Marshal(envMap)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal environment map: %w", err)
	}
	return mapBytes, nil
}
