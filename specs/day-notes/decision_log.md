# Decision Log: Day Notes

## Decision 1: Server-side storage in a new DynamoDB table

**Date**: 2026-04-27
**Status**: accepted

### Context

Notes need to be visible to both users of the system on multiple devices and survive iOS reinstalls. The existing Lambda API is read-only and the iOS app caches via SwiftData. We need to choose where notes live.

### Decision

Store notes server-side in a new `flux-notes` DynamoDB table (partition `sysSn`, sort `date`), exposed by the Lambda API. The Lambda API gains its first write endpoint.

### Rationale

The system is shared between two users on different devices; notes must sync. CloudKit only syncs within one Apple ID, so it does not solve the multi-user case. Local-only SwiftData would lose notes on reinstall. DynamoDB with the existing bearer-token auth fits the architecture's pattern (one auth path, one API).

### Alternatives Considered

- **SwiftData (local-only)**: No backend changes — Rejected because notes would not sync between users or survive reinstall.
- **SwiftData + iCloud (CloudKit)**: Per-Apple-ID sync — Rejected because it does not sync between two different users.

### Consequences

**Positive:**
- Notes follow the existing data model and IAM patterns.
- Both users see the same note for any given date.
- One round-trip per page (note bundled into existing endpoints).

**Negative:**
- First Lambda write endpoint introduces new IAM scope and method allowance.
- Adds a new DynamoDB table managed in the existing CloudFormation stack with `Retain` policies (matching the other Flux tables); no SSM SecureString work is required because the notes themselves are not secrets.

---

## Decision 2: One free-form text note per day, editable

**Date**: 2026-04-27
**Status**: accepted

### Context

Notes could be a single field, multiple log entries, structured (title + body), or categorised. We need a shape that matches the use case ("explain why this day is different") without growing into a journal feature.

### Decision

Each date holds at most one note. Notes are a single free-form text field. Editing replaces the note. Saving an empty (or whitespace-only) note deletes it.

### Rationale

The use case is "annotate a day with context" — a single explanation per day. Categories ossify quickly and become wrong. Title + body is overkill for a one-line context note. "Clearing text deletes" gives a single mental model: "what's in the box is the note."

### Alternatives Considered

- **Multiple notes per day, append-only**: More flexible — Rejected; adds list UI and per-note delete; not justified by the use case.
- **Free text + category enum**: Structured filtering — Rejected; categories will not stay accurate.

### Consequences

**Positive:**
- Single field; no list UI; no per-item delete.
- Save semantics are clear (write-with-empty == delete).

**Negative:**
- No way to keep older versions of a note if the user overwrites it.

---

## Decision 3: Notes only for today and past dates

**Date**: 2026-04-27
**Status**: accepted

### Context

Notes could be added to any date including the future ("we'll be away next week"). The use case framing is explanatory ("explain why a day is different"), which is retrospective.

### Decision

Notes can only be added or edited for today (Sydney) and past dates. Future dates show no add/edit affordance. The API rejects future-dated writes with 400.

### Rationale

Keeps the feature focused on its stated purpose. Future-dated planning would invite drift (planned events that get cancelled, dates that move) and adds UI ambiguity ("is this what happened or what was planned?").

### Alternatives Considered

- **Any date including future**: Allow planning ahead — Rejected; widens scope and changes the feature's purpose.
- **Only dates with data**: Restrict to dates the system has polled — Rejected; awkward for first install and data gaps.

### Consequences

**Positive:**
- One mental model per surface: "this is the explanation for what happened."
- Server validation is straightforward (date <= today Sydney).

**Negative:**
- Cannot pre-write a note for an upcoming holiday.

---

## Decision 4: Note rendered prominently on Dashboard, History, and Day Detail; edited only on Day Detail

**Date**: 2026-04-27
**Status**: accepted

### Context

Notes must be visible "when looking at historical data" and on the day they apply to. The app has three relevant surfaces: Dashboard (today live), History (range with selected day), Day Detail (single day). Editing on every surface adds UI weight.

### Decision

- Day Detail: full note text at the top; tap to open editor; Add affordance when empty.
- Dashboard: today's note rendered above secondary stats, read-only.
- History: selected day's note rendered at the top of the page, driven by the existing `selectedDay` state that already drives the summary card; read-only.

### Rationale

Day Detail is the canonical per-day surface, so editing belongs there. Dashboard and History are scanning surfaces — read-only display avoids accidental edits and keeps those views simple. Reusing the History `selectedDay` mechanism avoids new selection state.

### Alternatives Considered

- **Edit on every surface**: Most accessible — Rejected; multiplies UI and adds inconsistency between Dashboard's "today" and History's "selected day."
- **Day Detail only (no Dashboard/History rendering)**: Simplest — Rejected; defeats the "explain why this day is different at a glance" goal stated in the ticket.
- **Icon indicator on History instead of full text**: Less screen real estate — Rejected by user; full text is more informative.

### Consequences

**Positive:**
- One write path, three read paths.
- Dashboard and History rendering reuses one note-row component.

**Negative:**
- User must navigate to Day Detail to edit.

---

## Decision 5: 200-character note limit

**Date**: 2026-04-27
**Status**: accepted

### Context

Notes need a length limit to keep render heights bounded and to discipline "context note" vs "journal entry." DynamoDB items have a 400KB cap which is irrelevant here; the cap is a UI/product cap.

### Decision

Notes are limited to 200 characters. The editor shows a remaining-character count and prevents submission past the limit. The API enforces the same limit and returns 400 if violated.

### Rationale

200 characters fits the "this is why this day was different" framing — one to two sentences. Tweet-sized discipline keeps the rendered note from overwhelming the surfaces it appears on.

### Alternatives Considered

- **500 characters**: More room for context — Rejected; risks turning notes into mini-journals and bloating Dashboard layout.
- **No limit**: Maximum flexibility — Rejected; risks pathological inputs and unbounded UI height.

### Consequences

**Positive:**
- Predictable rendering height across surfaces.
- Tighter discipline produces more useful notes.

**Negative:**
- Some context may need to be abbreviated.

---

## Decision 6: Save errors stay in the editor; no offline queue

**Date**: 2026-04-27
**Status**: accepted

### Context

Network/auth failures on save need a defined behaviour. Options range from "fail and ask user to retry" to "queue locally and sync later."

### Decision

When a save fails, the editor stays open with the user's text intact and shows an error message. The user retries manually. No background queue.

### Rationale

Day Notes is a low-frequency, opt-in action (a few edits a month at most). An offline queue introduces local persistence, conflict resolution, and sync-state UI whose cost outweighs the benefit for that volume. Failing fast with a visible error and the user's text intact is the smallest contract that meets the use case.

### Alternatives Considered

- **Queue locally and retry**: Optimistic UX — Rejected; large scope expansion for low-frequency edits.

### Consequences

**Positive:**
- Save semantics are simple: succeed (server has it) or fail (user sees error).
- No local note storage to keep in sync with the server.

**Negative:**
- User must be online to save.

---

## Decision 7: Character limit counted as 200 grapheme clusters after NFC normalisation

**Date**: 2026-04-27
**Status**: accepted

### Context

"200 characters" is ambiguous across stacks. Go's `len()` returns bytes; Swift's `String.count` returns Unicode scalars; `String.utf16.count` returns UTF-16 code units. An emoji can be 1 grapheme, 2 code points, 4 UTF-16 units. If the iOS editor and the Go server count differently, users will see "the editor said 198, the API said 400 too long" and vice versa.

### Decision

The 200-character limit is defined as **200 Unicode grapheme clusters after Unicode NFC normalisation, with leading and trailing whitespace trimmed first**. The iOS editor and the Go server compute the same count using the same definition. Both sides perform NFC normalisation before counting and before storing.

### Rationale

Grapheme clusters are what users perceive as "one character" (one emoji = 1, one accented letter regardless of composition = 1). NFC normalisation eliminates the "two visually-identical strings count differently" trap. Internal whitespace is preserved (per AC 1.2) but leading/trailing whitespace is stripped before counting and storage so the user can't accidentally pad to 199 + 200 spaces.

### Alternatives Considered

- **Go `len()` (UTF-8 bytes)**: Cheap to implement — Rejected; a 200-byte limit truncates around 50–66 ideographs and produces inconsistent UI feedback.
- **Swift `String.count` (Unicode scalars)**: Easy on iOS — Rejected; doesn't match grapheme perception (a flag emoji is 2 scalars, 1 grapheme).
- **No normalisation**: Less code — Rejected; "café" composed differently can fail/pass depending on input method.

### Consequences

**Positive:**
- Editor character count and API rejection threshold agree.
- User-perceived "one character" maps to one count tick.

**Negative:**
- Both client and server need a consistent grapheme-cluster counting library; Swift's `unicodeScalars`/`Character` and Go's `golang.org/x/text/unicode/norm` + `rivo/uniseg` (or equivalent) must be wired up.

---

## Decision 8: Server is the authoritative clock for "today (Sydney)"

**Date**: 2026-04-27
**Status**: accepted

### Context

The editor disables future-date affordances and the API rejects future-dated writes. But "today" depends on which clock you ask. iOS devices may be set to any timezone; the server runs in UTC but interprets dates as Sydney local time. Two clocks can disagree across timezones and across DST transitions.

### Decision

The server is the authoritative clock for "today" in Sydney local time. The iOS client computes "today" in Sydney for UI affordances but, on submit, the server re-validates and may reject. iOS treats the server's rejection as the final answer.

### Rationale

Single source of truth eliminates the 23:59-Sydney-on-iPhone-set-to-NYC class of bugs. The server already has Sydney TZ embedded via `time/tzdata` and uses it for daily-energy date calculations. The iOS client's "today" check is a UX nicety (avoid presenting an editor for tomorrow) — not a security boundary.

### Alternatives Considered

- **Client decides "today"**: Single round-trip — Rejected; an iPhone in NYC at 11 PM Sydney time would let the user write a "tomorrow" note that the server later rejects, with no clear UX.
- **Both clocks must agree, fail otherwise**: Strict — Rejected; needlessly fails edits across DST or device-timezone changes.

### Consequences

**Positive:**
- One date-policy implementation and one source of correctness.
- DST and traveling devices stop being edge cases.

**Negative:**
- Client UI may briefly disagree with server policy at midnight Sydney; users see a 400 the moment after the new day rolls.

---

## Decision 9: Enable PITR on the notes table

**Date**: 2026-04-27
**Status**: accepted

### Context

Other Flux tables hold reconstructable data: readings can be re-polled from AlphaESS, daily energy can be recomputed, off-peak deltas can be rebuilt. Notes cannot — they are the only user-authored content in the system. A regression in the write endpoint or an accidental client bug could corrupt or delete notes with no recovery path.

### Decision

Enable Point-In-Time Recovery (PITR) on `flux-notes`. PITR is not enabled on the other Flux tables.

### Rationale

PITR provides 35 days of continuous backup and is the standard DynamoDB safety net for non-reconstructable data. At expected note volume (200 chars × 365 days × many years = single-digit MB), the cost is trivial (~$0.20/GB/month). Enabling PITR matches the existing `Retain` deletion-policy posture for the same reason: notes are deliberately durable.

### Alternatives Considered

- **No PITR (match other tables)**: Cheaper, simpler — Rejected; the failure mode is data loss with no recovery, which is the case PITR exists for.
- **DynamoDB on-demand backups + scheduled lambda**: More control — Rejected; PITR is a one-line CFN change with continuous coverage.

### Consequences

**Positive:**
- 35-day point-in-time restore on the only Flux data the user cannot recreate.
- Negligible cost at expected scale.

**Negative:**
- Slight CloudFormation change beyond the plain "table + IAM" pattern of other tables.

---

## Decision 10: Bundle notes into existing read endpoints rather than a separate read endpoint

**Date**: 2026-04-27
**Status**: accepted

### Context

Notes need to be visible on Dashboard (today), History (selected day in a 7/14/30-day range), and Day Detail (single day). They could be served by a dedicated `GET /notes?from=&to=` endpoint or bundled into the existing `/status`, `/history`, and `/day` responses.

### Decision

Bundle notes into the existing read responses: today's note in `/status`, per-day notes in `/history`, that-day's note in `/day`. No separate read endpoint.

### Rationale

Each existing surface already issues exactly one request per render. Bundling preserves that. At 200 chars per note × 30 days = ~6 KB worst-case `/history` payload addition, which is negligible. A separate endpoint would mean every screen issues two requests, doubling latency and complicating error states.

### Alternatives Considered

- **Dedicated `GET /notes` endpoint**: Cleaner separation of concerns — Rejected; doubles screen-load round trips for marginal architectural benefit.
- **WebSocket / push channel**: Real-time cross-device sync — Rejected; massive scope expansion, not warranted by the use case.

### Consequences

**Positive:**
- One round trip per screen.
- Cross-device read latency = client refresh cadence (10s on Dashboard, on-navigation elsewhere), no extra wiring needed.

**Negative:**
- Notes data shape couples to the existing endpoints; future redesign of those endpoints needs to consider notes.

---
