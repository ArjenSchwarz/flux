package api

import (
	"context"
	"log/slog"
	"strconv"
	"sync"
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

	// Fetch daily energy rows, today's readings, and per-day off-peak rows
	// concurrently. Today's row is reconciled against a live integration so
	// it matches the dashboard's /status view; past rows are already
	// finalized and pass through unchanged. Off-peak rows are joined by date
	// to expose the peak vs. off-peak grid split.
	var (
		items        []dynamo.DailyEnergyItem
		allReadings  []dynamo.ReadingItem
		offpeakItems []dynamo.OffpeakItem
	)

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		result, err := h.reader.QueryDailyEnergy(gctx, h.serial, startDate, today)
		items = result
		return err
	})
	g.Go(func() error {
		// 24h window in Unix seconds; computeTodayEnergy filters to
		// >= midnight Sydney, so any pre-midnight readings are discarded.
		nowUnix := now.Unix()
		result, err := h.reader.QueryReadings(gctx, h.serial, nowUnix-86400, nowUnix)
		allReadings = result
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

	// Notes read runs outside the errgroup so a failure logs and leaves the
	// per-day note field nil instead of cancelling the core queries.
	var (
		noteItems []dynamo.NoteItem
		nwg       sync.WaitGroup
	)
	nwg.Add(1)
	go func() {
		defer nwg.Done()
		result, err := h.reader.QueryNotes(ctx, h.serial, startDate, today)
		if err != nil {
			slog.Warn("history notes query failed; continuing without notes", "error", err)
			return
		}
		noteItems = result
	}()

	if err := g.Wait(); err != nil {
		nwg.Wait()
		slog.Error("history query failed", "error", err)
		return errorResponse(500, "internal error")
	}
	nwg.Wait()

	notesByDate := make(map[string]string, len(noteItems))
	for _, n := range noteItems {
		notesByDate[n.Date] = n.Text
	}

	var todayComputed *TodayEnergy
	if len(allReadings) > 0 {
		midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, sydneyTZ).Unix()
		todayComputed = computeTodayEnergy(allReadings, midnight)
	}

	offpeakByDate := make(map[string]dynamo.OffpeakItem, len(offpeakItems))
	for _, op := range offpeakItems {
		offpeakByDate[op.Date] = op
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
		day := DayEnergy{
			Date:       item.Date,
			Epv:        energy.Epv,
			EInput:     energy.EInput,
			EOutput:    energy.EOutput,
			ECharge:    energy.ECharge,
			EDischarge: energy.EDischarge,
		}
		if op, ok := offpeakByDate[item.Date]; ok {
			imp, exp, hasSplit := offpeakSplit(op, energy, item.Date == today)
			if hasSplit {
				day.OffpeakGridImportKwh = floatPtr(imp)
				day.OffpeakGridExportKwh = floatPtr(exp)
			}
		}
		if note, ok := notesByDate[item.Date]; ok {
			n := note
			day.Note = &n
		}
		result[i] = day
	}

	return jsonResponse(&HistoryResponse{Days: result})
}

// offpeakSplit returns the off-peak grid import and export for a single day.
//
// Complete records carry final deltas computed at window close. A pending
// record on today's date can be projected forward against the running daily
// energy totals; pending records on past dates indicate a poller failure and
// are reported as missing rather than zero. Returns hasSplit=false when the
// data is not usable.
func offpeakSplit(op dynamo.OffpeakItem, energy *TodayEnergy, isToday bool) (imp, exp float64, hasSplit bool) {
	if op.Status == dynamo.OffpeakStatusPending && !isToday {
		return 0, 0, false
	}
	deltas, ok := offpeakDeltas(op, energy)
	if !ok {
		return 0, 0, false
	}
	return roundEnergy(deltas.GridImport), roundEnergy(deltas.GridExport), true
}
