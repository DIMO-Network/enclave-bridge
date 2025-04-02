package client

import (
	"context"
	"fmt"
	"net"
	"net/http"

	"github.com/mdlayher/vsock"
)

func NewHTTPClient(port uint32) *http.Client {
	httpClient := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				vsockConn, err := vsock.Dial(16, port, nil)
				if err != nil {
					return nil, fmt.Errorf("failed to dial vsock: %w", err)
				}
				vsockConn.Write([]byte(addr + "\n"))
				return vsockConn, nil
			},
		},
	}
	return httpClient
}
