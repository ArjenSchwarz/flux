package dynamo

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ArjenSchwarz/flux/internal/derivedstats"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withUpdateItem adds an updateItemFn capability to the existing fake.
// (Re-uses the basic mockDynamoAPI but extends via embedding.)
type fakeDynamoAPIv2 struct {
	mockDynamoAPI
	updateItemFn func(ctx context.Context, params *dynamodb.UpdateItemInput) (*dynamodb.UpdateItemOutput, error)
}

func (f *fakeDynamoAPIv2) UpdateItem(ctx context.Context, params *dynamodb.UpdateItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
	if f.updateItemFn != nil {
		return f.updateItemFn(ctx, params)
	}
	return &dynamodb.UpdateItemOutput{}, nil
}

func TestDynamoStore_WriteDailyEnergy_UsesUpdateItem(t *testing.T) {
	var gotInput *dynamodb.UpdateItemInput
	var putCalled bool
	mock := &fakeDynamoAPIv2{
		mockDynamoAPI: mockDynamoAPI{
			putItemFn: func(_ context.Context, _ *dynamodb.PutItemInput) (*dynamodb.PutItemOutput, error) {
				putCalled = true
				return &dynamodb.PutItemOutput{}, nil
			},
		},
		updateItemFn: func(_ context.Context, params *dynamodb.UpdateItemInput) (*dynamodb.UpdateItemOutput, error) {
			gotInput = params
			return &dynamodb.UpdateItemOutput{}, nil
		},
	}

	store := NewDynamoStore(mock, testTables())
	item := DailyEnergyItem{
		SysSn: "AB1234", Date: "2026-04-13",
		Epv: 12.5, EInput: 4.2, EOutput: 2.1, ECharge: 8.0, EDischarge: 6.5, EGridCharge: 1.0,
	}
	require.NoError(t, store.WriteDailyEnergy(context.Background(), item))

	assert.False(t, putCalled, "WriteDailyEnergy must use UpdateItem, not PutItem (Decision 3)")
	require.NotNil(t, gotInput)
	require.NotNil(t, gotInput.UpdateExpression)
	expr := *gotInput.UpdateExpression
	// Each of the six energy attributes must appear in the SET expression.
	for _, name := range []string{"epv", "eInput", "eOutput", "eCharge", "eDischarge", "eGridCharge"} {
		assert.Contains(t, expr, name, "SET expression must update %s", name)
	}
	// derivedStats attributes must NOT appear (the energy writer never touches them).
	for _, name := range []string{"dailyUsage", "socLow", "peakPeriods", "derivedStatsComputedAt"} {
		assert.NotContains(t, expr, name, "energy writer must not touch derivedStats attribute %s", name)
	}
	// Key must be set correctly.
	assert.Equal(t, "AB1234", gotInput.Key["sysSn"].(*types.AttributeValueMemberS).Value)
	assert.Equal(t, "2026-04-13", gotInput.Key["date"].(*types.AttributeValueMemberS).Value)
}

func TestDynamoStore_WriteDailyEnergy_UpdateError(t *testing.T) {
	mock := &fakeDynamoAPIv2{
		updateItemFn: func(_ context.Context, _ *dynamodb.UpdateItemInput) (*dynamodb.UpdateItemOutput, error) {
			return nil, errors.New("throttled")
		},
	}
	store := NewDynamoStore(mock, testTables())
	err := store.WriteDailyEnergy(context.Background(), DailyEnergyItem{SysSn: "AB1234", Date: "2026-04-13"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "daily energy")
	assert.Contains(t, err.Error(), "AB1234")
}

func TestDynamoStore_UpdateDailyEnergyDerived(t *testing.T) {
	avg := 1.2
	stats := DerivedStats{
		DailyUsage: &DailyUsageAttr{
			Blocks: []DailyUsageBlockAttr{
				{
					Kind:              derivedstats.DailyUsageKindNight,
					Start:             "2026-04-12T14:00:00Z",
					End:               "2026-04-12T20:30:00Z",
					TotalKwh:          1.8,
					AverageKwhPerHour: &avg,
					PercentOfDay:      12,
					Status:            derivedstats.DailyUsageStatusComplete,
					BoundarySource:    derivedstats.DailyUsageBoundaryReadings,
				},
			},
		},
		SocLow:                 &SocLowAttr{Soc: 18, Timestamp: "2026-04-12T19:45:00Z"},
		PeakPeriods:            []PeakPeriodAttr{{Start: "2026-04-12T22:00:00Z", End: "2026-04-12T22:30:00Z", AvgLoadW: 3500, EnergyWh: 1750}},
		DerivedStatsComputedAt: "2026-04-13T00:30:00Z",
	}

	var gotInput *dynamodb.UpdateItemInput
	mock := &fakeDynamoAPIv2{
		updateItemFn: func(_ context.Context, params *dynamodb.UpdateItemInput) (*dynamodb.UpdateItemOutput, error) {
			gotInput = params
			return &dynamodb.UpdateItemOutput{}, nil
		},
	}
	store := NewDynamoStore(mock, testTables())
	require.NoError(t, store.UpdateDailyEnergyDerived(context.Background(), "AB1234", "2026-04-12", stats))

	require.NotNil(t, gotInput)
	require.NotNil(t, gotInput.UpdateExpression)
	expr := *gotInput.UpdateExpression
	for _, name := range []string{"dailyUsage", "socLow", "peakPeriods", "derivedStatsComputedAt"} {
		assert.Contains(t, expr, name, "SET expression must update %s", name)
	}
	// Energy attributes must NOT appear (derived writer never touches them).
	for _, name := range []string{"epv", "eInput", "eOutput", "eCharge", "eDischarge", "eGridCharge"} {
		assert.NotContains(t, expr, name, "derived writer must not touch energy attribute %s", name)
	}
	assert.Equal(t, "AB1234", gotInput.Key["sysSn"].(*types.AttributeValueMemberS).Value)
	assert.Equal(t, "2026-04-12", gotInput.Key["date"].(*types.AttributeValueMemberS).Value)
}

func TestDynamoStore_GetDailyEnergy(t *testing.T) {
	t.Run("returns nil when item missing", func(t *testing.T) {
		mock := &fakeDynamoAPIv2{
			mockDynamoAPI: mockDynamoAPI{
				getItemFn: func(_ context.Context, _ *dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
					return &dynamodb.GetItemOutput{Item: nil}, nil
				},
			},
		}
		store := NewDynamoStore(mock, testTables())
		got, err := store.GetDailyEnergy(context.Background(), "AB1234", "2026-04-13")
		require.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("returns item when found", func(t *testing.T) {
		full := DailyEnergyItem{
			SysSn: "AB1234", Date: "2026-04-13", Epv: 12.5, EInput: 4.2,
			DerivedStatsComputedAt: "2026-04-14T00:30:00Z",
			SocLow:                 &SocLowAttr{Soc: 18, Timestamp: "2026-04-13T19:45:00Z"},
		}
		av, err := attributevalue.MarshalMap(full)
		require.NoError(t, err)
		mock := &fakeDynamoAPIv2{
			mockDynamoAPI: mockDynamoAPI{
				getItemFn: func(_ context.Context, _ *dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
					return &dynamodb.GetItemOutput{Item: av}, nil
				},
			},
		}
		store := NewDynamoStore(mock, testTables())
		got, err := store.GetDailyEnergy(context.Background(), "AB1234", "2026-04-13")
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, full.Epv, got.Epv)
		assert.Equal(t, full.DerivedStatsComputedAt, got.DerivedStatsComputedAt)
		require.NotNil(t, got.SocLow)
		assert.Equal(t, full.SocLow.Soc, got.SocLow.Soc)
	})
}

// TestWriteDailyEnergy_StructTagCoverage walks DailyEnergyItem via reflect
// and asserts every non-derivedStats, non-key dynamodbav tag is referenced
// by the WriteDailyEnergy SET expression. The regression guard from the
// design's Q3: a future field added without updating the SET would silently
// drop on every write — this test catches that at build time.
func TestWriteDailyEnergy_StructTagCoverage(t *testing.T) {
	derivedTags := map[string]bool{
		"dailyUsage":             true,
		"socLow":                 true,
		"peakPeriods":            true,
		"derivedStatsComputedAt": true,
	}
	keyTags := map[string]bool{
		"sysSn": true,
		"date":  true,
	}

	var gotInput *dynamodb.UpdateItemInput
	mock := &fakeDynamoAPIv2{
		updateItemFn: func(_ context.Context, params *dynamodb.UpdateItemInput) (*dynamodb.UpdateItemOutput, error) {
			gotInput = params
			return &dynamodb.UpdateItemOutput{}, nil
		},
	}
	store := NewDynamoStore(mock, testTables())
	require.NoError(t, store.WriteDailyEnergy(context.Background(), DailyEnergyItem{SysSn: "x", Date: "y"}))

	require.NotNil(t, gotInput)
	require.NotNil(t, gotInput.UpdateExpression)
	expr := *gotInput.UpdateExpression

	rt := reflect.TypeOf(DailyEnergyItem{})
	for i := range rt.NumField() {
		fld := rt.Field(i)
		tag := fld.Tag.Get("dynamodbav")
		if tag == "" {
			continue
		}
		// Strip ",omitempty" or other options.
		if idx := strings.Index(tag, ","); idx >= 0 {
			tag = tag[:idx]
		}
		if keyTags[tag] || derivedTags[tag] {
			continue
		}
		assert.Contains(t, expr, tag,
			"struct field %s (tag=%q) is missing from WriteDailyEnergy SET expression — adding it without updating WriteDailyEnergy would silently drop the field on every write",
			fld.Name, tag)
	}
}

// TestLogStore_DerivedStatsStubs verifies the dry-run LogStore implements
// the new write methods without crashing.
func TestLogStore_DerivedStatsStubs(t *testing.T) {
	store, _ := newTestLogStore(t)

	t.Run("UpdateDailyEnergyDerived dry-run", func(t *testing.T) {
		err := store.UpdateDailyEnergyDerived(context.Background(), "x", "y", DerivedStats{
			DerivedStatsComputedAt: "2026-04-13T00:00:00Z",
		})
		assert.NoError(t, err)
	})

	t.Run("GetDailyEnergy dry-run returns nil", func(t *testing.T) {
		got, err := store.GetDailyEnergy(context.Background(), "x", "y")
		assert.NoError(t, err)
		assert.Nil(t, got)
	})
}
