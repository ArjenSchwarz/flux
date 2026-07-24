package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ArjenSchwarz/flux/internal/plan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The two vector files in testdata are shared with the FluxCore test suite
// (the note_lengths.json pattern). Segmentation and cost resolution exist in
// both Go and Swift; these vectors are what stop the two implementations from
// drifting, pinning AC 3.1–3.6 to identical numbers on both sides.
//
// The cost vectors are also the AC 5.2 proof: the tier-2 rows are the
// pre-band DayCosts formula, and cmd/migrate-pricing computes its golden
// values with the same plan.DayCosts helper these tests exercise.

// vectorWindow mirrors the plan wire shape's window entry.
type vectorWindow struct {
	Start string   `json:"start"`
	End   string   `json:"end"`
	Free  bool     `json:"free"`
	Rate  *float64 `json:"rate"`
}

// vectorPlan mirrors the plan wire shape.
type vectorPlan struct {
	DefaultRate          float64        `json:"defaultRate"`
	Windows              []vectorWindow `json:"windows"`
	FeedInRate           float64        `json:"feedInRate"`
	SavingsReferenceRate *float64       `json:"savingsReferenceRate"`
}

func (v vectorPlan) toPlan() plan.Plan {
	windows := make([]plan.Window, len(v.Windows))
	for i, w := range v.Windows {
		windows[i] = plan.Window{Start: w.Start, End: w.End, Free: w.Free}
		if w.Rate != nil {
			windows[i].Rate = *w.Rate
		}
	}
	return plan.Plan{
		StartDate:      "2026-01-01",
		DefaultRate:    v.DefaultRate,
		Windows:        windows,
		FeedInRate:     v.FeedInRate,
		SavingsRefRate: v.SavingsReferenceRate,
	}
}

type vectorSegment struct {
	Start string  `json:"start"`
	End   string  `json:"end"`
	Free  bool    `json:"free"`
	Rate  float64 `json:"rate"`
}

type segmentVector struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Plan        vectorPlan      `json:"plan"`
	Segments    []vectorSegment `json:"segments"`
}

type vectorOffpeak struct {
	GridImportKwh float64 `json:"gridImportKwh"`
	WindowStart   *string `json:"windowStart"`
	WindowEnd     *string `json:"windowEnd"`
	IntegratedAt  *string `json:"integratedAt"`
	SampleCount   int     `json:"sampleCount"`
}

type vectorBandImport struct {
	Start string  `json:"start"`
	End   string  `json:"end"`
	Kwh   float64 `json:"kwh"`
}

type vectorDay struct {
	EInput            *float64           `json:"eInput"`
	EOutput           *float64           `json:"eOutput"`
	PeakGridImportKwh *float64           `json:"peakGridImportKwh"`
	Offpeak           *vectorOffpeak     `json:"offpeak"`
	BandImports       []vectorBandImport `json:"bandImports"`
}

func (v vectorDay) toDayEnergy() plan.DayEnergy {
	out := plan.DayEnergy{
		EInput:            v.EInput,
		EOutput:           v.EOutput,
		PeakGridImportKwh: v.PeakGridImportKwh,
	}
	if v.Offpeak != nil {
		row := plan.OffpeakRow{
			GridImportKwh: v.Offpeak.GridImportKwh,
			SampleCount:   v.Offpeak.SampleCount,
		}
		if v.Offpeak.WindowStart != nil {
			row.WindowStart = *v.Offpeak.WindowStart
		}
		if v.Offpeak.WindowEnd != nil {
			row.WindowEnd = *v.Offpeak.WindowEnd
		}
		if v.Offpeak.IntegratedAt != nil {
			row.IntegratedAt = *v.Offpeak.IntegratedAt
		}
		out.Offpeak = &row
	}
	if v.BandImports != nil {
		out.BandImports = make([]plan.BandImport, len(v.BandImports))
		for i, b := range v.BandImports {
			out.BandImports[i] = plan.BandImport{Start: b.Start, End: b.End, Kwh: b.Kwh}
		}
	}
	return out
}

type vectorCosts struct {
	Tier         int     `json:"tier"`
	ImportCost   float64 `json:"importCost"`
	FeedInIncome float64 `json:"feedInIncome"`
	Net          float64 `json:"net"`
	Savings      float64 `json:"savings"`
}

type costVector struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Plan        vectorPlan  `json:"plan"`
	Day         vectorDay   `json:"day"`
	Expected    vectorCosts `json:"expected"`
}

func loadVectors[T any](t *testing.T, file string) []T {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", file))
	require.NoError(t, err, "read %s", file)
	var out []T
	require.NoError(t, json.Unmarshal(data, &out), "parse %s", file)
	require.NotEmpty(t, out, "%s must contain vectors", file)
	return out
}

// TestPricingSegmentVectors pins plan.Segments to the shared vectors the
// FluxCore segmentation helper is also tested against.
func TestPricingSegmentVectors(t *testing.T) {
	t.Parallel()
	for _, vec := range loadVectors[segmentVector](t, "pricing_segments.json") {
		t.Run(vec.Name, func(t *testing.T) {
			t.Parallel()
			want := make([]plan.Segment, len(vec.Segments))
			for i, s := range vec.Segments {
				want[i] = plan.Segment{Start: s.Start, End: s.End, Free: s.Free, Rate: s.Rate}
			}
			assert.Equal(t, want, plan.Segments(vec.Plan.toPlan()), vec.Description)
		})
	}
}

// TestPricingCostVectors pins plan.DayCosts — resolution tier and all four
// figures — to the shared vectors.
func TestPricingCostVectors(t *testing.T) {
	t.Parallel()
	for _, vec := range loadVectors[costVector](t, "pricing_costs.json") {
		t.Run(vec.Name, func(t *testing.T) {
			t.Parallel()
			got, tier := plan.DayCosts(vec.Plan.toPlan(), vec.Day.toDayEnergy())
			assert.Equal(t, plan.Tier(vec.Expected.Tier), tier, vec.Description)
			assert.InDelta(t, vec.Expected.ImportCost, got.ImportCost, 1e-9, "importCost")
			assert.InDelta(t, vec.Expected.FeedInIncome, got.FeedInIncome, 1e-9, "feedInIncome")
			assert.InDelta(t, vec.Expected.Net, got.Net, 1e-9, "net")
			assert.InDelta(t, vec.Expected.Savings, got.Savings, 1e-9, "savings")
		})
	}
}

// TestPricingCostVectorsCoverEveryTier guards the vector file itself: the
// design requires every resolution path and every tier-2 input combination to
// be represented, because these vectors are what the FluxCore implementation
// is held to.
func TestPricingCostVectorsCoverEveryTier(t *testing.T) {
	t.Parallel()
	vectors := loadVectors[costVector](t, "pricing_costs.json")

	byTier := map[int]int{}
	combos := map[string]bool{}
	for _, vec := range vectors {
		byTier[vec.Expected.Tier]++
		if vec.Expected.Tier != 2 {
			continue
		}
		key := "offpeak:" + boolLabel(vec.Day.Offpeak != nil) +
			" peak:" + boolLabel(vec.Day.PeakGridImportKwh != nil)
		combos[key] = true
	}

	for tier := 1; tier <= 3; tier++ {
		assert.NotZero(t, byTier[tier], "tier %d must be covered", tier)
	}
	for _, offpeak := range []bool{true, false} {
		for _, peak := range []bool{true, false} {
			key := "offpeak:" + boolLabel(offpeak) + " peak:" + boolLabel(peak)
			assert.True(t, combos[key], "tier-2 combination %s must be covered", key)
		}
	}
}

func boolLabel(v bool) string {
	if v {
		return "present"
	}
	return "absent"
}

// TestSingleRatePlansNeverReachFallback pins the property that makes AC 5.2
// hold without backfilling history: for a plan whose rated segments share one
// rate — every migrated legacy plan — tier 2 always resolves, so no historical
// day can degrade to the fallback.
func TestSingleRatePlansNeverReachFallback(t *testing.T) {
	t.Parallel()
	savings := 0.35
	migrated := plan.Plan{
		StartDate:      "2026-01-01",
		DefaultRate:    0.35,
		Windows:        []plan.Window{{Start: "11:00", End: "14:00", Free: true}},
		FeedInRate:     0.05,
		SavingsRefRate: &savings,
	}
	eInput := 20.0
	days := map[string]plan.DayEnergy{
		"nothing recorded":     {},
		"eInput only":          {EInput: &eInput},
		"unusable offpeak row": {EInput: &eInput, Offpeak: &plan.OffpeakRow{IntegratedAt: "2026-04-12T04:00:00Z"}},
		"mismatched split": {EInput: &eInput, BandImports: []plan.BandImport{
			{Start: "07:00", End: "09:00", Kwh: 1},
		}},
	}
	for name, day := range days {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, tier := plan.DayCosts(migrated, day)
			assert.NotEqual(t, plan.TierFallback, tier)
		})
	}
}
