package dynamo

import (
	"context"
	"fmt"
	"sort"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// SimulationPresetItem represents one row of the flux-simulation-presets
// table. PresetID is serialised as "id" so the Swift client decodes it through
// Identifiable. The table is system-wide and keyed by presetId only (no
// device/user partition), mirroring flux-pricing (Decision 10).
type SimulationPresetItem struct {
	PresetID  string `dynamodbav:"presetId" json:"id"`
	Label     string `dynamodbav:"label" json:"label"`
	Watts     int    `dynamodbav:"watts" json:"watts"`
	CreatedAt string `dynamodbav:"createdAt" json:"createdAt"` // RFC3339 UTC
	UpdatedAt string `dynamodbav:"updatedAt" json:"updatedAt"` // bumped on every PUT
}

// SimulationPresetAPI is the subset of the DynamoDB client used by the preset
// store. The live *dynamodb.Client satisfies every method. Unlike pricing,
// presets are independent rows, so there is no TransactWriteItems dependency.
type SimulationPresetAPI interface {
	PutItem(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
	DeleteItem(ctx context.Context, params *dynamodb.DeleteItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error)
	Scan(ctx context.Context, params *dynamodb.ScanInput, optFns ...func(*dynamodb.Options)) (*dynamodb.ScanOutput, error)
}

// DynamoSimulationPresetStore implements the preset read+write surface against
// a real DynamoDB table. It mirrors DynamoPricingStore minus the singleton
// sentinel and transactional machinery (Decision 10).
type DynamoSimulationPresetStore struct {
	client SimulationPresetAPI
	table  string
}

// NewDynamoSimulationPresetStore returns a store scoped to the given table.
func NewDynamoSimulationPresetStore(client SimulationPresetAPI, table string) *DynamoSimulationPresetStore {
	return &DynamoSimulationPresetStore{client: client, table: table}
}

// presetKey returns the DynamoDB key for a preset row.
func presetKey(id string) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{
		"presetId": &types.AttributeValueMemberS{Value: id},
	}
}

// presetListPageLimit caps each Scan page. A defensive cap of 20 presets is
// enforced on create, so 50 is comfortably above realistic usage while still
// bounding a pathological scan.
const presetListPageLimit = 50

// ListPresets returns every preset row sorted by CreatedAt ascending.
func (s *DynamoSimulationPresetStore) ListPresets(ctx context.Context) ([]SimulationPresetItem, error) {
	items := make([]SimulationPresetItem, 0)
	limit := int32(presetListPageLimit)
	input := &dynamodb.ScanInput{TableName: &s.table, Limit: &limit}
	for {
		out, err := s.client.Scan(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("scan simulation presets (table=%s): %w", s.table, err)
		}
		for _, av := range out.Items {
			var item SimulationPresetItem
			if err := attributevalue.UnmarshalMap(av, &item); err != nil {
				return nil, fmt.Errorf("unmarshal simulation preset (table=%s): %w", s.table, err)
			}
			items = append(items, item)
		}
		if out.LastEvaluatedKey == nil {
			break
		}
		input.ExclusiveStartKey = out.LastEvaluatedKey
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].CreatedAt < items[j].CreatedAt
	})
	return items, nil
}

// PutPreset inserts or overwrites a preset row. Create and update both route
// here; the handler is the authority for id assignment and timestamp bumping.
func (s *DynamoSimulationPresetStore) PutPreset(ctx context.Context, item SimulationPresetItem) error {
	av, err := attributevalue.MarshalMap(item)
	if err != nil {
		return fmt.Errorf("marshal simulation preset (presetId=%s): %w", item.PresetID, err)
	}
	_, err = s.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: &s.table,
		Item:      av,
	})
	if err != nil {
		return fmt.Errorf("put simulation preset (table=%s, presetId=%s): %w", s.table, item.PresetID, err)
	}
	return nil
}

// DeletePreset removes a preset row by id. Idempotent — deleting an absent row
// is a no-op (DynamoDB DeleteItem does not error on a missing key).
func (s *DynamoSimulationPresetStore) DeletePreset(ctx context.Context, id string) error {
	_, err := s.client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: &s.table,
		Key:       presetKey(id),
	})
	if err != nil {
		return fmt.Errorf("delete simulation preset (table=%s, presetId=%s): %w", s.table, id, err)
	}
	return nil
}
