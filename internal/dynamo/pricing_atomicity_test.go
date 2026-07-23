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

// reasonAt builds a CancellationReasons slice with `code` at position
// idx and `None` everywhere else, mirroring how DynamoDB reports a
// single failing item inside a TransactWriteItems request.
func reasonAt(idx, total int, code string) []types.CancellationReason {
	reasons := make([]types.CancellationReason, total)
	none := "None"
	for i := range reasons {
		reasons[i] = types.CancellationReason{Code: &none}
	}
	reasons[idx] = types.CancellationReason{Code: &code}
	return reasons
}

// canceledWith constructs a TransactionCanceledException carrying the
// given CancellationReasons. Returned via errors.As to match the
// production mapper.
func canceledWith(reasons []types.CancellationReason) error {
	return &types.TransactionCanceledException{
		CancellationReasons: reasons,
	}
}

// newAtomicityMock returns a *mockPricingAPI configured so every
// TransactWriteItems call fails with the supplied error. Used by the
// failure-shape tests to inject a deterministic CancellationReason set.
// The GetItem stub serves the band-shape closing row ReplaceOpenEnded reads
// before it commits; without it every case would fail on the legacy-shape
// guard instead of reaching the transaction it is trying to exercise.
func newAtomicityMock(transactErr error) *mockPricingAPI {
	return &mockPricingAPI{
		getItemFn: func(_ context.Context, params *dynamodb.GetItemInput) (*dynamodb.GetItemOutput, error) {
			id := params.Key["pricingId"].(*types.AttributeValueMemberS).Value
			av, err := attributevalue.MarshalMap(bandPricingItem(id, "2026-01-01", nil))
			if err != nil {
				return nil, err
			}
			return &dynamodb.GetItemOutput{Item: av}, nil
		},
		transactWriteFn: func(_ context.Context, _ *dynamodb.TransactWriteItemsInput) (*dynamodb.TransactWriteItemsOutput, error) {
			return nil, transactErr
		},
	}
}

// Case 1: sentinel race on replace-open-ended (Reasons[0] = ConditionalCheckFailed).
func TestPricingAtomicity_ReplaceOpenEnded_SentinelRace(t *testing.T) {
	mock := newAtomicityMock(canceledWith(reasonAt(0, 3, "ConditionalCheckFailed")))
	store := NewDynamoPricingStore(mock, pricingTestTable())

	err := store.ReplaceOpenEnded(context.Background(), "open-id", "2026-06-30", "2026-05-24T10:00:00Z",
		PricingItem{PricingID: "new-id", StartDate: "2026-07-01", DefaultRate: 0.3})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPricingConcurrentWrite,
		"sentinel position must map to concurrent_open_ended_write")
}

// Case 2: closing-row race on replace-open-ended (Reasons[1] = ConditionalCheckFailed).
func TestPricingAtomicity_ReplaceOpenEnded_ClosingRowRace(t *testing.T) {
	mock := newAtomicityMock(canceledWith(reasonAt(1, 3, "ConditionalCheckFailed")))
	store := NewDynamoPricingStore(mock, pricingTestTable())

	err := store.ReplaceOpenEnded(context.Background(), "open-id", "2026-06-30", "2026-05-24T10:00:00Z",
		PricingItem{PricingID: "new-id", StartDate: "2026-07-01", DefaultRate: 0.3})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPricingConcurrentWrite,
		"closing-row position must map to concurrent_open_ended_write")
}

// Case 3: UUID collision on replace-open-ended new row (Reasons[2] = ConditionalCheckFailed).
func TestPricingAtomicity_ReplaceOpenEnded_UUIDCollision(t *testing.T) {
	mock := newAtomicityMock(canceledWith(reasonAt(2, 3, "ConditionalCheckFailed")))
	store := NewDynamoPricingStore(mock, pricingTestTable())

	err := store.ReplaceOpenEnded(context.Background(), "open-id", "2026-06-30", "2026-05-24T10:00:00Z",
		PricingItem{PricingID: "new-id", StartDate: "2026-07-01", DefaultRate: 0.3})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPricingUUIDCollision,
		"new-row position must map to uuid_collision so the caller retries")
}

// Case 4: TransactionCanceledException with an unpopulated Reasons slice
// (SDK quirk). The mapper must fall through to a wrapped error that the
// handler renders as HTTP 500 with the raw exception logged.
func TestPricingAtomicity_EmptyReasonsFallsThroughTo500(t *testing.T) {
	mock := newAtomicityMock(&types.TransactionCanceledException{})
	store := NewDynamoPricingStore(mock, pricingTestTable())

	err := store.ReplaceOpenEnded(context.Background(), "open-id", "2026-06-30", "2026-05-24T10:00:00Z",
		PricingItem{PricingID: "new-id", StartDate: "2026-07-01", DefaultRate: 0.3})
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrPricingConcurrentWrite,
		"empty Reasons[] must not masquerade as a concurrent_open_ended_write")
	assert.NotErrorIs(t, err, ErrPricingUUIDCollision,
		"empty Reasons[] must not masquerade as a uuid_collision")
}

// Case 5: PutPricing of a new open-ended period — sentinel race
// (openEndedId != null at transaction time).
func TestPricingAtomicity_PutOpenEnded_SentinelRace(t *testing.T) {
	mock := newAtomicityMock(canceledWith(reasonAt(0, 2, "ConditionalCheckFailed")))
	store := NewDynamoPricingStore(mock, pricingTestTable())

	err := store.PutPricing(context.Background(),
		PricingItem{PricingID: "new-open", StartDate: "2026-07-01", DefaultRate: 0.3}, nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPricingConcurrentWrite)
}

// Case 6: UpdatePricing closed → open — sentinel race.
func TestPricingAtomicity_UpdateClosedToOpen_SentinelRace(t *testing.T) {
	mock := newAtomicityMock(canceledWith(reasonAt(0, 2, "ConditionalCheckFailed")))
	store := NewDynamoPricingStore(mock, pricingTestTable())

	// item.EndDate == nil and prevOpenEndedID == nil triggers the
	// closed→open transition in production.
	err := store.UpdatePricing(context.Background(),
		PricingItem{PricingID: "p-1", StartDate: "2026-01-01", DefaultRate: 0.3}, nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPricingConcurrentWrite)
}

// Case 7: UpdatePricing open → closed — sentinel race.
func TestPricingAtomicity_UpdateOpenToClosed_SentinelRace(t *testing.T) {
	mock := newAtomicityMock(canceledWith(reasonAt(0, 2, "ConditionalCheckFailed")))
	store := NewDynamoPricingStore(mock, pricingTestTable())

	end := "2026-06-30"
	openID := "p-1"
	// item.EndDate set + prevOpenEndedID matches item.PricingID — was
	// open, now closed.
	err := store.UpdatePricing(context.Background(),
		PricingItem{PricingID: openID, StartDate: "2026-01-01", EndDate: &end, DefaultRate: 0.3}, &openID)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPricingConcurrentWrite)
}

// Case 8: UpdatePricing open → open (rate-only edit on the open-ended
// row) — sentinel race because a concurrent writer flipped the sentinel.
func TestPricingAtomicity_UpdateOpenToOpen_SentinelRace(t *testing.T) {
	mock := newAtomicityMock(canceledWith(reasonAt(0, 2, "ConditionalCheckFailed")))
	store := NewDynamoPricingStore(mock, pricingTestTable())

	openID := "p-1"
	err := store.UpdatePricing(context.Background(),
		PricingItem{PricingID: openID, StartDate: "2026-01-01", DefaultRate: 0.3}, &openID)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPricingConcurrentWrite)
}

// Case 9: DeletePricing of the open-ended row — sentinel race.
func TestPricingAtomicity_DeleteOpenEnded_SentinelRace(t *testing.T) {
	mock := newAtomicityMock(canceledWith(reasonAt(0, 2, "ConditionalCheckFailed")))
	store := NewDynamoPricingStore(mock, pricingTestTable())

	openID := "p-1"
	err := store.DeletePricing(context.Background(), openID, &openID)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPricingConcurrentWrite)
}

// Case 10: first-write sentinel-creation race — both writers observe
// GetSentinel == nil, both submit a transaction with
// attribute_not_exists(pricingId); one wins, the other gets HTTP 409.
func TestPricingAtomicity_FirstWriteSentinelCreationRace(t *testing.T) {
	mock := newAtomicityMock(canceledWith(reasonAt(0, 2, "ConditionalCheckFailed")))
	store := NewDynamoPricingStore(mock, pricingTestTable())

	// prevOpenEndedID is nil for both callers; the production
	// sentinelUpdate uses the WasNull condition expression, but the
	// mapping treats a sentinel ConditionalCheckFailed at index 0 the
	// same way regardless of which clause fired.
	err := store.PutPricing(context.Background(),
		PricingItem{PricingID: "new-open", StartDate: "2026-07-01", DefaultRate: 0.3}, nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPricingConcurrentWrite)
}

// A non-cancellation transaction error (network, throttle) must wrap as
// a storage error and NOT masquerade as a typed concurrency miss.
func TestPricingAtomicity_NonCancellationErrorWraps(t *testing.T) {
	mock := newAtomicityMock(errors.New("provisioned throughput exceeded"))
	store := NewDynamoPricingStore(mock, pricingTestTable())

	openID := "p-1"
	err := store.DeletePricing(context.Background(), openID, &openID)
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrPricingConcurrentWrite)
	assert.NotErrorIs(t, err, ErrPricingUUIDCollision)
	assert.Contains(t, err.Error(), "provisioned throughput exceeded")
}
