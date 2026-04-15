package api

import (
	"context"
	"log/slog"
	"regexp"
	"time"

	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/aws/aws-lambda-go/events"
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

	// Query flux-readings for the full day.
	loc, _ := time.LoadLocation("Australia/Sydney")
	dayStart, _ := time.ParseInLocation("2006-01-02", date, loc)
	dayEnd := dayStart.AddDate(0, 0, 1)

	readings, err := h.reader.QueryReadings(ctx, h.serial, dayStart.Unix(), dayEnd.Unix()-1)
	if err != nil {
		slog.Error("day readings query failed", "error", err)
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

	// Get daily energy for summary.
	deItem, err := h.reader.GetDailyEnergy(ctx, h.serial, date)
	if err != nil {
		slog.Error("day energy query failed", "error", err)
		return errorResponse(500, "internal error")
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
			summary.SocLow = roundPower(socLow)
			summary.SocLowTime = time.Unix(socLowTime, 0).UTC().Format(time.RFC3339)
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
	points := make([]TimeSeriesPoint, len(items))
	for i, item := range items {
		// Parse uploadTime format "YYYY-MM-DD HH:MM:SS" in Sydney timezone.
		loc, _ := time.LoadLocation("Australia/Sydney")
		t, _ := time.ParseInLocation("2006-01-02 15:04:05", item.UploadTime, loc)
		points[i] = TimeSeriesPoint{
			Timestamp: t.UTC().Format(time.RFC3339),
			Soc:       roundPower(item.Cbat),
		}
	}
	return points
}

// findMinSOCFromPower finds the minimum cbat value from daily power items.
func findMinSOCFromPower(items []dynamo.DailyPowerItem) (soc float64, timestamp int64, found bool) {
	if len(items) == 0 {
		return 0, 0, false
	}
	loc, _ := time.LoadLocation("Australia/Sydney")
	minSoc := items[0].Cbat
	t, _ := time.ParseInLocation("2006-01-02 15:04:05", items[0].UploadTime, loc)
	minTS := t.Unix()
	for _, item := range items[1:] {
		if item.Cbat < minSoc {
			minSoc = item.Cbat
			t, _ = time.ParseInLocation("2006-01-02 15:04:05", item.UploadTime, loc)
			minTS = t.Unix()
		}
	}
	return minSoc, minTS, true
}
