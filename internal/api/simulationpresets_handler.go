package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"time"

	"github.com/ArjenSchwarz/flux/internal/dynamo"
)

// SetSimulationPresetStore wires the preset CRUD dependency. Called by
// cmd/api/main.go and rebuilds the mux so the routes pick up the store.
func (h *Handler) SetSimulationPresetStore(s SimulationPresetStore) {
	h.presets = s
	h.mux = h.buildMux()
}

// decodePresetPayload reads, size-limits, and JSON-decodes the request body.
// On failure the response is already written; callers return early.
func decodePresetPayload(w http.ResponseWriter, r *http.Request) (simulationPresetPayload, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, presetBodyMaxBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "malformed request body")
		return simulationPresetPayload{}, false
	}
	var payload simulationPresetPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		writeJSONError(w, http.StatusBadRequest, "malformed request body")
		return simulationPresetPayload{}, false
	}
	return payload, true
}

// handleListPresets returns every preset sorted by createdAt ascending.
func (h *Handler) handleListPresets(w http.ResponseWriter, r *http.Request) {
	if h.presets == nil {
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	presets, err := h.presets.ListPresets(r.Context())
	if err != nil {
		slog.Error("list simulation presets failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	sort.SliceStable(presets, func(i, j int) bool {
		return presets[i].CreatedAt < presets[j].CreatedAt
	})
	writeJSON(w, http.StatusOK, struct {
		Presets []dynamo.SimulationPresetItem `json:"presets"`
	}{Presets: presets})
}

// handleCreatePreset validates the payload, enforces the 20-preset cap,
// assigns server-side id/timestamps, and writes the row.
func (h *Handler) handleCreatePreset(w http.ResponseWriter, r *http.Request) {
	if h.presets == nil {
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	payload, ok := decodePresetPayload(w, r)
	if !ok {
		return
	}
	if msg := payload.validate(); msg != "" {
		writeJSONError(w, http.StatusBadRequest, msg)
		return
	}

	existing, err := h.presets.ListPresets(r.Context())
	if err != nil {
		slog.Error("list simulation presets failed during create", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if len(existing) >= presetCap {
		writeJSONError(w, http.StatusConflict, "preset cap reached")
		return
	}

	now := h.nowFunc().UTC().Format(time.RFC3339)
	item := dynamo.SimulationPresetItem{
		PresetID:  h.idFunc(),
		Label:     payload.Label,
		Watts:     payload.Watts,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := h.presets.PutPreset(r.Context(), item); err != nil {
		slog.Error("put simulation preset failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

// handleUpdatePreset validates and overwrites an existing preset row. The
// createdAt is preserved; updatedAt is bumped to now. 404 on unknown id.
func (h *Handler) handleUpdatePreset(w http.ResponseWriter, r *http.Request) {
	if h.presets == nil {
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "id required")
		return
	}
	payload, ok := decodePresetPayload(w, r)
	if !ok {
		return
	}
	if msg := payload.validate(); msg != "" {
		writeJSONError(w, http.StatusBadRequest, msg)
		return
	}

	existing, err := h.presets.ListPresets(r.Context())
	if err != nil {
		slog.Error("list simulation presets failed during update", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	var current *dynamo.SimulationPresetItem
	for i := range existing {
		if existing[i].PresetID == id {
			current = &existing[i]
			break
		}
	}
	if current == nil {
		writeJSONError(w, http.StatusNotFound, "preset not found")
		return
	}

	now := h.nowFunc().UTC().Format(time.RFC3339)
	item := dynamo.SimulationPresetItem{
		PresetID:  id,
		Label:     payload.Label,
		Watts:     payload.Watts,
		CreatedAt: current.CreatedAt,
		UpdatedAt: now,
	}
	if err := h.presets.PutPreset(r.Context(), item); err != nil {
		slog.Error("put simulation preset failed", "preset_id", id, "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// handleDeletePreset removes a preset by id. Idempotent — re-deleting a
// missing preset still returns 204.
func (h *Handler) handleDeletePreset(w http.ResponseWriter, r *http.Request) {
	if h.presets == nil {
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "id required")
		return
	}
	if err := h.presets.DeletePreset(r.Context(), id); err != nil {
		slog.Error("delete simulation preset failed", "preset_id", id, "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNoContent)
}
