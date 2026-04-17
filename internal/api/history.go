package api

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/aws/aws-lambda-go/events"
	"golang.org/x/sync/errgroup"
)

// validDays is the set of accepted values for the days query parameter.
var validDays = map[int]bool{7: true, 14: true, 30: true}

func (h *Handler) handleHistory(ctx context.Context, req events.LambdaFunctionURLRequest) events.LambdaFunctionURLResponse {
	now := h.nowFunc().In(sydneyTZ)
	today := now.Format("2006-01-02")

	// Parse and validate days parameter (default 7).
	days := 7
	if d := req.QueryStringParameters["days"]; d != "" {
		parsed, err := strconv.Atoi(d)
		if err != nil || !validDays[parsed] {
			return errorResponse(400, "invalid days parameter, must be 7, 14, or 30")
		}
		days = parsed
	}

	startDate := now.AddDate(0, 0, -(days - 1)).Format("2006-01-02")

	// Fetch daily energy rows and today's readings concurrently. Today's row is
	// reconciled against a live integration so it matches the dashboard's
	// /status view; past rows are already finalized and pass through unchanged.
	var (
		items       []dynamo.DailyEnergyItem
		allReadings []dynamo.ReadingItem
	)

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		result, err := h.reader.QueryDailyEnergy(gctx, h.serial, startDate, today)
		items = result
		return err
	})
	g.Go(func() error {
		nowUnix := now.Unix()
		result, err := h.reader.QueryReadings(gctx, h.serial, nowUnix-86400, nowUnix)
		allReadings = result
		return err
	})

	if err := g.Wait(); err != nil {
		slog.Error("history query failed", "error", err)
		return errorResponse(500, "internal error")
	}

	var todayComputed *TodayEnergy
	if len(allReadings) > 0 {
		midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, sydneyTZ).Unix()
		todayComputed = computeTodayEnergy(allReadings, midnight)
	}

	result := make([]DayEnergy, len(items))
	for i, item := range items {
		stored := &TodayEnergy{
			Epv:        roundEnergy(item.Epv),
			EInput:     roundEnergy(item.EInput),
			EOutput:    roundEnergy(item.EOutput),
			ECharge:    roundEnergy(item.ECharge),
			EDischarge: roundEnergy(item.EDischarge),
		}
		energy := stored
		if item.Date == today {
			energy = reconcileEnergy(todayComputed, stored)
		}
		result[i] = DayEnergy{
			Date:       item.Date,
			Epv:        energy.Epv,
			EInput:     energy.EInput,
			EOutput:    energy.EOutput,
			ECharge:    energy.ECharge,
			EDischarge: energy.EDischarge,
		}
	}

	return jsonResponse(&HistoryResponse{Days: result})
}
