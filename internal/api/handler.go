package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/aws/aws-lambda-go/events"
)

// NoteWriter is the api-package-local view of the dynamo writer the handler
// needs. Declared here so tests mock without importing dynamo internals.
type NoteWriter interface {
	PutNote(ctx context.Context, item dynamo.NoteItem) error
	DeleteNote(ctx context.Context, serial, date string) error
}

// Handler processes Lambda Function URL requests with auth and routing.
type Handler struct {
	reader       dynamo.Reader
	notes        NoteWriter
	serial       string
	apiToken     string
	offpeakStart string
	offpeakEnd   string
	// nowFunc returns the current time. Defaults to time.Now.
	// Exposed for testing to ensure consistent time capture per request.
	nowFunc func() time.Time
}

// NewHandler creates a Handler with all dependencies injected. Pass a nil
// notes writer in tests that don't exercise the write endpoint.
func NewHandler(reader dynamo.Reader, notes NoteWriter, serial, apiToken, offpeakStart, offpeakEnd string) *Handler {
	return &Handler{
		reader:       reader,
		notes:        notes,
		serial:       serial,
		apiToken:     apiToken,
		offpeakStart: offpeakStart,
		offpeakEnd:   offpeakEnd,
		nowFunc:      time.Now,
	}
}

// Handle is the Lambda entry point. Processing order:
// 1. Check HTTP method (GET only)
// 2. Validate bearer token (auth before routing)
// 3. Route to endpoint handler
// 4. Log request with method, path, status, duration
func (h *Handler) Handle(ctx context.Context, req events.LambdaFunctionURLRequest) (events.LambdaFunctionURLResponse, error) {
	start := time.Now()
	method := req.RequestContext.HTTP.Method
	path := req.RawPath

	resp := h.handle(ctx, req)

	slog.Info("request",
		"method", method,
		"path", path,
		"status", resp.StatusCode,
		"duration_ms", time.Since(start).Milliseconds(),
	)

	return resp, nil
}

// handle contains the core logic, separated from Handle to simplify logging.
func (h *Handler) handle(ctx context.Context, req events.LambdaFunctionURLRequest) events.LambdaFunctionURLResponse {
	if !h.validToken(req.Headers["authorization"]) {
		return errorResponse(401, "unauthorized")
	}

	method := req.RequestContext.HTTP.Method
	switch method {
	case http.MethodGet:
		switch req.RawPath {
		case "/status":
			return h.handleStatus(ctx, req)
		case "/history":
			return h.handleHistory(ctx, req)
		case "/day":
			return h.handleDay(ctx, req)
		}
	case http.MethodPut:
		if req.RawPath == "/note" {
			return h.handleNote(ctx, req)
		}
	}

	// Unknown method on a known path → 405 with the path's allowed methods.
	allow := http.MethodGet
	if req.RawPath == "/note" {
		allow = http.MethodPut
	}
	switch req.RawPath {
	case "/status", "/history", "/day", "/note":
		resp := errorResponse(405, "method not allowed")
		resp.Headers["Allow"] = allow
		return resp
	}
	return errorResponse(404, "not found")
}

// validToken checks the Authorization header using constant-time comparison.
// Returns false for missing, malformed, or incorrect tokens.
func (h *Handler) validToken(authHeader string) bool {
	token, ok := strings.CutPrefix(authHeader, "Bearer ")
	if !ok || token == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(h.apiToken)) == 1
}

// errorResponse builds a JSON error response with the given status and message.
func errorResponse(status int, message string) events.LambdaFunctionURLResponse {
	data, _ := json.Marshal(map[string]string{"error": message})
	return events.LambdaFunctionURLResponse{
		StatusCode: status,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       string(data),
	}
}

// jsonResponse builds a 200 JSON response from the given value.
func jsonResponse(v any) events.LambdaFunctionURLResponse {
	data, err := json.Marshal(v)
	if err != nil {
		slog.Error("marshal response", "error", err)
		return errorResponse(500, "internal error")
	}
	return events.LambdaFunctionURLResponse{
		StatusCode: 200,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       string(data),
	}
}
