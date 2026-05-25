package dynamo

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
)

// captureSlogDefault swaps the slog default for a buffered JSON logger. The
// restore func is returned for the test to defer. Matches the format the
// poller wires up in production (cmd/poller/logging.go).
func captureSlogDefault() (*bytes.Buffer, func()) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	old := slog.Default()
	slog.SetDefault(logger)
	return &buf, func() { slog.SetDefault(old) }
}

func TestLogOffpeakDrift_EmitsAllFiveDeltasAtInfo(t *testing.T) {
	buf, restore := captureSlogDefault()
	defer restore()

	item := OffpeakItem{
		Date:                "2026-05-18",
		StartEInput:         10.0,
		EndEInput:           28.95, // snapshotGrid = 18.95
		StartEpv:            0,
		EndEpv:              0, // snapshotSolar = 0
		StartECharge:        5.0,
		EndECharge:          18.0, // snapshotCharge = 13.0
		StartEDischarge:     2.0,
		EndEDischarge:       2.5, // snapshotDischarge = 0.5
		StartEOutput:        1.0,
		EndEOutput:          1.5,                    // snapshotExport = 0.5
		GridUsageKwh:        20.42,                  // drift = |20.42 - 18.95| = 1.47
		SolarKwh:            0.0,                    // drift = 0
		BatteryChargeKwh:    12.5,                   // drift = |12.5 - 13.0| = 0.5
		BatteryDischargeKwh: 0.6,                    // drift = 0.1
		GridExportKwh:       0.45,                   // drift = 0.05
		IntegratedAt:        "2026-05-18T14:00:30Z", // representative; not consumed
	}

	LogOffpeakDrift("2026-05-18", item)

	out := buf.String()
	// Exactly one log line.
	lines := bytes.Count(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	assert.Equal(t, 0, lines, "LogOffpeakDrift must emit exactly one log line")
	// CloudWatch Logs Insights handles both JSON ("driftGrid":1.47) and
	// Text (driftGrid=1.47) output. Assert the keys are present either way.
	for _, key := range []string{
		"date", "snapshotGrid", "integratedGrid", "driftGrid",
		"snapshotSolar", "integratedSolar", "driftSolar",
		"snapshotCharge", "integratedCharge", "driftCharge",
		"snapshotDischarge", "integratedDischarge", "driftDischarge",
		"snapshotExport", "integratedExport", "driftExport",
	} {
		assert.True(t, bytes.Contains([]byte(out), []byte(key)),
			"output must contain key %q. got: %s", key, out)
	}
	assert.Contains(t, out, "1.47", "driftGrid should be ~1.47. got: %s", out)
	assert.Contains(t, out, "2026-05-18")
	assert.Contains(t, out, "INFO", "must be emitted at INFO level (AC 6.2)")
}

func TestLogOffpeakDrift_StartSnapshotOnly_NoDrift(t *testing.T) {
	// Start snapshot non-zero, end snapshot non-zero (one tick over the
	// boundary so EndE* != 0) — exercises the standard drift line on a
	// near-zero-charge day.
	buf, restore := captureSlogDefault()
	defer restore()

	item := OffpeakItem{
		Date:         "2026-05-18",
		StartEInput:  3.0,
		EndEInput:    3.0,
		StartEpv:     0,
		EndEpv:       0.01, // nonzero so the recovery branch doesn't fire
		GridUsageKwh: 5.0,
		SolarKwh:     1.0,
	}
	LogOffpeakDrift("2026-05-18", item)
	out := buf.String()
	assert.Contains(t, out, "driftGrid")
	assert.Contains(t, out, "snapshotGrid")
	assert.Contains(t, out, "5")
}

// TestLogOffpeakDrift_NoSnapshotPair covers the positionAfter recovery path
// where handleEnd ran without a matching end snapshot (the row was finalised
// from readings only). Emits a dedicated log entry to avoid the misleading
// "snapshot = -startE*" line the standard branch would otherwise produce.
func TestLogOffpeakDrift_NoSnapshotPair(t *testing.T) {
	buf, restore := captureSlogDefault()
	defer restore()

	item := OffpeakItem{
		Date:                "2026-05-18",
		StartEInput:         10.0, // start captured but never paired with an end
		StartEpv:            0,
		StartECharge:        5.0,
		GridUsageKwh:        20.42,
		SolarKwh:            0.0,
		BatteryChargeKwh:    12.5,
		BatteryDischargeKwh: 0.6,
		GridExportKwh:       0.45,
		// EndE* all zero — recovery path.
	}
	LogOffpeakDrift("2026-05-18", item)

	out := buf.String()
	assert.Contains(t, out, "offpeak_drift_no_snapshot_pair")
	assert.Contains(t, out, "positionAfterRecovery")
	assert.NotContains(t, out, "driftGrid",
		"recovery branch must not emit the snapshot-diff drift fields")
	for _, key := range []string{
		"integratedGrid", "integratedSolar",
		"integratedCharge", "integratedDischarge", "integratedExport",
	} {
		assert.Contains(t, out, key)
	}
}
