// Package parser parses Moon+ Reader .an annotation cache files.
//
// Two format versions exist, distinguished by the first non-whitespace content:
//
// Format v1 (EPUB/MOBI): newline-delimited records separated by "#" lines.
//
//	<seqID>
//	indent:<bool>
//	trim:<bool>
//	#
//	<id>
//	...fields...
//
// Format v2 (PDF): inline-delimited records using #A* / #A@ sentinels.
//
//	<seqID>#A*<id>#A1#<ts>#A2#<pos>#A3#<len>#A4#<color>#A5#<type>#A6#<note>#A7#<text>#A8#<label>#A@
package parser

import (
	"bufio"
	"regexp"
	"strconv"
	"strings"
)

// Annotation holds a parsed annotation from a .an file.
type Annotation struct {
	ID            int
	Chapter       int // v1 only
	SplitIndex    int // v1 only
	CharOffset    int // v1 only
	Page          int // v2 (PDF) only
	Length        int
	Color         int64
	CreatedAt     int64 // Unix ms
	Type          int   // 1=highlight, 0=bookmark
	Underline     int
	Strikethrough int
	Note          string
	Text          string
	ChapterLabel  string // v2 only: chapter heading at this position
}

// ParseAN parses the decompressed content of a .an file.
func ParseAN(content string) ([]Annotation, error) {
	if strings.Contains(content, "#A*") {
		return parseANv2(content)
	}
	return parseANv1(content)
}

// parseANv1 parses the newline-delimited EPUB format.
func parseANv1(content string) ([]Annotation, error) {
	// Split into raw records on lines that are exactly "#"
	rawRecords := splitOnSeparator(content)

	var annotations []Annotation
	for _, raw := range rawRecords {
		ann, ok := parseANv1Record(raw)
		if ok {
			annotations = append(annotations, ann)
		}
	}
	return annotations, nil
}

// splitOnSeparator splits content into records, each delimited by a line == "#".
func splitOnSeparator(content string) [][]string {
	var records [][]string
	var current []string
	sc := bufio.NewScanner(strings.NewReader(content))
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "#" {
			if len(current) > 0 {
				records = append(records, current)
			}
			current = nil
		} else {
			current = append(current, line)
		}
	}
	if len(current) > 0 {
		records = append(records, current)
	}
	return records
}

// parseANv1Record parses one v1 record (slice of lines after the "#" separator).
// Line indices (0-based after the separator):
//
//	0: id
//	1: bookTitle
//	2: filePath (original case)
//	3: lowerFilePath
//	4: chapter
//	5: splitIndex
//	6: charOffset
//	7: highlightLength
//	8: highlightColor
//	9: timestamp (ms)
//	10: noteText
//	11: noteText2 (unused)
//	12: highlightedText (may span multiple lines)
//	then: type, underline, strikethrough as the last three non-empty lines
func parseANv1Record(lines []string) (Annotation, bool) {
	if len(lines) < 13 {
		return Annotation{}, false
	}

	parseInt := func(s string) int {
		v, _ := strconv.Atoi(strings.TrimSpace(s))
		return v
	}
	parseInt64 := func(s string) int64 {
		v, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		return v
	}

	ann := Annotation{
		ID: parseInt(lines[0]),
		// lines[1] = bookTitle, lines[2] = filePath, lines[3] = lowerFilePath
		Chapter:    parseInt(lines[4]),
		SplitIndex: parseInt(lines[5]),
		CharOffset: parseInt(lines[6]),
		Length:     parseInt(lines[7]),
		Color:      parseInt64(lines[8]),
		CreatedAt:  parseInt64(lines[9]),
		Note:       strings.TrimSpace(lines[10]),
		// lines[11] = noteText2 (unused)
	}

	// Highlighted text runs from line 12 until the last 3 non-empty lines.
	// The last 3 non-empty lines are type, underline, strikethrough.
	// Collect trailing non-empty lines as a suffix.
	trailing := []string{}
	textLines := lines[12:]
	for len(textLines) > 0 && len(trailing) < 3 {
		last := textLines[len(textLines)-1]
		if strings.TrimSpace(last) != "" {
			trailing = append([]string{last}, trailing...)
			textLines = textLines[:len(textLines)-1]
		} else {
			// empty lines are part of text, not trailing flags
			break
		}
	}

	ann.Text = strings.Join(textLines, "\n")
	if len(trailing) == 3 {
		ann.Type = parseInt(trailing[0])
		ann.Underline = parseInt(trailing[1])
		ann.Strikethrough = parseInt(trailing[2])
	}

	return ann, ann.ID > 0
}

// parseANv2 parses the inline-delimited PDF format.
var v2RecordRe = regexp.MustCompile(`#A\*([^#]*)#A1#([^#]*)#A2#([^#]*)#A3#([^#]*)#A4#([^#]*)#A5#([^#]*)#A6#([^#]*)#A7#([\s\S]*?)#A8#([^#]*)#A@`)

func parseANv2(content string) ([]Annotation, error) {
	matches := v2RecordRe.FindAllStringSubmatch(content, -1)

	parseInt := func(s string) int {
		v, _ := strconv.Atoi(strings.TrimSpace(s))
		return v
	}
	parseInt64 := func(s string) int64 {
		v, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		return v
	}

	var annotations []Annotation
	for _, m := range matches {
		// m[1]=id, m[2]=ts, m[3]=pos, m[4]=len, m[5]=color, m[6]=type,
		// m[7]=note, m[8]=text (may be multiline), m[9]=chapterLabel
		// Wait — field order per docs: A1=ts, A2=pos, A3=len, A4=color, A5=type,
		// A6=note, A7=text, A8=label
		// and the part before A1 (m[1]) is the record id
		idStr := strings.TrimPrefix(strings.TrimSpace(m[1]), "#")
		ann := Annotation{
			ID:           parseInt(idStr),
			CreatedAt:    parseInt64(m[2]),
			Page:         parseInt(m[3]),
			Length:       parseInt(m[4]),
			Color:        parseInt64(m[5]),
			Type:         parseInt(m[6]),
			Note:         strings.TrimSpace(m[7]),
			Text:         strings.TrimSpace(m[8]),
			ChapterLabel: strings.TrimSpace(m[9]),
		}
		if ann.ID > 0 {
			annotations = append(annotations, ann)
		}
	}
	return annotations, nil
}
