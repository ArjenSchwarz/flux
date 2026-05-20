package api

import (
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
)

// bearerTokenMiddleware short-circuits any request that does not carry a
// matching `Authorization: Bearer <token>` header. Auth runs before routing
// so unknown paths still surface 401 (matching the legacy handler's
// behaviour). The token comparison is constant-time.
func bearerTokenMiddleware(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// r.Header.Get canonicalises the key, so it handles both
		// "Authorization" and "authorization" forms from Lambda events.
		auth := r.Header.Get("Authorization")
		got, ok := strings.CutPrefix(auth, "Bearer ")
		if !ok || got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			writeJSONError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// writeJSONError writes a {"error": message} body with the given status.
// Mirrors errorResponse() but for the http.ResponseWriter path. The body is
// produced via json.Marshal so messages containing quotes or backslashes do
// not break the response.
func writeJSONError(w http.ResponseWriter, status int, message string) {
	body, err := json.Marshal(struct {
		Error string `json:"error"`
	}{Error: message})
	if err != nil {
		// json.Marshal of a plain string can't fail in practice; log and
		// fall back to a known-safe literal rather than emitting nothing.
		slog.Error("marshal error body", "error", err)
		body = []byte(`{"error":"internal error"}`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
