package ipmtokens

import "testing"

func TestTokensForClass_paintsWholeText(t *testing.T) {
	// as-token:e-marker on the word "(orange)" → one token over all of
	// it, carrying the event type + marker modifier (bold orange).
	toks, ok := TokensForClass("e-marker", "(orange)")
	if !ok {
		t.Fatal("TokensForClass(e-marker) ok=false")
	}
	if len(toks) != 1 {
		t.Fatalf("want 1 token, got %d", len(toks))
	}
	tk := toks[0]
	if tk.StartCol != 0 || tk.Length != 8 { // "(orange)" = 8 UTF-16 units
		t.Errorf("token should span whole text [0,8); got col=%d len=%d", tk.StartCol, tk.Length)
	}
	if TypeName(tk.Type) != "ipmEvent" || tk.Mods&ModMarker == 0 {
		t.Errorf("want ipmEvent+marker; got type=%s mods=%d", TypeName(tk.Type), tk.Mods)
	}
}

func TestTokensForClass_unknownNameAndEmpty(t *testing.T) {
	if _, ok := TokensForClass("nope", "x"); ok {
		t.Error("unknown NAME should return ok=false")
	}
	if _, ok := TokensForClass("e-marker", ""); ok {
		t.Error("empty visible should return ok=false")
	}
}

// TestTokenForClass_roundTripsClassSuffix is the local drift guard: for
// the names a few representative tokens map to, TokenForClass must invert
// ClassSuffix. (The full fixture-based check is the e2e test.)
func TestTokenForClass_roundTripsClassSuffix(t *testing.T) {
	for _, name := range []string{
		"e-title", "e-marker", "e-alias", "e-title-aliased",
		"t-title", "c-marker", "L", "P", "X", "N",
		"type-marker", "relation", "string", "comment", "tooltip",
		"unresolved",
	} {
		typ, mods, ok := TokenForClass(name)
		if !ok {
			t.Errorf("TokenForClass(%q) ok=false", name)
			continue
		}
		if got := ClassSuffix(TypeName(typ), mods); got != name {
			t.Errorf("round-trip %q → (type=%s, mods=%d) → ClassSuffix=%q", name, TypeName(typ), mods, got)
		}
	}
}

// fixtureBlock is a valid all-token ipmt graph that exercises every token
// style.
const fixtureBlock = `# a tiny scene: Patrick changes shirts
"morning" ::e dawn::a
Patrick wears black ::e wearB::a
  --> Patrick swaps it ::e swapT::a
dawn --> noon ::e
dawn --> wearB
black shirt ::t bs::a --::P--> wearB
plain thing ::t --::P--> swapT
wearB --::X--> change ::c chg::a
swapT --::X--> motion ::c
chg --- motion
bs --"during"--> swapT
swapT "a quick move" ::tip
mystery ::?etc`

// TestAsTokenDrift is the drift guard: every class the tokenizer emits on
// the all-token fixture must round-trip through the as-token name maps
// (ClassSuffix → TokenForClass → ClassSuffix), and all 22 named styles
// must appear. If the tokenizer changes such that a class has no name (or
// a name maps to the wrong token), this fails — keeping the hand-exposed
// `as-token` vocabulary honest.
func TestAsTokenDrift(t *testing.T) {
	want := map[string]bool{
		"e-title": false, "e-marker": false, "e-alias": false, "e-title-aliased": false,
		"t-title": false, "t-marker": false, "t-alias": false, "t-title-aliased": false,
		"c-title": false, "c-marker": false, "c-alias": false, "c-title-aliased": false,
		"L": false, "P": false, "X": false, "N": false,
		"type-marker": false, "relation": false, "string": false, "comment": false, "tooltip": false,
		"unresolved": false,
	}
	for _, tk := range Collect(fixtureBlock, fixtureBlock, 0) {
		cls := ClassSuffix(TypeName(tk.Type), tk.Mods)
		if cls == "" {
			t.Errorf("token type=%s mods=%d produced no class suffix", TypeName(tk.Type), tk.Mods)
			continue
		}
		// Round-trip: the as-token NAME for this class must resolve back
		// to the same (type, mods) → same class.
		typ, mods, ok := TokenForClass(cls)
		if !ok {
			t.Errorf("class %q (emitted by the tokenizer) has no TokenForClass entry", cls)
			continue
		}
		if back := ClassSuffix(TypeName(typ), mods); back != cls {
			t.Errorf("round-trip drift: %q → (type=%s, mods=%d) → %q", cls, TypeName(typ), mods, back)
		}
		if _, expected := want[cls]; expected {
			want[cls] = true
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("fixture did not exercise the %q style (coverage gap)", name)
		}
	}
}
