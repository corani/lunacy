// Package parser parses Moon+ Reader .po reading-position cache files.
//
// A .po file is a single line of plain text:
//
//	<deviceID>*<positionString>
//
// The deviceID is a fixed identifier shared across all .po files on a device
// (not a timestamp). The position string format depends on book type.
//
// EPUB/MOBI: <chapter>@<splitIndex>#<charOffset>:<pct>%
// PDF:       <page>:<pct>%
// Unopened:  0@0#0:0.0%
package parser

import (
	"fmt"
	"strconv"
	"strings"
)

// Position holds the parsed reading position from a .po file.
type Position struct {
	// EPUB fields (zero for PDF)
	Chapter    int
	SplitIndex int
	CharOffset int

	// PDF fields (zero for EPUB)
	Page int

	// Common
	Pct    float64
	IsPDF  bool
	IsNull bool // true when the book has never been opened
}

// ParsePO parses the content of a .po file and returns the Position.
func ParsePO(content string) (Position, error) {
	content = strings.TrimSpace(content)

	// Strip the deviceID prefix (<digits>*)
	if idx := strings.Index(content, "*"); idx >= 0 {
		content = content[idx+1:]
	}

	if content == "" {
		return Position{IsNull: true}, nil
	}

	if strings.Contains(content, "@") {
		return parseEPUBPosition(content)
	}
	return parsePDFPosition(content)
}

// parseEPUBPosition parses: <chapter>@<splitIndex>#<charOffset>:<pct>%
func parseEPUBPosition(s string) (Position, error) {
	var p Position

	atParts := strings.SplitN(s, "@", 2)
	if len(atParts) != 2 {
		return p, fmt.Errorf("invalid epub position %q: missing @", s)
	}
	chapter, err := strconv.Atoi(atParts[0])
	if err != nil {
		return p, fmt.Errorf("invalid chapter in %q: %w", s, err)
	}
	p.Chapter = chapter

	rest := atParts[1]
	hashParts := strings.SplitN(rest, "#", 2)
	if len(hashParts) != 2 {
		return p, fmt.Errorf("invalid epub position %q: missing #", s)
	}
	split, err := strconv.Atoi(hashParts[0])
	if err != nil {
		return p, fmt.Errorf("invalid splitIndex in %q: %w", s, err)
	}
	p.SplitIndex = split

	colonParts := strings.SplitN(hashParts[1], ":", 2)
	if len(colonParts) != 2 {
		return p, fmt.Errorf("invalid epub position %q: missing :", s)
	}
	charOffset, err := strconv.Atoi(colonParts[0])
	if err != nil {
		return p, fmt.Errorf("invalid charOffset in %q: %w", s, err)
	}
	p.CharOffset = charOffset

	pctStr := strings.TrimSuffix(colonParts[1], "%")
	pct, err := strconv.ParseFloat(pctStr, 64)
	if err != nil {
		return p, fmt.Errorf("invalid pct in %q: %w", s, err)
	}
	p.Pct = pct

	if p.Chapter == 0 && p.CharOffset == 0 && p.Pct == 0 {
		p.IsNull = true
	}
	return p, nil
}

// parsePDFPosition parses: <page>:<pct>%
func parsePDFPosition(s string) (Position, error) {
	var p Position
	p.IsPDF = true

	// Some PDF positions have extra fields we don't need; take first two colon parts.
	colonParts := strings.SplitN(s, ":", 2)
	if len(colonParts) != 2 {
		return p, fmt.Errorf("invalid pdf position %q: missing :", s)
	}

	// Page may be a float in some edge cases; take integer part.
	pageStr := strings.SplitN(colonParts[0], ".", 2)[0]
	page, err := strconv.Atoi(pageStr)
	if err != nil {
		return p, fmt.Errorf("invalid page in %q: %w", s, err)
	}
	p.Page = page

	pctStr := strings.TrimSuffix(colonParts[1], "%")
	// pct may itself have more content after % in complex PDF positions; trim safely.
	if idx := strings.IndexByte(pctStr, '@'); idx >= 0 {
		pctStr = pctStr[:idx]
	}
	pct, err := strconv.ParseFloat(pctStr, 64)
	if err != nil {
		return p, fmt.Errorf("invalid pct in %q: %w", s, err)
	}
	p.Pct = pct

	if p.Page == 0 && p.Pct == 0 {
		p.IsNull = true
	}
	return p, nil
}
