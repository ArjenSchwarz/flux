package poller

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gcRecorder captures the cascade order and conditional-delete attempts so
// tests can assert on (a) cascade ordering: fire-state, then rules, then
// device; (b) the conditional-delete uses the scanned lastRegisteredAt.
type gcRecorder struct {
	mu sync.Mutex

	devices         []dynamo.DeviceItem
	rulesByDevice   map[string][]dynamo.SoCRuleItem
	fireStateByPair map[string][]dynamo.SoCFireStateItem // key deviceId|ruleId

	cascadeOrder []string // "firestate:dev|rule", "rule:dev|rule", "device:dev|scanned"

	// reregistered is the deviceId set whose row is overwritten between
	// Scan and Delete; the conditional delete must report
	// ConditionalCheckFailedException for those.
	reregistered map[string]string // deviceId -> newLastRegisteredAt
}

func newGCRecorder() *gcRecorder {
	return &gcRecorder{
		rulesByDevice:   make(map[string][]dynamo.SoCRuleItem),
		fireStateByPair: make(map[string][]dynamo.SoCFireStateItem),
		reregistered:    make(map[string]string),
	}
}

func pairKey(d, r string) string { return d + "|" + r }

func (g *gcRecorder) ListDevices(_ context.Context) ([]dynamo.DeviceItem, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]dynamo.DeviceItem, len(g.devices))
	copy(out, g.devices)
	return out, nil
}

func (g *gcRecorder) ListRulesByDevice(_ context.Context, deviceID string) ([]dynamo.SoCRuleItem, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]dynamo.SoCRuleItem(nil), g.rulesByDevice[deviceID]...), nil
}

func (g *gcRecorder) ListFireStateByDeviceRule(_ context.Context, deviceID, ruleID string) ([]dynamo.SoCFireStateItem, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]dynamo.SoCFireStateItem(nil), g.fireStateByPair[pairKey(deviceID, ruleID)]...), nil
}

func (g *gcRecorder) AnyFireStateNewerThan(_ context.Context, deviceID string, cutoff time.Time) (bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, rules := range g.rulesByDevice[deviceID] {
		for _, fs := range g.fireStateByPair[pairKey(deviceID, rules.RuleID)] {
			ft, err := time.Parse(time.RFC3339, fs.FiredAt)
			if err == nil && ft.After(cutoff) {
				return true, nil
			}
		}
	}
	return false, nil
}

func (g *gcRecorder) DeleteFireStateRow(_ context.Context, deviceRule, windowStartDate string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	// deviceRule format: "deviceId#ruleId"
	for i := 0; i < len(deviceRule); i++ {
		if deviceRule[i] == '#' {
			d, r := deviceRule[:i], deviceRule[i+1:]
			g.cascadeOrder = append(g.cascadeOrder, "firestate:"+d+"|"+r+"@"+windowStartDate)
			rows := g.fireStateByPair[pairKey(d, r)]
			for j, row := range rows {
				if row.WindowStartDate == windowStartDate {
					g.fireStateByPair[pairKey(d, r)] = append(rows[:j], rows[j+1:]...)
					break
				}
			}
			break
		}
	}
	return nil
}

func (g *gcRecorder) DeleteRule(_ context.Context, deviceID, ruleID string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.cascadeOrder = append(g.cascadeOrder, "rule:"+pairKey(deviceID, ruleID))
	rules := g.rulesByDevice[deviceID]
	for i, r := range rules {
		if r.RuleID == ruleID {
			g.rulesByDevice[deviceID] = append(rules[:i], rules[i+1:]...)
			break
		}
	}
	return nil
}

func (g *gcRecorder) DeleteDeviceConditional(_ context.Context, deviceID, scanned string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.cascadeOrder = append(g.cascadeOrder, "device:"+deviceID+"|scanned="+scanned)
	// Emulate the conditional check.
	if newer, ok := g.reregistered[deviceID]; ok && newer != scanned {
		return &types.ConditionalCheckFailedException{}
	}
	for i, d := range g.devices {
		if d.DeviceID == deviceID {
			g.devices = append(g.devices[:i], g.devices[i+1:]...)
			break
		}
	}
	return nil
}

func TestOrphanGC_OldDeviceCascadeDeletes(t *testing.T) {
	rec := newGCRecorder()
	cutoff := time.Date(2026, 4, 19, 0, 0, 0, 0, time.UTC)
	old := cutoff.Add(-1 * time.Hour).Format(time.RFC3339) // 31+ days old

	rec.devices = []dynamo.DeviceItem{{
		DeviceID:         "device-old",
		LastRegisteredAt: old,
	}}
	rec.rulesByDevice["device-old"] = []dynamo.SoCRuleItem{
		{DeviceID: "device-old", RuleID: "rule-A"},
		{DeviceID: "device-old", RuleID: "rule-B"},
	}
	rec.fireStateByPair[pairKey("device-old", "rule-A")] = []dynamo.SoCFireStateItem{
		{DeviceRule: "device-old#rule-A", WindowStartDate: "2026-04-18"},
	}

	gc := NewOrphanDeviceGC(rec, 30*24*time.Hour, 24*time.Hour, func() time.Time {
		return cutoff.Add(31 * 24 * time.Hour)
	})
	gc.Run(context.Background())

	// Cascade order: fire-state first, then rules, then device row.
	// (Per-rule fire-state is interleaved but the device must be last.)
	require.NotEmpty(t, rec.cascadeOrder)
	last := rec.cascadeOrder[len(rec.cascadeOrder)-1]
	assert.Contains(t, last, "device:device-old", "device row must be deleted last so a crash leaves it visible to the next pass")
	// At least one fire-state delete should appear before any rule delete.
	firstRuleIdx := -1
	firstFireIdx := -1
	for i, e := range rec.cascadeOrder {
		if firstFireIdx == -1 && (len(e) > 9 && e[:9] == "firestate") {
			firstFireIdx = i
		}
		if firstRuleIdx == -1 && (len(e) > 4 && e[:4] == "rule") {
			firstRuleIdx = i
		}
	}
	if firstFireIdx != -1 && firstRuleIdx != -1 {
		assert.Less(t, firstFireIdx, firstRuleIdx,
			"fire-state must be deleted before rules so a crash leaves orphan rules visible to next pass")
	}
	// Row removed.
	assert.Empty(t, rec.devices)
}

func TestOrphanGC_FreshDeviceSkipped(t *testing.T) {
	rec := newGCRecorder()
	now := time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC)
	fresh := now.Add(-1 * 24 * time.Hour).Format(time.RFC3339)
	rec.devices = []dynamo.DeviceItem{{DeviceID: "device-fresh", LastRegisteredAt: fresh}}

	gc := NewOrphanDeviceGC(rec, 30*24*time.Hour, 24*time.Hour, func() time.Time { return now })
	gc.Run(context.Background())

	assert.Empty(t, rec.cascadeOrder, "devices newer than 30d must not be cascaded")
	assert.Len(t, rec.devices, 1)
}

func TestOrphanGC_RecentFireStateSkipsDevice(t *testing.T) {
	// AC 4.6 protection: a device with a fire-state row newer than 24h is
	// in-flight; GC must skip it.
	rec := newGCRecorder()
	now := time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC)
	old := now.Add(-31 * 24 * time.Hour).Format(time.RFC3339)
	rec.devices = []dynamo.DeviceItem{{DeviceID: "device-recent-fs", LastRegisteredAt: old}}
	rec.rulesByDevice["device-recent-fs"] = []dynamo.SoCRuleItem{
		{DeviceID: "device-recent-fs", RuleID: "rule-A"},
	}
	rec.fireStateByPair[pairKey("device-recent-fs", "rule-A")] = []dynamo.SoCFireStateItem{
		{DeviceRule: "device-recent-fs#rule-A", FiredAt: now.Add(-1 * time.Hour).Format(time.RFC3339)},
	}

	gc := NewOrphanDeviceGC(rec, 30*24*time.Hour, 24*time.Hour, func() time.Time { return now })
	gc.Run(context.Background())
	assert.Len(t, rec.devices, 1, "device with recent fire-state must be preserved this pass")
}

func TestOrphanGC_ConditionalDeleteHonoursReregistration(t *testing.T) {
	rec := newGCRecorder()
	now := time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC)
	scanned := now.Add(-31 * 24 * time.Hour).Format(time.RFC3339)
	rec.devices = []dynamo.DeviceItem{{DeviceID: "device-race", LastRegisteredAt: scanned}}
	// Between Scan and Delete the device re-registers — current stored
	// lastRegisteredAt is newer than what was scanned, so the conditional
	// delete must fail and we log flux_orphan_gc_skipped_reregistered.
	rec.reregistered["device-race"] = now.Add(-1 * time.Hour).Format(time.RFC3339)

	gc := NewOrphanDeviceGC(rec, 30*24*time.Hour, 24*time.Hour, func() time.Time { return now })
	gc.Run(context.Background())
	// Row preserved.
	assert.Len(t, rec.devices, 1)
}

func TestOrphanGC_ListDevicesErrorLogged(t *testing.T) {
	rec := &errCascadeRecorder{err: errors.New("scan failed")}
	gc := NewOrphanDeviceGC(rec, 30*24*time.Hour, 24*time.Hour, time.Now)
	// Should not panic.
	gc.Run(context.Background())
}

type errCascadeRecorder struct {
	gcRecorder
	err error
}

func (e *errCascadeRecorder) ListDevices(_ context.Context) ([]dynamo.DeviceItem, error) {
	return nil, e.err
}
