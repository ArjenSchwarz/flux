# Design: Day Notes

## Overview

Adds a per-date free-text note (≤200 grapheme clusters) shared across both system users. New `flux-notes` DynamoDB table with PITR; first write endpoint on the Lambda API; bundled into the existing read responses; rendered read-only on Dashboard (today) and History (selected day) and editable on Day Detail.

## Key decisions referenced inline

This design depends on these decision-log entries; not redebated here:
- Decision 5: last-write-wins, no conflict detection — concurrent edits silently overwrite. Accepted.
- Decision 7: 200 grapheme clusters after NFC + leading/trailing trim, identical client and server.
- Decision 8: server is the authoritative clock for "today (Sydney)".
- Decision 10: notes bundled into existing read endpoints (no `GET /notes`).

## Architecture

### Endpoint additions

| Method | Path     | Auth          | Purpose                                                                                                                                  |
|--------|----------|---------------|------------------------------------------------------------------------------------------------------------------------------------------|
| `PUT`  | `/note`  | Bearer (same) | Upsert (or delete via empty text) the note for a single date. Body: `{"date": "YYYY-MM-DD", "text": "..."}`. Returns the canonical record. |

`PUT` with the date in the body keeps routing flat (matches the existing `switch req.RawPath` in `internal/api/handler.go:73`). `PUT` is correct because the operation is idempotent for identical input.

### Handler changes

`Handler.handle` (`internal/api/handler.go:62`) currently rejects every method except `GET`. New shape:

```go
switch req.RequestContext.HTTP.Method {
case "GET":
    switch req.RawPath {
    case "/status":  return h.handleStatus(ctx, req)
    case "/history": return h.handleHistory(ctx, req)
    case "/day":     return h.handleDay(ctx, req)
    }
case "PUT":
    if req.RawPath == "/note" {
        return h.handleNote(ctx, req)
    }
}
// 405 with per-route Allow header.
allow := "GET"
if req.RawPath == "/note" {
    allow = "PUT"
}
resp := errorResponse(405, "method not allowed")
resp.Headers["Allow"] = allow
return resp
```

Auth check stays before routing (existing AC §5.4).

### `handleNote` validation order

Strict order, each step short-circuits:

1. **415** if `Content-Type` is missing or doesn't prefix-match `application/json` (i.e. `application/json` or `application/json; charset=utf-8` accepted; nothing else).
2. **Decode body**: if `req.IsBase64Encoded`, base64-decode first. Function URLs sometimes flag JSON as base64 depending on client framing.
3. **413** if decoded body > 4 KB.
4. **400** if JSON parse fails (`malformed request body`).
5. **400** if `date` missing/malformed/non-Gregorian (`invalid date`).
6. **400** if date is later than today in Sydney TZ per the server clock (`date may not be in the future`).
7. NFC-normalise + leading/trailing-trim `text`. **400** if grapheme count > 200 (`note must be 200 characters or fewer`).
8. If trimmed-normalised text is empty → `DeleteNote(serial, date)`. Else `PutNote(NoteItem{...UpdatedAt: now})`.
9. Return `noteResponse{Date, Text, UpdatedAt}` — `Text == ""` and `UpdatedAt == nil` on delete.

Note text is never written to logs. The `notePayload` type implements `slog.LogValuer` returning a redacted `slog.Group` with `date` + `text_len` only — that way `slog.Any("payload", p)` cannot accidentally leak the text in any future caller.

### Pattern parity audit (read-side bundling)

Notes must appear in all three read endpoints. Each endpoint also needs to issue a `flux-notes` `Get`/`Query` so the additional latency is hidden and the failure is isolated.

| Read site                       | Existing query pattern               | Notes integration                                                  | Needs equivalent? |
|---------------------------------|--------------------------------------|--------------------------------------------------------------------|-------------------|
| `/status` (`status.go`)         | errgroup with 4 concurrent queries   | Add 5th: `GetNote(today)` (see "Read-side failure isolation" below).| yes               |
| `/day` (`day.go`)               | errgroup with readings + dailyEnergy | Add 3rd: `GetNote(date)`.                                           | yes               |
| `/history` (`history.go`)       | errgroup with energy + readings + offpeak | Add 4th: `QueryNotes(startDate, today)`. Joined per-day onto `DayEnergy`. | yes |
| iOS `URLSessionAPIClient`       | `fetchStatus`/`fetchHistory`/`fetchDay` | Add `saveNote(date:text:)`. Existing methods decode the new fields. | yes               |
| iOS `MockFluxAPIClient`         | Static preview data                   | Add `saveNote` (no-op for previews) + sample note in preview data. | yes               |
| `CachedDayEnergy` (SwiftData)   | Caches `DayEnergy` for offline       | Add optional `note: String?` (lightweight migration, see Data Models). | yes               |

### Read-side failure isolation

`errgroup.Wait()` returns the first error and cancels siblings. To make note-read failures non-fatal without changing the existing fail-fast behaviour for the core data, the note read runs *outside* the errgroup as a parallel `sync.WaitGroup` goroutine that captures its result into a typed cell:

```go
type noteResult struct {
    note *string
    // err is intentionally not surfaced; failures are logged and the field is left nil.
}

var (
    nr   noteResult
    nwg  sync.WaitGroup
)
nwg.Add(1)
go func() {
    defer nwg.Done()
    item, err := h.reader.GetNote(ctx, h.serial, today)
    if err != nil {
        slog.Warn("note fetch failed; continuing without note", "date", today, "error", err)
        return
    }
    if item != nil {
        s := item.Text
        nr.note = &s
    }
}()

// existing errgroup runs unchanged
if err := g.Wait(); err != nil {
    nwg.Wait() // drain so we don't leak the goroutine; result is discarded on 500
    slog.Error("status query failed", "error", err)
    return errorResponse(500, "internal error")
}
nwg.Wait()
resp.Note = nr.note
```

The same pattern applies to `/day` (single `GetNote`) and `/history` (single `QueryNotes`, results joined into the existing `result []DayEnergy` by date).

### CloudFormation

```yaml
NotesTable:
  Type: AWS::DynamoDB::Table
  DeletionPolicy: Retain
  UpdateReplacePolicy: Retain
  Properties:
    TableName: flux-notes
    BillingMode: PAY_PER_REQUEST
    AttributeDefinitions:
      - { AttributeName: sysSn, AttributeType: S }
      - { AttributeName: date,  AttributeType: S }
    KeySchema:
      - { AttributeName: sysSn, KeyType: HASH }
      - { AttributeName: date,  KeyType: RANGE }
    PointInTimeRecoverySpecification:
      PointInTimeRecoveryEnabled: true
```

`LambdaExecutionRole` (template.yaml:205) gains:

```yaml
- Effect: Allow
  Action:
    - dynamodb:GetItem
    - dynamodb:Query
    - dynamodb:PutItem
    - dynamodb:DeleteItem
  Resource: !GetAtt NotesTable.Arn
```

The existing read-only block (lines 220–229) stays unchanged; the notes write actions are scoped to `NotesTable.Arn` only, satisfying §6.4.

`ApiFunction.Environment.Variables` (template.yaml:402) gains `TABLE_NOTES: !Ref NotesTable`. The Lambda's required-env list (`cmd/api/main.go:31`) also gains `TABLE_NOTES`. Single CloudFormation stack update lands the table, IAM, env var, and Lambda code together (§6.5).

### iOS view-model wiring

**Day Detail (`DayDetailViewModel`):**
- New `private(set) var note: String?` populated from `DayDetailResponse.note`.
- New `func saveNote(_ rawText: String) async throws` — calls the API client; on success replaces `self.note` with `resp.text` (`nil` if delete).

**`NoteEditorSheet` + `NoteEditorViewModel`:**
- Sheet-presented `NavigationStack` containing a `TextEditor`, a remaining-character counter, and a Save button. Mirrors the `SettingsView` sheet pattern used elsewhere.
- The editor view model owns `isSaving: Bool` (disables Save while a write is in flight, preventing double-saves on rapid taps) and `error: FluxAPIError?` (surfaces failure inline). It calls `parent.saveNote(...)`; on `throws` it stays open with text intact and shows `error.message`.

**Dashboard (`DashboardViewModel`):**
- No new state; reads `status.note` directly from `StatusResponse`.
- Refresh on return from Day Detail is covered by the existing `onAppear → startAutoRefresh → immediate refresh()` chain (DashboardView.swift:80, DashboardViewModel.swift:46–62), so AC §3.5 needs no new wiring.

**History (`HistoryViewModel`):**
- No new top-level state; the note for the selected day is read off `selectedDay.note` (a new field on `DayEnergy`). Existing selection (chart drag-select / summary tap) drives the displayed note for free.

### Layouts

**History** — selected-day note row sits between the picker and the chart cards, so it's at the top of the day-specific content. Reuses `HistoryCardChrome`. **Collapses entirely** when `selectedDay.note == nil` (no placeholder, no fixed-height shell).

```
┌──────────────────────────────────┐
│ [ 7d  | 14d | 30d ]   ← picker   │
├──────────────────────────────────┤
│ "Away in Bali"      ← note row   │  (only when selectedDay?.note != nil)
├──────────────────────────────────┤
│ Solar / Grid / Battery cards     │
│ Selected-day summary card        │
│ "View day detail" link           │
└──────────────────────────────────┘
```

**Day Detail** — note row is the first child of the `VStack` (above the existing power/SOC charts in `DayDetailView.swift:18`). Two states:
- `note != nil`: the note rendered as a tappable row using `HistoryCardChrome`-equivalent chrome (matches History styling). Tapping opens the editor.
- `note == nil` (today and past dates only): an "Add note" button row at the same height. Future dates (per AC §2.7): nothing rendered.

**Dashboard** — note row sits between `BatteryHeroView` and `PowerTrioView`. Read-only. **Collapses entirely** when `status.note == nil`.

### File / module placement

| New file                                                                            | Purpose                                                |
|-------------------------------------------------------------------------------------|--------------------------------------------------------|
| `internal/api/note.go`                                                              | `handleNote`, `notePayload`, `noteResponse`, validation. |
| `internal/api/note_test.go`                                                         | Handler tests.                                         |
| `internal/api/notetext.go`                                                          | NFC + grapheme count helpers used by the handler.       |
| `internal/api/notetext_test.go`                                                     | Property tests with `pgregory.net/rapid`.               |
| `internal/api/testdata/note_lengths.json`                                           | **Single source of truth** for cross-stack grapheme count fixture. |
| `internal/dynamo/notes.go`                                                          | `NoteItem` + `DynamoNoteWriter` write impl.             |
| `internal/dynamo/notes_test.go`                                                     | Round-trip + idempotency tests.                         |
| Read methods (`GetNote`, `QueryNotes`) added to `internal/dynamo/reader.go`         | Lambda's existing reader gains note reads.              |
| `Flux/Packages/FluxCore/Sources/FluxCore/Helpers/NoteText.swift`                    | NFC + grapheme count for the editor and pre-flight.     |
| `Flux/Flux/DayDetail/NoteRowView.swift`                                             | Shared read-only note row used by Dashboard, History, Day Detail. |
| `Flux/Flux/DayDetail/NoteEditorSheet.swift` / `NoteEditorViewModel.swift`           | Editor sheet + view model.                              |
| `Flux/FluxTests/NoteTextTests.swift`                                                | Reads `internal/api/testdata/note_lengths.json` via relative path from repo root and asserts client-side counts match. |

## Components and Interfaces

### Go: dynamo layer

Two new methods on the existing `Reader` interface (note **reads** stay on `Reader`); a small **separate** `WriteAPI` interface for `PutItem`/`DeleteItem` so we don't widen `ReadAPI` and break existing read-side mocks.

```go
// internal/dynamo/reader.go (extend)
type Reader interface {
    // ... existing 7 methods ...
    GetNote(ctx context.Context, serial, date string) (*NoteItem, error)
    QueryNotes(ctx context.Context, serial, startDate, endDate string) ([]NoteItem, error)
}
// DynamoReader gains GetNote/QueryNotes implementations using its existing
// ReadAPI client (Query + GetItem are sufficient).

// internal/dynamo/notes.go (new)
type NoteItem struct {
    SysSn     string `dynamodbav:"sysSn"`
    Date      string `dynamodbav:"date"`
    Text      string `dynamodbav:"text"`
    UpdatedAt string `dynamodbav:"updatedAt"` // RFC 3339 UTC
}

// WriteAPI is the subset of the DynamoDB client used by note writes.
// Kept separate from ReadAPI so existing read-side tests don't grow new
// no-op methods on every mock.
type WriteAPI interface {
    PutItem(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
    DeleteItem(ctx context.Context, params *dynamodb.DeleteItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error)
}

type DynamoNoteWriter struct {
    client WriteAPI
    table  string
}

func NewDynamoNoteWriter(client WriteAPI, table string) *DynamoNoteWriter { ... }

func (w *DynamoNoteWriter) PutNote(ctx context.Context, item NoteItem) error
func (w *DynamoNoteWriter) DeleteNote(ctx context.Context, serial, date string) error
```

`internal/dynamo/store.go` `TableNames` gains:

```go
type TableNames struct {
    Readings    string
    DailyEnergy string
    DailyPower  string
    System      string
    Offpeak     string
    Notes       string // new
}
```

`cmd/api/main.go` (`requiredEnvVars` list at line 31) adds `TABLE_NOTES`. `loadConfig` reads it into `TableNames.Notes`, then constructs both:

```go
ddbClient := dynamodb.NewFromConfig(awsCfg)
reader := dynamo.NewDynamoReader(ddbClient, dynamo.TableNames{...})
notes  := dynamo.NewDynamoNoteWriter(ddbClient, os.Getenv("TABLE_NOTES"))
handler := api.NewHandler(reader, notes, cfg.serial, cfg.apiToken, cfg.offpeakStart, cfg.offpeakEnd)
```

The single `*dynamodb.Client` satisfies both `ReadAPI` and `WriteAPI` at compile time; no actual coupling between read and write paths.

### Go: api package

```go
// internal/api/handler.go (extend)
type Handler struct {
    reader      dynamo.Reader
    notes       dynamo.NoteWriter // interface, see below
    serial      string
    apiToken    string
    offpeakStart, offpeakEnd string
    nowFunc     func() time.Time
}

// NoteWriter mirrors the dynamo-package methods the handler needs.
// Kept in this package so the api unit tests can mock it without
// importing the dynamo package's internals.
type NoteWriter interface {
    PutNote(ctx context.Context, item dynamo.NoteItem) error
    DeleteNote(ctx context.Context, serial, date string) error
}

// internal/api/note.go (new)

const noteMaxGraphemes = 200

func (h *Handler) handleNote(ctx context.Context, req events.LambdaFunctionURLRequest) events.LambdaFunctionURLResponse

type notePayload struct {
    Date string `json:"date"`
    Text string `json:"text"`
}

// LogValue redacts text from any slog output. Always log via slog.Any
// so this is invoked automatically.
func (n notePayload) LogValue() slog.Value {
    return slog.GroupValue(
        slog.String("date", n.Date),
        slog.Int("text_len", len(n.Text)),
    )
}

type noteResponse struct {
    Date      string  `json:"date"`
    Text      string  `json:"text"`
    UpdatedAt *string `json:"updatedAt"` // null on delete
}

// internal/api/notetext.go (new)
//
// normalise applies NFC + leading/trailing Unicode-whitespace trim.
// graphemeCount returns the count over the result. Both sides
// (this package and Swift's NoteText helper) MUST produce identical
// values for every entry in testdata/note_lengths.json.
func normalise(text string) string
func graphemeCount(s string) int
```

Dependency additions: `golang.org/x/text/unicode/norm` and `github.com/rivo/uniseg`.

### Go: response models

Drop `omitempty` on every `note` field — absent is always serialised as `null`:

```go
// internal/api/response.go (additions)

type StatusResponse struct {
    // ... existing ...
    Note *string `json:"note"`
}

type DayEnergy struct {
    // ... existing ...
    Note *string `json:"note"` // null when no note for that day
}

type DayDetailResponse struct {
    // ... existing ...
    Note *string `json:"note"`
}
```

### Swift: FluxCore additions

```swift
// FluxCore/Helpers/NoteText.swift
public enum NoteText {
    public static let maxGraphemes = 200

    /// NFC + leading/trailing whitespace trim. Server applies the equivalent.
    public static func normalised(_ text: String) -> String {
        text.precomposedStringWithCanonicalMapping
            .trimmingCharacters(in: .whitespacesAndNewlines)
    }

    /// Grapheme-cluster count over the NFC + trimmed string.
    /// `Character` in Swift is defined as a grapheme cluster (UAX #29).
    public static func graphemeCount(_ text: String) -> Int {
        normalised(text).count
    }
}
```

`FluxAPIClient` and the response models edit the **original** struct declarations (extensions cannot add stored properties):

```swift
// FluxCore/Networking/FluxAPIClient.swift
public protocol FluxAPIClient: Sendable {
    func fetchStatus() async throws -> StatusResponse
    func fetchHistory(days: Int) async throws -> HistoryResponse
    func fetchDay(date: String) async throws -> DayDetailResponse
    func saveNote(date: String, text: String) async throws -> NoteResponse
}

// FluxCore/Models/APIModels.swift (in-place edits to existing structs)
public struct StatusResponse: Codable, Sendable {
    public let live: LiveData?
    // ... existing fields ...
    public let note: String?
    // ... and the init updated to add `note: String?`
}

public struct DayEnergy: Codable, Sendable, Identifiable {
    // ... existing fields ...
    public let note: String?
    // ... init updated
}

public struct DayDetailResponse: Codable, Sendable {
    // ... existing fields ...
    public let note: String?
    // ... init updated
}

public struct NoteResponse: Codable, Sendable {
    public let date: String
    public let text: String       // empty string on delete
    public let updatedAt: String? // nil on delete
}
```

`URLSessionAPIClient.saveNote` reuses the JSON-decode + error-mapping flow but builds a `PUT` with `Content-Type: application/json` and a JSON-encoded `notePayload` body. Maps response codes to `FluxAPIError` exactly as the GET endpoints do (`URLSessionAPIClient.swift:81`); 413 and 415 fall under `unexpectedStatus(Int)`.

### Swift: view-model contract

```swift
// DayDetailViewModel additions
private(set) var note: String?

func saveNote(_ rawText: String) async throws {
    let normalised = NoteText.normalised(rawText)
    guard NoteText.graphemeCount(normalised) <= NoteText.maxGraphemes else {
        throw FluxAPIError.badRequest("Note must be 200 characters or fewer")
    }
    let resp = try await apiClient.saveNote(date: date, text: normalised)
    note = resp.text.isEmpty ? nil : resp.text
}
```

The editor sheet drives this via its `NoteEditorViewModel`:

```swift
@MainActor @Observable
final class NoteEditorViewModel {
    var draft: String
    private(set) var isSaving: Bool = false
    private(set) var error: FluxAPIError?

    private let parent: DayDetailViewModel
    init(initial: String, parent: DayDetailViewModel) { ... }

    var characterCount: Int { NoteText.graphemeCount(draft) }
    var canSave: Bool { !isSaving && characterCount <= NoteText.maxGraphemes }

    func save() async -> Bool {
        guard canSave else { return false }
        isSaving = true; defer { isSaving = false }
        do {
            try await parent.saveNote(draft)
            return true // sheet dismisses
        } catch {
            self.error = FluxAPIError.from(error)
            return false
        }
    }
}
```

Save button is bound to `canSave`; it disables both while saving (no double-tap) and while over the cap (no client/server disagreement).

## Data Models

Single new entity: `NoteItem` (Go) / `NoteResponse` (Swift). Schema covered above. No new SwiftData entity needed for live state — the note is read off the API response.

`CachedDayEnergy` gains an optional `note: String?`. SwiftData supports adding optional stored properties via lightweight migration on iOS 17+ (we target iOS 26), so no `MigrationPlan` is required. Verify by building against the existing on-disk store; new property defaults to `nil` for previously cached rows.

## Error Handling

New failure modes introduced by the write endpoint:

| Code | Cause                                     | Response body                                        |
|------|-------------------------------------------|------------------------------------------------------|
| 400  | Malformed JSON, bad date, future date     | `{"error": "<reason>"}`                              |
| 400  | Text > 200 graphemes after NFC + trim     | `{"error": "note must be 200 characters or fewer"}`  |
| 405  | Method other than PUT on `/note`          | `{"error": "method not allowed"}` + `Allow: PUT`     |
| 413  | Body > 4 KB                               | `{"error": "request too large"}`                     |
| 415  | Content-Type not `application/json[;...]` | `{"error": "unsupported media type"}`                |
| 500  | DynamoDB error                            | `{"error": "internal error"}`                        |

Read-side: `GetNote` / `QueryNotes` failures inside `/status`, `/day`, `/history` are non-fatal (see "Read-side failure isolation" above). Mirrors the existing offpeak-degradation pattern in `history.go:62–70` in spirit but uses a separate goroutine because errgroup's first-error-cancels semantics are wrong for "this is optional."

iOS error handling: existing `FluxAPIError` covers all the new codes (`badRequest`, `unauthorized`, `serverError`, `unexpectedStatus`). No new cases needed.

## Testing Strategy

### Backend (Go)

**Dynamo layer (`internal/dynamo/notes_test.go`):**
- `PutNote → GetNote` round-trip preserves text and `updatedAt`.
- `DeleteNote` of an existing key clears the row; subsequent `GetNote` returns `(nil, nil)`.
- `DeleteNote` of a non-existent key returns nil (idempotent).
- `QueryNotes(start, end)` returns chronological range; empty for no-match.

**API handler (`internal/api/note_test.go`):**
- 405 for `GET /note`, `POST /note`, `DELETE /note`, with `Allow: PUT`.
- 415 for missing or non-JSON `Content-Type`; 200 for `application/json` and `application/json; charset=utf-8`.
- 413 for >4 KB body (regardless of validity).
- 401 for missing/invalid token (auth runs centrally; re-asserted at the new endpoint).
- 400 cases:
  - bad JSON; missing `date`; missing `text` (treated as `""` → delete is allowed; assert in test plan)
  - malformed date (`2026-13-01`, `2026-02-30`)
  - future date driven by `nowFunc` injection
  - >200 graphemes using fixture sequences (combining accents NFD form, ZWJ family emoji, full-width CJK, skin-tone modifier sequences)
- 200 upsert returns canonical record with non-empty `updatedAt` (RFC 3339 UTC).
- 200 delete via empty text returns `{date,text:"",updatedAt:null}` whether or not a row existed.
- Captured-handler assertion: `text` field never appears in any slog output (asserted by intercepting `slog.Default()` or building handler with a captured handler).

**Existing handler tests** gain cases verifying the `note` field in `/status`, `/day`, `/history` responses:
- Populated when a note exists.
- `null` when absent.
- `null` and request still 200 when the underlying note read errors (verifies failure isolation).

### Property-based testing

`normalise` is idempotent: `normalise(normalise(s)) == normalise(s)`. Verify with `pgregory.net/rapid` (already in `go.mod`):

```go
func TestPropertyNormaliseIdempotent(t *testing.T) {
    rapid.Check(t, func(t *rapid.T) {
        s := rapid.String().Draw(t, "input")
        require.Equal(t, normalise(s), normalise(normalise(s)))
    })
}
```

### Cross-stack grapheme parity

Single-source fixture at `internal/api/testdata/note_lengths.json`:

```json
[
  { "name": "ascii",            "input": "hello",                "graphemes": 5 },
  { "name": "nfd-accent",       "input": "café",           "graphemes": 4 },
  { "name": "zwj-family",       "input": "👨‍👩‍👧‍👦", "graphemes": 1 },
  { "name": "flag-pair",        "input": "🇦🇺",                    "graphemes": 1 },
  { "name": "skin-tone",        "input": "👋🏽",                   "graphemes": 1 },
  { "name": "trailing-spaces",  "input": "hi   ",                "graphemes": 2 },
  { "name": "internal-space",   "input": "a b c",                "graphemes": 5 },
  { "name": "max-200",          "input": "<200 ASCII chars>",    "graphemes": 200 }
]
```

The Go test (`internal/api/notetext_test.go`) and the Swift test (`Flux/FluxTests/NoteTextTests.swift`) both load this exact file via paths relative to repo root and assert each entry's count. CI runs both — drift is caught by either side failing.

This catches today's mismatches but not future Unicode-version skew between `rivo/uniseg` and Foundation's ICU. Mitigation: pin `rivo/uniseg` in `go.mod` and document the policy "if Go and Swift counts diverge on a future Unicode upgrade, the server count wins; the client's apparent character cap may be slightly more conservative until libraries align."

### iOS (Swift Testing)

- `NoteTextTests`: NFC normalisation idempotence; trim behaviour; grapheme count agreement against the shared fixture.
- `DayDetailViewModelTests`: `saveNote` happy path; `saveNote("")` clears `note`; `saveNote` propagates server 400 as `FluxAPIError.badRequest`; client-side over-cap rejected without API call.
- `NoteEditorViewModelTests`: `canSave == false` while `isSaving`; `canSave == false` over cap; `save()` returns `true` on success; `save()` returns `false` and sets `error` on throw.
- `HistoryViewModelTests`: `selectedDay.note` updates when selection changes; cached fallback preserves notes through `CachedDayEnergy` round-trip.
- `URLSessionAPIClientTests`: PUT body shape, response decoding, error mapping (400/401/413/415).

### Integration / deploy

- Single CloudFormation deploy lands table + IAM + env var + Lambda binary; verify stack update lands without manual steps.
- Smoke: `curl -X PUT $URL/note -H "Authorization: Bearer $T" -H "Content-Type: application/json" -d '{"date":"2026-04-28","text":"smoke"}'`, then `curl $URL/day?date=2026-04-28` and assert `note == "smoke"`. Then `curl -X PUT … -d '{"date":"2026-04-28","text":""}'` and verify `/day` returns `note: null`.
- iOS: install over-the-air, edit a note on Day Detail, navigate to History/Dashboard, verify rendering.

### PITR restore runbook

Documented for the rollback case (§7.3) where notes corruption needs a point-in-time restore:

1. AWS Console → DynamoDB → `flux-notes` → "Restore to point in time" → choose timestamp before the bad write.
2. Restore writes to a new table (`flux-notes-restored-YYYYMMDD`); update `TABLE_NOTES` env var on the Lambda or copy items back via a one-shot script. The CloudFormation stack manages the canonical table, so prefer copy-back over swap to avoid drift between the stack template and reality.
3. Validate via `curl $URL/history?days=30 | jq '.days[].note'`.
