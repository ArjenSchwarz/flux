package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSocRuleStore implements the SocRuleStore interface in-memory so the
// CRUD handlers exercise the real validation + 10-rule cap path.
type fakeSocRuleStore struct {
	mu    sync.Mutex
	rules map[string][]dynamo.SoCRuleItem // device id -> rules (preserve insertion order)
	err   error
}

func newFakeSocRuleStore() *fakeSocRuleStore {
	return &fakeSocRuleStore{rules: make(map[string][]dynamo.SoCRuleItem)}
}

func (s *fakeSocRuleStore) ListRulesByDevice(_ context.Context, deviceID string) ([]dynamo.SoCRuleItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return nil, s.err
	}
	out := make([]dynamo.SoCRuleItem, len(s.rules[deviceID]))
	copy(out, s.rules[deviceID])
	return out, nil
}

func (s *fakeSocRuleStore) PutRule(_ context.Context, item dynamo.SoCRuleItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	// Replace by ruleId if present, otherwise append.
	rules := s.rules[item.DeviceID]
	for i, r := range rules {
		if r.RuleID == item.RuleID {
			rules[i] = item
			s.rules[item.DeviceID] = rules
			return nil
		}
	}
	s.rules[item.DeviceID] = append(rules, item)
	return nil
}

func (s *fakeSocRuleStore) DeleteRule(_ context.Context, deviceID, ruleID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	rules := s.rules[deviceID]
	for i, r := range rules {
		if r.RuleID == ruleID {
			s.rules[deviceID] = append(rules[:i], rules[i+1:]...)
			return nil
		}
	}
	return nil
}

// fakeFireStateCleaner records the deviceId/ruleId pairs that had their
// fire-state cleaned. The handler must always invoke this after a rule
// mutation, even when the deletion finds zero rows.
type fakeFireStateCleaner struct {
	mu    sync.Mutex
	calls []string
	err   error
}

func (f *fakeFireStateCleaner) DeleteFireStateByDeviceRule(_ context.Context, deviceID, ruleID string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return 0, f.err
	}
	f.calls = append(f.calls, deviceID+"|"+ruleID)
	return 0, nil
}

func newRulesTestHandler(rules *fakeSocRuleStore, cleaner *fakeFireStateCleaner) *Handler {
	h := newTestHandlerFor(&mockReader{}, nil, testSerial, testToken)
	h.rules = rules
	h.fireState = cleaner
	// Fix the clock and the UUID generator so tests are deterministic.
	fixedNow := time.Date(2026, 5, 19, 10, 0, 0, 0, time.UTC)
	h.nowFunc = func() time.Time { return fixedNow }
	counter := 0
	h.idFunc = func() string {
		counter++
		return fmt.Sprintf("rule-uuid-%d", counter)
	}
	// Rebuild the mux now that the rule handlers are wired.
	h.mux = h.buildMux()
	return h
}

func TestHandleListRules_SortByCreatedAtAsc(t *testing.T) {
	rules := newFakeSocRuleStore()
	// Insert with random createdAt timestamps; expect them sorted ascending.
	rules.rules["dev-1"] = []dynamo.SoCRuleItem{
		{DeviceID: "dev-1", RuleID: "z", ThresholdPercent: 10, WindowStart: "01:00", WindowEnd: "02:00", Enabled: true, CreatedAt: "2026-05-19T11:00:00Z"},
		{DeviceID: "dev-1", RuleID: "a", ThresholdPercent: 20, WindowStart: "02:00", WindowEnd: "03:00", Enabled: true, CreatedAt: "2026-05-19T09:00:00Z"},
		{DeviceID: "dev-1", RuleID: "m", ThresholdPercent: 30, WindowStart: "03:00", WindowEnd: "04:00", Enabled: true, CreatedAt: "2026-05-19T10:00:00Z"},
	}
	h := newRulesTestHandler(rules, &fakeFireStateCleaner{})

	req := makeRequest(http.MethodGet, "/devices/dev-1/rules", "Bearer "+testToken)
	resp, err := h.Handle(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var body struct {
		Rules []dynamo.SoCRuleItem `json:"rules"`
	}
	require.NoError(t, json.Unmarshal([]byte(resp.Body), &body))
	require.Len(t, body.Rules, 3)
	got := []string{body.Rules[0].RuleID, body.Rules[1].RuleID, body.Rules[2].RuleID}
	want := []string{"a", "m", "z"}
	sort.Strings(want)
	assert.Equal(t, want, got, "rules must come back sorted by createdAt ascending (AC 1.6)")
}

func TestHandleCreateRule_AssignsIDAndTimestamps(t *testing.T) {
	rules := newFakeSocRuleStore()
	cleaner := &fakeFireStateCleaner{}
	h := newRulesTestHandler(rules, cleaner)

	body := `{"thresholdPercent":40,"windowStart":"17:00","windowEnd":"00:00","enabled":true,"label":"Evening cooking"}`
	resp, err := h.Handle(context.Background(), makeDeviceJSONRequest(http.MethodPost, "/devices/dev-1/rules", body))
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var got dynamo.SoCRuleItem
	require.NoError(t, json.Unmarshal([]byte(resp.Body), &got))
	assert.Equal(t, "rule-uuid-1", got.RuleID)
	assert.Equal(t, "dev-1", got.DeviceID)
	assert.Equal(t, 40, got.ThresholdPercent)
	assert.Equal(t, "Evening cooking", got.Label)
	assert.NotEmpty(t, got.CreatedAt)
	assert.Equal(t, got.CreatedAt, got.UpdatedAt, "createdAt and updatedAt must match on creation")
}

func TestHandleCreateRule_ValidationParityWithAC1_3(t *testing.T) {
	rules := newFakeSocRuleStore()
	h := newRulesTestHandler(rules, &fakeFireStateCleaner{})

	bad := map[string]string{
		"threshold 0":             `{"thresholdPercent":0,"windowStart":"17:00","windowEnd":"18:00","enabled":true}`,
		"threshold 100":           `{"thresholdPercent":100,"windowStart":"17:00","windowEnd":"18:00","enabled":true}`,
		"threshold negative":      `{"thresholdPercent":-5,"windowStart":"17:00","windowEnd":"18:00","enabled":true}`,
		"start not HH:MM":         `{"thresholdPercent":40,"windowStart":"7pm","windowEnd":"18:00","enabled":true}`,
		"end not HH:MM":           `{"thresholdPercent":40,"windowStart":"17:00","windowEnd":"sundown","enabled":true}`,
		"start hour out of range": `{"thresholdPercent":40,"windowStart":"24:00","windowEnd":"18:00","enabled":true}`,
		"start == end":            `{"thresholdPercent":40,"windowStart":"17:00","windowEnd":"17:00","enabled":true}`,
		"label >40 chars":         `{"thresholdPercent":40,"windowStart":"17:00","windowEnd":"18:00","enabled":true,"label":"` + repeatA(41) + `"}`,
	}
	for name, body := range bad {
		t.Run(name, func(t *testing.T) {
			resp, err := h.Handle(context.Background(), makeDeviceJSONRequest(http.MethodPost, "/devices/dev-1/rules", body))
			require.NoError(t, err)
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		})
	}
}

func TestHandleCreateRule_Returns409OnEleventhRule(t *testing.T) {
	rules := newFakeSocRuleStore()
	for i := range 10 {
		rules.rules["dev-1"] = append(rules.rules["dev-1"], dynamo.SoCRuleItem{
			DeviceID:  "dev-1",
			RuleID:    fmt.Sprintf("rule-%d", i),
			CreatedAt: "2026-05-19T10:00:00Z",
		})
	}
	h := newRulesTestHandler(rules, &fakeFireStateCleaner{})

	body := `{"thresholdPercent":40,"windowStart":"17:00","windowEnd":"18:00","enabled":true}`
	resp, err := h.Handle(context.Background(), makeDeviceJSONRequest(http.MethodPost, "/devices/dev-1/rules", body))
	require.NoError(t, err)
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
	var body409 map[string]string
	require.NoError(t, json.Unmarshal([]byte(resp.Body), &body409))
	assert.Equal(t, "rule cap reached", body409["error"])
}

func TestHandleUpdateRule_CleansFireStateAndBumpsUpdatedAt(t *testing.T) {
	rules := newFakeSocRuleStore()
	rules.rules["dev-1"] = []dynamo.SoCRuleItem{{
		DeviceID:         "dev-1",
		RuleID:           "rule-abc",
		ThresholdPercent: 30,
		WindowStart:      "17:00",
		WindowEnd:        "19:00",
		Enabled:          true,
		CreatedAt:        "2026-05-19T08:00:00Z",
		UpdatedAt:        "2026-05-19T08:00:00Z",
	}}
	cleaner := &fakeFireStateCleaner{}
	h := newRulesTestHandler(rules, cleaner)

	body := `{"thresholdPercent":35,"windowStart":"17:00","windowEnd":"19:00","enabled":true}`
	resp, err := h.Handle(context.Background(), makeDeviceJSONRequest(http.MethodPut, "/devices/dev-1/rules/rule-abc", body))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var got dynamo.SoCRuleItem
	require.NoError(t, json.Unmarshal([]byte(resp.Body), &got))
	assert.Equal(t, "rule-abc", got.RuleID)
	assert.Equal(t, 35, got.ThresholdPercent)
	assert.NotEqual(t, "2026-05-19T08:00:00Z", got.UpdatedAt, "updatedAt must bump on every PUT")

	cleaner.mu.Lock()
	defer cleaner.mu.Unlock()
	assert.Equal(t, []string{"dev-1|rule-abc"}, cleaner.calls,
		"fire-state cleanup must run on PUT (AC 5.3)")
}

func TestHandleUpdateRule_EnabledToggleStillCleansFireState(t *testing.T) {
	rules := newFakeSocRuleStore()
	rules.rules["dev-1"] = []dynamo.SoCRuleItem{{
		DeviceID: "dev-1", RuleID: "r1", ThresholdPercent: 30,
		WindowStart: "17:00", WindowEnd: "19:00", Enabled: true,
		CreatedAt: "2026-05-19T08:00:00Z", UpdatedAt: "2026-05-19T08:00:00Z",
	}}
	cleaner := &fakeFireStateCleaner{}
	h := newRulesTestHandler(rules, cleaner)

	// Flip enabled to false; everything else identical.
	body := `{"thresholdPercent":30,"windowStart":"17:00","windowEnd":"19:00","enabled":false}`
	_, err := h.Handle(context.Background(), makeDeviceJSONRequest(http.MethodPut, "/devices/dev-1/rules/r1", body))
	require.NoError(t, err)
	cleaner.mu.Lock()
	defer cleaner.mu.Unlock()
	assert.Equal(t, []string{"dev-1|r1"}, cleaner.calls,
		"AC 5.3: re-enable / enable flip still resets fire-state")
}

func TestHandleUpdateRule_FireStateCleanupFailureDoesNotBlockSuccess(t *testing.T) {
	rules := newFakeSocRuleStore()
	rules.rules["dev-1"] = []dynamo.SoCRuleItem{{
		DeviceID: "dev-1", RuleID: "r1", ThresholdPercent: 30,
		WindowStart: "17:00", WindowEnd: "19:00", Enabled: true,
		CreatedAt: "2026-05-19T08:00:00Z", UpdatedAt: "2026-05-19T08:00:00Z",
	}}
	cleaner := &fakeFireStateCleaner{err: errors.New("dynamo down")}
	h := newRulesTestHandler(rules, cleaner)

	body := `{"thresholdPercent":35,"windowStart":"17:00","windowEnd":"19:00","enabled":true}`
	resp, err := h.Handle(context.Background(), makeDeviceJSONRequest(http.MethodPut, "/devices/dev-1/rules/r1", body))
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode,
		"PUT must still return success when cleanup fails (evaluator self-corrects)")
}

func TestHandleDeleteRule_CleansFireStateAndReturns204(t *testing.T) {
	rules := newFakeSocRuleStore()
	rules.rules["dev-1"] = []dynamo.SoCRuleItem{{
		DeviceID: "dev-1", RuleID: "r1", ThresholdPercent: 30,
		WindowStart: "17:00", WindowEnd: "19:00", Enabled: true,
		CreatedAt: "2026-05-19T08:00:00Z",
	}}
	cleaner := &fakeFireStateCleaner{}
	h := newRulesTestHandler(rules, cleaner)

	resp, err := h.Handle(context.Background(), makeRequest(http.MethodDelete, "/devices/dev-1/rules/r1", "Bearer "+testToken))
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)

	cleaner.mu.Lock()
	defer cleaner.mu.Unlock()
	assert.Equal(t, []string{"dev-1|r1"}, cleaner.calls, "fire-state cleanup must run on DELETE (AC 5.4)")

	// Rule actually gone.
	rs, _ := rules.ListRulesByDevice(context.Background(), "dev-1")
	assert.Empty(t, rs)
}

func TestHandleDeleteRule_IdempotentOnUnknownRule(t *testing.T) {
	rules := newFakeSocRuleStore()
	h := newRulesTestHandler(rules, &fakeFireStateCleaner{})
	resp, err := h.Handle(context.Background(), makeRequest(http.MethodDelete, "/devices/dev-1/rules/missing", "Bearer "+testToken))
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
}

func repeatA(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'a'
	}
	return string(b)
}
