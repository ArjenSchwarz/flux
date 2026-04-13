package dynamo

import (
	"testing"
	"time"

	"github.com/ArjenSchwarz/flux/internal/alphaess"
	"github.com/stretchr/testify/assert"
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
	now := time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)
	ttl := now.Add(30 * 24 * time.Hour).Unix()

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
				{SysSn: "AB1234", UploadTime: "2026-04-13 10:00:00", Cbat: 80.0, Ppv: 2.0, Load: 1.0, FeedIn: 0.5, TTL: ttl},
				{SysSn: "AB1234", UploadTime: "2026-04-13 10:05:00", Cbat: 82.0, Ppv: 2.5, Load: 1.1, FeedIn: 0.6, GridCharge: 0.1, TTL: ttl},
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
				{SysSn: "SN1", UploadTime: "2026-04-13 00:00:00", Cbat: -1.0, FeedIn: -0.5, TTL: ttl},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := NewDailyPowerItems(tc.serial, tc.snapshots, now)
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
