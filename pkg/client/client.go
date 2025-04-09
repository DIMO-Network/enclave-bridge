package client

import (
	"context"
	"fmt"
	"net"
	"net/http"

	"github.com/mdlayher/vsock"
)

const defaultHostCID = 3

func NewHTTPClient(port uint32) *http.Client {
	httpClient := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				vsockConn, err := vsock.Dial(defaultHostCID, port, nil)
				if err != nil {
					return nil, fmt.Errorf("failed to dial vsock: %w", err)
				}
				_, err = vsockConn.Write([]byte(addr + "\n"))
				if err != nil {
					return nil, fmt.Errorf("failed to write to vsock: %w", err)
				}
				return vsockConn, nil
			},
		},
	}
	return httpClient
}
