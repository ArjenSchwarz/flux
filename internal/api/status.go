package api

import (
	"context"
	"log/slog"
	"time"

	"github.com/ArjenSchwarz/flux/internal/derivedstats"
	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/ArjenSchwarz/flux/internal/plan"
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

func (h *Handler) handleStatus(ctx context.Context, req events.LambdaFunctionURLRequest) events.LambdaFunctionURLResponse {
	now := h.nowFunc().In(sydneyTZ)
	today := now.Format("2006-01-02")
	nowUnix := now.Unix()

	// simLoadW is the added simulated load in watts (0 when no simulation is
	// requested). An unparseable or out-of-range value 400s before any I/O so
	// the client treats it as simulation-unavailable rather than rendering
	// fabricated values ([4.6]). W > 0 also suppresses the off-peak indicator
	// (Decision 11) so a real reassurance never appears beside a simulated
	// "empty by".
	simLoadW, err := parseSimulateLoad(req.QueryStringParameters)
	if err != nil {
		return errorResponse(400, err.Error())
	}

	// Phase 1: concurrent DynamoDB queries via errgroup.
	// Any failure cancels remaining queries and returns 500.
	var (
		allReadings []dynamo.ReadingItem
		sysItem     *dynamo.SystemItem
		opItem      *dynamo.OffpeakItem
		deItem      *dynamo.DailyEnergyItem
		plans       []plan.Plan
	)

	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		items, err := h.reader.QueryReadings(gctx, h.serial, nowUnix-86400, nowUnix)
		allReadings = items
		return err
	})
	// Plans join the gated queries (Q25): the table holds a handful of rows,
	// and a read failure must fail the request rather than resolve as "no
	// plan" (Q14).
	g.Go(func() error {
		rows, err := h.listPlans(gctx)
		plans = rows
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

		// Allocate the added simulated load across the live trio via the
		// priority waterfall. At simLoadW == 0 this is a true no-op, so the
		// live values are the real reading unchanged ([3.3], [4.2]).
		live := allocateSimLoad(latest.Pload, latest.Pbat, latest.Pgrid, simLoadW)

		resp.Live = &LiveData{
			Ppv:            roundPower(latest.Ppv),
			Pload:          roundPower(live.pload),
			Pbat:           roundPower(live.pbat),
			Pgrid:          roundPower(live.pgrid),
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

	// todayWindow is the free window of the plan pricing today (AC 4.1); nil
	// when that plan has no free band or no plan prices today.
	todayWindow := resolveOffpeakWindow(plans, today)

	// nextOpWindowStart is the absolute Sydney-local time of the next off-peak
	// window start. Cutoff predictions at or after this boundary are
	// suppressed — the battery will be charged during that window, so any
	// projected cutoff past the boundary never actually occurs.
	nextOpWindowStart, hasOffpeakBoundary := nextOffpeakStart(now, plans)

	if liveFresh {
		latest := allReadings[len(allReadings)-1]
		// wBattery is the portion of the added load that reaches the battery
		// after current export is cut first. exportReduction is derived from
		// the live grid and threaded into both cutoff series: it only bites
		// while exporting (where the cutoff is nil anyway), so it never makes a
		// real "empty by" wrong; in the importing/zero-grid case it is 0 and
		// wBattery == simLoadW. Each cutoff series caps independently via its
		// own per-series headroom (latest.Pbat here, avgPbat below).
		wBattery := simLoadW - exportReductionFor(latest.Pgrid, simLoadW)
		simPbat := simDischarge(latest.Pbat, wBattery)
		if ct := computeCutoffTime(latest.Soc, simPbat, capacity, cutoffPercent, now); ct != nil {
			if !hasOffpeakBoundary || ct.Before(nextOpWindowStart) {
				s := ct.UTC().Format(time.RFC3339)
				battery.EstimatedCutoff = &s
			}
		}
		// T-1327: pbat-independent "can't empty before off-peak" indicator.
		// Computed only on the live branch — a stale SoC would produce a
		// misleading flag (Decision 7, mirrors EstimatedCutoff's gating).
		// Suppressed entirely while simulating (Decision 11): the worst-case
		// reassurance is meaningless under an added-load what-if and the hero
		// renders it instead of the simulated "empty by", so leaving it set
		// could hide or contradict the simulated estimate ([4.3]).
		if simLoadW == 0 {
			battery.CantEmptyBeforeOffpeak = computeCantEmptyBeforeOffpeak(cantEmptyInput{
				Soc:                 latest.Soc,
				CapacityKwh:         capacity,
				Now:                 now,
				NextOpStart:         nextOpWindowStart,
				HasBoundary:         hasOffpeakBoundary,
				WithinOffpeakWindow: withinOffpeakWindow(now, todayWindow),
			})
		}
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
		// The rolling cutoff is the hero "empty by". Under simulation the
		// rolling battery power uses simDischarge with its own per-series
		// headroom against avgPbat (capping independently of the live tile),
		// and the returned averages carry the same adjustment so the response
		// stays internally coherent. At simLoadW == 0 every term is a no-op.
		simAvgLoad := avgLoad + simLoadW
		wBattery := simLoadW
		if liveFresh {
			latest := allReadings[len(allReadings)-1]
			wBattery = simLoadW - exportReductionFor(latest.Pgrid, simLoadW)
		}
		simAvgPbat := simDischarge(avgPbat, wBattery)
		rolling := &RollingAvg{
			AvgLoad: roundPower(simAvgLoad),
			AvgPbat: roundPower(simAvgPbat),
		}
		if liveFresh {
			latest := allReadings[len(allReadings)-1]
			if ct := computeCutoffTime(latest.Soc, simAvgPbat, capacity, cutoffPercent, now); ct != nil {
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
			Epv:        derivedstats.RoundEnergy(deItem.Epv),
			EInput:     derivedstats.RoundEnergy(deItem.EInput),
			EOutput:    derivedstats.RoundEnergy(deItem.EOutput),
			ECharge:    derivedstats.RoundEnergy(deItem.ECharge),
			EDischarge: derivedstats.RoundEnergy(deItem.EDischarge),
		}
	}
	resp.TodayEnergy = reconcileEnergy(computedEnergy, storedEnergy)

	// Off-peak data — window times plus deltas when complete (from the
	// finalised row) or pending today (live-integrated from the readings
	// already in memory for live compute). Null on a day with no free window.
	resp.Offpeak = buildOffpeak(opItem, allReadings, now, todayWindow)

	// Projected SoC at the off-peak window end (T-1533). Computed only on the
	// fresh-live branch — same gate as EstimatedCutoff (AC 2.2) — and reusing
	// the `capacity` variable already resolved above so the two figures never
	// disagree about capacity (AC 1.4). projectOffpeakEndSoc returns nil
	// outside the window, on a day with no free window, or for non-positive
	// capacity; it never reads Pbat or the simulated load, so an active
	// simulation leaves the projection unchanged (AC 1.9, AC 2.4). A non-nil
	// projection implies a resolved window, which implies resp.Offpeak is
	// non-nil — both derive from the same todayWindow. The nil check keeps that
	// an invariant rather than a nil dereference in the 10s-polled hot path if
	// either function's window handling later diverges.
	if liveFresh && resp.Offpeak != nil {
		latest := allReadings[len(allReadings)-1]
		if p := projectOffpeakEndSoc(latest.Soc, capacity, now, todayWindow); p != nil {
			resp.Offpeak.ProjectedEndSoc = p
		}
	}

	// Peak grid import so far today: integrated directly from readings over the
	// two windows bracketing off-peak, independent of reconcileEnergy so the
	// off-peak sampling artifact never lands on peak (T-1421). Absent until the
	// morning window has enough samples; iOS then uses its residual fallback.
	if peak, ok := livePeakGridImport(allReadings, now, todayWindow); ok {
		resp.PeakGridImportKwh = floatPtr(derivedstats.RoundEnergy(peak))
	}

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
// Returns nil when the day has no free window — either its plan has no free
// band or no plan prices it (Q35/AC 4.4). The whole object is absent in that
// case rather than carrying empty window strings, because a client that
// received a window-less object would have nothing to render and might
// substitute its own default window constants.
//
// When a window exists its times are always included. Deltas come from one of
// two sources:
//   - Complete record: the poller has finalised the five integration-sourced
//     deltas, served directly from the row.
//   - Pending record on today, with now inside the window: live-integrate
//     readings over [window start, min(now, window end)). Battery delta
//     percent is unknown mid-window because we lack a fixed end SOC.
//
// Returns deltas as nil when neither source is usable (no row, pending row
// before the window opens, or sparse readings).
func buildOffpeak(item *dynamo.OffpeakItem, readings []dynamo.ReadingItem, now time.Time,
	window *offpeakWindow,
) *OffpeakData {
	if window == nil {
		return nil
	}
	od := &OffpeakData{
		WindowStart: window.startHHMM(),
		WindowEnd:   window.endHHMM(),
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
		deltas, ok = liveOffpeakDeltas(readings, now, window)
	}
	if !ok {
		return od
	}

	od.Status = item.Status
	od.GridUsageKwh = floatPtr(derivedstats.RoundEnergy(deltas.GridImport))
	od.SolarKwh = floatPtr(derivedstats.RoundEnergy(deltas.Solar))
	od.BatteryChargeKwh = floatPtr(derivedstats.RoundEnergy(deltas.BatteryCharge))
	od.BatteryDischargeKwh = floatPtr(derivedstats.RoundEnergy(deltas.BatteryDischarge))
	od.GridExportKwh = floatPtr(derivedstats.RoundEnergy(deltas.GridExport))
	if item.Status == dynamo.OffpeakStatusComplete {
		od.BatteryDeltaPercent = floatPtr(roundPower(item.BatteryDeltaPercent))
	}
	return od
}

func floatPtr(v float64) *float64 { return &v }
