package derivedstats

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"pgregory.net/rapid"
)

// TestPropertyIntegratePpvSplitAdditivity asserts that integratePpv is split-
// additive over its integration window: integratePpv([a, c)) is equal to
// integratePpv([a, b)) + integratePpv([b, c)) within float epsilon, provided
// no >60s gap straddles b. The generator bounds inter-sample gaps to <=60s
// so the qualifier is always satisfied; the assertion then checks that the
// trapezoidal algorithm's edge synthesis at b cancels exactly between the
// two halves.
func TestPropertyIntegratePpvSplitAdditivity(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		const base int64 = 1_700_000_000
		n := rapid.IntRange(2, 200).Draw(t, "n")
		readings := make([]Reading, n)
		ts := base
		for i := range n {
			gap := rapid.IntRange(8, 60).Draw(t, fmt.Sprintf("gap%d", i))
			ts += int64(gap)
			readings[i] = Reading{
				Timestamp: ts,
				Ppv:       rapid.Float64Range(-100, 5000).Draw(t, fmt.Sprintf("ppv%d", i)),
			}
		}
		firstTS := readings[0].Timestamp
		lastTS := readings[n-1].Timestamp
		windowLo := firstTS - 30
		windowHi := lastTS + 30

		a := rapid.Int64Range(windowLo, windowHi-1).Draw(t, "a")
		c := rapid.Int64Range(a+1, windowHi).Draw(t, "c")
		b := rapid.Int64Range(a, c).Draw(t, "b")

		full, _ := integratePpv(readings, a, c)
		left, _ := integratePpv(readings, a, b)
		right, _ := integratePpv(readings, b, c)

		assert.InDelta(t, full, left+right, 1e-9)
	})
}
