package dynamo

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockReadAPI records calls and returns configured responses.
type mockReadAPI struct {
	queryFn   func(ctx context.Context, params *dynamodb.QueryInput) (*dynamodb.QueryOutput, error)
	getItemFn func(ctx context.Context, params *dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error)
}

func (m *mockReadAPI) Query(ctx context.Context, params *dynamodb.QueryInput, _ ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
	if m.queryFn != nil {
		return m.queryFn(ctx, params)
	}
	return &dynamodb.QueryOutput{}, nil
}

func (m *mockReadAPI) GetItem(ctx context.Context, params *dynamodb.GetItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	if m.getItemFn != nil {
		return m.getItemFn(ctx, params)
	}
	return &dynamodb.GetItemOutput{}, nil
}

func newTestReader(mock *mockReadAPI) *DynamoReader {
	return NewDynamoReader(mock, testTables())
}

func marshalItem(t *testing.T, item any) map[string]types.AttributeValue {
	t.Helper()
	av, err := attributevalue.MarshalMap(item)
	require.NoError(t, err)
	return av
}

func TestDynamoReader_QueryReadings(t *testing.T) {
	tests := map[string]struct {
		queryFn func(ctx context.Context, params *dynamodb.QueryInput) (*dynamodb.QueryOutput, error)
		want    []ReadingItem
		wantErr string
	}{
		"returns readings in order": {
			queryFn: func(_ context.Context, params *dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
				assert.Equal(t, "test-readings", *params.TableName)
				assert.True(t, *params.ScanIndexForward)
				assert.Contains(t, *params.KeyConditionExpression, "BETWEEN")
				return &dynamodb.QueryOutput{
					Items: []map[string]types.AttributeValue{
						marshalItem(t, ReadingItem{SysSn: "SN", Timestamp: 100, Ppv: 1.0}),
						marshalItem(t, ReadingItem{SysSn: "SN", Timestamp: 200, Ppv: 2.0}),
					},
				}, nil
			},
			want: []ReadingItem{
				{SysSn: "SN", Timestamp: 100, Ppv: 1.0},
				{SysSn: "SN", Timestamp: 200, Ppv: 2.0},
			},
		},
		"empty result returns empty slice": {
			queryFn: func(_ context.Context, _ *dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
				return &dynamodb.QueryOutput{}, nil
			},
			want: []ReadingItem{},
		},
		"paginates across multiple pages": {
			queryFn: func() func(ctx context.Context, params *dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
				calls := 0
				return func(_ context.Context, params *dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
					calls++
					if calls == 1 {
						assert.Nil(t, params.ExclusiveStartKey)
						return &dynamodb.QueryOutput{
							Items: []map[string]types.AttributeValue{
								marshalItem(t, ReadingItem{SysSn: "SN", Timestamp: 100}),
							},
							LastEvaluatedKey: map[string]types.AttributeValue{
								"sysSn": &types.AttributeValueMemberS{Value: "SN"},
							},
						}, nil
					}
					assert.NotNil(t, params.ExclusiveStartKey)
					return &dynamodb.QueryOutput{
						Items: []map[string]types.AttributeValue{
							marshalItem(t, ReadingItem{SysSn: "SN", Timestamp: 200}),
						},
					}, nil
				}
			}(),
			want: []ReadingItem{
				{SysSn: "SN", Timestamp: 100},
				{SysSn: "SN", Timestamp: 200},
			},
		},
		"dynamo error wraps context": {
			queryFn: func(_ context.Context, _ *dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
				return nil, errors.New("throttled")
			},
			wantErr: "query readings (table=test-readings)",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			reader := newTestReader(&mockReadAPI{queryFn: tc.queryFn})

			got, err := reader.QueryReadings(context.Background(), "SN", 100, 200)
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestDynamoReader_GetSystem(t *testing.T) {
	tests := map[string]struct {
		getItemFn func(ctx context.Context, params *dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error)
		want      *SystemItem
		wantErr   string
	}{
		"returns system item": {
			getItemFn: func(_ context.Context, params *dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
				assert.Equal(t, "test-system", *params.TableName)
				return &dynamodb.GetItemOutput{
					Item: marshalItem(t, SystemItem{SysSn: "SN", Cobat: 13.34}),
				}, nil
			},
			want: &SystemItem{SysSn: "SN", Cobat: 13.34},
		},
		"returns nil when not found": {
			getItemFn: func(_ context.Context, _ *dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
				return &dynamodb.GetItemOutput{Item: nil}, nil
			},
			want: nil,
		},
		"dynamo error wraps context": {
			getItemFn: func(_ context.Context, _ *dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
				return nil, errors.New("timeout")
			},
			wantErr: "get system (table=test-system, sysSn=SN)",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			reader := newTestReader(&mockReadAPI{getItemFn: tc.getItemFn})

			got, err := reader.GetSystem(context.Background(), "SN")
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestDynamoReader_GetOffpeak(t *testing.T) {
	tests := map[string]struct {
		getItemFn func(ctx context.Context, params *dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error)
		want      *OffpeakItem
		wantErr   string
	}{
		"returns offpeak item": {
			getItemFn: func(_ context.Context, params *dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
				assert.Equal(t, "test-offpeak", *params.TableName)
				return &dynamodb.GetItemOutput{
					Item: marshalItem(t, OffpeakItem{SysSn: "SN", Date: "2026-04-15", Status: "complete"}),
				}, nil
			},
			want: &OffpeakItem{SysSn: "SN", Date: "2026-04-15", Status: "complete"},
		},
		"returns nil when not found": {
			getItemFn: func(_ context.Context, _ *dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
				return &dynamodb.GetItemOutput{Item: nil}, nil
			},
			want: nil,
		},
		"dynamo error wraps context": {
			getItemFn: func(_ context.Context, _ *dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
				return nil, errors.New("timeout")
			},
			wantErr: "get offpeak (table=test-offpeak, sysSn=SN, date=2026-04-15)",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			reader := newTestReader(&mockReadAPI{getItemFn: tc.getItemFn})

			got, err := reader.GetOffpeak(context.Background(), "SN", "2026-04-15")
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestDynamoReader_GetDailyEnergy(t *testing.T) {
	tests := map[string]struct {
		getItemFn func(ctx context.Context, params *dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error)
		want      *DailyEnergyItem
		wantErr   string
	}{
		"returns daily energy item": {
			getItemFn: func(_ context.Context, params *dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
				assert.Equal(t, "test-daily-energy", *params.TableName)
				return &dynamodb.GetItemOutput{
					Item: marshalItem(t, DailyEnergyItem{SysSn: "SN", Date: "2026-04-15", Epv: 12.5}),
				}, nil
			},
			want: &DailyEnergyItem{SysSn: "SN", Date: "2026-04-15", Epv: 12.5},
		},
		"returns nil when not found": {
			getItemFn: func(_ context.Context, _ *dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
				return &dynamodb.GetItemOutput{Item: nil}, nil
			},
			want: nil,
		},
		"dynamo error wraps context": {
			getItemFn: func(_ context.Context, _ *dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
				return nil, errors.New("timeout")
			},
			wantErr: "get daily energy (table=test-daily-energy, sysSn=SN, date=2026-04-15)",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			reader := newTestReader(&mockReadAPI{getItemFn: tc.getItemFn})

			got, err := reader.GetDailyEnergy(context.Background(), "SN", "2026-04-15")
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestDynamoReader_QueryDailyEnergy(t *testing.T) {
	tests := map[string]struct {
		queryFn func(ctx context.Context, params *dynamodb.QueryInput) (*dynamodb.QueryOutput, error)
		want    []DailyEnergyItem
		wantErr string
	}{
		"returns daily energy items for date range": {
			queryFn: func(_ context.Context, params *dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
				assert.Equal(t, "test-daily-energy", *params.TableName)
				assert.True(t, *params.ScanIndexForward)
				assert.Contains(t, *params.KeyConditionExpression, "BETWEEN")
				return &dynamodb.QueryOutput{
					Items: []map[string]types.AttributeValue{
						marshalItem(t, DailyEnergyItem{SysSn: "SN", Date: "2026-04-14", Epv: 10.0}),
						marshalItem(t, DailyEnergyItem{SysSn: "SN", Date: "2026-04-15", Epv: 12.0}),
					},
				}, nil
			},
			want: []DailyEnergyItem{
				{SysSn: "SN", Date: "2026-04-14", Epv: 10.0},
				{SysSn: "SN", Date: "2026-04-15", Epv: 12.0},
			},
		},
		"empty result returns empty slice": {
			queryFn: func(_ context.Context, _ *dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
				return &dynamodb.QueryOutput{}, nil
			},
			want: []DailyEnergyItem{},
		},
		"dynamo error wraps context": {
			queryFn: func(_ context.Context, _ *dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
				return nil, errors.New("throttled")
			},
			wantErr: "query daily energy (table=test-daily-energy)",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			reader := newTestReader(&mockReadAPI{queryFn: tc.queryFn})

			got, err := reader.QueryDailyEnergy(context.Background(), "SN", "2026-04-14", "2026-04-15")
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestDynamoReader_QueryOffpeak(t *testing.T) {
	tests := map[string]struct {
		queryFn func(ctx context.Context, params *dynamodb.QueryInput) (*dynamodb.QueryOutput, error)
		want    []OffpeakItem
		wantErr string
	}{
		"returns offpeak items for date range": {
			queryFn: func(_ context.Context, params *dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
				assert.Equal(t, "test-offpeak", *params.TableName)
				assert.True(t, *params.ScanIndexForward)
				assert.Contains(t, *params.KeyConditionExpression, "BETWEEN")
				return &dynamodb.QueryOutput{
					Items: []map[string]types.AttributeValue{
						marshalItem(t, OffpeakItem{SysSn: "SN", Date: "2026-04-14", Status: OffpeakStatusComplete, GridUsageKwh: 2.5}),
						marshalItem(t, OffpeakItem{SysSn: "SN", Date: "2026-04-15", Status: OffpeakStatusPending, StartEInput: 1.2}),
					},
				}, nil
			},
			want: []OffpeakItem{
				{SysSn: "SN", Date: "2026-04-14", Status: OffpeakStatusComplete, GridUsageKwh: 2.5},
				{SysSn: "SN", Date: "2026-04-15", Status: OffpeakStatusPending, StartEInput: 1.2},
			},
		},
		"empty result returns empty slice": {
			queryFn: func(_ context.Context, _ *dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
				return &dynamodb.QueryOutput{}, nil
			},
			want: []OffpeakItem{},
		},
		"dynamo error wraps context": {
			queryFn: func(_ context.Context, _ *dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
				return nil, errors.New("throttled")
			},
			wantErr: "query offpeak (table=test-offpeak)",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			reader := newTestReader(&mockReadAPI{queryFn: tc.queryFn})

			got, err := reader.QueryOffpeak(context.Background(), "SN", "2026-04-14", "2026-04-15")
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestDynamoReader_GetNote(t *testing.T) {
	tests := map[string]struct {
		getItemFn func(ctx context.Context, params *dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error)
		want      *NoteItem
		wantErr   string
	}{
		"returns nil when not found": {
			getItemFn: func(_ context.Context, _ *dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
				return &dynamodb.GetItemOutput{Item: nil}, nil
			},
			want: nil,
		},
		"returns note when present": {
			getItemFn: func(_ context.Context, params *dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
				assert.Equal(t, "test-notes", *params.TableName)
				return &dynamodb.GetItemOutput{
					Item: marshalItem(t, NoteItem{
						SysSn: "SN", Date: "2026-04-15",
						Text: "Away in Bali", UpdatedAt: "2026-04-15T01:23:45Z",
					}),
				}, nil
			},
			want: &NoteItem{SysSn: "SN", Date: "2026-04-15", Text: "Away in Bali", UpdatedAt: "2026-04-15T01:23:45Z"},
		},
		"dynamo error wraps context": {
			getItemFn: func(_ context.Context, _ *dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
				return nil, errors.New("timeout")
			},
			wantErr: "get note (table=test-notes, sysSn=SN, date=2026-04-15)",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			reader := newTestReader(&mockReadAPI{getItemFn: tc.getItemFn})

			got, err := reader.GetNote(context.Background(), "SN", "2026-04-15")
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestDynamoReader_QueryNotes(t *testing.T) {
	tests := map[string]struct {
		queryFn func(ctx context.Context, params *dynamodb.QueryInput) (*dynamodb.QueryOutput, error)
		want    []NoteItem
		wantErr string
	}{
		"returns notes in date order": {
			queryFn: func(_ context.Context, params *dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
				assert.Equal(t, "test-notes", *params.TableName)
				assert.True(t, *params.ScanIndexForward, "must scan ascending so callers get chronological order")
				assert.Contains(t, *params.KeyConditionExpression, "BETWEEN")
				return &dynamodb.QueryOutput{
					Items: []map[string]types.AttributeValue{
						marshalItem(t, NoteItem{SysSn: "SN", Date: "2026-04-13", Text: "first"}),
						marshalItem(t, NoteItem{SysSn: "SN", Date: "2026-04-15", Text: "third"}),
					},
				}, nil
			},
			want: []NoteItem{
				{SysSn: "SN", Date: "2026-04-13", Text: "first"},
				{SysSn: "SN", Date: "2026-04-15", Text: "third"},
			},
		},
		"empty range returns empty slice": {
			queryFn: func(_ context.Context, _ *dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
				return &dynamodb.QueryOutput{}, nil
			},
			want: []NoteItem{},
		},
		"dynamo error wraps context": {
			queryFn: func(_ context.Context, _ *dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
				return nil, errors.New("throttled")
			},
			wantErr: "query notes (table=test-notes)",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			reader := newTestReader(&mockReadAPI{queryFn: tc.queryFn})

			got, err := reader.QueryNotes(context.Background(), "SN", "2026-04-13", "2026-04-15")
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestDynamoReader_QueryDailyPower(t *testing.T) {
	tests := map[string]struct {
		queryFn func(ctx context.Context, params *dynamodb.QueryInput) (*dynamodb.QueryOutput, error)
		want    []DailyPowerItem
		wantErr string
	}{
		"returns daily power items with begins_with condition": {
			queryFn: func(_ context.Context, params *dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
				assert.Equal(t, "test-daily-power", *params.TableName)
				assert.True(t, *params.ScanIndexForward)
				assert.Contains(t, *params.KeyConditionExpression, "begins_with")
				return &dynamodb.QueryOutput{
					Items: []map[string]types.AttributeValue{
						marshalItem(t, DailyPowerItem{SysSn: "SN", UploadTime: "2026-04-15 10:00:00", Cbat: 80.0}),
					},
				}, nil
			},
			want: []DailyPowerItem{
				{SysSn: "SN", UploadTime: "2026-04-15 10:00:00", Cbat: 80.0},
			},
		},
		"empty result returns empty slice": {
			queryFn: func(_ context.Context, _ *dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
				return &dynamodb.QueryOutput{}, nil
			},
			want: []DailyPowerItem{},
		},
		"dynamo error wraps context": {
			queryFn: func(_ context.Context, _ *dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
				return nil, errors.New("throttled")
			},
			wantErr: "query daily power (table=test-daily-power)",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			reader := newTestReader(&mockReadAPI{queryFn: tc.queryFn})

			got, err := reader.QueryDailyPower(context.Background(), "SN", "2026-04-15")
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
