package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// RealClient implements Client using Cloudflare's REST API.
type RealClient struct {
	AccountID string
	ZoneID    string
	APIToken  string
	BaseURL   string
}

// NewRealClientFromEnv creates a RealClient reading CF_ACCOUNT_ID, CF_ZONE_ID, CF_API_TOKEN from env.
func NewRealClientFromEnv() *RealClient {
	c := &RealClient{
		AccountID: os.Getenv("CF_ACCOUNT_ID"),
		ZoneID:    os.Getenv("CF_ZONE_ID"),
		APIToken:  os.Getenv("CF_API_TOKEN"),
		BaseURL:   "https://api.cloudflare.com/client/v4",
	}
	// Basic validation so you fail fast with a clear message
	if c.APIToken == "" {
		panic("CF_API_TOKEN is not set")
	}
	if c.AccountID == "" {
		panic("CF_ACCOUNT_ID is not set")
	}
	if c.ZoneID == "" {
		panic("CF_ZONE_ID is not set")
	}
	return c
}

func (c *RealClient) auth(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(c.APIToken))
	req.Header.Set("Content-Type", "application/json")
}

// cfResponse wraps the common Cloudflare API response shape.
type cfResponse struct {
	Success bool `json:"success"`
	Errors  []struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
	Messages []string `json:"messages"`
}

func (r cfResponse) error(status int, body string) error {
	msgs := []string{fmt.Sprintf("HTTP %d", status)}
	for _, e := range r.Errors {
		msgs = append(msgs, fmt.Sprintf("[%d] %s", e.Code, e.Message))
	}
	if len(r.Messages) > 0 {
		msgs = append(msgs, "messages: "+strings.Join(r.Messages, ", "))
	}
	if body != "" && len(msgs) == 1 {
		msgs = append(msgs, "body: "+body)
	}
	return fmt.Errorf("cloudflare API: %s", strings.Join(msgs, "; "))
}

func (c *RealClient) CreateTunnel(ctx context.Context, name string) (*Tunnel, error) {
	body, _ := json.Marshal(map[string]string{"name": name, "config_src": "local"})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/accounts/%s/cfd_tunnel", c.BaseURL, c.AccountID), bytes.NewReader(body))
	c.auth(req)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	var result struct {
		cfResponse
		Result struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w (body: %s)", err, string(raw))
	}
	if !result.Success {
		return nil, result.cfResponse.error(resp.StatusCode, string(raw))
	}
	return &Tunnel{ID: result.Result.ID, Name: result.Result.Name}, nil
}

func (c *RealClient) DeleteTunnel(ctx context.Context, tunnelID string) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodDelete,
		fmt.Sprintf("%s/accounts/%s/cfd_tunnel/%s", c.BaseURL, c.AccountID, tunnelID), nil)
	c.auth(req)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	var result cfResponse
	if err := json.Unmarshal(raw, &result); err != nil {
		return fmt.Errorf("failed to decode response: %w (body: %s)", err, string(raw))
	}
	if !result.Success {
		return result.error(resp.StatusCode, string(raw))
	}
	return nil
}

func (c *RealClient) GetTunnelToken(ctx context.Context, tunnelID string) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/accounts/%s/cfd_tunnel/%s/token", c.BaseURL, c.AccountID, tunnelID), nil)
	c.auth(req)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	var result struct {
		cfResponse
		Result string `json:"result"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("failed to decode response: %w (body: %s)", err, string(raw))
	}
	if !result.Success {
		return "", result.cfResponse.error(resp.StatusCode, string(raw))
	}
	return result.Result, nil
}

func (c *RealClient) CreateDNSRecord(ctx context.Context, zoneID, hostname, tunnelID string) (string, error) {
	payload := map[string]interface{}{
		"type":    "CNAME",
		"name":    hostname,
		"content": fmt.Sprintf("%s.cfargotunnel.com", tunnelID),
		"ttl":     1, // auto
		"proxied": true,
	}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/zones/%s/dns_records", c.BaseURL, zoneID), bytes.NewReader(body))
	c.auth(req)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	var result struct {
		cfResponse
		Result struct {
			ID string `json:"id"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("failed to decode response: %w (body: %s)", err, string(raw))
	}
	if !result.Success {
		return "", result.cfResponse.error(resp.StatusCode, string(raw))
	}
	return result.Result.ID, nil
}

func (c *RealClient) DeleteDNSRecord(ctx context.Context, zoneID, recordID string) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodDelete,
		fmt.Sprintf("%s/zones/%s/dns_records/%s", c.BaseURL, zoneID, recordID), nil)
	c.auth(req)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	var result cfResponse
	if err := json.Unmarshal(raw, &result); err != nil {
		return fmt.Errorf("failed to decode response: %w (body: %s)", err, string(raw))
	}
	if !result.Success {
		return result.error(resp.StatusCode, string(raw))
	}
	return nil
}

func (c *RealClient) ListDNSRecords(ctx context.Context, zoneID, hostname string) ([]DNSRecord, error) {
	u := fmt.Sprintf("%s/zones/%s/dns_records?name=%s", c.BaseURL, zoneID, hostname)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	c.auth(req)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	var result struct {
		cfResponse
		Result []struct {
			ID      string `json:"id"`
			Type    string `json:"type"`
			Name    string `json:"name"`
			Content string `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w (body: %s)", err, string(raw))
	}
	if !result.Success {
		return nil, result.cfResponse.error(resp.StatusCode, string(raw))
	}

	records := make([]DNSRecord, len(result.Result))
	for i, r := range result.Result {
		records[i] = DNSRecord{ID: r.ID, Type: r.Type, Name: r.Name, Content: r.Content}
	}
	return records, nil
}
