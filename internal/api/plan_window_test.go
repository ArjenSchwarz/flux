package api

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/ArjenSchwarz/flux/internal/plan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file covers requirement 4: the free window used by every off-peak
// feature comes from the plan pricing the day in question, and its absence is
// rendered as "no window" rather than falling back to a default.

// win builds a resolved free window from its "HH:MM" bounds, for the unit
// tests of the helpers that take one directly.
func win(start, end string) *offpeakWindow {
	startMin, ok := plan.ParseBandTime(start)
	if !ok {
		panic("bad window start: " + start)
	}
	endMin, ok := plan.ParseBandTime(end)
	if !ok {
		panic("bad window end: " + end)
	}
	return &offpeakWindow{startMin: startMin, endMin: endMin}
}

func freeBand(start, end string) dynamo.PricingWindow {
	return dynamo.PricingWindow{Start: start, End: end, Free: true}
}

func ratedBand(start, end string, rate float64) dynamo.PricingWindow {
	return dynamo.PricingWindow{Start: start, End: end, Rate: &rate}
}

// planRow builds a stored plan row pricing [start, end) with the given
// exception windows.
func planRow(id, start string, end *string, windows ...dynamo.PricingWindow) dynamo.PricingItem {
	savings := 0.15
	return dynamo.PricingItem{
		PricingID:            id,
		StartDate:            start,
		EndDate:              end,
		DefaultRate:          0.3,
		Windows:              windows,
		FeedInRate:           0.05,
		SavingsReferenceRate: &savings,
		CreatedAt:            "2026-01-01T00:00:00Z",
		UpdatedAt:            "2026-01-01T00:00:00Z",
	}
}

// storeWithPlans returns a pricing store holding exactly the given rows.
func storeWithPlans(rows ...dynamo.PricingItem) *fakePricingStore {
	s := newFakePricingStore()
	for _, row := range rows {
		s.rows[row.PricingID] = row
		if row.EndDate == nil {
			id := row.PricingID
			s.openEndedID = &id
		}
	}
	return s
}

// handlerWithPlans builds a handler over the given reader whose pricing store
// holds exactly the given plans.
func handlerWithPlans(reader dynamo.Reader, rows ...dynamo.PricingItem) *Handler {
	h := NewHandler(reader, nil, testSerial, testToken)
	h.SetPricingStore(storeWithPlans(rows...))
	return h
}

// steadyImportReadings returns one reading per minute from Sydney midnight on
// now's date up to and including now, all importing importW watts.
func steadyImportReadings(now time.Time, importW float64) []dynamo.ReadingItem {
	local := now.In(sydneyTZ)
	midnight := startOfDaySydney(local)
	var out []dynamo.ReadingItem
	for ts := midnight; !ts.After(local); ts = ts.Add(time.Minute) {
		out = append(out, dynamo.ReadingItem{
			Timestamp: ts.Unix(),
			Pgrid:     importW,
			Pload:     importW,
			Soc:       50,
		})
	}
	return out
}

func readerWithReadings(readings []dynamo.ReadingItem) *mockReader {
	return &mockReader{
		queryReadingsFn: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
			return readings, nil
		},
	}
}

func TestStatus_OffpeakWindowComesFromThePlan(t *testing.T) {
	// AC 4.1: the window is the free band of the plan pricing today, not a
	// separately maintained configuration value.
	now := fixedNow()
	h := handlerWithPlans(&mockReader{}, planRow("p", "2026-01-01", nil, freeBand("10:00", "15:00")))
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), statusRequest())
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	sr := parseStatusResponse(t, resp)
	require.NotNil(t, sr.Offpeak)
	assert.Equal(t, "10:00", sr.Offpeak.WindowStart)
	assert.Equal(t, "15:00", sr.Offpeak.WindowEnd)
}

func TestStatus_OffpeakNullWhenPlanHasNoFreeBand(t *testing.T) {
	// Q35 / AC 4.4: a plan without a free band has no window to report, and
	// clients must render that as "no window" rather than substituting the
	// legacy default.
	now := fixedNow()
	h := handlerWithPlans(&mockReader{}, planRow("p", "2026-01-01", nil, ratedBand("01:00", "06:00", 0.28)))
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), statusRequest())
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	sr := parseStatusResponse(t, resp)
	assert.Nil(t, sr.Offpeak, "a no-free-band day has no off-peak object")

	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(resp.Body), &raw))
	assert.JSONEq(t, `null`, string(raw["offpeak"]), "absence serialises as explicit null")
}

func TestStatus_OffpeakNullWhenNoPlanPricesToday(t *testing.T) {
	// AC 2.7 / AC 4.4: an unpriced day behaves as it does when no off-peak
	// data exists — values absent, never zero and never a default window.
	now := fixedNow()
	h := handlerWithPlans(&mockReader{}) // no plans at all
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), statusRequest())
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	sr := parseStatusResponse(t, resp)
	assert.Nil(t, sr.Offpeak)
}

func TestStatus_NoPlanLeavesOffpeakAndPeakAbsentNotZero(t *testing.T) {
	// AC 4.4: absent, not zero. With readings present the peak integration
	// would otherwise happily report a number derived from a guessed window.
	now := time.Date(2026, 4, 15, 16, 0, 0, 0, sydneyTZ)
	readings := steadyImportReadings(now, 1000)
	mr := readerWithReadings(readings)
	mr.getOffpeakFn = func(_ context.Context, serial, date string) (*dynamo.OffpeakItem, error) {
		return &dynamo.OffpeakItem{SysSn: serial, Date: date, Status: dynamo.OffpeakStatusPending}, nil
	}
	h := handlerWithPlans(mr)
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), statusRequest())
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	sr := parseStatusResponse(t, resp)
	assert.Nil(t, sr.Offpeak)
	assert.Nil(t, sr.PeakGridImportKwh, "peak import needs a window to bracket, so it is absent too")
}

func TestStatus_PricingReadFailureReturns500(t *testing.T) {
	// Q14: a Lambda read failure must never be resolved as "no plan" — that
	// would silently strip a priced day's window and cost data.
	now := fixedNow()
	store := storeWithPlans(planRow("p", "2026-01-01", nil, freeBand("11:00", "14:00")))
	store.listErr = errors.New("dynamo down")
	h := NewHandler(&mockReader{}, nil, testSerial, testToken)
	h.SetPricingStore(store)
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), statusRequest())
	require.NoError(t, err)
	assert.Equal(t, 500, resp.StatusCode)
}

func TestStatus_CutoffSuppressionUsesSuccessorWindowOnSwitchEve(t *testing.T) {
	// AC 4.2 / Q11: on the eve of a plan switch the next charging window is
	// the successor's, so a cutoff landing inside it must be suppressed.
	//
	// now is 20:00 on 2026-07-31 (past today's window). At 55% SoC and 460 W
	// discharge against 13.34 kWh the projected cutoff is ~14.5 h out —
	// 10:30 on 2026-08-01, after the successor's 10:00 window start but
	// before the predecessor's 11:00 one.
	now := time.Date(2026, 7, 31, 20, 0, 0, 0, sydneyTZ)
	nowUnix := now.Unix()
	mr := readerWithReadings([]dynamo.ReadingItem{
		{Timestamp: nowUnix - 600, Pbat: 460, Pload: 500, Soc: 56},
		{Timestamp: nowUnix - 10, Pbat: 460, Pload: 500, Soc: 55},
	})
	mr.getSystemFn = func(_ context.Context, serial string) (*dynamo.SystemItem, error) {
		return &dynamo.SystemItem{SysSn: serial, Cobat: 13.34}, nil
	}

	switchDate := "2026-08-01"
	predecessor := planRow("old", "2026-01-01", &switchDate, freeBand("11:00", "14:00"))

	t.Run("successor moves the window earlier and suppresses the cutoff", func(t *testing.T) {
		h := handlerWithPlans(mr, predecessor,
			planRow("new", switchDate, nil, freeBand("10:00", "15:00")))
		h.nowFunc = func() time.Time { return now }

		resp, err := h.Handle(context.Background(), statusRequest())
		require.NoError(t, err)
		require.Equal(t, 200, resp.StatusCode)

		sr := parseStatusResponse(t, resp)
		require.NotNil(t, sr.Battery)
		assert.Nil(t, sr.Battery.EstimatedCutoff,
			"cutoff at ~10:30 falls inside the successor's 10:00-15:00 window")
		require.NotNil(t, sr.Rolling15m)
		assert.Nil(t, sr.Rolling15m.EstimatedCutoff)
	})

	t.Run("successor keeping the same window leaves the cutoff visible", func(t *testing.T) {
		h := handlerWithPlans(mr, predecessor,
			planRow("new", switchDate, nil, freeBand("11:00", "14:00")))
		h.nowFunc = func() time.Time { return now }

		resp, err := h.Handle(context.Background(), statusRequest())
		require.NoError(t, err)
		require.Equal(t, 200, resp.StatusCode)

		sr := parseStatusResponse(t, resp)
		require.NotNil(t, sr.Battery)
		require.NotNil(t, sr.Battery.EstimatedCutoff,
			"cutoff at ~10:30 is before the 11:00 window start")
	})
}

func TestNextOffpeakStart_ResolvesPerDayPlans(t *testing.T) {
	switchDate := "2026-08-01"
	predecessor := planRow("old", "2026-01-01", &switchDate, freeBand("11:00", "14:00")).Plan()
	successor := planRow("new", switchDate, nil, freeBand("10:00", "15:00")).Plan()
	openEnded := planRow("only", "2026-01-01", nil, freeBand("11:00", "14:00")).Plan()
	noFreeBand := planRow("flat", "2026-01-01", nil, ratedBand("01:00", "06:00", 0.28)).Plan()

	cases := map[string]struct {
		now   time.Time
		plans []plan.Plan
		want  time.Time
		ok    bool
	}{
		"before today's window": {
			now:   time.Date(2026, 7, 20, 8, 0, 0, 0, sydneyTZ),
			plans: []plan.Plan{openEnded},
			want:  time.Date(2026, 7, 20, 11, 0, 0, 0, sydneyTZ),
			ok:    true,
		},
		"exactly at today's window start": {
			now:   time.Date(2026, 7, 20, 11, 0, 0, 0, sydneyTZ),
			plans: []plan.Plan{openEnded},
			want:  time.Date(2026, 7, 20, 11, 0, 0, 0, sydneyTZ),
			ok:    true,
		},
		"exactly at today's window end rolls to tomorrow": {
			now:   time.Date(2026, 7, 20, 14, 0, 0, 0, sydneyTZ),
			plans: []plan.Plan{openEnded},
			want:  time.Date(2026, 7, 21, 11, 0, 0, 0, sydneyTZ),
			ok:    true,
		},
		"inside today's window": {
			now:   time.Date(2026, 7, 20, 12, 0, 0, 0, sydneyTZ),
			plans: []plan.Plan{openEnded},
			want:  time.Date(2026, 7, 20, 11, 0, 0, 0, sydneyTZ),
			ok:    true,
		},
		"after today's window rolls to tomorrow": {
			now:   time.Date(2026, 7, 20, 20, 0, 0, 0, sydneyTZ),
			plans: []plan.Plan{openEnded},
			want:  time.Date(2026, 7, 21, 11, 0, 0, 0, sydneyTZ),
			ok:    true,
		},
		"switch eve takes the successor's window": {
			now:   time.Date(2026, 7, 31, 20, 0, 0, 0, sydneyTZ),
			plans: []plan.Plan{predecessor, successor},
			want:  time.Date(2026, 8, 1, 10, 0, 0, 0, sydneyTZ),
			ok:    true,
		},
		"switch day itself uses the successor": {
			now:   time.Date(2026, 8, 1, 8, 0, 0, 0, sydneyTZ),
			plans: []plan.Plan{predecessor, successor},
			want:  time.Date(2026, 8, 1, 10, 0, 0, 0, sydneyTZ),
			ok:    true,
		},
		"unpriced today still finds tomorrow's window": {
			now:   time.Date(2026, 7, 31, 20, 0, 0, 0, sydneyTZ),
			plans: []plan.Plan{successor},
			want:  time.Date(2026, 8, 1, 10, 0, 0, 0, sydneyTZ),
			ok:    true,
		},
		"no plan at all has no boundary": {
			now:   time.Date(2026, 7, 20, 8, 0, 0, 0, sydneyTZ),
			plans: nil,
		},
		"plan without a free band has no boundary": {
			now:   time.Date(2026, 7, 20, 8, 0, 0, 0, sydneyTZ),
			plans: []plan.Plan{noFreeBand},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, ok := nextOffpeakStart(tc.now, tc.plans)
			require.Equal(t, tc.ok, ok)
			if tc.ok {
				assert.True(t, tc.want.Equal(got), "want %s, got %s", tc.want, got)
			}
		})
	}
}

func TestDay_TodayOffpeakSplitUsesThePlanWindow(t *testing.T) {
	// AC 4.1/4.3: today's live split integrates over the plan's free band.
	// A steady 1 kW import makes the expected value the window length in kWh,
	// so the 10:00-15:00 plan and the 11:00-14:00 plan give different answers.
	now := time.Date(2026, 4, 15, 16, 0, 0, 0, sydneyTZ)
	date := "2026-04-15"
	mr := readerWithReadings(steadyImportReadings(now, 1000))
	mr.getOffpeakFn = func(_ context.Context, serial, d string) (*dynamo.OffpeakItem, error) {
		return &dynamo.OffpeakItem{SysSn: serial, Date: d, Status: dynamo.OffpeakStatusPending}, nil
	}
	mr.getDailyEnergyFn = func(_ context.Context, serial, d string) (*dynamo.DailyEnergyItem, error) {
		return &dynamo.DailyEnergyItem{SysSn: serial, Date: d, EInput: 16}, nil
	}

	cases := map[string]struct {
		window dynamo.PricingWindow
		want   float64
	}{
		"legacy window":   {window: freeBand("11:00", "14:00"), want: 3},
		"new plan window": {window: freeBand("10:00", "15:00"), want: 5},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h := handlerWithPlans(mr, planRow("p", "2026-01-01", nil, tc.window))
			h.nowFunc = func() time.Time { return now }

			req := makeRequest("GET", "/day", "Bearer "+testToken)
			req.QueryStringParameters = map[string]string{"date": date}
			resp, err := h.Handle(context.Background(), req)
			require.NoError(t, err)
			require.Equal(t, 200, resp.StatusCode)

			var dr DayDetailResponse
			require.NoError(t, json.Unmarshal([]byte(resp.Body), &dr))
			require.NotNil(t, dr.Summary)
			require.NotNil(t, dr.Summary.OffpeakGridImportKwh)
			assert.InDelta(t, tc.want, *dr.Summary.OffpeakGridImportKwh, 0.05)
		})
	}
}

func TestDay_NoPlanLeavesOffpeakValuesAbsent(t *testing.T) {
	now := time.Date(2026, 4, 15, 16, 0, 0, 0, sydneyTZ)
	mr := readerWithReadings(steadyImportReadings(now, 1000))
	mr.getOffpeakFn = func(_ context.Context, serial, d string) (*dynamo.OffpeakItem, error) {
		return &dynamo.OffpeakItem{SysSn: serial, Date: d, Status: dynamo.OffpeakStatusPending}, nil
	}
	mr.getDailyEnergyFn = func(_ context.Context, serial, d string) (*dynamo.DailyEnergyItem, error) {
		return &dynamo.DailyEnergyItem{SysSn: serial, Date: d, EInput: 16}, nil
	}
	h := handlerWithPlans(mr)
	h.nowFunc = func() time.Time { return now }

	req := makeRequest("GET", "/day", "Bearer "+testToken)
	req.QueryStringParameters = map[string]string{"date": "2026-04-15"}
	resp, err := h.Handle(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	var dr DayDetailResponse
	require.NoError(t, json.Unmarshal([]byte(resp.Body), &dr))
	require.NotNil(t, dr.Summary)
	assert.Nil(t, dr.Summary.OffpeakGridImportKwh, "absent, not zero")
	assert.Nil(t, dr.Summary.OffpeakGridExportKwh)
	assert.Nil(t, dr.Summary.PeakGridImportKwh)
}

func TestDay_PricingReadFailureReturns500(t *testing.T) {
	now := time.Date(2026, 4, 15, 16, 0, 0, 0, sydneyTZ)
	store := storeWithPlans(planRow("p", "2026-01-01", nil, freeBand("11:00", "14:00")))
	store.listErr = errors.New("dynamo down")
	h := NewHandler(&mockReader{}, nil, testSerial, testToken)
	h.SetPricingStore(store)
	h.nowFunc = func() time.Time { return now }

	req := makeRequest("GET", "/day", "Bearer "+testToken)
	req.QueryStringParameters = map[string]string{"date": "2026-04-15"}
	resp, err := h.Handle(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, 500, resp.StatusCode)
}

func TestHistory_TodayRowUsesThePlanWindow(t *testing.T) {
	now := time.Date(2026, 4, 15, 16, 0, 0, 0, sydneyTZ)
	date := "2026-04-15"
	mr := readerWithReadings(steadyImportReadings(now, 1000))
	mr.queryDailyEnergyFn = func(_ context.Context, serial, _, _ string) ([]dynamo.DailyEnergyItem, error) {
		return []dynamo.DailyEnergyItem{{SysSn: serial, Date: date, EInput: 16}}, nil
	}
	mr.queryOffpeakFn = func(_ context.Context, serial, _, _ string) ([]dynamo.OffpeakItem, error) {
		return []dynamo.OffpeakItem{{SysSn: serial, Date: date, Status: dynamo.OffpeakStatusPending}}, nil
	}

	cases := map[string]struct {
		window dynamo.PricingWindow
		want   float64
	}{
		"legacy window":   {window: freeBand("11:00", "14:00"), want: 3},
		"new plan window": {window: freeBand("10:00", "15:00"), want: 5},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h := handlerWithPlans(mr, planRow("p", "2026-01-01", nil, tc.window))
			h.nowFunc = func() time.Time { return now }

			req := makeRequest("GET", "/history", "Bearer "+testToken)
			req.QueryStringParameters = map[string]string{"days": "1"}
			resp, err := h.Handle(context.Background(), req)
			require.NoError(t, err)
			require.Equal(t, 200, resp.StatusCode)

			var hr HistoryResponse
			require.NoError(t, json.Unmarshal([]byte(resp.Body), &hr))
			require.Len(t, hr.Days, 1)
			require.NotNil(t, hr.Days[0].OffpeakGridImportKwh)
			assert.InDelta(t, tc.want, *hr.Days[0].OffpeakGridImportKwh, 0.05)
		})
	}
}

func TestHistory_PricingReadFailureReturns500(t *testing.T) {
	now := time.Date(2026, 4, 15, 16, 0, 0, 0, sydneyTZ)
	store := storeWithPlans(planRow("p", "2026-01-01", nil, freeBand("11:00", "14:00")))
	store.listErr = errors.New("dynamo down")
	h := NewHandler(&mockReader{}, nil, testSerial, testToken)
	h.SetPricingStore(store)
	h.nowFunc = func() time.Time { return now }

	req := makeRequest("GET", "/history", "Bearer "+testToken)
	req.QueryStringParameters = map[string]string{"days": "7"}
	resp, err := h.Handle(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, 500, resp.StatusCode)
}
