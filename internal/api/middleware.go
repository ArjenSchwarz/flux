package api

import (
	"crypto/subtle"
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
// Mirrors errorResponse() but for the http.ResponseWriter path.
func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"error":"` + message + `"}`))
}
