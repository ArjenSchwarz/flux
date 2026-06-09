package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSimulationPresetStore implements the SimulationPresetStore interface
// in-memory so the CRUD handlers exercise the real validation + 20-preset cap
// path. Mirrors fakeSocRuleStore / the pricing fake store shape.
type fakeSimulationPresetStore struct {
	mu      sync.Mutex
	presets []dynamo.SimulationPresetItem
	err     error
}

func newFakeSimulationPresetStore() *fakeSimulationPresetStore {
	return &fakeSimulationPresetStore{}
}

func (s *fakeSimulationPresetStore) ListPresets(_ context.Context) ([]dynamo.SimulationPresetItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return nil, s.err
	}
	out := make([]dynamo.SimulationPresetItem, len(s.presets))
	copy(out, s.presets)
	return out, nil
}

func (s *fakeSimulationPresetStore) PutPreset(_ context.Context, item dynamo.SimulationPresetItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	for i, p := range s.presets {
		if p.PresetID == item.PresetID {
			s.presets[i] = item
			return nil
		}
	}
	s.presets = append(s.presets, item)
	return nil
}

func (s *fakeSimulationPresetStore) DeletePreset(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	for i, p := range s.presets {
		if p.PresetID == id {
			s.presets = append(s.presets[:i], s.presets[i+1:]...)
			return nil
		}
	}
	return nil
}

// newPresetsTestHandler wires a fake preset store with a fixed clock and a
// deterministic id generator so create/list/PUT/DELETE assertions are exact.
func newPresetsTestHandler(store *fakeSimulationPresetStore) *Handler {
	h := NewHandler(&mockReader{}, nil, testSerial, testToken, "11:00", "14:00")
	h.SetSimulationPresetStore(store)
	fixed := time.Date(2026, 5, 19, 10, 0, 0, 0, time.UTC)
	h.nowFunc = func() time.Time { return fixed }
	counter := 0
	h.idFunc = func() string {
		counter++
		return fmt.Sprintf("preset-uuid-%d", counter)
	}
	h.mux = h.buildMux()
	return h
}

func presetBody(label string, watts int) string {
	b, _ := json.Marshal(map[string]any{"label": label, "watts": watts})
	return string(b)
}

func TestHandleCreatePreset_AssignsIDAndTimestamps(t *testing.T) {
	store := newFakeSimulationPresetStore()
	h := newPresetsTestHandler(store)

	req := makeRequest(http.MethodPost, "/simulation-presets", "Bearer "+testToken)
	req.Body = presetBody("Charge car", 1700)
	resp, err := h.Handle(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var item dynamo.SimulationPresetItem
	require.NoError(t, json.Unmarshal([]byte(resp.Body), &item))
	assert.Equal(t, "preset-uuid-1", item.PresetID)
	assert.Equal(t, "Charge car", item.Label)
	assert.Equal(t, 1700, item.Watts)
	assert.Equal(t, "2026-05-19T10:00:00Z", item.CreatedAt)
	assert.Equal(t, "2026-05-19T10:00:00Z", item.UpdatedAt)

	// Persisted in the store.
	require.Len(t, store.presets, 1)
}

func TestHandleListPresets_ReturnsAllSortedByCreatedAt(t *testing.T) {
	store := newFakeSimulationPresetStore()
	store.presets = []dynamo.SimulationPresetItem{
		{PresetID: "z", Label: "Z", Watts: 100, CreatedAt: "2026-05-19T11:00:00Z"},
		{PresetID: "a", Label: "A", Watts: 200, CreatedAt: "2026-05-19T09:00:00Z"},
		{PresetID: "m", Label: "M", Watts: 300, CreatedAt: "2026-05-19T10:00:00Z"},
	}
	h := newPresetsTestHandler(store)

	req := makeRequest(http.MethodGet, "/simulation-presets", "Bearer "+testToken)
	resp, err := h.Handle(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var body struct {
		Presets []dynamo.SimulationPresetItem `json:"presets"`
	}
	require.NoError(t, json.Unmarshal([]byte(resp.Body), &body))
	require.Len(t, body.Presets, 3)
	assert.Equal(t, "a", body.Presets[0].PresetID)
	assert.Equal(t, "m", body.Presets[1].PresetID)
	assert.Equal(t, "z", body.Presets[2].PresetID)
}

func TestHandleUpdatePreset_BumpsUpdatedAtPreservesCreatedAt(t *testing.T) {
	store := newFakeSimulationPresetStore()
	store.presets = []dynamo.SimulationPresetItem{
		{PresetID: "p1", Label: "Old", Watts: 1000, CreatedAt: "2026-05-18T08:00:00Z", UpdatedAt: "2026-05-18T08:00:00Z"},
	}
	h := newPresetsTestHandler(store)

	req := makeRequest(http.MethodPut, "/simulation-presets/p1", "Bearer "+testToken)
	req.Body = presetBody("New", 2000)
	resp, err := h.Handle(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var item dynamo.SimulationPresetItem
	require.NoError(t, json.Unmarshal([]byte(resp.Body), &item))
	assert.Equal(t, "p1", item.PresetID)
	assert.Equal(t, "New", item.Label)
	assert.Equal(t, 2000, item.Watts)
	assert.Equal(t, "2026-05-18T08:00:00Z", item.CreatedAt, "createdAt preserved")
	assert.Equal(t, "2026-05-19T10:00:00Z", item.UpdatedAt, "updatedAt bumped to now")
}

func TestHandleUpdatePreset_UnknownIDReturns404(t *testing.T) {
	store := newFakeSimulationPresetStore()
	h := newPresetsTestHandler(store)

	req := makeRequest(http.MethodPut, "/simulation-presets/missing", "Bearer "+testToken)
	req.Body = presetBody("X", 1000)
	resp, err := h.Handle(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestHandleDeletePreset_Idempotent(t *testing.T) {
	store := newFakeSimulationPresetStore()
	store.presets = []dynamo.SimulationPresetItem{
		{PresetID: "p1", Label: "X", Watts: 1000, CreatedAt: "2026-05-18T08:00:00Z"},
	}
	h := newPresetsTestHandler(store)

	// First delete: 204 and removes the row.
	req := makeRequest(http.MethodDelete, "/simulation-presets/p1", "Bearer "+testToken)
	resp, err := h.Handle(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	assert.Empty(t, store.presets)

	// Second delete of the same id: still 204 (idempotent).
	resp, err = h.Handle(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
}

func TestHandlePresetValidation(t *testing.T) {
	cases := map[string]struct {
		label string
		watts int
	}{
		"empty label":      {label: "", watts: 1000},
		"label too long":   {label: strings.Repeat("x", 41), watts: 1000},
		"watts zero":       {label: "OK", watts: 0},
		"watts negative":   {label: "OK", watts: -5},
		"watts over 20000": {label: "OK", watts: 20001},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			store := newFakeSimulationPresetStore()
			h := newPresetsTestHandler(store)
			req := makeRequest(http.MethodPost, "/simulation-presets", "Bearer "+testToken)
			req.Body = presetBody(tc.label, tc.watts)
			resp, err := h.Handle(context.Background(), req)
			require.NoError(t, err)
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode, "%s must be 400", name)

			var body map[string]string
			require.NoError(t, json.Unmarshal([]byte(resp.Body), &body))
			assert.NotEmpty(t, body["error"], "400 carries a reason")

			// Nothing persisted on a rejected create.
			assert.Empty(t, store.presets)
		})
	}
}

func TestHandlePresetValidationBoundaryAccepted(t *testing.T) {
	cases := map[string]struct {
		label string
		watts int
	}{
		"label 1 char":  {label: "x", watts: 1},
		"label 40 char": {label: strings.Repeat("y", 40), watts: 20000},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			store := newFakeSimulationPresetStore()
			h := newPresetsTestHandler(store)
			req := makeRequest(http.MethodPost, "/simulation-presets", "Bearer "+testToken)
			req.Body = presetBody(tc.label, tc.watts)
			resp, err := h.Handle(context.Background(), req)
			require.NoError(t, err)
			assert.Equal(t, http.StatusCreated, resp.StatusCode, "%s must be accepted", name)
		})
	}
}

func TestHandleCreatePreset_CapReturns409(t *testing.T) {
	store := newFakeSimulationPresetStore()
	for i := 0; i < 20; i++ {
		store.presets = append(store.presets, dynamo.SimulationPresetItem{
			PresetID:  fmt.Sprintf("p%d", i),
			Label:     fmt.Sprintf("Preset %d", i),
			Watts:     1000,
			CreatedAt: "2026-05-19T10:00:00Z",
		})
	}
	h := newPresetsTestHandler(store)

	req := makeRequest(http.MethodPost, "/simulation-presets", "Bearer "+testToken)
	req.Body = presetBody("One too many", 1500)
	resp, err := h.Handle(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusConflict, resp.StatusCode, "21st preset must 409")
	assert.Len(t, store.presets, 20, "no preset added at the cap")
}
