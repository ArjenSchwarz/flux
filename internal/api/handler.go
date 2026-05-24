package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
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
	devices      DeviceStore
	rules        SocRuleStore
	fireState    FireStateCleaner
	pricing      PricingStore
	serial       string
	apiToken     string
	offpeakStart string
	offpeakEnd   string
	// nowFunc returns the current time. Defaults to time.Now.
	// Exposed for testing to ensure consistent time capture per request.
	nowFunc func() time.Time
	// idFunc generates a UUID for newly-created rules. Defaults to
	// crypto-quality UUID; tests inject a deterministic stub.
	idFunc func() string
	// mux is the routed http.Handler with bearer-auth middleware applied.
	// Built once in NewHandler so per-request work is just translation.
	mux http.Handler
}

// NewHandler creates a Handler with all dependencies injected. Pass a nil
// notes writer in tests that don't exercise the write endpoint.
func NewHandler(reader dynamo.Reader, notes NoteWriter, serial, apiToken, offpeakStart, offpeakEnd string) *Handler {
	h := &Handler{
		reader:       reader,
		notes:        notes,
		serial:       serial,
		apiToken:     apiToken,
		offpeakStart: offpeakStart,
		offpeakEnd:   offpeakEnd,
		nowFunc:      time.Now,
		idFunc:       defaultIDFunc,
	}
	h.mux = h.buildMux()
	return h
}

// SetNow overrides the clock used by request handlers. Intended for the
// integration test, which lives in another package and cannot reach the
// unexported nowFunc field directly. Safe to call before Handle.
func (h *Handler) SetNow(now func() time.Time) {
	h.nowFunc = now
}

// buildMux wires the existing event-returning handlers into a ServeMux and
// wraps the result with the bearer-token middleware. Auth runs before
// routing — an invalid token on an unknown path still surfaces 401, not 404.
func (h *Handler) buildMux() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", adaptEventHandler(h.handleStatus))
	mux.HandleFunc("GET /history", adaptEventHandler(h.handleHistory))
	mux.HandleFunc("GET /day", adaptEventHandler(h.handleDay))
	mux.HandleFunc("PUT /note", adaptEventHandler(h.handleNote))
	mux.HandleFunc("POST /devices", adaptEventHandler(h.handleRegisterDevice))
	mux.HandleFunc("GET /devices/{deviceId}/rules", h.handleListRules)
	mux.HandleFunc("POST /devices/{deviceId}/rules", h.handleCreateRule)
	mux.HandleFunc("PUT /devices/{deviceId}/rules/{ruleId}", h.handleUpdateRule)
	mux.HandleFunc("DELETE /devices/{deviceId}/rules/{ruleId}", h.handleDeleteRule)
	mux.HandleFunc("GET /pricing", h.handleListPricing)
	mux.HandleFunc("POST /pricing", h.handleCreatePricing)
	mux.HandleFunc("PUT /pricing/{id}", h.handleUpdatePricing)
	mux.HandleFunc("DELETE /pricing/{id}", h.handleDeletePricing)
	mux.HandleFunc("POST /pricing/replace-open-ended", h.handleReplaceOpenEnded)
	return bearerTokenMiddleware(h.apiToken, jsonNotFound(jsonMethodNotAllowed(mux)))
}

// SetDeviceStore wires the device upsert dependency. Called by cmd/api/main.go.
func (h *Handler) SetDeviceStore(s DeviceStore) {
	h.devices = s
}

// SetSocRuleStore wires the rule CRUD dependency. Called by cmd/api/main.go.
func (h *Handler) SetSocRuleStore(s SocRuleStore) {
	h.rules = s
}

// SetFireStateCleaner wires the fire-state cleanup dependency. Called by
// cmd/api/main.go.
func (h *Handler) SetFireStateCleaner(c FireStateCleaner) {
	h.fireState = c
}

// Handle is the Lambda entry point. Processing order:
// 1. Translate request to *http.Request.
// 2. ServeHTTP through the auth-wrapped mux.
// 3. Render the captured response back to Lambda shape.
// 4. Log request with method, path, status, duration.
func (h *Handler) Handle(ctx context.Context, req events.LambdaFunctionURLRequest) (events.LambdaFunctionURLResponse, error) {
	start := time.Now()
	method := req.RequestContext.HTTP.Method
	path := req.RawPath

	resp := h.serve(ctx, req)

	slog.Info("request",
		"method", method,
		"path", path,
		"status", resp.StatusCode,
		"duration_ms", time.Since(start).Milliseconds(),
	)

	return resp, nil
}

func (h *Handler) serve(ctx context.Context, req events.LambdaFunctionURLRequest) events.LambdaFunctionURLResponse {
	httpReq, err := lambdaToHTTPRequest(ctx, req)
	if err != nil {
		slog.Error("lambda request translation failed", "error", err)
		return errorResponse(http.StatusBadRequest, "malformed request")
	}
	httpReq = withRequestContext(httpReq, req)

	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, httpReq)
	return httpResponseToLambda(rec)
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
		return errorResponse(http.StatusInternalServerError, "internal error")
	}
	return events.LambdaFunctionURLResponse{
		StatusCode: 200,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       string(data),
	}
}

// jsonNotFound rewrites the default ServeMux 404 ("404 page not found\n",
// text/plain) into the project's JSON error shape, so clients consume a
// single error format across all status codes.
func jsonNotFound(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buffer := httptest.NewRecorder()
		next.ServeHTTP(buffer, r)
		if buffer.Code == http.StatusNotFound && buffer.Header().Get("Content-Type") != "application/json" {
			writeJSONError(w, http.StatusNotFound, "not found")
			return
		}
		copyRecorderToWriter(buffer, w)
	})
}

// jsonMethodNotAllowed does the same rewrite for 405 responses generated by
// ServeMux when a path is matched on the wrong method. The Allow header set
// by the mux is preserved.
func jsonMethodNotAllowed(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buffer := httptest.NewRecorder()
		next.ServeHTTP(buffer, r)
		if buffer.Code == http.StatusMethodNotAllowed && buffer.Header().Get("Content-Type") != "application/json" {
			if allow := buffer.Header().Get("Allow"); allow != "" {
				w.Header().Set("Allow", allow)
			}
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		copyRecorderToWriter(buffer, w)
	})
}

// copyRecorderToWriter writes a captured response through to the real
// http.ResponseWriter. Used by the JSON-error wrappers.
func copyRecorderToWriter(rec *httptest.ResponseRecorder, w http.ResponseWriter) {
	for k, v := range rec.Header() {
		w.Header()[k] = v
	}
	if rec.Code == 0 {
		rec.Code = http.StatusOK
	}
	w.WriteHeader(rec.Code)
	if rec.Body.Len() > 0 {
		_, _ = w.Write(rec.Body.Bytes())
	}
}
