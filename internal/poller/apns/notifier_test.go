package apns

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sideshow/apns2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakePusher is a function-based test double for the APNs HTTP call. The
// Notifier wraps it so we can simulate APNs status codes without an
// HTTP/2 server.
type fakePusher struct {
	calls     atomic.Int32
	responses []*apns2.Response
	errs      []error
}

func (f *fakePusher) Push(_ context.Context, _ *apns2.Notification) (*apns2.Response, error) {
	idx := int(f.calls.Add(1)) - 1
	if idx >= len(f.responses) {
		idx = len(f.responses) - 1
	}
	var resp *apns2.Response
	if idx >= 0 && idx < len(f.responses) {
		resp = f.responses[idx]
	}
	var err error
	if idx >= 0 && idx < len(f.errs) {
		err = f.errs[idx]
	}
	return resp, err
}

func newNotifierWithPusher(p PushClient) *Notifier {
	n := NewNotifier(p, "me.nore.ig.flux")
	// Zero out the backoff base so retry tests don't sleep.
	n.backoffBase = 0
	return n
}

func TestNotifier_Push_Success(t *testing.T) {
	p := &fakePusher{
		responses: []*apns2.Response{{StatusCode: http.StatusOK}},
	}
	n := newNotifierWithPusher(p)
	err := n.Push(context.Background(), "development", "token-abc", "collapse-1", Payload{Title: "T", Body: "B"})
	require.NoError(t, err)
	assert.Equal(t, int32(1), p.calls.Load())
}

func TestNotifier_Push_StaleTokenOnUnregistered(t *testing.T) {
	p := &fakePusher{
		responses: []*apns2.Response{{StatusCode: http.StatusGone, Reason: apns2.ReasonUnregistered}},
	}
	n := newNotifierWithPusher(p)
	err := n.Push(context.Background(), "development", "token-abc", "collapse-1", Payload{Title: "T", Body: "B"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrStaleToken), "410 must surface ErrStaleToken")
	assert.Equal(t, int32(1), p.calls.Load(), "410 must not retry")
}

func TestNotifier_Push_StaleTokenOnBadDeviceToken(t *testing.T) {
	p := &fakePusher{
		responses: []*apns2.Response{{StatusCode: http.StatusBadRequest, Reason: apns2.ReasonBadDeviceToken}},
	}
	n := newNotifierWithPusher(p)
	err := n.Push(context.Background(), "development", "token-abc", "collapse-1", Payload{Title: "T", Body: "B"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrStaleToken),
		"400 BadDeviceToken must classify as stale per AC 3.9")
	assert.Equal(t, int32(1), p.calls.Load())
}

func TestNotifier_Push_Retries5xx(t *testing.T) {
	p := &fakePusher{
		responses: []*apns2.Response{
			{StatusCode: http.StatusInternalServerError, Reason: apns2.ReasonInternalServerError},
			{StatusCode: http.StatusInternalServerError, Reason: apns2.ReasonInternalServerError},
			{StatusCode: http.StatusOK},
		},
	}
	n := newNotifierWithPusher(p)
	err := n.Push(context.Background(), "development", "token-abc", "collapse-1", Payload{Title: "T", Body: "B"})
	require.NoError(t, err)
	assert.Equal(t, int32(3), p.calls.Load(), "5xx must retry up to maxRetry attempts")
}

func TestNotifier_Push_Retries429(t *testing.T) {
	p := &fakePusher{
		responses: []*apns2.Response{
			{StatusCode: http.StatusTooManyRequests, Reason: apns2.ReasonTooManyRequests},
			{StatusCode: http.StatusOK},
		},
	}
	n := newNotifierWithPusher(p)
	err := n.Push(context.Background(), "development", "token-abc", "collapse-1", Payload{Title: "T", Body: "B"})
	require.NoError(t, err)
	assert.Equal(t, int32(2), p.calls.Load())
}

func TestNotifier_Push_RetriesTransport(t *testing.T) {
	p := &fakePusher{
		errs: []error{
			errors.New("conn reset"),
			errors.New("conn reset"),
			nil,
		},
		responses: []*apns2.Response{nil, nil, {StatusCode: http.StatusOK}},
	}
	n := newNotifierWithPusher(p)
	err := n.Push(context.Background(), "development", "token-abc", "collapse-1", Payload{Title: "T", Body: "B"})
	require.NoError(t, err)
	assert.Equal(t, int32(3), p.calls.Load(), "transport errors must retry")
}

func TestNotifier_Push_ExhaustedRetriesReturnsLastError(t *testing.T) {
	p := &fakePusher{
		responses: []*apns2.Response{
			{StatusCode: http.StatusInternalServerError, Reason: apns2.ReasonInternalServerError},
			{StatusCode: http.StatusInternalServerError, Reason: apns2.ReasonInternalServerError},
			{StatusCode: http.StatusServiceUnavailable, Reason: apns2.ReasonServiceUnavailable},
		},
	}
	n := newNotifierWithPusher(p)
	err := n.Push(context.Background(), "development", "token-abc", "collapse-1", Payload{Title: "T", Body: "B"})
	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrStaleToken),
		"exhausted retries must surface a transient error, not stale-token classification")
	assert.Equal(t, int32(3), p.calls.Load(), "must attempt up to maxRetry times")
}

func TestNotifier_Push_DoesNotRetryOn4xxNonStale(t *testing.T) {
	// 400 BadCollapseId / PayloadEmpty / etc. are permanent errors that
	// retrying won't fix. They should surface as errors but not consume
	// retries beyond the first attempt.
	p := &fakePusher{
		responses: []*apns2.Response{
			{StatusCode: http.StatusBadRequest, Reason: apns2.ReasonPayloadEmpty},
		},
	}
	n := newNotifierWithPusher(p)
	err := n.Push(context.Background(), "development", "token-abc", "collapse-1", Payload{Title: "T", Body: "B"})
	require.Error(t, err)
	assert.Equal(t, int32(1), p.calls.Load(),
		"permanent 4xx must not be retried (saves cycles for transient errors)")
}

func TestNotifier_BackoffSequenceJitterBounded(t *testing.T) {
	// 1s * 2^attempt * jitter[0.5..1.5]: enforces both bounds across 100
	// draws so the property holds without depending on a fixed RNG seed.
	for attempt := 0; attempt < 3; attempt++ {
		minD := time.Duration(float64(time.Second) * float64(int(1)<<attempt) * 0.5)
		maxD := time.Duration(float64(time.Second) * float64(int(1)<<attempt) * 1.5)
		for i := 0; i < 100; i++ {
			d := computeBackoff(attempt, time.Second)
			assert.GreaterOrEqual(t, d, minD,
				"attempt %d backoff below lower bound: %v < %v", attempt, d, minD)
			assert.LessOrEqual(t, d, maxD,
				"attempt %d backoff above upper bound: %v > %v", attempt, d, maxD)
		}
	}
}

func TestEnvironmentFromSSM(t *testing.T) {
	prodHost, err := apnsHostForEnv("production")
	require.NoError(t, err)
	assert.Equal(t, apns2.HostProduction, prodHost)

	devHost, err := apnsHostForEnv("development")
	require.NoError(t, err)
	assert.Equal(t, apns2.HostDevelopment, devHost)

	_, err = apnsHostForEnv("staging")
	assert.Error(t, err, "unknown environment value must error so we never silently default")
}

// TestNotifier_RoutesByEnvironment is the load-bearing test for multi-user
// support: two devices on different APNs environments must hit different
// HTTP/2 clients. Without per-env routing one user's pushes silently 400
// when their token reaches the wrong host.
func TestNotifier_RoutesByEnvironment(t *testing.T) {
	dev := &fakePusher{responses: []*apns2.Response{{StatusCode: http.StatusOK}}}
	prod := &fakePusher{responses: []*apns2.Response{{StatusCode: http.StatusOK}}}
	n := NewMultiEnvNotifier(dev, prod, "me.nore.ig.flux")
	n.backoffBase = 0

	require.NoError(t, n.Push(context.Background(), "development", "tok-dev", "c-1", Payload{Title: "T", Body: "B"}))
	require.NoError(t, n.Push(context.Background(), "production", "tok-prod", "c-2", Payload{Title: "T", Body: "B"}))

	assert.Equal(t, int32(1), dev.calls.Load(), "development push must hit the dev client")
	assert.Equal(t, int32(1), prod.calls.Load(), "production push must hit the prod client")
}

func TestNotifier_RejectsUnknownEnvironment(t *testing.T) {
	dev := &fakePusher{}
	prod := &fakePusher{}
	n := NewMultiEnvNotifier(dev, prod, "me.nore.ig.flux")
	err := n.Push(context.Background(), "staging", "tok", "c", Payload{Title: "T", Body: "B"})
	require.Error(t, err)
	assert.Equal(t, int32(0), dev.calls.Load(), "unknown env must not reach any client")
	assert.Equal(t, int32(0), prod.calls.Load())
}

func TestNotifier_EmptyEnvironmentDefaultsToDev(t *testing.T) {
	// Backwards compatibility: a device row from before this change has no
	// APNsEnvironment field. Treat the empty string as development so the
	// poller doesn't drop pushes for legacy registrations.
	dev := &fakePusher{responses: []*apns2.Response{{StatusCode: http.StatusOK}}}
	prod := &fakePusher{}
	n := NewMultiEnvNotifier(dev, prod, "me.nore.ig.flux")
	n.backoffBase = 0
	require.NoError(t, n.Push(context.Background(), "", "tok", "c", Payload{Title: "T", Body: "B"}))
	assert.Equal(t, int32(1), dev.calls.Load())
	assert.Equal(t, int32(0), prod.calls.Load())
}
