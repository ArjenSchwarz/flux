package api

import (
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLambdaToHTTPRequest_PathHeadersAndQuery(t *testing.T) {
	req := events.LambdaFunctionURLRequest{
		RawPath: "/devices",
		Headers: map[string]string{
			"authorization": "Bearer abc",
			"content-type":  "application/json",
		},
		QueryStringParameters: map[string]string{"days": "7"},
		RequestContext: events.LambdaFunctionURLRequestContext{
			HTTP: events.LambdaFunctionURLRequestContextHTTPDescription{
				Method: http.MethodPost,
			},
		},
		Body: `{"deviceId":"abc"}`,
	}

	httpReq, err := lambdaToHTTPRequest(req)
	require.NoError(t, err)
	assert.Equal(t, http.MethodPost, httpReq.Method)
	assert.Equal(t, "/devices", httpReq.URL.Path)
	assert.Equal(t, "7", httpReq.URL.Query().Get("days"))
	assert.Equal(t, "Bearer abc", httpReq.Header.Get("Authorization"))
	assert.Equal(t, "application/json", httpReq.Header.Get("Content-Type"))

	body, err := io.ReadAll(httpReq.Body)
	require.NoError(t, err)
	assert.JSONEq(t, `{"deviceId":"abc"}`, string(body))
}

func TestLambdaToHTTPRequest_Base64Body(t *testing.T) {
	raw := `{"hello":"world"}`
	req := events.LambdaFunctionURLRequest{
		RawPath:         "/devices",
		IsBase64Encoded: true,
		Body:            base64.StdEncoding.EncodeToString([]byte(raw)),
		RequestContext: events.LambdaFunctionURLRequestContext{
			HTTP: events.LambdaFunctionURLRequestContextHTTPDescription{Method: http.MethodPost},
		},
	}
	httpReq, err := lambdaToHTTPRequest(req)
	require.NoError(t, err)
	body, err := io.ReadAll(httpReq.Body)
	require.NoError(t, err)
	assert.Equal(t, raw, string(body))
}

func TestHTTPResponseToLambda_CapturesStatusHeadersBody(t *testing.T) {
	rec := httptest.NewRecorder()
	rec.Header().Set("Content-Type", "application/json")
	rec.Header().Set("Allow", "GET, POST")
	rec.WriteHeader(http.StatusAccepted)
	_, _ = rec.WriteString(`{"ok":true}`)

	resp := httpResponseToLambda(rec)
	assert.Equal(t, http.StatusAccepted, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Headers["Content-Type"])
	assert.Equal(t, "GET, POST", resp.Headers["Allow"])
	assert.JSONEq(t, `{"ok":true}`, resp.Body)
}

func TestHTTPResponseToLambda_DefaultStatusIs200WhenUnset(t *testing.T) {
	rec := httptest.NewRecorder()
	_, _ = rec.WriteString("ok")
	resp := httpResponseToLambda(rec)
	// httptest.NewRecorder defaults Code to 200, matching net/http behaviour
	// when no WriteHeader is called before Write.
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "ok", resp.Body)
}

func TestLambdaToHTTPRequest_EmptyBodyOK(t *testing.T) {
	req := events.LambdaFunctionURLRequest{
		RawPath: "/status",
		RequestContext: events.LambdaFunctionURLRequestContext{
			HTTP: events.LambdaFunctionURLRequestContextHTTPDescription{Method: http.MethodGet},
		},
	}
	httpReq, err := lambdaToHTTPRequest(req)
	require.NoError(t, err)
	body, err := io.ReadAll(httpReq.Body)
	require.NoError(t, err)
	assert.Equal(t, "", string(body))
}

func TestLambdaToHTTPRequest_PreservesRawQueryEncoding(t *testing.T) {
	req := events.LambdaFunctionURLRequest{
		RawPath:        "/day",
		RawQueryString: "date=2026-05-19",
		RequestContext: events.LambdaFunctionURLRequestContext{
			HTTP: events.LambdaFunctionURLRequestContextHTTPDescription{Method: http.MethodGet},
		},
	}
	httpReq, err := lambdaToHTTPRequest(req)
	require.NoError(t, err)
	assert.Equal(t, "date=2026-05-19", httpReq.URL.RawQuery,
		"raw query string should pass through unchanged for handlers that re-parse it")
	assert.Equal(t, "2026-05-19", httpReq.URL.Query().Get("date"))
}

// silenceStrings keeps `strings` referenced even if a future trim is removed.
var _ = strings.TrimSpace
