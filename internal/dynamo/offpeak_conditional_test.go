package dynamo

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDynamoStore_WriteOffpeakIfPendingOrAbsent covers the poller's window-end
// finalisation write (AC 3.5). The conditional expression must allow writes
// when the row does not exist OR the existing row has status="pending"; it
// must fail with ErrOffpeakConditionFailed when the row already has
// status="complete" (the backfill CLI or a peer poller got there first).
func TestDynamoStore_WriteOffpeakIfPendingOrAbsent(t *testing.T) {
	tests := map[string]struct {
		putErr       error
		wantErr      error
		wantSentinel bool
	}{
		"succeeds when row absent (condition true)": {
			putErr: nil,
		},
		"succeeds when row has status=pending (condition true)": {
			putErr: nil,
		},
		"fails with sentinel when row has status=complete": {
			putErr:       &types.ConditionalCheckFailedException{},
			wantSentinel: true,
		},
		"wraps non-condition errors": {
			putErr:  errors.New("throttled"),
			wantErr: errors.New("throttled"),
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			var gotCond string
			var gotValues map[string]types.AttributeValue
			mock := &mockDynamoAPI{
				putItemFn: func(_ context.Context, params *dynamodb.PutItemInput) (*dynamodb.PutItemOutput, error) {
					if params.ConditionExpression != nil {
						gotCond = *params.ConditionExpression
					}
					gotValues = params.ExpressionAttributeValues
					return &dynamodb.PutItemOutput{}, tc.putErr
				},
			}
			store := NewDynamoStore(mock, testTables())

			err := store.WriteOffpeakIfPendingOrAbsent(context.Background(), OffpeakItem{
				SysSn:  "AB1234",
				Date:   "2026-05-18",
				Status: OffpeakStatusComplete,
			})
			if tc.wantSentinel {
				require.Error(t, err)
				assert.ErrorIs(t, err, ErrOffpeakConditionFailed)
				return
			}
			if tc.wantErr != nil {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr.Error())
				return
			}
			require.NoError(t, err)
			// Condition must reference both branches per AC 3.5: absent OR pending.
			assert.Contains(t, gotCond, "attribute_not_exists")
			assert.Contains(t, gotCond, "pending")
			// Status placeholder is bound to "pending" for the OR branch.
			require.Contains(t, gotValues, ":pending")
			assert.Equal(t, OffpeakStatusPending,
				gotValues[":pending"].(*types.AttributeValueMemberS).Value)
		})
	}
}

// TestDynamoStore_WriteOffpeakIfComplete covers the backfill CLI's write
// (AC 7.8). The conditional expression must accept writes only when the
// existing row's status is already "complete"; it must fail with the sentinel
// when the row is absent OR has status="pending" (mid-poll). The same write
// must succeed when a previously-written complete row is re-finalised — that
// is the AC 7.3 idempotence path.
func TestDynamoStore_WriteOffpeakIfComplete(t *testing.T) {
	tests := map[string]struct {
		putErr       error
		wantSentinel bool
		wantErr      error
	}{
		"succeeds when row has status=complete (condition true)": {
			putErr: nil,
		},
		"fails with sentinel when row absent": {
			putErr:       &types.ConditionalCheckFailedException{},
			wantSentinel: true,
		},
		"fails with sentinel when row has status=pending": {
			putErr:       &types.ConditionalCheckFailedException{},
			wantSentinel: true,
		},
		"wraps non-condition errors": {
			putErr:  errors.New("provisioned throughput exceeded"),
			wantErr: errors.New("provisioned throughput exceeded"),
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			var gotCond string
			var gotValues map[string]types.AttributeValue
			mock := &mockDynamoAPI{
				putItemFn: func(_ context.Context, params *dynamodb.PutItemInput) (*dynamodb.PutItemOutput, error) {
					if params.ConditionExpression != nil {
						gotCond = *params.ConditionExpression
					}
					gotValues = params.ExpressionAttributeValues
					return &dynamodb.PutItemOutput{}, tc.putErr
				},
			}
			store := NewDynamoStore(mock, testTables())

			err := store.WriteOffpeakIfComplete(context.Background(), OffpeakItem{
				SysSn:  "AB1234",
				Date:   "2026-05-18",
				Status: OffpeakStatusComplete,
			})
			if tc.wantSentinel {
				require.Error(t, err)
				assert.ErrorIs(t, err, ErrOffpeakConditionFailed)
				return
			}
			if tc.wantErr != nil {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr.Error())
				return
			}
			require.NoError(t, err)
			// Condition must require status = complete per AC 7.8.
			assert.Contains(t, gotCond, "complete")
			require.Contains(t, gotValues, ":complete")
			assert.Equal(t, OffpeakStatusComplete,
				gotValues[":complete"].(*types.AttributeValueMemberS).Value)
		})
	}
}

// TestDynamoStore_QueryReadingsConsistent covers the poller's strongly-
// consistent readings query (Design "Error Handling" → race entry, AC 3.5
// support). The same KeyConditionExpression as the eventually-consistent
// QueryReadings, plus ConsistentRead=true on the Query input.
//
// Implementation choice: a sibling method on Store rather than an opts
// struct extension of QueryReadings. Less invasive — existing callers
// (api.DynamoReader, poller summarisation, LogStore dry-run) keep their
// current eventually-consistent path unchanged.
func TestDynamoStore_QueryReadingsConsistent(t *testing.T) {
	var gotConsistent *bool
	var gotKeyCond string
	mock := &mockDynamoAPI{
		queryFn: func(_ context.Context, params *dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
			gotConsistent = params.ConsistentRead
			if params.KeyConditionExpression != nil {
				gotKeyCond = *params.KeyConditionExpression
			}
			return &dynamodb.QueryOutput{}, nil
		},
	}
	store := NewDynamoStore(mock, testTables())

	_, err := store.QueryReadingsConsistent(context.Background(), "SN", 100, 200)
	require.NoError(t, err)
	require.NotNil(t, gotConsistent)
	assert.True(t, *gotConsistent, "ConsistentRead must be set true for the poller's window-end query")
	// Sanity: same key condition as the eventually-consistent sibling.
	assert.Contains(t, gotKeyCond, "BETWEEN")
}

// TestDynamoStore_QueryReadings_EventuallyConsistent guards the existing
// eventually-consistent path: ConsistentRead must remain nil (or false) so
// the API Lambda's reader keeps its current behaviour.
func TestDynamoStore_QueryReadings_EventuallyConsistent(t *testing.T) {
	var gotConsistent *bool
	mock := &mockDynamoAPI{
		queryFn: func(_ context.Context, params *dynamodb.QueryInput) (*dynamodb.QueryOutput, error) {
			gotConsistent = params.ConsistentRead
			return &dynamodb.QueryOutput{}, nil
		},
	}
	store := NewDynamoStore(mock, testTables())

	_, err := store.QueryReadings(context.Background(), "SN", 100, 200)
	require.NoError(t, err)
	// nil or false — both mean eventually-consistent. Production never opts in.
	if gotConsistent != nil {
		assert.False(t, *gotConsistent)
	}
}
