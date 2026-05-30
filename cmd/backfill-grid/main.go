// Package main is the standalone backfill CLI for the two readings-derived
// grid-import channels: off-peak (off-peak from readings, T-1341) and peak
// (peak from readings; Decision 7 renamed this tool from backfill-offpeak).
//
// For each non-today date in a range it does two things:
//
//  1. Off-peak (unchanged): recomputes the five flux-offpeak energy deltas
//     (gridUsageKwh, solarKwh, batteryChargeKwh, batteryDischargeKwh,
//     gridExportKwh) by integrating the power channels from flux-readings over
//     the SSM off-peak window, writing via WriteOffpeakIfComplete so a row
//     mid-poll (pending or absent) is never overwritten (AC 7.8).
//
//  2. Peak: computes peakGridImportKwh by integrating max(pgrid,0) over the
//     two windows bracketing off-peak ([dayStart, offpeakStart) and
//     [offpeakEnd, dayEnd)) and writes it plus the peakComputedAt sentinel to
//     the corresponding flux-daily-energy row via UpdateDailyEnergyDerived
//     (peak group only — the derived-stats group is left untouched). The
//     daily-energy row is fetched first; if it is absent the date is skipped
//     for peak (no phantom-row creation, Decision 7). If the integration's
//     usability gate fails for either sub-window the date keeps the iOS
//     fallback (Decision 4).
//
// Today's row is always skipped on both sides — the poller is the single
// authoritative writer for today (AC 7.2 / Decision 4).
//
// Usage (with operator AWS credentials):
//
//	go run ./cmd/backfill-grid \
//	    --serial=AB1234 \
//	    --from=2026-04-19 --to=2026-05-18 \
//	    --offpeak-start=11:00 --offpeak-end=14:00 \
//	    --table-offpeak=flux-offpeak \
//	    --table-readings=flux-readings \
//	    --table-daily-energy=flux-daily-energy \
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
	"time"

	_ "time/tzdata"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/ArjenSchwarz/flux/internal/derivedstats"
	"github.com/ArjenSchwarz/flux/internal/dynamo"
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
}

type backfillOpts struct {
	serial           string
	tableOffpeak     string
	tableReadings    string
	tableDailyEnergy string
	from             string
	to               string
	offpeakStart     string
	offpeakEnd       string
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

	// Peak accounting (Decision 7). Independent of the off-peak counters
	// above: a date can have its off-peak row written and its peak skipped, or
	// vice versa.
	PeakWritten       int // peakGridImportKwh written (or would be, in dry-run)
	PeakSkippedAbsent int // flux-daily-energy row absent — skipped (no phantom row)
	PeakSkippedSparse int // peak integration usability gate failed for a sub-window

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
	flag.StringVar(&opts.from, "from", defaultFrom, "start date inclusive (YYYY-MM-DD, Sydney TZ)")
	flag.StringVar(&opts.to, "to", defaultTo, "end date inclusive (YYYY-MM-DD, Sydney TZ)")
	flag.StringVar(&opts.offpeakStart, "offpeak-start", os.Getenv("OFFPEAK_START"), "off-peak window start HH:MM (or env OFFPEAK_START)")
	flag.StringVar(&opts.offpeakEnd, "offpeak-end", os.Getenv("OFFPEAK_END"), "off-peak window end HH:MM (or env OFFPEAK_END)")
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
		"peakWritten", res.PeakWritten,
		"peakSkippedAbsentRow", res.PeakSkippedAbsent,
		"peakSkippedSparse", res.PeakSkippedSparse,
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
	if o.offpeakStart == "" || o.offpeakEnd == "" {
		return fmt.Errorf("--offpeak-start and --offpeak-end are required")
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
	if _, _, ok := derivedstats.ParseOffpeakWindow(o.offpeakStart, o.offpeakEnd); !ok {
		return fmt.Errorf("invalid --offpeak-start %q / --offpeak-end %q", o.offpeakStart, o.offpeakEnd)
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
// For each non-today date it also backfills peakGridImportKwh on the
// corresponding flux-daily-energy row (Decision 7): it queries the full
// Sydney-local day's readings, integrates max(pgrid,0) over the two windows
// bracketing off-peak, and — only when the daily-energy row already exists —
// writes the peak group via UpdateDailyEnergyDerived. An absent row is skipped
// for peak (no phantom-row creation); a failed usability gate leaves the date
// on the iOS fallback (Decision 4). The off-peak recompute above is unchanged.
func runBackfill(ctx context.Context, client dynamoAPI, opts backfillOpts) (*backfillResult, error) {
	res := &backfillResult{}
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
		windowStart, windowEnd, ok := offpeakBoundaries(day, opts.location, opts.offpeakStart, opts.offpeakEnd)
		if !ok {
			res.RowsSkipped++
			slog.Warn("skip: invalid offpeak window", "date", row.Date)
			continue
		}

		// Off-peak recompute (unchanged behaviour). Its own query, its own
		// usability gate, its own conditional write — none of which gate the
		// peak side below (Decision 7).
		if err := backfillOffpeak(ctx, store, client, opts, row, windowStart, windowEnd, res); err != nil {
			return res, err
		}

		// Peak backfill. Independent of the off-peak outcome above: a sparse or
		// condition-rejected off-peak row does not stop peak from being written
		// (and vice versa). dayStart is Sydney-local midnight, dayEnd the next
		// local midnight (DST-correct via AddDate).
		dayStart := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, opts.location)
		dayEnd := dayStart.AddDate(0, 0, 1)
		if err := backfillPeak(ctx, store, client, opts, row.Date,
			dayStart, windowStart, windowEnd, dayEnd, res); err != nil {
			return res, err
		}
	}

	return res, nil
}

// backfillOffpeak recomputes the five off-peak deltas for one date and writes
// the patched flux-offpeak row via WriteOffpeakIfComplete. Behaviour is
// unchanged from the original backfill-offpeak CLI: sparse readings and
// conditional-write rejections are recorded on res and skipped (not fatal);
// only a readings-query error is fatal.
func backfillOffpeak(ctx context.Context, store *dynamo.DynamoStore, client dynamoAPI,
	opts backfillOpts, row dynamo.OffpeakItem, windowStart, windowEnd time.Time, res *backfillResult,
) error {
	readings, err := queryReadingsRange(ctx, client, opts.tableReadings, opts.serial,
		windowStart.Unix(), windowEnd.Unix())
	if err != nil {
		return fmt.Errorf("query readings (date=%s): %w", row.Date, err)
	}

	deltas, ok := derivedstats.IntegrateOffpeakDeltas(
		toDerivedReadings(readings), windowStart.Unix(), windowEnd.Unix())
	if !ok {
		res.RowsSparseSkipped++
		line := fmt.Sprintf("%s  SKIPPED (sparse readings; <2 usable samples in window)", row.Date)
		res.Summary = append(res.Summary, line)
		slog.Info("skip: sparse readings", "date", row.Date, "readings", len(readings))
		return nil
	}

	patched := patchOffpeakRow(row, deltas, opts.now().UTC())
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

// backfillPeak computes peakGridImportKwh over the two windows bracketing
// off-peak ([dayStart, offpeakStart) and [offpeakEnd, dayEnd)) and writes it,
// plus the peakComputedAt sentinel, to the flux-daily-energy row for date — but
// only when that row already exists (GET first; an absent row is skipped, never
// created, per Decision 7). When the integration's usability gate fails for
// either sub-window the date is skipped for peak and keeps the iOS fallback
// (Decision 4). Only the peak group is written (DerivedStatsComputedAt left
// empty), so the derived-stats group is untouched. Honours --dry-run.
func backfillPeak(ctx context.Context, store *dynamo.DynamoStore, client dynamoAPI,
	opts backfillOpts, date string, dayStart, offpeakStart, offpeakEnd, dayEnd time.Time, res *backfillResult,
) error {
	// Separate full-day readings query, kept independent of backfillOffpeak's
	// off-peak-window query on purpose: peak integrates the two windows
	// bracketing off-peak, so it needs the whole day. Off-peak recompute stays
	// byte-for-byte the original tool's behaviour (its own query, own write).
	readings, err := queryReadingsRange(ctx, client, opts.tableReadings, opts.serial,
		dayStart.Unix(), dayEnd.Unix())
	if err != nil {
		return fmt.Errorf("query readings for peak (date=%s): %w", date, err)
	}

	kwh, sampleCount, skippedPairs, ok := derivedstats.IntegratePeakGridImportKwh(
		toDerivedReadings(readings), dayStart.Unix(), offpeakStart.Unix(), offpeakEnd.Unix(), dayEnd.Unix())
	if !ok {
		res.PeakSkippedSparse++
		slog.Info("skip peak: sparse readings in a bracketing window", "date", date, "readings", len(readings))
		return nil
	}

	// GET the daily-energy row first: peak must never create a phantom row
	// (Decision 7). An absent row is skipped and keeps the iOS fallback.
	existing, err := store.GetDailyEnergy(ctx, opts.serial, date)
	if err != nil {
		return fmt.Errorf("get daily energy for peak (date=%s): %w", date, err)
	}
	if existing == nil {
		res.PeakSkippedAbsent++
		slog.Info("skip peak: flux-daily-energy row absent (no phantom-row creation)", "date", date)
		return nil
	}

	peakKwh := derivedstats.RoundEnergy(kwh)
	res.Summary = append(res.Summary, peakSummaryLine(date, existing.PeakGridImportKwh, peakKwh, sampleCount, skippedPairs))

	if opts.dryRun {
		res.PeakWritten++
		slog.Info("dry-run peak", "date", date, "peakGridImportKwh", peakKwh,
			"sampleCount", sampleCount, "skippedPairs", skippedPairs)
		return nil
	}

	stats := dynamo.DerivedStats{
		PeakGridImportKwh: &peakKwh,
		PeakComputedAt:    opts.now().UTC().Format(time.RFC3339),
	}
	if err := store.UpdateDailyEnergyDerived(ctx, opts.serial, date, stats); err != nil {
		return fmt.Errorf("write peak (date=%s): %w", date, err)
	}
	res.PeakWritten++
	slog.Info("wrote peak grid import", "date", date, "peakGridImportKwh", peakKwh,
		"sampleCount", sampleCount, "skippedPairs", skippedPairs)
	return nil
}

// offpeakBoundaries returns the absolute Sydney-local times of the off-peak
// window on the given day. False indicates an unparseable window.
func offpeakBoundaries(day time.Time, loc *time.Location, start, end string) (time.Time, time.Time, bool) {
	startMin, endMin, ok := derivedstats.ParseOffpeakWindow(start, end)
	if !ok {
		return time.Time{}, time.Time{}, false
	}
	midnight := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, loc)
	return midnight.Add(time.Duration(startMin) * time.Minute),
		midnight.Add(time.Duration(endMin) * time.Minute), true
}

// patchOffpeakRow returns a copy of stored with the five integration-sourced
// deltas, the three provenance fields, and status=complete. Every other
// field — the diagnostic startE*/endE* snapshots, socStart/socEnd,
// batteryDeltaPercent — is preserved verbatim per Decision 2.
func patchOffpeakRow(stored dynamo.OffpeakItem, deltas derivedstats.OffpeakDeltas, integratedAt time.Time) dynamo.OffpeakItem {
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

// peakSummaryLine formats a per-day peak line: the prior stored
// peakGridImportKwh (or "none" when the row had no peak yet), the newly
// integrated value, and the combined provenance counts across the two
// bracketing windows. Mirrors summaryLine's prev→new shape so the operator
// can scan peak alongside the off-peak deltas.
func peakSummaryLine(date string, prev *float64, next float64, sampleCount, skippedPairs int) string {
	prevStr := "none"
	if prev != nil {
		prevStr = fmt.Sprintf("%.2f", *prev)
		return fmt.Sprintf("%s  peak %s→%.2f |Δ|=%.2f  samples=%d skipped=%d",
			date, prevStr, next, math.Abs(next-*prev), sampleCount, skippedPairs)
	}
	return fmt.Sprintf("%s  peak %s→%.2f  samples=%d skipped=%d",
		date, prevStr, next, sampleCount, skippedPairs)
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
