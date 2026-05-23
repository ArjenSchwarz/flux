package api

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math"
	"net/http"
	"time"

	"github.com/ArjenSchwarz/flux/internal/dynamo"
)

// pricingPayload is the wire shape of POST /pricing and PUT
// /pricing/{id}. JSON numbers decode into float64 unchanged; the
// validator rejects > 4 decimal places before rounding.
type pricingPayload struct {
	StartDate          string   `json:"startDate"`
	EndDate            *string  `json:"endDate,omitempty"`
	PeakRate           *float64 `json:"peakRate"`
	FeedInRate         *float64 `json:"feedInRate"`
	OffPeakSavingsRate *float64 `json:"offPeakSavingsRate"`
}

// replaceOpenEndedPayload is the wire shape of
// POST /pricing/replace-open-ended.
type replaceOpenEndedPayload struct {
	ClosingPricingID string         `json:"closingPricingId"`
	NewPeriod        pricingPayload `json:"newPeriod"`
}

// SetPricingStore wires the pricing CRUD dependency. Called by
// cmd/api/main.go and rebuilds the mux so the routes pick up the store.
func (h *Handler) SetPricingStore(s PricingStore) {
	h.pricing = s
}

// pricingError is the JSON response shape for every pricing error.
// `openEndedId` is populated only when the offending row of an overlap
// is the unique open-ended period — the editor needs the id to surface
// the one-tap remediation from AC 3.6.
type pricingError struct {
	Error       string `json:"error"`
	Message     string `json:"message"`
	OpenEndedID string `json:"openEndedId,omitempty"`
}

// writePricingError serialises a {"error","message"} response with the
// given HTTP status. Unlike writeJSONError, this carries the AC 2.3
// machine-parseable code in the "error" field and the human-readable
// description in "message".
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

// writePricingOverlapError surfaces the offending open-ended row id when
// the offender is the unique open-ended period; otherwise behaves
// exactly like writePricingError. The id powers the editor's AC 3.6
// one-tap remediation flow.
func writePricingOverlapError(w http.ResponseWriter, openEndedID string) {
	body, err := json.Marshal(pricingError{
		Error:       pricingCodeOverlap,
		Message:     "pricing period overlaps an existing one",
		OpenEndedID: openEndedID,
	})
	if err != nil {
		slog.Error("marshal pricing overlap error", "error", err)
		body = []byte(`{"error":"overlap","message":"pricing period overlaps an existing one"}`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_, _ = w.Write(body)
}

// handleListPricing returns every pricing period sorted by startDate
// ascending. AC 2.5.
func (h *Handler) handleListPricing(w http.ResponseWriter, r *http.Request) {
	if h.pricing == nil {
		writePricingError(w, http.StatusInternalServerError, pricingCodeInternal, "pricing store not configured")
		return
	}
	rows, err := h.pricing.ListPricing(r.Context())
	if err != nil {
		slog.Error("list pricing failed", "error", err)
		writePricingError(w, http.StatusInternalServerError, pricingCodeInternal, "list pricing failed")
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Pricing []dynamo.PricingItem `json:"pricing"`
	}{Pricing: rows})
}

// handleCreatePricing validates the payload, enforces AC 1.10 ordering,
// assigns server-side id/timestamps, and writes the row.
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
		slog.Error("list pricing failed during create", "error", err)
		writePricingError(w, http.StatusInternalServerError, pricingCodeInternal, "list pricing failed")
		return
	}
	if !runPricingValidationChain(w, payload, existing, "") {
		return
	}

	prevOpenEndedID := openEndedIDFromList(existing)
	now := h.nowFunc().UTC().Format(time.RFC3339)
	item := payload.toItem(h.idFunc(), now, now)

	if err := h.pricing.PutPricing(r.Context(), item, prevOpenEndedID); err != nil {
		mapPricingStoreError(w, "put pricing", err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

// handleUpdatePricing validates and overwrites an existing pricing row.
// AC 1.7 / Decision 17: the row being updated is excluded from the
// overlap check.
func (h *Handler) handleUpdatePricing(w http.ResponseWriter, r *http.Request) {
	if h.pricing == nil {
		writePricingError(w, http.StatusInternalServerError, pricingCodeInternal, "pricing store not configured")
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writePricingError(w, http.StatusBadRequest, pricingCodeInternal, "id required")
		return
	}
	payload, ok := decodePricingPayload(w, r)
	if !ok {
		return
	}

	existing, err := h.pricing.ListPricing(r.Context())
	if err != nil {
		slog.Error("list pricing failed during update", "error", err)
		writePricingError(w, http.StatusInternalServerError, pricingCodeInternal, "list pricing failed")
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
		writePricingError(w, http.StatusNotFound, pricingCodeInternal, "pricing period not found")
		return
	}
	if !runPricingValidationChain(w, payload, existing, id) {
		return
	}

	prevOpenEndedID := openEndedIDFromList(existing)
	now := h.nowFunc().UTC().Format(time.RFC3339)
	item := payload.toItem(id, current.CreatedAt, now)

	if err := h.pricing.UpdatePricing(r.Context(), item, prevOpenEndedID); err != nil {
		mapPricingStoreError(w, "update pricing", err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// handleDeletePricing removes a pricing row by id. AC 2.4 / Decision 11:
// 404 on unknown id, 204 on success.
func (h *Handler) handleDeletePricing(w http.ResponseWriter, r *http.Request) {
	if h.pricing == nil {
		writePricingError(w, http.StatusInternalServerError, pricingCodeInternal, "pricing store not configured")
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writePricingError(w, http.StatusBadRequest, pricingCodeInternal, "id required")
		return
	}
	existing, err := h.pricing.GetPricing(r.Context(), id)
	if err != nil {
		slog.Error("get pricing failed during delete", "id", id, "error", err)
		writePricingError(w, http.StatusInternalServerError, pricingCodeInternal, "get pricing failed")
		return
	}
	if existing == nil {
		writePricingError(w, http.StatusNotFound, pricingCodeInternal, "pricing period not found")
		return
	}
	sentinel, err := h.pricing.GetSentinel(r.Context())
	if err != nil {
		slog.Error("get sentinel failed during delete", "error", err)
		writePricingError(w, http.StatusInternalServerError, pricingCodeInternal, "get sentinel failed")
		return
	}
	var prevOpenEndedID *string
	if sentinel != nil {
		prevOpenEndedID = sentinel.OpenEndedID
	}
	if err := h.pricing.DeletePricing(r.Context(), id, prevOpenEndedID); err != nil {
		mapPricingStoreError(w, "delete pricing", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNoContent)
}

// handleReplaceOpenEnded atomically closes the existing open-ended row
// at startDate − 1 day and inserts a new pricing row (AC 2.6). The
// closing-row endDate is derived server-side per AC 3.6.
func (h *Handler) handleReplaceOpenEnded(w http.ResponseWriter, r *http.Request) {
	if h.pricing == nil {
		writePricingError(w, http.StatusInternalServerError, pricingCodeInternal, "pricing store not configured")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, pricingBodyMaxBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writePricingError(w, http.StatusBadRequest, pricingCodeInternal, "malformed request body")
		return
	}
	var payload replaceOpenEndedPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		writePricingError(w, http.StatusBadRequest, pricingCodeInternal, "malformed request body")
		return
	}
	if payload.ClosingPricingID == "" {
		writePricingError(w, http.StatusBadRequest, pricingCodeInternal, "closingPricingId required")
		return
	}

	existing, err := h.pricing.ListPricing(r.Context())
	if err != nil {
		slog.Error("list pricing failed during replace-open-ended", "error", err)
		writePricingError(w, http.StatusInternalServerError, pricingCodeInternal, "list pricing failed")
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
		writePricingError(w, http.StatusNotFound, pricingCodeInternal, "closing pricing period not found")
		return
	}
	if closing.EndDate != nil {
		writePricingError(w, http.StatusBadRequest, pricingCodeSecondOpenEnded, "closing period is not open-ended")
		return
	}

	closingEndDate, err := previousDate(payload.NewPeriod.StartDate)
	if err != nil {
		writePricingError(w, http.StatusBadRequest, pricingCodeInvertedDates, "newPeriod.startDate invalid")
		return
	}

	// Simulate the resulting two-row state (closing capped at the new
	// startDate − 1 day; new row inserted) and run the same validation
	// chain against it.
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
	if !runPricingValidationChain(w, payload.NewPeriod, projected, "") {
		return
	}

	now := h.nowFunc().UTC().Format(time.RFC3339)
	newItem := payload.NewPeriod.toItem(h.idFunc(), now, now)
	if err := h.pricing.ReplaceOpenEnded(r.Context(), payload.ClosingPricingID, closingEndDate, newItem); err != nil {
		mapPricingStoreError(w, "replace open-ended pricing", err)
		return
	}

	// Return the resulting pair so the client can fold both rows back
	// into its local list without a second fetch.
	updated, err := h.pricing.ListPricing(r.Context())
	if err != nil {
		slog.Error("list pricing failed after replace-open-ended", "error", err)
		writePricingError(w, http.StatusInternalServerError, pricingCodeInternal, "list pricing failed")
		return
	}
	closingRow, newRow := pluckReplacedPair(updated, payload.ClosingPricingID, newItem.PricingID)
	writeJSON(w, http.StatusOK, struct {
		Pricing []dynamo.PricingItem `json:"pricing"`
	}{Pricing: []dynamo.PricingItem{closingRow, newRow}})
}

// pluckReplacedPair returns the (closing, new) pair from the post-write
// listing in canonical order so the response body is deterministic.
func pluckReplacedPair(rows []dynamo.PricingItem, closingID, newID string) (dynamo.PricingItem, dynamo.PricingItem) {
	var closing, newRow dynamo.PricingItem
	for _, r := range rows {
		if r.PricingID == closingID {
			closing = r
		}
		if r.PricingID == newID {
			newRow = r
		}
	}
	return closing, newRow
}

// decodePricingPayload reads, size-limits, and JSON-decodes the request
// body. On failure the response is already written; callers return early.
func decodePricingPayload(w http.ResponseWriter, r *http.Request) (pricingPayload, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, pricingBodyMaxBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writePricingError(w, http.StatusBadRequest, pricingCodeInternal, "malformed request body")
		return pricingPayload{}, false
	}
	var payload pricingPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		writePricingError(w, http.StatusBadRequest, pricingCodeInternal, "malformed request body")
		return pricingPayload{}, false
	}
	return payload, true
}

// toItem builds a PricingItem from the wire payload + server-assigned
// fields. Rates are rounded to exactly four decimal places per
// Decision 10 / Decision 20.
func (p pricingPayload) toItem(id, createdAt, updatedAt string) dynamo.PricingItem {
	return dynamo.PricingItem{
		PricingID:          id,
		StartDate:          p.StartDate,
		EndDate:            p.EndDate,
		PeakRate:           roundTo4DP(deref(p.PeakRate)),
		FeedInRate:         roundTo4DP(deref(p.FeedInRate)),
		OffPeakSavingsRate: roundTo4DP(deref(p.OffPeakSavingsRate)),
		CreatedAt:          createdAt,
		UpdatedAt:          updatedAt,
	}
}

func deref(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

// runPricingValidationChain executes AC 1.10 in order:
// inverted_dates → overlap → rate_precision → rate_out_of_range →
// second_open_ended. Writes the first failure and returns false; on
// success returns true with no response written.
//
// `excludeID` is the id of the row being updated; empty on create.
func runPricingValidationChain(w http.ResponseWriter, p pricingPayload, existing []dynamo.PricingItem, excludeID string) bool {
	if !validateInvertedDates(w, p) {
		return false
	}
	if !validateOverlap(w, p, existing, excludeID) {
		return false
	}
	if !validateRatePrecision(w, p) {
		return false
	}
	if !validateRateRange(w, p) {
		return false
	}
	if !validateSecondOpenEnded(w, p, existing, excludeID) {
		return false
	}
	return true
}

// validateInvertedDates fires AC 1.6 when endDate is present and
// strictly before startDate. A YYYY-MM-DD string compare is correct
// because the format is lexicographically chronological.
func validateInvertedDates(w http.ResponseWriter, p pricingPayload) bool {
	if !validISODate(p.StartDate) {
		writePricingError(w, http.StatusBadRequest, pricingCodeInvertedDates, "startDate must be YYYY-MM-DD")
		return false
	}
	if p.EndDate != nil {
		if !validISODate(*p.EndDate) {
			writePricingError(w, http.StatusBadRequest, pricingCodeInvertedDates, "endDate must be YYYY-MM-DD")
			return false
		}
		if *p.EndDate < p.StartDate {
			writePricingError(w, http.StatusBadRequest, pricingCodeInvertedDates, "endDate must not precede startDate")
			return false
		}
	}
	return true
}

// validateOverlap fires AC 1.7 when the candidate's date range
// intersects any existing row's date range (excluding excludeID).
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
		// Half-open intervals overlap iff start ≤ other.end && end ≥
		// other.start. Inclusive on both ends per AC 1.5.
		if p.StartDate <= rowEnd && candEnd >= row.StartDate {
			if row.EndDate == nil {
				writePricingOverlapError(w, row.PricingID)
				return false
			}
			writePricingError(w, http.StatusBadRequest, pricingCodeOverlap, "pricing period overlaps an existing one")
			return false
		}
	}
	return true
}

// validateRatePrecision fires AC 1.4 when any rate has > 4 decimal
// places. Float64 round-tripping is precise enough at 4 dp that
// multiplying by 10000 and comparing against the nearest integer is
// safe — Decision 20.
func validateRatePrecision(w http.ResponseWriter, p pricingPayload) bool {
	for _, rate := range []float64{deref(p.PeakRate), deref(p.FeedInRate), deref(p.OffPeakSavingsRate)} {
		scaled := rate * 10000
		if math.Abs(scaled-math.Round(scaled)) > 1e-6 {
			writePricingError(w, http.StatusBadRequest, pricingCodeRatePrecision, "rates must have at most 4 decimal places")
			return false
		}
	}
	return true
}

// validateRateRange fires AC 1.8 when any rate is < 0 or > the cap.
func validateRateRange(w http.ResponseWriter, p pricingPayload) bool {
	for _, rate := range []float64{deref(p.PeakRate), deref(p.FeedInRate), deref(p.OffPeakSavingsRate)} {
		if rate < 0 || rate > pricingRateCap {
			writePricingError(w, http.StatusBadRequest, pricingCodeRateOutOfRange, "rates must be between 0 and 10.0 AUD per kWh")
			return false
		}
	}
	return true
}

// validateSecondOpenEnded fires AC 1.9 when the candidate is
// open-ended and another existing row (other than excludeID) is also
// open-ended.
func validateSecondOpenEnded(w http.ResponseWriter, p pricingPayload, existing []dynamo.PricingItem, excludeID string) bool {
	if p.EndDate != nil {
		return true
	}
	for _, row := range existing {
		if row.PricingID == excludeID {
			continue
		}
		if row.EndDate == nil {
			writePricingError(w, http.StatusBadRequest, pricingCodeSecondOpenEnded, "another pricing period is already open-ended")
			return false
		}
	}
	return true
}

// openEndedIDFromList returns the unique open-ended row's id, or nil
// when no open-ended row exists. The validator's overlap and
// second_open_ended checks already ensure at most one is present.
func openEndedIDFromList(rows []dynamo.PricingItem) *string {
	for _, row := range rows {
		if row.EndDate == nil {
			id := row.PricingID
			return &id
		}
	}
	return nil
}

// mapPricingStoreError translates typed errors from the dynamo store
// into the appropriate HTTP response. Anything unrecognised falls
// through to HTTP 500 with the raw error logged.
func mapPricingStoreError(w http.ResponseWriter, op string, err error) {
	switch {
	case errors.Is(err, dynamo.ErrPricingConcurrentWrite):
		writePricingError(w, http.StatusConflict, pricingCodeConcurrentWrite, "concurrent open-ended write detected")
	case errors.Is(err, dynamo.ErrPricingUUIDCollision):
		slog.Warn("pricing uuid collision", "op", op, "error", err)
		writePricingError(w, http.StatusInternalServerError, pricingCodeInternal, "uuid collision; retry")
	default:
		slog.Error("pricing store error", "op", op, "error", err)
		writePricingError(w, http.StatusInternalServerError, pricingCodeInternal, "internal error")
	}
}

// validISODate returns true when s parses as YYYY-MM-DD in
// Australia/Melbourne. We don't load the location at runtime — the
// canonical layout match is sufficient for the wire format check.
func validISODate(s string) bool {
	if len(s) != 10 {
		return false
	}
	// Cheap structural check before time.Parse so common malformed
	// inputs fail fast.
	if s[4] != '-' || s[7] != '-' {
		return false
	}
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

// previousDate returns the YYYY-MM-DD string for one calendar day before
// `s`. Used by replace-open-ended to derive the closing row's endDate.
func previousDate(s string) (string, error) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return "", err
	}
	return t.AddDate(0, 0, -1).Format("2006-01-02"), nil
}

// roundTo4DP rounds v to exactly four decimal places. Used to normalise
// accepted rates on write so 0.28729999… stored as 0.2873.
func roundTo4DP(v float64) float64 {
	return math.Round(v*10000) / 10000
}
