package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadCommandsPreserveJSON(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		args []string
		path string
	}{
		"status":          {[]string{"status"}, "/api/status"},
		"history default": {[]string{"history"}, "/api/history?days=7"},
		"history days":    {[]string{"history", "--days", "31"}, "/api/history?days=31"},
		"history range":   {[]string{"history", "--from", "2026-10-01", "--to", "2026-10-31"}, "/api/history?end=2026-10-31&start=2026-10-01"},
		"day":             {[]string{"day", "2024-02-29"}, "/api/day?date=2024-02-29"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Preserve nulls, unknown fields, units, timestamp spelling and large
			// integers; decoding through generic floats would lose precision.
			body := ` {"live":null,"unknown":9007199254740993,"pbat":-125.50,"timestamp":"2026-10-03T14:00:00Z"} `
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.RequestURI() != tc.path {
					t.Errorf("request = %s %s, want GET %s", r.Method, r.URL.RequestURI(), tc.path)
				}
				if r.Header.Get("Authorization") != "Bearer fixture-token" || r.Header.Get("Accept") != "application/json" {
					t.Error("missing authentication or JSON accept header")
				}
				_, _ = io.WriteString(w, body)
			}))
			t.Cleanup(server.Close)
			var out, stderr bytes.Buffer
			code := run(t.Context(), tc.args, env(server.URL+"/api/", "fixture-token"), server.Client(), &out, &stderr)
			if code != 0 || out.String() != body+"\n" || stderr.Len() != 0 {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), stderr.String())
			}
		})
	}
}

func TestExplicitEndpointAndTokenFile(t *testing.T) {
	t.Parallel()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-file-token" {
			t.Error("token file did not override environment token")
		}
		_, _ = io.WriteString(w, "{\"live\":null}\n")
	}))
	t.Cleanup(server.Close)
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("fixture-file-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	code := run(t.Context(), []string{"status", "--url", server.URL, "--token-file", path},
		env("https://unused.invalid", "fixture-environment-token"), server.Client(), &out, &stderr)
	if code != 0 || out.String() != "{\"live\":null}\n" || stderr.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), stderr.String())
	}
}

func TestUsageAndConfiguration(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		args  []string
		url   string
		token string
	}{
		"unknown command":       {args: []string{"delete"}},
		"unexpected positional": {args: []string{"status", "extra"}},
		"unknown option":        {args: []string{"status", "--token", "do-not-print"}},
		"missing date":          {args: []string{"day"}},
		"invalid date":          {args: []string{"day", "2026-02-29"}},
		"noncanonical date":     {args: []string{"day", "2026-1-01"}},
		"short history":         {args: []string{"history", "--days", "0"}},
		"long history":          {args: []string{"history", "--days", "32"}},
		"invalid day count":     {args: []string{"history", "--days", "do-not-print"}},
		"partial range":         {args: []string{"history", "--from", "2026-01-01"}},
		"mixed range":           {args: []string{"history", "--days", "7", "--from", "2026-01-01", "--to", "2026-01-02"}},
		"reversed range":        {args: []string{"history", "--from", "2026-01-02", "--to", "2026-01-01"}},
		"oversized range":       {args: []string{"history", "--from", "2026-01-01", "--to", "2026-02-01"}},
		"missing URL":           {args: []string{"status"}, token: "fixture-token"},
		"insecure URL":          {args: []string{"status"}, url: "http://example.invalid", token: "fixture-token"},
		"URL credentials":       {args: []string{"status"}, url: "https://user:do-not-print@example.invalid", token: "fixture-token"},
		"URL query":             {args: []string{"status"}, url: "https://example.invalid?token=do-not-print", token: "fixture-token"},
		"missing token":         {args: []string{"status"}, url: "https://example.invalid"},
		"multiline token":       {args: []string{"status"}, url: "https://example.invalid", token: "do-not-print\nsecond-line"},
		"missing token file":    {args: []string{"status", "--token-file", "/nonexistent/do-not-print"}, url: "https://example.invalid"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				t.Error("invalid invocation made a request")
				return nil, errors.New("unexpected request")
			})}
			var out, stderr bytes.Buffer
			code := run(t.Context(), tc.args, env(tc.url, tc.token), client, &out, &stderr)
			if code != 2 || out.Len() != 0 || stderr.Len() == 0 || strings.Contains(stderr.String(), "do-not-print") {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), stderr.String())
			}
		})
	}
}

func TestHelpNeedsNoCredentials(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{nil, {"--help"}, {"status", "--help"}, {"history", "--help"}, {"day", "--help"}} {
		var out, stderr bytes.Buffer
		code := run(t.Context(), args, env("", ""), nil, &out, &stderr)
		if code != 0 || !strings.Contains(out.String(), "Usage:") || stderr.Len() != 0 {
			t.Fatalf("args=%v code=%d stderr=%s", args, code, stderr.String())
		}
	}
}

func TestResponseFailuresDoNotPrintBodies(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		status int
		body   string
	}{
		"unauthorized": {401, `{"error":"do-not-print"}`},
		"bad range":    {400, `{"error":"do-not-print"}`},
		"server error": {500, "do-not-print"},
		"invalid JSON": {200, "do-not-print"},
		"empty body":   {204, ""},
		"too large":    {200, `"` + strings.Repeat("x", maxResponseBytes) + `"`},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			t.Cleanup(server.Close)
			var out, stderr bytes.Buffer
			code := run(t.Context(), []string{"status"}, env(server.URL, "fixture-token"), server.Client(), &out, &stderr)
			if code != 1 || out.Len() != 0 || stderr.Len() == 0 || strings.Contains(stderr.String(), "do-not-print") {
				t.Fatalf("code=%d stdout length=%d stderr=%q", code, out.Len(), stderr.String())
			}
		})
	}
}

func TestRedirectIsNotFollowed(t *testing.T) {
	t.Parallel()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/status" {
			t.Error("followed redirect with bearer token")
		}
		http.Redirect(w, r, "/unexpected", http.StatusFound)
	}))
	t.Cleanup(server.Close)
	var out, stderr bytes.Buffer
	code := run(t.Context(), []string{"status"}, env(server.URL, "fixture-token"), server.Client(), &out, &stderr)
	if code != 1 || out.Len() != 0 || !strings.Contains(stderr.String(), "HTTP 302") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), stderr.String())
	}
}

func TestTransportErrorsAreSanitized(t *testing.T) {
	t.Parallel()
	for name, cause := range map[string]error{
		"canceled": context.Canceled,
		"timeout":  context.DeadlineExceeded,
		"network":  errors.New("do-not-print"),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, cause })}
			var out, stderr bytes.Buffer
			code := run(t.Context(), []string{"status"}, env("https://example.invalid", "fixture-token"), client, &out, &stderr)
			if code != 1 || out.Len() != 0 || stderr.Len() == 0 || strings.Contains(stderr.String(), "do-not-print") {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), stderr.String())
			}
		})
	}
}

func env(endpoint, token string) func(string) string {
	return func(key string) string {
		switch key {
		case "FLUX_API_URL":
			return endpoint
		case "FLUX_API_TOKEN":
			return token
		default:
			return ""
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
