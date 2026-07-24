package poller

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/ArjenSchwarz/flux/internal/plan"
)

const (
	// planLoadAttempts bounds the cold-start retry sequence. With
	// planLoadBaseDelay doubling each round it spans roughly 30 s, long enough
	// to ride out a DynamoDB blip or an IAM propagation delay at container
	// start without pinning a goroutine indefinitely.
	planLoadAttempts = 5

	// planLoadBaseDelay is the first cold-start backoff step.
	planLoadBaseDelay = 2 * time.Second
)

// PlanLister is the pricing read surface the poller needs. It is declared
// here, not imported from the API layer, so the poller depends only on the
// one method it uses — the Lambda keeps sole write access to the table.
type PlanLister interface {
	ListPricing(ctx context.Context) ([]dynamo.PricingItem, error)
}

// PlanSource serves the plan set that drives every window-dependent poller
// behaviour, reading through to the pricing table and caching the last good
// result.
//
// The cache exists to satisfy AC 4.6: a failure to read plan data must never
// be treated as "no plan", because that would silently strip a day of its
// free window and its band split. So a read failure with a warm cache is
// logged and served from the cache, and only a cold start with no cache at
// all can fail — after retrying with backoff (Q14).
//
// Plans is safe for concurrent use: the off-peak scheduler and the
// summarisation pass run in separate goroutines and share one source.
type PlanSource struct {
	lister PlanLister

	mu       sync.RWMutex
	cached   []plan.Plan
	hasCache bool

	// retryDelay is the first cold-start backoff step, overridable so tests
	// don't pay the production wait.
	retryDelay time.Duration
}

// NewPlanSource returns a PlanSource reading through the given lister.
func NewPlanSource(lister PlanLister) *PlanSource {
	return &PlanSource{lister: lister, retryDelay: planLoadBaseDelay}
}

// Plans returns the current plan set. Each call reads through to the store so
// callers see plan edits without waiting for a cache to expire; the read is
// cheap (a handful of rows) and both callers run at most hourly.
//
// A failed read falls back to the last good result. With no cache yet the
// read is retried with backoff, and only an exhausted retry sequence — or a
// cancelled context — returns an error. An empty table is a success, not a
// failure: "no plans configured" is a real state and caches like any other.
func (s *PlanSource) Plans(ctx context.Context) ([]plan.Plan, error) {
	var lastErr error
	for attempt := range planLoadAttempts {
		plans, err := s.load(ctx)
		if err == nil {
			return plans, nil
		}
		lastErr = err

		if cached, ok := s.snapshot(); ok {
			// A usable answer already exists, so blocking the caller through a
			// backoff sequence buys nothing.
			slog.Warn("pricing read failed; serving last-good plan cache",
				"error", err, "plans", len(cached))
			return cached, nil
		}

		if attempt == planLoadAttempts-1 {
			break
		}
		delay := s.retryDelay << attempt
		slog.Warn("pricing read failed with no cached plans; retrying",
			"error", err, "attempt", attempt+1, "retryIn", delay)
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("load pricing plans: %w", ctx.Err())
		case <-time.After(delay):
		}
	}
	return nil, fmt.Errorf("load pricing plans after %d attempts: %w", planLoadAttempts, lastErr)
}

// load performs one read and, on success, replaces the cache.
func (s *PlanSource) load(ctx context.Context) ([]plan.Plan, error) {
	rows, err := s.lister.ListPricing(ctx)
	if err != nil {
		return nil, fmt.Errorf("list pricing: %w", err)
	}
	plans := dynamo.PlansFromItems(rows)

	s.mu.Lock()
	s.cached = plans
	s.hasCache = true
	s.mu.Unlock()

	return plans, nil
}

// snapshot returns the cached plan set and whether one has ever been loaded.
func (s *PlanSource) snapshot() ([]plan.Plan, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cached, s.hasCache
}
