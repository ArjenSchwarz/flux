package apns

import (
	"context"
	"fmt"

	"github.com/sideshow/apns2"
	"github.com/sideshow/apns2/token"
)

// Credentials are the four SSM-loaded values plus the env switch.
// SSM-side values are fetched in cmd/poller/main.go; this struct is the
// boundary between AWS-specific lookup and apns2 wiring.
type Credentials struct {
	P8Key    string // PEM body
	KeyID    string
	TeamID   string
	BundleID string
	Env      string // "production" | "development"
}

// pushClientAdapter bridges *apns2.Client's `Push(*Notification)` method
// to the context-aware PushClient interface the Notifier expects.
type pushClientAdapter struct{ c *apns2.Client }

func (a *pushClientAdapter) Push(ctx context.Context, n *apns2.Notification) (*apns2.Response, error) {
	return a.c.PushWithContext(ctx, n)
}

// NewNotifierFromCredentials builds a Notifier wired to a real apns2.Client
// configured for the requested environment. The .p8 key is parsed once and
// the resulting token is reused across pushes; sideshow/apns2 refreshes the
// JWT every ~50 minutes as Apple requires.
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
	host, err := apnsHostForEnv(creds.Env)
	if err != nil {
		return nil, err
	}
	client := apns2.NewTokenClient(tk)
	client.Host = host
	return NewNotifier(&pushClientAdapter{c: client}, creds.BundleID), nil
}
