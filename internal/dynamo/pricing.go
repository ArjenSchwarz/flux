package dynamo

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/ArjenSchwarz/flux/internal/plan"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// PricingSentinelID is the partition key of the singleton sentinel row
// that pins which pricing period (if any) is currently open-ended. The
// row never appears in ListPricing output and is maintained inside every
// TransactWriteItems request that introduces, retires, or replaces an
// open-ended period (Decision 21).
//
// Exported because operator tools that scan the raw table (cmd/migrate-pricing)
// must filter the sentinel out by id before any shape detection runs — it is
// keyed, not shaped. A hand-copied literal there would be one edit away from
// the migration treating the sentinel as a pricing row.
const PricingSentinelID = "__open_ended"

// PricingWindow is one stored exception to a plan's default rate. Rate is
// absent on a free window — the domain ignores it there by contract.
type PricingWindow struct {
	Start string   `dynamodbav:"start" json:"start"`                   // HH:MM, Sydney local
	End   string   `dynamodbav:"end" json:"end"`                       // HH:MM, may be "24:00"
	Free  bool     `dynamodbav:"free" json:"free"`                     // true => no rate, valued via savingsReferenceRate
	Rate  *float64 `dynamodbav:"rate,omitempty" json:"rate,omitempty"` // AUD/kWh, 4dp
}

// PricingItem represents one row of the flux-pricing table. PricingID is
// serialised as "id" so the Swift client decodes it through Identifiable.
//
// The row stores the plan as entered — a default rate plus exception windows
// (Decision 4) — not the derived full-day segmentation. EndDate is exclusive
// (Decision 5): the row prices [StartDate, EndDate), so succession writes the
// same literal date to both rows.
type PricingItem struct {
	PricingID            string          `dynamodbav:"pricingId" json:"id"`
	StartDate            string          `dynamodbav:"startDate" json:"startDate"`                 // YYYY-MM-DD, Sydney local calendar
	EndDate              *string         `dynamodbav:"endDate,omitempty" json:"endDate,omitempty"` // exclusive switch date; absent => open-ended
	DefaultRate          float64         `dynamodbav:"defaultRate" json:"defaultRate"`             // AUD/kWh, 4dp
	Windows              []PricingWindow `dynamodbav:"windows" json:"windows"`
	FeedInRate           float64         `dynamodbav:"feedInRate" json:"feedInRate"`                                         // AUD/kWh, 4dp
	SavingsReferenceRate *float64        `dynamodbav:"savingsReferenceRate,omitempty" json:"savingsReferenceRate,omitempty"` // present iff a free window exists
	CreatedAt            string          `dynamodbav:"createdAt" json:"createdAt"`                                           // RFC3339 UTC
	UpdatedAt            string          `dynamodbav:"updatedAt" json:"updatedAt"`                                           // bumped on every write
}

// Plan converts the storage row into the domain plan the validation,
// segmentation, and window-resolution helpers operate on.
func (i PricingItem) Plan() plan.Plan {
	windows := make([]plan.Window, len(i.Windows))
	for n, w := range i.Windows {
		windows[n] = plan.Window{Start: w.Start, End: w.End, Free: w.Free}
		if w.Rate != nil {
			windows[n].Rate = *w.Rate
		}
	}
	end := ""
	if i.EndDate != nil {
		end = *i.EndDate
	}
	return plan.Plan{
		ID:             i.PricingID,
		StartDate:      i.StartDate,
		EndDate:        end,
		DefaultRate:    i.DefaultRate,
		Windows:        windows,
		FeedInRate:     i.FeedInRate,
		SavingsRefRate: i.SavingsReferenceRate,
	}
}

// PlansFromItems converts a list of storage rows to domain plans, ready for
// plan.PlanFor / plan.FreeWindow.
func PlansFromItems(items []PricingItem) []plan.Plan {
	plans := make([]plan.Plan, len(items))
	for i, item := range items {
		plans[i] = item.Plan()
	}
	return plans
}

// LegacyPricingItem is the pre-migration three-rate row. It exists only so
// the transform below has something to decode into; nothing writes this shape
// any more.
type LegacyPricingItem struct {
	PricingID          string  `dynamodbav:"pricingId"`
	StartDate          string  `dynamodbav:"startDate"`
	EndDate            *string `dynamodbav:"endDate,omitempty"` // INCLUSIVE, unlike the band shape
	PeakRate           float64 `dynamodbav:"peakRate"`
	FeedInRate         float64 `dynamodbav:"feedInRate"`
	OffPeakSavingsRate float64 `dynamodbav:"offPeakSavingsRate"`
	CreatedAt          string  `dynamodbav:"createdAt"`
	UpdatedAt          string  `dynamodbav:"updatedAt"`
}

// legacyPricingMarker is the attribute whose presence identifies a
// pre-migration row. The band shape has no such attribute.
const legacyPricingMarker = "peakRate"

// legacyFreeWindow is the window every legacy period's historical off-peak
// data was computed under (AC 5.1). It is the free band the transform gives
// each migrated plan.
var legacyFreeWindow = PricingWindow{Start: "11:00", End: "14:00", Free: true}

// IsLegacyPricingRow reports whether a raw attribute map is the legacy
// three-rate shape.
//
// Detection has to happen on the raw map: attributevalue silently drops
// attributes with no matching struct field, so unmarshalling a legacy row
// into PricingItem yields a zero-rate plan with no windows rather than
// anything recognisably wrong.
func IsLegacyPricingRow(av map[string]types.AttributeValue) bool {
	_, ok := av[legacyPricingMarker]
	return ok
}

// TransformLegacyPricing converts a legacy three-rate period into the band
// model (AC 5.1). It is now used only by cmd/migrate-pricing — the transitional
// read path that shared it is gone. Kept because the tool is idempotent and
// re-runnable, so it stays useful for a restored backup.
//
// The end date shifts from inclusive to exclusive by one day, so the period
// prices exactly the same calendar days before and after migration (AC 5.2).
func TransformLegacyPricing(old LegacyPricingItem) (PricingItem, error) {
	savings := old.OffPeakSavingsRate
	item := PricingItem{
		PricingID:            old.PricingID,
		StartDate:            old.StartDate,
		DefaultRate:          old.PeakRate,
		Windows:              []PricingWindow{legacyFreeWindow},
		FeedInRate:           old.FeedInRate,
		SavingsReferenceRate: &savings,
		CreatedAt:            old.CreatedAt,
		UpdatedAt:            old.UpdatedAt,
	}
	if old.EndDate != nil {
		parsed, err := time.Parse(pricingDateLayout, *old.EndDate)
		if err != nil {
			return PricingItem{}, fmt.Errorf("legacy pricing endDate (pricingId=%s, endDate=%q): %w", old.PricingID, *old.EndDate, err)
		}
		exclusive := parsed.AddDate(0, 0, 1).Format(pricingDateLayout)
		item.EndDate = &exclusive
	}
	return item, nil
}

// pricingDateLayout is the plan date format. Dates are calendar-only, so
// parsing in UTC is sufficient — no wall-clock arithmetic happens here.
const pricingDateLayout = "2006-01-02"

// decodePricingRow unmarshals one raw row into the band shape.
//
// The transitional conversion that used to happen here (Q28) is gone: the
// production migration has run, so every stored row is band-shaped and the
// write path rejects anything else permanently.
//
// A legacy row reaching this point is therefore an error, not something to
// repair silently. It is still detected on the raw attribute map because
// attributevalue drops attributes with no matching struct field — unmarshal
// alone would yield a zero-rate plan with no windows, which prices every day
// at $0.00 rather than failing.
func decodePricingRow(av map[string]types.AttributeValue, desc string) (PricingItem, error) {
	if IsLegacyPricingRow(av) {
		return PricingItem{}, fmt.Errorf("%w: %s carries the pre-migration three-rate shape", ErrPricingLegacyShape, desc)
	}
	var item PricingItem
	if err := attributevalue.UnmarshalMap(av, &item); err != nil {
		return PricingItem{}, fmt.Errorf("unmarshal %s: %w", desc, err)
	}
	return item, nil
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
// pricingListPageLimit caps each Scan page. The spec expects ≤50 rows
// in total over the feature's lifetime, so 200 is comfortably above
// realistic usage while still bounding a pathological case (accidental
// data, runaway test, etc.) from issuing an unbounded scan.
const pricingListPageLimit = 200

func (s *DynamoPricingStore) ListPricing(ctx context.Context) ([]PricingItem, error) {
	return ListPricingRows(ctx, s.client, s.table)
}

// PricingScanAPI is the single DynamoDB call the pricing read needs. The
// operator CLIs read plans without ever writing them, so they satisfy this
// rather than the full PricingAPI — the Lambda keeps sole write access.
type PricingScanAPI interface {
	Scan(ctx context.Context, params *dynamodb.ScanInput, optFns ...func(*dynamodb.Options)) (*dynamodb.ScanOutput, error)
}

// ListPricingRows is the shared implementation behind ListPricing: it pages
// the table, skips the sentinel, and converts legacy rows on the way. Exported
// so the backfill and migration CLIs get identical decoding without building a
// read/write store.
func ListPricingRows(ctx context.Context, client PricingScanAPI, table string) ([]PricingItem, error) {
	items := make([]PricingItem, 0)
	limit := int32(pricingListPageLimit)
	input := &dynamodb.ScanInput{TableName: &table, Limit: &limit}
	for {
		out, err := client.Scan(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("scan pricing (table=%s): %w", table, err)
		}
		for _, av := range out.Items {
			// Skip the sentinel — identified by partition key, not by
			// shape — before attempting to decode into PricingItem.
			if idAV, ok := av["pricingId"].(*types.AttributeValueMemberS); ok && idAV.Value == PricingSentinelID {
				continue
			}
			item, err := decodePricingRow(av, fmt.Sprintf("pricing (table=%s)", table))
			if err != nil {
				return nil, err
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
// It does not use the shared getItem helper because a legacy row needs the
// raw attribute map to be recognised before it is decoded.
func (s *DynamoPricingStore) GetPricing(ctx context.Context, id string) (*PricingItem, error) {
	desc := fmt.Sprintf("pricing (table=%s, pricingId=%s)", s.table, id)
	out, err := s.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: &s.table,
		Key:       pricingKey(id),
	})
	if err != nil {
		return nil, fmt.Errorf("get %s: %w", desc, err)
	}
	if out.Item == nil {
		return nil, nil
	}
	item, err := decodePricingRow(out.Item, desc)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

// GetSentinel returns the sentinel row, or nil when it has not yet been
// provisioned. The first transactional write lazily creates it via a
// ConditionExpression that tolerates the absent state.
func (s *DynamoPricingStore) GetSentinel(ctx context.Context) (*PricingSentinel, error) {
	return getItem[PricingSentinel](ctx, s.client, s.table, pricingKey(PricingSentinelID),
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
