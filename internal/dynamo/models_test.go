package dynamo

import (
	"testing"
	"time"

	"github.com/ArjenSchwarz/flux/internal/alphaess"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewReadingItem(t *testing.T) {
	now := time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)

	tests := map[string]struct {
		serial string
		data   *alphaess.PowerData
		want   ReadingItem
	}{
		"maps all fields with TTL": {
			serial: "AB1234",
			data:   &alphaess.PowerData{Ppv: 3.5, Pload: 1.2, Pbat: -0.5, Pgrid: 0.3, Soc: 85.0},
			want: ReadingItem{
				SysSn: "AB1234", Timestamp: now.Unix(),
				Ppv: 3.5, Pload: 1.2, Pbat: -0.5, Pgrid: 0.3, Soc: 85.0,
				TTL: now.Add(30 * 24 * time.Hour).Unix(),
			},
		},
		"zero values": {
			serial: "SN0",
			data:   &alphaess.PowerData{},
			want: ReadingItem{
				SysSn: "SN0", Timestamp: now.Unix(),
				TTL: now.Add(30 * 24 * time.Hour).Unix(),
			},
		},
		"negative power values": {
			serial: "SN1",
			data:   &alphaess.PowerData{Pbat: -2.5, Pgrid: -1.0},
			want: ReadingItem{
				SysSn: "SN1", Timestamp: now.Unix(),
				Pbat: -2.5, Pgrid: -1.0,
				TTL: now.Add(30 * 24 * time.Hour).Unix(),
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := NewReadingItem(tc.serial, tc.data, now)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestNewReadingItemFromSnapshot(t *testing.T) {
	loc, err := time.LoadLocation("Australia/Sydney")
	if err != nil {
		t.Fatalf("load Sydney: %v", err)
	}
	now := time.Date(2026, 5, 19, 9, 0, 0, 0, time.UTC)

	tests := map[string]struct {
		serial  string
		snap    alphaess.PowerSnapshot
		wantTS  int64
		wantPb  float64
		wantPg  float64
		wantSoc float64
		wantErr bool
	}{
		"importing — gridCharge > feedIn, ppv < load": {
			serial: "SN1",
			snap: alphaess.PowerSnapshot{
				Cbat: 72.0, Ppv: 100, Load: 400, FeedIn: 0, GridCharge: 50,
				UploadTime: "2026-05-18 23:30:00",
			},
			// pgrid = 50 - 0 = 50 (import); pbat = 400 - 100 - 50 = 250 (discharge)
			wantTS:  time.Date(2026, 5, 18, 23, 30, 0, 0, loc).Unix(),
			wantPb:  250,
			wantPg:  50,
			wantSoc: 72.0,
		},
		"exporting — feedIn > gridCharge": {
			serial: "SN1",
			snap: alphaess.PowerSnapshot{
				Cbat: 95.0, Ppv: 3000, Load: 500, FeedIn: 2000, GridCharge: 0,
				UploadTime: "2026-05-18 13:00:00",
			},
			// pgrid = 0 - 2000 = -2000 (export); pbat = 500 - 3000 - (-2000) = -500 (charge)
			wantTS:  time.Date(2026, 5, 18, 13, 0, 0, 0, loc).Unix(),
			wantPb:  -500,
			wantPg:  -2000,
			wantSoc: 95.0,
		},
		"invalid uploadTime returns error": {
			serial:  "SN1",
			snap:    alphaess.PowerSnapshot{UploadTime: "not-a-time"},
			wantErr: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := NewReadingItemFromSnapshot(tc.serial, tc.snap, loc, now)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.serial, got.SysSn)
			assert.Equal(t, tc.wantTS, got.Timestamp)
			assert.Equal(t, tc.wantPb, got.Pbat)
			assert.Equal(t, tc.wantPg, got.Pgrid)
			assert.Equal(t, tc.wantSoc, got.Soc)
			// TTL must be derived from `now`, not from the snapshot — backfilling
			// older snapshots shouldn't write rows that expire before they're
			// useful.
			assert.Equal(t, now.Add(30*24*time.Hour).Unix(), got.TTL)
		})
	}
}

func TestIsAllZeroReading(t *testing.T) {
	tests := map[string]struct {
		r    ReadingItem
		want bool
	}{
		"every field zero":             {r: ReadingItem{SysSn: "SN", Timestamp: 1}, want: true},
		"non-zero SoC alone":           {r: ReadingItem{Soc: 72.0}, want: false},
		"non-zero ppv alone":           {r: ReadingItem{Ppv: 0.001}, want: false},
		"negative pgrid (exporting)":   {r: ReadingItem{Pgrid: -100}, want: false},
		"sysSn/timestamp do not count": {r: ReadingItem{SysSn: "AB1234", Timestamp: 123456789, TTL: 999}, want: true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, IsAllZeroReading(tc.r))
		})
	}
}

func TestNewDailyEnergyItem(t *testing.T) {
	tests := map[string]struct {
		serial string
		date   string
		data   *alphaess.EnergyData
		want   DailyEnergyItem
	}{
		"maps all fields": {
			serial: "AB1234",
			date:   "2026-04-13",
			data:   &alphaess.EnergyData{Epv: 12.5, EInput: 3.0, EOutput: 1.5, ECharge: 5.0, EDischarge: 2.0, EGridCharge: 0.5},
			want: DailyEnergyItem{
				SysSn: "AB1234", Date: "2026-04-13",
				Epv: 12.5, EInput: 3.0, EOutput: 1.5, ECharge: 5.0, EDischarge: 2.0, EGridCharge: 0.5,
			},
		},
		"zero values": {
			serial: "SN0",
			date:   "2026-01-01",
			data:   &alphaess.EnergyData{},
			want:   DailyEnergyItem{SysSn: "SN0", Date: "2026-01-01"},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := NewDailyEnergyItem(tc.serial, tc.date, tc.data)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestNewDailyPowerItems(t *testing.T) {
	tests := map[string]struct {
		serial    string
		snapshots []alphaess.PowerSnapshot
		want      []DailyPowerItem
	}{
		"maps multiple snapshots": {
			serial: "AB1234",
			snapshots: []alphaess.PowerSnapshot{
				{Cbat: 80.0, Ppv: 2.0, Load: 1.0, FeedIn: 0.5, GridCharge: 0.0, UploadTime: "2026-04-13 10:00:00"},
				{Cbat: 82.0, Ppv: 2.5, Load: 1.1, FeedIn: 0.6, GridCharge: 0.1, UploadTime: "2026-04-13 10:05:00"},
			},
			want: []DailyPowerItem{
				{SysSn: "AB1234", UploadTime: "2026-04-13 10:00:00", Cbat: 80.0, Ppv: 2.0, Load: 1.0, FeedIn: 0.5},
				{SysSn: "AB1234", UploadTime: "2026-04-13 10:05:00", Cbat: 82.0, Ppv: 2.5, Load: 1.1, FeedIn: 0.6, GridCharge: 0.1},
			},
		},
		"empty snapshots": {
			serial:    "SN0",
			snapshots: []alphaess.PowerSnapshot{},
			want:      []DailyPowerItem{},
		},
		"negative values": {
			serial: "SN1",
			snapshots: []alphaess.PowerSnapshot{
				{Cbat: -1.0, Ppv: 0, Load: 0, FeedIn: -0.5, UploadTime: "2026-04-13 00:00:00"},
			},
			want: []DailyPowerItem{
				{SysSn: "SN1", UploadTime: "2026-04-13 00:00:00", Cbat: -1.0, FeedIn: -0.5},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := NewDailyPowerItems(tc.serial, tc.snapshots)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestNewSystemItem(t *testing.T) {
	now := time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)

	tests := map[string]struct {
		info *alphaess.SystemInfo
		want SystemItem
	}{
		"maps all fields": {
			info: &alphaess.SystemInfo{
				SysSn: "AB1234", Cobat: 10.0, Mbat: "bat-model",
				Minv: "inv-model", Popv: 5.0, Poinv: 5.0, EmsStatus: "Normal",
			},
			want: SystemItem{
				SysSn: "AB1234", Cobat: 10.0, Mbat: "bat-model",
				Minv: "inv-model", Popv: 5.0, Poinv: 5.0, EmsStatus: "Normal",
				LastUpdated: "2026-04-13T12:00:00Z",
			},
		},
		"zero values": {
			info: &alphaess.SystemInfo{SysSn: "SN0"},
			want: SystemItem{SysSn: "SN0", LastUpdated: "2026-04-13T12:00:00Z"},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := NewSystemItem(tc.info, now)
			assert.Equal(t, tc.want, got)
		})
	}
}
