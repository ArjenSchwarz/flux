package dynamo

import (
	"log/slog"
	"math"
)

// LogOffpeakDrift emits a structured INFO log entry comparing the snapshot-
// diff value (endE* − startE*) with the readings-integrated value for each
// of the five deltas, plus the absolute difference (specs/offpeak-from-readings
// AC 6.1 / 6.2).
//
// Lives in the dynamo package so both the poller (handleEnd) and the backfill
// CLI can call it without introducing a poller → CLI import; OffpeakItem is
// already defined here.
//
// The emission goes through slog.Info — production wires up slog.JSONHandler
// (cmd/poller/logging.go), CLIs use slog.TextHandler. Both formats are parseable
// by CloudWatch Logs Insights via the `fields date, driftGrid, …` syntax.
// No metric/alarm is emitted; alerting is deferred per Decision 6.
//
// When the row was finalised via positionAfter recovery the end snapshot is
// missing (handleStart ran but handleEnd never recorded the AlphaESS end
// values before the restart), so EndE* are all zero. In that case the
// snapshot-diff numbers would collapse to -startE* which is misleading, so a
// dedicated log line is emitted with the integrated values only.
func LogOffpeakDrift(date string, item OffpeakItem) {
	if item.EndEInput == 0 && item.EndEpv == 0 && item.EndECharge == 0 &&
		item.EndEDischarge == 0 && item.EndEOutput == 0 {
		slog.Info("offpeak_drift_no_snapshot_pair",
			"date", date,
			"reason", "positionAfterRecovery",
			"integratedGrid", item.GridUsageKwh,
			"integratedSolar", item.SolarKwh,
			"integratedCharge", item.BatteryChargeKwh,
			"integratedDischarge", item.BatteryDischargeKwh,
			"integratedExport", item.GridExportKwh,
		)
		return
	}

	snapGrid := item.EndEInput - item.StartEInput
	snapSolar := item.EndEpv - item.StartEpv
	snapCharge := item.EndECharge - item.StartECharge
	snapDischarge := item.EndEDischarge - item.StartEDischarge
	snapExport := item.EndEOutput - item.StartEOutput

	slog.Info("offpeak drift",
		"date", date,
		"snapshotGrid", snapGrid,
		"integratedGrid", item.GridUsageKwh,
		"driftGrid", math.Abs(item.GridUsageKwh-snapGrid),
		"snapshotSolar", snapSolar,
		"integratedSolar", item.SolarKwh,
		"driftSolar", math.Abs(item.SolarKwh-snapSolar),
		"snapshotCharge", snapCharge,
		"integratedCharge", item.BatteryChargeKwh,
		"driftCharge", math.Abs(item.BatteryChargeKwh-snapCharge),
		"snapshotDischarge", snapDischarge,
		"integratedDischarge", item.BatteryDischargeKwh,
		"driftDischarge", math.Abs(item.BatteryDischargeKwh-snapDischarge),
		"snapshotExport", snapExport,
		"integratedExport", item.GridExportKwh,
		"driftExport", math.Abs(item.GridExportKwh-snapExport),
	)
}
