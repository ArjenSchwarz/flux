package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBearerTokenMiddleware_ValidPasses(t *testing.T) {
	called := false
	mw := bearerTokenMiddleware("secret", http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		called = true
	}))
	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)
	assert.True(t, called)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestBearerTokenMiddleware_MissingHeaderReturns401(t *testing.T) {
	mw := bearerTokenMiddleware("secret", http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("handler should not be invoked on missing token")
	}))
	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	body, _ := io.ReadAll(rec.Body)
	assert.Contains(t, string(body), "unauthorized")
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
}

func TestBearerTokenMiddleware_WrongTokenReturns401(t *testing.T) {
	mw := bearerTokenMiddleware("secret", http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("handler should not be invoked on wrong token")
	}))
	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	req.Header.Set("Authorization", "Bearer not-the-token")
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestBearerTokenMiddleware_CanonicalLookupHandlesAnyCase(t *testing.T) {
	called := false
	mw := bearerTokenMiddleware("secret", http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		called = true
	}))
	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	// Header.Set canonicalises to "Authorization"; the adapter
	// (lambdaToHTTPRequest) does the same. The middleware uses Get which
	// canonicalises on read, so any case the adapter produces will land.
	req.Header.Set("authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)
	require.True(t, called, "middleware must accept canonical Authorization regardless of original case")
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestBearerTokenMiddleware_AuthBeforeRouting(t *testing.T) {
	// Apply middleware to a mux that does NOT register /missing; expect 401,
	// not 404, when the token is invalid. Matches AC: auth must short-circuit
	// before routing decides.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("/status must not be reached on invalid auth")
	})
	mw := bearerTokenMiddleware("secret", mux)

	req := httptest.NewRequest(http.MethodGet, "/missing", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}
