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

// mockDynamoAPI records calls and returns configured responses.
type mockDynamoAPI struct {
	putItemFn        func(ctx context.Context, params *dynamodb.PutItemInput) (*dynamodb.PutItemOutput, error)
	deleteItemFn     func(ctx context.Context, params *dynamodb.DeleteItemInput) (*dynamodb.DeleteItemOutput, error)
	getItemFn        func(ctx context.Context, params *dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error)
	batchWriteItemFn func(ctx context.Context, params *dynamodb.BatchWriteItemInput) (*dynamodb.BatchWriteItemOutput, error)
}

func (m *mockDynamoAPI) PutItem(ctx context.Context, params *dynamodb.PutItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	if m.putItemFn != nil {
		return m.putItemFn(ctx, params)
	}
	return &dynamodb.PutItemOutput{}, nil
}

func (m *mockDynamoAPI) DeleteItem(ctx context.Context, params *dynamodb.DeleteItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error) {
	if m.deleteItemFn != nil {
		return m.deleteItemFn(ctx, params)
	}
	return &dynamodb.DeleteItemOutput{}, nil
}

func (m *mockDynamoAPI) GetItem(ctx context.Context, params *dynamodb.GetItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	if m.getItemFn != nil {
		return m.getItemFn(ctx, params)
	}
	return &dynamodb.GetItemOutput{}, nil
}

func (m *mockDynamoAPI) BatchWriteItem(ctx context.Context, params *dynamodb.BatchWriteItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.BatchWriteItemOutput, error) {
	if m.batchWriteItemFn != nil {
		return m.batchWriteItemFn(ctx, params)
	}
	return &dynamodb.BatchWriteItemOutput{}, nil
}

func testTables() TableNames {
	return TableNames{
		Readings:    "test-readings",
		DailyEnergy: "test-daily-energy",
		DailyPower:  "test-daily-power",
		System:      "test-system",
		Offpeak:     "test-offpeak",
	}
}

func TestDynamoStore_WriteReading(t *testing.T) {
	tests := map[string]struct {
		item    ReadingItem
		putErr  error
		wantErr string
	}{
		"success": {
			item: ReadingItem{SysSn: "AB1234", Timestamp: 1000, Ppv: 3.5},
		},
		"put error wraps context": {
			item:    ReadingItem{SysSn: "AB1234", Timestamp: 1000},
			putErr:  errors.New("throttled"),
			wantErr: "put reading (sysSn=AB1234) (table=test-readings)",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			var gotTable string
			mock := &mockDynamoAPI{
				putItemFn: func(_ context.Context, params *dynamodb.PutItemInput) (*dynamodb.PutItemOutput, error) {
					gotTable = *params.TableName
					return &dynamodb.PutItemOutput{}, tc.putErr
				},
			}
			store := NewDynamoStore(mock, testTables())

			err := store.WriteReading(context.Background(), tc.item)
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "test-readings", gotTable)
		})
	}
}

func TestDynamoStore_WriteDailyEnergy(t *testing.T) {
	tests := map[string]struct {
		item    DailyEnergyItem
		putErr  error
		wantErr string
	}{
		"success": {
			item: DailyEnergyItem{SysSn: "AB1234", Date: "2026-04-13", Epv: 12.5},
		},
		"put error wraps context": {
			item:    DailyEnergyItem{SysSn: "AB1234", Date: "2026-04-13"},
			putErr:  errors.New("throttled"),
			wantErr: "put daily energy (sysSn=AB1234, date=2026-04-13) (table=test-daily-energy)",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			mock := &mockDynamoAPI{
				putItemFn: func(_ context.Context, _ *dynamodb.PutItemInput) (*dynamodb.PutItemOutput, error) {
					return &dynamodb.PutItemOutput{}, tc.putErr
				},
			}
			store := NewDynamoStore(mock, testTables())

			err := store.WriteDailyEnergy(context.Background(), tc.item)
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestDynamoStore_WriteDailyPower(t *testing.T) {
	t.Run("empty items is no-op", func(t *testing.T) {
		mock := &mockDynamoAPI{
			batchWriteItemFn: func(_ context.Context, _ *dynamodb.BatchWriteItemInput) (*dynamodb.BatchWriteItemOutput, error) {
				t.Fatal("should not be called for empty items")
				return nil, nil
			},
		}
		store := NewDynamoStore(mock, testTables())
		require.NoError(t, store.WriteDailyPower(context.Background(), nil))
	})

	t.Run("chunks items into batches of 25", func(t *testing.T) {
		items := make([]DailyPowerItem, 30)
		for i := range items {
			items[i] = DailyPowerItem{SysSn: "SN", UploadTime: "t"}
		}

		var batchCalls int
		var batchSizes []int
		mock := &mockDynamoAPI{
			batchWriteItemFn: func(_ context.Context, params *dynamodb.BatchWriteItemInput) (*dynamodb.BatchWriteItemOutput, error) {
				batchCalls++
				batchSizes = append(batchSizes, len(params.RequestItems["test-daily-power"]))
				return &dynamodb.BatchWriteItemOutput{}, nil
			},
		}
		store := NewDynamoStore(mock, testTables())

		require.NoError(t, store.WriteDailyPower(context.Background(), items))
		assert.Equal(t, 2, batchCalls)
		assert.Equal(t, []int{25, 5}, batchSizes)
	})

	t.Run("retries unprocessed items once", func(t *testing.T) {
		items := []DailyPowerItem{{SysSn: "SN", UploadTime: "t"}}
		av, _ := attributevalue.MarshalMap(items[0])

		calls := 0
		mock := &mockDynamoAPI{
			batchWriteItemFn: func(_ context.Context, _ *dynamodb.BatchWriteItemInput) (*dynamodb.BatchWriteItemOutput, error) {
				calls++
				if calls == 1 {
					return &dynamodb.BatchWriteItemOutput{
						UnprocessedItems: map[string][]types.WriteRequest{
							"test-daily-power": {{PutRequest: &types.PutRequest{Item: av}}},
						},
					}, nil
				}
				return &dynamodb.BatchWriteItemOutput{}, nil
			},
		}
		store := NewDynamoStore(mock, testTables())

		require.NoError(t, store.WriteDailyPower(context.Background(), items))
		assert.Equal(t, 2, calls)
	})

	t.Run("batch write error wraps context", func(t *testing.T) {
		items := []DailyPowerItem{{SysSn: "SN", UploadTime: "t"}}
		mock := &mockDynamoAPI{
			batchWriteItemFn: func(_ context.Context, _ *dynamodb.BatchWriteItemInput) (*dynamodb.BatchWriteItemOutput, error) {
				return nil, errors.New("service unavailable")
			},
		}
		store := NewDynamoStore(mock, testTables())

		err := store.WriteDailyPower(context.Background(), items)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "batch write daily power")
		assert.Contains(t, err.Error(), "test-daily-power")
	})
}

func TestDynamoStore_WriteSystem(t *testing.T) {
	mock := &mockDynamoAPI{}
	store := NewDynamoStore(mock, testTables())

	err := store.WriteSystem(context.Background(), SystemItem{SysSn: "AB1234"})
	require.NoError(t, err)
}

func TestDynamoStore_WriteOffpeak(t *testing.T) {
	mock := &mockDynamoAPI{}
	store := NewDynamoStore(mock, testTables())

	err := store.WriteOffpeak(context.Background(), OffpeakItem{SysSn: "AB1234", Date: "2026-04-13", Status: "pending"})
	require.NoError(t, err)
}

func TestDynamoStore_DeleteOffpeak(t *testing.T) {
	tests := map[string]struct {
		deleteErr error
		wantErr   string
	}{
		"success": {},
		"delete error wraps context": {
			deleteErr: errors.New("not found"),
			wantErr:   "delete offpeak (table=test-offpeak, sysSn=AB1234, date=2026-04-13)",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			var gotKey map[string]types.AttributeValue
			mock := &mockDynamoAPI{
				deleteItemFn: func(_ context.Context, params *dynamodb.DeleteItemInput) (*dynamodb.DeleteItemOutput, error) {
					gotKey = params.Key
					return &dynamodb.DeleteItemOutput{}, tc.deleteErr
				},
			}
			store := NewDynamoStore(mock, testTables())

			err := store.DeleteOffpeak(context.Background(), "AB1234", "2026-04-13")
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "AB1234", gotKey["sysSn"].(*types.AttributeValueMemberS).Value)
			assert.Equal(t, "2026-04-13", gotKey["date"].(*types.AttributeValueMemberS).Value)
		})
	}
}

func TestDynamoStore_GetOffpeak(t *testing.T) {
	t.Run("returns nil when item not found", func(t *testing.T) {
		mock := &mockDynamoAPI{
			getItemFn: func(_ context.Context, _ *dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
				return &dynamodb.GetItemOutput{Item: nil}, nil
			},
		}
		store := NewDynamoStore(mock, testTables())

		got, err := store.GetOffpeak(context.Background(), "AB1234", "2026-04-13")
		require.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("returns item when found", func(t *testing.T) {
		item := OffpeakItem{SysSn: "AB1234", Date: "2026-04-13", Status: "pending", SocStart: 50.0}
		av, _ := attributevalue.MarshalMap(item)

		mock := &mockDynamoAPI{
			getItemFn: func(_ context.Context, _ *dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
				return &dynamodb.GetItemOutput{Item: av}, nil
			},
		}
		store := NewDynamoStore(mock, testTables())

		got, err := store.GetOffpeak(context.Background(), "AB1234", "2026-04-13")
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, "pending", got.Status)
		assert.Equal(t, 50.0, got.SocStart)
	})

	t.Run("get error wraps context", func(t *testing.T) {
		mock := &mockDynamoAPI{
			getItemFn: func(_ context.Context, _ *dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
				return nil, errors.New("timeout")
			},
		}
		store := NewDynamoStore(mock, testTables())

		_, err := store.GetOffpeak(context.Background(), "AB1234", "2026-04-13")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "get offpeak (table=test-offpeak, sysSn=AB1234, date=2026-04-13)")
	})
}
