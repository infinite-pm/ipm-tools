package markdown

// CommonMark fenced-code-block delimiter handling, shared by every
// line-based scanner in this repo (ScanIPMTBlocks, mdembed.ScanBlocks,
// mdhtml's renderer, the inline-ipmt scanners).
//
// Why this exists: a ```ipmt fence nested inside a LONGER fence is
// LITERAL TEXT, not a real block —
//
//	````md
//	```ipmt unresolved
//	deploy ::?etc --::X--> safety ::?etc
//	```
//	````
//
// is how docs/ipmt-unresolved.md *shows* the syntax. GitHub and the VS
// Code markdown preview (CommonMark parsers) render it as an example;
// a flat line scanner that treats every ```ipmt line as an opener would
// embed an SVG marker INSIDE the example and corrupt the doc. Every
// scanner must therefore track open fences and only recognize ipmt
// fences (and markers, includes, titles…) at the top level.
//
// The CommonMark rules implemented here (spec §4.5 "Fenced code blocks"):
//   - an opening fence is a run of 3+ backticks or 3+ tildes, optionally
//     followed by an info string; a backtick fence's info string must not
//     contain a backtick (such a line reads as inline code instead — this
//     matters for prose like "A markdown ```` ```ipmt ```` fenced block…"
//     which must NOT open a fence)
//   - the closing fence is a run of AT LEAST as many of the SAME character,
//     with nothing but whitespace after it — so ``` cannot close ````md,
//     and ```text closes nothing
//   - everything between the delimiters is literal
//
// Deliberate deviation: CommonMark allows at most 3 spaces of indentation
// on a fence (4+ makes an indented code block) and un-indents content by
// the opener's indent. We accept ANY leading whitespace instead — ipmt
// fences legitimately sit inside list items at arbitrary indents, and the
// historical scanners were fully indent-lenient. The cost is that a
// fence-looking line inside a 4-space indented code block is treated as a
// fence; ipm docs don't use indented code blocks, fenced examples are the
// convention.

import "strings"

// Fence identifies an open fenced-code-block delimiter: the fence
// character and the length of the opening run. The zero value is not a
// valid fence.
type Fence struct {
	Char byte // '`' or '~'
	Len  int  // characters in the opening run, >= 3
}

// ParseFenceOpen reports whether line opens a fenced code block and, if
// so, returns the fence delimiter and its info string (trimmed; "" for a
// bare fence). See the package comment for the exact rules.
func ParseFenceOpen(line string) (f Fence, info string, ok bool) {
	t := strings.TrimSpace(line)
	if t == "" || (t[0] != '`' && t[0] != '~') {
		return Fence{}, "", false
	}
	c := t[0]
	n := 0
	for n < len(t) && t[n] == c {
		n++
	}
	if n < 3 {
		return Fence{}, "", false
	}
	info = strings.TrimSpace(t[n:])
	if c == '`' && strings.IndexByte(info, '`') >= 0 {
		return Fence{}, "", false // backtick in info: inline code, not a fence
	}
	return Fence{Char: c, Len: n}, info, true
}

// ClosedBy reports whether line closes this fence: a run of at least
// f.Len of f.Char with nothing but whitespace around it.
func (f Fence) ClosedBy(line string) bool {
	t := strings.TrimSpace(line)
	n := 0
	for n < len(t) && t[n] == f.Char {
		n++
	}
	return n >= f.Len && n == len(t)
}

// SkipFence returns the line index just past the fence opened at
// lines[open], i.e. the index after its closing delimiter — or
// len(lines) when the fence never closes (the rest of the document is
// literal fence content).
func SkipFence(lines []string, open int, f Fence) int {
	for j := open + 1; j < len(lines); j++ {
		if f.ClosedBy(lines[j]) {
			return j + 1
		}
	}
	return len(lines)
}

// IsIPMTInfo reports whether a fence info string (as returned by
// ParseFenceOpen) declares the ipmt language: "ipmt" exactly, optionally
// followed by whitespace-separated metadata ("ipmt unresolved"). A
// different language that merely starts with the letters ("ipmtX",
// "ipmt-x") does not match.
func IsIPMTInfo(info string) bool {
	rest, ok := strings.CutPrefix(info, "ipmt")
	return ok && (rest == "" || rest[0] == ' ' || rest[0] == '\t')
}

// FencedByteRanges returns the half-open byte ranges [start,end) of text
// covered by fenced code blocks, delimiter lines included. Text is walked
// as-is (no newline normalization) so the offsets align with the input —
// FindInlineIpmt uses this to drop matches whose marker or code span sits
// inside a fence, where they are literal text. An unterminated fence
// extends to len(text).
func FencedByteRanges(text string) [][2]int {
	var ranges [][2]int
	var open *Fence
	openStart := 0
	pos := 0
	for pos <= len(text) {
		lineEnd := strings.IndexByte(text[pos:], '\n')
		var next int
		var line string
		if lineEnd == -1 {
			line = text[pos:]
			next = len(text) + 1 // past the loop condition
		} else {
			line = text[pos : pos+lineEnd]
			next = pos + lineEnd + 1
		}
		if open != nil {
			if open.ClosedBy(line) {
				end := pos + len(line)
				ranges = append(ranges, [2]int{openStart, end})
				open = nil
			}
		} else if f, _, ok := ParseFenceOpen(line); ok {
			open = &f
			openStart = pos
		}
		pos = next
	}
	if open != nil {
		ranges = append(ranges, [2]int{openStart, len(text)})
	}
	return ranges
}
