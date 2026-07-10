package main

import (
	"sort"
	"strings"
	"testing"

	lsp "go.lsp.dev/protocol"

	"github.com/infinite-pm/ipm-tools/pkg/ipmtokens"
)

// runSemanticTokens calls semanticTokensFull and returns the decoded tokens
// (back into absolute coordinates) for easier assertions.
func runSemanticTokens(t *testing.T, languageID, body string) []ipmtokens.Token {
	t.Helper()
	srv := newCmdTestServer(t)
	uri := lsp.DocumentURI("file:///tmp/x")
	srv.docs[uri] = &document{
		uri:      uri,
		language: languageID,
		text:     body,
		version:  1,
	}
	res, err := srv.semanticTokensFull(t.Context(), &lsp.SemanticTokensParams{
		TextDocument: lsp.TextDocumentIdentifier{URI: uri},
	})
	if err != nil {
		t.Fatal(err)
	}
	return decodeSemanticTokens(res.Data)
}

// decodeSemanticTokens is the inverse of encodeSemanticTokens — handy for
// tests that want to inspect tokens by absolute position.
func decodeSemanticTokens(data []uint32) []ipmtokens.Token {
	var out []ipmtokens.Token
	var line, col uint32
	for i := 0; i+5 <= len(data); i += 5 {
		dl, dc := data[i], data[i+1]
		if dl != 0 {
			line += dl
			col = dc
		} else {
			col += dc
		}
		out = append(out, ipmtokens.Token{
			Line:     line,
			StartCol: col,
			Length:   data[i+2],
			Type:     data[i+3],
			Mods:     data[i+4],
		})
	}
	return out
}

func TestSemanticTokens_eventThingConcept(t *testing.T) {
	// Three nodes, one of each kind, plus two arrows between them.
	// We also emit a token per kind marker (`::e`, `::t`, `::c`) so the
	// editor and preview color those positions consistently.
	// The thing `bob` is the source of both edges (part-of to the event,
	// expresses to the concept), so the first arrow is written reversed
	// (`<--`): `bob ::t --> Alice ::e` is the rejected event→thing form.
	body := "Alice ::e <-- bob ::t --> idea ::c\n"
	got := runSemanticTokens(t, "ipmt", body)
	// Expect: 3 node-name tokens + 3 kind-marker tokens + 2 arrow tokens = 8.
	if len(got) != 8 {
		t.Fatalf("want 8 tokens (3 names + 3 ::kind markers + 2 arrows); got %d: %+v", len(got), got)
	}
	wantTypes := []uint32{
		ipmtokens.TypEvent,   // Alice
		ipmtokens.TypEvent,   // ::e
		ipmtokens.TypArrow,   // <--
		ipmtokens.TypThing,   // bob
		ipmtokens.TypThing,   // ::t
		ipmtokens.TypArrow,   // -->
		ipmtokens.TypConcept, // idea
		ipmtokens.TypConcept, // ::c
	}
	for i, tk := range got {
		if tk.Type != wantTypes[i] {
			t.Errorf("token %d type = %d, want %d (%s)",
				i, tk.Type, wantTypes[i], ipmtokens.SemanticTokenTypes[wantTypes[i]])
		}
		if tk.Line != 0 {
			t.Errorf("token %d line = %d, want 0", i, tk.Line)
		}
	}
}

func TestSemanticTokens_arrowSplitsAroundRelationMarker(t *testing.T) {
	// `<--::P--` is one edge in the parser, but for highlighting we
	// emit TWO ipmArrow tokens (`<--` and `--`) so the `::P` marker
	// can carry its own non-bold style without an overlap-resolution
	// trick. Surrounding the relation marker with arrow tokens (rather
	// than a single span that engulfs it) keeps the editor pane's
	// font-attribute layering from spilling bold onto `::P`.
	body := "a ::e <--::P-- b ::e\n"
	got := runSemanticTokens(t, "ipmt", body)
	var arrows []ipmtokens.Token
	var relations []ipmtokens.Token
	for _, tk := range got {
		switch tk.Type {
		case ipmtokens.TypArrow:
			arrows = append(arrows, tk)
		case ipmtokens.TypRelation:
			relations = append(relations, tk)
		}
	}
	if len(arrows) != 2 {
		t.Fatalf("want 2 arrow tokens (split around ::P), got %d: %+v", len(arrows), arrows)
	}
	if len(relations) != 1 {
		t.Fatalf("want 1 relation token (::P), got %d: %+v", len(relations), relations)
	}
	// Sanity-check the two arrow tokens occupy `<--` and `--` (and
	// nothing else — no leading whitespace, no overlap with ::P).
	for _, a := range arrows {
		txt := body[a.StartCol : a.StartCol+a.Length]
		for i := 0; i < len(txt); i++ {
			c := txt[i]
			if c != '-' && c != '<' && c != '>' {
				t.Errorf("arrow token contains non-symbol char %q at col=%d: %q",
					c, a.StartCol+uint32(i), txt)
			}
		}
	}
}

func TestSemanticTokens_arrowSplitsAroundEdgeTooltip(t *testing.T) {
	// `--"causes"-->` — the tooltip sits inside the arrow span, but
	// ipmArrow should be emitted only on the bracketing `--` runs so
	// the ipmTooltip token isn't overlapped.
	body := `a ::e --"causes"--> b ::e` + "\n"
	got := runSemanticTokens(t, "ipmt", body)
	var arrows []ipmtokens.Token
	for _, tk := range got {
		if tk.Type == ipmtokens.TypArrow {
			arrows = append(arrows, tk)
		}
	}
	if len(arrows) != 2 {
		t.Fatalf("want 2 arrow tokens (split around tooltip), got %d: %+v", len(arrows), arrows)
	}
	for _, a := range arrows {
		txt := body[a.StartCol : a.StartCol+a.Length]
		for i := 0; i < len(txt); i++ {
			c := txt[i]
			if c != '-' && c != '<' && c != '>' {
				t.Errorf("arrow token contains non-symbol char %q at col=%d: %q",
					c, a.StartCol+uint32(i), txt)
			}
		}
	}
}

func TestSemanticTokens_identifierEdgeLabelGetsTooltipToken(t *testing.T) {
	// `--then-->` is the identifier-form edge label (no quotes). The
	// parser stores `then` in the edge's Tooltip field; the arrow-span
	// walker must emit an ipmTooltip token for the identifier so it
	// doesn't fall through to plain text.
	body := "a ::e --then--> b ::e\n"
	got := runSemanticTokens(t, "ipmt", body)
	var sawTooltip bool
	for _, tk := range got {
		if tk.Type == ipmtokens.TypTooltip {
			text := body[tk.StartCol : tk.StartCol+tk.Length]
			if text == "then" {
				sawTooltip = true
			}
		}
	}
	if !sawTooltip {
		t.Fatalf("expected ipmTooltip for the identifier `then`; got: %+v", got)
	}
}

func TestSemanticTokens_identifierEdgeLabelWithExplicitRelation(t *testing.T) {
	// `<--::P involves--` should produce:
	//   - `<--` ipmArrow.partOf
	//   - `::P` ipmRelation
	//   - `involves` ipmTooltip
	//   - `--` ipmArrow.partOf
	body := "a ::e <--::P involves-- b ::e\n"
	got := runSemanticTokens(t, "ipmt", body)
	var sawRelation, sawTooltip bool
	for _, tk := range got {
		text := body[tk.StartCol : tk.StartCol+tk.Length]
		if tk.Type == ipmtokens.TypRelation && text == "::P" {
			sawRelation = true
		}
		if tk.Type == ipmtokens.TypTooltip && text == "involves" {
			sawTooltip = true
		}
	}
	if !sawRelation {
		t.Errorf("missing ipmRelation for ::P; got: %+v", got)
	}
	if !sawTooltip {
		t.Errorf("missing ipmTooltip for `involves`; got: %+v", got)
	}
}

func TestSemanticTokens_longFormReMentionGetsKindToken(t *testing.T) {
	// `In the library` is declared as a concept on line 1; on line 2
	// it's referenced by long-form name. Without the edge-endpoint
	// pass, the re-mention has no LSP token and renders plain.
	body := "In the library ::c\nIn the library --> elsewhere ::c\n"
	got := runSemanticTokens(t, "ipmt", body)
	var conceptTokens int
	for _, tk := range got {
		if tk.Type != ipmtokens.TypConcept || tk.Mods != 0 {
			continue
		}
		text := body[lineColToOffset(body, int(tk.Line), int(tk.StartCol)) : lineColToOffset(body, int(tk.Line), int(tk.StartCol))+int(tk.Length)]
		if text == "In the library" {
			conceptTokens++
		}
	}
	if conceptTokens < 2 {
		t.Fatalf("want >=2 ipmConcept tokens for `In the library` (decl + re-mention); got %d, all=%+v",
			conceptTokens, got)
	}
}

// lineColToOffset converts a (line, col) pair back to a byte offset in
// `body`. Used by tests that need to read the source slice of a token.
func lineColToOffset(body string, line, col int) int {
	off := 0
	for i := 0; i < line; i++ {
		next := -1
		for j := off; j < len(body); j++ {
			if body[j] == '\n' {
				next = j
				break
			}
		}
		if next < 0 {
			return off
		}
		off = next + 1
	}
	return off + col
}

func TestSemanticTokens_endpointPassPreservesHasAlias(t *testing.T) {
	// `Patrick wears black t-shirt ::e wearB::a` — the long label is
	// the source of an outgoing edge. The endpoint pass MUST NOT
	// emit a modifier-less ipmEvent at the same range, which would
	// clobber the hasAlias modifier set by the node-name pass and
	// render the long label in the full (non-light) kind color.
	body := "Patrick wears black t-shirt ::e wearB::a\n  --> Patrick swaps t-shirt ::e swapT::a\n"
	got := runSemanticTokens(t, "ipmt", body)
	// Find the long-label token for the first node ("Patrick wears
	// black t-shirt", col 0, len 27). The hasAlias bit MUST be set,
	// and no SUBSEQUENT token may overwrite that position with mods=0.
	var firstLong *ipmtokens.Token
	var collide *ipmtokens.Token
	for i := range got {
		tk := got[i]
		if tk.Type != ipmtokens.TypEvent {
			continue
		}
		if tk.Line == 0 && tk.StartCol == 0 && tk.Length == 27 {
			if firstLong == nil {
				firstLong = &got[i]
			} else {
				collide = &got[i]
			}
		}
	}
	if firstLong == nil {
		t.Fatalf("expected an ipmEvent token at line=0 col=0 len=27; got: %+v", got)
	}
	if firstLong.Mods&ipmtokens.ModHasAlias == 0 {
		t.Fatalf("long-label token must carry hasAlias modifier; tokMods=0x%x", firstLong.Mods)
	}
	if collide != nil {
		t.Fatalf("regression: a second ipmEvent token at the same range (mods=0x%x) clobbers the hasAlias modifier set by the node-name pass; full tokens=%+v",
			collide.Mods, got)
	}
}

func TestSemanticTokens_multilineEdgeDoesNotMisclassifyTargetFirstChar(t *testing.T) {
	// `c-X ::c --::N-->` followed by `  c-Y ::c` on the next line.
	// The parser's edge.Arrow range extends to byte offset 20, which
	// overlaps the target node's first byte (`c` of c-Y at offset 19).
	// Before the fix, the arrow-span walker crossed the newline and
	// emitted an ipmTooltip token over that lone `c`. The endpoint
	// pass then emitted an ipmConcept for the whole `c-Y` (3 bytes),
	// but per-character last-wins gave the `c` to ipmTooltip — visible
	// as a yellow `c` next to a blue `-Y` in the preview.
	body := "c-X ::c --::N-->\n  c-Y ::c\n"
	got := runSemanticTokens(t, "ipmt", body)
	for _, tk := range got {
		if tk.Type != ipmtokens.TypTooltip {
			continue
		}
		// Tooltip on line 1 (the continuation line) is the bug:
		// the second line is just the target node, no tooltip syntax.
		if tk.Line == 1 {
			t.Fatalf("regression: arrow-span walker leaked into the continuation line and emitted an ipmTooltip at line=%d col=%d len=%d; expected no tooltip tokens on the target line",
				tk.Line, tk.StartCol, tk.Length)
		}
	}
}

func TestSemanticTokens_legendOrder(t *testing.T) {
	// Guard against accidentally reordering the legend (which is part of
	// the protocol contract once a client has cached it). New entries
	// are appended at the end; existing positions are stable.
	want := []string{
		"ipmEvent", "ipmThing", "ipmConcept",
		"ipmRelation", "ipmArrow", "ipmTypeMarker",
		"ipmComment", "ipmString", "ipmTooltip",
		"ipmUnresolved",
	}
	if strings.Join(ipmtokens.SemanticTokenTypes, ",") != strings.Join(want, ",") {
		t.Fatalf("legend order changed:\n  got  %v\n  want %v", ipmtokens.SemanticTokenTypes, want)
	}
}

func TestSemanticTokens_modifiersLegendOrder(t *testing.T) {
	// Modifiers are a bitmask whose bit positions are stable across versions.
	want := []string{"alias", "leadsTo", "partOf", "expresses", "nearTo", "hasAlias", "marker"}
	if strings.Join(ipmtokens.SemanticTokenModifiers, ",") != strings.Join(want, ",") {
		t.Fatalf("modifier order changed:\n  got  %v\n  want %v", ipmtokens.SemanticTokenModifiers, want)
	}
}

func TestSemanticTokens_longLabelGetsHasAliasWhenAliased(t *testing.T) {
	// Node WITH alias: long label gets ipmtokens.ModHasAlias.
	withAlias := runSemanticTokens(t, "ipmt",
		"Patrick wears black t-shirt ::e wearB::a\n")
	var labelTok *ipmtokens.Token
	for i := range withAlias {
		// Long-label token: ipmEvent type, NO alias bit (that bit is on
		// the alias-identifier token).
		if withAlias[i].Type == ipmtokens.TypEvent && withAlias[i].Mods&ipmtokens.ModAlias == 0 {
			labelTok = &withAlias[i]
			break
		}
	}
	if labelTok == nil {
		t.Fatalf("no long-label ipmEvent token found in: %+v", withAlias)
	}
	if labelTok.Mods&ipmtokens.ModHasAlias == 0 {
		t.Fatalf("long-label token missing hasAlias modifier; tokMods=0x%x got=%+v",
			labelTok.Mods, withAlias)
	}
}

func TestSemanticTokens_longLabelHasNoHasAliasWhenNoAlias(t *testing.T) {
	// Node WITHOUT alias: long-label token must NOT carry hasAlias.
	noAlias := runSemanticTokens(t, "ipmt",
		"Patrick wears black t-shirt ::e\n")
	for _, tk := range noAlias {
		if tk.Type == ipmtokens.TypEvent && tk.Mods&ipmtokens.ModHasAlias != 0 {
			t.Fatalf("no-alias node should not carry hasAlias modifier; got %+v\nall=%+v",
				tk, noAlias)
		}
	}
}

func TestSemanticTokens_markdownInjection_offsetsLines(t *testing.T) {
	// Three blank lines + a fence opener; the first node-name token must
	// land on the line *inside* the fence, not on line 0.
	body := "intro\n\n```ipmt\nAlice ::e\n```\n"
	got := runSemanticTokens(t, "markdown", body)
	// 2 tokens: long-label `Alice` + kind marker `::e`.
	if len(got) != 2 {
		t.Fatalf("want 2 tokens (name + ::e marker); got %d: %+v", len(got), got)
	}
	// Block opens at line 2, content line is line 3.
	if got[0].Line != 3 {
		t.Errorf("token line = %d, want 3 (block content line in whole file)", got[0].Line)
	}
	if got[0].Type != 0 {
		t.Errorf("token type = %d, want 0 (ipmEvent)", got[0].Type)
	}
}

func TestSemanticTokens_emptyDocumentReturnsEmpty(t *testing.T) {
	got := runSemanticTokens(t, "ipmt", "")
	if len(got) != 0 {
		t.Errorf("empty doc should produce no tokens; got %+v", got)
	}
}

func TestSemanticTokens_unknownDocumentReturnsEmpty(t *testing.T) {
	srv := newCmdTestServer(t)
	res, err := srv.semanticTokensFull(t.Context(), &lsp.SemanticTokensParams{
		TextDocument: lsp.TextDocumentIdentifier{URI: lsp.DocumentURI("file:///nothing")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Data) != 0 {
		t.Errorf("unknown doc should produce empty data; got %v", res.Data)
	}
}

func TestSemanticTokens_arrowsByRelation(t *testing.T) {
	// Each arrow carries the modifier bit corresponding to its SST relation.
	cases := []struct {
		body    string
		wantMod uint32
		label   string
	}{
		// event --> event = leads-to
		{"A ::e --> B ::e\n", ipmtokens.ModLeadsTo, "leadsTo"},
		// thing --> event = part-of (containment)
		{"A --> B ::e\n", ipmtokens.ModPartOf, "partOf"},
		// thing --> concept = expresses
		{"A --> B ::c\n", ipmtokens.ModExpresses, "expresses"},
		// thing --- thing = near-to (undirected)
		{"A --- B\n", ipmtokens.ModNearTo, "nearTo"},
	}
	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			got := runSemanticTokens(t, "ipmt", c.body)
			// Find the arrow token (the only ipmArrow in the stream).
			var arrow *ipmtokens.Token
			for i := range got {
				if got[i].Type == ipmtokens.TypArrow {
					arrow = &got[i]
					break
				}
			}
			if arrow == nil {
				t.Fatalf("no ipmArrow token in stream: %+v", got)
			}
			if arrow.Mods != c.wantMod {
				t.Fatalf("arrow modifiers = 0x%x, want 0x%x (%s); tokens=%+v",
					arrow.Mods, c.wantMod, c.label, got)
			}
		})
	}
}

func TestSemanticTokens_aliasGetsKindAndAliasModifier(t *testing.T) {
	body := "Patrick wears black t-shirt ::e wearB::a\n"
	got := runSemanticTokens(t, "ipmt", body)
	// Find a token with the alias modifier set; should be an ipmEvent.
	var aliasTok *ipmtokens.Token
	for i := range got {
		if got[i].Mods&ipmtokens.ModAlias != 0 {
			aliasTok = &got[i]
			break
		}
	}
	if aliasTok == nil {
		t.Fatalf("no token with alias modifier found in: %+v", got)
	}
	if aliasTok.Type != ipmtokens.TypEvent {
		t.Fatalf("alias token type = %d (%s), want %d (ipmEvent); tokens=%+v",
			aliasTok.Type, ipmtokens.SemanticTokenTypes[aliasTok.Type], ipmtokens.TypEvent, got)
	}
}

func TestSemanticTokens_aliasReferencesColored(t *testing.T) {
	// Declaration on line 0/1, references on line 2. All four positions
	// carry ipmEvent + alias: the two declarations, plus the two line-2
	// references. The references are also edge endpoints, but the
	// endpoint-kind pass SKIPS endpoints referenced by the node's alias
	// (alias-wins-at-endpoints) — so after Flatten they keep the alias
	// color instead of being clobbered by the plain kind.
	body := "Patrick wears black t-shirt ::e wearB::a\n" +
		"  --> Patrick swaps t-shirt ::e swapT::a\n" +
		"Patrick --> wearB, swapT\n"
	got := runSemanticTokens(t, "ipmt", body)

	aliasCount := 0
	for _, tk := range got {
		if tk.Mods&ipmtokens.ModAlias == 0 {
			continue
		}
		aliasCount++
		if tk.Type != ipmtokens.TypEvent {
			t.Errorf("alias-modifier token has type %d, want %d (ipmEvent); tk=%+v",
				tk.Type, ipmtokens.TypEvent, tk)
		}
	}
	if aliasCount != 4 {
		t.Fatalf("want 4 alias tokens (2 decls + 2 refs); got %d\nall tokens: %+v", aliasCount, got)
	}
	assertNoOverlap(t, got)
}

// assertNoOverlap verifies the LSP semantic-token stream is
// non-overlapping — the contract Flatten guarantees and the LSP model
// requires. Tokens are single-line; two tokens on the same line must not
// share any column.
func assertNoOverlap(t *testing.T, tokens []ipmtokens.Token) {
	t.Helper()
	byLine := map[uint32][]ipmtokens.Token{}
	for _, tk := range tokens {
		byLine[tk.Line] = append(byLine[tk.Line], tk)
	}
	for line, toks := range byLine {
		sort.Slice(toks, func(i, j int) bool { return toks[i].StartCol < toks[j].StartCol })
		for i := 1; i < len(toks); i++ {
			prevEnd := toks[i-1].StartCol + toks[i-1].Length
			if toks[i].StartCol < prevEnd {
				t.Errorf("overlapping tokens on line %d: %+v ends at col %d but next starts at col %d",
					line, toks[i-1], prevEnd, toks[i].StartCol)
			}
		}
	}
}

func TestSemanticTokens_tooltipQuotedString(t *testing.T) {
	// Edge tooltip between dashes: `--"text"-->`
	body := "A ::e --\"why this matters\"--> B ::e\n"
	got := runSemanticTokens(t, "ipmt", body)
	var tipTok *ipmtokens.Token
	for i := range got {
		if got[i].Type == ipmtokens.TypTooltip {
			tipTok = &got[i]
			break
		}
	}
	if tipTok == nil {
		t.Fatalf("no ipmTooltip token found in: %+v", got)
	}
}

func TestEncodeSemanticTokens_deltaEncoding(t *testing.T) {
	// Two tokens on the same line.
	in := []ipmtokens.Token{
		{Line: 5, StartCol: 3, Length: 4, Type: 0},
		{Line: 5, StartCol: 10, Length: 5, Type: 1},
		{Line: 7, StartCol: 1, Length: 2, Type: 2},
	}
	got := encodeSemanticTokens(in)
	want := []uint32{
		5, 3, 4, 0, 0, // first: deltaLine=5, deltaStart=3 (absolute on new line)
		0, 7, 5, 1, 0, // same line: deltaStart relative (10 - 3 = 7)
		2, 1, 2, 2, 0, // new line 7: deltaLine=2, deltaStart=1 (absolute)
	}
	if !uint32SliceEqual(got, want) {
		t.Errorf("encode mismatch:\n  got  %v\n  want %v", got, want)
	}
}

func uint32SliceEqual(a, b []uint32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
