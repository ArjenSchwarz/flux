# Requirements: Day Notes

## Introduction

Day Notes lets users attach a short free-text note to a specific calendar date so that unusual energy patterns (holidays, parties, days away) become explainable when reviewing historical data. Notes are stored server-side in a new DynamoDB table and shared between users on the same system. Notes render prominently on Dashboard (today), History (selected day), and Day Detail; editing happens on Day Detail.

## Non-Goals

- Multiple notes per day, per-note timestamps shown to users, or threaded comments.
- Categorisation, tags, or templates ("holiday", "party", etc.).
- Authorship attribution, edit history, or per-user views.
- Conflict detection between concurrent edits — last-write-wins is intentional.
- Offline-write queue, optimistic UI, or background retry — saves require connectivity.
- A dedicated "all notes" browse/search screen in v1; may be added later as a read-only view (retention assumes this remains possible).
- Notes for future dates (planning).
- Notifications, reminders, or alerts based on notes.
- Notes attached to anything other than a calendar date (e.g. ranges, periods, individual readings).
- API-Gateway-style rate limiting; the Lambda Function URL plus bearer token is the v1 access boundary.

## Requirements

### 1. Note Persistence

**User Story:** As a user, I want my notes to be stored centrally and shared between both users of the system, so that either of us sees the same explanation when reviewing a day.

**Acceptance Criteria:**

1. <a name="1.1"></a>The system SHALL store at most one note per calendar date per system serial number.
2. <a name="1.2"></a>The system SHALL preserve internal whitespace and line breaks within stored note text exactly as submitted, while trimming only leading and trailing whitespace before length validation and storage.
3. <a name="1.3"></a>The system SHALL reject notes whose text exceeds 200 grapheme clusters (after Unicode NFC normalisation and leading/trailing trim) with a 400 response and an error message naming the limit.
4. <a name="1.4"></a>The system SHALL treat a save whose text is empty after trimming as a delete and remove any existing note for that date, idempotently (deleting a non-existent note returns success).
5. <a name="1.5"></a>WHEN concurrent save requests for the same date are received, the system SHALL apply last-write-wins with no conflict detection; this is intentional and not surfaced to users.
6. <a name="1.6"></a>The system SHALL retain notes indefinitely (no TTL).
7. <a name="1.7"></a>The system SHALL persist a server-generated `updatedAt` timestamp with each write, used for ordering and cache invalidation; not surfaced in v1 UI.

### 2. Note Authoring (Day Detail)

**User Story:** As a user reviewing a specific day, I want to add or edit that day's note from the Day Detail screen, so that I can explain context while looking at the data for that day.

**Acceptance Criteria:**

1. <a name="2.1"></a>The Day Detail screen SHALL render the day's note at the top of the page when one exists.
2. <a name="2.2"></a>The Day Detail screen SHALL show an "Add note" affordance at the same position when no note exists for that day.
3. <a name="2.3"></a>WHEN the user taps the rendered note (or the Add affordance), the app SHALL present an editor pre-populated with the existing text (or empty).
4. <a name="2.4"></a>The editor SHALL display a remaining-character count using the same grapheme-cluster definition the API enforces, and SHALL prevent submission once the input exceeds 200 grapheme clusters by disabling the Save action.
5. <a name="2.5"></a>WHEN the user saves successfully, the app SHALL dismiss the editor and update the displayed note in place without requiring an additional user-initiated action.
6. <a name="2.6"></a>IF the save request fails, the editor SHALL remain open with the user's text intact and SHALL display an error message describing the failure (using the same `{"error": "..."}` shape the existing endpoints return).
7. <a name="2.7"></a>The editor SHALL be available only for dates that are today or earlier in Sydney local time, where the server is the authoritative clock; future dates SHALL show neither an Add nor an Edit affordance.

### 3. Note Display (Dashboard)

**User Story:** As a user opening the Dashboard, I want to see today's note prominently if one exists, so that the current day's context informs the live readings I am looking at.

**Acceptance Criteria:**

1. <a name="3.1"></a>The Dashboard SHALL render today's note above the secondary stats section when one exists.
2. <a name="3.2"></a>The Dashboard SHALL render nothing in that position when today has no note (no empty placeholder, no Add affordance).
3. <a name="3.3"></a>The Dashboard's note display SHALL be read-only; editing from the Dashboard is out of scope.
4. <a name="3.4"></a>WHEN the Dashboard's `/status` poll completes (10s auto-refresh) the rendered note SHALL reflect the latest saved text returned by the server.
5. <a name="3.5"></a>WHEN the user returns to the Dashboard from another screen (including after editing today's note in Day Detail), the rendered note SHALL reflect the latest saved text without waiting for the next 10s poll.

### 4. Note Display (History)

**User Story:** As a user reviewing a range of past days, I want the selected day's note rendered on the History screen, so that I can read context without navigating into Day Detail.

**Acceptance Criteria:**

1. <a name="4.1"></a>The History screen SHALL render the currently selected day's note at the top of the page when one exists, scoped to whichever day the existing summary card already reflects.
2. <a name="4.2"></a>WHEN the user changes the selected day (via chart drag-select or summary navigation), the rendered note SHALL update to match the new selection.
3. <a name="4.3"></a>The History screen's note display SHALL be read-only.
4. <a name="4.4"></a>WHEN the selected day has no note, the History screen SHALL render nothing in the note position.

### 5. API Surface

**User Story:** As an iOS client, I want to read and write notes through the existing Lambda API with the existing bearer token, so that I do not need a separate auth path.

**Acceptance Criteria:**

1. <a name="5.1"></a>The Lambda API SHALL expose an upsert-or-delete write endpoint scoped to a single date that accepts the note text and returns the canonical stored representation (or confirmation of delete) on success.
2. <a name="5.2"></a>The write endpoint SHALL require a JSON request body and SHALL reject requests whose `Content-Type` is not `application/json` with 415.
3. <a name="5.3"></a>The write endpoint SHALL reject request bodies larger than 4 KB with 413, applied before any field-level validation.
4. <a name="5.4"></a>The write endpoint SHALL require the same bearer-token authorisation already used by `/status`, `/history`, and `/day`, returning 401 for missing or incorrect tokens before any routing.
5. <a name="5.5"></a>The system serial number used for note writes SHALL be derived from the Lambda's environment configuration; the system SHALL NOT accept a system serial number from the request body or URL.
6. <a name="5.6"></a>The Lambda API SHALL include the day's note (or null when absent) in the `/day` response under a single, documented field name.
7. <a name="5.7"></a>The Lambda API SHALL include each in-range day's note (or null when absent) in the `/history` response so History can render a note for any selected day without an extra request.
8. <a name="5.8"></a>The Lambda API SHALL include today's note (or null when absent) in the `/status` response so Dashboard receives a single response per refresh.
9. <a name="5.9"></a>The Lambda API SHALL validate that the supplied date is a valid Gregorian calendar date in `YYYY-MM-DD` form and is not later than today in Sydney local time (server-determined), returning 400 otherwise.
10. <a name="5.10"></a>The Lambda API SHALL return 400 when the supplied text exceeds 200 grapheme clusters after trimming and NFC normalisation, with an error message naming the limit.
11. <a name="5.11"></a>All error responses from the write endpoint SHALL match the existing `{"error": "..."}` JSON shape returned by the read endpoints.
12. <a name="5.12"></a>The write endpoint's structured logs SHALL match the existing slog format (method, path, status, duration_ms) and SHALL NOT log the note text.

### 6. Note Lifecycle on Backend

**User Story:** As an operator, I want note storage to follow the project's existing DynamoDB conventions, so that backups, IAM, and cost behave like the other tables.

**Acceptance Criteria:**

1. <a name="6.1"></a>The new DynamoDB table SHALL scope notes per system per date, matching the partition+sort convention used by the other Flux tables.
2. <a name="6.2"></a>The new DynamoDB table SHALL match the billing and retention posture of the other Flux tables (on-demand billing; `Retain` deletion policy).
3. <a name="6.3"></a>The new DynamoDB table SHALL have Point-In-Time Recovery enabled, since notes are the only user-authored, non-reconstructable data in Flux.
4. <a name="6.4"></a>The Lambda execution role SHALL have only read access on existing tables (status quo) and SHALL gain read+write access on the new notes table only; the role SHALL NOT gain `PutItem`, `UpdateItem`, `DeleteItem`, `BatchWriteItem`, or `TransactWriteItems` rights on any table other than the notes table.
5. <a name="6.5"></a>The CloudFormation change introducing the notes table, the IAM update, the Lambda environment variable for the table name, and the Lambda code change SHALL be deployable in a single stack update with no manual ordering required.

### 7. Threat Model and Operational Posture

**User Story:** As an operator, I want the threat surface and recovery posture for the first write endpoint stated, so that the team knows what is and isn't covered in v1.

**Acceptance Criteria:**

1. <a name="7.1"></a>The system SHALL treat the existing bearer token as the sole access control for the write endpoint; v1 SHALL NOT add rate limiting, IP allow-listing, or per-token quotas.
2. <a name="7.2"></a>WHEN the write endpoint receives a method other than the documented one, the system SHALL return 405 with an `Allow` header, matching the existing read endpoints.
3. <a name="7.3"></a>The rollback plan for the feature SHALL be: revert the Lambda code; the notes table is retained by `DeletionPolicy: Retain` and PITR; iOS clients tolerate the absence of note fields in API responses.

## Open Questions

None outstanding.
