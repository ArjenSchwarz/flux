package poller

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockPlanLister is a function-field test double for the pricing read
// surface. responses is consumed one entry per call; the last entry repeats
// once exhausted so a test can express "fails twice then succeeds forever".
type mockPlanLister struct {
	responses []planListerResponse
	calls     int
}

type planListerResponse struct {
	items []dynamo.PricingItem
	err   error
}

func (m *mockPlanLister) ListPricing(_ context.Context) ([]dynamo.PricingItem, error) {
	m.calls++
	if len(m.responses) == 0 {
		return nil, nil
	}
	i := min(m.calls-1, len(m.responses)-1)
	return m.responses[i].items, m.responses[i].err
}

func testPricingItem(id, start, end string, defaultRate float64) dynamo.PricingItem {
	savings := 0.35
	item := dynamo.PricingItem{
		PricingID:            id,
		StartDate:            start,
		DefaultRate:          defaultRate,
		Windows:              []dynamo.PricingWindow{{Start: "11:00", End: "14:00", Free: true}},
		FeedInRate:           0.05,
		SavingsReferenceRate: &savings,
	}
	if end != "" {
		item.EndDate = &end
	}
	return item
}

// testPlanSource builds a PlanSource with a backoff short enough that a
// retry sequence costs microseconds rather than seconds.
func testPlanSource(l PlanLister) *PlanSource {
	s := NewPlanSource(l)
	s.retryDelay = time.Microsecond
	return s
}

func TestPlanSource_LoadsPlansFromStore(t *testing.T) {
	lister := &mockPlanLister{responses: []planListerResponse{
		{items: []dynamo.PricingItem{testPricingItem("a", "2026-01-01", "", 0.35)}},
	}}
	src := testPlanSource(lister)

	plans, err := src.Plans(t.Context())

	require.NoError(t, err)
	require.Len(t, plans, 1)
	assert.Equal(t, "a", plans[0].ID)
	assert.Equal(t, 0.35, plans[0].DefaultRate)
	assert.Equal(t, 1, lister.calls)
}

func TestPlanSource_RefreshReplacesCache(t *testing.T) {
	lister := &mockPlanLister{responses: []planListerResponse{
		{items: []dynamo.PricingItem{testPricingItem("old", "2026-01-01", "2026-08-01", 0.35)}},
		{items: []dynamo.PricingItem{
			testPricingItem("old", "2026-01-01", "2026-08-01", 0.35),
			testPricingItem("new", "2026-08-01", "", 0.40),
		}},
	}}
	src := testPlanSource(lister)

	_, err := src.Plans(t.Context())
	require.NoError(t, err)

	plans, err := src.Plans(t.Context())
	require.NoError(t, err)
	require.Len(t, plans, 2)
	assert.Equal(t, "new", plans[1].ID)
}

// A read failure with a warm cache is transient by definition (Q14/AC 4.6):
// the last-good plan set is served so a window boundary is never processed
// as "no plan".
func TestPlanSource_ReadFailureServesLastGoodCache(t *testing.T) {
	buf, restore := captureLog()
	defer restore()

	lister := &mockPlanLister{responses: []planListerResponse{
		{items: []dynamo.PricingItem{testPricingItem("a", "2026-01-01", "", 0.35)}},
		{err: errors.New("dynamo unavailable")},
	}}
	src := testPlanSource(lister)

	_, err := src.Plans(t.Context())
	require.NoError(t, err)

	plans, err := src.Plans(t.Context())
	require.NoError(t, err)
	require.Len(t, plans, 1)
	assert.Equal(t, "a", plans[0].ID)
	assert.True(t, logContains(buf, "serving last-good plan cache"))
}

// A warm cache short-circuits the retry loop: there is a usable answer
// already, so blocking the caller through a backoff sequence buys nothing.
func TestPlanSource_ReadFailureWithCacheDoesNotRetry(t *testing.T) {
	lister := &mockPlanLister{responses: []planListerResponse{
		{items: []dynamo.PricingItem{testPricingItem("a", "2026-01-01", "", 0.35)}},
		{err: errors.New("dynamo unavailable")},
	}}
	src := testPlanSource(lister)

	_, err := src.Plans(t.Context())
	require.NoError(t, err)
	_, err = src.Plans(t.Context())
	require.NoError(t, err)

	assert.Equal(t, 2, lister.calls)
}

// An empty table is a legitimate answer, not a failed read: it caches like
// any other success, so a later blip serves the empty set rather than an error.
func TestPlanSource_EmptyResultCachesAsSuccess(t *testing.T) {
	lister := &mockPlanLister{responses: []planListerResponse{
		{items: nil},
		{err: errors.New("dynamo unavailable")},
	}}
	src := testPlanSource(lister)

	plans, err := src.Plans(t.Context())
	require.NoError(t, err)
	assert.Empty(t, plans)

	plans, err = src.Plans(t.Context())
	require.NoError(t, err)
	assert.Empty(t, plans)
}

func TestPlanSource_ColdStartRetriesWithBackoff(t *testing.T) {
	lister := &mockPlanLister{responses: []planListerResponse{
		{err: errors.New("table unreachable")},
		{err: errors.New("table unreachable")},
		{items: []dynamo.PricingItem{testPricingItem("a", "2026-01-01", "", 0.35)}},
	}}
	src := testPlanSource(lister)

	plans, err := src.Plans(t.Context())

	require.NoError(t, err)
	require.Len(t, plans, 1)
	assert.Equal(t, 3, lister.calls)
}

// Exhausting the cold-start retries surfaces an error. Returning an empty
// plan set instead would read downstream as "no plan prices this day", which
// AC 4.6 forbids.
func TestPlanSource_ColdStartExhaustedRetriesErrors(t *testing.T) {
	lister := &mockPlanLister{responses: []planListerResponse{
		{err: errors.New("table unreachable")},
	}}
	src := testPlanSource(lister)

	plans, err := src.Plans(t.Context())

	require.Error(t, err)
	assert.Nil(t, plans)
	assert.Equal(t, planLoadAttempts, lister.calls)
}

func TestPlanSource_ColdStartRespectsContextCancellation(t *testing.T) {
	lister := &mockPlanLister{responses: []planListerResponse{
		{err: errors.New("table unreachable")},
	}}
	src := NewPlanSource(lister)
	src.retryDelay = time.Hour // long enough that only cancellation ends the wait

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := src.Plans(ctx)

	require.Error(t, err)
	assert.Equal(t, 1, lister.calls)
}

// The legacy read transform lives in the dynamo layer, so a pre-migration
// table still yields usable band plans here (Q28).
func TestPlanSource_ServesPlansConvertedFromStoredRows(t *testing.T) {
	lister := &mockPlanLister{responses: []planListerResponse{
		{items: []dynamo.PricingItem{testPricingItem("a", "2026-01-01", "2026-08-01", 0.35)}},
	}}
	src := testPlanSource(lister)

	plans, err := src.Plans(t.Context())
	require.NoError(t, err)

	require.Len(t, plans, 1)
	assert.Equal(t, "2026-08-01", plans[0].EndDate)
	start, end, ok := plans[0].FreeWindowMinutes()
	require.True(t, ok)
	assert.Equal(t, 11*60, start)
	assert.Equal(t, 14*60, end)
}
