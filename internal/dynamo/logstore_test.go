package dynamo

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestLogStore creates a LogStore that writes to a buffer for assertion.
func newTestLogStore(t *testing.T) (*LogStore, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	return NewLogStore(logger), &buf
}

// logEntry parses the last JSON log line from the buffer.
func logEntry(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	require.NotEmpty(t, lines)
	var entry map[string]any
	require.NoError(t, json.Unmarshal(lines[len(lines)-1], &entry))
	return entry
}

func TestLogStore_WriteReading(t *testing.T) {
	store, buf := newTestLogStore(t)

	err := store.WriteReading(context.Background(), ReadingItem{
		SysSn: "AB1234", Timestamp: 1000, Ppv: 3.5, Soc: 85.0,
	})
	require.NoError(t, err)

	entry := logEntry(t, buf)
	assert.Equal(t, "dry-run write", entry["msg"])
	assert.Equal(t, "flux-readings", entry["table"])
	assert.NotNil(t, entry["item"])
}

func TestLogStore_WriteDailyEnergy(t *testing.T) {
	store, buf := newTestLogStore(t)

	err := store.WriteDailyEnergy(context.Background(), DailyEnergyItem{
		SysSn: "AB1234", Date: "2026-04-13", Epv: 12.5,
	})
	require.NoError(t, err)

	entry := logEntry(t, buf)
	assert.Equal(t, "dry-run write", entry["msg"])
	assert.Equal(t, "flux-daily-energy", entry["table"])
}

func TestLogStore_WriteDailyPower(t *testing.T) {
	store, buf := newTestLogStore(t)

	err := store.WriteDailyPower(context.Background(), []DailyPowerItem{
		{SysSn: "AB1234", UploadTime: "2026-04-13 10:00:00"},
		{SysSn: "AB1234", UploadTime: "2026-04-13 10:05:00"},
	})
	require.NoError(t, err)

	entry := logEntry(t, buf)
	assert.Equal(t, "dry-run write", entry["msg"])
	assert.Equal(t, "flux-daily-power", entry["table"])
	// Should log the count of items.
	assert.Equal(t, float64(2), entry["count"])
}

func TestLogStore_WriteSystem(t *testing.T) {
	store, buf := newTestLogStore(t)

	err := store.WriteSystem(context.Background(), SystemItem{SysSn: "AB1234", Cobat: 10.0})
	require.NoError(t, err)

	entry := logEntry(t, buf)
	assert.Equal(t, "dry-run write", entry["msg"])
	assert.Equal(t, "flux-system", entry["table"])
}

func TestLogStore_WriteOffpeak(t *testing.T) {
	store, buf := newTestLogStore(t)

	err := store.WriteOffpeak(context.Background(), OffpeakItem{
		SysSn: "AB1234", Date: "2026-04-13", Status: "pending",
	})
	require.NoError(t, err)

	entry := logEntry(t, buf)
	assert.Equal(t, "dry-run write", entry["msg"])
	assert.Equal(t, "flux-offpeak", entry["table"])
}

func TestLogStore_DeleteOffpeak(t *testing.T) {
	store, buf := newTestLogStore(t)

	err := store.DeleteOffpeak(context.Background(), "AB1234", "2026-04-13")
	require.NoError(t, err)

	entry := logEntry(t, buf)
	assert.Equal(t, "dry-run delete", entry["msg"])
	assert.Equal(t, "flux-offpeak", entry["table"])
	assert.Equal(t, "AB1234", entry["sysSn"])
	assert.Equal(t, "2026-04-13", entry["date"])
}

func TestLogStore_GetOffpeak_ReturnsNil(t *testing.T) {
	store, _ := newTestLogStore(t)

	got, err := store.GetOffpeak(context.Background(), "AB1234", "2026-04-13")
	require.NoError(t, err)
	assert.Nil(t, got)
}
