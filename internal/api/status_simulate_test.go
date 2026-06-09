package api

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/aws/aws-lambda-go/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// simulateStatusRequest builds an authenticated GET /status request carrying
// the simulateLoadWatts query parameter. The empty string omits the parameter
// entirely (no simulation), matching how a real Dashboard refresh without an
// active preset behaves.
func simulateStatusRequest(watts string) events.LambdaFunctionURLRequest {
	req := makeRequest("GET", "/status", "Bearer "+testToken)
	if watts != "" {
		req.QueryStringParameters = map[string]string{"simulateLoadWatts": watts}
	}
	return req
}

// TestHandleStatusSimulateWaterfall covers the load-allocation waterfall
// (reduce export -> battery capped at the ceiling -> grid import) for the live
// trio. Each case asserts energy balance -- delta load == delta battery +
// delta grid -- against the real reading, and that simDischarge is never below
// the real pbat.
func TestHandleStatusSimulateWaterfall(t *testing.T) {
	// Evening "now" well before the off-peak window so a discharge cutoff is
	// not suppressed; this keeps the focus on the trio allocation.
	now := time.Date(2026, 4, 15, 18, 0, 0, 0, sydneyTZ)
	nowUnix := now.Unix()

	cases := map[string]struct {
		// real live reading values
		pload, pbat, pgrid, soc float64
		watts                   int
		// expected simulated live values
		wantPload, wantPbat, wantPgrid float64
	}{
		// (a) importing / zero grid below ceiling -> battery takes all of W,
		// grid unchanged.
		"a) importing below ceiling, battery absorbs all": {
			pload: 2000, pbat: 1000, pgrid: 500, soc: 60,
			watts:     1000,
			wantPload: 3000, wantPbat: 2000, wantPgrid: 500,
		},
		// (b) 1.7 kW over a ~4 kW evening draw -> battery caps at the ceiling,
		// grid import absorbs the overflow.
		"b) evening draw + car caps at ceiling, grid takes overflow": {
			// pbat 4000, +1700 -> would be 5700 but caps at 5000 (absorbed
			// 1000), overflow 700 hits the grid.
			pload: 4200, pbat: 4000, pgrid: 200, soc: 60,
			watts:     1700,
			wantPload: 5900, wantPbat: 5000, wantPgrid: 900,
		},
		// (c) charging + exporting (full sun) -> export cut first, battery
		// charging unchanged.
		"c) charging and exporting, export cut first": {
			// pgrid -2500 (exporting 2.5 kW), pbat -2000 (charging 2.0 kW).
			// W=1700 < 2500 export -> exportReduction 1700, wBattery 0.
			// battery charging unchanged at -2000, grid -2500+1700 = -800.
			pload: 1000, pbat: -2000, pgrid: -2500, soc: 80,
			watts:     1700,
			wantPload: 2700, wantPbat: -2000, wantPgrid: -800,
		},
		// (d) export partially covers W -> remainder hits the battery.
		"d) export partially covers, remainder to battery": {
			// pgrid -500 (exporting 0.5 kW), pbat 1000. W=1500.
			// exportReduction 500, wBattery 1000 -> battery 1000+1000 = 2000.
			// grid -500 + 500 (export reduction) + 0 (no overflow) = 0.
			pload: 1500, pbat: 1000, pgrid: -500, soc: 60,
			watts:     1500,
			wantPload: 3000, wantPbat: 2000, wantPgrid: 0,
		},
		// (e) real pbat already at/above the ceiling -> battery left at the
		// real value, all of wBattery to grid (headroom is 0).
		"e) pbat already above ceiling, all to grid": {
			// pbat 5500 already above 5000 ceiling -> headroom 0, absorbed 0.
			// overflow = W = 1000 -> grid 100 + 1000 = 1100. battery unchanged.
			pload: 6000, pbat: 5500, pgrid: 100, soc: 60,
			watts:     1000,
			wantPload: 7000, wantPbat: 5500, wantPgrid: 1100,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			mr := &mockReader{
				queryReadingsFn: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
					return []dynamo.ReadingItem{
						{Timestamp: nowUnix - 20, Ppv: 0, Pload: tc.pload, Pbat: tc.pbat, Pgrid: tc.pgrid, Soc: tc.soc},
						{Timestamp: nowUnix - 10, Ppv: 0, Pload: tc.pload, Pbat: tc.pbat, Pgrid: tc.pgrid, Soc: tc.soc},
					}, nil
				},
				getSystemFn: func(_ context.Context, serial string) (*dynamo.SystemItem, error) {
					return &dynamo.SystemItem{SysSn: serial, Cobat: 13.34}, nil
				},
			}
			h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
			h.nowFunc = func() time.Time { return now }

			resp, err := h.Handle(context.Background(), simulateStatusRequest(strconv.Itoa(tc.watts)))
			require.NoError(t, err)
			require.Equal(t, 200, resp.StatusCode)

			sr := parseStatusResponse(t, resp)
			require.NotNil(t, sr.Live)
			assert.Equal(t, roundPower(tc.wantPload), sr.Live.Pload, "Pload")
			assert.Equal(t, roundPower(tc.wantPbat), sr.Live.Pbat, "Pbat (simDischarge)")
			assert.Equal(t, roundPower(tc.wantPgrid), sr.Live.Pgrid, "Pgrid")

			// simDischarge never below the real pbat.
			assert.GreaterOrEqual(t, sr.Live.Pbat, roundPower(tc.pbat),
				"adding load must never lower the shown discharge")

			// Energy balance: delta load == delta battery + delta grid.
			dLoad := sr.Live.Pload - roundPower(tc.pload)
			dBat := sr.Live.Pbat - roundPower(tc.pbat)
			dGrid := sr.Live.Pgrid - roundPower(tc.pgrid)
			assert.InDelta(t, dLoad, dBat+dGrid, 1e-6,
				"energy balance: dLoad (%v) == dBat (%v) + dGrid (%v)", dLoad, dBat, dGrid)
			// And delta load equals the added watts.
			assert.InDelta(t, float64(tc.watts), dLoad, 1e-6, "delta load must equal added watts")
		})
	}
}

// TestHandleStatusSimulateEmptyByEarlier verifies the simulated "empty by"
// (rolling15min cutoff) lands earlier than the real one, that the rolling
// averages reflect the simulated allocation, and the off-peak indicator is
// suppressed while simulating.
func TestHandleStatusSimulateEmptyByEarlier(t *testing.T) {
	// Evening so a discharge cutoff is not off-peak-suppressed.
	now := time.Date(2026, 4, 15, 18, 0, 0, 0, sydneyTZ)
	nowUnix := now.Unix()

	mkReader := func() *mockReader {
		return &mockReader{
			queryReadingsFn: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
				// Modest evening discharge well below the ceiling so +W stays
				// below 5 kW: rolling avg pbat == 1000 W.
				return []dynamo.ReadingItem{
					{Timestamp: nowUnix - 60, Ppv: 0, Pload: 1200, Pbat: 1000, Pgrid: 200, Soc: 50},
					{Timestamp: nowUnix - 10, Ppv: 0, Pload: 1200, Pbat: 1000, Pgrid: 200, Soc: 50},
				}, nil
			},
			getSystemFn: func(_ context.Context, serial string) (*dynamo.SystemItem, error) {
				return &dynamo.SystemItem{SysSn: serial, Cobat: 13.34}, nil
			},
		}
	}

	// Real (no simulation) cutoff.
	hReal := NewHandler(mkReader(), nil, testSerial, testToken, "11:00", "14:00")
	hReal.nowFunc = func() time.Time { return now }
	realResp, err := hReal.Handle(context.Background(), simulateStatusRequest(""))
	require.NoError(t, err)
	realSR := parseStatusResponse(t, realResp)
	require.NotNil(t, realSR.Rolling15m)
	require.NotNil(t, realSR.Rolling15m.EstimatedCutoff, "precondition: real rolling cutoff present")

	// Simulated cutoff with +2000 W.
	hSim := NewHandler(mkReader(), nil, testSerial, testToken, "11:00", "14:00")
	hSim.nowFunc = func() time.Time { return now }
	simResp, err := hSim.Handle(context.Background(), simulateStatusRequest("2000"))
	require.NoError(t, err)
	simSR := parseStatusResponse(t, simResp)
	require.NotNil(t, simSR.Rolling15m)
	require.NotNil(t, simSR.Rolling15m.EstimatedCutoff, "simulated rolling cutoff present")

	realCutoff, perr := time.Parse(time.RFC3339, *realSR.Rolling15m.EstimatedCutoff)
	require.NoError(t, perr)
	simCutoff, perr := time.Parse(time.RFC3339, *simSR.Rolling15m.EstimatedCutoff)
	require.NoError(t, perr)
	assert.True(t, simCutoff.Before(realCutoff),
		"simulated empty-by (%s) must be earlier than real (%s)", simCutoff, realCutoff)

	// AvgPbat under simulation reflects simDischarge(avgPbat) = 1000 + 2000.
	assert.Equal(t, roundPower(3000), simSR.Rolling15m.AvgPbat, "rolling AvgPbat reflects simDischarge")
	// AvgLoad under simulation reflects AvgLoad + W = 1200 + 2000.
	assert.Equal(t, roundPower(3200), simSR.Rolling15m.AvgLoad, "rolling AvgLoad reflects +W")

	// Live Pbat reflects simDischarge(latest.Pbat) = 1000 + 2000.
	require.NotNil(t, simSR.Live)
	assert.Equal(t, roundPower(3000), simSR.Live.Pbat, "live Pbat reflects simDischarge")

	// Off-peak indicator suppressed while simulating.
	require.NotNil(t, simSR.Battery)
	assert.Nil(t, simSR.Battery.CantEmptyBeforeOffpeak,
		"cantEmptyBeforeOffpeak must be nil while W>0")
}

// TestHandleStatusSimulateNoEmptyByWhenCharging verifies [4.4] is inherited:
// when the simulated battery is still net charging (export fully absorbs W),
// no "empty by" estimate is shown.
func TestHandleStatusSimulateNoEmptyByWhenCharging(t *testing.T) {
	now := time.Date(2026, 4, 15, 9, 0, 0, 0, sydneyTZ)
	nowUnix := now.Unix()

	mr := &mockReader{
		queryReadingsFn: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
			// Charging 2 kW, exporting 3 kW. +1700 W fully consumed by export
			// reduction -> battery still charging, no cutoff.
			return []dynamo.ReadingItem{
				{Timestamp: nowUnix - 60, Ppv: 6000, Pload: 1000, Pbat: -2000, Pgrid: -3000, Soc: 70},
				{Timestamp: nowUnix - 10, Ppv: 6000, Pload: 1000, Pbat: -2000, Pgrid: -3000, Soc: 70},
			}, nil
		},
		getSystemFn: func(_ context.Context, serial string) (*dynamo.SystemItem, error) {
			return &dynamo.SystemItem{SysSn: serial, Cobat: 13.34}, nil
		},
	}
	h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), simulateStatusRequest("1700"))
	require.NoError(t, err)
	sr := parseStatusResponse(t, resp)
	require.NotNil(t, sr.Live)
	assert.Equal(t, roundPower(-2000), sr.Live.Pbat, "battery still charging after export reduction")
	require.NotNil(t, sr.Battery)
	assert.Nil(t, sr.Battery.EstimatedCutoff, "no empty-by while charging (inherited [4.4])")
	require.NotNil(t, sr.Rolling15m)
	assert.Nil(t, sr.Rolling15m.EstimatedCutoff, "no rolling empty-by while charging")
}

// TestHandleStatusSimulateOffpeakBoundaryGate verifies the off-peak boundary
// suppression still gates the simulated cutoff: an early-morning light
// discharge whose simulated cutoff lands inside/after the off-peak window is
// suppressed.
func TestHandleStatusSimulateOffpeakBoundaryGate(t *testing.T) {
	now := time.Date(2026, 4, 15, 7, 0, 0, 0, sydneyTZ)
	nowUnix := now.Unix()

	mr := &mockReader{
		queryReadingsFn: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
			// Light discharge: real cutoff is many hours out (after off-peak).
			// A modest +W still leaves the cutoff after 11:00, so it stays
			// suppressed.
			return []dynamo.ReadingItem{
				{Timestamp: nowUnix - 60, Ppv: 0, Pload: 100, Pbat: 100, Pgrid: 0, Soc: 50},
				{Timestamp: nowUnix - 10, Ppv: 0, Pload: 100, Pbat: 100, Pgrid: 0, Soc: 50},
			}, nil
		},
		getSystemFn: func(_ context.Context, serial string) (*dynamo.SystemItem, error) {
			return &dynamo.SystemItem{SysSn: serial, Cobat: 13.34}, nil
		},
	}
	h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), simulateStatusRequest("200"))
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)
	sr := parseStatusResponse(t, resp)
	require.NotNil(t, sr.Battery)
	assert.Nil(t, sr.Battery.EstimatedCutoff,
		"simulated cutoff still suppressed when it lands after the off-peak window")
	require.NotNil(t, sr.Rolling15m)
	assert.Nil(t, sr.Rolling15m.EstimatedCutoff,
		"simulated rolling cutoff still suppressed when after off-peak")
}

// TestHandleStatusSimulateStaleGate verifies that a stale latest reading omits
// Live even under simulation (the liveFresh gate is unchanged), so no
// fabricated simulated values are returned.
func TestHandleStatusSimulateStaleGate(t *testing.T) {
	now := fixedNow()
	nowUnix := now.Unix()
	mr := &mockReader{
		queryReadingsFn: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
			return []dynamo.ReadingItem{
				// 2 hours old -> past the 90s gate.
				{Timestamp: nowUnix - 2*3600, Ppv: 0, Pload: 200, Pbat: 250, Pgrid: 150, Soc: 65},
			}, nil
		},
	}
	h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), simulateStatusRequest("1700"))
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)
	sr := parseStatusResponse(t, resp)
	assert.Nil(t, sr.Live, "live omitted when stale, even under simulation")
	require.NotNil(t, sr.Battery)
	assert.Nil(t, sr.Battery.EstimatedCutoff, "no fabricated cutoff when stale")
	assert.Nil(t, sr.Battery.CantEmptyBeforeOffpeak)
}

// TestHandleStatusSimulateInvalidParam verifies the 400 cases: zero,
// unparseable, negative, over the 20000 cap, whitespace, and float all reject
// with an error and do not return a status body.
func TestHandleStatusSimulateInvalidParam(t *testing.T) {
	now := fixedNow()
	nowUnix := now.Unix()

	cases := map[string]string{
		"zero":        "0",
		"unparseable": "abc",
		"negative":    "-100",
		"over cap":    "20001",
		"float":       "100.5",
	}

	for name, watts := range cases {
		t.Run(name, func(t *testing.T) {
			mr := &mockReader{
				queryReadingsFn: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
					return []dynamo.ReadingItem{
						{Timestamp: nowUnix - 10, Ppv: 0, Pload: 200, Pbat: 1000, Pgrid: 50, Soc: 50},
					}, nil
				},
			}
			h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
			h.nowFunc = func() time.Time { return now }

			resp, err := h.Handle(context.Background(), simulateStatusRequest(watts))
			require.NoError(t, err)
			assert.Equal(t, 400, resp.StatusCode, "watts=%q must reject", watts)

			var body map[string]string
			require.NoError(t, json.Unmarshal([]byte(resp.Body), &body))
			assert.NotEmpty(t, body["error"], "400 must carry an error reason")
		})
	}
}

// TestHandleStatusSimulateBoundaryAccepted verifies the inclusive upper bound
// (20000) and the lowest accepted value (1) are accepted, returning 200.
func TestHandleStatusSimulateBoundaryAccepted(t *testing.T) {
	now := time.Date(2026, 4, 15, 18, 0, 0, 0, sydneyTZ)
	nowUnix := now.Unix()

	for _, watts := range []string{"1", "20000"} {
		t.Run(watts, func(t *testing.T) {
			mr := &mockReader{
				queryReadingsFn: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
					return []dynamo.ReadingItem{
						{Timestamp: nowUnix - 10, Ppv: 0, Pload: 200, Pbat: 1000, Pgrid: 50, Soc: 50},
					}, nil
				},
				getSystemFn: func(_ context.Context, serial string) (*dynamo.SystemItem, error) {
					return &dynamo.SystemItem{SysSn: serial, Cobat: 13.34}, nil
				},
			}
			h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
			h.nowFunc = func() time.Time { return now }

			resp, err := h.Handle(context.Background(), simulateStatusRequest(watts))
			require.NoError(t, err)
			assert.Equal(t, 200, resp.StatusCode, "watts=%s must be accepted", watts)
		})
	}
}

// TestHandleStatusNoParamUnchanged verifies that a /status request with no
// simulateLoadWatts parameter returns the actual values unchanged ([3.3]).
func TestHandleStatusNoParamUnchanged(t *testing.T) {
	now := time.Date(2026, 4, 15, 18, 0, 0, 0, sydneyTZ)
	nowUnix := now.Unix()

	mr := &mockReader{
		queryReadingsFn: func(_ context.Context, _ string, _, _ int64) ([]dynamo.ReadingItem, error) {
			return []dynamo.ReadingItem{
				{Timestamp: nowUnix - 10, Ppv: 0, Pload: 2000, Pbat: 1000, Pgrid: 500, Soc: 60},
			}, nil
		},
		getSystemFn: func(_ context.Context, serial string) (*dynamo.SystemItem, error) {
			return &dynamo.SystemItem{SysSn: serial, Cobat: 13.34}, nil
		},
	}
	h := NewHandler(mr, nil, testSerial, testToken, "11:00", "14:00")
	h.nowFunc = func() time.Time { return now }

	resp, err := h.Handle(context.Background(), simulateStatusRequest(""))
	require.NoError(t, err)
	sr := parseStatusResponse(t, resp)
	require.NotNil(t, sr.Live)
	assert.Equal(t, roundPower(2000), sr.Live.Pload)
	assert.Equal(t, roundPower(1000), sr.Live.Pbat)
	assert.Equal(t, roundPower(500), sr.Live.Pgrid)
}
