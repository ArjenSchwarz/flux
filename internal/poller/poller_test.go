package poller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/ArjenSchwarz/flux/internal/alphaess"
	"github.com/ArjenSchwarz/flux/internal/config"
	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Mock client ---

type mockClient struct {
	lastPowerData    *alphaess.PowerData
	lastPowerErr     error
	oneDayPower      []alphaess.PowerSnapshot
	oneDayPowerErr   error
	oneDateEnergy    *alphaess.EnergyData
	oneDateEnergyErr error
	essList          *alphaess.SystemInfo
	essListErr       error

	// Track calls
	lastPowerCalls     int
	oneDayPowerCalls   int
	oneDateEnergyCalls int
	essListCalls       int
	lastEnergyDate     string
	lastPowerDate      string
}

func (m *mockClient) GetLastPowerData(_ context.Context, _ string) (*alphaess.PowerData, error) {
	m.lastPowerCalls++
	return m.lastPowerData, m.lastPowerErr
}

func (m *mockClient) GetOneDayPower(_ context.Context, _ string, date string) ([]alphaess.PowerSnapshot, error) {
	m.oneDayPowerCalls++
	m.lastPowerDate = date
	return m.oneDayPower, m.oneDayPowerErr
}

func (m *mockClient) GetOneDateEnergy(_ context.Context, _ string, date string) (*alphaess.EnergyData, error) {
	m.oneDateEnergyCalls++
	m.lastEnergyDate = date
	return m.oneDateEnergy, m.oneDateEnergyErr
}

func (m *mockClient) GetEssList(_ context.Context, _ string) (*alphaess.SystemInfo, error) {
	m.essListCalls++
	return m.essList, m.essListErr
}

// --- Mock store ---

type mockStore struct {
	writeReadingErr     error
	writeDailyEnergyErr error
	writeDailyPowerErr  error
	writeSystemErr      error
	writeOffpeakErr     error
	deleteOffpeakErr    error
	getOffpeakResult    *dynamo.OffpeakItem
	getOffpeakErr       error

	readingsWritten    int
	dailyEnergyWritten int
	dailyPowerWritten  int
	systemWritten      int
}

func (m *mockStore) WriteReading(_ context.Context, _ dynamo.ReadingItem) error {
	m.readingsWritten++
	return m.writeReadingErr
}

func (m *mockStore) WriteDailyEnergy(_ context.Context, _ dynamo.DailyEnergyItem) error {
	m.dailyEnergyWritten++
	return m.writeDailyEnergyErr
}

func (m *mockStore) WriteDailyPower(_ context.Context, _ []dynamo.DailyPowerItem) error {
	m.dailyPowerWritten++
	return m.writeDailyPowerErr
}

func (m *mockStore) WriteSystem(_ context.Context, _ dynamo.SystemItem) error {
	m.systemWritten++
	return m.writeSystemErr
}

func (m *mockStore) WriteOffpeak(_ context.Context, _ dynamo.OffpeakItem) error {
	return m.writeOffpeakErr
}

func (m *mockStore) DeleteOffpeak(_ context.Context, _, _ string) error {
	return m.deleteOffpeakErr
}

func (m *mockStore) GetOffpeak(_ context.Context, _, _ string) (*dynamo.OffpeakItem, error) {
	return m.getOffpeakResult, m.getOffpeakErr
}

// --- Helpers ---

func testPoller(client APIClient, store dynamo.Store, opts ...func(*config.Config)) *Poller {
	cfg := &config.Config{
		Serial:   "TEST123",
		Location: time.FixedZone("AEST", 10*60*60),
	}
	for _, o := range opts {
		o(cfg)
	}
	return &Poller{client: client, store: store, cfg: cfg, now: time.Now}
}

func withDryRun(cfg *config.Config) { cfg.DryRun = true }

func captureLog() (*bytes.Buffer, func()) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	old := slog.Default()
	slog.SetDefault(logger)
	return &buf, func() { slog.SetDefault(old) }
}

func logContains(buf *bytes.Buffer, substr string) bool {
	return bytes.Contains(buf.Bytes(), []byte(substr))
}

// --- Tests for fetchAndStoreLiveData ---

func TestFetchAndStoreLiveData_Success(t *testing.T) {
	mc := &mockClient{lastPowerData: &alphaess.PowerData{Ppv: 3.5, Soc: 85.0}}
	ms := &mockStore{}
	p := testPoller(mc, ms)

	p.fetchAndStoreLiveData(context.Background())

	assert.Equal(t, 1, mc.lastPowerCalls)
	assert.Equal(t, 1, ms.readingsWritten)
}

func TestFetchAndStoreLiveData_APIError_LogsAndSkips(t *testing.T) {
	buf, restore := captureLog()
	defer restore()

	mc := &mockClient{lastPowerErr: errors.New("api timeout")}
	ms := &mockStore{}
	p := testPoller(mc, ms)

	p.fetchAndStoreLiveData(context.Background())

	assert.Equal(t, 1, mc.lastPowerCalls)
	assert.Equal(t, 0, ms.readingsWritten)
	assert.True(t, logContains(buf, "api timeout"))
}

func TestFetchAndStoreLiveData_StoreError_LogsAndSkips(t *testing.T) {
	buf, restore := captureLog()
	defer restore()

	mc := &mockClient{lastPowerData: &alphaess.PowerData{Ppv: 1.0}}
	ms := &mockStore{writeReadingErr: errors.New("dynamo error")}
	p := testPoller(mc, ms)

	p.fetchAndStoreLiveData(context.Background())

	assert.Equal(t, 1, mc.lastPowerCalls)
	assert.Equal(t, 1, ms.readingsWritten)
	assert.True(t, logContains(buf, "dynamo error"))
}

func TestFetchAndStoreLiveData_DryRun_LogsPayload(t *testing.T) {
	buf, restore := captureLog()
	defer restore()

	mc := &mockClient{lastPowerData: &alphaess.PowerData{Ppv: 3.5}}
	ms := &mockStore{}
	p := testPoller(mc, ms, withDryRun)

	p.fetchAndStoreLiveData(context.Background())

	assert.True(t, logContains(buf, "dry-run api response"))
	assert.True(t, logContains(buf, "3.5"))
}

// --- Tests for fetchAndStoreDailyPower ---

func TestFetchAndStoreDailyPower_Success(t *testing.T) {
	mc := &mockClient{oneDayPower: []alphaess.PowerSnapshot{{Ppv: 2.0, UploadTime: "2026-04-13 10:00:00"}}}
	ms := &mockStore{}
	p := testPoller(mc, ms)

	p.fetchAndStoreDailyPower(context.Background())

	assert.Equal(t, 1, mc.oneDayPowerCalls)
	assert.Equal(t, 1, ms.dailyPowerWritten)
	// Verify today's date was used (in configured timezone).
	today := time.Now().In(p.cfg.Location).Format("2006-01-02")
	assert.Equal(t, today, mc.lastPowerDate)
}

func TestFetchAndStoreDailyPower_APIError_LogsAndSkips(t *testing.T) {
	buf, restore := captureLog()
	defer restore()

	mc := &mockClient{oneDayPowerErr: errors.New("power api fail")}
	ms := &mockStore{}
	p := testPoller(mc, ms)

	p.fetchAndStoreDailyPower(context.Background())

	assert.Equal(t, 0, ms.dailyPowerWritten)
	assert.True(t, logContains(buf, "power api fail"))
}

func TestFetchAndStoreDailyPower_StoreError_LogsAndSkips(t *testing.T) {
	buf, restore := captureLog()
	defer restore()

	mc := &mockClient{oneDayPower: []alphaess.PowerSnapshot{{Ppv: 1.0}}}
	ms := &mockStore{writeDailyPowerErr: errors.New("batch write fail")}
	p := testPoller(mc, ms)

	p.fetchAndStoreDailyPower(context.Background())

	assert.True(t, logContains(buf, "batch write fail"))
}

func TestFetchAndStoreDailyPower_DryRun_LogsPayload(t *testing.T) {
	buf, restore := captureLog()
	defer restore()

	mc := &mockClient{oneDayPower: []alphaess.PowerSnapshot{{Ppv: 2.0}}}
	ms := &mockStore{}
	p := testPoller(mc, ms, withDryRun)

	p.fetchAndStoreDailyPower(context.Background())

	assert.True(t, logContains(buf, "dry-run api response"))
}

// --- Tests for fetchAndStoreDailyEnergy ---

func TestFetchAndStoreDailyEnergy_Success(t *testing.T) {
	mc := &mockClient{oneDateEnergy: &alphaess.EnergyData{Epv: 12.5}}
	ms := &mockStore{}
	p := testPoller(mc, ms)

	p.fetchAndStoreDailyEnergy(context.Background(), "")

	assert.Equal(t, 1, mc.oneDateEnergyCalls)
	assert.Equal(t, 1, ms.dailyEnergyWritten)
	// When date is empty, should use today.
	today := time.Now().In(p.cfg.Location).Format("2006-01-02")
	assert.Equal(t, today, mc.lastEnergyDate)
}

func TestFetchAndStoreDailyEnergy_ExplicitDate(t *testing.T) {
	mc := &mockClient{oneDateEnergy: &alphaess.EnergyData{Epv: 5.0}}
	ms := &mockStore{}
	p := testPoller(mc, ms)

	p.fetchAndStoreDailyEnergy(context.Background(), "2026-04-12")

	assert.Equal(t, "2026-04-12", mc.lastEnergyDate)
	assert.Equal(t, 1, ms.dailyEnergyWritten)
}

func TestFetchAndStoreDailyEnergy_APIError_LogsAndSkips(t *testing.T) {
	buf, restore := captureLog()
	defer restore()

	mc := &mockClient{oneDateEnergyErr: errors.New("energy api fail")}
	ms := &mockStore{}
	p := testPoller(mc, ms)

	p.fetchAndStoreDailyEnergy(context.Background(), "")

	assert.Equal(t, 0, ms.dailyEnergyWritten)
	assert.True(t, logContains(buf, "energy api fail"))
}

func TestFetchAndStoreDailyEnergy_StoreError_LogsAndSkips(t *testing.T) {
	buf, restore := captureLog()
	defer restore()

	mc := &mockClient{oneDateEnergy: &alphaess.EnergyData{Epv: 1.0}}
	ms := &mockStore{writeDailyEnergyErr: errors.New("put item fail")}
	p := testPoller(mc, ms)

	p.fetchAndStoreDailyEnergy(context.Background(), "")

	assert.True(t, logContains(buf, "put item fail"))
}

func TestFetchAndStoreDailyEnergy_DryRun_LogsPayload(t *testing.T) {
	buf, restore := captureLog()
	defer restore()

	mc := &mockClient{oneDateEnergy: &alphaess.EnergyData{Epv: 12.5}}
	ms := &mockStore{}
	p := testPoller(mc, ms, withDryRun)

	p.fetchAndStoreDailyEnergy(context.Background(), "")

	assert.True(t, logContains(buf, "dry-run api response"))
	assert.True(t, logContains(buf, "12.5"))
}

// T-841 regression: AlphaESS returns all-zero values for "yesterday" during the
// day-finalisation window. Writing that response overwrites real running totals
// accumulated by the hourly poll.
func TestFetchAndStoreDailyEnergy_AllZero_SkipsWrite(t *testing.T) {
	buf, restore := captureLog()
	defer restore()

	mc := &mockClient{oneDateEnergy: &alphaess.EnergyData{}}
	ms := &mockStore{}
	p := testPoller(mc, ms)

	p.fetchAndStoreDailyEnergy(context.Background(), "2026-04-17")

	assert.Equal(t, 1, mc.oneDateEnergyCalls)
	assert.Equal(t, 0, ms.dailyEnergyWritten, "must not overwrite existing row with zeros")
	assert.True(t, logContains(buf, "2026-04-17"))
	assert.True(t, logContains(buf, "all-zero"))
}

// Defensive: GetOneDateEnergy can't currently return (nil, nil), but the
// poller shouldn't panic if a future refactor changes that.
func TestFetchAndStoreDailyEnergy_NilData_SkipsWrite(t *testing.T) {
	buf, restore := captureLog()
	defer restore()

	mc := &mockClient{oneDateEnergy: nil}
	ms := &mockStore{}
	p := testPoller(mc, ms)

	p.fetchAndStoreDailyEnergy(context.Background(), "2026-04-17")

	assert.Equal(t, 0, ms.dailyEnergyWritten)
	assert.True(t, logContains(buf, "nil or all-zero"))
}

func TestFetchAndStoreDailyEnergy_PartialZero_StillWrites(t *testing.T) {
	mc := &mockClient{oneDateEnergy: &alphaess.EnergyData{Epv: 0, EInput: 0, EOutput: 0, ECharge: 0.01, EDischarge: 0}}
	ms := &mockStore{}
	p := testPoller(mc, ms)

	p.fetchAndStoreDailyEnergy(context.Background(), "")

	assert.Equal(t, 1, ms.dailyEnergyWritten, "any non-zero field means AlphaESS has data")
}

// --- Tests for fetchAndStoreSystemInfo ---

func TestFetchAndStoreSystemInfo_Success(t *testing.T) {
	mc := &mockClient{essList: &alphaess.SystemInfo{SysSn: "TEST123", Cobat: 10.0}}
	ms := &mockStore{}
	p := testPoller(mc, ms)

	p.fetchAndStoreSystemInfo(context.Background())

	assert.Equal(t, 1, mc.essListCalls)
	assert.Equal(t, 1, ms.systemWritten)
}

func TestFetchAndStoreSystemInfo_APIError_LogsAndSkips(t *testing.T) {
	buf, restore := captureLog()
	defer restore()

	mc := &mockClient{essListErr: errors.New("ess list fail")}
	ms := &mockStore{}
	p := testPoller(mc, ms)

	p.fetchAndStoreSystemInfo(context.Background())

	assert.Equal(t, 0, ms.systemWritten)
	assert.True(t, logContains(buf, "ess list fail"))
}

func TestFetchAndStoreSystemInfo_StoreError_LogsAndSkips(t *testing.T) {
	buf, restore := captureLog()
	defer restore()

	mc := &mockClient{essList: &alphaess.SystemInfo{SysSn: "TEST123"}}
	ms := &mockStore{writeSystemErr: errors.New("system write fail")}
	p := testPoller(mc, ms)

	p.fetchAndStoreSystemInfo(context.Background())

	assert.True(t, logContains(buf, "system write fail"))
}

func TestFetchAndStoreSystemInfo_DryRun_LogsPayload(t *testing.T) {
	buf, restore := captureLog()
	defer restore()

	mc := &mockClient{essList: &alphaess.SystemInfo{SysSn: "TEST123", Cobat: 10.0}}
	ms := &mockStore{}
	p := testPoller(mc, ms, withDryRun)

	p.fetchAndStoreSystemInfo(context.Background())

	assert.True(t, logContains(buf, "dry-run api response"))
	assert.True(t, logContains(buf, "TEST123"))
}

// --- Tests for nextLocalMidnight (Task 16) ---

func TestNextLocalMidnight(t *testing.T) {
	aest := time.FixedZone("AEST", 10*60*60)

	tests := map[string]struct {
		now  time.Time
		want time.Time
	}{
		"afternoon returns next midnight": {
			now:  time.Date(2026, 4, 13, 15, 30, 0, 0, aest),
			want: time.Date(2026, 4, 14, 0, 0, 0, 0, aest),
		},
		"just before midnight": {
			now:  time.Date(2026, 4, 13, 23, 59, 59, 0, aest),
			want: time.Date(2026, 4, 14, 0, 0, 0, 0, aest),
		},
		"exactly midnight returns next midnight": {
			now:  time.Date(2026, 4, 14, 0, 0, 0, 0, aest),
			want: time.Date(2026, 4, 15, 0, 0, 0, 0, aest),
		},
		"early morning": {
			now:  time.Date(2026, 4, 13, 0, 5, 0, 0, aest),
			want: time.Date(2026, 4, 14, 0, 0, 0, 0, aest),
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := nextLocalMidnight(tc.now, aest)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestNextLocalMidnight_DST(t *testing.T) {
	// Australia/Sydney: AEDT→AEST first Sunday of April (clocks go back 1h at 3am)
	sydney, err := time.LoadLocation("Australia/Sydney")
	require.NoError(t, err)

	// 2026-04-05 is the first Sunday of April 2026 (DST transition day).
	// Before transition (still AEDT, UTC+11).
	now := time.Date(2026, 4, 4, 22, 0, 0, 0, sydney)
	got := nextLocalMidnight(now, sydney)

	// Should be midnight on April 5 in Sydney.
	assert.Equal(t, 2026, got.Year())
	assert.Equal(t, time.April, got.Month())
	assert.Equal(t, 5, got.Day())
	assert.Equal(t, 0, got.Hour())
	assert.Equal(t, 0, got.Minute())
}

func TestFetchAndStoreDailyEnergy_MidnightFinalizer_UsesYesterday(t *testing.T) {
	mc := &mockClient{oneDateEnergy: &alphaess.EnergyData{Epv: 20.0}}
	ms := &mockStore{}
	p := testPoller(mc, ms)

	yesterday := time.Now().In(p.cfg.Location).AddDate(0, 0, -1).Format("2006-01-02")
	p.fetchAndStoreDailyEnergy(context.Background(), yesterday)

	assert.Equal(t, yesterday, mc.lastEnergyDate)
	assert.Equal(t, 1, ms.dailyEnergyWritten)
}

// --- Test dry-run payload contains JSON ---

func TestDryRunPayload_ContainsValidJSON(t *testing.T) {
	buf, restore := captureLog()
	defer restore()

	mc := &mockClient{lastPowerData: &alphaess.PowerData{Ppv: 3.5, Soc: 85.0}}
	ms := &mockStore{}
	p := testPoller(mc, ms, withDryRun)

	p.fetchAndStoreLiveData(context.Background())

	// Find the dry-run log line and verify it contains valid JSON payload.
	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	found := false
	for _, line := range lines {
		var entry map[string]any
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		if msg, ok := entry["msg"].(string); ok && msg == "dry-run api response" {
			assert.NotNil(t, entry["payload"])
			found = true
			break
		}
	}
	assert.True(t, found, "expected dry-run api response log entry")
}
