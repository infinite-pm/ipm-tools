package parser

import (
	"fmt"
	"strings"
)

// LogicalLine is a single logical line after continuation merging.
type LogicalLine struct {
	Text     string // merged content (continuations joined with space)
	RawStart int    // byte offset in original input where this logical line group begins
	M2R      []int  // merged-index → raw-index mapping (len == len(Text))
	LineNo   int    // 1-based first physical line number
}

// preprocess takes raw input bytes and produces a slice of LogicalLine.
// It rejects \r, merges continuation lines (≥2 leading spaces), skips full-line
// comments (#) and blank lines, and builds per-line merged→raw offset maps.
//
// It also returns the [start,end) raw byte spans of every full-line comment
// (optional leading whitespace + `#` to end of line) so the semantic
// tokenizer can color them without re-scanning — one source of truth.
func preprocess(raw string) ([]LogicalLine, [][2]int, error) {
	if strings.Contains(raw, "\r") {
		return nil, nil, fmt.Errorf("invalid input: carriage return (\\r) is not supported; normalize to LF first")
	}

	rawLines := strings.Split(raw, "\n")

	// Precompute raw line start offsets (byte offset of each line in raw)
	starts := make([]int, len(rawLines))
	acc := 0
	for i := range rawLines {
		starts[i] = acc
		acc += len(rawLines[i]) + 1 // +1 for newline
	}

	// Comment spans: every physical full-line comment, mirroring the old
	// tokenizer regex `^[ \t]*#.*$` (leading whitespace included).
	var comments [][2]int
	for i, ln := range rawLines {
		if isCommentLine(ln) {
			comments = append(comments, [2]int{starts[i], starts[i] + len(ln)})
		}
	}

	var result []LogicalLine

	i := 0
	for i < len(rawLines) {
		base := rawLines[i]
		lineNo := i + 1

		// Build the merged line text and its m2r mapping
		var b strings.Builder
		var m2r []int

		// Append the base line, minus any trailing `# ` comment.
		baseEnd := len(base)
		if !isCommentLine(base) {
			if c := trailingCommentStart(base); c >= 0 {
				comments = append(comments, [2]int{starts[i] + c, starts[i] + len(base)})
				baseEnd = c
			}
		}
		for k := 0; k < baseEnd; k++ {
			b.WriteByte(base[k])
			m2r = append(m2r, starts[i]+k)
		}

		// Merge continuation lines (≥2 leading spaces)
		j := i + 1
		for j < len(rawLines) {
			ln := rawLines[j]
			if len(ln) < 2 || ln[0] != ' ' || ln[1] != ' ' {
				break
			}
			// Continuation line: skip if indented comment
			trimmed := strings.TrimLeft(ln, " ")
			if strings.HasPrefix(trimmed, "#") {
				j++
				continue
			}
			// Insert a single logical space
			indent := len(ln) - len(trimmed)
			if indent < 2 {
				indent = 2
			}
			mapTo := starts[j] + indent
			if mapTo >= starts[j]+len(ln) {
				mapTo = starts[j] + len(ln)
			}
			b.WriteByte(' ')
			m2r = append(m2r, mapTo)

			// Append trimmed continuation text, minus any trailing `# ` comment.
			contEnd := len(trimmed)
			if c := trailingCommentStart(trimmed); c >= 0 {
				comments = append(comments, [2]int{starts[j] + indent + c, starts[j] + len(ln)})
				contEnd = c
			}
			for k := 0; k < contEnd; k++ {
				b.WriteByte(trimmed[k])
				m2r = append(m2r, starts[j]+indent+k)
			}
			j++
		}

		text := b.String()
		stripped := strings.TrimSpace(text)

		// Skip blank lines and full-line comments
		if stripped == "" || isCommentLine(text) {
			i = j
			continue
		}

		result = append(result, LogicalLine{
			Text:     text,
			RawStart: starts[i],
			M2R:      m2r,
			LineNo:   lineNo,
		})
		i = j
	}

	return result, comments, nil
}

// isCommentLine returns true if the line is a full-line comment (optional leading space/tab + #).
func isCommentLine(s string) bool {
	t := strings.TrimLeft(s, " \t")
	return len(t) > 0 && t[0] == '#'
}

// trailingCommentStart returns the byte index of a trailing `#` comment in a
// single physical line, or -1 if there is none. A `#` starts a comment only
// when it sits OUTSIDE double quotes, is preceded by whitespace (or line
// start), and is followed by whitespace or end-of-line. This keeps `#` glued
// to a non-space literal — `Agent #1`, `Event #1`, `C#` — while `e2 ::e # note`
// drops the comment. Quoted text (with `\"` escapes) is never scanned for `#`.
func trailingCommentStart(line string) int {
	inQuote := false
	for k := 0; k < len(line); k++ {
		c := line[k]
		if inQuote {
			if c == '\\' {
				k++ // skip the escaped character
				continue
			}
			if c == '"' {
				inQuote = false
			}
			continue
		}
		switch c {
		case '"':
			inQuote = true
		case '#':
			prevWS := k == 0 || line[k-1] == ' ' || line[k-1] == '\t'
			nextWS := k+1 >= len(line) || line[k+1] == ' ' || line[k+1] == '\t'
			if prevWS && nextWS {
				return k
			}
		}
	}
	return -1
}
