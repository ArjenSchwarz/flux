package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/aws/aws-lambda-go/events"
)

// Handler processes Lambda Function URL requests with auth and routing.
type Handler struct {
	reader       dynamo.Reader
	serial       string
	apiToken     string
	offpeakStart string
	offpeakEnd   string
	// nowFunc returns the current time. Defaults to time.Now.
	// Exposed for testing to ensure consistent time capture per request.
	nowFunc func() time.Time
}

// NewHandler creates a Handler with all dependencies injected.
func NewHandler(reader dynamo.Reader, serial, apiToken, offpeakStart, offpeakEnd string) *Handler {
	return &Handler{
		reader:       reader,
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
	if req.RequestContext.HTTP.Method != "GET" {
		resp := errorResponse(405, "method not allowed")
		resp.Headers["Allow"] = "GET"
		return resp
	}

	if !h.validToken(req.Headers["authorization"]) {
		return errorResponse(401, "unauthorized")
	}

	switch req.RawPath {
	case "/status":
		return h.handleStatus(ctx, req)
	case "/history":
		return h.handleHistory(ctx, req)
	case "/day":
		return h.handleDay(ctx, req)
	default:
		return errorResponse(404, "not found")
	}
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
