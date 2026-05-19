package apns

import (
	"encoding/json"
)

// Payload is the in-memory shape passed to the Notifier; it lets the
// evaluator stay decoupled from APNs JSON encoding.
type Payload struct {
	Title            string
	Body             string
	RuleID           string
	ThresholdPercent int
	ObservedSoc      float64
}

// MarshalJSON renders the APNs payload exactly as described in
// design.md §APNs payload. Custom keys ride alongside the standard `aps`
// block so the iOS/macOS notification delegate can dispatch by ruleId or
// build a richer UI later.
func (p Payload) MarshalJSON() ([]byte, error) {
	body := struct {
		Aps              apsBlock `json:"aps"`
		RuleID           string   `json:"ruleId"`
		ThresholdPercent int      `json:"thresholdPercent"`
		ObservedSoc      float64  `json:"observedSoc"`
	}{
		Aps: apsBlock{
			Alert: alert{
				Title: p.Title,
				Body:  p.Body,
			},
			Sound: "default",
		},
		RuleID:           p.RuleID,
		ThresholdPercent: p.ThresholdPercent,
		ObservedSoc:      p.ObservedSoc,
	}
	return json.Marshal(body)
}

type apsBlock struct {
	Alert alert  `json:"alert"`
	Sound string `json:"sound"`
}

type alert struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}
