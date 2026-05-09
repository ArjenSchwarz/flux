// Package main is the standalone backfill CLI for the solar-by-block feature.
//
// It walks flux-daily-energy rows in a date range, finds rows whose daylight
// blocks are missing solarKwh, recomputes solarKwh from the still-available
// flux-readings via derivedstats.Blocks, and patches the field in place via
// a SET dailyUsage = :du UpdateItem. Every other DailyUsageBlock field
// (totalKwh, start, end, boundarySource, percentOfDay, status,
// averageKwhPerHour) is preserved byte-for-byte (Decision 7).
//
// Usage (with operator AWS credentials):
//
//	go run ./cmd/backfill-solar \
//	    --serial=AB1234 \
//	    --from=2026-04-09 --to=2026-05-08 \
//	    --offpeak-start=11:00 --offpeak-end=14:00 \
//	    --table-daily-energy=flux-daily-energy \
//	    --table-readings=flux-readings \
//	    [--dry-run]
//
// Defaults: from = today - 30d, to = yesterday (the practical readings TTL
// window). Set --dry-run to log intended writes without calling UpdateItem.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
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

// dynamoAPI is the subset of the DynamoDB client this CLI uses.
type dynamoAPI interface {
	Query(ctx context.Context, params *dynamodb.QueryInput, optFns ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error)
	UpdateItem(ctx context.Context, params *dynamodb.UpdateItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error)
}

type backfillOpts struct {
	serial           string
	tableDailyEnergy string
	tableReadings    string
	from             string
	to               string
	offpeakStart     string
	offpeakEnd       string
	location         *time.Location
	dryRun           bool
	now              func() time.Time
}

type backfillResult struct {
	RowsScanned int
	RowsSkipped int
	RowsWritten int
	RowsDryRun  int
	IntentLog   []string
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
	flag.StringVar(&opts.tableDailyEnergy, "table-daily-energy", os.Getenv("TABLE_DAILY_ENERGY"), "flux-daily-energy table name (or env TABLE_DAILY_ENERGY)")
	flag.StringVar(&opts.tableReadings, "table-readings", os.Getenv("TABLE_READINGS"), "flux-readings table name (or env TABLE_READINGS)")
	flag.StringVar(&opts.from, "from", defaultFrom, "start date inclusive (YYYY-MM-DD, Sydney TZ)")
	flag.StringVar(&opts.to, "to", defaultTo, "end date inclusive (YYYY-MM-DD, Sydney TZ)")
	flag.StringVar(&opts.offpeakStart, "offpeak-start", os.Getenv("OFFPEAK_START"), "off-peak window start HH:MM (or env OFFPEAK_START)")
	flag.StringVar(&opts.offpeakEnd, "offpeak-end", os.Getenv("OFFPEAK_END"), "off-peak window end HH:MM (or env OFFPEAK_END)")
	flag.BoolVar(&opts.dryRun, "dry-run", false, "log intended writes without invoking UpdateItem")
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
	slog.Info("backfill complete",
		"scanned", res.RowsScanned,
		"skipped", res.RowsSkipped,
		"written", res.RowsWritten,
		"dryRun", res.RowsDryRun,
	)
}

func validateOpts(o backfillOpts) error {
	if o.serial == "" {
		return fmt.Errorf("--serial is required")
	}
	if o.tableDailyEnergy == "" {
		return fmt.Errorf("--table-daily-energy is required")
	}
	if o.tableReadings == "" {
		return fmt.Errorf("--table-readings is required")
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
	return nil
}

// runBackfill is the testable core: it scans the daily-energy table for the
// configured date range, patches solarKwh on rows whose daylight blocks are
// missing it, and writes the patched DailyUsageAttr back via UpdateItem.
// When opts.dryRun is true, it logs intended writes and returns without
// calling UpdateItem.
func runBackfill(ctx context.Context, client dynamoAPI, opts backfillOpts) (*backfillResult, error) {
	res := &backfillResult{}
	rows, err := queryDailyEnergyRange(ctx, client, opts.tableDailyEnergy, opts.serial, opts.from, opts.to)
	if err != nil {
		return nil, fmt.Errorf("query daily energy (%s): %w", opts.tableDailyEnergy, err)
	}

	for _, row := range rows {
		res.RowsScanned++
		if row.DailyUsage == nil {
			res.RowsSkipped++
			slog.Info("skip: dailyUsage missing", "date", row.Date)
			continue
		}
		if !needsBackfill(row.DailyUsage) {
			res.RowsSkipped++
			slog.Info("skip: all daylight blocks already populated", "date", row.Date)
			continue
		}

		dayStart, err := time.ParseInLocation("2006-01-02", row.Date, opts.location)
		if err != nil {
			res.RowsSkipped++
			slog.Warn("skip: invalid date", "date", row.Date, "error", err)
			continue
		}
		dayEnd := dayStart.AddDate(0, 0, 1)

		readings, err := queryReadingsRange(ctx, client, opts.tableReadings, opts.serial, dayStart.Unix(), dayEnd.Unix()-1)
		if err != nil {
			res.RowsSkipped++
			slog.Warn("skip: readings query failed", "date", row.Date, "error", err)
			continue
		}
		if len(readings) == 0 {
			res.RowsSkipped++
			slog.Info("skip: no readings for date", "date", row.Date)
			continue
		}

		recomputed := derivedstats.Blocks(
			toDerivedReadings(readings),
			opts.offpeakStart,
			opts.offpeakEnd,
			row.Date,
			row.Date,
			opts.now().In(opts.location),
		)
		if recomputed == nil {
			res.RowsSkipped++
			slog.Warn("skip: blocks recomputation returned nil", "date", row.Date)
			continue
		}

		patched := patchSolar(row.DailyUsage, recomputed)
		populated, totalDaylight := countDaylightPopulated(patched)

		if opts.dryRun {
			res.RowsDryRun++
			for _, b := range patched.Blocks {
				if b.SolarKwh == nil {
					continue
				}
				entry := fmt.Sprintf("%s %s solarKwh=%.3f", row.Date, b.Kind, *b.SolarKwh)
				res.IntentLog = append(res.IntentLog, entry)
				slog.Info("dry-run patch", "date", row.Date, "kind", b.Kind, "solarKwh", *b.SolarKwh)
			}
			if populated < totalDaylight {
				slog.Warn("dry-run partial backfill — re-runs will keep rewriting this row until readings TTL prunes it",
					"date", row.Date, "populated", populated, "ofDaylight", totalDaylight)
			}
			continue
		}

		if err := writePatchedDailyUsage(ctx, client, opts.tableDailyEnergy, opts.serial, row.Date, patched); err != nil {
			return res, fmt.Errorf("update dailyUsage (date=%s): %w", row.Date, err)
		}
		res.RowsWritten++
		if populated < totalDaylight {
			slog.Warn("partial backfill — re-runs will keep rewriting this row until readings TTL prunes it",
				"date", row.Date, "populated", populated, "ofDaylight", totalDaylight)
		} else {
			slog.Info("patched solarKwh", "date", row.Date, "daylightBlocks", populated)
		}
	}

	return res, nil
}

// countDaylightPopulated reports how many daylight blocks (morning peak,
// off-peak, afternoon peak) carry a non-nil SolarKwh, and the total number
// of daylight blocks present on the row. Used to surface partial-backfill
// state — a row whose readings were partially TTL-pruned will keep
// re-triggering needsBackfill on every run, so operators benefit from
// seeing "2 of 3" rather than a silent-looking "patched" log line.
func countDaylightPopulated(d *dynamo.DailyUsageAttr) (populated, total int) {
	for _, b := range d.Blocks {
		switch b.Kind {
		case derivedstats.DailyUsageKindMorningPeak,
			derivedstats.DailyUsageKindOffPeak,
			derivedstats.DailyUsageKindAfternoonPeak:
			total++
			if b.SolarKwh != nil {
				populated++
			}
		}
	}
	return populated, total
}

// needsBackfill reports whether at least one daylight block is missing
// solarKwh. Night and evening blocks never carry solarKwh (Decision 1).
func needsBackfill(d *dynamo.DailyUsageAttr) bool {
	for _, b := range d.Blocks {
		switch b.Kind {
		case derivedstats.DailyUsageKindMorningPeak,
			derivedstats.DailyUsageKindOffPeak,
			derivedstats.DailyUsageKindAfternoonPeak:
			if b.SolarKwh == nil {
				return true
			}
		}
	}
	return false
}

// patchSolar returns a copy of stored with only the daylight blocks' SolarKwh
// updated from recomputed. Every other field is copied verbatim from the
// stored attribute (Decision 7) to preserve historically-correct totals
// against TTL-pruned readings.
func patchSolar(stored *dynamo.DailyUsageAttr, recomputed *derivedstats.DailyUsage) *dynamo.DailyUsageAttr {
	byKind := make(map[string]*float64, len(recomputed.Blocks))
	for i := range recomputed.Blocks {
		b := recomputed.Blocks[i]
		if b.SolarKwh != nil {
			v := *b.SolarKwh
			byKind[b.Kind] = &v
		}
	}
	out := &dynamo.DailyUsageAttr{Blocks: make([]dynamo.DailyUsageBlockAttr, len(stored.Blocks))}
	// Shallow copy: pointer fields on DailyUsageBlockAttr (currently
	// AverageKwhPerHour) are shared with `stored`. The patch loop below
	// only *replaces* the SolarKwh pointer, never dereferences and mutates
	// through any shared pointer, so this is safe today. If a future writer
	// is added that mutates other pointer fields on the returned blocks,
	// switch to a per-field deep copy to keep `stored` untouched.
	copy(out.Blocks, stored.Blocks)
	for i := range out.Blocks {
		if v, ok := byKind[out.Blocks[i].Kind]; ok {
			out.Blocks[i].SolarKwh = v
		}
	}
	return out
}

func writePatchedDailyUsage(ctx context.Context, client dynamoAPI, table, serial, date string, du *dynamo.DailyUsageAttr) error {
	av, err := attributevalue.Marshal(du)
	if err != nil {
		return fmt.Errorf("marshal dailyUsage: %w", err)
	}
	updateExpr := "SET dailyUsage = :du"
	// Guard against a vanished row: without this condition, an UpdateItem
	// against a deleted (or wrong-table) key silently creates a fresh row
	// containing only `dailyUsage` and no energy totals — a corrupted item
	// that no other write path would ever produce. ConditionalCheckFailed
	// surfaces the operational anomaly as an error instead.
	condExpr := "attribute_exists(sysSn)"
	_, err = client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: &table,
		Key: map[string]types.AttributeValue{
			"sysSn": &types.AttributeValueMemberS{Value: serial},
			"date":  &types.AttributeValueMemberS{Value: date},
		},
		UpdateExpression:    &updateExpr,
		ConditionExpression: &condExpr,
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":du": av,
		},
	})
	return err
}

// queryDailyEnergyRange paginates flux-daily-energy for one serial and a
// closed date range. Mirrors dynamo.QueryDailyEnergy but uses only the small
// dynamoAPI surface so the CLI's tests don't have to satisfy ReadAPI.
func queryDailyEnergyRange(ctx context.Context, client dynamoAPI, table, serial, from, to string) ([]dynamo.DailyEnergyItem, error) {
	keyCondition := "sysSn = :serial AND #d BETWEEN :start AND :end"
	exprNames := map[string]string{"#d": "date"}
	exprValues := map[string]types.AttributeValue{
		":serial": &types.AttributeValueMemberS{Value: serial},
		":start":  &types.AttributeValueMemberS{Value: from},
		":end":    &types.AttributeValueMemberS{Value: to},
	}
	return paginate[dynamo.DailyEnergyItem](ctx, client, table, keyCondition, exprNames, exprValues)
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

// toDerivedReadings mirrors poller.summaryToDerivedReadings — kept duplicated
// for the same Decision 9 reason: the derivedstats package must not import
// dynamo, and this is the only consumer in cmd/backfill-solar.
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
