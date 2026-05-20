package eval

import (
	"crypto/sha256"
	"encoding/base64"
)

// CollapseID returns the APNs `apns-collapse-id` for the given (deviceId,
// ruleId, windowStartDate) triple. Decision 14: 22 base64url chars
// (132 bits) is well under Apple's 64-byte cap and far above the
// cardinality this feature will ever produce. The same value is also
// written to the fire-state row so an APNs log entry can be
// cross-referenced.
func CollapseID(deviceID, ruleID, windowStartDate string) string {
	h := sha256.Sum256([]byte(deviceID + "|" + ruleID + "|" + windowStartDate))
	return base64.RawURLEncoding.EncodeToString(h[:])[:22]
}
