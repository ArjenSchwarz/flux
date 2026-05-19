package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ArjenSchwarz/flux/internal/dynamo"
)

// socRulePayload is the wire shape of POST/PUT /devices/{deviceId}/rules.
type socRulePayload struct {
	ThresholdPercent int    `json:"thresholdPercent"`
	WindowStart      string `json:"windowStart"`
	WindowEnd        string `json:"windowEnd"`
	Enabled          bool   `json:"enabled"`
	Label            string `json:"label,omitempty"`
}

// validate enforces AC 1.3 and AC 1.2 on the wire shape. The error message
// is returned to the client; pick wording carefully.
func (p socRulePayload) validate() string {
	if p.ThresholdPercent < 1 || p.ThresholdPercent > 99 {
		return "thresholdPercent must be 1..99"
	}
	if err := validateHHMM(p.WindowStart); err != "" {
		return "windowStart: " + err
	}
	if err := validateHHMM(p.WindowEnd); err != "" {
		return "windowEnd: " + err
	}
	if p.WindowStart == p.WindowEnd {
		return "windowStart must differ from windowEnd"
	}
	if utf8.RuneCountInString(p.Label) > labelMaxChars {
		return "label exceeds 40 characters"
	}
	return ""
}

// validateHHMM mirrors the parser used by the evaluator's window helper.
// Local copy keeps the api package from importing the eval package.
func validateHHMM(s string) string {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return "must be HH:MM"
	}
	h, herr := strconv.Atoi(parts[0])
	m, merr := strconv.Atoi(parts[1])
	if herr != nil || merr != nil {
		return "must be HH:MM"
	}
	if h < 0 || h > 23 {
		return "hour out of range"
	}
	if m < 0 || m > 59 {
		return "minute out of range"
	}
	return ""
}

// handleListRules returns the rules for the given device, sorted by
// createdAt ascending so the UI list ordering matches AC 1.6.
func (h *Handler) handleListRules(w http.ResponseWriter, r *http.Request) {
	if h.rules == nil {
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	deviceID := r.PathValue("deviceId")
	if deviceID == "" {
		writeJSONError(w, http.StatusBadRequest, "deviceId required")
		return
	}
	rules, err := h.rules.ListRulesByDevice(r.Context(), deviceID)
	if err != nil {
		slog.Error("list rules failed", "device_id", deviceID, "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	sort.SliceStable(rules, func(i, j int) bool {
		return rules[i].CreatedAt < rules[j].CreatedAt
	})
	writeJSON(w, http.StatusOK, struct {
		Rules []dynamo.SoCRuleItem `json:"rules"`
	}{Rules: rules})
}

// handleCreateRule validates the payload, enforces the 10-rule cap, assigns
// server-side id/timestamps, and writes the row.
func (h *Handler) handleCreateRule(w http.ResponseWriter, r *http.Request) {
	if h.rules == nil {
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	deviceID := r.PathValue("deviceId")
	if deviceID == "" {
		writeJSONError(w, http.StatusBadRequest, "deviceId required")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "malformed request body")
		return
	}
	var payload socRulePayload
	if err := json.Unmarshal(body, &payload); err != nil {
		writeJSONError(w, http.StatusBadRequest, "malformed request body")
		return
	}
	if msg := payload.validate(); msg != "" {
		writeJSONError(w, http.StatusBadRequest, msg)
		return
	}

	existing, err := h.rules.ListRulesByDevice(r.Context(), deviceID)
	if err != nil {
		slog.Error("list rules failed during create", "device_id", deviceID, "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if len(existing) >= ruleCap {
		writeJSONError(w, http.StatusConflict, "rule cap reached")
		return
	}

	now := h.nowFunc().UTC().Format(time.RFC3339)
	item := dynamo.SoCRuleItem{
		DeviceID:         deviceID,
		RuleID:           h.idFunc(),
		ThresholdPercent: payload.ThresholdPercent,
		WindowStart:      payload.WindowStart,
		WindowEnd:        payload.WindowEnd,
		Enabled:          payload.Enabled,
		Label:            payload.Label,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := h.rules.PutRule(r.Context(), item); err != nil {
		slog.Error("put rule failed", "device_id", deviceID, "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

// handleUpdateRule validates, writes, then cleans fire-state. The cleanup is
// best-effort: a Dynamo failure on the Query+Delete path is logged and the
// PUT still returns 200 (AC 5.3 — the evaluator self-corrects on the next
// cache refresh because UpdatedAt has changed).
func (h *Handler) handleUpdateRule(w http.ResponseWriter, r *http.Request) {
	if h.rules == nil {
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	deviceID := r.PathValue("deviceId")
	ruleID := r.PathValue("ruleId")
	if deviceID == "" || ruleID == "" {
		writeJSONError(w, http.StatusBadRequest, "deviceId and ruleId required")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "malformed request body")
		return
	}
	var payload socRulePayload
	if err := json.Unmarshal(body, &payload); err != nil {
		writeJSONError(w, http.StatusBadRequest, "malformed request body")
		return
	}
	if msg := payload.validate(); msg != "" {
		writeJSONError(w, http.StatusBadRequest, msg)
		return
	}

	existing, err := h.rules.ListRulesByDevice(r.Context(), deviceID)
	if err != nil {
		slog.Error("list rules failed during update", "device_id", deviceID, "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	var current *dynamo.SoCRuleItem
	for i := range existing {
		if existing[i].RuleID == ruleID {
			current = &existing[i]
			break
		}
	}
	if current == nil {
		writeJSONError(w, http.StatusNotFound, "rule not found")
		return
	}

	now := h.nowFunc().UTC().Format(time.RFC3339)
	item := dynamo.SoCRuleItem{
		DeviceID:         deviceID,
		RuleID:           ruleID,
		ThresholdPercent: payload.ThresholdPercent,
		WindowStart:      payload.WindowStart,
		WindowEnd:        payload.WindowEnd,
		Enabled:          payload.Enabled,
		Label:            payload.Label,
		CreatedAt:        current.CreatedAt,
		UpdatedAt:        now,
	}
	if err := h.rules.PutRule(r.Context(), item); err != nil {
		slog.Error("put rule failed", "device_id", deviceID, "rule_id", ruleID, "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	h.cleanupFireState(r.Context(), deviceID, ruleID)
	writeJSON(w, http.StatusOK, item)
}

// handleDeleteRule removes the row and cleans fire-state. Idempotent on
// missing rules — re-deleting still returns 204.
func (h *Handler) handleDeleteRule(w http.ResponseWriter, r *http.Request) {
	if h.rules == nil {
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	deviceID := r.PathValue("deviceId")
	ruleID := r.PathValue("ruleId")
	if deviceID == "" || ruleID == "" {
		writeJSONError(w, http.StatusBadRequest, "deviceId and ruleId required")
		return
	}
	if err := h.rules.DeleteRule(r.Context(), deviceID, ruleID); err != nil {
		slog.Error("delete rule failed", "device_id", deviceID, "rule_id", ruleID, "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	h.cleanupFireState(r.Context(), deviceID, ruleID)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNoContent)
}

// cleanupFireState invokes the cleaner and logs failures without bubbling
// them up. The evaluator's UpdatedAt-tag mechanism (Decision 16) guarantees
// correctness regardless of cleanup outcome; stale rows TTL out after 7d.
func (h *Handler) cleanupFireState(ctx context.Context, deviceID, ruleID string) {
	if h.fireState == nil {
		return
	}
	if _, err := h.fireState.DeleteFireStateByDeviceRule(ctx, deviceID, ruleID); err != nil {
		slog.Warn("flux_lambda_firestate_cleanup_failed",
			"device_id", deviceID, "rule_id", ruleID, "error", err)
	}
}

// writeJSON marshals v as JSON and writes it with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		slog.Error("marshal response", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}
