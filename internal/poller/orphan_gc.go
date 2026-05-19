package poller

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// OrphanDeviceGCBackend is the subset of dynamo behaviour the GC needs.
// Keeping the interface narrow lets the test double a single struct with
// all the required methods without pulling in the real Dynamo client.
type OrphanDeviceGCBackend interface {
	ListDevices(ctx context.Context) ([]dynamo.DeviceItem, error)
	ListRulesByDevice(ctx context.Context, deviceID string) ([]dynamo.SoCRuleItem, error)
	ListFireStateByDeviceRule(ctx context.Context, deviceID, ruleID string) ([]dynamo.SoCFireStateItem, error)
	AnyFireStateNewerThan(ctx context.Context, deviceID string, cutoff time.Time) (bool, error)
	DeleteFireStateRow(ctx context.Context, deviceRule, windowStartDate string) error
	DeleteRule(ctx context.Context, deviceID, ruleID string) error
	DeleteDeviceConditional(ctx context.Context, deviceID, scannedLastRegisteredAt string) error
}

// OrphanDeviceGC runs the per-night garbage collection that removes device
// rows whose lastRegisteredAt is older than orphanCutoff (default 30 days).
// Cascade order: fire-state -> rules -> device, so a crash leaves the device
// row visible to the next pass.
type OrphanDeviceGC struct {
	backend         OrphanDeviceGCBackend
	orphanCutoff    time.Duration
	inFlightProtect time.Duration
	now             func() time.Time
}

// NewOrphanDeviceGC constructs a GC with the given cutoffs.
//   - orphanCutoff: lastRegisteredAt older than (now - cutoff) is eligible.
//   - inFlightProtect: device skipped this pass if any fire-state row is
//     newer than (now - protect). Protects pushes still being delivered.
func NewOrphanDeviceGC(backend OrphanDeviceGCBackend, orphanCutoff, inFlightProtect time.Duration, now func() time.Time) *OrphanDeviceGC {
	return &OrphanDeviceGC{
		backend:         backend,
		orphanCutoff:    orphanCutoff,
		inFlightProtect: inFlightProtect,
		now:             now,
	}
}

// Run executes one GC pass. Errors are logged, never returned: the caller
// (runMidnightFinalizer) cannot do anything useful with them.
func (g *OrphanDeviceGC) Run(ctx context.Context) {
	now := g.now()
	cutoff := now.Add(-g.orphanCutoff)
	inFlightFloor := now.Add(-g.inFlightProtect)

	devices, err := g.backend.ListDevices(ctx)
	if err != nil {
		slog.Error("flux_orphan_gc_scan_failed", "error", err)
		return
	}
	scanned := 0
	deleted := 0
	skippedRecent := 0
	skippedReregistered := 0
	for _, d := range devices {
		scanned++
		ft, err := time.Parse(time.RFC3339, d.LastRegisteredAt)
		if err != nil {
			slog.Warn("flux_orphan_gc_invalid_lastRegisteredAt",
				"device_id", d.DeviceID, "value", d.LastRegisteredAt)
			continue
		}
		if ft.After(cutoff) {
			// Fresh device — keep.
			continue
		}
		// In-flight guard.
		hasRecent, err := g.backend.AnyFireStateNewerThan(ctx, d.DeviceID, inFlightFloor)
		if err != nil {
			slog.Warn("flux_orphan_gc_firestate_lookup_failed",
				"device_id", d.DeviceID, "error", err)
			continue
		}
		if hasRecent {
			skippedRecent++
			slog.Info("flux_orphan_gc_skipped_recent_firestate", "device_id", d.DeviceID)
			continue
		}
		if g.cascadeDelete(ctx, d) {
			deleted++
		} else {
			skippedReregistered++
		}
	}
	slog.Info("flux_orphan_gc_scanned", "count", scanned)
	slog.Info("flux_orphan_gc_deleted", "count", deleted)
	if skippedRecent > 0 {
		slog.Info("flux_orphan_gc_skipped_recent_firestate_total", "count", skippedRecent)
	}
	if skippedReregistered > 0 {
		slog.Info("flux_orphan_gc_skipped_reregistered", "count", skippedReregistered)
	}
}

// cascadeDelete removes fire-state rows, then rules, then the device row
// (with the conditional delete that protects against re-registration).
// Returns true if the device row was deleted.
func (g *OrphanDeviceGC) cascadeDelete(ctx context.Context, d dynamo.DeviceItem) bool {
	rules, err := g.backend.ListRulesByDevice(ctx, d.DeviceID)
	if err != nil {
		slog.Warn("flux_orphan_gc_list_rules_failed",
			"device_id", d.DeviceID, "error", err)
		return false
	}
	for _, r := range rules {
		rows, err := g.backend.ListFireStateByDeviceRule(ctx, d.DeviceID, r.RuleID)
		if err != nil {
			slog.Warn("flux_orphan_gc_list_firestate_failed",
				"device_id", d.DeviceID, "rule_id", r.RuleID, "error", err)
			continue
		}
		for _, fs := range rows {
			if err := g.backend.DeleteFireStateRow(ctx, fs.DeviceRule, fs.WindowStartDate); err != nil {
				slog.Warn("flux_orphan_gc_delete_firestate_failed",
					"device_rule", fs.DeviceRule, "error", err)
			}
		}
	}
	for _, r := range rules {
		if err := g.backend.DeleteRule(ctx, d.DeviceID, r.RuleID); err != nil {
			slog.Warn("flux_orphan_gc_delete_rule_failed",
				"device_id", d.DeviceID, "rule_id", r.RuleID, "error", err)
		}
	}
	if err := g.backend.DeleteDeviceConditional(ctx, d.DeviceID, d.LastRegisteredAt); err != nil {
		var ccf *types.ConditionalCheckFailedException
		if errors.As(err, &ccf) {
			slog.Info("flux_orphan_gc_skipped_reregistered", "device_id", d.DeviceID)
			return false
		}
		slog.Warn("flux_orphan_gc_delete_device_failed",
			"device_id", d.DeviceID, "error", err)
		return false
	}
	return true
}
