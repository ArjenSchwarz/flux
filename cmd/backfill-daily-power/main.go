// Package main is a one-off CLI that refills flux-daily-power for past dates
// whose end-of-day snapshots are missing.
//
// The poller previously only polled "today" for daily power; after Sydney
// midnight the previous day's snapshots from the last pre-midnight tick
// onwards (between 23:05 and 23:55) were never fetched. This tool re-queries
// AlphaESS getOneDayPowerBySn for each date in the range and overwrites the
// stored snapshots. Writes are idempotent — re-running is safe.
//
// Usage:
//
//	ALPHA_APP_ID=... ALPHA_APP_SECRET=... SYSTEM_SERIAL=... \
//	go run ./cmd/backfill-daily-power \
//	    --from=2026-04-10 --to=2026-05-10 \
//	    --table-daily-power=flux-daily-power \
//	    [--dry-run]
//
// Defaults: from = today - 30d, to = yesterday (Sydney TZ).
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"time"

	_ "time/tzdata"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"

	"github.com/ArjenSchwarz/flux/internal/alphaess"
	"github.com/ArjenSchwarz/flux/internal/dynamo"
)

type opts struct {
	serial          string
	tableDailyPower string
	from            string
	to              string
	dryRun          bool
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

	o := opts{}
	flag.StringVar(&o.serial, "serial", os.Getenv("SYSTEM_SERIAL"), "AlphaESS system serial number (or env SYSTEM_SERIAL)")
	flag.StringVar(&o.tableDailyPower, "table-daily-power", envOr("TABLE_DAILY_POWER", "flux-daily-power"), "flux-daily-power table name (or env TABLE_DAILY_POWER)")
	flag.StringVar(&o.from, "from", defaultFrom, "start date inclusive (YYYY-MM-DD, Sydney TZ)")
	flag.StringVar(&o.to, "to", defaultTo, "end date inclusive (YYYY-MM-DD, Sydney TZ)")
	flag.BoolVar(&o.dryRun, "dry-run", false, "fetch from AlphaESS but skip DynamoDB writes")
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

	var store interface {
		WriteDailyPower(ctx context.Context, items []dynamo.DailyPowerItem) error
	}
	if o.dryRun {
		store = dynamo.NewLogStore(slog.Default())
	} else {
		awsCfg, err := awsconfig.LoadDefaultConfig(ctx)
		if err != nil {
			slog.Error("load AWS config", "error", err)
			os.Exit(1)
		}
		store = dynamo.NewDynamoStore(dynamodb.NewFromConfig(awsCfg), dynamo.TableNames{
			DailyPower: o.tableDailyPower,
		})
	}

	var totalWritten, totalDays, totalSkipped int
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		date := d.Format("2006-01-02")
		snapshots, err := client.GetOneDayPower(ctx, o.serial, date)
		if err != nil {
			slog.Error("fetch failed", "date", date, "error", err)
			totalSkipped++
			continue
		}
		if len(snapshots) == 0 {
			slog.Warn("no snapshots returned by AlphaESS", "date", date)
			totalSkipped++
			continue
		}
		items := dynamo.NewDailyPowerItems(o.serial, snapshots)
		if err := store.WriteDailyPower(ctx, items); err != nil {
			slog.Error("write failed", "date", date, "error", err)
			totalSkipped++
			continue
		}
		slog.Info("stored", "date", date, "count", len(items), "dryRun", o.dryRun)
		totalWritten += len(items)
		totalDays++
	}

	slog.Info("backfill complete",
		"daysWritten", totalDays,
		"daysSkipped", totalSkipped,
		"itemsWritten", totalWritten,
		"dryRun", o.dryRun,
	)
	if totalSkipped > 0 {
		os.Exit(1)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// Compile-time interface check for the dry-run LogStore.
var _ interface {
	WriteDailyPower(ctx context.Context, items []dynamo.DailyPowerItem) error
} = (*dynamo.LogStore)(nil)
