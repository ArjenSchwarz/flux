// Package dynamo provides DynamoDB storage for the Flux poller.
package dynamo

import "context"

// Store defines the write operations for persisting poller data.
// Two implementations exist: DynamoStore (production) and LogStore (dry-run).
type Store interface {
	WriteReading(ctx context.Context, item ReadingItem) error
	WriteDailyEnergy(ctx context.Context, item DailyEnergyItem) error
	WriteDailyPower(ctx context.Context, items []DailyPowerItem) error
	WriteSystem(ctx context.Context, item SystemItem) error
	WriteOffpeak(ctx context.Context, item OffpeakItem) error
	DeleteOffpeak(ctx context.Context, serial, date string) error
	GetOffpeak(ctx context.Context, serial, date string) (*OffpeakItem, error)

	// daily-derived-stats spec — summarisation-pass write path.
	GetDailyEnergy(ctx context.Context, serial, date string) (*DailyEnergyItem, error)
	UpdateDailyEnergyDerived(ctx context.Context, serial, date string, stats DerivedStats) error
	// QueryReadings is needed by the poller summarisation pass to fetch the
	// day's readings. The Lambda Reader interface already exposes the same
	// method against a separate ReadAPI client; the poller's Store interface
	// gets it here so the pass can reuse the existing DynamoDB client.
	QueryReadings(ctx context.Context, serial string, from, to int64) ([]ReadingItem, error)
}

// TableNames holds the DynamoDB table names, loaded from environment variables.
type TableNames struct {
	Readings    string
	DailyEnergy string
	DailyPower  string
	System      string
	Offpeak     string
	Notes       string
}
