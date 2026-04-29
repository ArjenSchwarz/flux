package dynamo

import (
	"context"
	"encoding/json"
	"log/slog"
)

// LogStore is a dry-run Store implementation that logs what would be written.
type LogStore struct {
	logger *slog.Logger
}

// NewLogStore creates a LogStore that logs operations to the given logger.
func NewLogStore(logger *slog.Logger) *LogStore {
	return &LogStore{logger: logger}
}

func (s *LogStore) WriteReading(_ context.Context, item ReadingItem) error {
	s.logger.Info("dry-run write", "table", "flux-readings", "item", jsonAttr(item))
	return nil
}

func (s *LogStore) WriteDailyEnergy(_ context.Context, item DailyEnergyItem) error {
	s.logger.Info("dry-run write", "table", "flux-daily-energy", "item", jsonAttr(item))
	return nil
}

func (s *LogStore) WriteDailyPower(_ context.Context, items []DailyPowerItem) error {
	s.logger.Info("dry-run write", "table", "flux-daily-power", "count", len(items), "items", jsonAttr(items))
	return nil
}

func (s *LogStore) WriteSystem(_ context.Context, item SystemItem) error {
	s.logger.Info("dry-run write", "table", "flux-system", "item", jsonAttr(item))
	return nil
}

func (s *LogStore) WriteOffpeak(_ context.Context, item OffpeakItem) error {
	s.logger.Info("dry-run write", "table", "flux-offpeak", "item", jsonAttr(item))
	return nil
}

func (s *LogStore) DeleteOffpeak(_ context.Context, serial, date string) error {
	s.logger.Info("dry-run delete", "table", "flux-offpeak", "sysSn", serial, "date", date)
	return nil
}

// GetOffpeak returns nil in dry-run mode — no existing record to recover from.
func (s *LogStore) GetOffpeak(_ context.Context, _, _ string) (*OffpeakItem, error) {
	return nil, nil
}

// GetDailyEnergy returns nil in dry-run mode — no existing row to recover from.
func (s *LogStore) GetDailyEnergy(_ context.Context, _, _ string) (*DailyEnergyItem, error) {
	return nil, nil
}

// UpdateDailyEnergyDerived logs the would-be summarisation write.
func (s *LogStore) UpdateDailyEnergyDerived(_ context.Context, serial, date string, stats DerivedStats) error {
	s.logger.Info("dry-run derived stats update", "table", "flux-daily-energy", "sysSn", serial, "date", date, "stats", jsonAttr(stats))
	return nil
}

// QueryReadings returns an empty slice in dry-run mode — there are no
// readings to summarise without a real DynamoDB.
func (s *LogStore) QueryReadings(_ context.Context, _ string, _, _ int64) ([]ReadingItem, error) {
	return []ReadingItem{}, nil
}

// jsonAttr serializes a value to a JSON string for structured logging.
func jsonAttr(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "<marshal error>"
	}
	return string(b)
}
