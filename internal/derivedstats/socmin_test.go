package derivedstats

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMinSOC(t *testing.T) {
	tests := map[string]struct {
		readings  []Reading
		wantSoc   float64
		wantTS    int64
		wantFound bool
	}{
		"empty input": {
			readings:  nil,
			wantFound: false,
		},
		"single reading": {
			readings: []Reading{
				{Soc: 50, Timestamp: 1000},
			},
			wantSoc: 50, wantTS: 1000, wantFound: true,
		},
		"multiple readings": {
			readings: []Reading{
				{Soc: 80, Timestamp: 1000},
				{Soc: 30, Timestamp: 2000},
				{Soc: 60, Timestamp: 3000},
			},
			wantSoc: 30, wantTS: 2000, wantFound: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			soc, ts, found := MinSOC(tc.readings)
			assert.Equal(t, tc.wantFound, found)
			if found {
				assert.InDelta(t, tc.wantSoc, soc, 0.001)
				assert.Equal(t, tc.wantTS, ts)
			}
		})
	}
}
