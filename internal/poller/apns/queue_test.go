package apns

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sideshow/apns2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// blockingPusher holds every Push call until release is closed. Lets the
// test fill the queue past worker concurrency and trigger overflow.
type blockingPusher struct {
	release   chan struct{}
	calls     atomic.Int32
	responses []*apns2.Response
}

func (p *blockingPusher) Push(_ context.Context, _ *apns2.Notification) (*apns2.Response, error) {
	idx := int(p.calls.Add(1)) - 1
	<-p.release
	resp := &apns2.Response{StatusCode: 200}
	if idx >= 0 && idx < len(p.responses) {
		resp = p.responses[idx]
	}
	return resp, nil
}

// noopStaleSink swallows MarkStale calls so worker bookkeeping can run.
type noopStaleSink struct {
	stale []string
	mu    sync.Mutex
}

func (s *noopStaleSink) MarkStale(_ context.Context, deviceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stale = append(s.stale, deviceID)
	return nil
}

// fastPusher returns a precomputed response immediately.
type fastPusher struct {
	resp *apns2.Response
	err  error
}

func (p *fastPusher) Push(_ context.Context, _ *apns2.Notification) (*apns2.Response, error) {
	if p.err != nil {
		return nil, p.err
	}
	return p.resp, nil
}

func makeJob(id string) Job {
	return Job{
		DeviceID:   id,
		RuleID:     "rule-1",
		Token:      "token-" + id,
		CollapseID: "collapse-" + id,
		Payload:    Payload{Title: "T", Body: "B"},
	}
}

func TestQueue_EnqueueAndWorkerDrains(t *testing.T) {
	pusher := &fastPusher{resp: &apns2.Response{StatusCode: 200}}
	notifier := newNotifierWithPusher(pusher)
	stale := &noopStaleSink{}
	q := NewQueue(QueueConfig{Capacity: 4, Workers: 2, Notifier: notifier, Stale: stale})
	q.Start()
	defer q.Stop(context.Background())

	for i := 0; i < 4; i++ {
		require.NoError(t, q.Enqueue(context.Background(), makeJob("d")))
	}
	require.Eventually(t, func() bool {
		return q.Succeeded() == 4
	}, 2*time.Second, 5*time.Millisecond, "all queued jobs must be dispatched")
}

func TestQueue_EnqueueErrQueueFullWhenAtCapacity(t *testing.T) {
	blocked := &blockingPusher{release: make(chan struct{})}
	notifier := newNotifierWithPusher(blocked)
	stale := &noopStaleSink{}
	q := NewQueue(QueueConfig{Capacity: 2, Workers: 1, Notifier: notifier, Stale: stale})
	q.Start()
	defer func() {
		close(blocked.release)
		q.Stop(context.Background())
	}()

	// Wait for the worker to pull the first job off the channel (so the
	// channel buffer is in a known empty state with the worker blocked in
	// Push). After that the channel can hold exactly Capacity more jobs;
	// the Capacity+2-th Enqueue must hit ErrQueueFull.
	require.NoError(t, q.Enqueue(context.Background(), makeJob("d1")))
	require.Eventually(t, func() bool {
		return blocked.calls.Load() == 1
	}, time.Second, 5*time.Millisecond, "worker must consume the first job before the buffer fills")

	require.NoError(t, q.Enqueue(context.Background(), makeJob("d2")))
	require.NoError(t, q.Enqueue(context.Background(), makeJob("d3")))

	err := q.Enqueue(context.Background(), makeJob("d4"))
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrQueueFull), "Capacity+2-th Enqueue must return ErrQueueFull")
}

func TestQueue_StopDrainsInFlightThenReturns(t *testing.T) {
	blocked := &blockingPusher{release: make(chan struct{})}
	notifier := newNotifierWithPusher(blocked)
	stale := &noopStaleSink{}
	q := NewQueue(QueueConfig{Capacity: 8, Workers: 2, Notifier: notifier, Stale: stale})
	q.Start()

	for i := 0; i < 4; i++ {
		require.NoError(t, q.Enqueue(context.Background(), makeJob("d")))
	}
	// Allow the in-flight Push calls to complete; Stop should then return.
	close(blocked.release)

	done := make(chan struct{})
	go func() {
		q.Stop(context.Background())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return within 2s")
	}
}

func TestQueue_StaleTokenMarkedOnWorkerPath(t *testing.T) {
	stalePusher := &fastPusher{resp: &apns2.Response{StatusCode: 410, Reason: apns2.ReasonUnregistered}}
	notifier := newNotifierWithPusher(stalePusher)
	stale := &noopStaleSink{}
	q := NewQueue(QueueConfig{Capacity: 4, Workers: 1, Notifier: notifier, Stale: stale})
	q.Start()
	defer q.Stop(context.Background())

	require.NoError(t, q.Enqueue(context.Background(), makeJob("d-stale")))

	require.Eventually(t, func() bool {
		stale.mu.Lock()
		defer stale.mu.Unlock()
		return len(stale.stale) == 1
	}, 2*time.Second, 5*time.Millisecond, "worker must call MarkStale on 410")
	assert.Equal(t, []string{"d-stale"}, stale.stale)
	assert.Equal(t, int64(1), q.FailedByClass("stale"))
}

func TestQueue_FailedClassesObservable(t *testing.T) {
	// One transient-failure pusher: 500 three times -> retries exhausted ->
	// counted as failure with class=transient.
	failing := &fastPusher{resp: &apns2.Response{StatusCode: 500, Reason: apns2.ReasonInternalServerError}}
	notifier := newNotifierWithPusher(failing)
	stale := &noopStaleSink{}
	q := NewQueue(QueueConfig{Capacity: 2, Workers: 1, Notifier: notifier, Stale: stale})
	q.Start()
	defer q.Stop(context.Background())

	require.NoError(t, q.Enqueue(context.Background(), makeJob("d-transient")))
	require.Eventually(t, func() bool {
		return q.FailedByClass("transient") == 1
	}, 2*time.Second, 5*time.Millisecond)
}
