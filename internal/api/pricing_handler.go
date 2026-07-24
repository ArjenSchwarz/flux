package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math"
	"net/http"
	"time"

	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/ArjenSchwarz/flux/internal/plan"
)

// pricingPayload is the wire shape of POST /pricing and PUT /pricing/{id}.
//
// The plan is transmitted as entered — a default rate plus the exception
// windows that deviate from it (Decision 4) — and endDate is the exclusive
// switch date (Decision 5), stored verbatim. Rates are pointers so a missing
// field is distinguishable from an explicit zero; the validator rejects more
// than 4 decimal places before anything is rounded.
type pricingPayload struct {
	StartDate            string          `json:"startDate"`
	EndDate              *string         `json:"endDate,omitempty"`
	DefaultRate          *float64        `json:"defaultRate"`
	Windows              []windowPayload `json:"windows"`
	FeedInRate           *float64        `json:"feedInRate"`
	SavingsReferenceRate *float64        `json:"savingsReferenceRate,omitempty"`
}

// windowPayload is one exception window. Rate is absent on a free window.
type windowPayload struct {
	Start string   `json:"start"`
	End   string   `json:"end"`
	Free  bool     `json:"free"`
	Rate  *float64 `json:"rate,omitempty"`
}

// replaceOpenEndedPayload is the wire shape of
// POST /pricing/replace-open-ended. NewPeriod stays raw so the legacy-shape
// check below can inspect its keys before decoding.
type replaceOpenEndedPayload struct {
	ClosingPricingID string          `json:"closingPricingId"`
	NewPeriod        json.RawMessage `json:"newPeriod"`
}

// legacyPayloadMarker is the field whose presence identifies a pre-migration
// three-rate payload. The band shape has no such field.
const legacyPayloadMarker = "peakRate"

// SetPricingStore wires the pricing CRUD dependency. Called by
// cmd/api/main.go and rebuilds the mux so the routes pick up the store.
func (h *Handler) SetPricingStore(s PricingStore) {
	h.pricing = s
}

// pricingError is the JSON response shape for every pricing error.
//
// `conflictingPricingId` names the plan an overlap collides with (AC 2.5).
// `openEndedId` is populated only when that offender is the unique
// open-ended plan — the editor needs the id to surface the one-tap
// remediation from AC 6.5.
type pricingError struct {
	Error                string `json:"error"`
	Message              string `json:"message"`
	OpenEndedID          string `json:"openEndedId,omitempty"`
	ConflictingPricingID string `json:"conflictingPricingId,omitempty"`
}

// writePricingError serialises a {"error","message"} response with the
// given HTTP status. Unlike writeJSONError, this carries the machine-parseable
// code in the "error" field and the human-readable description in "message".
func writePricingError(w http.ResponseWriter, status int, code, message string) {
	body, err := json.Marshal(pricingError{Error: code, Message: message})
	if err != nil {
		slog.Error("marshal pricing error", "error", err)
		body = []byte(`{"error":"internal_error","message":"failed to serialise error"}`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// writePricingOverlapError names the plan the candidate collides with, and
// additionally surfaces it as `openEndedId` when that plan is the unique
// open-ended one — the id that powers the editor's remediation flow.
func writePricingOverlapError(w http.ResponseWriter, conflictingID string, isOpenEnded bool) {
	payload := pricingError{
		Error:                pricingCodeOverlap,
		Message:              "pricing plan overlaps existing plan " + conflictingID,
		ConflictingPricingID: conflictingID,
	}
	if isOpenEnded {
		payload.OpenEndedID = conflictingID
	}
	body, err := json.Marshal(payload)
	if err != nil {
		slog.Error("marshal pricing overlap error", "error", err)
		body = []byte(`{"error":"overlap","message":"pricing plan overlaps an existing one"}`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_, _ = w.Write(body)
}

// handleListPricing returns every pricing plan sorted by startDate ascending.
func (h *Handler) handleListPricing(w http.ResponseWriter, r *http.Request) {
	if h.pricing == nil {
		writePricingError(w, http.StatusInternalServerError, pricingCodeInternal, "pricing store not configured")
		return
	}
	rows, err := h.pricing.ListPricing(r.Context())
	if err != nil {
		mapPricingStoreError(w, "list pricing", err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Pricing []dynamo.PricingItem `json:"pricing"`
	}{Pricing: rows})
}

// handleCreatePricing validates the payload, assigns server-side
// id/timestamps, and writes the row.
func (h *Handler) handleCreatePricing(w http.ResponseWriter, r *http.Request) {
	if h.pricing == nil {
		writePricingError(w, http.StatusInternalServerError, pricingCodeInternal, "pricing store not configured")
		return
	}
	payload, ok := decodePricingPayload(w, r)
	if !ok {
		return
	}

	existing, err := h.pricing.ListPricing(r.Context())
	if err != nil {
		mapPricingStoreError(w, "list pricing during create", err)
		return
	}
	if !runPricingValidationChain(w, payload, existing, "") {
		return
	}

	prevOpenEndedID, ok := loadPrevOpenEndedID(w, r.Context(), h.pricing, "create")
	if !ok {
		return
	}
	now := h.nowFunc().UTC().Format(time.RFC3339)
	item := payload.toItem(h.idFunc(), now, now)

	if err := h.pricing.PutPricing(r.Context(), item, prevOpenEndedID); err != nil {
		mapPricingStoreError(w, "put pricing", err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

// handleUpdatePricing validates and overwrites an existing pricing row.
// Decision 17: the row being updated is excluded from the overlap check.
func (h *Handler) handleUpdatePricing(w http.ResponseWriter, r *http.Request) {
	if h.pricing == nil {
		writePricingError(w, http.StatusInternalServerError, pricingCodeInternal, "pricing store not configured")
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writePricingError(w, http.StatusBadRequest, pricingCodeBadRequest, "id required")
		return
	}
	payload, ok := decodePricingPayload(w, r)
	if !ok {
		return
	}

	existing, err := h.pricing.ListPricing(r.Context())
	if err != nil {
		mapPricingStoreError(w, "list pricing during update", err)
		return
	}
	var current *dynamo.PricingItem
	for i := range existing {
		if existing[i].PricingID == id {
			current = &existing[i]
			break
		}
	}
	if current == nil {
		writePricingError(w, http.StatusNotFound, pricingCodeNotFound, "pricing plan not found")
		return
	}
	if !runPricingValidationChain(w, payload, existing, id) {
		return
	}

	prevOpenEndedID, ok := loadPrevOpenEndedID(w, r.Context(), h.pricing, "update")
	if !ok {
		return
	}
	now := h.nowFunc().UTC().Format(time.RFC3339)
	item := payload.toItem(id, current.CreatedAt, now)

	if err := h.pricing.UpdatePricing(r.Context(), item, prevOpenEndedID); err != nil {
		mapPricingStoreError(w, "update pricing", err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// handleDeletePricing removes a pricing row by id. Decision 11: 404 on
// unknown id, 204 on success.
func (h *Handler) handleDeletePricing(w http.ResponseWriter, r *http.Request) {
	if h.pricing == nil {
		writePricingError(w, http.StatusInternalServerError, pricingCodeInternal, "pricing store not configured")
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writePricingError(w, http.StatusBadRequest, pricingCodeBadRequest, "id required")
		return
	}
	existing, err := h.pricing.GetPricing(r.Context(), id)
	if err != nil {
		mapPricingStoreError(w, "get pricing during delete", err)
		return
	}
	if existing == nil {
		writePricingError(w, http.StatusNotFound, pricingCodeNotFound, "pricing plan not found")
		return
	}
	prevOpenEndedID, ok := loadPrevOpenEndedID(w, r.Context(), h.pricing, "delete")
	if !ok {
		return
	}
	if err := h.pricing.DeletePricing(r.Context(), id, prevOpenEndedID); err != nil {
		mapPricingStoreError(w, "delete pricing", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNoContent)
}

// handleReplaceOpenEnded atomically closes the existing open-ended plan on the
// successor's start date and inserts the successor (AC 2.2/2.6). Both rows
// carry the same literal date: the predecessor's exclusive end is the
// successor's inclusive start, so the switch day belongs to the successor.
func (h *Handler) handleReplaceOpenEnded(w http.ResponseWriter, r *http.Request) {
	if h.pricing == nil {
		writePricingError(w, http.StatusInternalServerError, pricingCodeInternal, "pricing store not configured")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, pricingBodyMaxBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writePricingError(w, http.StatusBadRequest, pricingCodeBadRequest, "malformed request body")
		return
	}
	var payload replaceOpenEndedPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		writePricingError(w, http.StatusBadRequest, pricingCodeBadRequest, "malformed request body")
		return
	}
	if payload.ClosingPricingID == "" {
		writePricingError(w, http.StatusBadRequest, pricingCodeBadRequest, "closingPricingId required")
		return
	}
	newPeriod, ok := parsePricingPayload(w, payload.NewPeriod)
	if !ok {
		return
	}

	existing, err := h.pricing.ListPricing(r.Context())
	if err != nil {
		mapPricingStoreError(w, "list pricing during replace-open-ended", err)
		return
	}
	var closing *dynamo.PricingItem
	for i := range existing {
		if existing[i].PricingID == payload.ClosingPricingID {
			closing = &existing[i]
			break
		}
	}
	if closing == nil {
		writePricingError(w, http.StatusNotFound, pricingCodeNotFound, "closing pricing plan not found")
		return
	}
	if closing.EndDate != nil {
		writePricingError(w, http.StatusBadRequest, pricingCodeSecondOpenEnded, "closing plan is not open-ended")
		return
	}

	// Format-validate the successor's startDate up front so a malformed value
	// surfaces with the same error code the validation chain uses elsewhere,
	// not as a muddied "newPeriod.startDate invalid". The full chain runs
	// below against the projected post-write state.
	if !plan.ValidDate(newPeriod.StartDate) {
		writePricingError(w, http.StatusBadRequest, pricingCodeInvertedDates, "newPeriod.startDate must be YYYY-MM-DD")
		return
	}
	// Under exclusive end dates the closing row ends on the successor's start
	// date — the same literal string, no ±1 arithmetic (Decision 5).
	closingEndDate := newPeriod.StartDate

	// The validation chain below only ever sees the successor payload, so the
	// projected closing row's own dates have to be checked here. A successor
	// starting at or before the closing plan's start date would cap it at
	// endDate <= startDate — the zero-day/inverted plan plan.Validate rejects on
	// every other write path. The overlap check cannot catch it: a zero-day
	// half-open range intersects nothing.
	if closingEndDate <= closing.StartDate {
		writePricingError(w, http.StatusBadRequest, pricingCodeInvertedDates,
			"newPeriod.startDate must be after the closing plan's startDate")
		return
	}

	// Simulate the resulting two-row state (closing capped at the switch
	// date; new row inserted) and run the same validation chain against it.
	projected := make([]dynamo.PricingItem, 0, len(existing)+1)
	for _, row := range existing {
		if row.PricingID == payload.ClosingPricingID {
			rowCopy := row
			end := closingEndDate
			rowCopy.EndDate = &end
			projected = append(projected, rowCopy)
		} else {
			projected = append(projected, row)
		}
	}
	if !runPricingValidationChain(w, newPeriod, projected, "") {
		return
	}

	now := h.nowFunc().UTC().Format(time.RFC3339)
	newItem := newPeriod.toItem(h.idFunc(), now, now)
	if err := h.pricing.ReplaceOpenEnded(r.Context(), payload.ClosingPricingID, closingEndDate, now, newItem); err != nil {
		mapPricingStoreError(w, "replace open-ended pricing", err)
		return
	}

	// Return the resulting pair from the in-memory transaction inputs so
	// the client can fold both rows back into its local list without a
	// second fetch. Re-scanning here would be eventually-consistent and
	// could return stale rows for the just-committed transaction. The
	// `now` value is the same string the store wrote into the closing
	// row, so the response carries the canonical updatedAt.
	closingRow := *closing
	closingRow.EndDate = &closingEndDate
	closingRow.UpdatedAt = now
	writeJSON(w, http.StatusOK, struct {
		Pricing []dynamo.PricingItem `json:"pricing"`
	}{Pricing: []dynamo.PricingItem{closingRow, newItem}})
}

// decodePricingPayload reads, size-limits, and decodes the request body. On
// failure the response is already written; callers return early.
func decodePricingPayload(w http.ResponseWriter, r *http.Request) (pricingPayload, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, pricingBodyMaxBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writePricingError(w, http.StatusBadRequest, pricingCodeBadRequest, "malformed request body")
		return pricingPayload{}, false
	}
	return parsePricingPayload(w, body)
}

// parsePricingPayload decodes one plan payload, rejecting the legacy
// three-rate shape first (AC 7.3).
//
// Detection runs on the raw JSON keys because encoding/json silently drops
// unknown fields: a legacy body would otherwise decode into pricingPayload as
// a windowless plan with every rate at zero, which is a valid band plan and
// would be stored as one.
func parsePricingPayload(w http.ResponseWriter, raw []byte) (pricingPayload, bool) {
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		writePricingError(w, http.StatusBadRequest, pricingCodeBadRequest, "malformed request body")
		return pricingPayload{}, false
	}
	if _, legacy := keys[legacyPayloadMarker]; legacy {
		writePricingError(w, http.StatusBadRequest, pricingCodeLegacyShape,
			"three-rate pricing plans are no longer accepted; send defaultRate and windows")
		return pricingPayload{}, false
	}
	var payload pricingPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		writePricingError(w, http.StatusBadRequest, pricingCodeBadRequest, "malformed request body")
		return pricingPayload{}, false
	}
	return payload, true
}

// domainPlan converts the wire payload into the domain plan the validation and
// segmentation helpers operate on. Rates pass through unrounded: the precision
// rule fires on what the client actually sent, so rounding here would make it
// unfireable.
func (p pricingPayload) domainPlan(id string) plan.Plan {
	windows := make([]plan.Window, len(p.Windows))
	for i, w := range p.Windows {
		windows[i] = plan.Window{Start: w.Start, End: w.End, Free: w.Free}
		if !w.Free {
			windows[i].Rate = deref(w.Rate)
		}
	}
	end := ""
	if p.EndDate != nil {
		end = *p.EndDate
	}
	result := plan.Plan{
		ID:          id,
		StartDate:   p.StartDate,
		EndDate:     end,
		DefaultRate: deref(p.DefaultRate),
		Windows:     windows,
		FeedInRate:  deref(p.FeedInRate),
	}
	if p.SavingsReferenceRate != nil {
		savings := *p.SavingsReferenceRate
		result.SavingsRefRate = &savings
	}
	return result
}

// toItem builds the storage row from the wire payload plus the server-assigned
// id and timestamps. Rates are normalised to exactly four decimal places
// (Decision 10 / Decision 20) — validation has already rejected anything
// finer, so this only removes float representation noise.
func (p pricingPayload) toItem(id, createdAt, updatedAt string) dynamo.PricingItem {
	dp := p.domainPlan(id)
	windows := make([]dynamo.PricingWindow, len(dp.Windows))
	for i, w := range dp.Windows {
		windows[i] = dynamo.PricingWindow{Start: w.Start, End: w.End, Free: w.Free}
		if !w.Free {
			rate := roundTo4DP(w.Rate)
			windows[i].Rate = &rate
		}
	}
	item := dynamo.PricingItem{
		PricingID:   id,
		StartDate:   dp.StartDate,
		DefaultRate: roundTo4DP(dp.DefaultRate),
		Windows:     windows,
		FeedInRate:  roundTo4DP(dp.FeedInRate),
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}
	if dp.SavingsRefRate != nil {
		savings := roundTo4DP(*dp.SavingsRefRate)
		item.SavingsReferenceRate = &savings
	}
	if dp.EndDate != "" {
		end := dp.EndDate
		item.EndDate = &end
	}
	return item
}

func deref(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

// runPricingValidationChain reports the first violated rule, in the order
// inverted_dates → overlap → the remaining single-plan band rules →
// second_open_ended. Writes the failure and returns false; on success returns
// true with no response written.
//
// The single-plan rules come from plan.Validate, which sees one plan at a
// time; the date-range overlap and single-open-ended rules need the whole plan
// set and so are checked here. Date validity is pulled ahead of the overlap
// check because an unparseable date makes the range comparison meaningless.
//
// `excludeID` is the id of the row being updated; empty on create.
func runPricingValidationChain(w http.ResponseWriter, p pricingPayload, existing []dynamo.PricingItem, excludeID string) bool {
	errs := p.domainPlan(excludeID).Validate()
	for _, e := range errs {
		if e.Code == plan.CodeInvertedDates {
			writePricingError(w, http.StatusBadRequest, e.Code, e.Message)
			return false
		}
	}
	if !validateOverlap(w, p, existing, excludeID) {
		return false
	}
	if len(errs) > 0 {
		writePricingError(w, http.StatusBadRequest, errs[0].Code, errs[0].Message)
		return false
	}
	return validateSecondOpenEnded(w, p, existing, excludeID)
}

// validateOverlap fires when the candidate's date range intersects any
// existing plan's range (excluding excludeID), naming the offender per AC 2.5.
// Both sides are half-open [start, end) with exclusive end dates, so a plan
// ending on the day its successor starts does not overlap it (AC 2.2).
// Open-ended is modelled as endDate = "9999-12-31".
func validateOverlap(w http.ResponseWriter, p pricingPayload, existing []dynamo.PricingItem, excludeID string) bool {
	candEnd := pricingMaxEndDate
	if p.EndDate != nil {
		candEnd = *p.EndDate
	}
	for _, row := range existing {
		if row.PricingID == excludeID {
			continue
		}
		rowEnd := pricingMaxEndDate
		if row.EndDate != nil {
			rowEnd = *row.EndDate
		}
		// Half-open intervals intersect iff each starts before the other ends.
		if p.StartDate < rowEnd && candEnd > row.StartDate {
			writePricingOverlapError(w, row.PricingID, row.EndDate == nil)
			return false
		}
	}
	return true
}

// validateSecondOpenEnded fires when the candidate is open-ended and another
// existing row (other than excludeID) is also open-ended.
func validateSecondOpenEnded(w http.ResponseWriter, p pricingPayload, existing []dynamo.PricingItem, excludeID string) bool {
	if p.EndDate != nil {
		return true
	}
	for _, row := range existing {
		if row.PricingID == excludeID {
			continue
		}
		if row.EndDate == nil {
			writePricingError(w, http.StatusBadRequest, pricingCodeSecondOpenEnded, "another pricing plan is already open-ended")
			return false
		}
	}
	return true
}

// loadPrevOpenEndedID reads the sentinel and returns its OpenEndedID
// value for the transactional ConditionExpression. The sentinel is the
// authoritative source for "which row is currently open-ended" per
// Decision 21 — the list-derived value can drift under partial-write
// recovery, so every write path that needs prevOpenEndedID should go
// through this helper. On read failure the response is already written;
// the caller returns early.
func loadPrevOpenEndedID(w http.ResponseWriter, ctx context.Context, store PricingStore, op string) (*string, bool) {
	sentinel, err := store.GetSentinel(ctx)
	if err != nil {
		slog.Error("get sentinel failed", "op", op, "error", err)
		writePricingError(w, http.StatusInternalServerError, pricingCodeInternal, "get sentinel failed")
		return nil, false
	}
	if sentinel == nil {
		return nil, true
	}
	return sentinel.OpenEndedID, true
}

// mapPricingStoreError translates typed errors from the dynamo store
// into the appropriate HTTP response. Anything unrecognised falls
// through to HTTP 500 with the raw error logged.
func mapPricingStoreError(w http.ResponseWriter, op string, err error) {
	switch {
	case errors.Is(err, dynamo.ErrPricingConcurrentWrite):
		writePricingError(w, http.StatusConflict, pricingCodeConcurrentWrite, "concurrent open-ended write detected")
	case errors.Is(err, dynamo.ErrPricingLegacyShape):
		// Raised by succession refusing to patch a not-yet-migrated closing row
		// (Q32), and — since the transitional read conversion was removed
		// (task 39) — by any read that encounters a legacy row. Both mean the
		// same thing to an operator, and both are fixed the same way, so the
		// response says what to do rather than reporting a generic failure.
		slog.Warn("pricing blocked by legacy row", "op", op, "error", err)
		writePricingError(w, http.StatusBadRequest, pricingCodeLegacyShape,
			"the pricing table still holds a legacy three-rate row; run cmd/migrate-pricing first")
	case errors.Is(err, dynamo.ErrPricingUUIDCollision):
		slog.Warn("pricing uuid collision", "op", op, "error", err)
		writePricingError(w, http.StatusInternalServerError, pricingCodeInternal, "uuid collision; retry")
	default:
		slog.Error("pricing store error", "op", op, "error", err)
		writePricingError(w, http.StatusInternalServerError, pricingCodeInternal, "internal error")
	}
}

// roundTo4DP rounds v to exactly four decimal places. Used to normalise
// accepted rates on write so 0.28729999… stored as 0.2873.
func roundTo4DP(v float64) float64 {
	return math.Round(v*10000) / 10000
}
