package api

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/aws/aws-lambda-go/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockReader implements dynamo.Reader with configurable function fields.
type mockReader struct {
	queryReadingsFn    func(ctx context.Context, serial string, from, to int64) ([]dynamo.ReadingItem, error)
	getSystemFn        func(ctx context.Context, serial string) (*dynamo.SystemItem, error)
	getOffpeakFn       func(ctx context.Context, serial, date string) (*dynamo.OffpeakItem, error)
	queryOffpeakFn     func(ctx context.Context, serial, start, end string) ([]dynamo.OffpeakItem, error)
	getDailyEnergyFn   func(ctx context.Context, serial, date string) (*dynamo.DailyEnergyItem, error)
	queryDailyEnergyFn func(ctx context.Context, serial, start, end string) ([]dynamo.DailyEnergyItem, error)
	queryDailyPowerFn  func(ctx context.Context, serial, date string) ([]dynamo.DailyPowerItem, error)
	getNoteFn          func(ctx context.Context, serial, date string) (*dynamo.NoteItem, error)
	queryNotesFn       func(ctx context.Context, serial, start, end string) ([]dynamo.NoteItem, error)
}

func (m *mockReader) QueryReadings(ctx context.Context, serial string, from, to int64) ([]dynamo.ReadingItem, error) {
	if m.queryReadingsFn != nil {
		return m.queryReadingsFn(ctx, serial, from, to)
	}
	return []dynamo.ReadingItem{}, nil
}

func (m *mockReader) GetSystem(ctx context.Context, serial string) (*dynamo.SystemItem, error) {
	if m.getSystemFn != nil {
		return m.getSystemFn(ctx, serial)
	}
	return nil, nil
}

func (m *mockReader) GetOffpeak(ctx context.Context, serial, date string) (*dynamo.OffpeakItem, error) {
	if m.getOffpeakFn != nil {
		return m.getOffpeakFn(ctx, serial, date)
	}
	return nil, nil
}

func (m *mockReader) QueryOffpeak(ctx context.Context, serial, start, end string) ([]dynamo.OffpeakItem, error) {
	if m.queryOffpeakFn != nil {
		return m.queryOffpeakFn(ctx, serial, start, end)
	}
	return []dynamo.OffpeakItem{}, nil
}

func (m *mockReader) GetDailyEnergy(ctx context.Context, serial, date string) (*dynamo.DailyEnergyItem, error) {
	if m.getDailyEnergyFn != nil {
		return m.getDailyEnergyFn(ctx, serial, date)
	}
	return nil, nil
}

func (m *mockReader) QueryDailyEnergy(ctx context.Context, serial, start, end string) ([]dynamo.DailyEnergyItem, error) {
	if m.queryDailyEnergyFn != nil {
		return m.queryDailyEnergyFn(ctx, serial, start, end)
	}
	return []dynamo.DailyEnergyItem{}, nil
}

func (m *mockReader) QueryDailyPower(ctx context.Context, serial, date string) ([]dynamo.DailyPowerItem, error) {
	if m.queryDailyPowerFn != nil {
		return m.queryDailyPowerFn(ctx, serial, date)
	}
	return []dynamo.DailyPowerItem{}, nil
}

func (m *mockReader) GetNote(ctx context.Context, serial, date string) (*dynamo.NoteItem, error) {
	if m.getNoteFn != nil {
		return m.getNoteFn(ctx, serial, date)
	}
	return nil, nil
}

func (m *mockReader) QueryNotes(ctx context.Context, serial, start, end string) ([]dynamo.NoteItem, error) {
	if m.queryNotesFn != nil {
		return m.queryNotesFn(ctx, serial, start, end)
	}
	return []dynamo.NoteItem{}, nil
}

const testToken = "test-secret-token"
const testSerial = "AB1234"

// newTestHandler creates a Handler with a mock reader and test credentials.
func newTestHandler() *Handler {
	return NewHandler(&mockReader{}, nil, testSerial, testToken, "11:00", "14:00")
}

// makeRequest builds a LambdaFunctionURLRequest with the given method, path, and optional auth header.
func makeRequest(method, path, authHeader string) events.LambdaFunctionURLRequest {
	req := events.LambdaFunctionURLRequest{
		RawPath: path,
		Headers: map[string]string{},
		RequestContext: events.LambdaFunctionURLRequestContext{
			HTTP: events.LambdaFunctionURLRequestContextHTTPDescription{
				Method: method,
			},
		},
	}
	if authHeader != "" {
		req.Headers["authorization"] = authHeader
	}
	return req
}

func TestHandleMethod(t *testing.T) {
	h := newTestHandler()

	tests := map[string]struct {
		method     string
		wantStatus int
		wantError  string
	}{
		"GET passes": {
			method:     "GET",
			wantStatus: 200,
		},
		"POST returns 405": {
			method:     "POST",
			wantStatus: 405,
			wantError:  "method not allowed",
		},
		"PUT returns 405": {
			method:     "PUT",
			wantStatus: 405,
			wantError:  "method not allowed",
		},
		"DELETE returns 405": {
			method:     "DELETE",
			wantStatus: 405,
			wantError:  "method not allowed",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			req := makeRequest(tc.method, "/status", "Bearer "+testToken)
			resp, err := h.Handle(context.Background(), req)
			require.NoError(t, err)
			assert.Equal(t, tc.wantStatus, resp.StatusCode)

			if tc.wantError != "" {
				assert.Equal(t, "application/json", resp.Headers["Content-Type"])
				var body map[string]string
				require.NoError(t, json.Unmarshal([]byte(resp.Body), &body))
				assert.Equal(t, tc.wantError, body["error"])
			}
		})
	}
}

func TestHandleAuth(t *testing.T) {
	h := newTestHandler()

	tests := map[string]struct {
		authHeader string
		wantStatus int
		wantError  string
	}{
		"valid token": {
			authHeader: "Bearer " + testToken,
			wantStatus: 200,
		},
		"missing header": {
			authHeader: "",
			wantStatus: 401,
			wantError:  "unauthorized",
		},
		"wrong token": {
			authHeader: "Bearer wrong-token",
			wantStatus: 401,
			wantError:  "unauthorized",
		},
		"malformed bearer header": {
			authHeader: "Token " + testToken,
			wantStatus: 401,
			wantError:  "unauthorized",
		},
		"bearer without space": {
			authHeader: "Bearer",
			wantStatus: 401,
			wantError:  "unauthorized",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			req := makeRequest("GET", "/status", tc.authHeader)
			resp, err := h.Handle(context.Background(), req)
			require.NoError(t, err)
			assert.Equal(t, tc.wantStatus, resp.StatusCode)

			if tc.wantError != "" {
				assert.Equal(t, "application/json", resp.Headers["Content-Type"])
				var body map[string]string
				require.NoError(t, json.Unmarshal([]byte(resp.Body), &body))
				assert.Equal(t, tc.wantError, body["error"])
			}
		})
	}
}

func TestHandleAuthBeforeRouting(t *testing.T) {
	h := newTestHandler()

	// Invalid token + unknown path should return 401, not 404.
	// This verifies auth runs before routing.
	req := makeRequest("GET", "/nonexistent", "Bearer wrong-token")
	resp, err := h.Handle(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, 401, resp.StatusCode)

	var body map[string]string
	require.NoError(t, json.Unmarshal([]byte(resp.Body), &body))
	assert.Equal(t, "unauthorized", body["error"])
}

func TestHandleRouting(t *testing.T) {
	h := newTestHandler()

	tests := map[string]struct {
		path        string
		queryParams map[string]string
		wantStatus  int
		wantError   string
	}{
		"/status returns 200": {
			path:       "/status",
			wantStatus: 200,
		},
		"/history returns 200": {
			path:       "/history",
			wantStatus: 200,
		},
		"/day returns 200 with valid date": {
			path:        "/day",
			queryParams: map[string]string{"date": "2026-04-15"},
			wantStatus:  200,
		},
		"unknown path returns 404": {
			path:       "/unknown",
			wantStatus: 404,
			wantError:  "not found",
		},
		"root path returns 404": {
			path:       "/",
			wantStatus: 404,
			wantError:  "not found",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			req := makeRequest("GET", tc.path, "Bearer "+testToken)
			if tc.queryParams != nil {
				req.QueryStringParameters = tc.queryParams
			}
			resp, err := h.Handle(context.Background(), req)
			require.NoError(t, err)
			assert.Equal(t, tc.wantStatus, resp.StatusCode)

			if tc.wantError != "" {
				assert.Equal(t, "application/json", resp.Headers["Content-Type"])
				var body map[string]string
				require.NoError(t, json.Unmarshal([]byte(resp.Body), &body))
				assert.Equal(t, tc.wantError, body["error"])
			}

			// All responses should have Content-Type: application/json.
			assert.Equal(t, "application/json", resp.Headers["Content-Type"])
		})
	}
}

func TestHandleErrorResponseFormat(t *testing.T) {
	h := newTestHandler()

	// Verify error responses are valid JSON with the expected structure.
	tests := map[string]struct {
		method     string
		path       string
		auth       string
		wantStatus int
	}{
		"405 response": {
			method: "POST", path: "/status", auth: "Bearer " + testToken,
			wantStatus: 405,
		},
		"401 response": {
			method: "GET", path: "/status", auth: "",
			wantStatus: 401,
		},
		"404 response": {
			method: "GET", path: "/nope", auth: "Bearer " + testToken,
			wantStatus: 404,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			req := makeRequest(tc.method, tc.path, tc.auth)
			resp, err := h.Handle(context.Background(), req)
			require.NoError(t, err)
			assert.Equal(t, tc.wantStatus, resp.StatusCode)
			assert.Equal(t, "application/json", resp.Headers["Content-Type"])

			// Body must be valid JSON with an "error" key.
			var body map[string]string
			require.NoError(t, json.Unmarshal([]byte(resp.Body), &body), "body should be valid JSON")
			assert.NotEmpty(t, body["error"], "error field should be non-empty")
		})
	}
}
