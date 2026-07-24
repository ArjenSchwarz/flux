// Package main is the standalone backfill CLI for the readings-derived
// grid-import channels: off-peak (off-peak from readings, T-1341), peak, and
// the per-band split (time-of-use pricing).
//
// Each day's free window comes from the plan pricing that day, read from the
// pricing table — not from flags. A backfill spanning a plan switch needs a
// different window on either side of it, and a static flag pair would silently
// misattribute every day on the wrong side (Q24).
//
// For each non-today date in a range it does two things:
//
//  1. Off-peak: recomputes the five flux-offpeak energy deltas (gridUsageKwh,
//     solarKwh, batteryChargeKwh, batteryDischargeKwh, gridExportKwh) by
//     integrating the power channels from flux-readings over the day's free
//     window, writing via WriteOffpeakIfComplete so a row mid-poll (pending or
//     absent) is never overwritten (AC 7.8). The row also re-records the
//     window geometry it was integrated under, so a later plan edit shows up
//     as a mismatch instead of silently repricing the day (Q23/Q31).
//
//  2. Peak and bands: integrates max(pgrid,0) over each rated segment of the
//     day's plan and writes both the per-band split (bandImports) and their
//     total (peakGridImportKwh) to the flux-daily-energy row via
//     UpdateDailyEnergyDerived — the derived-stats group is left untouched.
//     Both values come from one integration so they cannot disagree. The
//     daily-energy row is fetched first; if it is absent the date is skipped
//     (no phantom-row creation, Decision 7). If any rated segment fails the
//     usability gate the date keeps the client-side fallback (Decision 4).
//
// The two steps write to different tables in separate calls, so a run
// interrupted between them leaves one repaired and the other not. Both writes
// are idempotent, so the fix is to re-run the same command.
//
// Today's row is always skipped on both sides — the poller is the single
// authoritative writer for today (AC 7.2 / Decision 4).
//
// Usage (with operator AWS credentials):
//
//	go run ./cmd/backfill-grid \
//	    --serial=AB1234 \
//	    --from=2026-04-19 --to=2026-05-18 \
//	    --table-offpeak=flux-offpeak \
//	    --table-readings=flux-readings \
//	    --table-daily-energy=flux-daily-energy \
//	    --table-pricing=flux-pricing \
//	    [--dry-run]
//
// Defaults: from = today - 30d, to = yesterday (the practical readings TTL
// window). Set --dry-run to log intended writes and print summaries without
// invoking any DynamoDB writes.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	_ "time/tzdata"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/ArjenSchwarz/flux/internal/derivedstats"
	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/ArjenSchwarz/flux/internal/plan"
)

// dynamoAPI is the subset of the DynamoDB client this CLI uses. It mirrors
// dynamo.DynamoAPI so the CLI can construct a real *dynamo.DynamoStore from
// the same client and route conditional writes through the canonical
// condition-expression source (see writeOffpeakIfComplete in dynamostore.go).
type dynamoAPI interface {
	Query(ctx context.Context, params *dynamodb.QueryInput, optFns ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error)
	PutItem(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
	DeleteItem(ctx context.Context, params *dynamodb.DeleteItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error)
	GetItem(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
	UpdateItem(ctx context.Context, params *dynamodb.UpdateItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error)
	BatchWriteItem(ctx context.Context, params *dynamodb.BatchWriteItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.BatchWriteItemOutput, error)
	// Scan serves the pricing read (ListPricingRows). The CLI never writes
	// pricing rows — the Lambda keeps sole write access to that table.
	Scan(ctx context.Context, params *dynamodb.ScanInput, optFns ...func(*dynamodb.Options)) (*dynamodb.ScanOutput, error)
}

type backfillOpts struct {
	serial           string
	tableOffpeak     string
	tableReadings    string
	tableDailyEnergy string
	tablePricing     string
	from             string
	to               string
	location         *time.Location
	dryRun           bool
	now              func() time.Time
}

type backfillResult struct {
	RowsScanned         int
	RowsSkipped         int // today's row skipped per AC 7.2
	RowsWritten         int
	RowsDryRun          int
	RowsSparseSkipped   int // <2 usable readings in window per AC 7.4
	RowsConditionFailed int // WriteOffpeakIfComplete condition rejected
	RowsNoPlan          int // no plan prices the date — window unknowable
	RowsNoFreeBand      int // the date's plan has no free window to recompute

	// Peak/band accounting (Decision 7). Independent of the off-peak counters
	// above: a date can have its off-peak row written and its bands skipped, or
	// vice versa. Peak and bands share these counters because they come from
	// one integration and are written together.
	PeakWritten       int // peakGridImportKwh + bandImports written (or would be, in dry-run)
	PeakSkippedAbsent int // flux-daily-energy row absent — skipped (no phantom row)
	PeakSkippedSparse int // a rated segment failed the integrator's usability gate

	Summary []string
}

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))

	loc, err := time.LoadLocation("Australia/Sydney")
	if err != nil {
		slog.Error("load Sydney timezone", "error", err)
		os.Exit(1)
	}
	today := time.Now().In(loc)
	defaultTo := today.AddDate(0, 0, -1).Format("2006-01-02")
	defaultFrom := today.AddDate(0, 0, -30).Format("2006-01-02")

	opts := backfillOpts{location: loc, now: time.Now}
	flag.StringVar(&opts.serial, "serial", os.Getenv("SYSTEM_SERIAL"), "AlphaESS system serial number (or env SYSTEM_SERIAL)")
	flag.StringVar(&opts.tableOffpeak, "table-offpeak", os.Getenv("TABLE_OFFPEAK"), "flux-offpeak table name (or env TABLE_OFFPEAK)")
	flag.StringVar(&opts.tableReadings, "table-readings", os.Getenv("TABLE_READINGS"), "flux-readings table name (or env TABLE_READINGS)")
	flag.StringVar(&opts.tableDailyEnergy, "table-daily-energy", os.Getenv("TABLE_DAILY_ENERGY"), "flux-daily-energy table name (or env TABLE_DAILY_ENERGY)")
	flag.StringVar(&opts.tablePricing, "table-pricing", os.Getenv("TABLE_PRICING"), "flux-pricing table name (or env TABLE_PRICING)")
	flag.StringVar(&opts.from, "from", defaultFrom, "start date inclusive (YYYY-MM-DD, Sydney TZ)")
	flag.StringVar(&opts.to, "to", defaultTo, "end date inclusive (YYYY-MM-DD, Sydney TZ)")
	flag.BoolVar(&opts.dryRun, "dry-run", false, "log intended writes without invoking PutItem")
	flag.Parse()

	if err := validateOpts(opts); err != nil {
		slog.Error("invalid options", "error", err)
		os.Exit(2)
	}

	ctx := context.Background()
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		slog.Error("load AWS config", "error", err)
		os.Exit(1)
	}
	client := dynamodb.NewFromConfig(awsCfg)

	res, err := runBackfill(ctx, client, opts)
	if err != nil {
		slog.Error("backfill failed", "error", err)
		os.Exit(1)
	}
	for _, line := range res.Summary {
		fmt.Println(line)
	}
	slog.Info("backfill complete",
		"scanned", res.RowsScanned,
		"written", res.RowsWritten,
		"dryRun", res.RowsDryRun,
		"todaySkipped", res.RowsSkipped,
		"sparseSkipped", res.RowsSparseSkipped,
		"conditionFailed", res.RowsConditionFailed,
		"noPlan", res.RowsNoPlan,
		"noFreeBand", res.RowsNoFreeBand,
		"bandsWritten", res.PeakWritten,
		"bandsSkippedAbsentRow", res.PeakSkippedAbsent,
		"bandsSkippedSparse", res.PeakSkippedSparse,
	)
}

func validateOpts(o backfillOpts) error {
	if o.serial == "" {
		return fmt.Errorf("--serial is required")
	}
	if o.tableOffpeak == "" {
		return fmt.Errorf("--table-offpeak is required")
	}
	if o.tableReadings == "" {
		return fmt.Errorf("--table-readings is required")
	}
	if o.tableDailyEnergy == "" {
		return fmt.Errorf("--table-daily-energy is required")
	}
	if o.tablePricing == "" {
		return fmt.Errorf("--table-pricing is required")
	}
	from, err := time.ParseInLocation("2006-01-02", o.from, o.location)
	if err != nil {
		return fmt.Errorf("invalid --from %q: %w", o.from, err)
	}
	to, err := time.ParseInLocation("2006-01-02", o.to, o.location)
	if err != nil {
		return fmt.Errorf("invalid --to %q: %w", o.to, err)
	}
	if from.After(to) {
		return fmt.Errorf("--from %s is after --to %s", o.from, o.to)
	}
	return nil
}

// runBackfill is the testable core: it scans the offpeak table for the
// configured date range, recomputes the five deltas via
// derivedstats.IntegrateOffpeakDeltas, emits a drift log entry, and writes
// the patched row via the WriteOffpeakIfComplete conditional put. Today's
// row (per Sydney-local opts.now) is always skipped (AC 7.2). Days with
// fewer than two usable readings in the window emit a SKIPPED summary line
// and are left unchanged (AC 7.4).
//
// For each non-today date it also backfills peakGridImportKwh and bandImports
// on the corresponding flux-daily-energy row: it queries the full Sydney-local
// day's readings, integrates max(pgrid,0) over each rated segment of the day's
// plan, and — only when the daily-energy row already exists — writes the peak
// and band groups via UpdateDailyEnergyDerived. An absent row is skipped (no
// phantom-row creation); a failed usability gate leaves the date on the
// client-side fallback (Decision 4).
//
// Every window comes from the plan pricing that particular date, so a range
// spanning a plan switch repairs each side under its own window (Q24).
func runBackfill(ctx context.Context, client dynamoAPI, opts backfillOpts) (*backfillResult, error) {
	res := &backfillResult{}
	plans, err := dynamo.ListPricingRows(ctx, client, opts.tablePricing)
	if err != nil {
		return nil, fmt.Errorf("list pricing (%s): %w", opts.tablePricing, err)
	}
	domainPlans := dynamo.PlansFromItems(plans)

	rows, err := queryOffpeakRange(ctx, client, opts.tableOffpeak, opts.serial, opts.from, opts.to)
	if err != nil {
		return nil, fmt.Errorf("query offpeak (%s): %w", opts.tableOffpeak, err)
	}

	today := opts.now().In(opts.location).Format("2006-01-02")
	store := dynamo.NewDynamoStore(client, dynamo.TableNames{
		Offpeak:     opts.tableOffpeak,
		DailyEnergy: opts.tableDailyEnergy,
	})

	for _, row := range rows {
		res.RowsScanned++

		if row.Date == today {
			res.RowsSkipped++
			slog.Info("skip: today's row is the poller's responsibility", "date", row.Date)
			continue
		}

		day, err := time.ParseInLocation("2006-01-02", row.Date, opts.location)
		if err != nil {
			res.RowsSkipped++
			slog.Warn("skip: invalid row date", "date", row.Date, "error", err)
			continue
		}

		datePlan, hasPlan := plan.PlanFor(domainPlans, row.Date)
		if !hasPlan {
			// Without a plan there is no window to integrate over and no
			// segmentation to capture — repairing the day would mean inventing
			// its geometry.
			res.RowsNoPlan++
			slog.Warn("skip: no plan prices this date", "date", row.Date)
			continue
		}

		// Off-peak recompute. Its own query, its own usability gate, its own
		// conditional write — none of which gate the band side below
		// (Decision 7). A plan with no free band has no off-peak row to
		// repair, but its rated bands still cover the whole day.
		if win, ok := freeWindowOn(datePlan, day, opts.location); ok {
			if err := backfillOffpeak(ctx, store, client, opts, row, win, res); err != nil {
				return res, err
			}
		} else {
			res.RowsNoFreeBand++
			slog.Info("skip off-peak recompute: the date's plan has no free band", "date", row.Date)
		}

		// Peak and band backfill. Independent of the off-peak outcome above: a
		// sparse or condition-rejected off-peak row does not stop the bands
		// from being written (and vice versa). dayStart is Sydney-local
		// midnight (DST-correct via time.Date).
		dayStart := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, opts.location)
		if err := backfillBands(ctx, store, client, opts, row.Date, datePlan, dayStart, res); err != nil {
			return res, err
		}
	}

	return res, nil
}

// offpeakWindow is one day's free window resolved to absolute local bounds,
// alongside the HH:MM geometry re-recorded on the repaired row.
type offpeakWindow struct {
	Start, End time.Time
	StartHHMM  string
	EndHHMM    string
}

// freeWindowOn resolves the plan's free band onto the given local day. ok is
// false when the plan has no free band.
func freeWindowOn(p plan.Plan, day time.Time, loc *time.Location) (offpeakWindow, bool) {
	startMin, endMin, ok := p.FreeWindowMinutes()
	if !ok {
		return offpeakWindow{}, false
	}
	// plan.WallClockAt is the shared boundary resolution — the same one the
	// segmentation and the poller use, so a backfill cannot disagree with the
	// capture it is repairing about where a window edge falls.
	return offpeakWindow{
		Start:     plan.WallClockAt(day, loc, startMin),
		End:       plan.WallClockAt(day, loc, endMin),
		StartHHMM: plan.FormatBandTime(startMin),
		EndHHMM:   plan.FormatBandTime(endMin),
	}, true
}

// backfillOffpeak recomputes the five off-peak deltas for one date and writes
// the patched flux-offpeak row via WriteOffpeakIfComplete. Sparse readings and
// conditional-write rejections are recorded on res and skipped (not fatal);
// only a readings-query error is fatal.
func backfillOffpeak(ctx context.Context, store *dynamo.DynamoStore, client dynamoAPI,
	opts backfillOpts, row dynamo.OffpeakItem, win offpeakWindow, res *backfillResult,
) error {
	readings, err := queryReadingsRange(ctx, client, opts.tableReadings, opts.serial,
		win.Start.Unix(), win.End.Unix())
	if err != nil {
		return fmt.Errorf("query readings (date=%s): %w", row.Date, err)
	}

	deltas, ok := derivedstats.IntegrateOffpeakDeltas(
		toDerivedReadings(readings), win.Start.Unix(), win.End.Unix())
	if !ok {
		res.RowsSparseSkipped++
		line := fmt.Sprintf("%s  SKIPPED (sparse readings; <2 usable samples in window)", row.Date)
		res.Summary = append(res.Summary, line)
		slog.Info("skip: sparse readings", "date", row.Date, "readings", len(readings))
		return nil
	}

	patched := patchOffpeakRow(row, deltas, opts.now().UTC(), win)
	summary := summaryLine(row.Date, row, patched)
	res.Summary = append(res.Summary, summary)

	dynamo.LogOffpeakDrift(row.Date, patched)

	if opts.dryRun {
		res.RowsDryRun++
		slog.Info("dry-run patch", "date", row.Date,
			"gridUsageKwh", patched.GridUsageKwh,
			"solarKwh", patched.SolarKwh,
			"sampleCount", patched.IntegrationSampleCount)
		return nil
	}

	if err := store.WriteOffpeakIfComplete(ctx, patched); err != nil {
		if errors.Is(err, dynamo.ErrOffpeakConditionFailed) {
			res.RowsConditionFailed++
			slog.Warn("conditional-write rejected (row state changed); skipping", "date", row.Date)
			return nil
		}
		return fmt.Errorf("write offpeak (date=%s): %w", row.Date, err)
	}
	res.RowsWritten++
	slog.Info("patched offpeak deltas", "date", row.Date,
		"gridUsageKwh", patched.GridUsageKwh,
		"sampleCount", patched.IntegrationSampleCount)
	return nil
}

// backfillBands integrates max(pgrid,0) over each rated segment of the date's
// plan and writes both the split (bandImports) and its total
// (peakGridImportKwh), plus their sentinels, to the flux-daily-energy row — but
// only when that row already exists (GET first; an absent row is skipped, never
// created, per Decision 7).
//
// The two values come from one integration on purpose: peak import is by
// definition the import outside the free window, which is the sum of the rated
// bands. Computing them separately would let the number on a cost card differ
// from the number on a stats card.
//
// When any rated segment fails the usability gate the date is skipped and keeps
// the client-side fallback (Decision 4). Only the peak and band groups are
// written, so the derived-stats group is untouched. Honours --dry-run.
func backfillBands(ctx context.Context, store *dynamo.DynamoStore, client dynamoAPI,
	opts backfillOpts, date string, datePlan plan.Plan, dayStart time.Time, res *backfillResult,
) error {
	// GET the daily-energy row first: the backfill must never create a phantom
	// row (Decision 7). An absent row is skipped and keeps the fallback — and
	// skipping before the readings query means a date with no row costs nothing
	// to reject.
	existing, err := store.GetDailyEnergy(ctx, opts.serial, date)
	if err != nil {
		return fmt.Errorf("get daily energy for bands (date=%s): %w", date, err)
	}
	if existing == nil {
		res.PeakSkippedAbsent++
		slog.Info("skip bands: flux-daily-energy row absent (no phantom-row creation)", "date", date)
		return nil
	}

	// Separate full-day readings query, kept independent of backfillOffpeak's
	// free-window query on purpose: the rated segments span the rest of the
	// day, so this needs all of it. The off-peak recompute keeps its own query
	// and its own write.
	dayEnd := dayStart.AddDate(0, 0, 1)
	readings, err := queryReadingsRange(ctx, client, opts.tableReadings, opts.serial,
		dayStart.Unix(), dayEnd.Unix())
	if err != nil {
		return fmt.Errorf("query readings for bands (date=%s): %w", date, err)
	}

	bands, totalKwh, ok := dynamo.IntegrateRatedBands(
		toDerivedReadings(readings), datePlan, dayStart, opts.location)
	if !ok {
		res.PeakSkippedSparse++
		slog.Info("skip bands: a rated segment failed the usability gate",
			"date", date, "readings", len(readings))
		return nil
	}

	res.Summary = append(res.Summary, bandSummaryLine(date, existing.PeakGridImportKwh, totalKwh, bands))

	if opts.dryRun {
		res.PeakWritten++
		slog.Info("dry-run bands", "date", date, "peakGridImportKwh", totalKwh, "bands", len(bands))
		return nil
	}

	now := opts.now().UTC().Format(time.RFC3339)
	stats := dynamo.DerivedStats{
		PeakGridImportKwh: &totalKwh,
		PeakComputedAt:    now,
		BandImports:       bands,
		BandsComputedAt:   now,
	}
	if err := store.UpdateDailyEnergyDerived(ctx, opts.serial, date, stats); err != nil {
		return fmt.Errorf("write bands (date=%s): %w", date, err)
	}
	res.PeakWritten++
	slog.Info("wrote band split and peak grid import",
		"date", date, "peakGridImportKwh", totalKwh, "bands", len(bands))
	return nil
}

// patchOffpeakRow returns a copy of stored with the five integration-sourced
// deltas, the three provenance fields, the window geometry, and
// status=complete. Every other field — the diagnostic startE*/endE* snapshots,
// socStart/socEnd, batteryDeltaPercent — is preserved verbatim per Decision 2.
//
// Re-recording the geometry matters because the repair may run under a
// different window than the row was originally written with; leaving the old
// snapshot would mark a correctly-repaired row as stale (Q23).
func patchOffpeakRow(stored dynamo.OffpeakItem, deltas derivedstats.OffpeakDeltas, integratedAt time.Time, win offpeakWindow) dynamo.OffpeakItem {
	out := stored
	out.Status = dynamo.OffpeakStatusComplete
	out.GridUsageKwh = derivedstats.RoundEnergy(deltas.GridImportKwh)
	out.SolarKwh = derivedstats.RoundEnergy(deltas.SolarKwh)
	out.BatteryChargeKwh = derivedstats.RoundEnergy(deltas.BatteryChargeKwh)
	out.BatteryDischargeKwh = derivedstats.RoundEnergy(deltas.BatteryDischargeKwh)
	out.GridExportKwh = derivedstats.RoundEnergy(deltas.GridExportKwh)
	out.IntegrationSampleCount = deltas.SampleCount
	out.IntegrationSkippedPairs = deltas.SkippedPairs
	out.IntegratedAt = integratedAt.Format(time.RFC3339)
	out.WindowStart = win.StartHHMM
	out.WindowEnd = win.EndHHMM
	return out
}

// summaryLine formats a per-day comparison of the prior row and the patched
// row so the operator can sanity-check the magnitude of each correction
// before it propagates to the clients (AC 7.5). Surfaces all five deltas
// (grid import, solar, battery charge, battery discharge, grid export) with
// the prior value, the new value, and the absolute difference so the
// operator can see at a glance which channels moved most.
func summaryLine(date string, prev, next dynamo.OffpeakItem) string {
	col := func(label string, p, n float64) string {
		return fmt.Sprintf("%s %.2f→%.2f |Δ|=%.2f", label, p, n, math.Abs(n-p))
	}
	return fmt.Sprintf(
		"%s  %s  %s  %s  %s  %s  samples=%d skipped=%d",
		date,
		col("grid", prev.GridUsageKwh, next.GridUsageKwh),
		col("solar", prev.SolarKwh, next.SolarKwh),
		col("chg", prev.BatteryChargeKwh, next.BatteryChargeKwh),
		col("dis", prev.BatteryDischargeKwh, next.BatteryDischargeKwh),
		col("exp", prev.GridExportKwh, next.GridExportKwh),
		next.IntegrationSampleCount,
		next.IntegrationSkippedPairs,
	)
}

// bandSummaryLine formats a per-day band line: the prior stored
// peakGridImportKwh (or "none" when the row had none yet), the newly
// integrated total, and each rated band's geometry and energy. Mirrors
// summaryLine's prev→new shape so the operator can scan the rated side
// alongside the off-peak deltas and see which band moved.
func bandSummaryLine(date string, prev *float64, next float64, bands []dynamo.BandImportAttr) string {
	parts := make([]string, 0, len(bands))
	for _, b := range bands {
		parts = append(parts, fmt.Sprintf("%s-%s=%.2f", b.Start, b.End, b.Kwh))
	}
	detail := strings.Join(parts, " ")
	if prev != nil {
		return fmt.Sprintf("%s  peak %.2f→%.2f |Δ|=%.2f  bands %s",
			date, *prev, next, math.Abs(next-*prev), detail)
	}
	return fmt.Sprintf("%s  peak none→%.2f  bands %s", date, next, detail)
}

// queryOffpeakRange paginates flux-offpeak for one serial and a closed date
// range. Mirrors dynamo.DynamoReader.QueryOffpeak but uses only the small
// dynamoAPI surface so the CLI's tests don't have to satisfy the full Reader.
func queryOffpeakRange(ctx context.Context, client dynamoAPI, table, serial, from, to string) ([]dynamo.OffpeakItem, error) {
	keyCondition := "sysSn = :serial AND #d BETWEEN :start AND :end"
	exprNames := map[string]string{"#d": "date"}
	exprValues := map[string]types.AttributeValue{
		":serial": &types.AttributeValueMemberS{Value: serial},
		":start":  &types.AttributeValueMemberS{Value: from},
		":end":    &types.AttributeValueMemberS{Value: to},
	}
	return paginate[dynamo.OffpeakItem](ctx, client, table, keyCondition, exprNames, exprValues)
}

// queryReadingsRange paginates flux-readings for one serial and a closed
// timestamp range.
func queryReadingsRange(ctx context.Context, client dynamoAPI, table, serial string, fromTS, toTS int64) ([]dynamo.ReadingItem, error) {
	keyCondition := "sysSn = :serial AND #ts BETWEEN :from AND :to"
	exprNames := map[string]string{"#ts": "timestamp"}
	exprValues := map[string]types.AttributeValue{
		":serial": &types.AttributeValueMemberS{Value: serial},
		":from":   &types.AttributeValueMemberN{Value: strconv.FormatInt(fromTS, 10)},
		":to":     &types.AttributeValueMemberN{Value: strconv.FormatInt(toTS, 10)},
	}
	return paginate[dynamo.ReadingItem](ctx, client, table, keyCondition, exprNames, exprValues)
}

func paginate[T any](
	ctx context.Context,
	client dynamoAPI,
	table, keyCondition string,
	exprNames map[string]string,
	exprValues map[string]types.AttributeValue,
) ([]T, error) {
	forward := true
	input := &dynamodb.QueryInput{
		TableName:                 &table,
		KeyConditionExpression:    &keyCondition,
		ExpressionAttributeValues: exprValues,
		ScanIndexForward:          &forward,
	}
	if len(exprNames) > 0 {
		input.ExpressionAttributeNames = exprNames
	}
	var out []T
	for {
		page, err := client.Query(ctx, input)
		if err != nil {
			return nil, err
		}
		decoded := make([]T, len(page.Items))
		for i, av := range page.Items {
			if err := attributevalue.UnmarshalMap(av, &decoded[i]); err != nil {
				return nil, fmt.Errorf("unmarshal %s row: %w", table, err)
			}
		}
		out = append(out, decoded...)
		if page.LastEvaluatedKey == nil {
			break
		}
		input.ExclusiveStartKey = page.LastEvaluatedKey
	}
	return out, nil
}

// toDerivedReadings mirrors api.toDerivedReadings / poller.summaryToDerivedReadings
// — kept duplicated for the same Decision 9 reason: derivedstats must not
// import dynamo, and this is the only consumer in cmd/backfill-grid.
func toDerivedReadings(in []dynamo.ReadingItem) []derivedstats.Reading {
	out := make([]derivedstats.Reading, len(in))
	for i, r := range in {
		out[i] = derivedstats.Reading{
			Timestamp: r.Timestamp,
			Ppv:       r.Ppv,
			Pload:     r.Pload,
			Soc:       r.Soc,
			Pbat:      r.Pbat,
			Pgrid:     r.Pgrid,
		}
	}
	return out
}
