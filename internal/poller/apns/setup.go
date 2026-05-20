package apns

import (
	"context"
	"fmt"

	"github.com/sideshow/apns2"
	"github.com/sideshow/apns2/token"
)

// Credentials are the SSM-loaded values needed to build the APNs notifier.
// The same .p8 key works against both APNs hosts (sandbox + production);
// the Notifier holds one client per host and the per-push environment
// (carried on each device row) selects which client handles the push.
type Credentials struct {
	P8Key    string // PEM body
	KeyID    string
	TeamID   string
	BundleID string
}

// pushClientAdapter bridges *apns2.Client's `Push(*Notification)` method
// to the context-aware PushClient interface the Notifier expects.
type pushClientAdapter struct{ c *apns2.Client }

func (a *pushClientAdapter) Push(ctx context.Context, n *apns2.Notification) (*apns2.Response, error) {
	return a.c.PushWithContext(ctx, n)
}

// NewNotifierFromCredentials builds a Notifier wired to two real
// apns2.Client instances (one against the sandbox host, one against the
// production host). The .p8 key is parsed once and the resulting JWT is
// reused across pushes; sideshow/apns2 refreshes the JWT every ~50 minutes
// as Apple requires.
func NewNotifierFromCredentials(creds Credentials) (*Notifier, error) {
	key, err := token.AuthKeyFromBytes([]byte(creds.P8Key))
	if err != nil {
		return nil, fmt.Errorf("apns: parse .p8 key: %w", err)
	}
	tk := &token.Token{
		AuthKey: key,
		KeyID:   creds.KeyID,
		TeamID:  creds.TeamID,
	}
	dev := apns2.NewTokenClient(tk)
	dev.Host = apns2.HostDevelopment
	prod := apns2.NewTokenClient(tk)
	prod.Host = apns2.HostProduction
	return NewMultiEnvNotifier(
		&pushClientAdapter{c: dev},
		&pushClientAdapter{c: prod},
		creds.BundleID,
	), nil
}
