# Implementation: Day Notes (Backend)

This document explains the Go backend portion of the Day Notes feature, covering Tasks 1–12 of `tasks.md`. Tasks 13 (Lambda env wiring in `cmd/api/main.go`) and 14 (CloudFormation infra: `flux-notes` table, IAM grant, `TABLE_NOTES` env) are explicitly out of scope and tracked as follow-ups.

Branch: `feature/day-notes` — three commits ahead of `origin/main`.

---

## Beginner Level

### What This Does

Flux is a battery-monitoring app. Users were asking "why was yesterday so different?" — so the app now lets them attach a one-line note to any past day (e.g. "we were away" or "had guests over for dinner"). The note shows up on the dashboard, in the history view, and on the day-detail view, and stays in sync between users on the same system.

This change is the *server* half of that feature. It adds a new way to save a note (`PUT /note`) and makes the existing screens send the note text along with everything else they already send, so the app gets the note for free without an extra request.

### Why It Matters

Without this, the app couldn't store user-written text anywhere central. The server was read-only — it polled the battery every 10 seconds and reported numbers back. Notes are the first piece of user-authored data in the system, so the server now has its first *write* endpoint, and a new database table sized to keep notes safe (with point-in-time backup, since unlike battery readings, a deleted note can't be re-fetched from anywhere).

### Key Concepts

- **Lambda Function URL**: a single web address for the server's API. Calling `PUT /note` with a JSON body saves a note.
- **DynamoDB**: AWS's key-value database. Each note is stored under a `(systemSerial, date)` key — at most one note per day.
- **Grapheme cluster**: what a person reads as "one character." An emoji counts as one grapheme even though it might be several Unicode codepoints internally. The 200-character note limit is measured this way so the iPhone editor and the server agree.
- **NFC normalisation**: a Unicode standard for collapsing equivalent text into one canonical form. `café` typed two different ways looks the same on screen but isn't byte-identical until normalised.
- **Bearer token**: a shared secret. The phone sends it on every request; the server checks it before doing anything.

---

## Intermediate Level

### Changes Overview

Eight new or modified Go files in two packages:

**`internal/api/`**
- `note.go` (new) — `handleNote` (the `PUT /note` handler), `notePayload` and `noteResponse` types, content-type and base64 size guards.
- `notetext.go` (new) — `normalise` (NFC + leading/trailing-trim) and `graphemeCount` / `graphemeCountNormalised` (UAX #29 cluster count via `rivo/uniseg`).
- `notes_fetch.go` (new) — `fetchNoteAsync` and `fetchNotesAsync` helpers that wrap the soft-fail-outside-errgroup pattern used by all three read handlers.
- `testdata/note_lengths.json` (new) — eight-entry fixture (ASCII, NFD-decomposed `café`, ZWJ family emoji, regional-indicator flag, skin-tone modifier, trailing whitespace, internal whitespace, 200-char upper bound) consumed by both Go and Swift tests.
- `handler.go` — routing now switches on method first (`http.MethodGet` → status/history/day, `http.MethodPut` → note), with 405 + per-path `Allow` header for unknown method/path pairs. New `NoteWriter` interface.
- `status.go`, `history.go`, `day.go` — each now starts a notes fetch alongside its existing `errgroup` and joins the result before returning.
- `response.go` — `Note *string` field added to `StatusResponse`, `DayEnergy`, and `DayDetailResponse`.

**`internal/dynamo/`**
- `notes.go` (new) — `NoteItem`, `WriteAPI` interface, `DynamoNoteWriter` (`PutNote` / `DeleteNote`).
- `reader.go` — `Reader` interface gains `GetNote` and `QueryNotes`; `DynamoReader` implements them via the existing generic `getItem` and `queryAll` helpers.
- `store.go` — `TableNames` gains `Notes`.

### Implementation Approach

**Validation order in `handleNote`** is fixed and short-circuits at each step: 415 (Content-Type) → base64 size guard + decode → 413 (decoded body > 4 KiB) → 400 (JSON parse) → 400 (date present + valid Gregorian) → 400 (date not in the future, Sydney clock) → NFC + trim → 400 (grapheme count > 200) → empty text deletes / non-empty upserts. The order matters because §5.3 requires 413 to fire before any field-level validation, and the design pins the sequence so the iOS test suite can reason about it.

**Read-side bundling** is a soft-fail. The three read handlers already use `errgroup` for their core queries, where the first error cancels siblings and returns 500. A notes-table failure must not do that — the iOS UI tolerates a missing note, but a missing dashboard would be a regression. So the notes read runs in a separate goroutine alongside the errgroup. The original implementation hand-rolled a `sync.WaitGroup` block in each handler; it's now consolidated into `fetchNoteAsync` / `fetchNotesAsync`, each returning a wait closure that yields the final value (or nil on error). The closure is passed `gctx` from the errgroup so a 500 on the core path cancels the in-flight notes read instead of making the request hang for a slow Dynamo response.

**`NoteWriter` interface** lives in `internal/api/handler.go` (not `dynamo`) so the api unit tests can mock writes without importing dynamo internals. The interface mentions `dynamo.NoteItem` directly — the leak is intentional and gated by the `WriteAPI` design pattern.

**Log redaction**: `notePayload.LogValue` returns a `slog.GroupValue` of `(date, text_len)` only. Anyone who logs `slog.Any("payload", p)` cannot accidentally leak the text, even from a future caller that doesn't know the rule.

### Trade-offs

- **`PUT /note` over `POST /notes/{date}`**: keeps routing flat (`switch req.RawPath`) and matches the existing read-endpoint shape. Idempotent for identical input, which is correct for an upsert.
- **Single goroutine outside errgroup vs. errgroup with swallowed error**: errgroup's first-error-cancels semantics are wrong for an optional read. A `g.Go` returning `nil` on note failure would work but quietly couples the cancellation lifecycle to the wrong group. The split makes the optional-ness visible.
- **`WriteAPI` separate from `ReadAPI`**: keeps existing read-side mocks from gaining unused `PutItem`/`DeleteItem` methods and gates the IAM split (read-only on existing tables, read+write on notes only) at compile time. The single live `*dynamodb.Client` satisfies both interfaces.
- **`graphemeCount` vs `graphemeCountNormalised`**: callers that already normalised (the validation path) skip a second NFC pass. External callers that pass raw input still get the safe single-call API.
- **Helper extraction (`notes_fetch.go`)**: eliminates ~50 lines of duplicated boilerplate across three handlers and removes one easy-to-miss bug (each handler's error path needed to drain the WaitGroup; the helper makes that automatic via the closure).

---

## Expert Level

### Technical Deep Dive

**Validation order edge cases.** `time.ParseInLocation("2006-01-02", "", sydneyTZ)` returns an error, so the `payload.Date == ""` guard at `note.go:79` is technically redundant. Kept for explicitness; reads as "empty is not valid" rather than "the parser happens to reject empty." The future-date check uses Sydney midnight (`time.Date(y,m,d,0,0,0,0,sydneyTZ)`) so a today-dated write submitted at 23:59 Sydney is accepted, while a tomorrow-dated write submitted at 00:01 is rejected. The `nowFunc` injection lets tests pin the clock.

**Base64 decode size guard.** Function URLs may flag JSON as base64 depending on client framing, so `IsBase64Encoded` is honoured. The decoded length is checked against `noteMaxBodyBytes`, but a multi-MiB encoded blob would allocate before that check fires. The guard `len(req.Body) > base64.StdEncoding.EncodedLen(noteMaxBodyBytes)` rejects oversize encoded input before allocating the decode buffer. Lambda's runtime caps payload at ~6 MB, so this is bounded either way; the guard removes a wasteful allocation on adversarial input.

**Grapheme count semantics.** `rivo/uniseg.GraphemeClusterCount` implements UAX #29. The cross-stack fixture pins parity against `Swift.String.count` (which is grapheme-cluster-typed since Swift 4). The known divergence risk is future Unicode-version skew between `rivo/uniseg`'s tables and Foundation's ICU; the fixture catches today's mismatches but not tomorrow's. Mitigation documented in design.md §Cross-stack grapheme parity: server count wins; client cap may be conservative until libraries align.

**Soft-fail isolation correctness.** Each handler calls `fetchNoteAsync(gctx, ...)` then runs `g.Wait()`. On 500 path: `g.Wait()` returns the first core error; the handler calls `waitNote()` to drain the goroutine before responding 500 (otherwise the goroutine leaks past the request boundary). Because the helper takes `gctx`, the in-flight notes Dynamo call observes the cancellation and returns promptly — the 500 path no longer blocks on the slow notes read. On the happy path: `waitNote()` blocks until the notes read finishes, which is a no-op if it already did. The buffered channel (`make(chan *string, 1)`) means the goroutine never blocks even if the caller races on the close.

**`updatedAt` timestamp.** Generated server-side via `h.nowFunc().UTC().Format(time.RFC3339)` and returned to the client. Stored as a string for consistency with other Flux tables (the `dynamodbav` package marshals time.Time to S anyway). The iOS client doesn't use `updatedAt` in v1 (per requirement 1.7) — kept on the wire for future cache-invalidation and ordering work.

**Last-write-wins.** `DynamoNoteWriter.PutNote` uses `PutItem` with no `ConditionExpression` — concurrent saves from two devices silently overwrite (Decision 5). DeleteNote uses `DeleteItem` with no condition — deletes are idempotent; deleting a non-existent key returns nil.

**Method routing 405 path.** When the request method/path pair doesn't match any of the four registered routes, the handler returns 405 with an `Allow` header derived from the path (`GET` for `/status`, `/history`, `/day`; `PUT` for `/note`) — but only for known paths. Unknown paths fall through to 404. Both branches go through `errorResponse` and emit the same `{"error":"..."}` shape (§5.11).

### Architecture Impact

- **First write endpoint.** Until this branch, the Lambda was read-only and the IAM policy granted `GetItem`/`Query` only. The new `WriteAPI` interface and `DynamoNoteWriter` add the first write path. The api package's `NoteWriter` interface gates the api/dynamo coupling so future write endpoints can drop in without further indirection.
- **`Reader` interface widened.** Two new methods (`GetNote`, `QueryNotes`) — every implementer needed updating, but only `DynamoReader` and the test mocks exist. Test mocks gained `queryNotesFn`/`getNoteFn` function fields per the consumer-defined-interface convention.
- **Helper extraction (`notes_fetch.go`)** is a small abstraction layer. If a future read endpoint also wants to bundle notes, it imports the helper and one line wires the soft-fail. Lower risk than letting the pattern stay duplicated and drift across handlers.
- **Always-serialised null `note` field.** `*string` without `omitempty` ensures every response carries the field even when nil. iOS decoders see a stable schema; missing-key vs. explicit-null isn't a decode-time distinction in Swift's `Codable`, but the wire shape is more honest.
- **No CLAUDE.md / template.yaml change in this commit.** The architecture description in CLAUDE.md still says "three endpoints" and "read-only API" — accurate for production until Tasks 13/14 ship. The deploy-ready commit will need to update both.

### Potential Issues

1. **`cfg.notes` is nil in production.** `cmd/api/main.go` adds the `notes` config field but `loadConfig` never assigns it. If this branch is deployed before Task 13, any authenticated `PUT /note` will nil-deref in `handleNote` and return 500 (Lambda recovers panics into 500). The four read handlers also pass `nil` to `fetchNoteAsync`, which then calls `reader.GetNote` — that *does* work because `reader` is wired, so the read-side bundling is safe to deploy. The blast radius is exactly the new endpoint, not the existing ones. Task 13 + Task 14 should land together.
2. **`TABLE_NOTES` not in `requiredEnvVars`.** Same reason — Task 13. Adding it now would break the existing deploy because the CFN template doesn't set it yet.
3. **Future Unicode skew.** Documented above. CI runs both fixture tests today; if `rivo/uniseg` diverges from Foundation's ICU on a future emoji or combining mark, both stacks need a coordinated bump.
4. **Notes goroutine on 500 path.** The error path now uses `gctx`, so cancellation propagates and the wait drains promptly. Before the refactor, the parent `ctx` was passed instead and the request would block on the in-flight Dynamo call — fixed by the pre-push refactor.
5. **`graphemeCountNormalised` is unexported but trusts the caller.** Nothing prevents a future caller from passing un-normalised input and getting a wrong count. Documented in the comment; no runtime guard. Could be tightened with a marker type if it becomes a foot-gun.

---

## Completeness Assessment

### Fully implemented in this branch

- §1.1 one-note-per-`(sysSn, date)` — `DynamoNoteWriter.PutNote` keys on `(sysSn, date)` only.
- §1.2 internal whitespace preserved, leading/trailing trimmed — `normalise` is `strings.TrimSpace(norm.NFC.String(text))`.
- §1.3 / §5.10 200-grapheme-cluster cap with NFC — `graphemeCountNormalised` over `normalise`d text; rejects 400 with the exact spec message.
- §1.4 empty text deletes idempotently — `DeleteNote` carries no condition; `TestHandleNote_DeleteIsIdempotentRegardlessOfPriorState`.
- §1.5 last-write-wins — `PutItem` carries no `ConditionExpression`; `TestDynamoNoteWriter_PutNoteOverwriteIsLastWriteWins`.
- §1.7 server-generated `updatedAt` (RFC 3339 UTC) — `note.go:103`.
- §5.1 upsert-or-delete write endpoint returning canonical record — `noteResponse`.
- §5.2 415 on non-`application/json` — `mime.ParseMediaType` accepts the registered mediatype with optional parameters; rejects everything else.
- §5.3 413 before field validation — body size check fires at step 3, before JSON parse.
- §5.4 401 before routing — `validToken` runs before the method/path switch in `handler.go:73`.
- §5.5 serial taken from env, never the body — `h.serial` only.
- §5.6 / §5.7 / §5.8 note bundled into `/day`, `/history`, `/status` with always-serialised `note` field.
- §5.9 date validation (Gregorian + not future, Sydney TZ) — `time.ParseInLocation` + Sydney midnight comparison.
- §5.11 errors match `{"error":"..."}` shape — every error path goes through `errorResponse`.
- §5.12 slog format unchanged + text never logged — `Handle` keeps the existing log line; `notePayload.LogValue` redacts text.
- §6.1 partition+sort matches existing convention — `sysSn` HASH, `date` RANGE.
- §7.2 405 with `Allow` header — `handler.go:99–104`.
- §7.3 rollback plan — code revert is sufficient; the table is `Retain` (asserted in design, not yet in template).
- Decision 7 cross-stack parity — `internal/api/testdata/note_lengths.json` consumed by `TestGraphemeCountFixture`.

### Partially implemented

- **§6.2 (on-demand billing, `Retain` deletion policy)** — encoded only in the spec/design today; the CloudFormation template change is Task 14.
- **§6.3 (PITR enabled)** — same. Task 14.
- **§6.4 (IAM gains write access on notes table only)** — the Go `WriteAPI` interface enforces this at compile time, but the actual IAM policy update is Task 14.
- **§6.5 (single CloudFormation stack update)** — the spec promises one deploy lands table + IAM + env + Lambda binary. This commit lands the Lambda binary changes only. Task 13 + Task 14 must land together to satisfy this acceptance criterion.

### Missing (out of scope for this branch by design)

- **Task 13: Lambda env wiring (`cmd/api/main.go`).** `cfg.notes` is declared but unassigned; `TABLE_NOTES` is not in `requiredEnvVars`; no `dynamo.NewDynamoNoteWriter` constructor call. `PUT /note` will 500 in production until this lands.
- **Task 14: CloudFormation infra.** `flux-notes` table, IAM grant, `TABLE_NOTES` env on `ApiFunction`. None of `infrastructure/template.yaml` is touched.
- **Section 2 (Day Detail editor), Section 3 (Dashboard rendering), Section 4 (History rendering).** All iOS — Tasks 15–27 in `tasks.md`, separate stream.

### Requirements with no clean explanation

None. Every spec requirement either maps to a concrete file/function in this commit or is explicitly deferred to Task 13 / 14 / iOS work. The two notable points — the nil `cfg.notes` deployment hazard and the IAM/PITR posture being spec-only until Task 14 — are both consequences of the branch's intentional partial scope, not implementation gaps.

---

## Validation Findings

### Gaps Identified

- **Deployment hazard from partial scope.** `cmd/api/main.go` declares `notes api.NoteWriter` but never assigns it. Recommend Tasks 13 and 14 land together in a single PR (or a tightly-coupled pair) so production never sees the half-wired state.
- **`graphemeCountNormalised` precondition is unenforced.** Comment-documented; no runtime guard. Acceptable, but worth a consistency check if a future caller is added.

### Logic Issues

None found. The validation order matches design §handleNote. The soft-fail goroutine pattern propagates `gctx` correctly after the pre-push refactor. The `WriteAPI` / `ReadAPI` split holds the IAM-at-compile-time invariant.

### Questions Raised

- Should `noteResponse.UpdatedAt` use `omitempty` rather than `*string`-as-null on delete? The current shape sends `"updatedAt": null` on delete; iOS decodes it as `nil` either way. Worth confirming the Swift decoder explicitly when Task 16+ lands the client side.
- Decision-log entries 1, 2, 5 are the canonical "size limits" / "last-write-wins" decisions. The user prompt referenced them with slightly different numbering; the implementation matches the spec's actual numbering, not the prompt's.

### Recommendations

1. Bundle Tasks 13 + 14 in the next PR; do not deploy this branch standalone.
2. When Task 14 lands, update `CLAUDE.md` (currently lists three endpoints and "read-only API") to mention `PUT /note` and the `flux-notes` table.
3. Consider a 400-path log-redaction test alongside the existing 200-path one (`TestHandleNote_TextNeverAppearsInLogs` only exercises upsert today).
