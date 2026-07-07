// Package lspos translates between three coordinate systems used in
// language-server work:
//
//	Byte offset       — Go-native; what slices and most pkg/ipm/* APIs use.
//	UTF-16 (line,col) — what the LSP protocol uses on the wire.
//	UTF-8 (line,col)  — convenient for some terminals / logs.
//
// The conversion is non-trivial for documents that contain non-ASCII
// characters: a single rune may take multiple UTF-8 bytes and one or two
// UTF-16 code units. Getting this wrong shows up as off-by-some
// highlighting bugs when node names contain accented letters, emoji, or
// CJK characters.
//
// Inspired by gopls' protocol/mapper.go. The shape is intentionally
// smaller; we only implement the conversions LSP method handlers
// actually call, not the full mapper surface gopls maintains.
package lspos

import (
	"strings"
	"unicode/utf16"
)

// Mapper precomputes the index needed to translate between byte offsets and
// (line, column) positions for a single document body. Build one per
// document version; throw it away on the next didChange and rebuild.
//
// Mapper is read-only after construction; safe to share across goroutines.
type Mapper struct {
	text string

	// lineStarts[i] is the byte offset of the first byte of line i (0-based).
	// lineStarts[0] is always 0; lineStarts[len-1] is len(text) when the
	// document ends with a newline, or the offset of the last line's start
	// otherwise.
	lineStarts []int
}

// NewMapper builds a Mapper for the given document body.
func NewMapper(text string) *Mapper {
	starts := []int{0}
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			starts = append(starts, i+1)
		}
	}
	return &Mapper{text: text, lineStarts: starts}
}

// LineCount returns the number of lines in the document (1 for "no trailing
// newline" docs; 2 for "foo\n", etc.). Convenience for capability advertise.
func (m *Mapper) LineCount() int { return len(m.lineStarts) }

// OffsetToUTF16 returns the (0-based line, 0-based UTF-16 column) position
// of the given byte offset. If the offset is past EOF it's clamped.
//
// "UTF-16 column" means: how many UTF-16 code units precede this offset on
// the same line. For ASCII text this equals the byte column; for a 4-byte
// emoji it's 2 (one surrogate pair). The LSP protocol expects this.
func (m *Mapper) OffsetToUTF16(off int) (line, col uint32) {
	if off < 0 {
		off = 0
	}
	if off > len(m.text) {
		off = len(m.text)
	}
	li := findLine(m.lineStarts, off)
	lineStart := m.lineStarts[li]
	col = utf16Width(m.text[lineStart:off])
	return uint32(li), col
}

// OffsetRangeToUTF16 is the natural extension to a [start, end) byte range.
func (m *Mapper) OffsetRangeToUTF16(start, end int) (startLine, startCol, endLine, endCol uint32) {
	sl, sc := m.OffsetToUTF16(start)
	el, ec := m.OffsetToUTF16(end)
	return sl, sc, el, ec
}

// LineColUTF16ToOffset is the inverse: given an LSP (line, col) position
// (both UTF-16, 0-based), return the byte offset into the document.
//
// Useful when an LSP client tells us "the cursor is at line 12, character
// 5" and we need to look up which AST node sits there.
//
// If the line is past the last line, returns len(text). If the column is
// past end-of-line, returns the offset of the newline (or EOF on the last
// line). Either way the returned offset is in [0, len(text)].
func (m *Mapper) LineColUTF16ToOffset(line, col uint32) int {
	if int(line) >= len(m.lineStarts) {
		return len(m.text)
	}
	lineStart := m.lineStarts[line]
	lineEnd := len(m.text)
	if int(line)+1 < len(m.lineStarts) {
		// Exclude the trailing '\n' from the next line's start.
		lineEnd = m.lineStarts[line+1] - 1
	}
	lineText := m.text[lineStart:lineEnd]

	// Walk runes counting UTF-16 code units until we hit `col`.
	pos := 0
	var used uint32 = 0
	for pos < len(lineText) {
		if used >= col {
			break
		}
		r, size := decodeRune(lineText, pos)
		used += utf16RuneWidth(r)
		pos += size
		if used > col {
			// Landed past col mid-character; back up to start of this rune.
			pos -= size
			break
		}
	}
	return lineStart + pos
}

// utf16Width counts UTF-16 code units in s. Each rune contributes 1 unless
// it's outside the BMP, in which case it's a surrogate pair (2 units).
func utf16Width(s string) uint32 {
	var n uint32
	for i := 0; i < len(s); {
		r, size := decodeRune(s, i)
		n += utf16RuneWidth(r)
		i += size
	}
	return n
}

func utf16RuneWidth(r rune) uint32 {
	if r < 0 || r > 0x10FFFF {
		return 1
	}
	r1, _ := utf16.EncodeRune(r)
	if r1 == 0xFFFD {
		// Not a surrogate pair; single code unit.
		return 1
	}
	return 2
}

// decodeRune is a wrapper around utf8.DecodeRuneInString that returns the
// size of the decoded rune in bytes. (Avoids the extra import + makes the
// inline call sites a little terser.)
func decodeRune(s string, off int) (r rune, size int) {
	return utf8DecodeRuneInString(s[off:])
}

// findLine returns the largest i with lineStarts[i] <= off. A simple linear
// scan is adequate at our document sizes; lineStarts is sorted, so this
// could become an O(log n) sort.Search if larger documents ever need it
// (gopls switches to a sort.Search at ~1k lines).
func findLine(lineStarts []int, off int) int {
	// Linear scan from the end. O(n) in the worst case (offsets near the top
	// of the file walk most of the slice); fine for the sizes we handle.
	for i := len(lineStarts) - 1; i >= 0; i-- {
		if lineStarts[i] <= off {
			return i
		}
	}
	return 0
}

// utf8DecodeRuneInString inlines unicode/utf8.DecodeRuneInString without
// pulling in the import. (Mostly a style choice; the import would also be
// fine.)
func utf8DecodeRuneInString(s string) (rune, int) {
	if s == "" {
		return 0, 0
	}
	b := s[0]
	if b < 0x80 {
		return rune(b), 1
	}
	// Defer to strings.Reader for multi-byte — we just need correctness.
	r := strings.NewReader(s)
	rn, sz, err := r.ReadRune()
	if err != nil {
		return 0xFFFD, 1
	}
	return rn, sz
}
