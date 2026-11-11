package client

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigRejectsUnsafeInput(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		url, token, tokenFile string
	}{
		"missing URL":        {token: "fixture-token"},
		"insecure URL":       {url: "http://example.invalid", token: "fixture-token"},
		"URL credentials":    {url: "https://user:do-not-print@example.invalid", token: "fixture-token"},
		"URL query":          {url: "https://example.invalid?token=do-not-print", token: "fixture-token"},
		"URL fragment":       {url: "https://example.invalid#do-not-print", token: "fixture-token"},
		"missing token":      {url: "https://example.invalid"},
		"blank token":        {url: "https://example.invalid", token: " \t "},
		"multiline token":    {url: "https://example.invalid", token: "do-not-print\nsecond-line"},
		"missing token file": {url: "https://example.invalid", tokenFile: "/nonexistent/do-not-print"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := LoadConfig(tc.url, tc.token, tc.tokenFile)
			if err == nil || strings.Contains(err.Error(), "do-not-print") {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestLoadConfigTokenFileOverridesEnvironment(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("  fixture-file-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig("https://example.invalid", "fixture-environment-token", path)
	if err != nil || cfg.token != "fixture-file-token" {
		t.Fatalf("token=%q err=%v", cfg.token, err)
	}
}

func TestGetJoinsPathAndPreservesBody(t *testing.T) {
	t.Parallel()
	body := ` {"live":null,"unknown":9007199254740993} `
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.RequestURI() != "/api/history?days=7" {
			t.Errorf("request = %s %s", r.Method, r.URL.RequestURI())
		}
		if r.Header.Get("Authorization") != "Bearer fixture-token" || r.Header.Get("Accept") != "application/json" {
			t.Error("missing authentication or JSON accept header")
		}
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	cfg, err := LoadConfig(server.URL+"/api/", "fixture-token", "")
	if err != nil {
		t.Fatal(err)
	}
	data, err := New(cfg, server.Client()).Get(t.Context(), "history", url.Values{"days": {"7"}})
	if err != nil || string(data) != body {
		t.Fatalf("data=%q err=%v", data, err)
	}
}

func TestGetFailuresAreSanitized(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		status int
		body   string
	}{
		"unauthorized": {401, `{"error":"do-not-print"}`},
		"server error": {500, "do-not-print"},
		"redirect":     {302, ""},
		"invalid JSON": {200, "do-not-print"},
		"too large":    {200, `"` + strings.Repeat("x", MaxResponseBytes) + `"`},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tc.status == 302 {
					w.Header().Set("Location", "/unexpected")
				}
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			t.Cleanup(server.Close)
			cfg, err := LoadConfig(server.URL, "fixture-token", "")
			if err != nil {
				t.Fatal(err)
			}
			_, err = New(cfg, server.Client()).Get(t.Context(), "status", nil)
			if err == nil || strings.Contains(err.Error(), "do-not-print") {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestGetTransportErrorsAreSanitized(t *testing.T) {
	t.Parallel()
	cfg, err := LoadConfig("https://example.invalid", "fixture-token", "")
	if err != nil {
		t.Fatal(err)
	}
	for name, cause := range map[string]error{
		"canceled": context.Canceled,
		"timeout":  context.DeadlineExceeded,
		"network":  errors.New("do-not-print"),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, cause })}
			_, err := New(cfg, httpClient).Get(t.Context(), "status", nil)
			if err == nil || strings.Contains(err.Error(), "do-not-print") {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestNewDoesNotMutateCallerClient(t *testing.T) {
	t.Parallel()
	httpClient := &http.Client{}
	New(Config{}, httpClient)
	if httpClient.CheckRedirect != nil {
		t.Fatal("New modified the caller's http.Client")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
