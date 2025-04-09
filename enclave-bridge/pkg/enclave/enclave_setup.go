package enclave

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/DIMO-Network/sample-enclave-api/enclave-bridge/pkg/config"
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
	_, err = conn.Write(append(marshaledSettings, '\n'))
	if err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}
	// wait for other side to close the connection
	_, err = conn.Read(make([]byte, 1))
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return fmt.Errorf("failed to wait for other side to close connection: %w", err)
	}

	return nil
}
