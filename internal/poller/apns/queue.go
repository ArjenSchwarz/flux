package apns

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
)

// ErrQueueFull is returned by Enqueue when the buffered channel is at its
// declared capacity. Callers (the evaluator) log and continue: the fire-state
// row is already written, so the rule does not re-fire today.
var ErrQueueFull = errors.New("apns: push queue full")

// Job is what the evaluator enqueues. Workers translate it to a Notifier.Push
// call and handle stale-token bookkeeping. PushJob lives in the eval package;
// the Adapter (defined here) converts between them so neither package
// imports the other.
type Job struct {
	DeviceID   string
	RuleID     string
	Token      string
	CollapseID string
	Payload    Payload
}

// StaleTokenSink is the side-effect interface used when APNs reports a
// stale token. The poller-side wiring passes a *dynamo.DynamoDeviceWriter
// here (its MarkStale method); the tests pass a counting double.
type StaleTokenSink interface {
	MarkStale(ctx context.Context, deviceID string) error
}

// QueueConfig parameterises Queue construction. All four fields are
// required; misconfiguration is caller error and Start will panic if any
// is missing (deliberate: a misconfigured queue silently dropping pushes
// is worse than a fail-fast at boot).
type QueueConfig struct {
	Capacity int
	Workers  int
	Notifier *Notifier
	Stale    StaleTokenSink
}

// Queue is a buffered-channel push dispatcher. It is goroutine-safe; the
// evaluator may call Enqueue from any goroutine while Workers process.
type Queue struct {
	cfg QueueConfig
	ch  chan Job
	wg  sync.WaitGroup

	succeeded atomic.Int64
	failures  sync.Map // class -> *atomic.Int64
}

// NewQueue allocates the buffered channel but does not start workers.
// Call Start once the rest of the poller is wired.
func NewQueue(cfg QueueConfig) *Queue {
	if cfg.Capacity <= 0 || cfg.Workers <= 0 || cfg.Notifier == nil || cfg.Stale == nil {
		panic("apns: invalid QueueConfig")
	}
	return &Queue{
		cfg: cfg,
		ch:  make(chan Job, cfg.Capacity),
	}
}

// Start spawns the configured number of worker goroutines. Each worker
// drains the channel until it is closed by Stop.
func (q *Queue) Start() {
	for i := 0; i < q.cfg.Workers; i++ {
		q.wg.Add(1)
		go q.workerLoop()
	}
}

// Stop closes the channel and waits for workers to drain. Jobs already
// taken off the channel are allowed to complete; jobs still in the channel
// when Stop is called will run to completion. Returns when all workers
// have exited or ctx is done.
func (q *Queue) Stop(ctx context.Context) {
	close(q.ch)
	done := make(chan struct{})
	go func() {
		q.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}
}

// Enqueue puts a job on the channel if capacity is available. Returns
// ErrQueueFull immediately when at capacity — non-blocking by design so a
// slow APNs cannot stall the live-poll cycle (Decision 15).
func (q *Queue) Enqueue(_ context.Context, job Job) error {
	select {
	case q.ch <- job:
		return nil
	default:
		return ErrQueueFull
	}
}

// Succeeded returns the total number of successful pushes; used by tests
// to observe the dispatch loop.
func (q *Queue) Succeeded() int64 {
	return q.succeeded.Load()
}

// FailedByClass returns the number of pushes that failed in the given
// failure class. Classes match the observability buckets defined in
// design.md §Error Handling: "stale", "transient", "permanent".
func (q *Queue) FailedByClass(class string) int64 {
	v, ok := q.failures.Load(class)
	if !ok {
		return 0
	}
	return v.(*atomic.Int64).Load()
}

func (q *Queue) workerLoop() {
	defer q.wg.Done()
	ctx := context.Background()
	for job := range q.ch {
		q.dispatch(ctx, job)
	}
}

func (q *Queue) dispatch(ctx context.Context, job Job) {
	err := q.cfg.Notifier.Push(ctx, job.Token, job.CollapseID, job.Payload)
	if err == nil {
		q.succeeded.Add(1)
		slog.Info("flux_apns_push_succeeded",
			"device_id", job.DeviceID, "rule_id", job.RuleID,
			"collapse_id", job.CollapseID)
		return
	}
	if errors.Is(err, ErrStaleToken) {
		q.incFailureClass("stale")
		slog.Info("flux_apns_push_failed",
			"device_id", job.DeviceID, "rule_id", job.RuleID, "class", "stale", "error", err)
		if markErr := q.cfg.Stale.MarkStale(ctx, job.DeviceID); markErr != nil {
			slog.Error("flux_apns_mark_stale_failed",
				"device_id", job.DeviceID, "error", markErr)
		}
		return
	}
	class := "transient"
	if isPermanentErr(err) {
		class = "permanent"
	}
	q.incFailureClass(class)
	slog.Warn("flux_apns_push_failed",
		"device_id", job.DeviceID, "rule_id", job.RuleID, "class", class, "error", err)
}

// isPermanentErr is a thin sniff for the permanent-class error message
// produced by Notifier.Push. Avoids leaking an enum across packages while
// still letting workers count failures by class for observability.
func isPermanentErr(err error) bool {
	return err != nil && containsString(err.Error(), "apns permanent")
}

// containsString avoids the strings package for this single use; the
// substring is fixed and short, so the inline loop is cheaper than the
// import.
func containsString(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	if len(sub) > len(s) {
		return false
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func (q *Queue) incFailureClass(class string) {
	v, _ := q.failures.LoadOrStore(class, new(atomic.Int64))
	v.(*atomic.Int64).Add(1)
}
