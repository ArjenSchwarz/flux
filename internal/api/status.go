package api

import (
	"context"
	"log/slog"
	"time"

	"github.com/ArjenSchwarz/flux/internal/derivedstats"
	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/aws/aws-lambda-go/events"
	"golang.org/x/sync/errgroup"
)

const (
	// fallbackCapacityKwh is used when the system record is missing or cobat is 0.
	fallbackCapacityKwh = 13.34
	// cutoffPercent is the fixed battery cutoff threshold.
	// Mirrored in iOS FluxCore/BatteryEnergy.swift — update both on hardware changes.
	cutoffPercent = 5
	// maxDischargeKW is the inverter's nameplate sustained discharge ceiling
	// used by the pbat-independent "can't empty before off-peak" check
	// (T-1327, Decision 1). Constant, not a parameter — there is one ceiling,
	// set in code.
	maxDischargeKW = 5.0
	// liveDataStalenessThreshold bounds how old the most recent reading can
	// be before /status stops surfacing it as live. The poller writes every
	// 10 s, so nine consecutive missed writes (90 s) is unambiguously broken
	// — most commonly AlphaESS going quiet overnight (T-1274).
	liveDataStalenessThreshold = 90 * time.Second
)

func (h *Handler) handleStatus(ctx context.Context, _ events.LambdaFunctionURLRequest) events.LambdaFunctionURLResponse {
	now := h.nowFunc().In(sydneyTZ)
	today := now.Format("2006-01-02")
	nowUnix := now.Unix()

	// Phase 1: concurrent DynamoDB queries via errgroup.
	// Any failure cancels remaining queries and returns 500.
	var (
		allReadings []dynamo.ReadingItem
		sysItem     *dynamo.SystemItem
		opItem      *dynamo.OffpeakItem
		deItem      *dynamo.DailyEnergyItem
	)

	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		items, err := h.reader.QueryReadings(gctx, h.serial, nowUnix-86400, nowUnix)
		allReadings = items
		return err
	})
	g.Go(func() error {
		item, err := h.reader.GetSystem(gctx, h.serial)
		sysItem = item
		return err
	})
	g.Go(func() error {
		item, err := h.reader.GetOffpeak(gctx, h.serial, today)
		opItem = item
		return err
	})
	g.Go(func() error {
		item, err := h.reader.GetDailyEnergy(gctx, h.serial, today)
		deItem = item
		return err
	})

	// Note read runs alongside the errgroup so a note-read failure logs and
	// leaves the field nil instead of cancelling the core queries (design
	// §Read-side failure isolation). Uses the parent ctx (not gctx) so the
	// note read isn't aborted when g.Wait returns successfully — gctx is
	// cancelled on Wait completion, which would race a still-in-flight
	// GetNote and yield a spurious nil.
	waitNote := fetchNoteAsync(ctx, h.reader, "status", h.serial, today)

	if err := g.Wait(); err != nil {
		waitNote()
		slog.Error("status query failed", "error", err)
		return errorResponse(500, "internal error")
	}
	noteText := waitNote()

	// Phase 2: in-memory computation — no I/O.
	resp := &StatusResponse{}

	// Live data from latest reading (last element of ascending-sorted results).
	// Treat the reading as live only when it is recent: a stale `Live` payload
	// would be rendered as current on the dashboard, hiding overnight gaps
	// when AlphaESS stops returning fresh snapshots (T-1274).
	liveFresh := len(allReadings) > 0 &&
		nowUnix-allReadings[len(allReadings)-1].Timestamp <= int64(liveDataStalenessThreshold.Seconds())
	if liveFresh {
		latest := allReadings[len(allReadings)-1]
		sixtySecReadings := filterReadings(allReadings, nowUnix-60, nowUnix)

		resp.Live = &LiveData{
			Ppv:            roundPower(latest.Ppv),
			Pload:          roundPower(latest.Pload),
			Pbat:           roundPower(latest.Pbat),
			Pgrid:          roundPower(latest.Pgrid),
			PgridSustained: computePgridSustained(sixtySecReadings),
			Soc:            roundPower(latest.Soc),
			Timestamp:      time.Unix(latest.Timestamp, 0).UTC().Format(time.RFC3339),
		}
	}

	// Battery info with fallback capacity.
	capacity := fallbackCapacityKwh
	if sysItem != nil && sysItem.Cobat > 0 {
		capacity = sysItem.Cobat
	}

	battery := &BatteryInfo{
		CapacityKwh:   capacity,
		CutoffPercent: cutoffPercent,
	}

	// nextOpWindowStart is the absolute Sydney-local time of the next off-peak
	// window start. Cutoff predictions at or after this boundary are
	// suppressed — the battery will be charged during that window, so any
	// projected cutoff past the boundary never actually occurs.
	nextOpWindowStart, hasOffpeakBoundary := nextOffpeakStart(now, h.offpeakStart, h.offpeakEnd)

	if liveFresh {
		latest := allReadings[len(allReadings)-1]
		if ct := computeCutoffTime(latest.Soc, latest.Pbat, capacity, cutoffPercent, now); ct != nil {
			if !hasOffpeakBoundary || ct.Before(nextOpWindowStart) {
				s := ct.UTC().Format(time.RFC3339)
				battery.EstimatedCutoff = &s
			}
		}
		// T-1327: pbat-independent "can't empty before off-peak" indicator.
		// Computed only on the live branch — a stale SoC would produce a
		// misleading flag (Decision 7, mirrors EstimatedCutoff's gating).
		battery.CantEmptyBeforeOffpeak = computeCantEmptyBeforeOffpeak(cantEmptyInput{
			Soc:                 latest.Soc,
			CapacityKwh:         capacity,
			Now:                 now,
			NextOpStart:         nextOpWindowStart,
			HasBoundary:         hasOffpeakBoundary,
			WithinOffpeakWindow: withinOffpeakWindow(now, h.offpeakStart, h.offpeakEnd),
		})
	}

	// Lowest SOC since 00:00 Sydney local on now's date — see Decision 4 in
	// specs/low-since-offpeak/decision_log.md. Field is left nil only when no
	// readings fall on or after Sydney midnight (briefly after midnight, before
	// the first reading of the new day). The 24h DynamoDB read window above
	// always covers Sydney midnight, regardless of where now sits.
	sinceMidnight := startOfDaySydney(now).Unix()
	sinceReadings := filterReadings(allReadings, sinceMidnight, nowUnix)
	if soc, ts, found := derivedstats.MinSOC(toDerivedReadings(sinceReadings)); found {
		battery.Low24h = &Low24h{
			Soc:       roundPower(soc),
			Timestamp: time.Unix(ts, 0).UTC().Format(time.RFC3339),
		}
	}
	resp.Battery = battery

	// Rolling 15-minute averages (requires >= 2 readings in window).
	fifteenMinReadings := filterReadings(allReadings, nowUnix-900, nowUnix)
	if len(fifteenMinReadings) >= 2 {
		avgLoad, avgPbat := computeRollingAverages(fifteenMinReadings)
		rolling := &RollingAvg{
			AvgLoad: roundPower(avgLoad),
			AvgPbat: roundPower(avgPbat),
		}
		if liveFresh {
			latest := allReadings[len(allReadings)-1]
			if ct := computeCutoffTime(latest.Soc, avgPbat, capacity, cutoffPercent, now); ct != nil {
				if !hasOffpeakBoundary || ct.Before(nextOpWindowStart) {
					s := ct.UTC().Format(time.RFC3339)
					rolling.EstimatedCutoff = &s
				}
			}
		}
		resp.Rolling15m = rolling
	}

	// Today's energy: compute from readings, reconcile with DailyEnergyItem.
	// Computed first so off-peak deltas can use the freshest available
	// totals rather than the up-to-6-hour-stale DailyEnergyItem snapshot.
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, sydneyTZ).Unix()
	computedEnergy := computeTodayEnergy(allReadings, midnight)

	var storedEnergy *TodayEnergy
	if deItem != nil {
		storedEnergy = &TodayEnergy{
			Epv:        roundEnergy(deItem.Epv),
			EInput:     roundEnergy(deItem.EInput),
			EOutput:    roundEnergy(deItem.EOutput),
			ECharge:    roundEnergy(deItem.ECharge),
			EDischarge: roundEnergy(deItem.EDischarge),
		}
	}
	resp.TodayEnergy = reconcileEnergy(computedEnergy, storedEnergy)

	// Off-peak data — always includes window times, plus deltas when
	// complete (from the finalised row) or pending today (live-integrated
	// from the readings already in memory for live compute).
	resp.Offpeak = buildOffpeak(opItem, allReadings, now, h.offpeakStart, h.offpeakEnd)
	resp.Note = noteText

	return jsonResponse(resp)
}

// filterReadings returns the subset of readings with timestamps in [from, to].
func filterReadings(readings []dynamo.ReadingItem, from, to int64) []dynamo.ReadingItem {
	var result []dynamo.ReadingItem
	for _, r := range readings {
		if r.Timestamp >= from && r.Timestamp <= to {
			result = append(result, r)
		}
	}
	return result
}

// buildOffpeak constructs the OffpeakData response.
//
// Window times are always included. Deltas come from one of two sources:
//   - Complete record: the poller has finalised the five integration-sourced
//     deltas, served directly from the row.
//   - Pending record on today, with now inside the window: live-integrate
//     readings over [offpeak-start, min(now, offpeak-end)). Battery delta
//     percent is unknown mid-window because we lack a fixed end SOC.
//
// Returns deltas as nil when neither source is usable (no row, pending row
// before the window opens, or sparse readings).
func buildOffpeak(item *dynamo.OffpeakItem, readings []dynamo.ReadingItem, now time.Time,
	offpeakStart, offpeakEnd string,
) *OffpeakData {
	od := &OffpeakData{
		WindowStart: offpeakStart,
		WindowEnd:   offpeakEnd,
	}
	if item == nil {
		return od
	}

	var (
		deltas offpeakDeltaValues
		ok     bool
	)
	switch item.Status {
	case dynamo.OffpeakStatusComplete:
		deltas, ok = offpeakDeltas(*item)
	case dynamo.OffpeakStatusPending:
		deltas, ok = liveOffpeakDeltas(readings, now, offpeakStart, offpeakEnd)
	}
	if !ok {
		return od
	}

	od.Status = item.Status
	od.GridUsageKwh = floatPtr(roundEnergy(deltas.GridImport))
	od.SolarKwh = floatPtr(roundEnergy(deltas.Solar))
	od.BatteryChargeKwh = floatPtr(roundEnergy(deltas.BatteryCharge))
	od.BatteryDischargeKwh = floatPtr(roundEnergy(deltas.BatteryDischarge))
	od.GridExportKwh = floatPtr(roundEnergy(deltas.GridExport))
	if item.Status == dynamo.OffpeakStatusComplete {
		od.BatteryDeltaPercent = floatPtr(roundPower(item.BatteryDeltaPercent))
	}
	return od
}

func floatPtr(v float64) *float64 { return &v }
