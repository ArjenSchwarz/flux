package derivedstats

// MinSOC scans readings for the minimum SOC value.
// Returns (soc, timestamp, found). found is false if readings is empty.
func MinSOC(readings []Reading) (soc float64, timestamp int64, found bool) {
	if len(readings) == 0 {
		return 0, 0, false
	}
	minSoc := readings[0].Soc
	minTS := readings[0].Timestamp
	for _, r := range readings[1:] {
		if r.Soc < minSoc {
			minSoc = r.Soc
			minTS = r.Timestamp
		}
	}
	return minSoc, minTS, true
}
