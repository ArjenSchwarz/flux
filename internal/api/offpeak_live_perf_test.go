package api

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHandleStatus_LiveOffpeak_P95Under500ms covers AC 8.2: a /status
// response on a day with live-integrated off-peak (pending row + readings
// integration over [offpeak-start, min(now, offpeak-end))) must complete
// within 500 ms p95 warm at the production memory configuration.
//
// The test drives a full /status request through the same handler that
// runs in Lambda — including JSON marshal, mux routing, and the live
// integration over a synthetic 1080-reading off-peak window. p95 is
// measured across many warm invocations against the same in-memory
// mockReader (no I/O latency to confound the measurement) and logged in
// the test output so the spec can reference the recorded number.
//
// 500 ms is generous on developer hardware (typical: <5 ms p95). The
// budget exists primarily to catch regressions that change algorithmic
// complexity or add per-request I/O.
func TestHandleStatus_LiveOffpeak_P95Under500ms(t *testing.T) {
	loc := sydneyTZ
	now := time.Date(2026, 4, 15, 13, 30, 0, 0, loc) // 30 min before window-end
	opStart := time.Date(2026, 4, 15, 11, 0, 0, 0, loc)

	// A full off-peak window's worth of 10-second readings spanning the
	// query horizon (nowUnix-86400 → nowUnix). Pgrid varies sample-to-sample
	// so the integration does real per-sample work, matching the production
	// pattern.
	var readings []dynamo.ReadingItem
	dayStart := time.Date(2026, 4, 15, 0, 0, 0, 0, loc)
	const intervalSeconds = 10
	for ts := dayStart.Unix(); ts <= now.Unix(); ts += intervalSeconds {
		readings = append(readings, dynamo.ReadingItem{
			Timestamp: ts,
			Ppv:       float64(500 + int(ts)%2000),
			Pgrid:     float64(-1500 + int(ts)%5500),
			Pbat:      float64(-2500 + int(ts)%4500),
			Pload:     float64(500 + int(ts)%4000),
			Soc:       float64(20 + int(ts)%70),
		})
	}
	require.GreaterOrEqual(t, len(readings), 1080,
		"fixture must include a full off-peak window's worth of readings")

	mr := &mockReader{
		queryReadingsFn: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
			return readings, nil
		},
		getOffpeakFn: func(_ context.Context, _, _ string) (*dynamo.OffpeakItem, error) {
			return &dynamo.OffpeakItem{Status: dynamo.OffpeakStatusPending}, nil
		},
	}
	h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
	h.nowFunc = func() time.Time { return now }

	// Warm-up — first call pays one-off costs (sync.Once init, allocator
	// warm-up) we don't want to attribute to steady-state p95.
	const warmup = 10
	for range warmup {
		resp, err := h.Handle(context.Background(), statusRequest())
		require.NoError(t, err)
		require.Equal(t, 200, resp.StatusCode)
	}

	// Warm measurement: integrate the live deltas across many requests.
	// 200 samples → p95 lands on the 190th sorted timing.
	const samples = 200
	timings := make([]time.Duration, samples)
	for i := range samples {
		started := time.Now()
		resp, err := h.Handle(context.Background(), statusRequest())
		timings[i] = time.Since(started)
		require.NoError(t, err)
		require.Equal(t, 200, resp.StatusCode)
	}

	// Confirm the live integration actually executed — sanity-checks the
	// fixture so a future refactor that bypasses liveOffpeakDeltas doesn't
	// silently make the timing meaningless.
	{
		resp, err := h.Handle(context.Background(), statusRequest())
		require.NoError(t, err)
		sr := parseStatusResponse(t, resp)
		require.NotNil(t, sr.Offpeak)
		require.Equal(t, dynamo.OffpeakStatusPending, sr.Offpeak.Status)
		require.NotNil(t, sr.Offpeak.GridUsageKwh,
			"live path must populate gridUsageKwh from the readings integration")
	}

	sort.Slice(timings, func(i, j int) bool { return timings[i] < timings[j] })
	p50 := timings[samples/2]
	p95 := timings[(samples*95)/100]
	p99 := timings[(samples*99)/100]
	max := timings[samples-1]
	opStartStr := opStart.Format("15:04:05")
	t.Logf("/status live-offpeak (opStart=%s, now=%s, readings=%d, n=%d): p50=%s p95=%s p99=%s max=%s",
		opStartStr, now.Format("15:04:05"), len(readings), samples, p50, p95, p99, max)

	assert.Less(t, p95, 500*time.Millisecond,
		"AC 8.2: /status p95 with live off-peak integration must be < 500 ms; got %s", p95)
}
