package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"

	"github.com/ArjenSchwarz/flux/internal/dynamo"
)

// ruleCap is the per-device limit enforced server-side (AC 1.5).
const ruleCap = 10

// labelMaxChars caps the rule label length (AC 1.2). Counted as Unicode code
// points rather than bytes — the iOS/macOS clients already enforce the same
// cap on the input side, but the server validates again so a misbehaving
// client cannot bypass.
const labelMaxChars = 40

// labelMaxBytes is a defence-in-depth cap on the encoded byte length. A
// 40-rune string of 4-byte code points encodes to 160 bytes; rejecting
// anything larger keeps a hostile client from inflating the APNs payload
// or the DynamoDB row beyond what the rune cap implies.
const labelMaxBytes = 160

// SocRuleStore is the api-package-local view of the rule store. Lambda
// wiring constructs a dynamo.DynamoSocRuleWriter and reader and adapts them
// to this interface so the api package doesn't import the dynamo writer
// type directly.
type SocRuleStore interface {
	ListRulesByDevice(ctx context.Context, deviceID string) ([]dynamo.SoCRuleItem, error)
	PutRule(ctx context.Context, item dynamo.SoCRuleItem) error
	DeleteRule(ctx context.Context, deviceID, ruleID string) error
}

// FireStateCleaner deletes every fire-state row for the given (device, rule).
// Used by the Lambda after a rule mutation (AC 5.3 / 5.4).
type FireStateCleaner interface {
	DeleteFireStateByDeviceRule(ctx context.Context, deviceID, ruleID string) (int, error)
}

// defaultIDFunc returns a 128-bit random hex string suitable as a rule UUID.
// Decision: the rule id format is opaque to the client, so a plain hex
// digest is simpler than pulling in a UUID library. 32 hex chars = 128 bits.
func defaultIDFunc() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
