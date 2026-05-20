package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"

	"github.com/aws/aws-lambda-go/events"
)

// lambdaToHTTPRequest translates a Lambda Function URL request into the
// standard *http.Request shape so handlers and middleware written against
// net/http can run unchanged. ~30 LOC as the design predicts. The Lambda
// invocation context is threaded through so downstream handlers and AWS
// SDK calls observe the same deadline.
func lambdaToHTTPRequest(ctx context.Context, req events.LambdaFunctionURLRequest) (*http.Request, error) {
	method := req.RequestContext.HTTP.Method
	if method == "" {
		method = http.MethodGet
	}

	rawQuery := req.RawQueryString
	if rawQuery == "" && len(req.QueryStringParameters) > 0 {
		values := url.Values{}
		for k, v := range req.QueryStringParameters {
			values.Set(k, v)
		}
		rawQuery = values.Encode()
	}

	target := req.RawPath
	if target == "" {
		target = "/"
	}
	if rawQuery != "" {
		target = target + "?" + rawQuery
	}

	var bodyReader io.Reader
	if req.Body != "" {
		if req.IsBase64Encoded {
			decoded, err := base64.StdEncoding.DecodeString(req.Body)
			if err != nil {
				return nil, fmt.Errorf("decode base64 body: %w", err)
			}
			bodyReader = bytes.NewReader(decoded)
		} else {
			bodyReader = strings.NewReader(req.Body)
		}
	} else {
		bodyReader = strings.NewReader("")
	}

	httpReq, err := http.NewRequestWithContext(ctx, method, target, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("build http request: %w", err)
	}
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}
	return httpReq, nil
}

// httpResponseToLambda renders an httptest.ResponseRecorder into the Lambda
// response shape. All current responses are JSON (valid UTF-8) so the body
// is passed through as-is via string conversion. If a future endpoint needs
// to emit binary bytes, switch to base64 encoding and set IsBase64Encoded.
func httpResponseToLambda(rec *httptest.ResponseRecorder) events.LambdaFunctionURLResponse {
	status := rec.Code
	if status == 0 {
		status = http.StatusOK
	}
	headers := make(map[string]string, len(rec.Header()))
	for k, v := range rec.Header() {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}
	bodyBytes := rec.Body.Bytes()
	return events.LambdaFunctionURLResponse{
		StatusCode: status,
		Headers:    headers,
		Body:       string(bodyBytes),
	}
}

// requestContextKey scopes the original Lambda request in the http.Request's
// context, so adapted handlers can recover it without rebuilding from
// scratch. Used by adaptEventHandler.
type requestContextKey struct{}

// adaptEventHandler wraps an event-returning handler as http.HandlerFunc.
// Existing handlers (handleStatus, handleHistory, handleDay, handleNote)
// keep their signature; only the call site changes.
func adaptEventHandler(fn func(ctx context.Context, req events.LambdaFunctionURLRequest) events.LambdaFunctionURLResponse) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		req, _ := r.Context().Value(requestContextKey{}).(events.LambdaFunctionURLRequest)
		resp := fn(r.Context(), req)
		for k, v := range resp.Headers {
			w.Header().Set(k, v)
		}
		if resp.StatusCode == 0 {
			resp.StatusCode = http.StatusOK
		}
		w.WriteHeader(resp.StatusCode)
		if resp.Body != "" {
			_, _ = io.WriteString(w, resp.Body)
		}
	}
}

// withRequestContext attaches the original Lambda request to the http.Request
// context so adapted handlers can reach req.Headers, req.Body, etc.
func withRequestContext(r *http.Request, req events.LambdaFunctionURLRequest) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), requestContextKey{}, req))
}
