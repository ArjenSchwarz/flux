package eval

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

// readingMaxAge bounds how stale a reading may be before the evaluator skips
// the cycle (AC 3.4). 60s matches the AC's wording verbatim.
const readingMaxAge = 60 * time.Second

// evalTimeout bounds the per-cycle Evaluate call so a misbehaving cache or
// fire-state store cannot stall the live-poll goroutine indefinitely. The
// 3s budget is set in design.md §Per-cycle budget.
const evalTimeout = 3 * time.Second

// DeviceWithRules is the per-device unit returned by RulesCache.Snapshot.
type DeviceWithRules struct {
	DeviceID     string
	Platform     string
	APNsToken    string
	TZIdentifier string
	TokenStatus  string // "active" | "stale"
	Rules        []RuleSnapshot
}

// RuleSnapshot is the read-only rule view used by the evaluator. The cache
// flattens Dynamo rows into this shape so the evaluator never sees the
// dynamo package's types.
type RuleSnapshot struct {
	RuleID           string
	ThresholdPercent int
	WindowStart      string
	WindowEnd        string
	Enabled          bool
	Label            string
	UpdatedAt        string
	CreatedAt        string
}

// SoCFireStateRecord is the input to FireStateRW.PutIfAbsent. Keeping the
// struct internal to eval keeps the dynamo package from importing eval and
// vice-versa.
type SoCFireStateRecord struct {
	DeviceID        string
	RuleID          string
	WindowStartDate string
	ObservedSoc     float64
	APNsCollapseID  string
	FiredAt         time.Time
}

// PushJob is the input to PushQueue.Enqueue. Workers translate it to an
// APNs request; the evaluator itself does not call APNs.
type PushJob struct {
	DeviceID         string
	RuleID           string
	APNsToken        string
	APNsCollapseID   string
	ThresholdPercent int
	ObservedSoc      float64
	WindowEnd        string
	Label            string
}

// RulesCache returns a current snapshot of rules per device.
type RulesCache interface {
	Snapshot(ctx context.Context) ([]DeviceWithRules, error)
}

// FireStateRW writes the per-(device, rule, day) fire-state row. PutIfAbsent
// returns (true, nil) when newly written and (false, nil) when the row
// already exists.
type FireStateRW interface {
	PutIfAbsent(ctx context.Context, rec SoCFireStateRecord) (bool, error)
}

// PushQueue accepts push jobs. Non-blocking up to its internal capacity.
type PushQueue interface {
	Enqueue(ctx context.Context, job PushJob) error
}

// prevKey scopes the comparator by (deviceId#ruleId, windowStartDate). The
// windowStartDate component closes the "yesterday poisons today" gap
// (Decision 16).
type prevKey struct {
	deviceRule      string
	windowStartDate string
}

// prevValue stores the last in-window SoC together with the rule version it
// was observed under. UpdatedAt tag drives the cross-process reset
// (Decision 16): when the cache reports a new UpdatedAt, the comparator is
// dropped on next lookup and re-seeded.
type prevValue struct {
	soc           float64
	ruleUpdatedAt string
}

// Evaluator is the per-cycle threshold checker. Construct one per poller
// process; Evaluate is safe to call from the single live-poll goroutine.
type Evaluator struct {
	cache     RulesCache
	fireState FireStateRW
	queue     PushQueue
	now       func() time.Time
	mu        sync.Mutex
	prev      map[prevKey]prevValue
}

// NewEvaluator constructs an Evaluator wired to the given dependencies.
// The clock defaults to time.Now and can be overridden by tests via SetNow.
func NewEvaluator(cache RulesCache, fireState FireStateRW, queue PushQueue) *Evaluator {
	return &Evaluator{
		cache:     cache,
		fireState: fireState,
		queue:     queue,
		now:       time.Now,
		prev:      make(map[prevKey]prevValue),
	}
}

// SetNow overrides the evaluator's clock. Intended for tests; production
// code uses the default time.Now baked in at construction.
func (e *Evaluator) SetNow(now func() time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.now = now
}

// Evaluate runs one cycle: for every enabled rule on every device, compare
// the current SoC to the rule threshold and fire when the previous in-window
// reading was strictly above and the current reading is at or below.
//
// Errors are logged, not returned: the caller (poller live-data path) is
// not in a position to do anything with them. The whole call is bounded by
// evalTimeout so a misbehaving cache or fire-state store can never stall
// the live-poll goroutine.
func (e *Evaluator) Evaluate(ctx context.Context, soc float64, readingAt time.Time) {
	ctx, cancel := context.WithTimeout(ctx, evalTimeout)
	defer cancel()

	// AC 3.4: skip when the reading is unusable.
	if soc < 0 || soc > 100 {
		slog.Info("soc_alerts skipped: soc out of range", "soc", soc)
		return
	}
	if e.now().Sub(readingAt) > readingMaxAge {
		slog.Info("soc_alerts skipped: reading is stale",
			"reading_at", readingAt, "now", e.now())
		return
	}

	devices, err := e.cache.Snapshot(ctx)
	if err != nil {
		slog.Error("soc_alerts cache snapshot failed", "error", err)
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	for _, d := range devices {
		e.evaluateDevice(ctx, d, soc, readingAt)
	}
}

// evaluateDevice runs all enabled rules for one device. Caller holds e.mu.
func (e *Evaluator) evaluateDevice(ctx context.Context, d DeviceWithRules, soc float64, readingAt time.Time) {
	if len(d.Rules) == 0 {
		return
	}
	loc, err := time.LoadLocation(d.TZIdentifier)
	if err != nil {
		slog.Warn("soc_alerts tz invalid", "device_id", d.DeviceID, "tz", d.TZIdentifier, "error", err)
		return
	}
	for _, r := range d.Rules {
		if !r.Enabled {
			continue
		}
		inside, windowStart := inWindow(readingAt, r.WindowStart, r.WindowEnd, loc)
		if !inside {
			continue
		}
		key := prevKey{
			deviceRule:      d.DeviceID + "#" + r.RuleID,
			windowStartDate: windowStart,
		}
		val, hasPrev := e.prev[key]
		// AC 5.3: UpdatedAt mismatch drops the entry so the next reading
		// re-seeds under the new configuration.
		if hasPrev && val.ruleUpdatedAt != r.UpdatedAt {
			hasPrev = false
		}
		// AC 3.3: first in-window reading (or post-reset reading) only seeds.
		if !hasPrev {
			e.prev[key] = prevValue{soc: soc, ruleUpdatedAt: r.UpdatedAt}
			continue
		}
		// Downward-crossing semantics (Decision 6).
		threshold := float64(r.ThresholdPercent)
		if val.soc > threshold && soc <= threshold {
			e.maybeFire(ctx, d, r, soc, readingAt, windowStart)
		}
		e.prev[key] = prevValue{soc: soc, ruleUpdatedAt: r.UpdatedAt}
	}
}

// maybeFire writes the fire-state row and (on success) enqueues the push.
// Decision 9: write before enqueue so a crash post-write produces at most a
// silent miss rather than a duplicate.
func (e *Evaluator) maybeFire(ctx context.Context, d DeviceWithRules, r RuleSnapshot, soc float64, readingAt time.Time, windowStartDate string) {
	collapseID := CollapseID(d.DeviceID, r.RuleID, windowStartDate)
	rec := SoCFireStateRecord{
		DeviceID:        d.DeviceID,
		RuleID:          r.RuleID,
		WindowStartDate: windowStartDate,
		ObservedSoc:     soc,
		APNsCollapseID:  collapseID,
		FiredAt:         readingAt,
	}
	wrote, err := e.fireState.PutIfAbsent(ctx, rec)
	if err != nil {
		slog.Error("soc_alerts firestate write failed",
			"device_id", d.DeviceID, "rule_id", r.RuleID, "error", err)
		return
	}
	if !wrote {
		// Another writer (rolling deploy or replayed call) already recorded
		// today's fire.
		return
	}
	// AC 2.3 / 3.9: do not push to stale tokens. The device row is retained
	// so the next registration recovers state.
	if d.TokenStatus == "stale" || d.APNsToken == "" {
		slog.Info("soc_alerts fire suppressed: no usable token",
			"device_id", d.DeviceID, "token_status", d.TokenStatus)
		return
	}
	job := PushJob{
		DeviceID:         d.DeviceID,
		RuleID:           r.RuleID,
		APNsToken:        d.APNsToken,
		APNsCollapseID:   collapseID,
		ThresholdPercent: r.ThresholdPercent,
		ObservedSoc:      soc,
		WindowEnd:        r.WindowEnd,
		Label:            r.Label,
	}
	if err := e.queue.Enqueue(ctx, job); err != nil {
		if errors.Is(err, ErrQueueFull) {
			slog.Warn("soc_alerts push queue overflow; fire-state row retained",
				"device_id", d.DeviceID, "rule_id", r.RuleID)
			return
		}
		slog.Error("soc_alerts enqueue failed",
			"device_id", d.DeviceID, "rule_id", r.RuleID, "error", err)
	}
}

// ErrQueueFull is returned by PushQueue.Enqueue when the queue is at
// capacity. Defined here so the evaluator can recognise the overflow case
// without importing the apns package (which would create a cycle).
var ErrQueueFull = errors.New("push queue full")
