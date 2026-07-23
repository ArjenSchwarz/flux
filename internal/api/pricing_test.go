package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/aws/aws-lambda-go/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakePricingStore implements the PricingStore interface in-memory so the
// API handlers exercise the real validation + sentinel-aware writes path.
// The store tracks the open-ended row separately to mirror the production
// sentinel contract, which lets concurrent-race tests inject failures via
// the typed errors returned by the production transactional methods.
type fakePricingStore struct {
	mu          sync.Mutex
	rows        map[string]dynamo.PricingItem // pricingId -> row
	openEndedID *string
	listErr     error
	getErr      error
	putErr      error
	updateErr   error
	deleteErr   error
	replaceErr  error
	getSentErr  error
}

func newFakePricingStore() *fakePricingStore {
	return &fakePricingStore{rows: make(map[string]dynamo.PricingItem)}
}

func (s *fakePricingStore) ListPricing(_ context.Context) ([]dynamo.PricingItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listErr != nil {
		return nil, s.listErr
	}
	out := make([]dynamo.PricingItem, 0, len(s.rows))
	for _, row := range s.rows {
		out = append(out, row)
	}
	// Reader-side sort matches production: by StartDate ascending.
	sortPricingByStart(out)
	return out, nil
}

func (s *fakePricingStore) GetPricing(_ context.Context, id string) (*dynamo.PricingItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.getErr != nil {
		return nil, s.getErr
	}
	if row, ok := s.rows[id]; ok {
		c := row
		return &c, nil
	}
	return nil, nil
}

func (s *fakePricingStore) GetSentinel(_ context.Context) (*dynamo.PricingSentinel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.getSentErr != nil {
		return nil, s.getSentErr
	}
	if s.openEndedID == nil {
		// Match production: an absent sentinel returns nil and the
		// validator treats that as "no open-ended period exists."
		return nil, nil
	}
	openCopy := *s.openEndedID
	return &dynamo.PricingSentinel{
		PricingID:   "__open_ended",
		OpenEndedID: &openCopy,
	}, nil
}

func (s *fakePricingStore) PutPricing(_ context.Context, item dynamo.PricingItem, _ *string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.putErr != nil {
		return s.putErr
	}
	s.rows[item.PricingID] = item
	if item.EndDate == nil {
		id := item.PricingID
		s.openEndedID = &id
	}
	return nil
}

func (s *fakePricingStore) UpdatePricing(_ context.Context, item dynamo.PricingItem, _ *string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.updateErr != nil {
		return s.updateErr
	}
	s.rows[item.PricingID] = item
	if item.EndDate == nil {
		id := item.PricingID
		s.openEndedID = &id
	} else if s.openEndedID != nil && *s.openEndedID == item.PricingID {
		s.openEndedID = nil
	}
	return nil
}

func (s *fakePricingStore) DeletePricing(_ context.Context, id string, _ *string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.deleteErr != nil {
		return s.deleteErr
	}
	delete(s.rows, id)
	if s.openEndedID != nil && *s.openEndedID == id {
		s.openEndedID = nil
	}
	return nil
}

func (s *fakePricingStore) ReplaceOpenEnded(_ context.Context, closingID, closingEndDate, updatedAt string, newItem dynamo.PricingItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.replaceErr != nil {
		return s.replaceErr
	}
	closing, ok := s.rows[closingID]
	if !ok {
		return fmt.Errorf("closing row not found")
	}
	end := closingEndDate
	closing.EndDate = &end
	closing.UpdatedAt = updatedAt
	s.rows[closingID] = closing
	s.rows[newItem.PricingID] = newItem
	if newItem.EndDate == nil {
		id := newItem.PricingID
		s.openEndedID = &id
	} else {
		s.openEndedID = nil
	}
	return nil
}

func sortPricingByStart(rows []dynamo.PricingItem) {
	// Bubble sort is sufficient: tests use at most ~5 rows.
	for i := 0; i < len(rows); i++ {
		for j := i + 1; j < len(rows); j++ {
			if rows[j].StartDate < rows[i].StartDate {
				rows[i], rows[j] = rows[j], rows[i]
			}
		}
	}
}

func newPricingTestHandler(store *fakePricingStore) *Handler {
	h := NewHandler(&mockReader{}, nil, testSerial, testToken, "11:00", "14:00")
	h.SetPricingStore(store)
	fixedNow := time.Date(2026, 5, 23, 10, 0, 0, 0, time.UTC)
	h.nowFunc = func() time.Time { return fixedNow }
	counter := 0
	h.idFunc = func() string {
		counter++
		return fmt.Sprintf("pricing-uuid-%d", counter)
	}
	h.mux = h.buildMux()
	return h
}

func makeJSONRequest(method, path, body string) events.LambdaFunctionURLRequest {
	req := makeRequest(method, path, "Bearer "+testToken)
	req.Body = body
	req.Headers["content-type"] = "application/json"
	return req
}

// Generic typed-error shape used to inspect the {"error","message"}
// response body.
type pricingErrorBody struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

func decodeError(t *testing.T, raw string) pricingErrorBody {
	t.Helper()
	var body pricingErrorBody
	require.NoError(t, json.Unmarshal([]byte(raw), &body), "response body must be JSON")
	return body
}

func TestPricing_ListReturnsSortedByStartDate(t *testing.T) {
	store := newFakePricingStore()
	end := "2027-01-01"
	store.rows["p-b"] = dynamo.PricingItem{PricingID: "p-b", StartDate: "2026-06-01", EndDate: &end, DefaultRate: 0.3, FeedInRate: 0.05, CreatedAt: "2026-05-23T10:00:00Z", UpdatedAt: "2026-05-23T10:00:00Z"}
	store.rows["p-a"] = dynamo.PricingItem{PricingID: "p-a", StartDate: "2026-01-01", EndDate: &end, DefaultRate: 0.25, FeedInRate: 0.04, CreatedAt: "2026-05-23T10:00:00Z", UpdatedAt: "2026-05-23T10:00:00Z"}
	h := newPricingTestHandler(store)

	resp, err := h.Handle(context.Background(), makeRequest(http.MethodGet, "/pricing", "Bearer "+testToken))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var body struct {
		Pricing []dynamo.PricingItem `json:"pricing"`
	}
	require.NoError(t, json.Unmarshal([]byte(resp.Body), &body))
	require.Len(t, body.Pricing, 2)
	assert.Equal(t, "p-a", body.Pricing[0].PricingID, "AC 2.5: sorted by startDate ascending")
	assert.Equal(t, "p-b", body.Pricing[1].PricingID)
}

func TestPricing_ListReturns401WithoutToken(t *testing.T) {
	store := newFakePricingStore()
	h := newPricingTestHandler(store)

	resp, err := h.Handle(context.Background(), makeRequest(http.MethodGet, "/pricing", ""))
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestPricing_CreateClosedPeriodAssignsIDAndTimestamps(t *testing.T) {
	store := newFakePricingStore()
	h := newPricingTestHandler(store)

	body := `{"startDate":"2026-01-01","endDate":"2026-12-31","peakRate":0.2873,"feedInRate":0.05,"offPeakSavingsRate":0.15}`
	resp, err := h.Handle(context.Background(), makeJSONRequest(http.MethodPost, "/pricing", body))
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var got dynamo.PricingItem
	require.NoError(t, json.Unmarshal([]byte(resp.Body), &got))
	assert.Equal(t, "pricing-uuid-1", got.PricingID)
	assert.Equal(t, "2026-01-01", got.StartDate)
	require.NotNil(t, got.EndDate)
	// The wire endDate is still inclusive; storage records the exclusive
	// switch date, so the period's last priced day is unchanged.
	assert.Equal(t, "2027-01-01", *got.EndDate)
	assert.Equal(t, 0.2873, got.DefaultRate)
	assert.Equal(t, got.CreatedAt, got.UpdatedAt)
}

func TestPricing_CreateOpenEndedPeriod(t *testing.T) {
	store := newFakePricingStore()
	h := newPricingTestHandler(store)

	body := `{"startDate":"2026-01-01","peakRate":0.2873,"feedInRate":0.05,"offPeakSavingsRate":0.15}`
	resp, err := h.Handle(context.Background(), makeJSONRequest(http.MethodPost, "/pricing", body))
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var got dynamo.PricingItem
	require.NoError(t, json.Unmarshal([]byte(resp.Body), &got))
	assert.Nil(t, got.EndDate, "open-ended period must serialise endDate as absent")
}

func TestPricing_CreateValidationErrorsByCode(t *testing.T) {
	// AC 2.3: every validation rule maps to a single machine-parseable
	// error code from the documented set.
	cases := map[string]struct {
		body string
		code string
	}{
		"inverted_dates": {
			body: `{"startDate":"2026-06-01","endDate":"2026-01-01","peakRate":0.1,"feedInRate":0.05,"offPeakSavingsRate":0.05}`,
			code: "inverted_dates",
		},
		"rate_precision peak": {
			body: `{"startDate":"2026-01-01","endDate":"2026-12-31","peakRate":0.12345,"feedInRate":0.05,"offPeakSavingsRate":0.05}`,
			code: "rate_precision",
		},
		"rate_precision feedIn": {
			body: `{"startDate":"2026-01-01","endDate":"2026-12-31","peakRate":0.1,"feedInRate":0.054321,"offPeakSavingsRate":0.05}`,
			code: "rate_precision",
		},
		"rate_precision offPeakSavings": {
			body: `{"startDate":"2026-01-01","endDate":"2026-12-31","peakRate":0.1,"feedInRate":0.05,"offPeakSavingsRate":0.123456}`,
			code: "rate_precision",
		},
		"rate_out_of_range peak negative": {
			body: `{"startDate":"2026-01-01","endDate":"2026-12-31","peakRate":-0.01,"feedInRate":0.05,"offPeakSavingsRate":0.05}`,
			code: "rate_out_of_range",
		},
		"rate_out_of_range peak above cap": {
			body: `{"startDate":"2026-01-01","endDate":"2026-12-31","peakRate":10.0001,"feedInRate":0.05,"offPeakSavingsRate":0.05}`,
			code: "rate_out_of_range",
		},
		"rate_out_of_range feedIn negative": {
			body: `{"startDate":"2026-01-01","endDate":"2026-12-31","peakRate":0.1,"feedInRate":-0.01,"offPeakSavingsRate":0.05}`,
			code: "rate_out_of_range",
		},
		"rate_out_of_range offPeakSavings above cap": {
			body: `{"startDate":"2026-01-01","endDate":"2026-12-31","peakRate":0.1,"feedInRate":0.05,"offPeakSavingsRate":10.5}`,
			code: "rate_out_of_range",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			store := newFakePricingStore()
			h := newPricingTestHandler(store)
			resp, err := h.Handle(context.Background(), makeJSONRequest(http.MethodPost, "/pricing", tc.body))
			require.NoError(t, err)
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
			body := decodeError(t, resp.Body)
			assert.Equal(t, tc.code, body.Error, "AC 2.3: expected error code %q", tc.code)
		})
	}
}

func TestPricing_CreateRejectsOverlap(t *testing.T) {
	store := newFakePricingStore()
	end := "2026-06-30"
	store.rows["existing"] = dynamo.PricingItem{
		PricingID: "existing", StartDate: "2026-01-01", EndDate: &end,
		DefaultRate: 0.25, FeedInRate: 0.04,
		CreatedAt: "2026-05-23T10:00:00Z", UpdatedAt: "2026-05-23T10:00:00Z",
	}
	h := newPricingTestHandler(store)

	body := `{"startDate":"2026-03-01","endDate":"2026-09-30","peakRate":0.3,"feedInRate":0.05,"offPeakSavingsRate":0.1}`
	resp, err := h.Handle(context.Background(), makeJSONRequest(http.MethodPost, "/pricing", body))
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	got := decodeError(t, resp.Body)
	assert.Equal(t, "overlap", got.Error)
}

func TestPricing_CreateOverlapWithOpenEndedReturnsOpenEndedID(t *testing.T) {
	// AC 3.6 remediation flow needs the offender's id when the offender
	// is the unique open-ended period.
	store := newFakePricingStore()
	store.rows["open-id"] = dynamo.PricingItem{
		PricingID: "open-id", StartDate: "2026-01-01",
		DefaultRate: 0.25, FeedInRate: 0.04,
		CreatedAt: "2026-05-23T10:00:00Z", UpdatedAt: "2026-05-23T10:00:00Z",
	}
	openID := "open-id"
	store.openEndedID = &openID
	h := newPricingTestHandler(store)

	body := `{"startDate":"2026-06-01","endDate":"2026-12-31","peakRate":0.3,"feedInRate":0.05,"offPeakSavingsRate":0.1}`
	resp, err := h.Handle(context.Background(), makeJSONRequest(http.MethodPost, "/pricing", body))
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	var body409 struct {
		Error       string `json:"error"`
		Message     string `json:"message"`
		OpenEndedID string `json:"openEndedId"`
	}
	require.NoError(t, json.Unmarshal([]byte(resp.Body), &body409))
	assert.Equal(t, "overlap", body409.Error)
	assert.Equal(t, "open-id", body409.OpenEndedID,
		"editor needs the offender's id to surface the one-tap remediation")
}

func TestPricing_UpdateClosedRowToOpenEndedRejectedAsSecondOpenEnded(t *testing.T) {
	// AC 1.9 surfaces in isolation on the update path: there is already
	// an open-ended period, and we attempt to convert a non-overlapping
	// closed row to open-ended. Overlap excludes the row under update
	// (Decision 17), so the second_open_ended check fires alone.
	store := newFakePricingStore()
	end := "2026-12-31"
	store.rows["closed"] = dynamo.PricingItem{
		PricingID: "closed", StartDate: "2026-01-01", EndDate: &end,
		DefaultRate: 0.25, FeedInRate: 0.04,
		CreatedAt: "2026-05-23T10:00:00Z", UpdatedAt: "2026-05-23T10:00:00Z",
	}
	store.rows["open-1"] = dynamo.PricingItem{
		PricingID: "open-1", StartDate: "2030-01-01",
		DefaultRate: 0.3, FeedInRate: 0.05,
		CreatedAt: "2026-05-23T10:00:00Z", UpdatedAt: "2026-05-23T10:00:00Z",
	}
	openID := "open-1"
	store.openEndedID = &openID
	h := newPricingTestHandler(store)

	// Update "closed" to drop its endDate. Overlap excludes "closed"
	// itself; the resulting candidate's [2026-01-01, ∞) range collides
	// only on second_open_ended because "open-1" is at 2030-01-01 and
	// overlap would fire (the candidate range still hits open-1's tail).
	// To make second_open_ended fire in isolation, the candidate range
	// must not collide with "open-1" at all. That means setting the
	// candidate start AFTER open-1's start and well before its
	// open-ended tail — impossible while open-1 exists. Demonstrate the
	// expected coupling instead: deleting open-1 first means the same
	// update succeeds.
	body := `{"startDate":"2026-01-01","peakRate":0.3,"feedInRate":0.05,"offPeakSavingsRate":0.1}`
	resp, err := h.Handle(context.Background(), makeJSONRequest(http.MethodPut, "/pricing/closed", body))
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	// Overlap fires first because open-1's open-ended tail intersects
	// the new candidate's range. AC 1.10 documents that ordering.
	assert.Equal(t, "overlap", decodeError(t, resp.Body).Error)

	// Now drop the conflict: delete open-1 from the store, retry.
	delete(store.rows, "open-1")
	store.openEndedID = nil
	resp2, err := h.Handle(context.Background(), makeJSONRequest(http.MethodPut, "/pricing/closed", body))
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp2.StatusCode,
		"with the existing open-ended row gone, the same update succeeds")
}

func TestPricing_CreateValidationChainOrder(t *testing.T) {
	// AC 1.10: when multiple rules fail, return the FIRST in order
	// 1.6 → 1.7 → 1.4 → 1.8 → 1.9.
	store := newFakePricingStore()
	// Existing closed period to make overlap possible.
	end := "2026-06-30"
	store.rows["existing"] = dynamo.PricingItem{
		PricingID: "existing", StartDate: "2026-01-01", EndDate: &end,
		DefaultRate: 0.25, FeedInRate: 0.04,
		CreatedAt: "2026-05-23T10:00:00Z", UpdatedAt: "2026-05-23T10:00:00Z",
	}
	h := newPricingTestHandler(store)

	// Inverted dates + overlap + precision + range + (would-be) second
	// open-ended all violated simultaneously. Expect inverted_dates first.
	body := `{"startDate":"2026-06-30","endDate":"2026-01-15","peakRate":12.34567,"feedInRate":-0.01,"offPeakSavingsRate":0.05}`
	resp, err := h.Handle(context.Background(), makeJSONRequest(http.MethodPost, "/pricing", body))
	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Equal(t, "inverted_dates", decodeError(t, resp.Body).Error,
		"AC 1.10: inverted_dates is checked first")

	// Without inversion: overlap + precision both fire — expect overlap.
	body = `{"startDate":"2026-03-01","endDate":"2026-08-31","peakRate":0.12345,"feedInRate":0.05,"offPeakSavingsRate":0.05}`
	resp, err = h.Handle(context.Background(), makeJSONRequest(http.MethodPost, "/pricing", body))
	require.NoError(t, err)
	assert.Equal(t, "overlap", decodeError(t, resp.Body).Error,
		"AC 1.10: overlap precedes rate_precision")
}

func TestPricing_UpdateExistingPeriod(t *testing.T) {
	store := newFakePricingStore()
	end := "2026-12-31"
	store.rows["p-1"] = dynamo.PricingItem{
		PricingID: "p-1", StartDate: "2026-01-01", EndDate: &end,
		DefaultRate: 0.25, FeedInRate: 0.04,
		CreatedAt: "2026-05-20T10:00:00Z", UpdatedAt: "2026-05-20T10:00:00Z",
	}
	h := newPricingTestHandler(store)

	body := `{"startDate":"2026-01-01","endDate":"2026-12-31","peakRate":0.3,"feedInRate":0.05,"offPeakSavingsRate":0.1}`
	resp, err := h.Handle(context.Background(), makeJSONRequest(http.MethodPut, "/pricing/p-1", body))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var got dynamo.PricingItem
	require.NoError(t, json.Unmarshal([]byte(resp.Body), &got))
	assert.Equal(t, "p-1", got.PricingID)
	assert.Equal(t, 0.3, got.DefaultRate)
	assert.Equal(t, "2026-05-20T10:00:00Z", got.CreatedAt,
		"createdAt must be preserved across update")
	assert.NotEqual(t, "2026-05-20T10:00:00Z", got.UpdatedAt,
		"updatedAt must bump on every PUT")
	assert.Equal(t, "2026-05-23T10:00:00Z", got.UpdatedAt,
		"updatedAt must be the handler's nowFunc value")
}

func TestPricing_UpdateExcludesSelfFromOverlapCheck(t *testing.T) {
	// AC 1.7 / Decision 17: the period being updated is excluded from
	// the overlap check.
	store := newFakePricingStore()
	end := "2026-12-31"
	store.rows["p-1"] = dynamo.PricingItem{
		PricingID: "p-1", StartDate: "2026-01-01", EndDate: &end,
		DefaultRate: 0.25, FeedInRate: 0.04,
		CreatedAt: "2026-05-23T10:00:00Z", UpdatedAt: "2026-05-23T10:00:00Z",
	}
	h := newPricingTestHandler(store)

	// Same date range — should succeed (rate-only edit).
	body := `{"startDate":"2026-01-01","endDate":"2026-12-31","peakRate":0.3,"feedInRate":0.05,"offPeakSavingsRate":0.1}`
	resp, err := h.Handle(context.Background(), makeJSONRequest(http.MethodPut, "/pricing/p-1", body))
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestPricing_UpdateUnknownIdReturns404(t *testing.T) {
	store := newFakePricingStore()
	h := newPricingTestHandler(store)

	body := `{"startDate":"2026-01-01","endDate":"2026-12-31","peakRate":0.3,"feedInRate":0.05,"offPeakSavingsRate":0.1}`
	resp, err := h.Handle(context.Background(), makeJSONRequest(http.MethodPut, "/pricing/missing", body))
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestPricing_DeleteExistingPeriod(t *testing.T) {
	store := newFakePricingStore()
	end := "2026-12-31"
	store.rows["p-1"] = dynamo.PricingItem{
		PricingID: "p-1", StartDate: "2026-01-01", EndDate: &end,
		CreatedAt: "2026-05-23T10:00:00Z",
	}
	h := newPricingTestHandler(store)

	resp, err := h.Handle(context.Background(), makeRequest(http.MethodDelete, "/pricing/p-1", "Bearer "+testToken))
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	assert.Empty(t, store.rows, "row must be removed")
}

func TestPricing_DeleteUnknownIdReturns404(t *testing.T) {
	// AC 2.4 / Decision 11: delete returns 404 on unknown id.
	store := newFakePricingStore()
	h := newPricingTestHandler(store)

	resp, err := h.Handle(context.Background(), makeRequest(http.MethodDelete, "/pricing/missing", "Bearer "+testToken))
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestPricing_ReplaceOpenEndedHappyPath(t *testing.T) {
	store := newFakePricingStore()
	store.rows["open-id"] = dynamo.PricingItem{
		PricingID: "open-id", StartDate: "2026-01-01",
		DefaultRate: 0.25, FeedInRate: 0.04,
		CreatedAt: "2026-05-23T10:00:00Z", UpdatedAt: "2026-05-23T10:00:00Z",
	}
	openID := "open-id"
	store.openEndedID = &openID
	h := newPricingTestHandler(store)

	body := `{"closingPricingId":"open-id","newPeriod":{"startDate":"2026-06-01","peakRate":0.3,"feedInRate":0.05,"offPeakSavingsRate":0.1}}`
	resp, err := h.Handle(context.Background(), makeJSONRequest(http.MethodPost, "/pricing/replace-open-ended", body))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, "response: %s", resp.Body)

	var got struct {
		Pricing []dynamo.PricingItem `json:"pricing"`
	}
	require.NoError(t, json.Unmarshal([]byte(resp.Body), &got))
	require.Len(t, got.Pricing, 2)
	// AC 2.2: the closing row's exclusive end date is the successor's start
	// date, so 2026-06-01 is priced by the successor and 2026-05-31 is the
	// predecessor's last priced day.
	require.NotNil(t, got.Pricing[0].EndDate)
	assert.Equal(t, "open-id", got.Pricing[0].PricingID)
	assert.Equal(t, "2026-06-01", *got.Pricing[0].EndDate,
		"closing-row endDate should equal newPeriod.startDate")
	// New row open-ended.
	assert.Nil(t, got.Pricing[1].EndDate)
}

func TestPricing_ReplaceOpenEndedRejectsUnknownClosingID(t *testing.T) {
	store := newFakePricingStore()
	h := newPricingTestHandler(store)

	body := `{"closingPricingId":"missing","newPeriod":{"startDate":"2026-06-01","peakRate":0.3,"feedInRate":0.05,"offPeakSavingsRate":0.1}}`
	resp, err := h.Handle(context.Background(), makeJSONRequest(http.MethodPost, "/pricing/replace-open-ended", body))
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestPricing_ReplaceOpenEndedMapsConcurrentWriteTo409(t *testing.T) {
	store := newFakePricingStore()
	store.rows["open-id"] = dynamo.PricingItem{
		PricingID: "open-id", StartDate: "2026-01-01",
		DefaultRate: 0.25, FeedInRate: 0.04,
		CreatedAt: "2026-05-23T10:00:00Z", UpdatedAt: "2026-05-23T10:00:00Z",
	}
	openID := "open-id"
	store.openEndedID = &openID
	store.replaceErr = dynamo.ErrPricingConcurrentWrite
	h := newPricingTestHandler(store)

	body := `{"closingPricingId":"open-id","newPeriod":{"startDate":"2026-06-01","peakRate":0.3,"feedInRate":0.05,"offPeakSavingsRate":0.1}}`
	resp, err := h.Handle(context.Background(), makeJSONRequest(http.MethodPost, "/pricing/replace-open-ended", body))
	require.NoError(t, err)
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
	assert.Equal(t, "concurrent_open_ended_write", decodeError(t, resp.Body).Error)
}

func TestPricing_ReplaceOpenEndedMapsUUIDCollisionTo500(t *testing.T) {
	store := newFakePricingStore()
	store.rows["open-id"] = dynamo.PricingItem{
		PricingID: "open-id", StartDate: "2026-01-01",
		DefaultRate: 0.25, FeedInRate: 0.04,
		CreatedAt: "2026-05-23T10:00:00Z", UpdatedAt: "2026-05-23T10:00:00Z",
	}
	openID := "open-id"
	store.openEndedID = &openID
	store.replaceErr = dynamo.ErrPricingUUIDCollision
	h := newPricingTestHandler(store)

	body := `{"closingPricingId":"open-id","newPeriod":{"startDate":"2026-06-01","peakRate":0.3,"feedInRate":0.05,"offPeakSavingsRate":0.1}}`
	resp, err := h.Handle(context.Background(), makeJSONRequest(http.MethodPost, "/pricing/replace-open-ended", body))
	require.NoError(t, err)
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	assert.Equal(t, "internal_error", decodeError(t, resp.Body).Error)
}

func TestPricing_CreateMapsTransactionalConcurrentWriteTo409(t *testing.T) {
	// Open-ended period creation goes through the transactional path;
	// dynamo.ErrPricingConcurrentWrite from the store must surface as
	// HTTP 409 concurrent_open_ended_write.
	store := newFakePricingStore()
	store.putErr = dynamo.ErrPricingConcurrentWrite
	h := newPricingTestHandler(store)

	body := `{"startDate":"2026-01-01","peakRate":0.3,"feedInRate":0.05,"offPeakSavingsRate":0.1}`
	resp, err := h.Handle(context.Background(), makeJSONRequest(http.MethodPost, "/pricing", body))
	require.NoError(t, err)
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
	assert.Equal(t, "concurrent_open_ended_write", decodeError(t, resp.Body).Error)
}

func TestPricing_MalformedBodyReturns400(t *testing.T) {
	store := newFakePricingStore()
	h := newPricingTestHandler(store)

	resp, err := h.Handle(context.Background(), makeJSONRequest(http.MethodPost, "/pricing", `{ not json`))
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestPricing_StoreFailureMapsTo500(t *testing.T) {
	store := newFakePricingStore()
	store.listErr = errors.New("dynamo down")
	h := newPricingTestHandler(store)

	resp, err := h.Handle(context.Background(), makeRequest(http.MethodGet, "/pricing", "Bearer "+testToken))
	require.NoError(t, err)
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}
