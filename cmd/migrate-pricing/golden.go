package main

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/ArjenSchwarz/flux/internal/plan"
)

// dayEnergy is one retained day's stored energy, joined from the daily-energy
// row and its off-peak row. Pointer fields keep "never recorded" distinct from
// a measured zero — the distinction the legacy formula's table turns on.
type dayEnergy struct {
	Date              string
	EInput            *float64
	EOutput           *float64
	PeakGridImportKwh *float64
	Offpeak           *dynamo.OffpeakItem
	BandImports       []dynamo.BandImportAttr
}

// toDayEnergy converts to the domain cost input, band split included.
func (d dayEnergy) toDayEnergy() plan.DayEnergy {
	out := plan.DayEnergy{
		EInput:            d.EInput,
		EOutput:           d.EOutput,
		PeakGridImportKwh: d.PeakGridImportKwh,
	}
	if d.Offpeak != nil {
		start, end := d.Offpeak.Geometry()
		out.Offpeak = &plan.OffpeakRow{
			GridImportKwh: d.Offpeak.GridUsageKwh,
			WindowStart:   start,
			WindowEnd:     end,
			IntegratedAt:  d.Offpeak.IntegratedAt,
			SampleCount:   d.Offpeak.IntegrationSampleCount,
		}
	}
	for _, b := range d.BandImports {
		out.BandImports = append(out.BandImports, plan.BandImport{Start: b.Start, End: b.End, Kwh: b.Kwh})
	}
	return out
}

// toTierTwoEnergy is toDayEnergy with the band split withheld, forcing the
// single-rate path.
//
// This is what the legacy formula is compared against, and the asymmetry is
// deliberate: the three-rate model has no notion of a per-band split, so
// including one would compare two different questions and flag the known ~1.5%
// gap between the stored peak figure and the summed bands (Q30) as a migration
// defect. The split's own accuracy is the poller's contract; what this check
// owns is whether the row conversion reprices a day.
func (d dayEnergy) toTierTwoEnergy() plan.DayEnergy {
	out := d.toDayEnergy()
	out.BandImports = nil
	return out
}

// legacyDayCosts is the pre-migration three-rate formula, written out in full
// rather than delegated: a golden check that calls the code under test proves
// nothing.
//
// It reproduces the behaviour exactly, including the two parts that a
// simplification would quietly drop — the preference for the server-computed
// peak figure over the eInput − off-peak residual (the two differ by ~1.5% by
// design, Q30), and the zero clamp on that residual.
func legacyDayCosts(row dynamo.LegacyPricingItem, day dayEnergy) plan.Costs {
	total := deref(day.EInput)
	rate := row.PeakRate

	var importCost, savings float64
	switch {
	case day.Offpeak != nil:
		off := day.Offpeak.GridUsageKwh
		peak := max(0, total-off)
		if day.PeakGridImportKwh != nil {
			peak = *day.PeakGridImportKwh
		}
		importCost = peak * rate
		savings = off * row.OffPeakSavingsRate
	case day.PeakGridImportKwh != nil:
		importCost = *day.PeakGridImportKwh * rate
	default:
		importCost = total * rate
	}

	feedIn := deref(day.EOutput) * row.FeedInRate
	return plan.Costs{
		ImportCost:   importCost,
		FeedInIncome: feedIn,
		Net:          importCost - feedIn,
		Savings:      savings,
	}
}

func deref(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}

// loadDays reads every retained daily-energy row for the serial and joins its
// off-peak row. Both tables are queried in full: the migration has to prove
// nothing changed for every day still on record, not a sampled range.
func loadDays(ctx context.Context, client dynamoAPI, opts migrateOpts) ([]dayEnergy, error) {
	energyRows, err := queryAll[dynamo.DailyEnergyItem](ctx, client, opts.tableDailyEnergy, opts.serial)
	if err != nil {
		return nil, fmt.Errorf("query daily energy (%s): %w", opts.tableDailyEnergy, err)
	}
	offpeakRows, err := queryAll[dynamo.OffpeakItem](ctx, client, opts.tableOffpeak, opts.serial)
	if err != nil {
		return nil, fmt.Errorf("query offpeak (%s): %w", opts.tableOffpeak, err)
	}

	byDate := make(map[string]dynamo.OffpeakItem, len(offpeakRows))
	for _, row := range offpeakRows {
		// A pending row carries no finalised deltas, so it cannot price a free
		// window — treat it as absent, exactly as the read endpoints do.
		if row.Status != dynamo.OffpeakStatusComplete {
			continue
		}
		byDate[row.Date] = row
	}

	out := make([]dayEnergy, 0, len(energyRows))
	for _, row := range energyRows {
		day := dayEnergy{
			Date:              row.Date,
			EInput:            ptr(row.EInput),
			EOutput:           ptr(row.EOutput),
			PeakGridImportKwh: row.PeakGridImportKwh,
			BandImports:       row.BandImports,
		}
		if op, ok := byDate[row.Date]; ok {
			day.Offpeak = &op
		}
		out = append(out, day)
	}
	return out, nil
}

func ptr(v float64) *float64 { return &v }

// queryAll pages one table for every row belonging to the serial.
func queryAll[T any](ctx context.Context, client dynamoAPI, table, serial string) ([]T, error) {
	keyCondition := "sysSn = :serial"
	forward := true
	input := &dynamodb.QueryInput{
		TableName:              &table,
		KeyConditionExpression: &keyCondition,
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":serial": &types.AttributeValueMemberS{Value: serial},
		},
		ScanIndexForward: &forward,
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
