package api

import (
	"context"
	"log/slog"
	"time"

	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/aws/aws-lambda-go/events"
	"golang.org/x/sync/errgroup"
)

const (
	// fallbackCapacityKwh is used when the system record is missing or cobat is 0.
	fallbackCapacityKwh = 13.34
	// cutoffPercent is the fixed battery cutoff threshold.
	cutoffPercent = 10
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

	if err := g.Wait(); err != nil {
		slog.Error("status query failed", "error", err)
		return errorResponse(500, "internal error")
	}

	// Phase 2: in-memory computation — no I/O.
	resp := &StatusResponse{}

	// Live data from latest reading (last element of ascending-sorted results).
	if len(allReadings) > 0 {
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

	if len(allReadings) > 0 {
		latest := allReadings[len(allReadings)-1]
		if ct := computeCutoffTime(latest.Soc, latest.Pbat, capacity, cutoffPercent, now); ct != nil {
			s := ct.UTC().Format(time.RFC3339)
			battery.EstimatedCutoff = &s
		}
	}

	if soc, ts, found := findMinSOC(allReadings); found {
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
		if len(allReadings) > 0 {
			latest := allReadings[len(allReadings)-1]
			if ct := computeCutoffTime(latest.Soc, avgPbat, capacity, cutoffPercent, now); ct != nil {
				s := ct.UTC().Format(time.RFC3339)
				rolling.EstimatedCutoff = &s
			}
		}
		resp.Rolling15m = rolling
	}

	// Off-peak data — always includes window times, deltas only when complete.
	resp.Offpeak = buildOffpeak(opItem, h.offpeakStart, h.offpeakEnd)

	// Today's energy.
	if deItem != nil {
		resp.TodayEnergy = &TodayEnergy{
			Epv:        roundEnergy(deItem.Epv),
			EInput:     roundEnergy(deItem.EInput),
			EOutput:    roundEnergy(deItem.EOutput),
			ECharge:    roundEnergy(deItem.ECharge),
			EDischarge: roundEnergy(deItem.EDischarge),
		}
	}

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
// Always includes window times; delta fields only populated when status is "complete".
func buildOffpeak(item *dynamo.OffpeakItem, windowStart, windowEnd string) *OffpeakData {
	od := &OffpeakData{
		WindowStart: windowStart,
		WindowEnd:   windowEnd,
	}
	if item != nil && item.Status == dynamo.OffpeakStatusComplete {
		od.GridUsageKwh = floatPtr(roundEnergy(item.GridUsageKwh))
		od.SolarKwh = floatPtr(roundEnergy(item.SolarKwh))
		od.BatteryChargeKwh = floatPtr(roundEnergy(item.BatteryChargeKwh))
		od.BatteryDischargeKwh = floatPtr(roundEnergy(item.BatteryDischargeKwh))
		od.GridExportKwh = floatPtr(roundEnergy(item.GridExportKwh))
		od.BatteryDeltaPercent = floatPtr(roundPower(item.BatteryDeltaPercent))
	}
	return od
}

func floatPtr(v float64) *float64 { return &v }
