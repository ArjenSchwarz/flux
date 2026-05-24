package dynamo

import (
	"context"
	"fmt"
	"sort"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// pricingSentinelID is the partition key of the singleton sentinel row
// that pins which pricing period (if any) is currently open-ended. The
// row never appears in ListPricing output and is maintained inside every
// TransactWriteItems request that introduces, retires, or replaces an
// open-ended period (Decision 21).
const pricingSentinelID = "__open_ended"

// PricingItem represents one row of the flux-pricing table. PricingID is
// serialised as "id" so the Swift client decodes it through Identifiable.
type PricingItem struct {
	PricingID          string  `dynamodbav:"pricingId" json:"id"`
	StartDate          string  `dynamodbav:"startDate" json:"startDate"`                   // YYYY-MM-DD, Melbourne local calendar
	EndDate            *string `dynamodbav:"endDate,omitempty" json:"endDate,omitempty"`   // absent => open-ended
	PeakRate           float64 `dynamodbav:"peakRate" json:"peakRate"`                     // AUD/kWh, 4dp
	FeedInRate         float64 `dynamodbav:"feedInRate" json:"feedInRate"`                 // AUD/kWh, 4dp
	OffPeakSavingsRate float64 `dynamodbav:"offPeakSavingsRate" json:"offPeakSavingsRate"` // AUD/kWh, 4dp
	CreatedAt          string  `dynamodbav:"createdAt" json:"createdAt"`                   // RFC3339 UTC
	UpdatedAt          string  `dynamodbav:"updatedAt" json:"updatedAt"`                   // bumped on every write
}

// PricingSentinel is the singleton row (pricingId = "__open_ended") whose
// OpenEndedID attribute points at the pricing row that currently has no
// end date — or is absent when no open-ended period exists. Every write
// that introduces, retires, or replaces an open-ended period maintains
// this row inside the same TransactWriteItems request so AC 1.9 ("at
// most one open-ended period") survives concurrent writers.
type PricingSentinel struct {
	PricingID   string  `dynamodbav:"pricingId"`
	OpenEndedID *string `dynamodbav:"openEndedId,omitempty"`
	UpdatedAt   string  `dynamodbav:"updatedAt"`
}

// PricingReadAPI is the read surface exposed to the API handler.
// ListPricing excludes the sentinel row. GetSentinel returns nil before
// the sentinel has been lazily provisioned (treated as "no open-ended
// period exists" by the validator).
type PricingReadAPI interface {
	ListPricing(ctx context.Context) ([]PricingItem, error)
	GetPricing(ctx context.Context, id string) (*PricingItem, error)
	GetSentinel(ctx context.Context) (*PricingSentinel, error)
}

// PricingWriteAPI is the write surface exposed to the API handler.
// prevOpenEndedID is the sentinel's openEndedId value the validator
// captured just before the write; transactional writes use it inside a
// ConditionExpression so concurrent writers race the sentinel.
type PricingWriteAPI interface {
	PutPricing(ctx context.Context, item PricingItem, prevOpenEndedID *string) error
	UpdatePricing(ctx context.Context, item PricingItem, prevOpenEndedID *string) error
	DeletePricing(ctx context.Context, id string, prevOpenEndedID *string) error
	ReplaceOpenEnded(ctx context.Context, closingID string, closingEndDate string, updatedAt string, newItem PricingItem) error
}

// PricingAPI is the subset of the DynamoDB client used by the pricing
// store. The live *dynamodb.Client satisfies every method.
type PricingAPI interface {
	PutItem(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
	GetItem(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
	UpdateItem(ctx context.Context, params *dynamodb.UpdateItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error)
	DeleteItem(ctx context.Context, params *dynamodb.DeleteItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error)
	Scan(ctx context.Context, params *dynamodb.ScanInput, optFns ...func(*dynamodb.Options)) (*dynamodb.ScanOutput, error)
	TransactWriteItems(ctx context.Context, params *dynamodb.TransactWriteItemsInput, optFns ...func(*dynamodb.Options)) (*dynamodb.TransactWriteItemsOutput, error)
}

// DynamoPricingStore implements both PricingReadAPI and PricingWriteAPI
// against a real DynamoDB table.
type DynamoPricingStore struct {
	client PricingAPI
	table  string
}

// NewDynamoPricingStore returns a store scoped to the given table name.
func NewDynamoPricingStore(client PricingAPI, table string) *DynamoPricingStore {
	return &DynamoPricingStore{client: client, table: table}
}

// pricingKey returns the DynamoDB key for a pricing row.
func pricingKey(id string) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{
		"pricingId": &types.AttributeValueMemberS{Value: id},
	}
}

// ListPricing returns every pricing row sorted by StartDate ascending.
// The sentinel row (pricingId = "__open_ended") is filtered out so the
// API never exposes it to clients.
func (s *DynamoPricingStore) ListPricing(ctx context.Context) ([]PricingItem, error) {
	items := make([]PricingItem, 0)
	input := &dynamodb.ScanInput{TableName: &s.table}
	for {
		out, err := s.client.Scan(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("scan pricing (table=%s): %w", s.table, err)
		}
		for _, av := range out.Items {
			// Skip the sentinel — identified by partition key, not by
			// shape — before attempting to decode into PricingItem.
			if idAV, ok := av["pricingId"].(*types.AttributeValueMemberS); ok && idAV.Value == pricingSentinelID {
				continue
			}
			var item PricingItem
			if err := attributevalue.UnmarshalMap(av, &item); err != nil {
				return nil, fmt.Errorf("unmarshal pricing (table=%s): %w", s.table, err)
			}
			items = append(items, item)
		}
		if out.LastEvaluatedKey == nil {
			break
		}
		input.ExclusiveStartKey = out.LastEvaluatedKey
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].StartDate < items[j].StartDate
	})
	return items, nil
}

// GetPricing returns the pricing row with the given id, or nil if absent.
func (s *DynamoPricingStore) GetPricing(ctx context.Context, id string) (*PricingItem, error) {
	return getItem[PricingItem](ctx, s.client, s.table, pricingKey(id),
		fmt.Sprintf("pricing (table=%s, pricingId=%s)", s.table, id),
	)
}

// GetSentinel returns the sentinel row, or nil when it has not yet been
// provisioned. The first transactional write lazily creates it via a
// ConditionExpression that tolerates the absent state.
func (s *DynamoPricingStore) GetSentinel(ctx context.Context) (*PricingSentinel, error) {
	return getItem[PricingSentinel](ctx, s.client, s.table, pricingKey(pricingSentinelID),
		fmt.Sprintf("pricing sentinel (table=%s)", s.table),
	)
}

// PutPricing inserts a new pricing row. For a closed period it issues a
// plain PutItem; for an open-ended period it co-writes the sentinel
// inside a TransactWriteItems request with a ConditionExpression on the
// sentinel's previous value (Decision 21).
func (s *DynamoPricingStore) PutPricing(ctx context.Context, item PricingItem, prevOpenEndedID *string) error {
	if item.EndDate != nil {
		return s.putClosedPeriod(ctx, item)
	}
	return s.putOpenEndedPeriod(ctx, item, prevOpenEndedID)
}

func (s *DynamoPricingStore) putClosedPeriod(ctx context.Context, item PricingItem) error {
	av, err := attributevalue.MarshalMap(item)
	if err != nil {
		return fmt.Errorf("marshal pricing (pricingId=%s): %w", item.PricingID, err)
	}
	cond := "attribute_not_exists(pricingId)"
	_, err = s.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           &s.table,
		Item:                av,
		ConditionExpression: &cond,
	})
	if err != nil {
		return fmt.Errorf("put pricing (table=%s, pricingId=%s): %w", s.table, item.PricingID, err)
	}
	return nil
}

// UpdatePricing updates an existing pricing row. The behaviour depends
// on the pre/post open-ended state:
//
//   - closed → closed: plain UpdateItem.
//   - closed → open:   TransactWriteItems (sentinel null → rowID, row update).
//   - open → closed:   TransactWriteItems (sentinel rowID → null, row update).
//   - open → open:     TransactWriteItems (sentinel rowID → rowID guard, row update).
//
// prevOpenEndedID is the sentinel's openEndedId value captured by the
// validator just before this write.
func (s *DynamoPricingStore) UpdatePricing(ctx context.Context, item PricingItem, prevOpenEndedID *string) error {
	wasOpen := prevOpenEndedID != nil && *prevOpenEndedID == item.PricingID
	isOpen := item.EndDate == nil

	if !wasOpen && !isOpen {
		return s.updateClosedToClosed(ctx, item)
	}
	return s.updateOpenEndedTransition(ctx, item, prevOpenEndedID, isOpen)
}

func (s *DynamoPricingStore) updateClosedToClosed(ctx context.Context, item PricingItem) error {
	av, err := attributevalue.MarshalMap(item)
	if err != nil {
		return fmt.Errorf("marshal pricing (pricingId=%s): %w", item.PricingID, err)
	}
	// Overwriting the row preserves correctness because the caller has
	// already fetched the existing row to discover createdAt.
	cond := "attribute_exists(pricingId)"
	_, err = s.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           &s.table,
		Item:                av,
		ConditionExpression: &cond,
	})
	if err != nil {
		return fmt.Errorf("update pricing (table=%s, pricingId=%s): %w", s.table, item.PricingID, err)
	}
	return nil
}

// DeletePricing removes a pricing row. For a closed period it issues a
// plain DeleteItem; for the open-ended period it co-writes the sentinel
// (rowID → null) inside a TransactWriteItems request.
func (s *DynamoPricingStore) DeletePricing(ctx context.Context, id string, prevOpenEndedID *string) error {
	deletingOpenEnded := prevOpenEndedID != nil && *prevOpenEndedID == id
	if !deletingOpenEnded {
		_, err := s.client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
			TableName: &s.table,
			Key:       pricingKey(id),
		})
		if err != nil {
			return fmt.Errorf("delete pricing (table=%s, pricingId=%s): %w", s.table, id, err)
		}
		return nil
	}
	return s.deleteOpenEndedPeriod(ctx, id, prevOpenEndedID)
}
