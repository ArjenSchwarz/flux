package api

import (
	"context"
	"log/slog"
	"regexp"
	"time"

	"github.com/ArjenSchwarz/flux/internal/alphaess"
	"github.com/ArjenSchwarz/flux/internal/derivedstats"
	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/ArjenSchwarz/flux/internal/plan"
	"github.com/aws/aws-lambda-go/events"
	"golang.org/x/sync/errgroup"
)

// datePattern validates YYYY-MM-DD format.
var datePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

func (h *Handler) handleDay(ctx context.Context, req events.LambdaFunctionURLRequest) events.LambdaFunctionURLResponse {
	date := req.QueryStringParameters["date"]
	if date == "" || !datePattern.MatchString(date) {
		return errorResponse(400, "invalid or missing date parameter")
	}

	// Validate the date is actually parseable (e.g. reject 2026-13-45).
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return errorResponse(400, "invalid or missing date parameter")
	}

	// Single clock read per AC 3.7 — bucketing decision uses one instant.
	now := h.nowFunc().In(sydneyTZ)
	today := now.Format("2006-01-02")
	isToday := date == today

	// Compute day boundaries in Sydney timezone.
	dayStart, _ := time.ParseInLocation("2006-01-02", date, sydneyTZ)
	dayEnd := dayStart.AddDate(0, 0, 1)

	// Concurrent queries: readings (today only), daily energy, and off-peak
	// record. Per AC 3.5 past dates skip the readings query entirely. The
	// off-peak query is supplementary — Day Detail summary still serves
	// without the peak/off-peak split if it fails — so a query error logs
	// and continues without aborting the request.
	var (
		readings []dynamo.ReadingItem
		deItem   *dynamo.DailyEnergyItem
		opItem   *dynamo.OffpeakItem
		plans    []plan.Plan
	)

	g, gctx := errgroup.WithContext(ctx)

	// Plans are gated, unlike the supplementary off-peak query: a read failure
	// must fail the request rather than resolve as "no plan" (Q14).
	g.Go(func() error {
		rows, err := h.listPlans(gctx)
		plans = rows
		return err
	})

	if isToday {
		g.Go(func() error {
			items, err := h.reader.QueryReadings(gctx, h.serial, dayStart.Unix(), dayEnd.Unix()-1)
			readings = items
			return err
		})
	}
	g.Go(func() error {
		item, err := h.reader.GetDailyEnergy(gctx, h.serial, date)
		deItem = item
		return err
	})
	g.Go(func() error {
		item, err := h.reader.GetOffpeak(gctx, h.serial, date)
		if err != nil {
			slog.Warn("day offpeak query failed; proceeding without split", "error", err)
			return nil
		}
		opItem = item
		return nil
	})

	// Note read runs alongside the errgroup so a failure logs and leaves the
	// field nil instead of cancelling the core queries. Uses the parent ctx
	// (not gctx) so the note read isn't aborted when g.Wait returns
	// successfully — gctx is cancelled on Wait completion, which would race
	// a still-in-flight GetNote and yield a spurious nil.
	waitNote := fetchNoteAsync(ctx, h.reader, "day", h.serial, date)

	if err := g.Wait(); err != nil {
		waitNote()
		slog.Error("day query failed", "error", err)
		return errorResponse(500, "internal error")
	}
	noteText := waitNote()

	// window is the free band of the plan pricing the requested date (AC 4.1),
	// nil when that plan has no free band or the date is unpriced.
	window := resolveOffpeakWindow(plans, date)
	windowStart, windowEnd := hhmmBounds(window)

	var points []TimeSeriesPoint
	var socLow float64
	var socLowTime int64
	var hasSocLow bool
	var peakPeriods []derivedstats.PeakPeriod
	var dailyUsage *derivedstats.DailyUsage

	if isToday {
		// Live-compute path for today (unchanged from pre-feature behaviour).
		if len(readings) > 0 {
			drs := toDerivedReadings(readings)
			socLow, socLowTime, hasSocLow = derivedstats.MinSOC(drs)
			points = downsample(readings, date)
			peakPeriods = derivedstats.PeakPeriods(drs, windowStart, windowEnd)
			dailyUsage = derivedstats.Blocks(drs, windowStart, windowEnd, date, today, now)
		} else {
			// Today with no readings: fall back to flux-daily-power for the
			// chart and socLow.
			powerItems, err := h.reader.QueryDailyPower(ctx, h.serial, date)
			if err != nil {
				slog.Error("day power query failed", "error", err)
				return errorResponse(500, "internal error")
			}
			if len(powerItems) > 0 {
				points = mapDailyPowerToPoints(powerItems)
				socLow, socLowTime, hasSocLow = findMinSOCFromPower(powerItems)
			}
		}
	} else {
		// Past-date path (AC 3.1, 3.3, 3.4, 3.5): read derivedStats from
		// storage. Skip readings query entirely. Preserve the existing
		// flux-daily-power fallback for the time-series chart and the
		// daily-power-derived SOC low when readings have aged out.
		if deItem != nil {
			dailyUsage = dynamo.DailyUsageFromAttr(deItem.DailyUsage)
			peakPeriods = dynamo.PeakPeriodsFromAttr(deItem.PeakPeriods)
			if deItem.SocLow != nil {
				socLow = deItem.SocLow.Soc
				if t, err := time.Parse(time.RFC3339, deItem.SocLow.Timestamp); err == nil {
					socLowTime = t.Unix()
					hasSocLow = true
				}
			}
		}

		// Daily-power fallback for the chart and (when SocLow attribute is
		// absent) the SOC low. Always queried because old dates may have lost
		// their readings to TTL but still have a DailyPower row.
		powerItems, err := h.reader.QueryDailyPower(ctx, h.serial, date)
		if err != nil {
			slog.Error("day power query failed", "error", err)
			return errorResponse(500, "internal error")
		}
		if len(powerItems) > 0 {
			points = mapDailyPowerToPoints(powerItems)
			if !hasSocLow {
				socLow, socLowTime, hasSocLow = findMinSOCFromPower(powerItems)
			}
		}
	}

	resp := &DayDetailResponse{
		Date:        date,
		Readings:    points,
		PeakPeriods: peakPeriods,
		DailyUsage:  dailyUsage,
		Note:        noteText,
	}
	if resp.Readings == nil {
		resp.Readings = []TimeSeriesPoint{}
	}
	if resp.PeakPeriods == nil {
		resp.PeakPeriods = []derivedstats.PeakPeriod{}
	}

	// Build summary: null when neither derivedStats / readings nor daily
	// energy exist.
	if hasSocLow || deItem != nil {
		summary := &DaySummary{}
		if hasSocLow {
			sl := roundPower(socLow)
			summary.SocLow = &sl
			slt := time.Unix(socLowTime, 0).UTC().Format(time.RFC3339)
			summary.SocLowTime = &slt
		}

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
		// Reconcile with live readings only for today: stored totals refresh
		// hourly from AlphaESS and lag the real-time integration. Past days'
		// stored totals are finalized at midnight and are authoritative.
		var computedEnergy *TodayEnergy
		if isToday && len(readings) > 0 {
			computedEnergy = computeTodayEnergy(readings, dayStart.Unix())
		}
		energy := reconcileEnergy(computedEnergy, storedEnergy)
		if energy != nil {
			summary.Epv = floatPtr(energy.Epv)
			summary.EInput = floatPtr(energy.EInput)
			summary.EOutput = floatPtr(energy.EOutput)
			summary.ECharge = floatPtr(energy.ECharge)
			summary.EDischarge = floatPtr(energy.EDischarge)
		}
		if opItem != nil {
			if imp, exp, hasSplit := offpeakSplit(*opItem, readings, now, isToday, window); hasSplit {
				summary.OffpeakGridImportKwh = floatPtr(imp)
				summary.OffpeakGridExportKwh = floatPtr(exp)
			}
		}
		// Peak grid import: today is integrated live from readings (T-1420,
		// superseding peak-from-readings Decision 4a), independent of the
		// off-peak split so it renders before the window opens. Past days use
		// the stored server-computed value. Absent on either path falls through
		// to the iOS residual fallback (e.g. pre-30-day rows, Decision 4b).
		if isToday {
			if peak, ok := livePeakGridImport(readings, now, window); ok {
				summary.PeakGridImportKwh = floatPtr(derivedstats.RoundEnergy(peak))
			}
		} else if deItem != nil && deItem.PeakGridImportKwh != nil {
			summary.PeakGridImportKwh = floatPtr(*deItem.PeakGridImportKwh)
		}
		// Per-band import split: today is integrated live from readings via
		// the helper /history also uses (AC 3.4); past days are served from
		// the split captured at day close, which outlives the readings TTL.
		if isToday {
			if p, priced := plan.PlanFor(plans, date); priced {
				if bands, ok := liveBandImports(readings, now, p); ok {
					summary.BandImports = bands
				}
			}
		} else if deItem != nil {
			summary.BandImports = bandImportsFromAttr(deItem.BandImports)
		}
		resp.Summary = summary
	}

	return jsonResponse(resp)
}

// mapDailyPowerToPoints converts fallback daily power items to time series points.
// Maps the 5-minute snapshot fields onto the live-reading shape: cbat → soc,
// load → pload, ppv → ppv, and (pgrid, pbat) via alphaess.DerivePower (matches
// the live-reading sign convention used by computeTodayEnergy and
// BatteryPowerChartView). Used directly without downsampling.
func mapDailyPowerToPoints(items []dynamo.DailyPowerItem) []TimeSeriesPoint {
	points := make([]TimeSeriesPoint, 0, len(items))
	for _, item := range items {
		t, err := time.ParseInLocation("2006-01-02 15:04:05", item.UploadTime, sydneyTZ)
		if err != nil {
			slog.Warn("skipping daily power item with unparseable uploadTime", "uploadTime", item.UploadTime, "error", err)
			continue
		}
		pgrid, pbat := alphaess.DerivePower(item.Load, item.Ppv, item.GridCharge, item.FeedIn)
		points = append(points, TimeSeriesPoint{
			Timestamp: t.UTC().Format(time.RFC3339),
			Soc:       roundPower(item.Cbat),
			Ppv:       roundPower(item.Ppv),
			Pload:     roundPower(item.Load),
			Pgrid:     roundPower(pgrid),
			Pbat:      roundPower(pbat),
		})
	}
	return points
}

// findMinSOCFromPower finds the minimum cbat value from daily power items.
func findMinSOCFromPower(items []dynamo.DailyPowerItem) (soc float64, timestamp int64, found bool) {
	var minSoc float64
	var minTS int64
	for _, item := range items {
		t, err := time.ParseInLocation("2006-01-02 15:04:05", item.UploadTime, sydneyTZ)
		if err != nil {
			slog.Warn("skipping daily power item with unparseable uploadTime", "uploadTime", item.UploadTime, "error", err)
			continue
		}
		if !found || item.Cbat < minSoc {
			minSoc = item.Cbat
			minTS = t.Unix()
			found = true
		}
	}
	return minSoc, minTS, found
}
