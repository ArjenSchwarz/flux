package alphaess

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// DerivePower must agree with the live-reading sign convention used by
// computeTodayEnergy and the iOS BatteryPowerChartView: pgrid > 0 means
// importing from the grid, pbat > 0 means the battery is discharging. The
// helper exists so the past-date Day Detail fallback (mapDailyPowerToPoints)
// and the readings backfill (NewReadingItemFromSnapshot) cannot disagree.
func TestDerivePower(t *testing.T) {
	tests := map[string]struct {
		load, ppv, gridCharge, feedIn float64
		wantPgrid, wantPbat           float64
	}{
		"importing — gridCharge > feedIn, ppv < load": {
			load: 400, ppv: 100, gridCharge: 50, feedIn: 0,
			wantPgrid: 50,  // 50 - 0
			wantPbat:  250, // 400 - 100 - 50 (discharging to cover the remaining load)
		},
		"exporting — feedIn > gridCharge, ppv > load": {
			load: 500, ppv: 3000, gridCharge: 0, feedIn: 2000,
			wantPgrid: -2000, // 0 - 2000 (export)
			wantPbat:  -500,  // 500 - 3000 - (-2000) (charging from excess solar)
		},
		"idle — every field zero": {
			load: 0, ppv: 0, gridCharge: 0, feedIn: 0,
			wantPgrid: 0,
			wantPbat:  0,
		},
		"solar covers load exactly": {
			load: 1000, ppv: 1000, gridCharge: 0, feedIn: 0,
			wantPgrid: 0,
			wantPbat:  0,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			pgrid, pbat := DerivePower(tc.load, tc.ppv, tc.gridCharge, tc.feedIn)
			assert.Equal(t, tc.wantPgrid, pgrid)
			assert.Equal(t, tc.wantPbat, pbat)
		})
	}
}
