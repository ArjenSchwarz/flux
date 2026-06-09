package api

import (
	"context"
	"unicode/utf8"

	"github.com/ArjenSchwarz/flux/internal/dynamo"
)

// presetCap is the defensive maximum number of simulation presets (Decision
// 12). Create returns 409 when the cap is reached; chosen for selection-menu
// legibility on the Dashboard rather than any storage limit.
const presetCap = 20

// presetWattsMax is the inclusive upper bound on a preset's watt value
// (Req 1.3). Presets are validated to the same bound the /status
// simulateLoadWatts parameter accepts, so a stored preset can never produce a
// rejected status request.
const presetWattsMax = simLoadMaxWatts

// presetLabelMaxChars caps the preset label length, counted as Unicode code
// points to match the client-side cap (Req 1.3).
const presetLabelMaxChars = 40

// presetBodyMaxBytes caps inbound JSON on every preset mutation. A maxed-out
// payload (40-rune label plus the small watts field) fits well under 512
// bytes; 4096 leaves generous room for whitespace.
const presetBodyMaxBytes = 4096

// SimulationPresetStore is the api-package-local view of the preset store.
// Lambda wiring constructs a *dynamo.DynamoSimulationPresetStore and passes it
// in directly; tests substitute an in-memory fake.
type SimulationPresetStore interface {
	ListPresets(ctx context.Context) ([]dynamo.SimulationPresetItem, error)
	PutPreset(ctx context.Context, item dynamo.SimulationPresetItem) error
	DeletePreset(ctx context.Context, id string) error
}

// simulationPresetPayload is the wire shape of POST /simulation-presets and
// PUT /simulation-presets/{id}.
type simulationPresetPayload struct {
	Label string `json:"label"`
	Watts int    `json:"watts"`
}

// validate enforces Req 1.3 on the wire shape: label 1..40 Unicode code
// points, watts 1..20000. Returns a human-readable reason, or "" when valid.
func (p simulationPresetPayload) validate() string {
	n := utf8.RuneCountInString(p.Label)
	if n < 1 {
		return "label must not be empty"
	}
	if n > presetLabelMaxChars {
		return "label exceeds 40 characters"
	}
	if p.Watts < 1 || p.Watts > presetWattsMax {
		return "watts must be between 1 and 20000"
	}
	return ""
}
