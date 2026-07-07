package ipmtokens

import "testing"

// TestFlatten_lastWinsOnOverlap pins the load-bearing resolution rule: when
// two tokens share UTF-16 columns, the one emitted LATER overwrites the
// earlier one on the shared columns. Output must be sorted and disjoint.
func TestFlatten_lastWinsOnOverlap(t *testing.T) {
	// A (event) covers cols 0..5; B (thing) covers cols 3..8 and is emitted
	// second, so it must own the shared cols 3,4.
	in := []Token{
		{Line: 0, StartCol: 0, Length: 5, Type: TypEvent},
		{Line: 0, StartCol: 3, Length: 5, Type: TypThing},
	}
	got := Flatten(in)
	want := []Token{
		{Line: 0, StartCol: 0, Length: 3, Type: TypEvent},
		{Line: 0, StartCol: 3, Length: 5, Type: TypThing},
	}
	assertTokensEqual(t, got, want)
}

// TestFlatten_multiLineSorted checks rows are emitted in ascending line order
// regardless of input order, and that within a row distinct runs are split.
func TestFlatten_multiLineSorted(t *testing.T) {
	in := []Token{
		{Line: 2, StartCol: 0, Length: 2, Type: TypConcept},
		{Line: 0, StartCol: 0, Length: 2, Type: TypEvent},
		{Line: 0, StartCol: 2, Length: 2, Type: TypThing},
	}
	got := Flatten(in)
	want := []Token{
		{Line: 0, StartCol: 0, Length: 2, Type: TypEvent},
		{Line: 0, StartCol: 2, Length: 2, Type: TypThing},
		{Line: 2, StartCol: 0, Length: 2, Type: TypConcept},
	}
	assertTokensEqual(t, got, want)
}

// TestFlatten_skipsEmptyClass verifies that a token whose type maps to no CSS
// class (ClassSuffix == "") is dropped and never paints over a colored token.
func TestFlatten_skipsEmptyClass(t *testing.T) {
	const noClassType uint32 = 999 // out of range → TypeName == "" → ClassSuffix == ""
	if ClassSuffix(TypeName(noClassType), 0) != "" {
		t.Fatalf("precondition: type %d should map to empty class", noClassType)
	}
	in := []Token{
		{Line: 0, StartCol: 0, Length: 4, Type: TypEvent},
		{Line: 0, StartCol: 1, Length: 2, Type: noClassType}, // must NOT overwrite
	}
	got := Flatten(in)
	want := []Token{
		{Line: 0, StartCol: 0, Length: 4, Type: TypEvent},
	}
	assertTokensEqual(t, got, want)
}

// TestFlatten_emptyInput returns nil for no tokens.
func TestFlatten_emptyInput(t *testing.T) {
	if got := Flatten(nil); got != nil {
		t.Errorf("Flatten(nil) = %v, want nil", got)
	}
}

// TestCollect_lineOffsetIsApplied covers the lineOffset!=0 path (every other
// Collect test passes 0): the emitted Line must carry the block's offset.
func TestCollect_lineOffsetIsApplied(t *testing.T) {
	src := "x ::e\n"
	const offset uint32 = 5
	base := Collect(src, src, 0)
	shifted := Collect(src, src, offset)
	if len(shifted) == 0 || len(shifted) != len(base) {
		t.Fatalf("token count mismatch: base=%d shifted=%d", len(base), len(shifted))
	}
	for i := range base {
		if shifted[i].Line != base[i].Line+offset {
			t.Errorf("token %d: shifted Line=%d, want %d (base %d + offset %d)",
				i, shifted[i].Line, base[i].Line+offset, base[i].Line, offset)
		}
		// All other fields must be identical.
		if shifted[i].StartCol != base[i].StartCol || shifted[i].Length != base[i].Length ||
			shifted[i].Type != base[i].Type || shifted[i].Mods != base[i].Mods {
			t.Errorf("token %d: offset changed a non-Line field: base=%+v shifted=%+v",
				i, base[i], shifted[i])
		}
	}
}

func assertTokensEqual(t *testing.T, got, want []Token) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("token count: got %d %+v, want %d %+v", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("token %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}
