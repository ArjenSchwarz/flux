package integration

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ArjenSchwarz/flux/internal/poller/eval"
)

// fakeRulesCache returns whatever Devices is set to. Tests mutate it
// between Evaluate calls to simulate a rule edit / re-enable.
type fakeRulesCache struct {
	mu      sync.Mutex
	Devices []eval.DeviceWithRules
}

func (f *fakeRulesCache) Snapshot(_ context.Context) ([]eval.DeviceWithRules, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]eval.DeviceWithRules, len(f.Devices))
	copy(out, f.Devices)
	return out, nil
}

// fakeFireStateRW emulates the dynamo conditional put: the first PutIfAbsent
// for a given (device, rule, day) returns wrote=true; subsequent ones
// return wrote=false. Cleared on rule edit via the Reset method exposed for
// the integration test, which simulates the Lambda's Query+Delete pass.
type fakeFireStateRW struct {
	mu         sync.Mutex
	rows       map[string]eval.SoCFireStateRecord
	puts       atomic.Int32
	suppressed atomic.Int32
}

func newFakeFireStateRW() *fakeFireStateRW {
	return &fakeFireStateRW{rows: make(map[string]eval.SoCFireStateRecord)}
}

func fsKey(deviceID, ruleID, day string) string {
	return deviceID + "#" + ruleID + "|" + day
}

func (f *fakeFireStateRW) PutIfAbsent(_ context.Context, rec eval.SoCFireStateRecord) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.puts.Add(1)
	key := fsKey(rec.DeviceID, rec.RuleID, rec.WindowStartDate)
	if _, ok := f.rows[key]; ok {
		f.suppressed.Add(1)
		return false, nil
	}
	f.rows[key] = rec
	return true, nil
}

// Reset emulates the Lambda's fire-state cleanup on rule edit/delete
// (AC 5.3 / 5.4) — every row for the given (device, rule) is wiped.
func (f *fakeFireStateRW) Reset(deviceID, ruleID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	prefix := deviceID + "#" + ruleID + "|"
	for k := range f.rows {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			delete(f.rows, k)
		}
	}
}

// fakeQueue records every Enqueue call.
type fakeQueue struct {
	mu   sync.Mutex
	jobs []eval.PushJob
}

func (q *fakeQueue) Enqueue(_ context.Context, job eval.PushJob) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.jobs = append(q.jobs, job)
	return nil
}

func (q *fakeQueue) JobCount() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.jobs)
}

func sydney(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Australia/Sydney")
	require.NoError(t, err)
	return loc
}

// minuteSequence returns timestamps every poll cycle (default 10s)
// between start and end, inclusive of start, exclusive of end.
func minuteSequence(start, end time.Time, interval time.Duration) []time.Time {
	var out []time.Time
	for t := start; t.Before(end); t = t.Add(interval) {
		out = append(out, t)
	}
	return out
}

// drive sweeps the evaluator with `soc` at every tick. Each evaluator call
// uses the per-tick reading as both the SoC value and the simulated "now"
// for the staleness check.
func drive(t *testing.T, ev *eval.Evaluator, ticks []time.Time, socFn func(t time.Time) float64) {
	t.Helper()
	ctx := context.Background()
	for _, ts := range ticks {
		ev.SetNow(func() time.Time { return ts })
		ev.Evaluate(ctx, socFn(ts), ts)
	}
}

func TestIntegration_SocAlerts_NormalDayFiresOnce(t *testing.T) {
	loc := sydney(t)
	cache := &fakeRulesCache{
		Devices: []eval.DeviceWithRules{{
			DeviceID:     "device-1",
			Platform:     "ios",
			APNsToken:    "token-1",
			TZIdentifier: "Australia/Sydney",
			TokenStatus:  "active",
			Rules: []eval.RuleSnapshot{{
				RuleID: "rule-evening", ThresholdPercent: 40,
				WindowStart: "17:00", WindowEnd: "00:00", Enabled: true,
				UpdatedAt: "v1",
			}},
		}},
	}
	fs := newFakeFireStateRW()
	q := &fakeQueue{}
	ev := eval.NewEvaluator(cache, fs, q)

	// Simulate the day: SoC drops from 60 to 35 between 18:00 and 19:00,
	// then oscillates around 30. Expect exactly one fire.
	day := time.Date(2026, 6, 15, 17, 0, 0, 0, loc)
	end := day.Add(7 * time.Hour) // through midnight
	ticks := minuteSequence(day, end, 10*time.Second)
	drive(t, ev, ticks, func(ts time.Time) float64 {
		hr := ts.Sub(day).Minutes()
		switch {
		case hr < 30:
			return 60.0
		case hr < 60:
			return 50.0
		case hr < 90:
			return 38.0 // crosses the 40% threshold downward
		default:
			return 32.0 + float64(ts.Unix()%3) // bounce 32..34
		}
	})

	assert.Equal(t, 1, q.JobCount(), "exactly one push per (device, rule, day)")
	assert.Equal(t, "rule-evening", q.jobs[0].RuleID)
}

func TestIntegration_SocAlerts_PollerRestartMidWindowDoesNotFireOnFirstReading(t *testing.T) {
	// AC 3.3: when the poller restarts, the first in-window reading must
	// only seed the comparator, even if already below threshold.
	loc := sydney(t)
	cache := &fakeRulesCache{
		Devices: []eval.DeviceWithRules{{
			DeviceID: "device-1", APNsToken: "t", TokenStatus: "active",
			TZIdentifier: "Australia/Sydney",
			Rules: []eval.RuleSnapshot{{
				RuleID: "r", ThresholdPercent: 40,
				WindowStart: "17:00", WindowEnd: "19:00", Enabled: true,
				UpdatedAt: "v1",
			}},
		}},
	}
	fs := newFakeFireStateRW()
	q := &fakeQueue{}
	ev := eval.NewEvaluator(cache, fs, q)

	// Simulate a fresh evaluator at 18:30 with SoC already at 30 (below
	// threshold). No fire — only a seed.
	ts := time.Date(2026, 6, 15, 18, 30, 0, 0, loc)
	ev.SetNow(func() time.Time { return ts })
	ev.Evaluate(context.Background(), 30.0, ts)
	require.Equal(t, 0, q.JobCount(), "fresh poller must not fire on first in-window reading")

	// One more reading still inside window at 35 (above 30 prev) — also
	// must not fire (rising); and at 28 - this is below threshold but prev
	// (30) was already below, so no downward-crossing trigger.
	for _, val := range []float64{35.0, 28.0} {
		ts = ts.Add(10 * time.Second)
		ev.SetNow(func() time.Time { return ts })
		ev.Evaluate(context.Background(), val, ts)
	}
	assert.Equal(t, 0, q.JobCount(),
		"poller restart mid-window may lose at most one fire per AC 3.3 trade-off")
}

func TestIntegration_SocAlerts_MidDayRuleEditClearsAndRefires(t *testing.T) {
	// AC 5.3: edit the threshold mid-window; the next downward crossing
	// under the new configuration must fire once.
	loc := sydney(t)
	cache := &fakeRulesCache{
		Devices: []eval.DeviceWithRules{{
			DeviceID: "device-1", APNsToken: "t", TokenStatus: "active",
			TZIdentifier: "Australia/Sydney",
			Rules: []eval.RuleSnapshot{{
				RuleID: "r", ThresholdPercent: 40,
				WindowStart: "17:00", WindowEnd: "19:00", Enabled: true,
				UpdatedAt: "v1",
			}},
		}},
	}
	fs := newFakeFireStateRW()
	q := &fakeQueue{}
	ev := eval.NewEvaluator(cache, fs, q)

	day := time.Date(2026, 6, 15, 17, 0, 0, 0, loc)
	// 17:10 — seed at 60.
	t1 := day.Add(10 * time.Minute)
	ev.SetNow(func() time.Time { return t1 })
	ev.Evaluate(context.Background(), 60.0, t1)

	// 17:20 — drop to 38: fires once.
	t2 := day.Add(20 * time.Minute)
	ev.SetNow(func() time.Time { return t2 })
	ev.Evaluate(context.Background(), 38.0, t2)
	require.Equal(t, 1, q.JobCount())

	// User edits the rule: threshold raised to 50 and UpdatedAt bumps.
	// The Lambda also wipes the fire-state row (simulated via Reset).
	cache.mu.Lock()
	cache.Devices[0].Rules[0].ThresholdPercent = 50
	cache.Devices[0].Rules[0].UpdatedAt = "v2"
	cache.mu.Unlock()
	fs.Reset("device-1", "r")

	// 17:30 — SoC at 48. Comparator was reset by UpdatedAt mismatch,
	// so this reading only seeds even though it's below the new threshold.
	t3 := day.Add(30 * time.Minute)
	ev.SetNow(func() time.Time { return t3 })
	ev.Evaluate(context.Background(), 48.0, t3)
	require.Equal(t, 1, q.JobCount(), "post-edit seed reading must not fire (AC 3.3)")

	// 17:40 — rise to 55 (above new threshold).
	t4 := day.Add(40 * time.Minute)
	ev.SetNow(func() time.Time { return t4 })
	ev.Evaluate(context.Background(), 55.0, t4)

	// 17:50 — drop to 45: fires once under new config.
	t5 := day.Add(50 * time.Minute)
	ev.SetNow(func() time.Time { return t5 })
	ev.Evaluate(context.Background(), 45.0, t5)
	assert.Equal(t, 2, q.JobCount(),
		"after rule edit + Lambda cleanup, next downward crossing must fire once")
}

func TestIntegration_SocAlerts_CrossMidnightFiresOnce(t *testing.T) {
	// AC 3.6: a 22:00-06:00 rule fires at most once per window-start day
	// even when crossing local midnight.
	loc := sydney(t)
	cache := &fakeRulesCache{
		Devices: []eval.DeviceWithRules{{
			DeviceID: "device-1", APNsToken: "t", TokenStatus: "active",
			TZIdentifier: "Australia/Sydney",
			Rules: []eval.RuleSnapshot{{
				RuleID: "r-overnight", ThresholdPercent: 30,
				WindowStart: "22:00", WindowEnd: "06:00", Enabled: true,
				UpdatedAt: "v1",
			}},
		}},
	}
	fs := newFakeFireStateRW()
	q := &fakeQueue{}
	ev := eval.NewEvaluator(cache, fs, q)

	// Open the window at 22:00 with 50% SoC (seed).
	openAt := time.Date(2026, 6, 15, 22, 0, 0, 0, loc)
	ev.SetNow(func() time.Time { return openAt })
	ev.Evaluate(context.Background(), 50.0, openAt)
	require.Equal(t, 0, q.JobCount())

	// Cross-midnight: 02:00 next day, SoC 20% — fire.
	preDawn := time.Date(2026, 6, 16, 2, 0, 0, 0, loc)
	ev.SetNow(func() time.Time { return preDawn })
	ev.Evaluate(context.Background(), 20.0, preDawn)
	require.Equal(t, 1, q.JobCount())

	// Stay low at 03:00 — must not re-fire (same window-start day).
	threeAM := time.Date(2026, 6, 16, 3, 0, 0, 0, loc)
	ev.SetNow(func() time.Time { return threeAM })
	ev.Evaluate(context.Background(), 18.0, threeAM)
	assert.Equal(t, 1, q.JobCount(),
		"second crossing within the same window-start day must be collapsed by fire-state")

	// Cross midnight again into the *next* opening day (22:00 on 16th):
	// the window-start day changes from 2026-06-15 to 2026-06-16, so
	// a fresh comparator + fresh fire-state apply.
	nextNight := time.Date(2026, 6, 16, 22, 30, 0, 0, loc)
	ev.SetNow(func() time.Time { return nextNight })
	ev.Evaluate(context.Background(), 50.0, nextNight) // seed for new day

	nextNightDown := time.Date(2026, 6, 16, 23, 30, 0, 0, loc)
	ev.SetNow(func() time.Time { return nextNightDown })
	ev.Evaluate(context.Background(), 20.0, nextNightDown)
	assert.Equal(t, 2, q.JobCount(),
		"the next opening day must re-arm and produce its own fire")
}
