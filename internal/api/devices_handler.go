package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/aws/aws-lambda-go/events"
)

// deviceRegistration is the wire shape of POST /devices.
type deviceRegistration struct {
	DeviceID     string  `json:"deviceId"`
	Platform     string  `json:"platform"`
	APNsToken    *string `json:"apnsToken,omitempty"`
	TZIdentifier string  `json:"tzIdentifier"`
	TZUpdatedAt  int64   `json:"tzUpdatedAt"`
}

// handleRegisterDevice upserts a device row. Idempotent: re-POSTing the same
// payload returns the same canonical body. Partial payloads (e.g., apnsToken
// absent because permission was denied) preserve the existing row's values
// where applicable.
func (h *Handler) handleRegisterDevice(ctx context.Context, req events.LambdaFunctionURLRequest) events.LambdaFunctionURLResponse {
	if h.devices == nil {
		slog.Error("device register attempted but devices store is nil")
		return errorResponse(http.StatusInternalServerError, "internal error")
	}

	body := requestBody(req)
	var payload deviceRegistration
	if err := json.Unmarshal(body, &payload); err != nil {
		return errorResponse(http.StatusBadRequest, "malformed request body")
	}
	if payload.DeviceID == "" {
		return errorResponse(http.StatusBadRequest, "deviceId required")
	}
	if payload.Platform != "ios" && payload.Platform != "macos" {
		return errorResponse(http.StatusBadRequest, "platform must be ios or macos")
	}
	if payload.TZIdentifier == "" {
		return errorResponse(http.StatusBadRequest, "tzIdentifier required")
	}

	now := h.nowFunc().UTC().Format(time.RFC3339)

	// Read the existing row so we can preserve fields the payload omits
	// (token, createdAt) and so we don't downgrade tokenStatus on a token-
	// absent registration.
	existing, err := h.devices.GetDevice(ctx, payload.DeviceID)
	if err != nil {
		slog.Error("get device failed", "device_id", payload.DeviceID, "error", err)
		return errorResponse(http.StatusInternalServerError, "internal error")
	}

	item := dynamo.DeviceItem{
		DeviceID:         payload.DeviceID,
		Platform:         payload.Platform,
		TZIdentifier:     payload.TZIdentifier,
		TZUpdatedAt:      payload.TZUpdatedAt,
		LastRegisteredAt: now,
		TokenStatus:      "active",
		CreatedAt:        now,
	}
	if payload.APNsToken != nil && *payload.APNsToken != "" {
		item.APNsToken = *payload.APNsToken
		item.APNsTokenUpdatedAt = now
	}
	if existing != nil {
		// Preserve creation timestamp; only first registration writes one.
		if existing.CreatedAt != "" {
			item.CreatedAt = existing.CreatedAt
		}
		// Token absent in the payload but present in the row: keep it.
		if payload.APNsToken == nil {
			item.APNsToken = existing.APNsToken
			item.APNsTokenUpdatedAt = existing.APNsTokenUpdatedAt
		}
	}

	if err := h.devices.PutDeviceConditional(ctx, item, payload.TZUpdatedAt); err != nil {
		if errConditionalCheckFailed(err) {
			return errorResponse(http.StatusConflict, "stale tzUpdatedAt")
		}
		slog.Error("put device failed", "device_id", payload.DeviceID, "error", err)
		return errorResponse(http.StatusInternalServerError, "internal error")
	}
	return jsonResponse(item)
}

// requestBody returns the raw request body bytes regardless of base64 framing
// — POST handlers care about the decoded shape, not the wire format. This
// duplicates a couple of lines from handleNote on purpose: handleNote also
// applies a 4KB size cap that does not apply to other endpoints, so sharing
// would muddy the limits.
func requestBody(req events.LambdaFunctionURLRequest) []byte {
	if req.IsBase64Encoded {
		decoded, err := base64Decode(req.Body)
		if err == nil {
			return decoded
		}
	}
	return []byte(req.Body)
}
