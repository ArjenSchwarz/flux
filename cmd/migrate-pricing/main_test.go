package main

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ArjenSchwarz/flux/internal/dynamo"
)

const (
	testSerial       = "TEST123"
	testPricingTable = "flux-pricing-test"
	testEnergyTable  = "flux-daily-energy-test"
	testOffpeakTable = "flux-offpeak-test"
)

// fakeDynamo serves the three tables the migration reads and records every
// PutItem so tests can assert on what would be persisted.
type fakeDynamo struct {
	pricingItems []map[string]types.AttributeValue
	energyRows   []dynamo.DailyEnergyItem
	offpeakRows  []dynamo.OffpeakItem
	puts         []*dynamodb.PutItemInput
	scanErr      error
	queryErr     error
	putErr       error
}

func (f *fakeDynamo) Scan(_ context.Context, _ *dynamodb.ScanInput, _ ...func(*dynamodb.Options)) (*dynamodb.ScanOutput, error) {
	if f.scanErr != nil {
		return nil, f.scanErr
	}
	return &dynamodb.ScanOutput{Items: f.pricingItems}, nil
}

func (f *fakeDynamo) Query(_ context.Context, params *dynamodb.QueryInput, _ ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	switch *params.TableName {
	case testEnergyTable:
		return marshalItems(f.energyRows)
	case testOffpeakTable:
		return marshalItems(f.offpeakRows)
	}
	return &dynamodb.QueryOutput{}, nil
}

func (f *fakeDynamo) PutItem(_ context.Context, params *dynamodb.PutItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	f.puts = append(f.puts, params)
	if f.putErr != nil {
		return nil, f.putErr
	}
	return &dynamodb.PutItemOutput{}, nil
}

func marshalItems[T any](items []T) (*dynamodb.QueryOutput, error) {
	avs := make([]map[string]types.AttributeValue, 0, len(items))
	for i := range items {
		av, err := attributevalue.MarshalMap(items[i])
		if err != nil {
			return nil, err
		}
		avs = append(avs, av)
	}
	return &dynamodb.QueryOutput{Items: avs}, nil
}

func testOpts() migrateOpts {
	return migrateOpts{
		serial:           testSerial,
		tablePricing:     testPricingTable,
		tableDailyEnergy: testEnergyTable,
		tableOffpeak:     testOffpeakTable,
	}
}

// legacyRowAV marshals a legacy three-rate row the way the live table holds
// it — peakRate present, endDate inclusive.
func legacyRowAV(t *testing.T, id, start, end string, peak, feedIn, savings float64) map[string]types.AttributeValue {
	t.Helper()
	item := dynamo.LegacyPricingItem{
		PricingID: id, StartDate: start,
		PeakRate: peak, FeedInRate: feedIn, OffPeakSavingsRate: savings,
		CreatedAt: "2025-01-01T00:00:00Z", UpdatedAt: "2025-01-01T00:00:00Z",
	}
	if end != "" {
		item.EndDate = &end
	}
	av, err := attributevalue.MarshalMap(item)
	require.NoError(t, err)
	return av
}

// bandRowAV marshals an already-migrated band row.
func bandRowAV(t *testing.T, id, start, end string) map[string]types.AttributeValue {
	t.Helper()
	savings := 0.35
	item := dynamo.PricingItem{
		PricingID: id, StartDate: start, DefaultRate: 0.35, FeedInRate: 0.05,
		Windows:              []dynamo.PricingWindow{{Start: "11:00", End: "14:00", Free: true}},
		SavingsReferenceRate: &savings,
		CreatedAt:            "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
	}
	if end != "" {
		item.EndDate = &end
	}
	av, err := attributevalue.MarshalMap(item)
	require.NoError(t, err)
	return av
}

func sentinelAV(t *testing.T, openEndedID string) map[string]types.AttributeValue {
	t.Helper()
	av, err := attributevalue.MarshalMap(dynamo.PricingSentinel{
		PricingID: "__open_ended", OpenEndedID: &openEndedID, UpdatedAt: "2026-01-01T00:00:00Z",
	})
	require.NoError(t, err)
	return av
}

func energyRow(date string, eInput, eOutput float64, peak *float64) dynamo.DailyEnergyItem {
	return dynamo.DailyEnergyItem{
		SysSn: testSerial, Date: date, EInput: eInput, EOutput: eOutput,
		PeakGridImportKwh: peak,
	}
}

func offpeakRow(date string, gridUsage float64) dynamo.OffpeakItem {
	return dynamo.OffpeakItem{
		SysSn: testSerial, Date: date, Status: dynamo.OffpeakStatusComplete,
		GridUsageKwh: gridUsage,
		IntegratedAt: "2026-05-01T04:00:00Z", IntegrationSampleCount: 900,
		WindowStart: "11:00", WindowEnd: "14:00",
	}
}

func ptrF(v float64) *float64 { return &v }
func ptrS(v string) *string   { return &v }

// --- The golden check ---

// AC 5.2 across all four tier-2 input combinations: the day's cost must be
// bit-identical before and after the row conversion.
func TestMigration_CostsUnchangedForEveryTierTwoCombination(t *testing.T) {
	f := &fakeDynamo{
		pricingItems: []map[string]types.AttributeValue{
			legacyRowAV(t, "p1", "2026-01-01", "", 0.35, 0.05, 0.32),
		},
		energyRows: []dynamo.DailyEnergyItem{
			energyRow("2026-05-01", 20, 8, ptrF(16.5)), // off-peak present, server peak present
			energyRow("2026-05-02", 20, 8, nil),        // off-peak present, server peak absent
			energyRow("2026-05-03", 20, 8, ptrF(16.5)), // off-peak absent, server peak present
			energyRow("2026-05-04", 20, 8, nil),        // both absent
		},
		offpeakRows: []dynamo.OffpeakItem{
			offpeakRow("2026-05-01", 3.5),
			offpeakRow("2026-05-02", 3.5),
		},
	}

	res, err := runMigration(context.Background(), f, testOpts())

	require.NoError(t, err)
	assert.Empty(t, res.Mismatches)
	assert.Equal(t, 4, res.DaysChecked)
	assert.Equal(t, 1, res.LegacyRows)
}

// The zero clamp on the eInput − off-peak residual: a day whose off-peak
// import exceeds its total must price at $0, not a negative amount.
func TestMigration_ZeroClampSurvivesTheTransform(t *testing.T) {
	f := &fakeDynamo{
		pricingItems: []map[string]types.AttributeValue{
			legacyRowAV(t, "p1", "2026-01-01", "", 0.35, 0.05, 0.32),
		},
		energyRows:  []dynamo.DailyEnergyItem{energyRow("2026-05-01", 2, 0, nil)},
		offpeakRows: []dynamo.OffpeakItem{offpeakRow("2026-05-01", 5.0)},
	}

	res, err := runMigration(context.Background(), f, testOpts())

	require.NoError(t, err)
	assert.Empty(t, res.Mismatches)
	assert.Equal(t, 1, res.DaysChecked)
}

// The inclusive→exclusive end-date shift must gain and lose no day (AC 5.2):
// the predecessor's last priced day stays with the predecessor, and the
// successor's first day stays with the successor. Different rates on the two
// rows make a misattributed day show up as a cost mismatch.
func TestMigration_EndDateShiftPreservesDayOwnership(t *testing.T) {
	f := &fakeDynamo{
		pricingItems: []map[string]types.AttributeValue{
			legacyRowAV(t, "old", "2026-01-01", "2026-05-31", 0.30, 0.05, 0.28),
			legacyRowAV(t, "new", "2026-06-01", "", 0.40, 0.06, 0.38),
		},
		energyRows: []dynamo.DailyEnergyItem{
			energyRow("2026-05-30", 20, 8, ptrF(16.5)),
			energyRow("2026-05-31", 20, 8, ptrF(16.5)), // predecessor's last day
			energyRow("2026-06-01", 20, 8, ptrF(16.5)), // successor's first day
		},
		offpeakRows: []dynamo.OffpeakItem{
			offpeakRow("2026-05-30", 3.5), offpeakRow("2026-05-31", 3.5), offpeakRow("2026-06-01", 3.5),
		},
	}

	res, err := runMigration(context.Background(), f, testOpts())

	require.NoError(t, err)
	assert.Empty(t, res.Mismatches)
	assert.Equal(t, 3, res.DaysChecked)

	// The closed row's exclusive end is the day after its inclusive one.
	var migrated dynamo.PricingItem
	require.NoError(t, attributevalue.UnmarshalMap(mustPut(t, f, "old").Item, &migrated))
	require.NotNil(t, migrated.EndDate)
	assert.Equal(t, "2026-06-01", *migrated.EndDate)
}

// A repriced day must be detected. There is no fixture that makes the real
// transform reprice a day — that is the property the tool exists to guarantee —
// so the detector is exercised directly against a post-migration plan set that
// prices the day at a different rate, which is what a broken transform would
// produce.
func TestCheckDay_RecordsAMismatchWhenTheDayReprices(t *testing.T) {
	legacy := []dynamo.LegacyPricingItem{{
		PricingID: "p1", StartDate: "2026-01-01",
		PeakRate: 0.30, FeedInRate: 0.05, OffPeakSavingsRate: 0.28,
	}}
	op := offpeakRow("2026-05-01", 3.5)
	day := dayEnergy{
		Date: "2026-05-01", EInput: ptrF(20), EOutput: ptrF(8),
		PeakGridImportKwh: ptrF(16.5), Offpeak: &op,
	}
	// The migrated row should carry rate 0.30; this one carries 0.40.
	wrong := dynamo.PlansFromItems([]dynamo.PricingItem{
		{
			PricingID: "p1", StartDate: "2026-01-01", DefaultRate: 0.40, FeedInRate: 0.05,
			Windows:              []dynamo.PricingWindow{{Start: "11:00", End: "14:00", Free: true}},
			SavingsReferenceRate: ptrF(0.28),
		},
	})

	res := &migrateResult{}
	checkDay(day, legacy, nil, wrong, res)

	assert.Equal(t, 1, res.DaysChecked)
	require.Len(t, res.Mismatches, 1)
	assert.Equal(t, "2026-05-01", res.Mismatches[0].Date)
	assert.NotEqual(t, res.Mismatches[0].Before.Net, res.Mismatches[0].After.Net)
}

// A day that loses its plan across the transform prices at zero, which the
// comparison reports rather than silently accepting — the shape a wrong
// end-date shift would take.
func TestCheckDay_DayLosingItsPlanIsAMismatch(t *testing.T) {
	legacy := []dynamo.LegacyPricingItem{{
		PricingID: "p1", StartDate: "2026-01-01", EndDate: ptrS("2026-05-31"),
		PeakRate: 0.30, FeedInRate: 0.05, OffPeakSavingsRate: 0.28,
	}}
	day := dayEnergy{Date: "2026-05-31", EInput: ptrF(20), EOutput: ptrF(8), PeakGridImportKwh: ptrF(16.5)}

	res := &migrateResult{}
	checkDay(day, legacy, nil, nil, res) // no post-migration plan covers the day

	require.Len(t, res.Mismatches, 1)
	assert.Zero(t, res.Mismatches[0].After.Net)
	assert.NotZero(t, res.Mismatches[0].Before.Net)
}

// The write gate: a failed check must abort before a single row is written,
// regardless of --apply.
func TestApplyMigration_MismatchAbortsBeforeAnyWrite(t *testing.T) {
	f := &fakeDynamo{}
	opts := testOpts()
	opts.apply = true
	res := &migrateResult{
		DaysChecked: 3,
		Mismatches:  []dayMismatch{{Date: "2026-05-01"}},
	}

	err := applyMigration(context.Background(), f, opts,
		[]dynamo.PricingItem{{PricingID: "p1", StartDate: "2026-01-01"}}, res)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrGoldenMismatch)
	assert.Empty(t, f.puts, "a failed check must write nothing")
	assert.Zero(t, res.RowsWritten)
	assert.Contains(t, res.Report[0], "MISMATCH 2026-05-01")
}

// --- Write behaviour ---

func TestMigration_DryRunIsTheDefaultAndWritesNothing(t *testing.T) {
	f := &fakeDynamo{
		pricingItems: []map[string]types.AttributeValue{
			legacyRowAV(t, "p1", "2026-01-01", "", 0.35, 0.05, 0.32),
		},
		energyRows:  []dynamo.DailyEnergyItem{energyRow("2026-05-01", 20, 8, ptrF(16.5))},
		offpeakRows: []dynamo.OffpeakItem{offpeakRow("2026-05-01", 3.5)},
	}

	res, err := runMigration(context.Background(), f, testOpts())

	require.NoError(t, err)
	assert.Empty(t, f.puts)
	assert.Zero(t, res.RowsWritten)
	assert.NotEmpty(t, res.Report, "a dry run still reports what it would do")
}

// AC 5.1: the migrated row carries the free window its historical data was
// computed under, the former flat rate as the default, and the former off-peak
// savings rate — with its id preserved so the sentinel keeps pointing at it.
func TestMigration_ApplyWritesTheBandShapePreservingIDs(t *testing.T) {
	f := &fakeDynamo{
		pricingItems: []map[string]types.AttributeValue{
			legacyRowAV(t, "p1", "2026-01-01", "", 0.35, 0.05, 0.32),
			sentinelAV(t, "p1"),
		},
		energyRows:  []dynamo.DailyEnergyItem{energyRow("2026-05-01", 20, 8, ptrF(16.5))},
		offpeakRows: []dynamo.OffpeakItem{offpeakRow("2026-05-01", 3.5)},
	}
	opts := testOpts()
	opts.apply = true

	res, err := runMigration(context.Background(), f, opts)

	require.NoError(t, err)
	assert.Equal(t, 1, res.RowsWritten)
	require.Len(t, f.puts, 1, "the sentinel row must not be rewritten")

	var got dynamo.PricingItem
	require.NoError(t, attributevalue.UnmarshalMap(f.puts[0].Item, &got))
	assert.Equal(t, "p1", got.PricingID)
	assert.Equal(t, 0.35, got.DefaultRate)
	assert.Equal(t, 0.05, got.FeedInRate)
	require.NotNil(t, got.SavingsReferenceRate)
	assert.Equal(t, 0.32, *got.SavingsReferenceRate)
	assert.Equal(t, []dynamo.PricingWindow{{Start: "11:00", End: "14:00", Free: true}}, got.Windows)
	assert.Nil(t, got.EndDate, "an open-ended row stays open-ended")
	assert.Equal(t, "2025-01-01T00:00:00Z", got.CreatedAt, "createdAt is carried across")

	// The written item must not carry the legacy marker, or a re-run would
	// treat it as unmigrated.
	assert.NotContains(t, f.puts[0].Item, "peakRate")
}

// --- Idempotence ---

func TestMigration_AlreadyMigratedTableIsANoOp(t *testing.T) {
	f := &fakeDynamo{
		pricingItems: []map[string]types.AttributeValue{
			bandRowAV(t, "p1", "2026-01-01", ""),
			sentinelAV(t, "p1"),
		},
		energyRows:  []dynamo.DailyEnergyItem{energyRow("2026-05-01", 20, 8, ptrF(16.5))},
		offpeakRows: []dynamo.OffpeakItem{offpeakRow("2026-05-01", 3.5)},
	}
	opts := testOpts()
	opts.apply = true

	res, err := runMigration(context.Background(), f, opts)

	require.NoError(t, err)
	assert.Zero(t, res.LegacyRows)
	assert.Equal(t, 1, res.AlreadyBandRows)
	assert.Empty(t, f.puts)
	assert.Contains(t, res.Report[0], "already migrated")
}

// A table part-way through migration converts only what is left, and the days
// priced by the untouched band rows are verified band-vs-band and reported.
func TestMigration_MixedTableConvertsOnlyLegacyRows(t *testing.T) {
	f := &fakeDynamo{
		pricingItems: []map[string]types.AttributeValue{
			legacyRowAV(t, "old", "2026-01-01", "2026-05-31", 0.30, 0.05, 0.28),
			bandRowAV(t, "new", "2026-06-01", ""),
		},
		energyRows: []dynamo.DailyEnergyItem{
			energyRow("2026-05-15", 20, 8, ptrF(16.5)),
			energyRow("2026-06-15", 20, 8, ptrF(16.5)),
		},
		offpeakRows: []dynamo.OffpeakItem{
			offpeakRow("2026-05-15", 3.5), offpeakRow("2026-06-15", 3.5),
		},
	}
	opts := testOpts()
	opts.apply = true

	res, err := runMigration(context.Background(), f, opts)

	require.NoError(t, err)
	assert.Equal(t, 1, res.LegacyRows)
	assert.Equal(t, 1, res.AlreadyBandRows)
	assert.Equal(t, 1, res.DaysChecked, "one day is priced by the legacy row")
	assert.Equal(t, 1, res.DaysBandChecked, "one day is priced by the already-band row")
	require.Len(t, f.puts, 1)

	var got dynamo.PricingItem
	require.NoError(t, attributevalue.UnmarshalMap(f.puts[0].Item, &got))
	assert.Equal(t, "old", got.PricingID)
}

// Days no row prices are counted, not compared: they showed no costs before
// the migration and must show none after (AC 2.7).
func TestMigration_UnpricedDaysAreReportedNotChecked(t *testing.T) {
	f := &fakeDynamo{
		pricingItems: []map[string]types.AttributeValue{
			legacyRowAV(t, "p1", "2026-05-01", "", 0.35, 0.05, 0.32),
		},
		energyRows: []dynamo.DailyEnergyItem{
			energyRow("2026-04-01", 20, 8, ptrF(16.5)), // before any plan
			energyRow("2026-05-01", 20, 8, ptrF(16.5)),
		},
		offpeakRows: []dynamo.OffpeakItem{offpeakRow("2026-05-01", 3.5)},
	}

	res, err := runMigration(context.Background(), f, testOpts())

	require.NoError(t, err)
	assert.Equal(t, 1, res.DaysUnpriced)
	assert.Equal(t, 1, res.DaysChecked)
}

// --- Read failures ---

func TestMigration_PricingScanError_IsFatal(t *testing.T) {
	f := &fakeDynamo{scanErr: errors.New("pricing table unreachable")}

	_, err := runMigration(context.Background(), f, testOpts())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "scan pricing")
}

func TestMigration_DailyEnergyQueryError_IsFatal(t *testing.T) {
	f := &fakeDynamo{
		pricingItems: []map[string]types.AttributeValue{
			legacyRowAV(t, "p1", "2026-01-01", "", 0.35, 0.05, 0.32),
		},
		queryErr: errors.New("throttled"),
	}

	_, err := runMigration(context.Background(), f, testOpts())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "query daily energy")
}

// --- Options ---

func TestValidateOpts(t *testing.T) {
	t.Parallel()
	tests := map[string]func(*migrateOpts){
		"serial":       func(o *migrateOpts) { o.serial = "" },
		"pricing":      func(o *migrateOpts) { o.tablePricing = "" },
		"daily energy": func(o *migrateOpts) { o.tableDailyEnergy = "" },
		"offpeak":      func(o *migrateOpts) { o.tableOffpeak = "" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			opts := testOpts()
			mutate(&opts)
			require.Error(t, validateOpts(opts))
		})
	}
	require.NoError(t, validateOpts(testOpts()))
}

// mustPut returns the PutItem whose row carries the given pricingId.
func mustPut(t *testing.T, f *fakeDynamo, id string) *dynamodb.PutItemInput {
	t.Helper()
	for _, p := range f.puts {
		if v, ok := p.Item["pricingId"].(*types.AttributeValueMemberS); ok && v.Value == id {
			return p
		}
	}
	// Dry-run fixtures have no puts; re-run with apply to inspect the payload.
	opts := testOpts()
	opts.apply = true
	_, err := runMigration(context.Background(), f, opts)
	require.NoError(t, err)
	for _, p := range f.puts {
		if v, ok := p.Item["pricingId"].(*types.AttributeValueMemberS); ok && v.Value == id {
			return p
		}
	}
	t.Fatalf("no PutItem for pricingId %q", id)
	return nil
}
