// Package main is a one-off CLI that repairs flux-readings rows corrupted by
// the T-1274 overnight outage.
//
// When AlphaESS started returning `code: 200` with `data: null` for
// `getLastPowerData`, the poller silently unmarshalled the missing payload
// into a zero-valued PowerData and wrote a fresh ReadingItem per 10 s with
// every field zero. Those rows then drove the iOS Dashboard to render 0%
// SoC / 0 W everywhere, and they corrupt the Day Detail "today" chart and
// the `low24h` calculation.
//
// This tool, for each Sydney-local date in the given range:
//
//  1. Queries the day's existing readings from flux-readings.
//  2. Deletes the all-zero ones (every field exactly 0, the bogus pattern).
//  3. Fetches the 5-minute snapshots from AlphaESS via getOneDayPowerBySn.
//  4. Writes a synthetic ReadingItem per snapshot using the same field
//     mapping the past-date Day Detail fallback already uses (cbat → soc,
//     gridCharge−feedIn → pgrid, load−ppv−pgrid → pbat).
//
// Writes are idempotent — re-running is safe. Backfilled rows are
// 5-minute-granular (not the live 10 s cadence) but that is good enough for
// the Day Detail chart and recovers `low24h`.
//
// Usage:
//
//	ALPHA_APP_ID=... ALPHA_APP_SECRET=... SYSTEM_SERIAL=... \
//	go run ./cmd/backfill-readings \
//	    --from=2026-05-18 --to=2026-05-18 \
//	    --table-readings=flux-readings \
//	    [--dry-run]
//
// Defaults: from = today - 2, to = today (Sydney TZ).
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	_ "time/tzdata"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/ArjenSchwarz/flux/internal/alphaess"
	"github.com/ArjenSchwarz/flux/internal/dynamo"
)

const (
	// batchWriteMax is the DynamoDB BatchWriteItem limit per request.
	batchWriteMax = 25
	// perCallDelay paces AlphaESS calls so a multi-day backfill doesn't
	// hammer the upstream. Matches cmd/backfill-daily-power.
	perCallDelay = 500 * time.Millisecond
)

type opts struct {
	serial        string
	tableReadings string
	from          string
	to            string
	dryRun        bool
}

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))

	loc, err := time.LoadLocation("Australia/Sydney")
	if err != nil {
		slog.Error("load Sydney timezone", "error", err)
		os.Exit(1)
	}
	today := time.Now().In(loc)
	defaultTo := today.Format("2006-01-02")
	defaultFrom := today.AddDate(0, 0, -2).Format("2006-01-02")

	o := opts{}
	flag.StringVar(&o.serial, "serial", os.Getenv("SYSTEM_SERIAL"), "AlphaESS system serial number (or env SYSTEM_SERIAL)")
	flag.StringVar(&o.tableReadings, "table-readings", envOr("TABLE_READINGS", "flux-readings"), "flux-readings table name (or env TABLE_READINGS)")
	flag.StringVar(&o.from, "from", defaultFrom, "start date inclusive (YYYY-MM-DD, Sydney TZ)")
	flag.StringVar(&o.to, "to", defaultTo, "end date inclusive (YYYY-MM-DD, Sydney TZ)")
	flag.BoolVar(&o.dryRun, "dry-run", false, "fetch from AlphaESS and report planned deletes/writes but make no DynamoDB changes")
	flag.Parse()

	appID := os.Getenv("ALPHA_APP_ID")
	appSecret := os.Getenv("ALPHA_APP_SECRET")
	if appID == "" || appSecret == "" {
		slog.Error("ALPHA_APP_ID and ALPHA_APP_SECRET env vars are required")
		os.Exit(2)
	}
	if o.serial == "" {
		slog.Error("--serial (or env SYSTEM_SERIAL) is required")
		os.Exit(2)
	}

	from, err := time.ParseInLocation("2006-01-02", o.from, loc)
	if err != nil {
		slog.Error("invalid --from", "value", o.from, "error", err)
		os.Exit(2)
	}
	to, err := time.ParseInLocation("2006-01-02", o.to, loc)
	if err != nil {
		slog.Error("invalid --to", "value", o.to, "error", err)
		os.Exit(2)
	}
	if to.Before(from) {
		slog.Error("--to is before --from", "from", o.from, "to", o.to)
		os.Exit(2)
	}

	ctx := context.Background()
	client := alphaess.NewClient(appID, appSecret, 10*time.Second)

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		slog.Error("load AWS config", "error", err)
		os.Exit(1)
	}
	ddb := dynamodb.NewFromConfig(awsCfg)
	reader := dynamo.NewDynamoReader(ddb, dynamo.TableNames{Readings: o.tableReadings})

	now := time.Now().UTC()

	var totals struct {
		days, daysSkipped, deleted, written int
	}
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		if d != from {
			time.Sleep(perCallDelay)
		}
		date := d.Format("2006-01-02")
		dayStart := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, loc)
		dayEnd := dayStart.AddDate(0, 0, 1)

		// 1. Query existing readings and identify the all-zero ones.
		existing, err := reader.QueryReadings(ctx, o.serial, dayStart.Unix(), dayEnd.Unix()-1)
		if err != nil {
			slog.Error("query existing readings failed", "date", date, "error", err)
			totals.daysSkipped++
			continue
		}
		var zeros []dynamo.ReadingItem
		for _, r := range existing {
			if dynamo.IsAllZeroReading(r) {
				zeros = append(zeros, r)
			}
		}

		// 2. Fetch the 5-minute snapshots.
		snapshots, err := client.GetOneDayPower(ctx, o.serial, date)
		if err != nil {
			slog.Error("AlphaESS fetch failed", "date", date, "error", err)
			totals.daysSkipped++
			continue
		}
		if len(snapshots) == 0 {
			slog.Warn("no snapshots returned by AlphaESS; nothing to backfill",
				"date", date, "zerosFound", len(zeros))
			totals.daysSkipped++
			continue
		}

		// 3. Build synthetic ReadingItems.
		items := make([]dynamo.ReadingItem, 0, len(snapshots))
		var skipped int
		for _, snap := range snapshots {
			item, err := dynamo.NewReadingItemFromSnapshot(o.serial, snap, loc, now)
			if err != nil {
				slog.Warn("skipping unparseable snapshot", "date", date, "uploadTime", snap.UploadTime, "error", err)
				skipped++
				continue
			}
			items = append(items, item)
		}

		// 4. Apply deletes + writes (or report under dry-run).
		if o.dryRun {
			slog.Info("dry-run plan",
				"date", date,
				"existing", len(existing),
				"zerosToDelete", len(zeros),
				"snapshotsFetched", len(snapshots),
				"syntheticReadingsToWrite", len(items),
				"snapshotsSkipped", skipped,
			)
			totals.days++
			continue
		}

		if err := batchDeleteReadings(ctx, ddb, o.tableReadings, zeros); err != nil {
			slog.Error("delete zero readings failed", "date", date, "error", err)
			totals.daysSkipped++
			continue
		}
		if err := batchWriteReadings(ctx, ddb, o.tableReadings, items); err != nil {
			slog.Error("write synthetic readings failed", "date", date, "error", err)
			totals.daysSkipped++
			continue
		}
		slog.Info("backfilled",
			"date", date,
			"existing", len(existing),
			"zerosDeleted", len(zeros),
			"snapshotsFetched", len(snapshots),
			"syntheticReadingsWritten", len(items),
			"snapshotsSkipped", skipped,
		)
		totals.days++
		totals.deleted += len(zeros)
		totals.written += len(items)
	}

	slog.Info("backfill complete",
		"daysProcessed", totals.days,
		"daysSkipped", totals.daysSkipped,
		"totalZeroRowsDeleted", totals.deleted,
		"totalSyntheticReadingsWritten", totals.written,
		"dryRun", o.dryRun,
	)
	if totals.daysSkipped > 0 {
		os.Exit(1)
	}
}

// batchDeleteReadings deletes the given ReadingItems from the readings table
// using BatchWriteItem (25 per request). No-op when items is empty.
func batchDeleteReadings(ctx context.Context, ddb *dynamodb.Client, table string, items []dynamo.ReadingItem) error {
	if len(items) == 0 {
		return nil
	}
	for i := 0; i < len(items); i += batchWriteMax {
		end := i + batchWriteMax
		if end > len(items) {
			end = len(items)
		}
		requests := make([]types.WriteRequest, 0, end-i)
		for _, r := range items[i:end] {
			key, err := attributevalue.MarshalMap(struct {
				SysSn     string `dynamodbav:"sysSn"`
				Timestamp int64  `dynamodbav:"timestamp"`
			}{r.SysSn, r.Timestamp})
			if err != nil {
				return fmt.Errorf("marshal delete key (sysSn=%s, ts=%d): %w", r.SysSn, r.Timestamp, err)
			}
			requests = append(requests, types.WriteRequest{
				DeleteRequest: &types.DeleteRequest{Key: key},
			})
		}
		if err := submitBatch(ctx, ddb, table, requests, "delete"); err != nil {
			return err
		}
	}
	return nil
}

// batchWriteReadings writes the given ReadingItems via BatchWriteItem.
func batchWriteReadings(ctx context.Context, ddb *dynamodb.Client, table string, items []dynamo.ReadingItem) error {
	if len(items) == 0 {
		return nil
	}
	for i := 0; i < len(items); i += batchWriteMax {
		end := i + batchWriteMax
		if end > len(items) {
			end = len(items)
		}
		requests := make([]types.WriteRequest, 0, end-i)
		for _, r := range items[i:end] {
			av, err := attributevalue.MarshalMap(r)
			if err != nil {
				return fmt.Errorf("marshal reading (sysSn=%s, ts=%d): %w", r.SysSn, r.Timestamp, err)
			}
			requests = append(requests, types.WriteRequest{
				PutRequest: &types.PutRequest{Item: av},
			})
		}
		if err := submitBatch(ctx, ddb, table, requests, "write"); err != nil {
			return err
		}
	}
	return nil
}

// submitBatch sends a single BatchWriteItem request and retries any
// unprocessed items once. Mirrors the existing WriteDailyPower retry pattern.
func submitBatch(ctx context.Context, ddb *dynamodb.Client, table string, requests []types.WriteRequest, action string) error {
	out, err := ddb.BatchWriteItem(ctx, &dynamodb.BatchWriteItemInput{
		RequestItems: map[string][]types.WriteRequest{table: requests},
	})
	if err != nil {
		return fmt.Errorf("batch %s (table=%s): %w", action, table, err)
	}
	if len(out.UnprocessedItems) > 0 {
		slog.Warn("retrying unprocessed items", "table", table, "action", action,
			"count", len(out.UnprocessedItems[table]))
		out, err = ddb.BatchWriteItem(ctx, &dynamodb.BatchWriteItemInput{
			RequestItems: out.UnprocessedItems,
		})
		if err != nil {
			return fmt.Errorf("retry batch %s (table=%s): %w", action, table, err)
		}
		if len(out.UnprocessedItems) > 0 {
			return fmt.Errorf("batch %s (table=%s): %d items still unprocessed after retry",
				action, table, len(out.UnprocessedItems[table]))
		}
	}
	return nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
