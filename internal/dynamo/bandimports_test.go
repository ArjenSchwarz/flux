package dynamo

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sampleBandImports is the rated-segment split of the incoming plan. The free
// band is deliberately absent: its import lives on the flux-offpeak row, which
// owns that quantity exclusively (Q31).
func sampleBandImports() []BandImportAttr {
	return []BandImportAttr{
		{Start: "00:00", End: "01:00", Kwh: 1.2},
		{Start: "01:00", End: "06:00", Kwh: 4.4},
		{Start: "06:00", End: "10:00", Kwh: 2.1},
		{Start: "15:00", End: "24:00", Kwh: 8.3},
	}
}

// TestDailyEnergyItem_BandImportsRoundTrip pins the storage shape: present
// when captured, omitted entirely on rows whose split was never computed.
func TestDailyEnergyItem_BandImportsRoundTrip(t *testing.T) {
	t.Parallel()
	t.Run("captured split round-trips", func(t *testing.T) {
		t.Parallel()
		in := DailyEnergyItem{
			SysSn: "AB1234", Date: "2026-08-02", EInput: 16.0,
			BandImports:     sampleBandImports(),
			BandsComputedAt: "2026-08-03T00:30:00Z",
		}
		av, err := attributevalue.MarshalMap(in)
		require.NoError(t, err)
		assert.Contains(t, av, "bandImports")
		assert.Contains(t, av, "bandsComputedAt")

		var out DailyEnergyItem
		require.NoError(t, attributevalue.UnmarshalMap(av, &out))
		assert.Equal(t, in.BandImports, out.BandImports)
		assert.Equal(t, in.BandsComputedAt, out.BandsComputedAt)
	})

	t.Run("pre-feature row carries neither attribute", func(t *testing.T) {
		t.Parallel()
		av, err := attributevalue.MarshalMap(DailyEnergyItem{SysSn: "AB1234", Date: "2026-04-12"})
		require.NoError(t, err)
		assert.NotContains(t, av, "bandImports")
		assert.NotContains(t, av, "bandsComputedAt")

		var out DailyEnergyItem
		require.NoError(t, attributevalue.UnmarshalMap(av, &out))
		assert.Nil(t, out.BandImports)
		assert.Empty(t, out.BandsComputedAt)
	})
}

// TestUpdateDailyEnergyDerived_BandGroup covers the third sentinel-gated
// group. It follows the peak group's contract (peak-from-readings Decision 3):
// each group is written only when its own sentinel is set, so a pass filling
// one never clobbers another.
func TestUpdateDailyEnergyDerived_BandGroup(t *testing.T) {
	t.Parallel()
	capture := func(t *testing.T, stats DerivedStats) *dynamodb.UpdateItemInput {
		t.Helper()
		var got *dynamodb.UpdateItemInput
		mock := &fakeDynamoAPIv2{
			updateItemFn: func(_ context.Context, params *dynamodb.UpdateItemInput) (*dynamodb.UpdateItemOutput, error) {
				got = params
				return &dynamodb.UpdateItemOutput{}, nil
			},
		}
		store := NewDynamoStore(mock, testTables())
		require.NoError(t, store.UpdateDailyEnergyDerived(context.Background(), "AB1234", "2026-08-02", stats))
		return got
	}

	t.Run("bands only — other groups absent", func(t *testing.T) {
		t.Parallel()
		got := capture(t, DerivedStats{
			BandImports:     sampleBandImports(),
			BandsComputedAt: "2026-08-03T00:30:00Z",
		})
		require.NotNil(t, got)
		expr := *got.UpdateExpression
		assert.Contains(t, expr, "bandImports")
		assert.Contains(t, expr, "bandsComputedAt")
		for _, name := range []string{"dailyUsage", "socLow", "peakPeriods", "derivedStatsComputedAt", "peakGridImportKwh", "peakComputedAt"} {
			assert.NotContains(t, expr, name, "%s must be absent when its sentinel is unset", name)
		}
		_, isList := got.ExpressionAttributeValues[":bi"].(*types.AttributeValueMemberL)
		assert.True(t, isList, "a captured split must marshal as a list")
	})

	t.Run("other groups only — band attributes absent", func(t *testing.T) {
		t.Parallel()
		got := capture(t, DerivedStats{DerivedStatsComputedAt: "2026-08-03T00:30:00Z"})
		require.NotNil(t, got)
		expr := *got.UpdateExpression
		assert.NotContains(t, expr, "bandImports")
		assert.NotContains(t, expr, "bandsComputedAt")
	})

	// Usability gate mirror of PeakGridImportKwh: when the integrator cannot
	// produce every rated segment the value stays absent, but the sentinel is
	// still set so the row is not re-attempted every hour.
	t.Run("nil split with sentinel set marshals as NULL", func(t *testing.T) {
		t.Parallel()
		got := capture(t, DerivedStats{BandsComputedAt: "2026-08-03T00:30:00Z"})
		require.NotNil(t, got)
		expr := *got.UpdateExpression
		assert.Contains(t, expr, "bandsComputedAt")
		_, isNull := got.ExpressionAttributeValues[":bi"].(*types.AttributeValueMemberNULL)
		assert.True(t, isNull, "an unavailable split must marshal as NULL")
	})

	t.Run("all three groups in one call", func(t *testing.T) {
		t.Parallel()
		peak := 12.5
		got := capture(t, DerivedStats{
			DerivedStatsComputedAt: "2026-08-03T00:30:00Z",
			PeakGridImportKwh:      &peak,
			PeakComputedAt:         "2026-08-03T00:30:00Z",
			BandImports:            sampleBandImports(),
			BandsComputedAt:        "2026-08-03T00:30:00Z",
		})
		require.NotNil(t, got)
		expr := *got.UpdateExpression
		for _, name := range []string{"dailyUsage", "socLow", "peakPeriods", "derivedStatsComputedAt", "peakGridImportKwh", "peakComputedAt", "bandImports", "bandsComputedAt"} {
			assert.Contains(t, expr, name)
		}
	})
}

// TestOffpeakItem_WindowGeometryRoundTrip pins the geometry snapshot that
// makes a later free-window edit detectable as a mismatch instead of silently
// mispricing the day.
func TestOffpeakItem_WindowGeometryRoundTrip(t *testing.T) {
	t.Parallel()
	t.Run("snapshot round-trips", func(t *testing.T) {
		t.Parallel()
		in := OffpeakItem{
			SysSn: "AB1234", Date: "2026-08-02", Status: "complete",
			GridUsageKwh: 3.2,
			WindowStart:  "10:00",
			WindowEnd:    "15:00",
		}
		av, err := attributevalue.MarshalMap(in)
		require.NoError(t, err)
		assert.Contains(t, av, "windowStart")
		assert.Contains(t, av, "windowEnd")

		var out OffpeakItem
		require.NoError(t, attributevalue.UnmarshalMap(av, &out))
		assert.Equal(t, "10:00", out.WindowStart)
		assert.Equal(t, "15:00", out.WindowEnd)
	})

	t.Run("pre-feature row carries no geometry", func(t *testing.T) {
		t.Parallel()
		av, err := attributevalue.MarshalMap(OffpeakItem{SysSn: "AB1234", Date: "2026-04-12", Status: "complete"})
		require.NoError(t, err)
		assert.NotContains(t, av, "windowStart")
		assert.NotContains(t, av, "windowEnd")
	})
}

// TestOffpeakItemGeometry covers the pre-feature default: a row with no
// snapshot can only have been computed under 11:00–14:00, the window that was
// configured for its whole lifetime.
func TestOffpeakItemGeometry(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		item      OffpeakItem
		wantStart string
		wantEnd   string
	}{
		"snapshotted":                 {item: OffpeakItem{WindowStart: "10:00", WindowEnd: "15:00"}, wantStart: "10:00", wantEnd: "15:00"},
		"pre-feature row":             {item: OffpeakItem{}, wantStart: "11:00", wantEnd: "14:00"},
		"half-written snapshot":       {item: OffpeakItem{WindowStart: "10:00"}, wantStart: "11:00", wantEnd: "14:00"},
		"half-written snapshot (end)": {item: OffpeakItem{WindowEnd: "15:00"}, wantStart: "11:00", wantEnd: "14:00"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			start, end := tc.item.Geometry()
			assert.Equal(t, tc.wantStart, start)
			assert.Equal(t, tc.wantEnd, end)
		})
	}
}

// TestOffpeakItemUsable pins the sparse-complete rule: a row integrated
// without any samples is a zero-delta artifact, not a measured zero, so it
// cannot be used to price a free band.
func TestOffpeakItemUsable(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		item OffpeakItem
		want bool
	}{
		"integrated with samples":      {item: OffpeakItem{IntegratedAt: "2026-08-02T05:00:00Z", IntegrationSampleCount: 900}, want: true},
		"integrated without samples":   {item: OffpeakItem{IntegratedAt: "2026-08-02T05:00:00Z"}, want: false},
		"pre-integration snapshot row": {item: OffpeakItem{GridUsageKwh: 3.0}, want: true},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, tc.item.Usable())
		})
	}
}
