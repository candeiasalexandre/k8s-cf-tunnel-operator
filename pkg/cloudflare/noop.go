package cloudflare

import "context"

// NoOpClient is a mock implementation that does nothing. Useful for local dev without CF credentials.
type NoOpClient struct{}

func (n *NoOpClient) CreateTunnel(_ context.Context, name string) (*Tunnel, error) {
	return &Tunnel{ID: "mock-" + name, Name: name}, nil
}

func (n *NoOpClient) DeleteTunnel(_ context.Context, _ string) error { return nil }

func (n *NoOpClient) GetTunnelToken(_ context.Context, _ string) (string, error) {
	return "mock-token", nil
}

func (n *NoOpClient) CreateDNSRecord(_ context.Context, _, _, _ string) (string, error) {
	return "mock-dns-id", nil
}

func (n *NoOpClient) DeleteDNSRecord(_ context.Context, _, _ string) error { return nil }

func (n *NoOpClient) ListDNSRecords(_ context.Context, _, _ string) ([]DNSRecord, error) {
	return nil, nil
}
