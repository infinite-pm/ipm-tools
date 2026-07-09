// Package mdhtml exports `.md` → `.html` for ipmt-rich docs.
//
// This file implements the syntax-highlighting half: given an ipmt
// block's source text and the LSP semantic tokens for it, emit the
// same `<span class="ipm-…">` HTML markup the VS Code markdown preview
// emits via previewHighlight.ts:renderFromLspTokens.
//
// Cross-language parity is enforced by:
//   - The (tokenType, modifierBit) → cssClassSuffix table comes from the
//     palette source of truth (pkg/ipmtokens/palette.json, via
//     ipmtokens.ClassSuffix); drift between languages at the data layer
//     is structurally impossible.
//   - A shared golden-fixture corpus (testdata/highlight-cases/*.json)
//     that BOTH this Go highlighter and the TS renderFromLspTokens are
//     asserted byte-equal against. Algorithmic drift is detected by
//     test, not by inspection.
package mdhtml

import (
	"sort"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/infinite-pm/ipm-tools/pkg/ipmtokens"
)

// HighlightTokens converts (source text, LSP tokens) into the
// `<pre><code class="language-ipmt">…</code></pre>` HTML body the
// markdown preview emits. Overlap resolution is LAST-WINS PER
// CHARACTER, matching previewHighlight.ts. Class names come from
// ipmtokens.ClassSuffix (pkg/ipmtokens/palette.json) — the same source
// that produces the preview's CSS.
func HighlightTokens(src string, tokens []ipmtokens.Token) string {
	// Token positions (StartCol, Length) and the per-line origin are
	// UTF-16 code units, matching the LSP spec and the TS preview
	// (previewHighlight.ts slices a native-UTF-16 JS string). The Go
	// `src` is UTF-8 bytes, so we build two maps in a single pass:
	//   - lineStartU16[line] = UTF-16 offset (from start of src) of each
	//     line's first code unit, so a token's (line, col) becomes an
	//     absolute UTF-16 offset.
	//   - u16ToByte[u] = byte offset of the UTF-16 code unit at absolute
	//     index u, with a trailing sentinel == len(src) so an exclusive
	//     end column maps cleanly. Both code units of an astral rune map
	//     to that rune's start byte; the next index maps past it.
	// For ASCII (the overwhelmingly common ipmt case) u16ToByte[i]==i, so
	// this is identical to the old byte-indexed path — just also correct
	// for the multi-byte content as-token markers can carry (e.g. `Nₑ`).
	lineStartU16 := []int{0}
	u16ToByte := make([]int, 0, len(src)+1)
	byteOff := 0
	u16Count := 0
	for byteOff < len(src) {
		r, size := utf8.DecodeRuneInString(src[byteOff:])
		n := utf16.RuneLen(r)
		if n < 1 {
			n = 1 // invalid rune (RuneError) → treat as one unit
		}
		for k := 0; k < n; k++ {
			u16ToByte = append(u16ToByte, byteOff)
		}
		byteOff += size
		u16Count += n
		if r == '\n' {
			lineStartU16 = append(lineStartU16, u16Count)
		}
	}
	u16ToByte = append(u16ToByte, len(src)) // sentinel for an exclusive end

	// classAt[i] = winning CSS class suffix for byte i. Empty string
	// means "no token covers this position." Tokens are processed in
	// input order; later tokens overwrite earlier ones at the same
	// position (last-wins per character).
	classAt := make([]string, len(src))
	for _, t := range tokens {
		suffix := ipmtokens.ClassSuffix(ipmtokens.TypeName(t.Type), t.Mods)
		if suffix == "" {
			continue
		}
		if int(t.Line) >= len(lineStartU16) {
			continue
		}
		startU16 := lineStartU16[t.Line] + int(t.StartCol)
		endU16 := startU16 + int(t.Length)
		// startU16 is always >= 0 (Line/StartCol are uint32 and lineStartU16
		// holds non-negative offsets); only the upper bound needs guarding.
		if endU16 >= len(u16ToByte) {
			continue
		}
		start := u16ToByte[startU16]
		end := u16ToByte[endU16]
		if end <= start || end > len(src) {
			continue
		}
		for i := start; i < end; i++ {
			classAt[i] = suffix
		}
	}

	// Walk classAt, emitting runs of same-class characters as one
	// <span>. Gaps (classAt[i] == "") render as bare HTML-escaped text.
	var b strings.Builder
	runStart := 0
	var runClass string
	if len(classAt) > 0 {
		runClass = classAt[0]
	}
	for i := 1; i < len(src); i++ {
		if classAt[i] != runClass {
			emitRun(&b, src[runStart:i], runClass)
			runStart = i
			runClass = classAt[i]
		}
	}
	if runStart < len(src) {
		emitRun(&b, src[runStart:], runClass)
	}
	return b.String()
}

func emitRun(b *strings.Builder, text, class string) {
	if class == "" {
		b.WriteString(escapeHTML(text))
		return
	}
	b.WriteString(`<span class="ipm-`)
	b.WriteString(class)
	b.WriteString(`">`)
	b.WriteString(escapeHTML(text))
	b.WriteString(`</span>`)
}

// escapeHTML matches the small set of replacements
// previewHighlight.ts:escapeHtml performs. Keep them in lock-step.
func escapeHTML(s string) string {
	repl := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&#039;",
	)
	return repl.Replace(s)
}

// SortedTokensCopy returns a copy of `tokens` sorted by (line, col)
// with stable ordering for equal positions. Useful for deterministic
// output and for tests that need to compare token streams across
// languages. Not used by HighlightTokens itself — last-wins doesn't
// require sorted input — but exported for callers that produce
// tokens in arbitrary order.
func SortedTokensCopy(tokens []ipmtokens.Token) []ipmtokens.Token {
	out := make([]ipmtokens.Token, len(tokens))
	copy(out, tokens)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Line != out[j].Line {
			return out[i].Line < out[j].Line
		}
		return out[i].StartCol < out[j].StartCol
	})
	return out
}
