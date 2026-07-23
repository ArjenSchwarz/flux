package dynamo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// ErrPricingConcurrentWrite is returned when a sentinel-touching
// transaction is cancelled because another writer flipped the sentinel
// (or the closing row's open-ended state) between the validator scan
// and the transaction. The API handler maps this to HTTP 409
// concurrent_open_ended_write.
var ErrPricingConcurrentWrite = errors.New("pricing: concurrent open-ended write")

// ErrPricingUUIDCollision is returned when a new-row insertion's
// attribute_not_exists(pricingId) ConditionCheck fires. Indicates a UUID
// collision; the handler maps this to HTTP 500 internal_error so the
// caller retries with a fresh UUID.
var ErrPricingUUIDCollision = errors.New("pricing: uuid collision on new row")

// ErrPricingLegacyShape is returned when a write path encounters a row that
// is still the pre-migration three-rate shape. The API handler maps this to
// the legacy_shape validation code (AC 7.3).
var ErrPricingLegacyShape = errors.New("pricing: legacy three-rate row shape")

// sentinelConditionExpression is the ConditionExpression placed on every
// sentinel write. The first clause lets the very first transactional
// write lazily create the sentinel; the second clause catches concurrent
// writers on every subsequent write (Decision 21).
const sentinelConditionExpression = "attribute_not_exists(pricingId) OR openEndedId = :prevOpenEndedID"

// sentinelConditionExpressionWasNull is used when the previous sentinel
// value was nil — DynamoDB cannot equality-compare an absent attribute,
// so we assert attribute_not_exists(openEndedId) instead.
const sentinelConditionExpressionWasNull = "attribute_not_exists(pricingId) OR attribute_not_exists(openEndedId)"

// sentinelUpdate constructs the sentinel-row Update inside a
// TransactWriteItems request. newOpenEndedID is the value to write
// (nil clears the attribute); prevOpenEndedID is the value asserted on
// the row before the write.
func (s *DynamoPricingStore) sentinelUpdate(newOpenEndedID, prevOpenEndedID *string, now string) types.TransactWriteItem {
	expr := "SET openEndedId = :newOpenEndedID, updatedAt = :updatedAt"
	if newOpenEndedID == nil {
		expr = "REMOVE openEndedId SET updatedAt = :updatedAt"
	}

	values := map[string]types.AttributeValue{
		":updatedAt": &types.AttributeValueMemberS{Value: now},
	}
	if newOpenEndedID != nil {
		values[":newOpenEndedID"] = &types.AttributeValueMemberS{Value: *newOpenEndedID}
	}

	cond := sentinelConditionExpressionWasNull
	if prevOpenEndedID != nil {
		cond = sentinelConditionExpression
		values[":prevOpenEndedID"] = &types.AttributeValueMemberS{Value: *prevOpenEndedID}
	}

	return types.TransactWriteItem{
		Update: &types.Update{
			TableName:                 &s.table,
			Key:                       pricingKey(pricingSentinelID),
			UpdateExpression:          &expr,
			ConditionExpression:       &cond,
			ExpressionAttributeValues: values,
		},
	}
}

// rowPutTransactItem builds the Put portion of a pricing-row transact
// write with a uniqueness guard so a UUID collision surfaces as a
// distinct error code at the index-2 position.
func (s *DynamoPricingStore) rowPutTransactItem(item PricingItem) (types.TransactWriteItem, error) {
	av, err := attributevalue.MarshalMap(item)
	if err != nil {
		return types.TransactWriteItem{}, fmt.Errorf("marshal pricing (pricingId=%s): %w", item.PricingID, err)
	}
	cond := "attribute_not_exists(pricingId)"
	return types.TransactWriteItem{
		Put: &types.Put{
			TableName:           &s.table,
			Item:                av,
			ConditionExpression: &cond,
		},
	}, nil
}

// putOpenEndedPeriod inserts a new open-ended pricing row inside a
// TransactWriteItems request, co-maintaining the sentinel row.
func (s *DynamoPricingStore) putOpenEndedPeriod(ctx context.Context, item PricingItem, prevOpenEndedID *string) error {
	rowItem, err := s.rowPutTransactItem(item)
	if err != nil {
		return err
	}
	now := nowRFC3339()
	out := s.sentinelUpdate(&item.PricingID, prevOpenEndedID, now)
	_, err = s.client.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{
		TransactItems: []types.TransactWriteItem{out, rowItem},
	})
	if err != nil {
		return mapTransactionError(err, []reasonHandler{
			// Sentinel race or stale prevOpenEndedID.
			conditionFailedAs(ErrPricingConcurrentWrite),
			// New row UUID collision.
			conditionFailedAs(ErrPricingUUIDCollision),
		}, fmt.Sprintf("put open-ended pricing (table=%s, pricingId=%s)", s.table, item.PricingID))
	}
	return nil
}

// updateOpenEndedTransition handles the three sub-cases that touch the
// open-ended sentinel during an update: closed→open, open→closed, and
// open→open (rate-only edit on the open-ended row). All three issue a
// two-item TransactWriteItems request (sentinel update + row update).
func (s *DynamoPricingStore) updateOpenEndedTransition(ctx context.Context, item PricingItem, prevOpenEndedID *string, isOpen bool) error {
	av, err := attributevalue.MarshalMap(item)
	if err != nil {
		return fmt.Errorf("marshal pricing (pricingId=%s): %w", item.PricingID, err)
	}
	// Row Put with attribute_exists(pricingId) so a deleted-then-updated
	// race surfaces as a transaction-cancel, not a silent re-creation.
	rowCond := "attribute_exists(pricingId)"
	rowItem := types.TransactWriteItem{
		Put: &types.Put{
			TableName:           &s.table,
			Item:                av,
			ConditionExpression: &rowCond,
		},
	}

	var newOpenEndedID *string
	if isOpen {
		newOpenEndedID = &item.PricingID
	}
	now := nowRFC3339()
	sentinel := s.sentinelUpdate(newOpenEndedID, prevOpenEndedID, now)

	_, err = s.client.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{
		TransactItems: []types.TransactWriteItem{sentinel, rowItem},
	})
	if err != nil {
		return mapTransactionError(err, []reasonHandler{
			conditionFailedAs(ErrPricingConcurrentWrite), // [0] sentinel race — openEndedId changed since validator scan
			conditionFailedAs(ErrPricingConcurrentWrite), // [1] row deleted between validator scan and transaction commit
		}, fmt.Sprintf("update pricing transition (table=%s, pricingId=%s)", s.table, item.PricingID))
	}
	return nil
}

// deleteOpenEndedPeriod removes the open-ended pricing row inside a
// TransactWriteItems request, co-maintaining the sentinel row.
func (s *DynamoPricingStore) deleteOpenEndedPeriod(ctx context.Context, id string, prevOpenEndedID *string) error {
	now := nowRFC3339()
	sentinel := s.sentinelUpdate(nil, prevOpenEndedID, now)
	rowCond := "attribute_exists(pricingId)"
	row := types.TransactWriteItem{
		Delete: &types.Delete{
			TableName:           &s.table,
			Key:                 pricingKey(id),
			ConditionExpression: &rowCond,
		},
	}
	_, err := s.client.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{
		TransactItems: []types.TransactWriteItem{sentinel, row},
	})
	if err != nil {
		return mapTransactionError(err, []reasonHandler{
			conditionFailedAs(ErrPricingConcurrentWrite),
			conditionFailedAs(ErrPricingConcurrentWrite),
		}, fmt.Sprintf("delete open-ended pricing (table=%s, pricingId=%s)", s.table, id))
	}
	return nil
}

// ReplaceOpenEnded atomically closes the existing open-ended row at
// closingEndDate and inserts newItem. The sentinel is updated to
// newItem.PricingID when newItem is open-ended, or cleared when newItem
// is closed. Three items per transaction: (1) sentinel, (2) closing-row
// update, (3) new-row insert.
//
// closingEndDate is stored verbatim. Under exclusive end dates (Decision 5)
// the caller passes the successor's start date, so both rows carry the same
// literal switch date and the switch day belongs to the successor (AC 2.2) —
// no date arithmetic happens here or in the caller.
func (s *DynamoPricingStore) ReplaceOpenEnded(ctx context.Context, closingID string, closingEndDate string, updatedAt string, newItem PricingItem) error {
	if err := s.rejectLegacyClosingRow(ctx, closingID); err != nil {
		return err
	}
	prevOpenEndedID := &closingID
	var newOpenEndedID *string
	if newItem.EndDate == nil {
		newOpenEndedID = &newItem.PricingID
	}

	// updatedAt is supplied by the caller so the handler's synthesised
	// response carries the same timestamp DynamoDB persists. Without this
	// the handler's response would diverge from the next GET /pricing
	// read by however long the two time.Now() calls drifted.
	sentinel := s.sentinelUpdate(newOpenEndedID, prevOpenEndedID, updatedAt)

	// Closing-row Update: set the end date, bump updatedAt, but only if
	// the row is still open-ended (attribute_not_exists(endDate)) and
	// still pointing at the supplied id. The attribute_not_exists check
	// catches a concurrent close.
	closingExpr := "SET endDate = :end, updatedAt = :updatedAt"
	closingCond := "attribute_not_exists(endDate) AND pricingId = :closingId"
	closing := types.TransactWriteItem{
		Update: &types.Update{
			TableName:        &s.table,
			Key:              pricingKey(closingID),
			UpdateExpression: &closingExpr,
			ExpressionAttributeValues: map[string]types.AttributeValue{
				":end":       &types.AttributeValueMemberS{Value: closingEndDate},
				":updatedAt": &types.AttributeValueMemberS{Value: updatedAt},
				":closingId": &types.AttributeValueMemberS{Value: closingID},
			},
			ConditionExpression: &closingCond,
		},
	}

	newRow, err := s.rowPutTransactItem(newItem)
	if err != nil {
		return err
	}

	_, err = s.client.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{
		TransactItems: []types.TransactWriteItem{sentinel, closing, newRow},
	})
	if err != nil {
		return mapTransactionError(err, []reasonHandler{
			conditionFailedAs(ErrPricingConcurrentWrite), // sentinel
			conditionFailedAs(ErrPricingConcurrentWrite), // closing row
			conditionFailedAs(ErrPricingUUIDCollision),   // new row
		}, fmt.Sprintf("replace open-ended pricing (table=%s, closingId=%s, newId=%s)", s.table, closingID, newItem.PricingID))
	}
	return nil
}

// rejectLegacyClosingRow refuses a succession whose closing row is still the
// legacy three-rate shape (Q32).
//
// Every other write path issues a full-item Put, but the closing write here
// is a partial UpdateItem: patching a legacy row would leave it still
// legacy-detected while carrying an exclusive end date, which the read
// transform and then the migration would each shift by a day. Rewriting the
// row inside the transaction was rejected as the fix — this call carries no
// predecessor state, so a rewrite needs an extra read and can clobber a
// concurrent edit — and the cutover order already runs the migration first.
//
// A read failure is not treated as "not legacy": the succession is refused so
// a transient blip cannot produce a double-shifted end date.
func (s *DynamoPricingStore) rejectLegacyClosingRow(ctx context.Context, closingID string) error {
	out, err := s.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: &s.table,
		Key:       pricingKey(closingID),
	})
	if err != nil {
		return fmt.Errorf("get closing pricing row (table=%s, pricingId=%s): %w", s.table, closingID, err)
	}
	if out.Item == nil {
		// The row vanished between the caller's validation scan and now.
		// Same outcome the transaction's ConditionExpression would produce.
		return ErrPricingConcurrentWrite
	}
	if IsLegacyPricingRow(out.Item) {
		return fmt.Errorf("%w (pricingId=%s): run cmd/migrate-pricing first", ErrPricingLegacyShape, closingID)
	}
	return nil
}

// reasonHandler interprets one position in a TransactionCanceledException
// Reasons slice. When the position's CancellationReason is a
// ConditionalCheckFailed entry the handler returns the typed error;
// nil means this position is not the failing one.
type reasonHandler func(reason types.CancellationReason) error

// conditionFailedAs returns a reasonHandler that maps a
// ConditionalCheckFailed entry at this position to the given error.
func conditionFailedAs(err error) reasonHandler {
	return func(reason types.CancellationReason) error {
		if reason.Code != nil && *reason.Code == "ConditionalCheckFailed" {
			return err
		}
		return nil
	}
}

// mapTransactionError converts a TransactionCanceledException into a
// typed error using positional reason handlers. Anything that isn't a
// TransactionCanceledException — or that comes back with an empty
// Reasons slice — falls through as a wrapped storage error so the
// handler logs the raw exception and returns HTTP 500.
func mapTransactionError(err error, handlers []reasonHandler, desc string) error {
	var canceled *types.TransactionCanceledException
	if !errors.As(err, &canceled) {
		return fmt.Errorf("%s: %w", desc, err)
	}
	for i, reason := range canceled.CancellationReasons {
		if i >= len(handlers) {
			break
		}
		if mapped := handlers[i](reason); mapped != nil {
			return mapped
		}
	}
	return fmt.Errorf("%s: %w", desc, err)
}

// nowRFC3339 returns the current time as an RFC3339 UTC string. Kept as
// a helper so atomicity tests can pin the format and the API handler
// reuses the same shape.
func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}
