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
