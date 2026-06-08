package cloudflare

import "context"

// Client abstracts Cloudflare API operations.
type Client interface {
	CreateTunnel(ctx context.Context, name string) (*Tunnel, error)
	DeleteTunnel(ctx context.Context, tunnelID string) error
	GetTunnelToken(ctx context.Context, tunnelID string) (string, error)
	CreateDNSRecord(ctx context.Context, zoneID, hostname, tunnelID string) (string, error)
	DeleteDNSRecord(ctx context.Context, zoneID, recordID string) error
	ListDNSRecords(ctx context.Context, zoneID, hostname string) ([]DNSRecord, error)
}

// Tunnel represents a Cloudflare Tunnel.
type Tunnel struct {
	ID   string
	Name string
}

// DNSRecord represents a Cloudflare DNS record.
type DNSRecord struct {
	ID      string
	Type    string
	Name    string
	Content string
}
