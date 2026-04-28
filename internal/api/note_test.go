package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/aws/aws-lambda-go/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockNoteWriter records calls and lets tests inject failures.
type mockNoteWriter struct {
	putFn    func(ctx context.Context, item dynamo.NoteItem) error
	deleteFn func(ctx context.Context, serial, date string) error
	puts     []dynamo.NoteItem
	deletes  []struct{ serial, date string }
}

func (m *mockNoteWriter) PutNote(ctx context.Context, item dynamo.NoteItem) error {
	m.puts = append(m.puts, item)
	if m.putFn != nil {
		return m.putFn(ctx, item)
	}
	return nil
}

func (m *mockNoteWriter) DeleteNote(ctx context.Context, serial, date string) error {
	m.deletes = append(m.deletes, struct{ serial, date string }{serial, date})
	if m.deleteFn != nil {
		return m.deleteFn(ctx, serial, date)
	}
	return nil
}

// newNoteTestHandler returns a handler wired to the supplied notes writer and
// a nowFunc pinned to 2026-04-15 10:00 Sydney so "today" is 2026-04-15.
func newNoteTestHandler(notes NoteWriter) *Handler {
	h := NewHandler(&mockReader{}, notes, testSerial, testToken, "11:00", "14:00")
	h.nowFunc = func() time.Time { return time.Date(2026, 4, 15, 10, 0, 0, 0, sydneyTZ) }
	return h
}

// noteRequest builds a /note request with the given method, headers, and body.
func noteRequest(method string, headers map[string]string, body string, isBase64 bool) events.LambdaFunctionURLRequest {
	req := events.LambdaFunctionURLRequest{
		RawPath: "/note",
		Headers: map[string]string{"authorization": "Bearer " + testToken},
		Body:    body,
		RequestContext: events.LambdaFunctionURLRequestContext{
			HTTP: events.LambdaFunctionURLRequestContextHTTPDescription{Method: method},
		},
		IsBase64Encoded: isBase64,
	}
	for k, v := range headers {
		req.Headers[strings.ToLower(k)] = v
	}
	return req
}

func TestHandleNote_AuthRequired(t *testing.T) {
	h := newNoteTestHandler(&mockNoteWriter{})

	tests := map[string]string{
		"missing":   "",
		"wrong":     "Bearer wrong-token",
		"malformed": "Token " + testToken,
	}
	for name, header := range tests {
		t.Run(name, func(t *testing.T) {
			req := noteRequest("PUT", map[string]string{"content-type": "application/json"}, `{"date":"2026-04-15","text":"hi"}`, false)
			req.Headers["authorization"] = header
			if header == "" {
				delete(req.Headers, "authorization")
			}
			resp, err := h.Handle(context.Background(), req)
			require.NoError(t, err)
			assert.Equal(t, 401, resp.StatusCode)
			assertErrorBody(t, resp, "unauthorized")
		})
	}
}

func TestHandleNote_MethodNotAllowed(t *testing.T) {
	h := newNoteTestHandler(&mockNoteWriter{})

	for _, method := range []string{"GET", "POST", "DELETE", "PATCH"} {
		t.Run(method, func(t *testing.T) {
			req := noteRequest(method, map[string]string{"content-type": "application/json"}, `{"date":"2026-04-15","text":"hi"}`, false)
			resp, err := h.Handle(context.Background(), req)
			require.NoError(t, err)
			assert.Equal(t, 405, resp.StatusCode)
			assert.Equal(t, "PUT", resp.Headers["Allow"])
		})
	}
}

func TestHandleNote_UnsupportedMediaType(t *testing.T) {
	h := newNoteTestHandler(&mockNoteWriter{})

	tests := map[string]map[string]string{
		"missing content-type":   nil,
		"text/plain":             {"content-type": "text/plain"},
		"application/xml":        {"content-type": "application/xml"},
		"application/jsonp":      {"content-type": "application/jsonp"}, // not a prefix match for application/json
		"multipart/form-data":    {"content-type": "multipart/form-data"},
		"application_underscore": {"content-type": "application/json-patch"},
	}
	for name, headers := range tests {
		t.Run(name, func(t *testing.T) {
			req := noteRequest("PUT", headers, `{"date":"2026-04-15","text":"hi"}`, false)
			resp, err := h.Handle(context.Background(), req)
			require.NoError(t, err)
			assert.Equal(t, 415, resp.StatusCode)
			assertErrorBody(t, resp, "unsupported media type")
		})
	}
}

func TestHandleNote_AcceptedContentTypes(t *testing.T) {
	for name, ct := range map[string]string{
		"plain":            "application/json",
		"with charset":     "application/json; charset=utf-8",
		"with extra param": "application/json;something=else",
	} {
		t.Run(name, func(t *testing.T) {
			h := newNoteTestHandler(&mockNoteWriter{})
			req := noteRequest("PUT", map[string]string{"content-type": ct}, `{"date":"2026-04-15","text":"hi"}`, false)
			resp, err := h.Handle(context.Background(), req)
			require.NoError(t, err)
			assert.Equal(t, 200, resp.StatusCode, "content-type %q should be accepted", ct)
		})
	}
}

func TestHandleNote_BodyTooLarge(t *testing.T) {
	h := newNoteTestHandler(&mockNoteWriter{})

	// Build a >4KB body. Even if it is otherwise invalid JSON, the size check
	// must short-circuit ahead of field validation.
	big := bytes.Repeat([]byte("x"), 4097)
	req := noteRequest("PUT", map[string]string{"content-type": "application/json"}, string(big), false)
	resp, err := h.Handle(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, 413, resp.StatusCode)
	assertErrorBody(t, resp, "request too large")
}

func TestHandleNote_Base64BodyDecoded(t *testing.T) {
	h := newNoteTestHandler(&mockNoteWriter{})

	body := `{"date":"2026-04-15","text":"hi"}`
	encoded := base64.StdEncoding.EncodeToString([]byte(body))
	req := noteRequest("PUT", map[string]string{"content-type": "application/json"}, encoded, true)
	resp, err := h.Handle(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode, "base64-encoded JSON body must be decoded before parse")
}

func TestHandleNote_MalformedJSON(t *testing.T) {
	h := newNoteTestHandler(&mockNoteWriter{})

	req := noteRequest("PUT", map[string]string{"content-type": "application/json"}, `{not-json`, false)
	resp, err := h.Handle(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, 400, resp.StatusCode)
	assertErrorBody(t, resp, "malformed request body")
}

func TestHandleNote_DateValidation(t *testing.T) {
	tests := map[string]struct {
		body    string
		wantMsg string
	}{
		"missing date":             {`{"text":"hi"}`, "invalid date"},
		"empty date":               {`{"date":"","text":"hi"}`, "invalid date"},
		"wrong format":             {`{"date":"15-04-2026","text":"hi"}`, "invalid date"},
		"non-Gregorian month":      {`{"date":"2026-13-01","text":"hi"}`, "invalid date"},
		"non-Gregorian day":        {`{"date":"2026-02-30","text":"hi"}`, "invalid date"},
		"future date Sydney clock": {`{"date":"2026-04-16","text":"hi"}`, "date may not be in the future"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			h := newNoteTestHandler(&mockNoteWriter{})
			req := noteRequest("PUT", map[string]string{"content-type": "application/json"}, tc.body, false)
			resp, err := h.Handle(context.Background(), req)
			require.NoError(t, err)
			assert.Equal(t, 400, resp.StatusCode)
			assertErrorBody(t, resp, tc.wantMsg)
		})
	}
}

func TestHandleNote_TodayAndPastAreAccepted(t *testing.T) {
	for name, date := range map[string]string{
		"today":     "2026-04-15",
		"yesterday": "2026-04-14",
	} {
		t.Run(name, func(t *testing.T) {
			h := newNoteTestHandler(&mockNoteWriter{})
			body, _ := json.Marshal(map[string]string{"date": date, "text": "ok"})
			req := noteRequest("PUT", map[string]string{"content-type": "application/json"}, string(body), false)
			resp, err := h.Handle(context.Background(), req)
			require.NoError(t, err)
			assert.Equal(t, 200, resp.StatusCode)
		})
	}
}

func TestHandleNote_OverGraphemeLimit(t *testing.T) {
	// Build inputs >200 graphemes using each fixture sequence so the API
	// agrees with the cross-stack count rather than naive byte/scalar counts.
	for _, entry := range loadFixture(t) {
		if entry.Graphemes == 0 {
			continue
		}
		t.Run(entry.Name+"_over_limit", func(t *testing.T) {
			repeat := (200 / entry.Graphemes) + 2
			text := strings.Repeat(entry.Input, repeat)
			require.Greater(t, graphemeCount(text), 200)

			body, _ := json.Marshal(map[string]string{"date": "2026-04-15", "text": text})
			if len(body) > noteMaxBodyBytes {
				// Sequences whose UTF-8 footprint pushes the body past 4KB
				// hit the 413 short-circuit by design (§handleNote step 3),
				// not the grapheme rule. Skip — the size check has its own
				// dedicated test.
				t.Skipf("body %d bytes exceeds 4KB limit; covered by TestHandleNote_BodyTooLarge", len(body))
			}

			h := newNoteTestHandler(&mockNoteWriter{})
			req := noteRequest("PUT", map[string]string{"content-type": "application/json"}, string(body), false)
			resp, err := h.Handle(context.Background(), req)
			require.NoError(t, err)
			assert.Equal(t, 400, resp.StatusCode)
			assertErrorBody(t, resp, "note must be 200 characters or fewer")
		})
	}
}

func TestHandleNote_UpsertReturnsCanonical(t *testing.T) {
	notes := &mockNoteWriter{}
	h := newNoteTestHandler(notes)

	body := `{"date":"2026-04-15","text":"  Away in Bali  "}`
	req := noteRequest("PUT", map[string]string{"content-type": "application/json"}, body, false)
	resp, err := h.Handle(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	var got struct {
		Date      string  `json:"date"`
		Text      string  `json:"text"`
		UpdatedAt *string `json:"updatedAt"`
	}
	require.NoError(t, json.Unmarshal([]byte(resp.Body), &got))
	assert.Equal(t, "2026-04-15", got.Date)
	assert.Equal(t, "Away in Bali", got.Text, "stored text is normalised + trimmed")
	require.NotNil(t, got.UpdatedAt)
	_, err = time.Parse(time.RFC3339, *got.UpdatedAt)
	require.NoError(t, err, "updatedAt must be RFC3339")
	assert.True(t, strings.HasSuffix(*got.UpdatedAt, "Z"), "updatedAt must be UTC")

	require.Len(t, notes.puts, 1)
	assert.Equal(t, "Away in Bali", notes.puts[0].Text)
	assert.Equal(t, testSerial, notes.puts[0].SysSn)
	assert.Empty(t, notes.deletes)
}

func TestHandleNote_EmptyTextDeletes(t *testing.T) {
	tests := map[string]string{
		"empty":           ``,
		"whitespace only": `   `,
		"newline only":    "\n\n",
		"missing field":   "<missing>",
		"unicode space":   " ",
	}
	for name, text := range tests {
		t.Run(name, func(t *testing.T) {
			notes := &mockNoteWriter{}
			h := newNoteTestHandler(notes)

			var bodyJSON string
			if text == "<missing>" {
				bodyJSON = `{"date":"2026-04-15"}`
			} else {
				b, _ := json.Marshal(map[string]string{"date": "2026-04-15", "text": text})
				bodyJSON = string(b)
			}
			req := noteRequest("PUT", map[string]string{"content-type": "application/json"}, bodyJSON, false)
			resp, err := h.Handle(context.Background(), req)
			require.NoError(t, err)
			require.Equal(t, 200, resp.StatusCode)

			var raw map[string]any
			require.NoError(t, json.Unmarshal([]byte(resp.Body), &raw))
			assert.Equal(t, "2026-04-15", raw["date"])
			assert.Equal(t, "", raw["text"], "delete returns empty text")
			assert.Nil(t, raw["updatedAt"], "delete returns null updatedAt")

			require.Len(t, notes.deletes, 1)
			assert.Equal(t, testSerial, notes.deletes[0].serial)
			assert.Equal(t, "2026-04-15", notes.deletes[0].date)
			assert.Empty(t, notes.puts)
		})
	}
}

func TestHandleNote_DeleteIsIdempotentRegardlessOfPriorState(t *testing.T) {
	// Underlying writer claims the row didn't exist (returns nil). The
	// handler must still respond 200 with the canonical "delete" payload.
	notes := &mockNoteWriter{
		deleteFn: func(_ context.Context, _, _ string) error { return nil },
	}
	h := newNoteTestHandler(notes)

	req := noteRequest("PUT", map[string]string{"content-type": "application/json"}, `{"date":"2026-04-15","text":""}`, false)
	resp, err := h.Handle(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestHandleNote_DynamoErrorReturns500(t *testing.T) {
	for name, notes := range map[string]*mockNoteWriter{
		"put fails": {
			putFn: func(_ context.Context, _ dynamo.NoteItem) error { return errors.New("throttled") },
		},
		"delete fails": {
			deleteFn: func(_ context.Context, _, _ string) error { return errors.New("throttled") },
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := newNoteTestHandler(notes)
			body := `{"date":"2026-04-15","text":"hi"}`
			if name == "delete fails" {
				body = `{"date":"2026-04-15","text":""}`
			}
			req := noteRequest("PUT", map[string]string{"content-type": "application/json"}, body, false)
			resp, err := h.Handle(context.Background(), req)
			require.NoError(t, err)
			assert.Equal(t, 500, resp.StatusCode)
		})
	}
}

func TestHandleNote_NilWriterReturns500NotPanic(t *testing.T) {
	// A misconfigured Lambda (e.g. TABLE_NOTES env var missing) wires a nil
	// writer. The handler must return 500 cleanly rather than nil-panic.
	h := NewHandler(&mockReader{}, nil, testSerial, testToken, "11:00", "14:00")
	h.nowFunc = func() time.Time { return time.Date(2026, 4, 15, 10, 0, 0, 0, sydneyTZ) }

	req := noteRequest("PUT", map[string]string{"content-type": "application/json"}, `{"date":"2026-04-15","text":"hi"}`, false)
	resp, err := h.Handle(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, 500, resp.StatusCode)
	assertErrorBody(t, resp, "internal error")
}

func TestHandleNote_TextNeverAppearsInLogs(t *testing.T) {
	// Capture slog output produced during the request and assert the secret
	// note text never lands in any log line, regardless of which path runs.
	var buf bytes.Buffer
	orig := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(orig) })

	const secret = "super-secret-note-content-shouldNeverLeak"

	notes := &mockNoteWriter{}
	h := newNoteTestHandler(notes)
	body, _ := json.Marshal(map[string]string{"date": "2026-04-15", "text": secret})
	req := noteRequest("PUT", map[string]string{"content-type": "application/json"}, string(body), false)
	resp, err := h.Handle(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	assert.NotContains(t, buf.String(), secret, "note text must never appear in any slog output")
}

// assertErrorBody parses the response body and checks the error field.
func assertErrorBody(t *testing.T, resp events.LambdaFunctionURLResponse, want string) {
	t.Helper()
	assert.Equal(t, "application/json", resp.Headers["Content-Type"])
	var body map[string]string
	require.NoError(t, json.Unmarshal([]byte(resp.Body), &body))
	assert.Equal(t, want, body["error"])
}
