package parser

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDays_FullRecord(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantLen    int
		wantDay    int
		wantTimeMs int64
		wantWords  int64
		wantPct    float64
		idx        int
	}{
		{
			name: "two full records", input: "20456|3912800@13284 #15.3%\n20461|466705@1949 #100%\n",
			wantLen: 2, idx: 0, wantDay: 20456, wantTimeMs: 3912800, wantWords: 13284, wantPct: 15.3,
		},
		{
			name: "second record at 100%", input: "20456|3912800@13284 #15.3%\n20461|466705@1949 #100%\n",
			wantLen: 2, idx: 1, wantDay: 20461, wantTimeMs: 466705, wantWords: 1949, wantPct: 100,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			days := parseDays(tt.input)
			require.Len(t, days, tt.wantLen)
			d := days[tt.idx]
			assert.Equal(t, tt.wantDay, d.Day)
			assert.Equal(t, tt.wantTimeMs, d.ReadTimeMs)
			assert.Equal(t, tt.wantWords, d.WordsRead)
			assert.Equal(t, tt.wantPct, d.Pct)
		})
	}
}

func TestParseDays_BareDay(t *testing.T) {
	days := parseDays("17976\n17983\n")
	require.Len(t, days, 2)
	assert.Equal(t, 17976, days[0].Day)
	assert.Equal(t, int64(0), days[0].ReadTimeMs)
	assert.Equal(t, 17983, days[1].Day)
}

func TestParseDays_Empty(t *testing.T) {
	assert.Empty(t, parseDays(""))
}
