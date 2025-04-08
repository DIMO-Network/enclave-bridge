package enclave

import (
	"encoding/json"
	"fmt"

	"github.com/DIMO-Network/sample-enclave-api/pkg/config"
	"github.com/mdlayher/vsock"
)

const InitPort = 5000

// SendConfig sends the config to the enclave.
func SendConfig(config *config.BridgeSettings) error {
	conn, err := vsock.Dial(DefaultHostCID, InitPort, nil)
	if err != nil {
		return fmt.Errorf("failed to dial vsock: %w", err)
	}
	defer conn.Close()
	marshaledSettings, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	_, err = conn.Write(marshaledSettings)
	if err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}
