// Package cover generates deterministic gradient SVG cover images from a
// title and author string. The gradient colors are derived by hashing the
// input so the same title+author always produces the same cover.
package cover

import (
	"crypto/sha256"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// Generate returns the path to an SVG cover for the given key (title+author,
// or any string). It caches the result under coverDir/<hash>.svg.
func Generate(coverDir, key string) (string, error) {
	if err := os.MkdirAll(coverDir, 0o755); err != nil {
		return "", err
	}

	hash := sha256.Sum256([]byte(key))
	name := fmt.Sprintf("%x.svg", hash[:8])
	path := filepath.Join(coverDir, name)

	if _, err := os.Stat(path); err == nil {
		return path, nil // cached
	}

	svg := generate(key)
	if err := os.WriteFile(path, []byte(svg), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// SVG returns the SVG markup for a key without caching.
func SVG(key string) string {
	return generate(key)
}

func generate(key string) string {
	h := sha256.Sum256([]byte(key))

	// Derive two hues for the gradient (separated by 40–140 degrees).
	hue1 := float64(uint16(h[0])<<8|uint16(h[1])) / 65535.0 * 360
	hue2 := math.Mod(hue1+float64(60+int(h[2])%80), 360)

	color1 := hslToHex(hue1, 0.55, 0.30)
	color2 := hslToHex(hue2, 0.60, 0.22)
	textColor := "#f0ede8"

	// Split key into title / author at first newline or " - ".
	title, author := splitKey(key)

	titleLines := wrapText(title, 16)
	authorLine := truncate(author, 22)

	var lines strings.Builder
	// Title lines, vertically centered in upper 70% of card.
	startY := 100 - (len(titleLines)-1)*18
	for i, line := range titleLines {
		fmt.Fprintf(&lines, `<text x="90" y="%d" text-anchor="middle" font-family="Georgia,serif" font-size="17" fill="%s" font-weight="bold">%s</text>`,
			startY+i*22, textColor, xmlEscape(line))
		lines.WriteByte('\n')
	}
	// Author line near bottom.
	fmt.Fprintf(&lines, `<text x="90" y="195" text-anchor="middle" font-family="Arial,sans-serif" font-size="12" fill="%s" opacity="0.75">%s</text>`,
		textColor, xmlEscape(authorLine))

	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="180" height="270" viewBox="0 0 180 270">
  <defs>
    <linearGradient id="g" x1="0" y1="0" x2="1" y2="1">
      <stop offset="0%%" stop-color="%s"/>
      <stop offset="100%%" stop-color="%s"/>
    </linearGradient>
  </defs>
  <rect width="180" height="270" fill="url(#g)" rx="4"/>
  <rect x="12" y="12" width="156" height="246" fill="none" stroke="%s" stroke-opacity="0.15" stroke-width="1" rx="3"/>
  %s
</svg>`, color1, color2, textColor, lines.String())
}

func splitKey(key string) (title, author string) {
	if before, after, ok := strings.Cut(key, "\n"); ok {
		return strings.TrimSpace(before), strings.TrimSpace(after)
	}
	if before, after, ok := strings.Cut(key, " - "); ok {
		return strings.TrimSpace(before), strings.TrimSpace(after)
	}
	return key, ""
}

func wrapText(s string, maxRunes int) []string {
	words := strings.Fields(s)
	var lines []string
	var cur strings.Builder
	for _, w := range words {
		if cur.Len() > 0 && utf8.RuneCountInString(cur.String())+1+utf8.RuneCountInString(w) > maxRunes {
			lines = append(lines, cur.String())
			cur.Reset()
		}
		if cur.Len() > 0 {
			cur.WriteByte(' ')
		}
		cur.WriteString(w)
	}
	if cur.Len() > 0 {
		lines = append(lines, cur.String())
	}
	if len(lines) == 0 {
		return []string{""}
	}
	// Cap at 4 lines.
	if len(lines) > 4 {
		lines = lines[:4]
		last := lines[3]
		if utf8.RuneCountInString(last) > maxRunes-1 {
			runes := []rune(last)
			last = string(runes[:maxRunes-1])
		}
		lines[3] = last + "…"
	}
	return lines
}

func truncate(s string, maxRunes int) string {
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes-1]) + "…"
}

// hslToHex converts HSL (h in 0-360, s and l in 0-1) to a CSS hex color.
func hslToHex(h, s, l float64) string {
	c := (1 - math.Abs(2*l-1)) * s
	x := c * (1 - math.Abs(math.Mod(h/60, 2)-1))
	m := l - c/2

	var r, g, b float64
	switch {
	case h < 60:
		r, g, b = c, x, 0
	case h < 120:
		r, g, b = x, c, 0
	case h < 180:
		r, g, b = 0, c, x
	case h < 240:
		r, g, b = 0, x, c
	case h < 300:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}

	ri := int((r+m)*255 + 0.5)
	gi := int((g+m)*255 + 0.5)
	bi := int((b+m)*255 + 0.5)
	return fmt.Sprintf("#%02x%02x%02x", ri, gi, bi)
}

func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	return s
}
