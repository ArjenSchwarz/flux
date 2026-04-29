package api

import (
	"context"
	"log/slog"
	"regexp"
	"time"

	"github.com/ArjenSchwarz/flux/internal/derivedstats"
	"github.com/ArjenSchwarz/flux/internal/dynamo"
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

	// Concurrent queries: readings (today only) and daily energy. Per AC 3.5
	// past dates skip the readings query entirely.
	var (
		readings []dynamo.ReadingItem
		deItem   *dynamo.DailyEnergyItem
	)

	g, gctx := errgroup.WithContext(ctx)

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
			peakPeriods = derivedstats.PeakPeriods(drs, h.offpeakStart, h.offpeakEnd)
			dailyUsage = derivedstats.Blocks(drs, h.offpeakStart, h.offpeakEnd, date, today, now)
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
				Epv:        roundEnergy(deItem.Epv),
				EInput:     roundEnergy(deItem.EInput),
				EOutput:    roundEnergy(deItem.EOutput),
				ECharge:    roundEnergy(deItem.ECharge),
				EDischarge: roundEnergy(deItem.EDischarge),
			}
		}
		// Reconcile with live readings only for today: stored totals refresh
		// hourly from AlphaESS and lag the real-time integration. Past days'
		// stored totals are finalized at midnight and are authoritative.
		var computedEnergy *TodayEnergy
		if isToday && len(readings) > 0 {
			computedEnergy = computeTodayEnergy(readings, dayStart.Unix())
		}
		if energy := reconcileEnergy(computedEnergy, storedEnergy); energy != nil {
			summary.Epv = floatPtr(energy.Epv)
			summary.EInput = floatPtr(energy.EInput)
			summary.EOutput = floatPtr(energy.EOutput)
			summary.ECharge = floatPtr(energy.ECharge)
			summary.EDischarge = floatPtr(energy.EDischarge)
		}
		resp.Summary = summary
	}

	return jsonResponse(resp)
}

// mapDailyPowerToPoints converts fallback daily power items to time series points.
// Maps cbat to soc, power fields to 0. Used directly without downsampling.
func mapDailyPowerToPoints(items []dynamo.DailyPowerItem) []TimeSeriesPoint {
	points := make([]TimeSeriesPoint, 0, len(items))
	for _, item := range items {
		t, err := time.ParseInLocation("2006-01-02 15:04:05", item.UploadTime, sydneyTZ)
		if err != nil {
			slog.Warn("skipping daily power item with unparseable uploadTime", "uploadTime", item.UploadTime, "error", err)
			continue
		}
		points = append(points, TimeSeriesPoint{
			Timestamp: t.UTC().Format(time.RFC3339),
			Soc:       roundPower(item.Cbat),
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
