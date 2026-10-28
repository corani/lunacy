package parser

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePO_EPUB(t *testing.T) {
	tests := []struct {
		input   string
		pct     float64
		chapter int
		offset  int
		null    bool
	}{
		{"1705161250742*39@0#32445:100%", 100, 39, 32445, false},
		{"1705161250742*37@0#488:100%", 100, 37, 488, false},
		{"1705161250742*60@0#0:28.8%", 28.8, 60, 0, false},
		{"1705161250742*0@0#0:0.0%", 0, 0, 0, true},
		{"39@0#32445:100%", 100, 39, 32445, false}, // no deviceID prefix
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			p, err := ParsePO(tt.input)
			require.NoError(t, err)
			assert.False(t, p.IsPDF)
			assert.Equal(t, tt.null, p.IsNull)
			if !tt.null {
				assert.Equal(t, tt.pct, p.Pct)
				assert.Equal(t, tt.chapter, p.Chapter)
				assert.Equal(t, tt.offset, p.CharOffset)
			}
		})
	}
}

func TestParsePO_PDF(t *testing.T) {
	tests := []struct {
		input string
		pct   float64
		page  int
	}{
		{"1705161250742*314:100%", 100, 314},
		{"1705161250742*12:2.8%", 2.8, 12},
		{"1705161250742*1:0.1%", 0.1, 1},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			p, err := ParsePO(tt.input)
			require.NoError(t, err)
			assert.True(t, p.IsPDF)
			assert.Equal(t, tt.pct, p.Pct)
			assert.Equal(t, tt.page, p.Page)
		})
	}
}

func TestParsePO_Empty(t *testing.T) {
	p, err := ParsePO("")
	require.NoError(t, err)
	assert.True(t, p.IsNull)
}
