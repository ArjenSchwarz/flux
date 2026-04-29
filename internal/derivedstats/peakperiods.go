package derivedstats

import (
	"cmp"
	"math"
	"slices"
	"time"
)

const (
	mergeGapSeconds  = 300 // max gap between clusters to merge (5 minutes)
	minPeriodSeconds = 120 // minimum period duration (2 minutes)
	maxPeakPeriods   = 3   // maximum number of peak periods to return
)

// PeakPeriods identifies the top peak usage periods from raw readings.
// It returns up to maxPeakPeriods periods ranked by energy consumed, excluding
// readings within the off-peak window. Always returns a non-nil slice.
func PeakPeriods(readings []Reading, offpeakStart, offpeakEnd string) []PeakPeriod {
	out := []PeakPeriod{}
	if len(readings) == 0 {
		return out
	}

	// Step 1: Parse off-peak window and precompute a mask so each reading only
	// incurs one timezone conversion (used in steps 2 and 3).
	offpeakStartMin, offpeakEndMin, hasOffpeak := ParseOffpeakWindow(offpeakStart, offpeakEnd)
	offpeakMask := make([]bool, len(readings))
	if hasOffpeak {
		for i, r := range readings {
			offpeakMask[i] = isOffpeak(r.Timestamp, offpeakStartMin, offpeakEndMin)
		}
	}

	// Step 2: Compute mean Pload threshold from non-off-peak readings.
	// Negative Pload readings (corrupted data or net-export accounting) are
	// clamped to 0 to stay consistent with the energy integration in step 5.
	var sum float64
	var count int
	for i, r := range readings {
		if offpeakMask[i] {
			continue
		}
		sum += max(r.Pload, 0)
		count++
	}
	if count == 0 {
		return out
	}
	threshold := sum / float64(count)

	// Step 3: Build initial clusters from above-threshold, non-off-peak readings.
	type cluster struct {
		startIdx, endIdx int
		sum              float64
		count            int
	}
	clusters := make([]cluster, 0, 16)
	var cur *cluster

	for i, r := range readings {
		if offpeakMask[i] || r.Pload <= threshold {
			if cur != nil {
				clusters = append(clusters, *cur)
				cur = nil
			}
			continue
		}
		if cur == nil {
			cur = &cluster{startIdx: i, endIdx: i, sum: r.Pload, count: 1}
		} else {
			cur.endIdx = i
			cur.sum += r.Pload
			cur.count++
		}
	}
	if cur != nil {
		clusters = append(clusters, *cur)
	}
	if len(clusters) == 0 {
		return out
	}

	// Step 4: Merge clusters within mergeGapSeconds, then discard short periods.
	merged := make([]cluster, 0, len(clusters))
	merged = append(merged, clusters[0])
	for _, c := range clusters[1:] {
		last := &merged[len(merged)-1]
		gap := readings[c.startIdx].Timestamp - readings[last.endIdx].Timestamp
		if gap <= mergeGapSeconds {
			last.endIdx = c.endIdx
			last.sum += c.sum
			last.count += c.count
		} else {
			merged = append(merged, c)
		}
	}

	// Filter by minimum duration.
	filtered := make([]cluster, 0, len(merged))
	for _, c := range merged {
		duration := readings[c.endIdx].Timestamp - readings[c.startIdx].Timestamp
		if duration >= minPeriodSeconds {
			filtered = append(filtered, c)
		}
	}
	if len(filtered) == 0 {
		return out
	}

	// Step 5: Compute energy via trapezoidal integration, rank, and return top N.
	type ranked struct {
		period   PeakPeriod
		energyWh float64 // unrounded for sorting
	}
	results := make([]ranked, 0, len(filtered))

	for _, c := range filtered {
		var energyWh float64
		for j := c.startIdx + 1; j <= c.endIdx; j++ {
			prev := readings[j-1]
			curr := readings[j]
			dt := float64(curr.Timestamp - prev.Timestamp)
			if dt > maxPairGapSeconds {
				continue
			}
			energyWh += (max(prev.Pload, 0) + max(curr.Pload, 0)) / 2 * dt / 3600
		}
		// Filter on the rounded value: a period that displays as "0 Wh" is
		// noise, not a peak.
		rounded := math.Round(energyWh)
		if rounded == 0 {
			continue
		}

		results = append(results, ranked{
			period: PeakPeriod{
				Start:    time.Unix(readings[c.startIdx].Timestamp, 0).UTC().Format(time.RFC3339),
				End:      time.Unix(readings[c.endIdx].Timestamp, 0).UTC().Format(time.RFC3339),
				AvgLoadW: roundPower(c.sum / float64(c.count)),
				EnergyWh: rounded,
			},
			energyWh: energyWh,
		})
	}

	slices.SortFunc(results, func(a, b ranked) int {
		return cmp.Compare(b.energyWh, a.energyWh)
	})

	n := min(len(results), maxPeakPeriods)
	out = make([]PeakPeriod, n)
	for i := range n {
		out[i] = results[i].period
	}
	return out
}

// roundEnergy rounds a kWh value to 2 decimal places.
func roundEnergy(v float64) float64 {
	return math.Round(v*100) / 100
}

// roundPower rounds a watts or SOC value to 1 decimal place.
func roundPower(v float64) float64 {
	return math.Round(v*10) / 10
}
