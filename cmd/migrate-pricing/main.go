// Package main is the one-shot migration CLI that converts the legacy
// three-rate pricing rows into the band model (Decision 3).
//
// The conversion itself is a handful of field moves, shared with the dynamo
// read path so the two can never disagree about what a migrated row looks
// like. The work here is the verification around it: AC 5.2 requires that
// every historical day's costs are identical before and after, and AC 5.3
// requires that to be checked against recorded pre-migration values before the
// legacy shape goes away.
//
// So the run is: read every pricing row and every retained daily-energy day,
// price each day under the pre-migration rules (inclusive end dates, the
// three-rate formula), transform the rows, price every day again under the
// band model, and diff. Any mismatch aborts before a single write — the tool
// is safe to run repeatedly, and refuses to be the thing that silently
// reprices history.
//
// The check is deliberately not "apply the same shared function twice": the
// golden side implements the legacy formula independently, including its
// server-peak preference and zero clamp (Q30), because a check that reuses the
// code under test proves nothing.
//
// Idempotent: a row with no peakRate attribute is already migrated and is
// skipped, so a second run is a no-op.
//
// Usage (with operator AWS credentials):
//
//	go run ./cmd/migrate-pricing \
//	    --serial=AB1234 \
//	    --table-pricing=flux-pricing \
//	    --table-daily-energy=flux-daily-energy \
//	    --table-offpeak=flux-offpeak \
//	    [--apply]
//
// Without --apply the tool reports what it would do and writes nothing.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"math"
	"os"
	"sort"

	_ "time/tzdata"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/ArjenSchwarz/flux/internal/plan"
)

// dynamoAPI is the subset of the DynamoDB client this CLI uses.
type dynamoAPI interface {
	Scan(ctx context.Context, params *dynamodb.ScanInput, optFns ...func(*dynamodb.Options)) (*dynamodb.ScanOutput, error)
	Query(ctx context.Context, params *dynamodb.QueryInput, optFns ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error)
	PutItem(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
}

// ErrGoldenMismatch is returned when at least one day prices differently
// before and after the transform. It is fatal by design: a migration that
// changes historical costs violates AC 5.2, and no partial write is preferable
// to a wrong one.
var ErrGoldenMismatch = errors.New("golden cost check failed: migration would change historical costs")

// costEpsilon is the tolerance for the before/after comparison. The two sides
// perform the same multiplications on the same inputs, so a real match is
// exact; the epsilon only absorbs float64 association differences.
const costEpsilon = 1e-9

type migrateOpts struct {
	serial           string
	tablePricing     string
	tableDailyEnergy string
	tableOffpeak     string
	apply            bool
}

// dayMismatch is one day whose costs changed across the transform.
type dayMismatch struct {
	Date          string
	Before, After plan.Costs
}

type migrateResult struct {
	LegacyRows      int // rows carrying peakRate — the ones to transform
	AlreadyBandRows int // rows already in the band shape — left alone
	DaysChecked     int // days priced by a legacy row, verified legacy-vs-band
	DaysBandChecked int // days priced by an already-band row, verified band-vs-band
	DaysUnpriced    int // days no row covers, before or after
	RowsWritten     int
	Mismatches      []dayMismatch
	Report          []string
}

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))

	var opts migrateOpts
	flag.StringVar(&opts.serial, "serial", os.Getenv("SYSTEM_SERIAL"), "AlphaESS system serial number (or env SYSTEM_SERIAL)")
	flag.StringVar(&opts.tablePricing, "table-pricing", os.Getenv("TABLE_PRICING"), "flux-pricing table name (or env TABLE_PRICING)")
	flag.StringVar(&opts.tableDailyEnergy, "table-daily-energy", os.Getenv("TABLE_DAILY_ENERGY"), "flux-daily-energy table name (or env TABLE_DAILY_ENERGY)")
	flag.StringVar(&opts.tableOffpeak, "table-offpeak", os.Getenv("TABLE_OFFPEAK"), "flux-offpeak table name (or env TABLE_OFFPEAK)")
	flag.BoolVar(&opts.apply, "apply", false, "write the migrated rows (default: report only)")
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

	res, err := runMigration(ctx, dynamodb.NewFromConfig(awsCfg), opts)
	if res != nil {
		for _, line := range res.Report {
			fmt.Println(line)
		}
	}
	if err != nil {
		slog.Error("migration aborted", "error", err)
		os.Exit(1)
	}
	slog.Info("migration complete",
		"legacyRows", res.LegacyRows,
		"alreadyBandRows", res.AlreadyBandRows,
		"daysChecked", res.DaysChecked,
		"daysBandChecked", res.DaysBandChecked,
		"daysUnpriced", res.DaysUnpriced,
		"rowsWritten", res.RowsWritten,
		"applied", opts.apply,
	)
	if !opts.apply {
		fmt.Println("\nDry run: nothing was written. Re-run with --apply to migrate.")
	}
}

func validateOpts(o migrateOpts) error {
	if o.serial == "" {
		return fmt.Errorf("--serial is required")
	}
	if o.tablePricing == "" {
		return fmt.Errorf("--table-pricing is required")
	}
	if o.tableDailyEnergy == "" {
		return fmt.Errorf("--table-daily-energy is required")
	}
	if o.tableOffpeak == "" {
		return fmt.Errorf("--table-offpeak is required")
	}
	return nil
}

// runMigration is the testable core. It never writes unless opts.apply is set
// and every day's costs survived the check.
func runMigration(ctx context.Context, client dynamoAPI, opts migrateOpts) (*migrateResult, error) {
	res := &migrateResult{}

	rows, err := scanPricingRaw(ctx, client, opts.tablePricing)
	if err != nil {
		return res, err
	}
	legacy, band := partitionRows(rows)
	res.LegacyRows = len(legacy)
	res.AlreadyBandRows = len(band)

	if len(legacy) == 0 {
		res.Report = append(res.Report, "No legacy pricing rows found — already migrated.")
		return res, nil
	}

	// Transform first so the check can price each day both ways in one pass.
	// Nothing is written until the check has passed in full.
	migrated := make([]dynamo.PricingItem, 0, len(legacy))
	for _, row := range legacy {
		item, err := dynamo.TransformLegacyPricing(row)
		if err != nil {
			return res, fmt.Errorf("transform legacy row (pricingId=%s): %w", row.PricingID, err)
		}
		migrated = append(migrated, item)
		res.Report = append(res.Report, transformLine(row, item))
	}

	days, err := loadDays(ctx, client, opts)
	if err != nil {
		return res, err
	}

	// Post-migration plan set: the transformed rows plus the ones that were
	// already in the band shape and are not being touched.
	afterPlans := dynamo.PlansFromItems(append(append([]dynamo.PricingItem{}, migrated...), band...))

	for _, day := range days {
		checkDay(day, legacy, band, afterPlans, res)
	}

	return res, applyMigration(ctx, client, opts, migrated, res)
}

// applyMigration is the write gate. It is a separate step so the ordering
// invariant — no row is written until every day has been verified — is one
// readable guard rather than a property of where a loop happens to sit.
func applyMigration(ctx context.Context, client dynamoAPI, opts migrateOpts,
	migrated []dynamo.PricingItem, res *migrateResult,
) error {
	if err := res.goldenErr(); err != nil {
		for _, m := range res.Mismatches {
			res.Report = append(res.Report, mismatchLine(m))
		}
		return err
	}
	if !opts.apply {
		return nil
	}
	for _, item := range migrated {
		if err := putPricingRow(ctx, client, opts.tablePricing, item); err != nil {
			return err
		}
		res.RowsWritten++
	}
	return nil
}

// goldenErr returns the abort error when any day priced differently across the
// transform, and nil when every one matched.
func (r *migrateResult) goldenErr() error {
	if len(r.Mismatches) == 0 {
		return nil
	}
	return fmt.Errorf("%w: %d of %d days differ", ErrGoldenMismatch, len(r.Mismatches), r.DaysChecked)
}

// checkDay prices one day before and after the transform and records a
// mismatch if the two disagree.
//
// The two sides are deliberately asymmetric for a legacy-priced day: the
// "before" is the independent three-rate formula under inclusive end dates,
// the "after" is the band model under exclusive ones. That asymmetry is the
// point — it is what catches a wrong end-date shift or a mis-mapped rate,
// which comparing the shared transform against itself never would.
func checkDay(day dayEnergy, legacy []dynamo.LegacyPricingItem, band []dynamo.PricingItem,
	afterPlans []plan.Plan, res *migrateResult,
) {
	if row, ok := legacyRowFor(legacy, day.Date); ok {
		before := legacyDayCosts(row, day)
		after, _ := plan.DayCosts(planFor(afterPlans, day.Date), day.toTierTwoEnergy())
		res.DaysChecked++
		if !costsEqual(before, after) {
			res.Mismatches = append(res.Mismatches, dayMismatch{Date: day.Date, Before: before, After: after})
		}
		return
	}
	if item, ok := bandRowFor(band, day.Date); ok {
		// The row is not touched by the migration, so its days are verified
		// band-formula-vs-band-formula and reported for the operator's record
		// rather than treated as at-risk.
		costs, tier := plan.DayCosts(item.Plan(), day.toDayEnergy())
		res.DaysBandChecked++
		res.Report = append(res.Report,
			fmt.Sprintf("%s  already band-shape (plan=%s tier=%d) net=%.4f — unchanged by migration",
				day.Date, item.PricingID, tier, costs.Net))
		return
	}
	res.DaysUnpriced++
}

// costsEqual compares the four figures every screen shows.
func costsEqual(a, b plan.Costs) bool {
	return math.Abs(a.ImportCost-b.ImportCost) < costEpsilon &&
		math.Abs(a.FeedInIncome-b.FeedInIncome) < costEpsilon &&
		math.Abs(a.Net-b.Net) < costEpsilon &&
		math.Abs(a.Savings-b.Savings) < costEpsilon
}

// planFor returns the plan pricing date, or the zero plan when none does. A
// day that loses its plan across the transform produces zero costs here, which
// the comparison reports as a mismatch — exactly the off-by-one the exclusive
// end date could introduce.
func planFor(plans []plan.Plan, date string) plan.Plan {
	p, _ := plan.PlanFor(plans, date)
	return p
}

// legacyRowFor finds the legacy row pricing date under the pre-migration
// inclusive end-date rule.
func legacyRowFor(rows []dynamo.LegacyPricingItem, date string) (dynamo.LegacyPricingItem, bool) {
	for _, r := range rows {
		if date < r.StartDate {
			continue
		}
		if r.EndDate == nil || date <= *r.EndDate {
			return r, true
		}
	}
	return dynamo.LegacyPricingItem{}, false
}

// bandRowFor finds the already-band row pricing date under the exclusive
// end-date rule.
func bandRowFor(rows []dynamo.PricingItem, date string) (dynamo.PricingItem, bool) {
	for _, r := range rows {
		if r.Plan().Covers(date) {
			return r, true
		}
	}
	return dynamo.PricingItem{}, false
}

// transformLine reports one row's conversion so the operator can eyeball the
// end-date shift before applying it.
func transformLine(old dynamo.LegacyPricingItem, next dynamo.PricingItem) string {
	end := func(v *string) string {
		if v == nil {
			return "open"
		}
		return *v
	}
	return fmt.Sprintf("row %s  %s..%s (inclusive) → %s..%s (exclusive)  rate=%.4f feedIn=%.4f savings=%.4f  free 11:00-14:00",
		old.PricingID, old.StartDate, end(old.EndDate), next.StartDate, end(next.EndDate),
		next.DefaultRate, next.FeedInRate, derefRate(next.SavingsReferenceRate))
}

func mismatchLine(m dayMismatch) string {
	return fmt.Sprintf("MISMATCH %s  import %.4f→%.4f  feedIn %.4f→%.4f  net %.4f→%.4f  savings %.4f→%.4f",
		m.Date,
		m.Before.ImportCost, m.After.ImportCost,
		m.Before.FeedInIncome, m.After.FeedInIncome,
		m.Before.Net, m.After.Net,
		m.Before.Savings, m.After.Savings)
}

func derefRate(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}

// putPricingRow writes the migrated row as a full item, preserving its
// pricingId. The sentinel row is never touched: it points at the open-ended
// row by id, and the transform does not change any id.
func putPricingRow(ctx context.Context, client dynamoAPI, table string, item dynamo.PricingItem) error {
	av, err := attributevalue.MarshalMap(item)
	if err != nil {
		return fmt.Errorf("marshal migrated row (pricingId=%s): %w", item.PricingID, err)
	}
	if _, err := client.PutItem(ctx, &dynamodb.PutItemInput{TableName: &table, Item: av}); err != nil {
		return fmt.Errorf("put migrated row (table=%s, pricingId=%s): %w", table, item.PricingID, err)
	}
	return nil
}

// scanPricingRaw pages the pricing table and returns the raw attribute maps,
// sentinel excluded. The raw form is required: attributevalue silently drops
// unknown attributes, so a legacy row decoded straight into PricingItem looks
// like a zero-rate band plan rather than anything recognisably legacy.
func scanPricingRaw(ctx context.Context, client dynamoAPI, table string) ([]map[string]types.AttributeValue, error) {
	var out []map[string]types.AttributeValue
	input := &dynamodb.ScanInput{TableName: &table}
	for {
		page, err := client.Scan(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("scan pricing (table=%s): %w", table, err)
		}
		for _, av := range page.Items {
			if id, ok := av["pricingId"].(*types.AttributeValueMemberS); ok && id.Value == pricingSentinelID {
				continue
			}
			out = append(out, av)
		}
		if page.LastEvaluatedKey == nil {
			break
		}
		input.ExclusiveStartKey = page.LastEvaluatedKey
	}
	return out, nil
}

// pricingSentinelID mirrors the unexported constant in internal/dynamo. The
// sentinel is keyed, not shaped, so it must be filtered by id before any
// shape detection runs.
const pricingSentinelID = "__open_ended"

// partitionRows splits the raw rows into the ones still carrying peakRate and
// the ones already in the band shape.
func partitionRows(rows []map[string]types.AttributeValue) ([]dynamo.LegacyPricingItem, []dynamo.PricingItem) {
	var legacy []dynamo.LegacyPricingItem
	var band []dynamo.PricingItem
	for _, av := range rows {
		if dynamo.IsLegacyPricingRow(av) {
			var item dynamo.LegacyPricingItem
			if err := attributevalue.UnmarshalMap(av, &item); err != nil {
				slog.Warn("skip: undecodable legacy row", "error", err)
				continue
			}
			legacy = append(legacy, item)
			continue
		}
		var item dynamo.PricingItem
		if err := attributevalue.UnmarshalMap(av, &item); err != nil {
			slog.Warn("skip: undecodable pricing row", "error", err)
			continue
		}
		band = append(band, item)
	}
	sort.SliceStable(legacy, func(i, j int) bool { return legacy[i].StartDate < legacy[j].StartDate })
	sort.SliceStable(band, func(i, j int) bool { return band[i].StartDate < band[j].StartDate })
	return legacy, band
}
