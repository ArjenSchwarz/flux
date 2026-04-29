---
references:
    - requirements.md
    - design.md
    - decision_log.md
---
# Day Notes Implementation

## Cross-stack fixture

- [x] 1. Create cross-stack grapheme fixture file <!-- id:mz8g5e4 -->
  - Path: internal/api/testdata/note_lengths.json.
  - Entries from design "Cross-stack grapheme parity": ascii (5), nfd-accent café (4), zwj-family (1), flag-pair (1), skin-tone (1), trailing-spaces hi+3 spaces (2), internal-space "a b c" (5), max-200 (200 ASCII chars).
  - Both Go and Swift tests load this exact file by repo-relative path.
  - Requirements: [1.3](requirements.md#1.3), [5.10](requirements.md#5.10)

## Backend

- [x] 2. Write Go notetext tests <!-- id:mz8g5e5 -->
  - File: internal/api/notetext_test.go.
  - Cover: graphemeCount matches every fixture entry; normalise applies NFC + leading/trailing trim while preserving internal whitespace (1.2); rapid property: normalise idempotent (normalise(normalise(s)) == normalise(s)).
  - Blocked-by: mz8g5e4 (Create cross-stack grapheme fixture file)
  - Stream: 1
  - Requirements: [1.2](requirements.md#1.2), [1.3](requirements.md#1.3), [5.10](requirements.md#5.10)

- [x] 3. Implement notetext helpers <!-- id:mz8g5e6 -->
  - File: internal/api/notetext.go.
  - Exports normalise(string) string and graphemeCount(string) int.
  - Add deps: golang.org/x/text/unicode/norm and github.com/rivo/uniseg via go get + go mod tidy.
  - Blocked-by: mz8g5e5 (Write Go notetext tests)
  - Stream: 1
  - Requirements: [1.2](requirements.md#1.2), [1.3](requirements.md#1.3), [5.10](requirements.md#5.10)

- [x] 4. Write dynamo notes writer tests <!-- id:mz8g5e7 -->
  - File: internal/dynamo/notes_test.go. Use the project's existing fake dynamo client pattern.
  - Cover: PutNote then GetNote round-trip preserves text and updatedAt; DeleteNote of existing key clears row, GetNote returns (nil, nil); DeleteNote of missing key returns nil (idempotent).
  - Include round-trip overwrite (last-write-wins per 1.5).
  - Stream: 1
  - Requirements: [1.1](requirements.md#1.1), [1.4](requirements.md#1.4), [1.5](requirements.md#1.5), [1.7](requirements.md#1.7), [6.1](requirements.md#6.1)

- [x] 5. Implement dynamo NoteItem and DynamoNoteWriter <!-- id:mz8g5e8 -->
  - File: internal/dynamo/notes.go.
  - Types: NoteItem (sysSn/date/text/updatedAt RFC3339 UTC), WriteAPI interface (PutItem, DeleteItem only — kept separate from ReadAPI per design), DynamoNoteWriter with NewDynamoNoteWriter constructor, PutNote, DeleteNote.
  - Extend internal/dynamo/store.go TableNames with Notes string field.
  - Blocked-by: mz8g5e7 (Write dynamo notes writer tests)
  - Stream: 1
  - Requirements: [1.1](requirements.md#1.1), [1.4](requirements.md#1.4), [1.5](requirements.md#1.5), [1.7](requirements.md#1.7), [6.1](requirements.md#6.1)

- [x] 6. Write Reader.GetNote and QueryNotes tests <!-- id:mz8g5e9 -->
  - Extend internal/dynamo/reader_test.go.
  - Cover: GetNote returns (nil,nil) when absent; returns NoteItem when present; QueryNotes returns chronological range scoped to (startDate, endDate); empty slice when no rows in range.
  - Blocked-by: mz8g5e8 (Implement dynamo NoteItem and DynamoNoteWriter)
  - Stream: 1
  - Requirements: [5.6](requirements.md#5.6), [5.7](requirements.md#5.7), [5.8](requirements.md#5.8)

- [x] 7. Add GetNote and QueryNotes to Reader <!-- id:mz8g5ea -->
  - Extend the Reader interface and DynamoReader implementation in internal/dynamo/reader.go using the existing ReadAPI client (GetItem + Query suffice).
  - Reads only — writes stay on DynamoNoteWriter.
  - Blocked-by: mz8g5e9 (Write Reader.GetNote and QueryNotes tests)
  - Stream: 1
  - Requirements: [5.6](requirements.md#5.6), [5.7](requirements.md#5.7), [5.8](requirements.md#5.8)

- [x] 8. Add Note field to response structs <!-- id:mz8g5eb -->
  - Edit internal/api/response.go: add Note *string `json:"note"` to StatusResponse, DayEnergy, DayDetailResponse.
  - NO omitempty — absent must serialise as null. Type-only change; tests in subsequent tasks assert behaviour.
  - Stream: 1
  - Requirements: [5.6](requirements.md#5.6), [5.7](requirements.md#5.7), [5.8](requirements.md#5.8)

- [x] 9. Write handleNote tests (full validation matrix) <!-- id:mz8g5ec -->
  - File: internal/api/note_test.go.
  - Cover: 401 missing/invalid token; 405 GET/POST/DELETE on /note with Allow: PUT header; 415 missing or non-JSON Content-Type; 200 for application/json and application/json; charset=utf-8; 413 body >4KB applied before field validation.
  - 400 cases: malformed JSON; missing/malformed/non-Gregorian date; future date (driven by injected nowFunc returning a fixed Sydney instant); over-200 graphemes using fixture sequences (NFD accents, ZWJ family, flag, skin-tone).
  - 200 upsert returns canonical NoteItem with non-empty RFC3339 UTC updatedAt; 200 delete via empty/whitespace-only text returns {date,text:"",updatedAt:null} regardless of prior state.
  - Captured-handler assertion: the note text never appears in any slog output.
  - Blocked-by: mz8g5e6 (Implement notetext helpers), mz8g5e8 (Implement dynamo NoteItem and DynamoNoteWriter), mz8g5eb (Add Note field to response structs)
  - Stream: 1
  - Requirements: [1.2](requirements.md#1.2), [1.3](requirements.md#1.3), [1.4](requirements.md#1.4), [5.1](requirements.md#5.1), [5.2](requirements.md#5.2), [5.3](requirements.md#5.3), [5.4](requirements.md#5.4), [5.5](requirements.md#5.5), [5.9](requirements.md#5.9), [5.10](requirements.md#5.10), [5.11](requirements.md#5.11), [5.12](requirements.md#5.12), [7.1](requirements.md#7.1), [7.2](requirements.md#7.2)

- [x] 10. Implement handleNote and handler routing <!-- id:mz8g5ed -->
  - New file internal/api/note.go: notePayload (with LogValue redacting text), noteResponse, handleNote pipeline ordered exactly as design §handleNote validation order (415 → base64-decode → 4KB → JSON parse → date present/valid → date not future Sydney → NFC+trim → grapheme count → put-or-delete → noteResponse).
  - Modify internal/api/handler.go: extend Handler struct with notes NoteWriter and nowFunc; update method routing to add PUT /note and produce 405 with Allow: PUT for unknown methods on /note (Allow: GET for the read paths); auth check stays before routing.
  - Define api.NoteWriter interface in handler.go (or note.go) so tests mock without importing dynamo internals.
  - Blocked-by: mz8g5ec (Write handleNote tests (full validation matrix))
  - Stream: 1
  - Requirements: [1.2](requirements.md#1.2), [1.3](requirements.md#1.3), [1.4](requirements.md#1.4), [5.1](requirements.md#5.1), [5.2](requirements.md#5.2), [5.3](requirements.md#5.3), [5.4](requirements.md#5.4), [5.5](requirements.md#5.5), [5.9](requirements.md#5.9), [5.10](requirements.md#5.10), [5.11](requirements.md#5.11), [5.12](requirements.md#5.12), [7.1](requirements.md#7.1), [7.2](requirements.md#7.2)

- [x] 11. Write tests for note bundling in /status, /day, /history <!-- id:mz8g5ee -->
  - Extend status_test.go, day_test.go, history_test.go.
  - For each: (a) populated note returned in correct field; (b) null returned when no note exists; (c) when the note read fails (fake reader returns error), the response still 200 and note=null (failure isolation).
  - For /history, assert per-day notes joined onto the correct DayEnergy by date.
  - Blocked-by: mz8g5ea (Add GetNote and QueryNotes to Reader), mz8g5eb (Add Note field to response structs)
  - Stream: 1
  - Requirements: [5.6](requirements.md#5.6), [5.7](requirements.md#5.7), [5.8](requirements.md#5.8)

- [x] 12. Bundle notes into /status, /day, /history handlers <!-- id:mz8g5ef -->
  - Edit internal/api/status.go, day.go, history.go.
  - Run note read in a separate sync.WaitGroup goroutine outside the existing errgroup so a note-read failure logs slog.Warn and leaves the field nil without cancelling siblings.
  - /status calls reader.GetNote(today); /day calls reader.GetNote(date); /history calls reader.QueryNotes(startDate, today) and joins results onto each DayEnergy by date.
  - After errgroup.Wait, drain the note WaitGroup before returning (don't leak the goroutine on the 500 path).
  - Blocked-by: mz8g5ee (Write tests for note bundling in /status, /day, /history)
  - Stream: 1
  - Requirements: [3.4](requirements.md#3.4), [5.6](requirements.md#5.6), [5.7](requirements.md#5.7), [5.8](requirements.md#5.8)

- [x] 13. Wire TABLE_NOTES env and DynamoNoteWriter in cmd/api/main.go <!-- id:mz8g5eg -->
  - Edit cmd/api/main.go: add TABLE_NOTES to requiredEnvVars; loadConfig reads it into TableNames.Notes; construct DynamoNoteWriter from the existing *dynamodb.Client (the single client satisfies both ReadAPI and WriteAPI at compile time); pass writer + nowFunc into NewHandler.
  - NewHandler signature update.
  - Blocked-by: mz8g5e8 (Implement dynamo NoteItem and DynamoNoteWriter), mz8g5ed (Implement handleNote and handler routing)
  - Stream: 1
  - Requirements: [6.5](requirements.md#6.5)

- [x] 14. Add NotesTable, IAM, and TABLE_NOTES env to CloudFormation <!-- id:mz8g5eh -->
  - Edit infrastructure/template.yaml.
  - Add NotesTable (PAY_PER_REQUEST, partition sysSn HASH + sort date RANGE, PointInTimeRecoveryEnabled: true, DeletionPolicy/UpdateReplacePolicy Retain).
  - Add an IAM Allow block to LambdaExecutionRole with GetItem/Query/PutItem/DeleteItem scoped to !GetAtt NotesTable.Arn (do NOT widen existing read-only block to other tables).
  - Add TABLE_NOTES: !Ref NotesTable to ApiFunction.Environment.Variables.
  - Stream: 1
  - Requirements: [1.6](requirements.md#1.6), [6.1](requirements.md#6.1), [6.2](requirements.md#6.2), [6.3](requirements.md#6.3), [6.4](requirements.md#6.4), [6.5](requirements.md#6.5), [7.3](requirements.md#7.3)

## iOS

- [x] 15. Write Swift NoteText tests <!-- id:mz8g5ei -->
  - File: Flux/FluxTests/NoteTextTests.swift.
  - Load internal/api/testdata/note_lengths.json via repo-relative path; assert NoteText.graphemeCount equals fixture .graphemes for every entry.
  - Cover: NFC normalisation idempotence; leading/trailing whitespace trim; internal whitespace preserved.
  - Blocked-by: mz8g5e4 (Create cross-stack grapheme fixture file)
  - Stream: 2
  - Requirements: [1.2](requirements.md#1.2), [1.3](requirements.md#1.3), [2.4](requirements.md#2.4), [5.10](requirements.md#5.10)

- [x] 16. Implement NoteText helper <!-- id:mz8g5ej -->
  - File: Flux/Packages/FluxCore/Sources/FluxCore/Helpers/NoteText.swift.
  - Exports maxGraphemes = 200, normalised(_:) (precomposedStringWithCanonicalMapping + trimmingCharacters(in: .whitespacesAndNewlines)), graphemeCount(_:) (normalised(text).count — Swift Character == grapheme cluster).
  - Blocked-by: mz8g5ei (Write Swift NoteText tests)
  - Stream: 2
  - Requirements: [1.2](requirements.md#1.2), [1.3](requirements.md#1.3), [2.4](requirements.md#2.4), [5.10](requirements.md#5.10)

- [x] 17. Add note fields to API model structs <!-- id:mz8g5ek -->
  - Edit Flux/Packages/FluxCore/Sources/FluxCore/Models/APIModels.swift in place (extensions cannot add stored properties).
  - Add note: String? to StatusResponse, DayEnergy, DayDetailResponse, and update each init.
  - Add new public struct NoteResponse: Codable, Sendable { date, text, updatedAt: String? }.
  - Type-only change; behaviour tested in saveNote tests.
  - Stream: 2
  - Requirements: [5.6](requirements.md#5.6), [5.7](requirements.md#5.7), [5.8](requirements.md#5.8)

- [x] 18. Write URLSessionAPIClient.saveNote tests <!-- id:mz8g5el -->
  - Extend the existing URLSession test pattern (or add new suite).
  - Cover: PUT to /note with Authorization: Bearer header and Content-Type: application/json; JSON body shape {date, text}; happy-path decodes NoteResponse; 400 maps to FluxAPIError.badRequest with server message; 401 maps to .unauthorized; 413/415 map to .unexpectedStatus(Int).
  - Blocked-by: mz8g5ek (Add note fields to API model structs)
  - Stream: 2
  - Requirements: [2.6](requirements.md#2.6), [5.1](requirements.md#5.1), [5.11](requirements.md#5.11)

- [x] 19. Implement saveNote on FluxAPIClient, URLSession, and Mock <!-- id:mz8g5em -->
  - Add saveNote(date:text:) async throws -> NoteResponse to FluxAPIClient protocol.
  - Implement in URLSessionAPIClient (PUT, JSON body, reuse existing decode + error-map flow).
  - Add a usable implementation on MockFluxAPIClient (track last call for tests; return a NoteResponse echoing inputs); also add a sample note to MockFluxAPIClient.preview data so previews show the row.
  - Blocked-by: mz8g5el (Write URLSessionAPIClient.saveNote tests)
  - Stream: 2
  - Requirements: [2.6](requirements.md#2.6), [5.1](requirements.md#5.1), [5.11](requirements.md#5.11)

- [x] 20. Add note field to CachedDayEnergy <!-- id:mz8g5en -->
  - Edit Flux/Flux/Models/CachedDayEnergy.swift: add var note: String? stored property; update init(from:) to copy dayEnergy.note; update asDayEnergy to pass note: through.
  - SwiftData lightweight migration on iOS 17+ handles the schema add for previously cached rows (default nil).
  - Blocked-by: mz8g5ek (Add note fields to API model structs)
  - Stream: 2
  - Requirements: [4.1](requirements.md#4.1)

- [x] 21. Write NoteEditorViewModel tests <!-- id:mz8g5eo -->
  - New test file.
  - Cover: characterCount uses NoteText.graphemeCount on draft; canSave false while isSaving; canSave false when graphemeCount > 200; save() returns true on success and parent.note updates; save() returns false on throw, error is set to FluxAPIError.from, sheet stays open with draft intact; rapid double-tap of save while isSaving must not call API twice.
  - Blocked-by: mz8g5ej (Implement NoteText helper)
  - Stream: 2
  - Requirements: [2.4](requirements.md#2.4), [2.5](requirements.md#2.5), [2.6](requirements.md#2.6)

- [x] 22. Implement NoteEditorViewModel and NoteEditorSheet <!-- id:mz8g5ep -->
  - Files: Flux/Flux/DayDetail/NoteEditorViewModel.swift and NoteEditorSheet.swift.
  - View: sheet-presented NavigationStack with TextEditor pre-populated, remaining-character counter (NoteText.maxGraphemes - characterCount), Save button bound to canSave.
  - View model owns draft, isSaving, error: FluxAPIError?; calls parent.saveNote and on throw stays open with text intact and surfaces error.message.
  - Mirror the SettingsView sheet pattern (Done/Cancel toolbar).
  - Blocked-by: mz8g5eo (Write NoteEditorViewModel tests)
  - Stream: 2
  - Requirements: [2.3](requirements.md#2.3), [2.4](requirements.md#2.4), [2.5](requirements.md#2.5), [2.6](requirements.md#2.6)

- [x] 23. Write DayDetailViewModel.saveNote tests <!-- id:mz8g5eq -->
  - Extend DayDetailViewModelTests.
  - Cover: note populated from DayDetailResponse on load; saveNote(rawText) calls api.saveNote with NFC+trimmed text and replaces note with response.text; saveNote("   ") (whitespace-only) results in note == nil after server confirms delete; saveNote propagates server FluxAPIError.badRequest on rejection; client-side over-cap input throws .badRequest WITHOUT calling the API (assert mock saw zero calls).
  - Blocked-by: mz8g5em (Implement saveNote on FluxAPIClient, URLSession, and Mock)
  - Stream: 2
  - Requirements: [1.4](requirements.md#1.4), [2.5](requirements.md#2.5), [2.6](requirements.md#2.6), [3.5](requirements.md#3.5)

- [x] 24. Add note state and saveNote to DayDetailViewModel <!-- id:mz8g5er -->
  - Edit Flux/Flux/DayDetail/DayDetailViewModel.swift.
  - Add private(set) var note: String? populated from DayDetailResponse.note.
  - Add func saveNote(_ rawText: String) async throws: pre-flight NoteText.normalised + graphemeCount cap (throw FluxAPIError.badRequest if over); call apiClient.saveNote(date:, text:); on success set self.note = resp.text.isEmpty ? nil : resp.text.
  - Blocked-by: mz8g5eq (Write DayDetailViewModel.saveNote tests)
  - Stream: 2
  - Requirements: [1.4](requirements.md#1.4), [2.5](requirements.md#2.5), [2.6](requirements.md#2.6)

- [x] 25. Implement shared NoteRowView read-only component <!-- id:mz8g5es -->
  - File: Flux/Flux/DayDetail/NoteRowView.swift.
  - Read-only row rendering text using HistoryCardChrome-equivalent chrome.
  - Used by Dashboard, History, and Day Detail (view-only state on Day Detail).
  - Returns an EmptyView when text is nil so callers can place it unconditionally and have it collapse.
  - Stream: 2
  - Requirements: [2.1](requirements.md#2.1), [3.1](requirements.md#3.1), [3.2](requirements.md#3.2), [4.1](requirements.md#4.1), [4.4](requirements.md#4.4)

- [x] 26. Write HistoryViewModel selected-day note tests <!-- id:mz8g5et -->
  - Extend HistoryViewModelTests.
  - Cover: selectedDay.note reflects the current selection; changing selection (drag-select / summary nav) updates the surfaced note; CachedDayEnergy round-trip (init(from:) + asDayEnergy) preserves note across cache load; cached-fallback path renders notes when offline path returns from SwiftData.
  - Blocked-by: mz8g5en (Add note field to CachedDayEnergy)
  - Stream: 2
  - Requirements: [4.1](requirements.md#4.1), [4.2](requirements.md#4.2), [4.4](requirements.md#4.4)

- [x] 27. Wire NoteRowView into Day Detail, Dashboard, and History views <!-- id:mz8g5eu -->
  - Day Detail (DayDetailView.swift): note row as first child of VStack; tap to present NoteEditorSheet pre-populated from viewModel.note; "Add note" button row when note == nil and date <= today Sydney; render nothing for future dates (AC 2.7).
  - Dashboard (DashboardView.swift): NoteRowView(text: viewModel.status?.note) between BatteryHeroView and PowerTrioView (collapses when nil; read-only).
  - History (HistoryView.swift): NoteRowView(text: selectedDay?.note) between the picker and the chart cards (collapses when nil; read-only).
  - Refresh-on-return for Dashboard (AC 3.5) is already covered by the existing onAppear → startAutoRefresh → refresh chain — no new code needed there, just confirm by manual reading.
  - Blocked-by: mz8g5ep (Implement NoteEditorViewModel and NoteEditorSheet), mz8g5er (Add note state and saveNote to DayDetailViewModel), mz8g5es (Implement shared NoteRowView read-only component), mz8g5et (Write HistoryViewModel selected-day note tests)
  - Stream: 2
  - Requirements: [2.1](requirements.md#2.1), [2.2](requirements.md#2.2), [2.3](requirements.md#2.3), [2.7](requirements.md#2.7), [3.1](requirements.md#3.1), [3.2](requirements.md#3.2), [3.3](requirements.md#3.3), [4.1](requirements.md#4.1), [4.2](requirements.md#4.2), [4.3](requirements.md#4.3), [4.4](requirements.md#4.4)
