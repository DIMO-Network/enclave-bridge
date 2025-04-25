package config

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/gofrs/uuid"
)

// BridgeSettings is the configuration for setting up the bridge.
type BridgeSettings struct {
	// AppName is the name of the application.
	AppName string `json:"appName"`
	// Watchdog is the configuration for the watchdog.
	Watchdog WatchdogSettings `json:"watchdog"`
	// Logger is the configuration for the logger.
	Logger LoggerSettings `json:"logger"`
	// Servers is the configuration for the servers.
	Servers []ServerSettings `json:"servers"`
	// Clients is the configuration for the clients.
	Clients []ClientSettings `json:"clients"`
}

// WatchdogSettings is the configuration for the watchdog which terminates the bridge if the enclave is unresponsive or restarted.
type WatchdogSettings struct {
	// EnclaveID is the ID of the enclave.
	EnclaveID uuid.UUID `json:"enclaveId"`
	// Interval if interval elapses without a heartbeat, the watchdog will terminate the bridge
	Interval time.Duration `json:"interval"`
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
	Level string `json:"level"`
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
