package api

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/ArjenSchwarz/flux/internal/derivedstats"
	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/aws/aws-lambda-go/events"
	"golang.org/x/sync/errgroup"
)

func (h *Handler) handleHistory(ctx context.Context, req events.LambdaFunctionURLRequest) events.LambdaFunctionURLResponse {
	now := h.nowFunc().In(sydneyTZ)
	today := now.Format("2006-01-02")

	// Parse and validate days parameter (default 7). To-date ranges resolve
	// to any inclusive day-count from 1 through 31, so accept that whole
	// range; a non-numeric value still 400s via the err check.
	days := 7
	if d := req.QueryStringParameters["days"]; d != "" {
		parsed, err := strconv.Atoi(d)
		if err != nil || parsed < 1 || parsed > 31 {
			return errorResponse(400, "invalid days parameter, must be between 1 and 31")
		}
		days = parsed
	}

	startDate := now.AddDate(0, 0, -(days - 1)).Format("2006-01-02")

	// Fetch daily energy rows and per-day off-peak rows concurrently. The
	// today readings query (used by both energy reconciliation and live
	// derivedStats compute) runs on a separate goroutine: per AC 4.9 a
	// failure there must NOT fail the whole request, so it stays out of the
	// errgroup that gates the other queries.
	var (
		items        []dynamo.DailyEnergyItem
		offpeakItems []dynamo.OffpeakItem
	)

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		result, err := h.reader.QueryDailyEnergy(gctx, h.serial, startDate, today)
		items = result
		return err
	})
	g.Go(func() error {
		// Off-peak data is supplementary — the iOS grid card already
		// renders a placeholder when the split is missing. A throttle on
		// the off-peak table shouldn't take down the entire history
		// response, so log and continue without the split.
		result, err := h.reader.QueryOffpeak(gctx, h.serial, startDate, today)
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
	// pre-midnight readings are discarded.
	type readingsResult struct {
		readings []dynamo.ReadingItem
		err      error
	}
	readingsCh := make(chan readingsResult, 1)
	go func() {
		nowUnix := now.Unix()
		r, err := h.reader.QueryReadings(ctx, h.serial, nowUnix-86400, nowUnix)
		readingsCh <- readingsResult{readings: r, err: err}
	}()

	// Notes read runs alongside the errgroup so a failure logs and leaves
	// the per-day note field nil instead of cancelling the core queries.
	// Uses the parent ctx (not gctx) so the notes read isn't aborted when
	// g.Wait returns successfully — gctx is cancelled on Wait completion,
	// which would race a still-in-flight QueryNotes and yield a spurious
	// empty map.
	waitNotes := fetchNotesAsync(ctx, h.reader, "history", h.serial, startDate, today)

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
		if op, ok := offpeakByDate[item.Date]; ok {
			imp, exp, hasSplit := offpeakSplit(op, todayReadings, now, isItemToday, h.offpeakStart, h.offpeakEnd)
			if hasSplit {
				day.OffpeakGridImportKwh = floatPtr(imp)
				day.OffpeakGridExportKwh = floatPtr(exp)
			}
		}
		// Peak grid import: today is integrated live from readings (T-1420,
		// superseding peak-from-readings Decision 4a), independent of the
		// off-peak split so it renders before the window opens. Past rows use
		// the stored server-computed value; absent on either path falls through
		// to the iOS residual fallback (e.g. pre-30-day rows, Decision 4b).
		if isItemToday {
			if peak, ok := livePeakGridImport(todayReadings, now, h.offpeakStart, h.offpeakEnd); ok {
				day.PeakGridImportKwh = floatPtr(derivedstats.RoundEnergy(peak))
			}
		} else if item.PeakGridImportKwh != nil {
			day.PeakGridImportKwh = floatPtr(*item.PeakGridImportKwh)
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
			day.DailyUsage = derivedstats.Blocks(todayDerivedReadings, h.offpeakStart, h.offpeakEnd, today, today, now)
			day.PeakPeriods = derivedstats.PeakPeriods(todayDerivedReadings, h.offpeakStart, h.offpeakEnd)
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
// [offpeak-start, min(now, offpeak-end)). Pending records on past dates
// indicate a poller failure and are reported as missing rather than zero.
// Returns hasSplit=false when the data is not usable (sparse readings or
// pre-window now).
func offpeakSplit(op dynamo.OffpeakItem, readings []dynamo.ReadingItem, now time.Time,
	isToday bool, offpeakStart, offpeakEnd string,
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
	deltas, ok := liveOffpeakDeltas(readings, now, offpeakStart, offpeakEnd)
	if !ok {
		return 0, 0, false
	}
	return derivedstats.RoundEnergy(deltas.GridImport), derivedstats.RoundEnergy(deltas.GridExport), true
}
