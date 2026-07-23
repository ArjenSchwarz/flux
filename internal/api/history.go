package api

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/ArjenSchwarz/flux/internal/derivedstats"
	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/ArjenSchwarz/flux/internal/plan"
	"github.com/aws/aws-lambda-go/events"
	"golang.org/x/sync/errgroup"
)

func (h *Handler) handleHistory(ctx context.Context, req events.LambdaFunctionURLRequest) events.LambdaFunctionURLResponse {
	now := h.nowFunc().In(sydneyTZ)
	today := now.Format("2006-01-02")

	// The endpoint accepts two mutually exclusive request forms (Decision 10/16):
	//   days=N      — inclusive window ending on the server's today; may
	//                 include live-computed values for today (unchanged).
	//   start/end   — explicit inclusive past range; stored values only.
	// Supplying both forms in one request is rejected rather than resolved by
	// precedence, so malformed client requests cannot be silently masked.
	daysParam := req.QueryStringParameters["days"]
	startParam := req.QueryStringParameters["start"]
	endParam := req.QueryStringParameters["end"]

	var startDate, endDate string
	includesToday := true

	switch {
	case daysParam != "" && (startParam != "" || endParam != ""):
		return errorResponse(400, "cannot combine days with start and end parameters")
	case (startParam == "") != (endParam == ""):
		return errorResponse(400, "start and end must be supplied together")
	case startParam != "":
		// Explicit range form. Past-only per Decision 15: end must be strictly
		// before the current Sydney date, so this path never performs live
		// compute and never includes a today row (req 5.3).
		start, err := time.ParseInLocation("2006-01-02", startParam, sydneyTZ)
		if err != nil {
			return errorResponse(400, "invalid start parameter, must be YYYY-MM-DD")
		}
		end, err := time.ParseInLocation("2006-01-02", endParam, sydneyTZ)
		if err != nil {
			return errorResponse(400, "invalid end parameter, must be YYYY-MM-DD")
		}
		if end.Before(start) {
			return errorResponse(400, "end must not be before start")
		}
		// String compare is safe: ParseInLocation guarantees zero-padded
		// canonical YYYY-MM-DD on both sides.
		if endParam >= today {
			return errorResponse(400, "end must be before the current date")
		}
		// Inclusive span cap of 31 days: start plus 30 days is the 31st (and
		// last allowed) day, so end may equal but not exceed it. AddDate is
		// calendar-aware, so a DST transition inside the window cannot produce
		// an off-by-one.
		if end.After(start.AddDate(0, 0, 30)) {
			return errorResponse(400, "date range must not exceed 31 days")
		}
		startDate, endDate = startParam, endParam
		includesToday = false
	default:
		// Day-count form (default 7). To-date ranges resolve to any inclusive
		// day-count from 1 through 31, so accept that whole range; a
		// non-numeric value still 400s via the err check.
		days := 7
		if daysParam != "" {
			parsed, err := strconv.Atoi(daysParam)
			if err != nil || parsed < 1 || parsed > 31 {
				return errorResponse(400, "invalid days parameter, must be between 1 and 31")
			}
			days = parsed
		}
		startDate = now.AddDate(0, 0, -(days - 1)).Format("2006-01-02")
		endDate = today
	}

	// Fetch daily energy rows and per-day off-peak rows concurrently. The
	// today readings query (used by both energy reconciliation and live
	// derivedStats compute) runs on a separate goroutine: per AC 4.9 a
	// failure there must NOT fail the whole request, so it stays out of the
	// errgroup that gates the other queries.
	var (
		items        []dynamo.DailyEnergyItem
		offpeakItems []dynamo.OffpeakItem
		plans        []plan.Plan
	)

	g, gctx := errgroup.WithContext(ctx)
	// One plan fetch for the whole range; per-day windows are resolved from it
	// below. Gated, unlike the supplementary off-peak query: a read failure
	// must fail the request rather than resolve as "no plan" (Q14).
	g.Go(func() error {
		rows, err := h.listPlans(gctx)
		plans = rows
		return err
	})
	g.Go(func() error {
		result, err := h.reader.QueryDailyEnergy(gctx, h.serial, startDate, endDate)
		items = result
		return err
	})
	g.Go(func() error {
		// Off-peak data is supplementary — the iOS grid card already
		// renders a placeholder when the split is missing. A throttle on
		// the off-peak table shouldn't take down the entire history
		// response, so log and continue without the split.
		result, err := h.reader.QueryOffpeak(gctx, h.serial, startDate, endDate)
		if err != nil {
			slog.Warn("history offpeak query failed; proceeding without split", "error", err)
			return nil
		}
		offpeakItems = result
		return nil
	})

	// Today readings: read on a sibling goroutine so a failure stays
	// isolated from the gated queries above (AC 4.9). The 24-hour window in
	// Unix seconds; computeTodayEnergy filters to >= midnight Sydney, so any
	// pre-midnight readings are discarded. The range form never includes
	// today (Decision 15), so it skips the query entirely and the channel is
	// pre-filled with an empty result.
	type readingsResult struct {
		readings []dynamo.ReadingItem
		err      error
	}
	readingsCh := make(chan readingsResult, 1)
	if includesToday {
		go func() {
			nowUnix := now.Unix()
			r, err := h.reader.QueryReadings(ctx, h.serial, nowUnix-86400, nowUnix)
			readingsCh <- readingsResult{readings: r, err: err}
		}()
	} else {
		readingsCh <- readingsResult{}
	}

	// Notes read runs alongside the errgroup so a failure logs and leaves
	// the per-day note field nil instead of cancelling the core queries.
	// Uses the parent ctx (not gctx) so the notes read isn't aborted when
	// g.Wait returns successfully — gctx is cancelled on Wait completion,
	// which would race a still-in-flight QueryNotes and yield a spurious
	// empty map.
	waitNotes := fetchNotesAsync(ctx, h.reader, "history", h.serial, startDate, endDate)

	if err := g.Wait(); err != nil {
		<-readingsCh // drain so the goroutine doesn't leak
		waitNotes()
		slog.Error("history query failed", "error", err)
		return errorResponse(500, "internal error")
	}
	notesByDate := waitNotes()
	rr := <-readingsCh
	allReadings := rr.readings
	if rr.err != nil {
		// AC 4.9: log and proceed; the today row will skip live derivedStats
		// and energy reconciliation but still serve its stored energy totals.
		slog.Warn("history today readings query failed; today row served without live compute", "error", rr.err)
		allReadings = nil
	}

	var todayComputed *TodayEnergy
	var todayReadings []dynamo.ReadingItem
	if len(allReadings) > 0 {
		midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, sydneyTZ).Unix()
		todayComputed = computeTodayEnergy(allReadings, midnight)
		// PeakPeriods/MinSOC have no date-boundary filter, so trim the 24h
		// sliding window to >= midnight Sydney before live-compute. Without
		// this, yesterday's afternoon peak could leak into today's
		// peakPeriods on /history (Blocks is safe — its integration is
		// bounded by date).
		for _, r := range allReadings {
			if r.Timestamp >= midnight {
				todayReadings = append(todayReadings, r)
			}
		}
	}

	offpeakByDate := make(map[string]dynamo.OffpeakItem, len(offpeakItems))
	for _, op := range offpeakItems {
		offpeakByDate[op.Date] = op
	}

	// Today's live-compute readings (lazy: only computed once if needed,
	// and only when allReadings is non-empty).
	var todayDerivedReadings []derivedstats.Reading

	result := make([]DayEnergy, len(items))
	for i, item := range items {
		stored := &TodayEnergy{
			Epv:        derivedstats.RoundEnergy(item.Epv),
			EInput:     derivedstats.RoundEnergy(item.EInput),
			EOutput:    derivedstats.RoundEnergy(item.EOutput),
			ECharge:    derivedstats.RoundEnergy(item.ECharge),
			EDischarge: derivedstats.RoundEnergy(item.EDischarge),
		}
		isItemToday := item.Date == today
		energy := stored
		if isItemToday {
			energy = reconcileEnergy(todayComputed, stored)
		}
		day := DayEnergy{
			Date:       item.Date,
			Epv:        energy.Epv,
			EInput:     energy.EInput,
			EOutput:    energy.EOutput,
			ECharge:    energy.ECharge,
			EDischarge: energy.EDischarge,
		}
		// Each day takes the free window of the plan pricing that day, so a
		// range spanning a switch date attributes each side correctly.
		window := resolveOffpeakWindow(plans, item.Date)
		if op, ok := offpeakByDate[item.Date]; ok {
			imp, exp, hasSplit := offpeakSplit(op, todayReadings, now, isItemToday, window)
			if hasSplit {
				day.OffpeakGridImportKwh = floatPtr(imp)
				day.OffpeakGridExportKwh = floatPtr(exp)
				day.OffpeakSource = offpeakSourceFrom(op, isItemToday, window)
			}
		}
		// Peak grid import: today is integrated live from readings (T-1420,
		// superseding peak-from-readings Decision 4a), independent of the
		// off-peak split so it renders before the window opens. Past rows use
		// the stored server-computed value; absent on either path falls through
		// to the iOS residual fallback (e.g. pre-30-day rows, Decision 4b).
		if isItemToday {
			if peak, ok := livePeakGridImport(todayReadings, now, window); ok {
				day.PeakGridImportKwh = floatPtr(derivedstats.RoundEnergy(peak))
			}
		} else if item.PeakGridImportKwh != nil {
			day.PeakGridImportKwh = floatPtr(*item.PeakGridImportKwh)
		}
		// Per-band import split: today via the same helper /day uses (AC 3.4),
		// past rows from the split captured at day close.
		if isItemToday {
			if p, priced := plan.PlanFor(plans, item.Date); priced {
				if bands, ok := liveBandImports(todayReadings, now, p); ok {
					day.BandImports = bands
				}
			}
		} else {
			day.BandImports = bandImportsFromAttr(item.BandImports)
		}
		if note, ok := notesByDate[item.Date]; ok {
			n := note
			day.Note = &n
		}

		// derivedStats: storage for past rows, live compute for today.
		if !isItemToday {
			day.DailyUsage = dynamo.DailyUsageFromAttr(item.DailyUsage)
			day.PeakPeriods = dynamo.PeakPeriodsFromAttr(item.PeakPeriods)
			if item.SocLow != nil {
				sl := item.SocLow.Soc
				day.SocLow = &sl
				slt := item.SocLow.Timestamp
				day.SocLowTime = &slt
			}
		} else if len(todayReadings) > 0 {
			// AC 4.3: live-compute against the same readings slice already
			// loaded for energy reconciliation, trimmed to today.
			if todayDerivedReadings == nil {
				todayDerivedReadings = toDerivedReadings(todayReadings)
			}
			windowStart, windowEnd := hhmmBounds(window)
			day.DailyUsage = derivedstats.Blocks(todayDerivedReadings, windowStart, windowEnd, today, today, now)
			day.PeakPeriods = derivedstats.PeakPeriods(todayDerivedReadings, windowStart, windowEnd)
			if soc, ts, found := derivedstats.MinSOC(todayDerivedReadings); found {
				slv := soc
				day.SocLow = &slv
				slt := time.Unix(ts, 0).UTC().Format(time.RFC3339)
				day.SocLowTime = &slt
			}
		}
		// AC 4.9: when isItemToday but allReadings is nil (readings query
		// failed) or todayReadings is empty (no post-midnight data yet),
		// derivedStats remain absent on the today row by design.

		result[i] = day
	}

	return jsonResponse(&HistoryResponse{Days: result})
}

// offpeakSplit returns the off-peak grid import and export for a single day.
//
// Complete records pass through the finalised deltas. A pending record on
// today's date live-integrates from the readings slice over
// [window start, min(now, window end)). Pending records on past dates
// indicate a poller failure and are reported as missing rather than zero.
// Returns hasSplit=false when the data is not usable (sparse readings,
// pre-window now, or a day with no free window).
func offpeakSplit(op dynamo.OffpeakItem, readings []dynamo.ReadingItem, now time.Time,
	isToday bool, window *offpeakWindow,
) (imp, exp float64, hasSplit bool) {
	if op.Status == dynamo.OffpeakStatusComplete {
		deltas, ok := offpeakDeltas(op)
		if !ok {
			return 0, 0, false
		}
		return derivedstats.RoundEnergy(deltas.GridImport), derivedstats.RoundEnergy(deltas.GridExport), true
	}
	if op.Status != dynamo.OffpeakStatusPending || !isToday {
		return 0, 0, false
	}
	deltas, ok := liveOffpeakDeltas(readings, now, window)
	if !ok {
		return 0, 0, false
	}
	return derivedstats.RoundEnergy(deltas.GridImport), derivedstats.RoundEnergy(deltas.GridExport), true
}
