package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"mime"
	"time"

	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/aws/aws-lambda-go/events"
)

const (
	noteMaxGraphemes = 200
	noteMaxBodyBytes = 4096
)

// notePayload is the request body for PUT /note. It implements slog.LogValuer
// so the note text never lands in any structured log line, regardless of
// which logger formats the value.
type notePayload struct {
	Date string `json:"date"`
	Text string `json:"text"`
}

// LogValue redacts text. Anyone logging the payload (slog.Any("payload", p))
// gets {date, text_len} only.
func (n notePayload) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("date", n.Date),
		slog.Int("text_len", len(n.Text)),
	)
}

// noteResponse is the canonical body returned by PUT /note. Empty text and
// nil updatedAt indicate a delete.
type noteResponse struct {
	Date      string  `json:"date"`
	Text      string  `json:"text"`
	UpdatedAt *string `json:"updatedAt"`
}

// handleNote implements the validation order documented in design.md
// §handleNote validation order. Each step short-circuits.
func (h *Handler) handleNote(ctx context.Context, req events.LambdaFunctionURLRequest) events.LambdaFunctionURLResponse {
	// 1. 415 if Content-Type is not application/json[;...].
	if !isJSONContentType(req.Headers["content-type"]) {
		return errorResponse(415, "unsupported media type")
	}

	// 2. Decode body. Function URLs sometimes flag JSON as base64 depending
	// on the calling client, so handle both. Reject base64 inputs that
	// could not possibly fit under noteMaxBodyBytes once decoded, before
	// allocating the decode buffer.
	body := []byte(req.Body)
	if req.IsBase64Encoded {
		if len(req.Body) > base64.StdEncoding.EncodedLen(noteMaxBodyBytes) {
			return errorResponse(413, "request too large")
		}
		decoded, err := base64.StdEncoding.DecodeString(req.Body)
		if err != nil {
			return errorResponse(400, "malformed request body")
		}
		body = decoded
	}

	// 3. 413 before any field validation.
	if len(body) > noteMaxBodyBytes {
		return errorResponse(413, "request too large")
	}

	// 4. Parse JSON.
	var payload notePayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return errorResponse(400, "malformed request body")
	}

	// 5. Date present, syntactically valid, and a real Gregorian date.
	parsed, err := time.ParseInLocation("2006-01-02", payload.Date, sydneyTZ)
	if err != nil {
		return errorResponse(400, "invalid date")
	}

	// 6. Date not later than today in Sydney.
	today := h.nowFunc().In(sydneyTZ)
	todayMidnight := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, sydneyTZ)
	if parsed.After(todayMidnight) {
		return errorResponse(400, "date may not be in the future")
	}

	// 7. NFC + leading/trailing trim, then grapheme count.
	text := normalise(payload.Text)
	if graphemeCountNormalised(text) > noteMaxGraphemes {
		return errorResponse(400, "note must be 200 characters or fewer")
	}

	// 8. Empty text → delete; non-empty → put.
	if text == "" {
		if err := h.notes.DeleteNote(ctx, h.serial, payload.Date); err != nil {
			slog.Error("note delete failed", "date", payload.Date, "error", err)
			return errorResponse(500, "internal error")
		}
		return jsonResponse(noteResponse{Date: payload.Date, Text: "", UpdatedAt: nil})
	}

	updatedAt := h.nowFunc().UTC().Format(time.RFC3339)
	item := dynamo.NoteItem{
		SysSn:     h.serial,
		Date:      payload.Date,
		Text:      text,
		UpdatedAt: updatedAt,
	}
	if err := h.notes.PutNote(ctx, item); err != nil {
		slog.Error("note put failed", "date", payload.Date, "error", err)
		return errorResponse(500, "internal error")
	}
	return jsonResponse(noteResponse{Date: payload.Date, Text: text, UpdatedAt: &updatedAt})
}

// isJSONContentType returns true for application/json with optional parameters
// (charset, etc). False for missing or non-JSON values.
func isJSONContentType(value string) bool {
	if value == "" {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(value)
	return err == nil && mediaType == "application/json"
}
