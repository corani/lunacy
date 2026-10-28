package parser

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const anV1Sample = `454839
indent:false
trim:false
#
1158
Killing Floor
/sdcard/Books/Lee Child/Jack Reacher/01. Killing Floor - Lee Child.epub
/sdcard/books/lee child/jack reacher/01. killing floor - lee child.epub
11
0
5290
361
1996532479
1763907035361


When the unexpected gets dumped on you, don't waste time.
1
0
0
#
1159
Killing Floor
/sdcard/Books/Lee Child/Jack Reacher/01. Killing Floor - Lee Child.epub
/sdcard/books/lee child/jack reacher/01. killing floor - lee child.epub
14
0
12788
262
1996532479
1763911623283

A note here
I may be an old guy, but the truth is old guys remember stuff real well.
0
1
0`

func TestParseAN_V1(t *testing.T) {
	anns, err := ParseAN(anV1Sample)
	require.NoError(t, err)
	require.Len(t, anns, 2)

	tests := []struct {
		ann           Annotation
		id            int
		chapter       int
		charOffset    int
		length        int
		color         int64
		createdAt     int64
		note          string
		annType       int
		underline     int
		strikethrough int
	}{
		{
			ann: anns[0], id: 1158, chapter: 11, charOffset: 5290,
			length: 361, color: 1996532479, createdAt: 1763907035361,
			note: "", annType: 1, underline: 0, strikethrough: 0,
		},
		{
			ann: anns[1], id: 1159, chapter: 14, charOffset: 12788,
			length: 262, color: 1996532479, createdAt: 1763911623283,
			note: "A note here", annType: 0, underline: 1, strikethrough: 0,
		},
	}
	for _, tt := range tests {
		a := tt.ann
		assert.Equal(t, tt.id, a.ID)
		assert.Equal(t, tt.chapter, a.Chapter)
		assert.Equal(t, tt.charOffset, a.CharOffset)
		assert.Equal(t, tt.length, a.Length)
		assert.Equal(t, tt.color, a.Color)
		assert.Equal(t, tt.createdAt, a.CreatedAt)
		assert.Equal(t, tt.note, a.Note)
		assert.Equal(t, tt.annType, a.Type)
		assert.Equal(t, tt.underline, a.Underline)
		assert.Equal(t, tt.strikethrough, a.Strikethrough)
	}
}

const anV2Sample = `315#A*#9#A1#1765377968047#A2#9#A3#0#A4#0#A5#0#A6##A7##A8#(10/315) Contents#A@#A*#26#A1#1786667172722#A2#26#A3#1391#A4#1996532479#A5#1#A6##A7#Al-Qaeda can take over the enemy's means#A8##A@`

func TestParseAN_V2(t *testing.T) {
	anns, err := ParseAN(anV2Sample)
	require.NoError(t, err)
	require.Len(t, anns, 2)

	tests := []struct {
		ann          Annotation
		id           int
		createdAt    int64
		page         int
		length       int
		color        int64
		annType      int
		chapterLabel string
	}{
		{
			ann: anns[0], id: 9, createdAt: 1765377968047, page: 9,
			length: 0, color: 0, annType: 0, chapterLabel: "(10/315) Contents",
		},
		{
			ann: anns[1], id: 26, createdAt: 1786667172722, page: 26,
			length: 1391, color: 1996532479, annType: 1, chapterLabel: "",
		},
	}
	for _, tt := range tests {
		a := tt.ann
		assert.Equal(t, tt.id, a.ID)
		assert.Equal(t, tt.createdAt, a.CreatedAt)
		assert.Equal(t, tt.page, a.Page)
		assert.Equal(t, tt.length, a.Length)
		assert.Equal(t, tt.color, a.Color)
		assert.Equal(t, tt.annType, a.Type)
		assert.Equal(t, tt.chapterLabel, a.ChapterLabel)
	}
}

func TestParseAN_Empty(t *testing.T) {
	anns, err := ParseAN("")
	require.NoError(t, err)
	assert.Empty(t, anns)
}
