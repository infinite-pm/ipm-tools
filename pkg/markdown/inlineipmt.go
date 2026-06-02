package markdown

import (
	"regexp"
	"strings"
)

// InlineIpmt describes one inline ipmt marker in a markdown source: an
// HTML-comment marker immediately before a backtick-delimited inline
// code span. Two marker forms:
//
//	`<!--ipmt-->` `t-shirt ::c`            ← bare: tokenize the code as ipmt
//	`<!--ipmt:as-token:e-marker-->` `::e`  ← paint the code in style NAME
//
// The marker MUST come before the code. Whitespace (including tabs and
// newlines) between marker and the first backtick is tolerated; no other
// token may appear. The marker is case-insensitive on `ipmt`/`as-token`;
// the NAME is taken verbatim (case-sensitive, matching the palette
// class suffix — e.g. `L`, `e-marker`, `type-marker`).
type InlineIpmt struct {
	// Byte offsets in the source markdown:
	MarkerStart int // start of the `<!--…-->` comment
	MarkerEnd   int // end (exclusive) of the comment
	CodeStart   int // start of the opening backtick run
	CodeEnd     int // end (exclusive) of the closing backtick run
	// ContentStart..ContentEnd is the inline-code content between the
	// opening and closing backtick runs.
	ContentStart int
	ContentEnd   int
	Code         string // the content between the backticks (== src[ContentStart:ContentEnd])

	// AsToken, when non-empty, is the NAME from an
	// `<!--ipmt:as-token:NAME-->` marker — a palette class suffix
	// (`e-marker`, `L`, `type-marker`, …). The whole visible Code is
	// painted in that style; Code is NOT parsed as ipmt. Empty means a
	// bare `<!--ipmt-->` marker (tokenize Code as a valid fragment).
	AsToken string
}

// inlineIpmtMarkerRE matches the two marker forms, case-insensitively on
// the `ipmt`/`as-token` keywords, with whitespace tolerated inside the
// comment. Group 1 captures the as-token NAME (verbatim), empty for the
// bare form.
//
// The OLD `cut=`/`as=`/`from=`/`pick=` attribute syntax does NOT match
// (no backward compatibility): such a comment is left as an ordinary,
// invisible HTML comment.
var inlineIpmtMarkerRE = regexp.MustCompile(`(?i)<!--\s*ipmt(?::as-token:([A-Za-z0-9-]+))?\s*-->`)

// LineStartInlineIpmt returns the 1-based numbers of lines whose first
// non-space content is an inline ipmt marker comment (`<!--ipmt-->` or
// `<!--ipmt:as-token:NAME-->`), with at most 3 spaces of indentation and
// outside any fenced code block.
//
// Such a line renders BROKEN in static HTML and the VS Code preview
// alike: CommonMark parses a line beginning (≤3 spaces indent) with
// `<!--` as an HTML block (type 2, comment), so the markdown engine
// passes the WHOLE line through verbatim — the adjacent backtick code
// span never becomes inline `<code>`, and the inline-ipmt rewrite (which
// keys on `<!--…--><code>`) never fires. Marker and code span both leak
// out raw.
//
// Fix in the source: put at least one non-marker character before the
// first marker on the line — lead with a word, or let table/list syntax
// occupy column 0 (a `| <!--ipmt…` table cell is inline, so it's fine).
//
// Limitation: markers nested inside a blockquote (`> <!--ipmt…`) are NOT
// detected here — `>` sits at column 0 — yet they break the same way.
// Callers that care can strip blockquote markers before calling.
func LineStartInlineIpmt(text string) []int {
	lines := strings.Split(NormalizeLF(text), "\n")
	var bad []int
	// Length-aware fence tracking (fence.go): a marker line inside ANY
	// fenced block is literal text. A naive any-```-line toggle would
	// desync on nested examples (````md containing ```ipmt).
	var open *Fence
	for i, line := range lines {
		if open != nil {
			if open.ClosedBy(line) {
				open = nil
			}
			continue
		}
		if f, _, ok := ParseFenceOpen(line); ok {
			open = &f
			continue
		}
		n := 0
		for n < len(line) && line[n] == ' ' {
			n++
		}
		if n > 3 {
			continue // 4+ spaces → indented code block, not an HTML block
		}
		if loc := inlineIpmtMarkerRE.FindStringIndex(line[n:]); loc != nil && loc[0] == 0 {
			bad = append(bad, i+1)
		}
	}
	return bad
}

// FindInlineIpmt scans `text` for inline ipmt markers and the adjacent
// backtick-delimited code spans they apply to. Returns all matches in
// source order.
//
// Recognition: the marker matches inlineIpmtMarkerRE; after it, only
// whitespace may appear before the opening backtick run. The opening run
// is N backticks; the closing run must be exactly N (CommonMark inline
// code). A match whose marker sits INSIDE a fenced code block — e.g. a
// ```md example showing the marker syntax — is literal text and is
// skipped, as is a marker whose code span would cross into a fence (a
// marker immediately above a ``` opener would otherwise swallow the
// fence delimiter as its "inline code").
//
// Shared between cmd/ipm-rpc (inline tokens in ipm.embedBuffer) and
// pkg/mdhtml (static-HTML output). Single source of truth for the marker
// grammar lives HERE.
func FindInlineIpmt(text string) []InlineIpmt {
	matches := inlineIpmtMarkerRE.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return nil
	}
	fenced := FencedByteRanges(text)
	inFence := func(from, to int) bool {
		for _, r := range fenced {
			if from < r[1] && to > r[0] {
				return true
			}
		}
		return false
	}
	out := make([]InlineIpmt, 0, len(matches))
	for _, m := range matches {
		markerStart, markerEnd := m[0], m[1]
		if inFence(markerStart, markerEnd) {
			continue
		}
		asToken := ""
		if m[2] >= 0 {
			asToken = text[m[2]:m[3]]
		}
		codeStart, codeEnd, contentStart, contentEnd, ok := scanInlineCodeAfter(text, markerEnd)
		if !ok {
			continue
		}
		if inFence(codeStart, codeEnd) {
			continue // the "code span" ran into a fence delimiter — not inline code
		}
		out = append(out, InlineIpmt{
			MarkerStart:  markerStart,
			MarkerEnd:    markerEnd,
			CodeStart:    codeStart,
			CodeEnd:      codeEnd,
			ContentStart: contentStart,
			ContentEnd:   contentEnd,
			Code:         text[contentStart:contentEnd],
			AsToken:      asToken,
		})
	}
	return out
}

// scanInlineCodeAfter looks at text starting at `from` and finds the
// next backtick-delimited inline code span, allowing only whitespace
// between `from` and the opening backtick run. Returns the absolute
// positions:
//   - codeStart, codeEnd: include the opening + closing backtick runs
//   - contentStart, contentEnd: just the content between them
//
// CommonMark-style: the opening run is N backticks, the closing run
// must be exactly N backticks.
func scanInlineCodeAfter(text string, from int) (codeStart, codeEnd, contentStart, contentEnd int, ok bool) {
	i := from
	for i < len(text) {
		c := text[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			i++
			continue
		}
		break
	}
	if i >= len(text) || text[i] != '`' {
		return 0, 0, 0, 0, false
	}
	codeStart = i
	// Count opening backticks.
	openTicks := 0
	for i < len(text) && text[i] == '`' {
		openTicks++
		i++
	}
	contentStart = i
	// Find a matching closing run of EXACTLY openTicks backticks.
	for i < len(text) {
		if text[i] != '`' {
			i++
			continue
		}
		closeStart := i
		runLen := 0
		for i < len(text) && text[i] == '`' {
			runLen++
			i++
		}
		if runLen == openTicks {
			return codeStart, i, contentStart, closeStart, true
		}
		// Wrong length — keep scanning past this run.
	}
	return 0, 0, 0, 0, false
}
