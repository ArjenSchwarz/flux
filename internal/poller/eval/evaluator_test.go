package eval

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubCache is a deterministic RulesCache that returns a fixed snapshot.
// Tests mutate Devices between Evaluate calls to simulate a Lambda-driven
// change being picked up on the next cache refresh.
type stubCache struct {
	mu      sync.Mutex
	Devices []DeviceWithRules
}

func (s *stubCache) Snapshot(_ context.Context) ([]DeviceWithRules, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]DeviceWithRules, len(s.Devices))
	copy(out, s.Devices)
	return out, nil
}

// stubFireState captures calls to PutIfAbsent so tests can assert the
// idempotency contract (one Put per fire). exists[key] short-circuits to
// wrote=false on the second call, matching the production
// "attribute_not_exists" condition.
type stubFireState struct {
	mu     sync.Mutex
	puts   []SoCFireStateRecord
	exists map[string]bool
	err    error
}

func newStubFireState() *stubFireState {
	return &stubFireState{exists: make(map[string]bool)}
}

func (s *stubFireState) PutIfAbsent(_ context.Context, rec SoCFireStateRecord) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return false, s.err
	}
	key := rec.DeviceID + "#" + rec.RuleID + "|" + rec.WindowStartDate
	if s.exists[key] {
		return false, nil
	}
	s.exists[key] = true
	s.puts = append(s.puts, rec)
	return true, nil
}

// stubQueue records enqueued push jobs in order.
type stubQueue struct {
	mu   sync.Mutex
	jobs []PushJob
	err  error
}

func (s *stubQueue) Enqueue(_ context.Context, job PushJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	s.jobs = append(s.jobs, job)
	return nil
}

func sydneyLoc(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Australia/Sydney")
	require.NoError(t, err)
	return loc
}

// devicesOneRule returns a single-device snapshot with one enabled rule.
func devicesOneRule(deviceID, ruleID, tzName string, threshold int, start, end, updatedAt string) []DeviceWithRules {
	return []DeviceWithRules{{
		DeviceID:     deviceID,
		Platform:     "ios",
		APNsToken:    "token-" + deviceID,
		TZIdentifier: tzName,
		Rules: []RuleSnapshot{{
			RuleID:           ruleID,
			ThresholdPercent: threshold,
			WindowStart:      start,
			WindowEnd:        end,
			Enabled:          true,
			UpdatedAt:        updatedAt,
		}},
	}}
}

// readingAt returns a wall-clock time inside the window 17:00-19:00 in
// Sydney on the given date for the chosen hour.
func readingAt(t *testing.T, date string, hour, minute int) time.Time {
	t.Helper()
	loc := sydneyLoc(t)
	parsed, err := time.ParseInLocation("2006-01-02", date, loc)
	require.NoError(t, err)
	return time.Date(parsed.Year(), parsed.Month(), parsed.Day(), hour, minute, 0, 0, loc)
}

func TestEvaluator_SeedDoesNotFire(t *testing.T) {
	// AC 3.3: first in-window reading must only seed the comparator.
	cache := &stubCache{Devices: devicesOneRule("d1", "r1", "Australia/Sydney", 40, "17:00", "19:00", "u1")}
	fs := newStubFireState()
	q := &stubQueue{}
	ev := NewEvaluator(cache, fs, q)

	now := readingAt(t, "2026-06-15", 17, 30) // inside the window
	ev.Evaluate(context.Background(), 30.0, now)

	assert.Len(t, fs.puts, 0, "first in-window reading must not fire (seed only)")
	assert.Len(t, q.jobs, 0, "seed reading must not enqueue a push")
}

func TestEvaluator_AboveToAtOrBelowFiresOnce(t *testing.T) {
	cache := &stubCache{Devices: devicesOneRule("d1", "r1", "Australia/Sydney", 40, "17:00", "19:00", "u1")}
	fs := newStubFireState()
	q := &stubQueue{}
	ev := NewEvaluator(cache, fs, q)
	ctx := context.Background()

	// Seed at 50%.
	ev.Evaluate(ctx, 50.0, readingAt(t, "2026-06-15", 17, 10))
	require.Len(t, fs.puts, 0)

	// Drop to 38% — must fire once.
	ev.Evaluate(ctx, 38.0, readingAt(t, "2026-06-15", 17, 20))
	require.Len(t, fs.puts, 1)
	assert.Equal(t, "d1", fs.puts[0].DeviceID)
	assert.Equal(t, "r1", fs.puts[0].RuleID)
	assert.Equal(t, "2026-06-15", fs.puts[0].WindowStartDate)
	require.Len(t, q.jobs, 1, "Enqueue must be called after PutIfAbsent returns wrote=true")

	// Stay below — must not fire again.
	ev.Evaluate(ctx, 35.0, readingAt(t, "2026-06-15", 17, 30))
	assert.Len(t, fs.puts, 1, "at-or-below -> at-or-below must not re-fire")
	assert.Len(t, q.jobs, 1)
}

func TestEvaluator_OutOfWindowSkipsAndDoesNotAdvanceComparator(t *testing.T) {
	cache := &stubCache{Devices: devicesOneRule("d1", "r1", "Australia/Sydney", 40, "17:00", "19:00", "u1")}
	fs := newStubFireState()
	q := &stubQueue{}
	ev := NewEvaluator(cache, fs, q)
	ctx := context.Background()

	// 12:00 — outside the window — no seed, no fire.
	ev.Evaluate(ctx, 30.0, readingAt(t, "2026-06-15", 12, 0))
	assert.Len(t, fs.puts, 0)
	assert.Len(t, q.jobs, 0)

	// 17:30 — first in-window — seeds at 30 (which is already at-or-below).
	// AC 3.3 says seed-only on first in-window reading; the next at-or-below
	// reading still must not fire because the seed is already below threshold.
	ev.Evaluate(ctx, 30.0, readingAt(t, "2026-06-15", 17, 30))
	assert.Len(t, fs.puts, 0, "12:00 reading must not have advanced the comparator")
}

func TestEvaluator_StaleReadingSkipped(t *testing.T) {
	// AC 3.4: reading older than 60s skips evaluation.
	cache := &stubCache{Devices: devicesOneRule("d1", "r1", "Australia/Sydney", 40, "17:00", "19:00", "u1")}
	fs := newStubFireState()
	q := &stubQueue{}
	ev := NewEvaluator(cache, fs, q)
	ctx := context.Background()

	// Seed at 50.
	ev.Evaluate(ctx, 50.0, readingAt(t, "2026-06-15", 17, 10))
	require.Len(t, fs.puts, 0)

	// Pretend "now" is two minutes after the reading — readingAt(17:10) is
	// stale by 120s relative to the simulated current time.
	stale := readingAt(t, "2026-06-15", 17, 10)
	ev.now = func() time.Time { return stale.Add(2 * time.Minute) }
	ev.Evaluate(ctx, 30.0, stale)
	assert.Len(t, fs.puts, 0, "stale reading must skip evaluation entirely")
}

func TestEvaluator_OutOfRangeSocSkipped(t *testing.T) {
	cache := &stubCache{Devices: devicesOneRule("d1", "r1", "Australia/Sydney", 40, "17:00", "19:00", "u1")}
	fs := newStubFireState()
	q := &stubQueue{}
	ev := NewEvaluator(cache, fs, q)
	ctx := context.Background()

	ev.Evaluate(ctx, -5.0, readingAt(t, "2026-06-15", 17, 10))
	ev.Evaluate(ctx, 105.0, readingAt(t, "2026-06-15", 17, 20))
	assert.Len(t, fs.puts, 0)
}

func TestEvaluator_RuleUpdatedAtMismatchClearsComparator(t *testing.T) {
	// AC 5.3: rule edit clears prev so the new config re-seeds.
	cache := &stubCache{Devices: devicesOneRule("d1", "r1", "Australia/Sydney", 40, "17:00", "19:00", "u1")}
	fs := newStubFireState()
	q := &stubQueue{}
	ev := NewEvaluator(cache, fs, q)
	ctx := context.Background()

	// Seed at 50.
	ev.Evaluate(ctx, 50.0, readingAt(t, "2026-06-15", 17, 10))
	require.Len(t, fs.puts, 0)

	// Lambda mutated the rule; UpdatedAt is now u2. The very next reading
	// must seed (no fire), even if it's at or below threshold.
	cache.mu.Lock()
	cache.Devices[0].Rules[0].UpdatedAt = "u2"
	cache.mu.Unlock()

	ev.Evaluate(ctx, 30.0, readingAt(t, "2026-06-15", 17, 20))
	assert.Len(t, fs.puts, 0, "UpdatedAt bump must clear prev so the new config re-seeds")

	// The subsequent reading sees a populated prev (30) and must not fire
	// at 28 because the seed is already below.
	ev.Evaluate(ctx, 28.0, readingAt(t, "2026-06-15", 17, 30))
	assert.Len(t, fs.puts, 0, "post-seed reading still below threshold must not fire")
}

func TestEvaluator_YesterdayCarryOverIsolated(t *testing.T) {
	// AC 3.3 + Decision 16: yesterday's last in-window SoC must not
	// influence today's first in-window evaluation.
	cache := &stubCache{Devices: devicesOneRule("d1", "r1", "Australia/Sydney", 40, "17:00", "19:00", "u1")}
	fs := newStubFireState()
	q := &stubQueue{}
	ev := NewEvaluator(cache, fs, q)
	ctx := context.Background()

	// Yesterday: SoC sat at 50% inside the window.
	ev.Evaluate(ctx, 50.0, readingAt(t, "2026-06-15", 18, 50))

	// Today: first in-window reading is 30 — below threshold. With the
	// (deviceRule, windowStartDate) keyed prev map, today's first reading
	// must seed, NOT fire from yesterday's value.
	ev.Evaluate(ctx, 30.0, readingAt(t, "2026-06-16", 17, 10))
	assert.Len(t, fs.puts, 0, "yesterday's comparator must not poison today")
}

func TestEvaluator_MultipleRulesFireIndependently(t *testing.T) {
	cache := &stubCache{
		Devices: []DeviceWithRules{{
			DeviceID:     "d1",
			Platform:     "ios",
			APNsToken:    "token-d1",
			TZIdentifier: "Australia/Sydney",
			Rules: []RuleSnapshot{
				{RuleID: "r1", ThresholdPercent: 40, WindowStart: "17:00", WindowEnd: "19:00", Enabled: true, UpdatedAt: "u1"},
				{RuleID: "r2", ThresholdPercent: 35, WindowStart: "17:00", WindowEnd: "19:00", Enabled: true, UpdatedAt: "u1"},
			},
		}},
	}
	fs := newStubFireState()
	q := &stubQueue{}
	ev := NewEvaluator(cache, fs, q)
	ctx := context.Background()

	ev.Evaluate(ctx, 50.0, readingAt(t, "2026-06-15", 17, 10)) // seed both
	ev.Evaluate(ctx, 30.0, readingAt(t, "2026-06-15", 17, 20)) // crosses both

	require.Len(t, fs.puts, 2, "each rule must produce its own fire-state row")
	rules := map[string]bool{fs.puts[0].RuleID: true, fs.puts[1].RuleID: true}
	assert.True(t, rules["r1"])
	assert.True(t, rules["r2"])
	assert.Len(t, q.jobs, 2)
}

func TestEvaluator_DisabledRuleNotEvaluated(t *testing.T) {
	cache := &stubCache{Devices: devicesOneRule("d1", "r1", "Australia/Sydney", 40, "17:00", "19:00", "u1")}
	cache.Devices[0].Rules[0].Enabled = false
	fs := newStubFireState()
	q := &stubQueue{}
	ev := NewEvaluator(cache, fs, q)
	ctx := context.Background()

	ev.Evaluate(ctx, 50.0, readingAt(t, "2026-06-15", 17, 10))
	ev.Evaluate(ctx, 30.0, readingAt(t, "2026-06-15", 17, 20))
	assert.Len(t, fs.puts, 0, "disabled rules must not fire")
}

func TestEvaluator_StaleTokenSkipsPush(t *testing.T) {
	// A device whose TokenStatus is "stale" should still seed (so that
	// rules edited while stale are tracked correctly) but no push job is
	// produced — the next registration will recover.
	cache := &stubCache{Devices: devicesOneRule("d1", "r1", "Australia/Sydney", 40, "17:00", "19:00", "u1")}
	cache.Devices[0].TokenStatus = "stale"
	fs := newStubFireState()
	q := &stubQueue{}
	ev := NewEvaluator(cache, fs, q)
	ctx := context.Background()

	ev.Evaluate(ctx, 50.0, readingAt(t, "2026-06-15", 17, 10))
	ev.Evaluate(ctx, 30.0, readingAt(t, "2026-06-15", 17, 20))
	assert.Len(t, q.jobs, 0, "stale-token device must not be pushed to")
}

func TestEvaluator_InvalidTZSkipsDevice(t *testing.T) {
	cache := &stubCache{Devices: devicesOneRule("d1", "r1", "Not/A_Zone", 40, "17:00", "19:00", "u1")}
	fs := newStubFireState()
	q := &stubQueue{}
	ev := NewEvaluator(cache, fs, q)
	ctx := context.Background()

	ev.Evaluate(ctx, 30.0, readingAt(t, "2026-06-15", 17, 10))
	assert.Len(t, fs.puts, 0, "invalid TZ must skip the device, not panic")
}
