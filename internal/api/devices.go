package api

import (
	"context"
	"encoding/base64"
	"errors"

	"github.com/ArjenSchwarz/flux/internal/dynamo"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// base64Decode wraps the standard library so device/rule handlers can decode
// optionally-base64 bodies without importing encoding/base64 each time.
func base64Decode(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}

// deviceIDMaxLen caps the deviceId path/body parameter. The Swift client
// sends UUID().uuidString (36 chars); the cap is loose enough to accept
// unforeseen formats while rejecting hostile multi-KB values.
const deviceIDMaxLen = 128

// validDeviceID returns true when s is a plausible device identifier:
// non-empty, no longer than deviceIDMaxLen, and composed only of characters
// safe for DynamoDB keys and URL paths.
func validDeviceID(s string) bool {
	if s == "" || len(s) > deviceIDMaxLen {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_':
		default:
			return false
		}
	}
	return true
}

// DeviceStore is the api-package-local view the device handler needs.
// The poller-side dynamo store and the Lambda-side dynamo store both
// satisfy this; declaring it here keeps the api package from importing
// dynamo internals other than the DeviceItem shape.
type DeviceStore interface {
	GetDevice(ctx context.Context, deviceID string) (*dynamo.DeviceItem, error)
	// PutDeviceConditional upserts the row only when the existing
	// tzUpdatedAt is less than or equal to incomingTZUpdatedAt. Returning
	// *types.ConditionalCheckFailedException signals AC 4.5's "stale TZ
	// rejected" branch — the handler maps this to 409.
	PutDeviceConditional(ctx context.Context, item dynamo.DeviceItem, incomingTZUpdatedAt int64) error
}

// errConditionalCheckFailed is a convenience to test the conditional-failed
// case without importing the smithy/types package in callers.
func errConditionalCheckFailed(err error) bool {
	var ccf *types.ConditionalCheckFailedException
	return errors.As(err, &ccf)
}
