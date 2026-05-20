// Package apns wraps github.com/sideshow/apns2 with the retry, classification,
// and back-pressure behaviour described in Decisions 10/13/15.
package apns

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"time"

	"github.com/sideshow/apns2"
)

// PushClient is the subset of *apns2.Client we depend on. Exposing the
// interface lets tests substitute a function-based double and removes the
// need for an HTTP/2 server in unit tests. The single production
// implementation is *apns2.Client, which already satisfies this shape.
type PushClient interface {
	Push(ctx context.Context, n *apns2.Notification) (*apns2.Response, error)
}

// maxRetryAttempts caps the per-Push retry count (Decision 10).
const maxRetryAttempts = 3

// defaultBackoffBase is the exponential-backoff base (Decision 10). Tests
// override the Notifier's backoffBase field with 0 to skip sleep.
const defaultBackoffBase = time.Second

// ErrStaleToken means APNs reported the token as invalid or unregistered.
// The worker pool reacts by marking the device's token as stale; the
// fire-state row is retained so we don't re-fire today.
var ErrStaleToken = errors.New("apns: device token is stale")

// ErrPermanent means APNs returned a permanent 4xx (e.g. PayloadEmpty,
// BadCollapseId). Retrying won't help; the worker counts it under the
// "permanent" failure class for observability.
var ErrPermanent = errors.New("apns: permanent failure")

// Notifier is a thin wrapper over PushClient that adds the project's retry,
// classification, and observability behaviour.
type Notifier struct {
	client      PushClient
	topic       string
	maxRetry    int
	backoffBase time.Duration
}

// NewNotifier constructs a Notifier with the production retry policy.
func NewNotifier(client PushClient, topic string) *Notifier {
	return &Notifier{
		client:      client,
		topic:       topic,
		maxRetry:    maxRetryAttempts,
		backoffBase: defaultBackoffBase,
	}
}

// Push submits the payload to APNs with the topic and collapse-id set per
// design.md. Returns ErrStaleToken when APNs reports the token as invalid;
// returns the last seen error after exhausted retries.
func (n *Notifier) Push(ctx context.Context, token, collapseID string, p Payload) error {
	body, err := p.MarshalJSON()
	if err != nil {
		return fmt.Errorf("marshal apns payload: %w", err)
	}
	note := &apns2.Notification{
		DeviceToken: token,
		Topic:       n.topic,
		CollapseID:  collapseID,
		Payload:     body,
	}
	var lastErr error
	for attempt := 0; attempt < n.maxRetry; attempt++ {
		if attempt > 0 {
			sleepFor := computeBackoff(attempt-1, n.backoffBase)
			if sleepFor > 0 {
				select {
				case <-time.After(sleepFor):
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}
		resp, err := n.client.Push(ctx, note)
		if err != nil {
			// Transport / I/O error — retryable.
			lastErr = fmt.Errorf("apns push: %w", err)
			continue
		}
		if resp == nil {
			lastErr = errors.New("apns: nil response")
			continue
		}
		switch classifyResponse(resp) {
		case classOK:
			return nil
		case classStale:
			return fmt.Errorf("apns push (%s): %w", resp.Reason, ErrStaleToken)
		case classTransient:
			lastErr = fmt.Errorf("apns transient (status=%d reason=%s)", resp.StatusCode, resp.Reason)
			continue
		case classPermanent:
			// Permanent 4xx (e.g., PayloadEmpty) won't be fixed by retry.
			return fmt.Errorf("apns permanent (status=%d reason=%s): %w", resp.StatusCode, resp.Reason, ErrPermanent)
		}
	}
	if lastErr == nil {
		lastErr = errors.New("apns: retries exhausted with no error recorded")
	}
	return lastErr
}

// responseClass tags APNs responses so the retry loop and the queue worker
// can branch without duplicating switch statements.
type responseClass int

const (
	classOK responseClass = iota
	classStale
	classTransient
	classPermanent
)

// classifyResponse maps an APNs response to one of four buckets.
func classifyResponse(resp *apns2.Response) responseClass {
	switch resp.StatusCode {
	case http.StatusOK:
		return classOK
	case http.StatusGone:
		return classStale
	case http.StatusBadRequest:
		if resp.Reason == apns2.ReasonBadDeviceToken {
			return classStale
		}
		return classPermanent
	case http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusServiceUnavailable:
		return classTransient
	}
	if resp.StatusCode >= 500 {
		return classTransient
	}
	if resp.StatusCode >= 400 {
		return classPermanent
	}
	return classTransient
}

// computeBackoff returns 1s * 2^attempt * jitter where jitter ∈ [0.5, 1.5].
// Exposed in the package so the test can verify the bounds.
func computeBackoff(attempt int, base time.Duration) time.Duration {
	if base == 0 {
		return 0
	}
	factor := 1 << attempt
	jitter := 0.5 + rand.Float64() // [0.5, 1.5)
	return time.Duration(float64(base) * float64(factor) * jitter)
}

// apnsHostForEnv maps the /flux/apns/env SSM value to the apns2 host URL.
// Returning an error on an unknown value is deliberate: a silent default
// would risk talking to the wrong APNs environment, which APNs rejects
// with confusing token-mismatch errors.
func apnsHostForEnv(env string) (string, error) {
	switch env {
	case "production":
		return apns2.HostProduction, nil
	case "development":
		return apns2.HostDevelopment, nil
	default:
		return "", fmt.Errorf("apns: unknown environment %q (expected production or development)", env)
	}
}
