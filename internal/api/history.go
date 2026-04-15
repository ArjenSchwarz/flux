package api

import (
	"context"
	"log/slog"
	"strconv"

	"github.com/aws/aws-lambda-go/events"
)

// validDays is the set of accepted values for the days query parameter.
var validDays = map[int]bool{7: true, 14: true, 30: true}

func (h *Handler) handleHistory(ctx context.Context, req events.LambdaFunctionURLRequest) events.LambdaFunctionURLResponse {
	now := h.nowFunc()
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

	items, err := h.reader.QueryDailyEnergy(ctx, h.serial, startDate, today)
	if err != nil {
		slog.Error("history query failed", "error", err)
		return errorResponse(500, "internal error")
	}

	result := make([]DayEnergy, len(items))
	for i, item := range items {
		result[i] = DayEnergy{
			Date:       item.Date,
			Epv:        roundEnergy(item.Epv),
			EInput:     roundEnergy(item.EInput),
			EOutput:    roundEnergy(item.EOutput),
			ECharge:    roundEnergy(item.ECharge),
			EDischarge: roundEnergy(item.EDischarge),
		}
	}

	return jsonResponse(&HistoryResponse{Days: result})
}
