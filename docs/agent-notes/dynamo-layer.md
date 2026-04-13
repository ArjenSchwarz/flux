# DynamoDB Layer — Agent Notes

## Package Structure

`internal/dynamo/` contains:
- `store.go` — `Store` interface (7 methods) and `TableNames` struct
- `models.go` — DynamoDB item structs with `dynamodbav` tags + transformation functions from AlphaESS types
- `dynamostore.go` — Production implementation using `DynamoAPI` interface (not concrete `dynamodb.Client`)
- `logstore.go` — Dry-run implementation that logs JSON to slog
- Tests in corresponding `_test.go` files

## Key Design Choices

- `DynamoAPI` interface (PutItem, DeleteItem, GetItem, BatchWriteItem) enables testing without AWS. The mock is defined in `dynamostore_test.go`.
- `LogStore` uses hardcoded table name labels (e.g. `"flux-readings"`) since in dry-run mode there are no real table names configured.
- `OffpeakItem.Status` field distinguishes `"pending"` (start captured) from `"complete"` (both snapshots + deltas).
- `WriteDailyPower` chunks at 25 items (DynamoDB limit) and retries unprocessed items once.
- TTL is 30 days for both readings and daily power items (Decision 10).
- `NewSystemItem` formats `LastUpdated` as RFC3339 in UTC.

## Testing Patterns

- Tests use `map[string]struct` table-driven style with `tc` variable (matching project conventions).
- `mockDynamoAPI` uses function fields (`putItemFn`, etc.) — nil means success.
- LogStore tests capture slog output to a `bytes.Buffer` and parse JSON entries.
- `testify/assert` and `testify/require` used throughout.
