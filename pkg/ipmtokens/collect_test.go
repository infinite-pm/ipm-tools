package ipmtokens

import "testing"

// tokenText returns the source slice a single-line token covers.
func tokenText(src string, tk Token) string {
	lineStarts := []int{0}
	for i := 0; i < len(src); i++ {
		if src[i] == '\n' {
			lineStarts = append(lineStarts, i+1)
		}
	}
	if int(tk.Line) >= len(lineStarts) {
		return ""
	}
	start := lineStarts[tk.Line] + int(tk.StartCol)
	end := start + int(tk.Length)
	if start > len(src) || end > len(src) || start > end {
		return ""
	}
	return src[start:end]
}

// TestCollect_undecidedMarkerPerLetterColors checks that `::?<letters>`
// emits one ipmUnresolved (grey) token for the `::?` prefix and one
// kind-marker token per candidate letter (each colored as its own kind),
// instead of one opaque span over the whole marker. The order in the
// source determines the emission order. The Unresolved-kind title (the
// word before `::?`) also paints with the neutral grey ipmUnresolved
// color and is filtered out here so the per-letter coloring is the
// focus.
func TestCollect_undecidedMarkerPerLetterColors(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []string // class suffix for each non-title token in order
	}{
		{"etc", "mystery ::?etc\n", []string{"unresolved", "e-marker", "t-marker", "c-marker"}},
		{"ce", "u0 ::?ce\n", []string{"unresolved", "c-marker", "e-marker"}},
		{"et", "x ::?et\n", []string{"unresolved", "e-marker", "t-marker"}},
		{"tc", "x ::?tc\n", []string{"unresolved", "t-marker", "c-marker"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got []string
			for _, tk := range Collect(c.src, c.src, 0) {
				cls := ClassSuffix(TypeName(tk.Type), tk.Mods)
				if cls == "" {
					continue
				}
				// Skip the Unresolved-kind title token itself (it paints with
				// the same `unresolved` class as the `::?` prefix marker —
				// distinguish by length, since the title spans the whole
				// node-name word while the prefix is exactly 3 chars).
				if cls == "unresolved" && tk.Length != 3 {
					continue
				}
				got = append(got, cls)
			}
			if len(got) != len(c.want) {
				t.Fatalf("token count: got %d %v, want %d %v", len(got), got, len(c.want), c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Errorf("token %d: got %q, want %q (full=%v)", i, got[i], c.want[i], got)
				}
			}
		})
	}
}

// TestCollect_unresolvedAliasAndEndpointAreColored guards a follow-up to
// the grey-unresolved change: an aliased Unresolved node has no long-title
// span, so the title pass can't paint its alias-only declaration. And
// references to an Unresolved node (alias-refs or edge endpoints) used to
// bail out because nodeTypeToToken returned false for model.Unresolved.
// nodeTypeToTokenAny now routes those emissions to TypUnresolved →
// "unresolved" (grey) too, so every visible mention of an Unresolved node
// gets the neutral grey color (modifiers fall back to the base via
// ClassSuffix).
func TestCollect_unresolvedAliasAndEndpointAreColored(t *testing.T) {
	t.Run("aliased decl", func(t *testing.T) {
		src := "alpha::a ::?etc\n"
		var seenAlphaUnresolved bool
		for _, tk := range Collect(src, src, 0) {
			if tokenText(src, tk) == "alpha" &&
				ClassSuffix(TypeName(tk.Type), tk.Mods) == "unresolved" {
				seenAlphaUnresolved = true
			}
		}
		if !seenAlphaUnresolved {
			t.Errorf("aliased Unresolved decl `alpha::a` must paint `alpha` as `unresolved` (grey); got %+v",
				dumpClasses(src, Collect(src, src, 0)))
		}
	})
	t.Run("endpoint reference", func(t *testing.T) {
		// `beta` is a thing and `alpha` is Unresolved, so the edge must be
		// written thing→event-or-unresolved (`beta --> alpha`); the reverse
		// is a rejected event→thing direction.
		src := "alpha ::?etc\nbeta ::t\nbeta --> alpha\n"
		// On line 2, the trailing `alpha` is the target endpoint — it must be
		// `unresolved` too (the grey applies to every visible reference).
		var seen bool
		for _, tk := range Collect(src, src, 0) {
			if tk.Line == 2 && tokenText(src, tk) == "alpha" &&
				ClassSuffix(TypeName(tk.Type), tk.Mods) == "unresolved" {
				seen = true
			}
		}
		if !seen {
			t.Errorf("endpoint reference to an Unresolved node must paint `unresolved` (grey); got %+v",
				dumpClasses(src, Collect(src, src, 0)))
		}
	})
	t.Run("alias reference (non-endpoint)", func(t *testing.T) {
		// `alpha` is an alias of an Unresolved node; the bare reference on
		// line 2 (subject of a node-tooltip statement, NOT an edge endpoint)
		// should still color grey via the alias-ref pass.
		src := "alpha::a ::?etc\n" +
			"beta ::t\n" +
			`alpha "context note" ::tip` + "\n"
		var seen bool
		for _, tk := range Collect(src, src, 0) {
			if tk.Line == 2 && tokenText(src, tk) == "alpha" &&
				ClassSuffix(TypeName(tk.Type), tk.Mods) == "unresolved" {
				seen = true
			}
		}
		if !seen {
			t.Errorf("non-endpoint alias reference to an Unresolved node must paint `unresolved` (grey); got %+v",
				dumpClasses(src, Collect(src, src, 0)))
		}
	})
}

// dumpClasses returns one "L:col len cls text" line per token, useful for
// failure messages in the tests above.
func dumpClasses(src string, toks []Token) []string {
	out := make([]string, 0, len(toks))
	for _, tk := range toks {
		out = append(out, "L"+itoa(int(tk.Line))+":"+itoa(int(tk.StartCol))+
			" len="+itoa(int(tk.Length))+
			" "+ClassSuffix(TypeName(tk.Type), tk.Mods)+
			" "+tokenText(src, tk))
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// TestCollect_nameSubstringOfPrecedingAlias guards a pre-existing
// NameStart bug surfaced by the parser-positions refactor: when the title
// is a substring of the preceding alias (e.g. "bcd" inside "abcd"), the
// old `strings.Index(text, origName)` matched the substring at offset 1.
// The fix finds origName by exclusion from the already-known lexeme spans,
// so the title token lands at the real `bcd`, not inside `abcd`.
func TestCollect_nameSubstringOfPrecedingAlias(t *testing.T) {
	src := "abcd::a bcd ::e\n"
	var title *Token
	toks := Collect(src, src, 0)
	for i := range toks {
		if toks[i].Type == TypEvent && toks[i].Mods&ModHasAlias != 0 {
			title = &toks[i]
		}
	}
	if title == nil {
		t.Fatalf("no e-title-aliased token; got %+v", toks)
	}
	if title.StartCol != 8 || title.Length != 3 {
		t.Errorf("title col=%d len=%d, want col=8 len=3 (the real `bcd`, not inside `abcd`)",
			title.StartCol, title.Length)
	}
}

// TestCollect_multibyteTitleMarkerPositions checks the byte→UTF-16 mapping:
// `é` is 2 UTF-8 bytes but 1 UTF-16 unit, so the marker after "café " must
// land at UTF-16 col 5 (the LSP coordinate), not byte offset 6.
func TestCollect_multibyteTitleMarkerPositions(t *testing.T) {
	src := "café ::e\n"
	toks := Collect(src, src, 0)
	var title, marker *Token
	for i := range toks {
		switch {
		case toks[i].Type == TypEvent && toks[i].Mods == 0:
			title = &toks[i]
		case toks[i].Type == TypEvent && toks[i].Mods&ModMarker != 0:
			marker = &toks[i]
		}
	}
	if title == nil || title.StartCol != 0 || title.Length != 4 {
		t.Errorf("title token: want col=0 len=4 (UTF-16 'café'); got %+v", title)
	}
	if marker == nil || marker.StartCol != 5 || marker.Length != 3 {
		t.Errorf("marker token: want col=5 len=3 (UTF-16); got %+v", marker)
	}
}

// TestCollect_escapedQuotesInEdgeTooltip exercises the arrow-walk's quoted
// scan with `\"` escapes — the whole tooltip is one token.
func TestCollect_escapedQuotesInEdgeTooltip(t *testing.T) {
	src := `a ::e --"says \"hi\""--> b ::e` + "\n"
	var tips []string
	for _, tk := range Collect(src, src, 0) {
		if tk.Type == TypTooltip {
			tips = append(tips, tokenText(src, tk))
		}
	}
	want := `"says \"hi\""`
	if len(tips) != 1 || tips[0] != want {
		t.Errorf("want one tooltip %q; got %v", want, tips)
	}
}

// TestCollect_commaSeparatedSpecMarkers checks per-part offset handling in
// parseNodeSpecs: each comma-separated spec's marker is positioned right.
func TestCollect_commaSeparatedSpecMarkers(t *testing.T) {
	src := "x ::e, y ::c\n"
	eMarker, cMarker := false, false
	for _, tk := range Collect(src, src, 0) {
		txt := tokenText(src, tk)
		if txt == "::e" && tk.Type == TypEvent && tk.Mods&ModMarker != 0 {
			eMarker = true
		}
		if txt == "::c" && tk.Type == TypConcept && tk.Mods&ModMarker != 0 {
			cMarker = true
		}
	}
	if !eMarker || !cMarker {
		t.Errorf("want e-marker and c-marker from comma specs; eMarker=%v cMarker=%v", eMarker, cMarker)
	}
}

// TestCollect_typeMarkersInCommentAreNotMarkers extends the context-blindness
// guard to `::a`/`::tip`/`::t` inside a comment.
func TestCollect_typeMarkersInCommentAreNotMarkers(t *testing.T) {
	src := "# mentions ::a and ::tip and ::t here\nx ::e\n"
	for _, tk := range Collect(src, src, 0) {
		if tk.Line == 0 && (tk.Type == TypTypeMarker || tk.Mods&ModMarker != 0) {
			t.Errorf("a marker was emitted inside the comment line: %q", tokenText(src, tk))
		}
	}
}

// TestCollect_trailingComment checks that a `# ` trailing comment is emitted
// as a single TypComment token covering `#`..end-of-line, while a `#` glued to
// a non-space (`Agent #1`) produces no comment token.
func TestCollect_trailingComment(t *testing.T) {
	src := "e1 ::e --> e2 ::e   # leads-to inferred from event\n"
	var comments []string
	for _, tk := range Collect(src, src, 0) {
		if tk.Type == TypComment {
			comments = append(comments, tokenText(src, tk))
		}
	}
	want := "# leads-to inferred from event"
	if len(comments) != 1 || comments[0] != want {
		t.Errorf("want one comment token %q; got %v", want, comments)
	}

	// A `#` glued to a non-space stays literal — no comment token.
	literal := "Agent #1 --> Event #1 ::e\n"
	for _, tk := range Collect(literal, literal, 0) {
		if tk.Type == TypComment {
			t.Errorf("unexpected comment token for literal `#1`: %q", tokenText(literal, tk))
		}
	}
}

// TestCollect_markerInsideTrailingCommentIsNotAMarker extends the
// context-blindness guard to a `::e` written inside a trailing comment: it
// must not be mis-highlighted as an event marker.
func TestCollect_markerInsideTrailingCommentIsNotAMarker(t *testing.T) {
	src := "lunch ::e   # also mentions ::e in the note\n"
	markers := 0
	for _, tk := range Collect(src, src, 0) {
		if tk.Mods&ModMarker == 0 {
			continue
		}
		markers++
		// The real ::e ends before the comment (col < 12); any marker at or
		// past the `#` came from the comment text.
		if tk.StartCol >= 12 {
			t.Errorf("kind marker emitted inside the trailing comment: %q", tokenText(src, tk))
		}
	}
	if markers != 1 {
		t.Errorf("want exactly 1 kind marker (the real ::e); got %d", markers)
	}
}

// TestCollect_markerInsideCommentIsNotAMarker guards the context-blindness
// fix: with markers sourced from the parser (which never tokenizes a
// comment's contents), a `::e` written inside a `#` comment is no longer
// mis-highlighted as an event marker. The old global kindMarkerRE matched
// it regardless of context.
func TestCollect_markerInsideCommentIsNotAMarker(t *testing.T) {
	src := "# a note mentioning ::e here\nlunch ::e\n"
	markers := 0
	for _, tk := range Collect(src, src, 0) {
		if tk.Mods&ModMarker == 0 {
			continue
		}
		markers++
		if tk.Line == 0 {
			t.Errorf("kind marker emitted inside the comment line: %q", tokenText(src, tk))
		}
	}
	if markers != 1 {
		t.Errorf("want exactly 1 kind marker (the real ::e on line 1); got %d", markers)
	}
}

// TestCollect_nonEndpointAliasReferenceIsColored covers the alias-ref case
// the old global regex handled poorly: a reference that is NOT an edge
// endpoint — here the `swapT` subject of a node-tooltip statement. It must
// still color as the alias (the parser positions it precisely, even
// line-leading).
func TestCollect_nonEndpointAliasReferenceIsColored(t *testing.T) {
	src := "Patrick swaps ::e swapT::a\nswapT \"a quick move\" ::tip\n"
	found := false
	for _, tk := range Collect(src, src, 0) {
		if tk.Line == 1 && tk.Type == TypEvent && tk.Mods&ModAlias != 0 && tokenText(src, tk) == "swapT" {
			found = true
		}
	}
	if !found {
		t.Error("alias reference `swapT` in a node-tooltip statement was not colored as e-alias")
	}
}

// TestCollect_multiWordEdgeTooltipIsOneToken guards the edgeTooltipRE
// removal: the arrow-span walker now scans a quoted edge tooltip to its
// closing quote, so a multi-word tooltip is a single token rather than
// being split on its interior spaces.
func TestCollect_multiWordEdgeTooltipIsOneToken(t *testing.T) {
	src := "A ::e --\"why this matters\"--> B ::e\n"
	var tips []string
	for _, tk := range Collect(src, src, 0) {
		if tk.Type == TypTooltip {
			tips = append(tips, tokenText(src, tk))
		}
	}
	if len(tips) != 1 || tips[0] != "\"why this matters\"" {
		t.Errorf("want one tooltip token `\"why this matters\"`; got %v", tips)
	}
}

// TestCollect_markerInsideQuotedTitleIsNotAMarker is the string-context
// half of the same fix: a `::e` inside a quoted title is string content,
// so only the real `::c` (outside the quotes) is a kind marker.
func TestCollect_markerInsideQuotedTitleIsNotAMarker(t *testing.T) {
	src := "\"a title with ::e inside\" ::c\n"
	var markerTexts []string
	for _, tk := range Collect(src, src, 0) {
		if tk.Mods&ModMarker != 0 {
			markerTexts = append(markerTexts, tokenText(src, tk))
		}
	}
	if len(markerTexts) != 1 || markerTexts[0] != "::c" {
		t.Errorf("want exactly one kind marker `::c`; got %v", markerTexts)
	}
}
