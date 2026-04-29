package dynamo

import "github.com/ArjenSchwarz/flux/internal/derivedstats"

// DailyUsageFromAttr converts a stored *DailyUsageAttr into the in-process
// *derivedstats.DailyUsage. Returns nil when the input is nil.
func DailyUsageFromAttr(a *DailyUsageAttr) *derivedstats.DailyUsage {
	if a == nil {
		return nil
	}
	out := &derivedstats.DailyUsage{Blocks: make([]derivedstats.DailyUsageBlock, len(a.Blocks))}
	for i, b := range a.Blocks {
		out.Blocks[i] = derivedstats.DailyUsageBlock{
			Kind:              b.Kind,
			Start:             b.Start,
			End:               b.End,
			TotalKwh:          b.TotalKwh,
			AverageKwhPerHour: b.AverageKwhPerHour,
			PercentOfDay:      b.PercentOfDay,
			Status:            b.Status,
			BoundarySource:    b.BoundarySource,
		}
	}
	return out
}

// DailyUsageToAttr converts a *derivedstats.DailyUsage into the storage shape.
// Returns nil when the input is nil.
func DailyUsageToAttr(d *derivedstats.DailyUsage) *DailyUsageAttr {
	if d == nil {
		return nil
	}
	out := &DailyUsageAttr{Blocks: make([]DailyUsageBlockAttr, len(d.Blocks))}
	for i, b := range d.Blocks {
		out.Blocks[i] = DailyUsageBlockAttr{
			Kind:              b.Kind,
			Start:             b.Start,
			End:               b.End,
			TotalKwh:          b.TotalKwh,
			AverageKwhPerHour: b.AverageKwhPerHour,
			PercentOfDay:      b.PercentOfDay,
			Status:            b.Status,
			BoundarySource:    b.BoundarySource,
		}
	}
	return out
}

// SocLow has no conversion helper: derivedstats.MinSOC returns
// (soc, unixTimestamp int64, found bool), which call sites convert to RFC3339
// once at write time. The storage shape (SocLowAttr) is read directly by the
// Lambda handlers.

// PeakPeriodsFromAttr converts a slice of stored peak periods into the
// in-process []derivedstats.PeakPeriod. Returns nil for a nil input.
func PeakPeriodsFromAttr(in []PeakPeriodAttr) []derivedstats.PeakPeriod {
	if in == nil {
		return nil
	}
	out := make([]derivedstats.PeakPeriod, len(in))
	for i, p := range in {
		out[i] = derivedstats.PeakPeriod{
			Start:    p.Start,
			End:      p.End,
			AvgLoadW: p.AvgLoadW,
			EnergyWh: p.EnergyWh,
		}
	}
	return out
}

// PeakPeriodsToAttr converts a slice of in-process peak periods into the
// storage shape. Returns nil for a nil input.
func PeakPeriodsToAttr(in []derivedstats.PeakPeriod) []PeakPeriodAttr {
	if in == nil {
		return nil
	}
	out := make([]PeakPeriodAttr, len(in))
	for i, p := range in {
		out[i] = PeakPeriodAttr{
			Start:    p.Start,
			End:      p.End,
			AvgLoadW: p.AvgLoadW,
			EnergyWh: p.EnergyWh,
		}
	}
	return out
}
