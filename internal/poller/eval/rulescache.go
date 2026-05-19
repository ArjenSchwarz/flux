package eval

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"sync"
	"time"
)

// cacheRefreshInterval bounds how often the RulesCache re-reads from
// DynamoDB. Decision 16: 30s + 10s poll cadence yields a ≤40s worst-case
// rule-edit-to-evaluator latency, which is acceptable for this feature.
const cacheRefreshInterval = 30 * time.Second

// DeviceListReader returns every registered device. Implementations Scan
// flux-devices.
type DeviceListReader interface {
	ListDevices(ctx context.Context) ([]DeviceWithRules, error)
}

// PerDeviceRulesReader returns the rules for one device.
type PerDeviceRulesReader interface {
	ListRulesForDevice(ctx context.Context, deviceID string) ([]RuleSnapshot, error)
}

// MemoizingRulesCache implements RulesCache by snapshotting devices+rules
// from Dynamo on demand and re-using the snapshot for cacheRefreshInterval.
// Refreshes are serialised; concurrent Snapshot callers during a refresh
// share the prior value until the new one lands (no thundering herd).
type MemoizingRulesCache struct {
	devices   DeviceListReader
	rules     PerDeviceRulesReader
	now       func() time.Time
	interval  time.Duration
	mu        sync.Mutex
	snapshot  []DeviceWithRules
	fetchedAt time.Time
	loading   sync.Mutex
}

// NewMemoizingRulesCache constructs a cache with the default 30s refresh
// interval. Tests can swap `now` for deterministic time.
func NewMemoizingRulesCache(devices DeviceListReader, rules PerDeviceRulesReader) *MemoizingRulesCache {
	return &MemoizingRulesCache{
		devices:  devices,
		rules:    rules,
		now:      time.Now,
		interval: cacheRefreshInterval,
	}
}

// Snapshot returns the current rule snapshot, refreshing it if older than
// the cache interval. Returns a defensive copy so callers cannot mutate the
// shared state.
func (c *MemoizingRulesCache) Snapshot(ctx context.Context) ([]DeviceWithRules, error) {
	c.mu.Lock()
	fresh := c.snapshot != nil && c.now().Sub(c.fetchedAt) < c.interval
	c.mu.Unlock()
	if !fresh {
		if err := c.refresh(ctx); err != nil {
			// On refresh failure, return the prior snapshot if we have one
			// so a transient Dynamo blip doesn't halt evaluation.
			c.mu.Lock()
			prior := c.snapshot
			c.mu.Unlock()
			if prior != nil {
				slog.Warn("soc_alerts rules cache refresh failed; using prior snapshot", "error", err)
				return copyDevices(prior), nil
			}
			return nil, err
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return copyDevices(c.snapshot), nil
}

// refresh re-reads devices and rules under loading; only one refresh runs at
// a time.
func (c *MemoizingRulesCache) refresh(ctx context.Context) error {
	c.loading.Lock()
	defer c.loading.Unlock()

	// Re-check freshness — another goroutine may have refreshed while we
	// were waiting on loading.
	c.mu.Lock()
	if c.snapshot != nil && c.now().Sub(c.fetchedAt) < c.interval {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()

	devs, err := c.devices.ListDevices(ctx)
	if err != nil {
		return err
	}
	out := make([]DeviceWithRules, 0, len(devs))
	for _, d := range devs {
		rules, err := c.rules.ListRulesForDevice(ctx, d.DeviceID)
		if err != nil {
			return err
		}
		// AC 1.6: list view is creation-order; the cache hands the
		// evaluator the same order so observability events are stable.
		sort.SliceStable(rules, func(i, j int) bool {
			return rules[i].CreatedAt < rules[j].CreatedAt
		})
		d.Rules = rules
		out = append(out, d)
	}

	c.mu.Lock()
	c.snapshot = out
	c.fetchedAt = c.now()
	c.mu.Unlock()
	return nil
}

// copyDevices deep-copies the slice so callers cannot scribble on the
// cache's internal state.
func copyDevices(in []DeviceWithRules) []DeviceWithRules {
	out := make([]DeviceWithRules, len(in))
	for i, d := range in {
		rules := make([]RuleSnapshot, len(d.Rules))
		copy(rules, d.Rules)
		d.Rules = rules
		out[i] = d
	}
	return out
}

// ErrCacheUnavailable is returned when the cache has no prior snapshot and
// the underlying store is unreachable. Callers should skip the cycle and
// retry on the next one.
var ErrCacheUnavailable = errors.New("rules cache unavailable")
