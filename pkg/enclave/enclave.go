// Package enclave provides helpful functions when communicating in and out of and enclave over a vsock connection.
package enclave

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"fmt"
	"io"

	"github.com/ethereum/go-ethereum/crypto"
)

const (
	// DefaultHostCID is the default host CID for the enclave.
	DefaultHostCID = 3
	// InitPort is the port used to initialize the enclave-bridge.
	InitPort = uint32(5000)
	// StdoutPort is the port used to send stdout to the enclave-bridge.
	StdoutPort = uint32(4999)
)

// ACK returns the ACK message used for communication between the enclave and the enclave-bridge.
var ACK = []byte{0x06, '\n'}

// WriteWithContext is a context aware wrapper around io.Writer.Write.
// The function will return after the write has completed or the context is canceled.
func WriteWithContext(ctx context.Context, writer io.Writer, data []byte) error {
	// Using a buffered channel with capacity 1 to prevent goroutine leaks
	// If the context is canceled after the goroutine completes but before we read from the
	// channel, the goroutine can still send its result without blocking
	writeChan := make(chan error, 1)

	go func() {
		// Perform the potentially blocking I/O operation in a separate goroutine
		_, err := writer.Write(data)

		// Only send on the channel if the context hasn't been canceled
		// This check prevents sending on the channel when no one is listening anymore
		if ctx.Err() == nil {
			writeChan <- err
		}

		// Close the channel from the sender side after sending (or not sending)
		// to signal completion and prevent resource leaks
		close(writeChan)
	}()

	// Wait for either the operation to complete or the context to be canceled
	// This provides cancellation semantics for the blocking operation
	select {
	case <-ctx.Done():
		// When context is canceled, return its error (typically context.Canceled or context.DeadlineExceeded)
		// The goroutine may still be running, but we don't wait for it
		return ctx.Err()
	case err := <-writeChan:
		// Operation completed before context was canceled, return its result
		// If the channel was closed without a send, this will return nil which is appropriate
		return err
	}
}

// ReadByteWithContext is a context aware wrapper around io.Reader.Read.
func ReadByteWithContext(ctx context.Context, reader io.Reader) (byte, error) {
	// Create a buffered reader for efficient byte reading
	bufReader := bufio.NewReader(reader)

	// Buffered channels (capacity 1) for communicating results back from the goroutine
	// The buffer prevents goroutine leaks if context is canceled right after goroutine finishes
	byteChan := make(chan byte, 1)
	errChan := make(chan error, 1)

	go func() {
		// Perform the potentially blocking read operation in a separate goroutine
		data, err := bufReader.ReadByte()

		// Only send results if context hasn't been canceled
		// This prevents sending when the parent function has already returned
		if ctx.Err() == nil {
			byteChan <- data
			errChan <- err
		}

		// Close channels after sending to prevent resource leaks
		// Closing happens in the goroutine to avoid closing before sending
		close(byteChan)
		close(errChan)
	}()

	// Wait for either operation completion or context cancellation
	select {
	case <-ctx.Done():
		// Context canceled before read completed, return appropriate error
		// The goroutine may continue to run but its result will be discarded
		return 0, ctx.Err()
	case data := <-byteChan:
		// Read completed successfully, return both the data and any error
		// We're guaranteed that errChan has a value since both sends happen together
		return data, <-errChan
	}
}

// ReadBytesWithContext is a context aware wrapper around bufio.Reader.ReadBytes.
// The function will return after the read has completed or the context is canceled.
func ReadBytesWithContext(ctx context.Context, reader io.Reader, delim byte) ([]byte, error) {
	// Create a buffered reader for efficient reading until delimiter
	bufReader := bufio.NewReader(reader)

	// Buffered channels with capacity 1 to safely communicate results
	// This buffering is critical to avoid goroutine leaks in case of context cancellation
	byteChan := make(chan []byte, 1)
	errChan := make(chan error, 1)

	go func() {
		// Perform potentially long-running read operation in a goroutine
		// This read will block until delimiter is found or EOF occurs
		data, err := bufReader.ReadBytes(delim)

		// Only send results back if the parent function is still waiting
		// This check prevents sends that would never be received
		if ctx.Err() == nil {
			byteChan <- data
			errChan <- err
		}

		// Close channels from the sender side after all sends are complete
		// This ensures proper cleanup regardless of how the function returns
		close(byteChan)
		close(errChan)
	}()

	// Wait for either completion or cancellation
	select {
	case <-ctx.Done():
		// Context was canceled, return its error (typically context.Canceled or context.DeadlineExceeded)
		// The goroutine will continue but its result will be discarded due to ctx.Err() check
		return nil, ctx.Err()
	case data := <-byteChan:
		// Read completed before context was canceled, return data and any error
		// We read from errChan after reading from byteChan, which is safe because they're sent together
		return data, <-errChan
	}
}

// CreateKeys creates a new wallet and cert private key.
func CreateKeys() (walletPrivateKey *ecdsa.PrivateKey, certPrivateKey *ecdsa.PrivateKey, err error) {
	certPrivateKey, err = ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate cert private key: %w", err)
	}
	walletPrivateKey, err = crypto.GenerateKey()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate wallet private key: %w", err)
	}
	return walletPrivateKey, certPrivateKey, nil
}
