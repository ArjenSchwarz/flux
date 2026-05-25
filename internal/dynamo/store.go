// Package dynamo provides DynamoDB storage for the Flux poller.
package dynamo

import (
	"context"
	"errors"
)

// ErrOffpeakConditionFailed is returned by WriteOffpeakIfPendingOrAbsent
// and WriteOffpeakIfComplete when DynamoDB rejects the put because the
// existing row violates the conditional expression. Callers log-and-skip:
// the other writer (poller or backfill CLI) got there first and its row is
// the surviving authority. See specs/offpeak-from-readings/design.md
// "Concurrent writer guard" for the race scenarios.
var ErrOffpeakConditionFailed = errors.New("offpeak conditional write failed: row in incompatible state")

// Store defines the write operations for persisting poller data.
// Two implementations exist: DynamoStore (production) and LogStore (dry-run).
type Store interface {
	WriteReading(ctx context.Context, item ReadingItem) error
	WriteDailyEnergy(ctx context.Context, item DailyEnergyItem) error
	WriteDailyPower(ctx context.Context, items []DailyPowerItem) error
	WriteSystem(ctx context.Context, item SystemItem) error
	WriteOffpeak(ctx context.Context, item OffpeakItem) error
	// WriteOffpeakIfPendingOrAbsent is the poller's window-end finalisation
	// write (AC 3.5). Fails with ErrOffpeakConditionFailed when the row
	// already has status="complete" (peer poller or backfill CLI won the
	// race).
	WriteOffpeakIfPendingOrAbsent(ctx context.Context, item OffpeakItem) error
	// WriteOffpeakIfComplete is the backfill CLI's write (AC 7.8). Fails
	// with ErrOffpeakConditionFailed when the row is absent or has
	// status="pending" — protects against overwriting rows mid-poll.
	WriteOffpeakIfComplete(ctx context.Context, item OffpeakItem) error
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
	// QueryReadingsConsistent is the same query with ConsistentRead=true,
	// used by the poller's off-peak window-end pass after waiting for an
	// at-or-after-boundary reading to land (specs/offpeak-from-readings AC
	// 3.5). Kept as a sibling rather than an opts param so existing callers
	// stay on the eventually-consistent path unchanged. The API Lambda's
	// Reader interface deliberately does NOT expose this method.
	QueryReadingsConsistent(ctx context.Context, serial string, from, to int64) ([]ReadingItem, error)
}

// TableNames holds the DynamoDB table names, loaded from environment variables.
type TableNames struct {
	Readings     string
	DailyEnergy  string
	DailyPower   string
	System       string
	Offpeak      string
	Notes        string
	Devices      string
	SocRules     string
	SocFireState string
}
