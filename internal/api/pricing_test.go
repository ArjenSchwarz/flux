package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
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
	for i := range rows {
		for j := i + 1; j < len(rows); j++ {
			if rows[j].StartDate < rows[i].StartDate {
				rows[i], rows[j] = rows[j], rows[i]
			}
		}
	}
}

// bandPlanRow builds a stored band-shape row for the fixtures below: a single
// free window over the legacy 11:00–14:00 hours plus a default rate covering
// the rest of the day.
func bandPlanRow(id, start string, end *string, defaultRate float64) dynamo.PricingItem {
	savings := 0.15
	return dynamo.PricingItem{
		PricingID:            id,
		StartDate:            start,
		EndDate:              end,
		DefaultRate:          defaultRate,
		Windows:              []dynamo.PricingWindow{{Start: "11:00", End: "14:00", Free: true}},
		FeedInRate:           0.05,
		SavingsReferenceRate: &savings,
		CreatedAt:            "2026-05-23T10:00:00Z",
		UpdatedAt:            "2026-05-23T10:00:00Z",
	}
}

func strPtr(s string) *string { return &s }

func newPricingTestHandler(store *fakePricingStore) *Handler {
	h := newTestHandlerFor(&mockReader{}, nil, testSerial, testToken)
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

// Generic typed-error shape used to inspect the
// {"error","message","conflictingPricingId"} response body.
type pricingErrorBody struct {
	Error                string `json:"error"`
	Message              string `json:"message"`
	OpenEndedID          string `json:"openEndedId"`
	ConflictingPricingID string `json:"conflictingPricingId"`
}

func decodeError(t *testing.T, raw string) pricingErrorBody {
	t.Helper()
	var body pricingErrorBody
	require.NoError(t, json.Unmarshal([]byte(raw), &body), "response body must be JSON")
	return body
}

// The incoming time-of-use plan (Q3): free 10:00–15:00, a cheaper 01:00–06:00
// band, and the default rate for the rest of the day.
const newPlanBody = `{"startDate":"2026-08-01",` +
	`"defaultRate":0.35,` +
	`"windows":[{"start":"10:00","end":"15:00","free":true},` +
	`{"start":"01:00","end":"06:00","free":false,"rate":0.28}],` +
	`"feedInRate":0.05,"savingsReferenceRate":0.35}`

// singleBandBody is the migrated shape of a legacy plan: free 11:00–14:00 and
// one flat default rate. Callers substitute the dates.
func singleBandBody(startDate string, endDate *string) string {
	end := "null"
	if endDate != nil {
		end = `"` + *endDate + `"`
	}
	return fmt.Sprintf(`{"startDate":%q,"endDate":%s,"defaultRate":0.3,`+
		`"windows":[{"start":"11:00","end":"14:00","free":true}],`+
		`"feedInRate":0.05,"savingsReferenceRate":0.15}`, startDate, end)
}

func TestPricing_ListReturnsSortedByStartDate(t *testing.T) {
	store := newFakePricingStore()
	store.rows["p-b"] = bandPlanRow("p-b", "2026-06-01", strPtr("2027-01-01"), 0.3)
	store.rows["p-a"] = bandPlanRow("p-a", "2026-01-01", strPtr("2026-06-01"), 0.25)
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
	require.Len(t, body.Pricing[0].Windows, 1, "band windows must round-trip on the wire")
	assert.True(t, body.Pricing[0].Windows[0].Free)
}

func TestPricing_ListReturns401WithoutToken(t *testing.T) {
	store := newFakePricingStore()
	h := newPricingTestHandler(store)

	resp, err := h.Handle(context.Background(), makeRequest(http.MethodGet, "/pricing", ""))
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestPricing_CreateBandPlanRoundTrips(t *testing.T) {
	store := newFakePricingStore()
	h := newPricingTestHandler(store)

	resp, err := h.Handle(context.Background(), makeJSONRequest(http.MethodPost, "/pricing", newPlanBody))
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, resp.StatusCode, "response: %s", resp.Body)

	var got dynamo.PricingItem
	require.NoError(t, json.Unmarshal([]byte(resp.Body), &got))
	assert.Equal(t, "pricing-uuid-1", got.PricingID)
	assert.Equal(t, "2026-08-01", got.StartDate)
	assert.Nil(t, got.EndDate)
	assert.InDelta(t, 0.35, got.DefaultRate, 1e-9)
	assert.InDelta(t, 0.05, got.FeedInRate, 1e-9)
	require.NotNil(t, got.SavingsReferenceRate)
	assert.InDelta(t, 0.35, *got.SavingsReferenceRate, 1e-9)
	// Windows are stored as entered (Decision 4) — order and all.
	require.Len(t, got.Windows, 2)
	assert.Equal(t, "10:00", got.Windows[0].Start)
	assert.Equal(t, "15:00", got.Windows[0].End)
	assert.True(t, got.Windows[0].Free)
	assert.Nil(t, got.Windows[0].Rate, "a free window carries no rate")
	assert.Equal(t, "01:00", got.Windows[1].Start)
	require.NotNil(t, got.Windows[1].Rate)
	assert.InDelta(t, 0.28, *got.Windows[1].Rate, 1e-9)
	assert.Equal(t, got.CreatedAt, got.UpdatedAt)
}

func TestPricing_CreateStoresEndDateAsGiven(t *testing.T) {
	// Decision 5: the wire endDate is the exclusive switch date and is stored
	// verbatim — no ±1 arithmetic anywhere on the write path.
	store := newFakePricingStore()
	h := newPricingTestHandler(store)

	resp, err := h.Handle(context.Background(),
		makeJSONRequest(http.MethodPost, "/pricing", singleBandBody("2026-01-01", strPtr("2026-08-01"))))
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, resp.StatusCode, "response: %s", resp.Body)

	var got dynamo.PricingItem
	require.NoError(t, json.Unmarshal([]byte(resp.Body), &got))
	require.NotNil(t, got.EndDate)
	assert.Equal(t, "2026-08-01", *got.EndDate)
}

func TestPricing_CreateOpenEndedPeriod(t *testing.T) {
	store := newFakePricingStore()
	h := newPricingTestHandler(store)

	resp, err := h.Handle(context.Background(),
		makeJSONRequest(http.MethodPost, "/pricing", singleBandBody("2026-01-01", nil)))
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, resp.StatusCode, "response: %s", resp.Body)

	var got dynamo.PricingItem
	require.NoError(t, json.Unmarshal([]byte(resp.Body), &got))
	assert.Nil(t, got.EndDate, "open-ended period must serialise endDate as absent")
}

func TestPricing_CreateValidationErrorsByCode(t *testing.T) {
	// Requirement 7.2: every band rule maps to a single machine-parseable
	// error code the editor can switch on.
	cases := map[string]struct {
		body string
		code string
	}{
		"inverted_dates end before start": {
			body: `{"startDate":"2026-06-01","endDate":"2026-01-01","defaultRate":0.3,"windows":[],"feedInRate":0.05}`,
			code: "inverted_dates",
		},
		"inverted_dates zero-day plan": {
			// Exclusive ends make endDate == startDate a plan that prices no
			// days at all.
			body: `{"startDate":"2026-06-01","endDate":"2026-06-01","defaultRate":0.3,"windows":[],"feedInRate":0.05}`,
			code: "inverted_dates",
		},
		"band_window_invalid malformed time": {
			body: `{"startDate":"2026-01-01","defaultRate":0.3,"windows":[{"start":"1000","end":"15:00","free":true}],"feedInRate":0.05,"savingsReferenceRate":0.3}`,
			code: "band_window_invalid",
		},
		"band_window_invalid past end of day": {
			body: `{"startDate":"2026-01-01","defaultRate":0.3,"windows":[{"start":"10:00","end":"25:00","free":true}],"feedInRate":0.05,"savingsReferenceRate":0.3}`,
			code: "band_window_invalid",
		},
		"band_window_invalid inverted window": {
			body: `{"startDate":"2026-01-01","defaultRate":0.3,"windows":[{"start":"15:00","end":"10:00","free":true}],"feedInRate":0.05,"savingsReferenceRate":0.3}`,
			code: "band_window_invalid",
		},
		"band_overlap": {
			body: `{"startDate":"2026-01-01","defaultRate":0.3,"windows":[{"start":"10:00","end":"15:00","free":true},{"start":"14:00","end":"16:00","free":false,"rate":0.2}],"feedInRate":0.05,"savingsReferenceRate":0.3}`,
			code: "band_overlap",
		},
		"multiple_free_bands": {
			body: `{"startDate":"2026-01-01","defaultRate":0.3,"windows":[{"start":"10:00","end":"12:00","free":true},{"start":"13:00","end":"15:00","free":true}],"feedInRate":0.05,"savingsReferenceRate":0.3}`,
			code: "multiple_free_bands",
		},
		"no_rated_band": {
			// AC 1.3 / Q17: a free window spanning the whole day leaves the
			// cost math and the fallback rate undefined.
			body: `{"startDate":"2026-01-01","defaultRate":0.3,"windows":[{"start":"00:00","end":"24:00","free":true}],"feedInRate":0.05,"savingsReferenceRate":0.3}`,
			code: "no_rated_band",
		},
		"savings_rate_missing": {
			body: `{"startDate":"2026-01-01","defaultRate":0.3,"windows":[{"start":"10:00","end":"15:00","free":true}],"feedInRate":0.05}`,
			code: "savings_rate_missing",
		},
		"rate_precision default": {
			body: `{"startDate":"2026-01-01","defaultRate":0.12345,"windows":[],"feedInRate":0.05}`,
			code: "rate_precision",
		},
		"rate_precision feedIn": {
			body: `{"startDate":"2026-01-01","defaultRate":0.3,"windows":[],"feedInRate":0.054321}`,
			code: "rate_precision",
		},
		"rate_precision savings reference": {
			body: `{"startDate":"2026-01-01","defaultRate":0.3,"windows":[{"start":"10:00","end":"15:00","free":true}],"feedInRate":0.05,"savingsReferenceRate":0.123456}`,
			code: "rate_precision",
		},
		"rate_precision window rate": {
			body: `{"startDate":"2026-01-01","defaultRate":0.3,"windows":[{"start":"01:00","end":"06:00","free":false,"rate":0.28125}],"feedInRate":0.05}`,
			code: "rate_precision",
		},
		"rate_out_of_range default negative": {
			body: `{"startDate":"2026-01-01","defaultRate":-0.01,"windows":[],"feedInRate":0.05}`,
			code: "rate_out_of_range",
		},
		"rate_out_of_range default above cap": {
			body: `{"startDate":"2026-01-01","defaultRate":10.0001,"windows":[],"feedInRate":0.05}`,
			code: "rate_out_of_range",
		},
		"rate_out_of_range window rate above cap": {
			body: `{"startDate":"2026-01-01","defaultRate":0.3,"windows":[{"start":"01:00","end":"06:00","free":false,"rate":10.5}],"feedInRate":0.05}`,
			code: "rate_out_of_range",
		},
		"rate_out_of_range feedIn negative": {
			body: `{"startDate":"2026-01-01","defaultRate":0.3,"windows":[],"feedInRate":-0.01}`,
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
			assert.Equal(t, tc.code, body.Error, "expected error code %q, body %s", tc.code, resp.Body)
			assert.NotEmpty(t, body.Message, "every validation failure carries a human-readable message")
		})
	}
}

func TestPricing_CreateRejectsLegacyThreeRateShape(t *testing.T) {
	// AC 7.3 / Q28: encoding/json drops unknown fields, so a legacy body
	// would otherwise decode as a band plan with every rate at zero. The
	// marker has to be detected on the raw JSON keys.
	store := newFakePricingStore()
	h := newPricingTestHandler(store)

	body := `{"startDate":"2026-01-01","endDate":"2026-12-31","peakRate":0.2873,"feedInRate":0.05,"offPeakSavingsRate":0.15}`
	resp, err := h.Handle(context.Background(), makeJSONRequest(http.MethodPost, "/pricing", body))
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Equal(t, "legacy_shape", decodeError(t, resp.Body).Error)
	assert.Empty(t, store.rows, "a legacy payload must not reach the store")
}

func TestPricing_UpdateRejectsLegacyThreeRateShape(t *testing.T) {
	store := newFakePricingStore()
	store.rows["p-1"] = bandPlanRow("p-1", "2026-01-01", strPtr("2026-12-31"), 0.25)
	h := newPricingTestHandler(store)

	body := `{"startDate":"2026-01-01","endDate":"2026-12-31","peakRate":0.3,"feedInRate":0.05,"offPeakSavingsRate":0.1}`
	resp, err := h.Handle(context.Background(), makeJSONRequest(http.MethodPut, "/pricing/p-1", body))
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Equal(t, "legacy_shape", decodeError(t, resp.Body).Error)
}

func TestPricing_CreateRejectsOverlapNamingConflictingPlan(t *testing.T) {
	// AC 2.5: the response identifies the plan that would price the same day.
	store := newFakePricingStore()
	store.rows["existing"] = bandPlanRow("existing", "2026-01-01", strPtr("2026-07-01"), 0.25)
	h := newPricingTestHandler(store)

	resp, err := h.Handle(context.Background(),
		makeJSONRequest(http.MethodPost, "/pricing", singleBandBody("2026-03-01", strPtr("2026-10-01"))))
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	got := decodeError(t, resp.Body)
	assert.Equal(t, "overlap", got.Error)
	assert.Equal(t, "existing", got.ConflictingPricingID)
}

func TestPricing_CreateAdjacentSwitchDateAccepted(t *testing.T) {
	// AC 2.2: the predecessor's exclusive endDate equals the successor's
	// startDate, so the two ranges abut without overlapping.
	store := newFakePricingStore()
	store.rows["existing"] = bandPlanRow("existing", "2026-01-01", strPtr("2026-08-01"), 0.25)
	h := newPricingTestHandler(store)

	resp, err := h.Handle(context.Background(), makeJSONRequest(http.MethodPost, "/pricing", newPlanBody))
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, resp.StatusCode, "response: %s", resp.Body)
}

func TestPricing_CreateOverlapWithOpenEndedReturnsOpenEndedID(t *testing.T) {
	// AC 6.5 remediation flow needs the offender's id when the offender is
	// the unique open-ended period.
	store := newFakePricingStore()
	store.rows["open-id"] = bandPlanRow("open-id", "2026-01-01", nil, 0.25)
	openID := "open-id"
	store.openEndedID = &openID
	h := newPricingTestHandler(store)

	resp, err := h.Handle(context.Background(),
		makeJSONRequest(http.MethodPost, "/pricing", singleBandBody("2026-06-01", strPtr("2026-12-31"))))
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	got := decodeError(t, resp.Body)
	assert.Equal(t, "overlap", got.Error)
	assert.Equal(t, "open-id", got.OpenEndedID,
		"editor needs the offender's id to surface the one-tap remediation")
	assert.Equal(t, "open-id", got.ConflictingPricingID)
}

func TestPricing_CreateValidationChainOrder(t *testing.T) {
	// Ordering is inverted_dates → overlap → the remaining band rules →
	// second_open_ended, carried over from the flat-rate chain.
	store := newFakePricingStore()
	store.rows["existing"] = bandPlanRow("existing", "2026-01-01", strPtr("2026-07-01"), 0.25)
	h := newPricingTestHandler(store)

	// Inverted dates + overlap + band overlap + precision all violated.
	body := `{"startDate":"2026-06-30","endDate":"2026-01-15","defaultRate":12.34567,` +
		`"windows":[{"start":"10:00","end":"15:00","free":true},{"start":"14:00","end":"16:00","free":false,"rate":0.2}],` +
		`"feedInRate":-0.01,"savingsReferenceRate":0.3}`
	resp, err := h.Handle(context.Background(), makeJSONRequest(http.MethodPost, "/pricing", body))
	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Equal(t, "inverted_dates", decodeError(t, resp.Body).Error,
		"inverted_dates is checked first")

	// Without inversion: overlap + band_overlap both fire — expect the
	// date-range overlap.
	body = `{"startDate":"2026-03-01","endDate":"2026-09-01","defaultRate":0.3,` +
		`"windows":[{"start":"10:00","end":"15:00","free":true},{"start":"14:00","end":"16:00","free":false,"rate":0.2}],` +
		`"feedInRate":0.05,"savingsReferenceRate":0.3}`
	resp, err = h.Handle(context.Background(), makeJSONRequest(http.MethodPost, "/pricing", body))
	require.NoError(t, err)
	assert.Equal(t, "overlap", decodeError(t, resp.Body).Error,
		"date-range overlap precedes the band rules")
}

func TestPricing_UpdateExistingPeriod(t *testing.T) {
	store := newFakePricingStore()
	store.rows["p-1"] = bandPlanRow("p-1", "2026-01-01", strPtr("2027-01-01"), 0.25)
	created := store.rows["p-1"]
	created.CreatedAt = "2026-05-20T10:00:00Z"
	created.UpdatedAt = "2026-05-20T10:00:00Z"
	store.rows["p-1"] = created
	h := newPricingTestHandler(store)

	resp, err := h.Handle(context.Background(),
		makeJSONRequest(http.MethodPut, "/pricing/p-1", singleBandBody("2026-01-01", strPtr("2027-01-01"))))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, "response: %s", resp.Body)

	var got dynamo.PricingItem
	require.NoError(t, json.Unmarshal([]byte(resp.Body), &got))
	assert.Equal(t, "p-1", got.PricingID)
	assert.InDelta(t, 0.3, got.DefaultRate, 1e-9)
	assert.Equal(t, "2026-05-20T10:00:00Z", got.CreatedAt,
		"createdAt must be preserved across update")
	assert.Equal(t, "2026-05-23T10:00:00Z", got.UpdatedAt,
		"updatedAt must be the handler's nowFunc value")
}

func TestPricing_UpdateExcludesSelfFromOverlapCheck(t *testing.T) {
	store := newFakePricingStore()
	store.rows["p-1"] = bandPlanRow("p-1", "2026-01-01", strPtr("2027-01-01"), 0.25)
	h := newPricingTestHandler(store)

	resp, err := h.Handle(context.Background(),
		makeJSONRequest(http.MethodPut, "/pricing/p-1", singleBandBody("2026-01-01", strPtr("2027-01-01"))))
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestPricing_UpdateEndsOpenEndedPlanWithoutSuccessor(t *testing.T) {
	// AC 2.4: giving the open-ended plan an end date is allowed on its own;
	// the days after it are simply unpriced until a successor exists.
	store := newFakePricingStore()
	store.rows["open-id"] = bandPlanRow("open-id", "2026-01-01", nil, 0.25)
	openID := "open-id"
	store.openEndedID = &openID
	h := newPricingTestHandler(store)

	resp, err := h.Handle(context.Background(),
		makeJSONRequest(http.MethodPut, "/pricing/open-id", singleBandBody("2026-01-01", strPtr("2026-08-01"))))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, "response: %s", resp.Body)

	var got dynamo.PricingItem
	require.NoError(t, json.Unmarshal([]byte(resp.Body), &got))
	require.NotNil(t, got.EndDate)
	assert.Equal(t, "2026-08-01", *got.EndDate)
	assert.Nil(t, store.openEndedID, "the plan is no longer open-ended")
}

func TestPricing_UpdateUnknownIdReturns404(t *testing.T) {
	store := newFakePricingStore()
	h := newPricingTestHandler(store)

	resp, err := h.Handle(context.Background(),
		makeJSONRequest(http.MethodPut, "/pricing/missing", singleBandBody("2026-01-01", strPtr("2027-01-01"))))
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestPricing_DeleteExistingPeriod(t *testing.T) {
	store := newFakePricingStore()
	store.rows["p-1"] = bandPlanRow("p-1", "2026-01-01", strPtr("2027-01-01"), 0.25)
	h := newPricingTestHandler(store)

	resp, err := h.Handle(context.Background(), makeRequest(http.MethodDelete, "/pricing/p-1", "Bearer "+testToken))
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	assert.Empty(t, store.rows, "row must be removed")
}

func TestPricing_DeleteUnknownIdReturns404(t *testing.T) {
	store := newFakePricingStore()
	h := newPricingTestHandler(store)

	resp, err := h.Handle(context.Background(), makeRequest(http.MethodDelete, "/pricing/missing", "Bearer "+testToken))
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestPricing_ReplaceOpenEndedSameDaySuccession(t *testing.T) {
	// AC 2.2: the closing row's exclusive endDate is the successor's start
	// date — the same literal string on both rows, so the switch day is
	// priced by the successor.
	store := newFakePricingStore()
	store.rows["open-id"] = bandPlanRow("open-id", "2026-01-01", nil, 0.25)
	openID := "open-id"
	store.openEndedID = &openID
	h := newPricingTestHandler(store)

	body := `{"closingPricingId":"open-id","newPeriod":` + newPlanBody + `}`
	resp, err := h.Handle(context.Background(), makeJSONRequest(http.MethodPost, "/pricing/replace-open-ended", body))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, "response: %s", resp.Body)

	var got struct {
		Pricing []dynamo.PricingItem `json:"pricing"`
	}
	require.NoError(t, json.Unmarshal([]byte(resp.Body), &got))
	require.Len(t, got.Pricing, 2)
	require.NotNil(t, got.Pricing[0].EndDate)
	assert.Equal(t, "open-id", got.Pricing[0].PricingID)
	assert.Equal(t, "2026-08-01", *got.Pricing[0].EndDate,
		"closing-row endDate equals newPeriod.startDate")
	assert.Equal(t, "2026-08-01", got.Pricing[1].StartDate)
	assert.Nil(t, got.Pricing[1].EndDate)
	require.Len(t, got.Pricing[1].Windows, 2, "successor keeps its band windows")
}

func TestPricing_ReplaceOpenEndedAcceptsFutureDatedSuccessor(t *testing.T) {
	// AC 2.3: the successor is entered ahead of its start date, which is what
	// makes the switch happen automatically.
	store := newFakePricingStore()
	store.rows["open-id"] = bandPlanRow("open-id", "2026-01-01", nil, 0.25)
	openID := "open-id"
	store.openEndedID = &openID
	h := newPricingTestHandler(store)

	// nowFunc is 2026-05-23; the successor starts more than two months later.
	body := `{"closingPricingId":"open-id","newPeriod":` + newPlanBody + `}`
	resp, err := h.Handle(context.Background(), makeJSONRequest(http.MethodPost, "/pricing/replace-open-ended", body))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, "a future start date is accepted: %s", resp.Body)

	stored := store.rows["open-id"]
	require.NotNil(t, stored.EndDate)
	assert.Equal(t, "2026-08-01", *stored.EndDate)
}

func TestPricing_ReplaceOpenEndedRejectsLegacyNewPeriod(t *testing.T) {
	store := newFakePricingStore()
	store.rows["open-id"] = bandPlanRow("open-id", "2026-01-01", nil, 0.25)
	openID := "open-id"
	store.openEndedID = &openID
	h := newPricingTestHandler(store)

	body := `{"closingPricingId":"open-id","newPeriod":{"startDate":"2026-06-01","peakRate":0.3,"feedInRate":0.05,"offPeakSavingsRate":0.1}}`
	resp, err := h.Handle(context.Background(), makeJSONRequest(http.MethodPost, "/pricing/replace-open-ended", body))
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Equal(t, "legacy_shape", decodeError(t, resp.Body).Error)
}

func TestPricing_ReplaceOpenEndedMapsLegacyClosingRowTo400(t *testing.T) {
	// Q32: the store refuses to patch a closing row that is still the legacy
	// three-rate shape — a partial UpdateItem would leave a legacy-detected
	// row carrying an exclusive end date, double-shifted by the read transform
	// and the migration.
	store := newFakePricingStore()
	store.rows["open-id"] = bandPlanRow("open-id", "2026-01-01", nil, 0.25)
	openID := "open-id"
	store.openEndedID = &openID
	store.replaceErr = dynamo.ErrPricingLegacyShape
	h := newPricingTestHandler(store)

	body := `{"closingPricingId":"open-id","newPeriod":` + newPlanBody + `}`
	resp, err := h.Handle(context.Background(), makeJSONRequest(http.MethodPost, "/pricing/replace-open-ended", body))
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Equal(t, "legacy_shape", decodeError(t, resp.Body).Error)
}

func TestPricing_ReplaceOpenEndedRejectsUnknownClosingID(t *testing.T) {
	store := newFakePricingStore()
	h := newPricingTestHandler(store)

	body := `{"closingPricingId":"missing","newPeriod":` + newPlanBody + `}`
	resp, err := h.Handle(context.Background(), makeJSONRequest(http.MethodPost, "/pricing/replace-open-ended", body))
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestPricing_ReplaceOpenEndedMapsConcurrentWriteTo409(t *testing.T) {
	store := newFakePricingStore()
	store.rows["open-id"] = bandPlanRow("open-id", "2026-01-01", nil, 0.25)
	openID := "open-id"
	store.openEndedID = &openID
	store.replaceErr = dynamo.ErrPricingConcurrentWrite
	h := newPricingTestHandler(store)

	body := `{"closingPricingId":"open-id","newPeriod":` + newPlanBody + `}`
	resp, err := h.Handle(context.Background(), makeJSONRequest(http.MethodPost, "/pricing/replace-open-ended", body))
	require.NoError(t, err)
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
	assert.Equal(t, "concurrent_open_ended_write", decodeError(t, resp.Body).Error)
}

func TestPricing_ReplaceOpenEndedMapsUUIDCollisionTo500(t *testing.T) {
	store := newFakePricingStore()
	store.rows["open-id"] = bandPlanRow("open-id", "2026-01-01", nil, 0.25)
	openID := "open-id"
	store.openEndedID = &openID
	store.replaceErr = dynamo.ErrPricingUUIDCollision
	h := newPricingTestHandler(store)

	body := `{"closingPricingId":"open-id","newPeriod":` + newPlanBody + `}`
	resp, err := h.Handle(context.Background(), makeJSONRequest(http.MethodPost, "/pricing/replace-open-ended", body))
	require.NoError(t, err)
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	assert.Equal(t, "internal_error", decodeError(t, resp.Body).Error)
}

func TestPricing_CreateMapsTransactionalConcurrentWriteTo409(t *testing.T) {
	store := newFakePricingStore()
	store.putErr = dynamo.ErrPricingConcurrentWrite
	h := newPricingTestHandler(store)

	resp, err := h.Handle(context.Background(),
		makeJSONRequest(http.MethodPost, "/pricing", singleBandBody("2026-01-01", nil)))
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

func TestPricing_BodyOverCapReturns400(t *testing.T) {
	// The 4 KB cap is retained: a plan with a pathological number of windows
	// is rejected before it reaches the validator.
	store := newFakePricingStore()
	h := newPricingTestHandler(store)

	windows := make([]string, 0, 200)
	for i := range 200 {
		windows = append(windows, fmt.Sprintf(`{"start":"%02d:00","end":"%02d:30","free":false,"rate":0.28}`, i%24, i%24))
	}
	body := `{"startDate":"2026-01-01","defaultRate":0.3,"windows":[` +
		strings.Join(windows, ",") + `],"feedInRate":0.05}`
	require.Greater(t, len(body), pricingBodyMaxBytes)

	resp, err := h.Handle(context.Background(), makeJSONRequest(http.MethodPost, "/pricing", body))
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Empty(t, store.rows)
}

func TestPricing_StoreFailureMapsTo500(t *testing.T) {
	store := newFakePricingStore()
	store.listErr = errors.New("dynamo down")
	h := newPricingTestHandler(store)

	resp, err := h.Handle(context.Background(), makeRequest(http.MethodGet, "/pricing", "Bearer "+testToken))
	require.NoError(t, err)
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}
