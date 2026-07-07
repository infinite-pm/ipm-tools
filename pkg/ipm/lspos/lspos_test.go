package lspos

import "testing"

func TestOffsetToUTF16_ascii(t *testing.T) {
	m := NewMapper("hello\nworld\n")
	cases := []struct {
		off      int
		wantLine uint32
		wantCol  uint32
	}{
		{0, 0, 0},   // 'h'
		{4, 0, 4},   // 'o'
		{5, 0, 5},   // '\n' on line 0
		{6, 1, 0},   // 'w' on line 1
		{10, 1, 4},  // 'd'
		{11, 1, 5},  // '\n' on line 1
		{12, 2, 0},  // EOF, on a virtual line 2
		{100, 2, 0}, // past EOF, clamped
	}
	for _, tc := range cases {
		l, c := m.OffsetToUTF16(tc.off)
		if l != tc.wantLine || c != tc.wantCol {
			t.Errorf("OffsetToUTF16(%d) = (%d,%d), want (%d,%d)",
				tc.off, l, c, tc.wantLine, tc.wantCol)
		}
	}
}

func TestOffsetToUTF16_multibyte(t *testing.T) {
	// "é" is 2 bytes UTF-8 (0xC3 0xA9), 1 UTF-16 code unit.
	// "🦀" is 4 bytes UTF-8, 2 UTF-16 code units (surrogate pair).
	m := NewMapper("a\xc3\xa9b\xf0\x9f\xa6\x80c") // "aébc with crab between b and c", but here "aéb🦀c"

	// Byte offsets:
	//  0  'a' (1 byte) -> col 0
	//  1  'é' (2 bytes) -> col 1
	//  3  'b' (1 byte) -> col 2
	//  4  '🦀' (4 bytes) -> col 3
	//  8  'c' (1 byte) -> col 5  (5 = 3 + 2 from surrogate pair)

	cases := []struct {
		off, wantCol int
	}{
		{0, 0},
		{1, 1}, // start of 'é'
		{3, 2}, // start of 'b'
		{4, 3}, // start of '🦀'
		{8, 5}, // start of 'c' (🦀 took 2 UTF-16 units)
		{9, 6}, // EOF
	}
	for _, tc := range cases {
		_, col := m.OffsetToUTF16(tc.off)
		if int(col) != tc.wantCol {
			t.Errorf("OffsetToUTF16(%d) col = %d, want %d", tc.off, col, tc.wantCol)
		}
	}
}

func TestLineColUTF16ToOffset_roundTrip(t *testing.T) {
	texts := []string{
		"hello\nworld\n",
		"a\xc3\xa9b\xf0\x9f\xa6\x80c\n", // multibyte mix
		"single line",
		"",
		"\n\n\n",
	}
	for _, text := range texts {
		m := NewMapper(text)
		// For every byte offset that lies at the start of a rune,
		// offset -> (line, col) -> offset must round-trip.
		for off := 0; off <= len(text); {
			l, c := m.OffsetToUTF16(off)
			gotOff := m.LineColUTF16ToOffset(l, c)
			if gotOff != off {
				t.Errorf("round-trip(%q, off=%d): (%d,%d) -> %d", text, off, l, c, gotOff)
			}
			// Advance to next rune boundary.
			if off >= len(text) {
				break
			}
			r, size := decodeRune(text, off)
			_ = r
			if size == 0 {
				size = 1
			}
			off += size
		}
	}
}

func TestLineColUTF16ToOffset_pastEOL(t *testing.T) {
	// Asking for a column past the end of a line should clamp to the
	// line's last byte (the newline, or EOF).
	m := NewMapper("hi\nworld\n")
	// Line 0 = "hi" (length 2). Asking for col 100 should return offset 2.
	if got := m.LineColUTF16ToOffset(0, 100); got != 2 {
		t.Errorf("clamp past EOL: got %d, want 2", got)
	}
	// Line 1 = "world" (5 chars). col 100 -> offset 8 (the newline).
	if got := m.LineColUTF16ToOffset(1, 100); got != 8 {
		t.Errorf("clamp past EOL (line 1): got %d, want 8", got)
	}
}

func TestOffsetRangeToUTF16(t *testing.T) {
	m := NewMapper("abc\ndef\n")
	sl, sc, el, ec := m.OffsetRangeToUTF16(1, 6)
	// 1 -> (line 0, col 1); 6 -> (line 1, col 2)
	if sl != 0 || sc != 1 || el != 1 || ec != 2 {
		t.Errorf("range = (%d,%d)-(%d,%d), want (0,1)-(1,2)", sl, sc, el, ec)
	}
}
