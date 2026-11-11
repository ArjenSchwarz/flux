// Package client is a minimal read-only HTTPS client for the Flux Lambda API.
//
// It holds the endpoint and credential handling shared by every local Flux
// tool (the flux CLI today): HTTPS-only base URLs without embedded
// credentials, a single-line bearer token, no redirect following, a bounded
// JSON response body, and error messages that never include tokens, response
// bodies or raw transport errors.
package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// Environment variables read by Flux tools for the API endpoint and token.
const (
	EnvURL   = "FLUX_API_URL"
	EnvToken = "FLUX_API_TOKEN"
)

// MaxResponseBytes caps how much of a response body is read.
const MaxResponseBytes = 8 << 20

// Config is a validated API endpoint and bearer token.
type Config struct {
	baseURL *url.URL
	token   string
}

// LoadConfig validates the API base URL and resolves the bearer token.
// Callers apply their own flag/environment precedence for baseURL. A non-empty
// tokenFile overrides envToken; surrounding whitespace is trimmed from either.
func LoadConfig(baseURL, envToken, tokenFile string) (Config, error) {
	endpoint, err := url.Parse(baseURL)
	if err != nil || endpoint.Scheme != "https" || endpoint.Hostname() == "" ||
		endpoint.User != nil || endpoint.RawQuery != "" || endpoint.ForceQuery || endpoint.Fragment != "" {
		return Config{}, errors.New("set --url or FLUX_API_URL to an HTTPS base URL without credentials, query or fragment")
	}
	token := envToken
	if tokenFile != "" {
		data, err := os.ReadFile(tokenFile)
		if err != nil {
			return Config{}, errors.New("could not read token file")
		}
		token = string(data)
	}
	token = strings.TrimSpace(token)
	if token == "" || strings.ContainsAny(token, "\r\n") {
		return Config{}, errors.New("supply a nonempty single-line token via --token-file or FLUX_API_TOKEN")
	}
	return Config{baseURL: endpoint, token: token}, nil
}

// Client performs authenticated GET requests against the Flux API.
type Client struct {
	cfg  Config
	http http.Client
}

// New returns a Client using a copy of httpClient (nil means
// http.DefaultClient) that never follows redirects.
func New(cfg Config, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	c := &Client{cfg: cfg, http: *httpClient}
	// Do not follow redirects, even to related hosts: the token belongs only
	// to the explicitly configured API endpoint.
	c.http.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return c
}

// Get requests path (relative to the base URL) with query and returns the raw
// JSON body unchanged. It fails on non-2xx statuses, oversized bodies and
// invalid JSON.
func (c *Client) Get(ctx context.Context, path string, query url.Values) ([]byte, error) {
	endpoint := c.cfg.baseURL.JoinPath(path)
	endpoint.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, errors.New("could not create request")
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.token)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, errors.New("request canceled")
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, errors.New("request timed out")
		}
		// Transport errors can include the URL; never print raw errors or headers.
		return nil, errors.New("request failed (network or TLS error)")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("API returned HTTP %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes+1))
	if err != nil {
		return nil, errors.New("could not read API response")
	}
	if len(data) > MaxResponseBytes {
		return nil, errors.New("API response exceeds 8 MiB limit")
	}
	if !json.Valid(data) {
		return nil, errors.New("API returned invalid JSON")
	}
	return data, nil
}
