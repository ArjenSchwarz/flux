package api

import (
	"context"
	"log/slog"
	"regexp"
	"time"

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

	// Compute day boundaries in Sydney timezone.
	dayStart, _ := time.ParseInLocation("2006-01-02", date, sydneyTZ)
	dayEnd := dayStart.AddDate(0, 0, 1)

	// Concurrent queries: readings and daily energy are independent.
	var (
		readings []dynamo.ReadingItem
		deItem   *dynamo.DailyEnergyItem
	)

	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		items, err := h.reader.QueryReadings(gctx, h.serial, dayStart.Unix(), dayEnd.Unix()-1)
		readings = items
		return err
	})
	g.Go(func() error {
		item, err := h.reader.GetDailyEnergy(gctx, h.serial, date)
		deItem = item
		return err
	})

	if err := g.Wait(); err != nil {
		slog.Error("day query failed", "error", err)
		return errorResponse(500, "internal error")
	}

	var points []TimeSeriesPoint
	var socLow float64
	var socLowTime int64
	var hasReadings bool

	if len(readings) > 0 {
		// Compute socLow from raw data before downsampling.
		socLow, socLowTime, hasReadings = findMinSOC(readings)
		points = downsample(readings, date)
	} else {
		// Fallback to flux-daily-power.
		powerItems, err := h.reader.QueryDailyPower(ctx, h.serial, date)
		if err != nil {
			slog.Error("day power query failed", "error", err)
			return errorResponse(500, "internal error")
		}

		if len(powerItems) > 0 {
			points = mapDailyPowerToPoints(powerItems)
			socLow, socLowTime, hasReadings = findMinSOCFromPower(powerItems)
		}
	}

	resp := &DayDetailResponse{
		Date:     date,
		Readings: points,
	}
	if resp.Readings == nil {
		resp.Readings = []TimeSeriesPoint{}
	}

	// Build summary: null when neither readings nor daily energy exist.
	if hasReadings || deItem != nil {
		summary := &DaySummary{}
		if hasReadings {
			sl := roundPower(socLow)
			summary.SocLow = &sl
			slt := time.Unix(socLowTime, 0).UTC().Format(time.RFC3339)
			summary.SocLowTime = &slt
		}
		if deItem != nil {
			summary.Epv = floatPtr(roundEnergy(deItem.Epv))
			summary.EInput = floatPtr(roundEnergy(deItem.EInput))
			summary.EOutput = floatPtr(roundEnergy(deItem.EOutput))
			summary.ECharge = floatPtr(roundEnergy(deItem.ECharge))
			summary.EDischarge = floatPtr(roundEnergy(deItem.EDischarge))
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
