# DynamoDB Layer — Agent Notes

## Package Structure

`internal/dynamo/` contains:
- `store.go` — `Store` interface and `TableNames` struct (now includes `Notes` for the day-notes table).
- `reader.go` — `Reader` interface, `ReadAPI` client interface, `DynamoReader` implementation, shared `getItem[T]`/`offpeakKey`/`queryAll[T]` helpers. Now also exposes `GetNote(ctx, serial, date)` and `QueryNotes(ctx, serial, startDate, endDate)`.
- `notes.go` — `NoteItem` (dynamodbav: `sysSn`, `date`, `text`, `updatedAt`), the small `WriteAPI` interface (PutItem + DeleteItem only), and `DynamoNoteWriter` (implements `PutNote` / `DeleteNote`). Lives outside the existing `Store` / `Reader` interfaces so the api-side dependency is a single small type.
- `models.go` — DynamoDB item structs with `dynamodbav` tags + transformation functions from AlphaESS types
- `dynamostore.go` — Production write implementation using `DynamoAPI` interface (not concrete `dynamodb.Client`). `GetOffpeak` uses shared `getItem` helper. Note writes are intentionally NOT in `DynamoStore` — the Lambda owns its own `DynamoNoteWriter` so the poller can't gain write access to the notes table.
- `logstore.go` — Dry-run implementation that logs JSON to slog
- Tests in corresponding `_test.go` files

## Key Design Choices

- `DynamoAPI` interface (PutItem, DeleteItem, GetItem, BatchWriteItem) enables testing without AWS. The mock is defined in `dynamostore_test.go`.
- `ReadAPI` interface (Query, GetItem) is separate from `DynamoAPI` to avoid forcing poller mocks to implement Query. The production DynamoDB client satisfies both.
- `WriteAPI` interface (PutItem, DeleteItem) is even narrower and used only by `DynamoNoteWriter`. Kept separate from `ReadAPI` so existing read-side mocks don't sprout no-op write methods. The single `*dynamodb.Client` in `cmd/api/main.go` satisfies both `ReadAPI` and `WriteAPI` at compile time.
- `LogStore` uses hardcoded table name labels (e.g. `"flux-readings"`) since in dry-run mode there are no real table names configured.
- `OffpeakItem.Status` field distinguishes `"pending"` (start captured) from `"complete"` (both snapshots + deltas).
- `WriteDailyPower` chunks at 25 items (DynamoDB limit) and retries unprocessed items once.
- TTL is 30 days for both readings and daily power items (Decision 10).
- `NewSystemItem` formats `LastUpdated` as RFC3339 in UTC.
- Shared `getItem[T]` generic helper used by both `DynamoStore.GetOffpeak` and all `DynamoReader.Get*` methods — avoids implementation divergence.
- Shared `offpeakKey` helper builds the composite key for offpeak items.
- `queryAll[T]` generic helper handles DynamoDB pagination for all Query methods, always sets `ScanIndexForward: true`.
- `QueryReadings` uses `#ts` expression attribute name because `timestamp` is a DynamoDB reserved word.
- `QueryDailyEnergy` uses `#d` expression attribute name because `date` is a DynamoDB reserved word.
- All `Get*` methods return `(nil, nil)` for not-found. All `Query*` methods return `([]T{}, nil)` for empty results.
- Notes table schema: partition `sysSn` (S), sort `date` (S, YYYY-MM-DD), payload `text` (S), `updatedAt` (S, RFC3339 UTC). No TTL — notes are user-authored and retained indefinitely.
- `DynamoNoteWriter.DeleteNote` is unconditionally idempotent: DynamoDB `DeleteItem` succeeds for missing keys without a `ConditionExpression`, so no pre-check is needed.
- `DynamoNoteWriter.PutNote` carries no `ConditionExpression` — concurrent saves resolve last-write-wins, intentional per design Decision 5.
- `QueryNotes` uses `#d` expression attribute name (date is a reserved word) and a `BETWEEN` sort-key condition over `[startDate, endDate]`.

## Testing Patterns

- Tests use `map[string]struct` table-driven style with `tc` variable (matching project conventions).
- `mockDynamoAPI` uses function fields (`putItemFn`, etc.) — nil means success.
- LogStore tests capture slog output to a `bytes.Buffer` and parse JSON entries.
- `testify/assert` and `testify/require` used throughout.
